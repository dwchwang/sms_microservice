# Refactor Phase R1: Infrastructure & Shared Module

> **Mục tiêu:** Chuẩn bị hạ tầng cho toàn bộ quá trình refactor — tách database, thay Kafka bằng Redis Stream, thay API Gateway bằng Traefik, sửa shared module.
>
> **Prerequisite:** Hệ thống hiện tại đang chạy ổn.
>
> **Kết quả:** Infrastructure mới sẵn sàng, shared module đã cập nhật, các service cũ vẫn chạy được (backward compatible trong quá trình chuyển đổi).

---

## Checklist tổng quan

- [x] **R1.1** Tạo 3 database riêng trong PostgreSQL
- [x] **R1.2** Viết lại `init.sql` cho thiết kế mới
- [x] **R1.3** Xóa shared/kafka module
- [x] **R1.4** Sửa shared/response — cập nhật response envelope
- [x] **R1.5** Sửa shared/errors — thêm error codes mới
- [x] **R1.6** Sửa shared/middleware — đổi cách đọc auth header (ForwardAuth)
- [x] **R1.7** Sửa shared/logger — thêm lumberjack integration
- [x] **R1.8** Tạo Traefik config files
- [x] **R1.9** Sửa docker-compose.yml — bỏ Kafka, thêm Traefik
- [x] **R1.10** Sửa .env / .env.example
- [x] **R1.11** Sửa Makefile
- [x] **R1.12** Verify infrastructure chạy OK

---

## R1.1. Tạo 3 Database riêng trong PostgreSQL

### Bước thực hiện

**R1.1.1.** Tạo file init SQL mới — chưa xóa file cũ, tạo file mới song song:

**File MỚI:** `deployments/docker/postgres/init-v2.sql`

