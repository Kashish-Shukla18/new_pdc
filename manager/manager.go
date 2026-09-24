// Package manager is the "remote control" for PMU connections.
//
// Start / stop receivers when the dashboard adds or deletes a device.
package manager

import (
	"context"
	"fmt"
	"log"
	"sort"
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

// PMUManager keeps track of active PMU receivers and handles starting/stopping them dynamically.
type PMUManager struct {
	mu        sync.Mutex
	receivers map[string]*pmuRun
	handler   receiver.FrameHandler
	// onSetChanged resets dashboard time alignment when the live PMU set changes.
	onSetChanged func(active []string)
}

// NewPMUManager creates a new PMUManager.
func NewPMUManager(handler receiver.FrameHandler) *PMUManager {
	return &PMUManager{
		receivers: make(map[string]*pmuRun),
		handler:   handler,
	}
}

// SetAlignerHook registers a callback used to reset dashboard time alignment
// whenever the live (handshaked) PMU set changes.
func (m *PMUManager) SetAlignerHook(fn func(active []string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSetChanged = fn
}

// liveNamesLocked = receivers that have a CFG-2 profile (handshake succeeded).
// Dead / not-yet-connected PMUs are excluded so they do not poison alignment.
func (m *PMUManager) liveNamesLocked() []string {
	names := make([]string, 0, len(m.receivers))
	for name := range m.receivers {
		if _, ok := parser.GetProfile(name); ok {
			names = append(names, name)
		}
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

// StartPMU starts a new PMU receiver if not already running.
func (m *PMUManager) StartPMU(ctx context.Context, cfg config.PMUConfig) error {
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
	r.SetOnSessionStart(func(_ string) {
		// Only after CFG-2 + DATA_ON — this PMU is actually live.
		m.notifyAligner()
	})
	go func() {
		defer close(done)
		r.Run(pmuCtx)
	}()

	monitoring.RecordConversation(cfg.Name, "SYSTEM", "PDC", "manager", "ok",
		fmt.Sprintf("Started PMU receiver %s:%d", cfg.IP, cfg.Port))
	log.Printf("[Manager] Started PMU receiver for %s %s:%d", cfg.Name, cfg.IP, cfg.Port)
	m.mu.Unlock()
	// Do NOT notify aligner here — wait until handshake succeeds (onSessionStart).
	return nil
}

// StopPMU stops a running PMU receiver and waits until its TCP loop has exited.
func (m *PMUManager) StopPMU(name string) error {
	m.mu.Lock()
	run, exists := m.receivers[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("PMU %s is not running", name)
	}
	run.cancel()
	delete(m.receivers, name)
	done := run.done
	m.mu.Unlock()

	m.notifyAligner()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		log.Printf("[Manager] PMU %s receiver did not exit within 15s after stop", name)
	}

	monitoring.RecordConversation(name, "SYSTEM", "PDC", "manager", "warn", "Stopped PMU receiver")
	log.Printf("[Manager] Stopped PMU receiver for %s", name)
	return nil
}

// StopAll stops all running PMU receivers.
func (m *PMUManager) StopAll() {
	m.mu.Lock()
	runs := make([]*pmuRun, 0, len(m.receivers))
	for _, run := range m.receivers {
		run.cancel()
		runs = append(runs, run)
	}
	m.receivers = make(map[string]*pmuRun)
	m.mu.Unlock()

	m.notifyAligner()

	for _, run := range runs {
		<-run.done
	}
}
