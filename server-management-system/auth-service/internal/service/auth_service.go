package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/auth-service/config"
	"github.com/vcs-sms/auth-service/internal/dto"
	"github.com/vcs-sms/auth-service/internal/model"
	"github.com/vcs-sms/auth-service/internal/repository"
	sharedjwt "github.com/vcs-sms/shared/pkg/jwt"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AuthService defines the authentication business logic interface.
type AuthService interface {
	Register(ctx context.Context, req *dto.RegisterRequest) (*dto.UserResponse, error)
	Login(ctx context.Context, req *dto.LoginRequest) (*dto.LoginResponse, error)
	RefreshToken(ctx context.Context, req *dto.RefreshRequest) (*dto.LoginResponse, error)
	Logout(ctx context.Context, tokenJTI string, tokenExp time.Time, refreshJTI string) error
	GetProfile(ctx context.Context, userID uuid.UUID) (*dto.UserResponse, error)
	UpdateUserRole(ctx context.Context, currentUserID uuid.UUID, targetUserID uuid.UUID, req *dto.UpdateUserRoleRequest) (*dto.UserResponse, error)
	ListUsers(ctx context.Context, page, pageSize int) (*dto.UserListResponse, error)
}

// authServiceImpl implements AuthService.
type authServiceImpl struct {
	repo   repository.UserRepository
	redis  *redis.Client
	jwtCfg config.JWTConfig
	secret string
}

// NewAuthService creates a new AuthService instance.
func NewAuthService(repo repository.UserRepository, rdb *redis.Client, jwtCfg config.JWTConfig) AuthService {
	return &authServiceImpl{
		repo:   repo,
		redis:  rdb,
		jwtCfg: jwtCfg,
		secret: jwtCfg.Secret,
	}
}

// Register creates a new user account.
func (s *authServiceImpl) Register(ctx context.Context, req *dto.RegisterRequest) (*dto.UserResponse, error) {
	// 1. Check username uniqueness
	existing, err := s.repo.FindByUsername(ctx, req.Username)
	if err == nil && existing != nil {
		return nil, ErrDuplicateUsername
	}

	// 2. Check email uniqueness
	existing, err = s.repo.FindByEmail(ctx, req.Email)
	if err == nil && existing != nil {
		return nil, ErrDuplicateEmail
	}

	// 3. Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// 4. Assign default "viewer" role (least privilege)
	role, err := s.repo.FindRoleByName(ctx, "viewer")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRoleNotFound, err)
	}

	// 5. Create user
	user := &model.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		FullName:     req.FullName,
		RoleID:       role.ID,
		IsActive:     true,
	}

	if err := s.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// 6. Build response with role info
	return s.buildUserResponse(user, role), nil
}

// Login authenticates a user and returns JWT tokens.
func (s *authServiceImpl) Login(ctx context.Context, req *dto.LoginRequest) (*dto.LoginResponse, error) {
	// 0. Check brute-force protection
	if err := s.checkLoginAttempts(ctx, req.Username); err != nil {
		return nil, ErrTooManyAttempts
	}

	// 1. Find user by username
	user, err := s.repo.FindByUsername(ctx, req.Username)
	if err != nil {
		s.recordFailedAttempt(ctx, req.Username)
		if err == gorm.ErrRecordNotFound {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("login error: %w", err)
	}

	// 2. Check if user is active
	if !user.IsActive {
		return nil, ErrInactiveAccount
	}

	// 3. Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		s.recordFailedAttempt(ctx, req.Username)
		return nil, ErrInvalidCredentials
	}

	// Reset failed attempts on successful login
	if s.redis != nil {
		s.redis.Del(ctx, fmt.Sprintf("auth:login_attempts:%s", req.Username))
	}

	// 4. Load role with permissions
	userWithRole, err := s.repo.FindByIDWithRole(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user role: %w", err)
	}

	// Extract scopes from role permissions
	scopes := make([]string, 0)
	if userWithRole.Role != nil {
		for _, p := range userWithRole.Role.Permissions {
			scopes = append(scopes, p.Scope)
		}
	}

	// 5. Generate tokens
	jwtSharedCfg := sharedjwt.TokenConfig{
		Secret:               s.secret,
		AccessTokenDuration:  s.jwtCfg.AccessTokenDuration(),
		RefreshTokenDuration: s.jwtCfg.RefreshTokenDuration(),
	}

	accessToken, _, err := sharedjwt.GenerateAccessToken(
		jwtSharedCfg,
		user.ID.String(),
		user.Username,
		userWithRole.Role.Name,
		scopes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, refreshJTI, err := sharedjwt.GenerateRefreshToken(jwtSharedCfg, user.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// 6. Store refresh token JTI in Redis
	refreshKey := fmt.Sprintf("auth:refresh:%s", refreshJTI)
	if err := s.redis.Set(ctx, refreshKey, user.ID.String(), s.jwtCfg.RefreshTokenDuration()).Err(); err != nil {
		return nil, fmt.Errorf("failed to store refresh token: %w", err)
	}

	// 7. Update last login
	_ = s.repo.UpdateLastLogin(ctx, user.ID)

	// 8. Return tokens
	return &dto.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(s.jwtCfg.AccessTokenDuration().Seconds()),
		TokenType:    "Bearer",
	}, nil
}

// RefreshToken generates new tokens using a valid refresh token.
func (s *authServiceImpl) RefreshToken(ctx context.Context, req *dto.RefreshRequest) (*dto.LoginResponse, error) {
	// 1. Validate refresh token
	claims, err := sharedjwt.ValidateToken(req.RefreshToken, s.secret)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired refresh token: %w", err)
	}

	// 2. Check if refresh JTI exists in Redis
	refreshKey := fmt.Sprintf("auth:refresh:%s", claims.ID)
	userIDStr, err := s.redis.Get(ctx, refreshKey).Result()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTokenRevoked, err)
	}

	// 3. Parse user ID and load user
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID in refresh token")
	}

	userWithRole, err := s.repo.FindByIDWithRole(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	if !userWithRole.IsActive {
		return nil, ErrInactiveAccount
	}

	// 4. Extract scopes
	scopes := make([]string, 0)
	roleName := ""
	if userWithRole.Role != nil {
		roleName = userWithRole.Role.Name
		for _, p := range userWithRole.Role.Permissions {
			scopes = append(scopes, p.Scope)
		}
	}

	// 5. Generate access token + NEW refresh token (rotation)
	jwtSharedCfg := sharedjwt.TokenConfig{
		Secret:               s.secret,
		AccessTokenDuration:  s.jwtCfg.AccessTokenDuration(),
		RefreshTokenDuration: s.jwtCfg.RefreshTokenDuration(),
	}

	accessToken, _, err := sharedjwt.GenerateAccessToken(
		jwtSharedCfg,
		userID.String(),
		userWithRole.Username,
		roleName,
		scopes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	// Generate new refresh token and rotate
	newRefreshToken, newRefreshJTI, err := sharedjwt.GenerateRefreshToken(jwtSharedCfg, userID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to generate new refresh token: %w", err)
	}

	// Revoke old refresh token + store new one
	newRefreshKey := fmt.Sprintf("auth:refresh:%s", newRefreshJTI)
	if s.redis != nil {
		s.redis.Del(ctx, refreshKey) // Delete old refresh token
		s.redis.Set(ctx, newRefreshKey, userID.String(), s.jwtCfg.RefreshTokenDuration())
	}

	return &dto.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresIn:    int(s.jwtCfg.AccessTokenDuration().Seconds()),
		TokenType:    "Bearer",
	}, nil
}

