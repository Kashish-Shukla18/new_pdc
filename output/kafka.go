package output

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"

	"pdc/parser"
)

// Publisher writes parsed readings to Kafka topic pmu.readings.
type Publisher struct {
	writer  *kafka.Writer
	brokers []string
	topic   string
}

func envOr(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func envIntOr(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func requiredAcksFromEnv() kafka.RequiredAcks {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("KAFKA_REQUIRED_ACKS")))
	switch v {
	case "0", "none":
		return kafka.RequireNone
	case "1", "one":
		return kafka.RequireOne
	default:
		return kafka.RequireAll
	}
}

func envBoolOr(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func ensureTopicIfEnabled(ctx context.Context, brokers []string, topic string) {
	if !envBoolOr("KAFKA_ENSURE_TOPIC", true) {
		return
	}
	if len(brokers) == 0 || topic == "" {
		return
	}

	partitions := envIntOr("KAFKA_TOPIC_PARTITIONS", 3)
	replicationFactor := envIntOr("KAFKA_TOPIC_REPLICATION_FACTOR", 1)
	adminTimeoutMs := envIntOr("KAFKA_ADMIN_TIMEOUT_MS", 8000)

	if partitions < 1 {
		partitions = 1
	}
	if replicationFactor < 1 {
		replicationFactor = 1
	}
	if adminTimeoutMs < 1000 {
		adminTimeoutMs = 1000
	}

	adminCtx, cancel := context.WithTimeout(ctx, time.Duration(adminTimeoutMs)*time.Millisecond)
	defer cancel()

	if err := ensureTopic(adminCtx, brokers[0], topic, partitions, replicationFactor); err != nil {
		log.Printf("kafka topic ensure skipped/failed for %q: %v", topic, err)
		return
	}

	log.Printf("kafka topic ready: topic=%q partitions=%d replication_factor=%d", topic, partitions, replicationFactor)
}

func ensureTopic(ctx context.Context, broker, topic string, partitions, replicationFactor int) error {
	conn, err := kafka.DialContext(ctx, "tcp", broker)
	if err != nil {
		return fmt.Errorf("dial broker %s: %w", broker, err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("get controller: %w", err)
	}

	controllerAddr := net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))
	controllerConn, err := kafka.DialContext(ctx, "tcp", controllerAddr)
	if err != nil {
		return fmt.Errorf("dial controller %s: %w", controllerAddr, err)
	}
	defer controllerConn.Close()

	err = controllerConn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: replicationFactor,
	})
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "already exists") {
			return nil
		}
		return fmt.Errorf("create topic %q: %w", topic, err)
	}

	return nil
}

func isUnknownTopicError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unknown topic") || strings.Contains(errMsg, "unknown topic or partition")
}

func NewPublisherFromEnv() *Publisher {
	brokersEnv := envOr("KAFKA_BROKERS", "127.0.0.1:9093")
	topic := envOr("KAFKA_TOPIC", "pmu.readings")

	brokers := make([]string, 0)
	for _, b := range strings.Split(brokersEnv, ",") {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			brokers = append(brokers, trimmed)
		}
	}

	if len(brokers) == 0 {
		return nil
	}

	ensureTopicIfEnabled(context.Background(), brokers, topic)

	// BatchTimeout: how long the writer waits to fill a batch before flushing.
	// Higher values reduce per-message overhead; default 50ms is a good balance
	// for PMU workloads (50fps × N PMUs produces many small messages rapidly).
	batchTimeoutMs := envIntOr("KAFKA_BATCH_TIMEOUT_MS", 1)
	// BatchSize: max messages per batch; 500 is safe for 50fps × 10 PMUs.
	batchSize := envIntOr("KAFKA_BATCH_SIZE", 500)
	writeTimeoutMs := envIntOr("KAFKA_WRITE_TIMEOUT_MS", 15000)
	readTimeoutMs := envIntOr("KAFKA_READ_TIMEOUT_MS", 15000)
	maxAttempts := envIntOr("KAFKA_MAX_ATTEMPTS", 10)
	// Async: true lets WriteMessages return immediately without waiting for the
	// broker ACK. Combined with the spool-on-error path, this is safe and removes
	// Kafka from the synchronous hot path entirely.
	asyncMode := envBoolOr("KAFKA_ASYNC", true)

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

	log.Printf("kafka writer ready: brokers=%v topic=%q async=%t batch_timeout=%dms batch_size=%d balancer=hash",
		brokers, topic, asyncMode, batchTimeoutMs, batchSize)

	return &Publisher{writer: w, brokers: brokers, topic: topic}
}

func (p *Publisher) Publish(ctx context.Context, r parser.Reading) error {
	if p == nil || p.writer == nil {
		return nil
	}

	payload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal reading: %w", err)
	}

	msg := kafka.Message{
		Key:   []byte(r.PMUName),
		Value: payload,
		Time:  r.Timestamp,
	}

	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		if isUnknownTopicError(err) {
			ensureTopicIfEnabled(ctx, p.brokers, p.topic)
			if retryErr := p.writer.WriteMessages(ctx, msg); retryErr == nil {
				return nil
			} else {
				return fmt.Errorf("kafka write retry after topic ensure: %w", retryErr)
			}
		}
		return fmt.Errorf("kafka write: %w", err)
	}
	return nil
}

func (p *Publisher) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}
