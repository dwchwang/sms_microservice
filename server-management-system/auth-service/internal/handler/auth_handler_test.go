package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/vcs-sms/auth-service/internal/dto"
	"github.com/vcs-sms/auth-service/internal/service"
	sharedjwt "github.com/vcs-sms/shared/pkg/jwt"
)

// ── Mock ──

type mockAuthService struct {
	registerResult   *dto.UserResponse
	registerErr      error
	loginResult      *dto.LoginResponse
	loginErr         error
	refreshResult    *dto.LoginResponse
	refreshErr       error
	logoutErr        error
	profileResult    *dto.UserResponse
	profileErr       error
	listUsersResult  *dto.UserListResponse
	listUsersErr     error
	updateRoleResult *dto.UserResponse
	updateRoleErr    error
}

func (m *mockAuthService) Register(ctx context.Context, req *dto.RegisterRequest) (*dto.UserResponse, error) {
	return m.registerResult, m.registerErr
}
func (m *mockAuthService) Login(ctx context.Context, req *dto.LoginRequest) (*dto.LoginResponse, error) {
	return m.loginResult, m.loginErr
}
func (m *mockAuthService) RefreshToken(ctx context.Context, req *dto.RefreshRequest) (*dto.LoginResponse, error) {
	return m.refreshResult, m.refreshErr
}
func (m *mockAuthService) Logout(ctx context.Context, jti string, exp time.Time, refreshJTI string) error {
	return m.logoutErr
}
func (m *mockAuthService) GetProfile(ctx context.Context, id uuid.UUID) (*dto.UserResponse, error) {
	return m.profileResult, m.profileErr
}
func (m *mockAuthService) ListUsers(ctx context.Context, page, pageSize int) (*dto.UserListResponse, error) {
	return m.listUsersResult, m.listUsersErr
}
func (m *mockAuthService) UpdateUserRole(ctx context.Context, currentUserID uuid.UUID, targetUserID uuid.UUID, req *dto.UpdateUserRoleRequest) (*dto.UserResponse, error) {
	return m.updateRoleResult, m.updateRoleErr
}

func setupTestRouter(handler *AuthHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	auth := r.Group("/api/v1/auth")
	{
		auth.POST("/register", handler.Register)
		auth.POST("/login", handler.Login)
		auth.POST("/refresh", handler.RefreshToken)
		auth.POST("/logout", handler.Logout)
		auth.GET("/profile", handler.GetProfile)
		auth.GET("/users", handler.ListUsers)
		auth.PUT("/users/:user_id/role", handler.UpdateUserRole)
	}
	return r
}

// Helper: generate a valid test JWT token
func generateTestToken(userID, Email, role string, scopes []string, secret string) string {
	cfg := sharedjwt.TokenConfig{Secret: secret, AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	token, _, _ := sharedjwt.GenerateAccessToken(cfg, userID, Email, role, scopes)
	return token
}

// ── Register Tests ──

func TestRegisterHandler_ValidBody(t *testing.T) {
	mock := &mockAuthService{
		registerResult: &dto.UserResponse{ID: uuid.New(), Email: "new@test.com", Role: "viewer"},
	}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"Email":"newuser","email":"new@test.com","password":"password123","full_name":"New User"}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegisterHandler_InvalidBody(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"Email":""}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestRegisterHandler_ConflictError(t *testing.T) {
	mock := &mockAuthService{
		registerErr: service.ErrDuplicateEmail,
	}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"Email":"newuser","email":"new@test.com","password":"password123","full_name":"New User"}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestRegisterHandler_DuplicateEmailConflict(t *testing.T) {
	mock := &mockAuthService{registerErr: service.ErrDuplicateEmail}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"Email":"newuser","email":"new@test.com","password":"password123","full_name":"New User"}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

// ── Login Tests ──

func TestLoginHandler_ValidCredentials(t *testing.T) {
	mock := &mockAuthService{
		loginResult: &dto.LoginResponse{AccessToken: "at", RefreshToken: "rt", ExpiresIn: 900, TokenType: "Bearer"},
	}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"Email":"admin","password":"password123"}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["data"].(map[string]interface{})["access_token"] != "at" {
		t.Error("unexpected access token")
	}
}

func TestLoginHandler_MissingFields(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"Email":"admin"}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestLoginHandler_InvalidCredentials(t *testing.T) {
	mock := &mockAuthService{loginErr: service.ErrInvalidCredentials}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"Email":"admin","password":"wrong"}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestLoginHandler_InactiveAndTooManyAttempts(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
	}{
		{name: "inactive", err: service.ErrInactiveAccount, code: http.StatusForbidden},
		{name: "too many", err: service.ErrTooManyAttempts, code: http.StatusTooManyRequests},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAuthService{loginErr: tt.err}
			handler := NewAuthHandler(mock, "test-secret")
			router := setupTestRouter(handler)

			body := `{"Email":"admin","password":"password123"}`
			req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.code {
				t.Errorf("expected %d, got %d", tt.code, w.Code)
			}
		})
	}
}

// ── Refresh Tests ──

