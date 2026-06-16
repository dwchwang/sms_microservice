# VCS Server Management System — Tài liệu thiết kế hệ thống

> **Phiên bản:** 1.0  
> **Ngày:** 2026-06-12  
> **Kiến trúc:** Microservices + API Gateway + Event-Driven (Kafka)

---

## 1. Tổng quan

### 1.1. Bài toán

Hệ thống quản lý tập trung **10.000 server** cho công ty VCS, bao gồm:
- Theo dõi trạng thái On/Off theo thời gian thực (TCP Health Check mỗi 60 giây)
- Quản lý danh sách server (CRUD, filter, sort, pagination)
- Import/Export danh sách server qua file Excel
- Báo cáo uptime định kỳ qua Email + API chủ động
- Phân quyền người dùng (3 roles: Admin, Operator, Viewer)

### 1.2. Yêu cầu phi chức năng

| Yêu cầu | Giải pháp |
|---------|----------|
| Health-check 10K servers | Worker Pool 100 goroutines + TCP Simulator (10K ports động) |
| High availability | Graceful shutdown, fail-closed Kafka publish, Redis degraded mode |
| Bảo mật | JWT HS256 + Scope RBAC + bcrypt hash + Rate Limiting |
| Performance | Redis cache-aside, ES bulk indexing, GORM connection pool |
| Observability | Structured JSON logging (zerolog) + lumberjack rotation |
| Test coverage | ≥ 90% core business packages, sqlmock + function-callback mocks |

---

## 2. Kiến trúc hệ thống

### 2.1. System Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                        CLIENT LAYER                               │
│              Postman / cURL / Frontend / Swagger UI               │
└───────────────────────────┬──────────────────────────────────────┘
                            │ HTTP :8080
                    ┌───────▼────────┐
                    │  API GATEWAY   │  Gin Framework
                    │  Port: 8080    │  • JWT Validation
                    │                │  • Scope RBAC (9 scopes)
                    │                │  • Rate Limiting (Redis)
                    │                │  • Reverse Proxy
                    └───┬───┬───┬───┬┘
                        │   │   │   │
        ┌───────────────┼───┼───┼───┼───────────────┐
        │               │   │   │   │               │
        ▼               ▼   ▼   ▼   ▼               ▼
┌──────────┐  ┌──────────┐ ┌──────────┐ ┌──────────────┐
│  AUTH    │  │  SERVER  │ │ MONITOR  │ │   REPORT     │
│  :8081   │  │  :8082   │ │  :8083   │ │   :8084      │
│          │  │          │ │          │ │              │
│ Register │  │ CRUD     │ │ Health-  │ │ Uptime       │
│ Login    │  │ Filter   │ │ Check    │ │ Summary      │
│ JWT      │  │ Sort     │ │ 60s cron │ │ Email (SMTP) │
│ Refresh  │  │ Paginate │ │ Worker   │ │ Daily Cron   │
│ Logout   │  │ Cache    │ │ Pool     │ │ HTML Template │
│ Profile  │  │ Events   │ │ ES Bulk  │ │ Snapshots    │
└────┬─────┘  └────┬─────┘ └────┬─────┘ └──────┬───────┘
     │             │            │               │
     │             │            │               │
┌────┴─────────────┴────────────┴───────────────┴──────┐
│                 FILE I/O SERVICE                     │
│                    Port: 8085                        │
│   Import Excel (async Kafka) + Export Excel (sync)   │
└────────────────────────┬─────────────────────────────┘
                         │
                         ▼
              ┌──────────────────┐
              │  TCP SIMULATOR   │
              │  Ports: 9001-    │
              │  19000           │
              │  10K Listeners   │
              │  Math Engine     │
              │  (On/Off động)   │
              └──────────────────┘
