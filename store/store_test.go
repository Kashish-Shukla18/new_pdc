package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"pdc/config"
)

func TestNewStoreDegradedWhenDBDown(t *testing.T) {
	prev := os.Getenv("POSTGRES_DSN")
	prevReconnect := os.Getenv("POSTGRES_RECONNECT_SEC")
	t.Cleanup(func() {
		_ = os.Setenv("POSTGRES_DSN", prev)
		_ = os.Setenv("POSTGRES_RECONNECT_SEC", prevReconnect)
	})
	// Nothing listens here.
	_ = os.Setenv("POSTGRES_DSN", "postgres://pdc:pdc@127.0.0.1:1/pdc?sslmode=disable")
	_ = os.Setenv("POSTGRES_RECONNECT_SEC", "1h") // avoid noisy reconnect during test

	s := NewStore()
	defer s.Close()

	if s.Ready() {
		t.Fatal("Ready() should be false when DB is unreachable")
	}
	_, err := s.GetAllPMUs(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("GetAllPMUs err=%v want ErrUnavailable", err)
	}
	err = s.SavePMU(context.Background(), config.PMUConfig{IP: "1.2.3.4", Port: 4712})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("SavePMU err=%v want ErrUnavailable", err)
	}
	err = s.UpdateStation(context.Background(), "1.2.3.4:4712", "STN")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("UpdateStation err=%v want ErrUnavailable", err)
	}
	err = s.DeletePMU(context.Background(), "1.2.3.4:4712")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("DeletePMU err=%v want ErrUnavailable", err)
	}
}

func TestReconnectIntervalEnv(t *testing.T) {
	prev := os.Getenv("POSTGRES_RECONNECT_SEC")
	t.Cleanup(func() { _ = os.Setenv("POSTGRES_RECONNECT_SEC", prev) })

	_ = os.Setenv("POSTGRES_RECONNECT_SEC", "2s")
	if got := reconnectInterval(); got != 2*time.Second {
		t.Fatalf("2s → %s", got)
	}
	_ = os.Setenv("POSTGRES_RECONNECT_SEC", "3")
	if got := reconnectInterval(); got != 3*time.Second {
		t.Fatalf("3 → %s", got)
	}
}
