# Phase 4: File I/O Service — Completion Report

> **Ngày hoàn thành:** 2026-06-11
> **Branch:** `main`
> **Người thực thi:** GitHub Copilot (DeepSeek V4 Pro)

---

## ✅ Checklist Phase 4

| # | Task | Status | Ghi chú |
|---|------|:------:|---------|
| 4.1 | Excel Parser (Import .xlsx) | ✅ | excelize v2, validate headers + required fields (server_id, server_name, ipv4), skip empty rows |
| 4.2 | Excel Generator (Export .xlsx) | ✅ | Styled headers (blue bg, white bold text), 12 columns, auto-fit, GenerateFilename helper |
| 4.3 | Import Service — Async Flow (Kafka) | ✅ | InitiateImport: validate `.xlsx` only → save file → create job → publish Kafka; publish fail marks job failed + cleanup file. ProcessImportJob: parse → check duplicates → atomic insert+detail → publish `server.created` |
| 4.4 | Export Service — Sync Stream | ✅ | Query server_schema → map rows → generate Excel → return buffer + filename |
| 4.5 | Import/Export Handlers | ✅ | 3 endpoints: POST /import (multipart), GET /import/:job_id (status), POST /export (JSON body → binary xlsx) |
| 4.6 | Import Job Repository (tracking) | ✅ | GORM: Create, FindByID, UpdateStatus, UpdateCompleted, UpdateFailed, SaveDetail, CreateServerWithDetail(transaction), SaveDetailsBatch, GetDetailsByJobID |
| 4.7 | Kafka Integration (produce/consume import jobs) | ✅ | segmentio/kafka-go: Producer publish `import.job.created` + `server.created`, Consumer group `fileio-group` |
| 4.8 | Entry Point (main.go) | ✅ | Full wiring (2 DBs + Redis + Kafka + Gin), graceful shutdown HTTP server + Kafka consumer |
| 4.9 | Unit Tests | ✅ | 77 tests: excel (16), handler (11), repository (23), service (27); all core packages coverage ≥ 90% |
| 4.10 | End-to-End Verification | ⚠️ | Code sẵn sàng, cần Docker infrastructure để chạy curl flow |
| 4.11 | Dockerfile | ✅ | Multi-stage build, Alpine 3.19, upload dir created |
| 4.12 | .env Configuration | ✅ | FILEIO_DB_*, SERVER_DB_* (cross-schema), FILEIO_UPLOAD_DIR, FILEIO_MAX_FILE_SIZE_MB |

---

## 📦 Chi tiết từng component

### 4.1. File I/O Service — Cấu trúc (22 files mới + 1 modified)

```
fileio-service/
├── Dockerfile                            ✅ Multi-stage build, port 8085
├── config/config.go                      ✅ Viper, FileIODB, ServerDB, Redis, Kafka, Log, Upload configs
├── cmd/main.go                           ✅ Entry point, full wiring, Kafka consumer, Gin router, graceful shutdown
└── internal/
    ├── database/
    │   ├── postgres.go                   ✅ GORM connection + connection pool
    │   └── redis.go                      ✅ go-redis v9, nil-guard khi Redis unavailable
    ├── model/
    │   ├── import_job.go                 ✅ GORM model → fileio_schema.import_jobs (UUID PK, status machine)
    │   ├── import_job_detail.go          ✅ GORM model → fileio_schema.import_job_details
    │   └── server.go                     ✅ Cross-schema model → server_schema.servers (TableName override)
    ├── dto/
    │   ├── request.go                    ✅ ExportFilter (status, server_name, ipv4, location, os, sort_by, sort_order)
    │   └── response.go                   ✅ ImportJobResponse, ImportJobStatusResponse, ImportRowResult, ServerExportRow
    ├── excel/
    │   ├── parser.go                     ✅ ExcelParser interface: Parse + ValidateHeaders, excelize v2
    │   ├── parser_test.go                ✅ 10 tests
    │   ├── generator.go                  ✅ ExcelGenerator interface: Generate, GenerateFilename
    │   └── generator_test.go             ✅ 6 tests
    ├── repository/
    │   ├── import_job_repo.go            ✅ ImportJobRepo: 9 methods, GORM, status machine, atomic server+detail transaction
    │   ├── server_writer.go              ✅ ServerWriter: cross-schema FindByServerIDOrName, Create, FindAllWithFilter
    │   ├── repository_test.go            ✅ 23 tests: sqlmock cho ImportJobRepo + ServerWriter + transaction rollback
    │   └── mocks/
    │       ├── import_job_repo_mock.go   ✅ Function-callback mock
    │       └── server_writer_mock.go     ✅ Function-callback mock
    ├── service/
    │   ├── import_service.go             ✅ ImportService: InitiateImport, ProcessImportJob, GetImportJobStatus, cacheInvalidator
    │   ├── import_service_test.go        ✅ 23 tests
    │   ├── export_service.go             ✅ ExportService: ExportServers
    │   └── export_service_test.go        ✅ 4 tests
    └── handler/
        ├── import_handler.go             ✅ POST /servers/import + GET /servers/import/:job_id
        ├── import_handler_test.go        ✅ 7 tests (httptest)
        ├── export_handler.go             ✅ POST /servers/export (stream .xlsx)
        └── export_handler_test.go        ✅ 4 tests (httptest)
```

