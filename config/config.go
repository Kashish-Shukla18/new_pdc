// Package config describes how to reach one PMU (IP, port, idcode, …).
//
// Think of it like a contact card for each device.
// The real list of PMUs lives in Postgres (see store/), not in YAML files.
package config

import (
	"fmt"
	"time"
)

// PMUConfig is the contact card for one phasor measurement unit.
type PMUConfig struct {
	Name         string  `json:"name"`
	IP           string  `json:"ip"`
	Port         int     `json:"port"`       // TCP dial port, or UDP listen port
	TCPPort      int     `json:"tcp_port"`   // UDP mode only: TCP port for handshake
	IDCode       uint16  `json:"idcode"`
	Protocol     string  `json:"protocol"`   // "tcp" (default) or "udp"
	TimeoutSec   int     `json:"timeout_sec"`
	ReconnectSec int     `json:"reconnect_sec"`
	Region       string  `json:"region"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
}

// Addr is "ip:port" — what we dial.
func (p *PMUConfig) Addr() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

// Timeout for dial / handshake reads.
func (p *PMUConfig) Timeout() time.Duration {
	if p.TimeoutSec <= 0 {
		return 60 * time.Second
	}
	return time.Duration(p.TimeoutSec) * time.Second
}

// DataReadTimeout is how long we wait for the next DATA frame on a quiet stream.
func (p *PMUConfig) DataReadTimeout() time.Duration {
	t := p.Timeout()
	if t < 60*time.Second {
		return 60 * time.Second
	}
	return t
}

// ReconnectInterval is how long we sleep before trying again after a drop.
func (p *PMUConfig) ReconnectInterval() time.Duration {
	if p.ReconnectSec <= 0 {
		return 5 * time.Second
	}
	return time.Duration(p.ReconnectSec) * time.Second
}

// NetworkProtocol is "tcp" unless you explicitly set "udp".
func (p *PMUConfig) NetworkProtocol() string {
	if p.Protocol == "udp" {
		return "udp"
	}
	return "tcp"
}

// Normalize fills blanks and cleans up common form mistakes.
func (p *PMUConfig) Normalize() {
	if p.Protocol == "" {
		p.Protocol = "tcp"
	}
	if p.NetworkProtocol() == "tcp" {
		p.TCPPort = 0
	}
	if p.TimeoutSec <= 0 {
		p.TimeoutSec = 60
	}
	if p.ReconnectSec <= 0 {
		p.ReconnectSec = 5
	}
}
