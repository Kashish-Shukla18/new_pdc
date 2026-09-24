// Package receiver — one phone line to one PMU.
//
// # Big picture (read this first)
//
// A PMU (Phasor Measurement Unit) is a device that measures the power grid
// many times per second. This file is the "phone call" code: we call the PMU,
// ask it how its data is shaped, tell it to start talking, then catch every
// measurement packet and hand it to the rest of the PDC.
//
// Think of it like ordering pizza over the phone:
//
//  1. DIAL     — call the PMU's IP:port (TCP or UDP)
//  2. ASK MENU — send "please send Config Frame 2" (CFG2 = the recipe for DATA)
//  3. GET MENU — wait until CFG2 arrives; save it in RAM (parser.SetProfile)
//  4. SAY GO   — send "turn data ON"
//  5. LISTEN   — read DATA frames forever; on hang-up, wait and redial
//
// # Protocol choice (config field "protocol")
//
//   - "tcp" (default, used by pmu-1/2/3 and most lab sims)
//       → connectTCP: everything on one TCP socket
//   - "udp"
//       → connectUDPDial  (no tcp_port): commands + DATA on one UDP socket
//       → connectUDPListen (tcp_port set): commands on TCP, DATA on UDP listen
//
// # File map
//
//   §1  Constants & frame helpers — frame types, CRC, build/read one frame
//   §2  Receiver lifecycle        — Run loop = dial, on error sleep & retry
//   §3  UDP path                  — dial mode & listen mode
//   §4  TCP path                  — connectTCP + handshakeCFG + DATA loop
//   §5  Shared send / wait tools  — sendCMD, readFrameOfType, UDP handshake
//
package receiver

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"pdc/config"
	"pdc/monitoring"
	"pdc/parser"
)

// ═══════════════════════════════════════════════════════════════════════════════
// §1  CONSTANTS & FRAME HELPERS
//     "What does an IEEE C37.118 packet look like?"
//     Every packet starts with 0xAA. The next byte says DATA / CFG2 / CMD / …
// ═══════════════════════════════════════════════════════════════════════════════

const (
	syncByte = 0xAA // first byte of every valid frame — like a "start here" flag

	// Frame type lives in SYNC bits 6-4. Version lives in bits 3-0.
	frameTypeMask = 0x70
	frameTypeData = 0x00 // measurement sample (what we stream forever)
	frameTypeHdr  = 0x10 // optional human-readable header text
	frameTypeCfg1 = 0x20 // "everything this PMU can do"
	frameTypeCfg2 = 0x30 // "what it is sending right now" ← we need this
	frameTypeCmd  = 0x40 // our orders TO the PMU
	frameTypeCfg3 = 0x50 // newer optional config (we rarely use)

	// C37.118.2-2011 version number in SYNC bits 3-0.
	syncVersion = 0x02

	// Command words inside a CMD frame (the "order" we write on the pizza slip)
	cmdDataOff  uint16 = 0x0001 // stop sending DATA
	cmdDataOn   uint16 = 0x0002 // start sending DATA
	cmdSendHdr  uint16 = 0x0003 // please send HEADER
	cmdSendCfg1 uint16 = 0x0004 // please send CFG-1
	cmdSendCfg2 uint16 = 0x0005 // please send CFG-2  ← main ask
	cmdSendCfg3 uint16 = 0x0006 // please send CFG-3
)

// ─── Frame helpers (build a command, read a frame, check the checksum) ────────

// crc16 is a fingerprint at the end of every frame. If it does not match,
// the bytes were corrupted on the wire — we reject that frame.
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

// buildCMDFrame packs one 18-byte "order slip" we send TO the PMU (version 2).
// Prefer buildCMDFrameVer when talking to Std2005 devices (version 1).
//
// Layout:  SYNC | SIZE | IDCODE | SOC | FRACSEC | CMD | CHECKSUM
//
// SOC/FRACSEC stay 0 (same as PMU Connection Tester).
func buildCMDFrame(idcode uint16, cmd uint16) []byte {
	return buildCMDFrameVer(idcode, cmd, syncVersion)
}

