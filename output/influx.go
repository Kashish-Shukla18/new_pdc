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
	"sync"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxapi "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/redis/go-redis/v9"

	"pdc/parser"
)

var errSinkDisabled = errors.New("sink disabled")

// Sink stores each validated reading to Redis (live state) and InfluxDB (time-series history).
// Redis writes are pipelined to reduce round-trips.
// InfluxDB writes are non-blocking and batched internally by the SDK.
type Sink struct {
	redis         *redis.Client
	influxClient  influxdb2.Client
	influxWrite   influxapi.WriteAPI  // non-blocking, batching write API
	influxOrg     string
	influxBucket  string
	influxURL     string
	influxToken   string
	influxMu      sync.Mutex
	redisTTL      time.Duration
	redisKeyPrefx string
	// errorsDone is closed when the error-drain goroutine exits.
	errorsDone chan struct{}
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

	// ── Redis ──────────────────────────────────────────────────────────────────
	redisAddr := env("REDIS_ADDR", "127.0.0.1:6380")
	redisDB := envInt("REDIS_DB", 0)
	redisTTL := time.Duration(envInt("REDIS_TTL_SECONDS", 120)) * time.Second
	keyPrefix := env("REDIS_KEY_PREFIX", "pmu")

	// Pool sizing: default 20 connections for throughput (env-tunable).
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

	// ── InfluxDB ───────────────────────────────────────────────────────────────
	influxURL := env("INFLUX_URL", "http://127.0.0.1:8087")
	influxToken := env("INFLUX_TOKEN", "my-super-secret-token")
	influxOrg := env("INFLUX_ORG", "pdc-org")
	influxBucket := env("INFLUX_BUCKET", "synchrophasor")

	// Batch size and flush interval are the key throughput knobs.
	// At 50fps × 10 PMUs = 500 points/s; a batch of 500 flushes once/sec.
	batchSize := uint(envInt("INFLUX_BATCH_SIZE", 500))
	flushIntervalMs := uint(envInt("INFLUX_FLUSH_INTERVAL_MS", 1000))
	retryCount := uint(envInt("INFLUX_RETRY_COUNT", 5))
	retryBufferLimit := uint(envInt("INFLUX_RETRY_BUFFER_LIMIT", 50000))

	if batchSize < 1 {
		batchSize = 500
	}
	if flushIntervalMs < 100 {
		flushIntervalMs = 100
	}

	opts := influxdb2.DefaultOptions().
		SetBatchSize(batchSize).
		SetFlushInterval(flushIntervalMs).
		SetMaxRetries(retryCount).
		SetRetryBufferLimit(retryBufferLimit).
		SetMaxRetryTime(30000).   // 30 s max retry window
		SetRetryInterval(2000).   // start at 2 s
		SetExponentialBase(2).
		SetHTTPRequestTimeout(10000) // 10 s per write request

	influxClient := influxdb2.NewClientWithOptions(influxURL, influxToken, opts)

	healthCtx, healthCancel := context.WithTimeout(ctx, 10*time.Second)
	defer healthCancel()
	if ok, err := influxClient.Health(healthCtx); err != nil {
		redisClient.Close()
		return nil, fmt.Errorf("influx health failed (%s): %w", influxURL, err)
	} else if ok.Status != "pass" {
		redisClient.Close()
		return nil, fmt.Errorf("influx not healthy (%s): %s", influxURL, ok.Status)
	}

	writeAPI := influxClient.WriteAPI(influxOrg, influxBucket)

	s := &Sink{
		redis:         redisClient,
		influxClient:  influxClient,
		influxWrite:   writeAPI,
		influxOrg:     influxOrg,
		influxBucket:  influxBucket,
		influxURL:     influxURL,
		influxToken:   influxToken,
		redisTTL:      redisTTL,
		redisKeyPrefx: keyPrefix,
		errorsDone:    make(chan struct{}),
	}

	// Drain the non-blocking write API error channel in the background.
	go s.drainInfluxErrors(ctx)

	log.Printf("sink ready: redis=%s influx=%s batch_size=%d flush_interval=%dms",
		redisAddr, influxURL, batchSize, flushIntervalMs)

	return s, nil
}

// drainInfluxErrors logs errors from the non-blocking write API and updates metrics.
func (s *Sink) drainInfluxErrors(ctx context.Context) {
	defer close(s.errorsDone)
	errorsCh := s.influxWrite.Errors()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-errorsCh:
			if !ok {
				return
			}
			if err != nil {
				log.Printf("[influx] async write error: %v", err)
			}
		}
	}
}

