# Refactor Phase R3: Server Service — Core

> **Mục tiêu:** Cập nhật các endpoint CRUD cơ bản của Server Service theo thiết kế mới — tách DB, đổi model, đổi logic caching cơ bản, bỏ Kafka publish.
>
> **Prerequisite:** Phase R1, R2 hoàn tất (Identity và Infrastructure đã sẵn sàng).
>
> **Kết quả:** Server Service kết nối `server_db`, các endpoint Create/Read/Update/Delete hoạt động không lỗi, không còn phụ thuộc Kafka.

---

## Checklist tổng quan

- [x] **R3.1** Đổi config kết nối sang `server_db`
- [x] **R3.2** Cập nhật model `Server` (cột mới, đổi TableName, bỏ schema)
- [x] **R3.3** Sửa `server_repository.go` — adapt với model mới
- [x] **R3.4** Sửa DTOs (`CreateServerRequest`, `UpdateServerRequest`, `ServerResponse`)
- [x] **R3.5** Bỏ hoàn toàn Kafka producer khỏi `server_service.go`
- [x] **R3.6** Cập nhật cơ chế cache-aside với `list_version`
- [x] **R3.7** Thêm validation cơ bản cho `tcp_port`
- [x] **R3.8** Sửa `cmd/main.go` — bỏ init Kafka
- [x] **R3.9** Fix các broken unit tests

---

## R3.1. Đổi DB Connection

### Bước thực hiện

**R3.1.1.** Sửa `server-service/config/config.go`:

```go
type DatabaseConfig struct {
    Host     string `env:"SERVER_DB_HOST" envDefault:"localhost"`
    Port     int    `env:"SERVER_DB_PORT" envDefault:"5432"`
    Name     string `env:"SERVER_DB_NAME" envDefault:"server_db"`
    User     string `env:"SERVER_DB_USER" envDefault:"server_user_v2"`
    Password string `env:"SERVER_DB_PASSWORD" envDefault:"server_pass_secret_v2"`
    SSLMode  string `env:"SERVER_DB_SSLMODE" envDefault:"disable"`
}
```

---

## R3.2. Cập nhật Model

### Bước thực hiện

**R3.2.1.** Sửa `server-service/internal/model/server.go`:

```go
package model

import (
    "time"
    "github.com/google/uuid"
    "gorm.io/gorm"
)

type Server struct {
    ID                uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    ServerID          string         `gorm:"type:varchar(100);not null" json:"server_id"`
    ServerName        string         `gorm:"type:varchar(255);not null" json:"server_name"`
    Status            string         `gorm:"type:varchar(20);not null;default:'UNKNOWN'" json:"status"`
    StatusChangedAt   *time.Time     `gorm:"type:timestamptz" json:"status_changed_at"`
    StatusVersion     int64          `gorm:"type:bigint;default:0" json:"status_version"`
    LastStatusEventID string         `gorm:"type:varchar(255)" json:"last_status_event_id"`
    IPv4              string         `gorm:"type:inet;not null" json:"ipv4"`
    TCPPort           int            `gorm:"type:int;not null;default:80" json:"tcp_port"`
    OS                string         `gorm:"type:varchar(100)" json:"os"`
    CPUCores          int            `gorm:"type:int" json:"cpu_cores"`
    RAMGB             int            `gorm:"type:int" json:"ram_gb"`
    DiskGB            int            `gorm:"type:int" json:"disk_gb"`
    Location          string         `gorm:"type:varchar(255)" json:"location"`
    Description       string         `gorm:"type:text" json:"description"`
    CreatedAt         time.Time      `json:"created_at"`
    UpdatedAt         time.Time      `json:"updated_at"`
    DeletedAt         gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Server) TableName() string { return "servers" }
```

---

## R3.3. Sửa Repository

### Bước thực hiện

Cập nhật `server_repository.go` để tương thích với model mới. Các constraint unique (`ux_servers_server_id`, `ux_servers_active_name`) đã được xử lý ở DB (xem `init-v2.sql`), repo cần catch error tương ứng để trả về `apperrors.CodeDuplicateServerID` hoặc `CodeDuplicateServerName`.

---

## R3.4. Sửa DTOs

### Bước thực hiện

**R3.4.1.** Sửa `CreateServerRequest` và `UpdateServerRequest`:

- Thêm trường `TCPPort int` (mặc định 80, validate > 0)
- `Status` không cho truyền từ client nữa.

**R3.4.2.** Sửa `ServerResponse`:

- Thêm `TCPPort`, `StatusChangedAt`, `LastStatusCheck`.

---

## R3.5. Bỏ Kafka Producer

### Bước thực hiện

- Trong `server-service/internal/service/server_service.go`, xóa field `kafkaProd`.
- Trong hàm `CreateServer`, `UpdateServer`, `DeleteServer`, **xóa logic publish Kafka**.
- Xóa hàm helper `publishEvent` nếu có.

---

## R3.6. Cập nhật Cache-aside (List Version)

### Bước thực hiện

**R3.6.1.** Đổi logic cache list trong `ServerService`:

Thay vì `SCAN` để delete toàn bộ cache list, sử dụng cơ chế version.

```go
func (s *serverServiceImpl) bumpListVersion(ctx context.Context) {
    s.rdb.Incr(ctx, "server:list:version")
}

func (s *serverServiceImpl) getListVersion(ctx context.Context) string {
    ver, err := s.rdb.Get(ctx, "server:list:version").Result()
    if err != nil {
        return "0"
    }
    return ver
}
```

Mỗi khi Get list, tạo cache key: `server:list:cache:{hash}:{version}`.
Mỗi khi Create/Update/Delete, gọi `bumpListVersion`.

---

## R3.7. Validation cơ bản

### Bước thực hiện

Đảm bảo request POST/PUT:
- `TCPPort` nằm trong khoảng 1-65535.
- Validation `IPv4` hợp lệ (phần allow list sẽ làm ở R4).

---

## Verify Phase R3

- [ ] API Create Server trả về 200, DB có data, status mặc định là UNKNOWN.
- [ ] API Update Server hoạt động, thay đổi được TCPPort.
- [ ] Xóa Kafka thành công, app vẫn boot lên.
- [ ] Bumping cache list version hoạt động.
- [ ] Unit tests cho Service/Handler chạy pass.
