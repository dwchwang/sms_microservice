# 02 — Chiến lược dữ liệu

> Ba database tách rời + Redis + Elasticsearch. Mỗi kho có **một** owner.

---

## 1. Database-per-service

| Database | Owner | DB user | Bảng |
|---|---|---|---|
| `identity_db` | auth-service | `identity_user` | `users`, `roles`, `permissions`, `role_permissions` |
| `server_db` | server-service | `server_user_v2` | `servers`, `api_idempotency` |
| `report_db` | report-service | `report_user_v2` | `report_jobs`, `daily_snapshots` |

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

## 4. Redis — mỗi namespace một owner

| Key | Owner | Người đọc | Mục đích |
|---|---|---|---|
| `server:monitor-target:ids` (Set) | server-service | monitor | Danh sách server cần ping |
| `server:monitor-target:{id}` (Hash) | server-service | monitor | `ipv4`, `tcp_port`, `server_name` |
| `server:monitor-target:ready` | server-service | monitor | Marker projection đã dựng xong |
| `server:list:version` | server-service | server-service | Bump để vô hiệu cache |
| `server:list:cache:{hash}:{version}` | server-service | server-service | Cache-aside |
| `monitor:round:current` | monitor | monitor | Round đang chạy |
| `monitor:ping:queue:{round_id}` (List) | monitor | monitor | Việc của round |
| `monitor:status:{id}` (Hash) | monitor | server-service | Status, `last_checked_at`, `total_checks`, `on_checks` |
| `monitor:uptime:index` (ZSet) | monitor | server-service | Uptime luỹ kế mỗi server, phục vụ dashboard |
| `stream:monitor.status` (Stream) | monitor | server-service | Event `status.changed` |

Không dùng `KEYS server:monitor-target:*` — lệnh đó block Redis và quét toàn
keyspace. Thay vào đó dùng Set `:ids` làm index và duyệt bằng `SSCAN`.

> ⚠️ Redis chạy `volatile-lru` + `appendonly yes`. **Không** dùng `allkeys-lru`: chỉ
> `server:list:cache:*` và các key round mới có TTL. Bộ đếm uptime, status, target
> projection và stream đều không TTL — evict bất kỳ cái nào là mất dữ liệu không tái
> tạo được từ PostgreSQL.

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

```text
server:list:cache:{query_hash}:{list_version}
```

Version nằm **trong key**. Bump `server:list:version` không cần xóa key nào — key cũ
đơn giản là không còn ai hỏi tới, và TTL sẽ dọn chúng.

Bump xảy ra khi: tạo/sửa/xóa/import server, hoặc consumer apply được một status event
có thật. Không bump khi status không đổi — đó là lý do cache sống sót.
