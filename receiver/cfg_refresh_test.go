package receiver

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestDataSTATHasCFGChange(t *testing.T) {
	raw := make([]byte, 18)
	raw[0] = syncByte
	raw[1] = frameTypeData | syncVersion
	binary.BigEndian.PutUint16(raw[2:], 18)
	// STAT at offset 14 with bit 13 set
	binary.BigEndian.PutUint16(raw[14:], 1<<13)
	if !dataSTATHasCFGChange(raw) {
		t.Fatal("expected CFG change bit")
	}
	binary.BigEndian.PutUint16(raw[14:], 0)
	if dataSTATHasCFGChange(raw) {
		t.Fatal("expected no CFG change")
	}
}

func TestCfgChangeWatchRequestOnce(t *testing.T) {
	var w cfgChangeWatch
	if !w.noteBit13() {
		t.Fatal("first note should be first")
	}
	if w.noteBit13() {
		t.Fatal("second note should not be first")
	}
	if w.shouldRequest() {
		t.Fatal("should not request before grace")
	}
	w.since = time.Now().Add(-cfgChangeGrace - time.Millisecond)
	if !w.shouldRequest() {
		t.Fatal("should request after grace")
	}
	w.markRequested()
	if w.shouldRequest() {
		t.Fatal("should not request twice")
	}
	w.clear()
	if w.shouldRequest() {
		t.Fatal("cleared watch must not request")
	}
}
