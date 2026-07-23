package redisstore

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

	// uptimeIndexKey scores every server by its uptime % for the current Vietnam
	// day, so the dashboard gets the distribution with ZCOUNT and the worst
	// servers with ZRANGE. Scores refresh within one round after midnight.
	uptimeIndexKey = "monitor:uptime:index"
)

// roundTTL spans two rounds so a round's keys outlive it briefly, then go.
const roundTTL = 120 * time.Second

func roundLockKey(roundID int64) string { return fmt.Sprintf("%s%d", roundLockPrefix, roundID) }
func queueKey(roundID int64) string     { return fmt.Sprintf("%s%d", queuePrefix, roundID) }
func targetKey(serverID string) string  { return targetKeyPrefix + serverID }
func statusKey(serverID string) string  { return statusKeyPrefix + serverID }
