# 🏗️ Sơ đồ kiến trúc — VCS-SMS

> Cập nhật: 21/07/2026 — viết lại theo mã nguồn thực tế trong `server-management-system/`.

---

## 1. System Context — hệ thống nhìn từ bên ngoài

```mermaid
graph LR
    ADMIN["👤 Admin<br/>toàn quyền"]
    OPER["👤 Operator<br/>vận hành server"]
    VIEW["👤 Viewer<br/>chỉ đọc"]

    SMS["<b>VCS-SMS</b><br/>Quản lý và giám sát<br/>10.000 server"]

    FLEET["🖥️ Đội server thật<br/>(TCP simulator khi dev)"]
    GMAIL["📧 Gmail SMTP<br/>smtp.gmail.com:587"]

    ADMIN --> SMS
    OPER --> SMS
    VIEW --> SMS

    SMS -->|"TCP connect mỗi 60s"| FLEET
    SMS -->|"STARTTLS + App Password<br/>báo cáo HTML + đính kèm .xlsx"| GMAIL
```

---

## 2. Container — 10 container, ai nói chuyện với ai

```mermaid
graph TB
    subgraph CLIENT["Trình duyệt"]
        WEB["🌐 Web UI · Next.js :3000"]
    end

    subgraph GATEWAY["Gateway"]
        TRAEFIK["🚪 Traefik v3 :8080<br/>CORS → ForwardAuth → RateLimit"]
    end

    subgraph APP["Application Services"]
        AUTH["🔐 auth-service :8081<br/>JWT · RBAC · ForwardAuth"]
        SRV["🖥️ server-service :8082<br/>CRUD · Import/Export · Consumer"]
        MON["📡 monitor-service :8083<br/>Scheduler · 200 worker · Facts"]
        REP["📊 report-service :8084<br/>Snapshot ⏰ · Email + Excel"]
    end

    subgraph DATA["Hạ tầng dữ liệu"]
        PG[("🐘 PostgreSQL 17<br/>identity_db · server_db · report_db")]
        REDIS[("⚡ Redis 8<br/>db0 = auth · db1 = monitor/cache")]
        ES[("🔍 Elasticsearch 8.12<br/>server-status-logs-*")]
    end

    SIM["🎭 tcp-simulator<br/>10.000 port 9001-19000"]

    WEB -->|"/api/v1/*"| TRAEFIK
    TRAEFIK -.->|"ForwardAuth<br/>GET /internal/verify"| AUTH
    TRAEFIK -->|"/api/v1/auth"| AUTH
    TRAEFIK -->|"🔒 /api/v1/servers"| SRV
    TRAEFIK -->|"🔒 /api/v1/reports"| REP

    AUTH --> PG
    AUTH -->|"blacklist · brute-force"| REDIS

    SRV --> PG
    SRV -->|"W target projection<br/>R status · uptime index"| REDIS
    SRV -.->|"R stream:monitor.status"| REDIS

    MON -->|"R target · W status<br/>W stream"| REDIS
    MON -.->|"bulk facts"| ES
    MON -->|"TCP ping"| SIM

    REP --> PG
    REP -->|"R aggregation 1 lần/ngày"| ES
    REP -->|"GET /internal/servers"| SRV
```

> **Điểm mấu chốt:** monitor-service **không** có PostgreSQL và **không** gọi HTTP tới server-service. Toàn bộ trao đổi giữa hai bên đi qua Redis.

---

## 3. Vòng đời một lượt đo — từ TCP ping tới email

```mermaid
graph LR
    subgraph R1["① Mỗi 60 giây"]
        SCH["Scheduler<br/>giành round lock"]
        Q(["monitor:ping:queue:{round}"])
        W["200 worker<br/>BRPOP → TCP dial"]
        SCH -->|"SSCAN 10.000 id"| Q
        Q --> W
    end

    subgraph R2["② Ghi kết quả nguyên tử"]
        LUA["Lua script<br/>1 lệnh · 3 key"]
        ST[("monitor:status:{id}<br/>total_checks · on_checks")]
        UX[("monitor:uptime:index<br/>ZSET % uptime")]
        STREAM[("stream:monitor.status<br/>CHỈ khi đổi trạng thái")]
        LUA --> ST
        LUA --> UX
        LUA --> STREAM
    end

    subgraph R3["③ Hai đường tách biệt"]
        CONS["Consumer<br/>server-service"]
        PGS[("servers.status<br/>PostgreSQL")]
        BUF["FactBuffer<br/>1000 fact / 5 giây"]
        ESI[("server-status-logs-<br/>YYYY.MM.DD")]
    end

    subgraph R4["④ 00:30 giờ VN ⏰"]
        JOB["Snapshot Job<br/>population LEFT JOIN facts"]
        SNAP[("daily_snapshots<br/>1 dòng / server / ngày")]
    end

    subgraph R5["⑤ 10:00 VN ⏰ hoặc theo yêu cầu"]
        MAIL["Email HTML<br/>+ uptime_*.xlsx"]
    end

    DASH["Dashboard<br/>/servers · /reports"]

    W --> LUA
    STREAM -.-> CONS --> PGS
    W -.-> BUF -.-> ESI
    ESI --> JOB
    JOB --> SNAP --> MAIL

    ST -->|"realtime"| DASH
    UX --> DASH
```

**Vì sao tách bước ② thành hai đường ở bước ③?**

Đường **stream** phải *chính xác* (trạng thái hiện tại của server), nên dùng Redis Stream có consumer group, ACK và version guard. Đường **fact** chỉ cần *đủ tốt* (số liệu thống kê); mất vài fact chỉ làm coverage giảm — vì thế `FactBuffer` được phép **drop** khi ES sập, thay vì phình bộ nhớ đến chết.

---