```

### 2.2. Service Boundaries

| Service | Schema | Tables | Phụ thuộc |
|---------|--------|--------|-----------|
| **API Gateway** | — | — | Redis |
| **Auth Service** | `auth_schema` | `users`, `roles`, `role_permissions` | PostgreSQL, Redis |
| **Server Service** | `server_schema` | `servers` | PostgreSQL, Redis, Kafka |
| **Monitor Service** | `monitor_schema` | `health_check_configs` | PostgreSQL, Redis, Kafka, Elasticsearch, TCP Simulator |
| **Report Service** | `report_schema` | `report_jobs`, `daily_snapshots` | PostgreSQL, Elasticsearch, Kafka, SMTP |
| **File I/O Service** | `fileio_schema` | `import_jobs`, `import_job_details` | PostgreSQL (cross-schema: server_schema), Kafka |
| **TCP Simulator** | — | — | Standalone |

### 2.3. Infrastructure Stack

| Thành phần | Công nghệ | Port | Mục đích |
|------------|----------|------|----------|
| **API Gateway** | Go + Gin | 8080 | Entry point, auth, routing |
| **PostgreSQL 17** | Docker | 5432 | Primary database (1 instance, 5 schemas) |
| **Redis 8** | Docker | 6379 | Cache + Rate Limiting + Token Blacklist |
| **Elasticsearch 8** | Docker | 9200 | Uptime logs & aggregation |
| **Kafka 3.9** | Docker | 9092 | Event-driven async communication |
| **Gmail SMTP** | External | 587 | Email reports |

---

## 3. Database Design

### 3.1. Schema Strategy — Shared Instance, Separate Schemas

```
PostgreSQL 17 (vcs_sms)
├── auth_schema          ← Auth Service (full ownership)
│   ├── roles
│   ├── role_permissions
│   └── users
├── server_schema        ← Server Service (full ownership)
│   └── servers          ← Monitor, FileIO, Report READ (GRANT SELECT)
├── monitor_schema       ← Monitor Service (full ownership)
│   └── health_check_configs
├── report_schema        ← Report Service (full ownership)
│   ├── report_jobs
│   └── daily_snapshots
└── fileio_schema        ← File I/O Service (full ownership)
    ├── import_jobs
    └── import_job_details
```

**Cross-schema GRANTs:**
- `monitor_user`: SELECT on `server_schema.servers`
- `fileio_user`: SELECT, INSERT on `server_schema.servers`
- `report_user`: SELECT on `server_schema.servers`

### 3.2. Core Tables

#### `server_schema.servers`

| Column | Type | Constraint |
|--------|------|------------|
| `id` | UUID | PK, DEFAULT gen_random_uuid() |
| `server_id` | VARCHAR(100) | UNIQUE, NOT NULL |
| `server_name` | VARCHAR(255) | UNIQUE, NOT NULL |
| `ipv4` | VARCHAR(45) | NOT NULL |
| `status` | VARCHAR(20) | DEFAULT 'off' |
| `os` | VARCHAR(100) | |
| `cpu_cores` | INTEGER | |
| `ram_gb` | NUMERIC | |
| `disk_gb` | NUMERIC | |
| `location` | VARCHAR(255) | |
| `description` | TEXT | |
| `created_at`, `updated_at`, `deleted_at` | TIMESTAMPTZ | Soft delete |

**Indexes:** `server_id`, `server_name`, `ipv4`, `status`, `location`, `os`

#### `auth_schema.users`

| Column | Type |
|--------|------|
| `id` | UUID PK |
| `username` | VARCHAR(100) UNIQUE |
| `email` | VARCHAR(255) UNIQUE |
| `password_hash` | VARCHAR(255) (bcrypt) |
| `full_name` | VARCHAR(255) |
| `role_id` | UUID FK → roles |
| `is_active` | BOOLEAN |
| `last_login_at` | TIMESTAMPTZ |
| `deleted_at` | TIMESTAMPTZ (soft delete) |

---

## 4. API Design

### 4.1. Tổng hợp Endpoints (17 endpoints)

| # | Method | Path | Service | Scope |
|---|--------|------|---------|-------|
| 1 | POST | `/api/v1/auth/register` | Auth | Public |
| 2 | POST | `/api/v1/auth/login` | Auth | Public |
| 3 | POST | `/api/v1/auth/refresh` | Auth | Public |
| 4 | POST | `/api/v1/auth/logout` | Auth | Authenticated |
| 5 | GET | `/api/v1/auth/profile` | Auth | Authenticated |
| 6 | POST | `/api/v1/servers` | Server | `server:create` |
| 7 | GET | `/api/v1/servers` | Server | `server:read` |
| 8 | GET | `/api/v1/servers/:server_id` | Server | `server:read` |
| 9 | PUT | `/api/v1/servers/:server_id` | Server | `server:update` |
| 10 | DELETE | `/api/v1/servers/:server_id` | Server | `server:delete` |
| 11 | POST | `/api/v1/servers/import` | FileIO | `server:import` |
| 12 | GET | `/api/v1/servers/import/:job_id` | FileIO | `server:import` |
| 13 | POST | `/api/v1/servers/export` | FileIO | `server:export` |
| 14 | GET | `/api/v1/monitor/health` | Monitor | Authenticated |
| 15 | GET | `/api/v1/monitor/servers/:server_id/status` | Monitor | Authenticated |
| 16 | GET | `/api/v1/reports/summary` | Report | `report:view` |
| 17 | POST | `/api/v1/reports` | Report | `report:send` |

### 4.2. Authentication Flow

```
Client                          Gateway                         Auth Service
  │                                │                                │
  │  POST /auth/login              │                                │
  │───────────────────────────────►│                                │
  │                                │  Forward                       │
  │                                │───────────────────────────────►│
  │                                │                                │ Validate credentials
  │                                │                                │ Generate JWT (HS256)
  │                                │  {access_token, refresh_token} │
  │                                │◄───────────────────────────────│
  │  {access_token, refresh_token} │                                │
  │◄───────────────────────────────│                                │
  │                                │                                │
  │  GET /servers                  │                                │
  │  Authorization: Bearer <token> │                                │
  │───────────────────────────────►│                                │
  │                                │ Validate JWT + Scope           │
  │                                │ Inject X-User-ID, X-Scopes     │
  │                                │──────────────────────────────► Server Service
  │                                │                                │