### 4.2. 3 API Endpoints

| Method | Path | Scope | Mô tả | Status code |
|--------|------|:----:|-------|:-----------:|
| POST | `/api/v1/servers/import` | `server:import` | Upload file .xlsx để import bất đồng bộ | 202 |
| GET | `/api/v1/servers/import/:job_id` | `server:import` | Kiểm tra tiến độ job import | 200 |
| POST | `/api/v1/servers/export` | `server:export` | Export danh sách server ra .xlsx | 200 |

### 4.3. Business Logic — ImportService

| Method | Steps |
|--------|-------|
| **InitiateImport** | 1. Validate extension `.xlsx` + file size (≤ 10MB) → 2. Generate job_id (UUID) → 3. Save file to /uploads/{job_id}.xlsx → 4. Create import_job `pending` → 5. Publish Kafka `import.job.created` → 6. Nếu publish fail: mark job `failed` + cleanup file + return error → 7. Return 202 only when queued |
| **ProcessImportJob** | 1. Update job `processing` → 2. Parse Excel → 3. Với mỗi row: validate fields → check duplicate (server_schema) → insert server + success detail trong transaction nếu unique → save failed detail ngay nếu invalid/duplicate/error → publish `server.created` sau insert thành công → 4. UpdateCompleted (total, success, failed) → 5. Invalidate Redis cache |
| **GetImportJobStatus** | 1. Find job by ID → 2. Get details → 3. Build response (success_list + failed_list) → 4. Return |

### 4.4. Business Logic — ExportService

| Method | Steps |
|--------|-------|
| **ExportServers** | 1. Validate filter → 2. Query server_schema.servers (filter, sort, max 50K rows) → 3. Map → []ServerExportRow → 4. Generate Excel buffer → 5. Generate filename → 6. Return buffer + filename |

### 4.5. Import Flow — Sequence

```
Client → Gateway → FileIO Handler
  1. Validate file (.xlsx, ≤ 10MB)
  2. Save file → /uploads/{uuid}.xlsx
  3. INSERT import_jobs (pending)
  4. Publish Kafka "import.job.created"
  5. Nếu publish fail → UPDATE import_jobs failed + xóa file upload + return 400
  6. Return 202 Accepted only when job queued

Kafka Consumer (async):
  1. UPDATE import_jobs → processing
  2. Parse Excel (excelize)
  3. For each row:
     a. Validate (server_id, server_name, ipv4 required)
     b. Check duplicate (SELECT server_schema.servers)
     c. If unique → transaction: INSERT server_schema.servers + INSERT import_job_details(success)
     d. Publish Kafka "server.created" for downstream monitor config
     e. If invalid/duplicate/error → INSERT import_job_details(failed) immediately
  4. UPDATE import_jobs → completed (counts)
  5. Invalidate Redis cache

Client poll:
  GET /api/v1/servers/import/{job_id}
  → SELECT import_jobs + import_job_details
  → Return success_list + failed_list
```

### 4.6. Cross-Schema Database Access

FileIO Service needs access to 2 schemas:
- **fileio_schema** (own): `import_jobs`, `import_job_details` — full CRUD
- **server_schema** (cross-schema): `servers` — SELECT + INSERT (GRANT)

```
PostgreSQL
├── fileio_schema (owned by fileio_user)
│   ├── import_jobs
│   └── import_job_details
└── server_schema (GRANT SELECT, INSERT to fileio_user)
    └── servers
```

---

## 🧪 Test Results

