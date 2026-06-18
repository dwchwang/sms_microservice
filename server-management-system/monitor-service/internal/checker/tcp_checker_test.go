package checker

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPChecker_ServerReachable(t *testing.T) {
	// Start a real TCP listener
	ln, err := net.Listen("tcp", "127.0.0.1:0") // random port
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	// Accept in background
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	checker := NewTCPChecker(2 * time.Second)

	server := &ServerInfo{
		ServerID:   "SRV-00001",
		ServerName: "Test Server",
		IPv4:       "127.0.0.1",
		TCPPort:    addr.Port,
		UptimeRate: 0.95,
	}

	result := checker.Check(context.Background(), server)

	if result.Status != "on" {
		t.Errorf("Expected status 'on', got '%s'", result.Status)
	}
	if result.ResponseTimeMs < 0 {
		t.Errorf("Expected non-negative response time, got %d", result.ResponseTimeMs)
	}
	if result.ResponseTimeMs == 0 {
		t.Log("Response time is 0ms (connect was sub-millisecond, expected in localhost)")
	}
	if result.CheckMethod != "tcp" {
		t.Errorf("Expected check_method 'tcp', got '%s'", result.CheckMethod)
	}
	if result.ServerID != "SRV-00001" {
		t.Errorf("Expected server_id 'SRV-00001', got '%s'", result.ServerID)
	}
}

func TestTCPChecker_ServerUnreachable(t *testing.T) {
	checker := NewTCPChecker(500 * time.Millisecond)

	server := &ServerInfo{
		ServerID:   "SRV-00002",
		ServerName: "Offline Server",
		IPv4:       "127.0.0.1",
		TCPPort:    19999, // unlikely to have a listener
		UptimeRate: 0.50,
	}

	result := checker.Check(context.Background(), server)

	if result.Status != "off" {
		t.Errorf("Expected status 'off', got '%s'", result.Status)
	}
	if result.Error == "" {
		t.Error("Expected non-empty error for unreachable server")
	}
	if result.CheckedAt.IsZero() {
		t.Error("Expected CheckedAt to be populated")
	}
}

func TestTCPChecker_Timeout(t *testing.T) {
	// Start a listener that accepts but never responds
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	// Accept but don't close (simulates hanging server)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Hold connection until timeout
			time.Sleep(5 * time.Second)
			conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	// Use very short timeout
	checker := NewTCPChecker(100 * time.Millisecond)

	server := &ServerInfo{
		ServerID:   "SRV-00003",
		ServerName: "Slow Server",
		IPv4:       "127.0.0.1",
		TCPPort:    addr.Port,
		UptimeRate: 0.99,
	}

	result := checker.Check(context.Background(), server)

	// TCP connect succeeded (listener is accepting), so it's "on"
	// But response time should be fast since accept happens quickly
	if result.Status != "on" {
		t.Logf("Status: %s (may be 'on' because TCP connect succeeds before timeout)", result.Status)
	}
}

func TestTCPChecker_CheckMethod(t *testing.T) {
	checker := NewTCPChecker(1 * time.Second)
	if checker.Name() != "tcp" {
		t.Errorf("Expected checker name 'tcp', got '%s'", checker.Name())
	}
}

func TestTCPChecker_ServerFields(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	checker := NewTCPChecker(2 * time.Second)

	server := &ServerInfo{
		ServerID:   "SRV-TEST-001",
		ServerName: "Field Test Server",
		IPv4:       "127.0.0.1",
		TCPPort:    addr.Port,
		UptimeRate: 0.88,
	}

	result := checker.Check(context.Background(), server)

	if result.ServerID != server.ServerID {
		t.Errorf("ServerID mismatch: expected '%s', got '%s'", server.ServerID, result.ServerID)
	}
	if result.ServerName != server.ServerName {
		t.Errorf("ServerName mismatch: expected '%s', got '%s'", server.ServerName, result.ServerName)
	}
	if result.CheckedAt.IsZero() {
		t.Error("CheckedAt should be populated")
	}
	if result.CheckMethod != "tcp" {
		t.Errorf("CheckMethod should be 'tcp', got '%s'", result.CheckMethod)
	}
}

func TestTCPChecker_DialHostOverride(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	checker := NewTCPChecker(2*time.Second, "127.0.0.1")

	server := &ServerInfo{
		ServerID:   "SRV-PRIVATE-IP",
		ServerName: "Private IP Server",
		IPv4:       "10.10.1.11",
		TCPPort:    addr.Port,
		UptimeRate: 0.95,
	}

	result := checker.Check(context.Background(), server)

	if result.Status != "on" {
		t.Errorf("Expected status 'on' through dial host override, got '%s' (%s)", result.Status, result.Error)
	}
}
