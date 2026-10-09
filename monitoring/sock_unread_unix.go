//go:build unix

package monitoring

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func connUnreadBytes(conn net.Conn) (int, error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return 0, fmt.Errorf("conn does not support SyscallConn")
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var (
		n    int
		cerr error
	)
	err = raw.Control(func(fd uintptr) {
		n, cerr = unix.IoctlGetInt(int(fd), unix.FIONREAD)
	})
	if err != nil {
		return 0, err
	}
	if cerr != nil {
		return 0, cerr
	}
	return n, nil
}
