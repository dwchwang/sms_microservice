package monitor

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics are the seven signals design §8.5 requires.
type Metrics struct {
	RoundDuration   prometheus.Histogram
	TargetsExpected prometheus.Gauge
	ChecksCompleted prometheus.Counter
	ChecksMissing   prometheus.Gauge
	QueueDepth      prometheus.Gauge
	TCPLatency      prometheus.Histogram
	ESBulkFailure   prometheus.Counter
}

// NewMetrics registers the collectors on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		RoundDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "vcs_monitor_round_duration_seconds",
			Help:    "Seconds from a round starting until its queue drained.",
			Buckets: []float64{1, 5, 10, 20, 30, 45, 60, 90, 120},
		}),
		TargetsExpected: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vcs_monitor_targets_expected",
			Help: "Targets the scheduler loaded into the last round's queue.",
		}),
		ChecksCompleted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "vcs_monitor_checks_completed_total",
			Help: "Pings completed by this instance.",
		}),
		ChecksMissing: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vcs_monitor_checks_missing",
			Help: "Queue entries left unpinged when the previous round ended. Sustained >0 means too few workers.",
		}),
		QueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "vcs_monitor_queue_depth",
			Help: "Current depth of the active round's ping queue.",
		}),
		TCPLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "vcs_monitor_tcp_latency_seconds",
			Help:    "TCP connect latency.",
			Buckets: prometheus.DefBuckets,
		}),
		ESBulkFailure: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "vcs_monitor_es_bulk_failure_total",
			Help: "Bulk batches abandoned after retries.",
		}),
	}
	reg.MustRegister(m.RoundDuration, m.TargetsExpected, m.ChecksCompleted,
		m.ChecksMissing, m.QueueDepth, m.TCPLatency, m.ESBulkFailure)
	return m
}

const sampleInterval = time.Second

// Sampler tracks queue depth, and derives round_duration from the moment a
// round's queue drains.
type Sampler struct {
	ops     RedisOps
	metrics *Metrics
}

// NewSampler creates a Sampler.
func NewSampler(ops RedisOps, metrics *Metrics) *Sampler {
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
				started := time.Unix(round*RoundSeconds, 0)
				s.metrics.RoundDuration.Observe(now.Sub(started).Seconds())
				recorded = true
			}
		}
	}
}
