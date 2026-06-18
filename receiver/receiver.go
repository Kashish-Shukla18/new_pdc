// Package receiver manages TCP connections to PMUs and implements the
// IEEE C37.118 connection handshake (request config-2 frame → start data).
package receiver

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"pdc/config"
	"pdc/monitoring"
)

// ─── IEEE C37.118 constants ───────────────────────────────────────────────────

const (
	syncByte = 0xAA // SYNC leading byte for all C37.118 frames

	// Frame type nibbles (upper nibble of FRAMETYP byte)
	frameTypeData = 0x00
	frameTypeHdr  = 0x10
	frameTypeCfg1 = 0x20
	frameTypeCfg2 = 0x30
	frameTypeCmd  = 0x41
	frameTypeCfg3 = 0x50

	// CMD word values (sent inside a CMD frame)
	cmdDataOff  uint16 = 0x0001 // turn off data transmission
	cmdDataOn   uint16 = 0x0002 // turn on data transmission
	cmdSendHdr  uint16 = 0x0003 // send header frame
	cmdSendCfg1 uint16 = 0x0004 // send config-1 frame
	cmdSendCfg2 uint16 = 0x0005 // send config-2 frame
	cmdSendCfg3 uint16 = 0x0006 // send config-3 frame
)

// ─── Frame helpers ────────────────────────────────────────────────────────────

// crc16 computes the CRC-CCITT (0xFFFF) used by C37.118.
func crc16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// buildCMDFrame constructs a C37.118 CMD frame for the given idcode and command.
//
//	SYNC(2) | FRAMESIZE(2) | IDCODE(2) | SOC(4) | FRACSEC(4) | CMD(2) | CHK(2)
func buildCMDFrame(idcode uint16, cmd uint16) []byte {
	const frameSize = 18
	buf := make([]byte, frameSize)

	buf[0] = syncByte
	buf[1] = frameTypeCmd // frame type = CMD
	binary.BigEndian.PutUint16(buf[2:], frameSize)
	binary.BigEndian.PutUint16(buf[4:], idcode)

	now := uint32(time.Now().Unix())
	binary.BigEndian.PutUint32(buf[6:], now)   // SOC
	binary.BigEndian.PutUint32(buf[10:], 0x00) // FRACSEC (quality = 0)
	binary.BigEndian.PutUint16(buf[14:], cmd)

	chk := crc16(buf[:frameSize-2])
	binary.BigEndian.PutUint16(buf[frameSize-2:], chk)

	return buf
}

// readFrame reads one complete C37.118 frame from r, returning the raw bytes.
// It reads the fixed 4-byte header first to learn FRAMESIZE, then reads the rest.
func readFrame(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, fmt.Errorf("read frame header: %w", err)
	}
	if hdr[0] != syncByte {
		return nil, fmt.Errorf("invalid SYNC byte: 0x%02X", hdr[0])
	}
	frameSize := int(binary.BigEndian.Uint16(hdr[2:]))
	if frameSize < 4 {
		return nil, fmt.Errorf("frame size too small: %d", frameSize)
	}

	buf := make([]byte, frameSize)
	copy(buf, hdr)
	if _, err := io.ReadFull(r, buf[4:]); err != nil {
		return nil, fmt.Errorf("read frame body: %w", err)
	}

	// Verify CRC.
	want := binary.BigEndian.Uint16(buf[frameSize-2:])
	got := crc16(buf[:frameSize-2])
	if want != got {
		return nil, fmt.Errorf("CRC mismatch: want 0x%04X got 0x%04X", want, got)
	}
	return buf, nil
}

// frameType returns the FRAMETYP nibble of a raw frame.
func frameType(raw []byte) byte {
	if len(raw) < 2 {
		return 0xFF
	}
	return raw[1] & 0xF0
}

func frameTypeName(frameType byte) string {
	switch frameType {
	case frameTypeData:
		return "DATA"
	case frameTypeHdr:
		return "HEADER"
	case frameTypeCfg1:
		return "CFG1"
	case frameTypeCfg2:
		return "CFG2"
	case frameTypeCmd:
		return "CMD"
	case frameTypeCfg3:
		return "CFG3"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02X)", frameType)
	}
}

