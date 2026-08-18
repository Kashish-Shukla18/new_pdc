package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfilePersistAndHydrate(t *testing.T) {
	dir := t.TempDir()
	ConfigureProfileStore(dir)
	t.Cleanup(func() { ConfigureProfileStore("") })

	prof := Profile{
		Station:  "STATION-A",
		IDCode:   42,
		Phnmr:    4,
		Annmr:    1,
		Dgnmr:    1,
		FnomHz:   50,
		DataRate: 100,
		Polar:    false,
		PhFloat:  true,
		Channels: []string{"VA", "VB", "VC", "IA"},
		HeaderText: "unit test header",
	}

	SetProfile("PMU-Alpha", prof)

	path := filepath.Join(dir, "PMU-Alpha.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected profile file %s: %v", path, err)
	}

	// Clear in-memory registry entry by overwriting with empty via Store after disable... 
	// Simulate restart: disable store writes, wipe by loading into fresh map via Load on empty registry.
	// Easiest: store a different profile then load from disk into registry.
	profileRegistry.Delete("PMU-Alpha")
	if _, ok := GetProfile("PMU-Alpha"); ok {
		t.Fatal("expected profile cleared from memory")
	}

	ConfigureProfileStore("") // load without re-persisting
	n, err := LoadPersistedProfiles(dir)
	if err != nil {
		t.Fatalf("LoadPersistedProfiles: %v", err)
	}
	if n != 1 {
		t.Fatalf("hydrated=%d want 1", n)
	}

	got, ok := GetProfile("PMU-Alpha")
	if !ok {
		t.Fatal("profile not hydrated")
	}
	if got.Station != "STATION-A" || got.IDCode != 42 || got.DataRate != 100 {
		t.Fatalf("hydrated profile mismatch: %+v", got)
	}
	if got.HeaderText != "unit test header" {
		t.Fatalf("header text lost: %q", got.HeaderText)
	}
	if len(got.Channels) != 4 || got.Channels[0] != "VA" {
		t.Fatalf("channels mismatch: %v", got.Channels)
	}
}

func TestSanitizeProfileFileName(t *testing.T) {
	cases := map[string]string{
		"PMU A":     "PMU_A",
		"pmu/../x":  "pmu_.._x",
		"":          "unknown",
		"ok-name_1": "ok-name_1",
	}
	for in, want := range cases {
		if got := sanitizeProfileFileName(in); got != want {
			t.Errorf("sanitize(%q)=%q want %q", in, got, want)
		}
	}
}
