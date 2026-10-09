//go:build windows

package monitoring

import (
	"fmt"
	"net"
	"syscall"
	"unsafe"

	winsys "golang.org/x/sys/windows"
)

// FIONREAD: how many bytes are queued in the socket receive buffer.
const fionread = 0x4004667f

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
		n    uint32
		cerr error
	)
	err = raw.Control(func(fd uintptr) {
		var bytesRet uint32
		cerr = winsys.WSAIoctl(
			winsys.Handle(fd),
			fionread,
			nil,
			0,
			(*byte)(unsafe.Pointer(&n)),
			uint32(unsafe.Sizeof(n)),
			&bytesRet,
			nil,
			0,
		)
	})
	if err != nil {
		return 0, err
	}
	if cerr != nil {
		return 0, cerr
	}
	return int(n), nil
}
