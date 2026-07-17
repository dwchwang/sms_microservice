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
  ├─ Gỡ target projection:
  │     SREM server:monitor-target:ids  {id}    ← id TRƯỚC
  │     DEL  server:monitor-target:{id}         ← hash SAU
  └─ INCR server:list:version
```

**Gỡ ID trước, xóa hash sau** — ngược với lúc tạo. Cả hai thứ tự đều nhằm để Monitor
không bao giờ thấy ID mà thiếu metadata.

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

## 6. `GET /servers/stats`

Trả số ON/OFF/UNKNOWN **hiện tại**, cache TTL 10s (`server:stats:cache`), phục vụ
dashboard realtime.

Đừng nhầm với `servers_on_at_end_at` trong report: cái đó là ON/OFF **tại cuối kỳ
report**. Hai câu hỏi khác nhau, hai nguồn khác nhau.
