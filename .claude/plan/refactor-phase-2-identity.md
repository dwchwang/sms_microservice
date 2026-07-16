# Refactor Phase R2: Identity Service

> **Mục tiêu:** Chuyển Auth Service sang thiết kế mới — tách DB, thêm `/internal/verify`, đổi scopes, Argon2id, brute-force protection.
>
> **Prerequisite:** Phase R1 hoàn tất (infrastructure mới sẵn sàng).
>
> **Kết quả:** Identity Service kết nối `identity_db` riêng, ForwardAuth endpoint hoạt động, scope names mới, Argon2id password hash.

---

## Checklist tổng quan

- [x] **R2.1** Đổi DB connection → `identity_db`
- [x] **R2.2** Sửa model — bỏ schema prefix TableName
- [x] **R2.3** Đổi scope names trong DB seed và code
- [x] **R2.4** Thêm endpoint `/internal/verify` (ForwardAuth)
- [x] **R2.5** Chuyển password hash sang Argon2id
- [x] **R2.6** Thêm brute-force protection (Redis login lockout)
- [x] **R2.7** Sửa cmd/main.go — đổi init, thêm lumberjack
- [x] **R2.8** Thêm endpoint `GET /api/v1/auth/users` và `PUT /api/v1/auth/users/{id}/role`
- [x] **R2.9** Unit test + integration test
- [x] **R2.10** Verify ForwardAuth qua Traefik

---

## R2.1. Đổi DB connection → identity_db

### Bước thực hiện

**R2.1.1.** Sửa `auth-service/config/config.go`:

```go
// Đổi từ:
// DB_HOST, DB_PORT, DB_NAME (vcs_sms), DB_USER (auth_user)
// Sang:
// IDENTITY_DB_HOST, IDENTITY_DB_PORT, IDENTITY_DB_NAME (identity_db), IDENTITY_DB_USER (identity_user)

type DatabaseConfig struct {
    Host     string `env:"IDENTITY_DB_HOST" envDefault:"localhost"`
    Port     int    `env:"IDENTITY_DB_PORT" envDefault:"5432"`
    Name     string `env:"IDENTITY_DB_NAME" envDefault:"identity_db"`
    User     string `env:"IDENTITY_DB_USER" envDefault:"identity_user"`
    Password string `env:"IDENTITY_DB_PASSWORD" envDefault:"identity_pass_secret"`
    SSLMode  string `env:"IDENTITY_DB_SSLMODE" envDefault:"disable"`
}
```

**R2.1.2.** Sửa DSN builder:

```go
func (c DatabaseConfig) DSN() string {
    return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
        c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode)
}
```

### Verify

- [ ] Service kết nối được `identity_db`
- [ ] Không còn tham chiếu đến `vcs_sms` hoặc `auth_schema`

---

## R2.2. Sửa model — Bỏ schema prefix

### Bước thực hiện

**R2.2.1.** Sửa tất cả model trong `auth-service/internal/model/`:

```go
// HIỆN TẠI:
func (Role) TableName() string { return "auth_schema.roles" }
func (RolePermission) TableName() string { return "auth_schema.role_permissions" }
func (User) TableName() string { return "auth_schema.users" }

// MỚI:
func (Role) TableName() string { return "roles" }
func (RolePermission) TableName() string { return "role_permissions" }
func (User) TableName() string { return "users" }
```

**R2.2.2.** Bỏ field `username` nếu thiết kế mới chỉ dùng `email`:
- Thiết kế mới: `users` có `email` unique, không có `username`
- Nếu hiện tại có `username` → giữ lại hoặc bỏ tùy tình huống (kiểm tra login flow)

### Verify

- [ ] GORM AutoMigrate hoặc query chạy đúng bảng trong `identity_db`
- [ ] Không còn `auth_schema.` prefix

---

## R2.3. Đổi scope names

### Bước thực hiện

**R2.3.1.** Cập nhật scope constants trong code:

```go
// HIỆN TẠI:
// "server:read" → "server:list" + "server:view"
// "user:manage" → "user:list" + "user:manage_role"
// "monitor:view" → BỎ (Monitor không có public endpoint)

// MỚI:
const (
    ScopeServerCreate    = "server:create"
    ScopeServerList      = "server:list"      // mới (thay server:read)
    ScopeServerView      = "server:view"      // mới (thay server:read)
    ScopeServerUpdate    = "server:update"
    ScopeServerDelete    = "server:delete"
    ScopeServerImport    = "server:import"
    ScopeServerExport    = "server:export"
    ScopeServerStats     = "server:stats"     // mới
    ScopeReportView      = "report:view"
    ScopeReportSend      = "report:send"
    ScopeReportViewDetail= "report:view_detail" // mới
    ScopeUserList        = "user:list"        // mới (thay user:manage)
    ScopeUserManageRole  = "user:manage_role" // mới (thay user:manage)
)
```

