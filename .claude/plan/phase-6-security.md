# Phase 6: Security Enhancement — Default Viewer Role + Admin Seed + User Role Management

> **Mục tiêu:** Loại bỏ `role_name` khỏi API Register (ai cũng thành viewer), seed 1 admin trong migration, thêm API quản lý role người dùng.
> **Thời gian:** 1 buổi
> **Prerequisite:** Phase 1-5 hoàn tất, tất cả services chạy OK
> **Điểm đạt được:** Củng cố bảo mật RBAC — không ai tự phong admin được

---

## Checklist tổng quan Phase 6

- [x] **6.1** Sửa DTO — bỏ `role_name` khỏi `RegisterRequest`, thêm `UpdateUserRoleRequest`
- [x] **6.2** Mở rộng Repository — thêm `FindAllUsers`, `FindByIDFull`, `UpdateRole`
- [x] **6.3** Sửa Service — `Register()` hardcode "viewer", thêm `UpdateUserRole()`, `ListUsers()`
- [x] **6.4** Thêm Handler — `ListUsers`, `UpdateUserRole`
- [x] **6.5** Đăng ký route mới trong `main.go`
- [x] **6.6** Seed admin trong migration `000003_create_users.up.sql`
- [x] **6.7** Cập nhật `docs/api-spec.yaml` + `api-gateway/docs/api-spec.yaml`
- [x] **6.8** Cập nhật `docs/user-guide.md`
- [x] **6.9** Cập nhật `README.md`
- [x] **6.10** Cập nhật unit tests
- [x] **6.11** Cập nhật mock repository
- [x] **6.12** Build & Test & Verify

---

## 6.1. Sửa DTO — bỏ `role_name`, thêm `UpdateUserRoleRequest`

**File:** `auth-service/internal/dto/request.go`

**Hiện tại:**
```go
type RegisterRequest struct {
    Username string `json:"username" binding:"required,min=3,max=100"`
    Email    string `json:"email" binding:"required,email"`
    Password string `json:"password" binding:"required,min=8"`
    FullName string `json:"full_name" binding:"required"`
    RoleName string `json:"role_name" binding:"required,oneof=admin operator viewer"`
}
```

**Sửa thành:**
```go
type RegisterRequest struct {
    Username string `json:"username" binding:"required,min=3,max=100"`
    Email    string `json:"email" binding:"required,email"`
    Password string `json:"password" binding:"required,min=8"`
    FullName string `json:"full_name" binding:"required"`
}

type UpdateUserRoleRequest struct {
    RoleName string `json:"role_name" binding:"required,oneof=admin operator viewer"`
}
```

> `LoginRequest` và `RefreshRequest` — **GIỮ NGUYÊN**

---

## 6.2. Mở rộng Repository — thêm 3 method mới

**File:** `auth-service/internal/repository/user_repository.go`

**Thêm vào interface `UserRepository`:**
```go
// FindAllUsers returns all active users with pagination, ordered by created_at DESC.
FindAllUsers(ctx context.Context, page, pageSize int) ([]model.User, int64, error)

// FindByIDFull returns a user by ID with Role and Permissions preloaded.
FindByIDFull(ctx context.Context, id uuid.UUID) (*model.User, error)

// UpdateRole changes a user's role_id.
UpdateRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error
```

**Thêm implementation (cuối file, sau `FindRoleByName`):**
```go
// FindAllUsers retrieves all active users with role preloaded, with pagination.
func (r *userRepository) FindAllUsers(ctx context.Context, page, pageSize int) ([]model.User, int64, error) {
    var users []model.User
    var total int64

    // Count total active users
    if err := r.db.WithContext(ctx).Model(&model.User{}).Count(&total).Error; err != nil {
        return nil, 0, err
    }

    // Calculate offset
    offset := (page - 1) * pageSize

    // Fetch users with role preloaded
    err := r.db.WithContext(ctx).
        Preload("Role").
        Order("created_at DESC").
        Offset(offset).
        Limit(pageSize).
        Find(&users).Error

    return users, total, err
}

// FindByIDFull retrieves a user by UUID with role and permissions fully preloaded.
func (r *userRepository) FindByIDFull(ctx context.Context, id uuid.UUID) (*model.User, error) {
    var user model.User
    err := r.db.WithContext(ctx).
        Preload("Role.Permissions").
        Where("id = ?", id).
        First(&user).Error
    if err != nil {
        return nil, err
    }
    return &user, nil
}

// UpdateRole changes the role_id for a user.
func (r *userRepository) UpdateRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error {
    return r.db.WithContext(ctx).
        Model(&model.User{}).
        Where("id = ?", userID).
        Update("role_id", roleID).Error
}
```

