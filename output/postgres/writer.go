// Package postgres is the TimescaleDB history writer (bounded queue + COPY).
//
// Live path must only call Enqueue* (non-blocking). DB slowness drops frames
// and increments metrics — it must never stall receivers or the aligner.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pdc/parser"
	"pdc/store"
)

// ErrDisabled means ENABLE_HISTORY / ENABLE_SINK is off.
var ErrDisabled = errors.New("history writer disabled")

var frameCopyColumns = []string{
	"time", "received_at", "pmu_id", "cfg_id",
	"quality_ok", "reject_reason",
	"idcode", "stat", "time_quality",
	"freq", "freq_dev", "rocof",
	"va_mag", "va_ang", "vb_mag", "vb_ang",
	"vc_mag", "vc_ang", "ia_mag", "ia_ang",
	"phasor_mag", "phasor_ang", "analogs", "digitals",
}

// FrameRow is one pmu_frame insert.
type FrameRow struct {
	Time         time.Time
	ReceivedAt   time.Time
	PMUID        string
	CfgID        int64 // 0 → SQL NULL
	QualityOK    bool
	RejectReason string
	IDCode       int32
	Stat         int32
	TimeQuality  int32
	Freq         float64
	FreqDev      float64
	ROCOF        float64
	VAMag        float64
	VAAng        float64
	VBMag        float64
	VBAng        float64
	VCMag        float64
	VCAng        float64
	IAMag        float64
	IAAng        float64
	PhasorMag    []float64
	PhasorAng    []float64
	Analogs      []float64
	Digitals     []int32
}

// EventRow is one pmu_event insert.
type EventRow struct {
	Time   time.Time
	PMUID  string
	Type   string
	Detail []byte // JSON object; nil → {}
}

// CfgVersion is a layout snapshot for pmu_cfg_version.
type CfgVersion struct {
	PMUID      string
	LayoutHash string
	Station    string
	IDCode     int32
	DataRate   int32
	TimeBase   int32
	Channels   []byte // JSON; nil → []
	RawCFG     []byte
}

func (r FrameRow) values() []any {
	var cfgID any
	if r.CfgID > 0 {
		cfgID = r.CfgID
	}
	var reject any
	if !r.QualityOK && r.RejectReason != "" {
		reject = r.RejectReason
	}
	return []any{
		r.Time, r.ReceivedAt, r.PMUID, cfgID,
		r.QualityOK, reject,
		r.IDCode, r.Stat, r.TimeQuality,
		r.Freq, r.FreqDev, r.ROCOF,
		r.VAMag, r.VAAng, r.VBMag, r.VBAng,
		r.VCMag, r.VCAng, r.IAMag, r.IAAng,
		r.PhasorMag, r.PhasorAng, r.Analogs, r.Digitals,
	}
}

// FrameFromReading maps a parsed DATA frame into a history row.
func FrameFromReading(r parser.Reading, qualityOK bool, rejectReason string, cfgID int64) FrameRow {
	ts := r.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	recv := time.Now().UTC()
	if r.Trace.ReceivedAtUnixNano > 0 {
		recv = time.Unix(0, r.Trace.ReceivedAtUnixNano).UTC()
	}

	phasorMag := make([]float64, len(r.Phasors))
	phasorAng := make([]float64, len(r.Phasors))
	for i, p := range r.Phasors {
		phasorMag[i] = float64(p.Phasor.Magnitude)
		phasorAng[i] = float64(p.Phasor.PhaseDegrees)
	}
	analogs := make([]float64, len(r.Analogs))
	for i, a := range r.Analogs {
		analogs[i] = float64(a.Value)
	}
	digitals := make([]int32, len(r.Digitals))
	for i, d := range r.Digitals {
		digitals[i] = int32(d)
	}

	return FrameRow{
		Time:         ts,
		ReceivedAt:   recv,
		PMUID:        r.PMUName,
		CfgID:        cfgID,
		QualityOK:    qualityOK,
		RejectReason: rejectReason,
		IDCode:       int32(r.IDCode),
		Stat:         int32(r.Stat),
		TimeQuality:  int32(r.TimeQuality),
		Freq:         float64(r.Frequency),
		FreqDev:      float64(r.FrequencyDeviation),
		ROCOF:        float64(r.ROCOF),
		VAMag:        float64(r.VA.Magnitude),
		VAAng:        float64(r.VA.PhaseDegrees),
		VBMag:        float64(r.VB.Magnitude),
		VBAng:        float64(r.VB.PhaseDegrees),
		VCMag:        float64(r.VC.Magnitude),
		VCAng:        float64(r.VC.PhaseDegrees),
		IAMag:        float64(r.IA.Magnitude),
		IAAng:        float64(r.IA.PhaseDegrees),
		PhasorMag:    phasorMag,
		PhasorAng:    phasorAng,
		Analogs:      analogs,
		Digitals:     digitals,
	}
}