```sql
-- ============================================================
-- VCS-SMS Database Initialization — v2 (New Design)
-- 3 separate databases + DB users
-- ============================================================

-- ============================
-- Tạo 3 databases
-- ============================
CREATE DATABASE identity_db;
CREATE DATABASE server_db;
CREATE DATABASE report_db;

-- ============================
-- Tạo DB users
-- ============================
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'identity_user') THEN
        CREATE USER identity_user WITH PASSWORD 'identity_pass_secret';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'server_user_v2') THEN
        CREATE USER server_user_v2 WITH PASSWORD 'server_pass_secret_v2';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'report_user_v2') THEN
        CREATE USER report_user_v2 WITH PASSWORD 'report_pass_secret_v2';
    END IF;
END
$$;

-- ============================
-- Grant permissions
-- ============================
GRANT ALL PRIVILEGES ON DATABASE identity_db TO identity_user;
GRANT ALL PRIVILEGES ON DATABASE server_db TO server_user_v2;
GRANT ALL PRIVILEGES ON DATABASE report_db TO report_user_v2;

-- ============================================================
-- identity_db tables
-- ============================================================
\c identity_db;

-- Grant schema usage
GRANT ALL ON SCHEMA public TO identity_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO identity_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO identity_user;

CREATE TABLE IF NOT EXISTS roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(50)   NOT NULL UNIQUE,
    description     TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope           VARCHAR(100)  NOT NULL UNIQUE,
    description     TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS role_permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id         UUID          NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    scope           VARCHAR(100)  NOT NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    UNIQUE(role_id, scope)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_role_id ON role_permissions(role_id);

CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           VARCHAR(255)  NOT NULL UNIQUE,
    password_hash   VARCHAR(500)  NOT NULL,  -- Argon2id produces longer hashes
    full_name       VARCHAR(255),
    role_id         UUID          NOT NULL REFERENCES roles(id),
    is_active       BOOLEAN       NOT NULL DEFAULT TRUE,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_role_id ON users(role_id);

-- Seed roles (thiết kế mới: viewer, operator, admin)
INSERT INTO roles (id, name, description) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'admin',    'Full access to all resources'),
    ('a0000000-0000-0000-0000-000000000002', 'operator', 'Can operate servers and reports'),
    ('a0000000-0000-0000-0000-000000000003', 'viewer',   'Read-only access')
ON CONFLICT (name) DO NOTHING;

-- Seed scopes MỚI (1:1 theo endpoint)
INSERT INTO permissions (scope, description) VALUES
    ('server:create',     'Create server'),
    ('server:list',       'List servers'),
    ('server:view',       'View server detail'),
    ('server:update',     'Update server'),
    ('server:delete',     'Delete server'),
    ('server:import',     'Import servers from Excel'),
    ('server:export',     'Export servers to Excel'),
    ('server:stats',      'View server stats'),
    ('report:view',       'View report summary'),
    ('report:send',       'Send report email'),
    ('report:view_detail','View report detail'),
    ('user:list',         'List users'),
    ('user:manage_role',  'Change user role')
ON CONFLICT (scope) DO NOTHING;

-- Seed role_permissions
-- Admin: tất cả scopes
INSERT INTO role_permissions (role_id, scope)
SELECT 'a0000000-0000-0000-0000-000000000001', scope FROM permissions
ON CONFLICT (role_id, scope) DO NOTHING;

-- Operator: Viewer + create/update/delete/import/export + report:send + report:view_detail
INSERT INTO role_permissions (role_id, scope) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'server:create'),
    ('a0000000-0000-0000-0000-000000000002', 'server:list'),
    ('a0000000-0000-0000-0000-000000000002', 'server:view'),
    ('a0000000-0000-0000-0000-000000000002', 'server:update'),
    ('a0000000-0000-0000-0000-000000000002', 'server:delete'),
    ('a0000000-0000-0000-0000-000000000002', 'server:import'),
    ('a0000000-0000-0000-0000-000000000002', 'server:export'),
    ('a0000000-0000-0000-0000-000000000002', 'server:stats'),
    ('a0000000-0000-0000-0000-000000000002', 'report:view'),
    ('a0000000-0000-0000-0000-000000000002', 'report:send'),
    ('a0000000-0000-0000-0000-000000000002', 'report:view_detail')
ON CONFLICT (role_id, scope) DO NOTHING;

-- Viewer: list + view + stats + report:view
INSERT INTO role_permissions (role_id, scope) VALUES
    ('a0000000-0000-0000-0000-000000000003', 'server:list'),
    ('a0000000-0000-0000-0000-000000000003', 'server:view'),
    ('a0000000-0000-0000-0000-000000000003', 'server:stats'),
    ('a0000000-0000-0000-0000-000000000003', 'report:view')
ON CONFLICT (role_id, scope) DO NOTHING;

-- Seed default admin (password: Admin@123456 — Argon2id hash)
-- Tạm giữ bcrypt cho đến khi Identity service chuyển sang Argon2id
INSERT INTO users (id, email, password_hash, full_name, role_id, is_active)
VALUES (
    'b0000000-0000-0000-0000-000000000001',
    'admin@vcs.com',
    '$2a$10$95QUyF2JLw7SJwUBrw80BO1BipqhRz7iQQF/TUlga.Z/ohFK9UlOi',
    'System Administrator',
    'a0000000-0000-0000-0000-000000000001',
    TRUE
) ON CONFLICT (email) DO NOTHING;


-- ============================================================
-- server_db tables
-- ============================================================
\c server_db;

GRANT ALL ON SCHEMA public TO server_user_v2;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO server_user_v2;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO server_user_v2;

CREATE TABLE IF NOT EXISTS servers (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id            VARCHAR(100)  NOT NULL,
    server_name          VARCHAR(255)  NOT NULL,
    status               VARCHAR(20)   NOT NULL DEFAULT 'UNKNOWN'
                         CHECK (status IN ('ON', 'OFF', 'UNKNOWN')),
    status_changed_at    TIMESTAMPTZ,
    status_version       BIGINT        NOT NULL DEFAULT 0,
    last_status_event_id VARCHAR(255),
    ipv4                 INET          NOT NULL,
    tcp_port             INT           NOT NULL DEFAULT 80
                         CHECK (tcp_port BETWEEN 1 AND 65535),
    os                   VARCHAR(100),
    cpu_cores            INT           CHECK (cpu_cores IS NULL OR cpu_cores > 0),
    ram_gb               INT           CHECK (ram_gb IS NULL OR ram_gb > 0),
    disk_gb              INT           CHECK (disk_gb IS NULL OR disk_gb > 0),
    location             VARCHAR(255),
    description          TEXT,
    created_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ
);

-- server_id unique toàn cục (kể cả deleted — bảo vệ lịch sử)
CREATE UNIQUE INDEX IF NOT EXISTS ux_servers_server_id
    ON servers (server_id);

-- server_name unique chỉ trên server active (cho phép tên trùng với server đã xóa)
CREATE UNIQUE INDEX IF NOT EXISTS ux_servers_active_name
    ON servers (server_name) WHERE deleted_at IS NULL;

-- Index phục vụ filter và GET /servers/stats
CREATE INDEX IF NOT EXISTS ix_servers_status
    ON servers (status) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS ix_servers_created_at
    ON servers (created_at) WHERE deleted_at IS NULL;

-- Idempotency table
CREATE TABLE IF NOT EXISTS api_idempotency (
    actor_id         VARCHAR(255)  NOT NULL,
    endpoint         VARCHAR(255)  NOT NULL,
    idempotency_key  VARCHAR(255)  NOT NULL,
    request_hash     VARCHAR(64)   NOT NULL,
    state            VARCHAR(20)   NOT NULL DEFAULT 'processing'
                     CHECK (state IN ('processing', 'completed', 'failed')),
    status_code      INT,
    response_body    JSONB,
    expires_at       TIMESTAMPTZ   NOT NULL,
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (actor_id, endpoint, idempotency_key)
);

CREATE INDEX IF NOT EXISTS ix_idempotency_expires
    ON api_idempotency (expires_at);


-- ============================================================
-- report_db tables
-- ============================================================
\c report_db;

GRANT ALL ON SCHEMA public TO report_user_v2;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO report_user_v2;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO report_user_v2;

CREATE TABLE IF NOT EXISTS report_jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_type       VARCHAR(20)   NOT NULL CHECK (report_type IN ('daily', 'on-demand')),
    requester_id      VARCHAR(255),
    idempotency_key   VARCHAR(255),
    start_at          DATE          NOT NULL,
    end_at            DATE          NOT NULL,
    recipient_email   VARCHAR(255)  NOT NULL,
    state             VARCHAR(30)   NOT NULL DEFAULT 'processing'
                      CHECK (state IN ('processing','generated','sending','sent','failed','delivery_unknown')),
    response_json     JSONB,
    smtp_message_id   VARCHAR(255),
    error_message     TEXT,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    sent_at           TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS ix_report_jobs_state ON report_jobs(state);
CREATE INDEX IF NOT EXISTS ix_report_jobs_created ON report_jobs(created_at);
CREATE INDEX IF NOT EXISTS ix_report_jobs_type ON report_jobs(report_type);

CREATE TABLE IF NOT EXISTS daily_snapshots (
    server_id        VARCHAR(100)  NOT NULL,
    date             DATE          NOT NULL,
    server_name      VARCHAR(255)  NOT NULL,
    on_checks        INT           NOT NULL DEFAULT 0,
    actual_checks    INT           NOT NULL DEFAULT 0,
    expected_checks  INT           NOT NULL DEFAULT 0,
    uptime_pct       NUMERIC(5,2),          -- NULL khi actual_checks = 0 (no_data)
    last_status      VARCHAR(10),           -- ON/OFF cuối ngày; NULL khi no_data
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (server_id, date)
);

CREATE INDEX IF NOT EXISTS ix_daily_snapshots_date_uptime
    ON daily_snapshots (date, uptime_pct);
```