| Package | Tests | Coverage | Ghi chú |
|---------|:-----:|:--------:|---------|
| `excel` | 16 | **90.2%** | Parser (10): valid/invalid headers/empty/missing fields/invalid IPv4/optional/skip empty/file not found/validate headers. Generator (6): valid data/empty/nil optional/large dataset/verify headers/filename |
| `handler` | 11 | **100.0%** | Import (7): valid file/fallback user header/service error/no file/missing job_id/status found/status not found. Export (4): valid/empty request/invalid JSON/service error |
| `repository` | 23 | **98.6%** | ImportJobRepo: CRUD/status invalid UUID paths, SaveDetail, CreateServerWithDetail transaction + rollback, SaveDetailsBatch, GetDetailsByJobID. ServerWriter: duplicate lookup/create/filter/sort |
| `service` | 27 | **90.6%** | Import: valid/invalid ext/.xls reject/file too large/create job error/publish queue failure/process success/duplicates/invalid rows/DB errors/detail errors/update errors/status response/cache invalidation. Export (4): no filter/with filter/empty result/DB error |
| **TOTAL** | **77** | **Core packages ≥ 90%** | `go build ./...` OK, `go test ./...` OK, 0 failures |

> ℹ️ Coverage toàn module vẫn có các package không có test riêng (`cmd`, `config`, `database`, `model`, `mocks`) nên khi tính toàn bộ service sẽ hiển thị 0% ở các package wiring/config. Tiêu chí core Phase 4 đã đạt ở 4 package nghiệp vụ chính.

### Test Infrastructure

| Interface | Mock file | Pattern |
|-----------|-----------|---------|
| `ImportJobRepo` | `repository/mocks/import_job_repo_mock.go` | Function-callback |
| `ServerWriter` | `repository/mocks/server_writer_mock.go` | Function-callback |
| `kafka.Producer` | `service/import_service_test.go` | fakeProducer struct with topic failure injection |
| `cacheInvalidator` | `service/import_service_test.go` | fakeCache for Redis invalidation regression tests |
| DB (sqlmock) | `repository/repository_test.go` | sqlmock.New + GORM SkipDefaultTransaction |

---

## 📊 Build Verification

| Module | `go build ./...` | `go test ./...` |
|--------|:----------------:|:---------------:|
| `fileio-service` | ✅ | ✅ 77/77 PASS |
| `excel` | ✅ | ✅ 16 tests |
| `handler` | ✅ | ✅ 11 tests |
| `repository` | ✅ | ✅ 23 tests |
| `service` | ✅ | ✅ 27 tests |

### Latest verification

```bash
cd server-management-system/fileio-service
go build ./...
go test ./internal/excel ./internal/handler ./internal/repository ./internal/service -cover -count=1
```

Kết quả coverage core packages:

```text
excel       90.2%
handler     100.0%
repository  98.6%
service     90.6%
```

---

## 📁 File Inventory

| Module | New files | Modified files | Total LOC (est.) |
|--------|:---------:|:--------------:|:----------------:|
| `fileio-service/config/` | 1 | 0 | ~100 |
| `fileio-service/cmd/` | 1 | 0 | ~180 |
| `fileio-service/Dockerfile` | 1 | 0 | ~25 |
| `fileio-service/internal/database/` | 2 | 0 | ~60 |
| `fileio-service/internal/model/` | 3 | 0 | ~120 |
| `fileio-service/internal/dto/` | 2 | 0 | ~60 |
| `fileio-service/internal/excel/` | 4 | 0 | ~400 |
| `fileio-service/internal/repository/` | 4 | 0 | ~350 |
| `fileio-service/internal/repository/mocks/` | 2 | 0 | ~100 |
| `fileio-service/internal/service/` | 4 | 0 | ~650 |
| `fileio-service/internal/handler/` | 4 | 0 | ~250 |
| `.env` | 0 | 1 | Updated |
| **TOTAL** | **28** | **1** | **~2,200** |

---

## 🔑 Quyết định thiết kế Phase 4

