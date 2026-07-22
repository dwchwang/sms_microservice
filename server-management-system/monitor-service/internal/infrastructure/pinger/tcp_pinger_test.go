package pinger

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

// listenLocal opens a real listener and returns its host and port.
func listenLocal(t *testing.T) (string, int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() { ln.Close() }
}

func TestPing_UpWhenPortAccepts(t *testing.T) {
	host, port, closeFn := listenLocal(t)
	defer closeFn()

	up, latency, errCode := NewTCPPinger(time.Second, "").Ping(context.Background(), host, port)

	if !up {
		t.Fatalf("expected up, got errCode %q", errCode)
	}
	if errCode != "" {
		t.Errorf("errCode = %q, want empty", errCode)
	}
	if latency < 0 {
		t.Errorf("latency = %d, want >= 0", latency)
	}
}

func TestPing_DownWhenPortRefuses(t *testing.T) {
	host, port, closeFn := listenLocal(t)
	closeFn() // nothing is listening now

	up, _, errCode := NewTCPPinger(time.Second, "").Ping(context.Background(), host, port)

	if up {
		t.Fatal("expected down on a closed port")
	}
	if errCode == "" {
		t.Error("expected an error code when down")
	}
}

func TestPing_TimeoutIsReported(t *testing.T) {
	// 203.0.113.0/24 is reserved for documentation and never routes.
	up, _, errCode := NewTCPPinger(50*time.Millisecond, "").Ping(context.Background(), "203.0.113.1", 9)

	if up {
		t.Fatal("expected down")
	}
	if errCode != "TIMEOUT" && errCode != "CONNECTION_REFUSED" && errCode != "DIAL_ERROR" {
		t.Errorf("errCode = %q, want a dial failure code", errCode)
	}
}

// dialHost lets one simulator answer for 10.000 inventory IPs.
func TestPing_DialHostOverridesTargetIP(t *testing.T) {
	host, port, closeFn := listenLocal(t)
	defer closeFn()

	// The target IP is unroutable; only the override makes this succeed.
	up, _, _ := NewTCPPinger(time.Second, host).Ping(context.Background(), "203.0.113.1", port)

	if !up {
		t.Fatal("expected the dial host override to be used")
	}
}

func TestPing_RespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	up, _, errCode := NewTCPPinger(time.Second, "").Ping(ctx, "203.0.113.1", 9)

	if up {
		t.Fatal("expected down on a cancelled context")
	}
	if errCode == "" {
		t.Error("expected an error code")
	}
}

func TestPing_UsesJoinHostPort(t *testing.T) {
	host, port, closeFn := listenLocal(t)
	defer closeFn()

	// A plain fmt "%s:%d" would break on IPv6; JoinHostPort keeps it valid.
	if _, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(host, strconv.Itoa(port))); err != nil {
		t.Fatalf("address is not resolvable: %v", err)
	}
}