**R1.1.2.** Cập nhật `docker-compose.yml` — postgres init volume mount:

```yaml
# Đổi từ:
# - ./deployments/docker/postgres/init.sql:/docker-entrypoint-initdb.d/01-init.sql
# Sang:
- ./deployments/docker/postgres/init-v2.sql:/docker-entrypoint-initdb.d/01-init-v2.sql
```

**R1.1.3.** Xóa volume postgres cũ và tạo lại:

```bash
docker compose down -v  # xóa volume cũ
docker compose up postgres -d  # tạo lại với schema mới
# Verify:
docker exec vcs-sms-postgres psql -U vcs_admin -l  # phải thấy identity_db, server_db, report_db
```

### Verify

- [ ] 3 database `identity_db`, `server_db`, `report_db` tồn tại
- [ ] Mỗi database có đúng bảng theo thiết kế
- [ ] Users `identity_user`, `server_user_v2`, `report_user_v2` có thể connect vào DB tương ứng
- [ ] Seed data (roles, permissions, admin user) tồn tại trong `identity_db`
- [ ] Bảng `servers` trong `server_db` có cột `tcp_port`, `status_version`, `status_changed_at`
- [ ] Bảng `daily_snapshots` trong `report_db` có PK composite `(server_id, date)`

---

## R1.2. (Bỏ qua — đã bao gồm trong R1.1)

---

## R1.3. Xóa shared/kafka module

### Bước thực hiện

**R1.3.1.** Xóa toàn bộ thư mục `shared/kafka/`:

