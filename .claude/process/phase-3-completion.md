# Phase 3: Report Service — Completion Report

> **Ngày hoàn thành:** 2026-06-11
> **Branch:** `main`
> **Người thực thi:** GitHub Copilot (DeepSeek V4 Pro)
> **Cập nhật sau review/fix:** Codex — hoàn thiện các bug Phase 3, bổ sung regression tests và nâng coverage core packages ≥ 90%

---

## ✅ Checklist Phase 3

| # | Task | Status | Ghi chú |
|---|------|:------:|---------|
| 3.1 | ES Uptime Calculator | ✅ | 2 method: GetUptimeSummary (bucket_script aggregation) + GetLowUptimeServers (top N), io.Reader-based parsing |
| 3.2 | Gmail SMTP Email Sender | ✅ | gomail.v2, TLS v1.2, interface EmailSender, App Password auth |
| 3.3 | HTML Email Template | ✅ | Go html/template, responsive design, stats cards + low-uptime table |
| 3.4 | Report Service (Business Logic) | ✅ | GetSummary (Redis cache-aside 1h + PG total server count), SendReport (pending→processing→completed/failed), SendDailyReport (daily job tracking + snapshot) |
| 3.5 | Report Handler | ✅ | 2 endpoints: GET /reports/summary + POST /reports, validation dates/email, inclusive end_date, max 90-day range |
| 3.6 | Daily Report Scheduler (Cron) | ✅ | robfig/cron v3, 5-field schedule `"0 8 * * *"`, graceful shutdown via context |
| 3.7 | Kafka Consumer | ⬜ | Optional — Report Service query thẳng ES khi cần, không bắt buộc |
| 3.8 | Redis Cache | ✅ | Key: report:summary:{start}:{end}, TTL=1h, cache full summary including low-uptime list |
| 3.9 | Entry Point (main.go) | ✅ | Full wiring (DB + Redis + ES + SMTP + Cron), HTTP :8084, graceful shutdown |
| 3.10 | Unit Tests | ✅ | 61 tests: core packages all pass; email/handler/repository/scheduler/service coverage đều ≥ 90% |
| 3.11 | End-to-End Verification | ⚠️ | Code sẵn sàng, cần Docker infrastructure để chạy curl flow |

---

## 📦 Chi tiết từng component

### 3.1. Report Service — Cấu trúc (30 files mới + 1 file modified)

```
report-service/
├── config/config.go                     Viper, SMTP, ES, DB, Redis, Kafka, Log configs
├── cmd/main.go                          Entry point, full wiring, cron + HTTP, graceful shutdown
└── internal/
    ├── database/
    │   ├── postgres.go                  GORM connection + connection pool (report_schema)
    │   └── redis.go                     go-redis v9, nil-guard khi Redis unavailable
    ├── model/
    │   ├── report_job.go                GORM model → report_schema.report_jobs (UUID PK, status machine)
    │   └── daily_snapshot.go            GORM model → report_schema.daily_snapshots (JSONB low_uptime_servers)
    ├── dto/
    │   ├── request.go                   SendReportRequest (start_date, end_date, email)
    │   └── response.go                  ReportSummaryResponse, SendReportResponse, ServerUptime
    ├── repository/
    │   ├── es_uptime_repository.go      UptimeCalculator interface: GetUptimeSummary (ES bucket_script agg) + GetLowUptimeServers (top N ASC)
    │   ├── report_job_repository.go     ReportJobRepo interface: Create, Update, FindByID, FindByDateRange
    │   ├── daily_snapshot_repository.go DailySnapshotRepo interface: Create, FindByDate, FindByDateRange
    │   ├── server_counter_repository.go ServerCounter: COUNT active servers từ server_schema.servers
    │   ├── repository_test.go           sqlmock + ES mock transport regression tests
    │   └── mocks/                       UptimeCalculatorMock, ReportJobRepoMock, DailySnapshotRepoMock, ServerCounterMock
    ├── email/
    │   ├── smtp_sender.go               GmailSender: gomail.v2, TLS v1.2, mockable dialer factory, ctx cancel guard
    │   ├── smtp_sender_test.go          SMTP success/error/context-cancel tests without real SMTP
    │   ├── template_renderer.go         Go html/template renderer, testable template path resolver
    │   ├── template_renderer_test.go    Template success/empty/error-path tests
    │   ├── templates/
    │   │   └── daily_report.html        Responsive HTML email: gradient header, 4 stat cards, top N low-uptime table
    │   └── mocks/                       EmailSenderMock (function-callback)
    ├── service/
    │   ├── report_service.go            ReportService: cache-aside, PG count, job lifecycle, daily snapshot
    │   └── report_service_test.go       24 tests covering cache/date/job/SMTP/snapshot failures
    ├── handler/
    │   ├── report_handler.go            2 endpoints: GET summary + POST send report
    │   └── report_handler_test.go       11 tests covering validation + service errors
    └── scheduler/
        ├── daily_report_cron.go         robfig/cron v3, "0 8 * * *" default, context-graceful shutdown
        └── daily_report_cron_test.go    Schedule parse/start/trigger/cancel tests
```

