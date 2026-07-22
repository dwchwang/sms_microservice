package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/monitor-service/internal/infrastructure/metrics"
	"github.com/vcs-sms/monitor-service/internal/model"
)

const (
	flushSize     = 1000
	flushInterval = 5 * time.Second
	maxRetries    = 3
	retryBackoff  = 500 * time.Millisecond
)

// factWriter is the sink the buffer flushes into.
type factWriter interface {
	Write(ctx context.Context, facts []model.Fact) error
}

// FactBuffer batches health facts and flushes them on size or interval.
// Its capacity is bounded: during a long Elasticsearch outage it drops facts
// rather than growing until the process dies. Reports then show reduced
// coverage, which is recoverable; an OOM is not.
type FactBuffer struct {
	writer   factWriter
	capacity int
	metrics  *metrics.Metrics
	log      zerolog.Logger

	mu      sync.Mutex
	pending []model.Fact

	dropped uint64
	failed  uint64
}

// NewFactBuffer creates a buffer holding at most capacity facts. metrics may be nil.
func NewFactBuffer(writer factWriter, capacity int, metrics *metrics.Metrics, log zerolog.Logger) *FactBuffer {
	return &FactBuffer{
		writer:   writer,
		capacity: capacity,
		metrics:  metrics,
		log:      log,
		pending:  make([]model.Fact, 0, flushSize),
	}
}

// Add queues a fact, dropping it when the buffer is full.
func (b *FactBuffer) Add(fact model.Fact) {
	b.mu.Lock()
	if len(b.pending) >= b.capacity {
		b.dropped++
		b.mu.Unlock()
		return
	}
	b.pending = append(b.pending, fact)
	full := len(b.pending) >= flushSize
	b.mu.Unlock()

	if full {
		go b.Flush(context.Background())
	}
}

// Run flushes on a timer until ctx is cancelled, then flushes what is left.
func (b *FactBuffer) Run(ctx context.Context) {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			b.Flush(flushCtx)
			cancel()
			b.log.Info().Uint64("dropped", b.Dropped()).Uint64("failed_batches", b.Failed()).
				Msg("model.Fact buffer stopped")
			return
		case <-ticker.C:
			b.Flush(ctx)
		}
	}
}

// Flush writes the pending facts, retrying a failed batch a bounded number of
// times before giving up on it.
func (b *FactBuffer) Flush(ctx context.Context) {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.pending
	b.pending = make([]model.Fact, 0, flushSize)
	b.mu.Unlock()

	for attempt := 1; ; attempt++ {
		err := b.writer.Write(ctx, batch)
		if err == nil {
			return
		}
		if attempt >= maxRetries || ctx.Err() != nil {
			b.mu.Lock()
			b.failed++
			b.dropped += uint64(len(batch))
			b.mu.Unlock()
			if b.metrics != nil {
				b.metrics.ESBulkFailure.Inc()
			}
			b.log.Error().Err(err).Int("facts", len(batch)).
				Msg("Dropping health facts after repeated bulk failures")
			return
		}
		sleep(ctx, time.Duration(attempt)*retryBackoff)
	}
}

// Dropped counts facts never written, for the coverage metric.
func (b *FactBuffer) Dropped() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// Failed counts bulk batches abandoned after retries.
func (b *FactBuffer) Failed() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failed
}