func cmdName(cmd uint16) string {
	switch cmd {
	case cmdDataOff:
		return "CMD_DATA_OFF"
	case cmdDataOn:
		return "CMD_DATA_ON"
	case cmdSendHdr:
		return "CMD_SEND_HDR"
	case cmdSendCfg1:
		return "CMD_SEND_CFG1"
	case cmdSendCfg2:
		return "CMD_SEND_CFG2"
	case cmdSendCfg3:
		return "CMD_SEND_CFG3"
	default:
		return fmt.Sprintf("CMD_UNKNOWN(0x%04X)", cmd)
	}
}

func logFrameTrace(pmuName, stage string, raw []byte) {
	if len(raw) < 16 {
		log.Printf("[%s] %s short frame: %d bytes", pmuName, stage, len(raw))
		return
	}

	syncWord := binary.BigEndian.Uint16(raw[0:2])
	frameSize := binary.BigEndian.Uint16(raw[2:4])
	idCode := binary.BigEndian.Uint16(raw[4:6])
	soc := binary.BigEndian.Uint32(raw[6:10])
	fracsec := binary.BigEndian.Uint32(raw[10:14])
	checksum := binary.BigEndian.Uint16(raw[len(raw)-2:])
	timeQuality := byte(fracsec >> 24)
	fracCount := fracsec & 0x00FFFFFF

	log.Printf("[%s] %s frame: sync=0x%04X type=%s size=%d idcode=%d soc=%d fracsec=0x%08X time_quality=0x%02X frac_count=%d checksum=0x%04X",
		pmuName,
		stage,
		syncWord,
		frameTypeName(frameType(raw)),
		frameSize,
		idCode,
		soc,
		fracsec,
		timeQuality,
		fracCount,
		checksum,
	)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func trimASCII(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == 0) {
		s = s[:len(s)-1]
	}
	return s
}

func decodeCFG2Details(pmuName string, raw []byte) {
	if len(raw) < 16 {
		return
	}
	p := raw[14 : len(raw)-2]
	if len(p) < 34 {
		log.Printf("[%s] cfg2 decode: payload too short (%d)", pmuName, len(p))
		return
	}

	o := 0
	timeBase := binary.BigEndian.Uint32(p[o : o+4])
	o += 4
	numPMU := binary.BigEndian.Uint16(p[o : o+2])
	o += 2
	if numPMU < 1 {
		log.Printf("[%s] cfg2 decode: NUM_PMU=%d", pmuName, numPMU)
		return
	}
	if len(p) < o+26 {
		log.Printf("[%s] cfg2 decode: missing PMU block", pmuName)
		return
	}

	station := trimASCII(p[o : o+16])
	o += 16
	idCode := binary.BigEndian.Uint16(p[o : o+2])
	o += 2
	format := binary.BigEndian.Uint16(p[o : o+2])
	o += 2
	phnmr := binary.BigEndian.Uint16(p[o : o+2])
	o += 2
	annmr := binary.BigEndian.Uint16(p[o : o+2])
	o += 2
	dgnmr := binary.BigEndian.Uint16(p[o : o+2])
	o += 2

	chnCount := int(phnmr) + int(annmr) + int(16*dgnmr)
	chnBytes := chnCount * 16
	if len(p) < o+chnBytes {
		log.Printf("[%s] cfg2 decode: channel labels truncated", pmuName)
		return
	}
	channelNames := make([]string, 0, minInt(8, chnCount))
	for i := 0; i < chnCount; i++ {
		name := trimASCII(p[o+i*16 : o+(i+1)*16])
		if i < 8 {
			channelNames = append(channelNames, name)
		}
	}
	o += chnBytes

	unitsBytes := int(phnmr)*4 + int(annmr)*4
	if len(p) < o+unitsBytes {
		log.Printf("[%s] cfg2 decode: unit block truncated", pmuName)
		return
	}
	o += unitsBytes

	var digNormal, digValid uint16
	if dgnmr > 0 {
		if len(p) < o+4 {
			log.Printf("[%s] cfg2 decode: digital unit block truncated", pmuName)
			return
		}
		digNormal = binary.BigEndian.Uint16(p[o : o+2])
		digValid = binary.BigEndian.Uint16(p[o+2 : o+4])
		o += int(dgnmr) * 4
	}

	if len(p) < o+6 {
		log.Printf("[%s] cfg2 decode: fnom/cfgcnt/rate block truncated", pmuName)
		return
	}
	fnomWord := binary.BigEndian.Uint16(p[o : o+2])
	o += 2
	cfgCnt := binary.BigEndian.Uint16(p[o : o+2])
	o += 2
	dataRate := int16(binary.BigEndian.Uint16(p[o : o+2]))

	fnomHz := 60
	if fnomWord&0x0001 == 1 {
		fnomHz = 50
	}

	phasorFloat := (format & 0x0002) != 0
	phasorRect := (format & 0x0001) == 0
	analogFloat := (format & 0x0004) != 0
	freqFloat := (format & 0x0008) != 0

	log.Printf("[%s] cfg2 details: station=%q idcode=%d time_base=%d num_pmu=%d fnom=%dHz data_rate=%d cfgcnt=%d",
		pmuName, station, idCode, timeBase, numPMU, fnomHz, dataRate, cfgCnt)
	log.Printf("[%s] cfg2 format: phasor_float=%t phasor_rect=%t analog_float=%t freq_float=%t phnmr=%d annmr=%d dgnmr=%d",
		pmuName, phasorFloat, phasorRect, analogFloat, freqFloat, phnmr, annmr, dgnmr)
	if dgnmr > 0 {
		log.Printf("[%s] cfg2 digital masks: normal=0x%04X valid=0x%04X", pmuName, digNormal, digValid)
	}
	if len(channelNames) > 0 {
		log.Printf("[%s] cfg2 first channel labels: %v", pmuName, channelNames)
	}
}