### 3.2. 2 API Endpoints

| Method | Path | Scope | Mô tả | Status code |
|--------|------|:----:|-------|:-----------:|
| GET | `/api/v1/reports/summary?start_date=&end_date=` | `report:view` | Uptime summary từ ES (cache Redis 1h) | 200 |
| POST | `/api/v1/reports` | `report:send` | Gửi báo cáo email on-demand, tracking trong report_jobs | 200 |

### 3.3. Business Logic — ReportService

| Method | Steps |
|--------|-------|
| **GetSummary** | 1. Check Redis cache (key: `report:summary:{start}:{end}`) → 2. Cache hit: return full cached summary, không query ES → 3. Cache miss: query ES GetUptimeSummary + GetLowUptimeServers(top 10) → 4. Query PG CountActiveServers từ `server_schema.servers` → 5. Cache full summary TTL=1h → 6. Return |
| **SendReport** | 1. Validate input (date format, end_date >= start_date, range <= 90 days) → 2. Create report_job `pending` → 3. Update `processing` → 4. GetSummary với end-date exclusive nội bộ → 5. Render HTML template → 6. Send email qua SMTP → 7. Update job `completed` hoặc `failed` kèm error_message → 8. Return response |
| **SendDailyReport** | 1. Calculate yesterday `[00:00, next day 00:00)` → 2. Create daily report_job `pending` → 3. Update `processing` → 4. GetSummary → 5. Render HTML → 6. Send email to SMTP_ADMIN_EMAIL → 7. Save daily_snapshot → 8. Update job `completed` hoặc `failed` |

### 3.3.1. Bugfixes sau code review

| # | Bug/Rủi ro | Trạng thái sau fix |
|---|------------|--------------------|
| 1 | Cron dùng `cron.WithSeconds()` nhưng schedule chỉ có 5 fields `"0 8 * * *"` khiến scheduler không đăng ký được job | ✅ Bỏ `WithSeconds()`, thêm tests parse/start/trigger |
| 2 | `total_servers` lấy từ số bucket ES, bỏ sót server không có log trong range | ✅ Thêm `ServerCounterRepo` query `server_schema.servers WHERE deleted_at IS NULL` |
| 3 | Daily report gửi email trước rồi mới tạo job, lỗi SMTP/summary không có record failed | ✅ Daily flow tạo job trước, status machine `pending→processing→completed/failed` |
| 4 | Handler/service cộng `end_date + 1 day` trước validate, có thể nhận range ngược ngày | ✅ Validate input inclusive trước, chỉ dùng exclusive end-date khi query ES/cache |
| 5 | Cache hit vẫn query ES để lấy low-uptime list; cache miss không lưu low-uptime | ✅ Cache lưu full summary; cache hit return ngay |
| 6 | SMTP/template/ES repo khó test vì phụ thuộc infrastructure thật | ✅ Tách mockable SMTP dialer, template path resolver, ES mock HTTP transport |

### 3.4. ES Uptime Calculator — Query Design