---

## 6.3. Sửa Service — Register hardcode "viewer", thêm UpdateUserRole, ListUsers

**File:** `auth-service/internal/service/auth_service.go`

### 6.3.1. Sửa method `Register()` (dòng ~70)

**Hiện tại:**
```go
// 4. Lookup role by name
role, err := s.repo.FindRoleByName(ctx, req.RoleName)
```

**Sửa thành:**
```go
// 4. Assign default "viewer" role (least privilege)
role, err := s.repo.FindRoleByName(ctx, "viewer")
```

> Toàn bộ phần còn lại của `Register()` — **GIỮ NGUYÊN**

### 6.3.2. Thêm `UpdateUserRole()` vào interface & implementation

**Thêm vào interface `AuthService`:**
```go
UpdateUserRole(ctx context.Context, currentUserID uuid.UUID, targetUserID uuid.UUID, req *dto.UpdateUserRoleRequest) (*dto.UserResponse, error)
ListUsers(ctx context.Context, page, pageSize int) (*dto.UserListResponse, error)
```

**Thêm implementation (cuối file, trước helper functions):**
```go
// UpdateUserRole changes the role of a target user. Only callable by admin.
// Admin cannot change their own role.
func (s *authServiceImpl) UpdateUserRole(ctx context.Context, currentUserID uuid.UUID, targetUserID uuid.UUID, req *dto.UpdateUserRoleRequest) (*dto.UserResponse, error) {
    // Prevent admin from changing their own role
    if currentUserID == targetUserID {
        return nil, ErrCannotChangeOwnRole
    }

    // Find target user
    targetUser, err := s.repo.FindByID(ctx, targetUserID)
    if err != nil {
        return nil, fmt.Errorf("%w: %w", ErrUserNotFound, err)
    }

    // Find the requested role
    role, err := s.repo.FindRoleByName(ctx, req.RoleName)
    if err != nil {
        return nil, fmt.Errorf("%w: %w", ErrRoleNotFound, err)
    }

    // Update role
    if err := s.repo.UpdateRole(ctx, targetUser.ID, role.ID); err != nil {
        return nil, fmt.Errorf("failed to update user role: %w", err)
    }

    // Load full user with new role
    updatedUser, err := s.repo.FindByIDFull(ctx, targetUser.ID)
    if err != nil {
        return nil, fmt.Errorf("failed to load updated user: %w", err)
    }

    return s.buildUserResponse(updatedUser, updatedUser.Role), nil
}

// ListUsers returns all users with pagination.
func (s *authServiceImpl) ListUsers(ctx context.Context, page, pageSize int) (*dto.UserListResponse, error) {
    users, total, err := s.repo.FindAllUsers(ctx, page, pageSize)
    if err != nil {
        return nil, fmt.Errorf("failed to list users: %w", err)
    }

    items := make([]dto.UserResponse, len(users))
    for i, u := range users {
        items[i] = *s.buildUserResponse(&u, u.Role)
    }

    totalPages := (int(total) + pageSize - 1) / pageSize

    return &dto.UserListResponse{
        Total:      int(total),
        Page:       page,
        PageSize:   pageSize,
        TotalPages: totalPages,
        Items:      items,
    }, nil
}
```

> `Login()`, `RefreshToken()`, `Logout()`, `GetProfile()` — **GIỮ NGUYÊN**

