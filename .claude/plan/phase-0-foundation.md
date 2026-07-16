# Phase 0: Foundation & Design

> **Mục tiêu:** Setup toàn bộ nền tảng để các phase sau chỉ cần tập trung viết business logic.
> **Thời gian:** Tuần 1
> **Kết quả:** Monorepo sẵn sàng, Docker chạy được, shared libs hoàn chỉnh, DB schemas đã tạo, TCP Simulator setup, Seed 10K servers.

---

## Checklist tổng quan Phase 0

- [x] **0.0** OpenAPI Spec — ✅ Đã hoàn thành (`docs/api-spec.yaml`)
- [ ] **0.1** Khởi tạo Monorepo structure
- [ ] **0.2** Setup Docker Compose (infrastructure + TCP Simulator)
- [ ] **0.3** Tạo Database schemas + migrations
- [ ] **0.4** Tạo Elasticsearch index mapping
- [ ] **0.5** Tạo Kafka topics
- [ ] **0.6** Xây dựng shared module
- [ ] **0.7** Setup Makefile
- [ ] **0.8** Tạo .env + config loader
- [ ] **0.9** Tạo Seed script 10.000 servers
- [ ] **0.10** Setup TCP Simulator service (code structure)
- [ ] **0.11** Verify toàn bộ infrastructure chạy OK

---

## 0.1. Khởi tạo Monorepo Structure

### Bước thực hiện:

**0.1.1.** Root project đã có sẵn: `server-management-system/`
```bash
cd server-management-system
git init   # nếu chưa khởi tạo git
```

**0.1.2.** Tạo toàn bộ cấu trúc thư mục:
```bash
# Service directories
mkdir -p api-gateway/{cmd,config,internal/{middleware,proxy,router}}
mkdir -p auth-service/{cmd,config,internal/{handler,service,repository,model,dto},migrations}
mkdir -p server-service/{cmd,config,docs,internal/{handler,service,repository,model,dto},migrations}
mkdir -p monitor-service/{cmd,config,internal/{handler,service,repository,scheduler,checker,worker,model},migrations}
mkdir -p report-service/{cmd,config,internal/{handler,service,repository,email/templates,scheduler,model,dto},migrations}
mkdir -p fileio-service/{cmd,config,internal/{handler,service,repository,excel,model,dto},migrations}
mkdir -p tcp-simulator/{cmd,simulator}

# Shared module
mkdir -p shared/{kafka,response,logger,middleware,validator,errors}

# Deployment & docs
mkdir -p deployments/docker/{postgres,elasticsearch,kafka}
mkdir -p deployments/config
mkdir -p migrations/{auth,server,monitor,report,fileio}
mkdir -p docs
mkdir -p logs/{gateway,auth,server,monitor,report,fileio}
mkdir -p uploads
```

**0.1.3.** Tạo `.gitignore`:
```gitignore
# Binaries
*.exe
*.dll
*.so
*.dylib

# Go
/vendor/
*.test
coverage.out
coverage.html

# Environment
.env
*.env.local

# IDE
.idea/
.vscode/
*.swp
*.swo

# Logs
logs/
*.log

# Uploads
uploads/*
!uploads/.gitkeep

# Docker volumes
postgres_data/
redis_data/
es_data/

# OS
.DS_Store
Thumbs.db
```

**0.1.4.** Khởi tạo Go modules cho từng service:
```bash
# Shared module (được các service khác reference)
cd shared && go mod init github.com/<your-username>/vcs-sms/shared && cd ..

# Từng service
cd api-gateway && go mod init github.com/<your-username>/vcs-sms/api-gateway && cd ..
cd auth-service && go mod init github.com/<your-username>/vcs-sms/auth-service && cd ..
cd server-service && go mod init github.com/<your-username>/vcs-sms/server-service && cd ..
cd monitor-service && go mod init github.com/<your-username>/vcs-sms/monitor-service && cd ..
cd report-service && go mod init github.com/<your-username>/vcs-sms/report-service && cd ..
cd fileio-service && go mod init github.com/<your-username>/vcs-sms/fileio-service && cd ..
cd tcp-simulator && go mod init github.com/<your-username>/vcs-sms/tcp-simulator && cd ..
```

**0.1.5.** Trong mỗi service `go.mod`, thêm replace directive để trỏ tới shared local:
```go
// Ví dụ trong server-service/go.mod
module github.com/<your-username>/vcs-sms/server-service

go 1.22

require (
    github.com/<your-username>/vcs-sms/shared v0.0.0
)

replace github.com/<your-username>/vcs-sms/shared => ../shared
```

