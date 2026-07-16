# ⚡ Sequence Diagrams — Các luồng vận hành chính

> **Ngày tạo:** 09/06/2026
> **Mô tả:** 6 Sequence Diagrams mô tả các luồng nghiệp vụ cốt lõi của VCS-SMS.

---

## 1. Health-Check Batch Cycle (60 giây / lần)

```mermaid
sequenceDiagram
    autonumber
    participant Cron as ⏰ Cron Scheduler
    participant Monitor as 📡 Monitor Service
    participant Redis as ⚡ Redis
    participant PG as 🐘 PostgreSQL
    participant ES as 🔍 Elasticsearch
    participant Kafka as 📨 Kafka

    Note over Cron,Kafka: 🔄 Chu kỳ mỗi 60 giây

    Cron->>Monitor: Trigger health-check cycle
    Monitor->>Redis: AcquireLock("health-check-lock", TTL=90s)

    alt 🔑 Lock acquired
        Monitor->>PG: SELECT * FROM server_schema.servers<br/>WHERE deleted_at IS NULL
        PG-->>Monitor: 10,000 servers (ipv4="tcp-simulator")

        Monitor->>Monitor: Spawn 100 Worker Goroutines

        rect rgb(230, 245, 255)
            Note over Monitor: Worker Pool — 100 goroutines
            loop Mỗi server
                Monitor->>Monitor: TCPChecker.Check(server)<br/>net.DialTimeout("tcp", "tcp-simulator:port", 5s)

                alt Port đang mở (ON)
                    Monitor->>Monitor: HealthResult{Status: "on"}
                else Port đang đóng (OFF)
                    Monitor->>Monitor: HealthResult{Status: "off"}
                end

                Monitor->>Redis: GET server:status:{server_id}
                Redis-->>Monitor: old_status

                alt Status thay đổi
                    Monitor->>Monitor: Thêm vào statusChanges[]
                end

                Monitor->>Redis: SET server:status:{server_id} = new (TTL=90s)
            end
        end

        Monitor->>ES: Bulk Index 10,000 documents → server-status-logs
        ES-->>Monitor: 200 OK

        Monitor->>PG: Batch UPDATE (chỉ server thay đổi status)
        PG-->>Monitor: rows affected

        Monitor->>Kafka: Publish "server.health.batch"
        loop Mỗi server thay đổi
            Monitor->>Kafka: Publish "server.status.changed"
        end

        Monitor->>Redis: ReleaseLock("health-check-lock")

    else 🔒 Lock NOT acquired
        Monitor->>Monitor: Skip (another instance running)
    end
```

---

## 2. Import Excel — Bất đồng bộ qua Kafka

```mermaid
sequenceDiagram
    autonumber
    participant Client as 👤 Client
    participant GW as 🚪 API Gateway
    participant FileIO as 📁 File I/O Service
    participant Kafka as 📨 Kafka
    participant PG as 🐘 PostgreSQL

    Note over Client,PG: 📥 Import Excel — Async

    Client->>GW: POST /api/v1/servers/import<br/>Content-Type: multipart/form-data<br/>file: servers.xlsx
    GW->>GW: JWT verify + scope check (server:import)
    GW->>FileIO: Forward request

    FileIO->>FileIO: Validate file format (.xlsx)

    alt File hợp lệ
        FileIO->>FileIO: Save to /uploads/{uuid}.xlsx
        FileIO->>PG: INSERT INTO import_jobs (status='pending')
        FileIO->>Kafka: Publish "import.job.created"
        FileIO-->>Client: 202 Accepted<br/>{job_id, status: "pending"}
    else File không hợp lệ
        FileIO-->>Client: 400 Bad Request
    end

    Note over Kafka,PG: ⚙️ Consumer xử lý bất đồng bộ

    Kafka->>FileIO: Consume "import.job.created"
    FileIO->>PG: UPDATE import_jobs SET status='processing'
    FileIO->>FileIO: Parse Excel (excelize)

    loop Mỗi row trong Excel
        FileIO->>FileIO: Validate row data

        alt Data hợp lệ
            FileIO->>PG: SELECT FROM servers<br/>WHERE server_id=? OR server_name=?

            alt Không trùng
                FileIO->>PG: INSERT INTO servers
                FileIO->>PG: INSERT INTO import_job_details (success)
                FileIO->>Kafka: Publish "server.created"
            else Trùng server_id/name
                FileIO->>PG: INSERT INTO import_job_details (failed, 'duplicate')
            end
        else Data không hợp lệ
            FileIO->>PG: INSERT INTO import_job_details (failed)
        end
    end

    FileIO->>PG: UPDATE import_jobs SET status='completed'

    Note over Client,FileIO: 👤 Client poll kết quả

    Client->>GW: GET /api/v1/servers/import/{job_id}
    GW->>FileIO: Forward
    FileIO->>PG: SELECT FROM import_jobs JOIN import_job_details
    FileIO-->>Client: 200 OK<br/>{status: "completed", success: 480, failed: 20}
```

