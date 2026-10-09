// sink.go — thin adapter over the Timescale history writer.
//
// Not wired into the live HandleFrame path yet (phases 3–4). Construct with
// NewSinkFromEnv when ENABLE_HISTORY=true.
package output

import (
	"context"
	"errors"
	"log"

	"pdc/output/postgres"
	"pdc/parser"
)

// Sink stores readings/events to Postgres/TimescaleDB history.
type Sink struct {
	w *postgres.Writer
}

// NewSinkFromEnv returns a sink when history is enabled.
// If disabled, returns (nil, nil). If enabled but DB is down, still returns a
// sink that reconnects in the background.
func NewSinkFromEnv(ctx context.Context) (*Sink, error) {
	w, err := postgres.NewWriterFromEnv(ctx)
	if err != nil {
		if errors.Is(err, postgres.ErrDisabled) {
			return nil, nil
		}
		return nil, err
	}
	log.Printf("sink ready: history writer on (%s)", w.StatsSnapshot())
	return &Sink{w: w}, nil
}

// Writer exposes the underlying history writer (cfg/events/frames).
func (s *Sink) Writer() *postgres.Writer {
	if s == nil {
		return nil
	}
	return s.w
}

// Store enqueues one reading as quality_ok=true (legacy helper). Prefer Writer().EnqueueFrame.
func (s *Sink) Store(_ context.Context, r parser.Reading) error {
	if s == nil || s.w == nil {
		return nil
	}
	s.w.EnqueueFrame(postgres.FrameFromReading(r, true, "", 0))
	return nil
}

// EnqueueFrame non-blocking frame enqueue.
func (s *Sink) EnqueueFrame(row postgres.FrameRow) {
	if s == nil || s.w == nil {
		return
	}
	s.w.EnqueueFrame(row)
}

func (s *Sink) Flush() {
	if s == nil || s.w == nil {
		return
	}
	s.w.Flush()
}

func (s *Sink) Close() {
	if s == nil || s.w == nil {
		return
	}
	s.w.Close()
}
