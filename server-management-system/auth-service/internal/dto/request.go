package dto

// RegisterRequest is the request body for user registration.
// New users are always assigned the "viewer" role (least privilege).
// Role can be upgraded by an admin via PUT /auth/users/{user_id}/role.
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=100"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	FullName string `json:"full_name" binding:"required"`
}

// UpdateUserRoleRequest is the request body for changing a user's role.
type UpdateUserRoleRequest struct {
	RoleName string `json:"role_name" binding:"required,oneof=admin operator viewer"`
}

// LoginRequest is the request body for user login.
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// RefreshRequest is the request body for token refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}
