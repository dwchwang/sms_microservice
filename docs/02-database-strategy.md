# 02 — Chiến lược dữ liệu

> Ba database tách rời + Redis + Elasticsearch. Mỗi kho có **một** owner.

---

## 1. Database-per-service

| Database | Owner | DB user | Bảng |
|---|---|---|---|
| `identity_db` | auth-service | `identity_user` | `users`, `roles`, `permissions`, `role_permissions` |
| `server_db` | server-service | `server_user_v2` | `servers`, `api_idempotency` |
| `report_db` | report-service | `report_user_v2` | `report_jobs`, `daily_snapshots`, `cron_runs` |

Không service nào có credential của database service khác. Đây là ranh giới được
**cưỡng chế bằng quyền DB**, không phải bằng quy ước.

### Vì sao không dùng schema chung

Schema chung cho phép service A `JOIN` thẳng vào bảng của service B. Một khi
chuyện đó xảy ra, đổi schema của B là làm gãy A, và ranh giới service chỉ còn trên
giấy. Tách database làm cho việc đó **không thể**, chứ không phải "không nên".

Cái giá: muốn dữ liệu của service khác thì phải gọi API. Ví dụ report-service lấy
population qua `GET /internal/servers` của server-service chứ không `SELECT` thẳng.

---

## 2. Bảng `servers` — những cột đáng chú ý

```sql
server_id            VARCHAR(100) NOT NULL   -- ID nghiệp vụ, unique TOÀN CỤC
status               VARCHAR(20)  NOT NULL DEFAULT 'UNKNOWN'
                     CHECK (status IN ('ON','OFF','UNKNOWN'))
status_version       BIGINT       NOT NULL DEFAULT 0   -- version guard
last_status_event_id VARCHAR(255)                      -- stream ID đã apply
ipv4                 INET         NOT NULL
cpu_cores            INT          CHECK (cpu_cores IS NULL OR cpu_cores > 0)
deleted_at           TIMESTAMPTZ                       -- soft delete
```

### Index

```sql
-- server_id unique kể cả row đã xóa: bảo vệ lịch sử, không cho tái sử dụng ID
CREATE UNIQUE INDEX ux_servers_server_id ON servers (server_id);

-- server_name chỉ unique trên row còn sống: cho phép trùng tên với server đã xóa
CREATE UNIQUE INDEX ux_servers_active_name ON servers (server_name)
    WHERE deleted_at IS NULL;
```

### `status_version` — vì sao cần

Event từ stream có thể tới **trùng** hoặc **sai thứ tự** (khi replay sau sự cố).
Mọi UPDATE đều đi kèm điều kiện:

```sql
UPDATE servers SET status=?, status_version=?
WHERE server_id=? AND status_version < ?
```

`status_version` chính là `round_id`, vốn tăng đơn điệu theo thời gian. Event cũ
không bao giờ ghi đè được dữ liệu mới → apply trở nên **idempotent**, và nhờ đó
việc replay cả stream là an toàn.

### Cột số dùng `*int` chứ không `int`

`cpu_cores`, `ram_gb`, `disk_gb` có ràng buộc `NULL hoặc > 0`. Với `int` thuần,
cột `NULL` bị scan thành `0` rồi ghi ngược `0` xuống → vi phạm CHECK. Kiểu con trỏ
phân biệt được "chưa set" và "bằng 0".

---

## 3. Bảng `daily_snapshots` — mắt xích retention

```sql
PRIMARY KEY (server_id, date)
on_checks        INT              -- số lần check trả về ON
actual_checks    INT              -- số lần thực sự đo được
expected_checks  INT              -- số lần LẼ RA phải đo, theo lifecycle server
uptime_pct       NUMERIC(5,2)     -- NULL khi actual_checks = 0
last_status      VARCHAR(10)      -- ON/OFF cuối ngày; NULL khi no_data
```

**`uptime_pct` NULL chứ không phải 0.** Server không ai đo được có uptime **không
xác định**, không phải 0%. Để 0 sẽ biến một sự cố thu thập dữ liệu thành một báo cáo
sai. NULL giữ nó ngoài `AVG()` và đưa nó vào nhóm `servers_no_data`.

**`expected_checks` theo lifecycle từng server.** Server tạo lúc 18:00 chỉ kỳ vọng
360 lần check chứ không phải 1.440 — nếu không, tạo server mới sẽ trông như sự cố
monitoring.

Bảng này là lý do ES chỉ cần giữ 7 ngày: dữ liệu thô được cô đọng vào đây trước khi
index bị ILM xóa. Xem [09-flow-reporting-email.md](./09-flow-reporting-email.md).

---

## 3b. Bảng `cron_runs` — trọng tài cho nhiều replica

