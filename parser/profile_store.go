package parser

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

// profileStoreDir, when non-empty, causes SetProfile to persist each profile to disk
// and enables LoadPersistedProfiles to hydrate the in-memory registry on startup.
var (
	profileStoreDir string
	profileStoreMu  sync.Mutex
)

// ConfigureProfileStore sets the directory used to persist CFG2 profiles.
// Pass an empty string to disable persistence (in-memory only).
func ConfigureProfileStore(dir string) {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	profileStoreDir = strings.TrimSpace(dir)
}

// ProfileStoreDir returns the configured persistence directory (may be empty).
func ProfileStoreDir() string {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	return profileStoreDir
}

// SetProfile stores the CFG2-derived layout for a PMU in memory and, when a
// profile store is configured, persists it to disk so DATA frames can be parsed
// after a processor restart without waiting for a new handshake.
func SetProfile(pmuName string, p Profile) {
	profileRegistry.Store(pmuName, p)

	dir := ProfileStoreDir()
	if dir == "" {
		return
	}
	if err := saveProfileToDisk(dir, pmuName, p); err != nil {
		log.Printf("[%s] cfg2 profile persist error: %v", pmuName, err)
	}
}

// LoadPersistedProfiles loads all JSON profiles from dir into the registry.
// Returns the number of profiles loaded. Missing directory is not an error.
func LoadPersistedProfiles(dir string) (int, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return 0, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read profile dir %s: %w", dir, err)
	}

	loaded := 0
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, ent.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			log.Printf("cfg2 profile load skip %s: %v", path, err)
			continue
		}
		var stored persistedProfile
		if err := json.Unmarshal(raw, &stored); err != nil {
			log.Printf("cfg2 profile load skip %s: %v", path, err)
			continue
		}
		name := strings.TrimSpace(stored.PMUName)
		if name == "" {
			name = strings.TrimSuffix(ent.Name(), filepath.Ext(ent.Name()))
		}
		if name == "" {
			continue
		}
		// Store in memory only — avoid re-writing every file on startup.
		profileRegistry.Store(name, stored.Profile)
		loaded++
	}
	return loaded, nil
}

type persistedProfile struct {
	PMUName string  `json:"pmu_name"`
	Profile Profile `json:"profile"`
}

func saveProfileToDisk(dir, pmuName string, p Profile) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	payload, err := json.MarshalIndent(persistedProfile{PMUName: pmuName, Profile: p}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	base := sanitizeProfileFileName(pmuName)
	finalPath := filepath.Join(dir, base+".json")
	tmpPath := finalPath + ".tmp"

	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return fmt.Errorf("write temp profile: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename profile: %w", err)
	}
	return nil
}

func sanitizeProfileFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." {
		return "unknown"
	}
	return out
}