---

## 6.4. Thêm DTO response mới

**File:** `auth-service/internal/dto/response.go`

**Thêm vào cuối file:**
```go
// UserListResponse is the paginated response for listing users.
type UserListResponse struct {
    Total      int            `json:"total"`
    Page       int            `json:"page"`
    PageSize   int            `json:"page_size"`
    TotalPages int            `json:"total_pages"`
    Items      []UserResponse `json:"items"`
}
```

---

## 6.5. Thêm Error mới

**File:** `auth-service/internal/service/errors.go`

**Thêm vào cuối file:**
```go
ErrCannotChangeOwnRole = errors.New("cannot change your own role")
```

---

## 6.6. Thêm Handler — ListUsers, UpdateUserRole

**File:** `auth-service/internal/handler/auth_handler.go`

**Thêm 2 handler mới vào cuối file (trước `handleAuthError`):**

```go
// ListUsers handles GET /auth/users
// @Summary List all users
// @Tags Auth
// @Security BearerAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(20)
// @Success 200 {object} response.ApiResponse
// @Failure 401 {object} response.ApiErrorResponse
// @Failure 403 {object} response.ApiErrorResponse
// @Router /api/v1/auth/users [get]
func (h *AuthHandler) ListUsers(c *gin.Context) {
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

    if page < 1 {
        page = 1
    }
    if pageSize < 1 || pageSize > 100 {
        pageSize = 20
    }

    result, err := h.svc.ListUsers(c.Request.Context(), page, pageSize)
    if err != nil {
        response.InternalError(c, "Failed to list users")
        return
    }

    response.Success(c, http.StatusOK, "Users retrieved", result)
}

// UpdateUserRole handles PUT /auth/users/{user_id}/role
// @Summary Update a user's role
// @Tags Auth
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param user_id path string true "User UUID"
// @Param request body dto.UpdateUserRoleRequest true "New role"
// @Success 200 {object} response.ApiResponse
// @Failure 400 {object} response.ApiErrorResponse
// @Failure 401 {object} response.ApiErrorResponse
// @Failure 403 {object} response.ApiErrorResponse
// @Failure 404 {object} response.ApiErrorResponse
// @Router /api/v1/auth/users/{user_id}/role [put]
func (h *AuthHandler) UpdateUserRole(c *gin.Context) {
    // Extract current user from JWT claims
    authHeader := c.GetHeader("Authorization")
    if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
        response.Unauthorized(c, "User not authenticated")
        return
    }
    tokenString := strings.TrimPrefix(authHeader, "Bearer ")
    claims, err := sharedjwt.ValidateToken(tokenString, h.secret)
    if err != nil {
        response.Unauthorized(c, "Invalid or expired token")
        return
    }
    currentUserID, err := uuid.Parse(claims.UserID)
    if err != nil {
        response.Unauthorized(c, "Invalid user identity")
        return
    }

    // Parse target user ID from path
    targetUserID, err := uuid.Parse(c.Param("user_id"))
    if err != nil {
        response.Error(c, http.StatusBadRequest, "Invalid user ID format")
        return
    }

    var req dto.UpdateUserRoleRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.ValidationError(c, "Invalid request body", parseValidationErrors(err)...)
        return
    }

    result, err := h.svc.UpdateUserRole(c.Request.Context(), currentUserID, targetUserID, &req)
    if err != nil {
        handleAuthError(c, err)
        return
    }

    response.Success(c, http.StatusOK, "User role updated", result)
}
```

> Cần thêm import `strconv` và `uuid` vào phần import của handler

---

## 6.7. Đăng ký route mới trong main.go

**File:** `auth-service/cmd/main.go`

**Hiện tại:**
```go
auth.POST("/register", authHandler.Register)
auth.POST("/login", authHandler.Login)
auth.POST("/refresh", authHandler.RefreshToken)
auth.POST("/logout", authHandler.Logout)
auth.GET("/profile", authHandler.GetProfile)
```

