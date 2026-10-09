// Package manager is the "remote control" for PMU connections.
//
// Two sets matter:
//
//	configured — operator started the receiver (address-book / StartPMU).
//	             Stays until StopPMU; reconnect keeps retrying in the background.
//	live       — handshake + DATA_ON succeeded and the stream has not ended.
//	             This is what the aligner waits on (complete among live only).
//
// Start / stop receivers when the dashboard adds or deletes a device.
package manager

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"pdc/config"
	"pdc/monitoring"
	"pdc/parser"
	"pdc/receiver"
)

type pmuRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// HistoryHooks records CFG / lifecycle events for Timescale (optional).
type HistoryHooks interface {
	NoteBecameLive(pmu string)
	NoteLeftLive(pmu string)
	NoteOperatorStart(pmu string)
	NoteOperatorStop(pmu string)
	NoteProfile(pmu string)
}

// PMUManager keeps track of configured PMU receivers and which of them are live.
type PMUManager struct {
	mu           sync.Mutex
	receivers    map[string]*pmuRun // configured
	live         map[string]struct{}
	handler      receiver.FrameHandler
	onSetChanged func(live []string) // aligner expected set = live names
	onStation    func(name, station string)
	onPMUConfig  func(cfg config.PMUConfig) // timestamp TZ / config cache for pipeline
	history      HistoryHooks
}

// NewPMUManager creates a new PMUManager.
func NewPMUManager(handler receiver.FrameHandler) *PMUManager {
	return &PMUManager{
		receivers: make(map[string]*pmuRun),
		live:      make(map[string]struct{}),
		handler:   handler,
	}
}

// SetAlignerHook registers a callback whenever the live PMU set changes.
func (m *PMUManager) SetAlignerHook(fn func(live []string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSetChanged = fn
}

// SetStationHook runs after a PMU becomes live with the CFG-2 station label
// (for persisting display metadata; identity stays the endpoint).
func (m *PMUManager) SetStationHook(fn func(name, station string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStation = fn
}

// SetHistoryHooks wires optional Timescale CFG/lifecycle recording.
func (m *PMUManager) SetHistoryHooks(h HistoryHooks) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = h
}

// SetPMUConfigHook runs when a PMU is started (so the pipeline can cache timestamp TZ).
func (m *PMUManager) SetPMUConfigHook(fn func(cfg config.PMUConfig)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPMUConfig = fn
}

// ConfiguredNames returns operator-started PMU names (sorted).
func (m *PMUManager) ConfiguredNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.receivers))
	for name := range m.receivers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsConfigured reports whether a receiver loop is running for name.
func (m *PMUManager) IsConfigured(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.receivers[name]
	return ok
}

// LiveNames returns currently streaming PMU names (sorted).
func (m *PMUManager) LiveNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.liveNamesLocked()
}