**✅ Kết quả:** Monorepo structure hoàn chỉnh, mỗi service là 1 Go module độc lập.

---

## 0.2. Setup Docker Compose (Infrastructure)

### Bước thực hiện:

**0.2.1.** Tạo file `docker-compose.yml` ở root với các services infrastructure:
- PostgreSQL 17 Alpine
- Redis 8 Alpine
- Elasticsearch 8.12
- Apache Kafka 3.9 KRaft (không cần Zookeeper)
- kafka-init container (tạo topics)
- **TCP Simulator** (10.000 TCP listeners)

> Chi tiết Docker Compose xem trong brainstorm Section 16.

**0.2.2.** Tạo file `docker-compose.dev.yml` (override cho development, chỉ chạy infra):
```yaml
# docker-compose.dev.yml — Chỉ infrastructure, services chạy local
version: '3.8'
services:
  postgres:
    extends:
      file: docker-compose.yml
      service: postgres
  redis:
    extends:
      file: docker-compose.yml
      service: redis
  elasticsearch:
    extends:
      file: docker-compose.yml
      service: elasticsearch
  kafka:
    extends:
      file: docker-compose.yml
      service: kafka
  kafka-init:
    extends:
      file: docker-compose.yml
      service: kafka-init
  tcp-simulator:
    extends:
      file: docker-compose.yml
      service: tcp-simulator
```

**0.2.3.** Test chạy infrastructure:
```bash
docker-compose -f docker-compose.dev.yml up -d
docker-compose -f docker-compose.dev.yml ps    # Kiểm tra tất cả healthy
```

**0.2.4.** Verify kết nối:
```bash
# PostgreSQL
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms -c "\l"

# Redis
docker exec -it vcs-sms-redis redis-cli -a redis_secret PING

# Elasticsearch
curl http://localhost:9200/_cluster/health

# Kafka
docker exec -it vcs-sms-kafka kafka-topics --list --bootstrap-server localhost:9092
```

**✅ Kết quả:** 5 infrastructure containers + TCP Simulator chạy, healthy, có thể connect.

---

## 0.3. Tạo Database Schemas + Migrations

### Bước thực hiện:

**0.3.1.** Tạo init script `deployments/docker/postgres/init.sql`:

Nội dung: Tạo 5 schemas, 5 DB users, GRANT permissions.
> Chi tiết SQL xem trong brainstorm Section 5.0.

**0.3.2.** Tạo migration files cho từng schema:

**Auth schema:**
```
migrations/auth/
├── 000001_create_roles.up.sql          # CREATE TABLE auth_schema.roles + seed data
├── 000001_create_roles.down.sql        # DROP TABLE
├── 000002_create_role_permissions.up.sql
├── 000002_create_role_permissions.down.sql
├── 000003_create_users.up.sql
└── 000003_create_users.down.sql
```

Nội dung `000001_create_roles.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS auth_schema.roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(50)   NOT NULL UNIQUE,
    description     TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

-- Seed 3 roles
INSERT INTO auth_schema.roles (id, name, description) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'admin',    'Full access to all resources'),
    ('a0000000-0000-0000-0000-000000000002', 'operator', 'Can read and update servers, view reports'),
    ('a0000000-0000-0000-0000-000000000003', 'viewer',   'Read-only access')
ON CONFLICT (name) DO NOTHING;
```

Nội dung `000002_create_role_permissions.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS auth_schema.role_permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id         UUID          NOT NULL REFERENCES auth_schema.roles(id) ON DELETE CASCADE,
    scope           VARCHAR(100)  NOT NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    UNIQUE(role_id, scope)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_role_id 
    ON auth_schema.role_permissions(role_id);

-- Seed admin permissions (9 scopes)
INSERT INTO auth_schema.role_permissions (role_id, scope) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'server:create'),
    ('a0000000-0000-0000-0000-000000000001', 'server:read'),
    ('a0000000-0000-0000-0000-000000000001', 'server:update'),
    ('a0000000-0000-0000-0000-000000000001', 'server:delete'),
    ('a0000000-0000-0000-0000-000000000001', 'server:import'),
    ('a0000000-0000-0000-0000-000000000001', 'server:export'),
    ('a0000000-0000-0000-0000-000000000001', 'report:view'),
    ('a0000000-0000-0000-0000-000000000001', 'report:send'),
    ('a0000000-0000-0000-0000-000000000001', 'user:manage'),
    -- Operator permissions (3 scopes)
    ('a0000000-0000-0000-0000-000000000002', 'server:read'),
    ('a0000000-0000-0000-0000-000000000002', 'server:update'),
    ('a0000000-0000-0000-0000-000000000002', 'report:view'),
    -- Viewer permissions (2 scopes)
    ('a0000000-0000-0000-0000-000000000003', 'server:read'),
    ('a0000000-0000-0000-0000-000000000003', 'report:view')
ON CONFLICT (role_id, scope) DO NOTHING;
```