// Stats are lock-free counters for ops/dashboard later.
type Stats struct {
	Enqueued    uint64
	Dropped     uint64
	Written     uint64
	CopyErrors  uint64
	EventsIn    uint64
	EventsDrop  uint64
	EventsWrote uint64
	QueueDepth  int
	Ready       bool
}

// Writer batches frames with COPY and inserts events/cfg on a dedicated pool.
type Writer struct {
	batchSize  int
	flushEvery time.Duration
	insertTO   time.Duration
	reconnect  time.Duration

	frames chan FrameRow
	events chan EventRow

	mu   sync.RWMutex
	pool *pgxpool.Pool

	cancel context.CancelFunc
	done   chan struct{}

	enqueued    atomic.Uint64
	dropped     atomic.Uint64
	written     atomic.Uint64
	copyErrors  atomic.Uint64
	eventsIn    atomic.Uint64
	eventsDrop  atomic.Uint64
	eventsWrote atomic.Uint64
}

func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envDurationMS(key string, fallbackMS int) time.Duration {
	ms := envInt(key, fallbackMS)
	if ms < 50 {
		ms = 50
	}
	return time.Duration(ms) * time.Millisecond
}

func historyEnabled() bool {
	// Prefer ENABLE_HISTORY; fall back to legacy ENABLE_SINK.
	if v := strings.TrimSpace(os.Getenv("ENABLE_HISTORY")); v != "" {
		return envBool("ENABLE_HISTORY", false)
	}
	if v := strings.TrimSpace(os.Getenv("ENABLE_SINK")); v != "" {
		return envBool("ENABLE_SINK", false)
	}
	return false
}

// NewWriterFromEnv starts the history writer when ENABLE_HISTORY/ENABLE_SINK is on.
// Always returns a Writer that reconnects if Postgres is down (never blocks the live path).
func NewWriterFromEnv(parent context.Context) (*Writer, error) {
	if !historyEnabled() {
		return nil, ErrDisabled
	}
	return NewWriter(parent, WriterConfig{})
}

// WriterConfig overrides env defaults (zero = use env/default).
type WriterConfig struct {
	QueueSize     int
	EventQueue    int
	BatchSize     int
	FlushEvery    time.Duration
	InsertTimeout time.Duration
	Reconnect     time.Duration
	DSN           string
}

// NewWriter constructs a writer (used by tests). Does not require ENABLE_HISTORY.
func NewWriter(parent context.Context, cfg WriterConfig) (*Writer, error) {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = envInt("HISTORY_QUEUE_SIZE", 8192)
	}
	if cfg.EventQueue <= 0 {
		cfg.EventQueue = envInt("HISTORY_EVENT_QUEUE_SIZE", 1024)
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = envInt("HISTORY_BATCH_SIZE", 500)
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = envDurationMS("HISTORY_FLUSH_INTERVAL_MS", 1000)
	}
	if cfg.InsertTimeout <= 0 {
		cfg.InsertTimeout = envDurationMS("HISTORY_INSERT_TIMEOUT_MS", 15000)
	}
	if cfg.Reconnect <= 0 {
		cfg.Reconnect = envDurationMS("HISTORY_RECONNECT_MS", 5000)
	}
	dsn := cfg.DSN
	if dsn == "" {
		dsn = store.DSN()
	}

	runCtx, cancel := context.WithCancel(parent)
	w := &Writer{
		batchSize:  cfg.BatchSize,
		flushEvery: cfg.FlushEvery,
		insertTO:   cfg.InsertTimeout,
		reconnect:  cfg.Reconnect,
		frames:     make(chan FrameRow, cfg.QueueSize),
		events:     make(chan EventRow, cfg.EventQueue),
		cancel:     cancel,
		done:       make(chan struct{}),
	}
	if err := w.tryConnect(dsn); err != nil {
		log.Printf("history writer: unavailable at startup: %v (will retry)", err)
	} else {
		log.Printf("history writer: connected batch=%d flush=%s queue=%d",
			w.batchSize, w.flushEvery, cfg.QueueSize)
	}
	go w.loop(runCtx, dsn)
	return w, nil
}