func (m *PMUManager) liveNamesLocked() []string {
	names := make([]string, 0, len(m.live))
	for name := range m.live {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// notifyAligner must be called WITHOUT holding m.mu.
func (m *PMUManager) notifyAligner() {
	m.mu.Lock()
	fn := m.onSetChanged
	names := m.liveNamesLocked()
	m.mu.Unlock()
	if fn != nil {
		fn(names)
	}
}

func (m *PMUManager) markLive(name string) {
	m.mu.Lock()
	if _, configured := m.receivers[name]; !configured {
		m.mu.Unlock()
		return
	}
	if _, already := m.live[name]; already {
		m.mu.Unlock()
		return
	}
	m.live[name] = struct{}{}
	onStation := m.onStation
	hist := m.history
	m.mu.Unlock()
	monitoring.RecordConversation(name, "SYSTEM", "PDC", "live", "ok", "PMU entered live set")
	log.Printf("[Manager] %s live (aligner will wait on it)", name)
	if hist != nil {
		hist.NoteBecameLive(name)
	}
	if onStation != nil {
		if prof, ok := parser.GetProfile(name); ok {
			if st := strings.TrimSpace(prof.Station); st != "" {
				onStation(name, st)
			}
		}
	}
	m.notifyAligner()
}

func (m *PMUManager) markNotLive(name string) {
	m.mu.Lock()
	if _, ok := m.live[name]; !ok {
		m.mu.Unlock()
		return
	}
	delete(m.live, name)
	configured := false
	if _, ok := m.receivers[name]; ok {
		configured = true
	}
	hist := m.history
	m.mu.Unlock()
	msg := "PMU left live set"
	if configured {
		msg += " (still configured — reconnecting)"
	}
	monitoring.RecordConversation(name, "SYSTEM", "PDC", "live", "warn", msg)
	log.Printf("[Manager] %s not live — %s", name, msg)
	if hist != nil {
		hist.NoteLeftLive(name)
	}
	m.notifyAligner()
}

func (m *PMUManager) refreshStation(name string) {
	m.mu.Lock()
	onStation := m.onStation
	hist := m.history
	m.mu.Unlock()
	if hist != nil {
		hist.NoteProfile(name)
	}
	if onStation == nil {
		return
	}
	if prof, ok := parser.GetProfile(name); ok {
		if st := strings.TrimSpace(prof.Station); st != "" {
			onStation(name, st)
		}
	}
}

// StartPMU starts a new PMU receiver if not already running.
// The PMU is configured immediately; it becomes live only after session start.
func (m *PMUManager) StartPMU(ctx context.Context, cfg config.PMUConfig) error {
	cfg.Normalize()
	if cfg.Name == "" {
		return fmt.Errorf("PMU missing ip/port")
	}
	m.mu.Lock()

	if m.handler == nil {
		m.mu.Unlock()
		return fmt.Errorf("PMU manager has no frame handler")
	}

	if _, exists := m.receivers[cfg.Name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("PMU %s is already running", cfg.Name)
	}

	pmuCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.receivers[cfg.Name] = &pmuRun{cancel: cancel, done: done}

	r := receiver.New(cfg, m.handler)
	r.SetOnSessionStart(func(name string) {
		m.markLive(name)
	})
	r.SetOnSessionEnd(func(name string) {
		m.markNotLive(name)
	})
	r.SetOnProfileUpdate(func(name string) {
		m.refreshStation(name)
	})
	go func() {
		defer close(done)
		r.Run(pmuCtx)
	}()

	monitoring.RecordConversation(cfg.Name, "SYSTEM", "PDC", "manager", "ok",
		fmt.Sprintf("Started PMU receiver %s:%d (configured, not yet live)", cfg.IP, cfg.Port))
	log.Printf("[Manager] Started PMU receiver for %s %s:%d (configured)", cfg.Name, cfg.IP, cfg.Port)
	hist := m.history
	onCfg := m.onPMUConfig
	m.mu.Unlock()
	if onCfg != nil {
		onCfg(cfg)
	}
	if hist != nil {
		hist.NoteOperatorStart(cfg.Name)
	}
	// Do NOT notify aligner here — wait until handshake succeeds (live).
	return nil
}

// StopPMU stops a running PMU receiver and waits until its TCP loop has exited.
// Removes from both configured and live sets.
func (m *PMUManager) StopPMU(name string) error {
	m.mu.Lock()
	run, exists := m.receivers[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("PMU %s is not running", name)
	}
	run.cancel()
	delete(m.receivers, name)
	delete(m.live, name)
	done := run.done
	hist := m.history
	m.mu.Unlock()

	m.notifyAligner()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		log.Printf("[Manager] PMU %s receiver did not exit within 15s after stop", name)
	}

	if hist != nil {
		hist.NoteOperatorStop(name)
		hist.NoteLeftLive(name)
	}

	monitoring.RecordConversation(name, "SYSTEM", "PDC", "manager", "warn", "Stopped PMU receiver")
	log.Printf("[Manager] Stopped PMU receiver for %s", name)
	return nil
}

// StopAll stops all running PMU receivers.
func (m *PMUManager) StopAll() {
	m.mu.Lock()
	runs := make([]*pmuRun, 0, len(m.receivers))
	names := make([]string, 0, len(m.receivers))
	for name, run := range m.receivers {
		run.cancel()
		runs = append(runs, run)
		names = append(names, name)
	}
	m.receivers = make(map[string]*pmuRun)
	m.live = make(map[string]struct{})
	hist := m.history
	m.mu.Unlock()

	m.notifyAligner()

	for _, run := range runs {
		<-run.done
	}
	if hist != nil {
		for _, name := range names {
			hist.NoteOperatorStop(name)
			hist.NoteLeftLive(name)
		}
	}
}
