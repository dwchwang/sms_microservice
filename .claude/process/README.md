# 📋 VCS-SMS — Tiến độ triển khai

> **Quy trình:** Design First → Code → Test → Deploy
> **Kiến trúc:** 6 Microservices + 1 Gateway + 1 TCP Simulator | Monorepo | 1 Postgres (5 schemas)

---

## 📊 Trạng thái các Phase

| # | Phase | Status | Date | Key deliverable |
|---|-------|:------:|------|----------------|
| 0 | Foundation & Design | ✅ | 2026-06-10 | Monorepo, Docker, DB 5 schemas, shared libs, TCP Simulator |
| 1 | Auth + Server + Gateway | ✅ | 2026-06-10 | ~55 files, 15 endpoints, 45+ tests, 14 bug fixes, mockery+sqlmock, coverage ≥88% |
| 2 | Monitor + TCP Simulator | ✅ | 2026-06-11 | segmentio/kafka-go, Health-check scheduler, ES bulk, Worker Pool, Redis lock/status fixes, mockery, stabilization tests |
| 3 | Report Service | ✅ | 2026-06-11 | ES aggregation, SMTP gomail, HTML email, daily cron, 26 tests |
| 4 | File I/O Service | ✅ | 2026-06-11 | Import/Export Excel, Kafka async, fail-closed queue, 77 tests |
| 5 | Polish & Docs | ✅ | 2026-06-12 | 7 Dockerfiles, docker-compose full stack, architecture.md, user-guide.md, README.md, auth test fixes |
| 6 | Security Enhancement | ✅ | 2026-06-12 | Default viewer role, admin seed, user role management API, 19 endpoints |

---

## 🟢 Phase 0 — Foundation

- 7 Go modules, Docker Compose (full + dev), 5 DB schemas, 20 migrations
- Shared: logger, response, errors (17 codes), validator, kafka interfaces, middleware
- TCP Simulator: 10K listeners, Math Engine, Dockerfile

---

## 🟢 Phase 2 — Hoàn thành (2026-06-11)

**Phase 2 implementation + stabilization complete | affected modules test/build OK | mockery (6 interfaces) | segmentio/kafka-go | RedisClient interface**

| Service | Components | Key features |
|---------|-----------|-------------|
| **monitor-service** | checker, worker, scheduler, repository, service | TCP health-check, Worker Pool, 8-step Cron scheduler, ES bulk, Kafka consumer/producer, RedisClient interface + mock |
| **tcp-simulator** | tests | 10 unit tests (Math Engine + Manager) |
| **shared/kafka** | segmentio + mocks | SegmentioProducer (Writer), SegmentioConsumer (Reader), ProducerMock, ConsumerMock |

**Kafka:** segmentio/kafka-go — pure Go, context-native, manual commit sau handler success (`FetchMessage` + `CommitMessages`)

**Testing:** mockery pattern cho 6 interfaces (HealthCheckConfigRepo, ServerReader, ESStatusLogRepo, HealthChecker, Producer, Consumer). Stabilization verification pass: `shared`, `monitor-service`, `server-service`, `api-gateway`, `tcp-simulator` all `go test ./...` and `go build ./...` OK.

**Bug fixes:** Initial review fixes + stabilization fixes for Kafka offset commits, Redis lock ownership, cache-miss DB status sync, Redis degraded mode, invalid worker count, and `server-service` repository test gate.

**Remaining verification:** Docker E2E flow still needs infrastructure run.

> 📄 Chi tiết: [phase-2-completion.md](./phase-2-completion.md)

---

## 🟢 Phase 3 — Hoàn thành (2026-06-11)

**21 files mới | 26/26 tests PASS | ES aggregation + SMTP gomail + HTML email + daily cron + Redis cache**

| Service | Components | Key features |
|---------|-----------|-------------|
| **report-service** | config, database, model, dto, repository, email, service, handler, scheduler | ES uptime aggregation (bucket_script), Gmail SMTP (gomail.v2), HTML email template, Redis cache-aside 1h, Daily cron (robfig/cron v3), report_jobs tracking, daily_snapshots |

**Endpoints:** `GET /api/v1/reports/summary` (report:view) + `POST /api/v1/reports` (report:send)