// Logout revokes the current access token and removes the refresh token.
func (s *authServiceImpl) Logout(ctx context.Context, tokenJTI string, tokenExp time.Time, refreshJTI string) error {
	if s.redis == nil {
		return fmt.Errorf("redis not available")
	}

	// 1. Add access token JTI to blacklist
	blacklistKey := fmt.Sprintf("auth:blacklist:%s", tokenJTI)
	remainingTTL := time.Until(tokenExp)
	if remainingTTL > 0 {
		if err := s.redis.Set(ctx, blacklistKey, "revoked", remainingTTL).Err(); err != nil {
			return fmt.Errorf("failed to blacklist access token: %w", err)
		}
	}

	// 2. Revoke refresh token if provided
	if refreshJTI != "" {
		refreshKey := fmt.Sprintf("auth:refresh:%s", refreshJTI)
		s.redis.Del(ctx, refreshKey)
	}

	return nil
}

// GetProfile retrieves the current user's profile.
func (s *authServiceImpl) GetProfile(ctx context.Context, userID uuid.UUID) (*dto.UserResponse, error) {
	userWithRole, err := s.repo.FindByIDWithRole(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUserNotFound, err)
	}

	if userWithRole.Role == nil {
		return nil, fmt.Errorf("user role not found")
	}

	return s.buildUserResponse(userWithRole, userWithRole.Role), nil
}

// buildUserResponse constructs a UserResponse from model entities.
func (s *authServiceImpl) buildUserResponse(user *model.User, role *model.Role) *dto.UserResponse {
	scopes := make([]string, 0)
	for _, p := range role.Permissions {
		scopes = append(scopes, p.Scope)
	}

	return &dto.UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		FullName:  user.FullName,
		Role:      role.Name,
		Scopes:    scopes,
		IsActive:  user.IsActive,
		CreatedAt: user.CreatedAt,
	}
}

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
		if u.Role == nil {
			return nil, fmt.Errorf("user role not found")
		}
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

// checkLoginAttempts checks if the user has exceeded the maximum failed login attempts.
func (s *authServiceImpl) checkLoginAttempts(ctx context.Context, username string) error {
	if s.redis == nil {
		return nil // Pass through if Redis is unavailable
	}
	key := fmt.Sprintf("auth:login_attempts:%s", username)
	count, err := s.redis.Get(ctx, key).Int()
	if err != nil {
		return nil // Key doesn't exist yet, no attempts
	}
	if count >= 5 {
		return fmt.Errorf("too many login attempts, try again in 15 minutes")
	}
	return nil
}

// recordFailedAttempt increments the failed login counter for a username.
func (s *authServiceImpl) recordFailedAttempt(ctx context.Context, username string) {
	if s.redis == nil {
		return
	}
	key := fmt.Sprintf("auth:login_attempts:%s", username)
	s.redis.Incr(ctx, key)
	s.redis.Expire(ctx, key, 15*time.Minute)
}
