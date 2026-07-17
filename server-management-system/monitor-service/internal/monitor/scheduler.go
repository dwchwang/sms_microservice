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
	ops RedisOps
	log zerolog.Logger
}

// NewScheduler creates a Scheduler.
func NewScheduler(ops RedisOps, log zerolog.Logger) *Scheduler {
	return &Scheduler{ops: ops, log: log}
}

// Run ticks once per round until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(RoundSeconds * time.Second)
	defer ticker.Stop()

	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			s.log.Info().Msg("Scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
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

	s.log.Info().Int64("round_id", roundID).Int("targets_expected", loaded).Msg("Round loaded")
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