```bash
rm -rf shared/kafka/
```

**R1.3.2.** Sửa `shared/go.mod` — bỏ dependency `github.com/segmentio/kafka-go`:

```bash
cd shared
# Xóa dòng require github.com/segmentio/kafka-go
# Chạy go mod tidy
go mod tidy
```

**R1.3.3.** Tạm thời: các service vẫn import `shared/kafka` → sẽ bị compile error. Đây là **có chủ đích** — buộc mỗi service phải sửa import trong phase tương ứng.

**Cách xử lý tạm:** Tạo file stub `shared/kafka/stub.go` nếu muốn giữ hệ thống compile được:

```go
// shared/kafka/stub.go — TEMPORARY: will be removed in R7
package kafka

import "context"

type Event struct {
    EventID   string      `json:"event_id"`
    EventType string      `json:"event_type"`
    Timestamp string      `json:"timestamp"`
    Source    string      `json:"source"`
    Data      interface{} `json:"data"`
}

type Producer interface {
    Publish(ctx context.Context, topic string, key string, value interface{}) error
    Close() error
}

type Consumer interface {
    Subscribe(topic, groupID string, handler EventHandler) error
    Start(ctx context.Context) error
    Close() error
}

type EventHandler func(ctx context.Context, event *Event) error

// NoopProducer is a stub that does nothing — used during transition
type NoopProducer struct{}

func (n *NoopProducer) Publish(ctx context.Context, topic string, key string, value interface{}) error {
    return nil // noop
}
func (n *NoopProducer) Close() error { return nil }
```

### Verify

- [ ] `shared/kafka/` đã được xóa hoặc chỉ còn stub
- [ ] `shared/go.mod` không còn `segmentio/kafka-go`
- [ ] `go mod tidy` chạy OK trong `shared/`

---

## R1.4. Sửa shared/response — Cập nhật response envelope

### Bước thực hiện

**R1.4.1.** Sửa `shared/response/` để response envelope đúng format thiết kế mới:

```go
// shared/response/response.go

package response

import (
    "time"
    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
)

// ApiResponse — success envelope
type ApiResponse struct {
    Status  string      `json:"status"`
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
    Meta    *Meta       `json:"meta"`
}

// ApiErrorResponse — error envelope
type ApiErrorResponse struct {
    Status  string       `json:"status"`
    Code    string       `json:"code"`       // error code string, e.g. "SERVER_VALIDATION_FAILED"
    Message string       `json:"message"`
    Errors  []FieldError `json:"errors,omitempty"`
    Meta    *Meta        `json:"meta"`
}

type Meta struct {
    RequestID  string `json:"request_id"`
    Timestamp  string `json:"timestamp"`
    DurationMs *int64 `json:"duration_ms,omitempty"` // cho import response
}

type FieldError struct {
    Field   string `json:"field"`
    Code    string `json:"code"`
    Message string `json:"message"`
}

func newMeta(c *gin.Context) *Meta {
    reqID := c.GetString("request_id")
    if reqID == "" {
        reqID = uuid.New().String()
    }
    return &Meta{
        RequestID: reqID,
        Timestamp: time.Now().UTC().Format(time.RFC3339),
    }
}

func Success(c *gin.Context, httpCode int, message string, data interface{}) {
    c.JSON(httpCode, ApiResponse{
        Status:  "success",
        Code:    httpCode,
        Message: message,
        Data:    data,
        Meta:    newMeta(c),
    })
}

func ErrorWithCode(c *gin.Context, httpCode int, errorCode string, message string, fieldErrors ...FieldError) {
    c.JSON(httpCode, ApiErrorResponse{
        Status:  "error",
        Code:    errorCode,
        Message: message,
        Errors:  fieldErrors,
        Meta:    newMeta(c),
    })
}
```

**R1.4.2.** Đảm bảo backward compatible — giữ các hàm cũ (`Error()`, `Conflict()`, `NotFound()`, v.v.) nhưng chuyển nội bộ sang dùng `ErrorWithCode()`.

### Verify

- [ ] `shared/response` compile OK
- [ ] Các hàm cũ vẫn hoạt động (backward compatible)
- [ ] Response format đúng thiết kế mới (có `meta.request_id`, `meta.timestamp`)

---

## R1.5. Sửa shared/errors — Thêm error codes mới

### Bước thực hiện

**R1.5.1.** Thêm error codes vào `shared/errors/`:

