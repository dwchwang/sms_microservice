/# Phase 5: Polish & Documentation

> **Mục tiêu:** Đảm bảo chất lượng, seed data, Docker deploy, tài liệu hoàn chỉnh.
> **Thời gian:** Tuần 6
> **Prerequisite:** Phase 0-4 hoàn tất (tất cả services chạy OK)
> **Điểm đạt được:** Consolidate tất cả điểm phi chức năng còn lại

---

## Checklist tổng quan Phase 5

- [ ] **5.1** Unit Test — Tổng hợp & đạt ≥ 90% coverage
- [ ] **5.2** Verify Seed 10.000 servers (từ Phase 0)
- [ ] **5.3** Dockerfile cho tất cả services
- [ ] **5.4** Docker Compose — chạy full stack
- [ ] **5.5** Swagger UI — merge & verify
- [ ] **5.6** Tài liệu thiết kế hệ thống (architecture.md)
- [ ] **5.7** Hướng dẫn sử dụng (user-guide.md)
- [ ] **5.8** README.md
- [ ] **5.9** Final verification checklist
- [ ] **5.10** Push to GitHub

---

## 5.1. Unit Test — Đạt ≥ 90% Coverage

### 5.1.1. Chạy test coverage cho tất cả services

```bash
# Script chạy toàn bộ
#!/bin/bash
echo "=== Running all tests with coverage ==="

services=("shared" "auth-service" "server-service" "monitor-service" "report-service" "fileio-service")

for svc in "${services[@]}"; do
    echo ""
    echo ">>> Testing $svc"
    cd $svc
    go test ./... -coverprofile=coverage.out -covermode=atomic -v 2>&1 | tail -20
    COVERAGE=$(go tool cover -func=coverage.out | tail -1 | awk '{print $3}')
    echo ">>> $svc coverage: $COVERAGE"
    
    if [[ "${COVERAGE%\%}" < "90.0" ]]; then
        echo "⚠️  WARNING: $svc coverage below 90%!"
    else
        echo "✅ $svc coverage OK"
    fi
    
    cd ..
done
```

### 5.1.2. Nếu coverage < 90%, bổ sung tests cho:

**Ưu tiên cao (dễ thiếu):**
- Error paths (DB errors, Redis errors, Kafka errors)
- Edge cases (empty input, nil values, max values)
- Middleware tests (auth, rate limiter, CORS)
- Config loading (invalid config, missing env)
- Validation logic (boundary values)

**Ưu tiên thấp (tốn effort):**
- Integration tests (chạy với testcontainers)
- Concurrency tests (race conditions)

### 5.1.3. Generate HTML coverage reports

```bash
for svc in auth-service server-service monitor-service report-service fileio-service; do
    cd $svc
    go tool cover -html=coverage.out -o coverage.html
    cd ..
done
# Open coverage.html in browser → xem visual coverage
```

---

## 5.2. Verify Seed 10.000 Servers Data

> Seed data đã được tạo ở Phase 0 (section 0.9) thông qua script `deployments/docker/postgres/seed_10k_servers.sql`.
> Tất cả servers có `ipv4 = 'tcp-simulator'` và `tcp_port = 9000 + index`.

### 5.2.1. Verify seed data đã đủ

```bash
# Kiểm tra số lượng servers
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms \
  -c "SELECT COUNT(*) FROM server_schema.servers"
# Expected: 10000

# Kiểm tra health_check_configs
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms \
  -c "SELECT COUNT(*) FROM monitor_schema.health_check_configs"
# Expected: 10000

# Kiểm tra mẫu dữ liệu
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms \
  -c "SELECT server_id, ipv4, status FROM server_schema.servers LIMIT 5"
# Expected: ipv4 = 'tcp-simulator' cho tất cả

# Kiểm tra port mapping
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms \
  -c "SELECT server_id, tcp_port, uptime_rate FROM monitor_schema.health_check_configs LIMIT 5"
# Expected: SRV-00001 → port 9001, SRV-00002 → port 9002, etc.
```

---

## 5.3. Dockerfile cho tất cả Services

### Template Dockerfile (dùng chung, chỉ thay path)

**File:** `{service-name}/Dockerfile`

```dockerfile
# ── Build stage ──
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy shared module first (for caching)
COPY shared/ ./shared/

# Copy service module
COPY {service-name}/ ./{service-name}/

# Build
WORKDIR /app/{service-name}
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/service ./cmd/main.go

# ── Run stage ──
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/bin/service .

# Copy email templates (chỉ cho report-service)
# COPY --from=builder /app/report-service/internal/email/templates ./templates

# Create log directory
RUN mkdir -p /var/log/vcs-sms

EXPOSE {port}

CMD ["./service"]
```

### Danh sách Dockerfiles cần tạo:

| Service | File | Port | Ghi chú |
|---------|------|------|---------|
| api-gateway | `api-gateway/Dockerfile` | 8080 | |
| auth-service | `auth-service/Dockerfile` | 8081 | |
| server-service | `server-service/Dockerfile` | 8082 | |
| monitor-service | `monitor-service/Dockerfile` | 8083 | |
| report-service | `report-service/Dockerfile` | 8084 | Copy email templates |
| fileio-service | `fileio-service/Dockerfile` | 8085 | Mount uploads volume |
| tcp-simulator | `tcp-simulator/Dockerfile` | 9001-19000 | Standalone, no shared module |

### Build & test

```bash
# Build all images
docker-compose build

# Verify images
docker images | grep vcs-sms
```

---

## 5.4. Docker Compose — Full Stack

### 5.4.1. Chạy toàn bộ

```bash
# Copy .env
cp .env.example .env
# Sửa SMTP_PASSWORD, JWT_SECRET, etc.

# Start all
docker-compose up -d

# Kiểm tra status
docker-compose ps

# Xem logs
docker-compose logs -f api-gateway
docker-compose logs -f monitor-service
```

### 5.4.2. Verify checklist

```bash
# 1. Health check tất cả services
curl http://localhost:8080/health    # Gateway
curl http://localhost:8081/health    # Auth
curl http://localhost:8082/health    # Server
curl http://localhost:8083/health    # Monitor
curl http://localhost:8084/health    # Report
curl http://localhost:8085/health    # FileIO

# 2. Full flow test
# Login → CRUD → check ES → report → import/export
```

---

## 5.5. Swagger UI — Merge & Verify

### 5.5.1. Generate Swagger cho từng service

```bash
# Install swag
go install github.com/swaggo/swag/cmd/swag@latest

# Generate
cd server-service && swag init -g cmd/main.go -o docs && cd ..
cd auth-service && swag init -g cmd/main.go -o docs && cd ..
cd report-service && swag init -g cmd/main.go -o docs && cd ..
cd fileio-service && swag init -g cmd/main.go -o docs && cd ..
```

### 5.5.2. Serve Swagger UI qua Gateway

```go
// Trong api-gateway/internal/router/router.go
r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
```

### 5.5.3. Verify

Mở browser: `http://localhost:8080/swagger/index.html`
- Tất cả 15 endpoints hiển thị đúng
- Có thể test trực tiếp từ Swagger UI
- Security scheme (Bearer token) hoạt động

---

## 5.6. Tài liệu thiết kế hệ thống

**File:** `docs/architecture.md`

### Nội dung cần viết:

```markdown
# VCS Server Management System — Tài liệu thiết kế

## 1. Tổng quan
- Mô tả bài toán
- Kiến trúc microservice (diagram)
- Technology stack

## 2. Kiến trúc hệ thống
- System architecture diagram
- Service boundaries
- Data flow diagram
- Sequence diagrams (các luồng chính)

## 3. Database Design
- Schema diagram (ERD)
- Mô tả từng table
- Indexing strategy

## 4. API Design
- Bảng tổng hợp 15 endpoints
- Authentication flow
- Error handling convention

## 5. Caching Strategy
- Redis use cases
- Cache invalidation

## 6. Message Queue
- Kafka topics & events
- Async processing flows

## 7. Monitoring & Health Check
- Health-check mechanism
- Elasticsearch data model
- Uptime calculation

## 8. Security
- JWT authentication
- RBAC (roles & scopes)
- SQL injection prevention

## 9. Logging
- Structured logging
- Log rotation

## 10. Deployment
- Docker setup
- Environment configuration
```

---

## 5.7. Hướng dẫn sử dụng

**File:** `docs/user-guide.md`

### Nội dung cần viết (kèm screenshots):

```markdown
# Hướng dẫn sử dụng VCS-SMS

## 1. Cài đặt & Khởi chạy
- Prerequisites (Docker, Docker Compose)
- Clone repo
- Cấu hình .env
- `docker-compose up -d`
- Verify services

## 2. Authentication
- Screenshot: Register API
- Screenshot: Login API → get token
- Screenshot: Swagger authorize

## 3. Quản lý Server
- Screenshot: Tạo server
- Screenshot: Danh sách server (filter, sort, pagination)
- Screenshot: Cập nhật server
- Screenshot: Xóa server

## 4. Import/Export
- Screenshot: Upload Excel file
- Screenshot: Check import status
- Screenshot: Export filtered servers

## 5. Monitoring
- Screenshot: Health-check logs (Elasticsearch)
- Screenshot: Server status cập nhật

## 6. Báo cáo
- Screenshot: GET report summary
- Screenshot: POST send report
- Screenshot: Email received (inbox)

## 7. Swagger UI
- Screenshot: Swagger UI overview
- Screenshot: Try it out
```

> **Ghi chú:** Screenshots chụp từ Postman hoặc Swagger UI, lưu vào `docs/images/`.

---

## 5.8. README.md

**File:** `README.md` (root)

