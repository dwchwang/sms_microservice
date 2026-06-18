package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
)

// SegmentioProducer is a Kafka producer backed by segmentio/kafka-go (Writer).
type SegmentioProducer struct {
	writer messageWriter
	logger zerolog.Logger
	closed bool
}

type messageWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// SegmentioProducerConfig holds configuration for the segmentio producer.
type SegmentioProducerConfig struct {
	Brokers      []string
	ClientID     string
	BatchSize    int           // default: 100
	BatchTimeout time.Duration // default: 100ms
	Async        bool          // default: false (sync)
}

// DefaultSegmentioProducerConfig returns sensible defaults.
func DefaultSegmentioProducerConfig(brokers []string) *SegmentioProducerConfig {
	return &SegmentioProducerConfig{
		Brokers:      brokers,
		ClientID:     "vcs-sms-producer",
		BatchSize:    100,
		BatchTimeout: 100 * time.Millisecond,
		Async:        false,
	}
}

// NewSegmentioProducer creates a new segmentio/kafka-go backed producer.
func NewSegmentioProducer(cfg *SegmentioProducerConfig, logger zerolog.Logger) *SegmentioProducer {
	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    cfg.BatchSize,
		BatchTimeout: cfg.BatchTimeout,
		Async:        cfg.Async,
		RequiredAcks: kafka.RequireAll,
		Compression:  kafka.Snappy,
	}

	logger.Info().
		Strs("brokers", cfg.Brokers).
		Str("client_id", cfg.ClientID).
		Int("batch_size", cfg.BatchSize).
		Msg("Segmentio Kafka producer connected")

	return &SegmentioProducer{
		writer: writer,
		logger: logger,
	}
}

// Publish sends an event to the specified Kafka topic.
func (p *SegmentioProducer) Publish(ctx context.Context, topic string, key string, value interface{}) error {
	if p.closed {
		return fmt.Errorf("producer is closed")
	}

	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	msg := kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: jsonValue,
		Time:  time.Now().UTC(),
	}

	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		p.logger.Error().
			Err(err).
			Str("topic", topic).
			Str("key", key).
			Msg("Failed to publish Kafka event")
		return fmt.Errorf("failed to write message: %w", err)
	}

	p.logger.Debug().
		Str("topic", topic).
		Str("key", key).
		Msg("Kafka event published")

	return nil
}

// Close shuts down the producer.
func (p *SegmentioProducer) Close() error {
	p.closed = true
	return p.writer.Close()
}
