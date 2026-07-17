# 05 — Bảo mật: JWT, ForwardAuth, RBAC

---

## 1. Hai tầng, hai câu hỏi khác nhau

```text
Traefik ForwardAuth  → "Token này có hợp lệ không?"        (authentication)
RequireScope trong service → "User này có quyền làm việc này không?"  (authorization)
```

Hai tầng này **bắt buộc phải có cả hai**. ForwardAuth chỉ chứng minh JWT hợp lệ; nó
không thể biết endpoint nào cần scope nào. Nếu service không tự kiểm scope thì một
token `viewer` hợp lệ sẽ xóa được server.

---

## 2. Luồng ForwardAuth

```text
Client ──Bearer JWT──► Traefik
                          │
                          ├─► GET auth-service /internal/verify
                          │      validate chữ ký + hạn JWT
                          │      200 + header:
                          │        X-User-Id, X-User-Email, X-User-Role, X-User-Scopes
                          │      401 nếu token sai/hết hạn
                          │
                          └─► forward request kèm các header đó xuống service
```

Traefik **xóa header rồi mới set lại** từ response của `/internal/verify`. Nhờ vậy
client tự gửi `X-User-Scopes: server:delete` cũng vô nghĩa — nó bị ghi đè.

> ⚠️ Điều này **chỉ đúng khi Traefik là lối vào duy nhất**. Nếu service publish port
> ra host, client gọi thẳng vào đó sẽ tự đặt được header và bỏ qua toàn bộ xác thực.
> Vì vậy trong `docker-compose.yml` bốn service dùng `expose:` chứ **không** `ports:`
> — chỉ Traefik (8080) và web (3000) ra tới host.

---

## 3. CORS phải đứng trước ForwardAuth

Web ở `:3000` gọi API ở `:8080` là **cross-origin** → trình duyệt gửi preflight `OPTIONS`
trước mỗi request. Preflight **không mang `Authorization`**, nên nếu ForwardAuth chạy
trước thì nó trả 401 và trình duyệt chặn **mọi** request, kể cả login.

Vì vậy trong `dynamic.yml`, chuỗi middleware là `cors` → `forward-auth` → `rate-limit`.
Traefik trả lời preflight ngay tại `cors`, không forward xuống service.

Hai header dễ quên:

| Header | Vì sao cần |
|---|---|
| `Idempotency-Key` trong `accessControlAllowHeaders` | FE gửi nó ở `POST /servers` và `/servers/import`; thiếu thì preflight từ chối |
| `Content-Disposition` trong `accessControlExposeHeaders` | Export cần đọc tên file; không expose thì JS không thấy header |

> `curl` **không** gửi preflight, nên verify bằng curl sẽ xanh trong khi trình duyệt hỏng
> hoàn toàn. Muốn kiểm CORS phải gửi `OPTIONS` kèm `Origin` và `Access-Control-Request-Method`.

---

## 4. Enforce scope trong service

```go
servers := r.Group("/api/v1/servers", middleware.AuthFromForwardAuth())
servers.DELETE("/:server_id", middleware.RequireScope("server:delete"), handler.DeleteServer)
```

`AuthFromForwardAuth` đọc `X-User-Id` / `X-User-Scopes` (401 nếu thiếu), `RequireScope`
đối chiếu scope yêu cầu (403 nếu thiếu).

### auth-service là ngoại lệ

Router `/api/v1/auth` **không** đi qua ForwardAuth — nếu có thì không ai login được.
Vì thế auth-service **tự validate JWT** bằng `sharedjwt.ValidateToken` và đọc scope
**từ claims trong token**, không tin header nào cả.

---

## 5. Bảng phân quyền

### Vai trò

| Role | Scope |
|---|---|
| `admin` | Tất cả |
| `operator` | Viewer + create/update/delete/import/export + `report:send` + `report:view_detail` |
| `viewer` | `server:list`, `server:view`, `server:stats`, `report:view` |

### Endpoint → scope

| Method | Path | Scope |
|---|---|---|
| POST | `/api/v1/servers` | `server:create` |
| GET | `/api/v1/servers` | `server:list` |
| GET | `/api/v1/servers/{id}` | `server:view` |
| PUT | `/api/v1/servers/{id}` | `server:update` |
| DELETE | `/api/v1/servers/{id}` | `server:delete` |
| POST | `/api/v1/servers/import` | `server:import` |
| POST | `/api/v1/servers/export` | `server:export` |
| GET | `/api/v1/servers/stats` | `server:stats` |
| GET | `/api/v1/servers/uptime` | `server:stats` |
| GET | `/api/v1/reports/summary` | `report:view` |
| POST | `/api/v1/reports` | `report:send` |
| GET | `/api/v1/reports/{id}` | `report:view_detail` |
| GET | `/api/v1/auth/users` | `user:list` |
| PUT | `/api/v1/auth/users/{id}/role` | `user:manage_role` |
| GET | `/api/v1/auth/profile` | Chỉ cần đăng nhập |
| POST | `/api/v1/auth/login` \| `/register` \| `/refresh` | Public |
| GET | `/internal/verify` | Chỉ ForwardAuth |
| GET | `/internal/servers` | Chỉ network nội bộ |

---

## 6. Mật khẩu và chống brute-force

**Argon2id** cho password hash (cột `password_hash VARCHAR(500)` đủ chỗ).

```text
auth:login-fail:{email}    đếm số lần sai trong cửa sổ 15 phút
auth:login-lock:{email}    khóa tạm account
```

Khóa theo **email**, không theo IP: khóa theo IP thì attacker đổi IP là xong, mà lại
chặn nhầm nhiều user thật sau cùng một NAT. Traefik có thêm rate limit theo IP
(auth endpoint: 10 req/s, còn lại 100 req/s) — hai cơ chế bổ sung cho nhau.

---

## 7. Các chốt bảo mật khác

### CIDR allowlist

`POST/PUT /servers` và import chỉ chấp nhận IPv4 nằm trong `SERVER_ALLOWED_CIDRS`.
Không có chốt này, hệ thống trở thành công cụ quét cổng nội mạng: ai cũng tạo được
"server" trỏ vào IP bất kỳ rồi đọc kết quả ON/OFF.

Allowlist rỗng = **chặn tất cả** (fail closed).

### Formula injection khi export

Ô Excel bắt đầu bằng `=`, `+`, `-`, `@`, tab hoặc CR bị prefix dấu `'`. Không làm vậy
thì `=cmd|'/c calc'!A1` trong tên server sẽ chạy khi ai đó mở file.

### Idempotency

`POST /servers` và `POST /servers/import` bắt buộc header `Idempotency-Key`.

| Tình huống | Kết quả |
|---|---|
| Cùng key + cùng body | Replay response đã lưu, **không** tạo row thứ hai |
| Cùng key + body khác | **409** |
| Đang xử lý | **409** |
| Lần trước thất bại | Key được nhả — sửa input rồi thử lại được |

Export **không** có idempotency: nó là thao tác đọc (dùng POST chỉ vì filter dài),
thêm vào chỉ tạo row rác trong `api_idempotency`.

### SMTP recipient allowlist

`SMTP_RECIPIENT_DOMAINS` giới hạn domain được nhận mail. **Để rỗng = cho gửi tới bất
kỳ ai**, tức hệ thống có thể bị lợi dụng làm mail relay. Phải set trước khi lên production.
