# 🧪 Phase 1 Test Guide — Auth + Server + Gateway

> **Ngày:** 2026-06-10
> **Thời gian test:** ~20 phút
> **Yêu cầu:** Docker Desktop, Go 1.25+, WSL terminal

---

## 📋 Tổng quan

Sau khi hoàn thành Phase 1, bạn test 3 service:
1. **Auth Service** (port 8081) — 5 endpoints JWT/RBAC
2. **Server Service** (port 8082) — 5 endpoints CRUD + Cache
3. **API Gateway** (port 8080) — JWT middleware + routing (bonus)

---

## Bước 0: Xác nhận file `.env`

File `server-management-system/.env` đã có sẵn. Các giá trị quan trọng dùng trong test:

| Biến | Giá trị | Dùng cho |
|------|---------|----------|
| `POSTGRES_USER` | `postgres` | Docker Postgres |
| `POSTGRES_PASSWORD` | `123456` | Docker Postgres |
| `REDIS_PASSWORD` | `redis123456` | Docker Redis + service |
| `AUTH_DB_USER` | `auth_user` | Auth service → DB |
| `AUTH_DB_PASSWORD` | `auth123456` | Auth service → DB |
| `SERVER_DB_USER` | `server_user` | Server service → DB |
| `SERVER_DB_PASSWORD` | `server123456` | Server service → DB |
| `APP_PORT` | `8081` | Auth service port |
| `JWT_SECRET` | _(đã set đủ dài)_ | JWT signing |

> **Không cần sửa gì.** Code đã tự đọc `../.env` qua Viper.

---

## Bước 1: Build tất cả modules

Mở terminal WSL, đứng tại `server-management-system/`:

```bash
# Build shared
cd shared && go build ./... && echo "✅ shared OK" && cd ..

# Build auth-service
cd auth-service && go build ./... && echo "✅ auth-service OK" && cd ..

# Build server-service
cd server-service && go build ./... && echo "✅ server-service OK" && cd ..

# Build api-gateway
cd api-gateway && go build ./... && echo "✅ api-gateway OK" && cd ..
```

**Kỳ vọng:** 4 dòng ✅

---

## Bước 2: Chạy unit tests

```bash
# Test shared (JWT)
cd shared && go test ./... -v -count=1 2>&1 | grep -E "PASS|FAIL|ok"
# Kỳ vọng: ok  github.com/vcs-sms/shared/pkg/jwt  0.0xs

# Test auth-service
cd ../auth-service && go test ./... -v -count=1 -short 2>&1 | grep -E "PASS|FAIL|SKIP|ok"
# Kỳ vọng: 2 dòng ok, 1 SKIP (Login_Success)

# Test server-service
cd ../server-service && go test ./... -v -count=1 -short 2>&1 | grep -E "PASS|FAIL|ok"
# Kỳ vọng: 2 dòng ok
```

**Kỳ vọng:** 39 tests PASS, 1 SKIP (Login cần Redis)

---

## Bước 3: Khởi động infrastructure

```bash
cd /mnt/c/Users/admin/Documents/VCS_Passport/CheckPoint1/server-management-system

# Chạy Postgres + Redis
docker compose -f docker-compose.dev.yml up -d postgres redis
```

**Kỳ vọng:**
```
[+] Running 2/2
 ✔ Container vcs-sms-postgres  Started
 ✔ Container vcs-sms-redis     Started
```

**Kiểm tra:**

```bash
# Postgres health
docker exec vcs-sms-postgres pg_isready -U postgres

# Redis health (dùng REDIS_PASSWORD từ .env: redis123456)
docker exec vcs-sms-redis redis-cli -a redis123456 ping

# Kiểm tra schemas đã init
docker exec vcs-sms-postgres psql -h localhost -U postgres -d vcs_sms -c "\dn"
# Phải thấy: auth_schema, server_schema, monitor_schema, report_schema, fileio_schema

# Kiểm tra roles đã seed
docker exec vcs-sms-postgres psql -h localhost -U postgres -d vcs_sms -c "SELECT name FROM auth_schema.roles;"
# Phải thấy: admin, operator, viewer
```

---

## Bước 4: Chạy Auth Service

Mở **terminal mới** (giữ Docker chạy ở terminal cũ):

