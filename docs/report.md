> # ⛔ TÀI LIỆU ĐÃ LỖI THỜI — GIỮ LẠI LÀM LỊCH SỬ
>
> Tài liệu này mô tả kiến trúc **Checkpoint 1** và **không còn đúng với hệ thống hiện tại**.
> Nó được giữ lại để đối chiếu quá trình refactor, không phải để tra cứu.
>
> **Bốn thay đổi lớn khiến tài liệu này sai:**
>
> | Bản này (Checkpoint 1) | Hệ thống hiện tại |
> |---|---|
> | **Kafka** cho event-driven | **Redis Stream** — 1 event, 1 producer, 1 consumer group |
> | **API Gateway tự viết** | **Traefik** + ForwardAuth |
> | **5 service** (có FileIO Service riêng) | **4 service** — import/export là adapter của server-service |
> | **Shared schema** một database | **Database-per-service** — `identity_db`, `server_db`, `report_db` |
>
> **Hãy đọc thay thế:**
> - [BaoCao-MoTa-ThietKe-HeThong.md](./BaoCao-MoTa-ThietKe-HeThong.md) — bản báo cáo thiết kế hiện hành
> - [01-architecture-overview.md](./01-architecture-overview.md) — tổng quan kiến trúc
> - [`.claude/diagrams/`](../.claude/diagrams/README.md) — bộ sơ đồ hệ thống
>
> *(Ghi chú thêm ngày 24/07/2026.)*

---

# BÁO CÁO DỰ ÁN: VCS SERVER MANAGEMENT SYSTEM (VCS-SMS)

> **Chương trình đào tạo:** VCS Passport  
> **Checkpoint:** 1  
> **Phiên bản:** 1.0 — ⛔ **đã bị thay thế, xem banner ở đầu file**  
> **Ngày:** 2026-06-15  
> **Tác giả:** Đặng Huy Chiêu Hoàng  
> **Ngôn ngữ:** Go 1.24+  
> **Kiến trúc:** Microservices + API Gateway + Event-Driven (Kafka)

---

## Mục lục

