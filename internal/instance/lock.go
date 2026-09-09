// Package instance ensures only one PDC process runs on a host.
package instance

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// Acquire binds a dedicated loopback TCP port for the lifetime of this process.
// A second PDC exits immediately with a clear error instead of competing for PMU sockets.
func Acquire() (release func(), err error) {
	addr := strings.TrimSpace(os.Getenv("PDC_INSTANCE_LOCK"))
	if addr == "" {
		addr = "127.0.0.1:21119"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("another PDC instance is already running (lock %s): %w — stop other pdc.exe / go run . processes first", addr, err)
	}
	return func() { _ = ln.Close() }, nil
}
