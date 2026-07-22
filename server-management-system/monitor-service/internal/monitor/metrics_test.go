package monitor

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	"github.com/vcs-sms/monitor-service/internal/infrastructure/metrics"
	"github.com/vcs-sms/monitor-service/internal/model"
)

func newTestMetrics() *metrics.Metrics {
	return metrics.NewMetrics(prometheus.NewRegistry())
}

func TestScheduler_RecordsTargetsExpected(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(model.Target{ServerID: "SRV-1", IPv4: "10.0.0.1", TCPPort: 80})
	ops.addTarget(model.Target{ServerID: "SRV-2", IPv4: "10.0.0.2", TCPPort: 80})
	m := newTestMetrics()

	NewScheduler(ops, m, zerolog.New(io.Discard)).tick(context.Background())

	if got := testutil.ToFloat64(m.TargetsExpected); got != 2 {
		t.Errorf("targets_expected = %v, want 2", got)
	}
}

// checks_missing is the only signal that the pool could not keep up.
func TestScheduler_ReportsUnpingedTargetsOfFinishedRound(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(model.Target{ServerID: "SRV-1", IPv4: "10.0.0.1", TCPPort: 80})
	ops.addTarget(model.Target{ServerID: "SRV-2", IPv4: "10.0.0.2", TCPPort: 80})
	ops.addTarget(model.Target{ServerID: "SRV-3", IPv4: "10.0.0.3", TCPPort: 80})
	m := newTestMetrics()
	s := NewScheduler(ops, m, zerolog.New(io.Discard))
	ctx := context.Background()

	s.tick(ctx)

	// Nobody pinged: all 3 stay queued when the round rolls over.
	ops.now = ops.now.Add(model.RoundSeconds * time.Second)
	s.tick(ctx)

	if got := testutil.ToFloat64(m.ChecksMissing); got != 3 {
		t.Errorf("checks_missing = %v, want 3", got)
	}
}

func TestScheduler_ChecksMissingIsZeroWhenRoundDrained(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(model.Target{ServerID: "SRV-1", IPv4: "10.0.0.1", TCPPort: 80})
	m := newTestMetrics()
	s := NewScheduler(ops, m, zerolog.New(io.Discard))
	ctx := context.Background()

	s.tick(ctx)
	if _, err := ops.PopTarget(ctx, model.RoundID(ops.now), time.Second); err != nil {
		t.Fatalf("failed to drain: %v", err)
	}

	ops.now = ops.now.Add(model.RoundSeconds * time.Second)
	s.tick(ctx)

	if got := testutil.ToFloat64(m.ChecksMissing); got != 0 {
		t.Errorf("checks_missing = %v, want 0", got)
	}
}

func TestPool_CountsCompletedChecks(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(model.Target{ServerID: "SRV-1", IPv4: "10.0.0.1", TCPPort: 80})
	pinger := newFakePinger()
	pinger.up["10.0.0.1"] = true
	m := newTestMetrics()

	p := NewPool(ops, pinger, &fakeSink{}, m, 1, zerolog.New(io.Discard))
	p.check(context.Background(), "SRV-1", 100)

	if got := testutil.ToFloat64(m.ChecksCompleted); got != 1 {
		t.Errorf("checks_completed = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(m.TCPLatency); got == 0 {
		t.Error("tcp_latency recorded nothing for a reachable target")
	}
}

func TestFactBuffer_CountsBulkFailure(t *testing.T) {
	b := NewFactBuffer(&stubWriter{err: errBoom}, 10, newTestMetricsBuffer(t), zerolog.New(io.Discard))
	b.Add(model.Fact{ServerID: "SRV-1"})
	b.Flush(context.Background())

	if got := testutil.ToFloat64(b.metrics.ESBulkFailure); got != 1 {
		t.Errorf("es_bulk_failure = %v, want 1", got)
	}
}

func newTestMetricsBuffer(t *testing.T) *metrics.Metrics {
	t.Helper()
	return newTestMetrics()
}

type stubWriter struct{ err error }

func (w *stubWriter) Write(ctx context.Context, facts []model.Fact) error { return w.err }
