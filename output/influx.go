package output

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/redis/go-redis/v9"

	"pdc/parser"
)

var errSinkDisabled = errors.New("sink disabled")

// Sink stores each validated snapshot to Redis (live) and InfluxDB (historical).
type Sink struct {
	redis         *redis.Client
	influxClient  influxdb2.Client
	influxWrite   api.WriteAPIBlocking
	influxOrg     string
	influxBucket  string
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

// NewSinkFromEnv initializes clients using docker-compose defaults.
func NewSinkFromEnv(ctx context.Context) (*Sink, error) {
	if strings.EqualFold(strings.TrimSpace(env("ENABLE_SINK", "true")), "false") {
		return nil, errSinkDisabled
	}

	redisAddr := env("REDIS_ADDR", "127.0.0.1:6380")
	redisDB := envInt("REDIS_DB", 0)
	redisTTL := time.Duration(envInt("REDIS_TTL_SECONDS", 60)) * time.Second
	keyPrefix := env("REDIS_KEY_PREFIX", "pmu")

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   redisDB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed (%s): %w", redisAddr, err)
	}

	influxURL := env("INFLUX_URL", "http://127.0.0.1:8087")
	influxToken := env("INFLUX_TOKEN", "my-super-secret-token")
	influxOrg := env("INFLUX_ORG", "pdc-org")
	influxBucket := env("INFLUX_BUCKET", "synchrophasor")
	influxClient := influxdb2.NewClient(influxURL, influxToken)

	if ok, err := influxClient.Health(ctx); err != nil {
		redisClient.Close()
		return nil, fmt.Errorf("influx health failed (%s): %w", influxURL, err)
	} else if ok.Status != "pass" {
		redisClient.Close()
		return nil, fmt.Errorf("influx not healthy (%s): %s", influxURL, ok.Status)
	}

	return &Sink{
		redis:         redisClient,
		influxClient:  influxClient,
		influxWrite:   influxClient.WriteAPIBlocking(influxOrg, influxBucket),
		influxOrg:     influxOrg,
		influxBucket:  influxBucket,
		redisTTL:      redisTTL,
		redisKeyPrefx: keyPrefix,
	}, nil
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
func (s *Sink) Store(ctx context.Context, r parser.Reading) error {
	if s == nil {
		return nil
	}

	jsonLine := fmt.Sprintf(
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
	if err := s.redis.ZAdd(ctx, s.keyTimeline(r.PMUName), redis.Z{
		Score:  ms,
		Member: jsonLine,
	}).Err(); err != nil {
		return fmt.Errorf("redis zadd: %w", err)
	}

	if err := s.redis.Expire(ctx, s.keyTimeline(r.PMUName), s.redisTTL).Err(); err != nil {
		return fmt.Errorf("redis expire timeline: %w", err)
	}

	if err := s.redis.Set(ctx, s.keyLatest(r.PMUName), jsonLine, s.redisTTL).Err(); err != nil {
		return fmt.Errorf("redis set latest: %w", err)
	}

	if err := s.influxWrite.WritePoint(ctx, s.toInfluxPoint(r)); err != nil {
		return fmt.Errorf("influx write: %w", err)
	}

	return nil
}

func (s *Sink) Close() {
	if s == nil {
		return
	}
	if s.redis != nil {
		_ = s.redis.Close()
	}
	if s.influxClient != nil {
		s.influxClient.Close()
	}
}
