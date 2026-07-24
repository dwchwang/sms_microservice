# BÁO CÁO HƯỚNG DẪN SỬ DỤNG
## VCS Server Management System (VCS-SMS)

> **Chương trình:** VCS Passport
> **Phiên bản:** 2.1 — hướng dẫn cho kiến trúc sau refactor
> **Ngày:** 2026-07-24

Tài liệu này hướng dẫn cài đặt, cấu hình và sử dụng toàn bộ tính năng của hệ thống VCS-SMS.
Mọi lệnh trong tài liệu đã được chạy thật trên hệ thống đang hoạt động (10.000 server).

> **Ghi chú phiên bản:** bản 1.0 (Checkpoint 1) mô tả kiến trúc cũ — 5 service + API Gateway
> tự viết + Kafka + đăng nhập bằng `username` — và không còn đúng. Những thay đổi ảnh hưởng
> trực tiếp tới người dùng:
>
> | Bản 1.0 | Hiện tại |
> |---|---|
> | Đăng nhập bằng `username` | Đăng nhập bằng **`email`** |
> | Import bất đồng bộ qua Kafka, trả `job_id` | Import **đồng bộ**, trả kết quả ngay trong response |
> | `GET /api/v1/monitor/status` | **Không còn** — monitor không có endpoint public |
> | Swagger UI tại `/swagger/index.html` | **Không có** — spec là file `docs/api-spec.yaml` |
> | `SMTP_ADMIN_EMAIL` | `REPORT_DAILY_RECIPIENT` |
> | Báo cáo tự động 8:00 | `REPORT_DAILY_CRON`, mặc định **10:00** giờ VN |

