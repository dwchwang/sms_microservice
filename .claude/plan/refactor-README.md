# 📋 Refactor Plan — Chuyển đổi sang thiết kế mới

> **Trạng thái:** ✅ **Hoàn tất R1 → R7** (cập nhật 17/07/2026)
>
> **Mục tiêu:** Refactor hệ thống cũ (5 service + Kafka + shared DB) sang thiết kế mới
> (4 service + Redis Stream + database-per-service)
>
> **Tài liệu tham chiếu:** `design.md` (thiết kế mới) + `refactor.md` (đối chiếu chi tiết)
>
> **Nguyên tắc:** Refactor theo phase, mỗi phase kết thúc hệ thống vẫn chạy được. Không big-bang.

---

## Checklist tổng quan

- [x] **R1** Infrastructure & Shared
- [x] **R2** Identity Service
- [x] **R3** Server Service — Core
- [x] **R4** Server Service — Advanced
- [x] **R5** Monitoring Service
- [x] **R6** Reporting Service
- [x] **R7** Cleanup & end-to-end verification

---

## Tổng quan 7 Refactor Phases

| Phase | Tên | Mục tiêu chính | Trạng thái |
|-------|-----|----------------|------------|
| **[R1](./refactor-phase-1-infrastructure.md)** | Infrastructure & Shared | Xóa Kafka, thêm Traefik, tách 3 DB, viết lại init.sql, sửa shared module, thêm lumberjack | ✅ |
| **[R2](./refactor-phase-2-identity.md)** | Identity Service | Đổi DB → identity_db, thêm `/internal/verify`, đổi scope names, Argon2id, brute-force protection | ✅ |
| **[R3](./refactor-phase-3-server-core.md)** | Server Service — Core | Đổi model (thêm cột, đổi status), đổi DB → server_db, đổi cache-aside, bỏ Kafka producer | ✅ |
| **[R4](./refactor-phase-4-server-advanced.md)** | Server Service — Advanced | Redis target projection, Redis Stream consumer, gộp import/export từ FileIO, internal API, CIDR validator, idempotency | ✅ |
| **[R5](./refactor-phase-5-monitoring.md)** | Monitoring Service | Viết lại scheduler (Redis rounds), worker (BRPOP), bỏ PostgreSQL, Lua script atomic, deterministic ES doc ID | ✅ |
| **[R6](./refactor-phase-6-reporting.md)** | Reporting Service | Snapshot job 00:30, daily_snapshots per-server, internal API client, Gmail SMTP, delivery_unknown, coverage | ✅ |
| **[R7](./refactor-phase-7-cleanup.md)** | Cleanup & Verification | Xóa FileIO + api-gateway, dọn rác, end-to-end test, OpenAPI | ✅ |

---

## Kiến trúc sau refactor

```
Client → Traefik (:8080)
           ├─ /api/v1/auth      → auth-service   (identity_db)
           ├─ /api/v1/servers   → server-service (server_db)  ┐ ForwardAuth
           └─ /api/v1/reports   → report-service (report_db)  ┘ → auth-service

monitor-service  — không có public endpoint, không có PostgreSQL
                   đọc Redis target projection → ping → Redis status + Lua XADD → ES

stream:monitor.status : monitor-service → server-service (consumer group "server-svc")
```

**Container cuối cùng (9 + web):** `postgres`, `redis`, `elasticsearch`, `tcp-simulator`,
`traefik`, `auth-service`, `server-service`, `monitor-service`, `report-service`, `web`.

**Database:** `identity_db`, `server_db`, `report_db` — không còn schema dùng chung.

### Đã xóa hẳn

`api-gateway/`, `fileio-service/`, `shared/kafka/`, `migrations/` (nhắm schema cũ),
`init.sql` v1 (5 schema + 5 user chết), `deployments/docker/kafka/`,
`deployments/docker/elasticsearch/mapping.json` (index template thay thế), `uploads/`,
2 file mock viết tay không ai import.

Rà cuối: **không còn dấu vết** Kafka / FileIO / api-gateway / `*_schema` / `health_check_configs`
trong `*.go`, `*.yml`, `*.sql`, `Makefile`, `.env*`.

---

## Test

| Module | Tests |
|---|---:|
| shared | 16 |
| auth-service | 83 |
| server-service | 175 |
| monitor-service | 35 |
| report-service | 62 |
| tcp-simulator | 15 |
| **Tổng** | **386** |

6/6 module: `go build` ✅ `go vet` ✅ `go test` ✅

---

