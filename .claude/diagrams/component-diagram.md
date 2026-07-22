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

```mermaid
graph TB
    subgraph HTTP["Gin router · ForwardAuth + RequireScope"]
        MW["Idempotency middleware<br/>bọc các POST tạo dữ liệu"]
        H1["ServerHandler<br/>CRUD · stats · uptime"]
        H2["ImportHandler POST /import"]
        H3["ExportHandler POST /export"]
        H4["InternalHandler<br/>GET /internal/servers"]
    end

    subgraph SVC["internal/service"]
        S1["ServerService"]
        S2["ImportService<br/>validate → lọc trùng → chèn hàng loạt"]
        S3["ExportService"]
    end

    subgraph SUP["Thành phần hỗ trợ"]
        V["validator · CIDR allowlist"]
        XP["excel.Parser"]
        XG["excel.Generator"]
        LC["status.LastCheckReader"]
        UR["status.UptimeReader"]
        CA["cache · list version"]
    end

    subgraph REPO["internal/repository"]
        R1["ServerRepository<br/>ON CONFLICT resurrect"]
        R2["IdempotencyRepository"]
    end

    subgraph BG["Goroutine nền"]
        PJ["projection.TargetProjection<br/>Sync · Delete · Rebuild"]
        CS["consumer.StatusConsumer<br/>readLoop + reclaimLoop"]
    end

    subgraph INFRA["Hạ tầng"]
        PG[("server_db")]
        RD[("Redis db1")]
    end

    MW --> H1
    MW --> H2
    H1 --> S1
    H2 --> S2 --> XP
    H3 --> S3 --> XG
    H4 --> R1

    S1 --> V
    S2 --> V
    S1 --> LC
    S1 --> UR
    S3 --> LC
    S1 --> CA

    S1 --> R1
    S2 --> R1
    S3 --> R1
    MW --> R2
    R1 --> PG
    R2 --> PG

    S1 -->|"mọi thay đổi server"| PJ
    S2 --> PJ
    PJ -->|"W server:monitor-target:*"| RD
    LC -->|"R monitor:status:*"| RD
    UR -->|"R monitor:uptime:index"| RD
    CA --> RD

    CS -.->|"XREADGROUP<br/>stream:monitor.status"| RD
    CS -->|"ApplyStatusEvent<br/>version guard"| R1
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

```mermaid
graph TB
    subgraph LOOPS["4 goroutine chạy song song, cùng vòng đời"]
        SCH["① Scheduler<br/>1 vòng / 60s<br/>giành round lock"]
        POOL["② Pool · 200 worker<br/>BRPOP → ping → ApplyStatus"]
        FB["③ FactBuffer<br/>flush 1000 fact hoặc 5s"]
        SMP["④ Sampler<br/>lấy mẫu mỗi giây"]
    end

    subgraph HELP["Thành phần hỗ trợ"]
        OPS["RedisOps<br/>bọc toàn bộ lệnh Redis"]
        PING["TCPPinger<br/>net.Dialer + phân loại lỗi"]
        LUA["statusScript (Lua)"]
        MET["Metrics · 7 chỉ số Prometheus"]
        FW["FactWriter · ES bulk"]
    end

    subgraph INFRA["Hạ tầng"]
        RD[("Redis db1")]
        ES[("Elasticsearch")]
        SIM["tcp-simulator"]
    end

    HTTP["Gin: /health · /metrics"]

    SCH --> OPS
    POOL --> OPS
    SMP --> OPS
    OPS --> RD
    OPS -->|"EVALSHA"| LUA --> RD

    POOL --> PING --> SIM
    POOL -.->|"Add(fact)"| FB
    FB --> FW --> ES

    SCH --> MET
    POOL --> MET
    FB --> MET
    SMP --> MET
    MET --> HTTP
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

```mermaid
graph TB
    subgraph HTTP["Gin router"]
        H1["ReportHandler<br/>GET /summary · POST / · GET /:id"]
        H2["POST /internal/snapshots/:date<br/>chạy lại snapshot thủ công"]
    end

    subgraph CRON["⏰ robfig/cron · WithLocation(Asia/Ho_Chi_Minh)"]
        C1["00:30 → snapshot hôm qua"]
        C2["10:00 → gửi báo cáo hôm qua"]
    end

    subgraph SVC["internal/service"]
        S1["ReportService<br/>ParseRange · Summary · ServerUptimeRows"]
        S2["SendService<br/>FSM: processing → generated<br/>→ sending → sent"]
    end

    JOBN["snapshot.Job.Run(date)<br/>population LEFT JOIN facts"]

    subgraph OUT["Xuất bản"]
        RN["email.Renderer<br/>daily_report.html"]
        XG["excel.Generator<br/>4 cột · chống formula injection"]
        SN["email.GmailSender<br/>STARTTLS · multipart/mixed"]
    end

    subgraph REPO["internal/repository + client"]
        R1["SnapshotRepository<br/>Totals · LowestUptime<br/>AllServerUptime · MissingDates"]
        R2["JobRepository"]
        R3["UptimeAggregator (ES)<br/>composite agg + after_key"]
        CL["ServerClient · HTTP nội bộ"]
    end

    subgraph INFRA["Hạ tầng"]
        PG[("report_db")]
        ES[("Elasticsearch")]
        SS["server-service<br/>/internal/servers"]
        SMTP["Gmail SMTP"]
    end

    H1 --> S1
    H1 --> S2
    H2 --> JOBN
    C1 --> JOBN
    C2 --> S2

    JOBN --> CL --> SS
    JOBN --> R3 --> ES
    JOBN --> R1

    S1 --> R1 --> PG
    S2 --> S1
    S2 --> R2 --> PG
    S2 --> RN
    S2 --> XG
    S2 --> SN --> SMTP
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