> 💡 Các lệnh `curl` nhiều dòng dùng cú pháp Bash/Git Bash/WSL. Trên PowerShell, dùng
> `curl.exe` thay cho `curl` và đổi `\` cuối dòng thành backtick `` ` ``.

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
10. [Tài liệu API (OpenAPI)](#10-tài-liệu-api-openapi)
11. [Vận hành & chẩn đoán](#11-vận-hành--chẩn-đoán)
12. [Xử lý lỗi thường gặp](#12-xử-lý-lỗi-thường-gặp)

---

## 1. Cài đặt & khởi chạy

### 1.1. Yêu cầu hệ thống

- **Docker** 24+ và **Docker Compose** v2+
- **RAM** tối thiểu 4GB (khuyến nghị 8GB — Elasticsearch chiếm ~1GB)
- **Disk** ~5GB cho image + volume
- **Go** 1.24+ (chỉ cần khi dev local hoặc chạy test)

### 1.2. Các bước cài đặt

**Bước 1 — Clone repository**
```bash
git clone <repo-url>
cd sms_microservice/server-management-system
```

**Bước 2 — Cấu hình môi trường**
```bash
cp .env.example .env
```

Bốn biến bắt buộc phải sửa trước khi chạy thật:

```env
# Tối thiểu 32 ký tự — service TỪ CHỐI khởi động nếu ngắn hơn
JWT_SECRET=chuoi-ngau-nhien-it-nhat-64-ky-tu

SMTP_USERNAME=your-email@gmail.com
SMTP_PASSWORD=your-16-char-app-password
SMTP_FROM=VCS-SMS <your-email@gmail.com>

# Người nhận báo cáo tự động hằng ngày. Để RỖNG = không đăng ký job gửi mail.
REPORT_DAILY_RECIPIENT=admin@company.com

# Domain được phép nhận mail. RỖNG = cho gửi tới BẤT KỲ AI (đừng dùng ở production).
SMTP_RECIPIENT_DOMAINS=
```

> **Lấy Gmail App Password:** Google Account → Security → 2-Step Verification → App
> passwords → tạo mới cho "Mail". Mật khẩu Gmail thường **không** dùng được cho SMTP.

**Bước 3 — Khởi động toàn bộ hệ thống**
```bash
docker compose up -d --build   # 10 container
docker compose ps              # postgres/redis/es/tcp-simulator/report phải healthy
```

Mười container: `postgres`, `redis`, `elasticsearch`, `tcp-simulator`, `traefik`,
`auth-service`, `server-service`, `monitor-service`, `report-service`, `web`.

**Bước 4 — Seed 10.000 server rồi dựng lại projection**
```bash
make seed             # nạp 10.000 server vào server_db
make rebuild-cache    # BẮT BUỘC — nếu thiếu, Monitoring bỏ qua MỌI round
```

> ⚠️ Hai lệnh này phải đi cùng nhau. Monitoring không đọc PostgreSQL: nó đọc một
> *projection* trong Redis, và projection chỉ được dựng bởi `rebuild-cache`. Thiếu bước
> hai, log monitor sẽ liên tục báo `target projection not ready` và không server nào được ping.

**Bước 5 — Truy cập hệ thống**

| Thành phần | Địa chỉ |
|---|---|
| Web UI | http://localhost:3000 |
| API (qua Traefik) | http://localhost:8080/api/v1 |
| Elasticsearch | http://localhost:9200 |
| PostgreSQL | `localhost:5432` |
| Redis | `localhost:6379` |

> 🔑 Tài khoản admin seed sẵn: **`admin@vcs.com` / `Admin@123456`**.

Bốn service ứng dụng dùng `expose` chứ **không** `ports`, nên không truy cập được trực
tiếp từ host. Đó là chủ đích: header `X-User-Id` / `X-User-Scopes` do Traefik tiêm vào được
coi là đáng tin, nên gọi thẳng vào service sẽ vượt mặt toàn bộ lớp xác thực.

### 1.3. Chạy dev local (chỉ hạ tầng trong Docker)

```bash
make dev-up                      # chỉ postgres, redis, elasticsearch, tcp-simulator
cd auth-service && go run ./cmd  # rồi tương tự cho từng service
```

---

## 2. Đăng nhập & Đăng ký

### 2.1. Đăng nhập (Login)

Mở **http://localhost:3000/login**, nhập **Email** và **Mật khẩu** rồi bấm **Đăng nhập**.

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@vcs.com","password":"Admin@123456"}'
```

**Response (200):**
```json
{
  "status": "success",
  "code": 200,
  "message": "Login successful",
  "data": {
    "access_token": "eyJhbGciOi...",
    "refresh_token": "eyJhbGciOi...",
    "expires_in": 900,
    "token_type": "Bearer"
  }
}
```

`access_token` TTL 15 phút (`JWT_ACCESS_EXPIRY_MINUTES`), `refresh_token` TTL 7 ngày
(`JWT_REFRESH_EXPIRY_DAYS`). **Scope được nhúng thẳng vào access token** lúc login — đó là
lý do đổi role chỉ có hiệu lực sau khi token cũ hết hạn hoặc người dùng đăng nhập lại.

Lưu token để dùng cho các lệnh sau:
```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@vcs.com","password":"Admin@123456"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["access_token"])')
```

> 🔒 Sai mật khẩu **5 lần** trong 15 phút sẽ khoá tạm theo email
> (`AUTH_TOO_MANY_ATTEMPTS`). Đăng nhập thành công thì bộ đếm được xoá. Traefik còn có
> rate limit riêng cho `/api/v1/auth`: trung bình 10 req/s, burst 20.

### 2.2. Đăng ký (Register)

Đăng ký tại **http://localhost:3000/register**. Tài khoản mới **luôn** nhận role `viewer`
(nguyên tắc đặc quyền tối thiểu); muốn nâng quyền phải có admin đổi (mục 8).

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"nhanvien@vcs.com","password":"Passw0rd@123","full_name":"Nguyễn Văn A"}'
```

Response trả về cả danh sách `scopes` của role vừa gán, nên client biết ngay mình được làm gì.

### 2.3. Làm mới token & Đăng xuất

```bash
# Refresh — trả về access token MỚI và refresh token MỚI (rotation)
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token>"}'

# Logout
curl -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer $TOKEN"
```

**Refresh token dùng được đúng một lần.** Mỗi lần refresh, jti cũ bị xoá khỏi Redis và jti
mới được ghi. Nếu token bị đánh cắp và dùng lần thứ hai, hệ thống trả lỗi thu hồi.

> ⚠️ **Giới hạn hiện tại:** logout ghi access token vào danh sách thu hồi
> (`auth:blacklist:{jti}`) nhưng endpoint xác thực của Traefik **chưa kiểm** danh sách đó.
> Nghĩa là access token đã logout vẫn dùng được cho tới khi hết hạn (tối đa 15 phút).
> Refresh token thì bị thu hồi ngay, nên phiên không kéo dài thêm được.

---

## 3. Trang Tổng quan (Dashboard)

Trang **Tổng quan** (`/`) hiển thị nhanh tình trạng toàn hệ thống và **tự động làm mới mỗi
60 giây** — cùng nhịp với chu kỳ health-check.

- **4 thẻ KPI:** Tổng server · Đang On · Đang Off · Chưa rõ (`UNKNOWN`).
- **Thao tác nhanh:** Quản lý servers, Xem báo cáo, Import Excel.
- Nút **Làm mới** + nhãn thời gian cập nhật gần nhất.

Nguồn dữ liệu là `GET /api/v1/servers/stats` (PostgreSQL, cache 10 giây):

```bash
curl http://localhost:8080/api/v1/servers/stats -H "Authorization: Bearer $TOKEN"
```

```json
{ "data": { "total": 10000, "on": 8910, "off": 1090, "unknown": 0 } }
```

**`UNKNOWN` có hai nghĩa hoàn toàn khác nhau:**

| Tình huống | Ý nghĩa |
|---|---|
| Server vừa tạo, chưa qua vòng ping nào | tạm thời, sẽ hết trong ≤ 60 giây |
| Số `UNKNOWN` lớn và không giảm | **báo động** — Monitoring không chạy, hoặc thiếu target projection |

Menu bên trái hiện theo scope: `/servers` cần `server:list`, `/reports` cần `report:view`,
`/users` cần `user:list`. Thiếu scope thì menu không hiện, và truy cập thẳng URL sẽ về
trang `/403`.

---

## 4. Kiểm tra trạng thái server (Health Check)

Đây là chức năng cốt lõi. Monitor Service ping **TCP 10.000 server mỗi 60 giây**, ghi
health fact vào Elasticsearch và cập nhật trạng thái On/Off.

### 4.1. Định nghĩa On/Off

```text
TCP connect tới ipv4:tcp_port trong MONITOR_TCP_TIMEOUT (mặc định 3000ms)
  thành công → ON
  thất bại   → OFF, kèm error_code: TIMEOUT | DNS_ERROR | CONNECTION_REFUSED | DIAL_ERROR
```

Chỉ là **TCP connect** — không gửi payload, không đọc gì. Câu hỏi là "cổng có mở không",
và connect trả lời đúng câu đó với chi phí thấp nhất.

### 4.2. Người dùng quan sát kết quả ở đâu

| Nơi | Hiển thị gì |
|---|---|
| Cột **Trạng thái** trong danh sách server | On/Off/Chưa rõ, đổi màu theo trạng thái |
| Cột **Kiểm tra lúc** (`last_status_check`) | Thời điểm ping gần nhất — nhích mỗi phút |
| Thẻ KPI ở Dashboard | Tổng/On/Off/Chưa rõ |
| Trang **Báo cáo** | Uptime **của hôm nay** + top 10 server tệ nhất |
| Trang chi tiết server | Trạng thái, thời điểm đổi trạng thái, thông tin giám sát |

> **Monitor Service không có endpoint public.** Nó không có route `/api/v1/...` nào và
> không publish port ra host. Người dùng nhìn kết quả qua `server-service`; người vận hành
> nhìn qua `/metrics` (mục 11).

### 4.3. Kiểm tra dữ liệu thô trong Elasticsearch

Index được tạo theo **ngày UTC**, nên phải dùng pattern chứ không một tên cố định:

```bash
# Danh sách index và số document
curl -s "http://localhost:9200/_cat/indices/server-status-logs-*?v&h=index,docs.count,store.size"

# Tổng số bản ghi health-check
curl -s "http://localhost:9200/server-status-logs-*/_count"
```

Ví dụ thật trên hệ thống đang chạy:

```text
index                         docs.count store.size
server-status-logs-2026.07.22    4286866      218mb
server-status-logs-2026.07.23    2720000    136.9mb
server-status-logs-2026.07.24    1689757     85.9mb
```

10.000 server × 1.440 vòng/ngày ≈ **14,4 triệu document/ngày** nếu chạy liên tục 24 giờ.
ILM policy `server-status-logs-retention` **xoá index sau 7 ngày** — dữ liệu lịch sử đã
được cô đọng vào `daily_snapshots` trước đó (mục 7).

---

## 5. Quản lý Server — CRUD

Vào menu **Servers**. Trang gồm ô tìm kiếm (ID, tên, IPv4, port), bộ lọc trạng thái, bảng
có sắp xếp theo cột và phân trang, cùng các nút **Tạo server / Import / Export / Làm mới**.

### 5.1. Xem danh sách (filter, sort, pagination)

```bash
curl "http://localhost:8080/api/v1/servers?status=ON&os=Ubuntu&sort_by=server_name&sort_order=asc&page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

| Tham số | Mô tả | Ví dụ |
|---------|-------|-------|
| `status` | `ON` / `OFF` / `UNKNOWN` (**chữ hoa**) | `status=ON` |
| `server_id` / `server_name` / `os` / `location` | lọc chứa (ILIKE) | `server_name=web` |
| `ipv4` | lọc theo **tiền tố** | `ipv4=10.20.` |
| `tcp_port` | khớp chính xác | `tcp_port=9001` |
| `sort_by` | cột sắp xếp | `sort_by=created_at` |
| `sort_order` | `asc` / `desc` | `sort_order=desc` |
| `page` / `page_size` | phân trang, **`page_size` bị cap ở 100** | `page=1&page_size=20` |

Mỗi dòng trả về gồm cả `last_status_check` — đọc tươi từ Redis lúc trả response, nên nó
nhích mỗi phút mà cache danh sách vẫn hit.

### 5.2. Tạo server (Create)

Bấm **Tạo server** → điền form → **Lưu**.

Bắt buộc: `server_id`, `server_name`, `ipv4`. Tuỳ chọn: `tcp_port` (mặc định 80), `os`,
`cpu_cores`, `ram_gb`, `disk_gb`, `location`, `description`.

```bash
curl -X POST http://localhost:8080/api/v1/servers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"server_id":"SRV-WEB-001","server_name":"web-server-01","ipv4":"10.0.1.100",
       "tcp_port":80,"os":"Ubuntu 22.04","cpu_cores":8,"ram_gb":16,"disk_gb":500,
       "location":"DC-HN"}'
```

Ba điều dễ vướng:

| Điều | Chi tiết |
|---|---|
| **`Idempotency-Key` là BẮT BUỘC** | Thiếu header → `400 COMMON_VALIDATION_FAILED`. Cùng key + cùng body → trả lại response cũ, không tạo dòng thứ hai. Cùng key + body khác → `409` |
| **IPv4 phải nằm trong CIDR allowlist** | `SERVER_CIDR_ALLOWLIST`, mặc định `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16`. Ngoài dải → `422 SERVER_IP_NOT_ALLOWED` |
| **Client không set được `status`** | DTO không có field đó. Server mới luôn `UNKNOWN` tới vòng ping kế tiếp (≤ 60 giây) |

### 5.3. Cập nhật server (Update)

Bấm biểu tượng **Sửa** ở dòng server → chỉnh sửa → **Lưu**.

```bash
curl -X PUT http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"server_name":"web-server-01-updated","cpu_cores":16,"ram_gb":32}'
```

> ⚠️ **`server_id` không thể thay đổi** — nó không có trong request body. Đây là chủ đích:
> `server_id` là khoá nối sang `daily_snapshots` và các health fact trong Elasticsearch.

Chỉ những field gửi lên mới bị đổi (partial update). Đổi `ipv4`/`tcp_port`/`server_name`
sẽ tự đồng bộ lại target projection, nên Monitoring dùng địa chỉ mới ngay vòng sau.

### 5.4. Xóa server (Delete)

```bash
curl -X DELETE http://localhost:8080/api/v1/servers/SRV-WEB-001 \
  -H "Authorization: Bearer $TOKEN"
```

**Soft delete** (`deleted_at = NOW()`): server ẩn khỏi danh sách, rời khỏi target
projection nên Monitoring ngừng ping, và bị gỡ khỏi bảng xếp hạng uptime.

> `server_id` **unique toàn cục kể cả với server đã xoá**, nên một ID đã dùng không tái sử
> dụng được cho server khác. Điều này bảo vệ lịch sử báo cáo khỏi việc lẫn hai server khác
> nhau trùng ID. Ngược lại, `server_name` của server đã xoá thì **được** dùng lại.

---

## 6. Import / Export Excel

### 6.1. Import server từ Excel

Tại trang Servers bấm **Import** → chọn file `.xlsx` → tải lên.

**Import chạy đồng bộ:** response *chính là* báo cáo kết quả, không có `job_id` để poll.

```bash
curl -X POST http://localhost:8080/api/v1/servers/import \
  -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: $(uuidgen)" \
  -F "file=@servers.xlsx"
```

**File import — 10 cột, đúng thứ tự này:**

```text
server_id · server_name · ipv4 · tcp_port · os
cpu_cores · ram_gb · disk_gb · location · description
```

| Cột | Bắt buộc | Ghi chú |
|---|:---:|---|
| `server_id` | ✅ | Unique toàn cục, kể cả với server đã xoá |
| `server_name` | ✅ | Unique trong các server đang sống |
| `ipv4` | ✅ | Phải nằm trong `SERVER_CIDR_ALLOWLIST` |
| `tcp_port` | ✅ | 1–65535 |
| `os`, `location`, `description` | ❌ | Rỗng → NULL |
| `cpu_cores`, `ram_gb`, `disk_gb` | ❌ | Rỗng → NULL; nếu có phải > 0 |

Giới hạn: tối đa **10.000 dòng dữ liệu**. Header sai tên hoặc sai thứ tự → **từ chối cả
file** (`400`), vì đọc tiếp một file lệch cột sẽ ghi `ipv4` vào cột `os`.

**Response — ba nhóm tách bạch:**

```json
{
  "total_rows": 9,
  "succeeded":         { "count": 2, "items": ["SRV-01", "SRV-02"] },
  "failed":            { "count": 3, "items": [
      { "row": 4, "server_id": "SRV-04", "reason": "SERVER_IP_NOT_ALLOWED" }
  ]},
  "skipped_duplicate": { "count": 4, "items": ["SRV-05", "SRV-06"] }
}
```

| Nhóm | Nghĩa là gì |
|---|---|
| `succeeded` | Đã chèn mới, **hoặc** hồi sinh một server đã xoá mềm |
| `failed` | Dòng sai — kèm số dòng và lý do để sửa file |
| `skipped_duplicate` | Trùng và bị bỏ qua: trùng trong chính file, hoặc `server_id`/`server_name` đã có server đang sống |

**Lỗi một dòng không làm hỏng cả request** — chỉ lỗi *file* mới từ chối tất cả. Import
10.000 dòng mà hỏng vì dòng thứ 3.000 là hành vi tệ.

> 💡 **Import lại đúng file cũ sau khi xoá vài server sẽ hồi sinh đúng những server đó.**
> Ví dụ: xoá 5 server rồi import lại file 10.000 dòng → kết quả là 5 `succeeded` và
> 9.995 `skipped_duplicate`.

### 6.2. Export server ra Excel

Bấm **Export** trên trang Servers — file xuất theo **đúng bộ lọc đang áp dụng** trên bảng.

```bash
curl -X POST http://localhost:8080/api/v1/servers/export \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"location":"DC-HN","page":1,"page_size":50}' \
  --output servers_export.xlsx
```

Dùng `POST` cho một thao tác *đọc* vì bộ lọc có thể dài; vì vậy export **không** cần
`Idempotency-Key`.

**File export — 14 cột** (nhiều hơn file import 4 cột):

```text
server_id · server_name · status · last_status_check · ipv4 · tcp_port · os
cpu_cores · ram_gb · disk_gb · location · description · created_at · updated_at
```

Bốn cột thêm là những thứ **hệ thống sinh ra**, không phải người dùng nhập. Vì vậy file
export không dùng lại được làm file import trực tiếp — đó là chủ đích: import mà nhận
`status` thì client sẽ tự đặt được trạng thái server.

Tên file nằm ở header `Content-Disposition`
(`servers_export_20260724_083901.xlsx`), và Traefik expose header đó qua CORS để trình
duyệt đọc được.

> 🔒 **Chống formula injection:** ô bắt đầu bằng `=`, `+`, `-`, `@`, tab hoặc CR được
> prefix dấu `'`. Không làm vậy thì một server tên `=cmd|'/c calc'!A1` sẽ chạy lệnh khi có
> người mở file.

---

## 7. Báo cáo & Email

Hệ thống có **hai** loại số liệu uptime, đừng nhầm lẫn:

| | Trang Báo cáo (web) | Báo cáo email |
|---|---|---|
| Câu hỏi | "Hôm nay hệ thống thế nào?" | "Ngày X→Y hệ thống thế nào?" |
| Nguồn | Bộ đếm trong **Redis** | Bảng **`daily_snapshots`** |
| Kỳ | Ngày hiện tại theo giờ VN | Khoảng ngày chỉ định |
| Có ngay không | **Có, mọi lúc** | Chỉ những ngày **đã kết thúc** và đã snapshot |
| API | `GET /servers/uptime` | `GET /reports/summary`, `POST /reports` |

### 7.1. Trang Báo cáo — uptime hôm nay

Vào menu **Báo cáo** (`/reports`), tự làm mới mỗi 60 giây:

- **4 thẻ KPI:** Tổng server · Đang On · Đang Off · **Uptime trung bình hôm nay**
  (cảnh báo màu khi < 95%, và ghi rõ bao nhiêu server chưa được check).
- **Biểu đồ donut** trạng thái hiện tại (On/Off/Chưa rõ).
- **Biểu đồ + bảng Top 10 server uptime thấp nhất hôm nay**, kèm `On / Tổng check hôm nay`.

```bash
curl http://localhost:8080/api/v1/servers/uptime -H "Authorization: Bearer $TOKEN"
```

```json
{ "data": {
  "total_servers": 10000, "servers_on": 8876, "servers_off": 1124, "servers_unknown": 0,
  "servers_no_data": 0,
  "servers_uptime_100": 0, "servers_uptime_partial": 10000, "servers_uptime_0": 0,
  "avg_uptime_pct": 89.62,
  "top_10_lowest_uptime": [
    { "server_id": "SRV-06126", "server_name": "storage-0613",
      "uptime_pct": 78.69, "total_checks": 169, "on_checks": 133 }
  ]
}}
```

**Con số này là của NGÀY HÔM NAY theo giờ Việt Nam** và tự reset lúc nửa đêm. Bộ đếm nằm
trong Redis, được Lua script cập nhật ngay trong mỗi lượt ping, nên endpoint trả lời trong
khoảng 0,1 giây với 10.000 server.

`servers_no_data` là số server Monitoring **chưa từng chấm điểm** — server vừa tạo được
đếm riêng chứ không bị coi là uptime 0%.

### 7.2. Báo cáo theo khoảng ngày

```bash
curl "http://localhost:8080/api/v1/reports/summary?start_date=2026-07-22&end_date=2026-07-23" \
  -H "Authorization: Bearer $TOKEN"
```

```json
{ "data": {
  "start_date": "2026-07-22", "end_date": "2026-07-23",
  "total_servers": 10002,
  "servers_on_at_end_at": 8930, "servers_off_at_end_at": 1070,
  "servers_uptime_100": 0, "servers_uptime_partial": 10001, "servers_uptime_0": 1,
  "servers_no_data": 0,
  "avg_uptime_pct": 87.47,
  "expected_checks": 28800835, "actual_checks": 7006866,
  "coverage_pct": 24.33, "degraded": true,
  "top_10_lowest_uptime": [ … ]
}}
```

**Công thức:**
```text
uptime_pct   = on_checks / actual_checks × 100        (mỗi server có dữ liệu)
coverage_pct = Σ actual_checks / Σ expected_checks × 100
```

| Field | Đọc thế nào |
|---|---|
| `servers_on_at_end_at` | ON/OFF **tại cuối kỳ**, là ảnh chụp một thời điểm — không đại diện cho cả khoảng |
| `avg_uptime_pct` | Trung bình uptime của các server **có dữ liệu** |
| `servers_no_data` | Thuộc kỳ báo cáo nhưng **không có lượt đo nào** |
| `coverage_pct` | Hệ thống **đo được bao nhiêu phần** so với lẽ ra phải đo |
| `degraded` | `true` khi `coverage_pct < REPORT_COVERAGE_THRESHOLD` (95%) — email sẽ có banner cảnh báo |

`coverage_pct` thấp **không** có nghĩa server có vấn đề; nó có nghĩa **hệ thống giám sát**
đã không chạy đủ. Ví dụ ở trên (24%) là vì Monitoring chỉ chạy vài giờ trong ngày đó, không
phải 24 giờ. Đây chính là điều báo cáo cố ý phơi ra thay vì tính trung bình trên số liệu thiếu.

**Ba quy tắc hợp lệ của khoảng ngày:**

| Vi phạm | Lỗi |
|---|---|
| `end_date` là hôm nay hoặc tương lai | `422 REPORT_INVALID_RANGE` — "end_date must be a day that has already finished" |
| Khoảng > `REPORT_MAX_RANGE_DAYS` (31) | `422 REPORT_INVALID_RANGE` |
| Có ngày chưa snapshot | `422 REPORT_DATA_UNAVAILABLE` — **nêu rõ tên ngày thiếu** |

Ví dụ thật:
```json
{ "status": "error", "code": "REPORT_DATA_UNAVAILABLE",
  "message": "missing snapshots for: 2026-07-19, 2026-07-20, 2026-07-21" }
```

Hệ thống **từ chối** chứ không lấy trung bình vắt qua lỗ hổng — trung bình trên một cái lỗ
là số bịa.

### 7.3. Gửi báo cáo qua Email (chủ động)

Bấm **Gửi báo cáo qua email** → nhập khoảng ngày và địa chỉ người nhận → **Gửi**.

```bash
curl -X POST http://localhost:8080/api/v1/reports \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"start_date":"2026-07-22","end_date":"2026-07-23",
       "recipient_email":"manager@company.com"}'
```

> Field là **`recipient_email`**, không phải `email`.

Email gồm thân HTML (thống kê + bảng top 10) và **đính kèm `.xlsx`** liệt kê uptime từng
server trong kỳ (4 cột: `server_id`, `server_name`, `uptime_pct`, `total_checks`).

Cộng cột `total_checks` trong file đính kèm sẽ đúng bằng `actual_checks` trong thân email —
hai bên cùng bắt nguồn từ `SUM(actual_checks)` trên `daily_snapshots`.

**Ba kết cục, và `state` nói cho bạn biết phải làm gì:**

| `state` | Người nhận có nhận được thư? | Hành động |
|---|---|---|
| `sent` | ✅ chắc chắn | không cần làm gì |
| `failed` | ❌ chắc chắn không | sửa nguyên nhân rồi gửi lại — an toàn |
| `delivery_unknown` | ❓ không ai biết | **tra `smtp_message_id` trong hộp Sent trước**, rồi mới quyết định |

`delivery_unknown` xảy ra khi kết nối đứt **sau** khi thân thư đã lên dây. Hệ thống ghi
nhận sự thật "không biết" và **không bao giờ tự gửi lại** — retry mù có thể gửi hai lần.

Tra lại một job đã gửi:
```bash
curl http://localhost:8080/api/v1/reports/<job_id> -H "Authorization: Bearer $TOKEN"
```

### 7.4. Báo cáo định kỳ tự động

Hai job chạy hằng ngày theo giờ **`Asia/Ho_Chi_Minh`**:

```env
REPORT_SNAPSHOT_CRON = 30 0 * * *   # cô đọng dữ liệu NGÀY HÔM QUA vào daily_snapshots
REPORT_DAILY_CRON    = 0 10 * * *   # gửi báo cáo ngày hôm qua tới REPORT_DAILY_RECIPIENT
```

Thứ tự là bắt buộc: job gửi mail kiểm tra job snapshot đã xong chưa, nên nó không thể đọc
một ngày mà snapshot chưa cô đọng. Khoảng cách 9,5 giờ đủ để chạy lại thủ công nếu job đêm hỏng.

`REPORT_DAILY_RECIPIENT` để rỗng = **không đăng ký job gửi mail** (job snapshot vẫn chạy).

**Khi chạy nhiều replica, chỉ một replica làm việc.** Trọng tài là bảng `cron_runs`: một
dòng cho mỗi cặp `(job_name, run_date)`, và PRIMARY KEY quyết định ai thắng. Tra lịch sử:

```bash
docker exec vcs-sms-postgres psql -U vcs_admin -d report_db -c \
  "SELECT job_name, run_date, state, owner, finished_at, error_message
     FROM cron_runs ORDER BY run_date DESC LIMIT 10;"
```

```text
   job_name   |  run_date  | state |    owner     |          finished_at
--------------+------------+-------+--------------+-------------------------------
 snapshot     | 2026-07-23 | done  | a8326c76734d | 2026-07-24 04:19:41.147619+00
 daily_report | 2026-07-23 | done  | 18f931005802 | 2026-07-24 04:34:46.291984+00
```

`state = done` là điểm dừng — job đó không chạy lại nữa. `state = failed` thì tick kế tiếp
sẽ tự thử lại.

**Chạy lại snapshot của một ngày cụ thể** (endpoint nội bộ, không qua Traefik):

```bash
docker exec vcs-sms-traefik wget -qO- --post-data='' \
  http://report-service:8084/internal/snapshots/2026-07-23
```

---

## 8. Quản lý người dùng & phân quyền

> 🔒 Chỉ **Admin** (scope `user:list`) thấy menu **Người dùng**.

```bash
# Danh sách người dùng
curl "http://localhost:8080/api/v1/auth/users?page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"

# Đổi role
curl -X PUT http://localhost:8080/api/v1/auth/users/<user_id>/role \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"role_name":"operator"}'
```

### Ba role, 13 scope

| Role | Scope |
|---|---|
| **admin** | Tất cả 13 scope |
| **operator** | viewer + `server:create/update/delete/import/export` + `report:send` + `report:view_detail` |
| **viewer** | `server:list`, `server:view`, `server:stats`, `report:view` |

Quan hệ là **bao hàm chặt**: `viewer ⊂ operator ⊂ admin`.

| Scope | Endpoint | viewer | operator | admin |
|---|---|:---:|:---:|:---:|
| `server:list` | `GET /servers` | ✅ | ✅ | ✅ |
| `server:view` | `GET /servers/{id}` | ✅ | ✅ | ✅ |
| `server:stats` | `GET /servers/stats`, `GET /servers/uptime` | ✅ | ✅ | ✅ |
| `report:view` | `GET /reports/summary` | ✅ | ✅ | ✅ |
| `server:create` | `POST /servers` | ❌ | ✅ | ✅ |
| `server:update` | `PUT /servers/{id}` | ❌ | ✅ | ✅ |
| `server:delete` | `DELETE /servers/{id}` | ❌ | ✅ | ✅ |
| `server:import` | `POST /servers/import` | ❌ | ✅ | ✅ |
| `server:export` | `POST /servers/export` | ❌ | ✅ | ✅ |
| `report:send` | `POST /reports` | ❌ | ✅ | ✅ |
| `report:view_detail` | `GET /reports/{id}` | ❌ | ✅ | ✅ |
| `user:list` | `GET /auth/users` | ❌ | ❌ | ✅ |
| `user:manage_role` | `PUT /auth/users/{id}/role` | ❌ | ❌ | ✅ |

Scope ánh xạ **1-1 theo endpoint**, không gộp thành `read`/`write`. Thêm endpoint mới là
thêm scope mới, chứ không nới rộng một scope sẵn có.

> ⚠️ **Admin không thể tự đổi role của chính mình** (`AUTH_CANNOT_CHANGE_OWN_ROLE`) — để
> không có tình huống admin cuối cùng tự hạ quyền và không ai còn quản trị được hệ thống.

> ⚠️ **Đổi role chỉ có hiệu lực sau khi token cũ hết hạn hoặc người dùng đăng nhập lại**,
> vì scope được nhúng thẳng vào JWT lúc login.

---

## 9. Hồ sơ cá nhân

Menu **Hồ sơ** (`/profile`) hiển thị email, họ tên, role và **toàn bộ scope** được cấp.

```bash
curl http://localhost:8080/api/v1/auth/profile -H "Authorization: Bearer $TOKEN"
```

```json
{ "data": {
  "id": "b0000000-0000-0000-0000-000000000001",
  "email": "admin@vcs.com",
  "full_name": "System Administrator",
  "role": "admin",
  "scopes": ["server:create","server:list", … ],
  "is_active": true
}}
```

Đây là cách nhanh nhất để trả lời "vì sao tôi bị 403": so scope trong danh sách này với
bảng ở mục 8.

---

## 10. Tài liệu API (OpenAPI)

Đặc tả nằm ở **`docs/api-spec.yaml`** (OpenAPI 3.0.3).

> **Hệ thống không host Swagger UI.** Không có route `/swagger` nào. Muốn giao diện thử
> nghiệm, mở file spec bằng một trong các cách sau:
>
> ```bash
> # Swagger UI cục bộ
> docker run --rm -p 8081:8080 \
>   -e SWAGGER_JSON=/spec/api-spec.yaml \
>   -v "$(pwd)/docs:/spec" swaggerapi/swagger-ui
> # → http://localhost:8081
> ```
>
> Hoặc dán nội dung vào https://editor.swagger.io, hoặc dùng extension OpenAPI của VS Code.

Khi thử nghiệm, nhớ hai điều: `Authorization: Bearer <access_token>`, và
`Idempotency-Key` cho `POST /servers` + `POST /servers/import`.

---

## 11. Vận hành & chẩn đoán

> ⚠️ Bốn image service Go là **distroless**: không shell, không `wget`, không `curl`. Mọi
> lệnh chẩn đoán phải gọi binary trực tiếp, hoặc đi từ container `traefik` (Alpine).

### 11.1. Bảy chỉ số Prometheus của Monitoring

```bash
docker exec vcs-sms-traefik wget -qO- http://monitor-service:8083/metrics | grep vcs_monitor
```

| Chỉ số | Ý nghĩa | Báo động khi |
|---|---|---|
| `vcs_monitor_round_duration_seconds` | Round bắt đầu → queue cạn | tiến sát 60s |
| `vcs_monitor_targets_expected` | Số target đã nạp vào queue | lệch số server thật |
| `vcs_monitor_checks_completed_total` | Số ping instance này hoàn thành | — |
| **`vcs_monitor_checks_missing`** | **Việc chưa kịp ping lúc round kết thúc** | **> 0 kéo dài → thiếu worker** |
| `vcs_monitor_queue_depth` | Độ sâu queue hiện tại | không về 0 |
| `vcs_monitor_tcp_latency_seconds` | Histogram độ trễ TCP connect | đuôi phân phối tăng |
| `vcs_monitor_es_bulk_failure_total` | Batch bulk bị bỏ sau khi retry | > 0 |

`checks_missing` là **tín hiệu duy nhất** báo thiếu worker. Không có nó, hệ thống ping
thiếu server mà không ai biết — không lỗi, không exception, chỉ là vài server im lặng
không được đo. Cách xử lý: tăng `MONITOR_WORKER_COUNT` hoặc thêm instance.

Số đo thật (10.000 server, 1 instance, 200 goroutine): `round_duration` ≈ 3,5s trong ngân
sách 60s, `checks_missing` = 0.

### 11.2. Các lệnh hay dùng

```bash
# Log
docker compose logs -f monitor-service
docker compose logs -f report-service

# Dựng lại target projection (sau seed, hoặc sau khi Redis mất dữ liệu)
docker compose exec server-service /app/server-service rebuild-monitor-cache

# Soi Redis — db1 cho dữ liệu giám sát, db0 cho auth
docker exec vcs-sms-redis redis-cli -a "$REDIS_PASSWORD" --no-auth-warning -n 1 dbsize
docker exec vcs-sms-redis redis-cli -a "$REDIS_PASSWORD" --no-auth-warning -n 1 \
  hgetall monitor:status:SRV-00001

# Số target đang được giám sát
docker exec vcs-sms-redis redis-cli -a "$REDIS_PASSWORD" --no-auth-warning -n 1 \
  scard server:monitor-target:ids

# Snapshot đã có cho những ngày nào
docker exec vcs-sms-postgres psql -U vcs_admin -d report_db -c \
  "SELECT date, count(*), round(avg(uptime_pct),2) FROM daily_snapshots GROUP BY date ORDER BY date;"

# Chạy toàn bộ test
make test
```

### 11.3. Trước khi lên production

| Việc | Vì sao |
|---|---|
| Đổi `JWT_SECRET` (≥ 32 ký tự) | Secret mặc định là công khai trong `.env.example` |
| Đặt `SMTP_RECIPIENT_DOMAINS` | Để rỗng = hệ thống có thể bị lợi dụng làm mail relay |
| Đổi mật khẩu Postgres/Redis | Mặc định trong `.env.example` là công khai |
| Tắt `POST /api/v1/auth/register` | Nếu không muốn ai cũng tự tạo được tài khoản |
| Siết `SERVER_CIDR_ALLOWLIST` | Càng rộng, hệ thống càng giống công cụ quét cổng nội mạng |
| Xem lại CORS trong `deployments/traefik/dynamic.yml` | Đang cho phép cả một dải LAN |

---

## 12. Xử lý lỗi thường gặp

### 12.1. Lỗi API

| Mã | Nguyên nhân | Cách khắc phục |
|---|---|---|
| `401 Unauthorized` | Token hết hạn / sai / thiếu | Đăng nhập lại hoặc `POST /auth/refresh` |
| `403 COMMON_FORBIDDEN_SCOPE` | Thiếu scope | Xem `GET /auth/profile` rồi so với bảng mục 8 |
| `400 COMMON_VALIDATION_FAILED` — *"Idempotency-Key header is required"* | Thiếu header ở `POST /servers` hoặc `/servers/import` | Thêm `Idempotency-Key: <uuid>` |
| `409 Conflict` | Trùng `server_id`/`server_name`, hoặc cùng `Idempotency-Key` với body khác | Dùng giá trị khác, hoặc key mới |
| `422 SERVER_IP_NOT_ALLOWED` | IPv4 ngoài `SERVER_CIDR_ALLOWLIST` | Sửa IP, hoặc mở rộng allowlist |
| `422 REPORT_INVALID_RANGE` | `end_date` chưa kết thúc, hoặc khoảng > 31 ngày | Chọn ngày đã qua, thu hẹp khoảng |
| `422 REPORT_DATA_UNAVAILABLE` | Có ngày chưa snapshot | Chạy `POST /internal/snapshots/{date}` cho ngày thiếu |
| `422 REPORT_RECIPIENT_BLOCKED` | Domain không nằm trong `SMTP_RECIPIENT_DOMAINS` | Thêm domain, hoặc dùng địa chỉ khác |
| `429 Too Many Requests` | Vượt rate limit của Traefik | Đợi rồi thử lại (auth 10 req/s, còn lại 100 req/s) |

### 12.2. Lỗi vận hành

| Triệu chứng | Nguyên nhân | Cách khắc phục |
|---|---|---|
| **Mọi server đều `UNKNOWN`, không đổi** | Thiếu marker `ready` — target projection chưa dựng | `make rebuild-cache`. Log monitor sẽ ngừng báo `target projection not ready` |
| Log monitor: *"target projection not ready; skipping round"* | Redis bị xoá sạch hoặc chưa từng seed | `make rebuild-cache` |
| `redis-cli` trả 0 key | Đang xem db0, dữ liệu giám sát ở **db1** | Thêm `-n 1` |
| `exec: "wget": executable file not found` | Image service là distroless | Gọi từ `vcs-sms-traefik`, hoặc gọi binary trực tiếp |
| `docker exec vcs-sms-report …` → *No such container* | `report-service` không có `container_name` | Dùng `docker compose exec report-service …` |
| `make seed` báo *"input device is not a TTY"* | Makefile dùng `docker exec -it` | Gọi `docker exec` không kèm `-it` |
| Mọi server `OFF` khi dev | `MONITOR_TCP_DIAL_HOST` chưa trỏ tới `tcp-simulator` | Biến này đã set trong `docker-compose.yml`; kiểm tra tcp-simulator có `healthy` |
| Email không gửi được | Dùng mật khẩu Gmail thường thay vì App Password | Tạo App Password 16 ký tự |
| `coverage_pct` rất thấp | Monitoring không chạy đủ 24 giờ của ngày đó | Đúng như thiết kế — báo cáo đang phơi ra phần nó không đo được |
| Báo cáo thiếu ngày | Job snapshot của ngày đó chưa chạy | `POST /internal/snapshots/{date}`; tra `cron_runs` để biết vì sao |
| Elasticsearch chưa có dữ liệu | Monitor hoặc tcp-simulator chưa sẵn sàng | `docker compose logs monitor-service` |
| Service không start | Port conflict, thiếu `.env`, hoặc `JWT_SECRET` < 32 ký tự | `docker compose logs <service>` |
| `cron_runs` thiếu bảng | DB volume cũ, `init.sql` không chạy lại | Chạy `deployments/docker/postgres/migrate_report_ha.sql` thủ công |
| Trình duyệt chặn request nhưng `curl` vẫn chạy | Lỗi CORS — `curl` không gửi preflight | Kiểm `deployments/traefik/dynamic.yml`; origin của bạn phải nằm trong allowlist |

---

> **Tài liệu liên quan:**
> [Báo cáo mô tả & thiết kế hệ thống](./BaoCao-MoTa-ThietKe-HeThong.md) ·
> [OpenAPI Spec](./api-spec.yaml) ·
> [Kiến trúc](./01-architecture-overview.md) ·
> [Sơ đồ hệ thống](../.claude/diagrams/README.md)
>
> **VCS Server Management System © 2026** — Chương trình đào tạo VCS Passport
