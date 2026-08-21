// Package receiver manages TCP connections to PMUs and implements the
// IEEE C37.118 connection handshake (request header → config-2 → start data).
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
	"pdc/parser"
)

// ─── IEEE C37.118 constants ───────────────────────────────────────────────────

const (
	syncByte = 0xAA // SYNC leading byte for all C37.118 frames

	// Frame type in SYNC bits 6-4 (mask with frameTypeMask). Version is bits 3-0.
	frameTypeMask = 0x70
	frameTypeData = 0x00
	frameTypeHdr  = 0x10
	frameTypeCfg1 = 0x20
	frameTypeCfg2 = 0x30
	frameTypeCmd  = 0x40
	frameTypeCfg3 = 0x50

	// C37.118.2-2011 version number in SYNC bits 3-0.
	syncVersion = 0x02

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
	buf[1] = frameTypeCmd | syncVersion // type=CMD (100), version=2
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
func readFrame(r io.Reader) ([]byte, error) {
	tf, err := readFrameTimed(r)
	return tf.raw, err
}

type timedFrame struct {
	raw  []byte
	wait time.Duration // blocking until first header byte (PMU inter-sample gap)
	copy time.Duration // first byte through CRC-verified complete frame
}

func (t timedFrame) total() time.Duration { return t.wait + t.copy }

// parseFrameBytes validates a complete C37.118 frame already in memory (UDP datagram).
func parseFrameBytes(pkt []byte, wait, copyDur time.Duration) (timedFrame, error) {
	if len(pkt) < 16 {
		return timedFrame{}, fmt.Errorf("datagram too short: %d (min 16)", len(pkt))
	}
	if pkt[0] != syncByte {
		return timedFrame{}, fmt.Errorf("invalid SYNC byte: 0x%02X", pkt[0])
	}
	frameSize := int(binary.BigEndian.Uint16(pkt[2:]))
	if frameSize < 16 {
		return timedFrame{}, fmt.Errorf("frame size too small: %d (min 16)", frameSize)
	}
	if len(pkt) < frameSize {
		return timedFrame{}, fmt.Errorf("datagram truncated: have %d want %d", len(pkt), frameSize)
	}
	buf := append([]byte(nil), pkt[:frameSize]...)
	want := binary.BigEndian.Uint16(buf[frameSize-2:])
	got := crc16(buf[:frameSize-2])
	if want != got {
		return timedFrame{}, fmt.Errorf("CRC mismatch: want 0x%04X got 0x%04X", want, got)
	}
	return timedFrame{raw: buf, wait: wait, copy: copyDur}, nil
}

// readUDPFrameTimed reads one full datagram then parses it as a single C37.118 frame.
func readUDPFrameTimed(conn net.Conn) (timedFrame, error) {
	buf := make([]byte, 65535)
	start := time.Now()
	n, err := conn.Read(buf)
	first := time.Now()
	if err != nil {
		return timedFrame{}, fmt.Errorf("read udp datagram: %w", err)
	}
	return parseFrameBytes(buf[:n], first.Sub(start), time.Since(first))
}

// readFrameTimed is readFrame with wait-vs-copy split.
// wait is time until the socket delivers the first byte; copy is the rest.
// UDP must read one datagram at a time — piecemeal ReadFull drops the rest of the packet.
func readFrameTimed(r io.Reader) (timedFrame, error) {
	if conn, ok := r.(net.Conn); ok {
		if _, isUDP := conn.(*net.UDPConn); isUDP {
			return readUDPFrameTimed(conn)
		}
	}

	start := time.Now()
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr[:1]); err != nil {
		return timedFrame{}, fmt.Errorf("read frame header: %w", err)
	}
	firstByte := time.Now()
	if _, err := io.ReadFull(r, hdr[1:]); err != nil {
		return timedFrame{}, fmt.Errorf("read frame header: %w", err)
	}
	if hdr[0] != syncByte {
		return timedFrame{}, fmt.Errorf("invalid SYNC byte: 0x%02X", hdr[0])
	}
	frameSize := int(binary.BigEndian.Uint16(hdr[2:]))
	if frameSize < 16 {
		return timedFrame{}, fmt.Errorf("frame size too small: %d (min 16)", frameSize)
	}

	buf := make([]byte, frameSize)
	copy(buf, hdr)
	if _, err := io.ReadFull(r, buf[4:]); err != nil {
		return timedFrame{}, fmt.Errorf("read frame body: %w", err)
	}
	done := time.Now()

	want := binary.BigEndian.Uint16(buf[frameSize-2:])
	got := crc16(buf[:frameSize-2])
	if want != got {
		return timedFrame{}, fmt.Errorf("CRC mismatch: want 0x%04X got 0x%04X", want, got)
	}
	return timedFrame{raw: buf, wait: firstByte.Sub(start), copy: done.Sub(firstByte)}, nil
}

