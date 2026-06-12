package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/auth-service/config"
	"github.com/vcs-sms/auth-service/internal/dto"
	"github.com/vcs-sms/auth-service/internal/model"
	sharedjwt "github.com/vcs-sms/shared/pkg/jwt"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ── Mock Repository ──

type mockUserRepo struct {
	users            map[string]*model.User // key = username
	usersByID        map[uuid.UUID]*model.User
	usersByEmail     map[string]*model.User
	roles            map[string]*model.Role
	createShouldFail bool
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:        make(map[string]*model.User),
		usersByID:    make(map[uuid.UUID]*model.User),
		usersByEmail: make(map[string]*model.User),
		roles:        make(map[string]*model.Role),
	}
}

func (m *mockUserRepo) Create(ctx context.Context, user *model.User) error {
	if m.createShouldFail {
		return errors.New("db error")
	}
	m.users[user.Username] = user
	m.usersByID[user.ID] = user
	m.usersByEmail[user.Email] = user
	return nil
}

func (m *mockUserRepo) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	u, ok := m.users[username]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return u, nil
}

func (m *mockUserRepo) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	u, ok := m.usersByEmail[email]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return u, nil
}

func (m *mockUserRepo) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	u, ok := m.usersByID[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return u, nil
}

func (m *mockUserRepo) FindByIDWithRole(ctx context.Context, id uuid.UUID) (*model.User, error) {
	u, ok := m.usersByID[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	// Attach role
	if u.RoleID != uuid.Nil {
		for _, r := range m.roles {
			if r.ID == u.RoleID {
				u.Role = r
				break
			}
		}
	}
	return u, nil
}

func (m *mockUserRepo) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *mockUserRepo) FindRoleByName(ctx context.Context, name string) (*model.Role, error) {
	r, ok := m.roles[name]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return r, nil
}

func (m *mockUserRepo) FindByIDFull(ctx context.Context, id uuid.UUID) (*model.User, error) {
	return m.FindByIDWithRole(ctx, id)
}

func (m *mockUserRepo) FindAllUsers(ctx context.Context, page, pageSize int) ([]model.User, int64, error) {
	var users []model.User
	for _, u := range m.users {
		user := *u
		if user.RoleID != uuid.Nil {
			for _, r := range m.roles {
				if r.ID == user.RoleID {
					user.Role = r
					break
				}
			}
		}
		users = append(users, user)
	}

	total := int64(len(users))
	start := (page - 1) * pageSize
	if start >= len(users) {
		return []model.User{}, total, nil
	}
	end := start + pageSize
	if end > len(users) {
		end = len(users)
	}
	return users[start:end], total, nil
}