// buildCMDFrameVer is like buildCMDFrame but sets SYNC version bits (1 = 2005, 2 = 2011).
// Typhoon / some field PMUs answer CFG2 as Version 1 and ignore CMD frames with version 2.
func buildCMDFrameVer(idcode uint16, cmd uint16, ver byte) []byte {
	const frameSize = 18
	buf := make([]byte, frameSize)
	if ver < 1 || ver > 2 {
		ver = syncVersion
	}

	buf[0] = syncByte
	buf[1] = frameTypeCmd | ver // type=CMD (100b), version in low nibble
	binary.BigEndian.PutUint16(buf[2:], frameSize)
	binary.BigEndian.PutUint16(buf[4:], idcode)
	// SOC + FRACSEC stay 0 (bytes 6..13)
	binary.BigEndian.PutUint16(buf[14:], cmd)

	chk := crc16(buf[:frameSize-2])
	binary.BigEndian.PutUint16(buf[frameSize-2:], chk)

	return buf
}

// readFrame pulls ONE full frame from a stream (TCP) or datagram (UDP).
func readFrame(r io.Reader) ([]byte, error) {
	tf, err := readFrameTimed(r)
	return tf.raw, err
}

// timedFrame is a frame plus two stopwatches for latency charts.
type timedFrame struct {
	raw  []byte
	wait time.Duration // how long we sat idle before the first byte arrived
	copy time.Duration // how long to finish copying the whole frame after that
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

// decodeCFG2Details prints a friendly summary of the CFG2 "menu" (station name,
// rate, how many phasors, …). Parsing for real use is in parser.ParseCFG2Frame.
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

// ═══════════════════════════════════════════════════════════════════════════════
// §2  RECEIVER LIFECYCLE
//     One Receiver = one PMU config + reconnect loop.
//     Run() keeps calling connect() forever until the program shuts down.
// ═══════════════════════════════════════════════════════════════════════════════

// FrameHandler is the "give this packet to someone else" callback.
// receivedAt = wall clock when the full frame finished arriving (before parse).
type FrameHandler func(pmuName string, raw []byte, receivedAt time.Time)

// Receiver is the worker that owns one PMU connection.
type Receiver struct {
	cfg            config.PMUConfig
	handler        FrameHandler
	reconnectFails int                   // how many times in a row we failed (for backoff)
	onSessionStart func(pmuName string) // optional: reset charts when stream starts
	cmdVersion     byte                  // SYNC version for CMDs after CFG2 (1 or 2); 0 = default
}

// New builds a Receiver (does not dial yet — call Run for that).
func New(cfg config.PMUConfig, handler FrameHandler) *Receiver {
	return &Receiver{
		cfg:     cfg,
		handler: handler,
	}
}

// SetOnSessionStart runs after a successful handshake + DATA_ON (and on reconnect).
func (r *Receiver) SetOnSessionStart(fn func(pmuName string)) {
	r.onSessionStart = fn
}

// Run is the outer loop:
//
//	for ever {
//	  try connect()          // dial + handshake + stream until error
//	  if program quitting → stop
//	  else sleep a bit (backoff) and try again
//	}
func (r *Receiver) Run(ctx context.Context) {
	for {
		err := r.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			return
		}
		r.reconnectFails++
		monitoring.IncConnectionReconnect(r.cfg.Name)
		delay := r.reconnectDelay()
		log.Printf("[%s] connection error: %v – reconnecting in %s (attempt %d)",
			r.cfg.Name, err, monitoring.FormatMs(delay), r.reconnectFails)
		monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connection", "error", err.Error())

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// reconnectDelay grows after repeated failures (capped), plus a tiny name-based
// jitter so many PMUs do not all retry at the exact same millisecond.
func (r *Receiver) reconnectDelay() time.Duration {
	base := r.cfg.ReconnectInterval()
	fails := r.reconnectFails
	if fails > 5 {
		fails = 5
	}
	delay := base * time.Duration(1<<fails)
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	var jitter int
	for _, c := range r.cfg.Name {
		jitter += int(c)
	}
	return delay + time.Duration(jitter%800)*time.Millisecond
}

// connect picks TCP or UDP from config, then runs that path.
func (r *Receiver) connect(ctx context.Context) error {
	if r.cfg.NetworkProtocol() == "udp" {
		return r.connectUDP(ctx)
	}
	return r.connectTCP(ctx)
}

// ═══════════════════════════════════════════════════════════════════════════════
// §3  UDP PATH
//     Used when config.protocol == "udp".
//
//     Two flavours:
//       A) Dial   (tcp_port unset) — we call the PMU’s UDP address; commands
//          and DATA share that one socket (like Connection Tester UDP tab).
//       B) Listen (tcp_port set)   — we open a local UDP port for DATA, and
//          use a separate TCP connection only for CFG2 / DATA_ON.
// ═══════════════════════════════════════════════════════════════════════════════

// connectUDP chooses dial vs listen.
func (r *Receiver) connectUDP(ctx context.Context) error {
	if r.cfg.TCPPort <= 0 {
		return r.connectUDPDial(ctx) // flavour A
	}
	return r.connectUDPListen(ctx) // flavour B
}

// connectUDPDial — flavour A (one UDP socket for everything)
//
// Steps:
//  1. DialUDP to IP:port
//  2. Handshake over UDP (ask CFG2 until it arrives)
//  3. Send DATA_ON
//  4. Loop: read datagrams → if DATA, dispatch to the rest of the PDC
func (r *Receiver) connectUDPDial(ctx context.Context) error {
	timeout := r.cfg.Timeout()
	ip := net.ParseIP(strings.TrimSpace(r.cfg.IP))
	if ip == nil {
		return fmt.Errorf("udp dial: invalid ip %q", r.cfg.IP)
	}
	remote := &net.UDPAddr{IP: ip, Port: r.cfg.Port}
	dialStart := time.Now()
	pc, err := net.DialUDP("udp4", nil, remote)
	if err != nil {
		return fmt.Errorf("udp dial %s: %w", remote, err)
	}
	defer pc.Close()
	_ = pc.SetReadBuffer(256 * 1024)
	dialDur := time.Since(dialStart)
	monitoring.ObserveStage(r.cfg.Name, monitoring.StageDial, dialDur)

	log.Printf("[%s] UDP dialed %s (local %s) in %s — Connection Tester style",
		r.cfg.Name, remote, pc.LocalAddr(), monitoring.FormatMs(dialDur))
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connect", "ok",
		fmt.Sprintf("udp dial %s", remote))

	if err := r.handshakeCFGUDP(ctx, pc, timeout); err != nil {
		return err
	}
	if err := r.sendCMDUDP(pc, cmdDataOn); err != nil {
		return fmt.Errorf("send CMD_DATA_ON: %w", err)
	}
	log.Printf("[%s] sent CMD_DATA_ON on UDP – streaming data from %s", r.cfg.Name, remote)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "stream", "ok", "sent CMD_DATA_ON via UDP")
	r.reconnectFails = 0
	if r.onSessionStart != nil {
		r.onSessionStart(r.cfg.Name)
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
			_ = r.sendCMDUDP(pc, cmdDataOff)
			return nil
		}
		_ = pc.SetReadDeadline(time.Now().Add(idleTimeout))
		start := time.Now()
		n, err := pc.Read(buf)
		first := time.Now()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return fmt.Errorf("udp idle timeout from %s (no datagrams — disconnect Connection Tester if it holds the stream)", remote)
			}
			return fmt.Errorf("udp read %s: %w", remote, err)
		}
		tf, err := parseFrameBytes(buf[:n], first.Sub(start), time.Since(first))
		if err != nil {
			log.Printf("[%s] udp bad datagram: %v", r.cfg.Name, err)
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
				logFrameTrace(r.cfg.Name, fmt.Sprintf("udp rx #%d wait=%s", dataFrames, monitoring.FormatMs(tf.wait)), tf.raw)
			}
			payload := append([]byte(nil), tf.raw...)
			r.dispatchDataFrame(payload, completeAt)
		default:
			if dataFrames < 3 {
				log.Printf("[%s] udp skip %s", r.cfg.Name, frameTypeName(ft))
			}
		}
	}
}

