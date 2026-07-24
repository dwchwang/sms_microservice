# 06 — Flow: CRUD server

---

## 1. Tạo server

```text
POST /api/v1/servers          Idempotency-Key: <bắt buộc>
  │
  ├─ Traefik ForwardAuth → JWT hợp lệ?
  ├─ RequireScope("server:create") → 403 nếu thiếu
  ├─ Idempotency middleware:
  │     Claim(actor, endpoint, key, sha256(body))
  │       ├─ key đã có + body trùng   → replay response cũ, DỪNG
  │       ├─ key đã có + body khác    → 409, DỪNG
  │       └─ key mới                  → state=processing, đi tiếp
  │
  ├─ Validate DTO  (client KHÔNG được set status)
  ├─ CIDR allowlist → 422 SERVER_IP_NOT_ALLOWED
  ├─ INSERT INTO servers (status='UNKNOWN', status_version=0)
  ├─ Sync target projection:
  │     HSET server:monitor-target:{id}  ipv4, tcp_port, server_name
  │     SADD server:monitor-target:ids   {id}          ← hash TRƯỚC, id SAU
  ├─ INCR server:list:version
  └─ 201 + lưu response vào api_idempotency (state=completed)
```

**Ghi hash trước, thêm ID sau.** Ngược lại thì Monitor có thể nhặt được ID mà chưa có
metadata để ping.

**Client không set được `status`.** DTO không có field đó. Status chỉ đến từ
monitoring — đó là toàn bộ ý nghĩa của nó.

---

## 2. Đọc — cache-aside

```text
GET /api/v1/servers?status=ON&page=1
  │
  ├─ v = GET server:list:version
  ├─ key = server:list:cache:{sha256(query)}:{v}
  ├─ HIT  → dùng luôn
  └─ MISS → SELECT ... → SET key (TTL)
  │
  └─ Với mọi server trong kết quả:
        HGET monitor:status:{id} last_checked_at    ← đọc tươi, pipeline
```

**`last_checked_at` không nằm trong cache entry.** Nó đổi mỗi phút; nếu nhét vào
cache thì phải bump version mỗi phút và cache thành vô dụng. Đọc tươi từ Redis bằng
pipeline rẻ hơn nhiều so với việc vứt cả cache đi.

Version nằm **trong key**, nên bump version không cần xóa key nào cả.

---

## 3. Sửa server

```text
PUT /api/v1/servers/{server_id}
  ├─ RequireScope("server:update")
  ├─ Validate + CIDR
  ├─ Tên mới trùng server đang sống khác? → 409
  ├─ UPDATE servers ...
  ├─ Sync lại target projection (ipv4/port/tên có thể đã đổi)
  └─ INCR server:list:version
```

> **Bug đã sửa:** `cpu_cores`/`ram_gb`/`disk_gb` từng dùng `int` thuần. Cột `NULL` bị
> GORM scan thành `0`, rồi `Save()` ghi `0` ngược xuống → vi phạm
> `CHECK (cpu_cores IS NULL OR cpu_cores > 0)` → **500**. Nay dùng `*int` để phân biệt
> "chưa set" với "bằng 0".

---

## 4. Xóa server

```text
DELETE /api/v1/servers/{server_id}
  ├─ RequireScope("server:delete")
  ├─ UPDATE servers SET deleted_at = NOW()      ← soft delete
  ├─ Gỡ target projection VÀ dọn dấu vết monitoring:
  │     SREM server:monitor-target:ids  {id}    ← id TRƯỚC
  │     DEL  server:monitor-target:{id}         ← hash SAU
  │     ZREM monitor:uptime:index       {id}    ← nếu không: vẫn chấm điểm mãi mãi
  │     DEL  monitor:status:{id}                ← nếu không: last_checked_at đứng im
  └─ INCR server:list:version
```

**Gỡ ID trước, xóa hash sau** — ngược với lúc tạo. Cả hai thứ tự đều nhằm để Monitor
không bao giờ thấy ID mà thiếu metadata.

