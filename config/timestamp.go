package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultFleetTimestampTZ is the last-resort fallback when the host zone cannot
// be resolved (unusual). Prefer PMU_TIMESTAMP_TZ=local or an explicit IANA name.
const DefaultFleetTimestampTZ = "Asia/Kolkata"

var (
	locCacheMu sync.Mutex
	locCache   = map[string]*time.Location{}

	hostTZOnce sync.Once
	hostTZName string
)

// DefaultTimestampTZ is the fleet default from PMU_TIMESTAMP_TZ.
// Empty / local / host / auto / system → timezone of the machine running the PDC.
func DefaultTimestampTZ() string {
	v := strings.TrimSpace(os.Getenv("PMU_TIMESTAMP_TZ"))
	if v == "" || isHostTZSentinel(v) {
		return HostTimestampTZ()
	}
	return NormalizeTimestampTZ(v)
}

func isHostTZSentinel(tz string) bool {
	switch strings.ToLower(strings.TrimSpace(tz)) {
	case "local", "host", "auto", "system":
		return true
	default:
		return false
	}
}

// HostTimestampTZ returns the IANA (or UTC±HH:MM) zone of this machine.
func HostTimestampTZ() string {
	hostTZOnce.Do(func() {
		hostTZName = detectHostTimestampTZ()
	})
	return hostTZName
}

func detectHostTimestampTZ() string {
	// Standard Unix override also used by some Windows setups.
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" {
		if strings.HasPrefix(tz, ":") || strings.EqualFold(tz, "localtime") {
			// fall through to system detection
		} else if n := NormalizeTimestampTZ(tz); n != "" {
			if _, err := time.LoadLocation(n); err == nil {
				return n
			}
		}
	}

	if name := ianaFromLocaltime(); name != "" {
		return NormalizeTimestampTZ(name)
	}

	locName := time.Local.String()
	if locName != "" && !strings.EqualFold(locName, "Local") {
		if n := NormalizeTimestampTZ(locName); n != "" {
			if _, err := time.LoadLocation(n); err == nil {
				return n
			}
		}
	}

	abbr, offset := time.Now().In(time.Local).Zone()
	if n := NormalizeTimestampTZ(abbr); n != "" {
		if _, err := time.LoadLocation(n); err == nil {
			return n
		}
	}
	if n := windowsTZAlias(abbr); n != "" {
		if _, err := time.LoadLocation(n); err == nil {
			return n
		}
	}
	if offset == 0 {
		return "UTC"
	}
	return fixedOffsetName(offset)
}

// ianaFromLocaltime reads the IANA name from /etc/localtime or /etc/timezone (Unix).
func ianaFromLocaltime() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	link, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	link = strings.ReplaceAll(link, "\\", "/")
	const marker = "zoneinfo/"
	if i := strings.LastIndex(link, marker); i >= 0 {
		return strings.TrimSpace(link[i+len(marker):])
	}
	return ""
}

func windowsTZAlias(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "india standard time", "india daylight time":
		return "Asia/Kolkata"
	case "pacific standard time", "pacific daylight time":
		return "America/Los_Angeles"
	case "eastern standard time", "eastern daylight time":
		return "America/New_York"
	case "gmt standard time", "greenwich standard time":
		return "Europe/London"
	case "tokyo standard time":
		return "Asia/Tokyo"
	case "china standard time":
		return "Asia/Shanghai"
	default:
		return ""
	}
}

func fixedOffsetName(offsetSec int) string {
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	h := offsetSec / 3600
	m := (offsetSec % 3600) / 60
	return fmt.Sprintf("UTC%s%02d:%02d", sign, h, m)
}

func parseFixedOffsetName(name string) (*time.Location, bool) {
	s := strings.TrimSpace(name)
	if !strings.HasPrefix(s, "UTC+") && !strings.HasPrefix(s, "UTC-") {
		return nil, false
	}
	sign := 1
	rest := s[3:]
	if s[3] == '-' {
		sign = -1
		rest = s[4:]
	} else if s[3] == '+' {
		rest = s[4:]
	}
	var h, m int
	if _, err := fmt.Sscanf(rest, "%d:%d", &h, &m); err != nil {
		return nil, false
	}
	if h > 14 || m < 0 || m >= 60 {
		return nil, false
	}
	return time.FixedZone(s, sign*(h*3600+m*60)), true
}

// NormalizeTimestampTZ canonicalizes aliases (IST → Asia/Kolkata, empty → "").
// Host sentinels (local/host/auto/system) are left unchanged so EffectiveTimestampTZ
// can resolve them at use time.
func NormalizeTimestampTZ(tz string) string {
	s := strings.TrimSpace(tz)
	if s == "" {
		return ""
	}
	if isHostTZSentinel(s) {
		return strings.ToLower(s)
	}
	switch strings.ToUpper(s) {
	case "UTC", "GMT", "Z", "ZULU":
		return "UTC"
	case "IST", "INDIA", "ASIA/KOLKATA", "ASIA/CALCUTTA":
		return "Asia/Kolkata"
	default:
		return s
	}
}

// IsLoopbackIP is true for localhost / 127.0.0.0/8 / ::1 (lab sims).
func IsLoopbackIP(ip string) bool {
	host := strings.TrimSpace(ip)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

// EffectiveTimestampTZ picks per-PMU override, else UTC for loopback, else fleet default.
func (p *PMUConfig) EffectiveTimestampTZ() string {
	if p == nil {
		return DefaultTimestampTZ()
	}
	if s := NormalizeTimestampTZ(p.TimestampTZ); s != "" {
		if isHostTZSentinel(s) {
			return HostTimestampTZ()
		}
		return s
	}
	if IsLoopbackIP(p.IP) {
		return "UTC"
	}
	return DefaultTimestampTZ()
}

// LoadLocation resolves a TZ name (UTC, IANA, or UTC±HH:MM). Empty → fleet default.
func LoadLocation(tz string) (*time.Location, error) {
	name := NormalizeTimestampTZ(tz)
	switch {
	case isHostTZSentinel(name):
		name = HostTimestampTZ()
	case name == "":
		name = DefaultTimestampTZ()
	}
	if name == "UTC" {
		return time.UTC, nil
	}
	locCacheMu.Lock()
	defer locCacheMu.Unlock()
	if loc, ok := locCache[name]; ok {
		return loc, nil
	}
	if loc, ok := parseFixedOffsetName(name); ok {
		locCache[name] = loc
		return loc, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	locCache[name] = loc
	return loc, nil
}

// ReinterpretAsTimezone treats ts's UTC clock face as wall time in loc, then returns real UTC.
// Example: 15:14:00Z with Asia/Kolkata → 09:44:00Z.
// If loc is nil or UTC, returns ts.UTC() unchanged.
func ReinterpretAsTimezone(ts time.Time, loc *time.Location) time.Time {
	if ts.IsZero() {
		return ts
	}
	u := ts.UTC()
	if loc == nil || loc == time.UTC {
		return u
	}
	y, m, d := u.Date()
	hh, mm, ss := u.Clock()
	return time.Date(y, m, d, hh, mm, ss, u.Nanosecond(), loc).UTC()
}

// CorrectMeasurementTime applies EffectiveTimestampTZ for this PMU config.
func (p *PMUConfig) CorrectMeasurementTime(ts time.Time) time.Time {
	loc, err := LoadLocation(p.EffectiveTimestampTZ())
	if err != nil {
		return ts.UTC()
	}
	return ReinterpretAsTimezone(ts, loc)
}