```go
// shared/errors/codes.go

package errors

// Error codes theo thiết kế mới (section 13.3 design.md)
const (
    // Common
    CodeValidationFailed  = "COMMON_VALIDATION_FAILED"
    CodeUnauthorized      = "COMMON_UNAUTHORIZED"
    CodeForbiddenScope    = "COMMON_FORBIDDEN_SCOPE"
    CodeNotFound          = "COMMON_NOT_FOUND"
    CodeRateLimited       = "COMMON_RATE_LIMITED"
    CodeInternalError     = "COMMON_INTERNAL_ERROR"

    // Auth
    CodeInvalidCredentials = "AUTH_INVALID_CREDENTIALS"
    CodeAccountLocked      = "AUTH_ACCOUNT_LOCKED"

    // Server
    CodeDuplicateServerID     = "SERVER_DUPLICATE_ID"
    CodeDuplicateServerName   = "SERVER_DUPLICATE_NAME"
    CodeServerValidation      = "SERVER_VALIDATION_FAILED"
    CodeServerIPNotAllowed    = "SERVER_IP_NOT_ALLOWED"
    CodeServerImportRejected  = "SERVER_IMPORT_FILE_REJECTED"
    CodeIdempotencyConflict   = "SERVER_IDEMPOTENCY_CONFLICT"

    // Report
    CodeReportInvalidRange      = "REPORT_INVALID_RANGE"
    CodeReportRecipientBlocked  = "REPORT_RECIPIENT_NOT_ALLOWED"
    CodeReportIdempotency       = "REPORT_IDEMPOTENCY_CONFLICT"
    CodeReportDataUnavailable   = "REPORT_DATA_UNAVAILABLE"
)

// HTTP status mapping
var ErrorHTTPStatus = map[string]int{
    CodeValidationFailed:       422,
    CodeUnauthorized:           401,
    CodeForbiddenScope:         403,
    CodeNotFound:               404,
    CodeRateLimited:            429,
    CodeInternalError:          500,
    CodeInvalidCredentials:     401,
    CodeAccountLocked:          423,
    CodeDuplicateServerID:      409,
    CodeDuplicateServerName:    409,
    CodeServerValidation:       422,
    CodeServerIPNotAllowed:     422,
    CodeServerImportRejected:   422,
    CodeIdempotencyConflict:    409,
    CodeReportInvalidRange:     422,
    CodeReportRecipientBlocked: 422,
    CodeReportIdempotency:      409,
    CodeReportDataUnavailable:  503,
}
```

### Verify

- [ ] Tất cả error codes từ design.md section 13.3 đã được khai báo
- [ ] HTTP status mapping chính xác

---

## R1.6. Sửa shared/middleware — ForwardAuth header

### Bước thực hiện

Hiện tại middleware đọc JWT trực tiếp từ `Authorization` header rồi tự verify. Thiết kế mới dùng Traefik ForwardAuth — Traefik gọi Identity `/internal/verify`, Identity trả header `X-User-Id` và `X-User-Scopes`, Traefik copy header đó sang request forward tới service.

**R1.6.1.** Sửa auth middleware trong `shared/middleware/`:

```go
// shared/middleware/auth.go — MỚI

package middleware

import (
    "net/http"
    "strings"
    "github.com/gin-gonic/gin"
    "github.com/vcs-sms/shared/response"
    apperrors "github.com/vcs-sms/shared/errors"
)

// AuthFromForwardAuth extracts user info from Traefik ForwardAuth headers.
// Traefik đã verify JWT qua Identity Service, service chỉ cần đọc header.
func AuthFromForwardAuth() gin.HandlerFunc {
    return func(c *gin.Context) {
        userID := c.GetHeader("X-User-Id")
        scopesStr := c.GetHeader("X-User-Scopes")

        if userID == "" {
            response.ErrorWithCode(c, http.StatusUnauthorized,
                apperrors.CodeUnauthorized, "Missing authentication")
            c.Abort()
            return
        }

        // Parse scopes
        var scopes []string
        if scopesStr != "" {
            scopes = strings.Split(scopesStr, ",")
        }

        // Set vào context cho handler dùng
        c.Set("user_id", userID)
        c.Set("user_scopes", scopes)
        c.Next()
    }
}

// RequireScope checks if the authenticated user has a required scope.
func RequireScope(scope string) gin.HandlerFunc {
    return func(c *gin.Context) {
        scopesVal, exists := c.Get("user_scopes")
        if !exists {
            response.ErrorWithCode(c, http.StatusForbidden,
                apperrors.CodeForbiddenScope, "No scopes available")
            c.Abort()
            return
        }

        scopes, ok := scopesVal.([]string)
        if !ok {
            response.ErrorWithCode(c, http.StatusForbidden,
                apperrors.CodeForbiddenScope, "Invalid scopes format")
            c.Abort()
            return
        }

        for _, s := range scopes {
            if strings.TrimSpace(s) == scope {
                c.Next()
                return
            }
        }

        response.ErrorWithCode(c, http.StatusForbidden,
            apperrors.CodeForbiddenScope,
            "Insufficient permissions: required scope '"+scope+"'")
        c.Abort()
    }
}
```

