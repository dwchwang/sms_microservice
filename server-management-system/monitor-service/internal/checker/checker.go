package checker

import (
	"context"
	"time"
)

// HealthResult represents the result of a single health-check.
type HealthResult struct {
	ServerID       string    `json:"server_id"`
	ServerName     string    `json:"server_name"`
	Status         string    `json:"status"` // "on" hoặc "off"
	ResponseTimeMs int       `json:"response_time_ms"`
	CheckMethod    string    `json:"check_method"` // luôn = "tcp"
	CheckedAt      time.Time `json:"checked_at"`
	Error          string    `json:"error,omitempty"`
}

// HealthChecker defines the interface for server health-checking.
type HealthChecker interface {
	Check(ctx context.Context, server *ServerInfo) *HealthResult
	Name() string
}

// ServerInfo holds the information needed to check a server.
type ServerInfo struct {
	ServerID   string
	ServerName string
	IPv4       string  // Inventory IP shown to users/reports.
	TCPPort    int     // 9001..19000
	UptimeRate float64 // stored for reference, not used by TCPChecker
}
