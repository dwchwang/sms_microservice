package service

import "errors"

// Sentinel errors for server service business logic.
var (
	ErrServerNotFound      = errors.New("server not found")
	ErrDuplicateServerID   = errors.New("server_id already exists")
	ErrDuplicateServerName = errors.New("server_name already exists")
)