1. [Tổng quan dự án](#1-tổng-quan-dự-án)
2. [Kiến trúc hệ thống](#2-kiến-trúc-hệ-thống)
3. [Thiết kế cơ sở dữ liệu](#3-thiết-kế-cơ-sở-dữ-liệu)
4. [Kiến trúc Event-Driven (Kafka)](#4-kiến-trúc-event-driven-kafka)
5. [Thiết kế bảo mật — JWT & RBAC](#5-thiết-kế-bảo-mật--jwt--rbac)
6. [Thiết kế API](#6-thiết-kế-api)
7. [Các luồng nghiệp vụ chính](#7-các-luồng-nghiệp-vụ-chính)
8. [Chiến lược Caching (Redis)](#8-chiến-lược-caching-redis)
9. [Chiến lược Testing](#9-chiến-lược-testing)
10. [Logging & Observability](#10-logging--observability)
11. [Triển khai & Vận hành](#11-triển-khai--vận-hành)
12. [Technology Stack](#12-technology-stack)
13. [Hướng dẫn sử dụng](#13-hướng-dẫn-sử-dụng)
14. [Tổng kết & Đánh giá](#14-tổng-kết--đánh-giá)

---

## 1. Tổng quan dự án

### 1.1. Bài toán

Công ty VCS hiện đang quản lý khoảng **10.000 server**. Dự án VCS-SMS xây dựng một hệ thống quản lý tập trung cho toàn bộ danh sách server này, bao gồm:

- **Theo dõi trạng thái On/Off** theo thời gian thực bằng TCP Health Check mỗi 60 giây
- **Quản lý danh sách server** (CRUD) với hỗ trợ filter, sort, pagination
- **Import/Export** danh sách server qua file Excel (.xlsx)
- **Báo cáo uptime** định kỳ hàng ngày qua Email + API báo cáo chủ động
- **Phân quyền người dùng** theo 3 roles: Admin, Operator, Viewer

### 1.2. Tổng hợp yêu cầu & điểm số

| Nhóm | Chức năng | Điểm |
|------|-----------|:----:|
| **Chức năng** | Kiểm tra trạng thái server định kỳ (Health Check) | 2.0 |
| | Tạo Server (Create) | 0.25 |
| | Xem Server (View — filter, sort, pagination) | 0.25 |
| | Cập nhật Server (Update) | 0.25 |
| | Xóa Server (Delete) | 0.25 |
| | Import Servers từ Excel | 0.5 |
| | Export Servers ra Excel | 0.5 |
| | Báo cáo định kỳ (Email) | 0.5 |
| | API Báo cáo chủ động | 0.5 |
| **Phi chức năng** | OpenAPI / Swagger | 0.5 |
| | Unit Test ≥ 90% coverage | 0.5 |
| | Chống SQL Injection (ORM) | 0.5 |
| | Error handling — mã lỗi và mô tả rõ ràng | 0.5 |
| | Log ra file + logrotate | 0.5 |
| | JWT Authentication + Scope Authorization | 0.5 |
| | Elasticsearch cho tính toán Uptime | 1.0 |
| | PostgreSQL Database | — |
| | Redis Cache | 0.5 |
| | Công nghệ khác (Kafka, Docker, TCP Simulator...) | 0.5 |
| | **Tổng** | **10.0** |

### 1.3. Các quyết định thiết kế quan trọng

| # | Vấn đề | Quyết định | Lý do |
|:-:|--------|------------|-------|
| 1 | Health-check Method | **TCP Simulator Pool** | Service Go (`tcp-simulator`) quản lý 10.000 TCP listeners, mở/đóng port động theo công thức toán học. Monitor Service dùng TCP Connect thật (`net.DialTimeout`). |
| 2 | API Gateway | **Tự viết bằng Gin** | Full control JWT Middleware, Rate Limiting, Reverse Proxy. Không phụ thuộc Kong/Traefik. |
| 3 | Email Provider | **Gmail SMTP + App Password** | Gửi email thật, cấu hình qua `.env`, đủ quota cho demo. |
| 4 | Repo Structure | **Monorepo** | Quản lý tập trung, shared libs dễ dàng, 1 `docker-compose.yml` cho toàn bộ. |
| 5 | Database Strategy | **Shared Instance, Separate Schemas** | 1 Postgres vật lý, mỗi service 1 schema riêng. Loose coupling + nhẹ tài nguyên. |
| 6 | Quy trình | **Design First** | Hoàn thiện DB Schema → OpenAPI Spec → Sequence Diagrams → Code. |

---

## 2. Kiến trúc hệ thống

### 2.1. Sơ đồ kiến trúc tổng quan

```
┌──────────────────────────────────────────────────────────────────┐
│                        CLIENT LAYER                              │
│              Postman / cURL / Frontend / Swagger UI              │
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
        │               │   │   │   │               │
        ▼               ▼   ▼   ▼   ▼               ▼
┌──────────┐  ┌──────────┐ ┌──────────┐ ┌──────────────┐
│  AUTH    │  │  SERVER  │ │ MONITOR  │ │   REPORT     │
│  :8081  │  │  :8082   │ │  :8083   │ │   :8084      │
│          │  │          │ │          │ │              │
│ Register │  │ CRUD     │ │ Health-  │ │ Uptime       │
│ Login    │  │ Filter   │ │ Check    │ │ Summary      │
│ JWT      │  │ Sort     │ │ 60s cron │ │ Email (SMTP) │
│ Refresh  │  │ Paginate │ │ Worker   │ │ Daily Cron   │
│ Logout   │  │ Cache    │ │ Pool     │ │ HTML Template│
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

### 2.2. Tại sao chọn kiến trúc Microservices?

Với hệ thống quản lý 10.000 server, tải trọng (workload) của các chức năng rất khác nhau:

- **Monitor Service**: Hoạt động liên tục, định kỳ mỗi phút quét 10.000 server — chịu tải CPU và Network I/O nặng nhất.
- **TCP Simulator Service**: Service hạ tầng hỗ trợ, quản lý 10.000 TCP listeners, mở/đóng port động.
- **Server Service (CRUD)**: Tải thấp, chỉ thỉnh thoảng có thao tác CRUD từ người dùng.
- **Report & File I/O**: Tải bất chợt (spiky workload), cần nhiều RAM để parse/generate file Excel hoặc gom dữ liệu lớn.

**Lợi ích:**

| Lợi ích | Mô tả |
|---------|-------|
| **Mở rộng độc lập** | Khi số server tăng lên 50.000, chỉ cần scale Monitor Service mà không cấp thêm tài nguyên cho Auth hay File I/O |
| **Cô lập lỗi** | Import Excel bị OOM chỉ crash File I/O Service, Monitor Service vẫn hoạt động bình thường |
| **Bảo mật** | Auth Service chứa logic nhạy cảm được bảo vệ chặt chẽ, các service khác không biết cấu trúc bảng Users |

### 2.3. Chiến lược Monorepo

Toàn bộ 7 services được đặt trong cùng một Git Repository (Monorepo):

- **Quản lý phiên bản thống nhất**: Không lo tình trạng service A dùng thư viện ver 1.0, service B dùng ver 2.0
- **Thư mục `shared/`**: Dễ dàng chia sẻ cấu trúc dữ liệu chung (Errors, Logger, Kafka Interface)
- **Deploy dễ dàng**: 1 file `docker-compose.yml` duy nhất dựng toàn bộ hệ thống

### 2.4. API Gateway — Tự viết bằng Gin

API Gateway đóng vai trò "nhân viên điều phối" đứng trước 5 microservices:

1. **Entry Point Duy Nhất**: Client chỉ gọi vào port 8080. Gateway định tuyến request tới đúng service.
2. **Xác thực tập trung**: Parse JWT, kiểm tra hợp lệ, lấy `user_id` và `scopes`, inject vào HTTP Header trước khi forward xuống backend.
3. **Chống Spam (Rate Limiting)**: Sử dụng Redis giới hạn 100 requests/phút/IP.
4. **Log tập trung**: Sinh `request_id` duy nhất và ghi log thời gian response.

**Middleware Chain:**
```
Request → Recovery → Logger → CORS → Rate Limiter (Redis) → JWT Auth → Scope Check → Reverse Proxy → Response
```

### 2.5. Ranh giới các Service (Service Boundaries)

| Service | Schema | Tables | Phụ thuộc |
|---------|--------|--------|-----------|
| **API Gateway** | — | — | Redis |
| **Auth Service** | `auth_schema` | `users`, `roles`, `role_permissions` | PostgreSQL, Redis |
| **Server Service** | `server_schema` | `servers` | PostgreSQL, Redis, Kafka |
| **Monitor Service** | `monitor_schema` | `health_check_configs` | PostgreSQL, Redis, Kafka, Elasticsearch, TCP Simulator |
| **Report Service** | `report_schema` | `report_jobs`, `daily_snapshots` | PostgreSQL, Elasticsearch, Kafka, SMTP |
| **File I/O Service** | `fileio_schema` | `import_jobs`, `import_job_details` | PostgreSQL (cross-schema), Kafka |
| **TCP Simulator** | — | — | Standalone |

---

## 3. Thiết kế cơ sở dữ liệu

### 3.1. Chiến lược: Shared Instance, Separate Schemas

Hệ thống sử dụng **PostgreSQL 17** với chiến lược **1 Database vật lý duy nhất** (`vcs_sms`), bên trong tạo **5 Schemas logic** riêng biệt:

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

1. **Đạt ranh giới Microservice**: Mỗi service có DB User riêng, chỉ có quyền trên schema của mình. Nếu lập trình viên vô tình viết query từ `report-service` cố gắng `DELETE FROM auth_schema.users`, PostgreSQL sẽ chặn ngay (Permission Denied).

2. **Nhẹ tài nguyên**: Chạy 5 Postgres databases riêng biệt tiêu tốn RAM và CPU overhead đáng kể. Shared Instance tối ưu tài nguyên khi chạy Docker trên máy local.

3. **Giải quyết Cross-Schema Read**: `monitor-service` cần danh sách 10.000 server IP để ping. Thay vì gọi HTTP API (chậm, phụ thuộc), ta `GRANT SELECT` trên `server_schema.servers` cho `monitor_user` — query thẳng với tốc độ nội bộ DB. Chỉ cấp quyền ĐỌC, quyền GHI vẫn thuộc độc quyền của `server-service`.

**Cross-schema GRANTs:**

| DB User | Quyền trên `server_schema.servers` |
|---------|-----------------------------------|
| `monitor_user` | SELECT |
| `fileio_user` | SELECT, INSERT |
| `report_user` | SELECT |

### 3.2. Thiết kế các bảng chính

#### Bảng `server_schema.servers`

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

#### Bảng `auth_schema.users`

| Column | Type | Constraint |
|--------|------|------------|
| `id` | UUID | PK |
| `username` | VARCHAR(100) | UNIQUE |
| `email` | VARCHAR(255) | UNIQUE |
| `password_hash` | VARCHAR(255) | bcrypt |
| `full_name` | VARCHAR(255) | |
| `role_id` | UUID | FK → roles |
| `is_active` | BOOLEAN | DEFAULT TRUE |
| `last_login_at` | TIMESTAMPTZ | |
| `deleted_at` | TIMESTAMPTZ | Soft delete |

#### Bảng `auth_schema.roles` & `role_permissions`

- **roles**: 3 roles mặc định — `admin`, `operator`, `viewer`
- **role_permissions**: Mapping role → scope (many-to-many), ví dụ `admin` → 10 scopes, `viewer` → 3 scopes

#### Bảng `fileio_schema.import_jobs` & `import_job_details`

- **import_jobs**: Theo dõi tiến trình import Excel (status: pending → processing → completed/failed)
- **import_job_details**: Chi tiết từng dòng import (success/failed + error_reason)

#### Bảng `report_schema.report_jobs` & `daily_snapshots`

- **report_jobs**: Theo dõi yêu cầu gửi báo cáo
- **daily_snapshots**: Lưu trữ kết quả tính toán uptime hàng ngày (tránh tính toán lại từ Elasticsearch)

### 3.3. Bổ trợ bằng Redis và Elasticsearch

| Công nghệ | Phiên bản | Mục đích |
|------------|:---------:|----------|
| **Redis** | 8 | Rate Limiting, Distributed Lock, JWT Blacklist, Caching API |
| **Elasticsearch** | 8.12 | Lưu trữ log health-check (time-series data). Mỗi phút 10.000 bản ghi, tính toán uptime aggregation với tốc độ mili-giây |

---

## 4. Kiến trúc Event-Driven (Kafka)

### 4.1. Tại sao cần Kafka thay vì HTTP API?

Hệ thống sử dụng **Apache Kafka 3.9 (KRaft)** làm Message Broker để liên kết các microservices theo mô hình bất đồng bộ (Asynchronous).

**Ví dụ luồng Import Excel**: Người dùng upload file 5000 servers → `fileio-service` chỉ bắn 1 event lên Kafka (vài ms) rồi phản hồi ngay: "Hệ thống đang xử lý". Sau đó `server-service` từ từ xử lý ngầm.

**Lợi ích Event-Driven:**

| Lợi ích | Mô tả |
|---------|-------|
| **Decoupling** | Producer không cần biết Consumer là ai. Nếu Consumer đang sập, message vẫn an toàn trong Kafka |
| **Buffer Spikes** | Khi 10 người cùng import 50.000 servers, Kafka lưu trữ như hàng đợi, service xử lý theo sức |
| **Eventual Consistency** | Trạng thái đồng bộ dần qua dòng sự kiện chung |

### 4.2. Kafka KRaft Mode

Sử dụng Kafka 3.9 ở chế độ KRaft — loại bỏ hoàn toàn ZooKeeper:
- Hệ thống nhẹ hơn, start/stop nhanh hơn
- Node Kafka tự đóng vai trò vừa broker vừa controller
- Cấu hình: `process.roles=broker,controller`

### 4.3. Các Topics & Events

| Topic | Partitions | Producer | Consumer | Mục đích |
|-------|:----------:|----------|----------|----------|
| `server.created` | 3 | Server Service, FileIO | Monitor Service | Tự động thêm vào danh sách health-check |
| `server.updated` | 3 | Server Service | Monitor Service | Cập nhật metadata server |
| `server.deleted` | 3 | Server Service | Monitor Service | Gỡ khỏi danh sách health-check |
| `server.status.changed` | 6 | Monitor Service | (future Alerting) | Sự kiện chuyển trạng thái On/Off |
| `server.health.batch` | 3 | Monitor Service | (future Analytics) | Batch kết quả health-check |
| `import.job.created` | 3 | FileIO Service | FileIO Service (self) | Xử lý import Excel bất đồng bộ |
| `report.daily.trigger` | 1 | (manual) | Report Service | Trigger báo cáo hàng ngày |

### 4.4. Go Kafka Client: segmentio/kafka-go

Lý do chọn `segmentio/kafka-go` thay vì `IBM/sarama`:

| Tiêu chí | segmentio/kafka-go | IBM/sarama |
|----------|:------------------:|:----------:|
| API simplicity | ⭐ `WriteMessages()`/`ReadMessage()` | Cần ConsumerGroupHandler |
| Context support | ✅ Native | ⚠️ Partial |
| Dependencies | Nhẹ, ít transitive deps | Nhiều (gokrb5, etc.) |
| Auto reconnect | ✅ Tự động | Manual config |

---

## 5. Thiết kế bảo mật — JWT & RBAC

### 5.1. JWT Authentication (Stateless)

Hệ thống sử dụng **JWT (JSON Web Token)** — Stateless:
- **Algorithm**: HS256 (HMAC-SHA256)
- **Access Token**: TTL 15 phút
- **Refresh Token**: TTL 7 ngày, rotation on use
- Payload chứa: `user_id`, `role`, `scopes`, `jti` (JWT ID)

**Refresh Token Flow:**
1. Đăng nhập thành công → Auth Service trả về `AccessToken` (15m) + `RefreshToken` (7d)
2. AccessToken hết hạn → Client tự động gửi RefreshToken lên `/auth/refresh`
3. Auth Service kiểm tra và cấp lại cặp Token mới

### 5.2. JWT Blacklist (Logout)

Vì JWT là Stateless, khi user đăng xuất, hệ thống đưa `jti` vào Redis Blacklist:
- Key: `auth:blacklist:<jti>` = 1
- TTL = thời gian sống còn lại của JWT
- API Gateway check Redis blacklist trước khi chấp nhận JWT (< 1ms)

### 5.3. RBAC — Roles & Scopes

| Role | Scopes |
|------|--------|
| **Admin** | `server:create`, `server:read`, `server:update`, `server:delete`, `server:import`, `server:export`, `monitor:view`, `report:view`, `report:send`, `user:manage` |
| **Operator** | `server:create`, `server:read`, `server:update`, `server:import`, `server:export`, `monitor:view`, `report:view`, `report:send` |
| **Viewer** | `server:read`, `server:export`, `report:view` |

**Scope Validation tại API Gateway:**
- Mỗi route khai báo cần scope gì (VD: `POST /api/v1/servers` → `server:create`)
- Gateway đọc JWT, kiểm tra mảng scopes. Thiếu scope → HTTP 403 Forbidden
- Backend không bao giờ nhận request trái phép

### 5.4. Security Context Injection

Sau khi Gateway xác minh JWT, nó đính kèm thông tin vào HTTP Headers nội bộ:
- `X-User-ID: <uuid>`
- `X-User-Scopes: server:read,report:view,...`

Backend service chỉ đọc Header tĩnh để biết user hiện tại — pattern chuẩn Microservice.

### 5.5. Các biện pháp bảo mật khác

| Biện pháp | Chi tiết |
|-----------|----------|
| **SQL Injection** | GORM parameterized queries — không raw SQL |
| **Brute Force** | Redis-based login attempt counter (khóa 15 phút sau 5 lần thất bại) |
| **Password** | bcrypt cost factor 12 |
| **Rate Limiting** | 100 requests/phút per IP tại Gateway |
| **CORS** | Configurable allowed origins qua `.env` |

---

## 6. Thiết kế API

### 6.1. Tổng hợp Endpoints (18 endpoints)

| # | Method | Path | Service | Scope | Mô tả |
|:-:|--------|------|---------|-------|-------|
| 1 | POST | `/api/v1/auth/register` | Auth | Public | Đăng ký (mặc định viewer) |
| 2 | POST | `/api/v1/auth/login` | Auth | Public | Đăng nhập |
| 3 | POST | `/api/v1/auth/refresh` | Auth | Public | Làm mới token |
| 4 | POST | `/api/v1/auth/logout` | Auth | Authenticated | Đăng xuất |
| 5 | GET | `/api/v1/auth/profile` | Auth | Authenticated | Thông tin cá nhân |
| 6 | GET | `/api/v1/auth/users` | Auth | `user:manage` | Danh sách người dùng |
| 7 | PUT | `/api/v1/auth/users/{user_id}/role` | Auth | `user:manage` | Đổi role người dùng |
| 8 | POST | `/api/v1/servers` | Server | `server:create` | Tạo server |
| 9 | GET | `/api/v1/servers` | Server | `server:read` | Danh sách server (filter, sort, paging) |
| 10 | GET | `/api/v1/servers/{server_id}` | Server | `server:read` | Chi tiết server |
| 11 | PUT | `/api/v1/servers/{server_id}` | Server | `server:update` | Cập nhật server |
| 12 | DELETE | `/api/v1/servers/{server_id}` | Server | `server:delete` | Xóa server (soft delete) |
| 13 | POST | `/api/v1/servers/import` | FileIO | `server:import` | Import Excel |
| 14 | GET | `/api/v1/servers/import/{job_id}` | FileIO | `server:import` | Trạng thái import |
| 15 | POST | `/api/v1/servers/export` | FileIO | `server:export` | Export Excel |
| 16 | GET | `/api/v1/monitor/status` | Monitor | `monitor:view` | Monitor service status |
| 17 | GET | `/api/v1/reports/summary` | Report | `report:view` | Uptime summary |
| 18 | POST | `/api/v1/reports` | Report | `report:send` | Gửi báo cáo email |

### 6.2. Authentication Flow

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

### 6.3. Error Response Format

Tất cả API đều trả về format thống nhất với mã lỗi và mô tả rõ ràng:

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

**Error Codes định nghĩa**: 401 (Unauthorized), 403 (Forbidden), 404 (Not Found), 409 (Conflict), 422 (Validation), 429 (Rate Limit), 500 (Internal), v.v. — tổng cộng **17 mã lỗi**.

### 6.4. OpenAPI / Swagger

Toàn bộ API được mô tả bằng **OpenAPI 3.0.3** specification (`api-spec.yaml`). Swagger UI được tích hợp sẵn tại: `http://localhost:8080/swagger/index.html`

---

## 7. Các luồng nghiệp vụ chính

### 7.1. Health Check 10.000 Server (2.0 điểm)

Đây là trái tim của hệ thống — chức năng quan trọng nhất và khó nhất.

#### 7.1.1. TCP Simulator — Giả lập 10.000 Server Thật

**Bài toán**: Lấy đâu ra 10.000 IP thật để test? Nếu toàn IP ảo → 100% Offline → vô nghĩa.

**Giải pháp**: TCP Simulator Service — 1 chương trình Go duy nhất, quản lý 10.000 TCP listeners:
- Mỗi server được gán 1 port riêng: `SRV-00001 → port 9001`, `SRV-10000 → port 19000`
- **Math Engine**: Mỗi 30 giây tính toán On/Off dựa trên:
  - `uptime_rate` (VD: 0.95 = 95% khả năng ON)
  - Biến thiên hàm Sin theo giờ (tạo pattern trồi sụt realistic ban ngày/ban đêm)
  - Offset riêng cho mỗi server (để không phải tất cả cùng ON/OFF 1 lúc)
- Server nên **ON** → mở TCP port (accept rồi đóng ngay)
- Server nên **OFF** → đóng TCP port (connection refused)

**Ưu điểm**: Monitor Service chạy **y hệt production** — code TCPChecker không hề biết đây là server giả. Chỉ thêm 1 container (~100-256MB RAM).

```
                                    TCP Simulator Service
                                   ┌────────────────────┐
                /-- Worker 1 --\   │  Port 9001: OPEN ✅ │
10.000 Servers --- Worker 2 --- ──▶│  Port 9002: CLOSED❌│
(Channel jobs) \-- Worker N --/   │  Port 9003: OPEN ✅ │
                                   │  ...                │
   Monitor Service                 │  Math Engine: mỗi  │
   (100 Workers, TCP Connect)      │  30s tính On/Off   │
                                   └────────────────────┘
```

#### 7.1.2. Worker Pool Pattern

Thay vì mở 10.000 luồng, hệ thống tạo **100 goroutines** cố định (Worker Pool):

- Băng chuyền (Channel) `jobs` chứa 10.000 server
- 100 công nhân liên tục đứng chực, ai rảnh lấy 1 server ra ping
- Tại bất kỳ thời điểm nào, chỉ có tối đa 100 kết nối TCP đang mở

**Timeout TCP**: 5 giây. Trường hợp xấu nhất (100% timeout):
- 1 Worker: 60s / 5s = 12 servers/phút
- 100 Workers: 1.200 servers/phút
- Thực tế kết nối thành công/lỗi trả về trong < 100ms → dư sức xử lý 10.000 server

#### 7.1.3. Distributed Lock với Redis

Để tránh nhiều instances của `monitor-service` cùng quét 10.000 server:

1. Instance A tới Redis tạo key `health-check-lock` (TTL 90 giây) → được phép chạy
2. Instance B đến → key đã tồn tại → bỏ qua vòng quét
3. Nếu Instance A crash → sau 90 giây khóa tự hết hạn → Instance B tiếp quản

#### 7.1.4. Quy trình Health Check mỗi phút

1. **Chuẩn bị**: Cron Scheduler reo chuông → Xin Distributed Lock từ Redis → Lấy danh sách 10.000 server từ PostgreSQL
2. **Ping**: 100 Workers TCP Connect song song tới `tcp-simulator:9001-19000`
3. **So sánh**: So 10.000 kết quả mới với kết quả phút trước (Redis) → Tìm server thay đổi trạng thái
4. **Kafka Events**: Phát sóng `server.status.changed` cho các server thay đổi
5. **PostgreSQL**: Batch Update chỉ các server thay đổi (VD: 10/10.000 server)
6. **Elasticsearch**: Bulk Index toàn bộ 10.000 bản ghi log (vài chục ms)
7. **Redis**: Cập nhật trạng thái mới nhất cho phút sau so sánh

#### 7.1.5. Elasticsearch Index

```
Index: server-status-logs
Mapping:
  - server_id    (keyword)
  - server_name  (text)
  - status       (keyword: on/off)
  - latency_ms   (integer)
  - checked_at   (date)
  - error_msg    (text)
```

### 7.2. Quản lý Server (CRUD)

Trong kiến trúc Event-Driven, mọi thao tác thay đổi dữ liệu đều tạo ra chuỗi phản ứng dây chuyền:

#### Create Server
1. Admin nhập thông tin → Gateway kiểm tra scope `server:create` → Forward tới `server-service`
2. Validate (IP format, trùng lặp) → Lưu PostgreSQL (status mặc định `off`)
3. Publish event `server.created` lên Kafka
4. `monitor-service` nhận event → cập nhật danh sách health-check

#### Update Server
1. Cập nhật PostgreSQL → Xóa Cache Redis → Bắn event `server.updated` lên Kafka

#### Delete Server (Soft Delete)
1. Cập nhật cột `deleted_at` (không xóa thật) → Bắn event `server.deleted`
2. `monitor-service` gạch tên server khỏi danh sách health-check

#### Read / Filter
1. Client gửi bộ lọc: status, OS, location, sort_by, sort_order, page, page_size
2. `server-service` dịch thành SQL, tận dụng Index
3. Kết quả cache vào Redis — lần sau trả về ngay không cần query DB

### 7.3. Import & Export Excel

#### Import Excel (Bất đồng bộ qua Kafka)
1. Upload `.xlsx` → `fileio-service` tạo import_job (PENDING) → Trả `job_id` ngay lập tức
2. Đẩy event `import.job.created` lên Kafka
3. Consumer nền của `fileio-service` parse Excel → validate → ghi các dòng hợp lệ vào `server_schema.servers`
4. Ghi kết quả từng dòng vào `import_job_details`, publish `server.created` cho các server thành công
5. Cập nhật tiến độ → Client poll `GET /api/v1/servers/import/{job_id}` để check

**Output**: Số lượng & danh sách server import thành công/thất bại + lý do lỗi.

#### Export Excel (Đồng bộ)
1. Client chọn bộ lọc → `fileio-service` query DB → Tạo file Excel trong RAM bằng `excelize`
2. Stream file byte về trình duyệt → Download popup

### 7.4. Báo cáo & Email

#### Báo cáo định kỳ (Daily Cron — 8:00 AM)
1. Cronjob kích hoạt → Query Elasticsearch tính uptime ngày hôm qua
2. Lưu Snapshot vào PostgreSQL (tránh tính toán lại)
3. Tạo HTML Email template (gradient header, stat cards, bảng Top 10 server xấu nhất)
4. Gửi qua Gmail SMTP (App Password)

**Công thức Uptime:**
```
Uptime(server) = (Số lần check status = "on") / (Tổng số lần check thực tế) × 100%
```

#### Báo cáo chủ động (On-Demand)
1. Client chọn khoảng ngày + nhập email → `POST /api/v1/reports`
2. Background Worker tính toán → Tạo HTML → Gửi email

---

## 8. Chiến lược Caching (Redis)

| Key Pattern | TTL | Mục đích | Service |
|-------------|:---:|---------|---------| 
| `servers:list:{hash}` | 5 min | Cache danh sách server phân trang | Server |
| `server:detail:{id}` | 10 min | Cache chi tiết 1 server | Server |
| `report:summary:{start}:{end}` | 1 hour | Cache uptime summary | Report |
| `rate_limit:{ip}` | 1 min | Sliding window rate limiter | Gateway |
| `token:blacklist:{jti}` | 15 min | Logout token blacklist | Auth |
| `health:lock` | 55 sec | Distributed scheduler lock | Monitor |
| `health:status:{server_id}` | 65 sec | Latest health status | Monitor |

**Cache Invalidation**: Write-through khi update/delete. Bulk pattern-delete (`SCAN` + `DEL`) khi mass import.

---

## 9. Chiến lược Testing

### 9.1. Hạ tầng Test

| Component | Tool | Pattern |
|-----------|------|---------|
| Database | `go-sqlmock` + GORM | ExpectQuery/ExpectExec with regex |
| HTTP | `httptest.NewRecorder` | Table-driven tests |
| Mocks | Function-callback structs | Custom mock structs implementing interfaces |
| Kafka | `fakeProducer` / `fakeConsumer` | In-memory channel-based |
| Redis | `fakeCache` / `miniredis` | In-memory implementation |
| Elasticsearch | Mock `http.RoundTripper` | Intercept HTTP calls |

### 9.2. Coverage theo Service (Core Business Packages)

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

## 10. Logging & Observability

### 10.1. Structured JSON Logging

Hệ thống sử dụng **zerolog** (v1.35) — thư viện logging zero-allocation cho Go:
- Format: **JSON** structured (dễ parse bằng các công cụ log management)
- Mỗi log entry gồm: `timestamp`, `level`, `request_id`, `service`, `message`, `caller`

### 10.2. Log Rotation (Logrotate)

Sử dụng **lumberjack** (v2.2) để tự động xoay log:
- Cấu hình max file size, max backups, max age
- Tự động tạo file log mới khi file hiện tại đạt giới hạn
- Log files được mount volumes trong Docker

---

## 11. Triển khai & Vận hành

### 11.1. Docker Compose — Full Stack

```bash
# 1. Cấu hình
cp .env.example .env
# Sửa JWT_SECRET, SMTP_PASSWORD

# 2. Khởi động toàn bộ (11 runtime containers + kafka-init one-shot)
docker compose up -d --build

# 3. Kiểm tra
docker compose ps

# 4. Seed 10.000 servers
make seed

# 5. Verify
curl http://localhost:8080/health
```

### 11.2. Container Inventory (12 containers)

| Container | Image | Port | Mô tả |
|-----------|-------|:----:|--------|
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
| `vcs-sms-tcp-simulator` | custom (Go) | 9001-19000 | TCP Simulator |
| `kafka-init` | custom | — | One-shot topic creator |

### 11.3. Dockerfiles — Multi-stage Build

Tất cả 7 services sử dụng multi-stage build:
- **Build stage**: `golang:1.24-alpine` + GOTOOLCHAIN=auto
- **Run stage**: `alpine:3.19` (~15MB)
- **Shared module**: Copy `shared/` → replace directive trong go.mod

---

## 12. Technology Stack

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

## 13. Hướng dẫn sử dụng

### 13.1. Yêu cầu hệ thống

- **Docker Desktop** hoặc Docker Engine có Docker Compose plugin v2+
- **Go** 1.24+ (chỉ cần nếu dev local)
- **RAM** tối thiểu 4GB (khuyến nghị 8GB cho Elasticsearch)
- **Disk** ~5GB cho images + volumes

### 13.2. Cài đặt & Khởi chạy

#### Bước 1: Clone repository
```bash
git clone https://github.com/<username>/vcs-sms.git
cd vcs-sms/server-management-system
```

#### Bước 2: Cấu hình môi trường
```bash
cp .env.example .env
```

Sửa các biến quan trọng:
```env
JWT_SECRET=your-random-64-char-string-here
SMTP_USERNAME=your-email@gmail.com
SMTP_PASSWORD=your-16-char-app-password
SMTP_ADMIN_EMAIL=admin@yourcompany.com
```

#### Bước 3: Khởi động toàn bộ hệ thống
```bash
docker compose up -d --build
# Đợi 30-60 giây cho tất cả services khởi động
docker compose ps
```

#### Bước 4: Seed 10.000 servers
```bash
make seed
```

#### Bước 5: Đăng nhập
```bash
# Tài khoản admin mặc định: admin / Admin@123456
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "Admin@123456"}'
```

### 13.3. Sử dụng các tính năng

#### A. Authentication

**Đăng ký (Register):**
```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username": "operator01", "email": "operator@vcs.com", "password": "Operator@123", "full_name": "John Operator"}'
```

**Đăng nhập (Login):**
```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "Admin@123456"}'
```

**Làm mới Token (Refresh):**
```bash
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token": "<refresh_token>"}'
```

**Đăng xuất (Logout):**
```bash
curl -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer <access_token>"
```

#### B. Quản lý Server (CRUD)

**Tạo Server:**
```bash
curl -X POST http://localhost:8080/api/v1/servers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"server_id": "SRV-WEB-001", "server_name": "web-server-01", "ipv4": "10.0.1.100", "os": "Ubuntu 22.04", "cpu_cores": 8, "ram_gb": 16, "disk_gb": 500, "location": "DC-HN"}'
```

**Xem danh sách (Filter, Sort, Pagination):**
```bash
# Lọc server ON, Ubuntu, sắp xếp theo tên
curl "http://localhost:8080/api/v1/servers?status=on&os=Ubuntu&sort_by=server_name&sort_order=asc&page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

| Tham số | Mô tả | Ví dụ |
|---------|-------|-------|
| `status` | on / off | `status=on` |
| `server_name` | Tìm theo tên | `server_name=web` |
| `ipv4` | Tìm theo IP | `ipv4=10.0.1` |
| `os` | Lọc OS | `os=Ubuntu` |
| `location` | Lọc vị trí | `location=DC-HN` |
| `sort_by` | Sắp xếp theo cột | `sort_by=created_at` |
| `sort_order` | asc / desc | `sort_order=desc` |
| `page` | Số trang | `page=1` |
| `page_size` | Số lượng/trang (max 100) | `page_size=20` |

**Cập nhật Server:**
```bash
curl -X PUT http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"server_name": "web-server-01-updated", "cpu_cores": 16, "ram_gb": 32}'
```
> ⚠️ `server_id` không thể thay đổi sau khi tạo.

**Xóa Server (Soft Delete):**
```bash
curl -X DELETE http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN"
```

#### C. Import / Export Excel

**Import:**
```bash
# Upload file .xlsx
curl -X POST http://localhost:8080/api/v1/servers/import \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@servers.xlsx"

# Kiểm tra tiến độ
curl http://localhost:8080/api/v1/servers/import/<job_id> \
  -H "Authorization: Bearer $TOKEN"
```

Cột bắt buộc trong file Excel: `server_id`, `server_name`, `ipv4`. Giới hạn: file ≤ 10MB, định dạng `.xlsx`.

**Export:**
```bash
curl -X POST http://localhost:8080/api/v1/servers/export \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status": "on", "os": "Ubuntu", "sort_by": "server_name"}' \
  --output servers.xlsx
```

#### D. Monitoring & Health Check

```bash
# Xem trạng thái Monitor Service
curl http://localhost:8080/api/v1/monitor/status \
  -H "Authorization: Bearer $TOKEN"

# Kiểm tra dữ liệu Elasticsearch
curl "http://localhost:9200/server-status-logs/_count"
```

#### E. Báo cáo & Email

**Xem Uptime Summary:**
```bash
curl "http://localhost:8080/api/v1/reports/summary?start_date=2026-06-01&end_date=2026-06-15" \
  -H "Authorization: Bearer $TOKEN"
```

**Gửi báo cáo qua Email:**
```bash
curl -X POST http://localhost:8080/api/v1/reports \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"start_date": "2026-06-01", "end_date": "2026-06-15", "email": "manager@company.com"}'
```

#### F. Swagger UI

Mở trình duyệt: **http://localhost:8080/swagger/index.html**

1. Nhấn **Authorize** 🔒 → Nhập `Bearer <access_token>` → **Authorize**
2. Chọn endpoint → **Try it out** → **Execute**

### 13.4. Xử lý lỗi thường gặp

| Lỗi | Nguyên nhân | Cách khắc phục |
|-----|-------------|----------------|
| `401 Unauthorized` | Token hết hạn hoặc sai | Đăng nhập lại |
| `403 Forbidden` | Không đủ quyền (scope) | Kiểm tra role |
| `429 Too Many Requests` | Vượt rate limit (100 req/phút) | Đợi 1 phút |
| `422 Validation Failed` | Dữ liệu không hợp lệ | Xem `errors[]` trong response |
| `409 Conflict` | Trùng `server_id` / `server_name` | Dùng giá trị khác |
| Service không start | Port conflict / thiếu .env | `docker compose logs <service>` |
| Email không gửi được | SMTP sai / App Password hết hạn | Kiểm tra .env SMTP config |
| ES không có data | Monitor / TCP Simulator chưa sẵn sàng | `docker compose logs monitor-service` |

---

## 14. Tổng kết & Đánh giá

### 14.1. Các tính năng đã hoàn thành

| Tính năng | Trạng thái | Ghi chú |
|-----------|:----------:|---------|
| ✅ Health Check 10K servers (TCP) | Hoàn thành | Worker Pool 100 goroutines + TCP Simulator |
| ✅ Server CRUD | Hoàn thành | Filter, sort, pagination, caching |
| ✅ Import Excel (async) | Hoàn thành | Kafka + background job + progress tracking |
| ✅ Export Excel | Hoàn thành | Streaming download + filter support |
| ✅ Báo cáo định kỳ (Daily Email) | Hoàn thành | Cron 8:00 AM + HTML template |
| ✅ Báo cáo chủ động (On-Demand) | Hoàn thành | API + email delivery |
| ✅ JWT Authentication | Hoàn thành | HS256 + Refresh Token + Blacklist |
| ✅ RBAC (3 roles, 10 scopes) | Hoàn thành | Admin, Operator, Viewer |
| ✅ OpenAPI / Swagger UI | Hoàn thành | 18 endpoints documented |
| ✅ Unit Test ≥ 90% | Hoàn thành | Core business packages (fileio, report, monitor) |
| ✅ SQL Injection Protection | Hoàn thành | GORM parameterized queries |
| ✅ Error Handling | Hoàn thành | 17 error codes + structured response |
| ✅ Logging + Logrotate | Hoàn thành | zerolog (JSON) + lumberjack |
| ✅ Elasticsearch Uptime | Hoàn thành | Bulk indexing + aggregation queries |
| ✅ Redis Cache | Hoàn thành | Cache-aside + rate limiting + distributed lock |
| ✅ Kafka Event-Driven | Hoàn thành | 7 topics, async processing |
| ✅ Docker Compose | Hoàn thành | 1 lệnh → 11 containers |

### 14.2. Kiến trúc nổi bật

- **Microservices + Monorepo**: 7 services trong 1 repository, chia sẻ shared libraries
- **Event-Driven**: Kafka KRaft 3.9 với 7 topics, xử lý bất đồng bộ
- **TCP Simulator Pool**: Giải pháp sáng tạo giả lập 10.000 server thật bằng 1 container
- **Worker Pool**: 100 goroutines xử lý concurrent health-check với throttling
- **Distributed Lock**: Redis-based lock tránh overlap khi multiple instances
- **Separate Schemas**: Isolation dữ liệu giữa các service ở cấp độ database
- **Cache-aside Strategy**: Redis cache với TTL và invalidation cho mọi read-heavy endpoints

### 14.3. Cấu trúc dự án (Monorepo)

```
server-management-system/
├── api-gateway/           # API Gateway (Gin, JWT, Rate Limit, Proxy)
├── auth-service/          # Authentication Service
├── server-service/        # Server CRUD Service
├── monitor-service/       # Health-check Monitor (Worker Pool, ES, Kafka)
├── report-service/        # Report & Email Service
├── fileio-service/        # Excel Import/Export Service
├── tcp-simulator/         # TCP Simulator (10K dynamic listeners)
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

### 14.4. Tài liệu liên quan

| Tài liệu | Đường dẫn |
|----------|-----------|
| Thiết kế hệ thống chi tiết | `docs/architecture.md` |
| Hướng dẫn sử dụng | `docs/user-guide.md` |
| OpenAPI Specification | `docs/api-spec.yaml` |
| Database Strategy | `docs/02-database-strategy.md` |
| Event-Driven Kafka | `docs/03-event-driven-kafka.md` |
| Worker Pool Design | `docs/04-high-concurrency-worker-pool.md` |
| Security JWT RBAC | `docs/05-security-jwt-rbac.md` |
| Flow: Server CRUD | `docs/06-flow-server-crud.md` |
| Flow: Health Check | `docs/07-flow-health-check.md` |
| Flow: Import/Export | `docs/08-flow-import-export.md` |
| Flow: Reporting & Email | `docs/09-flow-reporting-email.md` |

---

> **VCS Server Management System © 2026** — Chương trình đào tạo VCS Passport
