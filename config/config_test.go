package config

import (
	"testing"
	"time"
)

func TestIdentityTCPAndUDP(t *testing.T) {
	tcp := PMUConfig{IP: "10.0.0.5", Port: 4712, Protocol: "tcp"}
	tcp.Normalize()
	if tcp.Name != "10.0.0.5:4712" {
		t.Fatalf("tcp identity=%q", tcp.Name)
	}
	udp := PMUConfig{IP: " 10.0.0.5 ", Port: 4712, Protocol: "udp"}
	udp.Normalize()
	if udp.Name != "udp:10.0.0.5:4712" {
		t.Fatalf("udp identity=%q", udp.Name)
	}
}

func TestDisplayLabelPrefersStation(t *testing.T) {
	p := PMUConfig{IP: "1.2.3.4", Port: 1, Station: "  FEEDER-1  "}
	p.Normalize()
	if p.DisplayLabel() != "FEEDER-1" {
		t.Fatalf("label=%q", p.DisplayLabel())
	}
	p.Station = ""
	if p.DisplayLabel() != "1.2.3.4:1" {
		t.Fatalf("fallback label=%q", p.DisplayLabel())
	}
}

func TestEndpointKeyUniqueness(t *testing.T) {
	a := PMUConfig{IP: "1.1.1.1", Port: 10, Protocol: "tcp"}
	b := PMUConfig{IP: "1.1.1.1", Port: 10, Protocol: "udp"}
	a.Normalize()
	b.Normalize()
	if a.EndpointKey() == b.EndpointKey() {
		t.Fatal("tcp and udp on same ip:port must be distinct endpoints")
	}
}

func TestLivenessTimeout(t *testing.T) {
	// 60 fps → period ~16.7ms → 3× < 1s → clamp to 1s
	if d := LivenessTimeout(60); d != time.Second {
		t.Fatalf("60fps liveness=%s want 1s", d)
	}
	// 1 fps → 3s
	if d := LivenessTimeout(1); d != 3*time.Second {
		t.Fatalf("1fps liveness=%s want 3s", d)
	}
	// unknown → default 20fps → 150ms → clamp 1s
	if d := LivenessTimeout(0); d != time.Second {
		t.Fatalf("default liveness=%s want 1s", d)
	}
	// negative rate: -5 → 0.2 fps → period 5s → 15s
	if d := LivenessTimeout(-5); d != 15*time.Second {
		t.Fatalf("neg rate liveness=%s want 15s", d)
	}
}

func TestReinterpretISTToUTC(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	// Device stamped "15:14:00" as if UTC.
	fake := time.Date(2026, 10, 1, 15, 14, 0, 0, time.UTC)
	got := ReinterpretAsTimezone(fake, ist)
	want := time.Date(2026, 10, 1, 9, 44, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
	if same := ReinterpretAsTimezone(fake, time.UTC); !same.Equal(fake) {
		t.Fatalf("UTC no-op failed: %s", same)
	}
}

func TestEffectiveTimestampTZ(t *testing.T) {
	t.Setenv("PMU_TIMESTAMP_TZ", "Asia/Kolkata")
	field := PMUConfig{IP: "172.26.82.247", Port: 4712}
	field.Normalize()
	if field.EffectiveTimestampTZ() != "Asia/Kolkata" {
		t.Fatalf("field=%q", field.EffectiveTimestampTZ())
	}
	lab := PMUConfig{IP: "127.0.0.1", Port: 4712}
	lab.Normalize()
	if lab.EffectiveTimestampTZ() != "UTC" {
		t.Fatalf("loopback default=%q want UTC", lab.EffectiveTimestampTZ())
	}
	lab.TimestampTZ = "IST"
	lab.Normalize()
	if lab.EffectiveTimestampTZ() != "Asia/Kolkata" {
		t.Fatalf("explicit IST=%q", lab.EffectiveTimestampTZ())
	}
	field.TimestampTZ = "UTC"
	field.Normalize()
	if field.EffectiveTimestampTZ() != "UTC" {
		t.Fatalf("explicit UTC=%q", field.EffectiveTimestampTZ())
	}
}

func TestDefaultTimestampTZLocalUsesHost(t *testing.T) {
	t.Setenv("PMU_TIMESTAMP_TZ", "local")
	got := DefaultTimestampTZ()
	if got == "" {
		t.Fatal("empty host tz")
	}
	if _, err := LoadLocation(got); err != nil {
		t.Fatalf("host tz %q not loadable: %v", got, err)
	}
}

func TestFixedOffsetName(t *testing.T) {
	if got := fixedOffsetName(19800); got != "UTC+05:30" {
		t.Fatalf("got %q", got)
	}
	loc, err := LoadLocation("UTC+05:30")
	if err != nil {
		t.Fatal(err)
	}
	_, off := time.Now().In(loc).Zone()
	if off != 19800 {
		t.Fatalf("offset=%d", off)
	}
}

func TestCorrectMeasurementTimeOnConfig(t *testing.T) {
	t.Setenv("PMU_TIMESTAMP_TZ", "Asia/Kolkata")
	p := PMUConfig{IP: "172.26.82.247", Port: 4712}
	p.Normalize()
	in := time.Date(2026, 10, 1, 15, 14, 0, 0, time.UTC)
	got := p.CorrectMeasurementTime(in)
	want := time.Date(2026, 10, 1, 9, 44, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}