**Thêm 2 dòng:**
```go
auth.GET("/users", authHandler.ListUsers)
auth.PUT("/users/:user_id/role", authHandler.UpdateUserRole)
```

---

## 6.8. Seed admin trong migration

**File:** `migrations/auth/000003_create_users.up.sql`

**Thêm vào cuối file:**
```sql
-- Seed default admin account (password: Admin@123456)
-- bcrypt hash generated with cost 10
INSERT INTO auth_schema.users (id, username, email, password_hash, full_name, role_id, is_active)
VALUES (
    'b0000000-0000-0000-0000-000000000001',
    'admin',
    'admin@vcs.com',
    '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy',
    'System Administrator',
    'a0000000-0000-0000-0000-000000000001',
    TRUE
) ON CONFLICT (username) DO NOTHING;
```

> ⚠️ **Lưu ý:** bcrypt hash phải được generate từ `Admin@123456` bằng `bcrypt.GenerateFromPassword([]byte("Admin@123456"), bcrypt.DefaultCost)`. Hash trên chỉ là placeholder — cần chạy generate thật trước khi insert.

---

## 6.9. Cập nhật api-spec.yaml (2 files)

**Files:**
- `docs/api-spec.yaml`
- `api-gateway/docs/api-spec.yaml`

### 6.9.1. Thêm tag mới

**Thêm vào phần `tags:`:**
```yaml
  - name: Users
    description: Quản lý người dùng (Admin only)
```

### 6.9.2. Thêm schemas mới (sau `UserProfile`)

```yaml
    UpdateUserRoleRequest:
      type: object
      required: [role_name]
      properties:
        role_name:
          type: string
          enum: [admin, operator, viewer]
          description: "Vai trò mới cho user"
          example: operator

    UserListResponse:
      type: object
      properties:
        total:
          type: integer
          example: 42
        page:
          type: integer
          example: 1
        page_size:
          type: integer
          example: 20
        total_pages:
          type: integer
          example: 3
        items:
          type: array
          items:
            $ref: '#/components/schemas/UserProfile'
```

### 6.9.3. Thêm 2 paths mới (sau `/auth/profile`)

```yaml
  /auth/users:
    get:
      tags: [Users]
      summary: Lấy danh sách người dùng (Admin only)
      description: |
        **Scope:** `user:manage`
        **Role:** Admin only
      operationId: listUsers
      security:
        - BearerAuth: [user:manage]
      parameters:
        - name: page
          in: query
          schema:
            type: integer
            minimum: 1
            default: 1
          description: Trang hiện tại
        - name: page_size
          in: query
          schema:
            type: integer
            minimum: 1
            maximum: 100
            default: 20
          description: Số lượng user mỗi trang
      responses:
        '200':
          description: Danh sách người dùng
          content:
            application/json:
              schema:
                allOf:
                  - $ref: '#/components/schemas/SuccessResponse'
                  - type: object
                    properties:
                      data:
                        $ref: '#/components/schemas/UserListResponse'
        '401':
          description: Thiếu JWT token
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '403':
          description: Không có scope `user:manage`
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'

  /auth/users/{user_id}/role:
    put:
      tags: [Users]
      summary: Thay đổi role của người dùng (Admin only)
      description: |
        Admin có thể nâng cấp hoặc hạ cấp role của user khác.
        Không thể tự thay đổi role của chính mình.
        **Scope:** `user:manage`
        **Role:** Admin only
      operationId: updateUserRole
      security:
        - BearerAuth: [user:manage]
      parameters:
        - name: user_id
          in: path
          required: true
          schema:
            type: string
            format: uuid
          example: "550e8400-e29b-41d4-a716-446655440000"
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/UpdateUserRoleRequest'
      responses:
        '200':
          description: Role đã được cập nhật
          content:
            application/json:
              schema:
                allOf:
                  - $ref: '#/components/schemas/SuccessResponse'
                  - type: object
                    properties:
                      data:
                        $ref: '#/components/schemas/UserProfile'
        '400':
          description: Không thể tự đổi role của mình hoặc request không hợp lệ
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '401':
          description: Thiếu JWT token
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '403':
          description: Không có scope `user:manage`
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
        '404':
          description: User không tồn tại
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
```

