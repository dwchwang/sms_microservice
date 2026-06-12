package service

import "errors"

// Sentinel errors for auth service business logic.
// These are used instead of string matching in handlers.
var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrDuplicateUsername   = errors.New("username already exists")
	ErrDuplicateEmail      = errors.New("email already exists")
	ErrUserNotFound        = errors.New("user not found")
	ErrRoleNotFound        = errors.New("role not found")
	ErrInactiveAccount     = errors.New("account is deactivated")
	ErrTokenRevoked        = errors.New("refresh token revoked or not found")
	ErrTooManyAttempts     = errors.New("too many login attempts, try again in 15 minutes")
	ErrCannotChangeOwnRole = errors.New("cannot change your own role")
)
