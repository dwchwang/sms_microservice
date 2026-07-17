package projection

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	targetKeyPrefix = "server:monitor-target:"
	targetIDsKey    = "server:monitor-target:ids"
	targetReadyKey  = "server:monitor-target:ready"

	// Written by Monitoring, cleared here: a deleted server must leave the uptime
	// index, or it keeps scoring in the dashboard and the worst-10 forever.
	statusKeyPrefix = "monitor:status:"
	uptimeIndexKey  = "monitor:uptime:index"

	rebuildPageSize = 1000
)

// Target is a single server Monitoring needs in order to ping it.
// ServerName is carried so Monitoring can denormalise it onto each health fact.
type Target struct {
	ServerID   string
	ServerName string
	IPv4       string
	TCPPort    int
}

// TargetSource pages through the servers that belong in the projection.
type TargetSource interface {
	NextTargets(ctx context.Context, cursor string, limit int) ([]Target, error)
}

// TargetProjection maintains the Redis view of servers Monitoring should ping.
type TargetProjection interface {
	Sync(ctx context.Context, t Target) error
	Delete(ctx context.Context, serverID string) error
	Rebuild(ctx context.Context, src TargetSource) (int, error)
}

// targetOps is the subset of Redis commands the projection needs.
type targetOps interface {
	HSet(ctx context.Context, key string, values ...any) error
	SAdd(ctx context.Context, key string, members ...any) error
	SRem(ctx context.Context, key string, members ...any) error
	ZRem(ctx context.Context, key string, members ...any) error
	Del(ctx context.Context, keys ...string) error
	WriteTargets(ctx context.Context, idsKey string, targets []Target) error
	Rename(ctx context.Context, src, dst string) error
	Set(ctx context.Context, key, value string) error
	ScanTargetHashes(ctx context.Context, cursor uint64, count int64) ([]string, uint64, error)
	SIsMember(ctx context.Context, key, member string) (bool, error)
}

type redisTargetOps struct {
	client *redis.Client
}

func (r *redisTargetOps) HSet(ctx context.Context, key string, values ...any) error {
	return r.client.HSet(ctx, key, values...).Err()
}

func (r *redisTargetOps) SAdd(ctx context.Context, key string, members ...any) error {
	return r.client.SAdd(ctx, key, members...).Err()
}

func (r *redisTargetOps) SRem(ctx context.Context, key string, members ...any) error {
	return r.client.SRem(ctx, key, members...).Err()
}

func (r *redisTargetOps) ZRem(ctx context.Context, key string, members ...any) error {
	return r.client.ZRem(ctx, key, members...).Err()
}

func (r *redisTargetOps) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