Nội dung `000003_create_users.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS auth_schema.users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username        VARCHAR(100)  NOT NULL UNIQUE,
    email           VARCHAR(255)  NOT NULL UNIQUE,
    password_hash   VARCHAR(255)  NOT NULL,
    full_name       VARCHAR(255),
    role_id         UUID          NOT NULL REFERENCES auth_schema.roles(id),
    is_active       BOOLEAN       NOT NULL DEFAULT TRUE,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_username 
    ON auth_schema.users(username) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_email 
    ON auth_schema.users(email) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_role_id 
    ON auth_schema.users(role_id);
```

**Server schema:**
```
migrations/server/
├── 000001_create_servers.up.sql
└── 000001_create_servers.down.sql
```

Nội dung `000001_create_servers.up.sql`:
```sql
CREATE TABLE IF NOT EXISTS server_schema.servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id       VARCHAR(100)  NOT NULL UNIQUE,
    server_name     VARCHAR(255)  NOT NULL UNIQUE,
    status          VARCHAR(20)   NOT NULL DEFAULT 'off' 
                    CHECK (status IN ('on', 'off')),
    ipv4            VARCHAR(15)   NOT NULL,
    os              VARCHAR(100),
    cpu_cores       INTEGER       CHECK (cpu_cores > 0),
    ram_gb          DECIMAL(10,2) CHECK (ram_gb > 0),
    disk_gb         DECIMAL(10,2) CHECK (disk_gb > 0),
    location        VARCHAR(255),
    description     TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_servers_server_id 
    ON server_schema.servers(server_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_server_name 
    ON server_schema.servers(server_name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_status 
    ON server_schema.servers(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_ipv4 
    ON server_schema.servers(ipv4) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_created_at 
    ON server_schema.servers(created_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_status_created 
    ON server_schema.servers(status, created_at DESC) WHERE deleted_at IS NULL;
```

**Monitor schema:**
```
migrations/monitor/
├── 000001_create_health_check_configs.up.sql
└── 000001_create_health_check_configs.down.sql
```

**Report schema:**
```
migrations/report/
├── 000001_create_report_jobs.up.sql
├── 000001_create_report_jobs.down.sql
├── 000002_create_daily_snapshots.up.sql
└── 000002_create_daily_snapshots.down.sql
```

**FileIO schema:**
```
migrations/fileio/
├── 000001_create_import_jobs.up.sql
├── 000001_create_import_jobs.down.sql
├── 000002_create_import_job_details.up.sql
└── 000002_create_import_job_details.down.sql
```

> Chi tiết SQL cho mỗi bảng xem trong brainstorm Section 5.1 → 5.6.

**0.3.3.** Tạo seed script `deployments/docker/postgres/seed_10k_servers.sql`:

> Chi tiết SQL seed xem trong brainstorm Section 5.4. Script tạo:
> - 10.000 servers trong `server_schema.servers` với `ipv4 = 'tcp-simulator'`
> - 10.000 health_check_configs trong `monitor_schema.health_check_configs` với `tcp_port = 9000 + index`
> - Phân bố uptime_rate: 70% (93-99%), 20% (80-93%), 10% (50-80%)

**0.3.4.** Chạy init script:
```bash
docker-compose -f docker-compose.dev.yml down -v   # Reset
docker-compose -f docker-compose.dev.yml up -d postgres
# Chờ healthy rồi verify:
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms -c "\dn"
# Kết quả phải thấy 5 schemas: auth_schema, server_schema, monitor_schema, report_schema, fileio_schema
```

