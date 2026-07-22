# 🔄 Sơ đồ tuần tự — 8 luồng nghiệp vụ

> Cập nhật: 21/07/2026

| # | Luồng | Kích hoạt |
|---|-------|-----------|
| [1](#1-đăng-nhập-và-xác-thực-mọi-request-sau-đó) | Đăng nhập + ForwardAuth | người dùng |
| [2](#2-một-vòng-giám-sát-60-giây) | Một vòng giám sát 60s | tự động |
| [3](#3-lan-truyền-thay-đổi-trạng-thái-redis--postgresql) | Lan truyền đổi trạng thái | tự động |
| [4](#4-import-10000-server-từ-excel) | Import Excel | người dùng |
| [5](#5-export-theo-đúng-bộ-lọc-đang-áp-dụng) | Export theo bộ lọc | người dùng |
| [6](#6-snapshot-hằng-ngày--0030-giờ-vn-) | Snapshot hằng ngày ⏰ | cron |
| [7](#7-gửi-báo-cáo-kèm-file-excel) | Gửi báo cáo + Excel | cron / người dùng |
| [8](#8-dashboard-uptime-thời-gian-thực) | Dashboard realtime | người dùng |

---

## 1. Đăng nhập và xác thực mọi request sau đó

```mermaid
sequenceDiagram
    autonumber
    actor U as Người dùng
    participant W as Web UI
    participant T as Traefik
    participant A as auth-service
    participant PG as identity_db
    participant R as Redis db0
    participant S as server-service

    rect rgb(235, 245, 255)
    note over U, R: Giai đoạn A — đăng nhập
    U->>W: email + mật khẩu
    W->>T: POST /api/v1/auth/login
    note right of T: router auth-api KHÔNG có forward-auth<br/>chỉ rate-limit-auth (10 req/s)
    T->>A: chuyển tiếp
    A->>R: login:attempts:{email} còn trong ngưỡng?
    alt vượt ngưỡng
        R-->>A: quá nhiều lần thử
        A-->>W: 429 ErrTooManyAttempts
    end
    A->>PG: SELECT user + role + scopes
    A->>A: so khớp mật khẩu đã băm
    A->>A: sinh access + refresh token<br/>NHÚNG SẴN scopes vào claims
    A-->>W: access_token + refresh_token
    end

    rect rgb(240, 255, 240)
    note over U, S: Giai đoạn B — mọi request có xác thực về sau
    U->>W: mở /servers
    W->>T: GET /api/v1/servers<br/>Authorization: Bearer ...
    T->>A: ForwardAuth: GET /internal/verify
    note right of A: CHỈ kiểm chữ ký JWT trong bộ nhớ<br/>KHÔNG truy vấn PostgreSQL
    A-->>T: 200 + X-User-Id, X-User-Scopes, X-User-Email
    T->>S: chuyển tiếp KÈM các header đó
    S->>S: AuthFromForwardAuth() đọc header
    S->>S: RequireScope("server:list")
    alt thiếu scope
        S-->>W: 403 Forbidden
    end
    S-->>W: 200 danh sách server
    end
```

**Vì sao `/internal/verify` không đọc DB?** Nó nằm trên đường đi của *mọi* request. Một truy vấn ở đây là một truy vấn nhân với toàn bộ lưu lượng hệ thống. Đổi lại: đổi role của user chỉ có hiệu lực sau khi token cũ hết hạn hoặc user đăng nhập lại.

---

## 2. Một vòng giám sát 60 giây

```mermaid
sequenceDiagram
    autonumber
    participant SC as Scheduler
    participant R as Redis db1
    participant W as Worker (1 trong 200)
    participant SIM as tcp-simulator
    participant L as Lua script
    participant FB as FactBuffer
    participant ES as Elasticsearch

    rect rgb(255, 248, 235)
    note over SC, R: Pha 1 — nạp hàng đợi (chỉ 1 instance làm)
    SC->>R: TIME
    note right of SC: dùng ĐỒNG HỒ REDIS, không dùng đồng hồ máy<br/>để mọi instance đồng thuận round_id
    R-->>SC: t
    SC->>SC: round_id = t / 60
    SC->>R: đo queue vòng TRƯỚC → checks_missing
    SC->>R: SETNX monitor:round:lock:{round} TTL 120s
    alt thua lock
        R-->>SC: đã có người giữ
        note right of SC: bình thường — instance này<br/>vẫn ping từ queue người khác nạp
    else thắng lock
        SC->>R: EXISTS server:monitor-target:ready
        alt projection chưa sẵn sàng
            SC->>SC: BỎ QUA vòng này<br/>vòng dở dang = báo cáo sai
        else sẵn sàng
            loop SSCAN từng 500 id
                SC->>R: SSCAN server:monitor-target:ids
                SC->>R: RPUSH monitor:ping:queue:{round} (từng 500)
            end
            SC->>R: EXPIRE queue 120s
            SC->>R: SET monitor:round:current = round_id
            note right of SC: đặt CUỐI CÙNG — worker thấy round nào<br/>thì queue của round đó phải đã đầy
        end
    end
    end

    rect rgb(240, 248, 255)
    note over W, ES: Pha 2 — 200 worker rút queue song song
    loop cho tới khi hết ctx
        W->>R: GET monitor:round:current
        note right of W: đọc lại MỖI vòng lặp, không nhớ round<br/>⇒ chuyển vòng là tự nhiên
        W->>R: BRPOP monitor:ping:queue:{round} (1s)
        R-->>W: server_id
        W->>R: HGETALL server:monitor-target:{id}
        alt target đã biến mất
            note right of W: server bị xoá sau khi queue nạp → bỏ qua
        end
        W->>SIM: net.Dial TCP, timeout 3000ms
        alt kết nối được
            SIM-->>W: OK → ON
        else thất bại
            SIM-->>W: TIMEOUT / CONNECTION_REFUSED /<br/>DNS_ERROR / DIAL_ERROR → OFF
        end
        W->>L: EVALSHA statusScript
        L-->>W: 0 = cũ, bỏ · 1 = ĐỔI · 2 = giữ nguyên
        W-)FB: Add(fact) — không chặn
    end
    end

    rect rgb(245, 245, 245)
    note over FB, ES: Pha 3 — đường phụ, được phép mất
    FB->>FB: đủ 1000 fact HOẶC đủ 5 giây
    FB->>ES: bulk index
    alt bulk lỗi
        FB->>ES: thử lại tối đa 3 lần, backoff tăng dần
        FB->>FB: vẫn lỗi → DROP + es_bulk_failure_total++
        note right of FB: coverage giảm là hồi phục được.<br/>Hết RAM thì không.
    end
    end
```

### Bên trong Lua script — vì sao phải nguyên tử

```mermaid
sequenceDiagram
    autonumber
    participant W as Worker
    participant L as statusScript
    participant K1 as monitor:status:{id}
    participant K2 as monitor:uptime:index
    participant K3 as stream:monitor.status

    W->>L: (server_id, status, checked_at, latency, round_id)
    L->>K1: HGET status, round_id
    K1-->>L: old_status, old_round

    alt round_id ≤ old_round
        L-->>W: return 0 — vòng cũ hoặc phát lại, KHÔNG ghi gì
    end

    L->>K1: HSET status, last_checked_at, latency_ms, round_id
    L->>K1: HINCRBY total_checks 1
    note right of L: đếm SAU chốt chặn cũ ⇒ phát lại không thổi phồng
    opt status == ON
        L->>K1: HINCRBY on_checks 1
    end
    L->>K2: ZADD uptime_index (on/total)*100

    alt old_status không tồn tại HOẶC khác status mới
        L->>K3: XADD status.changed MAXLEN ~100000
        note right of L: lần check đầu (chưa có old_status)<br/>cũng là một chuyển đổi thật
        L-->>W: return 1 — ĐÃ ĐỔI
    else giống hệt
        L-->>W: return 2 — giữ nguyên, không đẩy stream
    end
```

Ghi status, cộng counter và đẩy stream nằm trong **một** lệnh Redis. Nếu tách rời, sẽ có khe hở mà Redis nói "server ON" còn stream chưa hề báo — hoặc ngược lại.

---

## 3. Lan truyền thay đổi trạng thái: Redis → PostgreSQL

```mermaid
sequenceDiagram
    autonumber
    participant L as Lua (monitor)
    participant ST as stream:monitor.status
    participant C as StatusConsumer<br/>(trong server-service)
    participant PG as server_db

    L->>ST: XADD event_type=status.changed<br/>status_version = round_id

    loop readLoop
        C->>ST: XREADGROUP group=server-svc<br/>consumer={hostname} COUNT 100 BLOCK 2s
        ST-->>C: các message

        C->>C: parseStatusEvent()
        alt message hỏng (status lạ, thiếu field...)
            C->>ST: XACK — vẫn ACK
            note right of C: không ACK thì nó bị giao lại VĨNH VIỄN
        else hợp lệ
            C->>PG: UPDATE servers SET status = ?<br/>WHERE server_id = ?<br/>AND status_version < ?
            alt 1 dòng đổi
                C->>ST: XACK + tăng version cache
            else 0 dòng (event cũ / lặp)
                C->>ST: XACK, KHÔNG đụng cache
            else DB lỗi
                note right of C: KHÔNG ACK → nằm pending → được thu hồi sau
            end
        end
    end

    loop reclaimLoop mỗi 30s
        C->>ST: XAUTOCLAIM min-idle 60s
        note right of C: nhận lại message của consumer đã chết
    end

    alt Redis bị FLUSHDB / restart mất group
        ST-->>C: lỗi NOGROUP
        C->>ST: XGROUP CREATE từ "0" — PHÁT LẠI toàn bộ
        note right of C: bắt đầu từ 0 chứ không phải $:<br/>Monitoring chỉ đẩy khi ĐỔI trạng thái,<br/>bỏ qua event cũ ⇒ PostgreSQL kẹt sai<br/>tới tận lần đổi tiếp theo.<br/>Phát lại an toàn nhờ version guard.
    end
```

**Hai lớp chống ghi đè ngược thời gian:**

| Lớp | Cơ chế | Bảo vệ khỏi |
|-----|--------|-------------|
| Redis | Lua: `round_id ≤ old_round → return 0` | worker chậm ghi đè kết quả mới hơn |
| PostgreSQL | `WHERE status_version < ?` | message tới không đúng thứ tự / phát lại |

---

## 4. Import 10.000 server từ Excel

```mermaid
sequenceDiagram
    autonumber
    actor U as Operator
    participant T as Traefik
    participant I as ImportHandler
    participant ID as Idempotency MW
    participant S as ImportService
    participant P as excel.Parser
    participant V as CIDR validator
    participant R as ServerRepository
    participant PG as server_db
    participant PJ as TargetProjection
    participant RD as Redis

    U->>T: POST /api/v1/servers/import<br/>Idempotency-Key: ...
    note right of T: router riêng, priority 100<br/>responseHeaderTimeout 120s cho file lớn
    T->>I: 🔒 forward-auth + scope server:import

    I->>ID: kiểm khoá idempotency
    alt key đã dùng, cùng request_hash
        ID-->>U: trả lại kết quả cũ, KHÔNG import lần nữa
    end

    I->>S: Import(reader)
    S->>P: Parse(xlsx)
    alt file hỏng
        P-->>S: lỗi
        S-->>U: 400 ErrImportFileRejected — TỪ CHỐI CẢ FILE
    end

    rect rgb(255, 250, 240)
    note over S, V: Lọc theo từng dòng — một dòng hỏng KHÔNG làm hỏng request
    loop mỗi dòng
        alt parser đánh dấu không hợp lệ
            S->>S: failed[] += mã lỗi
        else IP ngoài CIDR allowlist
            S->>V: Validate(ipv4)
            S->>S: failed[] += SERVER_IP_NOT_ALLOWED
        else trùng ngay trong file
            S->>S: skipped[] — bản xuất hiện đầu tiên thắng
        else hợp lệ
            S->>S: candidates[]
        end
    end
    end

    S->>R: lọc bỏ tên đã bị server còn sống chiếm
    note right of S: ON CONFLICT chỉ xử lý được server_id,<br/>nên tên phải lọc trước bằng tay

    rect rgb(240, 255, 240)
    note over S, PG: Chèn theo lô 500 dòng
    loop từng 500
        S->>R: InsertBatch(...)
        R->>PG: INSERT ... ON CONFLICT (server_id) DO UPDATE<br/>SET ..., deleted_at = NULL, status_version = 0<br/>WHERE servers.deleted_at IS NOT NULL<br/>RETURNING server_id
        PG-->>R: các server_id thực sự được ghi
        note right of PG: bản ghi còn sống: WHERE không khớp<br/>⇒ vắng mặt trong RETURNING ⇒ báo trùng<br/>bản ghi đã xoá mềm: khớp ⇒ HỒI SINH
        R-->>S: succeeded[]
    end
    end

    S->>PJ: Sync(target) cho từng dòng thành công
    PJ->>RD: HSET target + SADD ids
    S->>RD: tăng version cache danh sách

    S-->>U: 200 { total_rows, succeeded{}, failed{}, skipped_duplicate{} }
```

**Câu chuyện thực tế đã sửa:** import lại đúng file 10.000 dòng sau khi xoá 5 server, kết quả đúng phải là 5 thành công / 9.995 trùng. Trước khi sửa, `ON CONFLICT DO NOTHING` cộng với index `UNIQUE(server_id)` không có mệnh đề `WHERE` khiến 5 server đã xoá mềm không bao giờ hồi sinh được → báo trùng cả 10.000.

---

## 5. Export theo đúng bộ lọc đang áp dụng

```mermaid
sequenceDiagram
    autonumber
    actor U as Người dùng
    participant W as Web UI
    participant E as ExportHandler
    participant S as ExportService
    participant R as ServerRepository
    participant LC as LastCheckReader
    participant G as excel.Generator

    U->>W: lọc còn 37 server → bấm Export
    W->>E: POST /api/v1/servers/export<br/>body JSON = ĐÚNG bộ lọc đang dùng
    note right of E: ShouldBindJSON đọc thân request<br/>⇒ ServerFilter phải có tag `json:`,<br/>không chỉ tag `form:`
    E->>S: Export(filter)
    S->>R: List(filter) — dùng lại y hệt hàm của trang danh sách
    R-->>S: 37 server
    S->>LC: đọc monitor:status:* lấy lần check gần nhất
    S->>G: Generate(rows)
    G-->>S: buffer .xlsx
    S-->>W: file + Content-Disposition
    note right of W: CORS phải expose Content-Disposition,<br/>nếu không trình duyệt đọc không ra tên file
```

**Lỗi đã sửa:** `ServerFilter` chỉ có tag `form:` nên khi bind từ thân JSON, các trường `server_id` / `server_name` / `page_size` bị bỏ im lặng → export ra cả 10.000 thay vì 37 dòng đang lọc.

---

## 6. Snapshot hằng ngày — 00:30 giờ VN ⏰

```mermaid
sequenceDiagram
    autonumber
    participant C as cron<br/>WithLocation(Asia/Ho_Chi_Minh)
    participant J as snapshot.Job
    participant SC as ServerClient
    participant SS as server-service
    participant ES as Elasticsearch
    participant PG as report_db

    C->>J: 00:30 VN → RunYesterday(now)
    J->>J: start = 00:00 hôm qua (VN)<br/>end = 00:00 hôm nay (VN)<br/>cửa sổ nửa mở [start, end)

    rect rgb(255, 250, 240)
    note over J, SS: ① Dân số — đọc từ Server Service, KHÔNG suy từ fact
    J->>SC: Population(start, end)
    SC->>SS: GET /internal/servers?from=&to=
    note right of SS: endpoint nội bộ, không đi qua Traefik
    SS-->>J: [{server_id, server_name, created_at, deleted_at}]
    end

    rect rgb(240, 248, 255)
    note over J, ES: ② Số đo — composite aggregation, phân trang
    loop cho tới khi hết after_key
        J->>ES: composite agg theo server_id<br/>+ filter status=ON<br/>+ top_hits lấy fact cuối
        ES-->>J: 1000 bucket + after_key
    end
    note right of ES: terms agg KHÔNG phân trang nổi 10.000 bucket;<br/>composite thì được
    end

    rect rgb(240, 255, 240)
    note over J, PG: ③ LEFT JOIN dân số lên số đo
    loop mỗi server trong dân số
        J->>J: expected = (thời gian sống trong ngày) / 60s
        alt CÓ fact
            J->>J: uptime = on/actual*100<br/>lấy server_name TỪ FACT (tên tại ngày đó)
        else KHÔNG có fact
            J->>J: uptime_pct = NULL (no-data), actual = 0<br/>NHƯNG expected VẪN tính<br/>⇒ chính nó kéo coverage xuống
        end
    end
    J->>PG: UPSERT daily_snapshots theo lô 500<br/>ON CONFLICT (server_id, date) DO UPDATE
    note right of PG: upsert ⇒ chạy lại cùng một ngày là an toàn
    end

    J-->>C: {servers, servers_no_data, coverage_pct}
```

**Vì sao dân số phải đọc từ Server Service?** Một server *không ai ping được* vẫn phải xuất hiện trong báo cáo. Nếu suy dân số từ chính đống fact, server đó biến mất — và lỗ hổng giám sát tự xoá dấu vết của nó.

**Chạy lại thủ công khi job đêm hỏng:**
```
POST /internal/snapshots/2026-07-17
```

---

## 7. Gửi báo cáo kèm file Excel

```mermaid
sequenceDiagram
    autonumber
    actor U as Operator
    participant H as ReportHandler
    participant SS as SendService
    participant RS as ReportService
    participant JR as JobRepository
    participant PG as report_db
    participant RN as Renderer
    participant XG as excel.Generator
    participant SM as GmailSender
    participant G as Gmail

    alt ⏰ cron 10:00 VN
        note over SS: tự động, khoảng = hôm qua, người gửi = "scheduler"
    else 👤 theo yêu cầu
        U->>H: POST /api/v1/reports<br/>{start_date, end_date, recipient_email}
    end

    H->>SS: Send(req, type, requesterID, idempotencyKey)

    SS->>RS: ParseRange(start, end, now)
    alt end_date là hôm nay hoặc tương lai
        RS-->>U: 400 — ngày chưa kết thúc thì báo cáo vô nghĩa
    else khoảng vượt REPORT_MAX_RANGE_DAYS (31)
        RS-->>U: 400 ErrInvalidRange
    end

    SS->>JR: Create(job, state=processing)
    JR->>PG: INSERT report_jobs
    note right of JR: dòng job tồn tại TRƯỚC khi thử gửi mail,<br/>để một lần gửi mập mờ vẫn có vết

    SS->>RS: Summary(start, end)
    RS->>PG: MissingDates(start, end)
    alt có ngày thiếu snapshot
        RS-->>SS: ErrDataUnavailable + liệt kê ngày
        SS->>JR: SetFailed(failed)
        note right of RS: TỪ CHỐI thay vì lấy trung bình vắt qua lỗ hổng
    end
    RS->>PG: Totals · CountByLastStatus · LowestUptime(10)
    RS-->>SS: SummaryResponse<br/>(total_servers, avg_uptime, actual_checks,<br/>coverage_pct, degraded, top10)

    SS->>JR: SetGenerated(response_json) → state=generated
    SS->>RN: Render(summary)
    RN-->>SS: subject + HTML<br/>(gồm dòng "Tổng số lượt kiểm tra")
    SS->>JR: SetState(sending)

    rect rgb(255, 250, 240)
    note over SS, XG: Đính kèm — PHỤ TRỢ, hỏng cũng không được mất báo cáo
    SS->>RS: ServerUptimeRows(start, end)
    RS->>PG: AllServerUptime — GIỮ cả dòng NULL uptime
    note right of PG: trả về TOÀN BỘ dân số ⇒ số dòng Excel<br/>khớp đúng total_servers trong thân mail
    SS->>XG: Generate(rows)
    XG-->>SS: xlsx 4 cột:<br/>server_id · server_name · uptime_pct · total_checks
    alt bước nào lỗi
        SS->>SS: log WARN, gửi mail KHÔNG đính kèm
    end
    end

    SS->>SM: Send(Message{To, Subject, HTML, Attachment})
    SM->>SM: compose multipart/mixed<br/>base64 xuống dòng 76 ký tự
    SM->>G: STARTTLS → AUTH → DATA

    alt gửi thành công
        G-->>SM: 250 OK + Message-ID
        SS->>JR: SetSent → state=sent
    else lỗi rõ ràng (535 sai mật khẩu...)
        G-->>SM: lỗi
        SS->>JR: SetFailed → state=failed
    else lỗi mập mờ (đứt kết nối sau DATA)
        SM-->>SS: ErrAmbiguousDelivery
        SS->>JR: SetFailed → state=delivery_unknown<br/>GIỮ LẠI Message-ID
        note right of SS: KHÔNG tự retry — thư có thể đã tới,<br/>retry mù sẽ gửi hai lần.<br/>Người vận hành tra Message-ID trong hộp Sent.
    end
```

**Số liệu lượt kiểm tra khớp nhau ở hai nơi:**
- Thân email: `ActualChecks` = **tổng** toàn hệ thống (ví dụ 1.110.043).
- Excel cột `total_checks` = **từng server** (ví dụ 137 lượt/server).
- Cộng cột `total_checks` lại đúng bằng con số trong thân email — hai bên cùng bắt nguồn từ `SUM(actual_checks)` trên `daily_snapshots`.

---

## 8. Dashboard uptime thời gian thực

```mermaid
sequenceDiagram
    autonumber
    actor U as Người dùng
    participant W as Web UI
    participant S as server-service
    participant UR as UptimeReader
    participant RD as Redis db1

    U->>W: mở /reports
    W->>S: GET /api/v1/servers/uptime 🔒 server:stats
    S->>UR: đọc phân bố
    UR->>RD: ZCOUNT monitor:uptime:index 100 100
    UR->>RD: ZCOUNT ... 0 0
    UR->>RD: ZRANGE ... 0 9 WITHSCORES
    RD-->>UR: phân bố + 10 server tệ nhất
    S-->>W: uptime realtime
```

> ⚠️ **Con số này là LUỸ KẾ TRỌN ĐỜI**, không phải "hôm nay". Đổi `SIMULATOR_DEFAULT_UPTIME_RATE` từ 0,95 xuống 0,75 sẽ **không** làm dashboard đổi ngay — số đếm cũ vẫn nằm đó. Muốn thấy tỉ lệ mới cần xoá `monitor:status:*` và `monitor:uptime:index` để đếm lại từ đầu.
>
> Đây là con số **khác** với uptime trong email: email đọc `daily_snapshots` (cắt theo từng ngày), dashboard đọc Redis (trọn đời).
