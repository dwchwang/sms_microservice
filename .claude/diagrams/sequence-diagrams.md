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
    participant T as Traefik
    participant A as auth-service
    participant S as service đích

    note over U,A: A — đăng nhập (router auth KHÔNG qua ForwardAuth)
    U->>T: POST /auth/login (email + mật khẩu)
    T->>A: rate-limit-auth 10 req/s
    A->>A: kiểm brute-force · so khớp hash<br/>sinh JWT, NHÚNG scopes vào claims
    A-->>U: access_token + refresh_token

    note over U,S: B — mọi request có xác thực về sau
    U->>T: GET /servers + Bearer JWT
    T->>A: ForwardAuth /internal/verify (chỉ kiểm chữ ký, KHÔNG đụng DB)
    A-->>T: 200 + X-User-Id / X-User-Scopes
    T->>S: forward KÈM header
    S->>S: RequireScope("server:list") → 403 nếu thiếu
    S-->>U: 200 danh sách server
```

**Vì sao `/internal/verify` không đọc DB?** Nó nằm trên đường đi của *mọi* request. Một truy vấn ở đây là một truy vấn nhân với toàn bộ lưu lượng hệ thống. Đổi lại: đổi role của user chỉ có hiệu lực sau khi token cũ hết hạn hoặc user đăng nhập lại.

---

## 2. Một vòng giám sát 60 giây

```mermaid
sequenceDiagram
    autonumber
    participant SC as Scheduler
    participant R as Redis db1
    participant W as Worker ×200
    participant SIM as tcp-simulator
    participant ES as Elasticsearch

    note over SC,R: Pha 1 — nạp queue (chỉ instance thắng lock)
    SC->>R: TIME → round_id = t/60 (đồng hồ Redis)
    SC->>R: đo queue vòng trước → checks_missing
    SC->>R: SETNX round lock · EXISTS target:ready
    SC->>R: SSCAN ids → RPUSH queue → SET round:current

    note over W,ES: Pha 2 — 200 worker rút queue song song (mọi instance)
    loop tới khi hết ctx
        W->>R: GET round:current · BRPOP queue (1s)
        W->>R: HGETALL target — nil → bỏ qua (server đã xoá)
        W->>SIM: TCP dial 3s → ON / OFF
        W->>R: EVALSHA Lua → 0 cũ · 1 ĐỔI · 2 giữ
        W-)ES: Add(fact) — buffer, đường phụ được phép mất
    end
```

Scheduler đặt `round:current` **cuối cùng** để worker thấy round nào thì queue của round
đó chắc chắn đã nạp đầy. Thua lock là bình thường — worker vẫn ping từ queue người khác
nạp. FactBuffer flush khi đủ 1000 fact hoặc 5s; ES lỗi thì retry 3 lần rồi **drop**
(coverage giảm là hồi phục được, OOM thì không).

### Bên trong Lua script — vì sao phải nguyên tử

```mermaid
sequenceDiagram
    autonumber
    participant W as Worker
    participant L as statusScript
    participant K as Redis<br/>status · uptime · stream

    W->>L: (server_id, status, round_id)
    alt round_id ≤ round cũ
        L-->>W: 0 — bỏ qua, KHÔNG ghi gì
    else mới hơn
        L->>K: HSET status + HINCRBY counter + ZADD uptime
        alt lần đầu HOẶC status khác cũ
            L->>K: XADD stream.changed
            L-->>W: 1 — ĐÃ ĐỔI
        else giống hệt
            L-->>W: 2 — giữ nguyên, không đẩy stream
        end
    end