**0.3.5.** Chạy migrations (dùng golang-migrate CLI hoặc tích hợp vào code):
```bash
# Cài golang-migrate CLI
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Chạy migrations cho từng schema
migrate -path migrations/auth -database "postgres://auth_user:auth_pass_secret@localhost:5432/vcs_sms?sslmode=disable&search_path=auth_schema" up
migrate -path migrations/server -database "postgres://server_user:server_pass_secret@localhost:5432/vcs_sms?sslmode=disable&search_path=server_schema" up
migrate -path migrations/monitor -database "postgres://monitor_user:monitor_pass_secret@localhost:5432/vcs_sms?sslmode=disable&search_path=monitor_schema" up
migrate -path migrations/report -database "postgres://report_user:report_pass_secret@localhost:5432/vcs_sms?sslmode=disable&search_path=report_schema" up
migrate -path migrations/fileio -database "postgres://fileio_user:fileio_pass_secret@localhost:5432/vcs_sms?sslmode=disable&search_path=fileio_schema" up
```

**0.3.6.** Chạy seed script 10.000 servers:
```bash
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms -f /docker-entrypoint-initdb.d/seed_10k_servers.sql
```

**0.3.7.** Verify tables + seed data:
```bash
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms -c "\dt auth_schema.*"
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms -c "\dt server_schema.*"
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms -c "\dt monitor_schema.*"
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms -c "\dt report_schema.*"
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms -c "\dt fileio_schema.*"
# Verify seed:
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms \
  -c "SELECT COUNT(*) FROM server_schema.servers"
# Expected: 10000
docker exec -it vcs-sms-postgres psql -U vcs_admin -d vcs_sms \
  -c "SELECT COUNT(*) FROM monitor_schema.health_check_configs"
# Expected: 10000
```

**✅ Kết quả:** 5 schemas với tất cả tables, indexes, 10.000 servers + health_check_configs đã seed.

---

## 0.4. Tạo Elasticsearch Index Mapping

### Bước thực hiện:

**0.4.1.** Tạo file `deployments/docker/elasticsearch/mapping.json`:
```json
{
  "mappings": {
    "properties": {
      "server_id":       { "type": "keyword" },
      "server_name":     { "type": "keyword" },
      "status":          { "type": "keyword" },
      "checked_at":      { "type": "date", "format": "strict_date_optional_time||epoch_millis" },
      "response_time_ms": { "type": "integer" },
      "check_method":    { "type": "keyword" }
    }
  },
  "settings": {
    "number_of_shards": 3,
    "number_of_replicas": 0,
    "refresh_interval": "5s"
  }
}
```

**0.4.2.** Tạo script `deployments/docker/elasticsearch/init-index.sh`:
```bash
#!/bin/bash
echo "Waiting for Elasticsearch..."
until curl -s http://localhost:9200/_cluster/health | grep -q '"status":"green\|yellow"'; do
    sleep 2
done

echo "Creating index server-status-logs..."
curl -X PUT "http://localhost:9200/server-status-logs" \
  -H "Content-Type: application/json" \
  -d @/scripts/mapping.json

echo "Index created!"
curl http://localhost:9200/server-status-logs/_mapping
```

**0.4.3.** Chạy tạo index:
```bash
curl -X PUT "http://localhost:9200/server-status-logs" \
  -H "Content-Type: application/json" \
  -d @deployments/docker/elasticsearch/mapping.json
```

**0.4.4.** Verify:
```bash
curl http://localhost:9200/server-status-logs/_mapping?pretty
```

**✅ Kết quả:** Index `server-status-logs` đã tạo với mapping đúng.

---

## 0.5. Tạo Kafka Topics

### Bước thực hiện:

**0.5.1.** Tạo file `deployments/docker/kafka/create-topics.sh`:
```bash
#!/bin/bash
echo "Creating Kafka topics..."

BROKER="kafka:29092"
KAFKA_BIN="/opt/kafka/bin"  # Apache Kafka 3.9 KRaft

$KAFKA_BIN/kafka-topics.sh --create --if-not-exists --bootstrap-server $BROKER \
  --partitions 3 --replication-factor 1 --topic server.created

$KAFKA_BIN/kafka-topics.sh --create --if-not-exists --bootstrap-server $BROKER \
  --partitions 3 --replication-factor 1 --topic server.updated

$KAFKA_BIN/kafka-topics.sh --create --if-not-exists --bootstrap-server $BROKER \
  --partitions 3 --replication-factor 1 --topic server.deleted

$KAFKA_BIN/kafka-topics.sh --create --if-not-exists --bootstrap-server $BROKER \
  --partitions 6 --replication-factor 1 --topic server.status.changed

$KAFKA_BIN/kafka-topics.sh --create --if-not-exists --bootstrap-server $BROKER \
  --partitions 3 --replication-factor 1 --topic server.health.batch

$KAFKA_BIN/kafka-topics.sh --create --if-not-exists --bootstrap-server $BROKER \
  --partitions 3 --replication-factor 1 --topic import.job.created

$KAFKA_BIN/kafka-topics.sh --create --if-not-exists --bootstrap-server $BROKER \
  --partitions 1 --replication-factor 1 --topic report.daily.trigger

echo "All topics created!"
$KAFKA_BIN/kafka-topics.sh --list --bootstrap-server $BROKER
```