// ─── Receiver ────────────────────────────────────────────────────────────────

// FrameHandler is called for every data frame received from the PMU.
// raw contains the complete, CRC-verified C37.118 frame bytes.
type FrameHandler func(pmuName string, raw []byte)

// Receiver manages a persistent, auto-reconnecting TCP connection to one PMU.
type Receiver struct {
	cfg     config.PMUConfig
	handler FrameHandler
	sem     chan struct{}
}

func frameHandlerMaxInflight() int {
	v := strings.TrimSpace(os.Getenv("FRAME_HANDLER_MAX_INFLIGHT"))
	if v == "" {
		return 128
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 128
	}
	return n
}

// New creates a Receiver for the given PMU configuration.
func New(cfg config.PMUConfig, handler FrameHandler) *Receiver {
	return &Receiver{cfg: cfg, handler: handler, sem: make(chan struct{}, frameHandlerMaxInflight())}
}

// Run connects to the PMU and streams data frames until ctx is cancelled.
// On any connection or protocol error it waits ReconnectInterval and retries.
func (r *Receiver) Run(ctx context.Context) {
	for {
		if err := r.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return // context cancelled – exit cleanly
			}
			log.Printf("[%s] connection error: %v – reconnecting in %s",
				r.cfg.Name, err, r.cfg.ReconnectInterval())
			monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connection", "error", err.Error())
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(r.cfg.ReconnectInterval()):
		}
	}
}