**R2.3.2.** Seed data đã được cập nhật trong R1 (`init-v2.sql`), verify đúng.

**R2.3.3.** Sửa JWT token generation — đảm bảo scope snapshot trong JWT dùng tên scope mới.

### Verify

- [ ] JWT token chứa scope names mới
- [ ] Role-permission mapping đúng (viewer/operator/admin)

---

## R2.4. Thêm `/internal/verify` endpoint

### Bước thực hiện

**R2.4.1.** Tạo file `auth-service/internal/handler/verify_handler.go`:

```go
package handler

import (
    "net/http"
    "strings"
    "github.com/gin-gonic/gin"
)

// VerifyHandler handles ForwardAuth requests from Traefik.
type VerifyHandler struct {
    jwtService JWTService // interface để verify JWT
}

func NewVerifyHandler(jwtService JWTService) *VerifyHandler {
    return &VerifyHandler{jwtService: jwtService}
}

// Verify handles GET /internal/verify
// Traefik calls this endpoint for ForwardAuth.
// Returns 200 with user headers if JWT is valid, 401 otherwise.
func (h *VerifyHandler) Verify(c *gin.Context) {
    // 1. Xóa header giả mạo từ client (phòng trường hợp client gửi trực tiếp)
    c.Request.Header.Del("X-User-Id")
    c.Request.Header.Del("X-User-Scopes")
    c.Request.Header.Del("X-User-Email")

    // 2. Lấy JWT từ Authorization header
    authHeader := c.GetHeader("Authorization")
    if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
        c.AbortWithStatus(http.StatusUnauthorized)
        return
    }
    token := strings.TrimPrefix(authHeader, "Bearer ")

    // 3. Verify JWT
    claims, err := h.jwtService.ValidateToken(token)
    if err != nil {
        c.AbortWithStatus(http.StatusUnauthorized)
        return
    }

    // 4. Set response headers — Traefik sẽ copy sang request forward
    c.Header("X-User-Id", claims.UserID)
    c.Header("X-User-Scopes", strings.Join(claims.Scopes, ","))
    c.Header("X-User-Email", claims.Email)

    c.Status(http.StatusOK)
}
```

**R2.4.2.** Đăng ký route trong `cmd/main.go`:

```go
// Internal routes (không qua Traefik public entrypoint)
internalGroup := router.Group("/internal")
{
    internalGroup.GET("/verify", verifyHandler.Verify)
}
```

**R2.4.3.** Đảm bảo route `/internal/verify` KHÔNG nằm trong Traefik router (chỉ nội bộ Docker network).

### Verify

- [ ] `GET /internal/verify` với valid JWT → 200 + headers X-User-Id, X-User-Scopes
- [ ] `GET /internal/verify` với invalid/expired JWT → 401
- [ ] `GET /internal/verify` không có Authorization header → 401
- [ ] Headers giả mạo X-User-Id bị xóa trước khi verify

---

## R2.5. Chuyển password hash sang Argon2id

### Bước thực hiện

**R2.5.1.** Thêm dependency:

```bash
cd auth-service
go get golang.org/x/crypto
```

**R2.5.2.** Tạo password hasher:

```go
// auth-service/internal/service/password.go

package service

import (
    "crypto/rand"
    "encoding/base64"
    "fmt"
    "strings"

    "golang.org/x/crypto/argon2"
    "golang.org/x/crypto/bcrypt"
)

type PasswordHasher interface {
    Hash(password string) (string, error)
    Verify(password, hash string) (bool, error)
}

type argon2Hasher struct {
    time    uint32
    memory  uint32
    threads uint8
    keyLen  uint32
    saltLen uint32
}

func NewArgon2Hasher() PasswordHasher {
    return &argon2Hasher{
        time:    1,
        memory:  64 * 1024, // 64MB
        threads: 4,
        keyLen:  32,
        saltLen: 16,
    }
}

func (h *argon2Hasher) Hash(password string) (string, error) {
    salt := make([]byte, h.saltLen)
    if _, err := rand.Read(salt); err != nil {
        return "", err
    }

    hash := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.threads, h.keyLen)

    return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
        h.memory, h.time, h.threads,
        base64.RawStdEncoding.EncodeToString(salt),
        base64.RawStdEncoding.EncodeToString(hash),
    ), nil
}

func (h *argon2Hasher) Verify(password, encodedHash string) (bool, error) {
    // Backward compatible: nếu hash bắt đầu bằng $2a$ → bcrypt
    if strings.HasPrefix(encodedHash, "$2a$") || strings.HasPrefix(encodedHash, "$2b$") {
        err := bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(password))
        return err == nil, nil
    }

    // Parse argon2id hash
    // Format: $argon2id$v=19$m=65536,t=1,p=4$salt$hash
    parts := strings.Split(encodedHash, "$")
    if len(parts) != 6 { return false, fmt.Errorf("invalid hash format") }

    var memory uint32
    var time uint32
    var threads uint8
    fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)

    salt, _ := base64.RawStdEncoding.DecodeString(parts[4])
    expectedHash, _ := base64.RawStdEncoding.DecodeString(parts[5])

    computedHash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expectedHash)))

    // Constant-time comparison
    if len(computedHash) != len(expectedHash) { return false, nil }
    var diff byte
    for i := range computedHash {
        diff |= computedHash[i] ^ expectedHash[i]
    }
    return diff == 0, nil
}
```