**0.5.2.** Topics được tạo tự động bởi `kafka-init` container trong docker-compose.

**0.5.3.** Verify:
```bash
docker exec -it vcs-sms-kafka /opt/kafka/bin/kafka-topics.sh --list --bootstrap-server localhost:9092
# Expected: 7 topics
```

**✅ Kết quả:** 7 Kafka topics đã tạo.

---

## 0.6. Xây dựng Shared Module

> Đây là module quan trọng nhất của Phase 0 — tất cả services sẽ dùng chung.

### 0.6.1. Logger (`shared/logger/logger.go`)

**Mục tiêu:** Cung cấp structured JSON logger (zerolog) + logrotate (lumberjack).

**Interface:**
```go
package logger

// NewLogger tạo zerolog.Logger với file rotation
// - serviceName: tên service (auth-service, server-service, ...)
// - cfg: LogConfig từ viper
func NewLogger(serviceName string, cfg *LogConfig) zerolog.Logger

type LogConfig struct {
    Level      string // "debug", "info", "warn", "error"
    Dir        string // "/var/log/vcs-sms"
    MaxSize    int    // MB
    MaxBackups int
    MaxAge     int    // days
    Compress   bool
}
```

**Nội dung cần code:**
1. Setup `lumberjack.Logger` (file writer)
2. Setup `zerolog.MultiLevelWriter` (stdout + file)
3. Attach `timestamp`, `service` field
4. Parse log level từ config
5. Export global logger instance

**Dependencies:**
```bash
go get github.com/rs/zerolog
go get gopkg.in/natefinish/lumberjack.v2
```

---

### 0.6.2. Response Format (`shared/response/response.go`)

**Mục tiêu:** Standard API response format cho tất cả services.

**Structs cần tạo:**
```go
package response

// Success response
type ApiResponse struct {
    Status  string      `json:"status"`          // "success"
    Code    int         `json:"code"`            // HTTP status code
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
    Meta    *Meta       `json:"meta,omitempty"`
}

// Error response
type ApiErrorResponse struct {
    Status  string        `json:"status"`        // "error"
    Code    int           `json:"code"`          // HTTP status code
    Message string        `json:"message"`
    Errors  []FieldError  `json:"errors,omitempty"`
    Meta    *Meta         `json:"meta,omitempty"`
}

type FieldError struct {
    Field   string `json:"field"`
    Code    string `json:"code"`
    Message string `json:"message"`
}

type Meta struct {
    RequestID string `json:"request_id"`
    Timestamp string `json:"timestamp"`
}

// Paginated response wrapper
type PaginatedData struct {
    Total    int64       `json:"total"`
    Page     int         `json:"page"`
    PageSize int         `json:"page_size"`
    Items    interface{} `json:"items"`
}
```

**Helper functions:**
```go
func Success(c *gin.Context, code int, message string, data interface{})
func Error(c *gin.Context, code int, message string, errors ...FieldError)
func Paginated(c *gin.Context, total int64, page, pageSize int, items interface{})
```

---

### 0.6.3. Error Codes (`shared/errors/app_errors.go`)

**Mục tiêu:** Định nghĩa tất cả application error codes.

