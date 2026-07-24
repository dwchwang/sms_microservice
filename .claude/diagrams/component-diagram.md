# 🧩 Sơ đồ thành phần — bên trong từng service

> Cập nhật: 24/07/2026 · Ánh xạ 1-1 với thư mục `internal/` của từng service.

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
        RD[("Redis db0<br/>auth:refresh · auth:blacklist<br/>auth:login_attempts")]
    end

    JWT["shared/pkg/jwt"]

    H1 --> S1 --> S2
    S1 --> S3 --> RD
    S1 --> R1 --> PG
    S1 -->|"blacklist khi logout"| RD
    H2 -->|"ValidateToken<br/>KHÔNG chạm DB"| JWT
```

**Điểm đáng chú ý:** `VerifyHandler` nằm trên đường đi của **mọi** request có xác thực, nên nó chỉ kiểm chữ ký JWT trong bộ nhớ — không truy vấn PostgreSQL. Scope được nhúng sẵn trong token lúc login.

**Ba key trong db0** (tên đầy đủ, để `redis-cli` khỏi tìm sai):

| Key | Giá trị | TTL |
|---|---|---|
| `auth:refresh:{jti}` | `user_id` | `JWT_REFRESH_EXPIRY_DAYS` (7d) |
| `auth:blacklist:{jti}` | `"revoked"` | thời gian còn lại của access token |
| `auth:login_attempts:{email}` | số lần sai | 15 phút; khoá khi ≥ 5 |

`refresh` có **rotation**: mỗi `POST /auth/refresh` xoá jti cũ và ghi jti mới, nên một
refresh token chỉ dùng được đúng một lần.

**Mật khẩu: Argon2id, với đường di trú từ bcrypt.** `HashPassword` luôn sinh Argon2id
(`m=64MB, t=1, p=4`). `VerifyPassword` nhận **cả** `$2a$`/`$2b$` (bcrypt) lẫn
`$argon2id$`, và khi khớp một hash bcrypt thì trả `needsRehash = true` để `Login`
lặng lẽ nâng cấp hash trong DB. Admin seed trong `init.sql` là bcrypt, và nó chuyển
thành Argon2id ngay sau lần đăng nhập đầu tiên.

> `internal/service/login_guard.go` định nghĩa một `LoginGuard` làm đúng việc mà
> `authServiceImpl.checkLoginAttempts` / `recordFailedAttempt` đang làm. Hiện tại
> **không ai gọi** `LoginGuard` — sơ đồ trên vẽ theo đường thật sự chạy.

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
| `vcs_monitor_round_duration_seconds` | vòng chạy hết bao lâu | tiến sát 60s |
| `vcs_monitor_targets_expected` | số target đã nạp | lệch số server thật |
| `vcs_monitor_checks_completed_total` | số ping instance này làm | — |
| `vcs_monitor_checks_missing` | queue còn thừa khi vòng kết thúc | **> 0 kéo dài → thiếu worker** |
| `vcs_monitor_queue_depth` | độ sâu hàng đợi hiện tại | không về 0 |
| `vcs_monitor_tcp_latency_seconds` | độ trễ TCP connect | đuôi phân phối tăng |
| `vcs_monitor_es_bulk_failure_total` | batch bị bỏ sau khi retry | > 0 |

Image của monitor là distroless (không có shell, không có `wget`/`curl`), nên đọc
metrics phải đi từ một container khác trên cùng network:

```bash
docker exec vcs-sms-traefik wget -qO- http://monitor-service:8083/metrics | grep vcs_monitor
```

---

## 4. report-service — `:8084` (service duy nhất chạy nhiều replica có state)

Hai công việc chính: **snapshot** cô đọng dữ liệu của ngày hôm qua, **send** đọc rồi
gửi mail. Cả hai do `scheduler.Scheduler` điều phối, và `Scheduler` chạy trên **mọi**
replica.

```mermaid
graph LR
    H["HTTP handlers"] --> RS["ReportService<br/>ParseRange · Summary"]
    H --> SEND["SendService<br/>FSM gửi mail"]

    SCH["scheduler.Scheduler<br/>tick 60s · mọi replica"]
    SCH -->|"TryClaim / Heartbeat<br/>MarkDone / MarkFailed"| CR[("cron_runs")]
    SCH -->|"thắng claim: snapshot"| JOB["snapshot.Job<br/>population ⟕ facts"]
    SCH -->|"thắng claim: daily_report"| SEND

    JOB -->|"population"| SS["server-service<br/>/internal/servers"]
    JOB -->|"composite agg"| ES[("Elasticsearch")]
    JOB -->|"UPSERT theo lô 500"| PG[("report_db")]

    RS --> PG
    SEND --> RS
    SEND --> OUT["Renderer + Excel<br/>→ Gmail SMTP"]
