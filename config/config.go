package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// PMUConfig holds the connection parameters for a single PMU.
type PMUConfig struct {
	Name         string `yaml:"name"`
	IP           string `yaml:"ip"`
	Port         int    `yaml:"port"`
	IDCode       uint16 `yaml:"idcode"`
	Protocol     string `yaml:"protocol"`      // "tcp" (default) or "udp"
	TimeoutSec   int    `yaml:"timeout_sec"`   // dial / read timeout
	ReconnectSec int    `yaml:"reconnect_sec"` // reconnect back-off
}

// Addr returns the "host:port" string used for dialing.
func (p *PMUConfig) Addr() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

// Timeout returns the dial/read timeout as a time.Duration.
func (p *PMUConfig) Timeout() time.Duration {
	if p.TimeoutSec <= 0 {
		return 5 * time.Second
	}
	return time.Duration(p.TimeoutSec) * time.Second
}

// ReconnectInterval returns the reconnect back-off as a time.Duration.
func (p *PMUConfig) ReconnectInterval() time.Duration {
	if p.ReconnectSec <= 0 {
		return 10 * time.Second
	}
	return time.Duration(p.ReconnectSec) * time.Second
}

// NetworkProtocol returns "tcp" unless "udp" is explicitly set.
func (p *PMUConfig) NetworkProtocol() string {
	if p.Protocol == "udp" {
		return "udp"
	}
	return "tcp"
}

// Config is the top-level configuration loaded from pmus.yaml.
type Config struct {
	PMUs []PMUConfig `yaml:"pmus"`
}

// Load reads and parses the YAML configuration file at path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path) // #nosec G304 – path comes from a controlled CLI argument
	if err != nil {
		return nil, fmt.Errorf("config: open %q: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config: validation: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.PMUs) == 0 {
		return fmt.Errorf("no PMUs defined")
	}
	seen := make(map[uint16]string)
	for i, p := range c.PMUs {
		if p.IP == "" {
			return fmt.Errorf("pmus[%d] (%s): ip is required", i, p.Name)
		}
		if p.Port <= 0 || p.Port > 65535 {
			return fmt.Errorf("pmus[%d] (%s): port %d is invalid", i, p.Name, p.Port)
		}
		if p.IDCode == 0 {
			return fmt.Errorf("pmus[%d] (%s): idcode must be non-zero", i, p.Name)
		}
		if prev, dup := seen[p.IDCode]; dup {
			return fmt.Errorf("pmus[%d] (%s): duplicate idcode %d (already used by %s)",
				i, p.Name, p.IDCode, prev)
		}
		seen[p.IDCode] = p.Name
	}
	return nil
}