// connectUDPListen — flavour B (TCP for commands, UDP listen for DATA)
//
// Steps:
//  1. ListenUDP on our local Port (PMU will push DATA here)
//  2. Dial TCP to IP:tcp_port for the handshake
//  3. CFG2 + DATA_ON on TCP
//  4. Loop: ReadFromUDP → only keep packets from the expected IP → dispatch DATA
func (r *Receiver) connectUDPListen(ctx context.Context) error {
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
	r.reconnectFails = 0
	if r.onSessionStart != nil {
		r.onSessionStart(r.cfg.Name)
	}
	go drainTCPQuiet(tcpConn)

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
			r.dispatchDataFrame(payload, completeAt)
		default:
			if dataFrames < 3 {
				log.Printf("[%s] udp skip %s from %s", r.cfg.Name, frameTypeName(ft), src)
			}
		}
	}
}

// udpSourceAllowed: ignore UDP packets that did not come from our PMU’s IP.
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

// drainTCPQuiet quietly reads (and discards) anything left on the TCP control
// socket so the peer does not stall if it also sends DATA there.
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

// ═══════════════════════════════════════════════════════════════════════════════
// §4  TCP PATH  (default — most lab PMUs / simulators)
//
// Steps inside connectTCP:
//  1. Dial TCP to IP:port
//  2. handshakeCFG  → keep asking for CFG2 until we get it
//  3. Send CMD_DATA_ON
//  4. Loop forever reading DATA frames → dispatchDataFrame
//     (on error, Run() will sleep and redial)
// ═══════════════════════════════════════════════════════════════════════════════