**Testing:** mockery (4 interfaces): UptimeCalculator, ReportJobRepo, DailySnapshotRepo, EmailSender. sqlmock repo tests. httptest handler tests. All 6 other modules verified build+test OK — zero regression.

**Remaining verification:** Docker E2E flow (ES phải có dữ liệu từ monitor-service để test thật).

> 📄 Chi tiết: [phase-3-completion.md](./phase-3-completion.md)

---

## 🟢 Phase 4 — Hoàn thành (2026-06-11)

**28 files mới | 77/77 tests PASS | Excel import/export + Kafka async + cross-schema PostgreSQL | core coverage ≥90%**

| Service | Components | Key features |
|---------|-----------|-------------|
| **fileio-service** | config, database, model, dto, excel, repository, service, handler | Excel parser (excelize v2), Excel generator (styled), Async import qua Kafka fail-closed, atomic server+detail tracking, `server.created` publish, Sync export (stream .xlsx), Cross-schema server_schema access, Redis cache invalidation |

**Endpoints:** `POST /api/v1/servers/import` (server:import) + `GET /api/v1/servers/import/:job_id` (server:import) + `POST /api/v1/servers/export` (server:export)

**Testing:** mockery-style function mocks (2 interfaces): ImportJobRepo, ServerWriter. sqlmock repo tests. httptest handler tests. fakeProducer for Kafka failure injection. Core package coverage: excel 90.2%, handler 100%, repository 98.6%, service 90.6%.

**Docker:** Multi-stage Dockerfile (Alpine 3.19), port 8085.

**Remaining verification:** Docker E2E flow cần infrastructure up.

> 📄 Chi tiết: [phase-4-completion.md](./phase-4-completion.md)

---

## 🟢 Phase 1 — Hoàn thành (2026-06-10)

**~55 files | 4 modules build OK | 45+ tests | coverage ≥88% | 14 bug fixes**

| Service | Endpoints | Key features |
|---------|:---------:|-------------|
| **auth-service** | 5 | Register, Login, Refresh rotate, Logout revoke, Profile — JWT HS256, bcrypt, Redis blacklist, brute-force protection |
| **server-service** | 5 | CRUD, filter/sort/pagination, Redis cache-aside, Kafka events, anti-SQLi |
| **api-gateway** | routing | JWT auth, Scope RBAC, Rate limiter fail-closed, CORS configurable, Reverse proxy |

**Shared mới:** `pkg/jwt/` (14 tests, 88% coverage), `middleware/logger.go`

**Testing:** mockery (repo mocks), sqlmock (DB tests), httptest (handler), `.claude/conventions/testing.md`

**Bug fixes:** 6 critical + 4 important + 4 minor (xem [phase-1-bugfix.md](../plan/phase-1-bugfix.md))

> 📄 Chi tiết: [phase-1-completion.md](./phase-1-completion.md) | Test guide: [phase-1-test-guide.md](../test/phase-1-test-guide.md)

---

## 📁 Cấu trúc dự án

```
server-management-system/
├── docker-compose.yml, docker-compose.dev.yml, Makefile, .env, .env.example, .mockery.yaml
├── shared/                 ✅ logger, response, middleware, errors, validator, kafka, pkg/jwt (14 tests)
├── tcp-simulator/          ✅ 10K listeners, Math Engine, Dockerfile
├── api-gateway/            ✅ JWT+Scope+RateLimit+Proxy — 9 files + CORS tests
├── auth-service/           ✅ 5 endpoints JWT/RBAC — 17 files + mocks + sqlmock tests
├── server-service/         ✅ 5 endpoints CRUD+Cache — 15 files + mocks + sqlmock tests
├── monitor-service/        ✅ 22 files — Health-check scheduler, Worker Pool, ES, Kafka
├── report-service/         ✅ 30 files — Uptime aggregation, SMTP email, Daily cron
├── fileio-service/         ✅ 28 files — Excel import/export, Kafka async, Cross-schema PG
├── fileio-service/         ⬜ Structure only
├── deployments/docker/     ✅ init.sql, seed_10k, ES mapping, kafka topics
├── migrations/             ✅ 20 files (auth/server/monitor/report/fileio)
├── docs/                   ✅ 9 design docs + api-spec.yaml
└── .claude/
    ├── conventions/        ✅ testing.md
    ├── plan/               ✅ 7 phase plans (0-5 + bugfix)
    ├── process/            ✅ README.md, phase-0-completion.md, phase-1-completion.md
    └── test/               ✅ phase-1-test-guide.md
```

