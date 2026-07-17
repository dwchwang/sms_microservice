package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	popTimeout   = time.Second
	idleSleep    = time.Second
	errorBackoff = time.Second
)

// Pinger performs the reachability check for one target.
type Pinger interface {
	Ping(ctx context.Context, ipv4 string, tcpPort int) (up bool, latencyMs int, errCode string)
}

// FactSink receives one health fact per completed check.
type FactSink interface {
	Add(fact Fact)
}

// Fact is a single check result, as stored for uptime reporting.
type Fact struct {
	ServerID   string
	ServerName string
	Status     string
	CheckedAt  time.Time
	RoundID    int64
	LatencyMs  int
	ErrorCode  string
}

// Pool runs the goroutines that drain the round queue. Every instance runs a
// pool, including the one that won the scheduling lock.
type Pool struct {
	ops     RedisOps
	pinger  Pinger
	facts   FactSink
	workers int
	log     zerolog.Logger
}

// NewPool creates a Pool of the given size.
func NewPool(ops RedisOps, pinger Pinger, facts FactSink, workers int, log zerolog.Logger) *Pool {
	return &Pool{ops: ops, pinger: pinger, facts: facts, workers: workers, log: log}
}

// Run blocks until ctx is cancelled.
func (p *Pool) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(p.workers)
	for i := range p.workers {
		go func() {
			defer wg.Done()
			p.work(ctx, i)
		}()
	}
	p.log.Info().Int("workers", p.workers).Msg("Worker pool started")
	wg.Wait()
	p.log.Info().Msg("Worker pool stopped")
}

// work loops until cancelled. It re-reads the current round every iteration and
// never remembers one, which is the whole of the round-switch mechanism: when a
// new round starts, the next loop simply pops from the new queue and whatever
// is left in the old one expires.
func (p *Pool) work(ctx context.Context, id int) {
	for {
		if ctx.Err() != nil {
			return
		}

		round, err := p.ops.CurrentRound(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			p.log.Error().Err(err).Int("worker", id).Msg("Failed to read current round")
			sleep(ctx, errorBackoff)
			continue
		}
		if round == 0 {
			sleep(ctx, idleSleep)
			continue
		}

		serverID, err := p.ops.PopTarget(ctx, round, popTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			p.log.Error().Err(err).Int("worker", id).Msg("Failed to pop target")
			sleep(ctx, errorBackoff)
			continue
		}
		if serverID == "" {
			continue
		}

		p.check(ctx, serverID, round)
	}
}

// check pings one server and records the outcome.
func (p *Pool) check(ctx context.Context, serverID string, round int64) {
	target, err := p.ops.GetTarget(ctx, serverID)
	if err != nil {
		p.log.Error().Err(err).Str("server_id", serverID).Msg("Failed to read target")
		return
	}
	// The server was deleted after the queue was loaded.
	if target == nil {
		return
	}

	up, latency, errCode := p.pinger.Ping(ctx, target.IPv4, target.TCPPort)
	status := statusOFF
	if up {
		status = statusON
	}
	checkedAt := time.Now().UTC()

	// RFC3339 is the contract Server Service parses for both changed_at on the
	// stream and last_checked_at on the status hash.
	code, err := p.ops.ApplyStatus(ctx, *target, status,
		checkedAt.Format(time.RFC3339), latency, round)
	if err != nil {
		p.log.Error().Err(err).Str("server_id", serverID).Msg("Failed to apply status")
		return
	}
	if code == statusSkippedStale {
		return
	}
	if code == statusChanged {
		p.log.Info().Str("server_id", serverID).Str("status", status).
			Int64("round_id", round).Msg("Status changed")
	}

	// Elasticsearch is the side path: a check counts even if the fact is lost.
	p.facts.Add(Fact{
		ServerID:   target.ServerID,
		ServerName: target.ServerName,
		Status:     status,
		CheckedAt:  checkedAt,
		RoundID:    round,
		LatencyMs:  latency,
		ErrorCode:  errCode,
	})
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