| # | Quyết định | Lý do |
|---|-----------|-------|
| 22 | **Async import qua Kafka** | Import file lớn (10MB, hàng nghìn dòng) không nên block HTTP request. Client nhận 202 Accepted ngay, poll status sau |
| 23 | **Skip duplicate, không halt** | Đúng yêu cầu đề bài: "Bỏ qua các server_id hoặc server_name đã tồn tại". Mỗi dòng độc lập, không rollback |
| 24 | **Cross-schema INSERT** | FileIO ghi trực tiếp server_schema.servers qua GRANT. Đơn giản hơn gọi API server-service (tránh HTTP overhead cho batch import) |
| 25 | **excelize v2** | Thư viện Go thuần, không cần CGO, hỗ trợ .xlsx đầy đủ |
| 26 | **Export sync (stream)** | Export là read-only, dữ liệu có sẵn trong PG, không cần async. Stream thẳng .xlsx về client |
| 27 | **Status machine** | pending→processing→completed/failed. Mỗi row trong details có status riêng (success/failed + error_reason) |
| 28 | **Redis cache invalidation** | Sau import thành công, quét và xóa keys `servers:list:*` + `server:detail:*` để server-service cache được refresh |
| 29 | **2 DB connections** | FileIO cần fileio_schema (chủ) + server_schema (cross-schema). Dùng 2 GORM connections riêng biệt |
| 30 | **Mockable Kafka producer** | fakeProducer struct trong test, không phụ thuộc Kafka thật |
| 31 | **Queue publish failure fail-closed** | Import chỉ trả 202 khi `import.job.created` publish thành công; nếu Kafka queue fail thì job bị mark `failed` và file upload được cleanup để tránh kẹt `pending` |
| 32 | **Atomic insert server + success detail** | `CreateServerWithDetail` dùng GORM transaction để tránh case server đã tạo nhưng import detail/job tracking fail |
| 33 | **Publish `server.created` từ import** | Server import thành công vẫn phát event như server-service create flow để monitor-service nhận và tạo health-check config |
| 34 | **Cache invalidator adapter** | Tách Redis scan/delete sau interface nhỏ để test được invalidation path mà không cần Redis thật |
| 35 | **HTTP graceful shutdown** | `main.go` gọi `srv.Shutdown` khi nhận SIGINT/SIGTERM, đồng thời cancel Kafka consumer context |

---

## ⚠️ Technical Debt & Lưu ý

| # | Vấn đề | Mức độ | Cách khắc phục |
|---|--------|:------:|---------------|
| 1 | E2E verification chưa chạy | 🟡 Medium | Cần Docker infrastructure up (Postgres, Kafka) |
| 2 | Service coverage thấp sau review | ✅ Done | Đã bổ sung regression tests; service coverage 90.6%, core packages đều ≥ 90% |
| 3 | ServerWriter chưa có mock test riêng | 🟢 Low | Đã test qua sqlmock + service tests dùng mock |
| 4 | Gateway routes đã có sẵn từ Phase 1 | ✅ Done | server:import + server:export scopes |
| 5 | Migrations đã có sẵn từ Phase 0 | ✅ Done | 000001_create_import_jobs, 000002_create_import_job_details |
| 6 | Docker Compose chưa có fileio-service | 🟢 Low | Cần thêm service definition vào docker-compose.yml |
| 7 | Kafka queue failure có thể làm job kẹt pending | ✅ Done | Publish failure mark job failed + cleanup upload + return error |
| 8 | Import thiếu `server.created` event | ✅ Done | `ProcessImportJob` publish event sau atomic insert thành công |
| 9 | Server insert và import detail không atomic | ✅ Done | Thêm `CreateServerWithDetail` transaction + rollback regression test |
| 10 | Shutdown chưa dừng HTTP server | ✅ Done | Thêm `srv.Shutdown` với timeout 30s |

---

## 🔜 Next: Phase 5 — Polish & Documentation

Cần triển khai:
- Tăng coverage toàn hệ thống ≥ 90%
- Docker Compose đầy đủ 6 services
- E2E testing với curl flow
- Tài liệu hướng dẫn sử dụng + ảnh chụp màn hình
- OpenAPI spec hoàn chỉnh
- README.md tổng quan dự án

---

> **Kết luận:** Phase 4 đã hoàn thiện sau review/fix, 28 files mới + 1 file modified (.env), 77/77 tests pass. Build + test OK trên tất cả packages. Core packages đạt coverage ≥ 90%. File I/O Service hoạt động với import async qua Kafka theo hướng fail-closed khi queue lỗi, tracking import bền hơn nhờ transaction server+detail, phát `server.created` cho downstream, graceful shutdown HTTP/Kafka, và export sync (query → generate Excel → stream download). Ready cho Phase 5 sau khi chạy E2E với Docker infrastructure.
