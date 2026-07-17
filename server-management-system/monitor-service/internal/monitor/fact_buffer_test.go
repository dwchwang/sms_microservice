package monitor

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type fakeWriter struct {
	mu       sync.Mutex
	batches  [][]Fact
	err      error
	failFor  int // fail this many calls, then succeed
	attempts int
}

func (w *fakeWriter) Write(ctx context.Context, facts []Fact) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.attempts++
	if w.failFor > 0 {
		w.failFor--
		return errBoom
	}
	if w.err != nil {
		return w.err
	}
	w.batches = append(w.batches, append([]Fact(nil), facts...))
	return nil
}

func (w *fakeWriter) written() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, b := range w.batches {
		n += len(b)
	}
	return n
}

func (w *fakeWriter) attemptCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.attempts
}

func newTestBuffer(w factWriter, capacity int) *FactBuffer {
	return NewFactBuffer(w, capacity, nil, zerolog.New(io.Discard))
}

func fact(id string) Fact {
	return Fact{ServerID: id, Status: statusON, CheckedAt: time.Now().UTC(), RoundID: 1}
}

func TestFactBuffer_FlushWritesPending(t *testing.T) {
	w := &fakeWriter{}
	b := newTestBuffer(w, 100)
	b.Add(fact("SRV-001"))
	b.Add(fact("SRV-002"))

	b.Flush(context.Background())

	if w.written() != 2 {
		t.Fatalf("wrote %d facts, want 2", w.written())
	}
}

func TestFactBuffer_FlushOnEmptyIsNoop(t *testing.T) {
	w := &fakeWriter{}
	newTestBuffer(w, 100).Flush(context.Background())

	if w.attemptCount() != 0 {
		t.Error("flushed an empty buffer")
	}
}

// A long Elasticsearch outage must not grow the buffer without limit: dropping
// facts costs report coverage, while an OOM costs the whole service.
func TestFactBuffer_DropsWhenFull(t *testing.T) {
	w := &fakeWriter{err: errBoom}
	b := newTestBuffer(w, 3)

	for i := range 10 {
		b.Add(fact(string(rune('A' + i))))
	}

	if got := b.Dropped(); got != 7 {
		t.Errorf("dropped = %d, want 7", got)
	}
}

func TestFactBuffer_RetriesThenGivesUp(t *testing.T) {
	w := &fakeWriter{err: errBoom}
	b := newTestBuffer(w, 100)
	b.Add(fact("SRV-001"))

	b.Flush(context.Background())

	if got := w.attemptCount(); got != maxRetries {
		t.Errorf("attempted %d times, want %d", got, maxRetries)
	}
	if b.Failed() != 1 {
		t.Errorf("failed batches = %d, want 1", b.Failed())
	}
	if b.Dropped() != 1 {
		t.Errorf("dropped = %d, want 1", b.Dropped())
	}
}

func TestFactBuffer_RetrySucceeds(t *testing.T) {
	w := &fakeWriter{failFor: 1}
	b := newTestBuffer(w, 100)
	b.Add(fact("SRV-001"))

	b.Flush(context.Background())

	if w.written() != 1 {
		t.Errorf("wrote %d facts, want 1 after a retry", w.written())
	}
	if b.Failed() != 0 {
		t.Errorf("failed = %d, want 0", b.Failed())
	}
}

// A failed batch must not block the next one.
func TestFactBuffer_FailedBatchDoesNotBlockNext(t *testing.T) {
	w := &fakeWriter{err: errBoom}
	b := newTestBuffer(w, 100)
	b.Add(fact("SRV-001"))
	b.Flush(context.Background())

	w.mu.Lock()
	w.err = nil
	w.mu.Unlock()
	b.Add(fact("SRV-002"))
	b.Flush(context.Background())

	if w.written() != 1 || w.batches[0][0].ServerID != "SRV-002" {
		t.Errorf("expected only SRV-002 to be written, got %v", w.batches)
	}
}

func TestFactBuffer_RunFlushesOnShutdown(t *testing.T) {
	w := &fakeWriter{}
	b := newTestBuffer(w, 100)
	b.Add(fact("SRV-001"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if w.written() != 1 {
		t.Error("pending facts were lost at shutdown")
	}
}
