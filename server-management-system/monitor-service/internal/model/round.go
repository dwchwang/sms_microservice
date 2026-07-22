package model

import "time"

const (
	// RoundSeconds is the check interval every round_id is derived from.
	RoundSeconds = 60

	StatusON  = "ON"
	StatusOFF = "OFF"
)

// RoundID buckets a wall-clock time into a round. Callers pass Redis server
// time so every instance agrees on the round even when local clocks drift.
func RoundID(t time.Time) int64 {
	return t.Unix() / RoundSeconds
}
