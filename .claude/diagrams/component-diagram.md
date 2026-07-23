# 🧩 Sơ đồ thành phần — bên trong từng service

> Cập nhật: 21/07/2026 · Ánh xạ 1-1 với thư mục `internal/` của từng service.

Tất cả service theo cùng một mạch: **handler → service → repository → hạ tầng**, cộng thêm các goroutine nền chạy song song với HTTP server.

---

## 1. auth-service — `:8081`

```mermaid
graph TB
    subgraph HTTP["Gin router"]
        H1["AuthHandler<br/>register · login · refresh<br/>logout · profile · users"]
        H2["VerifyHandler<br/>GET /internal/verify"]
    end

    subgraph SVC["internal/service"]
        S1["AuthService<br/>xác thực · sinh token · đổi role"]
        S2["HashPassword / VerifyPassword"]
        S3["checkLoginAttempts<br/>chống brute-force"]
    end

    subgraph REPO["internal/repository"]
        R1["UserRepository<br/>users + roles + role_permissions"]
    end

    subgraph INFRA["Hạ tầng"]
        PG[("identity_db")]
        RD[("Redis db0<br/>blacklist:jti · login:attempts")]
    end

    JWT["shared/pkg/jwt"]

    H1 --> S1 --> S2
    S1 --> S3 --> RD
    S1 --> R1 --> PG
    S1 -->|"blacklist khi logout"| RD
    H2 -->|"ValidateToken<br/>KHÔNG chạm DB"| JWT
```

**Điểm đáng chú ý:** `VerifyHandler` nằm trên đường đi của **mọi** request có xác thực, nên nó chỉ kiểm chữ ký JWT trong bộ nhớ — không truy vấn PostgreSQL. Scope được nhúng sẵn trong token lúc login.

---

## 2. server-service — `:8082` (service phức tạp nhất)

Service này chạy **ba dòng độc lập** cùng lúc. Vẽ tách theo dòng cho dễ đọc:

```mermaid
graph LR
    subgraph F1["① HTTP · người dùng"]
        H["Handlers<br/>CRUD · Import · Export · /internal"]
        S["Services<br/>+ validator/CIDR · excel · cache"]
        H --> S
    end

    subgraph F2["② Projection · mọi thay đổi server"]
        PJ["TargetProjection<br/>Sync · Delete · Rebuild"]
    end

    subgraph F3["③ Consumer · event từ monitor"]
        CS["StatusConsumer<br/>read + reclaim loop"]
    end

    R["Repository<br/>ServerRepo · IdempotencyRepo"]
    PG[("server_db")]
    RD[("Redis db1")]

    S --> R --> PG
    S -->|"mọi thay đổi"| PJ
    PJ -->|"W target projection"| RD
    S -->|"R status/uptime · cache"| RD
    CS -. "XREADGROUP stream" .-> RD
    CS -->|"ApplyStatusEvent · version guard"| R
```

**Ba dòng chảy độc lập bên trong service này:**

| Dòng | Kích hoạt bởi | Làm gì |
|------|---------------|--------|
| **HTTP** | người dùng | CRUD, import, export |
| **Projection** | mọi thay đổi server | đồng bộ danh sách target sang Redis cho Monitoring |
| **Consumer** | Monitoring đẩy event | cập nhật `servers.status` trong PostgreSQL |

Consumer chạy trong chính process này (goroutine khởi động ở `cmd/main.go`), dừng cùng lúc với HTTP server khi nhận SIGTERM.

`cmd/main.go` còn có một chế độ chạy phụ: `server-service rebuild-monitor-cache` — nạp lại toàn bộ projection rồi thoát, dùng khi Redis bị xoá sạch.

---

## 3. monitor-service — `:8083` (bốn goroutine song song)

Bốn goroutine chạy song song, cùng vòng đời. Tất cả cùng cập nhật `Metrics` (7 chỉ số
Prometheus tại `/metrics`) — lược khỏi sơ đồ cho gọn:

```mermaid
graph LR
    subgraph LOOPS["4 goroutine song song"]
        SCH["① Scheduler · 60s"]
        POOL["② Pool · 200 worker"]
        FB["③ FactBuffer"]
        SMP["④ Sampler · 1s"]
    end

    RD[("Redis db1")]
    ES[("Elasticsearch")]
    SIM["tcp-simulator"]

    SCH -->|"nạp queue"| RD
    SMP -->|"đo queue"| RD
    POOL -->|"BRPOP + Lua EVALSHA"| RD
    POOL -->|"TCP ping"| SIM
    POOL -. "Add(fact)" .-> FB
    FB -->|"bulk"| ES
```

