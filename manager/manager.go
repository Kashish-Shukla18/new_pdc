package manager

import (
	"context"
	"fmt"
	"log"
	"sync"

	"pdc/config"
	"pdc/monitoring"
	"pdc/receiver"
)

// PMUManager keeps track of active PMU receivers and handles starting/stopping them dynamically.
type PMUManager struct {
	mu        sync.Mutex
	receivers map[string]context.CancelFunc
	handler   receiver.FrameHandler
	rawPub    receiver.RawFramePublisher
}

// NewPMUManager creates a new PMUManager.
// rawPub, when non-nil, enables ingress mode (TCP → Kafka). handler is used for direct mode.
func NewPMUManager(handler receiver.FrameHandler, rawPub receiver.RawFramePublisher) *PMUManager {
	return &PMUManager{
		receivers: make(map[string]context.CancelFunc),
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
	m.receivers[cfg.Name] = cancel

	r := receiver.New(cfg, m.handler, m.rawPub)
	go r.Run(pmuCtx)

	mode := "direct"
	if m.rawPub != nil && m.handler != nil {
		mode = "live+kafka"
	} else if m.rawPub != nil {
		mode = "ingress"
	}
	monitoring.RecordConversation(cfg.Name, "SYSTEM", "PDC", "manager", "ok",
		fmt.Sprintf("Started PMU receiver (%s)", mode))
	log.Printf("[Manager] Started PMU receiver for %s (%s)", cfg.Name, mode)
	return nil
}

// StopPMU stops a running PMU receiver.
func (m *PMUManager) StopPMU(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cancel, exists := m.receivers[name]
	if !exists {
		return fmt.Errorf("PMU %s is not running", name)
	}

	cancel()
	delete(m.receivers, name)

	monitoring.RecordConversation(name, "SYSTEM", "PDC", "manager", "warn", "Stopped PMU receiver")
	log.Printf("[Manager] Stopped PMU receiver for %s", name)
	return nil
}

// StopAll stops all running PMU receivers.
func (m *PMUManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, cancel := range m.receivers {
		cancel()
		delete(m.receivers, name)
	}
}
