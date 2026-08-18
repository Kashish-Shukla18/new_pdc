package output

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

const (
	rawHeaderPMUName   = "pmu_name"
	rawHeaderFrameType = "frame_type"
	rawHeaderIDCode    = "idcode"
	rawHeaderRecvAt    = "received_at_unix_nano"

	FrameTypeData = "data"
	FrameTypeCFG2 = "cfg2"
	FrameTypeHDR  = "hdr"
)

// RawFramePublisher writes CRC-verified C37.118 frame bytes to Kafka.
type RawFramePublisher struct {
	writer  *kafka.Writer
	brokers []string
	topic   string
}

// RawFrame is a consumed raw C37.118 frame with metadata.
type RawFrame struct {
	PMUName    string
	FrameType  string
	IDCode     uint16
	ReceivedAt time.Time
	Payload    []byte
}

// RawFrameHandler is invoked for each consumed raw frame.
type RawFrameHandler func(ctx context.Context, frame RawFrame) error

// RawFrameConsumer reads raw frames from Kafka for a consumer group.
type RawFrameConsumer struct {
	reader *kafka.Reader
	topic  string
	group  string
}

func NewRawFramePublisherFromEnv() *RawFramePublisher {
	brokersEnv := envOr("KAFKA_BROKERS", "127.0.0.1:9093")
	topic := envOr("KAFKA_RAW_TOPIC", "pmu.raw.frames")

	brokers := splitBrokers(brokersEnv)
	if len(brokers) == 0 {
		return nil
	}

	// Ensure raw topic independently of the readings topic.
	partitions := envIntOr("KAFKA_RAW_TOPIC_PARTITIONS", envIntOr("KAFKA_TOPIC_PARTITIONS", 6))
	replicationFactor := envIntOr("KAFKA_TOPIC_REPLICATION_FACTOR", 1)
	adminTimeoutMs := envIntOr("KAFKA_ADMIN_TIMEOUT_MS", 8000)
	if envBoolOr("KAFKA_ENSURE_TOPIC", true) {
		adminCtx, cancel := context.WithTimeout(context.Background(), time.Duration(adminTimeoutMs)*time.Millisecond)
		defer cancel()
		if err := ensureTopic(adminCtx, brokers[0], topic, partitions, replicationFactor); err != nil {
			log.Printf("kafka raw topic ensure skipped/failed for %q: %v", topic, err)
		} else {
			log.Printf("kafka raw topic ready: topic=%q partitions=%d replication_factor=%d", topic, partitions, replicationFactor)
		}
	}

	batchTimeoutMs := envIntOr("KAFKA_RAW_BATCH_TIMEOUT_MS", envIntOr("KAFKA_BATCH_TIMEOUT_MS", 20))
	batchSize := envIntOr("KAFKA_RAW_BATCH_SIZE", envIntOr("KAFKA_BATCH_SIZE", 500))
	writeTimeoutMs := envIntOr("KAFKA_WRITE_TIMEOUT_MS", 15000)
	readTimeoutMs := envIntOr("KAFKA_READ_TIMEOUT_MS", 15000)
	maxAttempts := envIntOr("KAFKA_MAX_ATTEMPTS", 10)
	// Default sync writes so a slow broker applies backpressure to the TCP
	// read loop instead of dropping frames in the ingress.
	asyncMode := envBoolOr("KAFKA_RAW_ASYNC", false)

	if batchTimeoutMs < 1 {
		batchTimeoutMs = 1
	}
	if batchSize < 1 {
		batchSize = 100
	}
	if writeTimeoutMs < 1000 {
		writeTimeoutMs = 1000
	}
	if readTimeoutMs < 1000 {
		readTimeoutMs = 1000
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	// Hash balancer keeps all frames for one PMU on one partition (CFG before DATA).
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		RequiredAcks: requiredAcksFromEnv(),
		BatchTimeout: time.Duration(batchTimeoutMs) * time.Millisecond,
		BatchSize:    batchSize,
		WriteTimeout: time.Duration(writeTimeoutMs) * time.Millisecond,
		ReadTimeout:  time.Duration(readTimeoutMs) * time.Millisecond,
		MaxAttempts:  maxAttempts,
		Async:        asyncMode,
		Balancer:     &kafka.Hash{},
	}

	log.Printf("kafka raw writer ready: brokers=%v topic=%q async=%t batch_timeout=%dms batch_size=%d balancer=hash",
		brokers, topic, asyncMode, batchTimeoutMs, batchSize)

	return &RawFramePublisher{writer: w, brokers: brokers, topic: topic}
}

