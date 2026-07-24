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

**Argon2id** cho password hash (`m=64MB, t=1, p=4`, salt 16B, key 32B — cột
`password_hash VARCHAR(500)` đủ chỗ cho chuỗi encode `$argon2id$v=19$m=...$salt$hash`).

### Đường di trú từ bcrypt

`VerifyPassword` nhận **cả hai** định dạng:

```text
$2a$… / $2b$…   → bcrypt (legacy)  → khớp thì trả needsRehash = true
$argon2id$…     → Argon2id         → khớp thì so tham số, lệch thì needsRehash = true
```

Khi `needsRehash`, `Login` lặng lẽ hash lại bằng Argon2id và `UpdatePassword` (best-effort:
lỗi ghi không làm hỏng lần đăng nhập). Admin seed trong `init.sql` là bcrypt và trở thành
Argon2id ngay sau lần đăng nhập đầu tiên — không cần migration script nào.

Cùng cơ chế đó xử lý việc **nâng tham số Argon2id** về sau: đổi `currentArgonConfig` là
mọi hash cũ tự nâng cấp dần theo lượt đăng nhập.

### Chống brute-force

```text
auth:login_attempts:{email}    đếm số lần sai · TTL 15 phút · khoá khi ≥ 5
```

Một key duy nhất làm cả hai việc đếm và khoá — không có key `lock` riêng. Đăng nhập thành
công thì `DEL` key này.

Khóa theo **email**, không theo IP: khóa theo IP thì attacker đổi IP là xong, mà lại
chặn nhầm nhiều user thật sau cùng một NAT. Traefik có thêm rate limit theo IP
(`rate-limit-auth`: average 10 / burst 20 req/s cho `/api/v1/auth`;
`rate-limit-global`: average 100 / burst 200 cho phần còn lại) — hai cơ chế bổ sung cho nhau.

### Hai key còn lại trong db0

| Key | Giá trị | TTL | Ghi khi |
|---|---|---|---|
| `auth:refresh:{jti}` | `user_id` | `JWT_REFRESH_EXPIRY_DAYS` (7d) | login + mỗi lần refresh |
| `auth:blacklist:{jti}` | `"revoked"` | thời gian còn lại của access token | logout |

**Refresh token có rotation:** mỗi `POST /auth/refresh` `DEL` jti cũ rồi `SET` jti mới, nên
một refresh token chỉ dùng được đúng một lần. Nếu nó bị đánh cắp và dùng lần thứ hai,
`GET auth:refresh:{jti}` không tìm thấy → `ErrTokenRevoked`.

> ⚠️ `auth:blacklist:{jti}` **được ghi khi logout nhưng chưa được kiểm** ở
> `/internal/verify` — handler đó chỉ validate chữ ký và hạn token. Nghĩa là access token
> đã logout vẫn dùng được cho tới khi hết hạn (tối đa `JWT_ACCESS_EXPIRY_MINUTES` = 15
> phút). Đây là đánh đổi có ý thức: kiểm blacklist ở đó là một lệnh Redis nhân với **toàn
> bộ** lưu lượng hệ thống. Refresh token thì bị thu hồi ngay, nên phiên không kéo dài được.

---

## 7. Các chốt bảo mật khác

### CIDR allowlist

`POST/PUT /servers` và import chỉ chấp nhận IPv4 nằm trong **`SERVER_CIDR_ALLOWLIST`**
(mặc định `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16`). Không có chốt này, hệ thống trở
thành công cụ quét cổng nội mạng: ai cũng tạo được "server" trỏ vào IP bất kỳ rồi đọc
kết quả ON/OFF.

Allowlist rỗng = **chặn tất cả** (fail closed) — service log `Warn` lúc khởi động để
chuyện đó không diễn ra âm thầm.

Vi phạm trả `422` với mã `SERVER_IP_NOT_ALLOWED`. Trong import, nó là lỗi **theo dòng**
(dòng đó vào nhóm `failed`), không phải lỗi cả file.

### Formula injection khi export

Ô Excel bắt đầu bằng `=`, `+`, `-`, `@`, tab hoặc CR bị prefix dấu `'`. Không làm vậy
thì `=cmd|'/c calc'!A1` trong tên server sẽ chạy khi ai đó mở file.

### Idempotency

`POST /servers` và `POST /servers/import` **bắt buộc** header `Idempotency-Key`; thiếu là
`400 COMMON_VALIDATION_FAILED`.

| Tình huống | Kết quả |
|---|---|
| Cùng key + cùng body | Replay response đã lưu, **không** tạo row thứ hai |
| Cùng key + body khác | **409** |
| Đang xử lý | **409** |
| Lần trước thất bại | Key được nhả — sửa input rồi thử lại được |

Khoá là `(actor_id, endpoint, idempotency_key)` với `actor_id` lấy từ header `X-User-Id`,
nên hai user dùng trùng key không đụng nhau. `request_hash` là SHA-256 của thân request,
TTL 24 giờ.

Chi tiết đáng chú ý: chỉ response `2xx` mới được lưu. Khi handler trả lỗi, middleware
`Release` **nhả key** — nếu không, một lần gửi sai input sẽ khoá vĩnh viễn key đó và
người dùng không sửa rồi thử lại được.

`POST /reports` thì **không bắt buộc** `Idempotency-Key`. Nếu client gửi, `SendService`
dùng nó để replay job cũ (và trả `409 REPORT_IDEMPOTENCY` nếu cùng key mà khác khoảng
ngày / người nhận); nếu không gửi, request tạo job mới bình thường. Đó là lý do
`ux_report_jobs_idem` là **partial** unique index với `WHERE idempotency_key <> ''`.

Export **không** có idempotency: nó là thao tác đọc (dùng POST chỉ vì filter dài),
thêm vào chỉ tạo row rác trong `api_idempotency`.

### SMTP recipient allowlist

`SMTP_RECIPIENT_DOMAINS` giới hạn domain được nhận mail. **Để rỗng = cho gửi tới bất
kỳ ai**, tức hệ thống có thể bị lợi dụng làm mail relay. Phải set trước khi lên production.
