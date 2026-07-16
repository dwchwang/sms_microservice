# Phase 1: Auth Service + Server Service + API Gateway

> **Mục tiêu:** Xây dựng 3 core components: Auth (JWT/RBAC), Server CRUD, và API Gateway.
> **Thời gian:** Tuần 2
> **Prerequisite:** Phase 0 hoàn tất (infrastructure + shared module chạy OK)
> **Điểm đạt được:** 1.0 (CRUD) + 0.5 (JWT) + 0.5 (GORM/SQL Injection) + phần Redis Cache

---

## Checklist tổng quan Phase 1

- [ ] **1.1** Auth Service — Models & Repository
- [ ] **1.2** Auth Service — Service Layer (register, login, JWT)
- [ ] **1.3** Auth Service — Handler Layer (HTTP endpoints)
- [ ] **1.4** Auth Service — Unit Tests
- [ ] **1.5** Server Service — Models & Repository
- [ ] **1.6** Server Service — Service Layer (CRUD + Kafka publish)
- [ ] **1.7** Server Service — Handler Layer (HTTP + filter/sort/pagination)
- [ ] **1.8** Server Service — Redis Cache integration
- [ ] **1.9** Server Service — Unit Tests
- [ ] **1.10** API Gateway — Middleware chain
- [ ] **1.11** API Gateway — Reverse Proxy + Route config
- [ ] **1.12** Integration test (Gateway ↔ Auth ↔ Server)
- [ ] **1.13** Swagger annotations cho Auth + Server

---

## 1.1. Auth Service — Models & Repository

### 1.1.1. Tạo GORM models

**File:** `auth-service/internal/model/role.go`
```go
package model

type Role struct {
    ID          uuid.UUID        `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    Name        string           `gorm:"type:varchar(50);uniqueIndex;not null" json:"name"`
    Description string           `gorm:"type:text" json:"description"`
    Permissions []RolePermission `gorm:"foreignKey:RoleID" json:"permissions,omitempty"`
    CreatedAt   time.Time        `json:"created_at"`
    UpdatedAt   time.Time        `json:"updated_at"`
}

func (Role) TableName() string { return "auth_schema.roles" }
```

**File:** `auth-service/internal/model/role_permission.go`
```go
type RolePermission struct {
    ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    RoleID    uuid.UUID `gorm:"type:uuid;not null;index" json:"role_id"`
    Scope     string    `gorm:"type:varchar(100);not null" json:"scope"`
    CreatedAt time.Time `json:"created_at"`
}

func (RolePermission) TableName() string { return "auth_schema.role_permissions" }
```

**File:** `auth-service/internal/model/user.go`
```go
type User struct {
    ID           uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    Username     string         `gorm:"type:varchar(100);uniqueIndex;not null" json:"username"`
    Email        string         `gorm:"type:varchar(255);uniqueIndex;not null" json:"email"`
    PasswordHash string         `gorm:"type:varchar(255);not null" json:"-"`
    FullName     string         `gorm:"type:varchar(255)" json:"full_name"`
    RoleID       uuid.UUID      `gorm:"type:uuid;not null" json:"role_id"`
    Role         *Role          `gorm:"foreignKey:RoleID" json:"role,omitempty"`
    IsActive     bool           `gorm:"not null;default:true" json:"is_active"`
    LastLoginAt  *time.Time     `json:"last_login_at"`
    CreatedAt    time.Time      `json:"created_at"`
    UpdatedAt    time.Time      `json:"updated_at"`
    DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string { return "auth_schema.users" }
```

### 1.1.2. Tạo Repository interface + implementation

**File:** `auth-service/internal/repository/user_repository.go`

```go
package repository

type UserRepository interface {
    Create(ctx context.Context, user *model.User) error
    FindByUsername(ctx context.Context, username string) (*model.User, error)
    FindByEmail(ctx context.Context, email string) (*model.User, error)
    FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
    UpdateLastLogin(ctx context.Context, id uuid.UUID) error
}

type userRepository struct {
    db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
    return &userRepository{db: db}
}
```

**Lưu ý quan trọng (GORM schema):** Set default schema trong connection string:
```go
dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
    cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.Schema)