func (m *mockUserRepo) UpdateRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error {
	u, ok := m.usersByID[userID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	u.RoleID = roleID
	return nil
}

func (m *mockUserRepo) addRole(name string, scopes []string) *model.Role {
	roleID := uuid.New()
	r := &model.Role{
		ID:          roleID,
		Name:        name,
		Description: name + " role",
	}
	for _, s := range scopes {
		r.Permissions = append(r.Permissions, model.RolePermission{
			ID:     uuid.New(),
			RoleID: roleID,
			Scope:  s,
		})
	}
	m.roles[name] = r
	return r
}

func (m *mockUserRepo) addUser(username, email, password, roleName string, active bool) *model.User {
	hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	role := m.roles[roleName]
	u := &model.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		PasswordHash: string(hashed),
		FullName:     username + " Full",
		RoleID:       role.ID,
		IsActive:     active,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	m.users[username] = u
	m.usersByID[u.ID] = u
	m.usersByEmail[email] = u
	return u
}

// ── Test Helper ──

func newTestAuthService() (AuthService, *mockUserRepo) {
	repo := newMockUserRepo()
	// Add default roles
	repo.addRole("admin", []string{"server:create", "server:read", "server:update", "server:delete"})
	repo.addRole("operator", []string{"server:read", "server:update"})
	repo.addRole("viewer", []string{"server:read"})

	// Use a miniredis-like approach — just pass nil redis for tests
	// Tests that need Redis should use integration tests
	jwtCfg := config.JWTConfig{
		Secret:              "test-jwt-secret",
		AccessExpiryMinutes: 15,
		RefreshExpiryDays:   7,
	}

	svc := &authServiceImpl{
		repo:   repo,
		redis:  nil, // Redis is optional for most tests
		jwtCfg: jwtCfg,
		secret: jwtCfg.Secret,
	}
	return svc, repo
}

// ── Register Tests ──

func TestRegister_Success(t *testing.T) {
	svc, _ := newTestAuthService()

	req := &dto.RegisterRequest{
		Username: "newuser",
		Email:    "new@test.com",
		Password: "password123",
		FullName: "New User",
	}

	resp, err := svc.Register(context.Background(), req)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if resp.Username != "newuser" {
		t.Errorf("expected username 'newuser', got '%s'", resp.Username)
	}
	if resp.Role != "viewer" {
		t.Errorf("expected role 'viewer' (default), got '%s'", resp.Role)
	}
	if len(resp.Scopes) != 1 {
		t.Errorf("expected 1 scope for viewer, got %d", len(resp.Scopes))
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("existing", "existing@test.com", "pass", "viewer", true)

	req := &dto.RegisterRequest{
		Username: "existing",
		Email:    "other@test.com",
		Password: "password123",
		FullName: "Dupe User",
	}

	_, err := svc.Register(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for duplicate username")
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("user1", "taken@test.com", "pass", "viewer", true)

	req := &dto.RegisterRequest{
		Username: "user2",
		Email:    "taken@test.com",
		Password: "password123",
		FullName: "Dupe Email",
	}

	_, err := svc.Register(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
}

func TestRegister_CreateError(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.createShouldFail = true

	req := &dto.RegisterRequest{
		Username: "createfail",
		Email:    "createfail@test.com",
		Password: "password123",
		FullName: "Create Fail",
	}

	_, err := svc.Register(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when repository create fails")
	}
}

// ── User Management Tests ──

func TestUpdateUserRole_Success(t *testing.T) {
	svc, repo := newTestAuthService()
	currentAdmin := repo.addUser("adminuser", "adminuser@test.com", "password123", "admin", true)
	target := repo.addUser("vieweruser", "vieweruser@test.com", "password123", "viewer", true)

	resp, err := svc.UpdateUserRole(context.Background(), currentAdmin.ID, target.ID, &dto.UpdateUserRoleRequest{RoleName: "operator"})
	if err != nil {
		t.Fatalf("UpdateUserRole failed: %v", err)
	}
	if resp.Role != "operator" {
		t.Fatalf("expected role operator, got %s", resp.Role)
	}
	if repo.usersByID[target.ID].RoleID != repo.roles["operator"].ID {
		t.Fatal("expected target role_id to be updated")
	}
	if len(resp.Scopes) != 2 {
		t.Fatalf("expected operator scopes in response, got %#v", resp.Scopes)
	}
}

func TestUpdateUserRole_CannotChangeOwnRole(t *testing.T) {
	svc, repo := newTestAuthService()
	admin := repo.addUser("selfadmin", "selfadmin@test.com", "password123", "admin", true)

	_, err := svc.UpdateUserRole(context.Background(), admin.ID, admin.ID, &dto.UpdateUserRoleRequest{RoleName: "viewer"})
	if !errors.Is(err, ErrCannotChangeOwnRole) {
		t.Fatalf("expected ErrCannotChangeOwnRole, got %v", err)
	}
}

func TestUpdateUserRole_UserNotFound(t *testing.T) {
	svc, repo := newTestAuthService()
	admin := repo.addUser("adminmissing", "adminmissing@test.com", "password123", "admin", true)

	_, err := svc.UpdateUserRole(context.Background(), admin.ID, uuid.New(), &dto.UpdateUserRoleRequest{RoleName: "viewer"})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUpdateUserRole_RoleNotFound(t *testing.T) {
	svc, repo := newTestAuthService()
	admin := repo.addUser("adminrole", "adminrole@test.com", "password123", "admin", true)
	target := repo.addUser("targetrole", "targetrole@test.com", "password123", "viewer", true)

	_, err := svc.UpdateUserRole(context.Background(), admin.ID, target.ID, &dto.UpdateUserRoleRequest{RoleName: "missing"})
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestListUsers_Success(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("user1", "user1@test.com", "password123", "viewer", true)
	repo.addUser("user2", "user2@test.com", "password123", "operator", true)

	resp, err := svc.ListUsers(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if resp.Total != 2 || resp.Page != 1 || resp.PageSize != 1 || resp.TotalPages != 2 {
		t.Fatalf("unexpected pagination response: %#v", resp)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected one paginated item, got %d", len(resp.Items))
	}
	if resp.Items[0].Role == "" {
		t.Fatal("expected role in list response")
	}
	if len(resp.Items[0].Scopes) == 0 {
		t.Fatal("expected scopes in list response")
	}
}

func TestListUsers_Empty(t *testing.T) {
	svc, _ := newTestAuthService()

	resp, err := svc.ListUsers(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if resp.Total != 0 || resp.TotalPages != 0 || len(resp.Items) != 0 {
		t.Fatalf("unexpected empty response: %#v", resp)
	}
}

// ── Login Tests ──

func TestLogin_Success(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("testuser", "test@test.com", "password123", "admin", true)

	// Try Redis connection — skip if unavailable
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	if err := svcImpl.redis.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not available, skipping login test: %v", err)
	}

	req := &dto.LoginRequest{
		Username: "testuser",
		Password: "password123",
	}

	resp, err := svc.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}
	if resp.RefreshToken == "" {
		t.Fatal("expected non-empty refresh token")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("expected TokenType 'Bearer', got '%s'", resp.TokenType)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("testuser", "test@test.com", "password123", "admin", true)

	req := &dto.LoginRequest{
		Username: "testuser",
		Password: "wrongpassword",
	}

	_, err := svc.Login(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	svc, _ := newTestAuthService()

	req := &dto.LoginRequest{
		Username: "nonexistent",
		Password: "password",
	}

	_, err := svc.Login(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
}

func TestLogin_InactiveUser(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("inactive", "inactive@test.com", "password123", "admin", false)

	req := &dto.LoginRequest{
		Username: "inactive",
		Password: "password123",
	}

	_, err := svc.Login(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for inactive user")
	}
}

func TestLogin_LoadRoleError(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("noroleload", "noroleload@test.com", "password123", "admin", true)
	delete(repo.usersByID, u.ID)

	req := &dto.LoginRequest{
		Username: "noroleload",
		Password: "password123",
	}

	_, err := svc.Login(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when role lookup fails")
	}
}

// ── GetProfile Tests ──

func TestGetProfile_Success(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("profileuser", "profile@test.com", "pass", "admin", true)

	resp, err := svc.GetProfile(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}
	if resp.Username != "profileuser" {
		t.Errorf("expected username 'profileuser', got '%s'", resp.Username)
	}
	if resp.Role != "admin" {
		t.Errorf("expected role 'admin', got '%s'", resp.Role)
	}
}

func TestGetProfile_NotFound(t *testing.T) {
	svc, _ := newTestAuthService()

	_, err := svc.GetProfile(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
}

func TestGetProfile_RoleMissing(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("missingrole", "missingrole@test.com", "pass", "viewer", true)
	u.RoleID = uuid.Nil

	_, err := svc.GetProfile(context.Background(), u.ID)
	if err == nil {
		t.Fatal("expected error for user without role")
	}
}

// ── RefreshToken Tests ──

func TestRefreshToken_InvalidToken(t *testing.T) {
	svc, _ := newTestAuthService()

	req := &dto.RefreshRequest{RefreshToken: "invalid-token"}
	_, err := svc.RefreshToken(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid refresh token")
	}
}

func TestRefreshToken_RevokedToken(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("refreshuser", "refresh@test.com", "pass", "admin", true)

	// Generate a valid refresh token
	jwtSharedCfg := sharedjwt.TokenConfig{Secret: "test-jwt-secret-that-is-32-bytes!", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	refreshToken, _, _ := sharedjwt.GenerateRefreshToken(jwtSharedCfg, u.ID.String())

	// Try to refresh without storing in Redis → should fail
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	if svcImpl.redis.Ping(context.Background()).Err() != nil {
		t.Skip("Redis not available")
	}

	// Don't store the token → simulate revoked
	req := &dto.RefreshRequest{RefreshToken: refreshToken}
	_, err := svc.RefreshToken(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for revoked/non-existent refresh token")
	}
}

// ── Logout Tests ──

func TestLogout_Success(t *testing.T) {
	svc, repo := newTestAuthService()
	_ = repo.addUser("logoutuser", "logout@test.com", "pass", "admin", true)

	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	if svcImpl.redis.Ping(context.Background()).Err() != nil {
		t.Skip("Redis not available")
	}

	// Generate a token to blacklist
	jwtSharedCfg := sharedjwt.TokenConfig{Secret: "test-jwt-secret-that-is-32-bytes!", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	_, accessJTI, _ := sharedjwt.GenerateAccessToken(jwtSharedCfg, "user-1", "test", "admin", nil)

	err := svc.Logout(context.Background(), accessJTI, time.Now().Add(15*time.Minute), "")
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
}

func TestLogout_NoRedis(t *testing.T) {
	svc, _ := newTestAuthService()
	// redis is nil → should fail
	err := svc.Logout(context.Background(), "some-jti", time.Now().Add(time.Hour), "")
	if err == nil {
		t.Fatal("expected error when Redis is not available")
	}
}

// ── Error Types Tests ──

func TestErrorTypes(t *testing.T) {
	if ErrInvalidCredentials == ErrDuplicateUsername {
		t.Error("ErrInvalidCredentials and ErrDuplicateUsername should be distinct")
	}
	if ErrUserNotFound == ErrRoleNotFound {
		t.Error("ErrUserNotFound and ErrRoleNotFound should be distinct")
	}
}
