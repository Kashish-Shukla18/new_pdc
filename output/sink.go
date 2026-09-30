// sink.go — PARKED storage writer (Postgres/Timescale history only).
//
// Not wired into main.go today. When you turn storage back on, NewSinkFromEnv
// enqueues each reading for TimescaleDB history.
package output

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"pdc/output/postgres"
	"pdc/parser"
)

var errSinkDisabled = errors.New("sink disabled")

// Sink stores each validated reading to Postgres/TimescaleDB (history).
// Writes are buffered and flushed off the ingest path.
type Sink struct {
	history *postgres.History
}

func env(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

// NewSinkFromEnv initialises Sink clients using environment variables / docker-compose defaults.
func NewSinkFromEnv(ctx context.Context) (*Sink, error) {
	if strings.EqualFold(strings.TrimSpace(env("ENABLE_SINK", "true")), "false") {
		return nil, errSinkDisabled
	}

	history, err := postgres.NewHistoryFromEnv(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres history: %w", err)
	}

	log.Printf("sink ready: postgres history on")
	return &Sink{history: history}, nil
}

// Store enqueues one reading for Postgres (non-blocking).
func (s *Sink) Store(_ context.Context, r parser.Reading) error {
	if s == nil {
		return nil
	}
	s.history.Enqueue(postgres.RowFromReading(r))
	return nil
}

func (s *Sink) Flush() {
	if s == nil {
		return
	}
	s.history.Flush()
}

func (s *Sink) Close() {
	if s == nil {
		return
	}
	s.history.Close()
}