**Nội dung cần tạo:**
```go
package errors

type AppError struct {
    HTTPStatus int
    AppCode    int
    Message    string
}

var (
    ErrBadRequest         = &AppError{400, 40001, "Bad request"}
    ErrInvalidParameter   = &AppError{400, 40002, "Invalid parameter"}
    ErrUnauthorized       = &AppError{401, 40101, "Unauthorized"}
    ErrTokenRevoked       = &AppError{401, 40102, "Token revoked"}
    ErrForbidden          = &AppError{403, 40301, "Insufficient permissions"}
    ErrNotFound           = &AppError{404, 40401, "Resource not found"}
    ErrDuplicateServerID  = &AppError{409, 40901, "Server ID already exists"}
    ErrDuplicateServerName= &AppError{409, 40902, "Server name already exists"}
    ErrValidation         = &AppError{422, 42201, "Validation error"}
    ErrRateLimit          = &AppError{429, 42901, "Rate limit exceeded"}
    ErrInternal           = &AppError{500, 50001, "Internal server error"}
    ErrDatabase           = &AppError{500, 50002, "Database error"}
    ErrElasticsearch      = &AppError{500, 50003, "Elasticsearch error"}
    ErrKafka              = &AppError{500, 50004, "Kafka error"}
    ErrEmail              = &AppError{500, 50005, "Email sending error"}
)
```

---

### 0.6.4. Kafka Client (`shared/kafka/`)

**Mục tiêu:** Producer và Consumer wrapper cho tất cả services.

**Files cần tạo:**

`shared/kafka/event.go`:
```go
type Event struct {
    EventID   string      `json:"event_id"`
    EventType string      `json:"event_type"`
    Timestamp time.Time   `json:"timestamp"`
    Source    string       `json:"source"`
    Data     interface{}  `json:"data"`
}
```

`shared/kafka/producer.go`:
```go
type Producer interface {
    Publish(ctx context.Context, topic string, event *Event) error
    Close() error
}

// Sử dụng IBM/sarama (recommended cho Apache Kafka 3.9 KRaft)
func NewProducer(brokers []string) (Producer, error)
```

`shared/kafka/consumer.go`:
```go
type MessageHandler func(ctx context.Context, event *Event) error

type Consumer interface {
    Subscribe(topic string, groupID string, handler MessageHandler) error
    Close() error
}

func NewConsumer(brokers []string) (Consumer, error)
```

**Dependencies:**
```bash
go get github.com/IBM/sarama
```

---

### 0.6.5. Middleware (`shared/middleware/request_id.go`)

**Mục tiêu:** Generate request_id cho mỗi request, inject vào context + response header.

```go
func RequestIDMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        requestID := c.GetHeader("X-Request-ID")
        if requestID == "" {
            requestID = uuid.New().String()
        }
        c.Set("request_id", requestID)
        c.Header("X-Request-ID", requestID)
        c.Next()
    }
}
```

---

### 0.6.6. Validator (`shared/validator/validator.go`)

**Mục tiêu:** Custom validators cho IPv4, server_id format, etc.

```go
func ValidateIPv4(ip string) bool
func ValidateServerID(id string) bool
func InitValidator() // Register custom validators với gin
```

---

### 0.6.7. Install shared dependencies:
```bash
cd shared
go get github.com/rs/zerolog
go get gopkg.in/natefinish/lumberjack.v2
go get github.com/google/uuid
go get github.com/IBM/sarama
go get github.com/gin-gonic/gin
go mod tidy
cd ..
```

**✅ Kết quả:** Shared module với 6 packages sẵn sàng cho các services import.

---

## 0.7. Setup Makefile

### Bước thực hiện:

**0.7.1.** Tạo file `Makefile` ở root:
```makefile
.PHONY: help infra-up infra-down build test coverage migrate-up migrate-down

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
	awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ── Infrastructure ──
infra-up: ## Start infrastructure (PG, Redis, ES, Kafka)
	docker-compose -f docker-compose.dev.yml up -d

infra-down: ## Stop infrastructure
	docker-compose -f docker-compose.dev.yml down

infra-reset: ## Reset infrastructure (destroy volumes)
	docker-compose -f docker-compose.dev.yml down -v

# ── Build ──
build-all: ## Build all services
	cd api-gateway && go build -o ../bin/api-gateway ./cmd/main.go
	cd auth-service && go build -o ../bin/auth-service ./cmd/main.go
	cd server-service && go build -o ../bin/server-service ./cmd/main.go
	cd monitor-service && go build -o ../bin/monitor-service ./cmd/main.go
	cd report-service && go build -o ../bin/report-service ./cmd/main.go
	cd fileio-service && go build -o ../bin/fileio-service ./cmd/main.go

# ── Test ──
test-all: ## Run all tests
	cd shared && go test ./... -v
	cd auth-service && go test ./... -v
	cd server-service && go test ./... -v
	cd monitor-service && go test ./... -v
	cd report-service && go test ./... -v
	cd fileio-service && go test ./... -v

coverage-all: ## Run tests with coverage
	cd shared && go test ./... -coverprofile=coverage.out -covermode=atomic
	cd auth-service && go test ./... -coverprofile=coverage.out -covermode=atomic
	cd server-service && go test ./... -coverprofile=coverage.out -covermode=atomic
	cd monitor-service && go test ./... -coverprofile=coverage.out -covermode=atomic
	cd report-service && go test ./... -coverprofile=coverage.out -covermode=atomic
	cd fileio-service && go test ./... -coverprofile=coverage.out -covermode=atomic

# ── Migrations ──
migrate-up: ## Run all migrations
	migrate -path migrations/auth -database "$(AUTH_DB_URL)" up
	migrate -path migrations/server -database "$(SERVER_DB_URL)" up
	migrate -path migrations/monitor -database "$(MONITOR_DB_URL)" up
	migrate -path migrations/report -database "$(REPORT_DB_URL)" up
	migrate -path migrations/fileio -database "$(FILEIO_DB_URL)" up

# ── Docker ──
docker-build: ## Build all Docker images
	docker-compose build

docker-up: ## Start all services with Docker
	docker-compose up -d

docker-down: ## Stop all services
	docker-compose down

# ── Swagger ──
swagger: ## Generate Swagger docs
	cd server-service && swag init -g cmd/main.go -o docs
	cd auth-service && swag init -g cmd/main.go -o docs
```

