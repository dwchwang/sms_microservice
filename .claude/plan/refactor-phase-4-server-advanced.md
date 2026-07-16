# Refactor Phase R4: Server Service — Advanced

> **Mục tiêu:** Thêm các tính năng nâng cao vào Server Service theo đúng trách nhiệm mới trong thiết kế — bao gồm Redis target projection, Redis stream consumer, Import/Export từ FileIO chuyển sang, API nội bộ và Idempotency.
>
> **Prerequisite:** Phase R3 hoàn tất.

---

## Checklist tổng quan

- [ ] **R4.1** Tạo Redis Target Projection (đồng bộ Redis khi CRUD)
- [ ] **R4.2** Thêm Redis Stream Consumer (`status_consumer.go`)
- [ ] **R4.3** Migrate logic Import/Export (FileIO → Server Service)
- [ ] **R4.4** Thêm API nội bộ (`GET /internal/servers`) cho Reporting
- [ ] **R4.5** Implement IPv4 CIDR Validator
- [ ] **R4.6** Thêm Idempotency middleware (bảng `api_idempotency`)
- [ ] **R4.7** Cập nhật Endpoint `/servers/stats`

---

## R4.1. Redis Target Projection

**File:** `server-service/internal/projection/target_projection.go`

- Quản lý danh sách target cho Monitor Service.
- Format Redis hash: `server:monitor-target:{server_id}` chứa `ipv4`, `tcp_port`.
- Danh sách IDs: Set `server:monitor-target:ids`.
- `SyncCreate`/`SyncUpdate`: Ghi Hash trước, Set sau.
- `SyncDelete`: Xóa Set trước, xóa Hash sau.

Gắn logic này vào `server_service.go` khi Create, Update, Delete.

---

## R4.2. Redis Stream Consumer

**File:** `server-service/internal/consumer/status_consumer.go`

- Lắng nghe stream `stream:monitor.status` với Consumer Group `server-svc`.
- Vòng lặp `XREADGROUP BLOCK 2000 COUNT 100`.
- Parse data, batch update trạng thái server trong bảng `servers`.
- Bumping `server:list:version` nếu có row bị thay đổi.
- Chạy một goroutine riêng cho `XAUTOCLAIM` (chống consumer chết bỏ việc).

---

## R4.3. Import/Export

- Chép logic từ `fileio-service/internal/excel` sang `server-service/internal/excel`.
- Chép `import_service.go` và `export_service.go`.
- Sửa lại import logic:
  - Thay vì Kafka async, làm đồng bộ (synchronous).
  - Trả về response chi tiết `success`, `failed`, `skipped`.
  - Hỗ trợ batch insert (ON CONFLICT).
  - Tích hợp CIDR validation.
- Export logic: Thêm `LastStatusCheck` qua pipeline Redis HGET.

---

## R4.4. Internal API

**File:** `server-service/internal/handler/internal_handler.go`

- GET `/internal/servers?created_before=X&deleted_after=Y&cursor=Z&limit=1000`.
- Dùng cursor pagination cho reporting service móc dữ liệu dân số server.

---

## R4.5. CIDR Allowlist

**File:** `server-service/internal/validator/cidr_validator.go`

- Parse `SERVER_CIDR_ALLOWLIST` từ ENV.
- Reject 0.0.0.0, 127.0.0.0/8, 169.254.0.0/16, 224.0.0.0/4.
- Cho phép nếu nằm trong allow list.

---

## R4.6. Idempotency

- Tạo logic xử lý Idempotency-Key.
- Lưu trữ request/response trong bảng `api_idempotency`.
- Ngăn chặn double submit (nhất là API import/export nặng).

---

## Verify Phase R4

- [ ] Redis được cập nhật Hash và Set khi tạo mới server.
- [ ] Import Excel chạy đồng bộ và trả kết quả chính xác.
- [ ] Target projection hoạt động trơn tru.
- [ ] Idempotency-Key ngăn chặn request trùng lặp.