// frameType returns the frame type field (SYNC bits 6-4).
func frameType(raw []byte) byte {
	if len(raw) < 2 {
		return 0xFF
	}
	return raw[1] & frameTypeMask
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
// Used only in direct mode (no raw Kafka ingress).
type FrameHandler func(pmuName string, raw []byte)

// RawFramePublisher publishes CRC-verified frames to the raw Kafka topic.
// Implementations must be safe for concurrent use from multiple PMU receivers.
type RawFramePublisher interface {
	Publish(ctx context.Context, pmuName, frameType string, idCode uint16, raw []byte, tcpWait, tcpCopy time.Duration) error
}

// Receiver manages a persistent, auto-reconnecting TCP connection to one PMU.
type Receiver struct {
	cfg     config.PMUConfig
	handler FrameHandler
	rawPub  RawFramePublisher
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
// handler, when set, parses/displays in-process (live path).
// rawPub, when set, also publishes CRC-verified frames to Kafka (durability).
// Both may be set together in mode=all so Kafka is not on the live hot path.
func New(cfg config.PMUConfig, handler FrameHandler, rawPub RawFramePublisher) *Receiver {
	return &Receiver{
		cfg:     cfg,
		handler: handler,
		rawPub:  rawPub,
		sem:     make(chan struct{}, frameHandlerMaxInflight()),
	}
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
	if r.cfg.NetworkProtocol() == "udp" {
		return r.connectUDP(ctx)
	}
	return r.connectTCP(ctx)
}

// connectUDP receives spontaneous / unicast UDP DATA.
// Port = local UDP listen port (matches "Remote UDP Port" in PMU Connection Tester).
// TCPPort = optional TCP command port (matches "Local TCP Port") for CFG2 + DATA_ON.
func (r *Receiver) connectUDP(ctx context.Context) error {
	timeout := r.cfg.Timeout()
	listenAddr := &net.UDPAddr{IP: net.IPv4zero, Port: r.cfg.Port}
	pc, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return fmt.Errorf("udp listen :%d: %w", r.cfg.Port, err)
	}
	defer pc.Close()
	_ = pc.SetReadBuffer(256 * 1024)

	log.Printf("[%s] UDP listening on %s (unicast DATA); source filter=%s",
		r.cfg.Name, pc.LocalAddr(), r.cfg.IP)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connect", "ok",
		fmt.Sprintf("udp listen %s", pc.LocalAddr()))
	monitoring.ObserveStage(r.cfg.Name, monitoring.StageDial, 0)

	tcpPort := r.cfg.TCPPort
	var tcpConn net.Conn
	if tcpPort > 0 {
		tcpAddr := fmt.Sprintf("%s:%d", r.cfg.IP, tcpPort)
		dialer := &net.Dialer{Timeout: timeout}
		dialStart := time.Now()
		tcpConn, err = dialer.DialContext(ctx, "tcp", tcpAddr)
		dialDur := time.Since(dialStart)
		monitoring.ObserveStage(r.cfg.Name, monitoring.StageDial, dialDur)
		if err != nil {
			return fmt.Errorf("tcp dial %s for UDP CFG: %w", tcpAddr, err)
		}
		defer tcpConn.Close()
		configureStreamConn(tcpConn)
		log.Printf("[%s] UDP mode: TCP control connected to %s in %s",
			r.cfg.Name, tcpAddr, monitoring.FormatMs(dialDur))

		if err := r.handshakeCFG(ctx, tcpConn, timeout); err != nil {
			return err
		}
		if err := r.sendCMD(tcpConn, cmdDataOn); err != nil {
			return fmt.Errorf("send CMD_DATA_ON: %w", err)
		}
		log.Printf("[%s] sent CMD_DATA_ON on TCP – expecting DATA on UDP :%d", r.cfg.Name, r.cfg.Port)
		monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "stream", "ok",
			fmt.Sprintf("DATA_ON via TCP :%d; listening UDP :%d", tcpPort, r.cfg.Port))

		// Drain TCP so the peer is not blocked if it also emits DATA there.
		go drainTCPQuiet(tcpConn)
	} else if _, ok := parser.GetProfile(r.cfg.Name); !ok {
		return fmt.Errorf("udp listen :%d needs tcp_port for CFG2 (set tcp_port to Connection Tester Local TCP Port), or connect TCP once first", r.cfg.Port)
	} else {
		log.Printf("[%s] UDP listen :%d using existing CFG2 profile (no tcp_port)", r.cfg.Name, r.cfg.Port)
	}

	buf := make([]byte, 65535)
	dataFrames := 0
	var lastComplete time.Time
	idleTimeout := timeout
	if idleTimeout < 15*time.Second {
		idleTimeout = 15 * time.Second
	}

	for {
		if ctx.Err() != nil {
			if tcpConn != nil {
				_ = r.sendCMD(tcpConn, cmdDataOff)
			}
			return nil
		}
		_ = pc.SetReadDeadline(time.Now().Add(idleTimeout))
		start := time.Now()
		n, src, err := pc.ReadFromUDP(buf)
		first := time.Now()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return fmt.Errorf("udp idle timeout on :%d (no datagrams from tester — check Remote UDP address/port and that Start is running)", r.cfg.Port)
			}
			return fmt.Errorf("udp read :%d: %w", r.cfg.Port, err)
		}
		if !udpSourceAllowed(r.cfg.IP, src) {
			continue
		}
		tf, err := parseFrameBytes(buf[:n], first.Sub(start), time.Since(first))
		if err != nil {
			log.Printf("[%s] udp bad datagram from %s: %v", r.cfg.Name, src, err)
			continue
		}
		completeAt := time.Now()
		ft := frameType(tf.raw)
		switch ft {
		case frameTypeCfg2:
			if profile, perr := parser.ParseCFG2Frame(tf.raw); perr == nil {
				parser.SetProfile(r.cfg.Name, profile)
				log.Printf("[%s] UDP CFG2 registered station=%q", r.cfg.Name, profile.Station)
			}
			_ = r.publishRaw(ctx, "cfg2", tf.raw, 0, 0)
		case frameTypeData:
			monitoring.ObserveStage(r.cfg.Name, monitoring.StageTCPWait, tf.wait)
			monitoring.ObserveStage(r.cfg.Name, monitoring.StageTCPCopy, tf.copy)
			monitoring.ObserveStage(r.cfg.Name, monitoring.StageTCPRead, tf.total())
			if !lastComplete.IsZero() {
				monitoring.ObserveStage(r.cfg.Name, monitoring.StageTCPInterarrival, completeAt.Sub(lastComplete))
			}
			lastComplete = completeAt
			dataFrames++
			if dataFrames <= 3 {
				logFrameTrace(r.cfg.Name, fmt.Sprintf("udp rx #%d from=%s wait=%s", dataFrames, src, monitoring.FormatMs(tf.wait)), tf.raw)
			}
			payload := append([]byte(nil), tf.raw...)
			r.dispatchDataFrame(ctx, payload, tf.wait, tf.copy)
		default:
			if dataFrames < 3 {
				log.Printf("[%s] udp skip %s from %s", r.cfg.Name, frameTypeName(ft), src)
			}
		}
	}
}

