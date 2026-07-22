package pinger

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"time"
)

// TCPPinger reports a server up when a TCP connect succeeds within the timeout.
type TCPPinger struct {
	timeout time.Duration
	// dialHost, when set, replaces the target IP so the TCP simulator can stand
	// in for 10.000 hosts on one address. Empty in production.
	dialHost string
}

// NewTCPPinger creates a TCPPinger.
func NewTCPPinger(timeout time.Duration, dialHost string) *TCPPinger {
	return &TCPPinger{timeout: timeout, dialHost: dialHost}
}

// Ping dials the target and classifies the failure when it does not connect.
func (p *TCPPinger) Ping(ctx context.Context, ipv4 string, tcpPort int) (bool, int, string) {
	host := ipv4
	if p.dialHost != "" {
		host = p.dialHost
	}
	addr := net.JoinHostPort(host, strconv.Itoa(tcpPort))

	start := time.Now()
	dialer := &net.Dialer{Timeout: p.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	latency := int(time.Since(start).Milliseconds())

	if err != nil {
		return false, latency, classify(err)
	}
	conn.Close()
	return true, latency, ""
}

// classify turns a dial error into a stable code for reporting.
func classify(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "TIMEOUT"
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "TIMEOUT"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "TIMEOUT"
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "DNS_ERROR"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return "CONNECTION_REFUSED"
	}
	return "DIAL_ERROR"
}
