package monitor

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newTestScheduler(ops RedisOps) *Scheduler {
	return NewScheduler(ops, zerolog.New(io.Discard))
}

func TestRoundID_BucketsByMinute(t *testing.T) {
	base := time.Unix(1_784_218_260, 0).UTC() // exactly on a minute boundary
	round := RoundID(base)

	if got := RoundID(base.Add(59 * time.Second)); got != round {
		t.Errorf("59s later = %d, want the same round %d", got, round)
	}
	if got := RoundID(base.Add(60 * time.Second)); got != round+1 {
		t.Errorf("60s later = %d, want %d", got, round+1)
	}
}

func TestTick_LoadsQueueAndPublishesRound(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
	ops.addTarget(Target{ServerID: "SRV-002", IPv4: "10.0.0.2", TCPPort: 80})
	round := RoundID(ops.now)

	newTestScheduler(ops).tick(context.Background())

	if got := ops.queues[round]; len(got) != 2 {
		t.Fatalf("queue = %v, want both targets", got)
	}
	if ops.current != round {
		t.Errorf("current round = %d, want %d", ops.current, round)
	}
}

// The queue must be fully loaded before the round is published, or a worker
// could see the new round and find nothing to do.
func TestTick_PublishesRoundOnlyAfterLoading(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
	ops.pushErr = errBoom
	round := RoundID(ops.now)

	newTestScheduler(ops).tick(context.Background())

	if ops.current == round {
		t.Error("round was published even though loading the queue failed")
	}
}

// Losing the lock is the normal case for all but one instance.
func TestTick_DoesNothingWithoutTheLock(t *testing.T) {
	ops := newFakeOps()
	ops.addTarget(Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
	round := RoundID(ops.now)
	ops.locksHeld[round] = true // another instance already won

	newTestScheduler(ops).tick(context.Background())

	if len(ops.queues[round]) != 0 {
		t.Errorf("queue = %v, want the lock loser to load nothing", ops.queues[round])
	}
	if ops.current != 0 {
		t.Error("the lock loser published a round")
	}
}

// A half-built projection would report live servers as unchecked.
func TestTick_SkipsRoundWhenProjectionNotReady(t *testing.T) {
	ops := newFakeOps()
	ops.ready = false
	ops.addTarget(Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
	round := RoundID(ops.now)

	newTestScheduler(ops).tick(context.Background())

	if len(ops.queues[round]) != 0 {
		t.Error("loaded a queue without the ready marker")
	}
	if ops.current != 0 {
		t.Error("published a round without the ready marker")
	}
}

func TestTick_PushesInBatches(t *testing.T) {
	ops := newFakeOps()
	total := pushBatch*2 + 7
	for i := range total {
		ops.addTarget(Target{ServerID: fmt.Sprintf("SRV-%05d", i), IPv4: "10.0.0.1", TCPPort: 80})
	}
	round := RoundID(ops.now)

	newTestScheduler(ops).tick(context.Background())

	if got := len(ops.queues[round]); got != total {
		t.Fatalf("queued %d, want %d", got, total)
	}
	for i, push := range ops.pushes {
		if len(push) > pushBatch {
			t.Fatalf("push %d had %d ids, want at most %d", i, len(push), pushBatch)
		}
	}
}

func TestTick_SurvivesRedisErrors(t *testing.T) {
	cases := map[string]func(*fakeOps){
		"time":  func(o *fakeOps) { o.timeErr = errBoom },
		"lock":  func(o *fakeOps) { o.lockErr = errBoom },
		"ready": func(o *fakeOps) { o.readyErr = errBoom },
		"scan":  func(o *fakeOps) { o.scanErr = errBoom },
	}

	for name, brk := range cases {
		ops := newFakeOps()
		ops.addTarget(Target{ServerID: "SRV-001", IPv4: "10.0.0.1", TCPPort: 80})
		brk(ops)

		newTestScheduler(ops).tick(context.Background()) // must not panic

		if ops.current != 0 {
			t.Errorf("%s: published a round despite the failure", name)
		}
	}
}

func TestScheduler_RunStopsOnContextCancel(t *testing.T) {
	ops := newFakeOps()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() { newTestScheduler(ops).Run(ctx); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