func udpSourceAllowed(wantIP string, src *net.UDPAddr) bool {
	if src == nil {
		return true
	}
	wantIP = strings.TrimSpace(wantIP)
	if wantIP == "" || wantIP == "0.0.0.0" || wantIP == "::" {
		return true
	}
	// Accept loopback aliases when configured as 127.0.0.1.
	if wantIP == "127.0.0.1" || wantIP == "localhost" {
		return src.IP.IsLoopback()
	}
	return src.IP.Equal(net.ParseIP(wantIP))
}

func drainTCPQuiet(conn net.Conn) {
	buf := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
	}
}

// handshakeCFG runs HDR (optional) + CFG2 on a TCP control connection.
func (r *Receiver) handshakeCFG(ctx context.Context, conn net.Conn, timeout time.Duration) error {
	handshakeStart := time.Now()
	_ = r.sendCMD(conn, cmdDataOff)

	headerText := ""
	log.Printf("[%s] handshake step 1: sending %s", r.cfg.Name, cmdName(cmdSendHdr))
	if err := r.sendCMD(conn, cmdSendHdr); err != nil {
		return fmt.Errorf("send HDR request: %w", err)
	}
	hdrDeadline := hdrWaitTimeout()
	if timeout > 0 && timeout < hdrDeadline {
		hdrDeadline = timeout
	}
	hdrWaitStart := time.Now()
	if hdrRaw, err := readFrameOfType(conn, frameTypeHdr, hdrDeadline); err != nil {
		hdrDur := time.Since(hdrWaitStart)
		monitoring.ObserveStage(r.cfg.Name, monitoring.StageHandshakeHDR, hdrDur)
		log.Printf("[%s] optional HEADER not received after %s (%v) – continuing with CFG2",
			r.cfg.Name, monitoring.FormatMs(hdrDur), err)
	} else {
		hdrDur := time.Since(hdrWaitStart)
		monitoring.ObserveStage(r.cfg.Name, monitoring.StageHandshakeHDR, hdrDur)
		if text, perr := parser.ParseHeaderFrame(hdrRaw); perr == nil {
			headerText = text
		}
		_ = r.publishRaw(ctx, "hdr", hdrRaw, 0, 0)
	}

	log.Printf("[%s] handshake step 2: sending %s", r.cfg.Name, cmdName(cmdSendCfg2))
	if err := r.sendCMD(conn, cmdSendCfg2); err != nil {
		return fmt.Errorf("send CFG2 request: %w", err)
	}
	cfgWaitStart := time.Now()
	cfg2, err := readFrameOfType(conn, frameTypeCfg2, timeout)
	if err != nil {
		return fmt.Errorf("read CFG2 frame: %w", err)
	}
	cfgDur := time.Since(cfgWaitStart)
	monitoring.ObserveStage(r.cfg.Name, monitoring.StageHandshakeCFG2, cfgDur)
	logFrameTrace(r.cfg.Name, "handshake CFG2", cfg2)
	decodeCFG2Details(r.cfg.Name, cfg2)
	if profile, err := parser.ParseCFG2Frame(cfg2); err != nil {
		return fmt.Errorf("parse CFG2: %w", err)
	} else {
		profile.HeaderText = headerText
		parser.SetProfile(r.cfg.Name, profile)
		log.Printf("[%s] registered CFG2 profile: station=%q rate=%d", r.cfg.Name, profile.Station, profile.DataRate)
	}
	if err := r.publishRaw(ctx, "cfg2", cfg2, 0, 0); err != nil {
		return fmt.Errorf("publish CFG2 to kafka: %w", err)
	}
	monitoring.ObserveStage(r.cfg.Name, monitoring.StageHandshakeTotal, time.Since(handshakeStart))
	return nil
}