---

## 3. Export Excel — Đồng bộ

```mermaid
sequenceDiagram
    autonumber
    participant Client as 👤 Client
    participant GW as 🚪 API Gateway
    participant FileIO as 📁 File I/O Service
    participant PG as 🐘 PostgreSQL

    Note over Client,PG: 📤 Export Excel — Sync

    Client->>GW: POST /api/v1/servers/export<br/>{filter: {status: "on"}, sort_by: "server_name"}
    GW->>GW: JWT verify + scope check (server:export)
    GW->>FileIO: Forward request

    FileIO->>PG: SELECT FROM server_schema.servers<br/>WHERE status='on' AND deleted_at IS NULL<br/>ORDER BY server_name ASC
    PG-->>FileIO: Result set

    FileIO->>FileIO: Generate Excel (excelize)<br/>Header: server_id, name, status, ipv4, os, cpu, ram...
    FileIO->>FileIO: Fill data rows from query result

    FileIO-->>Client: 200 OK<br/>Content-Type: application/vnd.openxmlformats...<br/>Content-Disposition: attachment<br/>[Binary .xlsx file]
```

---

## 4. Daily Report Email — Cron 08:00 AM

```mermaid
sequenceDiagram
    autonumber
    participant Cron as ⏰ Cron (08:00 AM)
    participant Report as 📊 Report Service
    participant PG as 🐘 PostgreSQL
    participant ES as 🔍 Elasticsearch
    participant Redis as ⚡ Redis
    participant SMTP as 📧 Gmail SMTP

    Note over Cron,SMTP: 📊 Báo cáo định kỳ — Mỗi ngày 1 lần

    Cron->>Report: Trigger daily report

    Report->>PG: INSERT INTO report_jobs<br/>{type='daily', status='processing',<br/>start_date=yesterday, end_date=yesterday}

    Report->>ES: Aggregation Query<br/>(server-status-logs, yesterday 00:00 → 23:59)

    Note over ES: 🔢 Tính toán:<br/>- per_server uptime_rate<br/>- avg_uptime toàn hệ thống<br/>- status_counts (on/off)

    ES-->>Report: {avg_uptime: 97.85, on: 9523, off: 477,<br/>low_uptime_servers: [...]}

    Report->>PG: SELECT COUNT(*) FROM servers<br/>WHERE deleted_at IS NULL
    PG-->>Report: total = 10000

    Report->>Report: Build HTML email template

    Report->>SMTP: Send via Gmail SMTP (TLS :587)

    alt ✅ Gửi thành công
        SMTP-->>Report: 250 OK
        Report->>PG: UPDATE report_jobs SET status='completed'
        Report->>PG: INSERT INTO daily_snapshots
        Report->>Redis: SET report:summary:yesterday (TTL=24h)
    else ❌ Gửi thất bại
        SMTP-->>Report: Error
        Report->>PG: UPDATE report_jobs SET status='failed'
        Report->>Report: Log error, retry (max 3)
    end
```

