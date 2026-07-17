package monitor

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

const (
	scanBatch = 500
	pushBatch = 500
)

// Scheduler loads one ping queue per round. It runs on every instance; only
// the instance that wins the round lock does the loading.
type Scheduler struct {
	ops     RedisOps
	metrics *Metrics
	log     zerolog.Logger

	// prevRound is the round measured for checks_missing on the next tick.
	prevRound int64
}

// NewScheduler creates a Scheduler. metrics may be nil.
func NewScheduler(ops RedisOps, metrics *Metrics, log zerolog.Logger) *Scheduler {
	return &Scheduler{ops: ops, metrics: metrics, log: log}
}

// Run loads one round per round boundary until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		s.tick(ctx)
		if !s.sleepToNextRound(ctx) {
			s.log.Info().Msg("Scheduler stopped")
			return
		}
	}
}

// nextRoundDelay is the time left in the current round, re-read from Redis every
// round. A fixed ticker would instead run on whatever phase the process booted
// at, so a round's queue would load partway through the round, and a single late
// tick would drop a round with no queue for checks_missing to see.
func (s *Scheduler) nextRoundDelay(ctx context.Context) time.Duration {
	now, err := s.ops.Time(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to read Redis time; falling back to a fixed delay")
		return RoundSeconds * time.Second
	}
	return time.Duration(RoundSeconds-now.Unix()%RoundSeconds) * time.Second
}

func (s *Scheduler) sleepToNextRound(ctx context.Context) bool {
	timer := time.NewTimer(s.nextRoundDelay(ctx))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// tick loads the queue for the current round if this instance wins the lock.
// Losing the lock is normal and costs nothing: this instance's workers still
// ping from the queue the winner loads.
func (s *Scheduler) tick(ctx context.Context) {
	now, err := s.ops.Time(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to read Redis time")
		return
	}
	roundID := RoundID(now)
	s.measurePrevRound(ctx, roundID)

	won, err := s.ops.AcquireRoundLock(ctx, roundID)
	if err != nil {
		s.log.Error().Err(err).Int64("round_id", roundID).Msg("Failed to acquire round lock")
		return
	}
	if !won {
		return
	}

	// Without the ready marker the projection may be half-built, and a partial
	// round would report servers as unchecked.
	ready, err := s.ops.TargetsReady(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to check target projection readiness")
		return
	}
	if !ready {
		s.log.Warn().Int64("round_id", roundID).
			Msg("Target projection not ready; skipping round. Run 'server-service rebuild-monitor-cache'")
		return
	}

	loaded, err := s.loadQueue(ctx, roundID)
	if err != nil {
		s.log.Error().Err(err).Int64("round_id", roundID).Msg("Failed to load ping queue")
		return
	}

	if err := s.ops.ExpireQueue(ctx, roundID); err != nil {
		s.log.Error().Err(err).Int64("round_id", roundID).Msg("Failed to expire ping queue")
	}

	// Published last: a worker seeing this round must find a loaded queue.
	if err := s.ops.SetCurrentRound(ctx, roundID); err != nil {
		s.log.Error().Err(err).Int64("round_id", roundID).Msg("Failed to publish current round")
		return
	}

	if s.metrics != nil {
		s.metrics.TargetsExpected.Set(float64(loaded))
	}
	s.log.Info().Int64("round_id", roundID).Int("targets_expected", loaded).Msg("Round loaded")
}

// measurePrevRound reports what the finished round never got to ping.
func (s *Scheduler) measurePrevRound(ctx context.Context, roundID int64) {
	if s.metrics == nil || s.prevRound == 0 || s.prevRound == roundID {
		s.prevRound = roundID
		return
	}

	missing, err := s.ops.QueueDepth(ctx, s.prevRound)
	if err != nil {
		s.log.Error().Err(err).Int64("round_id", s.prevRound).Msg("Failed to measure checks_missing")
		s.prevRound = roundID
		return
	}
	s.metrics.ChecksMissing.Set(float64(missing))
	if missing > 0 {
		s.log.Warn().Int64("round_id", s.prevRound).Int64("checks_missing", missing).
			Msg("Round ended with unpinged targets; consider more workers or instances")
	}
	s.prevRound = roundID
}

// loadQueue scans the target set and pushes it onto the round's queue.
func (s *Scheduler) loadQueue(ctx context.Context, roundID int64) (int, error) {
	var cursor uint64
	loaded := 0
	pending := make([]string, 0, pushBatch)

	for {
		ids, next, err := s.ops.ScanTargetIDs(ctx, cursor, scanBatch)
		if err != nil {
			return loaded, err
		}
		pending = append(pending, ids...)

		for len(pending) >= pushBatch {
			if err := s.ops.PushQueue(ctx, roundID, pending[:pushBatch]); err != nil {
				return loaded, err
			}
			loaded += pushBatch
			pending = pending[pushBatch:]
		}

		cursor = next
		if cursor == 0 {
			break
		}
	}

	if len(pending) > 0 {
		if err := s.ops.PushQueue(ctx, roundID, pending); err != nil {
			return loaded, err
		}
		loaded += len(pending)
	}
	return loaded, nil
}
