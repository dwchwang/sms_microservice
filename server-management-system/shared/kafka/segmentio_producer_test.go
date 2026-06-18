package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	segmentio "github.com/segmentio/kafka-go"
)

type fakeMessageWriter struct {
	messages []segmentio.Message
	writeErr error
	closed   bool
}

func (w *fakeMessageWriter) WriteMessages(ctx context.Context, msgs ...segmentio.Message) error {
	if w.writeErr != nil {
		return w.writeErr
	}
	w.messages = append(w.messages, msgs...)
	return nil
}

func (w *fakeMessageWriter) Close() error {
	w.closed = true
	return nil
}

func TestDefaultSegmentioProducerConfig(t *testing.T) {
	cfg := DefaultSegmentioProducerConfig([]string{"localhost:9092"})
	if cfg.ClientID != "vcs-sms-producer" || cfg.BatchSize != 100 || cfg.BatchTimeout != 100*time.Millisecond || cfg.Async {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestNewSegmentioProducer(t *testing.T) {
	cfg := DefaultSegmentioProducerConfig([]string{"localhost:9092"})
	producer := NewSegmentioProducer(cfg, zerolog.Nop())
	if producer == nil || producer.writer == nil {
		t.Fatal("expected producer with writer")
	}
	_ = producer.Close()
}

func TestSegmentioProducer_PublishSuccess(t *testing.T) {
	writer := &fakeMessageWriter{}
	producer := &SegmentioProducer{writer: writer, logger: zerolog.Nop()}

	event := &Event{EventID: "evt-1", EventType: "server.created"}
	if err := producer.Publish(context.Background(), "topic", "key", event); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	if len(writer.messages) != 1 {
		t.Fatalf("expected one message, got %d", len(writer.messages))
	}
	if writer.messages[0].Topic != "topic" || string(writer.messages[0].Key) != "key" {
		t.Fatalf("unexpected message metadata: %#v", writer.messages[0])
	}
	var decoded Event
	if err := json.Unmarshal(writer.messages[0].Value, &decoded); err != nil {
		t.Fatalf("message value is not event JSON: %v", err)
	}
	if decoded.EventID != "evt-1" {
		t.Fatalf("unexpected event payload: %#v", decoded)
	}
}

func TestSegmentioProducer_PublishErrors(t *testing.T) {
	t.Run("closed", func(t *testing.T) {
		producer := &SegmentioProducer{writer: &fakeMessageWriter{}, logger: zerolog.Nop(), closed: true}
		if err := producer.Publish(context.Background(), "topic", "key", &Event{}); err == nil {
			t.Fatal("expected closed producer error")
		}
	})

	t.Run("marshal", func(t *testing.T) {
		producer := &SegmentioProducer{writer: &fakeMessageWriter{}, logger: zerolog.Nop()}
		if err := producer.Publish(context.Background(), "topic", "key", make(chan int)); err == nil {
			t.Fatal("expected marshal error")
		}
	})

	t.Run("write", func(t *testing.T) {
		producer := &SegmentioProducer{writer: &fakeMessageWriter{writeErr: fmt.Errorf("broker down")}, logger: zerolog.Nop()}
		if err := producer.Publish(context.Background(), "topic", "key", &Event{}); err == nil {
			t.Fatal("expected write error")
		}
	})
}

func TestSegmentioProducer_Close(t *testing.T) {
	writer := &fakeMessageWriter{}
	producer := &SegmentioProducer{writer: writer, logger: zerolog.Nop()}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !producer.closed || !writer.closed {
		t.Fatalf("expected producer and writer closed")
	}
}