## Các quyết định lệch plan/design (có chủ đích)

Ghi lại để người đọc sau không tưởng là code sai.

| # | Chỗ lệch | Lý do |
|---|---|---|
| 1 | **Lua script trong `refactor.md` §2.4 có bug** — dùng `old_status == nil` để bắt lần check đầu | Redis Lua trả `false` cho field không tồn tại, **không phải `nil`**. Điều kiện đó vĩnh viễn sai → event `UNKNOWN → ON/OFF` đầu tiên không bao giờ phát → server mới kẹt `UNKNOWN` mãi. Code dùng `old_status == false`. |
| 2 | **`round_id` lấy từ Redis TIME**, không phải `time.Now()` như plan R5.2 | Design §8.3. Clock drift giữa các instance sẽ khiến chúng nạp/đọc round khác nhau. |
| 3 | **`RoundSeconds` là hằng số**, không phải config | Hai instance cấu hình khác nhau → `round_id` không thống nhất → vỡ toàn bộ cơ chế. Đã bỏ `MONITOR_CHECK_INTERVAL` khỏi `.env`. |
| 4 | **`server_name` thêm vào target projection** (design §7.4 chỉ có `ipv4` + `tcp_port`) | §12.1 bắt ES document phải có `server_name`, nhưng Monitoring không đọc PostgreSQL nữa → không có nguồn nào khác. Hai mục design mâu thuẫn; đã chọn theo §12.1 để giữ ngữ nghĩa "tên tại thời điểm check". |
| 5 | **Tự sinh Message-ID**, không parse từ response Gmail (design §9.9) | Dòng `250` của Gmail mang **queue ID**, không phải Message-ID. Tự sinh là chuẩn RFC 5322 và đúng mục đích §9.8 (operator tra hộp thư Sent). |
| 6 | **`net/smtp` thay `gomail`** | `gomail.DialAndSend` chỉ trả `error`, không cho biết lỗi xảy ra **trước hay sau** khi body lên dây → không thể phân biệt `failed` vs `delivery_unknown`. |
| 7 | **Index template thay `EnsureIndexMapping`** | Code cũ tạo mapping cho **một** index cố định và **DELETE index** nếu mapping sai — với daily index thì vừa không chạy được vừa cực nguy hiểm. |
| 8 | **Consumer tạo lại group tại `0`, không phải `$`** (khi phục hồi) | Monitoring chỉ phát event khi status **đổi**. Bỏ qua event cũ sẽ để server kẹt trạng thái sai đến lần đổi tiếp theo. Replay an toàn nhờ version guard. Lúc boot lần đầu vẫn dùng `$` theo design §7.6. |
| 9 | **Import response dùng `succeeded`/`failed`/`skipped_duplicate`** (plan R4.3 ghi `success`/`failed`/`skipped`) | Theo design §7.8. |
| 10 | **Idempotency chỉ cho `POST /servers` + `POST /servers/import`** (plan R4.6 gợi ý cả export) | Design §7.7. Export là thao tác đọc (dùng POST chỉ vì filter dài) — thêm idempotency sẽ tạo row rác trong `api_idempotency`. |
| 11 | **`internal/monitor` gộp scheduler + worker + Lua** (plan R5 tách 2 file) | Chúng dùng chung Redis contract quá chặt; tách package chỉ tạo indirection. |
| 12 | **`exportPageSize = 100`** | `FindAll` cap page_size ở 100 để bảo vệ list API. Đặt 1000 sẽ bị clamp âm thầm — hằng số nói dối. Đổi lại: export 10.000 server tốn 100 query. |

---

## Bug tìm ra khi verify thật (unit test không bắt được)