```sql
CREATE TABLE cron_runs (
    job_name      VARCHAR(50)  NOT NULL,   -- 'snapshot' | 'daily_report'
    run_date      DATE         NOT NULL,   -- NGÀY DỮ LIỆU, luôn là hôm qua
    state         VARCHAR(20)  NOT NULL CHECK (state IN ('running','done','failed')),
    owner         VARCHAR(255) NOT NULL,   -- hostname replica đang giữ claim
    started_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    heartbeat_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ,
    error_message TEXT,
    PRIMARY KEY (job_name, run_date)
);
```

`report-service` chạy nhiều replica (`docker-stack.yml`: `replicas: 3`) và **mọi** replica
đều chạy scheduler. Không có bảng này thì 10:00 sáng có ba email giống nhau bay ra.

**PRIMARY KEY là trọng tài duy nhất** — không có lock nào khác. `TryClaim` là một
`INSERT ... ON CONFLICT DO UPDATE` với mệnh đề `WHERE` quyết định ai được nhận:

```sql
ON CONFLICT (job_name, run_date) DO UPDATE
   SET state = 'running', owner = EXCLUDED.owner, started_at = NOW(), heartbeat_at = NOW()
 WHERE cron_runs.state = 'failed'
    OR (cron_runs.state = 'running'
        AND cron_runs.heartbeat_at < NOW() - make_interval(secs => ?))
RETURNING job_name
```

| State hiện tại | Claim lại được? | Vì sao |
|---|:---:|---|
| `done` | ❌ | Xong rồi. Đây là thứ bảo đảm một job chạy **đúng một lần** cho mỗi ngày dữ liệu |
| `failed` | ✅ ngay tick sau | Lỗi tạm thời (ES nghẹt, DB timeout) tự khỏi mà không cần can thiệp |
| `running` | ✅ nếu `heartbeat_at` cũ hơn 3 phút | Phân biệt job chạy chậm với replica đã chết |

`RETURNING job_name` trả 0 dòng khi `WHERE` không khớp — đó chính là tín hiệu "thua claim".

**`run_date` là ngày *dữ liệu*, không phải ngày chạy.** Job chạy 00:30 ngày 24 xử lý dữ
liệu ngày 23 → `run_date = 2026-07-23`. Nhờ vậy chạy lại vào lúc nào, hay restart bao nhiêu
lần, cũng không tạo dòng thứ hai.

**Mọi lệnh ghi đều có `WHERE owner = ?`.** Nếu replica A bị treo mạng 4 phút, B cướp claim
và chạy xong; A hồi phục rồi gọi `MarkDone` sẽ ghi 0 dòng thay vì ghi đè kết quả của B.

> Bảng được thêm sau nên có `deployments/docker/postgres/migrate_report_ha.sql` cho
> database đã tồn tại — `init.sql` chỉ chạy khi volume Postgres còn rỗng.

---

## 4. Redis — mỗi namespace một owner

**db1** (`REDIS_DB=1`) — server-service + monitor-service:

| Key | Owner | Người đọc | TTL | Mục đích |
|---|---|---|---|---|
| `server:monitor-target:ids` (Set) | server-service | monitor | — | Danh sách server cần ping |
| `server:monitor-target:{id}` (Hash) | server-service | monitor | — | `server_name`, `ipv4`, `tcp_port` |
| `server:monitor-target:ready` | server-service | monitor | — | Marker projection đã dựng xong |
| `server:list:version` | server-service | server-service | — | Bump để vô hiệu cache |
| `server:list:cache:{hash}:{version}` | server-service | server-service | 30s | Cache-aside cho `GET /servers` |
| `server:detail:cache:{id}:{version}` | server-service | server-service | 30s | Cache-aside cho `GET /servers/{id}` |
| `server:stats:cache` | server-service | server-service | 10s | `GET /servers/stats` |
| `server:uptime:cache` | server-service | server-service | 10s | `GET /servers/uptime` |
| `monitor:round:lock:{round_id}` | monitor | monitor | 120s | `SETNX` — chọn kẻ nạp queue |
| `monitor:round:current` | monitor | monitor | 120s | Round đang chạy |
| `monitor:ping:queue:{round_id}` (List) | monitor | monitor | 120s | Việc của round |
| `monitor:status:{id}` (Hash) | monitor | server-service | — | `status`, `last_checked_at`, `latency_ms`, `round_id`, `day`, `day_total`, `day_on` |
| `monitor:uptime:index` (ZSet) | monitor | server-service | — | Uptime **của ngày hôm nay (giờ VN)** mỗi server, phục vụ dashboard |
| `stream:monitor.status` (Stream) | monitor | server-service | — | Event `status.changed`, `MAXLEN ~100000` |