```

### 1.1.3. Dependencies cần install
```bash
cd auth-service
go get gorm.io/gorm
go get gorm.io/driver/postgres
go get github.com/google/uuid
go get golang.org/x/crypto/bcrypt
go get github.com/golang-jwt/jwt/v5
go get github.com/gin-gonic/gin
go get github.com/spf13/viper
go get github.com/go-redis/redis/v9
go mod tidy
```

---

## 1.2. Auth Service — Service Layer

### 1.2.1. Tạo DTOs

**File:** `auth-service/internal/dto/request.go`
```go
type RegisterRequest struct {
    Username string `json:"username" binding:"required,min=3,max=100"`
    Email    string `json:"email" binding:"required,email"`
    Password string `json:"password" binding:"required,min=8"`
    FullName string `json:"full_name" binding:"required"`
    RoleName string `json:"role_name" binding:"required,oneof=admin operator viewer"`
}

type LoginRequest struct {
    Username string `json:"username" binding:"required"`
    Password string `json:"password" binding:"required"`
}

type RefreshRequest struct {
    RefreshToken string `json:"refresh_token" binding:"required"`
}
```

**File:** `auth-service/internal/dto/response.go`
```go
type LoginResponse struct {
    AccessToken  string `json:"access_token"`
    RefreshToken string `json:"refresh_token"`
    ExpiresIn    int    `json:"expires_in"`     // seconds
    TokenType    string `json:"token_type"`     // "Bearer"
}

type UserResponse struct {
    ID        uuid.UUID `json:"id"`
    Username  string    `json:"username"`
    Email     string    `json:"email"`
    FullName  string    `json:"full_name"`
    Role      string    `json:"role"`
    Scopes    []string  `json:"scopes"`
    IsActive  bool      `json:"is_active"`
    CreatedAt time.Time `json:"created_at"`
}
```

### 1.2.2. Tạo JWT utility

**File:** `auth-service/pkg/jwt/jwt.go`

Logic cần implement:
1. `GenerateAccessToken(user *model.User, scopes []string) (string, error)`
   - Claims: user_id, username, role, scopes, jti (uuid), iat, exp (15min)
   - Signing method: HS256 với JWT_SECRET từ config
2. `GenerateRefreshToken(userID uuid.UUID) (string, error)`
   - Claims: user_id, jti, iat, exp (7 days)
3. `ValidateToken(tokenString string) (*Claims, error)`
   - Parse + verify signature + check expiry
4. `ExtractClaims(tokenString string) (*Claims, error)`

**Custom Claims struct:**
```go
type Claims struct {
    UserID   uuid.UUID `json:"user_id"`
    Username string    `json:"username"`
    Role     string    `json:"role"`
    Scopes   []string  `json:"scopes"`
    jwt.RegisteredClaims
}
```

### 1.2.3. Tạo Auth Service

**File:** `auth-service/internal/service/auth_service.go`

```go
type AuthService interface {
    Register(ctx context.Context, req *dto.RegisterRequest) (*dto.UserResponse, error)
    Login(ctx context.Context, req *dto.LoginRequest) (*dto.LoginResponse, error)
    RefreshToken(ctx context.Context, req *dto.RefreshRequest) (*dto.LoginResponse, error)
    Logout(ctx context.Context, tokenJTI string, tokenExp time.Time) error
    GetProfile(ctx context.Context, userID uuid.UUID) (*dto.UserResponse, error)
}
```

**Logic chi tiết cho mỗi method:**

| Method | Steps |
|--------|-------|
| `Register` | 1. Validate input → 2. Check username/email unique → 3. Hash password (bcrypt) → 4. Lookup role by name → 5. Create user in DB → 6. Return UserResponse |
| `Login` | 1. Find user by username → 2. Compare password hash → 3. Load role + permissions → 4. Generate access token → 5. Generate refresh token → 6. Store refresh JTI in Redis (TTL=7d) → 7. Update last_login_at → 8. Return tokens |
| `RefreshToken` | 1. Validate refresh token → 2. Check JTI exists in Redis → 3. Load user + role → 4. Generate new access token → 5. Return new tokens |
| `Logout` | 1. Add access token JTI to Redis blacklist (TTL = remaining exp) → 2. Delete refresh JTI from Redis |
| `GetProfile` | 1. Find user by ID → 2. Load role + permissions → 3. Return UserResponse |

---

## 1.3. Auth Service — Handler Layer

**File:** `auth-service/internal/handler/auth_handler.go`

```go
type AuthHandler struct {
    service service.AuthService
}

