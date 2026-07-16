# Refactor Phase R6: Reporting Service

> **Mục tiêu:** Cập nhật Reporting Service theo thiết kế mới — tạo snapshot aggregate vào lúc 00:30 mỗi đêm (per-server), query dân số server thông qua internal API, gửi report qua Gmail SMTP.
>
> **Prerequisite:** Phase R4 (Internal API) và R5 (ES data format) hoàn tất.

---

## Checklist tổng quan

- [ ] **R6.1** Đổi config kết nối sang `report_db`, thêm Gmail SMTP
- [ ] **R6.2** Cập nhật model (`daily_snapshots`, `report_jobs`)
- [ ] **R6.3** Tạo Internal API Client (`server_client.go`)
- [ ] **R6.4** Tạo Job Snapshot Lúc 00:30 (`snapshot_job.go`)
- [ ] **R6.5** Viết lại luồng On-Demand & Daily Report
- [ ] **R6.6** Cập nhật Email Template (Text/HTML) với metric coverage
- [ ] **R6.7** Tích hợp SendMail qua Gmail SMTP

---

## R6.1 & R6.2. Config & Models

- Trỏ DB connection sang `report_db`.
- Model `daily_snapshots`: Không còn aggregate sum toàn hệ thống, đổi thành 1 row = 1 server 1 ngày.
  - PK: `server_id` + `date`.
  - Chứa `uptime_pct`, `actual_checks`, `expected_checks`, `last_status`.
- Model `report_jobs`: Thay đổi ENUM status, thêm idempotency, message ID.

---

## R6.3. Internal Server API Client

**File:** `internal/client/server_client.go`

- Viết REST Client gọi `http://server-service:8082/internal/servers`.
- Tham số `created_before`, `deleted_after`, `cursor`, `limit`.
- Sử dụng HTTP Client có timeout.

---

## R6.4. Job Snapshot (00:30)

**File:** `internal/snapshot/snapshot_job.go`

- Không tạo report khi gọi API, mà có worker chạy ngầm lúc 00:30.
- Bước 1: Fetch toàn bộ Server dân số ngày hôm qua từ Server API (phân trang).
- Bước 2: Chạy Elasticsearch Composite Aggregation tính `on_checks` và `total_checks` per server ngày hôm qua.
- Bước 3: Merge dữ liệu (LEFT JOIN) -> Server không có log ES thì `actual_checks = 0` (No Data).
- Bước 4: Batch Upsert vào `daily_snapshots`.

---

## R6.5. Luồng Report (Get Summary)

**File:** `internal/service/report_service.go`

- Function `GetSummary`: Query trực tiếp bảng `daily_snapshots`.
- Tính Average Uptime.
- Trích xuất Top 10 Worst.
- Trích xuất Coverage (tổng actual / tổng expected).
- Xử lý cache: Có thể bỏ qua Cache vì Query trên DB Postgres có Index đã rất nhanh (so với query ES trực tiếp trước đây).

---

## R6.6 & R6.7. Email & SMTP

- Sửa đổi HTML Email Template: Bổ sung chỉ số "Coverage" và "Không có dữ liệu".
- Thay thế dummy SMTP bằng kết nối thư viện `net/smtp` với TLS.
- Auth bằng Gmail App Password.
- Cập nhật state `report_jobs` (sending -> sent / delivery_unknown).

---

## Verify Phase R6

- [ ] Snapshot Job 00:30 tổng hợp được chính xác data từ ES.
- [ ] Generate Báo cáo On-Demand trả về kết quả đúng (1 row daily_snapshots query).
- [ ] Gửi Email đến Gmail/MailTrap thành công (với giao diện mới).