```bash
cd /mnt/c/Users/admin/Documents/VCS_Passport/CheckPoint1/server-management-system/auth-service
go run cmd/main.go
```

**Kỳ vọng thấy 3 dòng log:**
```
[DB] Connected to PostgreSQL successfully
[Redis] Connected successfully
INF Starting auth service addr=:8081
```

Nếu báo lỗi `FATAL: JWT_SECRET must be at least 32 characters`, kiểm tra file `.env` đã có `JWT_SECRET=` đủ dài chưa.

---

## Bước 5: Test Auth API

Mở **terminal thứ 3** để gọi curl:

### 5.1 Register — Tạo user mới

```bash
curl -s -X POST http://localhost:8081/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin01",
    "email": "admin@test.com",
    "password": "Admin@123456",
    "full_name": "Admin User",
    "role_name": "admin"
  }' | python3 -m json.tool
```

**Kỳ vọng:** `"status": "success"`, `"code": 201`, `"data"` chứa user info

### 5.2 Test duplicate username → 409

```bash
curl -s -X POST http://localhost:8081/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"admin01","email":"other@test.com","password":"Admin@123456","full_name":"Dup","role_name":"viewer"}' | python3 -m json.tool
```

**Kỳ vọng:** `"code": 409`, `"message": "Dữ liệu đã tồn tại"`

### 5.3 Login — Lấy token

```bash
curl -s -X POST http://localhost:8081/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin01","password":"Admin@123456"}' | python3 -m json.tool
```

**Kỳ vọng:** `"status": "success"`, trong `data` có:
- `access_token` (string dài)
- `refresh_token` (string dài)
- `expires_in: 900`
- `token_type: "Bearer"`

### 5.4 Lưu token vào biến môi trường

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin01","password":"Admin@123456"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['access_token'])")