// handshakeCFG — "ask for the menu" over a TCP (or TCP-control) connection.
//
// Default (simple) path:
//   send CMD_SEND_CFG2 → wait up to 500ms → if no CFG2, send again → …
// until CFG2 arrives or the handshake timeout runs out.
//
// Optional full path (env C37118_SIMPLE_HANDSHAKE=0):
//   DATA_OFF → ask HEADER (optional) → then same CFG2 retry loop.
func (r *Receiver) handshakeCFG(ctx context.Context, conn net.Conn, timeout time.Duration) error {
	handshakeStart := time.Now()
	headerText := ""

	if simpleHandshakeEnabled() {
		log.Printf("[%s] handshake (simple): sending %s", r.cfg.Name, cmdName(cmdSendCfg2))
	} else {
		_ = r.sendCMD(conn, cmdDataOff)

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
		}

		log.Printf("[%s] handshake step 2: sending %s", r.cfg.Name, cmdName(cmdSendCfg2))
	}

	// Step: keep mailing "please send CFG2" until the menu arrives.
	// Alternate SYNC version 1 (2005) and 2 (2011) — Connection Tester CFG2
	// from Typhoon reported Version=1 / Std2005 and ignored our AA42 CMDs.
	cfgWaitStart := time.Now()
	cfgDeadline := cfgWaitStart.Add(timeout)
	const cfg2RetryWait = 500 * time.Millisecond
	var cfg2 []byte
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		remain := time.Until(cfgDeadline)
		if remain <= 0 {
			return fmt.Errorf("read CFG2 frame: timeout after %d request(s)", attempt-1)
		}
		ver := cmdVersionForAttempt(attempt)
		if err := r.sendCMDVer(conn, cmdSendCfg2, ver); err != nil {
			return fmt.Errorf("send CFG2 request #%d: %w", attempt, err)
		}
		wait := cfg2RetryWait
		if wait > remain {
			wait = remain
		}
		log.Printf("[%s] CFG2 request #%d sent (sync ver=%d), waiting up to %s for response",
			r.cfg.Name, attempt, ver, monitoring.FormatMs(wait))
		raw, err := readFrameOfType(conn, frameTypeCfg2, wait)
		if err == nil {
			cfg2 = raw
			r.rememberCMDVersion(cfg2)
			log.Printf("[%s] CFG2 received after %d request(s) (using cmd sync ver=%d)",
				r.cfg.Name, attempt, r.cmdSyncVer())
			break
		}
		if !isTimeoutErr(err) {
			return fmt.Errorf("read CFG2 frame: %w (after %d request(s))", err, attempt)
		}
		if time.Now().After(cfgDeadline) {
			return fmt.Errorf("read CFG2 frame: %w (after %d request(s))", err, attempt)
		}
		log.Printf("[%s] no CFG2 yet (%v) – resending", r.cfg.Name, err)
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
	monitoring.ObserveStage(r.cfg.Name, monitoring.StageHandshakeTotal, time.Since(handshakeStart))
	return nil
}

