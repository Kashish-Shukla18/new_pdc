package parser

import "testing"

func TestClassifyPhasorName_Aliases(t *testing.T) {
	cases := map[string]string{
		"V1":              "va",
		"I1":              "ia",
		"VA":              "va",
		"PHASOR CH 1:V1":  "va",
		"PHASOR CH 2:I1":  "ia",
		"CH1:VAN":         "va",
		"IB":              "ib",
		"PHASE A V":       "va",
		"unknown":         "",
	}
	for in, want := range cases {
		if got := ClassifyPhasorName(in); got != want {
			t.Fatalf("ClassifyPhasorName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMapPhasorsToStandard_V1I1(t *testing.T) {
	names := []string{"PHASOR CH 1:V1", "PHASOR CH 2:I1"}
	phasors := []Phasor{
		{Magnitude: 110, PhaseDegrees: 1},
		{Magnitude: 5, PhaseDegrees: -30},
	}
	va, vb, vc, ia := mapPhasorsToStandard(names, phasors)
	if va.Magnitude != 110 || ia.Magnitude != 5 {
		t.Fatalf("va=%v ia=%v vb=%v vc=%v", va, ia, vb, vc)
	}
	if vb.Magnitude != 0 || vc.Magnitude != 0 {
		t.Fatalf("unexpected vb/vc: %v %v", vb, vc)
	}
}

func TestResolveNominalHz_FloatMismatch(t *testing.T) {
	// CFG says 50 Hz, absolute float FREQ sits at 60 — snap for Δf.
	got := ResolveNominalHz(60.0, 50, true)
	if got != 60 {
		t.Fatalf("got %v want 60", got)
	}
	if d := 60.0 - got; d != 0 {
		t.Fatalf("Δf=%v want 0", d)
	}
	// Matching band stays.
	if got := ResolveNominalHz(50.02, 50, true); got != 50 {
		t.Fatalf("got %v want 50", got)
	}
	// Integer FREQ path never snaps (already relative to CFG FNOM).
	if got := ResolveNominalHz(60.01, 50, false); got != 50 {
		t.Fatalf("integer path got %v want 50", got)
	}
}
