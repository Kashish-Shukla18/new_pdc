package monitoring

import (
	"math"
	"testing"
	"time"
)

func TestSnapReportedFPS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		measured, cfg, want float64
	}{
		{199.2, 200, 200},
		{204.1, 200, 200},
		{190, 200, 200},
		{206.8, 200, 200},
		{180, 200, 180},
		{160, 200, 160},
		{50, 0, 50},
	}
	for _, tc := range cases {
		got := snapReportedFPS(tc.measured, tc.cfg)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("snapReportedFPS(%v, %v)=%v want %v", tc.measured, tc.cfg, got, tc.want)
		}
	}
}

func TestNoteUniqueStampFPSUsesMeasurementTime(t *testing.T) {
	t.Parallel()
	var st pmuRuntime
	recv := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	meas := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	period := 5 * time.Millisecond

	st.noteUniqueStampFPS(recv, meas, 1, 0, 200, time.Hour)
	for i := 1; i <= 200; i++ {
		recv = recv.Add(6 * time.Millisecond) // slower wall clock (TCP burst/gap)
		meas = meas.Add(period)
		st.noteUniqueStampFPS(recv, meas, 1, uint32(i*5000), 200, 6*time.Millisecond)
	}
	if st.approxFPS != 200 {
		t.Fatalf("approxFPS=%v want 200 (measurement-time rate, not wall clock)", st.approxFPS)
	}
}