**Server Service dọn hai key mà Monitoring sở hữu.** Đây là ngoại lệ duy nhất của quy tắc
"mỗi namespace một owner", và nó cần thiết: `monitor:uptime:index` và `monitor:status:*`
**không có TTL**, nên một server đã xoá sẽ tiếp tục xuất hiện trong bảng "10 server tệ
nhất" của dashboard cho tới khi có người xoá tay. Monitoring không thể tự dọn vì nó không
biết server nào đã bị xoá — nó chỉ đọc projection, và server đó đã rời projection rồi.

`server_id` unique **toàn cục kể cả row đã xóa**, nên một ID đã dùng thì không tái sử
dụng được. Đây là chủ ý: nó bảo vệ lịch sử trong `daily_snapshots` và ES khỏi bị
lẫn giữa hai server khác nhau trùng ID.

---

## 5. Rebuild projection

```text
make rebuild-cache
  │
  ├─ Duyệt server đang sống theo cursor (page 1000)
  ├─ Ghi vào key TẠM: server:monitor-target:ids:{generation}
  ├─ RENAME key tạm → server:monitor-target:ids     ← atomic swap
  ├─ SET server:monitor-target:ready 1
  └─ Quét & xóa hash mồ côi (hash còn nhưng ID không còn trong Set)
```

Dựng Set dưới key tạm rồi `RENAME` để Monitor **không bao giờ thấy Set dựng dở**.

Sửa được 3 loại lệch: thiếu hash, thiếu ID, hash mồ côi.

> **Luôn chạy `make rebuild-cache` sau khi seed hoặc khôi phục dữ liệu.** Thiếu marker
> `ready`, Monitoring **bỏ qua mọi round**.

---

## 6. `GET /servers/stats` và `GET /servers/uptime`

Hai endpoint, cùng scope `server:stats`, hai câu hỏi khác nhau:

| | `/servers/stats` | `/servers/uptime` |
|---|---|---|
| Trả gì | `total`, `on`, `off`, `unknown` | phân bố uptime + top 10 tệ nhất |
| Nguồn | PostgreSQL `COUNT(*)` theo `status` | Redis ZSET `monitor:uptime:index` |
| Kỳ | **hiện tại** | **ngày hôm nay** (giờ VN) |
| Cache | `server:stats:cache` TTL 10s | `server:uptime:cache` TTL 10s |

`/servers/uptime` gọi `/servers/stats` bên trong (nên bao gồm luôn `total_servers`,
`servers_on/off/unknown`), rồi bổ sung phần uptime bằng **một** pipeline Redis:

```text
ZCARD  monitor:uptime:index                → số server đã được chấm điểm
ZCOUNT monitor:uptime:index 100 100        → servers_uptime_100
ZCOUNT monitor:uptime:index 0 0            → servers_uptime_0
ZCOUNT monitor:uptime:index (0 (100        → servers_uptime_partial
ZRANGE monitor:uptime:index 0 9 WITHSCORES → top_10_lowest_uptime
HMGET  monitor:status:{id} day_total day_on → số đếm thô cho 10 server đó
```

Không lệnh nào scale theo số server, nên endpoint này trả lời trong khoảng 0,1s với 10.000
server.

`servers_no_data = total_servers − ZCARD`: server vừa tạo chưa qua round nào thì không có
mặt trong ZSET, và nó phải được **đếm riêng** chứ không bị coi là uptime 0%.

`avg_uptime_pct` là trung bình các **phần trăm từng server**, không phải tỉ lệ
`Σon / Σtotal` toàn hệ thống — cùng định nghĩa với `avg_uptime_pct` của report, để hai con
số so sánh được với nhau.

Đừng nhầm với `servers_on_at_end_at` trong report: cái đó là ON/OFF **tại cuối kỳ
report**. Hai câu hỏi khác nhau, hai nguồn khác nhau.
