package receiver

import (
	"bytes"
	"io"
	"testing"
	"time"
)

type delayedReader struct {
	rest       []byte
	firstDelay time.Duration
	started    bool
}

func (d *delayedReader) Read(p []byte) (int, error) {
	if !d.started {
		d.started = true
		if d.firstDelay > 0 {
			time.Sleep(d.firstDelay)
		}
	}
	if len(d.rest) == 0 {
		return 0, io.EOF
	}
	n := copy(p, d.rest)
	d.rest = d.rest[n:]
	return n, nil
}

func TestReadFrameTimed_WaitIsInterarrivalNotCopy(t *testing.T) {
	frame := buildCMDFrame(1, cmdSendCfg2)
	r := &delayedReader{rest: append([]byte(nil), frame...), firstDelay: 25 * time.Millisecond}
	got, err := readFrameTimed(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.raw, frame) {
		t.Fatalf("frame mismatch size=%d want=%d", len(got.raw), len(frame))
	}
	if got.wait < 20*time.Millisecond {
		t.Fatalf("wait=%s want >=20ms (blocked on first byte)", got.wait)
	}
	if got.copy > 8*time.Millisecond {
		t.Fatalf("copy=%s should be cheap once bytes are available", got.copy)
	}
}

func TestReadFrameTimed_NoDelayIsTinyWait(t *testing.T) {
	frame := buildCMDFrame(7, cmdDataOn)
	got, err := readFrameTimed(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	if got.wait > 5*time.Millisecond {
		t.Fatalf("wait=%s for in-memory reader", got.wait)
	}
	if got.copy > 5*time.Millisecond {
		t.Fatalf("copy=%s for in-memory reader", got.copy)
	}
}

func TestHdrWaitTimeoutDefault(t *testing.T) {
	t.Setenv("C37118_HDR_WAIT", "")
	if d := hdrWaitTimeout(); d != 200*time.Millisecond {
		t.Fatalf("default=%s want 200ms", d)
	}
	t.Setenv("C37118_HDR_WAIT", "150ms")
	if d := hdrWaitTimeout(); d != 150*time.Millisecond {
		t.Fatalf("override=%s", d)
	}
}
