package model

import "time"

// Fact is a single check result, as stored for uptime reporting.
type Fact struct {
	ServerID   string
	ServerName string
	Status     string
	CheckedAt  time.Time
	RoundID    int64
	LatencyMs  int
	ErrorCode  string
}
