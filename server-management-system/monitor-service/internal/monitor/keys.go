package monitor

import (
	"fmt"
	"time"
)

// Keys owned by Server Service.
const (
	targetReadyKey  = "server:monitor-target:ready"
	targetIDsKey    = "server:monitor-target:ids"
	targetKeyPrefix = "server:monitor-target:"
)

// Keys owned by Monitoring.
const (
	roundLockPrefix = "monitor:round:lock:"
	roundCurrentKey = "monitor:round:current"
	queuePrefix     = "monitor:ping:queue:"
	statusKeyPrefix = "monitor:status:"
	statusStream    = "stream:monitor.status"
)

const (
	// RoundSeconds is the check interval every round_id is derived from.
	RoundSeconds = 60

	// roundTTL spans two rounds so a round's keys outlive it briefly, then go.
	roundTTL = 120 * time.Second

	statusON  = "ON"
	statusOFF = "OFF"
)

// RoundID buckets a wall-clock time into a round. Callers pass Redis server
// time so every instance agrees on the round even when local clocks drift.
func RoundID(t time.Time) int64 {
	return t.Unix() / RoundSeconds
}

func roundLockKey(roundID int64) string { return fmt.Sprintf("%s%d", roundLockPrefix, roundID) }
func queueKey(roundID int64) string     { return fmt.Sprintf("%s%d", queuePrefix, roundID) }
func targetKey(serverID string) string  { return targetKeyPrefix + serverID }
func statusKey(serverID string) string  { return statusKeyPrefix + serverID }