// WriteTargets pipelines the hash and set writes for a whole page of targets.
func (r *redisTargetOps) WriteTargets(ctx context.Context, idsKey string, targets []Target) error {
	pipe := r.client.Pipeline()
	for _, t := range targets {
		pipe.HSet(ctx, targetKey(t.ServerID), targetFields(t)...)
		pipe.SAdd(ctx, idsKey, t.ServerID)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (r *redisTargetOps) Rename(ctx context.Context, src, dst string) error {
	return r.client.Rename(ctx, src, dst).Err()
}

func (r *redisTargetOps) Set(ctx context.Context, key, value string) error {
	return r.client.Set(ctx, key, value, 0).Err()
}

func (r *redisTargetOps) ScanTargetHashes(ctx context.Context, cursor uint64, count int64) ([]string, uint64, error) {
	return r.client.Scan(ctx, cursor, targetKeyPrefix+"*", count).Result()
}

func (r *redisTargetOps) SIsMember(ctx context.Context, key, member string) (bool, error) {
	return r.client.SIsMember(ctx, key, member).Result()
}

type targetProjection struct {
	ops targetOps
}

// NewTargetProjection creates a Redis-backed TargetProjection.
func NewTargetProjection(rdb *redis.Client) TargetProjection {
	return &targetProjection{ops: &redisTargetOps{client: rdb}}
}

func targetKey(serverID string) string {
	return targetKeyPrefix + serverID
}

// Sync writes the hash before the ID so Monitoring never sees an ID without metadata.
func (p *targetProjection) Sync(ctx context.Context, t Target) error {
	if err := p.ops.HSet(ctx, targetKey(t.ServerID), targetFields(t)...); err != nil {
		return fmt.Errorf("failed to write target hash for %s: %w", t.ServerID, err)
	}
	if err := p.ops.SAdd(ctx, targetIDsKey, t.ServerID); err != nil {
		return fmt.Errorf("failed to add target id %s: %w", t.ServerID, err)
	}
	return nil
}

// targetFields is the hash payload Monitoring reads for one target.
func targetFields(t Target) []any {
	return []any{
		"server_name", t.ServerName,
		"ipv4", t.IPv4,
		"tcp_port", strconv.Itoa(t.TCPPort),
	}
}

// Delete removes the ID before the hash so Monitoring never picks up a deleted target.
func (p *targetProjection) Delete(ctx context.Context, serverID string) error {
	if err := p.ops.SRem(ctx, targetIDsKey, serverID); err != nil {
		return fmt.Errorf("failed to remove target id %s: %w", serverID, err)
	}
	if err := p.ops.Del(ctx, targetKey(serverID)); err != nil {
		return fmt.Errorf("failed to delete target hash for %s: %w", serverID, err)
	}
	if err := p.ops.ZRem(ctx, uptimeIndexKey, serverID); err != nil {
		return fmt.Errorf("failed to remove %s from the uptime index: %w", serverID, err)
	}
	if err := p.ops.Del(ctx, statusKeyPrefix+serverID); err != nil {
		return fmt.Errorf("failed to delete monitor status for %s: %w", serverID, err)
	}
	return nil
}

// Rebuild repopulates the projection from src and marks it ready, returning the
// number of targets written. The ID set is built under a temporary key and
// renamed in, so Monitoring never observes a half-built set.
func (p *targetProjection) Rebuild(ctx context.Context, src TargetSource) (int, error) {
	generation := strconv.FormatInt(time.Now().UnixNano(), 10)
	tempIDsKey := targetIDsKey + ":" + generation

	written := 0
	cursor := ""
	for {
		targets, err := src.NextTargets(ctx, cursor, rebuildPageSize)
		if err != nil {
			_ = p.ops.Del(ctx, tempIDsKey)
			return 0, fmt.Errorf("failed to read targets: %w", err)
		}
		if len(targets) == 0 {
			break
		}
		if err := p.ops.WriteTargets(ctx, tempIDsKey, targets); err != nil {
			_ = p.ops.Del(ctx, tempIDsKey)
			return 0, fmt.Errorf("failed to write targets: %w", err)
		}
		written += len(targets)
		cursor = targets[len(targets)-1].ServerID
	}

	// RENAME fails on a missing key, which is what an empty source produces.
	if written == 0 {
		if err := p.ops.Del(ctx, targetIDsKey); err != nil {
			return 0, fmt.Errorf("failed to clear target ids: %w", err)
		}
	} else if err := p.ops.Rename(ctx, tempIDsKey, targetIDsKey); err != nil {
		_ = p.ops.Del(ctx, tempIDsKey)
		return 0, fmt.Errorf("failed to swap target ids: %w", err)
	}

	if err := p.ops.Set(ctx, targetReadyKey, "1"); err != nil {
		return 0, fmt.Errorf("failed to set ready marker: %w", err)
	}

	if err := p.deleteOrphanHashes(ctx); err != nil {
		return written, fmt.Errorf("targets rebuilt but orphan cleanup failed: %w", err)
	}
	return written, nil
}

// deleteOrphanHashes drops target hashes whose ID is no longer in the set.
func (p *targetProjection) deleteOrphanHashes(ctx context.Context) error {
	var cursor uint64
	for {
		keys, next, err := p.ops.ScanTargetHashes(ctx, cursor, 500)
		if err != nil {
			return fmt.Errorf("failed to scan target hashes: %w", err)
		}

		var orphans []string
		for _, key := range keys {
			serverID := strings.TrimPrefix(key, targetKeyPrefix)
			// The ids key and its temporary generations share the prefix.
			if serverID == "ids" || strings.HasPrefix(serverID, "ids:") || serverID == "ready" {
				continue
			}
			member, err := p.ops.SIsMember(ctx, targetIDsKey, serverID)
			if err != nil {
				return fmt.Errorf("failed to check target id %s: %w", serverID, err)
			}
			if !member {
				orphans = append(orphans, key)
			}
		}
		if len(orphans) > 0 {
			if err := p.ops.Del(ctx, orphans...); err != nil {
				return fmt.Errorf("failed to delete orphan hashes: %w", err)
			}
		}

		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
