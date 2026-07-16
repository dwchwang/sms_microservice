package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/vcs-sms/auth-service/internal/dto"
	"github.com/vcs-sms/auth-service/internal/service"
	apperrors "github.com/vcs-sms/shared/errors"
	sharedauth "github.com/vcs-sms/shared/pkg/auth"
	sharedjwt "github.com/vcs-sms/shared/pkg/jwt"
	"github.com/vcs-sms/shared/response"
)

// AuthHandler handles HTTP requests for authentication endpoints.
type AuthHandler struct {
	svc    service.AuthService
	secret string
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(svc service.AuthService, jwtSecret string) *AuthHandler {
	return &AuthHandler{
		svc:    svc,
		secret: jwtSecret,
	}
}

// Register handles POST /auth/register
// @Summary Register a new user
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body dto.RegisterRequest true "Registration details"
// @Success 201 {object} response.ApiResponse
// @Failure 400 {object} response.ApiErrorResponse
// @Failure 409 {object} response.ApiErrorResponse
// @Router /api/v1/auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Invalid request body", parseValidationErrors(err)...)
		return
	}

	result, err := h.svc.Register(c.Request.Context(), &req)
	if err != nil {
		handleAuthError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, "User registered successfully", result)
}

// Login handles POST /auth/login
// @Summary Login and get JWT tokens
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body dto.LoginRequest true "Login credentials"
// @Success 200 {object} response.ApiResponse
// @Failure 400 {object} response.ApiErrorResponse
// @Failure 401 {object} response.ApiErrorResponse
// @Router /api/v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Invalid request body", parseValidationErrors(err)...)
		return
	}

	result, err := h.svc.Login(c.Request.Context(), &req)
	if err != nil {
		handleAuthError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "Login successful", result)
}

// RefreshToken handles POST /auth/refresh
// @Summary Refresh an access token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body dto.RefreshRequest true "Refresh token"
// @Success 200 {object} response.ApiResponse
// @Failure 400 {object} response.ApiErrorResponse
// @Failure 401 {object} response.ApiErrorResponse
// @Router /api/v1/auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req dto.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, "Invalid request body", parseValidationErrors(err)...)
		return
	}

	result, err := h.svc.RefreshToken(c.Request.Context(), &req)
	if err != nil {
		handleAuthError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "Token refreshed successfully", result)
}

// Logout handles POST /auth/logout
// @Summary Logout and revoke current token
// @Tags Auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} response.ApiResponse
// @Failure 401 {object} response.ApiErrorResponse
// @Router /api/v1/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	// Extract token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		response.Error(c, http.StatusBadRequest, "Missing or invalid Authorization header")
		return
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")

	// Parse token to get JTI and expiry
	claims, err := sharedjwt.ExtractClaims(tokenString, h.secret)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid token format")
		return
	}

	// Optional: extract refresh token JTI from body
	var body struct {
		RefreshTokenJTI string `json:"refresh_token_jti"`
	}
	c.ShouldBindJSON(&body) // Ignore error — optional field

	expTime := claims.ExpiresAt.Time
	if err := h.svc.Logout(c.Request.Context(), claims.ID, expTime, body.RefreshTokenJTI); err != nil {
		response.InternalError(c, "Failed to process logout")
		return
	}

	response.Success(c, http.StatusOK, "Logged out successfully", nil)
}

// GetProfile handles GET /auth/profile
// @Summary Get current user profile
// @Tags Auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} response.ApiResponse
// @Failure 401 {object} response.ApiErrorResponse
// @Failure 404 {object} response.ApiErrorResponse
// @Router /api/v1/auth/profile [get]
func (h *AuthHandler) GetProfile(c *gin.Context) {
	// Extract token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")

	// Validate and extract claims
	claims, err := sharedjwt.ValidateToken(tokenString, h.secret)
	if err != nil {
		response.Unauthorized(c, "Invalid or expired token")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Unauthorized(c, "Invalid user identity")
		return
	}

	result, err := h.svc.GetProfile(c.Request.Context(), userID)
	if err != nil {
		handleAuthError(c, err)
		return
	}

	response.Success(c, http.StatusOK, "Profile retrieved", result)
}

// ListUsers handles GET /auth/users
// @Summary List all users
// @Tags Users
// @Security BearerAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param page_size query int false "Page size" default(20)
// @Success 200 {object} response.ApiResponse
// @Failure 401 {object} response.ApiErrorResponse
// @Failure 403 {object} response.ApiErrorResponse
// @Router /api/v1/auth/users [get]
func (h *AuthHandler) ListUsers(c *gin.Context) {
	if _, ok := h.requireScope(c, sharedauth.ScopeUserList); !ok {
		return
	}

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
// @Tags Users
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
	claims, ok := h.requireScope(c, sharedauth.ScopeUserManageRole)
	if !ok {
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

func (h *AuthHandler) requireScope(c *gin.Context, requiredScope string) (*sharedjwt.Claims, bool) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		response.Unauthorized(c, "User not authenticated")
		return nil, false
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	claims, err := sharedjwt.ValidateToken(tokenString, h.secret)
	if err != nil {
		response.Unauthorized(c, "Invalid or expired token")
		return nil, false
	}

	for _, scope := range claims.Scopes {
		if scope == requiredScope {
			return claims, true
		}
	}

	response.Forbidden(c, fmt.Sprintf("Required scope: %s", requiredScope))
	return nil, false
}

// handleAuthError maps service errors to HTTP error responses using errors.Is.
func handleAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		response.Error(c, http.StatusUnauthorized, apperrors.ErrInvalidCredentials.Code, response.FieldError{
			Field: "credentials", Code: "INVALID", Message: apperrors.ErrInvalidCredentials.Message,
		})
	case errors.Is(err, service.ErrDuplicateEmail):
		response.Conflict(c, apperrors.ErrDuplicateEmail.Message)
	case errors.Is(err, service.ErrUserNotFound) || errors.Is(err, service.ErrRoleNotFound):
		response.NotFound(c, apperrors.ErrNotFound.Message)
	case errors.Is(err, service.ErrInactiveAccount):
		response.Forbidden(c, apperrors.ErrInactiveUser.Message)
	case errors.Is(err, service.ErrTokenRevoked):
		response.Unauthorized(c, apperrors.ErrTokenRevoked.Message)
	case errors.Is(err, service.ErrTooManyAttempts):
		response.Error(c, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", response.FieldError{
			Field: "credentials", Code: "TOO_MANY_ATTEMPTS", Message: "Too many login attempts. Try again in 15 minutes.",
		})
	case errors.Is(err, service.ErrCannotChangeOwnRole):
		response.Error(c, http.StatusBadRequest, "Cannot change your own role")
	default:
		response.InternalError(c, apperrors.ErrInternalServer.Message)
	}
}

// parseValidationErrors converts Gin validation errors to field-level FieldError slice.
func parseValidationErrors(err error) []response.FieldError {
	var fieldErrors []response.FieldError

	if ve, ok := err.(validator.ValidationErrors); ok {
		for _, fe := range ve {
			fieldErrors = append(fieldErrors, response.FieldError{
				Field:   fe.Field(),
				Code:    fe.Tag(),
				Message: fmt.Sprintf("validation failed on '%s'", fe.Tag()),
			})
		}
		return fieldErrors
	}

	// Fallback
	return []response.FieldError{
		{Field: "body", Code: "VALIDATION", Message: err.Error()},
	}
}
