package instance

import (
	"fmt"
	"net"
	"strings"
)

// ListenOrExit binds addr and returns the listener, or an error if the port is taken.
func ListenOrExit(component, addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%s listen %s: %w (another PDC or process may already own this port)", component, addr, err)
	}
	return ln, nil
}

// NormalizeAddr ensures host:port forms like ":2112" are valid for net.Listen.
func NormalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, ":") {
		return "0.0.0.0" + addr
	}
	return addr
}
