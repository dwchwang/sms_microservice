package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/monitor-service/internal/checker"
	"github.com/vcs-sms/monitor-service/internal/model"
	"github.com/vcs-sms/monitor-service/internal/repository"
	"github.com/vcs-sms/monitor-service/internal/worker"
	"github.com/vcs-sms/shared/kafka"
)

const defaultTCPTimeoutMs = 5000

// HealthCheckScheduler orchestrates periodic health-check cycles.
type HealthCheckScheduler struct {
	pool         *worker.Pool
	serverReader repository.ServerReader
	configRepo   repository.HealthCheckConfigRepo
	esRepo       repository.ESStatusLogRepo
	redisClient  RedisClient
	kafkaProd    kafka.Producer
	logger       zerolog.Logger
	interval     time.Duration
}

// NewHealthCheckScheduler creates a new HealthCheckScheduler.
func NewHealthCheckScheduler(
	pool *worker.Pool,
	serverReader repository.ServerReader,
	configRepo repository.HealthCheckConfigRepo,
	esRepo repository.ESStatusLogRepo,
	redisClient RedisClient,
	kafkaProd kafka.Producer,
	logger zerolog.Logger,
	interval time.Duration,
) *HealthCheckScheduler {
	return &HealthCheckScheduler{
		pool:         pool,
		serverReader: serverReader,
		configRepo:   configRepo,
		esRepo:       esRepo,
		redisClient:  redisClient,
		kafkaProd:    kafkaProd,
		logger:       logger,
		interval:     interval,
	}
}

// Start begins the cron loop. Runs immediately on start, then every interval.
// Panic recovery ensures a single bad cycle doesn't crash the entire scheduler.
func (s *HealthCheckScheduler) Start(ctx context.Context) {
	s.logger.Info().
		Dur("interval", s.interval).
		Msg("Health-check scheduler started")

	// Run immediately on start (with panic recovery)
	s.runCycleSafe(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Msg("Health-check scheduler stopped")
			return
		case <-ticker.C:
			s.runCycleSafe(ctx)
		}
	}
}

// runCycleSafe wraps runCycle with panic recovery to prevent a single
// panic from crashing the entire scheduler goroutine.
func (s *HealthCheckScheduler) runCycleSafe(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error().
				Interface("panic", r).
				Msg("Health-check cycle panicked — recovered, scheduler will continue on next tick")
		}
	}()
	s.runCycle(ctx)
}

