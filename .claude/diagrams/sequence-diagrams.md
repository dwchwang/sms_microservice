# 🔄 Sơ đồ tuần tự — 8 luồng nghiệp vụ

> Cập nhật: 24/07/2026

| # | Luồng | Kích hoạt |
|---|-------|-----------|
| [1](#1-đăng-nhập-và-xác-thực-mọi-request-sau-đó) | Đăng nhập + ForwardAuth | người dùng |
| [2](#2-một-vòng-giám-sát-60-giây) | Một vòng giám sát 60s | tự động |
| [3](#3-lan-truyền-thay-đổi-trạng-thái-redis--postgresql) | Lan truyền đổi trạng thái | tự động |
| [4](#4-import-10000-server-từ-excel) | Import Excel | người dùng |
| [5](#5-export-theo-đúng-bộ-lọc-đang-áp-dụng) | Export theo bộ lọc | người dùng |
| [6](#6-snapshot-hằng-ngày-) | Snapshot hằng ngày ⏰ | cron (có leader election) |
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

    W->>L: KEYS status·stream·uptime<br/>ARGV server_id, status, checked_at,<br/>latency, round_id, day(VN)
    alt round_id ≤ round cũ
        L-->>W: 0 — bỏ qua, KHÔNG ghi gì
    else mới hơn
        L->>K: HSET status, last_checked_at, latency_ms, round_id
        opt field 'day' khác ARGV day
            L->>K: HSET day, day_total=0, day_on=0 — SANG NGÀY MỚI
        end
        L->>K: HINCRBY day_total (+ day_on nếu ON) → ZADD uptime %
        alt lần đầu (old_status == false) HOẶC status khác cũ
            L->>K: XADD stream.changed
            L-->>W: 1 — ĐÃ ĐỔI
        else giống hệt
            L-->>W: 2 — giữ nguyên, không đẩy stream
        end
    end
```

Ghi status, reset/cộng bộ đếm ngày, cập nhật ZSET và đẩy stream nằm trong **một** lệnh
Redis. Nếu tách rời, sẽ có khe hở mà Redis nói "server ON" còn stream chưa hề báo — hoặc
ngược lại.

Bộ đếm nằm **sau** chốt chặn round cũ, nên một round phát lại hay tới muộn không thổi
phồng số. `day` do Go tính theo `Asia/Ho_Chi_Minh` rồi truyền vào, không phải Lua tự lấy
giờ — `redis.call('TIME')` trả UTC, và dùng nó sẽ làm bộ đếm reset lúc 7 giờ sáng VN.

`old_status == false`, **không** `== nil`: Redis Lua trả `false` cho field không tồn tại.
Viết `nil` thì điều kiện vĩnh viễn sai, event đầu tiên (`UNKNOWN → ON/OFF`) không bao giờ
phát, và server mới kẹt `UNKNOWN` mãi mãi trong PostgreSQL.

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

## 6. Snapshot hằng ngày ⏰

```mermaid
sequenceDiagram
    autonumber
    participant SC as Scheduler<br/>(mọi replica, tick 60s)
    participant CR as cron_runs
    participant J as snapshot.Job
    participant SS as server-service
    participant ES as Elasticsearch
    participant PG as report_db

    SC->>SC: due(REPORT_SNAPSHOT_CRON, now)? — giờ nổ hôm nay đã qua chưa
    SC->>CR: TryClaim("snapshot", hôm_qua, hostname, staleAfter=3m)
    alt thua claim
        CR-->>SC: false — replica khác đang/đã làm, dừng
    else thắng claim
        CR-->>SC: true (state=running, owner=tôi)
        par heartbeat
            SC->>CR: UPDATE heartbeat_at = NOW() mỗi 30s<br/>mất claim → cancel job đang chạy
        and công việc thật
            SC->>J: Run(hôm_qua) — cửa sổ [00:00, 24:00) giờ VN
            J->>SS: ① GET /internal/servers?created_before=&deleted_after=<br/>→ dân số (KHÔNG suy từ fact)
            loop ② composite agg + after_key, size 1000
                J->>ES: bucket theo server_id · on_checks · last_fact
            end
            J->>J: ③ LEFT JOIN dân số ⟕ số đo<br/>có fact → uptime = on/actual · không → NULL (no-data)
            note right of J: server no-data vẫn tính expected ⇒ tự kéo coverage xuống
            J->>PG: ④ UPSERT daily_snapshots theo lô (chạy lại an toàn)
            J-->>SC: {servers, servers_no_data, coverage_pct}
        end
        SC->>CR: MarkDone WHERE owner = tôi
    end
```

**Vì sao dân số phải đọc từ Server Service?** Một server *không ai ping được* vẫn phải xuất hiện trong báo cáo. Nếu suy dân số từ chính đống fact, server đó biến mất — và lỗ hổng giám sát tự xoá dấu vết của nó.

Endpoint nội bộ nhận **hai** tham số bắt buộc, cả hai RFC3339:
`created_before` = 00:00 ngày kế tiếp, `deleted_after` = 00:00 ngày cần snapshot. Đó chính
là điều kiện "server này có tồn tại trong ngày đó không". Trả về theo cursor
(`next_cursor` = `server_id` cuối), tối đa 1000 dòng mỗi trang → ~10 request cho 10.000 server.

**Vì sao `TryClaim` chứ không chỉ dựa vào cron nổ một lần?** `report-service` chạy 3
replica, mỗi replica có scheduler riêng. Không có claim thì 3 replica cùng aggregate 14,4
triệu document và cùng UPSERT — vô hại về dữ liệu (UPSERT idempotent) nhưng tốn 3 lần tài
nguyên và làm Elasticsearch nghẹt.

**Chạy lại thủ công khi job đêm hỏng** (không đi qua Traefik, không cần claim):
```bash
docker exec vcs-sms-traefik wget -qO- --post-data='' \
  http://report-service:8084/internal/snapshots/2026-07-23
```

---

## 7. Gửi báo cáo kèm file Excel

```mermaid
sequenceDiagram
    autonumber
    actor U as Operator / ⏰ Scheduler
    participant SS as SendService
    participant RS as ReportService
    participant PG as report_db
    participant SM as GmailSender

    U->>SS: Send(range, recipient, reportType, requesterID, idemKey)
    SS->>RS: ParseRange — end_date phải ĐÃ kết thúc · ≤ REPORT_MAX_RANGE_DAYS
    opt có Idempotency-Key
        SS->>PG: FindByIdempotency(requesterID, key)
        alt tìm thấy, cùng nội dung
            PG-->>SS: job cũ → REPLAY, dừng ở đây
        else tìm thấy, khác nội dung
            PG-->>SS: ErrIdempotencyConflict
        end
    end
    SS->>PG: Create job (state=processing) — có TRƯỚC khi gửi
    note right of PG: ux_report_jobs_idem là partial unique index;<br/>insert bị chối ⇒ replica khác vừa giành key ⇒ replay
    SS->>RS: Summary
    RS->>PG: MissingDates? → từ chối + nêu ngày thiếu
    RS-->>SS: total · avg_uptime · coverage · top10
    SS->>PG: state=generated, lưu response_json
    SS->>SS: render HTML → state=sending
    SS->>SS: sinh Excel (đính kèm là phụ trợ, hỏng vẫn gửi — chỉ log WARN)
    SS->>SM: STARTTLS → AUTH → DATA
    alt 250 OK
        SM-->>SS: sent + Message-ID
    else lỗi RÕ (535, domain bị chặn)
        SM-->>SS: failed
    else lỗi MẬP MỜ (đứt sau DATA)
        SM-->>SS: delivery_unknown — giữ Message-ID, KHÔNG retry
    end
```

### Đường tự động có thêm một chốt mà đường người dùng không có

```mermaid
graph LR
    A["Scheduler tick"] --> B{"IsDone('snapshot', hôm_qua)?"}
    B -->|"chưa"| Z["bỏ qua tick này"]
    B -->|"rồi"| C{"TryClaim('daily_report', hôm_qua)"}
    C -->|"thua"| Z
    C -->|"thắng"| D{"FindLatestDaily(hôm_qua)<br/>đã có job nào chưa?"}
    D -->|"có, state KHÔNG resendable"| E["log WARN, KHÔNG gửi lại"]
    D -->|"chưa, hoặc processing/generated/failed"| F["SendService.Send"]
```

`resendable(state)` chỉ nhận `processing`, `generated`, `failed` — ba trạng thái mà body
**chưa** lên dây. `sending` **không** resendable, vì nó được ghi *trước* khi gọi SMTP: một
replica chết đúng khoảng đó thì không ai biết mail đã đi hay chưa, và đoán "chưa" là cách
gửi hai lần.

Đây là lý do claim `cron_runs` một mình không đủ. Claim chỉ bảo đảm "cùng một lúc chỉ một
replica chạy"; nó không nói gì về "lần chạy trước đã làm tới đâu".

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
    participant PG as server_db

    U->>W: mở / (dashboard)
    W->>S: GET /api/v1/servers/uptime 🔒 server:stats
    note right of S: cache server:uptime:cache TTL 10s<br/>→ hit thì trả ngay
    S->>PG: GetStats — COUNT theo status (cache 10s)
    S->>UR: Stats(worstN = 10) — MỘT pipeline
    UR->>RD: ZCARD · ZCOUNT 100 100 · ZCOUNT 0 0 · ZCOUNT (0 (100
    UR->>RD: ZRANGE 0 9 WITHSCORES
    UR->>RD: HMGET monitor:status:{id} day_total day_on (pipeline)
    RD-->>UR: phân bố + 10 server tệ nhất + số đếm thô
    S->>PG: FindByServerID cho 10 server đó — lấy server_name
    S-->>W: uptime của HÔM NAY
```

> **Con số này là uptime của NGÀY HÔM NAY theo giờ Việt Nam**, không phải luỹ kế trọn đời.
> Lua giữ field `day` trong `monitor:status:{id}`; lần check đầu tiên của ngày mới thấy
> `day` khác thì đặt lại `day_total`/`day_on` về 0, nên toàn bộ ZSET tự làm mới trong
> **một round** sau nửa đêm.
>
> Hệ quả: đổi `SIMULATOR_DEFAULT_UPTIME_RATE` sẽ thấy dashboard hội tụ về tỉ lệ mới trong
> vòng một ngày, **không** cần xoá key thủ công.

**Ba con số, ba nguồn — trong cùng một response:**

| Nhóm field | Nguồn | Ý nghĩa |
|---|---|---|
| `total_servers`, `servers_on/off/unknown` | PostgreSQL `COUNT(*)` theo `status` | trạng thái **hiện tại** |
| `servers_uptime_100/partial/0`, `avg_uptime_pct`, `top_10_lowest_uptime` | Redis ZSET | uptime **hôm nay** |
| `servers_no_data` | `total_servers − ZCARD` | server Monitoring chưa từng chấm điểm |

`servers_no_data` được **trừ ra**, không đoán: một server vừa tạo chưa qua round nào thì
không có mặt trong ZSET, và nó phải được đếm riêng chứ không bị coi là uptime 0%.

`server_name` lấy từ PostgreSQL chứ không từ Redis, vì PostgreSQL là chủ sở hữu của tên.
Cái giá: 10 truy vấn `FindByServerID` cho đúng 10 dòng của bảng xếp hạng.

> Đây là con số **khác** với uptime trong email: email đọc `daily_snapshots` (mọi ngày đã
> kết thúc, cắt theo khoảng người dùng chọn), dashboard đọc Redis (chỉ hôm nay, có ngay
> mọi lúc kể cả khi chưa có snapshot nào).