```

### 4.3. Error Response Format

```json
{
  "status": "error",
  "code": 42201,
  "message": "Validation failed",
  "errors": [
    {"field": "ipv4", "code": "INVALID_FORMAT", "message": "Invalid IPv4 format"}
  ],
  "meta": {
    "request_id": "req-abc123",
    "timestamp": "2026-06-12T10:00:00Z"
  }
}
```

**Error codes (17 defined):** 401 (Unauthorized), 403 (Forbidden), 404 (Not Found), 409 (Conflict), 422 (Validation), 429 (Rate Limit), 500 (Internal), etc.

---

## 5. Event-Driven Architecture (Kafka)

### 5.1. Topics & Events

| Topic | Partitions | Producer | Consumer | Purpose |
|-------|:----------:|----------|----------|---------|
| `server.created` | 3 | Server Service, FileIO Service | Monitor Service | Auto-register health-check config |
| `server.updated` | 3 | Server Service | Monitor Service | Refresh server metadata |
| `server.deleted` | 3 | Server Service | Monitor Service | Remove from health-check rotation |
| `server.status.changed` | 6 | Monitor Service | (future Alerting) | Status transition events |
| `server.health.batch` | 3 | Monitor Service | (future Analytics) | Batch health-check results |
| `import.job.created` | 3 | FileIO Service | FileIO Service (self) | Async Excel import processing |
| `report.daily.trigger` | 1 | (external/manual) | Report Service | Trigger daily report |

### 5.2. Async Import Flow (File I/O)

```
Client → Gateway → FileIO Handler
  1. Validate file (.xlsx, ≤ 10MB)
  2. Save → /uploads/{uuid}.xlsx
  3. INSERT import_jobs (pending)
  4. Publish Kafka "import.job.created"
     → Nếu fail: mark job failed + cleanup file + return 400
  5. Return 202 Accepted

Kafka Consumer (async):
  1. UPDATE import_jobs → processing
  2. Parse Excel (excelize v2)
  3. For each row:
     a. Validate (server_id, server_name, ipv4 required)
     b. Check duplicate (SELECT server_schema.servers)
     c. If unique → TX: INSERT server + INSERT import_job_details(success)
     d. Publish Kafka "server.created"
     e. If invalid/duplicate → INSERT import_job_details(failed)
  4. UPDATE import_jobs → completed (counts)
  5. Invalidate Redis cache

Client poll: GET /api/v1/servers/import/{job_id}
  → Return success_list + failed_list
