package kafka

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	segmentio "github.com/segmentio/kafka-go"
)

type fakeMessageReader struct {
	messages  []segmentio.Message
	mu        sync.Mutex
	commits   int
	onCommit  func()
	commitErr error
	closeErr  error
	closed    bool
}

func (r *fakeMessageReader) FetchMessage(ctx context.Context) (segmentio.Message, error) {
	r.mu.Lock()
	if len(r.messages) > 0 {
		msg := r.messages[0]
		r.messages = r.messages[1:]
		r.mu.Unlock()
		return msg, nil
	}
	r.mu.Unlock()
	<-ctx.Done()
	return segmentio.Message{}, ctx.Err()
}

func (r *fakeMessageReader) CommitMessages(ctx context.Context, msgs ...segmentio.Message) error {
	if r.commitErr != nil {
		return r.commitErr
	}
	r.mu.Lock()
	r.commits += len(msgs)
	r.mu.Unlock()
	if r.onCommit != nil {
		r.onCommit()
	}
	return nil
}

func (r *fakeMessageReader) Close() error {
	r.closed = true
	return r.closeErr
}

func (r *fakeMessageReader) commitCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.commits
}

func TestSegmentioConsumer_CommitsAfterHandlerSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &fakeMessageReader{
		messages: []segmentio.Message{{
			Value: []byte(`{"event_id":"evt-1","event_type":"server.created","timestamp":"2026-06-11T00:00:00Z","source":"test","data":{"server_id":"SRV-001"}}`),
		}},
		onCommit: cancel,
	}
	consumer := NewSegmentioConsumer(DefaultSegmentioConsumerConfig([]string{"localhost:9092"}, "test-group"), zerolog.Nop())
	consumer.handlerBackoff = 0
	consumer.handlers["server.created"] = func(ctx context.Context, event *Event) error {
		if event.EventType != "server.created" {
			t.Fatalf("unexpected event type: %s", event.EventType)
		}
		return nil
	}

	consumer.consumeLoop(ctx, "server.created", reader)

	if got := reader.commitCount(); got != 1 {
		t.Fatalf("expected one commit after handler success, got %d", got)
	}
}

func TestSegmentioConsumer_DoesNotCommitOnHandlerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &fakeMessageReader{
		messages: []segmentio.Message{{
			Value: []byte(`{"event_id":"evt-1","event_type":"server.created","timestamp":"2026-06-11T00:00:00Z","source":"test","data":{"server_id":"SRV-001"}}`),
		}},
	}
	consumer := NewSegmentioConsumer(DefaultSegmentioConsumerConfig([]string{"localhost:9092"}, "test-group"), zerolog.Nop())
	consumer.handlerBackoff = 0
	consumer.handlers["server.created"] = func(ctx context.Context, event *Event) error {
		cancel()
		return fmt.Errorf("temporary repo error")
	}

	consumer.consumeLoop(ctx, "server.created", reader)

	if got := reader.commitCount(); got != 0 {
		t.Fatalf("expected no commit after handler error, got %d", got)
	}
}

func TestSegmentioConsumer_CommitsMalformedJSON(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &fakeMessageReader{
		messages: []segmentio.Message{{Value: []byte(`not-json`)}},
		onCommit: cancel,
	}
	consumer := NewSegmentioConsumer(DefaultSegmentioConsumerConfig([]string{"localhost:9092"}, "test-group"), zerolog.Nop())
	consumer.handlerBackoff = 0

	consumer.consumeLoop(ctx, "server.created", reader)

	if got := reader.commitCount(); got != 1 {
		t.Fatalf("expected malformed JSON to be committed once, got %d", got)
	}
}

func TestSegmentioConsumer_CommitsWhenNoHandlerRegistered(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &fakeMessageReader{
		messages: []segmentio.Message{{
			Value: []byte(`{"event_id":"evt-1","event_type":"unknown","timestamp":"2026-06-11T00:00:00Z","source":"test","data":{}}`),
		}},
		onCommit: cancel,
	}
	consumer := NewSegmentioConsumer(DefaultSegmentioConsumerConfig([]string{"localhost:9092"}, "test-group"), zerolog.Nop())
	consumer.handlerBackoff = 0

	consumer.consumeLoop(ctx, "unknown", reader)

	if got := reader.commitCount(); got != 1 {
		t.Fatalf("expected commit without handler, got %d", got)
	}
}

func TestSegmentioConsumer_CommitErrorDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &fakeMessageReader{
		messages: []segmentio.Message{{
			Value: []byte(`{"event_id":"evt-1","event_type":"server.created","timestamp":"2026-06-11T00:00:00Z","source":"test","data":{}}`),
		}},
		commitErr: fmt.Errorf("commit failed"),
		onCommit:  cancel,
	}
	consumer := NewSegmentioConsumer(DefaultSegmentioConsumerConfig([]string{"localhost:9092"}, "test-group"), zerolog.Nop())
	consumer.handlerBackoff = 0
	consumer.handlers["server.created"] = func(ctx context.Context, event *Event) error {
		cancel()
		return nil
	}

	consumer.consumeLoop(ctx, "server.created", reader)
}

func TestSegmentioConsumer_SubscribeStartAndCloseStates(t *testing.T) {
	consumer := NewSegmentioConsumer(DefaultSegmentioConsumerConfig([]string{"localhost:9092"}, "test-group"), zerolog.Nop())
	if err := consumer.Subscribe("server.created", "group-1", func(ctx context.Context, event *Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	reader := &fakeMessageReader{}
	consumer.readers["server.created"] = reader
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := consumer.Start(context.Background()); err == nil {
		t.Fatal("expected already started error")
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !reader.closed {
		t.Fatal("expected reader to be closed")
	}
	if err := consumer.Subscribe("other", "group-1", func(ctx context.Context, event *Event) error { return nil }); err == nil {
		t.Fatal("expected subscribe error after close")
	}
}

func TestSegmentioConsumer_StartAfterClose(t *testing.T) {
	consumer := NewSegmentioConsumer(DefaultSegmentioConsumerConfig([]string{"localhost:9092"}, "test-group"), zerolog.Nop())
	if err := consumer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := consumer.Start(context.Background()); err == nil {
		t.Fatal("expected start error after close")
	}
}