// runCycle performs one full health-check cycle.
func (s *HealthCheckScheduler) runCycle(ctx context.Context) {
	cycleStart := time.Now()
	cycleID := uuid.New().String()

	s.logger.Info().
		Str("cycle_id", cycleID).
		Msg("Health-check cycle started")

	// Step 1: Load servers (cross-schema) + configs
	servers, err := s.serverReader.GetAllActiveServers(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to load servers")
		return
	}

	configs, err := s.configRepo.GetAllEnabled(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to load health-check configs")
		return
	}

	// Build config lookup map
	configMap := make(map[string]model.HealthCheckConfig)
	maxTCPTimeoutMs := defaultTCPTimeoutMs
	for _, cfg := range configs {
		configMap[cfg.ServerID] = cfg
		if cfg.TCPTimeoutMs > maxTCPTimeoutMs {
			maxTCPTimeoutMs = cfg.TCPTimeoutMs
		}
	}

	// Step 3: Merge server + config → ServerInfo list
	var serverInfos []*checker.ServerInfo
	serverStatusMap := make(map[string]string, len(servers))
	for _, srv := range servers {
		serverStatusMap[srv.ServerID] = srv.Status
		info := &checker.ServerInfo{
			ServerID:   srv.ServerID,
			ServerName: srv.ServerName,
			IPv4:       srv.IPv4,
			TCPPort:    80,
			UptimeRate: 0.95,
		}
		if cfg, ok := configMap[srv.ServerID]; ok {
			info.TCPPort = cfg.TCPPort
			info.UptimeRate = cfg.UptimeRate
		}
		serverInfos = append(serverInfos, info)
	}

	// Step 3: Acquire distributed lock after load so TTL can match expected cycle length.
	if s.redisClient != nil {
		lockKey := "health-check-lock"
		lockTTL := calculateLockTTL(len(serverInfos), s.pool.WorkerCount(), maxTCPTimeoutMs, s.interval)
		acquired, err := s.redisClient.SetNX(ctx, lockKey, cycleID, lockTTL)
		if err != nil || !acquired {
			s.logger.Warn().
				Err(err).
				Str("cycle_id", cycleID).
				Dur("lock_ttl", lockTTL).
				Msg("Could not acquire distributed lock, skipping cycle")
			return
		}
		defer func() {
			_ = s.redisClient.ReleaseLock(ctx, lockKey, cycleID)
		}()
	} else {
		s.logger.Warn().Str("cycle_id", cycleID).Msg("Redis unavailable, running without distributed lock")
	}

	s.logger.Info().
		Str("cycle_id", cycleID).
		Int("server_count", len(serverInfos)).
		Msg("Servers loaded, starting health-checks")

	// Step 4: Execute worker pool
	results, err := s.pool.Execute(ctx, serverInfos)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("cycle_id", cycleID).
			Int("checked", len(results)).
			Int("total", len(serverInfos)).
			Msg("Health-check cycle incomplete (context cancelled)")
		// Return early — don't write partial results to ES/PG/Kafka
		return
	}

	// Step 5: Detect status changes. Redis cache is preferred; DB status is fallback.
	var statusChanges []repository.StatusChangeEvent
	for _, r := range results {
		oldStatus := serverStatusMap[r.ServerID]
		cacheKey := fmt.Sprintf("server:status:%s", r.ServerID)
		if s.redisClient != nil {
			cachedStatus, _ := s.redisClient.Get(ctx, cacheKey)
			if cachedStatus != "" {
				oldStatus = cachedStatus
			}
		}

		if oldStatus != "" && oldStatus != r.Status {
			statusChanges = append(statusChanges, repository.StatusChangeEvent{
				ServerID:  r.ServerID,
				OldStatus: oldStatus,
				NewStatus: r.Status,
				ChangedAt: r.CheckedAt,
			})
		}

		if s.redisClient != nil {
			_ = s.redisClient.Set(ctx, cacheKey, r.Status, s.interval+30*time.Second)
		}
	}

	// Step 6: Bulk write to Elasticsearch
	if err := s.esRepo.BulkIndex(ctx, results); err != nil {
		s.logger.Error().Err(err).Msg("Failed to bulk index to Elasticsearch")
		// Continue — don't stop the cycle for ES failure
	}

	// Step 7: Batch update PostgreSQL (only status changes)
	if len(statusChanges) > 0 {
		if err := s.serverReader.BatchUpdateStatus(ctx, statusChanges); err != nil {
			s.logger.Error().Err(err).Msg("Failed to batch update status in PostgreSQL")
		}
	}

	// Step 8: Publish Kafka events
	onCount := countByStatus(results, "on")
	offCount := countByStatus(results, "off")

	// 8a. Batch summary event
	batchEvent := &kafka.Event{
		EventID:   uuid.New().String(),
		EventType: "server.health.batch",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    "monitor-service",
		Data: map[string]interface{}{
			"cycle_id":      cycleID,
			"total_servers": len(results),
			"servers_on":    onCount,
			"servers_off":   offCount,
			"changed_count": len(statusChanges),
			"duration_ms":   time.Since(cycleStart).Milliseconds(),
		},
	}
	if err := s.kafkaProd.Publish(ctx, "server.health.batch", cycleID, batchEvent); err != nil {
		s.logger.Error().Err(err).Msg("Failed to publish batch event to Kafka")
	}

	// 8b. Individual status change events
	for _, change := range statusChanges {
		changeEvent := &kafka.Event{
			EventID:   uuid.New().String(),
			EventType: "server.status.changed",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Source:    "monitor-service",
			Data:      change,
		}
		if err := s.kafkaProd.Publish(ctx, "server.status.changed", change.ServerID, changeEvent); err != nil {
			s.logger.Error().
				Err(err).
				Str("server_id", change.ServerID).
				Msg("Failed to publish status change event")
		}
	}

	s.logger.Info().
		Str("cycle_id", cycleID).
		Int("total", len(results)).
		Int("on", onCount).
		Int("off", offCount).
		Int("changed", len(statusChanges)).
		Int64("duration_ms", time.Since(cycleStart).Milliseconds()).
		Msg("Health-check cycle completed")
}

// countByStatus counts results with a given status.
func countByStatus(results []*checker.HealthResult, status string) int {
	count := 0
	for _, r := range results {
		if r.Status == status {
			count++
		}
	}
	return count
}

func calculateLockTTL(serverCount, workerCount, tcpTimeoutMs int, interval time.Duration) time.Duration {
	if workerCount < 1 {
		workerCount = 1
	}
	if tcpTimeoutMs < 1 {
		tcpTimeoutMs = defaultTCPTimeoutMs
	}

	batches := 1
	if serverCount > 0 {
		batches = (serverCount + workerCount - 1) / workerCount
	}

	estimatedTTL := time.Duration(batches*tcpTimeoutMs)*time.Millisecond + 30*time.Second
	minTTL := interval + 30*time.Second
	if estimatedTTL > minTTL {
		return estimatedTTL
	}
	return minTTL
}
