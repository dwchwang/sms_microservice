# Tài liệu Refactor — Chuyển đổi hệ thống hiện tại sang thiết kế mới

**Ngày:** 16/07/2026  
**Mục tiêu:** Đối chiếu hệ thống hiện tại với thiết kế mới (`design.md`) và liệt kê chi tiết mọi thay đổi cần thực hiện.

---

## Mục lục

1. [Tổng quan thay đổi kiến trúc](#1-tổng-quan-thay-đổi-kiến-trúc)
2. [Loại bỏ Kafka — Thay bằng Redis Stream](#2-loại-bỏ-kafka--thay-bằng-redis-stream)
3. [Loại bỏ FileIO Service — Gộp vào Server Service](#3-loại-bỏ-fileio-service--gộp-vào-server-service)
4. [Thay đổi API Gateway — Từ custom Go sang Traefik](#4-thay-đổi-api-gateway--từ-custom-go-sang-traefik)
5. [Thay đổi Database — Từ shared DB sang database-per-service](#5-thay-đổi-database--từ-shared-db-sang-database-per-service)
6. [Refactor Server Service](#6-refactor-server-service)
7. [Refactor Monitor Service (Monitoring Service)](#7-refactor-monitor-service-monitoring-service)
8. [Refactor Report Service (Reporting Service)](#8-refactor-report-service-reporting-service)
9. [Refactor Auth Service (Identity Service)](#9-refactor-auth-service-identity-service)
10. [Thay đổi Docker Compose](#10-thay-đổi-docker-compose)
11. [Thay đổi Shared module](#11-thay-đổi-shared-module)
12. [Thay đổi Response/Error contract](#12-thay-đổi-responseerror-contract)
13. [Thay đổi Logging](#13-thay-đổi-logging)
14. [Thay đổi Security & Scope](#14-thay-đổi-security--scope)
15. [Thay đổi Migration & Init SQL](#15-thay-đổi-migration--init-sql)
16. [Tóm tắt file cần tạo mới / sửa / xóa](#16-tóm-tắt-file-cần-tạo-mới--sửa--xóa)

---

## 1. Tổng quan thay đổi kiến trúc

### 1.1 So sánh kiến trúc hiện tại vs thiết kế mới

| Khía cạnh | Hiện tại | Thiết kế mới |
|---|---|---|
| **Số lượng service** | 5 service (auth, server, monitor, report, **fileio**) + api-gateway | 4 service (identity, server, monitoring, reporting) + Traefik |
| **Message broker** | **Kafka** (segmentio/kafka-go) | **Redis Stream** (consumer group) |
| **API Gateway** | **Custom Go service** (api-gateway/) | **Traefik** + ForwardAuth |
| **Database** | 1 PostgreSQL instance, 5 schema (shared DB) | 1 PostgreSQL instance, **3 DB riêng** (Identity DB, Server DB, Report DB) |
| **Import/Export** | **FileIO Service riêng** | **Gộp vào Server Service** |
| **Status model** | `on`/`off` (chữ thường) | `ON`/`OFF`/`UNKNOWN` (chữ hoa) |
| **Status update** | Monitor **trực tiếp ghi PostgreSQL** (cross-schema) | Monitor **publish Redis Stream** → Server Service **consume và ghi DB** |
| **Target projection** | Monitor **đọc trực tiếp** bảng `server_schema.servers` | Server Service **duy trì Redis target projection** → Monitor đọc từ Redis |
| **Health check config** | Bảng `monitor_schema.health_check_configs` | **Không có** — `tcp_port` nằm trực tiếp trong bảng `servers` |
| **Cache strategy** | Cache key cứng, invalidate bằng SCAN pattern | Cache-aside với `list_version`, invalidate bằng version bump |
| **`last_status_check`** | Không có trong model | Đọc từ Redis `monitor:status:{id}`, **không lưu trong PostgreSQL** |
| **Report data source** | Query ES trực tiếp lúc tạo report | Job snapshot 00:30 → `daily_snapshots` → report đọc từ snapshot |
| **Report population** | Đếm server hiện tại từ bảng `servers` | Lấy population qua **internal API** phân trang từ Server Service |
| **Email** | MailHog / SMTP config chung | **Gmail SMTP** + App Password |
| **Log rotation** | Docker log driver | **lumberjack** ghi JSON ra file + stdout |

### 1.2 Lý do chính cần refactor

1. **Không đúng microservice**: Monitor và FileIO đều **đọc/ghi trực tiếp** bảng `server_schema.servers` — vi phạm nguyên tắc data ownership.
2. **Kafka quá nặng cho use case**: Chỉ có 1 cặp producer/consumer, Redis Stream đã là dependency sẵn có, đủ dùng.
3. **FileIO không cần tách riêng**: Import/export là adapter của Server Service, tách ra tăng complexity không cần thiết.
4. **Custom API Gateway**: Phải tự maintain routing/auth/rate-limit, Traefik làm sẵn và tốt hơn.

---

## 2. Loại bỏ Kafka — Thay bằng Redis Stream

### 2.1 Hiện tại

- **`shared/kafka/`**: Chứa `event.go`, `producer.go`, `consumer.go`, `segmentio_producer.go`, `segmentio_consumer.go` — dùng thư viện `segmentio/kafka-go`.
- **7 Kafka topics** được tạo trong `docker-compose.yml` qua `kafka-init` container:
  - `server.created`, `server.updated`, `server.deleted`
  - `server.status.changed`, `server.health.batch`
  - `import.job.created`, `report.daily.trigger`
- **Server Service** publish event `server.created`/`server.updated`/`server.deleted` qua Kafka.
- **Monitor Service** consume `server.created`/`server.deleted` từ Kafka để tự tạo/disable `health_check_configs`.
- **Monitor Service** publish `server.status.changed` và `server.health.batch` qua Kafka.
- **FileIO Service** publish `server.created` và `import.job.created` qua Kafka.

### 2.2 Thiết kế mới

Chỉ còn **1 Redis Stream** duy nhất:

```
stream:monitor.status
  Producer: Monitoring Service
  Consumer: Server Service (consumer group "server-svc")
  Event type: status.changed
```

Không cần các event `server.created`/`server.updated`/`server.deleted` nữa vì:
- Server Service tự quản lý Redis target projection (không cần gửi event cho Monitor).
- Monitor không cần `health_check_configs` — đọc `tcp_port` từ target projection.

### 2.3 Các file cần thay đổi

| Hành động | File/Thư mục |
|---|---|
| **XÓA toàn bộ** | `shared/kafka/` (tất cả file) |
| **XÓA** | `docker-compose.yml`: service `kafka`, `kafka-init` |
| **XÓA dependency** | `server-service/go.mod`: bỏ `segmentio/kafka-go` |
| **XÓA dependency** | `monitor-service/go.mod`: bỏ `segmentio/kafka-go` |
| **XÓA dependency** | `fileio-service/go.mod`: bỏ `segmentio/kafka-go` (service này sẽ bị xóa luôn) |
| **XÓA dependency** | `report-service/go.mod`: bỏ `segmentio/kafka-go` |
| **SỬA** | `server-service/internal/service/server_service.go`: xóa `kafka.Producer`, xóa `publishEvent()` |
| **SỬA** | `monitor-service/internal/scheduler/health_check_scheduler.go`: xóa Kafka publish, thay bằng `XADD` trong Lua script |
| **SỬA** | `monitor-service/internal/service/event_consumer.go`: **XÓA file này** — Monitor không consume event từ Server nữa |
| **THÊM** | `server-service/internal/consumer/status_consumer.go`: **Consumer Redis Stream** `stream:monitor.status` |
| **THÊM** | `shared/stream/` hoặc tích hợp trực tiếp: Redis Stream helper (XREADGROUP, XACK, XAUTOCLAIM) |

### 2.4 Chi tiết code cần viết

#### Server Service — Consumer Redis Stream

```go
// server-service/internal/consumer/status_consumer.go
// Consumer loop:
//   XREADGROUP GROUP server-svc {hostname} COUNT 100 BLOCK 2000
//     STREAMS stream:monitor.status >
//   → parse event
//   → UPDATE servers SET status=:status, status_changed_at=:changed_at,
//       status_version=:version, last_status_event_id=:stream_id
//     WHERE server_id=:server_id AND deleted_at IS NULL
//       AND status_version < :version
//   → XACK stream:monitor.status server-svc {message_id}
//   → Nếu row affected > 0: bump server:list:version

// Goroutine reclaim (mỗi 30s):
//   XAUTOCLAIM stream:monitor.status server-svc {hostname} 60000 0 COUNT 100
```

#### Monitoring Service — Lua Script publish event

```lua
-- Thay vì publish Kafka, dùng Lua script trong Redis:
local key = "monitor:status:" .. server_id
local old_status = redis.call("HGET", key, "status")
local old_round = tonumber(redis.call("HGET", key, "round_id") or "0")

if incoming_round <= old_round then
  return 0  -- out-of-order, bỏ qua
end

redis.call("HSET", key,
  "status", new_status,
  "last_checked_at", checked_at,
  "latency_ms", latency,
  "round_id", incoming_round)

if old_status ~= nil and old_status ~= new_status then
  -- Hoặc lần đầu check (old_status == nil)
  redis.call("XADD", "stream:monitor.status", "MAXLEN", "~", "100000", "*",
    "event_type", "status.changed",
    "server_id", server_id,
    "status", new_status,
    "changed_at", checked_at,
    "checked_at", checked_at,
    "status_version", tostring(incoming_round))
end
-- Lần đầu (UNKNOWN → ON/OFF) cũng phát event
if old_status == nil then
  redis.call("XADD", "stream:monitor.status", "MAXLEN", "~", "100000", "*",
    "event_type", "status.changed",
    "server_id", server_id,
    "status", new_status,
    "changed_at", checked_at,
    "checked_at", checked_at,
    "status_version", tostring(incoming_round))
end
return 1
```

---

## 3. Loại bỏ FileIO Service — Gộp vào Server Service

### 3.1 Hiện tại

- **`fileio-service/`** là service riêng biệt với:
  - Import: async qua Kafka (`InitiateImport` → publish `import.job.created` → `ProcessImportJob`)
  - Export: query `server_schema.servers` trực tiếp
  - Có schema riêng `fileio_schema` chứa `import_jobs`, `import_job_details`
  - Có DB user `fileio_user` với quyền SELECT, INSERT trên `server_schema`
  - **Vi phạm data ownership**: ghi trực tiếp vào bảng `server_schema.servers`

### 3.2 Thiết kế mới

- Import/export là **adapter** nằm trong Server Service
- Import là **đồng bộ** (synchronous) trong 1 HTTP request, không qua message queue
- Bảng `import_jobs`/`import_job_details` → **KHÔNG CÒN** (thiết kế mới dùng idempotency key + response trực tiếp)
- Bảng `api_idempotency` nằm trong Server DB

### 3.3 Các file cần thay đổi

| Hành động | File/Thư mục |
|---|---|
| **XÓA toàn bộ** | `fileio-service/` (xóa service) |
| **XÓA** | `docker-compose.yml`: service `fileio-service` |
| **XÓA** | `migrations/fileio/` (nếu có) |
| **XÓA schema** | `fileio_schema` trong `init.sql` |
| **XÓA user** | `fileio_user` trong `init.sql` |
| **DI CHUYỂN + SỬA** | `fileio-service/internal/excel/` → `server-service/internal/excel/` |
| **DI CHUYỂN + SỬA** | Logic import → `server-service/internal/service/import_service.go` |
| **DI CHUYỂN + SỬA** | Logic export → `server-service/internal/service/export_service.go` |
| **THÊM handler** | `server-service/internal/handler/import_handler.go` |
| **THÊM handler** | `server-service/internal/handler/export_handler.go` |
| **THÊM route** | `POST /api/v1/servers/import`, `POST /api/v1/servers/export` |

### 3.4 Chi tiết thay đổi logic Import

**Hiện tại** (async, qua Kafka):
```
User upload file → FileIO validate → save file → create import_job → publish Kafka
→ Consumer nhận → parse Excel → insert từng dòng → update job status
```

**Thiết kế mới** (sync, trong 1 request):
```
User upload file → Server Service validate file
→ Parse xlsx streaming → Validate từng dòng → tách valid/failed
→ Dedupe trong file → Batch check trùng tên trong DB
→ Batch insert 500 dòng/batch (ON CONFLICT server_id DO NOTHING)
→ Nếu batch ném 23505 trên ux_servers_active_name → fallback per-row
→ Update Redis target projection (pipeline)
→ Bump server:list:version
→ Trả response {succeeded, failed, skipped_duplicate}
```

**Thay đổi chi tiết:**

1. **Bỏ `import_jobs`, `import_job_details`** — response trả trực tiếp trong HTTP response
2. **Thêm `api_idempotency`** — POST import yêu cầu `Idempotency-Key` header
3. **Batch insert 500 dòng** thay vì insert từng dòng
4. **ON CONFLICT (server_id) DO NOTHING** cho trùng ID
5. **Fallback per-row** khi batch ném unique violation trên `server_name`
6. **Phân loại 3 nhóm**: `succeeded`, `failed`, `skipped_duplicate` (hiện tại chỉ có `success`/`failed`)
7. **Thêm `tcp_port`** vào template import (hiện tại không có)
8. **Thêm CIDR allowlist validation** cho IPv4

### 3.5 Chi tiết thay đổi logic Export

**Hiện tại:**
- Export dùng `GET` method
- FileIO query trực tiếp `server_schema.servers`
- Không có `last_status_check` trong export

**Thiết kế mới:**
- Export dùng **`POST`** method (filter phức tạp)
- Server Service tự query (data ownership đúng)
- Dùng chung `ServerQuerySpec` với list API
- **Thêm `last_status_check`** — đọc từ Redis pipeline 10.000 HGET
- **Escape formula injection**: cell bắt đầu `=`, `+`, `-`, `@` phải prefix `'`

---

## 4. Thay đổi API Gateway — Từ custom Go sang Traefik

### 4.1 Hiện tại

- **`api-gateway/`** là Go service custom:
  - Routing tới các service backend
  - Auth middleware gọi auth-service để verify token
  - Rate limiting (có thể)
  - CORS handling

### 4.2 Thiết kế mới

- Dùng **Traefik** container (image chính thức)
- **ForwardAuth** middleware gọi Identity Service `/internal/verify`
- **Rate limiting** tại Traefik
- Routing bằng **labels** hoặc **file provider**

### 4.3 Các file cần thay đổi

| Hành động | File/Thư mục |
|---|---|
| **XÓA toàn bộ** | `api-gateway/` (xóa service) |
| **THÊM** | `deployments/traefik/traefik.yml` (static config) |
| **THÊM** | `deployments/traefik/dynamic.yml` (dynamic config: routers, middlewares, services) |
| **SỬA** | `docker-compose.yml`: thay service `api-gateway` bằng `traefik` |

### 4.4 Config Traefik cần tạo

```yaml
# traefik.yml (static)
entryPoints:
  web:
    address: ":8080"
providers:
  file:
    filename: /etc/traefik/dynamic.yml
api:
  dashboard: false

# dynamic.yml
http:
  middlewares:
    forwardAuth:
      forwardAuth:
        address: "http://identity-service:8081/internal/verify"
        authResponseHeaders:
          - "X-User-Id"
          - "X-User-Scopes"
    rate-limit-global:
      rateLimit:
        average: 100
        burst: 200
    rate-limit-auth:
      rateLimit:
        average: 10
        burst: 20

  routers:
    server-api:
      rule: "PathPrefix(`/api/v1/servers`)"
      service: server-service
      middlewares: [forwardAuth, rate-limit-global]
    report-api:
      rule: "PathPrefix(`/api/v1/reports`)"
      service: reporting-service
      middlewares: [forwardAuth, rate-limit-global]
    auth-api:
      rule: "PathPrefix(`/api/v1/auth`)"
      service: identity-service
      middlewares: [rate-limit-auth]

  services:
    server-service:
      loadBalancer:
        servers:
          - url: "http://server-service:8082"
    reporting-service:
      loadBalancer:
        servers:
          - url: "http://reporting-service:8084"
    identity-service:
      loadBalancer:
        servers:
          - url: "http://identity-service:8081"
```

### 4.5 Internal routes

Thiết kế mới yêu cầu:
- `GET /internal/servers` (Server Service) — chỉ Reporting gọi, không publish qua Traefik
- `GET /internal/verify` (Identity Service) — chỉ ForwardAuth gọi

Các route `/internal/` **không** nằm trong Traefik router → chỉ truy cập được qua Docker network nội bộ.

---

## 5. Thay đổi Database — Từ shared DB sang database-per-service

### 5.1 Hiện tại

- **1 database `vcs_sms`** với 5 schema:
  - `auth_schema` (auth_user)
  - `server_schema` (server_user)
  - `monitor_schema` (monitor_user)
  - `report_schema` (report_user)
  - `fileio_schema` (fileio_user)
- Monitor và FileIO **đọc/ghi trực tiếp** `server_schema` → vi phạm data ownership
- Monitor có bảng `health_check_configs` riêng

### 5.2 Thiết kế mới

- **3 database riêng** trên cùng 1 PostgreSQL instance:
  - `identity_db` (identity_user) — bảng: `users`, `roles`, `permissions`, `role_permissions`
  - `server_db` (server_user) — bảng: `servers`, `api_idempotency`
  - `report_db` (report_user) — bảng: `report_jobs`, `daily_snapshots`
- **Monitor Service** không có PostgreSQL database riêng — dùng Redis + ES
- **Không còn `fileio_schema`**
- **Không còn `monitor_schema`**
- **Không còn cross-schema access**

### 5.3 Các file cần thay đổi

| Hành động | File |
|---|---|
| **VIẾT LẠI** | `deployments/docker/postgres/init.sql` |
| **XÓA** | Mọi migration liên quan đến `monitor_schema`, `fileio_schema` |
| **THÊM** | Migration cho `api_idempotency` trong server_db |
| **SỬA** | `docker-compose.yml`: PostgreSQL init tạo 3 database |
| **SỬA** | `.env`: connection string riêng cho mỗi service |

### 5.4 Init SQL mới

```sql
-- Tạo 3 database
CREATE DATABASE identity_db;
CREATE DATABASE server_db;
CREATE DATABASE report_db;

-- Tạo users
CREATE USER identity_user WITH PASSWORD '...';
CREATE USER server_user WITH PASSWORD '...';
CREATE USER report_user WITH PASSWORD '...';

-- Grant
GRANT ALL ON DATABASE identity_db TO identity_user;
GRANT ALL ON DATABASE server_db TO server_user;
GRANT ALL ON DATABASE report_db TO report_user;
```

### 5.5 Thay đổi bảng `servers`

| Thay đổi | Hiện tại | Mới |
|---|---|---|
| Table name | `server_schema.servers` | `servers` (trong `server_db`) |
| Status values | `'on'`, `'off'` | `'ON'`, `'OFF'`, `'UNKNOWN'` |
| Status default | `'off'` | `'UNKNOWN'` |
| **THÊM cột** `tcp_port` | Không có (nằm trong `health_check_configs`) | `INT NOT NULL CHECK (tcp_port BETWEEN 1 AND 65535)` |
| **THÊM cột** `status_changed_at` | Không có | `TIMESTAMPTZ NULL` |
| **THÊM cột** `status_version` | Không có | `BIGINT DEFAULT 0` |
| **THÊM cột** `last_status_event_id` | Không có | `VARCHAR` |
| Unique index `server_id` | Partial (WHERE deleted_at IS NULL) | **Toàn cục** (kể cả deleted) |
| Unique index `server_name` | Partial (WHERE deleted_at IS NULL) | Partial (WHERE deleted_at IS NULL) — giữ nguyên |
| IPv4 type | `VARCHAR(15)` | `INET` (PostgreSQL native) |

### 5.6 THÊM bảng `api_idempotency` (trong server_db)

```sql
CREATE TABLE api_idempotency (
    actor_id         VARCHAR NOT NULL,
    endpoint         VARCHAR NOT NULL,
    idempotency_key  VARCHAR NOT NULL,
    request_hash     VARCHAR NOT NULL,
    state            VARCHAR NOT NULL DEFAULT 'processing',
    status_code      INT,
    response_body    JSONB,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (actor_id, endpoint, idempotency_key)
);
```

### 5.7 Thay đổi bảng `daily_snapshots` (trong report_db)

| Thay đổi | Hiện tại | Mới |
|---|---|---|
| Primary key | UUID `id` + unique `snapshot_date` | Composite PK `(server_id, date)` |
| Cấu trúc | 1 row/ngày (aggregate toàn hệ thống) | **1 row per server per ngày** (10.000 row/ngày) |
| **THÊM cột** `server_id` | Không có | `VARCHAR NOT NULL` |
| **THÊM cột** `server_name` | Không có (denormalize) | `VARCHAR NOT NULL` |
| **THÊM cột** `on_checks` | Không có | `INT NOT NULL` |
| **THÊM cột** `actual_checks` | Không có | `INT NOT NULL` |
| **THÊM cột** `expected_checks` | Không có | `INT NOT NULL` |
| **THÊM cột** `uptime_pct` | Có (aggregate) | `NUMERIC(5,2) NULL` (NULL = no data) |
| **THÊM cột** `last_status` | Không có | `VARCHAR NULL` (ON/OFF cuối ngày) |
| **BỎ cột** `id` | UUID PK | Không cần |
| **BỎ cột** `total_servers` | Có | Tính từ count rows |
| **BỎ cột** `servers_on` | Có | Tính từ aggregate |
| **BỎ cột** `low_uptime_servers` | JSONB | Query trực tiếp |

### 5.8 Thay đổi bảng `report_jobs` (trong report_db)

| Thay đổi | Hiện tại | Mới |
|---|---|---|
| Status values | `pending/processing/completed/failed` | `processing/generated/sending/sent/failed/delivery_unknown` |
| **THÊM cột** `idempotency_key` | Không có | `VARCHAR` |
| **THÊM cột** `requester_id` | Không có | `VARCHAR` |
| **THÊM cột** `response_json` | Không có | `JSONB` |
| **THÊM cột** `smtp_message_id` | Không có | `VARCHAR` |
| **THÊM state** `delivery_unknown` | Không có | Xử lý SMTP mơ hồ |
| **THÊM state** `generated` | Không có | Report đã tạo, chưa gửi |
| **THÊM state** `sending` | Không có | Đang gửi SMTP |

### 5.9 XÓA bảng

| Bảng | Lý do |
|---|---|
| `monitor_schema.health_check_configs` | Không cần — `tcp_port` nằm trong `servers`, uptime_rate bỏ |
| `fileio_schema.import_jobs` | FileIO service bị xóa, import đồng bộ |
| `fileio_schema.import_job_details` | FileIO service bị xóa |

---

## 6. Refactor Server Service

Đây là service thay đổi **nhiều nhất** vì nó nhận thêm trách nhiệm từ FileIO, thêm consumer Redis Stream, thêm Redis target projection, và thêm internal API.

### 6.1 Thay đổi Model (`server-service/internal/model/server.go`)

```go
// HIỆN TẠI
type Server struct {
    ID          uuid.UUID      `gorm:"type:uuid;primaryKey"`
    ServerID    string         `gorm:"type:varchar(100);uniqueIndex;not null"`
    ServerName  string         `gorm:"type:varchar(255);uniqueIndex;not null"`
    Status      string         `gorm:"type:varchar(20);not null;default:'off'"`
    IPv4        string         `gorm:"type:varchar(15);not null"`
    // ... metadata fields
    DeletedAt   gorm.DeletedAt `gorm:"index"`
}
func (Server) TableName() string { return "server_schema.servers" }

// MỚI — cần thay đổi:
type Server struct {
    ID                uuid.UUID      `gorm:"type:uuid;primaryKey"`
    ServerID          string         `gorm:"type:varchar(100);not null"`  // unique toàn cục (kể cả deleted)
    ServerName        string         `gorm:"type:varchar(255);not null"`
    Status            string         `gorm:"type:varchar(20);not null;default:'UNKNOWN'"`  // ON/OFF/UNKNOWN
    StatusChangedAt   *time.Time     `gorm:"type:timestamptz"`  // THÊM MỚI
    StatusVersion     int64          `gorm:"type:bigint;default:0"`  // THÊM MỚI
    LastStatusEventID string         `gorm:"type:varchar(255)"`  // THÊM MỚI
    IPv4              string         `gorm:"type:inet;not null"`  // ĐỔI sang INET
    TCPPort           int            `gorm:"type:int;not null"`  // THÊM MỚI
    // ... metadata fields giữ nguyên
    DeletedAt         gorm.DeletedAt `gorm:"index"`
}
func (Server) TableName() string { return "servers" }  // ĐỔI: bỏ schema prefix
```

### 6.2 Thêm Redis Target Projection

**File mới**: `server-service/internal/projection/target_projection.go`

```go
type TargetProjection interface {
    SyncCreate(ctx context.Context, serverID, ipv4 string, tcpPort int) error
    SyncUpdate(ctx context.Context, serverID, ipv4 string, tcpPort int) error
    SyncDelete(ctx context.Context, serverID string) error
    Rebuild(ctx context.Context) error
}
```

**Logic tại mỗi CRUD operation:**

| Operation | Redis commands |
|---|---|
| Create | `HSET server:monitor-target:{id} ipv4 {ipv4} tcp_port {port}` → `SADD server:monitor-target:ids {id}` |
| Update | Same as Create (overwrite hash) |
| Delete | `SREM server:monitor-target:ids {id}` → `DEL server:monitor-target:{id}` |

**Thứ tự ghi quan trọng:**
- Create/Update: ghi hash TRƯỚC rồi mới add ID vào set
- Delete: remove ID khỏi set TRƯỚC rồi mới xóa hash

### 6.3 Thêm Cache-aside với `list_version`

**Hiện tại** (`server_service.go`):
```go
// Cache key cứng: "server:detail:{id}" hoặc "servers:list:{hash}"
// Invalidate: SCAN "servers:list:*" rồi DEL từng key
```

**Mới:**
```go
// Cache key có version: "server:list:cache:{hash}:{version}"
// Cache key detail: "server:detail:cache:{id}:{version}"
// Invalidate: INCR "server:list:version" → key cũ tự hết hạn
// Stats cache: "server:stats:cache" TTL 10s
```

**File cần sửa:** `server-service/internal/service/server_service.go`

Thay đổi `invalidateCache()`:
```go
// HIỆN TẠI: Scan + delete pattern
func (s *serverServiceImpl) invalidateCache(ctx context.Context, serverID string) {
    s.cache.Del(ctx, fmt.Sprintf("server:detail:%s", serverID))
    keys, _ := s.cache.ScanKeys(ctx, "servers:list:*", 100)
    for _, key := range keys { s.cache.Del(ctx, key) }
}

// MỚI: Chỉ bump version
func (s *serverServiceImpl) bumpListVersion(ctx context.Context) {
    s.rdb.Incr(ctx, "server:list:version")
}
```

Thay đổi `GetServer()` và `ListServers()`:
```go
// Luồng đọc mới:
// 1. Đọc server:list:version
// 2. Build cache key = server:list:cache:{hash}:{version}
// 3. Cache hit → dùng
// 4. Cache miss → query PostgreSQL → SET cache TTL 30s
// 5. DÙ hit hay miss → pipeline HGET monitor:status:{id} last_checked_at
//    cho các server_id trong trang → ghép vào response dưới tên last_status_check
```

### 6.4 Thêm `last_status_check` vào Response

**File sửa:** `server-service/internal/dto/` (thêm field)

```go
type ServerResponse struct {
    // ... existing fields
    LastStatusCheck *time.Time `json:"last_status_check"` // THÊM MỚI — đọc từ Redis, không lưu DB
}
```

**Logic ghép:**
```go
// Sau khi lấy row từ DB/cache:
func (s *serverServiceImpl) enrichWithLastCheck(ctx context.Context, servers []ServerResponse) {
    pipe := s.rdb.Pipeline()
    cmds := make([]*redis.StringCmd, len(servers))
    for i, srv := range servers {
        cmds[i] = pipe.HGet(ctx, "monitor:status:"+srv.ServerID, "last_checked_at")
    }
    pipe.Exec(ctx) // lỗi pipeline thì last_status_check = null
    for i, cmd := range cmds {
        if val, err := cmd.Result(); err == nil {
            t, _ := time.Parse(time.RFC3339, val)
            servers[i].LastStatusCheck = &t
        }
    }
}
```

### 6.5 Thay đổi CRUD operations

#### CreateServer
```
HIỆN TẠI:
1. Check exists → Insert DB → Invalidate cache → Publish Kafka

MỚI:
1. Validate input (bao gồm CIDR allowlist cho ipv4)
2. Insert DB (status = UNKNOWN, tcp_port từ request)
3. Update Redis target projection
4. Bump server:list:version
5. Return response
// KHÔNG publish Kafka nữa
```

#### UpdateServer
```
HIỆN TẠI:
1. Find → Apply updates → Save DB → Invalidate cache → Publish Kafka

MỚI:
1. Find existing
2. Không cho phép cập nhật server_id, status, status_changed_at
3. Apply updates (cho phép đổi server_name, ipv4, tcp_port, metadata)
4. Save DB
5. Đồng bộ lại Redis target projection (nếu ipv4 hoặc tcp_port đổi)
6. Bump server:list:version
// KHÔNG publish Kafka nữa
```

#### DeleteServer
```
HIỆN TẠI:
1. Find → Soft delete → Invalidate cache → Publish Kafka

MỚI:
1. Find → Soft delete (set deleted_at)
2. Remove khỏi Redis target projection
3. Bump server:list:version
// KHÔNG publish Kafka nữa
```

### 6.6 Thêm Internal API cho Reporting

**File mới:** `server-service/internal/handler/internal_handler.go`

```go
// GET /internal/servers?created_before=&deleted_after=&cursor=&limit=1000
// Chỉ Reporting gọi, mỗi ngày 1 lần lúc 00:30
// Trả population server theo lifecycle, phân trang bằng cursor trên server_id

func (h *InternalHandler) ListServerPopulation(c *gin.Context) {
    // Parse params: created_before, deleted_after, cursor, limit
    // Query:
    // SELECT server_id, server_name, created_at, deleted_at
    // FROM servers
    // WHERE created_at < :created_before
    //   AND (deleted_at IS NULL OR deleted_at > :deleted_after)
    //   AND server_id > :cursor
    // ORDER BY server_id
    // LIMIT :limit
    // Return: {servers: [...], next_cursor: "..."}
}
```

### 6.7 Thêm Endpoint `GET /api/v1/servers/stats`

```go
// GET /api/v1/servers/stats — scope: server:stats
// SELECT status, COUNT(*) FROM servers WHERE deleted_at IS NULL GROUP BY status
// Cache TTL 10s trong server:stats:cache
```

### 6.8 Thêm Rebuild Target Projection command

```go
// server-service rebuild-monitor-cache
// 1. Đọc tất cả active server từ DB theo page
// 2. Ghi target hash vào Redis bằng pipeline
// 3. Dựng set tạm server:monitor-target:ids:{generation}
// 4. Rename set tạm → server:monitor-target:ids
// 5. Đặt marker server:monitor-target:ready = 1
// 6. Dọn hash mồ côi
```

### 6.9 Thêm CIDR Allowlist validation

**File mới:** `server-service/internal/validator/cidr_validator.go`

```go
// Validate IPv4 khi create/update/import:
// - Từ chối loopback (127.0.0.0/8)
// - Từ chối link-local (169.254.0.0/16)
// - Từ chối multicast (224.0.0.0/4)
// - Từ chối unspecified (0.0.0.0)
// - Từ chối cloud metadata (169.254.169.254)
// - Chỉ cho phép IP thuộc CIDR allowlist config
```

### 6.10 Tổng hợp file thay đổi Server Service

| Hành động | File |
|---|---|
| **SỬA** | `internal/model/server.go` — thêm cột, đổi TableName, đổi status |
| **SỬA** | `internal/service/server_service.go` — bỏ Kafka, thêm projection, đổi cache strategy |
| **SỬA** | `internal/repository/server_repository.go` — đổi TableName, thêm method batch insert |
| **SỬA** | `internal/handler/server_handler.go` — thêm route stats, đổi error handling |
| **SỬA** | `internal/dto/` — thêm field `tcp_port`, `last_status_check`, `ServerQuerySpec` |
| **SỬA** | `config/` — thêm config CIDR allowlist, đổi DB connection string |
| **SỬA** | `cmd/main.go` — khởi tạo consumer goroutine, projection, bỏ Kafka |
| **SỬA** | `go.mod` — bỏ `segmentio/kafka-go`, thêm dependencies mới |
| **THÊM** | `internal/consumer/status_consumer.go` — Redis Stream consumer |
| **THÊM** | `internal/projection/target_projection.go` — Redis target projection |
| **THÊM** | `internal/handler/internal_handler.go` — internal API cho Reporting |
| **THÊM** | `internal/handler/import_handler.go` — import endpoint |
| **THÊM** | `internal/handler/export_handler.go` — export endpoint |
| **THÊM** | `internal/service/import_service.go` — import logic (từ FileIO) |
| **THÊM** | `internal/service/export_service.go` — export logic (từ FileIO) |
| **THÊM** | `internal/excel/` — copy từ FileIO, sửa model references |
| **THÊM** | `internal/validator/cidr_validator.go` — CIDR allowlist |
| **THÊM** | `internal/idempotency/` — Idempotency-Key middleware |

---

## 7. Refactor Monitor Service (Monitoring Service)

### 7.1 Thay đổi kiến trúc Monitoring

| Khía cạnh | Hiện tại | Mới |
|---|---|---|
| **Tên** | `monitor-service` | `monitoring-service` (tùy chọn, có thể giữ) |
| **Nguồn danh sách server** | Đọc trực tiếp PostgreSQL `server_schema.servers` | Đọc từ **Redis target projection** |
| **Health check config** | Từ bảng `monitor_schema.health_check_configs` | **Không có** — `tcp_port` đọc từ `server:monitor-target:{id}` |
| **Cập nhật status vào DB** | **Trực tiếp ghi PostgreSQL** (cross-schema) | **Publish Redis Stream** → Server Service consume |
| **Distributed lock** | Lock đơn giản `SetNX` → chỉ 1 instance chạy | Lock **chỉ cho scheduler nạp queue** — **tất cả instance đều ping** |
| **Worker pool** | Fan-out trong memory | **Redis List** làm queue → worker `BRPOP` |
| **Kafka** | Publish `server.status.changed`, `server.health.batch` | Không dùng Kafka — `XADD` trong Lua script |
| **Consume events** | Consume `server.created`/`server.deleted` từ Kafka | **Không consume** — không cần vì đọc target projection |
| **PostgreSQL dependency** | Có (đọc server list + ghi status + health_check_configs) | **Không có PostgreSQL** |
| **round_id** | UUID random | `floor(redis_unix_seconds / 60)` — deterministic |
| **TTL cho lock/queue** | Không có TTL → key rác | **TTL 120s** cho lock, queue, round current |
| **ES document ID** | Random (auto-generated) | **Deterministic** `server_id:round_id` |

### 7.2 Các file cần thay đổi

| Hành động | File |
|---|---|
| **VIẾT LẠI** | `internal/scheduler/health_check_scheduler.go` — đổi toàn bộ luồng |
| **VIẾT LẠI** | `internal/worker/pool.go` — đổi từ in-memory fan-out sang Redis BRPOP |
| **SỬA** | `internal/repository/es_repository.go` — thêm deterministic `_id`, bounded bulk buffer |
| **SỬA** | `internal/scheduler/redis_client.go` — thêm nhiều Redis operations |
| **XÓA** | `internal/service/event_consumer.go` — không consume Kafka event nữa |
| **XÓA** | `internal/repository/server_reader.go` — không đọc PostgreSQL nữa |
| **XÓA** | `internal/repository/config_repository.go` — không có health_check_configs nữa |
| **XÓA** | `internal/model/` — không cần model cho health_check_configs |
| **XÓA** | `internal/database/` — không kết nối PostgreSQL nữa |
| **SỬA** | `cmd/main.go` — bỏ PostgreSQL, Kafka; thêm Redis operations |
| **SỬA** | `config/` — bỏ DB config, Kafka config |
| **SỬA** | `go.mod` — bỏ GORM, `segmentio/kafka-go` |
| **THÊM** | Lua script cho atomic status update + XADD |
| **SỬA** | `Dockerfile` — bỏ dependency PostgreSQL |

### 7.3 Chi tiết viết lại Scheduler

```go
// HIỆN TẠI (health_check_scheduler.go):
// - Đọc servers từ PostgreSQL: s.serverReader.GetAllActiveServers(ctx)
// - Đọc health_check_configs: s.configRepo.GetAllEnabled(ctx)
// - Merge server + config → ServerInfo list
// - Lock bằng SetNX (chỉ 1 instance chạy toàn bộ)
// - pool.Execute(ctx, serverInfos) — in-memory fan-out
// - Detect status changes: so sánh với Redis cache "server:status:{id}"
// - BatchUpdateStatus trực tiếp vào PostgreSQL
// - Publish Kafka events

// MỚI:
// Scheduler goroutine (chạy trên MỌI instance, mỗi 60s):
// 1. Redis TIME → round_id = floor(redis_unix_seconds / 60)
// 2. SET monitor:round:lock:{round_id} 1 NX EX 120
//    - Thất bại → không làm gì (worker vẫn ping bình thường)
//    - Thành công:
//      a. Kiểm tra server:monitor-target:ready, nếu chưa có → bỏ round
//      b. SSCAN server:monitor-target:ids COUNT 500
//      c. RPUSH monitor:ping:queue:{round_id} (pipeline, batch 500)
//      d. EXPIRE monitor:ping:queue:{round_id} 120
//      e. SET monitor:round:current {round_id} EX 120

// Worker goroutine (200 worker, chạy liên tục):
// Vòng lặp:
//   round = GET monitor:round:current
//   nếu rỗng → sleep 1s → lặp lại
//   server_id = BRPOP monitor:ping:queue:{round} timeout=1
//   - timeout → lặp lại (đọc lại monitor:round:current)
//   - có việc:
//     HGETALL server:monitor-target:{server_id}
//     → nếu hash rỗng → bỏ qua
//     → TCP dial ipv4:tcp_port, timeout 3s
//     → Chạy Lua script cập nhật monitor:status:{server_id}
//     → Đẩy health fact vào bulk buffer ES
```

### 7.4 Chi tiết viết lại Worker Pool

```go
// HIỆN TẠI (pool.go):
// - Dùng Go channel làm job queue
// - Fan-out: servers → channel → workers → results channel
// - Workers nhận job từ Go channel

// MỚI:
// - Workers nhận job từ Redis List bằng BRPOP
// - Mỗi vòng lặp đọc lại monitor:round:current
// - BRPOP timeout 1s (không block vô hạn)
// - Khi round đổi, worker tự chuyển sang queue mới
// - Không cần graceful drain — việc cũ hết TTL tự dọn
```

### 7.5 Chi tiết thay đổi ES Repository

```go
// HIỆN TẠI:
// - Index name cố định (từ config)
// - Document ID tự sinh (auto-generated)
// - Không có batch flush

// MỚI:
// - Daily index: server-status-logs-YYYY.MM.DD
// - Document ID deterministic: server_id + ":" + round_id
// - Bounded bulk buffer:
//   + Batch 1000 doc hoặc flush mỗi 5s
//   + Retry có bound (429/5xx, exponential backoff)
//   + Buffer có giới hạn cứng — ES outage dài thì drop + ghi metric
// - Thêm field server_name (denormalize) vào document
// - Đổi field names theo mapping mới
```

### 7.6 Bỏ PostgreSQL hoàn toàn

```go
// XÓA:
// - server_reader.go (GetAllActiveServers, BatchUpdateStatus)
// - config_repository.go (HealthCheckConfigRepo, health_check_configs)
// - database/ directory
// - GORM dependency trong go.mod
// - PostgreSQL connection trong cmd/main.go
// - docker-compose: monitor-service không depends_on postgres nữa
```

---

## 8. Refactor Report Service (Reporting Service)

### 8.1 Thay đổi kiến trúc Reporting

| Khía cạnh | Hiện tại | Mới |
|---|---|---|
| **Data source cho report** | Query ES trực tiếp lúc tạo report | Đọc từ `daily_snapshots` |
| **daily_snapshots** | 1 row aggregate/ngày | **1 row per server per ngày** (10.000 row/ngày) |
| **Population** | `s.serverCounter.CountActiveServers()` | **Internal API** `GET /internal/servers` từ Server Service |
| **Snapshot job** | Chạy khi gửi daily report | **Job riêng lúc 00:30** — tách khỏi report |
| **Email** | MailHog/SMTP config | **Gmail SMTP** + App Password |
| **State machine** | `pending/processing/completed/failed` | `processing/generated/sending/sent/failed/delivery_unknown` |
| **Report time range** | Cho phép query tương lai | `end_date < hôm nay` — bắt buộc |
| **Max range** | 90 ngày | **31 ngày** |
| **Coverage** | Không có | **coverage_pct** + degraded warning |
| **servers_no_data** | Không có | Server thuộc population nhưng không có fact |
| **Kafka** | Consume `report.daily.trigger` | **Cron job nội bộ** — không Kafka |
| **Redis dependency** | Có (cache summary) | Có thể giữ nhưng đơn giản hơn |
| **PostgreSQL cross-schema** | Đọc `server_schema.servers` (CountActiveServers) | **Không** — gọi internal API |

### 8.2 Các file cần thay đổi

| Hành động | File |
|---|---|
| **VIẾT LẠI** | `internal/service/report_service.go` — đổi data source, logic |
| **SỬA** | `internal/model/` — đổi `daily_snapshots` model, đổi `report_jobs` model |
| **SỬA** | `internal/repository/` — đổi snapshot repo, thêm internal API client |
| **SỬA** | `internal/dto/` — thêm coverage, servers_no_data, servers_on_at_end_at |
| **SỬA** | `internal/email/` — đổi template email theo format mới |
| **SỬA** | `internal/handler/` — thêm `GET /api/v1/reports/{id}` |
| **SỬA** | `internal/scheduler/` — đổi cron schedule |
| **THÊM** | `internal/client/server_client.go` — HTTP client gọi internal API |
| **THÊM** | `internal/snapshot/snapshot_job.go` — job 00:30 aggregate ES |
| **SỬA** | `config/` — đổi DB connection, thêm Gmail SMTP config |
| **SỬA** | `cmd/main.go` — bỏ Kafka, đổi init |
| **SỬA** | `go.mod` — bỏ `segmentio/kafka-go` |

### 8.3 Chi tiết Job Snapshot 00:30

```
Job snapshot (chạy 00:30 Asia/Ho_Chi_Minh cho ngày hôm trước):

1. Gọi GET /internal/servers?created_before=end_at&deleted_after=start_at&cursor=&limit=1000
   → Lặp 10 request (cursor pagination) → thu được population 10.000 server

2. Composite aggregation ES cho ngày hôm trước:
   - Group by server_id (size 1000, after_key pagination)
   - Sub-agg: filter status=ON → on_checks
   - Sub-agg: top_hits sort checked_at desc size 1 → server_name, last_status
   → Lặp 10 vòng → thu được aggregate cho tất cả server có data

3. LEFT JOIN population ⟕ aggregate:
   - Server có fact → on_checks, actual_checks từ ES
   - Server không có fact → actual_checks = 0, uptime_pct = NULL

4. INSERT INTO daily_snapshots ... ON CONFLICT (server_id, date) DO UPDATE
   → 10.000 row/ngày

5. Ghi metric: coverage, duration, servers_no_data count
```

### 8.4 Chi tiết thay đổi Report logic

```go
// HIỆN TẠI:
// GetSummary → query ES trực tiếp → return

// MỚI:
// GetSummary(startDate, endDate):
// 1. Validate: end_date < today, range <= 31 days
// 2. Kiểm tra daily_snapshots có đủ cho mọi ngày trong range
//    → Nếu thiếu → return REPORT_DATA_UNAVAILABLE + danh sách ngày thiếu
// 3. Query daily_snapshots:
//    - total_servers = COUNT(DISTINCT server_id) WHERE date BETWEEN start AND end
//    - avg_uptime_pct = AVG(uptime_pct) WHERE uptime_pct IS NOT NULL
//    - servers_uptime_100 = COUNT WHERE uptime_pct = 100
//    - servers_no_data = COUNT WHERE uptime_pct IS NULL
//    - servers_on_at_end_at = COUNT WHERE date = end_date AND last_status = 'ON'
//    - coverage_pct = SUM(actual_checks) / SUM(expected_checks) * 100
//    - top_10_lowest = ORDER BY uptime_pct ASC LIMIT 10
```

### 8.5 Thay đổi Email template

```
HIỆN TẠI: HTML template với TotalServers, ServersOn, ServersOff, AvgUptimePct, LowUptimeServers

MỚI: Text/HTML template với:
  - Tổng số server trong hệ thống: total_servers
  - Số server On (lúc 23:59:59): servers_on_at_end_at
  - Số server Off (lúc 23:59:59): servers_off_at_end_at
  - Uptime trung bình: avg_uptime_pct
  - Phân bố uptime:
    - Uptime 100%: servers_uptime_100
    - Uptime một phần: servers_uptime_partial
    - Uptime 0%: servers_uptime_0
    - Không có dữ liệu: servers_no_data
  - Top 10 server uptime thấp nhất (kèm server_name)
  - Coverage dữ liệu: coverage_pct
  - Cảnh báo nếu coverage < 95%
```

### 8.6 Thay đổi Email sender → Gmail SMTP

```go
// HIỆN TẠI: SMTP config chung (có thể MailHog)
// MỚI: Gmail SMTP
// SMTP_HOST=smtp.gmail.com
// SMTP_PORT=587
// SMTP_TLS=starttls
// SMTP_PASSWORD=App Password 16 ký tự
// Parse smtp_message_id từ response "250 2.0.0 OK <message-id>"
// Xử lý delivery_unknown khi SMTP mất kết nối
```

### 8.7 Thêm state `delivery_unknown`

```go
// Khi gửi email:
// 1. state = "sending"
// 2. Send email
//    - Success → state = "sent", lưu smtp_message_id
//    - Error rõ ràng (connection refused, auth fail) → state = "failed"
//    - Error mơ hồ (timeout sau DATA, connection reset) → state = "delivery_unknown"
// 3. delivery_unknown: không retry mù, operator kiểm tra
```

---

## 9. Refactor Auth Service (Identity Service)

### 9.1 Thay đổi

| Khía cạnh | Hiện tại | Mới |
|---|---|---|
| **Tên** | `auth-service` | `identity-service` (tùy chọn rename) |
| **Database** | `auth_schema` trong shared DB | `identity_db` riêng |
| **TableName** | `auth_schema.users`, `auth_schema.roles` | `users`, `roles` (bỏ schema prefix) |
| **Password hash** | bcrypt | **Argon2id** |
| **ForwardAuth** | Custom middleware trong api-gateway | `/internal/verify` endpoint cho Traefik |
| **Scope names** | `server:read`, `user:manage` | `server:list`, `server:view`, `server:stats`, `user:list`, `user:manage_role`, `report:view_detail` |
| **Brute-force** | Cơ bản | Redis login lockout: `auth:login-fail:{email}`, `auth:login-lock:{email}` |
| **Refresh token** | (kiểm tra implementation hiện tại) | Opaque token, Lua script atomic rotation, lưu digest trong Redis |

### 9.2 Các file cần thay đổi

| Hành động | File |
|---|---|
| **SỬA** | `internal/model/` — đổi TableName bỏ schema prefix |
| **SỬA** | `internal/service/` — đổi password hash sang Argon2id |
| **THÊM** | `internal/handler/verify_handler.go` — `/internal/verify` cho Traefik ForwardAuth |
| **SỬA** | `config/` — đổi DB connection string sang `identity_db` |
| **SỬA** | `cmd/main.go` — đổi init |

### 9.3 Thay đổi Scope

Cần cập nhật seed data trong init SQL:

```sql
-- HIỆN TẠI:
-- 'server:create', 'server:read', 'server:update', 'server:delete',
-- 'server:import', 'server:export', 'monitor:view', 'report:view',
-- 'report:send', 'user:manage'

-- MỚI:
-- 'server:create', 'server:list', 'server:view', 'server:update',
-- 'server:delete', 'server:import', 'server:export', 'server:stats',
-- 'report:view', 'report:send', 'report:view_detail',
-- 'user:list', 'user:manage_role'
```

Mapping role → scope mới:

| Role | Scopes |
|---|---|
| Viewer | `server:list`, `server:view`, `server:stats`, `report:view` |
| Operator | Viewer + `server:create`, `server:update`, `server:delete`, `server:import`, `server:export`, `report:send`, `report:view_detail` |
| Admin | Toàn bộ + `user:list`, `user:manage_role` |

### 9.4 Thêm `/internal/verify` endpoint

```go
// GET /internal/verify
// Traefik ForwardAuth gọi endpoint này
// 1. Lấy JWT từ Authorization header
// 2. Verify signature + expiry
// 3. Trả response headers:
//    - X-User-Id: user_id
//    - X-User-Scopes: "server:list,server:view,..."
// 4. Status 200 = verified, 401 = denied
```

---

## 10. Thay đổi Docker Compose

### 10.1 Services cần XÓA

```yaml
# XÓA:
kafka:           # Thay bằng Redis Stream
kafka-init:      # Không cần tạo Kafka topics
api-gateway:     # Thay bằng Traefik
fileio-service:  # Gộp vào Server Service
```

### 10.2 Services cần THÊM

```yaml
# THÊM:
traefik:
  image: traefik:v3.0
  ports:
    - "8080:8080"
  volumes:
    - ./deployments/traefik:/etc/traefik
  networks:
    - vcs-network
```

### 10.3 Services cần SỬA

```yaml
# server-service:
#   depends_on: BỎ kafka, kafka-init
#   SỬA environment: DB connection → server_db

# monitor-service:
#   depends_on: BỎ postgres, kafka, kafka-init
#   BỎ PostgreSQL environment variables
#   GIỮ: redis, elasticsearch, tcp-simulator

# report-service:
#   depends_on: BỎ kafka, kafka-init
#   SỬA environment: DB connection → report_db

# auth-service:
#   SỬA environment: DB connection → identity_db

# postgres:
#   SỬA init.sql: tạo 3 database riêng
```

### 10.4 Dependencies mới

```yaml
server-service:
  depends_on:
    postgres: {condition: service_healthy}
    redis: {condition: service_healthy}
  # KHÔNG depends_on kafka nữa

monitor-service:
  depends_on:
    redis: {condition: service_healthy}
    elasticsearch: {condition: service_healthy}
    tcp-simulator: {condition: service_healthy}
  # KHÔNG depends_on postgres, kafka nữa

report-service:
  depends_on:
    postgres: {condition: service_healthy}
    elasticsearch: {condition: service_healthy}
  # KHÔNG depends_on kafka nữa. Thêm phụ thuộc vào server-service cho internal API

auth-service:
  depends_on:
    postgres: {condition: service_healthy}
    redis: {condition: service_healthy}
  # Giữ nguyên
```

---

## 11. Thay đổi Shared module

### 11.1 XÓA

```
shared/kafka/              → XÓA toàn bộ (consumer.go, producer.go, event.go, segmentio_*.go)
shared/kafka/mocks/        → XÓA
```

### 11.2 GIỮ NGUYÊN (có thể sửa nhỏ)

```
shared/errors/             → Cập nhật error codes mới
shared/logger/             → Thêm lumberjack integration
shared/middleware/         → Đổi scope names, đổi cách đọc header từ Traefik ForwardAuth
shared/response/           → Cập nhật response envelope theo design mới
shared/validator/          → Có thể giữ
shared/pkg/                → Có thể giữ
```

### 11.3 THÊM (tùy chọn)

```
shared/stream/             → Redis Stream helper (nếu muốn share giữa services)
```

---

## 12. Thay đổi Response/Error contract

### 12.1 Response envelope

```json
// HIỆN TẠI (shared/response/):
// Có thể đã có format gần đúng

// MỚI — cần đảm bảo format chính xác:
// Success:
{
  "status": "success",
  "code": 200,
  "message": "...",
  "data": {},
  "meta": {
    "request_id": "req-abc123",
    "timestamp": "2026-07-16T10:00:00Z"
  }
}

// Error:
{
  "status": "error",
  "code": "SERVER_VALIDATION_FAILED",
  "message": "...",
  "errors": [
    {"field": "ipv4", "code": "INVALID_FORMAT", "message": "..."}
  ],
  "meta": {
    "request_id": "...",
    "timestamp": "..."
  }
}
```

### 12.2 Error codes MỚI cần thêm

| Code | HTTP | Ý nghĩa |
|---|---:|---|
| `SERVER_IP_NOT_ALLOWED` | 422 | IPv4 ngoài CIDR allowlist |
| `SERVER_IMPORT_FILE_REJECTED` | 422 | Lỗi cấp file import |
| `SERVER_IDEMPOTENCY_CONFLICT` | 409 | Idempotency-Key conflict |
| `REPORT_INVALID_RANGE` | 422 | Khoảng report không hợp lệ |
| `REPORT_RECIPIENT_NOT_ALLOWED` | 422 | Email không thuộc allowlist |
| `REPORT_IDEMPOTENCY_CONFLICT` | 409 | Idempotency-Key conflict report |
| `REPORT_DATA_UNAVAILABLE` | 503 | Thiếu snapshot |
| `AUTH_ACCOUNT_LOCKED` | 423 | Account bị khóa tạm |

---

## 13. Thay đổi Logging

### 13.1 Hiện tại

- Dùng zerolog
- Ghi ra stdout (Docker log driver)
- Có thể có volume mount cho log

### 13.2 Thiết kế mới

```go
// Thêm lumberjack cho mỗi service:
import "gopkg.in/natefinisher/lumberjack.v2"

logWriter := &lumberjack.Logger{
    Filename:   "/var/log/vcs-sms/{service-name}.log",
    MaxSize:    100,   // MB
    MaxBackups: 7,
    MaxAge:     14,    // ngày
    Compress:   true,
}

logger := zerolog.New(
    zerolog.MultiLevelWriter(logWriter, os.Stdout),
).With().Timestamp().Str("service", "{service-name}").Logger()
```

### 13.3 Các file cần thay đổi

| Hành động | File |
|---|---|
| **SỬA** | `shared/logger/` — thêm lumberjack integration |
| **SỬA** | Mỗi service `cmd/main.go` — khởi tạo logger với lumberjack |
| **THÊM** | `go.mod` (mỗi service): thêm dependency `gopkg.in/natefinisher/lumberjack.v2` |
| **SỬA** | `docker-compose.yml`: mount volume `/var/log/vcs-sms` cho mỗi service |

---

## 14. Thay đổi Security & Scope

### 14.1 Thay đổi scope names

| API hiện tại | Scope hiện tại | Scope mới |
|---|---|---|
| `GET /servers` | `server:read` | `server:list` |
| `GET /servers/{id}` | `server:read` | `server:view` |
| **MỚI** `GET /servers/stats` | — | `server:stats` |
| `GET /reports/summary` | `report:view` | `report:view` |
| **MỚI** `GET /reports/{id}` | — | `report:view_detail` |
| `GET /auth/users` | `user:manage` | `user:list` |
| `PUT /auth/users/{id}/role` | `user:manage` | `user:manage_role` |

### 14.2 ForwardAuth flow

```
Client → Traefik
  → ForwardAuth: Traefik gọi Identity /internal/verify
    → Identity xóa header giả mạo, verify JWT, trả X-User-Id + X-User-Scopes
  → Traefik copy headers sang request → forward tới service đích
  → Service đích kiểm tra scope endpoint
```

**Thay đổi trong middleware:**

```go
// HIỆN TẠI (shared/middleware/):
// Middleware đọc JWT trực tiếp từ Authorization header
// Tự verify JWT

// MỚI:
// Middleware đọc X-User-Id và X-User-Scopes từ header (đã qua ForwardAuth)
// Không cần tự verify JWT — Traefik đã gọi Identity verify
// Chỉ cần check scope cho endpoint hiện tại
```

---

## 15. Thay đổi Migration & Init SQL

### 15.1 Init SQL mới

```sql
-- ============================================================
-- VCS-SMS Database Initialization — Thiết kế mới
-- 3 database riêng + DB users
-- ============================================================

-- Tạo databases
CREATE DATABASE identity_db;
CREATE DATABASE server_db;
CREATE DATABASE report_db;

-- Tạo users
CREATE USER identity_user WITH PASSWORD '...';
CREATE USER server_user WITH PASSWORD '...';
CREATE USER report_user WITH PASSWORD '...';

GRANT ALL ON DATABASE identity_db TO identity_user;
GRANT ALL ON DATABASE server_db TO server_user;
GRANT ALL ON DATABASE report_db TO report_user;

-- === identity_db ===
\c identity_db;
CREATE TABLE roles (...);
CREATE TABLE permissions (scope VARCHAR NOT NULL UNIQUE);
CREATE TABLE role_permissions (role_id UUID, scope VARCHAR, ...);
CREATE TABLE users (email VARCHAR UNIQUE, password_hash VARCHAR, role_id UUID, ...);

-- === server_db ===
\c server_db;
CREATE TABLE servers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id VARCHAR(100) NOT NULL,
    server_name VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'UNKNOWN' CHECK (status IN ('ON','OFF','UNKNOWN')),
    status_changed_at TIMESTAMPTZ,
    status_version BIGINT DEFAULT 0,
    last_status_event_id VARCHAR(255),
    ipv4 INET NOT NULL,
    tcp_port INT NOT NULL CHECK (tcp_port BETWEEN 1 AND 65535),
    os VARCHAR(100),
    cpu_cores INT,
    ram_gb INT,
    disk_gb INT,
    location VARCHAR(255),
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX ux_servers_server_id ON servers (server_id);
CREATE UNIQUE INDEX ux_servers_active_name ON servers (server_name) WHERE deleted_at IS NULL;
ALTER TABLE servers ADD CONSTRAINT ck_servers_tcp_port CHECK (tcp_port BETWEEN 1 AND 65535);
CREATE INDEX ix_servers_status ON servers (status) WHERE deleted_at IS NULL;

CREATE TABLE api_idempotency (...);

-- === report_db ===
\c report_db;
CREATE TABLE report_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_type VARCHAR(20) NOT NULL,
    requester_id VARCHAR,
    idempotency_key VARCHAR,
    start_at DATE NOT NULL,
    end_at DATE NOT NULL,
    recipient_email VARCHAR(255) NOT NULL,
    state VARCHAR(30) NOT NULL DEFAULT 'processing',
    response_json JSONB,
    smtp_message_id VARCHAR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ
);

CREATE TABLE daily_snapshots (
    server_id VARCHAR NOT NULL,
    date DATE NOT NULL,
    server_name VARCHAR NOT NULL,
    on_checks INT NOT NULL,
    actual_checks INT NOT NULL,
    expected_checks INT NOT NULL,
    uptime_pct NUMERIC(5,2),
    last_status VARCHAR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (server_id, date)
);
CREATE INDEX ix_daily_snapshots_date_uptime ON daily_snapshots (date, uptime_pct);
```

---

## 16. Tóm tắt file cần tạo mới / sửa / xóa

### 16.1 Thư mục/Service cần XÓA hoàn toàn

| Thư mục | Lý do |
|---|---|
| `fileio-service/` | Gộp vào Server Service |
| `api-gateway/` | Thay bằng Traefik |
| `shared/kafka/` | Thay bằng Redis Stream |

### 16.2 File cần XÓA trong service còn lại

| File | Service | Lý do |
|---|---|---|
| `internal/service/event_consumer.go` | monitor-service | Không consume Kafka event |
| `internal/repository/server_reader.go` | monitor-service | Không đọc PostgreSQL |
| `internal/repository/config_repository.go` | monitor-service | Bỏ health_check_configs |
| `internal/model/health_check_config.go` | monitor-service | Bỏ health_check_configs |
| `internal/database/` | monitor-service | Bỏ PostgreSQL |

### 16.3 File cần THÊM MỚI

| File | Service | Mục đích |
|---|---|---|
| `internal/consumer/status_consumer.go` | server-service | Redis Stream consumer |
| `internal/projection/target_projection.go` | server-service | Redis target projection |
| `internal/handler/internal_handler.go` | server-service | Internal API cho Reporting |
| `internal/handler/import_handler.go` | server-service | Import endpoint |
| `internal/handler/export_handler.go` | server-service | Export endpoint |
| `internal/service/import_service.go` | server-service | Import business logic |
| `internal/service/export_service.go` | server-service | Export business logic |
| `internal/excel/` | server-service | Di chuyển từ FileIO |
| `internal/validator/cidr_validator.go` | server-service | CIDR allowlist validation |
| `internal/idempotency/` | server-service | Idempotency-Key middleware |
| `internal/handler/verify_handler.go` | auth-service | ForwardAuth endpoint |
| `internal/client/server_client.go` | report-service | HTTP client gọi internal API |
| `internal/snapshot/snapshot_job.go` | report-service | Job snapshot 00:30 |
| `deployments/traefik/traefik.yml` | infrastructure | Traefik static config |
| `deployments/traefik/dynamic.yml` | infrastructure | Traefik dynamic config |
| Lua script files | monitor-service | Atomic status update + XADD |

### 16.4 File cần SỬA (major changes)

| File | Service | Thay đổi chính |
|---|---|---|
| `internal/model/server.go` | server-service | Thêm cột, đổi status values, đổi TableName |
| `internal/service/server_service.go` | server-service | Bỏ Kafka, thêm projection, đổi cache, thêm last_status_check |
| `internal/repository/server_repository.go` | server-service | Thêm batch insert, thêm method cho import |
| `internal/handler/server_handler.go` | server-service | Thêm routes stats, đổi error handling |
| `internal/dto/` | server-service | Thêm tcp_port, last_status_check, import/export DTOs |
| `internal/scheduler/health_check_scheduler.go` | monitor-service | **Viết lại hoàn toàn** |
| `internal/worker/pool.go` | monitor-service | **Viết lại** — từ in-memory sang Redis BRPOP |
| `internal/repository/es_repository.go` | monitor-service | Deterministic ID, daily index, bounded buffer |
| `internal/scheduler/redis_client.go` | monitor-service | Thêm nhiều operations |
| `internal/service/report_service.go` | report-service | **Viết lại** — đổi data source, logic, validation |
| `internal/email/` | report-service | Đổi template, Gmail SMTP |
| `internal/model/` | report-service | Đổi daily_snapshots, report_jobs |
| `docker-compose.yml` | root | XÓA kafka, api-gateway, fileio; THÊM traefik |
| `deployments/docker/postgres/init.sql` | root | **Viết lại** — 3 database riêng |
| `.env` / `.env.example` | root | Đổi connection strings, thêm Gmail config, bỏ Kafka config |
| `Makefile` | root | Bỏ lệnh liên quan fileio, kafka |
| Mỗi service `cmd/main.go` | all | Đổi init: bỏ Kafka, đổi DB, thêm lumberjack |
| Mỗi service `go.mod` | all | Bỏ segmentio/kafka-go, thêm lumberjack |
| `shared/middleware/` | shared | Đổi scope names, đổi cách đọc header ForwardAuth |
| `shared/errors/` | shared | Thêm error codes mới |
| `shared/response/` | shared | Đảm bảo format response đúng design |

### 16.5 Thứ tự thực hiện đề xuất

1. **Phase 1 — Infrastructure**: Tạo Traefik config, viết lại init.sql, sửa docker-compose (bỏ Kafka, bỏ api-gateway, bỏ fileio-service, thêm Traefik)
2. **Phase 2 — Shared**: Xóa `shared/kafka/`, sửa `shared/middleware/`, `shared/errors/`, `shared/response/`, thêm lumberjack
3. **Phase 3 — Identity Service**: Đổi DB connection, thêm `/internal/verify`, đổi scope names, đổi sang Argon2id
4. **Phase 4 — Server Service**: Refactor lớn nhất — thêm model fields, bỏ Kafka, thêm Redis projection, thêm consumer, gộp import/export, thêm internal API, thêm CIDR validator, đổi cache strategy
5. **Phase 5 — Monitoring Service**: Viết lại scheduler + worker, bỏ PostgreSQL, bỏ Kafka, thêm Lua script, đổi ES repository
6. **Phase 6 — Reporting Service**: Đổi data source, thêm snapshot job, thêm internal API client, đổi email template/sender, đổi state machine
7. **Phase 7 — Testing & Verification**: Chạy toàn bộ hệ thống, verify luồng end-to-end
