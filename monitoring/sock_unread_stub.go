//go:build !windows && !unix

package monitoring

import (
	"fmt"
	"net"
)

func connUnreadBytes(conn net.Conn) (int, error) {
	_ = conn
	return 0, fmt.Errorf("socket unread peek not supported on this OS")
}
