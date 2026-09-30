package monitoring

// eventlog.go — structured ops logging (slog) with per-key rate limits.
//
// The old "conversation" ring fed the React alert list AND acted as a chatty
// log. Full replacement with slog alone would break Overview alerts, so we:
//   1. Always try slog (rate-limited for noisy OK heartbeats)
//   2. Keep a small in-memory ring + SSE only for UI-worthy events
//      (errors, rejects, connect/manager/startup — not stream counters)

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	eventLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	rateMu   sync.Mutex
	rateLast = map[string]time.Time{}

	// Noisy OK/info lines share this floor. Errors still rate-limit but shorter.
	rateOK    = 2 * time.Second
	rateError = 500 * time.Millisecond
)

func rateAllow(key string, min time.Duration) bool {
	rateMu.Lock()
	defer rateMu.Unlock()
	now := time.Now()
	if t, ok := rateLast[key]; ok && now.Sub(t) < min {
		return false
	}
	rateLast[key] = now
	return true
}

func eventSeverity(status string) slog.Level {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "error"), strings.Contains(s, "reject"), strings.Contains(s, "fail"):
		return slog.LevelError
	case strings.Contains(s, "warn"):
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func isErrorish(status string) bool {
	s := strings.ToLower(status)
	return strings.Contains(s, "error") ||
		strings.Contains(s, "reject") ||
		strings.Contains(s, "fail") ||
		strings.Contains(s, "warn")
}

// uiWorthy: should this appear in the dashboard alert feed?
func uiWorthy(stage, status string) bool {
	if isErrorish(status) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "startup", "manager", "connection", "connect", "handshake":
		return true
	case "stream", "summary":
		return false // heartbeats / aligner summaries → slog only
	case "parse", "quality":
		return false // only when errorish (caught above)
	default:
		return !strings.EqualFold(status, "ok") && !strings.EqualFold(status, "info")
	}
}

// logEvent writes one structured line. Returns false if rate-limited (caller
// should skip both slog and UI append for that sample).
func logEvent(pmu, from, to, stage, status, message string) bool {
	key := pmu + "|" + stage + "|" + strings.ToLower(status)
	min := rateOK
	if isErrorish(status) {
		min = rateError
	}
	if !rateAllow(key, min) {
		return false
	}
	level := eventSeverity(status)
	eventLogger.Log(nil, level, message,
		slog.String("pmu", pmu),
		slog.String("stage", stage),
		slog.String("status", status),
		slog.String("from", from),
		slog.String("to", to),
	)
	return true
}
