# Hướng dẫn sử dụng VCS-SMS

> **Phiên bản:** 1.0 | **Ngày:** 2026-06-12

Hướng dẫn này giúp bạn cài đặt, cấu hình và sử dụng toàn bộ tính năng của hệ thống VCS Server Management System.

> Các lệnh nhiều dòng trong tài liệu dùng cú pháp Bash/Git Bash/WSL. Nếu chạy bằng PowerShell, dùng `curl.exe` thay cho `curl` và đổi ký tự xuống dòng `\` thành backtick `` ` `` hoặc chạy lệnh trên một dòng.

---

## Mục lục

1. [Cài đặt & Khởi chạy](#1-cài-đặt--khởi-chạy)
2. [Authentication](#2-authentication)
3. [Quản lý Server (CRUD)](#3-quản-lý-server-crud)
4. [Import/Export Excel](#4-importexport-excel)
5. [Monitoring & Health Check](#5-monitoring--health-check)
6. [Báo cáo & Email](#6-báo-cáo--email)
7. [Swagger UI](#7-swagger-ui)
8. [Xử lý lỗi thường gặp](#8-xử-lý-lỗi-thường-gặp)

---

## 1. Cài đặt & Khởi chạy

### 1.1. Yêu cầu hệ thống

- **Docker Desktop** hoặc Docker Engine có **Docker Compose plugin v2+**
- **Go** 1.24+ (chỉ cần nếu dev local)
- **RAM** tối thiểu 4GB (khuyến nghị 8GB cho Elasticsearch)
- **Disk** ~5GB cho images + volumes

### 1.2. Các bước cài đặt

#### Bước 1: Clone repository

```bash
git clone https://github.com/<username>/vcs-sms.git
cd vcs-sms/server-management-system
```

#### Bước 2: Cấu hình môi trường

```bash
cp .env.example .env
```

Sửa các biến quan trọng trong `.env`:

```env
# BẮT BUỘC — JWT Secret (≥ 64 ký tự ngẫu nhiên)
JWT_SECRET=your-random-64-char-string-here-change-in-production

# BẮT BUỘC — Gmail SMTP (dùng App Password)
SMTP_USERNAME=your-email@gmail.com
SMTP_PASSWORD=your-16-char-app-password
SMTP_ADMIN_EMAIL=admin@yourcompany.com
```

> **Cách lấy Gmail App Password:** Vào Google Account → Security → 2-Step Verification → App passwords → Tạo mới cho "Mail" + "Other".

#### Bước 3: Khởi động toàn bộ hệ thống

```bash
# Build images + start containers
docker compose up -d --build

# Đợi khoảng 30-60 giây để tất cả services khởi động
# Kiểm tra trạng thái:
docker compose ps
```

Kết quả mong đợi: 11 runtime containers có status **Up**/**healthy**; `kafka-init` chạy xong và thoát thành công.

Nếu đây là lần chạy lại sau khi từng tạo volume DB cũ và bạn muốn init schema từ đầu:

```bash
docker compose down -v
docker compose up -d --build
```

#### Bước 4: Seed dữ liệu 10.000 servers

```bash
make seed
```

#### Bước 5: Đăng nhập với tài khoản Admin mặc định

> 🔑 Tài khoản admin đã được seed sẵn trong database.
> Username: `admin` | Password: `Admin@123456`

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "Admin@123456"}'
```

---

## 2. Authentication

### 2.1. Đăng ký (Register)

> ⚠️ **Lưu ý:** Người dùng mới đăng ký sẽ tự động được gán role `viewer` (xem server/báo cáo và export danh sách server).
> Để được nâng cấp lên `operator` hoặc `admin`, cần có admin thực hiện (xem section 2.6).

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "operator01",
    "email": "operator@vcs.com",
    "password": "Operator@123",
    "full_name": "John Operator"
  }'
```

**Response (201):**
```json
{
  "status": "success",
  "message": "User registered successfully",
  "data": {
    "id": "uuid...",
    "username": "operator01",
    "email": "operator@vcs.com",
    "role": "viewer"
  }
}
```

### 2.2. Đăng nhập (Login)

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "Admin@123456"}'
```

**Response (200):**
```json
{
  "status": "success",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIs...",
    "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
    "token_type": "Bearer",
    "expires_in": 900
  }
}
```

> 💡 **Lưu `access_token`** — dùng cho tất cả API yêu cầu xác thực. Token hết hạn sau 15 phút.

### 2.3. Làm mới Token (Refresh)

```bash
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token": "eyJhbGciOiJIUzI1NiIs..."}'
```

### 2.4. Đăng xuất (Logout)

```bash
curl -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer <access_token>"
```

### 2.5. Xem Profile

```bash
curl http://localhost:8080/api/v1/auth/profile \
  -H "Authorization: Bearer <access_token>"
```

### 2.6. Quản lý người dùng (Admin only)

> 🔒 **Yêu cầu:** Scope `user:manage`. Chỉ Admin mới có quyền này.

#### Xem danh sách người dùng

```bash
curl "http://localhost:8080/api/v1/auth/users?page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

**Response (200):** Danh sách user với role hiện tại, không hiển thị mật khẩu.

#### Nâng cấp / Hạ cấp role người dùng

```bash
curl -X PUT http://localhost:8080/api/v1/auth/users/<user_id>/role \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role_name": "operator"}'
```

**Response (200):** User được cập nhật với role mới.

> ⚠️ **Giới hạn:**
> - Admin không thể tự thay đổi role của chính mình (400 Bad Request).
> - Role hợp lệ: `admin`, `operator`, `viewer`.

---

## 3. Quản lý Server (CRUD)

> **Biến môi trường:** Lưu token vào biến để dùng lại:
> ```bash
> TOKEN="eyJhbGciOiJIUzI1NiIs..."
> ```

### 3.1. Tạo Server mới

```bash
curl -X POST http://localhost:8080/api/v1/servers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "server_id": "SRV-WEB-001",
    "server_name": "web-server-01",
    "ipv4": "10.0.1.100",
    "os": "Ubuntu 22.04",
    "cpu_cores": 8,
    "ram_gb": 16,
    "disk_gb": 500,
    "location": "DC-HN",
    "description": "Production web server"
  }'
```

**Response (201):** Server object với `status: "off"` mặc định.

### 3.2. Xem danh sách Server (Filter, Sort, Pagination)

```bash
# Lấy tất cả server, trang 1, 20 server/trang
curl "http://localhost:8080/api/v1/servers?page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"

# Lọc theo trạng thái ON + Ubuntu + sắp xếp theo tên
curl "http://localhost:8080/api/v1/servers?status=on&os=Ubuntu&sort_by=server_name&sort_order=asc&page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"

# Tìm kiếm theo tên hoặc IP
curl "http://localhost:8080/api/v1/servers?server_name=web" \
  -H "Authorization: Bearer $TOKEN"
```

**Tham số hỗ trợ:**

| Param | Mô tả | Ví dụ |
|-------|-------|-------|
| `status` | on / off | `status=on` |
| `server_name` | Tìm theo tên | `server_name=web` |
| `ipv4` | Tìm theo IP | `ipv4=10.0.1` |
| `os` | Lọc OS | `os=Ubuntu` |
| `location` | Lọc vị trí | `location=DC-HN` |
| `sort_by` | Sắp xếp theo cột | `sort_by=created_at` |
| `sort_order` | asc / desc | `sort_order=desc` |
| `page` | Số trang | `page=1` |
| `page_size` | Số lượng/trang (max 100) | `page_size=20` |

### 3.3. Xem chi tiết Server

```bash
curl http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN"
```

### 3.4. Cập nhật Server

```bash
curl -X PUT http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "server_name": "web-server-01-updated",
    "cpu_cores": 16,
    "ram_gb": 32,
    "description": "Upgraded production server"
  }'
```

> ⚠️ `server_id` không thể thay đổi sau khi tạo.

### 3.5. Xóa Server (Soft Delete)

```bash
curl -X DELETE http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN"
```

**Response (200):** Server bị ẩn khỏi danh sách, Monitor Service ngừng health-check.

---

## 4. Import/Export Excel

### 4.1. Import Server từ Excel

#### Chuẩn bị file Excel

Tạo file `.xlsx` với các cột theo thứ tự:

| server_id | server_name | ipv4 | os | cpu_cores | ram_gb | disk_gb | location | description |
|-----------|-------------|------|-----|-----------|--------|---------|----------|-------------|
| SRV-001 | web-01 | 10.0.1.1 | Ubuntu | 8 | 16 | 500 | DC-HN | Web server |

> 📌 **Cột bắt buộc:** `server_id`, `server_name`, `ipv4`. Các cột còn lại là tùy chọn.

#### Upload file

```bash
curl -X POST http://localhost:8080/api/v1/servers/import \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@servers.xlsx"
```

**Response (202):**
```json
{
  "status": "success",
  "message": "Import job queued",
  "data": {
    "job_id": "uuid...",
    "status": "pending",
    "file_name": "servers.xlsx",
    "message": "File đã được nhận, đang xử lý bất đồng bộ"
  }
}
```

#### Kiểm tra tiến độ

```bash
curl http://localhost:8080/api/v1/servers/import/<job_id> \
  -H "Authorization: Bearer $TOKEN"
```

**Response (200):**
```json
{
  "status": "success",
  "data": {
    "job_id": "uuid...",
    "status": "completed",
    "total_rows": 100,
    "success_count": 95,
    "failed_count": 5,
    "success_list": [ ... ],
    "failed_list": [
      {"row_number": 5, "server_id": "SRV-001", "status": "failed", "reason": "Duplicate server_id"},
      {"row_number": 12, "server_id": "", "status": "failed", "reason": "server_id is required"}
    ],
    "created_at": "2026-06-12T10:00:00Z"
  }
}
```

> ⚠️ **Giới hạn:** File ≤ 10MB, định dạng `.xlsx`. Import xử lý bất đồng bộ qua Kafka — các dòng trùng lặp bị bỏ qua, không làm dừng toàn bộ job.

### 4.2. Export Server ra Excel

```bash
# Export tất cả server
curl -X POST http://localhost:8080/api/v1/servers/export \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}' \
  --output servers_export.xlsx

# Export với bộ lọc
curl -X POST http://localhost:8080/api/v1/servers/export \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "status": "on",
    "os": "Ubuntu",
    "location": "DC-HN",
    "sort_by": "server_name",
    "sort_order": "asc"
  }' \
  --output servers_filtered.xlsx
```

File Excel kết quả có 12 cột với header được định dạng (nền xanh, chữ trắng đậm).

---

## 5. Monitoring & Health Check

### 5.1. Xem trạng thái Monitor Service

```bash
curl http://localhost:8080/api/v1/monitor/status \
  -H "Authorization: Bearer $TOKEN"
```

**Response (200):**

```json
{
  "status": "ok",
  "service": "monitor-service",
  "check_interval": 60,
  "worker_count": 100,
  "tcp_timeout_ms": 5000,
  "elasticsearch": "http://elasticsearch:9200",
  "index": "server-status-logs",
  "redis_available": true
}
```

### 5.2. Kiểm tra dữ liệu trong Elasticsearch

```bash
# Tổng số bản ghi health-check
curl "http://localhost:9200/server-status-logs/_count"

# Xem 10 bản ghi mới nhất
curl "http://localhost:9200/server-status-logs/_search?size=10&sort=checked_at:desc"
```

---

## 6. Báo cáo & Email

### 6.1. Xem Uptime Summary

```bash
curl "http://localhost:8080/api/v1/reports/summary?start_date=2026-06-01&end_date=2026-06-12" \
  -H "Authorization: Bearer $TOKEN"
```

**Response (200):**
```json
{
  "status": "success",
  "data": {
    "start_date": "2026-06-01",
    "end_date": "2026-06-12",
    "total_servers": 10000,
    "servers_on": 9523,
    "servers_off": 477,
    "avg_uptime_pct": 95.7,
    "total_checks": 120000,
    "low_uptime_servers": [
      {"server_id": "SRV-05231", "server_name": "db-231", "uptime_pct": 45.2, "total_checks": 12, "on_checks": 5}
    ]
  }
}
```

> 💡 Kết quả được cache trong Redis 1 giờ. Lần gọi tiếp theo trong 1 giờ sẽ trả về ngay không cần query Elasticsearch.

### 6.2. Gửi báo cáo qua Email

```bash
curl -X POST http://localhost:8080/api/v1/reports \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "start_date": "2026-06-01",
    "end_date": "2026-06-12",
    "email": "manager@company.com"
  }'
```

**Response (200):**
```json
{
  "status": "success",
  "message": "Report sent successfully",
  "data": {
    "report_id": "uuid...",
    "status": "completed",
    "message": "Report sent successfully",
    "summary": {
      "total_servers": 10000,
      "servers_on": 9523,
      "servers_off": 477,
      "avg_uptime_pct": 95.7
    }
  }
}
```

Email HTML bao gồm:
- 🟢 Header gradient (tím)
- 📊 4 stat cards: Tổng Server, Online, Offline, Avg Uptime
- 📋 Bảng Top 10 server có uptime thấp nhất
- 📅 Daily report tự động gửi lúc 8:00 AM (cấu hình trong `.env`)

### 6.3. Kiểm tra Email đã nhận

Mở Gmail inbox của `SMTP_ADMIN_EMAIL` — kiểm tra email từ `VCS-SMS`.

---

## 7. Swagger UI

Mở trình duyệt: **http://localhost:8080/swagger/index.html**

### Cách sử dụng:

1. Nhấn nút **Authorize** 🔒 (góc trên bên phải)
2. Nhập `Bearer <access_token>` vào ô Value
3. Nhấn **Authorize** → **Close**
4. Chọn endpoint muốn test → **Try it out** → **Execute**

Tất cả 18 endpoints đều có thể test trực tiếp từ Swagger UI.

---

## 8. Xử lý lỗi thường gặp

| Lỗi | Nguyên nhân | Cách khắc phục |
|-----|------------|---------------|
| `401 Unauthorized` | Token hết hạn hoặc sai | Đăng nhập lại để lấy token mới |
| `403 Forbidden` | Không đủ quyền (scope) | Kiểm tra role của user |
| `429 Too Many Requests` | Vượt rate limit (100 req/phút) | Đợi 1 phút rồi thử lại |
| `422 Validation Failed` | Dữ liệu không hợp lệ | Kiểm tra `errors[]` trong response |
| `409 Conflict` | Trùng `server_id` hoặc `server_name` | Dùng giá trị khác |
| Service không start | Port conflict hoặc thiếu biến môi trường | `docker compose logs <service>` |
| Kafka không nhận message | Kafka chưa sẵn sàng | Đợi Kafka healthy (`docker compose ps`) |
| Email không gửi được | SMTP sai hoặc App Password hết hạn | Kiểm tra `.env` SMTP config |
| Elasticsearch không có dữ liệu | Monitor Service chưa chạy hoặc TCP Simulator chưa sẵn sàng | `docker compose logs monitor-service` |
| `make seed` báo relation không tồn tại | DB volume cũ chưa chạy `init.sql` mới | `docker compose down -v` rồi `docker compose up -d --build` |

---

## 📚 Tham khảo thêm

- [Tài liệu thiết kế hệ thống](architecture.md)
- [OpenAPI Specification](api-spec.yaml)
- [Database Strategy](02-database-strategy.md)
- [Event-Driven Architecture](03-event-driven-kafka.md)
