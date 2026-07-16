# Refactor Phase R7: Cleanup & Verification

> **Mục tiêu:** Dọn dẹp rác, loại bỏ các service cũ (API Gateway, FileIO), chạy kiểm thử end-to-end toàn hệ thống để đảm bảo kiến trúc mới hoạt động đúng thiết kế.
>
> **Prerequisite:** Các phase R1-R6 hoàn tất và pass unit test/manual test.

---

## Checklist tổng quan

- [ ] **R7.1** Xóa FileIO Service & API Gateway custom
- [ ] **R7.2** Xóa Kafka stub/mock (nếu còn)
- [ ] **R7.3** Dọn file `.env.example`, `Makefile`
- [ ] **R7.4** End-to-end Verification
- [ ] **R7.5** Tài liệu Swagger / OpenAPI Updates

---

## R7.1. Xóa Service Cũ

```bash
# Xóa source code không dùng đến
rm -rf api-gateway/
rm -rf fileio-service/
```

- Đảm bảo `docker-compose.yml` final sạch sẽ, chỉ bao gồm:
  - `postgres` (chung, nhưng 3 databases)
  - `redis`
  - `elasticsearch`
  - `traefik`
  - `identity-service`
  - `server-service`
  - `monitor-service`
  - `report-service`
  - `tcp-simulator`

---

## R7.2. Xóa Stub & Rác

- Nếu R1.3 tạo Kafka stub trong `shared/kafka`, thì giờ xóa hẳn mục đó.
- Xóa các middleware JWT cũ (chỉ để lại ForwardAuth Traefik middleware).

---

## R7.3. Cập nhật Makefile & Docs

- Sửa lại các lệnh build, run, test trong `Makefile`. Bỏ các lệnh của `api-gateway` và `fileio`.
- Khai báo lại cấu trúc trong `README.md` hoặc `docs/debai.md` nếu cần.

---

## R7.4. End-to-end Verification Flow

1. **Khởi động**: `docker compose up --build -d`
2. **Setup**: Đảm bảo DB được init tự động qua v2 sql.
3. **Identity Flow**: POST `/api/v1/auth/login` (Admin/Admin@123456) => Nhận JWT.
4. **Server Flow**: Tạo Server (POST `/api/v1/servers`), Sửa Server (chú ý `tcp_port`).
5. **Import Excel Flow**: Dùng JWT + Scope, POST file Excel => Xác nhận API đồng bộ, Redis được bơm.
6. **Monitor Flow**: Bật TCP Simulator. Chờ 2 phút. Xem Elasticsearch data và log XADD stream.
7. **Consumer Flow**: Check Postgres bảng `servers` - Xem field `status_changed_at` có thay đổi sau khi Simulator thay đổi TCP port không.
8. **Report Flow**: Force chạy Snapshot Job => Lấy Report Summary => Trả về chính xác (chú ý Coverage metric). Gửi thử email.

---

## R7.5. OpenAPI Updates

- Kiểm tra file `docs/api-spec.yaml`. Cập nhật các endpoint đã bỏ (FileIO), endpoint mới (Import/Export vào Server) và Response formats.

> Hoàn tất Phase 7 là hoàn tất dự án refactor từ Monolithic-style Microservice qua đúng chuẩn Microservices Design!