// connectTCP — full TCP session for one PMU.
func (r *Receiver) connectTCP(ctx context.Context) error {
	addr := r.cfg.Addr()
	proto := r.cfg.NetworkProtocol()
	timeout := r.cfg.Timeout()

	// ── Step 1: dial ──────────────────────────────────────────────────────────
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
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	configureStreamConn(conn)

	monitoring.ObserveStage(r.cfg.Name, monitoring.StageDial, dialDur)
	log.Printf("[%s] connected to %s in %s", r.cfg.Name, addr, monitoring.FormatMs(dialDur))
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "connect", "ok",
		fmt.Sprintf("connected to %s in %s", addr, monitoring.FormatMs(dialDur)))

	// ── Step 2: get CFG2 (the menu) ───────────────────────────────────────────
	if err := r.handshakeCFG(ctx, conn, timeout); err != nil {
		return err
	}

	// ── Step 3: tell PMU to start streaming ───────────────────────────────────
	log.Printf("[%s] handshake step 3: sending %s", r.cfg.Name, cmdName(cmdDataOn))
	if err := r.sendCMD(conn, cmdDataOn); err != nil {
		return fmt.Errorf("send CMD_DATA_ON: %w", err)
	}
	log.Printf("[%s] sent CMD_DATA_ON – streaming data", r.cfg.Name)
	monitoring.RecordConversation(r.cfg.Name, "PDC", "PMU", "stream", "ok", "sent CMD_DATA_ON")
	r.reconnectFails = 0
	if r.onSessionStart != nil {
		r.onSessionStart(r.cfg.Name)
	}

	// ── Step 4: read DATA forever ─────────────────────────────────────────────
	dataFrames := 0
	var lastComplete time.Time
	readTimeout := r.cfg.DataReadTimeout()

	for {
		if ctx.Err() != nil {
			_ = r.sendCMD(conn, cmdDataOff)
			return nil
		}

		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
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
			r.dispatchDataFrame(payload, completeAt)
		}
	}
}

// dispatchDataFrame hands one DATA frame to main.go (parse → quality → aligner).
// We stay on this goroutine so samples stay in wire order.
func (r *Receiver) dispatchDataFrame(payload []byte, receivedAt time.Time) {
	if r.handler != nil {
		r.handler(r.cfg.Name, payload, receivedAt)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// §5  SHARED SEND / WAIT TOOLS
//     Used by both TCP and UDP paths to mail commands and wait for answers.
// ═══════════════════════════════════════════════════════════════════════════════

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

// simpleHandshakeEnabled: true (default) = just CFG2 then DATA_ON.
// Set env C37118_SIMPLE_HANDSHAKE=0 for the longer DATA_OFF → HEADER → CFG2 path.
func simpleHandshakeEnabled() bool {
	v := strings.TrimSpace(os.Getenv("C37118_SIMPLE_HANDSHAKE"))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// configureStreamConn tunes TCP for low-latency streaming (no Nagle delay, bigger buffers).
func configureStreamConn(conn net.Conn) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tcp.SetNoDelay(true)
	_ = tcp.SetReadBuffer(256 * 1024)
	_ = tcp.SetWriteBuffer(64 * 1024)
}

// isTimeoutErr: true if we simply ran out of time waiting (safe to retry CFG2).
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "i/o timeout")
}

// readFrameOfType keeps reading until we see the frame type we want.
// Stray DATA frames (left over from a previous session) are skipped.
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

// sendCMD writes one command frame (uses version learned from CFG2 when known).
func (r *Receiver) sendCMD(conn net.Conn, cmd uint16) error {
	return r.sendCMDVer(conn, cmd, r.cmdSyncVer())
}

// sendCMDVer writes a command with an explicit SYNC version (1 or 2).
func (r *Receiver) sendCMDVer(conn net.Conn, cmd uint16, ver byte) error {
	frame := buildCMDFrameVer(r.cfg.IDCode, cmd, ver)
	logFrameTrace(r.cfg.Name, fmt.Sprintf("tx %s", cmdName(cmd)), frame)
	log.Printf("[%s] command payload: name=%s word=0x%04X sync_ver=%d", r.cfg.Name, cmdName(cmd), cmd, ver)
	if err := conn.SetWriteDeadline(time.Now().Add(r.cfg.Timeout())); err != nil {
		return err
	}
	_, err := conn.Write(frame)
	return err
}

// sendCMDUDP is the same order slip, mailed as one UDP datagram.
func (r *Receiver) sendCMDUDP(pc *net.UDPConn, cmd uint16) error {
	return r.sendCMDUDPVer(pc, cmd, r.cmdSyncVer())
}

func (r *Receiver) sendCMDUDPVer(pc *net.UDPConn, cmd uint16, ver byte) error {
	frame := buildCMDFrameVer(r.cfg.IDCode, cmd, ver)
	logFrameTrace(r.cfg.Name, fmt.Sprintf("tx %s (udp)", cmdName(cmd)), frame)
	log.Printf("[%s] command payload: name=%s word=0x%04X sync_ver=%d", r.cfg.Name, cmdName(cmd), cmd, ver)
	if err := pc.SetWriteDeadline(time.Now().Add(r.cfg.Timeout())); err != nil {
		return err
	}
	_, err := pc.Write(frame)
	return err
}

func (r *Receiver) cmdSyncVer() byte {
	if r != nil && r.cmdVersion >= 1 && r.cmdVersion <= 2 {
		return r.cmdVersion
	}
	return syncVersion
}