### 6.9.4. Thêm error handling cho new error

**Thêm vào `handleAuthError()`:**
```go
case errors.Is(err, service.ErrCannotChangeOwnRole):
    response.Error(c, http.StatusBadRequest, "Cannot change your own role")
```

---

## 6.10. Cập nhật user-guide.md

**File:** `docs/user-guide.md`

### 6.10.1. Sửa section 2.1 (Register)

**Xóa `role_name` khỏi curl example:**
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

**Cập nhật response example — role luôn là "viewer":**
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

### 6.10.2. Thêm section 2.6 (Quản lý người dùng) — sau section 2.5

```markdown
### 2.6. Quản lý người dùng (Admin only)

> 🔒 **Yêu cầu:** Scope `user:manage`. Chỉ Admin mới có quyền này.

#### Xem danh sách người dùng

```bash
curl "http://localhost:8080/api/v1/auth/users?page=1&page_size=20" \
  -H "Authorization: Bearer $TOKEN"
```

#### Nâng cấp/hạ cấp role người dùng

```bash
curl -X PUT http://localhost:8080/api/v1/auth/users/<user_id>/role \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role_name": "operator"}'
```

> ⚠️ **Giới hạn:** Admin không thể tự thay đổi role của chính mình. Role hợp lệ: `admin`, `operator`, `viewer`.
```

---

## 6.11. Cập nhật README.md

**File:** `README.md`

### 6.11.1. Thêm 2 dòng vào bảng API Endpoints

| Method | Path | Scope | Mô tả |
|--------|------|-------|-------|
| GET | `/api/v1/auth/users` | `user:manage` | Danh sách người dùng |
| PUT | `/api/v1/auth/users/{user_id}/role` | `user:manage` | Đổi role người dùng |

### 6.11.2. Sửa curl register example

Bỏ `role_name` khỏi curl đăng ký admin trong Quick Start section.

---

## 6.12. Cập nhật Unit Tests

### 6.12.1. Mock Repository

**File:** `auth-service/internal/repository/mocks/user_repository_mock.go`

Thêm mock methods cho:
- `FindAllUsers(ctx, page, pageSize) ([]model.User, int64, error)`
- `FindByIDFull(ctx, id) (*model.User, error)`
- `UpdateRole(ctx, userID, roleID) error`

### 6.12.2. Service Test

**File:** `auth-service/internal/service/auth_service_test.go`

- Sửa test `Register` — bỏ `role_name` khỏi request, assert role = "viewer"
- Thêm test `TestUpdateUserRole_Success`
- Thêm test `TestUpdateUserRole_CannotChangeOwnRole`
- Thêm test `TestUpdateUserRole_UserNotFound`
- Thêm test `TestUpdateUserRole_RoleNotFound`
- Thêm test `TestListUsers_Success`
- Thêm test `TestListUsers_Empty`

### 6.12.3. Handler Test

**File:** `auth-service/internal/handler/auth_handler_test.go`

- Thêm test `TestListUsers_Success`
- Thêm test `TestUpdateUserRole_Success`
- Thêm test `TestUpdateUserRole_CannotChangeOwnRole`
- Thêm test `TestUpdateUserRole_InvalidUserID`

---

## Sơ đồ phụ thuộc giữa các step

```
6.1 (DTO) ─────────────────────────────────────────────────────────────────┐
6.2 (Repository) ──────────────────────────────────────────────────────────┤
     │                                                                      │
     ├──→ 6.3 (Service) ──→ 6.6 (Handler) ──→ 6.7 (Routes)                │
     │         │                                                            │
     │         └──→ 6.12 (Tests)                                            │
     │                                                                      │
     └──→ 6.12 (Mock)                                                       │
                                                                            │
6.4 (DTO Response) — song song với 6.3                                     │
6.5 (Errors) — song song với 6.3                                            │
6.8 (Migration seed) — độc lập                                              │
6.9 (api-spec.yaml ×2) — độc lập                                            │
6.10 (user-guide.md) — độc lập                                              │
6.11 (README.md) — độc lập                                                  │
```