```markdown
# 🖥️ VCS Server Management System (VCS-SMS)

Hệ thống quản lý 10.000 server theo kiến trúc Microservice.

## Architecture
![Architecture](docs/images/architecture.png)

## Tech Stack
Go • Gin • GORM • PostgreSQL • Redis • Elasticsearch • Kafka • Docker

## Quick Start
\```bash
git clone https://github.com/<username>/vcs-sms.git
cd vcs-sms
cp .env.example .env
# Sửa .env: JWT_SECRET, SMTP_PASSWORD
docker-compose up -d
\```

## Services
| Service | Port | Description |
|---------|------|-------------|
| API Gateway | 8080 | Entry point, JWT auth, rate limiting |
| Auth Service | 8081 | User management, JWT tokens |
| Server Service | 8082 | CRUD server operations |
| Monitor Service | 8083 | Health-check scheduler |
| Report Service | 8084 | Uptime reports, email |
| File I/O Service | 8085 | Excel import/export |
| TCP Simulator | 9001-19000 | Fake 10.000 servers (On/Off theo Math Engine) |

## API Documentation
Swagger UI: http://localhost:8080/swagger/index.html

## Documentation
- [System Design](docs/architecture.md)
- [User Guide](docs/user-guide.md)
- [API Spec](docs/api-spec.yaml)
```

---

## 5.9. Final Verification Checklist

### Chức năng (5.0 điểm)

| # | Chức năng | Test command | Expected | ✅ |
|---|-----------|-------------|----------|---|
| 1 | Health-check định kỳ (2.0đ) | `curl localhost:9200/server-status-logs/_count` | count tăng mỗi 60s | |
| 2 | Tạo server (0.25đ) | `POST /servers` | 201 + server data | |
| 3 | View server (0.25đ) | `GET /servers?status=on&page=1` | paginated list | |
| 4 | Update server (0.25đ) | `PUT /servers/SRV-001` | updated data, server_id unchanged | |
| 5 | Delete server (0.25đ) | `DELETE /servers/SRV-001` | soft deleted | |
| 6 | Import Excel (0.5đ) | `POST /servers/import` → `GET /import/{job_id}` | success + failed lists | |
| 7 | Export Excel (0.5đ) | `POST /servers/export` | .xlsx file downloaded | |
| 8 | Báo cáo định kỳ (0.5đ) | Check cron logs + email inbox | email received | |
| 9 | API báo cáo (0.5đ) | `POST /reports` | summary + email sent | |

### Phi chức năng (5.0 điểm)

| # | Yêu cầu | Verify | ✅ |
|---|---------|--------|---|
| 1 | OpenAPI (0.5đ) | `http://localhost:8080/swagger/index.html` | |
| 2 | Unit Test ≥ 90% (0.5đ) | `go test ./... -cover` per service | |
| 3 | Chống SQL Injection (0.5đ) | GORM parameterized queries, no raw SQL | |
| 4 | Error handling (0.5đ) | Consistent JSON error format with codes | |
| 5 | Log + logrotate (0.5đ) | JSON logs + lumberjack rotation | |
| 6 | JWT + Scope (0.5đ) | Unauthorized → 401, Wrong scope → 403 | |
| 7 | Elasticsearch uptime (1.0đ) | ES aggregation returns uptime % | |
| 8 | Redis Cache (0.5đ) | Cache hit visible in logs | |
| 9 | Công nghệ khác (0.5đ) | Kafka, Docker, etc. | |

### Tài liệu yêu cầu

| # | Tài liệu | Path | ✅ |
|---|---------|------|---|
| 1 | Tài liệu thiết kế | `docs/architecture.md` | |
| 2 | Hướng dẫn sử dụng + screenshots | `docs/user-guide.md` + `docs/images/` | |
| 3 | GitHub repo | `https://github.com/<username>/vcs-sms` | |

---

## 5.10. Push to GitHub

```bash
# 1. Final commit
git add .
git commit -m "feat: complete VCS-SMS microservice system

- 5 microservices + API Gateway + TCP Simulator
- JWT auth with RBAC (3 roles, 9 scopes)
- Health-check 10K servers (TCP Connect → TCP Simulator)
- TCP Simulator: 10K dynamic TCP listeners (Math Engine)
- Elasticsearch uptime tracking
- Redis caching + rate limiting
- Kafka event-driven architecture
- Excel import/export
- Gmail SMTP daily reports
- Docker Compose deployment
- Unit tests >= 90% coverage
- OpenAPI/Swagger documentation"

# 2. Create GitHub repo
# → https://github.com/new → vcs-sms → Public

# 3. Push
git remote add origin https://github.com/<username>/vcs-sms.git
git branch -M main
git push -u origin main

# 4. Verify: mở browser → repo → check files & README
```

---

## 🎉 Project Complete!

Sau khi Phase 5 hoàn tất, bạn sẽ có:

1. ✅ **5 Microservices + 1 Gateway + 1 TCP Simulator** hoạt động đầy đủ
2. ✅ **10.000 servers** được quản lý & monitor
3. ✅ **10/10 điểm** — tất cả yêu cầu chức năng & phi chức năng
4. ✅ **3 tài liệu** nộp: architecture.md, user-guide.md, GitHub link
5. ✅ **Docker Compose** — 1 lệnh deploy toàn bộ
6. ✅ **≥ 90% test coverage** cho mỗi service