// connect performs the full C37.118 connection handshake and reads data frames
// until an error occurs or ctx is cancelled.
func (r *Receiver) connect(ctx context.Context) error {
	addr := r.cfg.Addr()
	proto := r.cfg.NetworkProtocol()
	timeout := r.cfg.Timeout()

	log.Printf("[%s] connecting to %s (%s) …", r.cfg.Name, addr, proto)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connect", "info", fmt.Sprintf("dial %s (%s)", addr, proto))

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, proto, addr)
	if err != nil {
		return fmt.Errorf("dial %s %s: %w", proto, addr, err)
	}
	defer conn.Close()

	log.Printf("[%s] connected to %s", r.cfg.Name, addr)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connect", "ok", fmt.Sprintf("connected to %s", addr))

	// ── Step 1: request Config-2 frame ───────────────────────────────────────
	log.Printf("[%s] handshake step 1: sending %s", r.cfg.Name, cmdName(cmdSendCfg2))
	if err := r.sendCMD(conn, cmdSendCfg2); err != nil {
		return fmt.Errorf("send CFG2 request: %w", err)
	}
	log.Printf("[%s] sent CMD_SEND_CFG2", r.cfg.Name)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "handshake", "ok", "sent CMD_SEND_CFG2")

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set read deadline: %w", err)
	}
	cfg2, err := readFrame(conn)
	if err != nil {
		return fmt.Errorf("read CFG2 frame: %w", err)
	}
	if frameType(cfg2) != frameTypeCfg2 {
		return fmt.Errorf("expected CFG2 frame (0x%02X), got 0x%02X",
			frameTypeCfg2, frameType(cfg2))
	}
	logFrameTrace(r.cfg.Name, "handshake step 1 rx", cfg2)
	decodeCFG2Details(r.cfg.Name, cfg2)
	log.Printf("[%s] received CFG2 frame (%d bytes)", r.cfg.Name, len(cfg2))
	monitoring.RecordConversation(r.cfg.Name, "PMU", "PDC", "handshake", "ok", fmt.Sprintf("received CFG2 frame (%d bytes)", len(cfg2)))

	// ── Step 2: start data transmission ──────────────────────────────────────
	log.Printf("[%s] handshake step 2: sending %s", r.cfg.Name, cmdName(cmdDataOn))
	if err := r.sendCMD(conn, cmdDataOn); err != nil {
		return fmt.Errorf("send CMD_DATA_ON: %w", err)
	}
	log.Printf("[%s] sent CMD_DATA_ON – streaming data", r.cfg.Name)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "stream", "ok", "sent CMD_DATA_ON")

	dataFrames := 0

	// ── Step 3: stream data frames ────────────────────────────────────────────
	for {
		if ctx.Err() != nil {
			// Politely stop data before closing.
			_ = r.sendCMD(conn, cmdDataOff)
			return nil
		}

		if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}

		raw, err := readFrame(conn)
		if err != nil {
			return fmt.Errorf("read data frame: %w", err)
		}

		if frameType(raw) == frameTypeData {
			dataFrames++
			if dataFrames <= 3 {
				logFrameTrace(r.cfg.Name, fmt.Sprintf("stream rx #%d", dataFrames), raw)
			}
			if dataFrames%50 == 0 {
				monitoring.RecordConversation(r.cfg.Name, "PMU", "PDC", "stream", "ok", fmt.Sprintf("received %d data frames", dataFrames))
			}
			payload := append([]byte(nil), raw...)

			// Non-blocking semaphore acquisition: if the handler pool is full, drop
			// this frame and record a metric rather than stalling the TCP read loop.
			// Stalling the read loop causes OS TCP buffers to fill, which eventually
			// makes the PMU retransmit or disconnect.
			select {
			case r.sem <- struct{}{}:
				go func(name string, frame []byte) {
					defer func() { <-r.sem }()
					r.handler(name, frame)
				}(r.cfg.Name, payload)
			default:
				monitoring.IncFramesDropped()
				log.Printf("[%s] frame dropped: handler pool full (inflight=%d)", r.cfg.Name, len(r.sem))
				monitoring.RecordConversation(r.cfg.Name, "PDC", "PDC", "overload", "warn",
					fmt.Sprintf("frame dropped – handler pool full (inflight=%d/%d)", len(r.sem), cap(r.sem)))
			}
		}
	}
}

// sendCMD writes a C37.118 CMD frame to the connection.
func (r *Receiver) sendCMD(conn net.Conn, cmd uint16) error {
	frame := buildCMDFrame(r.cfg.IDCode, cmd)
	logFrameTrace(r.cfg.Name, fmt.Sprintf("tx %s", cmdName(cmd)), frame)
	log.Printf("[%s] command payload: name=%s word=0x%04X", r.cfg.Name, cmdName(cmd), cmd)
	if err := conn.SetWriteDeadline(time.Now().Add(r.cfg.Timeout())); err != nil {
		return err
	}
	_, err := conn.Write(frame)
	return err
}