```

---

## 6. Health Check Mechanism

### 6.1. Architecture

```
Monitor Service (every 60s)
  │
  ├── Scheduler (Cron)
  │     ├── Step 1: Load servers from PostgreSQL
  │     ├── Step 2: Acquire Redis distributed lock
  │     ├── Step 3: Dispatch to Worker Pool (100 goroutines)
  │     │     └── TCP Connect (net.DialTimeout, 5s) → tcp-simulator:{port}
  │     ├── Step 4: Collect results
  │     ├── Step 5: Bulk index to Elasticsearch
  │     ├── Step 6: Publish status changes to Kafka
  │     ├── Step 7: Update Redis status cache
  │     └── Step 8: Release Redis lock
  │
  └── Worker Pool
        └── 100 concurrent goroutines
             └── Each: TCP Dial → record latency + status
```

### 6.2. Elasticsearch Index

```
Index: server-status-logs
Mapping:
  - server_id    (keyword)
  - server_name  (text)
  - status       (keyword: on/off)
  - latency_ms   (integer)
  - checked_at   (date)
  - error_msg    (text)

Uptime Aggregation (Report Service):
  - Terms on server_id → bucket_script: on_count / total_count * 100
  - Top N lowest uptime servers
```

---

## 7. Caching Strategy (Redis)

| Key Pattern | TTL | Purpose | Service |
|-------------|:---:|---------|---------|
| `servers:list:{hash}` | 5 min | Paginated server list cache | Server |
| `server:detail:{id}` | 10 min | Single server detail cache | Server |
| `report:summary:{start}:{end}` | 1 hour | Uptime summary cache | Report |
| `rate_limit:{ip}` | 1 min | Sliding window rate limiter | Gateway |
| `token:blacklist:{jti}` | 15 min | Logout token blacklist | Auth |
| `health:lock` | 55 sec | Distributed scheduler lock | Monitor |
| `health:status:{server_id}` | 65 sec | Latest health status | Monitor |

**Cache Invalidation:** Write-through on update/delete. Bulk pattern-delete (`SCAN` + `DEL`) on mass import.

---

## 8. Security

### 8.1. JWT Authentication

- **Algorithm:** HS256 (HMAC-SHA256)
- **Access Token:** 15 minutes TTL
- **Refresh Token:** 7 days TTL, rotation on use
- **Blacklist:** Redis SET with TTL on logout

### 8.2. RBAC — Roles & Scopes

| Role | Scopes |
|------|--------|
| **Admin** | `server:create`, `server:read`, `server:update`, `server:delete`, `server:import`, `server:export`, `report:view`, `report:send`, `user:manage` |
| **Operator** | `server:create`, `server:read`, `server:update`, `server:import`, `server:export`, `report:view`, `report:send` |
| **Viewer** | `server:read`, `report:view` |

### 8.3. Defenses

- **SQL Injection:** GORM parameterized queries (no raw SQL)
- **Brute Force:** Redis-based login attempt counter (15 min block after 5 failures)
- **Password:** bcrypt cost factor 12
- **Rate Limiting:** 100 requests/min per IP at Gateway level
- **CORS:** Configurable allowed origins via `.env`

---

## 9. Deployment

### 9.1. Docker Compose — Full Stack

```bash
# 1. Cấu hình
cp .env.example .env
# Sửa JWT_SECRET, SMTP_PASSWORD

# 2. Khởi động toàn bộ (11 runtime containers + kafka-init one-shot)
docker compose up -d

