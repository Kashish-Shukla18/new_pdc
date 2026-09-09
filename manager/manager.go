package manager

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"pdc/config"
	"pdc/monitoring"
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
	rawPub    receiver.RawFramePublisher
}

// NewPMUManager creates a new PMUManager.
// rawPub, when non-nil, enables ingress mode (TCP → Kafka). handler is used for direct mode.
func NewPMUManager(handler receiver.FrameHandler, rawPub receiver.RawFramePublisher) *PMUManager {
	return &PMUManager{
		receivers: make(map[string]*pmuRun),
		handler:   handler,
		rawPub:    rawPub,
	}
}

// StartPMU starts a new PMU receiver if not already running.
func (m *PMUManager) StartPMU(ctx context.Context, cfg config.PMUConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.rawPub == nil && m.handler == nil {
		return fmt.Errorf("PMU manager has no ingress publisher or direct handler (processor-only mode?)")
	}

	if _, exists := m.receivers[cfg.Name]; exists {
		return fmt.Errorf("PMU %s is already running", cfg.Name)
	}

	pmuCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.receivers[cfg.Name] = &pmuRun{cancel: cancel, done: done}

	r := receiver.New(cfg, m.handler, m.rawPub)
	go func() {
		defer close(done)
		r.Run(pmuCtx)
	}()

	mode := "direct"
	if m.rawPub != nil && m.handler != nil {
		mode = "live+kafka"
	} else if m.rawPub != nil {
		mode = "ingress"
	}
	monitoring.RecordConversation(cfg.Name, "SYSTEM", "PDC", "manager", "ok",
		fmt.Sprintf("Started PMU receiver (%s) %s:%d", mode, cfg.IP, cfg.Port))
	log.Printf("[Manager] Started PMU receiver for %s (%s) %s:%d", cfg.Name, mode, cfg.IP, cfg.Port)
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

	for _, run := range runs {
		<-run.done
	}
}
