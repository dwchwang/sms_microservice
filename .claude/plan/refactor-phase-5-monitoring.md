# Refactor Phase R5: Monitoring Service

> **Mục tiêu:** Viết lại toàn bộ lõi của Monitoring Service để nó chuyển từ kiểu pull data trực tiếp DB sang kiểu lock round trong Redis và push metrics qua Redis Stream. Không còn phụ thuộc PostgreSQL.
>
> **Prerequisite:** Phase R4 đã hoàn tất phần Redis Target Projection.

---

## Checklist tổng quan

- [ ] **R5.1** Dọn dẹp: Xóa PostgreSQL, GORM, Kafka, Event Consumer
- [ ] **R5.2** Viết lại `health_check_scheduler.go` (cơ chế sinh Round IDs & Nạp Queue)
- [ ] **R5.3** Viết lại `pool.go` (BRPOP Worker)
- [ ] **R5.4** Viết Lua script nguyên tử cho Status Update + XADD
- [ ] **R5.5** Cập nhật `es_repository.go` (Bounded Bulk & Deterministic ID)
- [ ] **R5.6** Kiểm tra end-to-end với TCP Simulator

---

## R5.1. Dọn dẹp Dependency

- Bỏ hoàn toàn `server_reader.go` (truy cập postgres)
- Bỏ `config_repository.go` (health_check_configs)
- Bỏ `event_consumer.go` (Kafka)
- Gỡ bỏ `gorm` và `kafka-go` khỏi `go.mod`.
- Đảm bảo `Dockerfile` và `docker-compose` không còn wait `postgres`.

---

## R5.2. Scheduler: Sinh Round & Nạp Queue

**File:** `health_check_scheduler.go`

- Chạy mỗi phút (60s).
- Tính `round_id = floor(time.Now().Unix() / 60)`.
- `SETNX monitor:round:lock:{round_id}` với TTL 120s. Nếu hụt, bỏ qua chu kỳ (scheduler khác đang làm).
- Đọc `server:monitor-target:ids` (qua SSCAN hoặc SMEMBERS nếu đủ nhỏ).
- `RPUSH` vào `monitor:ping:queue:{round_id}` (batching).
- Gắn `EXPIRE` 120s cho queue.
- `SET monitor:round:current {round_id}` (TTL 120s) làm tín hiệu cho worker biết round hiện tại.

---

## R5.3. Worker Pool: BRPOP Worker

**File:** `pool.go`

- Worker khởi tạo bằng Go channel không còn phù hợp. Đổi sang Redis Queue.
- Worker vòng lặp vô hạn:
  1. `GET monitor:round:current` -> round_id
  2. `BRPOP monitor:ping:queue:{round_id} 1`
  3. Nếu có việc: `HGETALL server:monitor-target:{server_id}` (lấy IP, Port)
  4. TCP Check (gọi `tcp_checker.go` có sẵn, override port bằng port lấy từ Redis).
  5. Cập nhật Status qua Lua Script.
  6. Lưu log ES (nhét vào Bulk Buffer).

---

## R5.4. Lua Script XADD (Cập nhật Status)

Thay vì GORM và Kafka, Worker gọi Lua Script:
1. `HGET monitor:status:{server_id} status`
2. Update Hash (status, checked_at, latency_ms)
3. Nếu status đổi (hoặc lần đầu): `XADD stream:monitor.status MAXLEN ~ 100000 * ...`
4. Trả về thành công.

---

## R5.5. Cập nhật Elasticsearch Repo

**File:** `es_repository.go`

- Thay đổi Document ID thành `{server_id}:{round_id}` để tránh duplicate logs (Idempotent).
- Index tên: `server-status-logs-YYYY.MM.DD`.
- Implement background bulk flush (thay vì flush ngay lập tức).

---

## Verify Phase R5

- [ ] Monitor Service boot mà không cần PostgreSQL/Kafka.
- [ ] Lock chia round giữa nhiều instances (nếu scale > 1) hoạt động trơn tru.
- [ ] Dữ liệu ping lưu vào ES thành công với format ID mới.
- [ ] Events đẩy vào `stream:monitor.status` chính xác (kiểm tra bằng `XREAD`).
