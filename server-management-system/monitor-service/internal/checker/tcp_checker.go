package checker

import (
	"context"
	"fmt"
	"net"
	"time"
)

// TCPChecker implements HealthChecker using real TCP connect.
// It dials the server's IPv4:TCPPort and measures response time.
type TCPChecker struct {
	Timeout  time.Duration
	DialHost string
}

// NewTCPChecker creates a new TCP health checker.
func NewTCPChecker(timeout time.Duration, dialHost ...string) *TCPChecker {
	checker := &TCPChecker{Timeout: timeout}
	if len(dialHost) > 0 {
		checker.DialHost = dialHost[0]
	}
	return checker
}

// Check performs a TCP health-check on the given server.
// Uses context-aware dialing so cancellation properly aborts in-flight connections.
func (c *TCPChecker) Check(ctx context.Context, server *ServerInfo) *HealthResult {
	start := time.Now()
	host := server.IPv4
	if c.DialHost != "" {
		host = c.DialHost
	}
	addr := fmt.Sprintf("%s:%d", host, server.TCPPort)

	result := &HealthResult{
		ServerID:    server.ServerID,
		ServerName:  server.ServerName,
		CheckMethod: "tcp",
		CheckedAt:   time.Now().UTC(),
	}

	// Use DialContext so context cancellation aborts the dial
	dialer := &net.Dialer{Timeout: c.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		result.Status = "off"
		result.ResponseTimeMs = 0
		result.Error = err.Error()
	} else {
		conn.Close()
		result.Status = "on"
		result.ResponseTimeMs = int(elapsed)
	}

	return result
}

// Name returns the checker name.
func (c *TCPChecker) Name() string { return "tcp" }