func NewAuthHandler(svc service.AuthService) *AuthHandler
func (h *AuthHandler) Register(c *gin.Context)     // POST /auth/register
func (h *AuthHandler) Login(c *gin.Context)         // POST /auth/login
func (h *AuthHandler) RefreshToken(c *gin.Context)  // POST /auth/refresh
func (h *AuthHandler) Logout(c *gin.Context)        // POST /auth/logout
func (h *AuthHandler) GetProfile(c *gin.Context)    // GET  /auth/profile
```

**Handler pattern cho mỗi endpoint:**
```go
func (h *AuthHandler) Login(c *gin.Context) {
    // 1. Bind JSON request
    var req dto.LoginRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.Error(c, 400, "Invalid request body", parseValidationErrors(err)...)
        return
    }
    
    // 2. Call service
    result, err := h.service.Login(c.Request.Context(), &req)
    if err != nil {
        // Handle specific errors (not found, wrong password, etc.)
        handleServiceError(c, err)
        return
    }
    
    // 3. Return success
    response.Success(c, 200, "Login successful", result)
}
```

### 1.3.1. Setup Router

**File:** `auth-service/cmd/main.go`
```go
func main() {
    // 1. Load config
    cfg := config.LoadConfig()
    
    // 2. Init logger
    log := logger.NewLogger("auth-service", &cfg.Log)
    
    // 3. Connect DB (GORM)
    db := database.Connect(cfg.Database)
    
    // 4. Connect Redis
    rdb := redis.NewClient(cfg.Redis)
    
    // 5. Init layers
    userRepo := repository.NewUserRepository(db)
    authSvc := service.NewAuthService(userRepo, rdb, cfg.JWT)
    authHandler := handler.NewAuthHandler(authSvc)
    
    // 6. Setup Gin router
    r := gin.Default()
    r.Use(middleware.RequestIDMiddleware())
    r.Use(middleware.LoggerMiddleware(log))
    
    auth := r.Group("/api/v1/auth")
    {
        auth.POST("/register", authHandler.Register)
        auth.POST("/login", authHandler.Login)
        auth.POST("/refresh", authHandler.RefreshToken)
        auth.POST("/logout", authHandler.Logout)
        auth.GET("/profile", authHandler.GetProfile)
    }
    
    // 7. Start server
    r.Run(fmt.Sprintf(":%s", cfg.App.Port))
}
```

---

## 1.4. Auth Service — Unit Tests

### Test files cần tạo:

| File | Test focus | Mock |
|------|-----------|------|
| `repository/user_repository_test.go` | DB operations | sqlmock |
| `service/auth_service_test.go` | Business logic | Mock UserRepo, Mock Redis |
| `handler/auth_handler_test.go` | HTTP request/response | Mock AuthService |
| `pkg/jwt/jwt_test.go` | Token gen/validate | None |

### Test cases cho Auth Service:

**`auth_service_test.go`:**
```
✅ TestRegister_Success
✅ TestRegister_DuplicateUsername → expect error
✅ TestRegister_DuplicateEmail → expect error
✅ TestRegister_InvalidRole → expect error
✅ TestLogin_Success → expect tokens
✅ TestLogin_WrongPassword → expect 401
✅ TestLogin_UserNotFound → expect 401
✅ TestLogin_InactiveUser → expect 403
✅ TestRefreshToken_Success → expect new access token
✅ TestRefreshToken_InvalidToken → expect error
✅ TestRefreshToken_RevokedToken → expect error
✅ TestLogout_Success → token added to blacklist
✅ TestGetProfile_Success
✅ TestGetProfile_NotFound
```

**`auth_handler_test.go`:**
```
✅ TestRegisterHandler_ValidBody → 201
✅ TestRegisterHandler_InvalidBody → 400
✅ TestLoginHandler_ValidCredentials → 200 + tokens
✅ TestLoginHandler_MissingFields → 400
✅ TestLogoutHandler_ValidToken → 200
```

### Chạy tests:
```bash
cd auth-service
go test ./... -v -coverprofile=coverage.out
go tool cover -func=coverage.out    # Target ≥ 90%
```

---

## 1.5. Server Service — Models & Repository

### 1.5.1. GORM Model

**File:** `server-service/internal/model/server.go`
```go
type Server struct {
    ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    ServerID    string         `gorm:"type:varchar(100);uniqueIndex;not null" json:"server_id"`
    ServerName  string         `gorm:"type:varchar(255);uniqueIndex;not null" json:"server_name"`
    Status      string         `gorm:"type:varchar(20);not null;default:'off'" json:"status"`
    IPv4        string         `gorm:"type:varchar(15);not null" json:"ipv4"`
    OS          string         `gorm:"type:varchar(100)" json:"os,omitempty"`
    CPUCores    *int           `gorm:"type:integer" json:"cpu_cores,omitempty"`
    RAMGB       *float64       `gorm:"type:decimal(10,2)" json:"ram_gb,omitempty"`
    DiskGB      *float64       `gorm:"type:decimal(10,2)" json:"disk_gb,omitempty"`
    Location    string         `gorm:"type:varchar(255)" json:"location,omitempty"`
    Description string         `gorm:"type:text" json:"description,omitempty"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Server) TableName() string { return "server_schema.servers" }
```

### 1.5.2. Repository

**File:** `server-service/internal/repository/server_repository.go`

```go
type ServerRepository interface {
    Create(ctx context.Context, server *model.Server) error
    FindByServerID(ctx context.Context, serverID string) (*model.Server, error)
    FindAll(ctx context.Context, filter *dto.ServerFilter) ([]model.Server, int64, error)
    Update(ctx context.Context, server *model.Server) error
    Delete(ctx context.Context, serverID string) error
    ExistsByServerID(ctx context.Context, serverID string) (bool, error)
    ExistsByServerName(ctx context.Context, serverName string) (bool, error)
}
```

**FindAll với filter/sort/pagination — logic chi tiết:**
```go
func (r *serverRepository) FindAll(ctx context.Context, filter *dto.ServerFilter) ([]model.Server, int64, error) {
    var servers []model.Server
    var total int64
    
    query := r.db.WithContext(ctx).Model(&model.Server{})
    
    // Apply filters
    if filter.Status != "" {
        query = query.Where("status = ?", filter.Status)
    }
    if filter.ServerName != "" {
        query = query.Where("server_name ILIKE ?", "%"+filter.ServerName+"%")
    }
    if filter.IPv4 != "" {
        query = query.Where("ipv4 LIKE ?", filter.IPv4+"%")
    }
    if filter.OS != "" {
        query = query.Where("os ILIKE ?", "%"+filter.OS+"%")
    }
    if filter.Location != "" {
        query = query.Where("location ILIKE ?", "%"+filter.Location+"%")
    }
    
    // Count total (before pagination)
    query.Count(&total)
    
    // Apply sorting
    sortBy := "created_at"
    sortOrder := "DESC"
    allowedSortFields := map[string]bool{
        "server_id": true, "server_name": true, "status": true,
        "ipv4": true, "created_at": true, "updated_at": true,
    }
    if filter.SortBy != "" && allowedSortFields[filter.SortBy] {
        sortBy = filter.SortBy
    }
    if filter.SortOrder == "asc" {
        sortOrder = "ASC"
    }
    query = query.Order(fmt.Sprintf("%s %s", sortBy, sortOrder))
    
    // Apply pagination
    page := max(filter.Page, 1)
    pageSize := min(max(filter.PageSize, 1), 100) // max 100 per page
    offset := (page - 1) * pageSize
    query = query.Offset(offset).Limit(pageSize)
    
    // Execute
    err := query.Find(&servers).Error
    return servers, total, err
}
```

> **Lưu ý bảo mật:** GORM tự động parameterize queries → chống SQL Injection. KHÔNG BAO GIỜ dùng `fmt.Sprintf` cho WHERE clause values.

---

## 1.6. Server Service — Service Layer

**File:** `server-service/internal/service/server_service.go`

```go
type ServerService interface {
    CreateServer(ctx context.Context, req *dto.CreateServerRequest) (*dto.ServerResponse, error)
    GetServer(ctx context.Context, serverID string) (*dto.ServerResponse, error)
    ListServers(ctx context.Context, filter *dto.ServerFilter) (*dto.ListServerResponse, error)
    UpdateServer(ctx context.Context, serverID string, req *dto.UpdateServerRequest) (*dto.ServerResponse, error)
    DeleteServer(ctx context.Context, serverID string) error
}
```

**Logic chi tiết cho mỗi method:**

| Method | Steps |
|--------|-------|
| `CreateServer` | 1. Validate input → 2. Check server_id unique → 3. Check server_name unique → 4. Create in DB → 5. Invalidate Redis cache → 6. Publish `server.created` to Kafka → 7. Return ServerResponse |
| `GetServer` | 1. Check Redis cache `server:detail:{id}` → 2. If miss: query DB → 3. Set Redis cache (TTL=5min) → 4. Return |
| `ListServers` | 1. Hash filter params → Check Redis `servers:list:{hash}` → 2. If miss: query DB with filter/sort/page → 3. Set Redis cache (TTL=2min) → 4. Return paginated |
| `UpdateServer` | 1. Validate (server_id KHÔNG được thay đổi) → 2. Find server → 3. Update fields → 4. Save to DB → 5. Invalidate Redis cache → 6. Publish `server.updated` to Kafka → 7. Return |
| `DeleteServer` | 1. Find server → 2. Soft delete (GORM) → 3. Invalidate Redis cache → 4. Publish `server.deleted` to Kafka |

### DTOs

**File:** `server-service/internal/dto/request.go`
```go
type CreateServerRequest struct {
    ServerID    string   `json:"server_id" binding:"required,max=100"`
    ServerName  string   `json:"server_name" binding:"required,max=255"`
    IPv4        string   `json:"ipv4" binding:"required,ipv4"`
    OS          string   `json:"os,omitempty"`
    CPUCores    *int     `json:"cpu_cores,omitempty" binding:"omitempty,gt=0"`
    RAMGB       *float64 `json:"ram_gb,omitempty" binding:"omitempty,gt=0"`
    DiskGB      *float64 `json:"disk_gb,omitempty" binding:"omitempty,gt=0"`
    Location    string   `json:"location,omitempty"`
    Description string   `json:"description,omitempty"`
}

type UpdateServerRequest struct {
    ServerName  *string  `json:"server_name,omitempty" binding:"omitempty,max=255"`
    IPv4        *string  `json:"ipv4,omitempty" binding:"omitempty,ipv4"`
    OS          *string  `json:"os,omitempty"`
    CPUCores    *int     `json:"cpu_cores,omitempty" binding:"omitempty,gt=0"`
    RAMGB       *float64 `json:"ram_gb,omitempty" binding:"omitempty,gt=0"`
    DiskGB      *float64 `json:"disk_gb,omitempty" binding:"omitempty,gt=0"`
    Location    *string  `json:"location,omitempty"`
    Description *string  `json:"description,omitempty"`
    // NOTE: server_id KHÔNG có ở đây → không cho phép update
}

type ServerFilter struct {
    Status     string `form:"status"`
    ServerName string `form:"server_name"`
    IPv4       string `form:"ipv4"`
    OS         string `form:"os"`
    Location   string `form:"location"`
    SortBy     string `form:"sort_by" binding:"omitempty,oneof=server_id server_name status ipv4 created_at updated_at"`
    SortOrder  string `form:"sort_order" binding:"omitempty,oneof=asc desc"`
    Page       int    `form:"page" binding:"omitempty,gt=0"`
    PageSize   int    `form:"page_size" binding:"omitempty,gt=0,max=100"`
}
```

---

## 1.7. Server Service — Handler Layer

**File:** `server-service/internal/handler/server_handler.go`

5 endpoints cần implement:

| Handler method | HTTP | Logic |
|---------------|------|-------|
| `CreateServer` | POST `/servers` | Bind JSON → call service → return 201 |
| `GetServer` | GET `/servers/:server_id` | Extract param → call service → return 200 |
| `ListServers` | GET `/servers` | Bind query params (filter) → call service → return 200 paginated |
| `UpdateServer` | PUT `/servers/:server_id` | Extract param + Bind JSON → call service → return 200 |
| `DeleteServer` | DELETE `/servers/:server_id` | Extract param → call service → return 200 |

**Swagger annotations cho mỗi handler** — thêm comments dạng `@Summary`, `@Tags`, `@Param`, `@Success`, `@Failure`, `@Security`, `@Router`.

---

## 1.8. Server Service — Redis Cache

### Cache implementation trong Service layer:

```go
// Trong server_service.go

func (s *serverService) GetServer(ctx context.Context, serverID string) (*dto.ServerResponse, error) {
    // 1. Check Redis cache
    cacheKey := fmt.Sprintf("server:detail:%s", serverID)
    cached, err := s.redis.Get(ctx, cacheKey).Result()
    if err == nil {
        var resp dto.ServerResponse
        json.Unmarshal([]byte(cached), &resp)
        return &resp, nil
    }
    
    // 2. Query DB
    server, err := s.repo.FindByServerID(ctx, serverID)
    if err != nil {
        return nil, ErrNotFound
    }
    
    // 3. Cache result
    resp := mapServerToResponse(server)
    data, _ := json.Marshal(resp)
    s.redis.Set(ctx, cacheKey, data, 5*time.Minute)
    
    return resp, nil
}

// Cache invalidation helper
func (s *serverService) invalidateCache(ctx context.Context, serverID string) {
    // Delete specific server cache
    s.redis.Del(ctx, fmt.Sprintf("server:detail:%s", serverID))
    
    // Delete all list caches (pattern delete)
    iter := s.redis.Scan(ctx, 0, "servers:list:*", 100).Iterator()
    for iter.Next(ctx) {
        s.redis.Del(ctx, iter.Val())
    }
}
```

---

## 1.9. Server Service — Unit Tests

### Test cases cần implement:

**`server_service_test.go`:**
```
✅ TestCreateServer_Success
✅ TestCreateServer_DuplicateServerID → expect 409
✅ TestCreateServer_DuplicateServerName → expect 409
✅ TestCreateServer_InvalidIPv4 → expect 422
✅ TestGetServer_Success
✅ TestGetServer_FromCache → verify Redis hit
✅ TestGetServer_NotFound → expect 404
✅ TestListServers_NoFilter → expect all servers paginated
✅ TestListServers_FilterByStatus → expect filtered results
✅ TestListServers_SortByCreatedAt → expect ordered
✅ TestListServers_Pagination → expect correct page/total
✅ TestUpdateServer_Success → expect updated fields
✅ TestUpdateServer_CannotChangeServerID → expect error if server_id in body
✅ TestUpdateServer_NotFound → expect 404
✅ TestUpdateServer_DuplicateServerName → expect 409
✅ TestDeleteServer_Success
✅ TestDeleteServer_NotFound → expect 404
```

**`server_handler_test.go`:**
```
✅ TestCreateServerHandler_ValidBody → 201
✅ TestCreateServerHandler_InvalidBody → 400
✅ TestListServersHandler_DefaultPagination → 200
✅ TestListServersHandler_WithFilters → 200
✅ TestUpdateServerHandler_ValidBody → 200
✅ TestDeleteServerHandler_Success → 200
```

**`server_repository_test.go` (sqlmock):**
```
✅ TestCreate_Success
✅ TestFindByServerID_Found
✅ TestFindByServerID_NotFound
✅ TestFindAll_WithFilters
✅ TestUpdate_Success
✅ TestDelete_SoftDelete
```

---

## 1.10. API Gateway — Middleware Chain

### 1.10.1. JWT Auth Middleware

**File:** `api-gateway/internal/middleware/auth.go`

```go
func JWTAuthMiddleware(jwtSecret string, redisClient *redis.Client) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 1. Extract token from Authorization header
        authHeader := c.GetHeader("Authorization")
        if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
            response.Error(c, 401, "Missing or invalid authorization header")
            c.Abort()
            return
        }
        tokenString := strings.TrimPrefix(authHeader, "Bearer ")
        
        // 2. Parse & validate JWT
        claims, err := jwt.ValidateToken(tokenString, jwtSecret)
        if err != nil {
            response.Error(c, 401, "Invalid or expired token")
            c.Abort()
            return
        }
        
        // 3. Check blacklist (Redis)
        blacklisted, _ := redisClient.Get(c, "auth:blacklist:"+claims.ID).Result()
        if blacklisted != "" {
            response.Error(c, 401, "Token has been revoked")
            c.Abort()
            return
        }
        
        // 4. Inject claims into context & headers (for backend services)
        c.Set("user_id", claims.UserID.String())
        c.Set("username", claims.Username)
        c.Set("role", claims.Role)
        c.Set("scopes", claims.Scopes)
        
        // Forward user info to backend via headers
        c.Request.Header.Set("X-User-ID", claims.UserID.String())
        c.Request.Header.Set("X-Username", claims.Username)
        c.Request.Header.Set("X-Role", claims.Role)
        c.Request.Header.Set("X-Scopes", strings.Join(claims.Scopes, ","))
        
        c.Next()
    }
}
```

### 1.10.2. Scope Authorization Middleware

**File:** `api-gateway/internal/middleware/auth.go` (thêm vào)

```go
func ScopeMiddleware(requiredScope string) gin.HandlerFunc {
    return func(c *gin.Context) {
        scopes, exists := c.Get("scopes")
        if !exists {
            response.Error(c, 403, "No scopes found")
            c.Abort()
            return
        }
        
        userScopes := scopes.([]string)
        for _, s := range userScopes {
            if s == requiredScope {
                c.Next()
                return
            }
        }
        
        response.Error(c, 403, fmt.Sprintf("Required scope: %s", requiredScope))
        c.Abort()
    }
}
```

### 1.10.3. Rate Limiter Middleware

**File:** `api-gateway/internal/middleware/rate_limiter.go`

```go
// Sliding window rate limiter using Redis
func RateLimiterMiddleware(redisClient *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
    return func(c *gin.Context) {
        ip := c.ClientIP()
        key := fmt.Sprintf("rate:limit:%s", ip)
        
        // Redis INCR + EXPIRE pattern
        count, _ := redisClient.Incr(c, key).Result()
        if count == 1 {
            redisClient.Expire(c, key, window)
        }
        
        if count > int64(limit) {
            response.Error(c, 429, "Rate limit exceeded")
            c.Abort()
            return
        }
        
        c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
        c.Header("X-RateLimit-Remaining", strconv.Itoa(max(0, limit-int(count))))
        c.Next()
    }
}
```

### 1.10.4. Other Middlewares

- `cors.go` — CORS headers
- `logger.go` — Request logging (method, path, status, latency)
- `recovery.go` — Panic recovery

---

## 1.11. API Gateway — Reverse Proxy + Route Config

**File:** `api-gateway/internal/proxy/reverse_proxy.go`

```go
func NewReverseProxy(target string) gin.HandlerFunc {
    url, _ := url.Parse(target)
    proxy := httputil.NewSingleHostReverseProxy(url)
    
    return func(c *gin.Context) {
        proxy.ServeHTTP(c.Writer, c.Request)
    }
}
```

**File:** `api-gateway/internal/router/router.go`

```go
func SetupRouter(cfg *config.Config, redisClient *redis.Client) *gin.Engine {
    r := gin.New()
    
    // Global middleware
    r.Use(gin.Recovery())
    r.Use(middleware.RequestIDMiddleware())
    r.Use(middleware.LoggerMiddleware())
    r.Use(middleware.CORSMiddleware())
    r.Use(middleware.RateLimiterMiddleware(redisClient, cfg.RateLimit, time.Minute))
    
    // Public routes (no auth)
    public := r.Group("/api/v1")
    {
        public.Any("/auth/*path", proxy.NewReverseProxy(cfg.AuthServiceURL))
    }
    
    // Protected routes (JWT required)
    protected := r.Group("/api/v1")
    protected.Use(middleware.JWTAuthMiddleware(cfg.JWTSecret, redisClient))
    {
        // Server CRUD
        servers := protected.Group("/servers")
        {
            servers.POST("", middleware.ScopeMiddleware("server:create"),
                proxy.NewReverseProxy(cfg.ServerServiceURL))
            servers.GET("", middleware.ScopeMiddleware("server:read"),
                proxy.NewReverseProxy(cfg.ServerServiceURL))
            servers.GET("/:server_id", middleware.ScopeMiddleware("server:read"),
                proxy.NewReverseProxy(cfg.ServerServiceURL))
            servers.PUT("/:server_id", middleware.ScopeMiddleware("server:update"),
                proxy.NewReverseProxy(cfg.ServerServiceURL))
            servers.DELETE("/:server_id", middleware.ScopeMiddleware("server:delete"),
                proxy.NewReverseProxy(cfg.ServerServiceURL))
            
            // Import/Export → FileIO Service (Phase 4)
            servers.POST("/import", middleware.ScopeMiddleware("server:import"),
                proxy.NewReverseProxy(cfg.FileIOServiceURL))
            servers.GET("/import/:job_id", middleware.ScopeMiddleware("server:import"),
                proxy.NewReverseProxy(cfg.FileIOServiceURL))
            servers.POST("/export", middleware.ScopeMiddleware("server:export"),
                proxy.NewReverseProxy(cfg.FileIOServiceURL))
        }
        
        // Reports → Report Service (Phase 3)
        reports := protected.Group("/reports")
        {
            reports.GET("/summary", middleware.ScopeMiddleware("report:view"),
                proxy.NewReverseProxy(cfg.ReportServiceURL))
            reports.POST("", middleware.ScopeMiddleware("report:send"),
                proxy.NewReverseProxy(cfg.ReportServiceURL))
        }
    }
    
    return r
}
```

---

## 1.12. Integration Test

### Bước thực hiện:

1. Start infrastructure: `make infra-up`
2. Start Auth Service: `cd auth-service && go run cmd/main.go`
3. Start Server Service: `cd server-service && go run cmd/main.go`
4. Start API Gateway: `cd api-gateway && go run cmd/main.go`

### Test flow với curl:

```bash
# 1. Register
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"admin01","email":"admin@test.com","password":"Admin123!","full_name":"Admin","role_name":"admin"}'

# 2. Login → lấy access_token
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin01","password":"Admin123!"}' | jq -r '.data.access_token')

# 3. Create Server
curl -X POST http://localhost:8080/api/v1/servers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"server_id":"SRV-001","server_name":"web-01","ipv4":"192.168.1.100","os":"Ubuntu 22.04"}'

# 4. List Servers
curl http://localhost:8080/api/v1/servers?page=1&page_size=10 \
  -H "Authorization: Bearer $TOKEN"

# 5. Update Server
curl -X PUT http://localhost:8080/api/v1/servers/SRV-001 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"os":"Ubuntu 24.04","cpu_cores":8}'

# 6. Delete Server
curl -X DELETE http://localhost:8080/api/v1/servers/SRV-001 \
  -H "Authorization: Bearer $TOKEN"

# 7. Test unauthorized (viewer cannot delete)
# Login as viewer, then try DELETE → expect 403
```

---

## 1.13. Swagger Annotations

**Chạy swag init cho từng service:**
```bash
# Install swag CLI
go install github.com/swaggo/swag/cmd/swag@latest

# Generate docs
cd server-service && swag init -g cmd/main.go -o docs
cd auth-service && swag init -g cmd/main.go -o docs
```

**Thêm Swagger UI route vào Gateway hoặc từng service:**
```go
import swaggerFiles "github.com/swaggo/files"
import ginSwagger "github.com/swaggo/gin-swagger"

r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
```

---

## Deliverables Phase 1

| # | Deliverable | Verify |
|---|------------|--------|
| 1 | Auth Service: 5 API endpoints | curl test pass |
| 2 | Server Service: 5 CRUD endpoints | curl test pass |
| 3 | API Gateway: routing + JWT + rate limit | Protected routes return 401 without token |
| 4 | Redis cache: server detail + list | Cache hit visible in logs |
| 5 | Kafka events: server.created/updated/deleted | Consume logs visible |
| 6 | Unit tests | `go test ./... -cover` ≥ 90% per service |
| 7 | Swagger docs | http://localhost:8080/swagger/index.html |
| 8 | Standard error responses | Consistent JSON format |
| 9 | Structured logging | JSON logs in /var/log/vcs-sms/ |

---

> **Tiếp theo:** [Phase 2: Monitor Service →](./phase-2-monitor.md)
