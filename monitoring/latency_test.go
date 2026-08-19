package monitoring

import (
	"strings"
	"testing"
	"time"
)

func TestPercentileSorted(t *testing.T) {
	got := percentileSorted([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0.95)
	if got != 10 {
		t.Fatalf("p95=%v want 10", got)
	}
	if percentileSorted(nil, 0.95) != 0 {
		t.Fatal("empty should be 0")
	}
	if percentileSorted([]float64{4}, 0.95) != 4 {
		t.Fatal("single sample")
	}
}

func TestObserveStageAndSnapshot(t *testing.T) {
	ObserveStage("TEST-PMU", StageParse, 2*time.Millisecond)
	snap := SnapshotPipelineLatency()
	var parse StageLatency
	for _, st := range snap.Stages {
		if st.ID == StageParse {
			parse = st
			break
		}
	}
	if parse.Count < 1 {
		t.Fatalf("expected parse samples, got %+v", parse)
	}
	hops := LastHopsForPMU("TEST-PMU")
	if hops[StageParse] <= 0 {
		t.Fatalf("last hops missing parse: %v", hops)
	}
}

func TestFormatLatencySummarySeparatesConnection(t *testing.T) {
	ObserveStage("TEST-PMU", StageHandshakeTotal, 3300*time.Millisecond)
	ObserveStage("TEST-PMU", StageClockSkewPMU, 620*time.Millisecond)
	ObserveStage("TEST-PMU", StageTCPWait, 22*time.Millisecond)
	ObserveStage("TEST-PMU", StageParse, 1*time.Millisecond)
	s := FormatLatencySummary()
	if !strings.Contains(s, "connection (one-time") {
		t.Fatalf("expected one-time connection line, got:\n%s", s)
	}
	if !strings.Contains(s, "PMU clock skew") {
		t.Fatalf("expected clock skew line, got:\n%s", s)
	}
	if !strings.Contains(s, "wait-for-PMU") {
		t.Fatalf("expected wait-for-PMU line, got:\n%s", s)
	}
	if !strings.Contains(s, "processing slowest=") {
		t.Fatalf("expected processing line, got:\n%s", s)
	}
}