func (w *Writer) tryConnect(dsn string) error {
	w.mu.RLock()
	ok := w.pool != nil
	w.mu.RUnlock()
	if ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return err
	}
	pcfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return err
	}
	w.mu.Lock()
	if w.pool != nil {
		w.mu.Unlock()
		pool.Close()
		return nil
	}
	w.pool = pool
	w.mu.Unlock()
	return nil
}

func (w *Writer) getPool() *pgxpool.Pool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.pool
}

func (w *Writer) clearPool() {
	w.mu.Lock()
	if w.pool != nil {
		w.pool.Close()
		w.pool = nil
	}
	w.mu.Unlock()
}

// Ready reports a live DB pool.
func (w *Writer) Ready() bool {
	return w != nil && w.getPool() != nil
}

// StatsSnapshot returns counters for tests/ops.
func (w *Writer) StatsSnapshot() Stats {
	if w == nil {
		return Stats{}
	}
	return Stats{
		Enqueued:    w.enqueued.Load(),
		Dropped:     w.dropped.Load(),
		Written:     w.written.Load(),
		CopyErrors:  w.copyErrors.Load(),
		EventsIn:    w.eventsIn.Load(),
		EventsDrop:  w.eventsDrop.Load(),
		EventsWrote: w.eventsWrote.Load(),
		QueueDepth:  len(w.frames),
		Ready:       w.Ready(),
	}
}

// EnqueueFrame is non-blocking. On a full queue the frame is dropped.
func (w *Writer) EnqueueFrame(row FrameRow) {
	if w == nil {
		return
	}
	select {
	case w.frames <- row:
		w.enqueued.Add(1)
	default:
		w.dropped.Add(1)
	}
}

// EnqueueEvent is non-blocking.
func (w *Writer) EnqueueEvent(row EventRow) {
	if w == nil {
		return
	}
	if row.Time.IsZero() {
		row.Time = time.Now().UTC()
	}
	select {
	case w.events <- row:
		w.eventsIn.Add(1)
	default:
		w.eventsDrop.Add(1)
	}
}

