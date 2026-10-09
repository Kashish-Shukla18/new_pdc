package monitoring

// sock_unread.go — track each PMU's live DATA socket and report OS unread bytes.
//
// This is the kernel receive queue (bytes arrived, not yet Read by the PDC).
// Plotting / aligner paths are untouched — we only peek with FIONREAD.

import (
	"net"
	"sync"
)

// ConfiguredRecvBufBytes matches receiver.configureStreamConn SetReadBuffer.
const ConfiguredRecvBufBytes = 256 * 1024

type dataSock struct {
	conn net.Conn
	kind string // "tcp" or "udp"
}

var dataSocks sync.Map // pmu name → *dataSock

// RegisterDataSocket remembers the live DATA path socket for unread peeking.
// kind is "tcp" or "udp". Call UnregisterDataSocket when the session ends.
func RegisterDataSocket(name string, conn net.Conn, kind string) {
	if name == "" || conn == nil {
		return
	}
	if kind == "" {
		kind = "tcp"
	}
	dataSocks.Store(name, &dataSock{conn: conn, kind: kind})
}

// UnregisterDataSocket clears the registry entry if it still points at conn.
func UnregisterDataSocket(name string, conn net.Conn) {
	if name == "" {
		return
	}
	v, ok := dataSocks.Load(name)
	if !ok {
		return
	}
	ds := v.(*dataSock)
	if conn == nil || ds.conn == conn {
		dataSocks.Delete(name)
	}
}

// TCPUnreadSnapshot is one PMU's kernel TCP receive-queue peek (bytes, not frames).
type TCPUnreadSnapshot struct {
	UnreadBytes int    `json:"unreadBytes"`
	RecvBufMax  int    `json:"recvBufMax"`
	Kind        string `json:"kind"` // "tcp" when meaningful for the TCP column
	OK          bool   `json:"ok"`
}

// PeekTCPUnread returns OS unread bytes for a TCP DATA socket.
// UDP DATA streams return OK=false (no TCP receive queue on the DATA path).
func PeekTCPUnread(name string) TCPUnreadSnapshot {
	out := TCPUnreadSnapshot{RecvBufMax: ConfiguredRecvBufBytes}
	v, ok := dataSocks.Load(name)
	if !ok {
		return out
	}
	ds := v.(*dataSock)
	out.Kind = ds.kind
	if ds.kind != "tcp" || ds.conn == nil {
		return out
	}
	n, err := connUnreadBytes(ds.conn)
	if err != nil || n < 0 {
		return out
	}
	out.UnreadBytes = n
	out.OK = true
	return out
}

// PeekAllTCPUnread snapshots every registered name (for tests / dumps).
func PeekAllTCPUnread() map[string]TCPUnreadSnapshot {
	out := make(map[string]TCPUnreadSnapshot)
	dataSocks.Range(func(k, _ any) bool {
		name, _ := k.(string)
		out[name] = PeekTCPUnread(name)
		return true
	})
	return out
}
