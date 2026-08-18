package output

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go"

	"pdc/parser"
)

// ReadingHandler is called for each consumed parsed reading.
type ReadingHandler func(ctx context.Context, r parser.Reading) error

// ReadingsConsumer reads parsed readings from Kafka (pmu.readings).
// Use separate consumer-group IDs so dashboard and sink each get a full copy (fan-out).
type ReadingsConsumer struct {
	reader *kafka.Reader
	topic  string
	group  string
}

// NewReadingsConsumerFromEnv creates a consumer for the readings topic.
// groupDefault is used when groupEnvKey is unset (e.g. "pdc-dashboard", "pdc-sink").
func NewReadingsConsumerFromEnv(groupEnvKey, groupDefault string) *ReadingsConsumer {
	brokersEnv := envOr("KAFKA_BROKERS", "127.0.0.1:9093")
	topic := envOr("KAFKA_TOPIC", "pmu.readings")
	group := envOr(groupEnvKey, groupDefault)

	brokers := splitBrokers(brokersEnv)
	if len(brokers) == 0 {
		return nil
	}

	minBytes := envIntOr("KAFKA_READINGS_MIN_BYTES", 1)
	maxBytes := envIntOr("KAFKA_READINGS_MAX_BYTES", 10_000_000)
	maxWaitMs := envIntOr("KAFKA_READINGS_MAX_WAIT_MS", 50)
	queueCapacity := envIntOr("KAFKA_READINGS_QUEUE_CAPACITY", 1024)

	if minBytes < 1 {
		minBytes = 1
	}
	if maxBytes < minBytes {
		maxBytes = 10_000_000
	}
	if maxWaitMs < 1 {
		maxWaitMs = 50
	}
	if queueCapacity < 1 {
		queueCapacity = 256
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        group,
		MinBytes:       minBytes,
		MaxBytes:       maxBytes,
		MaxWait:        time.Duration(maxWaitMs) * time.Millisecond,
		QueueCapacity:  queueCapacity,
		CommitInterval: time.Second,
		StartOffset:    kafka.LastOffset,
	})

	log.Printf("kafka readings consumer ready: brokers=%v topic=%q group=%q", brokers, topic, group)
	return &ReadingsConsumer{reader: r, topic: topic, group: group}
}

func (c *ReadingsConsumer) Topic() string {
	if c == nil {
		return ""
	}
	return c.topic
}

func (c *ReadingsConsumer) Group() string {
	if c == nil {
		return ""
	}
	return c.group
}

func (c *ReadingsConsumer) Run(ctx context.Context, handler ReadingHandler) error {
	if c == nil || c.reader == nil {
		return fmt.Errorf("readings consumer not configured")
	}
	if handler == nil {
		return fmt.Errorf("readings handler is nil")
	}

	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("fetch reading: %w", err)
		}

		var reading parser.Reading
		if err := json.Unmarshal(msg.Value, &reading); err != nil {
			log.Printf("[readings-consumer group=%s] unmarshal error: %v", c.group, err)
			_ = c.reader.CommitMessages(ctx, msg)
			continue
		}
		if reading.PMUName == "" && len(msg.Key) > 0 {
			reading.PMUName = string(msg.Key)
		}

		if err := handler(ctx, reading); err != nil {
			log.Printf("[readings-consumer group=%s] handler error pmu=%s: %v", c.group, reading.PMUName, err)
		}

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("[readings-consumer group=%s] commit error: %v", c.group, err)
		}
	}
}

func (c *ReadingsConsumer) Close() error {
	if c == nil || c.reader == nil {
		return nil
	}
	return c.reader.Close()
}
