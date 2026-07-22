package monitor

import (
	"context"
	"errors"
	"github.com/vcs-sms/monitor-service/internal/infrastructure/redisstore"
	"github.com/vcs-sms/monitor-service/internal/model"
	"sync"
	"time"
)

// fakeOps models the Redis contract in memory, including the parts that make
// the round mechanism work: the lock, the queue and the current-round pointer.
type fakeOps struct {
	mu sync.Mutex

	now        time.Time
	ready      bool
	targetIDs  []string
	targets    map[string]*model.Target
	locksHeld  map[int64]bool
	queues     map[int64][]string
	current    int64
	applied    []appliedStatus
	applyCode  int
	pushes     [][]string
	timeErr    error
	readyErr   error
	scanErr    error
	lockErr    error
	pushErr    error
	popErr     error
	targetErr  error
	applyErr   error
	currentErr error
}

type appliedStatus struct {
	ServerID  string
	Status    string
	CheckedAt string
	Latency   int
	RoundID   int64
}

func newFakeOps() *fakeOps {
	return &fakeOps{
		now:       time.Unix(1_784_218_260, 0).UTC(),
		ready:     true,
		targets:   make(map[string]*model.Target),
		locksHeld: make(map[int64]bool),
		queues:    make(map[int64][]string),
		applyCode: redisstore.StatusChanged,
	}
}

func (f *fakeOps) addTarget(t model.Target) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targetIDs = append(f.targetIDs, t.ServerID)
	f.targets[t.ServerID] = &t
}

func (f *fakeOps) Time(ctx context.Context) (time.Time, error) {
	if f.timeErr != nil {
		return time.Time{}, f.timeErr
	}
	return f.now, nil
}

func (f *fakeOps) TargetsReady(ctx context.Context) (bool, error) {
	if f.readyErr != nil {
		return false, f.readyErr
	}
	return f.ready, nil
}

func (f *fakeOps) ScanTargetIDs(ctx context.Context, cursor uint64, count int64) ([]string, uint64, error) {
	if f.scanErr != nil {
		return nil, 0, f.scanErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	start := int(cursor)
	if start >= len(f.targetIDs) {
		return nil, 0, nil
	}
	end := min(start+int(count), len(f.targetIDs))
	next := uint64(end)
	if end >= len(f.targetIDs) {
		next = 0
	}
	return append([]string(nil), f.targetIDs[start:end]...), next, nil
}

func (f *fakeOps) AcquireRoundLock(ctx context.Context, roundID int64) (bool, error) {
	if f.lockErr != nil {
		return false, f.lockErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locksHeld[roundID] {
		return false, nil
	}
	f.locksHeld[roundID] = true
	return true, nil
}

func (f *fakeOps) PushQueue(ctx context.Context, roundID int64, serverIDs []string) error {
	if f.pushErr != nil {
		return f.pushErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes = append(f.pushes, append([]string(nil), serverIDs...))
	f.queues[roundID] = append(f.queues[roundID], serverIDs...)
	return nil
}

func (f *fakeOps) ExpireQueue(ctx context.Context, roundID int64) error { return nil }

func (f *fakeOps) SetCurrentRound(ctx context.Context, roundID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = roundID
	return nil
}

func (f *fakeOps) CurrentRound(ctx context.Context) (int64, error) {
	if f.currentErr != nil {
		return 0, f.currentErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current, nil
}

func (f *fakeOps) QueueDepth(ctx context.Context, roundID int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.queues[roundID])), nil
}

func (f *fakeOps) PopTarget(ctx context.Context, roundID int64, timeout time.Duration) (string, error) {
	if f.popErr != nil {
		return "", f.popErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q := f.queues[roundID]
	if len(q) == 0 {
		return "", nil
	}
	id := q[0]
	f.queues[roundID] = q[1:]
	return id, nil
}

func (f *fakeOps) GetTarget(ctx context.Context, serverID string) (*model.Target, error) {
	if f.targetErr != nil {
		return nil, f.targetErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.targets[serverID], nil
}

func (f *fakeOps) ApplyStatus(ctx context.Context, t model.Target, status, checkedAt string, latencyMs int, roundID int64) (int, error) {
	if f.applyErr != nil {
		return 0, f.applyErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, appliedStatus{
		ServerID: t.ServerID, Status: status, CheckedAt: checkedAt,
		Latency: latencyMs, RoundID: roundID,
	})
	return f.applyCode, nil
}

func (f *fakeOps) appliedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied)
}

// fakePinger answers from a fixed up/down map.
type fakePinger struct {
	mu   sync.Mutex
	up   map[string]bool
	seen []string
}

func newFakePinger() *fakePinger {
	return &fakePinger{up: make(map[string]bool)}
}

func (p *fakePinger) Ping(ctx context.Context, ipv4 string, tcpPort int) (bool, int, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, ipv4)
	if p.up[ipv4] {
		return true, 5, ""
	}
	return false, 3000, "TIMEOUT"
}

// fakeSink collects the facts a check produces.
type fakeSink struct {
	mu    sync.Mutex
	facts []model.Fact
}

func (s *fakeSink) Add(f model.Fact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts = append(s.facts, f)
}

func (s *fakeSink) all() []model.Fact {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.Fact(nil), s.facts...)
}

var errBoom = errors.New("boom")
