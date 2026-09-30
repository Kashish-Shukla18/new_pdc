package monitoring

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestUIWorthy(t *testing.T) {
	if !uiWorthy("connect", "ok") {
		t.Fatal("connect ok should be UI-worthy")
	}
	if !uiWorthy("parse", "error") {
		t.Fatal("parse error should be UI-worthy")
	}
	if uiWorthy("stream", "ok") {
		t.Fatal("stream ok should not flood the UI")
	}
	if uiWorthy("summary", "ok") {
		t.Fatal("aligner summary should not flood the UI")
	}
}

func TestRateAllow(t *testing.T) {
	key := "test-rate-" + time.Now().Format(time.RFC3339Nano)
	if !rateAllow(key, time.Second) {
		t.Fatal("first allow")
	}
	if rateAllow(key, time.Second) {
		t.Fatal("second within window should deny")
	}
}

func TestLogEventStructured(t *testing.T) {
	var buf bytes.Buffer
	prev := eventLogger
	eventLogger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	defer func() { eventLogger = prev }()

	keyPMU := "slog-test-" + time.Now().Format("150405.000")
	if !logEvent(keyPMU, "PDC", "PMU", "connect", "ok", "dialled") {
		t.Fatal("expected logEvent to allow")
	}
	out := buf.String()
	if !strings.Contains(out, "pmu="+keyPMU) || !strings.Contains(out, "stage=connect") {
		t.Fatalf("expected structured attrs, got: %s", out)
	}
}