---

## Relevant Files (chỉ những file bị sửa)

| # | File | Thay đổi |
|---|------|---------|
| 1 | `auth-service/internal/dto/request.go` | XÓA `RoleName` khỏi `RegisterRequest`, THÊM `UpdateUserRoleRequest` |
| 2 | `auth-service/internal/dto/response.go` | THÊM `UserListResponse` |
| 3 | `auth-service/internal/repository/user_repository.go` | THÊM 3 method signature + implementation |
| 4 | `auth-service/internal/repository/mocks/user_repository_mock.go` | THÊM mock cho 3 method mới |
| 5 | `auth-service/internal/service/errors.go` | THÊM `ErrCannotChangeOwnRole` |
| 6 | `auth-service/internal/service/auth_service.go` | SỬA `Register()`, THÊM `UpdateUserRole()`, `ListUsers()`, cập nhật interface |
| 7 | `auth-service/internal/handler/auth_handler.go` | THÊM `ListUsers`, `UpdateUserRole`, cập nhật `handleAuthError` |
| 8 | `auth-service/cmd/main.go` | THÊM 2 route mới |
| 9 | `migrations/auth/000003_create_users.up.sql` | THÊM INSERT admin seed |
| 10 | `docs/api-spec.yaml` | SỬA `RegisterRequest`, THÊM tag Users, schemas + paths mới |
| 11 | `api-gateway/docs/api-spec.yaml` | Đồng bộ giống `docs/api-spec.yaml` |
| 12 | `docs/user-guide.md` | SỬA section register, THÊM section user management |
| 13 | `README.md` | Cập nhật bảng API + curl example |
| 14 | `auth-service/internal/handler/auth_handler_test.go` | THÊM test cases |
| 15 | `auth-service/internal/service/auth_service_test.go` | SỬA + THÊM test cases |
| 16 | `auth-service/internal/repository/user_repository_test.go` | THÊM test cases |

---

## Files tuyệt đối KHÔNG đụng

- `shared/` — toàn bộ
- `api-gateway/internal/` — toàn bộ (trừ `docs/api-spec.yaml`)
- `server-service/`, `monitor-service/`, `report-service/`, `fileio-service/`, `tcp-simulator/` — toàn bộ
- `docker-compose.yml`, `docker-compose.dev.yml`, `Makefile`, `.env.example`
- `migrations/` — tất cả trừ `000003_create_users.up.sql`
- `deployments/` — toàn bộ
- `config/` — toàn bộ trong tất cả services
- `logs/`, `uploads/` — toàn bộ

---

## Verification Checklist

- [ ] **Build check:** `cd auth-service && go build ./...` — không lỗi biên dịch
- [ ] **Unit test:** `cd auth-service && go test ./... -cover -count=1 -v` — tất cả test pass, coverage ≥ 90%
- [ ] **Docker compose up:** `docker compose up -d --build` — 11 containers healthy
- [ ] **Register (curl):** Đăng ký user không có `role_name` → response `role: "viewer"`
- [ ] **Register (Swagger):** Swagger UI hiển thị form không có field `role_name` → đăng ký OK
- [ ] **Login admin seed:** `admin / Admin@123456` → nhận JWT với scopes đầy đủ
- [ ] **List users:** `GET /auth/users` với admin token → thấy danh sách users
- [ ] **Update role:** `PUT /auth/users/{id}/role` với admin token → role được cập nhật
- [ ] **Self-role-change blocked:** Admin cố đổi role của chính mình → 400 Bad Request
- [ ] **Viewer denied:** Viewer gọi `GET /auth/users` → 403 Forbidden
- [ ] **Legacy API intact:** 17 endpoint cũ vẫn hoạt động bình thường