func (r *Receiver) connectTCP(ctx context.Context) error {
	addr := r.cfg.Addr()
	proto := r.cfg.NetworkProtocol()
	timeout := r.cfg.Timeout()

	log.Printf("[%s] connecting to %s (%s) …", r.cfg.Name, addr, proto)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connect", "info", fmt.Sprintf("dial %s (%s)", addr, proto))

	dialer := &net.Dialer{Timeout: timeout}
	dialStart := time.Now()
	conn, err := dialer.DialContext(ctx, proto, addr)
	dialDur := time.Since(dialStart)
	if err != nil {
		monitoring.ObserveStage(r.cfg.Name, monitoring.StageDial, dialDur)
		return fmt.Errorf("dial %s %s: %w", proto, addr, err)
	}
	defer conn.Close()

	configureStreamConn(conn)

	monitoring.ObserveStage(r.cfg.Name, monitoring.StageDial, dialDur)
	log.Printf("[%s] connected to %s in %s", r.cfg.Name, addr, monitoring.FormatMs(dialDur))
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connect", "ok",
		fmt.Sprintf("connected to %s in %s", addr, monitoring.FormatMs(dialDur)))

	if err := r.handshakeCFG(ctx, conn, timeout); err != nil {
		return err
	}

	log.Printf("[%s] handshake step 3: sending %s", r.cfg.Name, cmdName(cmdDataOn))
	if err := r.sendCMD(conn, cmdDataOn); err != nil {
		return fmt.Errorf("send CMD_DATA_ON: %w", err)
	}
	log.Printf("[%s] sent CMD_DATA_ON – streaming data", r.cfg.Name)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "stream", "ok", "sent CMD_DATA_ON")

	dataFrames := 0
	var lastComplete time.Time

	for {
		if ctx.Err() != nil {
			_ = r.sendCMD(conn, cmdDataOff)
			return nil
		}

		if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}

		tf, err := readFrameTimed(conn)
		completeAt := time.Now()
		if err != nil {
			if strings.Contains(err.Error(), "CRC mismatch") {
				monitoring.NoteFrameCRCFail(r.cfg.Name, err.Error(), nil)
			}
			if strings.Contains(err.Error(), "EOF") {
				return fmt.Errorf("read data frame: %w (peer closed — close other PDC / Connection Tester DATA client)", err)
			}
			return fmt.Errorf("read data frame: %w", err)
		}

		if frameType(tf.raw) == frameTypeData {
			monitoring.ObserveStage(r.cfg.Name, monitoring.StageTCPWait, tf.wait)
			monitoring.ObserveStage(r.cfg.Name, monitoring.StageTCPCopy, tf.copy)
			monitoring.ObserveStage(r.cfg.Name, monitoring.StageTCPRead, tf.total())
			if !lastComplete.IsZero() {
				monitoring.ObserveStage(r.cfg.Name, monitoring.StageTCPInterarrival, completeAt.Sub(lastComplete))
			}
			lastComplete = completeAt

			monitoring.NoteFrameTCPComplete(r.cfg.Name)
			dataFrames++
			if dataFrames <= 3 {
				logFrameTrace(r.cfg.Name, fmt.Sprintf("stream rx #%d wait=%s copy=%s", dataFrames, monitoring.FormatMs(tf.wait), monitoring.FormatMs(tf.copy)), tf.raw)
			}
			if dataFrames%50 == 0 {
				monitoring.RecordConversation(r.cfg.Name, "PMU", "PDC", "stream", "ok", fmt.Sprintf("received %d data frames", dataFrames))
			}
			payload := append([]byte(nil), tf.raw...)
			r.dispatchDataFrame(ctx, payload, tf.wait, tf.copy)
		}
	}
}

