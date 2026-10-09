// Package config describes how to reach one PMU (IP, port, idcode, …).
//
// Identity is the network endpoint (not a form nickname, not CFG station).
// Station name from CFG-2 is a display label only.
package config

import (
	"fmt"
	"strings"
	"time"
)

// PMUConfig is the contact card for one phasor measurement unit.
// Name is always derived from the endpoint in Normalize — clients need not send it.
type PMUConfig struct {
	Name         string  `json:"name"` // endpoint identity; set by Normalize
	IP           string  `json:"ip"`
	Port         int     `json:"port"`     // TCP dial port, or UDP listen port
	TCPPort      int     `json:"tcp_port"` // UDP mode only: TCP port for handshake
	IDCode       uint16  `json:"idcode"`
	Protocol     string  `json:"protocol"` // "tcp" (default) or "udp"
	TimeoutSec   int     `json:"timeout_sec"`
	ReconnectSec int     `json:"reconnect_sec"`
	Region       string  `json:"region"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	// Station is the CFG-2 STN label learned after handshake (display only).
	Station string `json:"station,omitempty"`
	// TimestampTZ overrides how SOC is interpreted: "" = fleet default
	// (PMU_TIMESTAMP_TZ / host local). Loopback IPs default to UTC when unset.
	// Values: "UTC", "IST", "Asia/Kolkata", "local", etc.
	TimestampTZ string `json:"timestamp_tz,omitempty"`
	// Active is false when the operator has disconnected the stream (kept in the
	// address book for reconnect). Omitted from client writes; server is source of truth.
	Active bool `json:"active"`
}

// Identity is the stable PDC key for this connection: ip:port (tcp) or udp:ip:port.
func (p *PMUConfig) Identity() string {
	if p == nil {
		return ""
	}
	ip := strings.TrimSpace(p.IP)
	if ip == "" || p.Port <= 0 {
		return ""
	}
	if p.NetworkProtocol() == "udp" {
		return fmt.Sprintf("udp:%s:%d", ip, p.Port)
	}
	return fmt.Sprintf("%s:%d", ip, p.Port)
}

// EndpointKey is used for uniqueness checks (ip, port, protocol).
func (p *PMUConfig) EndpointKey() string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%d", p.NetworkProtocol(), strings.TrimSpace(p.IP), p.Port)
}

// DisplayLabel prefers the learned station name; falls back to endpoint identity.
func (p *PMUConfig) DisplayLabel() string {
	if p == nil {
		return ""
	}
	if s := strings.TrimSpace(p.Station); s != "" {
		return s
	}
	if p.Name != "" {
		return p.Name
	}
	return p.Identity()
}

// Addr is "ip:port" — what we dial (TCP) or the UDP peer/listen target.
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

// DataReadTimeout is a fallback stream idle bound when CFG DATA_RATE is unknown.
// Prefer LivenessTimeout(dataRate) after handshake (no 60s floor).
func (p *PMUConfig) DataReadTimeout() time.Duration {
	return p.Timeout()
}

// LivenessTimeout is how long we may go without a valid DATA frame before
// treating the PMU as disconnected (leave live set, keep reconnecting).
// Rule: 3 × frame period from CFG DATA_RATE, clamped to [1s, 30s].
func LivenessTimeout(dataRate int16) time.Duration {
	fps := 20.0 // default when rate unknown
	if dataRate > 0 {
		fps = float64(dataRate)
	} else if dataRate < 0 {
		fps = 1.0 / float64(-dataRate)
	}
	if fps < 0.05 {
		fps = 0.05
	}
	period := time.Duration(float64(time.Second) / fps)
	d := 3 * period
	if d < time.Second {
		d = time.Second
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
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
	if strings.EqualFold(strings.TrimSpace(p.Protocol), "udp") {
		return "udp"
	}
	return "tcp"
}

// Normalize fills blanks, cleans form mistakes, and sets Name = Identity().
func (p *PMUConfig) Normalize() {
	p.IP = strings.TrimSpace(p.IP)
	p.Protocol = p.NetworkProtocol()
	if p.NetworkProtocol() == "tcp" {
		p.TCPPort = 0
	}
	if p.TimeoutSec <= 0 {
		p.TimeoutSec = 60
	}
	if p.ReconnectSec <= 0 {
		p.ReconnectSec = 5
	}
	p.Station = strings.TrimSpace(p.Station)
	p.TimestampTZ = NormalizeTimestampTZ(p.TimestampTZ)
	if id := p.Identity(); id != "" {
		p.Name = id
	}
}