echo "Token: ${TOKEN:0:20}..."
```

### 5.5 Get Profile (cần token)

```bash
curl -s http://localhost:8081/api/v1/auth/profile \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
```

**Kỳ vọng:** User info + `"role": "admin"` + `"scopes": ["server:create","server:read","server:update","server:delete"]`

### 5.6 Get Profile không token → 401

```bash
curl -s http://localhost:8081/api/v1/auth/profile | python3 -m json.tool
```

**Kỳ vọng:** `"code": 401` (vì handler tự check user_id từ context)

### 5.7 Login sai password → 401

```bash
curl -s -X POST http://localhost:8081/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin01","password":"wrongpassword"}' | python3 -m json.tool
```

**Kỳ vọng:** `"code": 401`, `"message": "Username hoặc password không đúng"`

### 5.8 Refresh Token (rotation)

```bash
# Lấy refresh token
REFRESH=$(curl -s -X POST http://localhost:8081/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin01","password":"Admin@123456"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['refresh_token'])")

# Refresh — sẽ trả về token MỚI
curl -s -X POST http://localhost:8081/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH\"}" | python3 -m json.tool
```

**Kỳ vọng:** `"status": "success"`, `refresh_token` trong response **khác** với `$REFRESH` ban đầu (rotation hoạt động)

### 5.9 Refresh lại token CŨ → lỗi

```bash
# Dùng lại token cũ đã bị rotate
curl -s -X POST http://localhost:8081/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"$REFRESH\"}" | python3 -m json.tool
```

**Kỳ vọng:** `"code": 401`, token cũ đã bị revoke

### 5.10 Logout

```bash
curl -s -X POST http://localhost:8081/api/v1/auth/logout \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
```

**Kỳ vọng:** `"message": "Logged out successfully"`

### 5.11 Gọi API sau logout → 401

```bash
curl -s http://localhost:8081/api/v1/auth/profile \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
```

**Kỳ vọng:** `"code": 401` (token đã blacklist)

### 5.12 Brute-force test (thử 6 lần login sai)

```bash
for i in {1..6}; do
  echo "Attempt $i:"
  curl -s -X POST http://localhost:8081/api/v1/auth/login \
    -H "Content-Type: application/json" \
    -d '{"username":"admin01","password":"wrong"}' | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  Code: {d[\"code\"]}, Message: {d[\"message\"]}')"
  sleep 0.5
done
```

**Kỳ vọng:** 5 lần đầu → `401 Invalid credentials`, lần thứ 6 → `429 Too many attempts`

---

## Bước 6: Chạy Server Service

Mở **terminal thứ 4**:

```bash
cd /mnt/c/Users/admin/Documents/VCS_Passport/CheckPoint1/server-management-system/server-service

# Server service cần port 8082 (khác auth 8081 do .env set APP_PORT=8081)
# Cách 1: export tạm
export APP_PORT=8082
# Cách 2: sửa dòng APP_PORT=8082 trong .env trước khi chạy

go run cmd/main.go
```

> **Tại sao export APP_PORT?** Vì `.env` đang set `APP_PORT=8081` cho auth-service. Server cần port khác.

**Kỳ vọng:**
```
[DB] Connected to PostgreSQL successfully
[Redis] Connected successfully
INF Starting server service addr=:8082
```

---

## Bước 7: Test Server CRUD API

### 7.1 Tạo server

```bash
curl -s -X POST http://localhost:8082/api/v1/servers \
  -H "Content-Type: application/json" \
  -d '{
    "server_id": "SRV-001",
    "server_name": "web-server-01",
    "ipv4": "192.168.1.100",
    "os": "Ubuntu 22.04",
    "cpu_cores": 8,
    "ram_gb": 16,
    "disk_gb": 256,
    "location": "Hanoi-DC1",
    "description": "Primary web server"
  }' | python3 -m json.tool
```

**Kỳ vọng:** `"code": 201`, `"status": "off"`, có `created_at`

### 7.2 Tạo thêm server

```bash
curl -s -X POST http://localhost:8082/api/v1/servers \
  -H "Content-Type: application/json" \
  -d '{"server_id":"SRV-002","server_name":"db-server-01","ipv4":"10.0.1.50","os":"CentOS 9","location":"Hanoi-DC2"}' | python3 -m json.tool

curl -s -X POST http://localhost:8082/api/v1/servers \
  -H "Content-Type: application/json" \
  -d '{"server_id":"SRV-003","server_name":"cache-server-01","ipv4":"172.16.0.10","os":"Debian 12","location":"Hanoi-DC1"}' | python3 -m json.tool
```

### 7.3 Test duplicate server_id → 409

```bash
curl -s -X POST http://localhost:8082/api/v1/servers \
  -H "Content-Type: application/json" \
  -d '{"server_id":"SRV-001","server_name":"other","ipv4":"10.0.0.1"}' | python3 -m json.tool
```

**Kỳ vọng:** `"code": 409`

### 7.4 Test validation error (thiếu ipv4) → 422

```bash
curl -s -X POST http://localhost:8082/api/v1/servers \
  -H "Content-Type: application/json" \
  -d '{"server_id":"SRV-004","server_name":"bad-server"}' | python3 -m json.tool
```

**Kỳ vọng:** `"code": 422`, `"errors"` chứa field-level detail

### 7.5 Get server detail

```bash
curl -s http://localhost:8082/api/v1/servers/SRV-001 | python3 -m json.tool
```

**Kỳ vọng:** Đầy đủ 12 field của server

### 7.6 Get server không tồn tại → 404

```bash
curl -s http://localhost:8082/api/v1/servers/NONEXIST | python3 -m json.tool
```

**Kỳ vọng:** `"code": 404`

### 7.7 List servers (pagination)

```bash
curl -s "http://localhost:8082/api/v1/servers?page=1&page_size=10" | python3 -m json.tool
```

**Kỳ vọng:** `"total": 3`, `"page": 1`, `"page_size": 10`, `"total_pages": 1`

### 7.8 List với filter

```bash
# Filter theo location
curl -s "http://localhost:8082/api/v1/servers?location=Hanoi-DC1" | python3 -c "
import sys,json
d=json.load(sys.stdin)['data']
print(f'Total: {d[\"total\"]}, Servers: {[s[\"server_name\"] for s in d[\"servers\"]]}')"

# Filter theo OS
curl -s "http://localhost:8082/api/v1/servers?os=Ubuntu" | python3 -c "
import sys,json
d=json.load(sys.stdin)['data']
print(f'Total: {d[\"total\"]}, Servers: {[s[\"server_name\"] for s in d[\"servers\"]]}')"
```

**Kỳ vọng:** Filter hoạt động, trả về đúng servers

### 7.9 List với sort

```bash
curl -s "http://localhost:8082/api/v1/servers?sort_by=server_name&sort_order=asc" | python3 -c "
import sys,json
d=json.load(sys.stdin)['data']
print([s['server_name'] for s in d['servers']])"
```

**Kỳ vọng:** `['cache-server-01', 'db-server-01', 'web-server-01']` (alphabetical)

### 7.10 Update server (partial)

```bash
curl -s -X PUT http://localhost:8082/api/v1/servers/SRV-001 \
  -H "Content-Type: application/json" \
  -d '{"os":"Ubuntu 24.04","cpu_cores":16,"ram_gb":32}' | python3 -m json.tool
```

**Kỳ vọng:** `os → "Ubuntu 24.04"`, `cpu_cores → 16`, `ram_gb → 32`, các field khác giữ nguyên

### 7.11 Update server không tồn tại → 404

```bash
curl -s -X PUT http://localhost:8082/api/v1/servers/NONEXIST \
  -H "Content-Type: application/json" \
  -d '{"os":"test"}' | python3 -m json.tool
```

**Kỳ vọng:** `"code": 404`

### 7.12 Update trùng tên → 409

```bash
curl -s -X PUT http://localhost:8082/api/v1/servers/SRV-001 \
  -H "Content-Type: application/json" \
  -d '{"server_name":"db-server-01"}' | python3 -m json.tool
```

**Kỳ vọng:** `"code": 409` (tên đã tồn tại ở SRV-002)

### 7.13 Delete server

```bash
curl -s -X DELETE http://localhost:8082/api/v1/servers/SRV-003 | python3 -m json.tool
```

**Kỳ vọng:** `"message": "Server deleted successfully"`

### 7.14 Verify deleted → 404

```bash
curl -s http://localhost:8082/api/v1/servers/SRV-003 | python3 -m json.tool
```

**Kỳ vọng:** `"code": 404`

---

## Bước 8 (Bonus): Test qua API Gateway

Mở **terminal thứ 5**:

```bash
cd /mnt/c/Users/admin/Documents/VCS_Passport/CheckPoint1/server-management-system/api-gateway
export APP_PORT=8080
go run ./cmd/
```

### 8.1 Gọi không token → bị chặn

```bash
curl -s http://localhost:8080/api/v1/servers/SRV-001 | python3 -m json.tool
```

**Kỳ vọng:** `"code": 401`, `"Missing or invalid authorization header"`

### 8.2 Login qua Gateway

```bash
GW_TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin01","password":"Admin@123456"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['access_token'])")

echo "Gateway token: ${GW_TOKEN:0:20}..."
```

### 8.3 Gọi server qua Gateway (có token)

```bash
curl -s http://localhost:8080/api/v1/servers/SRV-001 \
  -H "Authorization: Bearer $GW_TOKEN" | python3 -m json.tool
```

**Kỳ vọng:** `"status": "success"`, dữ liệu server (proxy qua gateway → server-service)

### 8.4 Gọi với scope không đủ → 403

Đăng ký user `viewer` và thử DELETE (viewer chỉ có `server:read`):

```bash
# Đăng ký viewer
curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"viewer01","email":"viewer@test.com","password":"Viewer@123456","full_name":"Viewer","role_name":"viewer"}'