// EnsureCfgVersion inserts or returns existing cfg_id (sync, timed). Safe for CFG path, not per-frame.
func (w *Writer) EnsureCfgVersion(ctx context.Context, v CfgVersion) (int64, error) {
	if w == nil {
		return 0, ErrDisabled
	}
	pool := w.getPool()
	if pool == nil {
		return 0, store.ErrUnavailable
	}
	channels := v.Channels
	if len(channels) == 0 {
		channels = []byte("[]")
	}
	cctx, cancel := context.WithTimeout(ctx, w.insertTO)
	defer cancel()
	var id int64
	err := pool.QueryRow(cctx, `
		INSERT INTO pmu_cfg_version (
			pmu_id, layout_hash, station, idcode, data_rate, time_base, channels, raw_cfg
		) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
		ON CONFLICT (pmu_id, layout_hash) DO UPDATE
			SET station = EXCLUDED.station,
			    idcode = EXCLUDED.idcode,
			    data_rate = EXCLUDED.data_rate,
			    time_base = EXCLUDED.time_base,
			    channels = EXCLUDED.channels,
			    raw_cfg = COALESCE(EXCLUDED.raw_cfg, pmu_cfg_version.raw_cfg)
		RETURNING cfg_id`,
		v.PMUID, v.LayoutHash, v.Station, v.IDCode, v.DataRate, v.TimeBase, channels, v.RawCFG,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (w *Writer) loop(ctx context.Context, dsn string) {
	defer close(w.done)
	t := time.NewTicker(w.flushEvery)
	defer t.Stop()
	reconnect := time.NewTicker(w.reconnect)
	defer reconnect.Stop()

	buf := make([]FrameRow, 0, w.batchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		batch := buf
		buf = make([]FrameRow, 0, w.batchSize)
		w.flushFrames(batch)
	}

	for {
		select {
		case <-ctx.Done():
			// Drain remaining quickly.
			for {
				select {
				case row := <-w.frames:
					buf = append(buf, row)
					if len(buf) >= w.batchSize {
						flush()
					}
				default:
					flush()
					w.drainEvents()
					return
				}
			}
		case <-reconnect.C:
			if w.getPool() == nil {
				if err := w.tryConnect(dsn); err != nil {
					log.Printf("history writer: reconnect failed: %v", err)
				} else {
					log.Printf("history writer: reconnected")
				}
			}
		case <-t.C:
			flush()
			w.drainEvents()
		case row := <-w.frames:
			buf = append(buf, row)
			if len(buf) >= w.batchSize {
				flush()
			}
		case ev := <-w.events:
			w.writeEvent(ev)
		}
	}
}

func (w *Writer) drainEvents() {
	for {
		select {
		case ev := <-w.events:
			w.writeEvent(ev)
		default:
			return
		}
	}
}

func (w *Writer) flushFrames(batch []FrameRow) {
	pool := w.getPool()
	if pool == nil {
		w.dropped.Add(uint64(len(batch)))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.insertTO)
	defer cancel()
	_, err := pool.CopyFrom(ctx, pgx.Identifier{"pmu_frame"}, frameCopyColumns,
		pgx.CopyFromSlice(len(batch), func(i int) ([]any, error) {
			return batch[i].values(), nil
		}),
	)
	if err != nil {
		w.copyErrors.Add(1)
		log.Printf("history writer: copy %d rows: %v", len(batch), err)
		// Drop pool on connection-class errors so reconnect can heal.
		if strings.Contains(err.Error(), "conn") || strings.Contains(err.Error(), "closed") {
			w.clearPool()
		}
		return
	}
	w.written.Add(uint64(len(batch)))
}

func (w *Writer) writeEvent(ev EventRow) {
	pool := w.getPool()
	if pool == nil {
		w.eventsDrop.Add(1)
		return
	}
	detail := ev.Detail
	if len(detail) == 0 {
		detail = []byte("{}")
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.insertTO)
	defer cancel()
	_, err := pool.Exec(ctx, `
		INSERT INTO pmu_event (time, pmu_id, type, detail)
		VALUES ($1, $2, $3, $4::jsonb)`,
		ev.Time, ev.PMUID, ev.Type, detail,
	)
	if err != nil {
		w.copyErrors.Add(1)
		log.Printf("history writer: event insert: %v", err)
		return
	}
	w.eventsWrote.Add(1)
}

// Flush waits briefly for the queue to drain via the loop ticker — best-effort for tests.
func (w *Writer) Flush() {
	if w == nil {
		return
	}
	deadline := time.Now().Add(w.flushEvery + w.insertTO + 500*time.Millisecond)
	for time.Now().Before(deadline) {
		if len(w.frames) == 0 && len(w.events) == 0 {
			time.Sleep(w.flushEvery + 50*time.Millisecond)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Close stops the writer and closes the pool.
func (w *Writer) Close() {
	if w == nil {
		return
	}
	w.cancel()
	<-w.done
	w.clearPool()
}

// String helps debug logs.
func (s Stats) String() string {
	return fmt.Sprintf("enqueued=%d dropped=%d written=%d copy_err=%d events_in=%d events_drop=%d events_wrote=%d q=%d ready=%v",
		s.Enqueued, s.Dropped, s.Written, s.CopyErrors, s.EventsIn, s.EventsDrop, s.EventsWrote, s.QueueDepth, s.Ready)
}