```

Đếm counter nằm **sau** chốt chặn round cũ nên phát lại không thổi phồng số. Ghi status,
cộng counter và đẩy stream gói trong **một** lệnh Redis — không có khe hở để Redis và
stream bất đồng.

Ghi status, cộng counter và đẩy stream nằm trong **một** lệnh Redis. Nếu tách rời, sẽ có khe hở mà Redis nói "server ON" còn stream chưa hề báo — hoặc ngược lại.

---

## 3. Lan truyền thay đổi trạng thái: Redis → PostgreSQL

```mermaid
sequenceDiagram
    autonumber
    participant L as Lua (monitor)
    participant ST as stream.status
    participant C as StatusConsumer<br/>(server-service)
    participant PG as server_db

    L->>ST: XADD status.changed (status_version = round_id)
    loop readLoop
        C->>ST: XREADGROUP group=server-svc COUNT 100 BLOCK 2s
        alt event hỏng
            C->>ST: XACK rồi bỏ (không thì redeliver mãi)
        else hợp lệ
            C->>PG: UPDATE servers ... WHERE status_version < ?
            C->>ST: XACK · bump cache CHỈ khi có row đổi<br/>(DB lỗi → không ACK, để pending)
        end
    end
    loop reclaimLoop 30s
        C->>ST: XAUTOCLAIM min-idle 60s — nhận việc consumer chết
    end
    note over C,ST: mất group (FLUSHDB / restart) → XGROUP CREATE từ "0",<br/>replay an toàn nhờ version guard
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
    participant S as ImportService
    participant PG as server_db
    participant RD as Redis

    U->>T: POST /servers/import + Idempotency-Key
    T->>S: 🔒 scope server:import
    note right of S: cùng key+body → trả kết quả cũ, KHÔNG import lại

    S->>S: Parse Excel — file hỏng → 400, từ chối CẢ file
    note right of S: lọc từng dòng: invalid / ngoài CIDR → failed;<br/>trùng trong file / tên đã tồn tại → skipped

    loop mỗi lô 500 dòng hợp lệ
        S->>PG: INSERT ... ON CONFLICT (server_id) DO UPDATE<br/>WHERE deleted_at IS NOT NULL RETURNING server_id
        PG-->>S: id ghi được<br/>(vắng mặt = trùng · đã xoá mềm = HỒI SINH)
    end

    S->>RD: HSET target + SADD ids · bump cache version
    S-->>U: 200 { succeeded, failed, skipped_duplicate }
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
    participant C as cron 00:30 VN
    participant J as snapshot.Job
    participant SS as server-service
    participant ES as Elasticsearch
    participant PG as report_db

    C->>J: RunYesterday — cửa sổ [00:00, 24:00) hôm qua (VN)
    J->>SS: ① GET /internal/servers → dân số (KHÔNG suy từ fact)
    loop ② composite agg + after_key
        J->>ES: bucket theo server_id · on_checks · fact cuối
    end
    J->>J: ③ LEFT JOIN dân số ⟕ số đo<br/>có fact → uptime = on/actual · không → NULL (no-data)
    note right of J: server no-data vẫn tính expected ⇒ tự kéo coverage xuống
    J->>PG: UPSERT daily_snapshots theo lô 500 (chạy lại an toàn)
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
    actor U as Operator / ⏰ cron 10:00
    participant SS as SendService
    participant RS as ReportService
    participant PG as report_db
    participant SM as GmailSender

    U->>SS: Send(range, recipient)
    SS->>RS: ParseRange — end_date phải ĐÃ kết thúc · ≤ 31 ngày
    SS->>PG: Create job (state=processing) — có TRƯỚC khi gửi
    SS->>RS: Summary
    RS->>PG: MissingDates? → từ chối + nêu ngày thiếu
    RS-->>SS: total · avg_uptime · coverage · top10
    SS->>SS: render HTML + sinh Excel (đính kèm là phụ trợ, hỏng vẫn gửi)
    SS->>SM: STARTTLS → AUTH → DATA
    alt 250 OK
        SM-->>SS: sent + Message-ID
    else lỗi RÕ (535, domain bị chặn)
        SM-->>SS: failed
    else lỗi MẬP MỜ (đứt sau DATA)
        SM-->>SS: delivery_unknown — giữ Message-ID, KHÔNG retry
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
