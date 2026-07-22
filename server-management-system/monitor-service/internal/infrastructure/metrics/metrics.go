package metrics

import "github.com/prometheus/client_golang/prometheus"

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