**✅ Kết quả:** `make help` hiển thị tất cả commands. Development workflow sẵn sàng.

---

## 0.8. Tạo .env + Config Loader

### Bước thực hiện:

**0.8.1.** Tạo file `.env.example` ở root (copy toàn bộ nội dung từ brainstorm Section 17).

**0.8.2.** Copy sang `.env`:
```bash
cp .env.example .env
# Sửa các giá trị sensitive: JWT_SECRET, SMTP_PASSWORD, etc.
```

**0.8.3.** Tạo config loader pattern (sẽ dùng chung cho tất cả services):

Mỗi service sẽ có `config/config.go` riêng dùng Viper:
```go
package config

import (
    "github.com/spf13/viper"
)

type Config struct {
    App      AppConfig
    Database DatabaseConfig
    Redis    RedisConfig
    Kafka    KafkaConfig
    // ... service-specific configs
}

func LoadConfig() (*Config, error) {
    viper.SetConfigFile(".env")
    viper.AutomaticEnv()
    
    if err := viper.ReadInConfig(); err != nil {
        // Fallback to env vars (Docker)
    }
    
    return &Config{
        App: AppConfig{
            Env:   viper.GetString("APP_ENV"),
            Debug: viper.GetBool("APP_DEBUG"),
        },
        // ...
    }, nil
}
```

**Dependencies (cho tất cả services):**
```bash
go get github.com/spf13/viper
```

**✅ Kết quả:** `.env` file sẵn sàng, config loader pattern cho mỗi service.

---

## 0.9. Tạo Seed Script 10.000 Servers

> Seed script đã được nhúng vào brainstorm Section 5.4. Nó tạo 10.000 servers với `ipv4 = 'tcp-simulator'` và 10.000 health_check_configs với port mapping (9001–19000).

**File:** `deployments/docker/postgres/seed_10k_servers.sql`

Nội dung đã mô tả ở step 0.3.3. Script sẽ được chạy tự động khi init DB hoặc manual bằng `psql -f`.

**✅ Kết quả:** 10.000 servers + configs sẵn sàng cho TCP Simulator.

---

## 0.10. Setup TCP Simulator Service

### Bước thực hiện:

**0.10.1.** Tạo cấu trúc thư mục:
```bash
tcp-simulator/
├── cmd/
│   └── main.go              # Entry point
├── simulator/
│   ├── manager.go           # SimulatorManager — quản lý 10.000 listeners
│   ├── listener.go          # FakeServer struct — mở/đóng port
│   ├── math_engine.go       # Công thức toán học On/Off
│   ├── math_engine_test.go  # Unit tests cho math engine
│   └── config.go            # Load SIMULATOR_* env vars
├── Dockerfile
├── go.mod
└── go.sum
```

**0.10.2.** Tạo `tcp-simulator/go.mod`:
```go
module github.com/<your-username>/vcs-sms/tcp-simulator

go 1.22
```

> TCP Simulator không dùng shared module — nó hoàn toàn standalone, không cần GORM, Kafka, Redis.

