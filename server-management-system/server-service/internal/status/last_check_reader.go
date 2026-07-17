package status

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	statusKeyPrefix = "monitor:status:"
	lastCheckedItem = "last_checked_at"
)

// LastCheckReader reads the display-only last_checked_at written by Monitoring.
type LastCheckReader interface {
	LastCheckedAt(ctx context.Context, serverIDs []string) map[string]time.Time
}

type redisLastCheckReader struct {
	client *redis.Client
}

// NewLastCheckReader creates a Redis-backed LastCheckReader.
func NewLastCheckReader(rdb *redis.Client) LastCheckReader {
	return &redisLastCheckReader{client: rdb}
}

// LastCheckedAt pipelines one HGET per server. Servers with no value, an
// unparseable value, or a Redis failure are simply absent from the result,
// which the caller renders as null.
func (r *redisLastCheckReader) LastCheckedAt(ctx context.Context, serverIDs []string) map[string]time.Time {
	out := make(map[string]time.Time, len(serverIDs))
	if len(serverIDs) == 0 {
		return out
	}

	pipe := r.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(serverIDs))
	for i, id := range serverIDs {
		cmds[i] = pipe.HGet(ctx, statusKeyPrefix+id, lastCheckedItem)
	}
	// redis.Nil for a missing key surfaces here; per-command results still apply.
	_, _ = pipe.Exec(ctx)

	for i, cmd := range cmds {
		raw, err := cmd.Result()
		if err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		out[serverIDs[i]] = ts.UTC()
	}
	return out
}