func TestRefreshHandler_Success(t *testing.T) {
	mock := &mockAuthService{
		refreshResult: &dto.LoginResponse{AccessToken: "new-at", RefreshToken: "new-rt", ExpiresIn: 900, TokenType: "Bearer"},
	}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"refresh_token":"valid-refresh"}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefreshHandler_InvalidBody(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestRefreshHandler_RevokedToken(t *testing.T) {
	mock := &mockAuthService{refreshErr: service.ErrTokenRevoked}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	body := `{"refresh_token":"revoked-refresh"}`
	req, _ := http.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ── Logout Tests ──

func TestLogoutHandler_Success(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("user-1", "test", "admin", []string{"server:read"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("POST", "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogoutHandler_NoToken(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestLogoutHandler_MalformedToken(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer not.a.real.jwt")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestLogoutHandler_ServiceError(t *testing.T) {
	mock := &mockAuthService{logoutErr: context.DeadlineExceeded}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "test", "admin", []string{"server:read"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("POST", "/api/v1/auth/logout", bytes.NewBufferString(`{"refresh_token_jti":"rt-jti"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ── GetProfile Tests ──

func TestGetProfileHandler_Success(t *testing.T) {
	mock := &mockAuthService{
		profileResult: &dto.UserResponse{ID: uuid.New(), Email: "test@test.com", Role: "admin", Scopes: []string{"server:read"}},
	}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "testuser", "admin", []string{"server:read"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("GET", "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetProfileHandler_NoToken(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "test-secret")
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/auth/profile", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetProfileHandler_InvalidToken(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	// Token signed with wrong secret
	token := generateTestToken("user-1", "test", "admin", nil, "different-secret-key-for-testing!!")
	req, _ := http.NewRequest("GET", "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetProfileHandler_InvalidUserID(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("not-a-uuid", "test", "admin", nil, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("GET", "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetProfileHandler_NotFound(t *testing.T) {
	mock := &mockAuthService{profileErr: service.ErrUserNotFound}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "test", "admin", nil, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("GET", "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ── User Management Tests ──

func TestListUsersHandler_Success(t *testing.T) {
	mock := &mockAuthService{
		listUsersResult: &dto.UserListResponse{
			Total:      1,
			Page:       1,
			PageSize:   20,
			TotalPages: 1,
			Items: []dto.UserResponse{
				{ID: uuid.New(), Email: "viewer@test.com", Role: "viewer", Scopes: []string{"server:read"}},
			},
		},
	}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("GET", "/api/v1/auth/users?page=1&page_size=20", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListUsersHandler_NormalizesPagination(t *testing.T) {
	mock := &mockAuthService{
		listUsersResult: &dto.UserListResponse{Total: 0, Page: 1, PageSize: 20, TotalPages: 0},
	}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("GET", "/api/v1/auth/users?page=-5&page_size=500", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListUsersHandler_ServiceError(t *testing.T) {
	mock := &mockAuthService{listUsersErr: context.DeadlineExceeded}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("GET", "/api/v1/auth/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestListUsersHandler_NoToken(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/auth/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestListUsersHandler_MissingUserManageScope(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "viewer", "viewer", []string{"server:read"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("GET", "/api/v1/auth/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestUpdateUserRoleHandler_Success(t *testing.T) {
	targetID := uuid.New()
	mock := &mockAuthService{
		updateRoleResult: &dto.UserResponse{ID: targetID, Email: "operator@test.com", Role: "operator", Scopes: []string{"server:read", "server:update"}},
	}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("PUT", "/api/v1/auth/users/"+targetID.String()+"/role", bytes.NewBufferString(`{"role_name":"operator"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateUserRoleHandler_MissingUserManageScope(t *testing.T) {
	targetID := uuid.New()
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "viewer", "viewer", []string{"server:read"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("PUT", "/api/v1/auth/users/"+targetID.String()+"/role", bytes.NewBufferString(`{"role_name":"admin"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestUpdateUserRoleHandler_InvalidCurrentUserID(t *testing.T) {
	targetID := uuid.New()
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("not-a-uuid", "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("PUT", "/api/v1/auth/users/"+targetID.String()+"/role", bytes.NewBufferString(`{"role_name":"admin"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestUpdateUserRoleHandler_CannotChangeOwnRole(t *testing.T) {
	targetID := uuid.New()
	mock := &mockAuthService{updateRoleErr: service.ErrCannotChangeOwnRole}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken(targetID.String(), "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("PUT", "/api/v1/auth/users/"+targetID.String()+"/role", bytes.NewBufferString(`{"role_name":"viewer"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateUserRoleHandler_InvalidUserID(t *testing.T) {
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("PUT", "/api/v1/auth/users/not-a-uuid/role", bytes.NewBufferString(`{"role_name":"operator"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateUserRoleHandler_InvalidBody(t *testing.T) {
	targetID := uuid.New()
	mock := &mockAuthService{}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("PUT", "/api/v1/auth/users/"+targetID.String()+"/role", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestUpdateUserRoleHandler_RoleNotFound(t *testing.T) {
	targetID := uuid.New()
	mock := &mockAuthService{updateRoleErr: service.ErrRoleNotFound}
	handler := NewAuthHandler(mock, "my-32-byte-secret-key-for-testing!")
	router := setupTestRouter(handler)

	token := generateTestToken("550e8400-e29b-41d4-a716-446655440000", "admin", "admin", []string{"user:list", "user:manage_role"}, "my-32-byte-secret-key-for-testing!")
	req, _ := http.NewRequest("PUT", "/api/v1/auth/users/"+targetID.String()+"/role", bytes.NewBufferString(`{"role_name":"viewer"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