**0.10.3.** Code skeleton cho `main.go`:
```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"
    
    "github.com/<your-username>/vcs-sms/tcp-simulator/simulator"
)

func main() {
    cfg := simulator.LoadConfig()
    
    manager := simulator.NewSimulatorManager(cfg)
    
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    go manager.RunControlLoop(ctx)
    
    log.Printf("TCP Simulator started: %d servers on ports %d-%d, tick=%s",
        cfg.NumServers, cfg.BasePort, cfg.BasePort+cfg.NumServers-1, cfg.TickInterval)
    
    // Graceful shutdown
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    
    cancel()
    time.Sleep(1 * time.Second) // chờ shutdown
    log.Println("TCP Simulator stopped")
}
```

**0.10.4.** Tạo Dockerfile (`tcp-simulator/Dockerfile`):
```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY tcp-simulator/ ./tcp-simulator/
WORKDIR /app/tcp-simulator
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/tcp-simulator ./cmd/main.go

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/tcp-simulator .
CMD ["./tcp-simulator"]
```

**0.10.5.** Verify TCP Simulator chạy local:
```bash
cd tcp-simulator
SIMULATOR_BASE_PORT=9001 SIMULATOR_NUM_SERVERS=100 SIMULATOR_TICK_INTERVAL=10s go run cmd/main.go &

# Test ping 1 port
nc -z localhost 9001  # Nếu port đang mở → success
nc -z localhost 9050  # Nếu port đang đóng → fail

# Cleanup
kill %1
```

> Chi tiết code Math Engine và Listener Manager xem trong brainstorm Section 4.5.

**✅ Kết quả:** TCP Simulator service chạy được, mở/đóng port theo toán học.

---

## 0.11. Verify toàn bộ Infrastructure

### Bước thực hiện:

**0.11.1.** Chạy full stack infrastructure:
```bash
make infra-up
```

**0.11.2.** Checklist verify:

| # | Component | Command | Expected |
|---|-----------|---------|----------|
| 1 | PostgreSQL | `psql -c "\dn"` | 5 schemas visible |
| 2 | PostgreSQL | `psql -c "\dt auth_schema.*"` | 3 tables: roles, role_permissions, users |
| 3 | PostgreSQL | `psql -c "\dt server_schema.*"` | 1 table: servers |
| 4 | PostgreSQL | `psql -c "SELECT * FROM auth_schema.roles"` | 3 rows (admin, operator, viewer) |
| 5 | PostgreSQL | `psql -c "SELECT COUNT(*) FROM server_schema.servers"` | 10000 |
| 6 | PostgreSQL | `psql -c "SELECT COUNT(*) FROM monitor_schema.health_check_configs"` | 10000 |
| 7 | Redis | `redis-cli PING` | PONG |
| 8 | Elasticsearch | `curl localhost:9200/_cat/indices` | Index `server-status-logs` exists |
| 9 | Kafka | `kafka-topics --list` | 7 topics |
| 10 | TCP Simulator | `nc -z tcp-simulator 9001` (từ trong Docker) | Connection succeeded |
| 11 | Go build | `cd shared && go build ./...` | No errors |
| 12 | Go build | `cd tcp-simulator && go build ./...` | No errors |

**0.11.3.** Nếu tất cả pass → Phase 0 hoàn tất ✅

---

## Deliverables Phase 0

| # | Deliverable | File/Path |
|---|------------|-----------|
| 1 | Monorepo structure | Tất cả directories đã tạo (including `tcp-simulator/`) |
| 2 | Docker Compose | `docker-compose.yml`, `docker-compose.dev.yml` |
| 3 | DB init script | `deployments/docker/postgres/init.sql` |
| 4 | Seed script 10K | `deployments/docker/postgres/seed_10k_servers.sql` |
| 5 | Migration files | `migrations/{auth,server,monitor,report,fileio}/*.sql` |
| 6 | ES mapping | `deployments/docker/elasticsearch/mapping.json` |
| 7 | Kafka topics script | `deployments/docker/kafka/create-topics.sh` |
| 8 | Shared module | `shared/{logger,response,errors,kafka,middleware,validator}` |
| 9 | TCP Simulator skeleton | `tcp-simulator/{cmd,simulator}` |
| 10 | Makefile | `Makefile` |
| 11 | Environment config | `.env.example`, `.env` |
| 12 | Git setup | `.gitignore`, initial commit |

---

> **Tiếp theo:** [Phase 1: Auth + Server Service →](./phase-1-auth-server.md)