| # | Bug | Root cause |
|---|---|---|
| 1 | **`POST /servers` trả 500** khi request không gửi `cpu_cores` | `CHECK (cpu_cores IS NULL OR > 0)` nhưng GORM insert `0`. Bug có sẵn từ R3. |
| 2 | **`PUT /servers` trả 500** — cùng triệu chứng, sâu hơn | Cột `NULL` được GORM scan vào `int` thành **0**, `Save()` ghi 0 ngược lại. `int` không phân biệt "chưa set" và "bằng 0" → phải dùng `*int`. |
| 3 | **Consumer không bao giờ phục hồi khi mất consumer group** | Tạo group một lần lúc boot. Redis restart không persistence / `FLUSHDB` / `maxmemory-policy allkeys-lru` evict key stream → quay vòng vô hạn với `NOGROUP` trong khi Monitoring vẫn ghi. **Status trong PostgreSQL đứng im vĩnh viễn**, chỉ có log báo. Đã thêm tự phát hiện + tạo lại group (~4s). |
| 4 | **`seed_10k_servers.sql` ghi vào schema đã chết** | Ghi `server_schema.servers` + `monitor_schema.health_check_configs`. "Chạy được" nhờ `init.sql` v1 còn tạo schema đó → seed vào bảng không ai đọc, Monitor thấy 0 target. Xóa `init.sql` mà giữ seed thì Postgres init **fail** và cả stack không lên. Đã viết lại cho `server_db`. |
| 5 | **`SMTP_FROM=VCS-SMS <email@gmail.com>` làm hỏng `MAIL FROM` và Message-ID** | Display name bị nhét nguyên vào envelope (SMTP chỉ nhận địa chỉ trần), domain bị cắt thành `gmail.com>` kèm dấu ngoặc. Fix bằng `mail.ParseAddress`. |
| 6 | **Test report sẽ hết hạn vào ngày hôm sau** | `Summary` gọi `time.Now()` bên trong → test pass chỉ vì hôm đó tình cờ là ngày phù hợp. Đã tiêm clock. |
| 7 | **OpenAPI cho phép client sửa `status`** | Code đã chặn (DTO không có field đó), spec nói dối. |

---

## Verify end-to-end đã chạy

Trên stack sạch (`docker compose down -v` → `up --build`):

| Hạng mục | Kết quả |
|---|---|
| Init fresh | 10 container lên, **0 schema cũ**, đúng 3 database |
| Identity | Login qua Traefik → JWT; không token → **401** |
| CIDR allowlist | loopback → **422** `SERVER_IP_NOT_ALLOWED` |
| Idempotency | Cùng key+body → replay **201 không tạo row thứ 2**; key trùng body khác → **409** |
| Target projection | Create ghi hash+set; delete gỡ cả hai; rebuild sửa được 3 loại drift |
| **Version guard** | Event cũ và trùng đều no-op; `list:version` **đứng yên** |
| Poison message | Event hỏng được ACK, `XPENDING` = 0 |
| Lua script (5 case) | first-check→1, unchanged→2, stale→0, replay→0, transition→1 |
| `last_status_check` | Cache key có version, entry cache **không** chứa field này, cache hit vẫn đọc tươi từ Redis |
| **`list:version` qua trọn 1 round** | **Đứng nguyên**, trong khi `last_checked_at` nhích và ES +3 doc — luận điểm trung tâm §6.3 |
| Import | 9 dòng → 2 succeeded / 3 failed / 4 skipped, `duration_ms` có; optionals lưu **NULL** không phải 0 |
| Export | xlsx thật, 3 payload formula injection đều bị prefix `'` |
| Monitor | Ping TCP Simulator thật → 3/3 **ON**; deterministic `_id` = `SRV-00002:29737001` |
| Chuỗi đầy đủ | ping → Redis status → Lua XADD → consumer → PostgreSQL `status=ON` |
| TTL | round lock ~120s tự dọn; `monitor:status:*` TTL = **-1** |
| Snapshot job | `expected_checks = 1410` (không phải 1440 — server tạo lúc 00:30); coverage phơi bày đúng |
| **Bất biến report §9.4** | `4 = 1+1+1+1` và `4 = 1+2+1`; `avg = 50` = AVG(100,50,0) trên **3** server có data (no_data bị `NULL` loại đúng); `coverage = 75%` (no_data góp mẫu số, **không biến mất**); `top_10` loại no_data |
| Report guard | Hôm nay → `REPORT_INVALID_RANGE`; thiếu snapshot → `REPORT_DATA_UNAVAILABLE` **nêu tên ngày** |
| SMTP | Bắt tay thật với Gmail: STARTTLS ok, AUTH `535` (credential placeholder) → state **`failed`** đúng (không phải `delivery_unknown`, vì auth fail **trước** DATA); `smtp_message_id` rỗng; summary vẫn được lưu |
| Consumer recovery | `FLUSHDB` giữa lúc chạy → tự tạo lại group trong **~4s**, không cần restart |

---

## ⚠️ Còn tồn đọng

### 1. Chưa gửi email thật
`.env` đang là App Password placeholder. Đường SMTP đã chạy đến bước auth và bị Gmail
từ chối đúng như mong đợi — chỉ cần điền credential thật.

