package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSetsMissingOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "" +
		"# comment\n" +
		"HISTORY_BATCH_SIZE=250\n" +
		"ENABLE_HISTORY=true\n" +
		"QUOTED=\"hello world\"\n" +
		"export POSTGRES_RECONNECT_SEC=7s\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ENABLE_HISTORY", "false") // must win over .env
	_ = os.Unsetenv("HISTORY_BATCH_SIZE")
	_ = os.Unsetenv("QUOTED")
	_ = os.Unsetenv("POSTGRES_RECONNECT_SEC")

	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("ENABLE_HISTORY"); got != "false" {
		t.Fatalf("OS env should win: %q", got)
	}
	if got := os.Getenv("HISTORY_BATCH_SIZE"); got != "250" {
		t.Fatalf("batch=%q", got)
	}
	if got := os.Getenv("QUOTED"); got != "hello world" {
		t.Fatalf("quoted=%q", got)
	}
	if got := os.Getenv("POSTGRES_RECONNECT_SEC"); got != "7s" {
		t.Fatalf("reconnect=%q", got)
	}
}

func TestLoadMissingFileOK(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Fatal(err)
	}
}