**Lưu ý quan trọng:** Hàm `Verify` backward compatible — vẫn verify được bcrypt hash cũ. User đăng nhập với password cũ (bcrypt) → thành công → hệ thống có thể tự re-hash sang Argon2id.

### Verify

- [ ] Tạo password mới → Argon2id hash
- [ ] Login với password bcrypt cũ → vẫn thành công (backward compatible)
- [ ] Login sai → thất bại
- [ ] Unit test cho cả Argon2id và bcrypt backward compat

---

## R2.6. Thêm brute-force protection

### Bước thực hiện

**R2.6.1.** Tạo `auth-service/internal/service/login_guard.go`:

```go
package service

import (
    "context"
    "fmt"
    "time"
    "github.com/redis/go-redis/v9"
)

type LoginGuard struct {
    rdb          *redis.Client
    maxAttempts  int           // default 5
    windowSec    int           // default 900 (15 min)
    lockDuration time.Duration // default 15 min
}

func NewLoginGuard(rdb *redis.Client) *LoginGuard {
    return &LoginGuard{
        rdb:          rdb,
        maxAttempts:  5,
        windowSec:    900,
        lockDuration: 15 * time.Minute,
    }
}

// IsLocked checks if account is temporarily locked
func (g *LoginGuard) IsLocked(ctx context.Context, email string) (bool, error) {
    lockKey := fmt.Sprintf("auth:login-lock:%s", email)
    exists, err := g.rdb.Exists(ctx, lockKey).Result()
    return exists > 0, err
}

// RecordFailure records a failed login attempt
func (g *LoginGuard) RecordFailure(ctx context.Context, email string) error {
    failKey := fmt.Sprintf("auth:login-fail:%s", email)
    lockKey := fmt.Sprintf("auth:login-lock:%s", email)

    pipe := g.rdb.Pipeline()
    incr := pipe.Incr(ctx, failKey)
    pipe.Expire(ctx, failKey, time.Duration(g.windowSec)*time.Second)
    _, err := pipe.Exec(ctx)
    if err != nil { return err }

    if incr.Val() >= int64(g.maxAttempts) {
        g.rdb.Set(ctx, lockKey, "1", g.lockDuration)
        g.rdb.Del(ctx, failKey)
    }
    return nil
}

// ClearFailures clears failure count after successful login
func (g *LoginGuard) ClearFailures(ctx context.Context, email string) {
    failKey := fmt.Sprintf("auth:login-fail:%s", email)
    g.rdb.Del(ctx, failKey)
}
```

**R2.6.2.** Integrate vào login flow:

```go
// Trong auth service login handler:
// 1. Check IsLocked → 423 AUTH_ACCOUNT_LOCKED
// 2. Verify credentials
// 3. Nếu sai → RecordFailure
// 4. Nếu đúng → ClearFailures → issue JWT
```

### Verify

- [ ] 5 login sai liên tiếp → account locked 15 phút
- [ ] Login đúng sau lock → vẫn bị từ chối (423)
- [ ] Login đúng trước khi bị lock → thành công + clear counter

---

## R2.7. Sửa cmd/main.go

### Bước thực hiện

- Đổi DB init → `identity_db`
- Thêm lumberjack logger
- Đăng ký `/internal/verify` route
- Bỏ Kafka consumer (nếu có)
- Thêm LoginGuard vào dependency injection

---

## R2.8-R2.10. Endpoints mới + Tests

**R2.8.** Thêm `GET /api/v1/auth/users` (scope `user:list`) và `PUT /api/v1/auth/users/{id}/role` (scope `user:manage_role`).

**R2.9.** Unit test cho:
- `/internal/verify` — valid/invalid JWT
- Argon2id + bcrypt backward compat
- Login lockout (5 failures → lock)
- Scope check middleware

**R2.10.** Integration test:
- Client → Traefik → ForwardAuth → Identity → verify → copy headers → Server Service

### Rollback plan

1. Revert model TableName về `auth_schema.*`
2. Revert config DB connection
3. Revert scope names