// rememberCMDVersion copies the version nibble from a received CFG/DATA frame
// so later DATA_ON / DATA_OFF match what the device speaks.
func (r *Receiver) rememberCMDVersion(raw []byte) {
	if r == nil || len(raw) < 2 {
		return
	}
	ver := raw[1] & 0x0F
	if ver >= 1 && ver <= 2 {
		r.cmdVersion = ver
	}
}

// cmdVersionForAttempt: odd attempts → v1 (2005), even → v2 (2011).
func cmdVersionForAttempt(attempt int) byte {
	if attempt%2 == 1 {
		return 1
	}
	return 2
}

// handshakeCFGUDP — same "ask for menu until it arrives" idea, over UDP.
func (r *Receiver) handshakeCFGUDP(ctx context.Context, pc *net.UDPConn, timeout time.Duration) error {
	handshakeStart := time.Now()
	log.Printf("[%s] handshake (udp): sending %s", r.cfg.Name, cmdName(cmdSendCfg2))

	cfgWaitStart := time.Now()
	cfgDeadline := cfgWaitStart.Add(timeout)
	const cfg2RetryWait = 500 * time.Millisecond
	var cfg2 []byte
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		remain := time.Until(cfgDeadline)
		if remain <= 0 {
			return fmt.Errorf("read CFG2 frame: timeout after %d request(s)", attempt-1)
		}
		ver := cmdVersionForAttempt(attempt)
		if err := r.sendCMDUDPVer(pc, cmdSendCfg2, ver); err != nil {
			return fmt.Errorf("send CFG2 request #%d: %w", attempt, err)
		}
		wait := cfg2RetryWait
		if wait > remain {
			wait = remain
		}
		log.Printf("[%s] CFG2 request #%d sent (udp, sync ver=%d), waiting up to %s for response",
			r.cfg.Name, attempt, ver, monitoring.FormatMs(wait))
		raw, err := readUDPFrameOfType(pc, frameTypeCfg2, wait)
		if err == nil {
			cfg2 = raw
			r.rememberCMDVersion(cfg2)
			log.Printf("[%s] CFG2 received after %d UDP request(s) (using cmd sync ver=%d)",
				r.cfg.Name, attempt, r.cmdSyncVer())
			break
		}
		if !isTimeoutErr(err) {
			return fmt.Errorf("read CFG2 frame: %w (after %d request(s))", err, attempt)
		}
		if time.Now().After(cfgDeadline) {
			return fmt.Errorf("read CFG2 frame: %w (after %d request(s))", err, attempt)
		}
		log.Printf("[%s] no CFG2 yet (%v) – resending over UDP", r.cfg.Name, err)
	}
	cfgDur := time.Since(cfgWaitStart)
	monitoring.ObserveStage(r.cfg.Name, monitoring.StageHandshakeCFG2, cfgDur)
	logFrameTrace(r.cfg.Name, "handshake CFG2 (udp)", cfg2)
	decodeCFG2Details(r.cfg.Name, cfg2)
	if profile, err := parser.ParseCFG2Frame(cfg2); err != nil {
		return fmt.Errorf("parse CFG2: %w", err)
	} else {
		parser.SetProfile(r.cfg.Name, profile)
		log.Printf("[%s] registered CFG2 profile: station=%q rate=%d", r.cfg.Name, profile.Station, profile.DataRate)
	}
	monitoring.ObserveStage(r.cfg.Name, monitoring.StageHandshakeTotal, time.Since(handshakeStart))
	return nil
}

// readUDPFrameOfType waits for one UDP datagram of the wanted type (skips DATA).
func readUDPFrameOfType(pc *net.UDPConn, wantType byte, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 65535)
	skipped := 0
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return nil, fmt.Errorf("timeout waiting for %s (skipped %d data frames)", frameTypeName(wantType), skipped)
		}
		if err := pc.SetReadDeadline(time.Now().Add(remain)); err != nil {
			return nil, err
		}
		n, err := pc.Read(buf)
		if err != nil {
			return nil, err
		}
		tf, err := parseFrameBytes(buf[:n], 0, 0)
		if err != nil {
			return nil, err
		}
		ft := frameType(tf.raw)
		if ft == wantType {
			return tf.raw, nil
		}
		if ft == frameTypeData {
			skipped++
			continue
		}
		return nil, fmt.Errorf("expected %s, got %s", frameTypeName(wantType), frameTypeName(ft))
	}
}