// publishRaw sends a frame to the raw Kafka topic when ingress publishing is enabled.
func (r *Receiver) dispatchDataFrame(ctx context.Context, payload []byte, tcpWait, tcpCopy time.Duration) {
	if r.handler != nil {
		select {
		case r.sem <- struct{}{}:
			frame := append([]byte(nil), payload...)
			go func(name string, frame []byte) {
				defer func() { <-r.sem }()
				r.handler(name, frame)
			}(r.cfg.Name, frame)
		default:
			monitoring.IncFramesDropped()
			monitoring.NoteFrameHandlerDrop(r.cfg.Name, payload)
			log.Printf("[%s] frame dropped: handler pool full (inflight=%d)", r.cfg.Name, len(r.sem))
			monitoring.RecordConversation(r.cfg.Name, "PDC", "PDC", "overload", "warn",
				fmt.Sprintf("frame dropped – handler pool full (inflight=%d/%d)", len(r.sem), cap(r.sem)))
		}
	}

	if r.rawPub == nil {
		return
	}
	if err := r.publishRaw(ctx, "data", payload, tcpWait, tcpCopy); err != nil {
		monitoring.IncQueuePublishErrors()
		monitoring.IncKafkaErrorForPMU(r.cfg.Name)
		log.Printf("[%s] raw kafka publish error: %v", r.cfg.Name, err)
		monitoring.RecordConversation(r.cfg.Name, "PDC", "KAFKA", "raw-publish", "error", err.Error())
	}
}

func (r *Receiver) publishRaw(ctx context.Context, frameType string, raw []byte, tcpWait, tcpCopy time.Duration) error {
	if r.rawPub == nil {
		return nil
	}
	monitoring.IncRawFramesPublished()
	return r.rawPub.Publish(ctx, r.cfg.Name, frameType, r.cfg.IDCode, raw, tcpWait, tcpCopy)
}

func hdrWaitTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("C37118_HDR_WAIT"))
	if v == "" {
		return 200 * time.Millisecond
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 200 * time.Millisecond
	}
	return d
}

func configureStreamConn(conn net.Conn) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tcp.SetNoDelay(true)
	_ = tcp.SetReadBuffer(256 * 1024)
	_ = tcp.SetWriteBuffer(64 * 1024)
}

// readFrameOfType reads frames until one matching wantType arrives, skipping
// DATA frames (common when a prior session left transmission enabled).
func readFrameOfType(conn net.Conn, wantType byte, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	skipped := 0
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return nil, fmt.Errorf("timeout waiting for %s (skipped %d data frames)", frameTypeName(wantType), skipped)
		}
		if err := conn.SetReadDeadline(time.Now().Add(remain)); err != nil {
			return nil, err
		}
		raw, err := readFrame(conn)
		if err != nil {
			return nil, err
		}
		ft := frameType(raw)
		if ft == wantType {
			return raw, nil
		}
		if ft == frameTypeData {
			skipped++
			continue
		}
		return nil, fmt.Errorf("expected %s, got %s", frameTypeName(wantType), frameTypeName(ft))
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