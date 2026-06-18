# 🖥️ VCS Server Management System (VCS-SMS)

Hệ thống quản lý tập trung **10.000 server** theo kiến trúc Microservice + Event-Driven.

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-336791?logo=postgresql)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-8-DC382D?logo=redis)](https://redis.io/)
[![Elasticsearch](https://img.shields.io/badge/Elasticsearch-8.12-005571?logo=elasticsearch)](https://www.elastic.co/)
[![Kafka](https://img.shields.io/badge/Kafka-3.9-231F20?logo=apachekafka)](https://kafka.apache.org/)
[![Docker](https://img.shields.io/badge/Docker-29+-2496ED?logo=docker)](https://www.docker.com/)

---

## 📋 Tính năng

| Tính năng | Mô tả | Điểm |
|-----------|-------|:----:|
| 🔍 **Health Check** | TCP ping 10.000 server mỗi 60 giây, ghi Elasticsearch | 2.0 |
| 📝 **Server CRUD** | Tạo/Xem/Sửa/Xóa server với filter, sort, pagination | 1.0 |
| 📥 **Import Excel** | Upload .xlsx import hàng loạt bất đồng bộ qua Kafka | 0.5 |
| 📤 **Export Excel** | Export danh sách server ra file .xlsx | 0.5 |
| 📊 **Báo cáo** | Uptime report + Email HTML (Gmail SMTP) + Daily Cron | 1.0 |
| 🔐 **Auth & RBAC** | JWT HS256, 3 roles (Admin/Operator/Viewer), 10 scopes | 0.5 |
| 🛡️ **Bảo mật** | Chống SQL Injection (GORM), Rate Limiting (Redis), bcrypt | 0.5 |
| 📋 **OpenAPI** | Swagger UI đầy đủ 18 endpoints | 0.5 |
| 🧪 **Unit Test** | Test coverage ≥ 90% core business packages | 0.5 |
| 📝 **Logging** | Structured JSON logs + logrotate (lumberjack) | 0.5 |
| 🚀 **Deploy** | Docker Compose 1 lệnh → 11 containers | — |

---

## 🏗️ Kiến trúc

```
Client → API Gateway (:8080)
           ├── Auth Service (:8081)      — JWT, Users, Roles
           ├── Server Service (:8082)    — CRUD, Cache, Events
           ├── Monitor Service (:8083)   — Health Check, ES, Worker Pool
           ├── Report Service (:8084)    — Uptime, Email, Cron
           ├── File I/O Service (:8085)  — Excel Import/Export
           └── TCP Simulator (:9001-19000) — 10K Fake Servers

Infrastructure: PostgreSQL 17 | Redis 8 | Elasticsearch 8 | Kafka 3.9
```

📖 [Tài liệu thiết kế chi tiết](docs/architecture.md)

---

## 🚀 Quick Start

### Yêu cầu

- **Docker** 29+ & **Docker Compose** v5+
- **Go** 1.24+ (để dev local)
- **Make** (tùy chọn)

### 1. Clone & cấu hình

```bash
git clone https://github.com/<username>/vcs-sms.git
cd vcs-sms/server-management-system
cp .env.example .env
# Sửa các biến trong .env:
#   JWT_SECRET          — chuỗi ngẫu nhiên ≥ 64 ký tự
#   SMTP_USERNAME       — Gmail address
#   SMTP_PASSWORD       — Gmail App Password (16 ký tự)
#   SMTP_ADMIN_EMAIL    — Email nhận báo cáo
```

### 2. Khởi động toàn bộ hệ thống

```bash
# Build & start tất cả services
docker compose up -d

# Kiểm tra trạng thái
docker compose ps

# Xem logs
docker compose logs -f api-gateway
```

### 3. Seed 10.000 servers (sau khi DB sẵn sàng)

```bash
make seed
# Hoặc:
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms \
  -f /seed/seed_10k_servers.sql
```

### 4. Verify

```bash
# Health check
curl http://localhost:8080/health

# Đăng nhập với tài khoản admin mặc định (đã được seed sẵn)
# Username: admin | Password: Admin@123456
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"Admin@123456"}'

# Swagger UI
open http://localhost:8080/swagger/index.html
```

---

## 📡 API Endpoints (18)

| Method | Path | Scope | Mô tả |
|--------|------|-------|-------|
| POST | `/api/v1/auth/register` | Public | Đăng ký (mặc định viewer) |
| POST | `/api/v1/auth/login` | Public | Đăng nhập |
| POST | `/api/v1/auth/refresh` | Public | Làm mới token |
| POST | `/api/v1/auth/logout` | Auth | Đăng xuất |
| GET | `/api/v1/auth/profile` | Auth | Thông tin cá nhân |
| GET | `/api/v1/auth/users` | `user:manage` | Danh sách người dùng |
| PUT | `/api/v1/auth/users/{user_id}/role` | `user:manage` | Đổi role người dùng |
| POST | `/api/v1/servers` | `server:create` | Tạo server |
| GET | `/api/v1/servers` | `server:read` | Danh sách server |
| GET | `/api/v1/servers/{server_id}` | `server:read` | Chi tiết server |
| PUT | `/api/v1/servers/{server_id}` | `server:update` | Cập nhật server |
| DELETE | `/api/v1/servers/{server_id}` | `server:delete` | Xóa server |
| POST | `/api/v1/servers/import` | `server:import` | Import Excel |
| GET | `/api/v1/servers/import/{job_id}` | `server:import` | Trạng thái import |
| POST | `/api/v1/servers/export` | `server:export` | Export Excel |
| GET | `/api/v1/monitor/status` | `monitor:view` | Monitor service status |
| GET | `/api/v1/reports/summary` | `report:view` | Uptime summary |
| POST | `/api/v1/reports` | `report:send` | Gửi báo cáo email |

📖 [OpenAPI Spec](docs/api-spec.yaml)

---

## 🧪 Development

### Build & Test

```bash
# Build tất cả services
make build

# Chạy tests với coverage
make test

# Chạy infrastructure-only (dev mode)
make dev-up

# Generate HTML coverage reports
make coverage
```

### Project Structure

```
├── api-gateway/         # API Gateway (Gin, JWT, Rate Limit, Proxy)
├── auth-service/        # Auth Service (Register, Login, JWT, RBAC)
├── server-service/      # Server CRUD Service
├── monitor-service/     # Health-check Monitor (Worker Pool, ES, Kafka)
├── report-service/      # Report Service (ES Agg, SMTP, Cron)
├── fileio-service/      # File I/O Service (Excel Import/Export)
├── tcp-simulator/       # TCP Simulator (10K dynamic listeners)
├── shared/              # Shared libraries (errors, kafka, logger, middleware, jwt, response)
├── deployments/         # Docker configs, SQL init, seed scripts
├── migrations/          # SQL migrations (5 schemas, 20 files)
├── docs/                # Tài liệu thiết kế + API spec
├── docker-compose.yml   # Full stack deployment
└── Makefile             # Build, test, seed, deploy
```

---

## 📚 Tài liệu

| Tài liệu | Mô tả |
|----------|-------|
| [System Design](docs/architecture.md) | Kiến trúc tổng quan, database, caching, security, deployment |
| [User Guide](docs/user-guide.md) | Hướng dẫn sử dụng chi tiết kèm curl examples |
| [API Specification](docs/api-spec.yaml) | OpenAPI 3.0.3 — 18 endpoints |
| [Database Strategy](docs/02-database-strategy.md) | 5 schemas, cross-schema access |
| [Event-Driven Kafka](docs/03-event-driven-kafka.md) | 7 topics, async flows |
| [Worker Pool Design](docs/04-high-concurrency-worker-pool.md) | 100 goroutines, TCP health check |
| [Security JWT RBAC](docs/05-security-jwt-rbac.md) | Authentication & Authorization |

---

## 🔑 License

MIT — VCS Server Management System © 2026
