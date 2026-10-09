package monitoring

import (
	"net"
	"testing"
)

func TestRegisterDataSocketTCPOnly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		done <- c
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-done
	defer server.Close()

	RegisterDataSocket("pmu-tcp", client, "tcp")
	defer UnregisterDataSocket("pmu-tcp", client)

	snap := PeekTCPUnread("pmu-tcp")
	if !snap.OK {
		t.Fatalf("expected OK peek on live TCP, got %#v", snap)
	}
	if snap.RecvBufMax != ConfiguredRecvBufBytes {
		t.Fatalf("recvBufMax=%d", snap.RecvBufMax)
	}
	if snap.UnreadBytes < 0 {
		t.Fatalf("unread=%d", snap.UnreadBytes)
	}

	// Push bytes the client has not Read yet → unread should rise on client.
	payload := make([]byte, 64)
	if _, err := server.Write(payload); err != nil {
		t.Fatal(err)
	}
	// Give the kernel a moment to queue.
	for i := 0; i < 50; i++ {
		snap = PeekTCPUnread("pmu-tcp")
		if snap.OK && snap.UnreadBytes >= 64 {
			break
		}
	}
	if !snap.OK || snap.UnreadBytes < 64 {
		t.Fatalf("want unread>=64 after write, got %#v", snap)
	}

	RegisterDataSocket("pmu-udp", client, "udp")
	defer UnregisterDataSocket("pmu-udp", client)
	udpSnap := PeekTCPUnread("pmu-udp")
	if udpSnap.OK {
		t.Fatal("UDP kind must not report TCP unread")
	}
}
