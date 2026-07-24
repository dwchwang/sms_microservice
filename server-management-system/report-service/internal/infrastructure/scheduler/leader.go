package scheduler

import (
	"context"
	"sync"
	"time"
)

// runClaimed executes a job this instance owns. The heartbeat is what lets the
// other replicas tell a slow job from a dead one.
func (s *Scheduler) runClaimed(ctx context.Context, j job, runDate time.Time, date string) {
	jobCtx, cancel := context.WithTimeout(ctx, j.timeout)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.beat(jobCtx, cancel, j.name, date)
	}()

	s.log.Info().Str("job", j.name).Str("date", date).Msg("Job claimed")
	runErr := j.run(jobCtx, runDate)

	cancel()
	wg.Wait()

	// Shutting down: leave the claim to go stale so another replica retries it.
	if ctx.Err() != nil {
		s.log.Warn().Str("job", j.name).Str("date", date).
			Msg("Shut down mid-job; leaving the claim to expire")
		return
	}

	// Both writes are guarded by owner, so a claim already stolen stays untouched.
	if runErr != nil {
		s.log.Error().Err(runErr).Str("job", j.name).Str("date", date).Msg("Job failed")
		if err := s.runs.MarkFailed(ctx, j.name, date, s.owner, runErr.Error()); err != nil {
			s.log.Error().Err(err).Str("job", j.name).Msg("Failed to record the job failure")
		}
		return
	}

	if err := s.runs.MarkDone(ctx, j.name, date, s.owner); err != nil {
		s.log.Error().Err(err).Str("job", j.name).Msg("Failed to mark the job done")
		return
	}
	s.log.Info().Str("job", j.name).Str("date", date).Msg("Job done")
}

// beat refreshes the claim, and cancels the work once the claim is lost so two
// instances never write the same run.
func (s *Scheduler) beat(ctx context.Context, cancel context.CancelFunc, job, date string) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			held, err := s.runs.Heartbeat(ctx, job, date, s.owner)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				s.log.Error().Err(err).Str("job", job).Msg("Failed to heartbeat the job claim")
				continue
			}
			if !held {
				s.log.Warn().Str("job", job).Str("date", date).
					Msg("Lost the job claim; abandoning this run")
				cancel()
				return
			}
		}
	}
}
