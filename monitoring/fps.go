package monitoring

import (
	"fmt"
	"math"
	"time"

	"pdc/parser"
)

func cfgRateFPS(dataRate int16) float64 {
	if dataRate > 0 {
		return float64(dataRate)
	}
	if dataRate < 0 {
		return 1.0 / float64(-dataRate)
	}
	return 0
}

func profileRateFPS(pmu string) float64 {
	prof, ok := parser.GetProfile(pmu)
	if !ok {
		return 0
	}
	return cfgRateFPS(prof.DataRate)
}

// snapReportedFPS maps receive-jitter around CFG DATA_RATE back to the advertised
// rate so the dashboard does not look like a fault when no frames were lost.
func snapReportedFPS(measured, cfgRate float64) float64 {
	if cfgRate <= 0 || measured <= 0 || math.IsNaN(measured) {
		return measured
	}
	tol := math.Max(3, cfgRate*0.05) // ±3 fps or ±5%
	if math.Abs(measured-cfgRate) <= tol {
		return cfgRate
	}
	return measured
}

func (st *pmuRuntime) noteUniqueStampFPS(recvNow, meas time.Time, soc, fracCount uint32, cfgRate float64, recvGap time.Duration) {
	stampKey := fmt.Sprintf("%d:%d", soc, fracCount)
	newStamp := stampKey != st.lastFPSKey
	if newStamp {
		st.lastFPSKey = stampKey
	}

	restart := st.fpsMeasStart.IsZero() || recvGap > 750*time.Millisecond
	if !meas.IsZero() && !st.fpsMeasStart.IsZero() && meas.Before(st.fpsMeasStart) {
		restart = true
	}
	if restart {
		st.fpsWindowStart = recvNow
		if !meas.IsZero() {
			st.fpsMeasStart = meas
		} else {
			st.fpsMeasStart = recvNow
		}
		if newStamp {
			st.fpsWindowCount = 1
		} else {
			st.fpsWindowCount = 0
		}
		return
	}
	if !newStamp {
		return
	}
	st.fpsWindowCount++

	end := meas
	start := st.fpsMeasStart
	if end.IsZero() {
		end = recvNow
		start = st.fpsWindowStart
	}
	elapsed := end.Sub(start)
	if elapsed < time.Second || st.fpsWindowCount < 2 {
		return
	}

	instant := float64(st.fpsWindowCount-1) / elapsed.Seconds()
	instant = snapReportedFPS(instant, cfgRate)
	if st.approxFPS <= 0 || (cfgRate > 0 && instant == cfgRate) {
		st.approxFPS = instant
	} else {
		st.approxFPS = snapReportedFPS(st.approxFPS*0.35+instant*0.65, cfgRate)
	}

	st.fpsWindowStart = recvNow
	st.fpsMeasStart = end
	st.fpsWindowCount = 1
}