---

## 🔑 Quyết định thiết kế

| # | Quyết định | Lý do |
|---|-----------|-------|
| 1 | TCP Simulator Pool | 10.000 listeners, mở/đóng theo Math Engine |
| 2 | API Gateway tự viết (Gin) | Full control JWT + Rate Limit + Reverse Proxy |
| 3 | Monorepo + replace directives | Shared libs, 1 docker-compose |
| 4 | 1 Postgres, 5 schemas riêng | Đơn giản vận hành, schema isolation |
| 5 | JWT utility trong shared/ | Dùng chung auth-service + gateway |
| 6 | Redis nil-guard pattern | Service chạy OK dù Redis unavailable |
| 7 | GORM parameterized queries | Chống SQL Injection (whitelist sort columns) |
| 8 | Viper SetConfigFile(.env) | Đọc config từ file .env, không cần export |
| 9 | Mockery + sqlmock | Generate mock tự động, DB test không cần Postgres thật |
| 10 | segmentio/kafka-go | Pure Go, context-native, API đơn giản hơn IBM/sarama, ít transitive deps |
| 11 | ES Aggregation (bucket_script) | Tính uptime ngay trong ES query, tránh load toàn bộ dữ liệu về app |
| 12 | Redis cache-aside 1h TTL (report) | Dữ liệu quá khứ không thay đổi, cache dài hạn hợp lý |
| 13 | robfig/cron v3 | Cron expression linh hoạt "0 8 * * *", khác ticker loop của monitor |
| 14 | gomail.v2 | Thư viện SMTP chuẩn, TLS tích hợp, đơn giản hơn net/smtp |
| 15 | Go html/template | stdlib, không cần dependency ngoài, đủ mạnh cho email template |
| 16 | report_jobs tracking | Audit trail mỗi lần gửi report, status machine |
| 17 | daily_snapshots | Lưu snapshot mỗi ngày để truy vấn nhanh không cần ES |
| 18 | Default viewer role + admin seed | Người dùng mới auto viewer (least privilege), 1 admin seed trong migration, nâng cấp role qua API `user:manage` |

---

## 🟢 Phase 6 — Hoàn thành (2026-06-12)

**16 files sửa đổi | 48+ tests PASS | Security enhancement: Default viewer role + Admin seed + User role management API**

| Service | Endpoints | Key features |
|---------|:---------:|-------------|
| **auth-service** | +2 mới (tổng 7) | `GET /auth/users` (list users, pagination), `PUT /auth/users/{user_id}/role` (nâng cấp/hạ cấp role); Register auto gán viewer; Admin seed trong migration; Chặn tự đổi role chính mình |

**API mới:** 19 endpoints (từ 17). `GET /auth/users` + `PUT /auth/users/{user_id}/role` — scope `user:manage`, chỉ admin.

**Security flow:**
```
Register → auto "viewer" (least privilege)
Admin seed → admin / Admin@123456 (migration)
Admin → GET /auth/users → xem danh sách
Admin → PUT /auth/users/{id}/role → nâng cấp operator/admin
Admin → KHÔNG thể tự đổi role mình (400)
```

**Testing:** Handler mock + service mock cập nhật. Xóa `TestRegister_InvalidRole`. Tất cả tests pass.

> 📄 Chi tiết: [phase-6-completion.md](./phase-6-completion.md)

---

## 📝 Ghi chú

- **Go:** 1.25.0 tất cả module (auto-upgraded bởi gin v1.12)
- **WSL:** Ubuntu trên Windows
- **Kafka:** segmentio/kafka-go (SegmentioProducer + SegmentioConsumer) — thay thế DummyProducer từ Phase 2
- **Redis:** Required cho Login/Refresh/Logout + Rate Limiter
- **Testing:** Mockery cho repository mocks, sqlmock cho DB tests, `make test` để chạy full
- **Coverage target:** ≥ 90% per package (`go test ./... -cover`)
- **Integration test:** `docker compose -f docker-compose.dev.yml up -d` rồi chạy curl flow
- **Last updated:** 2026-06-12
