# 📋 Refactor Plan — Chuyển đổi sang thiết kế mới

> **Mục tiêu:** Refactor hệ thống hiện tại (5 service + Kafka + shared DB) sang thiết kế mới (4 service + Redis Stream + database-per-service)
>
> **Tài liệu tham chiếu:** `design.md` (thiết kế mới) + `refactor.md` (đối chiếu chi tiết)
>
> **Nguyên tắc:** Refactor theo phase, mỗi phase kết thúc hệ thống vẫn chạy được (hoặc ít nhất compile được). Không refactor big-bang.

---

## Tổng quan 7 Refactor Phases

| Phase | Tên | Mục tiêu chính |
|-------|-----|----------------|
| **[R1](./refactor-phase-1-infrastructure.md)** | Infrastructure & Shared | Xóa Kafka, thêm Traefik, tách 3 DB, viết lại init.sql, sửa shared module, thêm lumberjack |
| **[R2](./refactor-phase-2-identity.md)** | Identity Service | Đổi DB → identity_db, thêm `/internal/verify`, đổi scope names, Argon2id, brute-force protection |
| **[R3](./refactor-phase-3-server-core.md)** | Server Service — Core | Đổi model (thêm cột, đổi status), đổi DB → server_db, đổi cache-aside, bỏ Kafka producer |
| **[R4](./refactor-phase-4-server-advanced.md)** | Server Service — Advanced | Thêm Redis target projection, thêm Redis Stream consumer, gộp import/export từ FileIO, thêm internal API, CIDR validator, idempotency |
| **[R5](./refactor-phase-5-monitoring.md)** | Monitoring Service | Viết lại scheduler (Redis rounds), viết lại worker (BRPOP), bỏ PostgreSQL, Lua script atomic, deterministic ES doc ID |
| **[R6](./refactor-phase-6-reporting.md)** | Reporting Service | Thêm snapshot job 00:30, đổi daily_snapshots (per-server), internal API client, Gmail SMTP, delivery_unknown, coverage |
| **[R7](./refactor-phase-7-cleanup.md)** | Cleanup & Verification | Xóa FileIO service, xóa api-gateway, xóa code thừa, end-to-end test, docker-compose final |

---

## Dependency Graph

```
R1 (Infrastructure & Shared)
 │
 ├──→ R2 (Identity Service)
 │      │
 │      └──→ R3 (Server Core)
 │             │
 │             ├──→ R4 (Server Advanced)
 │             │      │
 │             │      └──→ R5 (Monitoring)
 │             │             │
 │             │             └──→ R6 (Reporting)
 │             │                    │
 │             │                    └──→ R7 (Cleanup & Verify)
 │             │
 │             └──→ R5 (Monitoring) [có thể song song với R4]
 │
 └──→ R7 (Cleanup) [final step]
```

**Lưu ý:** R4 và R5 có thể làm song song nếu R3 đã xong. R6 phụ thuộc cả R4 (internal API) và R5 (ES data).

---

## Chiến lược Refactor

### Nguyên tắc giữ hệ thống chạy được

1. **Không xóa cũ trước khi có mới chạy**: Ví dụ, giữ Kafka dependency cho đến khi Redis Stream consumer đã chạy ổn.
2. **Feature flag / dual-write**: Khi chuyển đổi messaging, có thể chạy song song Kafka + Redis Stream rồi tắt Kafka sau.
3. **Migration data**: Chạy migration thêm cột mới TRƯỚC, deploy code mới SAU.
4. **Rollback plan**: Mỗi phase có rollback step — revert migration + revert code.

### Thứ tự migration database

```
1. Thêm 3 database mới (identity_db, server_db, report_db) — song song với DB cũ
2. Tạo bảng mới trong DB mới
3. Migrate data từ schema cũ sang DB mới
4. Đổi connection string trong service
5. Verify — chạy test
6. Xóa schema cũ (Phase R7)
```

---

## Checklist tổng quan

- [ ] **R1** Infrastructure & Shared hoàn tất
- [ ] **R2** Identity Service refactor xong
- [ ] **R3** Server Service core refactor xong
- [ ] **R4** Server Service advanced features xong
- [ ] **R5** Monitoring Service viết lại xong
- [ ] **R6** Reporting Service refactor xong
- [ ] **R7** Cleanup & end-to-end verification