# 3. Kiểm tra
docker compose ps
curl http://localhost:8080/health
```

### 9.2. Container Inventory

| Container | Image | Port |
|-----------|-------|------|
| `vcs-sms-postgres` | postgres:17-alpine | 5432 |
| `vcs-sms-redis` | redis:8-alpine | 6379 |
| `vcs-sms-elasticsearch` | elasticsearch:8.12.0 | 9200 |
| `vcs-sms-kafka` | apache/kafka:3.9.0 | 9092 |
| `vcs-sms-tcp-simulator` | custom (Go) | 9001-19000 |
| `vcs-sms-gateway` | custom (Go) | 8080 |
| `vcs-sms-auth` | custom (Go) | 8081 |
| `vcs-sms-server` | custom (Go) | 8082 |
| `vcs-sms-monitor` | custom (Go) | 8083 |
| `vcs-sms-report` | custom (Go) | 8084 |
| `vcs-sms-fileio` | custom (Go) | 8085 |

### 9.3. Service Dockerfiles

Tất cả 7 services sử dụng multi-stage build:
- **Build stage:** `golang:1.24-alpine` + GOTOOLCHAIN=auto
- **Run stage:** `alpine:3.19` (~15MB)
- **Shared module:** Copy `shared/` → replace directive trong go.mod

---

## 10. Technology Stack

| Layer | Technology | Version |
|-------|-----------|:-------:|
| **Language** | Go | 1.24+ |
| **HTTP Framework** | Gin | v1.12 |
| **ORM** | GORM | v1.31 |
| **Database** | PostgreSQL | 17 |
| **Cache** | Redis | 8 |
| **Search** | Elasticsearch | 8.12 |
| **Message Queue** | Apache Kafka | 3.9 |
| **Kafka Client** | segmentio/kafka-go | v0.4 |
| **Excel** | excelize | v2 |
| **Email** | gomail | v2 |
| **Scheduler** | robfig/cron | v3 |
| **Logging** | zerolog + lumberjack | v1.35 / v2.2 |
| **Config** | viper | v1.21 |
| **Testing** | sqlmock + mockery + httptest | — |
| **Container** | Docker + Docker Compose | 29+ / v5 |

---

## 11. Testing Strategy

### 11.1. Test Infrastructure

| Component | Tool | Pattern |
|-----------|------|---------|
| Database | `go-sqlmock` + GORM | ExpectQuery/ExpectExec with regex |
| HTTP | `httptest.NewRecorder` | Table-driven tests |
| Mocks | Function-callback structs | Custom mock structs implementing interfaces |
| Kafka | `fakeProducer` / `fakeConsumer` | In-memory channel-based |
| Redis | `fakeCache` / `miniredis` | In-memory implementation |
| ES | Mock `http.RoundTripper` | Intercept HTTP calls |

### 11.2. Coverage by Service (Core Business Packages)

| Service | Packages | Coverage |
|---------|----------|:--------:|
| **fileio-service** | excel, handler, repository, service | ≥ 90% |
| **report-service** | email, handler, repository, scheduler, service | ≥ 90% |
| **monitor-service** | checker, worker | ≥ 90% |
| **auth-service** | repository, handler, service | 36-72% |
| **server-service** | handler, repository, service | 49-74% |
| **api-gateway** | middleware | 21% |

> **Ghi chú:** Coverage tính trên core business packages. Các package wiring (`cmd`, `config`, `database`, `model`, `dto`, `mocks`) không có unit test riêng vì là glue code hoặc data structure.

---

## 12. Project Structure (Monorepo)

```
server-management-system/
├── api-gateway/           # API Gateway (Gin)
├── auth-service/          # Authentication Service
├── server-service/        # Server CRUD Service
├── monitor-service/       # Health-check Monitor Service
├── report-service/        # Report & Email Service
├── fileio-service/        # Excel Import/Export Service
├── tcp-simulator/         # 10K TCP Listeners Simulator
├── shared/                # Shared libraries
│   ├── errors/            # Error codes (17)
│   ├── kafka/             # Kafka interfaces + mocks
│   ├── logger/            # Structured logger (zerolog)
│   ├── middleware/        # Request ID, Logger middleware
│   ├── pkg/jwt/           # JWT utilities
│   ├── response/          # Standard API response
│   └── validator/         # Input validation
├── deployments/
│   └── docker/
│       └── postgres/      # init.sql + seed_10k_servers.sql
├── migrations/            # SQL migrations (5 schemas, 20 files)
├── logs/                  # Application logs (mounted volumes)
├── uploads/               # Excel upload directory
├── docker-compose.yml     # Full stack deployment
├── docker-compose.dev.yml # Infrastructure-only (dev mode)
├── Makefile               # Build, test, seed, deploy commands
└── .env.example           # Environment template
```

---

> **Tài liệu liên quan:**
> - [Database Strategy](02-database-strategy.md)
> - [Event-Driven Kafka](03-event-driven-kafka.md)
> - [Worker Pool Design](04-high-concurrency-worker-pool.md)
> - [Security JWT RBAC](05-security-jwt-rbac.md)
> - [Flow: Server CRUD](06-flow-server-crud.md)
> - [Flow: Health Check](07-flow-health-check.md)
> - [Flow: Import/Export](08-flow-import-export.md)
> - [Flow: Reporting & Email](09-flow-reporting-email.md)
> - [API Specification](api-spec.yaml)