**R1.6.2.** Giữ middleware cũ (đọc JWT trực tiếp) trong file riêng, đánh dấu deprecated. Xóa ở R7.

### Verify

- [ ] `AuthFromForwardAuth()` đọc được `X-User-Id` và `X-User-Scopes`
- [ ] `RequireScope()` check scope chính xác
- [ ] Unit test cho middleware mới

---

## R1.7. Sửa shared/logger — Thêm lumberjack

### Bước thực hiện

**R1.7.1.** Thêm dependency lumberjack vào `shared/go.mod`:

```bash
cd shared
go get gopkg.in/natefinisher/lumberjack.v2
```

**R1.7.2.** Tạo/sửa `shared/logger/logger.go`:

```go
package logger

import (
    "os"
    "github.com/rs/zerolog"
    "gopkg.in/natefinisher/lumberjack.v2"
)

// Config cho logger
type Config struct {
    ServiceName string
    LogDir      string // e.g. "/var/log/vcs-sms"
    MaxSizeMB   int    // default 100
    MaxBackups  int    // default 7
    MaxAgeDays  int    // default 14
    Compress    bool   // default true
    Environment string // "development" or "production"
}

// NewLogger tạo zerolog.Logger ghi đồng thời ra file + stdout.
func NewLogger(cfg Config) zerolog.Logger {
    if cfg.MaxSizeMB == 0 { cfg.MaxSizeMB = 100 }
    if cfg.MaxBackups == 0 { cfg.MaxBackups = 7 }
    if cfg.MaxAgeDays == 0 { cfg.MaxAgeDays = 14 }

    logFile := cfg.LogDir + "/" + cfg.ServiceName + ".log"

    fileWriter := &lumberjack.Logger{
        Filename:   logFile,
        MaxSize:    cfg.MaxSizeMB,
        MaxBackups: cfg.MaxBackups,
        MaxAge:     cfg.MaxAgeDays,
        Compress:   cfg.Compress,
    }

    // MultiLevelWriter: ghi ra cả file và stdout
    multi := zerolog.MultiLevelWriter(fileWriter, os.Stdout)

    logger := zerolog.New(multi).With().
        Timestamp().
        Str("service", cfg.ServiceName).
        Str("environment", cfg.Environment).
        Logger()

    if cfg.Environment == "development" {
        // Development: pretty console output trên stdout
        consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
        multi = zerolog.MultiLevelWriter(fileWriter, consoleWriter)
        logger = zerolog.New(multi).With().
            Timestamp().
            Str("service", cfg.ServiceName).
            Logger()
    }

    return logger
}
```

### Verify

- [ ] Logger ghi ra file JSON trong `/var/log/vcs-sms/{service}.log`
- [ ] Logger cũng ghi ra stdout
- [ ] File rotate khi vượt MaxSize
- [ ] File cũ được gzip

---

## R1.8. Tạo Traefik config files

### Bước thực hiện

**R1.8.1.** Tạo thư mục:

```bash
mkdir -p deployments/traefik
```

**R1.8.2.** Tạo `deployments/traefik/traefik.yml` (static config):

```yaml
# Static configuration
entryPoints:
  web:
    address: ":8080"

providers:
  file:
    filename: /etc/traefik/dynamic.yml
    watch: true

api:
  dashboard: false

log:
  level: INFO

accessLog:
  filePath: /var/log/traefik/access.log
```

**R1.8.3.** Tạo `deployments/traefik/dynamic.yml` (dynamic config):