# Login viewer
VIEWER_TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"viewer01","password":"Viewer@123456"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['access_token'])")

# Thử DELETE (cần scope server:delete)
curl -s -X DELETE http://localhost:8080/api/v1/servers/SRV-001 \
  -H "Authorization: Bearer $VIEWER_TOKEN" | python3 -m json.tool
```

**Kỳ vọng:** `"code": 403`, `"Required scope: server:delete"`

---

## Bước 9: Graceful shutdown test

Nhấn `Ctrl+C` ở terminal đang chạy auth-service hoặc server-service.

**Kỳ vọng thấy log:**
```
INF Shutting down auth service...
INF Server exited
```

Service dừng sạch, không crash.

---

## Bước 10: Dọn dẹp

```bash
# Dừng Docker containers
cd /mnt/c/Users/admin/Documents/VCS_Passport/CheckPoint1/server-management-system
docker compose -f docker-compose.dev.yml down

# Ctrl+C các terminal còn lại
```

---

## 📊 Checklist nhanh

| # | Test | Endpoint | Kỳ vọng | ✓ |
|---|------|----------|----------|---|
| 1 | Build 4 modules | — | 4 ✅ | ☐ |
| 2 | Unit tests | — | 39 PASS, 1 SKIP | ☐ |
| 3 | Docker up | — | 2 containers | ☐ |
| 4 | DB init | — | 5 schemas, 3 roles | ☐ |
| 5 | Register | POST /auth/register | 201 | ☐ |
| 6 | Duplicate username | POST /auth/register | 409 | ☐ |
| 7 | Login | POST /auth/login | 200 + tokens | ☐ |
| 8 | Get Profile | GET /auth/profile | 200 + scopes | ☐ |
| 9 | No token profile | GET /auth/profile | 401 | ☐ |
| 10 | Wrong password | POST /auth/login | 401 | ☐ |
| 11 | Refresh token | POST /auth/refresh | 200 + rotation | ☐ |
| 12 | Reuse old refresh | POST /auth/refresh | 401 revoked | ☐ |
| 13 | Logout | POST /auth/logout | 200 | ☐ |
| 14 | After logout | GET /auth/profile | 401 blacklisted | ☐ |
| 15 | Brute-force 6 lần | POST /auth/login | 429 lần 6 | ☐ |
| 16 | Create server | POST /servers | 201 | ☐ |
| 17 | Duplicate server_id | POST /servers | 409 | ☐ |
| 18 | Validation error | POST /servers | 422 field-level | ☐ |
| 19 | Get server | GET /servers/:id | 200 | ☐ |
| 20 | Not found | GET /servers/:id | 404 | ☐ |
| 21 | List servers | GET /servers | pagination OK | ☐ |
| 22 | List + filter | GET /servers?location= | filter OK | ☐ |
| 23 | List + sort | GET /servers?sort_by= | sort OK | ☐ |
| 24 | Update server | PUT /servers/:id | 200 partial | ☐ |
| 25 | Update not found | PUT /servers/:id | 404 | ☐ |
| 26 | Update duplicate name | PUT /servers/:id | 409 | ☐ |
| 27 | Delete server | DELETE /servers/:id | 200 | ☐ |
| 28 | Verify deleted | GET /servers/:id | 404 | ☐ |
| 29 | Gateway no token | GET /servers/:id | 401 | ☐ |
| 30 | Gateway with token | GET /servers/:id | 200 proxy | ☐ |
| 31 | Gateway scope deny | DELETE /servers/:id | 403 | ☐ |
| 32 | Graceful shutdown | Ctrl+C | clean exit | ☐ |

---

## ⚠️ Troubleshooting

| Lỗi | Nguyên nhân | Cách fix |
|-----|------------|----------|
| `FATAL: JWT_SECRET must be at least 32 characters` | JWT secret trong `.env` quá ngắn | Thêm secret >= 32 ký tự |
| `dial tcp 127.0.0.1:5432: connect: connection refused` | Postgres chưa chạy | `docker compose -f docker-compose.dev.yml up -d postgres` |
| `dial tcp 127.0.0.1:6379: connect: connection refused` | Redis chưa chạy | `docker compose -f docker-compose.dev.yml up -d redis` |
| `pq: password authentication failed for user "auth_user"` | DB password trong `.env` (`auth123456`) không khớp `init.sql` | Kiểm tra `AUTH_DB_PASSWORD` và user đã được tạo trong DB |
| `NOAUTH Authentication required` (Redis) | Redis cần password | Dùng `redis-cli -a redis123456` (theo `.env`) |
| `role "vcs_admin" does not exist` (psql) | User trong `.env` là `postgres`, không phải `vcs_admin` | Dùng `-U postgres` |
| `pq: relation "auth_schema.users" does not exist` | DB chưa init | Chạy lại `docker compose down -v && docker compose up -d` |
| `address already in use` | Port đã dùng | Kill process cũ hoặc đổi `APP_PORT` |
| Build lỗi `package not found` | Chưa `go mod tidy` | `cd <service> && go mod tidy` |
