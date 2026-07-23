package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/monitor-service/internal/model"
	"github.com/vcs-sms/shared/timezone"
)

// vnLoc is the zone the daily uptime counters roll over in. The dashboard's
// "today" is a Vietnam calendar day, so a check at 23:59 UTC already belongs to
// the next day here.
var vnLoc = func() *time.Location {
	loc, err := timezone.Load()
	if err != nil {
		return time.UTC
	}
	return loc
}()

// vnDay is the Vietnam calendar day of checkedAt, as YYYY-MM-DD. The Lua script
// resets the day counters whenever this value changes.
func vnDay(checkedAt string) string {
	t, err := time.Parse(time.RFC3339, checkedAt)
	if err != nil {
		t = time.Now()
	}
	return t.In(vnLoc).Format("2006-01-02")
}

// RedisOps is the Redis surface Monitoring uses.
type RedisOps interface {
	// Time returns Redis server time, the shared clock for round IDs.
	Time(ctx context.Context) (time.Time, error)
	TargetsReady(ctx context.Context) (bool, error)
	ScanTargetIDs(ctx context.Context, cursor uint64, count int64) ([]string, uint64, error)
	AcquireRoundLock(ctx context.Context, roundID int64) (bool, error)
	PushQueue(ctx context.Context, roundID int64, serverIDs []string) error
	ExpireQueue(ctx context.Context, roundID int64) error
	SetCurrentRound(ctx context.Context, roundID int64) error
	CurrentRound(ctx context.Context) (int64, error)
	QueueDepth(ctx context.Context, roundID int64) (int64, error)
	// PopTarget blocks up to timeout for the next server_id of the round.
	PopTarget(ctx context.Context, roundID int64, timeout time.Duration) (string, error)
	GetTarget(ctx context.Context, serverID string) (*model.Target, error)
	ApplyStatus(ctx context.Context, t model.Target, status, checkedAt string, latencyMs int, roundID int64) (int, error)
}

type redisOps struct {
	client *redis.Client
}

// NewRedisOps creates a Redis-backed RedisOps.
func NewRedisOps(client *redis.Client) RedisOps {
	return &redisOps{client: client}
}

func (r *redisOps) Time(ctx context.Context) (time.Time, error) {
	return r.client.Time(ctx).Result()
}

func (r *redisOps) TargetsReady(ctx context.Context) (bool, error) {
	n, err := r.client.Exists(ctx, targetReadyKey).Result()
	return n > 0, err
}

// ScanTargetIDs uses SSCAN so a 10.000-member set never blocks Redis.
func (r *redisOps) ScanTargetIDs(ctx context.Context, cursor uint64, count int64) ([]string, uint64, error) {
	return r.client.SScan(ctx, targetIDsKey, cursor, "", count).Result()
}

func (r *redisOps) AcquireRoundLock(ctx context.Context, roundID int64) (bool, error) {
	return r.client.SetNX(ctx, roundLockKey(roundID), "1", roundTTL).Result()
}

func (r *redisOps) PushQueue(ctx context.Context, roundID int64, serverIDs []string) error {
	if len(serverIDs) == 0 {
		return nil
	}
	members := make([]any, len(serverIDs))
	for i, id := range serverIDs {
		members[i] = id
	}
	return r.client.RPush(ctx, queueKey(roundID), members...).Err()
}

func (r *redisOps) ExpireQueue(ctx context.Context, roundID int64) error {
	return r.client.Expire(ctx, queueKey(roundID), roundTTL).Err()
}

func (r *redisOps) SetCurrentRound(ctx context.Context, roundID int64) error {
	return r.client.Set(ctx, roundCurrentKey, roundID, roundTTL).Err()
}

// CurrentRound reports 0 when no round is loaded.
func (r *redisOps) CurrentRound(ctx context.Context) (int64, error) {
	v, err := r.client.Get(ctx, roundCurrentKey).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

func (r *redisOps) QueueDepth(ctx context.Context, roundID int64) (int64, error) {
	return r.client.LLen(ctx, queueKey(roundID)).Result()
}

// PopTarget returns an empty string when the timeout elapses with no work.
func (r *redisOps) PopTarget(ctx context.Context, roundID int64, timeout time.Duration) (string, error) {
	res, err := r.client.BRPop(ctx, timeout, queueKey(roundID)).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(res) != 2 {
		return "", nil
	}
	return res[1], nil
}

// GetTarget returns nil when the server was deleted mid-round.
func (r *redisOps) GetTarget(ctx context.Context, serverID string) (*model.Target, error) {
	fields, err := r.client.HGetAll(ctx, targetKey(serverID)).Result()
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, nil
	}
	port, err := strconv.Atoi(fields["tcp_port"])
	if err != nil {
		return nil, fmt.Errorf("target %s has invalid tcp_port %q", serverID, fields["tcp_port"])
	}
	return &model.Target{
		ServerID:   serverID,
		ServerName: fields["server_name"],
		IPv4:       fields["ipv4"],
		TCPPort:    port,
	}, nil
}

func (r *redisOps) ApplyStatus(ctx context.Context, t model.Target, status, checkedAt string, latencyMs int, roundID int64) (int, error) {
	return statusScript.Run(ctx, r.client,
		[]string{statusKey(t.ServerID), statusStream, uptimeIndexKey},
		t.ServerID, status, checkedAt, latencyMs, roundID, vnDay(checkedAt),
	).Int()
}
