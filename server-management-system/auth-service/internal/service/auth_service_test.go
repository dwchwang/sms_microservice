package service

import (
	"context"
	"errors"
	"fmt"
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
	users             map[string]*model.User // key = username
	usersByID         map[uuid.UUID]*model.User
	usersByEmail      map[string]*model.User
	roles             map[string]*model.Role
	createShouldFail  bool
	findAllShouldFail bool
	updateRoleErr     error
	findFullErr       error
}

type fakeRedis struct {
	values map[string]string
	ints   map[string]int64
	setErr error
	getErr error
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{
		values: make(map[string]string),
		ints:   make(map[string]int64),
	}
}

func (r *fakeRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	if r.getErr != nil {
		return redis.NewStringResult("", r.getErr)
	}
	if v, ok := r.values[key]; ok {
		return redis.NewStringResult(v, nil)
	}
	if v, ok := r.ints[key]; ok {
		return redis.NewStringResult(fmt.Sprintf("%d", v), nil)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (r *fakeRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	if r.setErr != nil {
		return redis.NewStatusResult("", r.setErr)
	}
	r.values[key] = fmt.Sprintf("%v", value)
	return redis.NewStatusResult("OK", nil)
}

func (r *fakeRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	var deleted int64
	for _, key := range keys {
		if _, ok := r.values[key]; ok {
			delete(r.values, key)
			deleted++
		}
		if _, ok := r.ints[key]; ok {
			delete(r.ints, key)
			deleted++
		}
	}
	return redis.NewIntResult(deleted, nil)
}

func (r *fakeRedis) Incr(ctx context.Context, key string) *redis.IntCmd {
	r.ints[key]++
	return redis.NewIntResult(r.ints[key], nil)
}

func (r *fakeRedis) Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd {
	return redis.NewBoolResult(true, nil)
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
	if m.findFullErr != nil {
		return nil, m.findFullErr
	}
	return m.FindByIDWithRole(ctx, id)
}

func (m *mockUserRepo) FindAllUsers(ctx context.Context, page, pageSize int) ([]model.User, int64, error) {
	if m.findAllShouldFail {
		return nil, 0, errors.New("list users failed")
	}
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
	if m.updateRoleErr != nil {
		return m.updateRoleErr
	}
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
	repo.addRole("admin", []string{
		"server:create", "server:read", "server:update", "server:delete",
		"server:import", "server:export", "monitor:view",
		"report:view", "report:send", "user:manage",
	})
	repo.addRole("operator", []string{
		"server:create", "server:read", "server:update",
		"server:import", "server:export", "monitor:view",
		"report:view", "report:send",
	})
	repo.addRole("viewer", []string{"server:read", "server:export", "report:view"})

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

func assertScopes(t *testing.T, actual []string, expected ...string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("expected scopes %#v, got %#v", expected, actual)
	}

	seen := make(map[string]bool, len(actual))
	for _, scope := range actual {
		seen[scope] = true
	}
	for _, scope := range expected {
		if !seen[scope] {
			t.Fatalf("expected scopes %#v, got %#v", expected, actual)
		}
	}
}

// ── Register Tests ──

func TestNewAuthService_InitializesImplementation(t *testing.T) {
	repo := newMockUserRepo()
	jwtCfg := config.JWTConfig{Secret: "secret", AccessExpiryMinutes: 1, RefreshExpiryDays: 1}

	svc := NewAuthService(repo, nil, jwtCfg)
	impl, ok := svc.(*authServiceImpl)
	if !ok {
		t.Fatalf("expected *authServiceImpl, got %T", svc)
	}
	if impl.repo != repo || impl.jwtCfg.Secret != "secret" || impl.secret != "secret" {
		t.Fatalf("service not initialized from inputs: %#v", impl)
	}
}

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
	assertScopes(t, resp.Scopes, "server:read", "server:export", "report:view")
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

func TestRegister_DefaultRoleMissing(t *testing.T) {
	svc, repo := newTestAuthService()
	delete(repo.roles, "viewer")

	req := &dto.RegisterRequest{
		Username: "norole",
		Email:    "norole@test.com",
		Password: "password123",
		FullName: "No Role",
	}

	_, err := svc.Register(context.Background(), req)
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
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
	assertScopes(t, resp.Scopes,
		"server:create",
		"server:read",
		"server:update",
		"server:import",
		"server:export",
		"monitor:view",
		"report:view",
		"report:send",
	)
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

func TestUpdateUserRole_UpdateRoleError(t *testing.T) {
	svc, repo := newTestAuthService()
	admin := repo.addUser("adminupdate", "adminupdate@test.com", "password123", "admin", true)
	target := repo.addUser("targetupdate", "targetupdate@test.com", "password123", "viewer", true)
	repo.updateRoleErr = errors.New("update failed")

	_, err := svc.UpdateUserRole(context.Background(), admin.ID, target.ID, &dto.UpdateUserRoleRequest{RoleName: "operator"})
	if err == nil {
		t.Fatal("expected update role error")
	}
}

func TestUpdateUserRole_LoadUpdatedUserError(t *testing.T) {
	svc, repo := newTestAuthService()
	admin := repo.addUser("adminload", "adminload@test.com", "password123", "admin", true)
	target := repo.addUser("targetload", "targetload@test.com", "password123", "viewer", true)
	repo.findFullErr = errors.New("load failed")

	_, err := svc.UpdateUserRole(context.Background(), admin.ID, target.ID, &dto.UpdateUserRoleRequest{RoleName: "operator"})
	if err == nil {
		t.Fatal("expected load updated user error")
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

func TestListUsers_RepositoryError(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.findAllShouldFail = true

	_, err := svc.ListUsers(context.Background(), 1, 20)
	if err == nil {
		t.Fatal("expected repository error")
	}
}

func TestListUsers_UserWithoutRole(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("norolelist", "norolelist@test.com", "password123", "viewer", true)
	u.RoleID = uuid.Nil

	_, err := svc.ListUsers(context.Background(), 1, 20)
	if err == nil {
		t.Fatal("expected role missing error")
	}
}

// ── Login Tests ──

func TestLogin_Success(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("testuser", "test@test.com", "password123", "admin", true)

	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = newFakeRedis()

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

func TestLogin_StoreRefreshTokenError(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("redisfail", "redisfail@test.com", "password123", "admin", true)

	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = &fakeRedis{values: make(map[string]string), ints: make(map[string]int64), setErr: errors.New("redis down")}

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "redisfail",
		Password: "password123",
	})
	if err == nil {
		t.Fatal("expected refresh token storage error")
	}
}

func TestLogin_TooManyAttempts(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("locked", "locked@test.com", "password123", "admin", true)

	rdb := newFakeRedis()
	rdb.ints["auth:login_attempts:locked"] = 5
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = rdb

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "locked",
		Password: "password123",
	})
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("expected ErrTooManyAttempts, got %v", err)
	}
}

func TestLogin_WrongPasswordRecordsFailedAttempt(t *testing.T) {
	svc, repo := newTestAuthService()
	repo.addUser("attempt", "attempt@test.com", "password123", "admin", true)

	rdb := newFakeRedis()
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = rdb

	_, err := svc.Login(context.Background(), &dto.LoginRequest{
		Username: "attempt",
		Password: "wrongpassword",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if rdb.ints["auth:login_attempts:attempt"] != 1 {
		t.Fatalf("expected failed attempt to be recorded, got %d", rdb.ints["auth:login_attempts:attempt"])
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
	jwtSharedCfg := sharedjwt.TokenConfig{Secret: "test-jwt-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	refreshToken, _, _ := sharedjwt.GenerateRefreshToken(jwtSharedCfg, u.ID.String())

	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = newFakeRedis()

	// Don't store the token → simulate revoked
	req := &dto.RefreshRequest{RefreshToken: refreshToken}
	_, err := svc.RefreshToken(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for revoked/non-existent refresh token")
	}
}

func TestRefreshToken_ValidTokenMissingFromRedis(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("refreshmissing", "refreshmissing@test.com", "pass", "admin", true)

	jwtSharedCfg := sharedjwt.TokenConfig{
		Secret:               "test-jwt-secret",
		AccessTokenDuration:  15 * time.Minute,
		RefreshTokenDuration: 7 * 24 * time.Hour,
	}
	refreshToken, _, err := sharedjwt.GenerateRefreshToken(jwtSharedCfg, u.ID.String())
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}

	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = &fakeRedis{values: make(map[string]string), ints: make(map[string]int64), getErr: errors.New("redis down")}

	_, err = svc.RefreshToken(context.Background(), &dto.RefreshRequest{RefreshToken: refreshToken})
	if err == nil {
		t.Fatal("expected token revoked error")
	}
}

func TestRefreshToken_SuccessRotatesRefreshToken(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("refreshok", "refreshok@test.com", "pass", "admin", true)

	jwtSharedCfg := sharedjwt.TokenConfig{
		Secret:               "test-jwt-secret",
		AccessTokenDuration:  15 * time.Minute,
		RefreshTokenDuration: 7 * 24 * time.Hour,
	}
	refreshToken, refreshJTI, err := sharedjwt.GenerateRefreshToken(jwtSharedCfg, u.ID.String())
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}

	rdb := newFakeRedis()
	rdb.values[fmt.Sprintf("auth:refresh:%s", refreshJTI)] = u.ID.String()
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = rdb

	resp, err := svc.RefreshToken(context.Background(), &dto.RefreshRequest{RefreshToken: refreshToken})
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" || resp.TokenType != "Bearer" {
		t.Fatalf("unexpected refresh response: %#v", resp)
	}
	if _, ok := rdb.values[fmt.Sprintf("auth:refresh:%s", refreshJTI)]; ok {
		t.Fatal("expected old refresh token to be revoked")
	}
}

func TestRefreshToken_InvalidUserIDInRedis(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("baduid", "baduid@test.com", "pass", "admin", true)

	jwtSharedCfg := sharedjwt.TokenConfig{Secret: "test-jwt-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	refreshToken, refreshJTI, err := sharedjwt.GenerateRefreshToken(jwtSharedCfg, u.ID.String())
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}

	rdb := newFakeRedis()
	rdb.values[fmt.Sprintf("auth:refresh:%s", refreshJTI)] = "not-a-uuid"
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = rdb

	_, err = svc.RefreshToken(context.Background(), &dto.RefreshRequest{RefreshToken: refreshToken})
	if err == nil {
		t.Fatal("expected invalid user ID error")
	}
}

func TestRefreshToken_InactiveUser(t *testing.T) {
	svc, repo := newTestAuthService()
	u := repo.addUser("inactive-refresh", "inactive-refresh@test.com", "pass", "admin", false)

	jwtSharedCfg := sharedjwt.TokenConfig{Secret: "test-jwt-secret", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	refreshToken, refreshJTI, err := sharedjwt.GenerateRefreshToken(jwtSharedCfg, u.ID.String())
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}

	rdb := newFakeRedis()
	rdb.values[fmt.Sprintf("auth:refresh:%s", refreshJTI)] = u.ID.String()
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = rdb

	_, err = svc.RefreshToken(context.Background(), &dto.RefreshRequest{RefreshToken: refreshToken})
	if !errors.Is(err, ErrInactiveAccount) {
		t.Fatalf("expected ErrInactiveAccount, got %v", err)
	}
}

// ── Logout Tests ──

func TestLogout_Success(t *testing.T) {
	svc, repo := newTestAuthService()
	_ = repo.addUser("logoutuser", "logout@test.com", "pass", "admin", true)

	svcImpl := svc.(*authServiceImpl)
	rdb := newFakeRedis()
	svcImpl.redis = rdb

	// Generate a token to blacklist
	jwtSharedCfg := sharedjwt.TokenConfig{Secret: "test-jwt-secret-that-is-32-bytes!", AccessTokenDuration: 15 * time.Minute, RefreshTokenDuration: 7 * 24 * time.Hour}
	_, accessJTI, _ := sharedjwt.GenerateAccessToken(jwtSharedCfg, "user-1", "test", "admin", nil)

	err := svc.Logout(context.Background(), accessJTI, time.Now().Add(15*time.Minute), "")
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if rdb.values[fmt.Sprintf("auth:blacklist:%s", accessJTI)] != "revoked" {
		t.Fatal("expected access token JTI to be blacklisted")
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

func TestLogout_BlacklistWriteError(t *testing.T) {
	svc, _ := newTestAuthService()
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = &fakeRedis{values: make(map[string]string), ints: make(map[string]int64), setErr: errors.New("redis down")}

	err := svc.Logout(context.Background(), "some-jti", time.Now().Add(time.Hour), "refresh-jti")
	if err == nil {
		t.Fatal("expected blacklist write error")
	}
}

func TestLogout_ExpiredAccessTokenDeletesRefreshOnly(t *testing.T) {
	svc, _ := newTestAuthService()
	rdb := newFakeRedis()
	rdb.values["auth:refresh:refresh-jti"] = "user-id"
	svcImpl := svc.(*authServiceImpl)
	svcImpl.redis = rdb

	err := svc.Logout(context.Background(), "expired-jti", time.Now().Add(-time.Hour), "refresh-jti")
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if _, ok := rdb.values["auth:blacklist:expired-jti"]; ok {
		t.Fatal("expired access token should not be blacklisted")
	}
	if _, ok := rdb.values["auth:refresh:refresh-jti"]; ok {
		t.Fatal("expected refresh token to be deleted")
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
