# BÁO CÁO MÔ TẢ VÀ THIẾT KẾ HỆ THỐNG
## VCS Server Management System (VCS-SMS)

> **Chương trình:** VCS Passport — Checkpoint 1
> **Phiên bản:** 1.0
> **Ngày:** 2026-06-19
> **Ngôn ngữ lập trình:** Go 1.24+
> **Kiến trúc:** Microservices + API Gateway + Event-Driven (Kafka)

---

## Mục lục

1. [Tổng quan dự án](#1-tổng-quan-dự-án)
2. [Phân tích yêu cầu](#2-phân-tích-yêu-cầu)
3. [Kiến trúc tổng quan hệ thống](#3-kiến-trúc-tổng-quan-hệ-thống)
4. [Thiết kế cơ sở dữ liệu](#4-thiết-kế-cơ-sở-dữ-liệu)
5. [Kiến trúc Event-Driven (Kafka)](#5-kiến-trúc-event-driven-kafka)
6. [Thiết kế bảo mật — JWT & RBAC](#6-thiết-kế-bảo-mật--jwt--rbac)
7. [Thiết kế API](#7-thiết-kế-api)
8. [Các luồng nghiệp vụ chính](#8-các-luồng-nghiệp-vụ-chính)
9. [Chiến lược Caching (Redis)](#9-chiến-lược-caching-redis)
10. [Logging & Observability](#10-logging--observability)
11. [Chiến lược kiểm thử (Testing)](#11-chiến-lược-kiểm-thử-testing)
12. [Triển khai & vận hành](#12-triển-khai--vận-hành)
13. [Công nghệ sử dụng](#13-công-nghệ-sử-dụng)
14. [Tổng kết](#14-tổng-kết)

---

## 1. Tổng quan dự án

### 1.1. Bài toán

Công ty VCS hiện đang quản lý khoảng **10.000 server**. Dự án **VCS-SMS** xây dựng một hệ thống quản lý tập trung cho toàn bộ danh sách server này, với các nhóm chức năng:

- **Theo dõi trạng thái On/Off** theo thời gian thực bằng cơ chế TCP Health Check định kỳ mỗi 60 giây.
- **Quản lý danh sách server (CRUD)** với hỗ trợ filter, sort, pagination.
- **Import/Export** danh sách server qua file Excel (`.xlsx`).
- **Báo cáo uptime** định kỳ hàng ngày qua Email + API báo cáo chủ động theo khoảng thời gian.
- **Xác thực & phân quyền người dùng** theo 3 vai trò: Admin, Operator, Viewer.

### 1.2. Định nghĩa trạng thái On/Off

Một server được coi là **ON** nếu Monitor Service thực hiện kết nối **TCP** thành công tới cổng tương ứng của server trong khoảng timeout 5 giây; ngược lại (connection refused / timeout) được coi là **OFF**. Đây là cách định nghĩa sát với thực tế giám sát hạ tầng (TCP reachability) và cho phép đo cả độ trễ phản hồi (latency).

### 1.3. Các quyết định thiết kế quan trọng

| # | Vấn đề | Quyết định | Lý do |
|:-:|--------|-----------|-------|
| 1 | Phương pháp Health-check | **TCP Simulator Pool** | Một service Go (`tcp-simulator`) quản lý 10.000 TCP listeners, mở/đóng port động theo công thức toán học. Monitor Service dùng TCP Connect thật (`net.DialTimeout`) — chạy y hệt production. |
| 2 | API Gateway | **Tự viết bằng Gin** | Toàn quyền kiểm soát JWT Middleware, Rate Limiting, Scope, Reverse Proxy; không phụ thuộc Kong/Traefik. |
| 3 | Email Provider | **Gmail SMTP + App Password** | Gửi email thật, cấu hình qua `.env`, đủ quota cho demo. |
| 4 | Cấu trúc repo | **Monorepo** | Quản lý tập trung, chia sẻ `shared/` dễ dàng, 1 `docker-compose.yml` dựng toàn bộ. |
| 5 | Chiến lược DB | **Shared Instance, Separate Schemas** | 1 Postgres vật lý, mỗi service 1 schema riêng + DB user riêng. Loose coupling + nhẹ tài nguyên. |
| 6 | Quy trình phát triển | **Design First** | Hoàn thiện DB Schema → OpenAPI Spec → Sequence Diagrams → Code. |

---

## 2. Phân tích yêu cầu

### 2.1. Yêu cầu chức năng

| Nhóm | Chức năng | Điểm | Hiện thực |
|------|-----------|:----:|-----------|
| **Health Check** | Kiểm tra trạng thái server định kỳ | 2.0 | Worker Pool 100 goroutines + TCP Simulator 10K port |
| **CRUD** | Tạo server | 0.25 | `POST /api/v1/servers` |
| | Xem server (filter, sort, pagination) | 0.25 | `GET /api/v1/servers` |
| | Cập nhật server (không cho sửa `server_id`) | 0.25 | `PUT /api/v1/servers/{server_id}` |
| | Xóa server | 0.25 | `DELETE /api/v1/servers/{server_id}` (soft delete) |
| **Excel** | Import từ Excel (bỏ qua trùng) | 0.5 | `POST /api/v1/servers/import` (bất đồng bộ qua Kafka) |
| | Export ra Excel | 0.5 | `POST /api/v1/servers/export` |
| **Báo cáo** | Báo cáo định kỳ qua Email | 0.5 | Cron 8:00 AM + HTML email |
| | API báo cáo chủ động | 0.5 | `POST /api/v1/reports` |

### 2.2. Yêu cầu phi chức năng

| Yêu cầu | Điểm | Giải pháp |
|---------|:----:|-----------|
| OpenAPI | 0.5 | OpenAPI 3.0.3 (`api-spec.yaml`) + Swagger UI tại Gateway |
| Unit Test ≥ 90% coverage | 0.5 | Toàn bộ core business packages của 7 service đạt ≥ 90% |
| Chống SQL Injection | 0.5 | GORM parameterized queries (không raw SQL) |
| Error handling rõ ràng (mã + mô tả) | 0.5 | 17 mã lỗi định nghĩa sẵn + format response chuẩn |
| Log ra file + logrotate | 0.5 | zerolog (JSON) + lumberjack rotation |
| JWT Authentication + Scope per API | 0.5 | JWT HS256 + RBAC 10 scopes tại Gateway |
| Elasticsearch tính Uptime | 1.0 | Bulk index + aggregation query |
| PostgreSQL | — | PostgreSQL 17, 5 schemas |
| Redis Cache | 0.5 | Cache-aside + rate limit + distributed lock + blacklist |
| Công nghệ khác | 0.5 | Kafka, Docker Compose, TCP Simulator, Next.js Frontend |

### 2.3. Thông tin cơ bản của một Server

| Trường | Kiểu | Mô tả |
|--------|------|-------|
| `server_id` | VARCHAR(100), UNIQUE | Mã định danh (không trùng, **không sửa được**) |
| `server_name` | VARCHAR(255), UNIQUE | Tên server (không trùng) |
| `ipv4` | VARCHAR(45) | Địa chỉ IPv4 |
| `status` | VARCHAR(20) | Trạng thái `on`/`off` (mặc định `off`) |
| `created_at` / `updated_at` | TIMESTAMPTZ | Thời gian tạo / cập nhật cuối |
| `os`, `cpu_cores`, `ram_gb`, `disk_gb`, `location`, `description` | — | Thông tin mở rộng tự định nghĩa |

---

## 3. Kiến trúc tổng quan hệ thống

### 3.1. Sơ đồ kiến trúc

```
┌──────────────────────────────────────────────────────────────────┐
│                        CLIENT LAYER                               │
│        Web UI (Next.js) / Postman / cURL / Swagger UI             │
└───────────────────────────┬──────────────────────────────────────┘
                            │ HTTP :8080
                    ┌───────▼────────┐
                    │  API GATEWAY   │  Gin Framework
                    │  Port: 8080    │  • JWT Validation
                    │                │  • Scope RBAC (10 scopes)
                    │                │  • Rate Limiting (Redis)
                    │                │  • Reverse Proxy
                    └───┬───┬───┬───┬┘
                        │   │   │   │
        ┌───────────────┼───┼───┼───┼───────────────┐
        ▼               ▼   ▼   ▼   ▼               ▼
┌──────────┐  ┌──────────┐ ┌──────────┐ ┌──────────────┐
│  AUTH    │  │  SERVER  │ │ MONITOR  │ │   REPORT     │
│  :8081   │  │  :8082   │ │  :8083   │ │   :8084      │
│ Register │  │ CRUD     │ │ Health-  │ │ Uptime       │
│ Login    │  │ Filter   │ │ Check    │ │ Summary      │
│ JWT      │  │ Sort     │ │ 60s cron │ │ Email (SMTP) │
│ Refresh  │  │ Paginate │ │ Worker   │ │ Daily Cron   │
│ Logout   │  │ Cache    │ │ Pool     │ │ HTML Template│
│ Profile  │  │ Events   │ │ ES Bulk  │ │ Snapshots    │
└────┬─────┘  └────┬─────┘ └────┬─────┘ └──────┬───────┘
     └─────────────┴────────────┴───────────────┘
┌──────────────────────────────────────────────────────┐
│                 FILE I/O SERVICE  (:8085)            │
│   Import Excel (async Kafka) + Export Excel (sync)   │
└────────────────────────┬─────────────────────────────┘
                         ▼
              ┌──────────────────┐
              │  TCP SIMULATOR   │  Ports 9001–19000
              │  10K Listeners   │  Math Engine (On/Off động)
              └──────────────────┘

Hạ tầng dùng chung: PostgreSQL 17 | Redis 8 | Elasticsearch 8.12 | Kafka 3.9
```

### 3.2. Vì sao chọn kiến trúc Microservices?

Với hệ thống quản lý 10.000 server, tải trọng (workload) của các chức năng rất khác nhau:

- **Monitor Service**: hoạt động liên tục, mỗi phút quét 10.000 server — chịu tải CPU và Network I/O nặng nhất.
- **TCP Simulator**: service hạ tầng hỗ trợ, quản lý 10.000 TCP listeners, mở/đóng port động.
- **Server Service (CRUD)**: tải thấp, thỉnh thoảng mới có thao tác CRUD từ người dùng.
- **Report & File I/O**: tải bất chợt (spiky), cần nhiều RAM để parse/gen Excel hoặc gom dữ liệu lớn.

**Lợi ích:**

| Lợi ích | Mô tả |
|---------|-------|
| **Mở rộng độc lập** | Khi số server tăng lên 50.000, chỉ cần scale Monitor Service và TCP Simulator, không lãng phí tài nguyên cho Auth/File I/O. |
| **Cô lập lỗi** | Import Excel bị OOM chỉ làm sập File I/O Service; Monitor Service vẫn ping server bình thường. |
| **Bảo mật** | Auth Service chứa logic mật khẩu nhạy cảm được bảo vệ riêng; service khác không biết cấu trúc bảng Users. |

### 3.3. Chiến lược Monorepo

Toàn bộ 7 service nằm trong cùng một Git Repository:
- **Quản lý phiên bản thống nhất**: tránh xung đột phiên bản thư viện giữa các service.
- **Thư mục `shared/`**: chia sẻ cấu trúc dữ liệu chung (Errors, Logger, Kafka Interface, JWT, Response) — không cần publish package riêng.
- **Triển khai dễ dàng**: 1 file `docker-compose.yml` duy nhất dựng toàn bộ hệ thống.

### 3.4. API Gateway — tự viết bằng Gin

API Gateway đóng vai trò "điều phối viên" đứng trước 5 microservices và thực thi chuỗi middleware:

```
Request → Recovery → Logger → CORS → Rate Limiter (Redis) → JWT Auth → Scope Check → Reverse Proxy → Response
```

1. **Entry point duy nhất**: client chỉ gọi vào port 8080; Gateway route tới đúng service (`/api/v1/auth/*` → Auth, `/api/v1/servers/*` → Server/FileIO, …).
2. **Xác thực tập trung**: parse JWT, kiểm tra hợp lệ, lấy `user_id` + `scopes`, inject vào HTTP Header `X-User-ID`, `X-User-Scopes` trước khi forward.
3. **Rate Limiting**: dùng Redis giới hạn 100 requests/phút/IP.
4. **Log tập trung**: sinh `request_id` duy nhất, ghi log thời gian xử lý cho toàn hệ thống.

### 3.5. Ranh giới các service (Service Boundaries)

| Service | Schema | Bảng | Phụ thuộc |
|---------|--------|------|-----------|
| **API Gateway** | — | — | Redis |
| **Auth Service** | `auth_schema` | `users`, `roles`, `role_permissions` | PostgreSQL, Redis |
| **Server Service** | `server_schema` | `servers` | PostgreSQL, Redis, Kafka |
| **Monitor Service** | `monitor_schema` | `health_check_configs` | PostgreSQL, Redis, Kafka, Elasticsearch, TCP Simulator |
| **Report Service** | `report_schema` | `report_jobs`, `daily_snapshots` | PostgreSQL, Elasticsearch, Kafka, SMTP |
| **File I/O Service** | `fileio_schema` | `import_jobs`, `import_job_details` | PostgreSQL (cross-schema), Kafka |
| **TCP Simulator** | — | — | Standalone |

---

## 4. Thiết kế cơ sở dữ liệu

### 4.1. Chiến lược: Shared Instance, Separate Schemas

Hệ thống dùng **PostgreSQL 17** với **1 Database vật lý duy nhất** (`vcs_sms`), bên trong tạo **5 Schemas logic** riêng biệt:

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

**Lý do chọn chiến lược này:**

1. **Đạt ranh giới Microservice ở cấp quyền hạn**: mỗi service có DB user riêng, chỉ có quyền trên schema của mình. Nếu lập trình viên vô tình viết query từ `report-service` `DELETE FROM auth_schema.users`, PostgreSQL chặn ngay (Permission Denied).
2. **Nhẹ tài nguyên**: chạy 5 Postgres database riêng tốn nhiều RAM/CPU overhead (background workers, shared memory). Shared Instance tối ưu khi chạy Docker trên máy local.
3. **Giải quyết Cross-Schema Read**: `monitor-service` cần danh sách 10.000 server IP để ping. Thay vì gọi HTTP API (chậm, phụ thuộc), ta `GRANT SELECT` trên `server_schema.servers` cho `monitor_user` — query tốc độ nội bộ DB. Chỉ cấp quyền ĐỌC; quyền GHI vẫn độc quyền của `server-service` → giữ Data Integrity.

**Cross-schema GRANTs:**

| DB User | Quyền trên `server_schema.servers` |
|---------|-----------------------------------|
| `monitor_user` | SELECT |
| `fileio_user` | SELECT, INSERT |
| `report_user` | SELECT |

### 4.2. Các bảng chính

#### `server_schema.servers`

| Column | Type | Constraint |
|--------|------|------------|
| `id` | UUID | PK, DEFAULT `gen_random_uuid()` |
| `server_id` | VARCHAR(100) | UNIQUE, NOT NULL |
| `server_name` | VARCHAR(255) | UNIQUE, NOT NULL |
| `ipv4` | VARCHAR(45) | NOT NULL |
| `status` | VARCHAR(20) | DEFAULT `'off'` |
| `os` | VARCHAR(100) | |
| `cpu_cores` | INTEGER | |
| `ram_gb` / `disk_gb` | NUMERIC | |
| `location` | VARCHAR(255) | |
| `description` | TEXT | |
| `created_at`, `updated_at`, `deleted_at` | TIMESTAMPTZ | Soft delete |

**Indexes:** `server_id`, `server_name`, `ipv4`, `status`, `location`, `os`.

#### `auth_schema.users`

| Column | Type | Constraint |
|--------|------|------------|
| `id` | UUID | PK |
| `username` | VARCHAR(100) | UNIQUE |
| `email` | VARCHAR(255) | UNIQUE |
| `password_hash` | VARCHAR(255) | bcrypt cost 12 |
| `full_name` | VARCHAR(255) | |
| `role_id` | UUID | FK → roles |
| `is_active` | BOOLEAN | DEFAULT TRUE |
| `last_login_at` | TIMESTAMPTZ | |
| `deleted_at` | TIMESTAMPTZ | Soft delete |

#### Các bảng khác
- **`auth_schema.roles` & `role_permissions`**: 3 roles mặc định (`admin`, `operator`, `viewer`); mapping role → scope (many-to-many).
- **`fileio_schema.import_jobs` & `import_job_details`**: theo dõi tiến trình import (`pending → processing → completed/failed`) và chi tiết từng dòng (success/failed + lý do).
- **`report_schema.report_jobs` & `daily_snapshots`**: theo dõi yêu cầu gửi báo cáo và lưu snapshot uptime hàng ngày (tránh tính lại từ Elasticsearch).
- **`monitor_schema.health_check_configs`**: cấu hình health-check cho mỗi server (port TCP tương ứng).

> Sơ đồ ERD chi tiết từng schema xem trong thư mục `docs/DB Schema/`.

### 4.3. Bổ trợ bằng Redis & Elasticsearch

| Công nghệ | Phiên bản | Mục đích |
|-----------|:---------:|----------|
| **Redis** | 8 | Rate Limiting, Distributed Lock, JWT Blacklist, Caching API |
| **Elasticsearch** | 8.12 | Lưu log health-check (time-series). Mỗi phút 10.000 bản ghi (~14.4 triệu/ngày nếu chạy 24/24), tính uptime aggregation tốc độ mili-giây |

---

## 5. Kiến trúc Event-Driven (Kafka)

### 5.1. Vì sao cần Kafka thay vì gọi HTTP API trực tiếp?

Ví dụ luồng Import Excel 5.000 server: nếu xử lý đồng bộ trong 1 request HTTP, người dùng phải chờ lâu và rủi ro timeout cao. Với Kafka, `fileio-service` chỉ lưu file, tạo `import_jobs`, bắn event `import.job.created` rồi phản hồi ngay `job_id`; consumer nền xử lý dần.

**Lợi ích:**

| Lợi ích | Mô tả |
|---------|-------|
| **Decoupling** | Producer không cần biết Consumer là ai/có sống không. Consumer sập thì message vẫn nằm an toàn trong Kafka. |
| **Buffer Spikes** | Khi nhiều người cùng import hàng chục nghìn server, Kafka đệm như hàng đợi, service rút dần xử lý theo sức. |
| **Eventual Consistency** | Trạng thái hệ thống đồng bộ dần qua dòng sự kiện chung. |

### 5.2. Kafka KRaft Mode

Sử dụng Apache Kafka 3.9 ở chế độ **KRaft** — loại bỏ hoàn toàn ZooKeeper: hệ thống nhẹ hơn, start/stop nhanh hơn. Node Kafka tự đóng vai trò vừa broker vừa controller (`process.roles=broker,controller`).

### 5.3. Các Topics & Events

| Topic | Partitions | Producer | Consumer | Mục đích |
|-------|:----------:|----------|----------|----------|
| `server.created` | 3 | Server, FileIO | Monitor | Tự động thêm cấu hình health-check |
| `server.updated` | 3 | Server | Monitor | Cập nhật metadata server |
| `server.deleted` | 3 | Server | Monitor | Gỡ khỏi danh sách health-check |
| `server.status.changed` | 6 | Monitor | (future Alerting) | Sự kiện chuyển trạng thái On/Off |
| `server.health.batch` | 3 | Monitor | (future Analytics) | Batch kết quả health-check |
| `import.job.created` | 3 | FileIO | FileIO (self) | Xử lý import Excel bất đồng bộ |
| `report.daily.trigger` | 1 | (manual) | Report | Trigger báo cáo hàng ngày |

### 5.4. Go Kafka Client: segmentio/kafka-go

Chọn `segmentio/kafka-go` thay vì `IBM/sarama` vì: API đơn giản (`WriteMessages()`/`ReadMessage()`), context-native, ít dependencies (không kéo theo gokrb5), tự động reconnect.

---

## 6. Thiết kế bảo mật — JWT & RBAC

### 6.1. JWT Authentication (Stateless)

- **Thuật toán:** HS256 (HMAC-SHA256).
- **Access Token:** TTL 15 phút. **Refresh Token:** TTL 7 ngày, rotation khi dùng.
- **Payload:** `user_id`, `role`, `scopes[]`, `jti` (JWT ID).
- Gateway chỉ cần Secret Key để xác minh chữ ký — **không cần truy vấn DB** → không trở thành bottleneck.

**Refresh Token Flow:** Đăng nhập → nhận `access_token` (15m) + `refresh_token` (7d). Khi access token hết hạn, client tự gửi refresh token lên `/auth/refresh` để nhận cặp token mới — bảo mật cao mà không hy sinh trải nghiệm.

### 6.2. JWT Blacklist (Logout)

Vì JWT là stateless, khi logout hệ thống lưu `jti` vào Redis (`auth:blacklist:<jti> = 1`) với TTL bằng thời gian sống còn lại của token. Gateway kiểm tra blacklist trước khi chấp nhận token (< 1ms).

### 6.3. RBAC — Roles & Scopes

| Role | Scopes |
|------|--------|
| **Admin** | `server:create`, `server:read`, `server:update`, `server:delete`, `server:import`, `server:export`, `monitor:view`, `report:view`, `report:send`, `user:manage` |
| **Operator** | `server:create`, `server:read`, `server:update`, `server:import`, `server:export`, `monitor:view`, `report:view`, `report:send` |
| **Viewer** | `server:read`, `server:export`, `report:view` |

Mỗi route khai báo scope cần thiết. Gateway đọc mảng `scopes` trong JWT; thiếu scope → HTTP **403 Forbidden**, backend không bao giờ nhận request trái phép. Việc map Role → Scope nằm trong DB (`role_permissions`), tránh hardcode `if role == "admin"` → dễ mở rộng.

### 6.4. Các biện pháp bảo mật khác

| Biện pháp | Chi tiết |
|-----------|----------|
| **SQL Injection** | GORM parameterized queries — không raw SQL |
| **Brute Force** | Redis-based login attempt counter (khóa 15 phút sau 5 lần thất bại) |
| **Password** | bcrypt cost factor 12 |
| **Rate Limiting** | 100 requests/phút per IP tại Gateway |
| **CORS** | Allowed origins cấu hình qua `.env` |
| **Security Context Injection** | Gateway inject `X-User-ID`, `X-User-Scopes`; backend chỉ đọc header tĩnh |

---

## 7. Thiết kế API

### 7.1. Tổng hợp Endpoints (18 endpoints)

| # | Method | Path | Service | Scope |
|:-:|--------|------|---------|-------|
| 1 | POST | `/api/v1/auth/register` | Auth | Public |
| 2 | POST | `/api/v1/auth/login` | Auth | Public |
| 3 | POST | `/api/v1/auth/refresh` | Auth | Public |
| 4 | POST | `/api/v1/auth/logout` | Auth | Authenticated |
| 5 | GET | `/api/v1/auth/profile` | Auth | Authenticated |
| 6 | GET | `/api/v1/auth/users` | Auth | `user:manage` |
| 7 | PUT | `/api/v1/auth/users/{user_id}/role` | Auth | `user:manage` |
| 8 | POST | `/api/v1/servers` | Server | `server:create` |
| 9 | GET | `/api/v1/servers` | Server | `server:read` |
| 10 | GET | `/api/v1/servers/{server_id}` | Server | `server:read` |
| 11 | PUT | `/api/v1/servers/{server_id}` | Server | `server:update` |
| 12 | DELETE | `/api/v1/servers/{server_id}` | Server | `server:delete` |
| 13 | POST | `/api/v1/servers/import` | FileIO | `server:import` |
| 14 | GET | `/api/v1/servers/import/{job_id}` | FileIO | `server:import` |
| 15 | POST | `/api/v1/servers/export` | FileIO | `server:export` |
| 16 | GET | `/api/v1/monitor/status` | Monitor | `monitor:view` |
| 17 | GET | `/api/v1/reports/summary` | Report | `report:view` |
| 18 | POST | `/api/v1/reports` | Report | `report:send` |

### 7.2. Định dạng Response chuẩn

Thành công:
```json
{ "status": "success", "code": 200, "message": "...", "data": { } }
```

Lỗi (mã + mô tả rõ ràng):
```json
{
  "status": "error",
  "code": 42201,
  "message": "Validation failed",
  "errors": [
    {"field": "ipv4", "code": "INVALID_FORMAT", "message": "Invalid IPv4 format"}
  ],
  "meta": { "request_id": "req-abc123", "timestamp": "2026-06-12T10:00:00Z" }
}
```

**Mã lỗi định nghĩa (17):** 401 (Unauthorized), 403 (Forbidden), 404 (Not Found), 409 (Conflict), 422 (Validation), 429 (Rate Limit), 500 (Internal), …

### 7.3. OpenAPI / Swagger

Toàn bộ API được mô tả bằng **OpenAPI 3.0.3** (`api-spec.yaml`). Swagger UI tích hợp sẵn tại `http://localhost:8080/swagger/index.html`.

---

## 8. Các luồng nghiệp vụ chính

### 8.1. Health Check 10.000 server (2.0 điểm) — trái tim hệ thống

#### TCP Simulator — giả lập 10.000 server thật
**Bài toán:** lấy đâu ra 10.000 IP thật để test? Nếu toàn IP ảo → 100% Offline → vô nghĩa.
**Giải pháp:** TCP Simulator — một chương trình Go quản lý 10.000 TCP listeners:
- Mỗi server gán 1 port (`SRV-00001 → 9001`, …, `SRV-10000 → 19000`).
- **Math Engine** mỗi 30 giây tính On/Off dựa trên `uptime_rate` (vd 0.95), biến thiên hàm Sin theo giờ (pattern ngày/đêm) và offset riêng từng server.
- Server **ON** → mở TCP port; **OFF** → đóng port (connection refused).
- Monitor Service dùng TCP Connect thật → code chạy y hệt production, chỉ tốn 1 container (~100–256MB RAM).

#### Worker Pool Pattern
Thay vì mở 10.000 luồng, hệ thống tạo cố định **100 goroutines**. Channel `jobs` chứa 10.000 server; 100 worker liên tục lấy server ra ping. Tại mọi thời điểm chỉ tối đa 100 kết nối TCP mở (throttling). Timeout TCP 5 giây; thực tế phản hồi < 100ms → dư sức quét 10.000 server trong 1 phút. Số worker cấu hình được qua `.env`.

#### Distributed Lock với Redis
Để tránh nhiều instance `monitor-service` cùng quét: trước mỗi vòng, instance giành key `health-check-lock` (TTL 90s) trong Redis. Instance không lấy được khóa thì bỏ qua vòng đó. Nếu instance giữ khóa crash, sau 90s khóa tự hết hạn → tránh deadlock.

#### Quy trình mỗi phút
1. Cron reo → xin distributed lock → load danh sách server từ PostgreSQL (đọc chéo schema).
2. 100 worker TCP Connect song song tới `tcp-simulator:9001-19000`.
3. So sánh 10.000 kết quả mới với kết quả phút trước (Redis) → tìm server đổi trạng thái.
4. Bắn event `server.status.changed` cho các server thay đổi (vd 10/10.000).
5. **Batch Update** PostgreSQL chỉ các server thay đổi (tiết kiệm I/O).
6. **Bulk Index** toàn bộ 10.000 bản ghi log vào Elasticsearch (vài chục ms).
7. Cập nhật trạng thái mới nhất vào Redis cho phút sau so sánh.

#### Elasticsearch Index `server-status-logs`
Mapping: `server_id` (keyword), `server_name` (text), `status` (keyword on/off), `latency_ms` (integer), `checked_at` (date), `error_msg` (text).

### 8.2. Quản lý Server (CRUD)
- **Create**: validate (IP format, trùng lặp) → lưu PostgreSQL (`status=off`) → publish `server.created` → Monitor tạo cấu hình health-check.
- **Update**: cập nhật PostgreSQL → xóa cache Redis → publish `server.updated`. **Không cho sửa `server_id`.**
- **Delete (soft)**: set `deleted_at` → publish `server.deleted` → Monitor gỡ khỏi danh sách.
- **Read/Filter**: lọc theo `status`, `os`, `location`, `server_name`, `ipv4`; `sort_by` + `sort_order`; `page` + `page_size`. Kết quả cache Redis.

### 8.3. Import & Export Excel
- **Import (async)**: upload `.xlsx` → tạo `import_job` (pending) → trả `job_id` ngay → bắn `import.job.created` → consumer nền parse, validate, ghi các dòng hợp lệ, **bỏ qua `server_id`/`server_name` trùng**, ghi `import_job_details`, publish `server.created`. Client poll `GET /servers/import/{job_id}` để lấy danh sách thành công/thất bại + lý do.
- **Export (sync)**: query DB theo bộ lọc → tạo file Excel trong RAM (`excelize`) → stream về client.

### 8.4. Báo cáo & Email
- **Báo cáo định kỳ (Daily Cron 8:00 AM)**: query Elasticsearch tính uptime ngày hôm trước → lưu snapshot PostgreSQL → tạo HTML email (header gradient, 4 stat cards, bảng Top 10 server uptime thấp nhất) → gửi qua Gmail SMTP.
- **Báo cáo chủ động (On-Demand)**: `POST /api/v1/reports` với khoảng ngày + email → tính toán → gửi email.

**Công thức Uptime:**
```
Uptime(server) = (số lần check status = "on") / (tổng số lần check thực tế) × 100%
```

---

## 9. Chiến lược Caching (Redis)

| Key Pattern | TTL | Mục đích | Service |
|-------------|:---:|---------|---------|
| `servers:list:{hash}` | 5 min | Cache danh sách server phân trang | Server |
| `server:detail:{id}` | 10 min | Cache chi tiết 1 server | Server |
| `report:summary:{start}:{end}` | 1 hour | Cache uptime summary | Report |
| `rate_limit:{ip}` | 1 min | Sliding window rate limiter | Gateway |
| `token:blacklist:{jti}` | 15 min | Logout token blacklist | Auth |
| `health:lock` | 55–90 sec | Distributed scheduler lock | Monitor |
| `health:status:{server_id}` | 65 sec | Trạng thái health mới nhất | Monitor |

**Cache Invalidation:** write-through khi update/delete; bulk pattern-delete (`SCAN` + `DEL`) khi mass import.

---

## 10. Logging & Observability

- **Structured JSON Logging** với **zerolog** (zero-allocation). Mỗi log entry gồm: `timestamp`, `level`, `request_id`, `service`, `message`, `caller`.
- **Log Rotation** với **lumberjack**: cấu hình max size, max backups, max age; tự xoay file khi đạt giới hạn. Log files được mount volume trong Docker (`logs/`).
- `request_id` xuyên suốt từ Gateway xuống các service giúp trace 1 request qua toàn hệ thống.

---

## 11. Chiến lược kiểm thử (Testing)

### 11.1. Hạ tầng test

| Thành phần | Công cụ | Pattern |
|-----------|---------|---------|
| Database | `go-sqlmock` + GORM | ExpectQuery/ExpectExec với regex |
| HTTP | `httptest.NewRecorder` | Table-driven tests |
| Mocks | Function-callback structs | Mock struct implement interface |
| Kafka | `fakeProducer` / `fakeConsumer` | In-memory channel-based |
| Redis | `fakeCache` / `miniredis` | In-memory |
| Elasticsearch | Mock `http.RoundTripper` | Intercept HTTP call |

### 11.2. Coverage theo service (core business packages)

Số liệu đo bằng `go test ./... -cover -count=1` trên từng module. Toàn bộ core business package đều đạt **≥ 90%**.

| Service | Package (coverage) | Khoảng |
|---------|--------------------|:------:|
| **auth-service** | handler 96.8% · repository 95.2% · service 94.4% | 94.4–96.8% |
| **server-service** | handler 96.6% · repository 98.2% · service 91.4% | 91.4–98.2% |
| **api-gateway** | middleware 93.0% · proxy 91.7% · router 97.6% · swagger 91.3% | 91.3–97.6% |
| **monitor-service** | checker 100% · service 100% · repository 97.8% · scheduler 94.4% · worker 90.0% | 90.0–100% |
| **report-service** | handler 94.3% · email 94.1% · scheduler 93.8% · service 90.3% · repository 90.2% | 90.2–94.3% |
| **fileio-service** | handler 100% · repository 95.1% · service 91.0% · excel 90.2% | 90.2–100% |
| **tcp-simulator** | simulator 94.1% | 94.1% |
| **shared** | pkg/jwt 94.4% · kafka 92.6% | 92.6–94.4% |

> Coverage tính trên core business packages. Các package wiring/khai báo (`cmd`, `config`, `database`, `model`, `dto`, `mocks`) là glue code hoặc data structure nên không có unit test riêng (hiển thị `no test files`).

---

## 12. Triển khai & vận hành

### 12.1. Docker Compose — Full Stack

```bash
cp .env.example .env          # cấu hình JWT_SECRET, SMTP_PASSWORD...
docker compose up -d --build  # 11 runtime containers + kafka-init one-shot
docker compose ps
make seed                     # seed 10.000 servers
curl http://localhost:8080/health
```

### 12.2. Container Inventory

| Container | Image | Port | Mô tả |
|-----------|-------|:----:|-------|
| `vcs-sms-postgres` | postgres:17-alpine | 5432 | Primary database |
| `vcs-sms-redis` | redis:8-alpine | 6379 | Cache + Rate Limiting |
| `vcs-sms-elasticsearch` | elasticsearch:8.12.0 | 9200 | Uptime logs |
| `vcs-sms-kafka` | apache/kafka:3.9.0 | 9092 | Message broker |
| `vcs-sms-gateway` | custom (Go) | 8080 | API Gateway |
| `vcs-sms-auth` | custom (Go) | 8081 | Auth Service |
| `vcs-sms-server` | custom (Go) | 8082 | Server Service |
| `vcs-sms-monitor` | custom (Go) | 8083 | Monitor Service |
| `vcs-sms-report` | custom (Go) | 8084 | Report Service |
| `vcs-sms-fileio` | custom (Go) | 8085 | FileIO Service |
| `vcs-sms-tcp-simulator` | custom (Go) | 9001–19000 | TCP Simulator |
| `vcs-sms-web` | custom (Next.js) | 3000 | Frontend Dashboard |

Tất cả service Go dùng **multi-stage build**: build stage `golang:1.24-alpine`, run stage `alpine:3.19` (~15MB).

---

## 13. Công nghệ sử dụng

| Lớp | Công nghệ | Phiên bản |
|-----|-----------|:---------:|
| Ngôn ngữ | Go | 1.24+ |
| HTTP Framework | Gin | v1.12 |
| ORM | GORM | v1.31 |
| Database | PostgreSQL | 17 |
| Cache | Redis | 8 |
| Search/Analytics | Elasticsearch | 8.12 |
| Message Queue | Apache Kafka (KRaft) | 3.9 |
| Kafka Client | segmentio/kafka-go | v0.4 |
| Excel | excelize | v2 |
| Email | gomail | v2 |
| Scheduler | robfig/cron | v3 |
| Logging | zerolog + lumberjack | v1.35 / v2.2 |
| Config | viper | v1.21 |
| Testing | sqlmock + httptest | — |
| Frontend | Next.js + React + TailwindCSS | — |
| Container | Docker + Docker Compose | 29+ / v5 |

---

## 14. Tổng kết

### 14.1. Mức độ hoàn thành

| Tính năng | Trạng thái |
|-----------|:----------:|
| Health Check 10K server (TCP, Worker Pool, ES) | ✅ Hoàn thành |
| Server CRUD (filter, sort, paginate, cache) | ✅ Hoàn thành |
| Import Excel (async Kafka) | ✅ Hoàn thành |
| Export Excel | ✅ Hoàn thành |
| Báo cáo định kỳ (Daily Email) | ✅ Hoàn thành |
| API báo cáo chủ động | ✅ Hoàn thành |
| JWT Auth + Refresh + Blacklist | ✅ Hoàn thành |
| RBAC (3 roles, 10 scopes) | ✅ Hoàn thành |
| OpenAPI / Swagger UI | ✅ Hoàn thành |
| Unit Test ≥ 90% (core) | ✅ Hoàn thành |
| SQL Injection Protection (GORM) | ✅ Hoàn thành |
| Error Handling (17 mã lỗi) | ✅ Hoàn thành |
| Logging + Logrotate | ✅ Hoàn thành |
| Elasticsearch Uptime | ✅ Hoàn thành |
| Redis Cache | ✅ Hoàn thành |
| Kafka Event-Driven (7 topics) | ✅ Hoàn thành |
| Docker Compose + Web UI | ✅ Hoàn thành |

### 14.2. Điểm nhấn kiến trúc

- **TCP Simulator Pool**: giải pháp sáng tạo giả lập 10.000 server thật bằng 1 container, cho phép Monitor Service chạy y hệt production.
- **Worker Pool + Distributed Lock**: xử lý concurrent health-check có throttling, an toàn khi scale nhiều instance.
- **Separate Schemas + Cross-Schema GRANT**: cô lập dữ liệu cấp database nhưng vẫn tối ưu hiệu năng đọc chéo.
- **Event-Driven (Kafka KRaft)**: decoupling, chống sốc tải, nền tảng cho Alerting/Analytics tương lai.
- **Cache-aside (Redis)**: tối ưu read-heavy endpoints + rate limiting + distributed lock + blacklist.

---

> **Tài liệu liên quan:** `docs/architecture.md`, `docs/api-spec.yaml`, `docs/02-database-strategy.md`, `docs/03-event-driven-kafka.md`, `docs/04-high-concurrency-worker-pool.md`, `docs/05-security-jwt-rbac.md`, `docs/06-flow-server-crud.md`, `docs/07-flow-health-check.md`, `docs/08-flow-import-export.md`, `docs/09-flow-reporting-email.md`.
>
> **VCS Server Management System © 2026** — Chương trình đào tạo VCS Passport
