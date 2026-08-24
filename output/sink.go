package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"pdc/parser"
)

var errSinkDisabled = errors.New("sink disabled")

// Sink stores each validated reading to Redis (live state) and Postgres/TimescaleDB (history).
type Sink struct {
	redis         *redis.Client
	pg            *PostgresHistory
	redisTTL      time.Duration
	redisKeyPrefx string
}

func env(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// NewSinkFromEnv initialises Sink clients using environment variables / docker-compose defaults.
func NewSinkFromEnv(ctx context.Context) (*Sink, error) {
	if strings.EqualFold(strings.TrimSpace(env("ENABLE_SINK", "true")), "false") {
		return nil, errSinkDisabled
	}

	redisAddr := env("REDIS_ADDR", "127.0.0.1:6380")
	redisDB := envInt("REDIS_DB", 0)
	redisTTL := time.Duration(envInt("REDIS_TTL_SECONDS", 120)) * time.Second
	keyPrefix := env("REDIS_KEY_PREFIX", "pmu")

	redisPoolSize := envInt("REDIS_POOL_SIZE", 20)
	redisMinIdle := envInt("REDIS_MIN_IDLE_CONNS", 5)
	redisDialTimeout := time.Duration(envInt("REDIS_DIAL_TIMEOUT_MS", 3000)) * time.Millisecond
	redisReadTimeout := time.Duration(envInt("REDIS_READ_TIMEOUT_MS", 300)) * time.Millisecond
	redisWriteTimeout := time.Duration(envInt("REDIS_WRITE_TIMEOUT_MS", 300)) * time.Millisecond

	redisClient := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		DB:           redisDB,
		PoolSize:     redisPoolSize,
		MinIdleConns: redisMinIdle,
		DialTimeout:  redisDialTimeout,
		ReadTimeout:  redisReadTimeout,
		WriteTimeout: redisWriteTimeout,
	})

	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed (%s): %w", redisAddr, err)
	}

	pg, err := NewPostgresHistoryFromEnv(ctx)
	if err != nil {
		_ = redisClient.Close()
		return nil, fmt.Errorf("postgres history: %w", err)
	}

	s := &Sink{
		redis:         redisClient,
		pg:            pg,
		redisTTL:      redisTTL,
		redisKeyPrefx: keyPrefix,
	}

	log.Printf("sink ready: redis=%s postgres=%s", redisAddr, "history+config")
	return s, nil
}

func (s *Sink) keyLatest(pmu string) string {
	return fmt.Sprintf("%s:%s:latest", s.redisKeyPrefx, pmu)
}

func (s *Sink) keyTimeline(pmu string) string {
	return fmt.Sprintf("%s:%s:timeline", s.redisKeyPrefx, pmu)
}

// Store writes one reading to Redis and enqueues it for Postgres batch insert.
func (s *Sink) Store(ctx context.Context, r parser.Reading) error {
	if s == nil {
		return nil
	}

	latestPayload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal reading for redis: %w", err)
	}

	timelineLine := fmt.Sprintf(
		`{"ts":"%s","freq":%.6f,"rocof":%.6f,"mw":%.6f,"mvar":%.6f,"stat":%d,"digital":%d}`,
		r.Timestamp.Format(time.RFC3339Nano),
		r.Frequency,
		r.ROCOF,
		r.MW,
		r.MVAR,
		r.Stat,
		r.Digital,
	)
	ms := float64(r.Timestamp.UnixNano()) / float64(time.Millisecond)

	pipe := s.redis.Pipeline()
	pipe.ZAdd(ctx, s.keyTimeline(r.PMUName), redis.Z{Score: ms, Member: timelineLine})
	pipe.Expire(ctx, s.keyTimeline(r.PMUName), s.redisTTL)
	pipe.Set(ctx, s.keyLatest(r.PMUName), latestPayload, s.redisTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline: %w", err)
	}

	if s.pg != nil {
		s.pg.Enqueue(r)
	}
	return nil
}

// GetLatestReading returns the full latest Reading for one PMU from Redis.
func (s *Sink) GetLatestReading(ctx context.Context, pmuName string) (parser.Reading, bool, error) {
	if s == nil || s.redis == nil {
		return parser.Reading{}, false, nil
	}
	raw, err := s.redis.Get(ctx, s.keyLatest(pmuName)).Bytes()
	if err == redis.Nil {
		return parser.Reading{}, false, nil
	}
	if err != nil {
		return parser.Reading{}, false, fmt.Errorf("redis get latest %s: %w", pmuName, err)
	}
	var r parser.Reading
	if err := json.Unmarshal(raw, &r); err != nil {
		return parser.Reading{}, false, fmt.Errorf("unmarshal latest %s: %w", pmuName, err)
	}
	if r.PMUName == "" {
		r.PMUName = pmuName
	}
	return r, true, nil
}

// ListLatestReadings scans Redis for all :latest keys and returns full Readings.
func (s *Sink) ListLatestReadings(ctx context.Context) ([]parser.Reading, error) {
	if s == nil || s.redis == nil {
		return nil, nil
	}

	pattern := fmt.Sprintf("%s:*:latest", s.redisKeyPrefx)
	var (
		cursor uint64
		out    []parser.Reading
	)
	for {
		keys, next, err := s.redis.Scan(ctx, cursor, pattern, 64).Result()
		if err != nil {
			return out, fmt.Errorf("redis scan %s: %w", pattern, err)
		}
		for _, key := range keys {
			raw, err := s.redis.Get(ctx, key).Bytes()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				log.Printf("redis get %s: %v", key, err)
				continue
			}
			var r parser.Reading
			if err := json.Unmarshal(raw, &r); err != nil {
				log.Printf("redis latest unmarshal skip %s: %v", key, err)
				continue
			}
			if r.PMUName == "" {
				r.PMUName = pmuNameFromLatestKey(key, s.redisKeyPrefx)
			}
			if r.PMUName != "" {
				out = append(out, r)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return out, nil
}

func pmuNameFromLatestKey(key, prefix string) string {
	trim := strings.TrimPrefix(key, prefix+":")
	trim = strings.TrimSuffix(trim, ":latest")
	return strings.TrimSpace(trim)
}

// Flush forces pending Postgres batches to disk.
func (s *Sink) Flush() {
	if s == nil || s.pg == nil {
		return
	}
	s.pg.Flush()
}

func (s *Sink) Close() {
	if s == nil {
		return
	}
	if s.pg != nil {
		s.pg.Close()
	}
	if s.redis != nil {
		_ = s.redis.Close()
	}
}
