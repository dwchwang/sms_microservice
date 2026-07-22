package monitor

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/monitor-service/internal/infrastructure/redisstore"
	"github.com/vcs-sms/monitor-service/internal/model"
)

func newTestPool(ops redisstore.RedisOps, pinger Pinger, sink FactSink, workers int) *Pool {
	return NewPool(ops, pinger, sink, nil, workers, zerolog.New(io.Discard))
}

func TestCheck_ReportsServerUp(t *testing.T) {
	ops := newFakeOps()
	target := model.Target{ServerID: "SRV-001", ServerName: "web-01", IPv4: "10.0.0.1", TCPPort: 8080}
	ops.addTarget(target)
	pinger := newFakePinger()
	pinger.up["10.0.0.1"] = true
	sink := &fakeSink{}

	newTestPool(ops, pinger, sink, 1).check(context.Background(), "SRV-001", 100)

	if len(ops.applied) != 1 {
		t.Fatalf("applied %d statuses, want 1", len(ops.applied))
	}
	got := ops.applied[0]
	if got.Status != model.StatusON {
		t.Errorf("Status = %q, want ON", got.Status)
	}
	if got.RoundID != 100 {
		t.Errorf("RoundID = %d, want 100", got.RoundID)
	}
	// Server Service parses this with time.Parse(time.RFC3339, ...).
	if _, err := time.Parse(time.RFC3339, got.CheckedAt); err != nil {
		t.Errorf("checked_at %q is not RFC3339: %v", got.CheckedAt, err)
	}

	facts := sink.all()
	if len(facts) != 1 {
		t.Fatalf("recorded %d facts, want 1", len(facts))
	}
	// Reporting reads server_name off the fact, so it has to be carried through.
	if facts[0].ServerName != "web-01" {
		t.Errorf("fact ServerName = %q, want web-01", facts[0].ServerName)
	}
	if facts[0].Status != model.StatusON || facts[0].RoundID != 100 {
		t.Errorf("unexpected fact %#v", facts[0])
	}
}

func TestCheck_ReportsServerDownWithErrorCode(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(model.Target{ServerID: "SRV-001", ServerName: "web-01", IPv4: "10.0.0.9", TCPPort: 80})
	sink := &fakeSink{}

	newTestPool(ops, newFakePinger(), sink, 1).check(context.Background(), "SRV-001", 100)

	if ops.applied[0].Status != model.StatusOFF {
		t.Errorf("Status = %q, want OFF", ops.applied[0].Status)
	}
	if got := sink.all()[0].ErrorCode; got != "TIMEOUT" {
		t.Errorf("ErrorCode = %q, want TIMEOUT", got)
	}
}

// The queue was loaded before the delete, so the target hash is already gone.
func TestCheck_SkipsServerDeletedMidRound(t *testing.T) {
	ops := newFakeOps()
	pinger := newFakePinger()
	sink := &fakeSink{}

	newTestPool(ops, pinger, sink, 1).check(context.Background(), "SRV-GONE", 100)

	if len(pinger.seen) != 0 {
		t.Error("pinged a server that no longer exists")
	}
	if len(ops.applied) != 0 || len(sink.all()) != 0 {
		t.Error("recorded a result for a deleted server")
	}
}

// A stale result must not produce a fact, or actual_checks would count a check
// that the status hash rejected.
func TestCheck_StaleRoundRecordsNoFact(t *testing.T) {
	ops := newFakeOps()
	ops.applyCode = redisstore.StatusSkippedStale
	ops.addTarget(model.Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
	sink := &fakeSink{}

	newTestPool(ops, newFakePinger(), sink, 1).check(context.Background(), "SRV-001", 100)

	if len(sink.all()) != 0 {
		t.Error("recorded a fact for a stale round")
	}
}

func TestCheck_UnchangedStatusStillRecordsFact(t *testing.T) {
	ops := newFakeOps()
	ops.applyCode = redisstore.StatusUnchanged
	ops.addTarget(model.Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
	sink := &fakeSink{}

	newTestPool(ops, newFakePinger(), sink, 1).check(context.Background(), "SRV-001", 100)

	// Uptime is computed from every check, not only from transitions.
	if len(sink.all()) != 1 {
		t.Error("an unchanged status must still produce a health fact")
	}
}

// Elasticsearch is the side path, but Redis failing means the check is lost.
func TestCheck_ApplyErrorRecordsNoFact(t *testing.T) {
	ops := newFakeOps()
	ops.applyErr = errBoom
	ops.addTarget(model.Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
	sink := &fakeSink{}

	newTestPool(ops, newFakePinger(), sink, 1).check(context.Background(), "SRV-001", 100)

	if len(sink.all()) != 0 {
		t.Error("recorded a fact even though the status write failed")
	}
}

func TestPool_DrainsTheQueue(t *testing.T) {
	ops := newFakeOps()
	for _, id := range []string{"SRV-001", "SRV-002", "SRV-003"} {
		ops.addTarget(model.Target{ServerID: id, IPv4: "10.0.0.1", TCPPort: 80})
	}
	ops.current = 100
	ops.queues[100] = []string{"SRV-001", "SRV-002", "SRV-003"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newTestPool(ops, newFakePinger(), &fakeSink{}, 3).Run(ctx)

	waitFor(t, func() bool { return ops.appliedCount() == 3 })
}

// Workers never remember a round: they re-read it every loop, which is the
// whole round-switch mechanism.
func TestPool_FollowsRoundSwitch(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(model.Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
	ops.current = 100
	ops.queues[100] = []string{"SRV-001"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newTestPool(ops, newFakePinger(), &fakeSink{}, 1).Run(ctx)

	waitFor(t, func() bool { return ops.appliedCount() == 1 })

	// A new round arrives with fresh work; leftovers of round 100 are abandoned.
	ops.mu.Lock()
	ops.queues[101] = []string{"SRV-001"}
	ops.current = 101
	ops.mu.Unlock()

	waitFor(t, func() bool { return ops.appliedCount() == 2 })

	ops.mu.Lock()
	defer ops.mu.Unlock()
	if ops.applied[1].RoundID != 101 {
		t.Errorf("second check used round %d, want 101", ops.applied[1].RoundID)
	}
}

func TestPool_IdlesWhenNoRoundIsLoaded(t *testing.T) {
	ops := newFakeOps() // current stays 0
	ops.addTarget(model.Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	newTestPool(ops, newFakePinger(), &fakeSink{}, 1).Run(ctx)

	if ops.appliedCount() != 0 {
		t.Error("worked without a current round")
	}
}

func TestPool_StopsOnContextCancel(t *testing.T) {
	ops := newFakeOps()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() { newTestPool(ops, newFakePinger(), &fakeSink{}, 4).Run(ctx); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