```yaml
http:
  # ============================================
  # Middlewares
  # ============================================
  middlewares:
    # ForwardAuth → Identity Service
    forward-auth:
      forwardAuth:
        address: "http://identity-service:8081/internal/verify"
        authResponseHeaders:
          - "X-User-Id"
          - "X-User-Scopes"
          - "X-User-Email"
        authResponseHeadersRegex: "^X-"

    # Rate limit global
    rate-limit-global:
      rateLimit:
        average: 100
        burst: 200
        period: "1s"

    # Rate limit cho auth endpoints (chặt hơn)
    rate-limit-auth:
      rateLimit:
        average: 10
        burst: 20
        period: "1s"

    # Rate limit cho report send (chống spam Gmail)
    rate-limit-report-send:
      rateLimit:
        average: 5
        burst: 10
        period: "1m"

    # CORS
    cors:
      headers:
        accessControlAllowMethods:
          - GET
          - POST
          - PUT
          - DELETE
          - OPTIONS
        accessControlAllowHeaders:
          - Content-Type
          - Authorization
          - Idempotency-Key
        accessControlAllowOriginList:
          - "http://localhost:3000"
        accessControlMaxAge: 3600

    # Import timeout (120s)
    import-timeout:
      buffering:
        maxResponseBodyBytes: 0
        maxRequestBodyBytes: 52428800  # 50MB

  # ============================================
  # Routers
  # ============================================
  routers:
    # --- Auth routes (public, no ForwardAuth) ---
    auth-login:
      rule: "PathPrefix(`/api/v1/auth/login`)"
      service: identity-service
      middlewares:
        - cors
        - rate-limit-auth
      entryPoints:
        - web

    auth-register:
      rule: "PathPrefix(`/api/v1/auth/register`)"
      service: identity-service
      middlewares:
        - cors
        - rate-limit-auth
      entryPoints:
        - web

    auth-refresh:
      rule: "PathPrefix(`/api/v1/auth/refresh`)"
      service: identity-service
      middlewares:
        - cors
        - rate-limit-auth
      entryPoints:
        - web

    # --- Auth routes (authenticated) ---
    auth-protected:
      rule: "PathPrefix(`/api/v1/auth`) && !PathPrefix(`/api/v1/auth/login`) && !PathPrefix(`/api/v1/auth/register`) && !PathPrefix(`/api/v1/auth/refresh`)"
      service: identity-service
      middlewares:
        - cors
        - forward-auth
        - rate-limit-global
      entryPoints:
        - web

    # --- Server routes (all authenticated) ---
    server-import:
      rule: "Path(`/api/v1/servers/import`)"
      service: server-service
      middlewares:
        - cors
        - forward-auth
        - rate-limit-global
        - import-timeout
      entryPoints:
        - web
      priority: 10

    server-api:
      rule: "PathPrefix(`/api/v1/servers`)"
      service: server-service
      middlewares:
        - cors
        - forward-auth
        - rate-limit-global
      entryPoints:
        - web

    # --- Report routes (authenticated) ---
    report-send:
      rule: "Path(`/api/v1/reports`) && Method(`POST`)"
      service: reporting-service
      middlewares:
        - cors
        - forward-auth
        - rate-limit-report-send
      entryPoints:
        - web
      priority: 10

    report-api:
      rule: "PathPrefix(`/api/v1/reports`)"
      service: reporting-service
      middlewares:
        - cors
        - forward-auth
        - rate-limit-global
      entryPoints:
        - web

  # ============================================
  # Services (backends)
  # ============================================
  services:
    identity-service:
      loadBalancer:
        servers:
          - url: "http://identity-service:8081"

    server-service:
      loadBalancer:
        servers:
          - url: "http://server-service:8082"

    reporting-service:
      loadBalancer:
        servers:
          - url: "http://reporting-service:8084"
```

### Verify

- [ ] Traefik static config valid
- [ ] Dynamic config có đúng route cho tất cả endpoints
- [ ] ForwardAuth chỉ áp dụng cho protected routes
- [ ] Login/register/refresh KHÔNG qua ForwardAuth
- [ ] Rate limit khác nhau cho auth vs global vs report send
- [ ] Import route có timeout riêng

---

## R1.9. Sửa docker-compose.yml

### Bước thực hiện

**R1.9.1.** XÓA các services:
- `kafka`
- `kafka-init`
- `api-gateway`
- `fileio-service`

**R1.9.2.** THÊM service `traefik`:

```yaml
traefik:
  image: traefik:v3.1
  container_name: vcs-sms-traefik
  ports:
    - "${GATEWAY_PORT:-8080}:8080"
  volumes:
    - ./deployments/traefik/traefik.yml:/etc/traefik/traefik.yml:ro
    - ./deployments/traefik/dynamic.yml:/etc/traefik/dynamic.yml:ro
    - ./logs/traefik:/var/log/traefik
  networks:
    - vcs-network
  depends_on:
    identity-service:
      condition: service_started
```

