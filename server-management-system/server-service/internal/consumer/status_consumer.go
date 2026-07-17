package consumer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/server-service/internal/cache"
	"github.com/vcs-sms/server-service/internal/repository"
)

const (
	StatusStream  = "stream:monitor.status"
	ConsumerGroup = "server-svc"

	readCount       = 100
	readBlock       = 2 * time.Second
	reclaimInterval = 30 * time.Second
	reclaimMinIdle  = 60 * time.Second
	reclaimCount    = 100
	errorBackoff    = time.Second
)

// statusApplier is the repository behaviour the consumer needs.
type statusApplier interface {
	ApplyStatusEvent(ctx context.Context, u repository.StatusUpdate) (int64, error)
}

// StatusConsumer applies status.changed events onto the servers read model.
type StatusConsumer struct {
	rdb  *redis.Client
	repo statusApplier
	name string
	log  zerolog.Logger
}

// NewStatusConsumer creates a consumer identified by name within the group.
func NewStatusConsumer(rdb *redis.Client, repo statusApplier, name string, log zerolog.Logger) *StatusConsumer {
	return &StatusConsumer{rdb: rdb, repo: repo, name: name, log: log}
}

// Run consumes the stream until ctx is cancelled.
func (c *StatusConsumer) Run(ctx context.Context) {
	// $ on a fresh deploy: do not replay history nobody was waiting for.
	if err := c.ensureGroup(ctx, "$"); err != nil {
		c.log.Error().Err(err).Msg("Failed to create status consumer group")
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); c.readLoop(ctx) }()
	go func() { defer wg.Done(); c.reclaimLoop(ctx) }()
	wg.Wait()

	c.log.Info().Msg("Status consumer stopped")
}

// ensureGroup creates the consumer group at startID, tolerating an existing one.
func (c *StatusConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.rdb.XGroupCreateMkStream(ctx, StatusStream, ConsumerGroup, startID).Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// isMissingGroup reports that the group or stream vanished under us — Redis was
// flushed, restarted without persistence, or evicted the key.
func isMissingGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NOGROUP")
}

// recoverGroup rebuilds a group that disappeared. It starts at 0 rather than $
// so events the lost group never acked are replayed: Monitoring only publishes
// on a real transition, so skipping them would leave a server's status stale in
// PostgreSQL until it happened to change again. Replay is safe because the
// version guard makes every apply idempotent.
func (c *StatusConsumer) recoverGroup(ctx context.Context) {
	c.log.Warn().Msg("Status consumer group is missing; recreating and replaying the stream")
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Error().Err(err).Msg("Failed to recreate the status consumer group")
		c.sleep(ctx, errorBackoff)
	}
}

// readLoop consumes messages delivered to this consumer.
func (c *StatusConsumer) readLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    ConsumerGroup,
			Consumer: c.name,
			Streams:  []string{StatusStream, ">"},
			Count:    readCount,
			Block:    readBlock,
		}).Result()
		if err != nil {
			// redis.Nil just means the block elapsed with no messages.
			if errors.Is(err, redis.Nil) || ctx.Err() != nil {
				continue
			}
			if isMissingGroup(err) {
				c.recoverGroup(ctx)
				continue
			}
			c.log.Error().Err(err).Msg("Failed to read status stream")
			c.sleep(ctx, errorBackoff)
			continue
		}
		for _, stream := range streams {
			c.process(ctx, stream.Messages)
		}
	}
}

// reclaimLoop takes over messages left pending by a dead consumer.
func (c *StatusConsumer) reclaimLoop(ctx context.Context) {
	ticker := time.NewTicker(reclaimInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msgs, _, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   StatusStream,
				Group:    ConsumerGroup,
				Consumer: c.name,
				MinIdle:  reclaimMinIdle,
				Start:    "0",
				Count:    reclaimCount,
			}).Result()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				// The read loop recreates the group; skip this tick.
				if isMissingGroup(err) {
					continue
				}
				c.log.Error().Err(err).Msg("Failed to reclaim pending status events")
				continue
			}
			if len(msgs) > 0 {
				c.log.Info().Int("count", len(msgs)).Msg("Reclaimed pending status events")
				c.process(ctx, msgs)
			}
		}
	}
}

// process applies a batch, then acks and bumps the cache version once.
func (c *StatusConsumer) process(ctx context.Context, msgs []redis.XMessage) {
	var ackIDs []string
	bump := false

	for _, msg := range msgs {
		ack, changed := c.apply(ctx, msg)
		if ack {
			ackIDs = append(ackIDs, msg.ID)
		}
		bump = bump || changed
	}

	if len(ackIDs) > 0 {
		if err := c.rdb.XAck(ctx, StatusStream, ConsumerGroup, ackIDs...).Err(); err != nil {
			c.log.Error().Err(err).Msg("Failed to ack status events")
		}
	}
	if bump {
		if err := c.rdb.Incr(ctx, cache.ListVersionKey).Err(); err != nil {
			c.log.Error().Err(err).Msg("Failed to bump list version")
		}
	}
}

// apply handles one message, reporting whether to ack it and whether it
// actually changed a row.
func (c *StatusConsumer) apply(ctx context.Context, msg redis.XMessage) (ack, changed bool) {
	update, err := parseStatusEvent(msg)
	if err != nil {
		// Acked deliberately: an unparseable message would be redelivered forever.
		c.log.Error().Err(err).Str("message_id", msg.ID).Msg("Discarding malformed status event")
		return true, false
	}

	rows, err := c.repo.ApplyStatusEvent(ctx, update)
	if err != nil {
		// Left pending so another consumer reclaims it.
		c.log.Error().Err(err).Str("server_id", update.ServerID).Msg("Failed to apply status event")
		return false, false
	}
	if rows == 0 {
		c.log.Debug().Str("server_id", update.ServerID).Int64("version", update.StatusVersion).
			Msg("Status event skipped as stale or server missing")
	}
	return true, rows > 0
}

// parseStatusEvent reads a status.changed message off the stream.
func parseStatusEvent(msg redis.XMessage) (repository.StatusUpdate, error) {
	var u repository.StatusUpdate

	eventType, _ := msg.Values["event_type"].(string)
	if eventType != "status.changed" {
		return u, fmt.Errorf("unexpected event_type %q", eventType)
	}

	serverID, _ := msg.Values["server_id"].(string)
	if serverID == "" {
		return u, errors.New("missing server_id")
	}

	status, _ := msg.Values["status"].(string)
	if status != "ON" && status != "OFF" {
		return u, fmt.Errorf("invalid status %q", status)
	}

	rawVersion, _ := msg.Values["status_version"].(string)
	version, err := strconv.ParseInt(rawVersion, 10, 64)
	if err != nil {
		return u, fmt.Errorf("invalid status_version %q", rawVersion)
	}

	rawChangedAt, _ := msg.Values["changed_at"].(string)
	changedAt, err := time.Parse(time.RFC3339, rawChangedAt)
	if err != nil {
		return u, fmt.Errorf("invalid changed_at %q", rawChangedAt)
	}

	return repository.StatusUpdate{
		ServerID:      serverID,
		Status:        status,
		ChangedAt:     changedAt.UTC(),
		StatusVersion: version,
		StreamID:      msg.ID,
	}, nil
}

func (c *StatusConsumer) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