**db0** (`REDIS_DB=0`) — auth-service:

| Key | Giá trị | TTL |
|---|---|---|
| `auth:refresh:{jti}` | `user_id` | `JWT_REFRESH_EXPIRY_DAYS` — rotate mỗi lần refresh |
| `auth:blacklist:{jti}` | `"revoked"` | thời gian còn lại của access token |
| `auth:login_attempts:{email}` | số lần sai | 15 phút, khoá khi ≥ 5 |

Không dùng `KEYS server:monitor-target:*` — lệnh đó block Redis và quét toàn
keyspace. Thay vào đó dùng Set `:ids` làm index và duyệt bằng `SSCAN`.

### `monitor:uptime:index` đếm theo ngày, không luỹ kế trọn đời

Lua giữ thêm field `day` (`YYYY-MM-DD` theo `Asia/Ho_Chi_Minh`) trong
`monitor:status:{id}`. Lần check đầu tiên của ngày mới thấy `day` khác thì đặt lại
`day_total` và `day_on` về 0, nên toàn bộ ZSET tự làm mới trong **một round** sau nửa đêm.

Bản trước đếm trọn đời và dashboard trở nên vô nghĩa sau vài ngày: một server chết cả hôm
nay vẫn hiện 92% nhờ tuần trước, và AOF mang con số đó qua mọi lần restart.

> Trên Redis đã chạy từ trước, `monitor:status:{id}` có thể còn `total_checks`/`on_checks`
> — **field của bản cũ** còn sót trong AOF, không code nào đọc hay ghi nữa. Tên JSON
> `total_checks`/`on_checks` trong response API được giữ cho hợp đồng với frontend, nhưng
> giá trị lấy từ `day_total`/`day_on`.

> ⚠️ Redis chạy `volatile-lru` + `appendonly yes`. **Không** dùng `allkeys-lru`: chỉ
> `server:*:cache:*` và các key round mới có TTL. Bộ đếm uptime, status, target
> projection, `server:list:version` và stream đều không TTL — evict bất kỳ cái nào là mất
> dữ liệu không tái tạo được từ PostgreSQL. Mất `server:monitor-target:*` là nghiêm trọng
> nhất: Monitoring **dừng hẳn** cho tới khi có người chạy `rebuild-monitor-cache`.

---

## 5. Elasticsearch — chỉ một người đọc

Index `server-status-logs-YYYY.MM.DD`, một document cho mỗi (server, round).

```text
_id = "{server_id}:{round_id}"     ← tất định
```

`_id` tất định làm cho **retry bulk không tạo document trùng**. Đây là điều kiện để
`actual_checks` không bị đếm thừa.

Mapping do index template cài đặt lúc monitor khởi động. Các field
`server_id` / `server_name` / `status` phải là **`keyword`**: nếu để ES tự map động,
chúng thành `text` và toàn bộ aggregation uptime sẽ **âm thầm trả 0**.

ILM: hot 0–7 ngày → xóa index sau 7 ngày (~100 triệu doc, 15–20 GB).

**Job snapshot là consumer duy nhất của ES.** Report đọc `daily_snapshots`, API đọc
PostgreSQL. Nhờ vậy ES chết chỉ ảnh hưởng snapshot đêm nay, không làm chết API nào.

---

## 6. Cache-aside

Bốn key cache, hai kiểu vô hiệu hoá khác nhau:

```text
server:list:cache:{query_hash}:{list_version}    TTL 30s  → version trong key
server:detail:cache:{server_id}:{list_version}   TTL 30s  → version trong key
server:stats:cache                               TTL 10s  → chỉ TTL
server:uptime:cache                              TTL 10s  → chỉ TTL
```

Với hai key đầu, version nằm **trong key**. Bump `server:list:version` không cần xóa key
nào — key cũ đơn giản là không còn ai hỏi tới, và TTL sẽ dọn chúng.

Bump xảy ra khi: tạo/sửa/xóa/import server, hoặc consumer apply được một status event
có thật. Không bump khi status không đổi — đó là lý do cache sống sót.

Hai key sau chỉ dựa vào TTL 10 giây, vì chúng là **số tổng hợp toàn hệ thống**: một
version cho từng truy vấn không có ý nghĩa gì, và 10 giây đủ ngắn để dashboard trông sống.

`last_status_check` **không** nằm trong bất kỳ cache entry nào. Nó đổi mỗi phút; nhét vào
cache thì phải bump version mỗi 60 giây và cache thành vô dụng. Nó được đọc tươi từ
`monitor:status:{id}` bằng một pipeline `HGET` lúc serialize response — rẻ hơn nhiều so
với việc vứt cả cache đi.
