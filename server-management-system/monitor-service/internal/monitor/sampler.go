package monitor

import (
	"context"
	"time"

	"github.com/vcs-sms/monitor-service/internal/infrastructure/metrics"
	"github.com/vcs-sms/monitor-service/internal/infrastructure/redisstore"
	"github.com/vcs-sms/monitor-service/internal/model"
)

const sampleInterval = time.Second

// Sampler tracks queue depth, and derives round_duration from the moment a
// round's queue drains.
type Sampler struct {
	ops     redisstore.RedisOps
	metrics *metrics.Metrics
}

// NewSampler creates a Sampler.
func NewSampler(ops redisstore.RedisOps, metrics *metrics.Metrics) *Sampler {
	return &Sampler{ops: ops, metrics: metrics}
}

// Run samples until ctx is cancelled.
func (s *Sampler) Run(ctx context.Context) {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	var currentRound int64
	var sawWork, recorded bool

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			round, err := s.ops.CurrentRound(ctx)
			if err != nil || round == 0 {
				continue
			}
			if round != currentRound {
				currentRound, sawWork, recorded = round, false, false
			}

			depth, err := s.ops.QueueDepth(ctx, round)
			if err != nil {
				continue
			}
			s.metrics.QueueDepth.Set(float64(depth))

			if depth > 0 {
				sawWork = true
				continue
			}
			// Drained: the round is done. Redis time, since round_id derives from it.
			if sawWork && !recorded {
				now, err := s.ops.Time(ctx)
				if err != nil {
					continue
				}
				started := time.Unix(round*model.RoundSeconds, 0)
				s.metrics.RoundDuration.Observe(now.Sub(started).Seconds())
				recorded = true
			}
		}
	}
}