func (s *Sink) keyLatest(pmu string) string {
	return fmt.Sprintf("%s:%s:latest", s.redisKeyPrefx, pmu)
}

func (s *Sink) keyTimeline(pmu string) string {
	return fmt.Sprintf("%s:%s:timeline", s.redisKeyPrefx, pmu)
}

func (s *Sink) toInfluxPoint(r parser.Reading) *write.Point {
	return write.NewPoint(
		"pmu_readings",
		map[string]string{
			"pmu":    r.PMUName,
			"idcode": fmt.Sprintf("%d", r.IDCode),
		},
		map[string]interface{}{
			// Raw phasor components
			"va_r": r.VA.Real,
			"va_i": r.VA.Imag,
			"vb_r": r.VB.Real,
			"vb_i": r.VB.Imag,
			"vc_r": r.VC.Real,
			"vc_i": r.VC.Imag,
			"ia_r": r.IA.Real,
			"ia_i": r.IA.Imag,
			// Derived phasor metrics
			"va_mag":       r.VA.Magnitude,
			"va_phase_deg": r.VA.PhaseDegrees,
			"vb_mag":       r.VB.Magnitude,
			"vb_phase_deg": r.VB.PhaseDegrees,
			"vc_mag":       r.VC.Magnitude,
			"vc_phase_deg": r.VC.PhaseDegrees,
			"ia_mag":       r.IA.Magnitude,
			"ia_phase_deg": r.IA.PhaseDegrees,
			// Voltage analysis
			"voltage_imbalance": r.VoltageImbalancePercent,
			"vab_phase_diff":    r.VAB_PhaseAngleDifference,
			"vbc_phase_diff":    r.VBC_PhaseAngleDifference,
			"vca_phase_diff":    r.VCA_PhaseAngleDifference,
			// Frequency
			"frequency":     r.Frequency,
			"frequency_dev": r.FrequencyDeviation,
			"rocof":         r.ROCOF,
			// Power
			"mw":           r.MW,
			"mvar":         r.MVAR,
			"mva":          r.MVA,
			"power_factor": r.PowerFactor,
			"power_real":   r.TotalPowerReal,
			"power_imag":   r.TotalPowerImag,
			// Status
			"digital":      int64(r.Digital),
			"stat":         int64(r.Stat),
			"crc_valid":    r.ChecksumValid,
			"time_quality": int64(r.TimeQuality),
		},
		r.Timestamp,
	)
}

// Store writes one reading to both Redis and InfluxDB.
//
// Redis:
//   - :latest holds the full JSON Reading (shared live state for dashboard / multi-instance)
//   - :timeline holds compact points for a short rolling window (size-bounded)
//
// InfluxDB writes are queued to the non-blocking batching API.
// Returns an error only if the Redis pipeline fails — InfluxDB errors are reported
// asynchronously through the drainInfluxErrors goroutine.
func (s *Sink) Store(ctx context.Context, r parser.Reading) error {
	if s == nil {
		return nil
	}

	latestPayload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal reading for redis: %w", err)
	}

	// Compact timeline member — enough for quick charts without huge sorted-set growth.
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

	s.influxWrite.WritePoint(s.toInfluxPoint(r))
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
// Used to hydrate the live dashboard after restart / for multi-instance shared state.
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
				// Older compact timeline-style payloads are not full Readings — skip.
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
	// expected: <prefix>:<pmu>:latest
	trim := strings.TrimPrefix(key, prefix+":")
	trim = strings.TrimSuffix(trim, ":latest")
	return strings.TrimSpace(trim)
}

// Flush forces the InfluxDB batch to be written immediately.
// Call this before shutdown.
func (s *Sink) Flush() {
	if s == nil || s.influxWrite == nil {
		return
	}
	s.influxWrite.Flush()
}

func (s *Sink) Close() {
	if s == nil {
		return
	}
	// Flush pending InfluxDB writes before closing.
	if s.influxWrite != nil {
		s.influxWrite.Flush()
	}
	if s.influxClient != nil {
		s.influxClient.Close()
	}
	if s.redis != nil {
		_ = s.redis.Close()
	}
	// Wait for error drain goroutine to finish.
	if s.errorsDone != nil {
		<-s.errorsDone
	}
}
