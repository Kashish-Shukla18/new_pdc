package output

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pdc/parser"
)

// ReadingSpool keeps failed writes on disk and replays them later.
type ReadingSpool struct {
	path string
	mu   sync.Mutex
}

type ReplayStats struct {
	Replayed      int
	Pending       int
	ReplayedByPMU map[string]int
}

func spoolFsyncEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("SPOOL_FSYNC")))
	if v == "" {
		return true
	}
	return !(v == "0" || v == "false" || v == "no" || v == "off")
}

func NewReadingSpool(path string) *ReadingSpool {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return nil
	}
	return &ReadingSpool{path: clean}
}

func isSpoolTokenTooLong(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, bufio.ErrTooLong) || strings.Contains(strings.ToLower(err.Error()), "token too long")
}

func (s *ReadingSpool) quarantineCorrupt(cause error) error {
	if s == nil {
		return cause
	}
	ts := time.Now().Format("20060102-150405")
	quarantine := s.path + ".corrupt." + ts
	if err := os.Rename(s.path, quarantine); err != nil {
		return fmt.Errorf("quarantine corrupt spool: %w (original: %v)", err, cause)
	}
	return fmt.Errorf("spool quarantined to %s: %w", quarantine, cause)
}

func replaceFileWithRetry(dst, src string) error {
	const attempts = 12
	const delay = 150 * time.Millisecond

	var lastErr error
	for i := 0; i < attempts; i++ {
		// Windows cannot rename over an existing file while handles are active.
		if rmErr := os.Remove(dst); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			lastErr = rmErr
		}

		if err := os.Rename(src, dst); err == nil {
			return nil
		} else {
			lastErr = err
		}

		time.Sleep(delay)
	}

	// Last-resort fallback: copy tmp content into destination then delete tmp.
	// This avoids perpetual replay failure when rename is intermittently blocked.
	sf, err := os.Open(src)
	if err != nil {
		if lastErr != nil {
			return fmt.Errorf("rename failed: %v; open src fallback: %w", lastErr, err)
		}
		return err
	}
	defer sf.Close()

	df, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		if lastErr != nil {
			return fmt.Errorf("rename failed: %v; open dst fallback: %w", lastErr, err)
		}
		return err
	}

	if _, err := io.Copy(df, sf); err != nil {
		_ = df.Close()
		if lastErr != nil {
			return fmt.Errorf("rename failed: %v; copy fallback: %w", lastErr, err)
		}
		return err
	}

	if err := df.Close(); err != nil {
		if lastErr != nil {
			return fmt.Errorf("rename failed: %v; close fallback dst: %w", lastErr, err)
		}
		return err
	}

	if err := os.Remove(src); err != nil && !errors.Is(err, os.ErrNotExist) {
		if lastErr != nil {
			return fmt.Errorf("rename failed: %v; remove tmp fallback: %w", lastErr, err)
		}
		return err
	}

	return nil
}

func removeFileWithRetry(path string) error {
	const attempts = 12
	const delay = 150 * time.Millisecond
	for i := 0; i < attempts; i++ {
		err := os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		time.Sleep(delay)
	}
	// Final attempt, return the error if it still fails
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *ReadingSpool) Append(r parser.Reading) error {
	if s == nil {
		return nil
	}

	payload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal spool reading: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir spool dir: %w", err)
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open spool: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("append spool: %w", err)
	}

	if spoolFsyncEnabled() {
		if err := f.Sync(); err != nil {
			return fmt.Errorf("sync spool: %w", err)
		}
	}
	return nil
}

func (s *ReadingSpool) CountsByPMU() (map[string]int, error) {
	counts := make(map[string]int)
	if s == nil {
		return counts, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return counts, nil
		}
		return nil, fmt.Errorf("open spool for count: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*64)
	scanner.Buffer(buf, 1024*1024*10)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var r parser.Reading
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		pmu := strings.TrimSpace(r.PMUName)
		if pmu == "" {
			pmu = "SYSTEM"
		}
		counts[pmu]++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan spool for count: %w", err)
	}

	return counts, nil
}

// Replay attempts up to maxBatch pending records using fn.
// It preserves order and keeps failed entries in the spool file.
func (s *ReadingSpool) Replay(ctx context.Context, fn func(context.Context, parser.Reading) error, maxBatch int) (stats ReplayStats, err error) {
	if s == nil {
		stats.ReplayedByPMU = make(map[string]int)
		return stats, nil
	}
	if maxBatch <= 0 {
		maxBatch = 1
	}
	stats.ReplayedByPMU = make(map[string]int)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return stats, nil
		}
		return stats, fmt.Errorf("open spool for replay: %w", err)
	}

	tmpPath := s.path + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		_ = f.Close()
		return stats, fmt.Errorf("open tmp spool: %w", err)
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*64)
	scanner.Buffer(buf, 1024*1024*10)
	processed := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			if processed >= maxBatch {
				if _, err := tmp.WriteString(line + "\n"); err != nil {
					return stats, fmt.Errorf("write tmp spool: %w", err)
				}
				stats.Pending++
				continue
			}

			var r parser.Reading
			if unmarshalErr := json.Unmarshal([]byte(line), &r); unmarshalErr != nil {
				if _, err := tmp.WriteString(line + "\n"); err != nil {
					return stats, fmt.Errorf("write tmp spool: %w", err)
				}
				stats.Pending++
				continue
			}

			if replayErr := fn(ctx, r); replayErr != nil {
				if _, err := tmp.WriteString(line + "\n"); err != nil {
					return stats, fmt.Errorf("write tmp spool: %w", err)
				}
				stats.Pending++
				processed++
				continue
			}

			stats.Replayed++
			pmu := strings.TrimSpace(r.PMUName)
			if pmu == "" {
				pmu = "SYSTEM"
			}
			stats.ReplayedByPMU[pmu]++
			processed++
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		_ = tmp.Close()
		_ = f.Close()
		_ = removeFileWithRetry(tmpPath)
		if isSpoolTokenTooLong(scanErr) {
			return stats, s.quarantineCorrupt(scanErr)
		}
		return ReplayStats{}, fmt.Errorf("scan spool: %w", scanErr)
	}

	if err := f.Close(); err != nil {
		_ = tmp.Close()
		return stats, fmt.Errorf("close spool before replace: %w", err)
	}

	if stats.Replayed == 0 && stats.Pending == 0 {
		_ = tmp.Close()
		_ = removeFileWithRetry(tmpPath)
		_ = removeFileWithRetry(s.path)
		return stats, nil
	}

	if stats.Pending == 0 {
		if err := tmp.Close(); err != nil {
			return stats, fmt.Errorf("close tmp spool: %w", err)
		}
		_ = removeFileWithRetry(tmpPath)
		if rmErr := removeFileWithRetry(s.path); rmErr != nil {
			return stats, fmt.Errorf("remove empty spool: %w", rmErr)
		}
		return stats, nil
	}

	if err := tmp.Close(); err != nil {
		return stats, fmt.Errorf("close tmp spool: %w", err)
	}

	if err := replaceFileWithRetry(s.path, tmpPath); err != nil {
		return stats, fmt.Errorf("replace spool: %w", err)
	}

	return stats, nil
}
