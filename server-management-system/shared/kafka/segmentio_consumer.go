package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
)

// SegmentioConsumer is a Kafka consumer backed by segmentio/kafka-go (Reader).
// Each subscribed topic gets its own Reader goroutine.
type SegmentioConsumer struct {
	readers        map[string]messageReader // topic → reader
	handlers       map[string]EventHandler  // topic → handler
	mu             sync.RWMutex
	logger         zerolog.Logger
	groupID        string
	brokers        []string
	closed         bool
	started        bool
	cancelFunc     context.CancelFunc // single cancel for all reader goroutines
	handlerBackoff time.Duration
}

type messageReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// SegmentioConsumerConfig holds configuration for the segmentio consumer.
type SegmentioConsumerConfig struct {
	Brokers        []string
	GroupID        string
	ClientID       string
	MinBytes       int           // default: 10KB
	MaxBytes       int           // default: 10MB
	MaxWait        time.Duration // default: 1s
	CommitInterval time.Duration // default: 5s
	StartOffset    int64         // kafka.LastOffset or kafka.FirstOffset
}

// DefaultSegmentioConsumerConfig returns sensible defaults.
func DefaultSegmentioConsumerConfig(brokers []string, groupID string) *SegmentioConsumerConfig {
	return &SegmentioConsumerConfig{
		Brokers:        brokers,
		GroupID:        groupID,
		ClientID:       "vcs-sms-consumer",
		MinBytes:       10e3, // 10KB
		MaxBytes:       10e6, // 10MB
		MaxWait:        1 * time.Second,
		CommitInterval: 5 * time.Second,
		StartOffset:    kafka.LastOffset,
	}
}

// NewSegmentioConsumer creates a new segmentio/kafka-go backed consumer.
func NewSegmentioConsumer(cfg *SegmentioConsumerConfig, logger zerolog.Logger) *SegmentioConsumer {
	logger.Info().
		Strs("brokers", cfg.Brokers).
		Str("group_id", cfg.GroupID).
		Msg("Segmentio Kafka consumer created")

	return &SegmentioConsumer{
		readers:        make(map[string]messageReader),
		handlers:       make(map[string]EventHandler),
		logger:         logger,
		groupID:        cfg.GroupID,
		brokers:        cfg.Brokers,
		handlerBackoff: time.Second,
	}
}

// Subscribe registers a handler for a topic. Creates a new Reader for each topic.
// Must be called before Start().
func (c *SegmentioConsumer) Subscribe(topic, groupID string, handler EventHandler) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("consumer is closed")
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        c.brokers,
		GroupID:        groupID,
		Topic:          topic,
		MinBytes:       10e3, // 10KB
		MaxBytes:       10e6, // 10MB
		MaxWait:        1 * time.Second,
		CommitInterval: 5 * time.Second,
		StartOffset:    kafka.FirstOffset,
	})

	c.readers[topic] = r
	c.handlers[topic] = handler

	c.logger.Info().
		Str("topic", topic).
		Str("group_id", groupID).
		Msg("Kafka consumer subscribed")

	return nil
}

// Start begins consuming messages. Spawns a goroutine per subscribed topic.
// Blocks until ctx is cancelled or Close() is called.
// Returns error if called more than once or after Close().
func (c *SegmentioConsumer) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("consumer is closed")
	}
	if c.started {
		c.mu.Unlock()
		return fmt.Errorf("consumer already started")
	}
	c.started = true

	// Create a cancellable context so Close() can stop all readers
	consumerCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel

	topics := make([]string, 0, len(c.readers))
	for t := range c.readers {
		topics = append(topics, t)
	}
	c.mu.Unlock()

	c.logger.Info().Strs("topics", topics).Msg("Kafka consumer started")

	var wg sync.WaitGroup

	c.mu.RLock()
	for topic, reader := range c.readers {
		wg.Add(1)
		go func(t string, r messageReader) {
			defer wg.Done()
			c.consumeLoop(consumerCtx, t, r)
		}(topic, reader)
	}
	c.mu.RUnlock()

	wg.Wait()
	c.logger.Info().Msg("Kafka consumer stopped")
	return nil
}

// consumeLoop reads messages from a single topic/reader.
func (c *SegmentioConsumer) consumeLoop(ctx context.Context, topic string, reader messageReader) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil || err == io.EOF {
				return // context cancelled, normal shutdown
			}
			c.logger.Error().
				Err(err).
				Str("topic", topic).
				Msg("Failed to read Kafka message, retrying...")
			time.Sleep(time.Second)
			continue
		}

		// Parse event from message value
		var event Event
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			c.logger.Error().
				Err(err).
				Str("topic", topic).
				Int64("offset", msg.Offset).
				Msg("Failed to unmarshal Kafka event")
			c.commitMessage(ctx, topic, reader, msg)
			continue
		}

		// Call handler
		c.mu.RLock()
		handler, exists := c.handlers[topic]
		c.mu.RUnlock()

		if !exists {
			c.logger.Warn().Str("topic", topic).Msg("No handler registered")
			c.commitMessage(ctx, topic, reader, msg)
			continue
		}

		if err := handler(ctx, &event); err != nil {
			c.logger.Error().
				Err(err).
				Str("topic", topic).
				Str("event_type", event.EventType).
				Msg("Event handler failed")
			if c.handlerBackoff > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(c.handlerBackoff):
				}
			}
			continue
		}

		c.commitMessage(ctx, topic, reader, msg)
	}
}

func (c *SegmentioConsumer) commitMessage(ctx context.Context, topic string, reader messageReader, msg kafka.Message) {
	if err := reader.CommitMessages(ctx, msg); err != nil && ctx.Err() == nil {
		c.logger.Error().
			Err(err).
			Str("topic", topic).
			Int64("offset", msg.Offset).
			Msg("Failed to commit Kafka message")
	}
}

// Close shuts down all readers and cancels the consumer context.
func (c *SegmentioConsumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true

	// Cancel consumer context to stop all reader goroutines
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	// Close all readers
	for topic, reader := range c.readers {
		if err := reader.Close(); err != nil {
			c.logger.Error().Err(err).Str("topic", topic).Msg("Failed to close reader")
		}
	}

	c.logger.Info().Msg("All Kafka readers closed")
	return nil
}