**Cần:** bật 2-Step Verification → tạo App Password 16 ký tự → điền `SMTP_USERNAME`,
`SMTP_PASSWORD`, `SMTP_FROM`, `REPORT_DAILY_RECIPIENT`.

Nhớ set `SMTP_RECIPIENT_DOMAINS` trước khi lên production — để rỗng nghĩa là **cho phép
gửi tới bất kỳ ai**, tức hệ thống có thể bị lợi dụng làm mail relay (design §9.9).

### 2. Chưa load test 10.000 server
Mới verify với 3–5 server. Seed đã đúng schema nên chạy được:

```bash
make seed           # 10.000 row vào server_db, tcp_port = 9000 + index
make rebuild-cache  # bơm Redis target projection — không có bước này Monitor bỏ qua mọi round
```

**Chưa đo được (design yêu cầu ghi lại con số thật):**
- Công thức sizing worker §8.5: `worker >= ceil(10000 × 3s / 60s) = 500`, +20% → 600 goroutine.
  Chưa biết 200 worker/instance × 3 instance có đủ không.
- Chi phí `top_hits` trên 14,4 triệu document/ngày §12.5. Nếu vượt ngưỡng, phương án thay
  thế là bỏ `top_hits`, lấy `last_status` bằng query riêng với `collapse` theo `server_id`.
- Thời gian import 10.000 dòng (design §7.8 ước tính ~8s).

### 3. Chưa có metrics endpoint
Design §8.5 liệt kê 7 metric **bắt buộc**: `round_duration`, `targets_expected`,
`checks_completed`, `checks_missing`, `queue_depth`, `tcp_latency`, `es_bulk_failure`.

Quan trọng nhất là **`checks_missing`** (`LLEN monitor:ping:queue:{round_id}` đo lúc round
kết thúc) — đây là tín hiệu **duy nhất** báo thiếu worker. Không có nó thì hệ thống ping
thiếu server mà không ai biết.

`FactBuffer` đã đếm sẵn `Dropped()` và `Failed()` (nguồn của `es_bulk_failure`) nhưng chưa
expose ra ngoài.

### 4. ILM retention chưa cấu hình
Design §12.4: ES giữ raw fact **7 ngày** rồi xóa index. Hiện chưa có ILM policy →
14,4 triệu doc/ngày (~2–3 GB/ngày) sẽ tích tụ vô hạn.

Index template đã có (`server-status-logs-*`), chỉ cần gắn thêm ILM policy.

### 5. Export tốn 100 query cho 10.000 server
`FindAll` cap `page_size` ở 100 để bảo vệ list API công khai, và export dùng chung nó
(design §7.9 cấm viết SQL riêng cho export). Mỗi page lặp lại một `COUNT(*)`.

Nếu export chậm thật, hướng đúng là chuyển cap xuống tầng validate DTO — nhưng đó là
thay đổi hành vi của list API.

### 6. 12 file markdown trong `docs/` đã lỗi thời
Vẫn mô tả Kafka / FileIO / shared DB: `01-architecture-overview.md`,
`03-event-driven-kafka.md`, `08-flow-import-export.md`, `report.md`, `architecture.md`, …

`design.md` + `refactor.md` đã thay thế chúng. `docs/api-spec.yaml` **đã** được cập nhật ở R7.5.

### 7. Chưa test multi-instance
Design §8.1 nói lock chỉ dành cho scheduler, **mọi instance đều ping**. Mới chạy 1 instance
monitor — chưa chứng minh 2+ instance chia việc đúng qua `BRPOP`.

---

## Ghi chú vận hành

### Sau khi seed hoặc khôi phục dữ liệu
Luôn chạy `make rebuild-cache`. Monitoring **bỏ qua mọi round** nếu thiếu marker
`server:monitor-target:ready`, và marker đó chỉ do rebuild đặt (design §7.5).

### Contract giữa Monitoring và Server Service
- `changed_at` trên stream và `last_checked_at` trong `monitor:status:{id}` đều phải là
  **RFC3339**. Lệch format → consumer coi mọi event là malformed, **ACK rồi vứt đi im lặng**,
  chỉ thấy log Error.
- `status` trên stream chỉ nhận `ON`/`OFF`. `UNKNOWN` là giá trị hợp lệ của cột nhưng không
  bao giờ là transition hợp lệ.

### Redis
`maxmemory-policy allkeys-lru` có thể evict `stream:monitor.status`. Consumer giờ tự phục
hồi, nhưng nếu chuyện này xảy ra thường xuyên thì cần tăng maxmemory hoặc đổi policy —
event bị evict là event mất thật.