---

## 5. On-Demand Report API

```mermaid
sequenceDiagram
    autonumber
    participant Client as 👤 Client
    participant GW as 🚪 API Gateway
    participant Report as 📊 Report Service
    participant Redis as ⚡ Redis
    participant ES as 🔍 Elasticsearch
    participant PG as 🐘 PostgreSQL
    participant SMTP as 📧 Gmail SMTP

    Note over Client,SMTP: 📊 Báo cáo chủ động — User trigger

    Client->>GW: POST /api/v1/reports<br/>{start_date, end_date, email}
    GW->>GW: JWT verify + scope check (report:send)
    GW->>Report: Forward

    Report->>Report: Validate (dates, email format)

    Report->>Redis: GET report:summary:{start}:{end}

    alt 💾 Cache hit
        Redis-->>Report: Cached summary
    else ⚙️ Cache miss
        Report->>ES: Aggregation Query (start → end)
        ES-->>Report: Results
        Report->>PG: SELECT COUNT(*) FROM servers
        PG-->>Report: total
        Report->>Redis: SET report:summary:{start}:{end} (TTL=1h)
    end

    Report->>PG: INSERT INTO report_jobs (type='on_demand', ...)

    Report->>Report: Build HTML email
    Report->>SMTP: Send email
    SMTP-->>Report: 250 OK

    Report->>PG: UPDATE report_jobs SET status='completed'

    Report-->>Client: 200 OK<br/>{message: "Report sent", summary: {...}}
```

---

## 6. JWT Authentication Flow

```mermaid
sequenceDiagram
    autonumber
    participant Client as 👤 Client
    participant GW as 🚪 API Gateway
    participant Auth as 🔐 Auth Service
    participant PG as 🐘 PostgreSQL
    participant Redis as ⚡ Redis

    Note over Client,Redis: 🔐 Đăng nhập

    Client->>GW: POST /api/v1/auth/login<br/>{username, password}
    GW->>Auth: Forward (public route)

    Auth->>PG: SELECT FROM users<br/>JOIN roles JOIN role_permissions<br/>WHERE username = ?
    PG-->>Auth: User + Role + Scopes

    Auth->>Auth: Verify password (bcrypt.Compare)

    alt ✅ Password đúng
        Auth->>Auth: Generate Access Token (JWT, 15min)<br/>{user_id, username, role, scopes, jti}
        Auth->>Auth: Generate Refresh Token (JWT, 7d)
        Auth->>Redis: SET refresh:{jti} = user_id (TTL=7d)
        Auth->>PG: UPDATE users SET last_login_at = NOW()
        Auth-->>Client: 200 OK<br/>{access_token, refresh_token}
    else ❌ Password sai
        Auth-->>Client: 401 Unauthorized
    end

    Note over Client,Redis: 🔒 Authenticated Request

    Client->>GW: GET /api/v1/servers<br/>Authorization: Bearer {access_token}

    GW->>GW: Extract JWT from header
    GW->>Redis: GET auth:blacklist:{jti}

    alt Token NOT blacklisted
        GW->>GW: Verify JWT signature + expiry
        GW->>GW: Extract scopes from claims
        GW->>GW: Check "server:read" scope

        alt ✅ Authorized
            GW->>GW: Inject user_id, scopes → header
            GW->>GW: Reverse Proxy → server-service:8082
        else ❌ Forbidden
            GW-->>Client: 403 Forbidden
        end
    else Token revoked
        GW-->>Client: 401 Unauthorized
    end

    Note over Client,Redis: 🚪 Đăng xuất

    Client->>GW: POST /api/v1/auth/logout
    GW->>Auth: Forward
    Auth->>Redis: SET auth:blacklist:{jti} = 1<br/>(TTL = remaining token TTL)
    Auth->>Redis: DEL refresh:{refresh_jti}
    Auth-->>Client: 200 OK {message: "Logged out"}
```