**R1.9.3.** SỬA dependencies:

```yaml
# server-service: BỎ kafka, kafka-init
server-service:
  depends_on:
    postgres:
      condition: service_healthy
    redis:
      condition: service_healthy

# monitor-service: BỎ postgres, kafka, kafka-init
monitor-service:
  depends_on:
    redis:
      condition: service_healthy
    elasticsearch:
      condition: service_healthy
    tcp-simulator:
      condition: service_healthy

# report-service: BỎ kafka, kafka-init
report-service:
  depends_on:
    postgres:
      condition: service_healthy
    elasticsearch:
      condition: service_healthy
```

**R1.9.4.** Rename service names cho phù hợp (tuỳ chọn):
- `auth-service` → `identity-service`
- Container name tương ứng

### Verify

- [ ] `docker compose config` không có lỗi
- [ ] Không còn service kafka, kafka-init, api-gateway, fileio-service
- [ ] Traefik service đã thêm
- [ ] Dependencies đúng

---

## R1.10. Sửa .env / .env.example

### Bước thực hiện

**R1.10.1.** Thêm biến môi trường mới:

```env
# === Database connections (thiết kế mới — 3 DB riêng) ===
IDENTITY_DB_HOST=postgres
IDENTITY_DB_PORT=5432
IDENTITY_DB_NAME=identity_db
IDENTITY_DB_USER=identity_user
IDENTITY_DB_PASSWORD=identity_pass_secret

SERVER_DB_HOST=postgres
SERVER_DB_PORT=5432
SERVER_DB_NAME=server_db
SERVER_DB_USER=server_user_v2
SERVER_DB_PASSWORD=server_pass_secret_v2

REPORT_DB_HOST=postgres
REPORT_DB_PORT=5432
REPORT_DB_NAME=report_db
REPORT_DB_USER=report_user_v2
REPORT_DB_PASSWORD=report_pass_secret_v2

# === Gmail SMTP ===
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_TLS=starttls
SMTP_USERNAME=your-email@gmail.com
SMTP_PASSWORD=your-16-char-app-password
SMTP_FROM=your-email@gmail.com
SMTP_ADMIN_EMAIL=admin@vcs.com

# === CIDR Allowlist ===
SERVER_CIDR_ALLOWLIST=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16
```

**R1.10.2.** BỎ biến Kafka:

```env
# XÓA:
# KAFKA_BROKERS=kafka:29092
# KAFKA_HOST=kafka
# KAFKA_PORT=9092
```

### Verify

- [ ] `.env.example` có đầy đủ biến mới
- [ ] Không còn biến Kafka
- [ ] Gmail SMTP config có mặt

---

## R1.11. Sửa Makefile

### Bước thực hiện

Cập nhật Makefile:
- BỎ target liên quan `fileio`, `kafka`, `api-gateway`
- THÊM target `rebuild-projection` (chạy server-service rebuild-monitor-cache)
- SỬA target build/test để reflect 4 services thay vì 5+1

---

## R1.12. Verify toàn bộ Infrastructure

### Checklist verification cuối Phase R1

```bash
# 1. Docker compose lên được
docker compose up -d postgres redis elasticsearch tcp-simulator

# 2. PostgreSQL có 3 database
docker exec vcs-sms-postgres psql -U vcs_admin -l
# → identity_db, server_db, report_db

# 3. Tables đúng
docker exec vcs-sms-postgres psql -U vcs_admin -d identity_db -c "\dt"
# → roles, permissions, role_permissions, users

docker exec vcs-sms-postgres psql -U vcs_admin -d server_db -c "\dt"
# → servers, api_idempotency

docker exec vcs-sms-postgres psql -U vcs_admin -d report_db -c "\dt"
# → report_jobs, daily_snapshots

# 4. Redis healthy
docker exec vcs-sms-redis redis-cli -a redis_secret ping

# 5. ES healthy
curl http://localhost:9200/_cluster/health

# 6. shared module compile
cd shared && go build ./...

# 7. Traefik config valid (sau khi services lên)
# docker compose up traefik → check log không error
```

### Rollback plan

Nếu R1 fail:
1. Revert `docker-compose.yml`
2. Revert `init.sql` (dùng lại file cũ)
3. `docker compose down -v && docker compose up -d`
4. Revert shared module changes