```
ES Index: server-status-logs
Query type: Aggregation (size: 0)
  └─ per_server (terms on server_id, size 10000)
       ├─ total_checks (value_count on status)
       ├─ on_checks (filter status=on)
       ├─ latest_check (top_hits size=1, sort checked_at desc) → server_name + current status
       └─ uptime_rate (bucket_script: on_checks / total_checks * 100)
  └─ avg_uptime (avg_bucket on per_server>uptime_rate)
```

### 3.5. HTML Email Template

- Gradient header (#667eea → #764ba2)
- 4 stat cards: Tổng Server, 🟢 Online, 🔴 Offline, 📈 Avg Uptime
- Table: Top N server có uptime thấp nhất (Server ID, Server Name, Uptime %)
- Footer: "VCS Server Management System — Automated Report"
- No-data handling khi không có low-uptime servers

---

## 🧪 Test Results

| Package | Tests | Coverage | Ghi chú |
|---------|:-----:|:--------:|---------|
| `email` | 8 | **94.1%** | SMTP sender success/error/context-cancel/default dialer; template success/empty/path-error/parse-error |
| `handler` | 11 | **94.3%** | Valid dates, missing/invalid dates, previous-day end rejected, range>90d, service errors, send request validation |
| `repository` | 14 | **90.2%** | GORM/sqlmock repos, ServerCounterRepo, ES parser, ES client mock transport, ES error paths |
| `scheduler` | 4 | **93.8%** | Default 5-field cron schedule, invalid schedule, context cancel, trigger with `@every` |
| `service` | 24 | **90.1%** | Cache hit/miss, PG total count, date validation, SendReport lifecycle, daily job failed/completed paths |
| **TOTAL** | **61** | **Core packages ≥ 90%** | `go test ./...` PASS, 0 failures |

> ℹ️ Coverage toàn module vẫn có các package không có test riêng (`cmd`, `config`, `database`, `model`, `mocks`) nên khi tính toàn bộ service sẽ hiển thị 0% ở các package wiring/config. Tiêu chí core Phase 3 đã đạt ở 5 package nghiệp vụ chính.

### Test Infrastructure

| Interface | Mock file | Pattern |
|-----------|-----------|---------|
| `UptimeCalculator` | `repository/mocks/uptime_calculator_mock.go` | Function-callback |
| `ReportJobRepo` | `repository/mocks/report_job_repo_mock.go` | Function-callback |
| `DailySnapshotRepo` | `repository/mocks/daily_snapshot_repo_mock.go` | Function-callback |
| `ServerCounter` | `repository/mocks/server_counter_mock.go` | Function-callback |
| `EmailSender` | `email/mocks/email_sender_mock.go` | Function-callback |
| SMTP dialer | `email/smtp_sender_test.go` | Package-level override, no real SMTP |
| ES client | `repository/repository_test.go` | Mock `http.RoundTripper`, no real Elasticsearch |

---

## 📊 Build Verification

| Module | `go build ./...` | `go test ./...` |
|--------|:----------------:|:---------------:|
| `shared` | ✅ | ✅ |
| `auth-service` | ✅ | — |
| `server-service` | ✅ | ✅ |
| `api-gateway` | ✅ | ✅ |
| `monitor-service` | ✅ | ✅ |
| `tcp-simulator` | ✅ | ✅ |
| **`report-service`** | ✅ | ✅ 61/61 PASS |

### Latest verification sau fix

```bash
cd server-management-system/report-service
go test ./...
go test ./internal/email ./internal/handler ./internal/repository ./internal/scheduler ./internal/service -cover
```

Kết quả coverage core packages:

```text
email       94.1%
handler     94.3%
repository  90.2%
scheduler   93.8%
service     90.1%
```

---

## 📁 File Inventory

| Module | New files | Modified files | Total LOC (est.) |
|--------|:---------:|:--------------:|:----------------:|
| `report-service/config/` | 1 | 0 | ~120 |
| `report-service/cmd/` | 1 | 0 | ~130 |
| `report-service/internal/database/` | 2 | 0 | ~50 |
| `report-service/internal/model/` | 2 | 0 | ~100 |
| `report-service/internal/dto/` | 2 | 0 | ~50 |
| `report-service/internal/repository/` | 5 | 0 | ~500 |
| `report-service/internal/repository/mocks/` | 4 | 0 | ~120 |
| `report-service/internal/email/` | 4 | 0 | ~250 |
| `report-service/internal/email/mocks/` | 1 | 0 | ~20 |
| `report-service/internal/service/` | 2 | 0 | ~700 |
| `report-service/internal/handler/` | 2 | 0 | ~200 |
| `report-service/internal/scheduler/` | 2 | 0 | ~120 |
| `report-service/go.sum` | 1 | 0 | Generated dependency lock |
| `report-service/go.mod` | 0 | 1 | Updated |
| **TOTAL** | **30** | **1** | **~2,700** |

---

## ⚠️ Technical Debt & Lưu ý

| # | Vấn đề | Mức độ | Cách khắc phục |
|---|--------|:------:|---------------|
| 1 | E2E verification chưa chạy | 🟡 Medium | Cần Docker infrastructure up (Postgres, Redis, Kafka, ES) |
| 2 | Dockerfile cho report-service chưa có | 🟢 Low | Tạo multi-stage Dockerfile như auth/server-service |
| 3 | ✅ ES Repository coverage thấp | ✅ Done | Đã dùng mock HTTP transport, repository coverage 90.2% |
| 4 | ✅ SMTP Sender coverage thấp | ✅ Done | Đã tách mockable dialer factory, email coverage 94.1% |
| 5 | ✅ Redis cache khó test | ✅ Done | Đã tách `summaryCache` + regression tests cache hit/miss |
| 6 | ✅ report_jobs tracking daily chưa đầy đủ | ✅ Done | Daily report tạo job trước và update failed/completed đúng status |
| 7 | ✅ Gateway routes đã có sẵn từ Phase 1 | ✅ Done | report:view + report:send scopes |

---

## 🔑 Quyết định thiết kế mới

| # | Quyết định | Lý do |
|---|-----------|-------|
| 11 | ES Aggregation (bucket_script) | Tính uptime ngay trong ES query, tránh load toàn bộ dữ liệu về app |
| 12 | Redis cache-aside 1h TTL | Dữ liệu quá khứ không thay đổi, cache dài hạn hợp lý |
| 13 | robfig/cron v3 | Cron expression linh hoạt "0 8 * * *", khác với ticker loop của monitor |
| 14 | gomail.v2 | Thư viện SMTP chuẩn, TLS tích hợp, đơn giản hơn net/smtp |
| 15 | Go html/template | stdlib, không cần dependency ngoài, đủ mạnh cho email template |
| 16 | report_jobs tracking | Audit trail mỗi lần gửi report, status machine pending→processing→completed/failed |
| 17 | daily_snapshots | Lưu snapshot mỗi ngày để truy vấn nhanh không cần ES |
| 18 | ServerCounterRepo | `total_servers` phải lấy từ PostgreSQL inventory, không lấy từ số bucket ES |
| 19 | Inclusive API dates + exclusive ES range | API/job/cache dùng ngày người dùng nhập; query ES dùng `[start, end+1day)` để không mất dữ liệu cuối ngày |
| 20 | Full-summary cache | Cache hit không query ES; cached summary bao gồm low-uptime list để đúng cache-aside pattern |
| 21 | Mockable infra clients | SMTP/ES/template path được tách seam test để unit test không cần infrastructure thật |

---

## 🔜 Next: Phase 4 — File I/O Service

Cần triển khai:
- Import Servers từ Excel (.xlsx) qua Kafka async job
- Export Servers ra Excel với filter/sort
- File I/O Service: handler upload/download, background worker xử lý import
- Integration: FileIO ↔ Server Service ↔ Kafka

---

> **Kết luận:** Phase 3 đã hoàn thiện sau review/fix, 30 files mới + 1 file modified, 61/61 tests pass. Core packages đạt coverage ≥ 90%, các lỗi cron/date/cache/PG total server count/report_jobs tracking đã được xử lý. Ready cho Phase 4 sau khi chạy E2E với Docker infrastructure.
