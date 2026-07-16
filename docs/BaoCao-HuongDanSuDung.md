# BÁO CÁO HƯỚNG DẪN SỬ DỤNG
## VCS Server Management System (VCS-SMS)

> **Chương trình:** VCS Passport — Checkpoint 1
> **Phiên bản:** 1.0
> **Ngày:** 2026-06-19

Tài liệu này hướng dẫn cài đặt, cấu hình và sử dụng toàn bộ tính năng của hệ thống VCS-SMS, **kèm ảnh chụp màn hình** cho từng tính năng đã yêu cầu trong đề bài.

> 📸 **Quy ước ảnh chụp màn hình:** Tất cả ảnh được đặt trong thư mục `docs/screenshots/` và nhúng theo cú pháp `![...](screenshots/<tên-ảnh>.png)`. Nếu một ảnh chưa có, phần chú thích bên dưới mô tả nội dung màn hình tương ứng để dễ đối chiếu.

> 💡 Các lệnh `curl` nhiều dòng dùng cú pháp Bash/Git Bash/WSL. Trên PowerShell, dùng `curl.exe` thay cho `curl`.

---

## Mục lục

1. [Cài đặt & khởi chạy](#1-cài-đặt--khởi-chạy)
2. [Đăng nhập & Đăng ký](#2-đăng-nhập--đăng-ký)
3. [Trang Tổng quan (Dashboard)](#3-trang-tổng-quan-dashboard)
4. [Kiểm tra trạng thái server (Health Check)](#4-kiểm-tra-trạng-thái-server-health-check)
5. [Quản lý Server — CRUD](#5-quản-lý-server--crud)
6. [Import / Export Excel](#6-import--export-excel)
7. [Báo cáo & Email](#7-báo-cáo--email)
8. [Quản lý người dùng & phân quyền](#8-quản-lý-người-dùng--phân-quyền)
9. [Hồ sơ cá nhân](#9-hồ-sơ-cá-nhân)
10. [Swagger UI (OpenAPI)](#10-swagger-ui-openapi)
11. [Xử lý lỗi thường gặp](#11-xử-lý-lỗi-thường-gặp)

---

## 1. Cài đặt & khởi chạy

### 1.1. Yêu cầu hệ thống

- **Docker** 29+ và **Docker Compose** v2+
- **RAM** tối thiểu 4GB (khuyến nghị 8GB cho Elasticsearch)
- **Disk** ~5GB cho images + volumes
- **Go** 1.24+ (chỉ cần khi dev local)

### 1.2. Các bước cài đặt

**Bước 1 — Clone repository**
```bash
git clone https://github.com/<username>/vcs-sms.git
cd vcs-sms/server-management-system
```

**Bước 2 — Cấu hình môi trường**
```bash
cp .env.example .env
```
Sửa các biến quan trọng trong `.env`:
```env
JWT_SECRET=chuoi-ngau-nhien-it-nhat-64-ky-tu
SMTP_USERNAME=your-email@gmail.com
SMTP_PASSWORD=your-16-char-app-password
SMTP_ADMIN_EMAIL=admin@yourcompany.com
```
> **Lấy Gmail App Password:** Google Account → Security → 2-Step Verification → App passwords → tạo mới cho "Mail".

**Bước 3 — Khởi động toàn bộ hệ thống**
```bash
docker compose up -d --build   # 11 runtime container + web + kafka-init
docker compose ps              # tất cả phải ở trạng thái Up/healthy
```

**Bước 4 — Seed 10.000 server**
```bash
make seed
```

**Bước 5 — Truy cập hệ thống**
- **Web UI:** http://localhost:3000
- **API Gateway:** http://localhost:8080
- **Swagger UI:** http://localhost:8080/swagger/index.html

> 🔑 Tài khoản admin mặc định đã được seed sẵn: **`admin` / `Admin@123456`**.

---

## 2. Đăng nhập & Đăng ký

### 2.1. Đăng nhập (Login)

Mở **http://localhost:3000/login**, nhập **Tên đăng nhập** và **Mật khẩu** rồi bấm **Đăng nhập**. Sau khi thành công, hệ thống chuyển tới trang Tổng quan.

![Màn hình đăng nhập](screenshots/01-login.png)
*Hình 2.1 — Màn hình Đăng nhập với form Tên đăng nhập / Mật khẩu.*

Tương đương qua API:
```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"Admin@123456"}'
```
**Response (200):** trả về `access_token` (TTL 15 phút) và `refresh_token` (TTL 7 ngày).

### 2.2. Đăng ký (Register)

Người dùng mới đăng ký tại **http://localhost:3000/register**. Tài khoản mới mặc định được gán role **`viewer`**; admin có thể nâng cấp role sau (xem mục 8).

![Màn hình đăng ký](screenshots/02-register.png)
*Hình 2.2 — Màn hình Đăng ký tài khoản mới (mặc định role viewer).*

### 2.3. Làm mới token & Đăng xuất
```bash
# Refresh
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token>"}'

# Logout (đưa token vào blacklist)
curl -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer <access_token>"
```

---

## 3. Trang Tổng quan (Dashboard)

Trang **Tổng quan** hiển thị nhanh tình trạng toàn hệ thống và **tự động làm mới mỗi 60 giây**:
- 3 thẻ KPI: **Tổng server**, **Đang On**, **Đang Off**.
- Khối **Thao tác nhanh**: Quản lý servers, Xem báo cáo, Import Excel.
- Nút **Làm mới** + nhãn thời gian cập nhật gần nhất.

![Trang Tổng quan](screenshots/03-dashboard.png)
*Hình 3 — Dashboard: KPI tổng/On/Off cập nhật theo thời gian thực.*

---

## 4. Kiểm tra trạng thái server (Health Check)

Đây là chức năng cốt lõi (2.0 điểm). Monitor Service tự động **ping TCP 10.000 server mỗi 60 giây**, ghi log vào Elasticsearch và cập nhật trạng thái On/Off. Người dùng quan sát kết quả health-check qua:

- **Cột Trạng thái** (On/Off) trong danh sách server (mục 5.1) — đổi màu theo trạng thái thực tế.
- **Thẻ KPI On/Off** ở Dashboard và trang Báo cáo.
- **Trang chi tiết server** hiển thị trạng thái & thông tin giám sát.

![Chi tiết & trạng thái server](screenshots/10-server-detail.png)
*Hình 4 — Trang chi tiết server: trạng thái On/Off và thông tin giám sát.*

Kiểm tra trạng thái Monitor Service và dữ liệu Elasticsearch qua API:
```bash
# Trạng thái Monitor Service (scope monitor:view)
curl http://localhost:8080/api/v1/monitor/status \
  -H "Authorization: Bearer $TOKEN"

# Tổng số bản ghi health-check trong Elasticsearch
curl "http://localhost:9200/server-status-logs/_count"
```
**Response `/monitor/status`** gồm: `check_interval` (60s), `worker_count` (100), `tcp_timeout_ms` (5000), `index` (`server-status-logs`), `redis_available`.

---

## 5. Quản lý Server — CRUD

Vào menu **Servers**. Trang gồm thanh tìm kiếm (ID, tên, IPv4), bộ lọc trạng thái (Tất cả / On / Off), bảng có **sắp xếp theo cột** và **phân trang**, cùng các nút **Tạo server / Import / Export / Refresh**.

![Danh sách server](screenshots/04-servers-list.png)
*Hình 5.1 — Danh sách server: tìm kiếm, lọc, sắp xếp, phân trang.*

### 5.1. Xem danh sách (filter, sort, pagination)

- **Tìm kiếm:** nhập ID / tên / IPv4 rồi bấm **Search**.
- **Lọc trạng thái:** chọn tab **On** hoặc **Off**.
- **Sắp xếp:** bấm vào tiêu đề cột (ID, Tên, Trạng thái, IPv4, Vị trí, Ngày tạo, Cập nhật) để đổi `asc`/`desc`.
- **Phân trang:** chọn số dòng/trang (10/20/50/100) và chuyển trang ở chân bảng.

![Lọc & sắp xếp server](screenshots/05-servers-filter-sort.png)
*Hình 5.2 — Lọc server đang On + sắp xếp theo tên (asc).*

Tương đương qua API:
```bash
curl "http://localhost:8080/api/v1/servers?status=on&os=Ubuntu&sort_by=server_name&sort_order=asc&page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

| Tham số | Mô tả | Ví dụ |
|---------|-------|-------|
| `status` | on / off | `status=on` |
| `server_name` / `ipv4` / `os` / `location` | lọc theo trường | `server_name=web` |
| `sort_by` | cột sắp xếp | `sort_by=created_at` |
| `sort_order` | asc / desc | `sort_order=desc` |
| `page` / `page_size` | phân trang (max 100/trang) | `page=1&page_size=20` |

### 5.2. Tạo server (Create)

Bấm **Tạo server** → điền form (`server_id`, `server_name`, `ipv4` bắt buộc; OS, CPU, RAM, Disk, Vị trí, Mô tả tùy chọn) → **Lưu**. Server mới mặc định `status = off` cho tới chu kỳ health-check kế tiếp.

![Dialog tạo server](screenshots/06-create-server-dialog.png)
*Hình 5.3 — Form Tạo server mới.*

```bash
curl -X POST http://localhost:8080/api/v1/servers \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"server_id":"SRV-WEB-001","server_name":"web-server-01","ipv4":"10.0.1.100","os":"Ubuntu 22.04","cpu_cores":8,"ram_gb":16,"disk_gb":500,"location":"DC-HN"}'
```

### 5.3. Cập nhật server (Update)

Bấm biểu tượng ✏️ (Sửa) ở dòng server → chỉnh sửa → **Lưu**.
> ⚠️ Trường **`server_id` không thể thay đổi** sau khi tạo.

![Dialog sửa server](screenshots/07-edit-server-dialog.png)
*Hình 5.4 — Form Cập nhật server (server_id bị khóa).*

```bash
curl -X PUT http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"server_name":"web-server-01-updated","cpu_cores":16,"ram_gb":32}'
```

### 5.4. Xóa server (Delete)

Bấm biểu tượng 🗑️ (Xóa) → xác nhận trong hộp thoại. Hệ thống thực hiện **soft delete** (ẩn khỏi danh sách, Monitor ngừng health-check).

![Dialog xác nhận xóa](screenshots/08-delete-server-dialog.png)
*Hình 5.5 — Hộp thoại xác nhận xóa server.*

```bash
curl -X DELETE http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN"
```

---

## 6. Import / Export Excel

### 6.1. Import server từ Excel

Tại trang Servers bấm **Import** → chọn file `.xlsx` → tải lên. Import chạy **bất đồng bộ** (qua Kafka): hệ thống trả về `job_id` ngay, sau đó cập nhật tiến độ. Các dòng có `server_id`/`server_name` **trùng sẽ bị bỏ qua**, không làm dừng cả job.

> 📌 Cột bắt buộc: `server_id`, `server_name`, `ipv4`. Giới hạn: file ≤ 10MB, định dạng `.xlsx`.

![Dialog import Excel](screenshots/09-import-dialog.png)
*Hình 6.1 — Hộp thoại Import Excel và kết quả thành công/thất bại.*

```bash
# Upload
curl -X POST http://localhost:8080/api/v1/servers/import \
  -H "Authorization: Bearer $TOKEN" -F "file=@servers.xlsx"

# Theo dõi tiến độ (success_list / failed_list + lý do)
curl http://localhost:8080/api/v1/servers/import/<job_id> \
  -H "Authorization: Bearer $TOKEN"
```

### 6.2. Export server ra Excel

Bấm **Export** trên trang Servers — hệ thống xuất file `.xlsx` theo đúng bộ lọc/sắp xếp hiện tại của bảng. File kết quả có 12 cột với header được định dạng.

```bash
curl -X POST http://localhost:8080/api/v1/servers/export \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"status":"on","os":"Ubuntu","sort_by":"server_name","sort_order":"asc"}' \
  --output servers_export.xlsx
```

---

## 7. Báo cáo & Email

Vào menu **Báo cáo**. Chọn khoảng **Từ ngày → Đến ngày**, bấm **Xem báo cáo**. Trang hiển thị:
- 4 thẻ KPI: **Tổng server**, **On**, **Off**, **Uptime trung bình** (kèm số lượt check).
- Biểu đồ **donut On/Off** và biểu đồ **Top server uptime thấp nhất**.
- Bảng **chi tiết server uptime thấp** (On/Tổng, % uptime).

![Trang báo cáo uptime](screenshots/11-reports.png)
*Hình 7.1 — Báo cáo uptime: KPI, biểu đồ On/Off và bảng server uptime thấp.*

**Công thức Uptime:** `Uptime = (số lần check "on") / (tổng số lần check) × 100%`.

```bash
curl "http://localhost:8080/api/v1/reports/summary?start_date=2026-06-01&end_date=2026-06-19" \
  -H "Authorization: Bearer $TOKEN"
```

### 7.1. Gửi báo cáo qua Email (chủ động)

Bấm **Gửi qua email** → nhập khoảng ngày và địa chỉ email người nhận → **Gửi**. Hệ thống tính toán uptime từ Elasticsearch và gửi email HTML (header gradient, 4 stat cards, bảng Top 10 server uptime thấp nhất).

![Dialog gửi báo cáo email](screenshots/12-send-report-dialog.png)
*Hình 7.2 — Hộp thoại gửi báo cáo qua email.*

```bash
curl -X POST http://localhost:8080/api/v1/reports \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"start_date":"2026-06-01","end_date":"2026-06-19","email":"manager@company.com"}'
```

### 7.2. Báo cáo định kỳ tự động

Hệ thống tự gửi email báo cáo tình trạng server ngày hôm trước **lúc 8:00 sáng hằng ngày** tới `SMTP_ADMIN_EMAIL` (cron nội bộ trong Report Service). Nội dung gồm: tổng server, số On, số Off và tỉ lệ uptime trung bình.

---

## 8. Quản lý người dùng & phân quyền

> 🔒 Chỉ **Admin** (scope `user:manage`) thấy menu **Người dùng**.

Trang **Người dùng** liệt kê danh sách tài khoản kèm role hiện tại. Admin có thể **nâng cấp/hạ cấp role** (`viewer` ↔ `operator` ↔ `admin`).

![Trang quản lý người dùng](screenshots/13-users.png)
*Hình 8 — Quản lý người dùng và phân quyền (RBAC).*

```bash
# Danh sách người dùng
curl "http://localhost:8080/api/v1/auth/users?page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"

# Đổi role
curl -X PUT http://localhost:8080/api/v1/auth/users/<user_id>/role \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"role_name":"operator"}'
```

**Bảng phân quyền (RBAC):**

| Role | Quyền chính |
|------|-------------|
| **Admin** | Toàn quyền (10 scopes), gồm quản lý người dùng |
| **Operator** | CRUD server, import/export, xem & gửi báo cáo |
| **Viewer** | Chỉ xem server, export, xem báo cáo |

> ⚠️ Admin không thể tự đổi role của chính mình. Role hợp lệ: `admin`, `operator`, `viewer`.

---

## 9. Hồ sơ cá nhân

Menu **Hồ sơ** hiển thị thông tin tài khoản đang đăng nhập: username, email, họ tên, role và danh sách scope được cấp.

![Trang hồ sơ](screenshots/14-profile.png)
*Hình 9 — Hồ sơ cá nhân & quyền hạn.*

```bash
curl http://localhost:8080/api/v1/auth/profile \
  -H "Authorization: Bearer $TOKEN"
```

---

## 10. Swagger UI (OpenAPI)

Toàn bộ 18 endpoint được mô tả bằng OpenAPI 3.0.3. Mở **http://localhost:8080/swagger/index.html** để thử nghiệm trực tiếp:

1. Bấm **Authorize** 🔒 → nhập `Bearer <access_token>` → **Authorize**.
2. Chọn endpoint → **Try it out** → nhập tham số → **Execute**.

![Swagger UI](screenshots/15-swagger.png)
*Hình 10 — Swagger UI: tài liệu hóa & thử nghiệm toàn bộ API.*

---

## 11. Xử lý lỗi thường gặp

| Lỗi | Nguyên nhân | Cách khắc phục |
|-----|-------------|----------------|
| `401 Unauthorized` | Token hết hạn hoặc sai | Đăng nhập lại để lấy token mới |
| `403 Forbidden` | Không đủ quyền (scope) | Kiểm tra role của tài khoản |
| `429 Too Many Requests` | Vượt rate limit (100 req/phút) | Đợi 1 phút rồi thử lại |
| `422 Validation Failed` | Dữ liệu không hợp lệ | Xem mảng `errors[]` trong response |
| `409 Conflict` | Trùng `server_id` / `server_name` | Dùng giá trị khác |
| Service không start | Port conflict / thiếu `.env` | `docker compose logs <service>` |
| Email không gửi được | SMTP sai / App Password hết hạn | Kiểm tra cấu hình SMTP trong `.env` |
| Elasticsearch chưa có dữ liệu | Monitor / TCP Simulator chưa sẵn sàng | `docker compose logs monitor-service` |
| `make seed` báo relation không tồn tại | DB volume cũ chưa chạy `init.sql` mới | `docker compose down -v` rồi `up -d --build` |

---

> **Tài liệu liên quan:** Báo cáo mô tả & thiết kế hệ thống (`docs/BaoCao-MoTa-ThietKe-HeThong.md`), OpenAPI Spec (`docs/api-spec.yaml`).
>
> **VCS Server Management System © 2026** — Chương trình đào tạo VCS Passport