## 4. Ba tầng dữ liệu uptime — đừng nhầm lẫn

```mermaid
graph TB
    subgraph L1["⚡ Redis — luỹ kế TRỌN ĐỜI"]
        A1["monitor:status:{id}<br/>total_checks, on_checks"]
        A2["monitor:uptime:index (ZSET)"]
        A3["Dùng cho: dashboard realtime<br/>GET /servers/uptime"]
        A4["⚠ Không có khái niệm 'ngày'.<br/>Đổi tỉ lệ simulator sẽ KHÔNG<br/>làm số này đổi ngay."]
    end

    subgraph L2["🔍 Elasticsearch — FACT THÔ"]
        B1["1 document = 1 lượt ping<br/>~14,4 triệu doc/ngày"]
        B2["Index đặt tên theo ngày UTC"]
        B3["Dùng cho: DUY NHẤT snapshot job,<br/>1 lần/ngày lúc 00:30"]
    end

    subgraph L3["🐘 PostgreSQL — KẾT TINH THEO NGÀY"]
        C1["daily_snapshots<br/>(server_id, date) PK"]
        C2["on/actual/expected_checks,<br/>uptime_pct, last_status"]
        C3["Dùng cho: MỌI báo cáo và email"]
    end

    L1 -.->|"độc lập"| L2
    L2 -->|"composite agg<br/>00:30 VN"| L3
```

| | Redis | Elasticsearch | PostgreSQL |
|---|---|---|---|
| Phạm vi thời gian | trọn đời | mỗi lượt ping | mỗi ngày |
| Ai ghi | Lua script (monitor) | FactBuffer (monitor) | Snapshot job (report) |
| Ai đọc | dashboard | snapshot job | báo cáo + email |
| Mất dữ liệu thì sao | mất số đếm, không khôi phục được | coverage giảm | báo cáo bị TỪ CHỐI |

---

## 5. Bốn cơ chế bảo đảm tính đúng đắn

```mermaid
graph TB
    subgraph M1["🔐 Ranh giới xác thực"]
        A1["Traefik ForwardAuth<br/>chứng minh JWT hợp lệ"]
        A2["Service tự kiểm scope<br/>RequireScope('server:create')"]
        A3["Service dùng expose,<br/>không publish port ra host"]
        A1 --> A2 --> A3
    end

    subgraph M2["⏱️ Chống ghi đè ngược thời gian"]
        B1["round_id = unix/60<br/>lấy từ ĐỒNG HỒ REDIS"]
        B2["Lua: round_id ≤ old_round<br/>→ return 0, bỏ qua"]
        B3["SQL: WHERE status_version < ?<br/>→ 0 row, vẫn ACK"]
        B1 --> B2 --> B3
    end

    subgraph M3["📉 Suy giảm có kiểm soát"]
        C1["ES sập → FactBuffer drop<br/>coverage giảm, service vẫn sống"]
        C2["coverage < 95%<br/>→ email gắn cờ CẢNH BÁO"]
        C3["thiếu snapshot cả ngày<br/>→ TỪ CHỐI, không trung bình lên lỗ hổng"]
        C1 --> C2 --> C3
    end

    subgraph M4["📮 Gửi mail không nhân đôi"]
        D1["job row tồn tại TRƯỚC khi gửi"]
        D2["lỗi rõ ràng → failed"]
        D3["lỗi mập mờ → delivery_unknown<br/>giữ Message-ID, KHÔNG tự retry"]
        D1 --> D2
        D1 --> D3
    end
```

---

## 6. Bảng cổng và giao thức

| Thành phần | Cổng | Publish ra host? | Giao thức |
|-----------|------|------------------|-----------|
| web | 3000 | ✅ | HTTP |
| traefik | 8080 | ✅ | HTTP |
| auth-service | 8081 | ❌ `expose` | HTTP |
| server-service | 8082 | ❌ `expose` | HTTP |
| monitor-service | 8083 | ❌ `expose` | HTTP (`/health`, `/metrics`) |
| report-service | 8084 | ❌ `expose` | HTTP |
| postgres | 5432 | ✅ | PG wire |
| redis | 6379 | ✅ | RESP |
| elasticsearch | 9200 | ✅ | HTTP |
| tcp-simulator | 9001-19000 | ❌ | TCP thuần |

> Bốn service ứng dụng dùng `expose` chứ không `ports`: header `X-User-Id` / `X-User-Scopes` do Traefik tiêm vào được coi là **đáng tin**, nên truy cập trực tiếp sẽ vượt mặt toàn bộ lớp xác thực.

---

## 7. Các quyết định kiến trúc chính

| # | Quyết định | Lý do |
|---|-----------|-------|
| 1 | **Traefik + ForwardAuth** thay vì gateway tự viết | Xác thực tập trung một chỗ; service chỉ còn lo scope |
| 2 | **Database-per-service** (3 DB, 3 DB user) | Không service nào đọc chéo bảng của service khác |
| 3 | **Redis Streams** thay vì Kafka | Đã có Redis cho projection + status; thêm Kafka là thừa một hệ thống phải vận hành |
| 4 | **Lua script** cho ghi trạng thái | Ghi status + counter + stream nguyên tử — không có khe hở để Redis và stream bất đồng |
| 5 | **Snapshot theo ngày** thay vì query ES lúc gửi báo cáo | Báo cáo đọc 10.000 dòng thay vì 14 triệu document |
| 6 | **Population đọc từ Server Service**, không suy ra từ fact | Server không ai ping được vẫn phải xuất hiện trong báo cáo — nếu suy từ fact thì lỗ hổng biến mất |
| 7 | **Múi giờ VN chỉ ở Report Service** | Monitoring và ES thuần UTC; quy đổi tập trung một chỗ, tránh lệch ngày rải rác |
