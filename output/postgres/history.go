package postgres

import (
	"context"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pdc/parser"
	"pdc/store"
)

var copyColumns = []string{
	"time", "entity_id", "idcode",
	"freq", "freq_dev", "rocof",
	"mw", "mvar", "mva", "power_factor",
	"va_mag", "va_ang", "vb_mag", "vb_ang",
	"vc_mag", "vc_ang", "ia_mag", "ia_ang",
	"stat", "digital", "crc_valid", "time_quality",
}

// Row is one history sample written to pmu_readings.
type Row struct {
	Time        time.Time
	EntityID    string
	IDCode      int32
	Freq        float64
	FreqDev     float64
	ROCOF       float64
	MW          float64
	MVAR        float64
	MVA         float64
	PowerFactor float64
	VAMag       float64
	VAAng       float64
	VBMag       float64
	VBAng       float64
	VCMag       float64
	VCAng       float64
	IAMag       float64
	IAAng       float64
	Stat        int32
	Digital     int32
	CRCValid    bool
	TimeQuality int32
}

func RowFromReading(r parser.Reading) Row {
	ts := r.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return Row{
		Time:        ts,
		EntityID:    r.PMUName,
		IDCode:      int32(r.IDCode),
		Freq:        float64(r.Frequency),
		FreqDev:     float64(r.FrequencyDeviation),
		ROCOF:       float64(r.ROCOF),
		MW:          float64(r.MW),
		MVAR:        float64(r.MVAR),
		MVA:         float64(r.MVA),
		PowerFactor: float64(r.PowerFactor),
		VAMag:       float64(r.VA.Magnitude),
		VAAng:       float64(r.VA.PhaseDegrees),
		VBMag:       float64(r.VB.Magnitude),
		VBAng:       float64(r.VB.PhaseDegrees),
		VCMag:       float64(r.VC.Magnitude),
		VCAng:       float64(r.VC.PhaseDegrees),
		IAMag:       float64(r.IA.Magnitude),
		IAAng:       float64(r.IA.PhaseDegrees),
		Stat:        int32(r.Stat),
		Digital:     int32(r.Digital),
		CRCValid:    r.ChecksumValid,
		TimeQuality: int32(r.TimeQuality),
	}
}

func (r Row) values() []any {
	return []any{
		r.Time, r.EntityID, r.IDCode,
		r.Freq, r.FreqDev, r.ROCOF,
		r.MW, r.MVAR, r.MVA, r.PowerFactor,
		r.VAMag, r.VAAng, r.VBMag, r.VBAng,
		r.VCMag, r.VCAng, r.IAMag, r.IAAng,
		r.Stat, r.Digital, r.CRCValid, r.TimeQuality,
	}
}

// History buffers readings and bulk-inserts them with COPY.
type History struct {
	pool       *pgxpool.Pool
	batchSize  int
	flushEvery time.Duration

	mu     sync.Mutex
	buf    []Row
	flushC chan struct{}
	done   chan struct{}
	stop   context.CancelFunc
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func NewHistoryFromEnv(parent context.Context) (*History, error) {
	batch := envInt("POSTGRES_BATCH_SIZE", 500)
	flushMs := envInt("POSTGRES_FLUSH_INTERVAL_MS", 1000)
	if flushMs < 100 {
		flushMs = 100
	}

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, store.DSN())
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	runCtx, stop := context.WithCancel(context.Background())
	h := &History{
		pool:       pool,
		batchSize:  batch,
		flushEvery: time.Duration(flushMs) * time.Millisecond,
		buf:        make([]Row, 0, batch),
		flushC:     make(chan struct{}, 1),
		done:       make(chan struct{}),
		stop:       stop,
	}
	go h.loop(runCtx)
	log.Printf("postgres history writer: batch=%d flush=%s", batch, h.flushEvery)
	return h, nil
}

func (h *History) Enqueue(row Row) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.buf = append(h.buf, row)
	full := len(h.buf) >= h.batchSize
	h.mu.Unlock()
	if full {
		select {
		case h.flushC <- struct{}{}:
		default:
		}
	}
}

func (h *History) loop(ctx context.Context) {
	defer close(h.done)
	t := time.NewTicker(h.flushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			h.flush()
			return
		case <-t.C:
			h.flush()
		case <-h.flushC:
			h.flush()
		}
	}
}

func (h *History) flush() {
	h.mu.Lock()
	if len(h.buf) == 0 {
		h.mu.Unlock()
		return
	}
	batch := h.buf
	h.buf = make([]Row, 0, h.batchSize)
	h.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := h.pool.CopyFrom(ctx, pgx.Identifier{"pmu_readings"}, copyColumns,
		pgx.CopyFromSlice(len(batch), func(i int) ([]any, error) {
			return batch[i].values(), nil
		}),
	)
	if err != nil {
		log.Printf("[postgres] copy %d rows: %v", len(batch), err)
	}
}

func (h *History) Flush() {
	if h == nil {
		return
	}
	h.flush()
}

func (h *History) Close() {
	if h == nil {
		return
	}
	h.stop()
	<-h.done
	h.pool.Close()
}