```

### Vì sao Scheduler *reconcile* mỗi phút thay vì nổ đúng khoảnh khắc cron

`robfig/cron` được dùng **chỉ để parse biểu thức**, không để đăng ký callback. Vòng lặp
thật là: mỗi 60 giây, với mỗi job, hỏi ba câu.

```mermaid
graph LR
    T["tick 60s"] --> D{"due(schedule, now)?<br/>giờ nổ hôm nay ĐÃ qua chưa"}
    D -->|"chưa"| SKIP["bỏ qua"]
    D -->|"rồi"| DEP{"job phụ thuộc<br/>đã done chưa?"}
    DEP -->|"chưa"| SKIP
    DEP -->|"rồi"| CLAIM{"TryClaim(job, run_date)"}
    CLAIM -->|"thua"| SKIP
    CLAIM -->|"thắng"| RUN["runClaimed:<br/>heartbeat 30s song song"]
```

Nổ theo callback thì một replica boot lúc 10:05 sẽ **không bao giờ** biết job 10:00 chưa
ai làm. Reconcile thì nó thấy `due` = true, `cron_runs` chưa có dòng nào cho ngày đó, và
tự nhận việc. Đây là điều làm cho một lần deploy giữa giờ cron không mất báo cáo.

| Tham số | Giá trị | Ý nghĩa |
|---|---|---|
| `tickInterval` | 60s | nhịp reconcile |
| `heartbeatInterval` | 30s | làm mới claim khi đang chạy |
| `staleAfter` | 3 phút | 6 nhịp heartbeat bị bỏ → replica khác được cướp claim |
| `snapshotTimeout` | 1 giờ | ngân sách cho job snapshot |
| `dailyTimeout` | 10 phút | ngân sách cho job gửi mail |

`beat()` không chỉ làm mới claim — nếu `Heartbeat` trả về "anh không còn giữ nó nữa",
nó **cancel context của chính job đang chạy**. Nhờ vậy hai replica không bao giờ cùng
ghi một `run_date`, kể cả trong tình huống replica cũ chỉ bị treo mạng chứ chưa chết.

**`run_date` luôn là *hôm qua*.** Cả hai job đều làm việc với ngày đã kết thúc:
`runDate = startOfDay(now) - 1 day`. Đó cũng là khoá claim, nên một job chỉ chạy đúng
một lần cho mỗi ngày **dữ liệu**, bất kể có bao nhiêu replica hay bao nhiêu lần restart.

**Thứ tự hai cron là bắt buộc, không phải ngẫu nhiên:** `daily_report` khai báo
`dependsOn: snapshot` và mỗi tick đều kiểm `IsDone(snapshot, date)`, nên nó không thể
đọc một ngày mà snapshot chưa cô đọng. Mặc định `30 0 * * *` → `0 10 * * *` để lại
9,5 giờ đệm, đủ để chạy lại thủ công qua `POST /internal/snapshots/{date}` nếu job đêm hỏng.

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
        M2["pkg/jwt<br/>Generate/ValidateToken · Claims"]
        M3["pkg/confighelper<br/>đọc *_FILE (Docker secret)"]
        M4["logger · zerolog + lumberjack"]
        M5["response · errors · validator"]
        M6["timezone · Asia/Ho_Chi_Minh"]
        M7["pkg/auth/scopes<br/>hằng số tên scope"]
    end

    AUTH["auth-service"] --> M1
    AUTH --> M2
    AUTH --> M7
    SRV["server-service"] --> M1
    REP["report-service"] --> M1
    MON["monitor-service"] --> M4
    MON --> M6
    AUTH --> M4
    SRV --> M4
    REP --> M4
    REP --> M6
    SRV --> M5
    REP --> M5
    AUTH --> M3
    SRV --> M3
    MON --> M3
    REP --> M3
```

`AuthFromForwardAuth()` đọc header `X-User-Id` / `X-User-Scopes` mà Traefik tiêm vào; `RequireScope("...")` so khớp scope. Đây chính là lý do bốn service không được publish port ra host.

**`timezone` được cả monitor và report dùng, không chỉ report.** Monitor cần nó để tính
field `day` của bộ đếm uptime; report cần nó cho ranh giới ngày và lịch cron. Một hàm
`Load()` duy nhất là cách để hai bên không có hai định nghĩa "hôm nay" khác nhau.

**`pkg/confighelper`** đọc cặp `X` / `X_FILE`: nếu `REDIS_PASSWORD_FILE` được set thì
giá trị lấy từ nội dung file. Đây là cầu nối để `docker-stack.yml` dùng Docker secret
mà không phải sửa code config của service nào.