**Vì sao Scheduler chạy trên *mọi* instance nhưng chỉ một instance nạp hàng đợi?**
`SETNX monitor:round:lock:{round}` — ai thắng thì nạp queue. Instance thua **vẫn** chạy Pool và vẫn ping, nên thêm instance là thêm năng lực ping chứ không nhân đôi công việc.

**Bảy chỉ số Prometheus:**

| Chỉ số | Ý nghĩa | Báo động khi |
|--------|---------|--------------|
| `round_duration_seconds` | vòng chạy hết bao lâu | tiến sát 60s |
| `targets_expected` | số target đã nạp | lệch số server thật |
| `checks_completed_total` | số ping instance này làm | — |
| `checks_missing` | queue còn thừa khi vòng kết thúc | **> 0 kéo dài → thiếu worker** |
| `queue_depth` | độ sâu hàng đợi hiện tại | không về 0 |
| `tcp_latency_seconds` | độ trễ TCP connect | đuôi phân phối tăng |
| `es_bulk_failure_total` | batch bị bỏ sau khi retry | > 0 |

---

## 4. report-service — `:8084`

Hai công việc chính: **snapshot** (cron 00:30) cô đọng dữ liệu, **send** (cron 10:00
hoặc người dùng) đọc rồi gửi mail.

```mermaid
graph LR
    H["HTTP handlers"] --> RS["ReportService<br/>ParseRange · Summary"]
    H --> SEND["SendService<br/>FSM gửi mail"]
    C1["⏰ 00:30"] --> JOB["snapshot.Job<br/>population ⟕ facts"]
    C2["⏰ 10:00"] --> SEND

    JOB -->|"population"| SS["server-service<br/>/internal/servers"]
    JOB -->|"composite agg"| ES[("Elasticsearch")]
    JOB -->|"UPSERT"| PG[("report_db")]

    RS --> PG
    SEND --> RS
    SEND --> OUT["Renderer + Excel<br/>→ Gmail SMTP"]
```

**Thứ tự hai cron là bắt buộc, không phải ngẫu nhiên:** báo cáo chỉ đọc được thứ mà snapshot đã ghi. 00:30 → 10:00 để lại 9,5 giờ đệm, đủ để chạy lại thủ công nếu job đêm hỏng.

---

## 5. Đính kèm Excel — luồng dữ liệu chi tiết

```mermaid
graph LR
    A["SendService.Send()"] --> B["Summary() thành công<br/>⇒ coverage đã được kiểm"]
    B --> C["ServerUptimeRows(start, end)"]
    C --> D["AllServerUptime<br/>SQL: per_server + latest_name"]
    D --> E["[]ServerUptimeRow<br/>id · name · total_checks · uptime_pct"]
    E --> F["excel.Generate()<br/>sheet 'Uptime'"]
    F --> G["email.Attachment<br/>uptime_YYYY-MM-DD.xlsx"]
    G --> H["compose() multipart/mixed<br/>base64 xuống dòng 76 ký tự"]

    X["Log WARN<br/>GỬI MAIL KHÔNG ĐÍNH KÈM"]
    D -.->|"lỗi"| X
    F -.->|"lỗi"| X
```

Đính kèm là **phụ trợ**: hỏng file Excel không được phép làm mất báo cáo mà phần thân email đã mang.

`uptime_pct` là con trỏ `*float64` — `nil` nghĩa là *không ai đo được* (ô trống trong Excel), khác hẳn `0` nghĩa là *server chết cả ngày*.

---

## 6. shared/ — module dùng chung

```mermaid
graph LR
    subgraph SH["shared/"]
        M1["middleware<br/>RequestID · Logger<br/>AuthFromForwardAuth · RequireScope"]
        M2["pkg/jwt<br/>GenerateToken · ValidateToken"]
        M4["logger · zerolog + lumberjack"]
        M5["response · errors · validator"]
    end

    AUTH["auth-service"] --> M1
    AUTH --> M2
    SRV["server-service"] --> M1
    REP["report-service"] --> M1
    MON["monitor-service"] --> M4
    AUTH --> M4
    SRV --> M4
    REP --> M4
    SRV --> M5
    REP --> M5
```

`AuthFromForwardAuth()` đọc header `X-User-Id` / `X-User-Scopes` mà Traefik tiêm vào; `RequireScope("...")` so khớp scope. Đây chính là lý do bốn service không được publish port ra host.
