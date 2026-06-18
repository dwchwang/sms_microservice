package kafka

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestDummyProducer(t *testing.T) {
	producer := NewDummyProducer(zerolog.Nop())
	if err := producer.Publish(context.Background(), "topic", "key", &Event{EventID: "evt-1"}); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := producer.Publish(context.Background(), "topic", "key", &Event{EventID: "evt-1"}); err == nil {
		t.Fatal("expected publish error after close")
	}
}

func TestDummyConsumer(t *testing.T) {
	consumer := NewDummyConsumer(zerolog.Nop())
	if err := consumer.Subscribe("topic", "group", func(ctx context.Context, event *Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := consumer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := consumer.Subscribe("topic", "group", func(ctx context.Context, event *Event) error { return nil }); err == nil {
		t.Fatal("expected subscribe error after close")
	}
}