func (p *RawFramePublisher) Publish(ctx context.Context, pmuName, frameType string, idCode uint16, raw []byte) error {
	if p == nil || p.writer == nil {
		return nil
	}
	if len(raw) == 0 {
		return fmt.Errorf("empty raw frame")
	}

	now := time.Now()
	msg := kafka.Message{
		Key:   []byte(pmuName),
		Value: append([]byte(nil), raw...),
		Time:  now,
		Headers: []kafka.Header{
			{Key: rawHeaderPMUName, Value: []byte(pmuName)},
			{Key: rawHeaderFrameType, Value: []byte(frameType)},
			{Key: rawHeaderIDCode, Value: []byte(strconv.FormatUint(uint64(idCode), 10))},
			{Key: rawHeaderRecvAt, Value: []byte(strconv.FormatInt(now.UnixNano(), 10))},
		},
	}

	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		if isUnknownTopicError(err) {
			partitions := envIntOr("KAFKA_RAW_TOPIC_PARTITIONS", envIntOr("KAFKA_TOPIC_PARTITIONS", 6))
			replicationFactor := envIntOr("KAFKA_TOPIC_REPLICATION_FACTOR", 1)
			_ = ensureTopic(ctx, p.brokers[0], p.topic, partitions, replicationFactor)
			if retryErr := p.writer.WriteMessages(ctx, msg); retryErr == nil {
				return nil
			} else {
				return fmt.Errorf("kafka raw write retry after topic ensure: %w", retryErr)
			}
		}
		return fmt.Errorf("kafka raw write: %w", err)
	}
	return nil
}

func (p *RawFramePublisher) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}

func NewRawFrameConsumerFromEnv() *RawFrameConsumer {
	brokersEnv := envOr("KAFKA_BROKERS", "127.0.0.1:9093")
	topic := envOr("KAFKA_RAW_TOPIC", "pmu.raw.frames")
	group := envOr("KAFKA_RAW_GROUP", "pdc-processor")

	brokers := splitBrokers(brokersEnv)
	if len(brokers) == 0 {
		return nil
	}

	minBytes := envIntOr("KAFKA_RAW_MIN_BYTES", 1)
	maxBytes := envIntOr("KAFKA_RAW_MAX_BYTES", 10_000_000)
	maxWaitMs := envIntOr("KAFKA_RAW_MAX_WAIT_MS", 50)
	queueCapacity := envIntOr("KAFKA_RAW_QUEUE_CAPACITY", 1024)

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

	log.Printf("kafka raw consumer ready: brokers=%v topic=%q group=%q", brokers, topic, group)
	return &RawFrameConsumer{reader: r, topic: topic, group: group}
}

func (c *RawFrameConsumer) Run(ctx context.Context, handler RawFrameHandler) error {
	if c == nil || c.reader == nil {
		return fmt.Errorf("raw frame consumer not configured")
	}
	if handler == nil {
		return fmt.Errorf("raw frame handler is nil")
	}

	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("fetch raw frame: %w", err)
		}

		frame := decodeRawFrame(msg)
		if err := handler(ctx, frame); err != nil {
			log.Printf("[raw-consumer] handler error pmu=%s type=%s: %v", frame.PMUName, frame.FrameType, err)
			// Still commit to avoid poison-pill loops; parse errors are counted upstream.
		}

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("[raw-consumer] commit error: %v", err)
		}
	}
}

func (c *RawFrameConsumer) Close() error {
	if c == nil || c.reader == nil {
		return nil
	}
	return c.reader.Close()
}

func decodeRawFrame(msg kafka.Message) RawFrame {
	frame := RawFrame{
		Payload:    append([]byte(nil), msg.Value...),
		ReceivedAt: msg.Time,
		PMUName:    string(msg.Key),
		FrameType:  FrameTypeData,
	}
	if frame.ReceivedAt.IsZero() {
		frame.ReceivedAt = time.Now()
	}

	for _, h := range msg.Headers {
		switch h.Key {
		case rawHeaderPMUName:
			if len(h.Value) > 0 {
				frame.PMUName = string(h.Value)
			}
		case rawHeaderFrameType:
			if len(h.Value) > 0 {
				frame.FrameType = string(h.Value)
			}
		case rawHeaderIDCode:
			if n, err := strconv.ParseUint(string(h.Value), 10, 16); err == nil {
				frame.IDCode = uint16(n)
			}
		case rawHeaderRecvAt:
			if n, err := strconv.ParseInt(string(h.Value), 10, 64); err == nil && n > 0 {
				frame.ReceivedAt = time.Unix(0, n)
			}
		}
	}

	if frame.PMUName == "" {
		frame.PMUName = "unknown"
	}
	return frame
}

func splitBrokers(brokersEnv string) []string {
	brokers := make([]string, 0)
	for _, b := range strings.Split(brokersEnv, ",") {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			brokers = append(brokers, trimmed)
		}
	}
	return brokers
}
