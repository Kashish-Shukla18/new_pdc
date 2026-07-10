package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

func crc(data []byte) uint16 {
	c := uint16(0xFFFF)
	for _, b := range data {
		c ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if c&0x8000 != 0 {
				c = (c << 1) ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	return c
}

func cmd(id uint16, word uint16) []byte {
	f := make([]byte, 18)
	f[0], f[1] = 0xAA, 0x42 // CMD type=0x40, version=2
	binary.BigEndian.PutUint16(f[2:], 18)
	binary.BigEndian.PutUint16(f[4:], id)
	binary.BigEndian.PutUint32(f[6:], uint32(time.Now().Unix()))
	binary.BigEndian.PutUint32(f[10:], 0)
	binary.BigEndian.PutUint16(f[14:], word)
	binary.BigEndian.PutUint16(f[16:], crc(f[:16]))
	return f
}

func readFrame(r io.Reader) ([]byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[2:]))
	if n < 4 || n > 65535 {
		return nil, fmt.Errorf("bad size %d (%x)", n, hdr)
	}
	rest := make([]byte, n-4)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}
	return append(hdr, rest...), nil
}

func trim(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func decodeCFG2(b []byte) (station string, pmuID uint16, format uint16, phnmr, annmr, dgnmr uint16, dataRate int16) {
	if len(b) < 46 {
		return
	}
	station = trim(b[20:36])
	pmuID = binary.BigEndian.Uint16(b[36:38])
	format = binary.BigEndian.Uint16(b[38:40])
	phnmr = binary.BigEndian.Uint16(b[40:42])
	annmr = binary.BigEndian.Uint16(b[42:44])
	dgnmr = binary.BigEndian.Uint16(b[44:46])
	if len(b) >= 6 {
		dataRate = int16(binary.BigEndian.Uint16(b[len(b)-4 : len(b)-2]))
	}
	return
}

func listenUDP(ports []int, d time.Duration) {
	for _, p := range ports {
		pc, err := net.ListenPacket("udp", fmt.Sprintf("0.0.0.0:%d", p))
		if err != nil {
			fmt.Printf("udp listen :%d failed: %v\n", p, err)
			continue
		}
		go func(p int, pc net.PacketConn) {
			defer pc.Close()
			_ = pc.SetDeadline(time.Now().Add(d))
			buf := make([]byte, 65535)
			for {
				n, addr, err := pc.ReadFrom(buf)
				if err != nil {
					return
				}
				fmt.Printf("UDP :%d from %s bytes=%d head=%x\n", p, addr, n, buf[:min(16, n)])
			}
		}(p, pc)
		fmt.Printf("listening UDP :%d\n", p)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	addr := flag.String("addr", "172.24.105.87:4712", "tcp host:port")
	wait := flag.Duration("wait", 20*time.Second, "wait for data after DATA_ON")
	idcodeFlag := flag.Uint("idcode", 0, "command idcode (0 = auto from CFG2)")
	flag.Parse()

	listenUDP([]int{4712, 4713, 4714, 8881}, *wait+5*time.Second)

	conn, err := net.DialTimeout("tcp", *addr, 6*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	fmt.Printf("TCP connected to %s (local=%s)\n", conn.RemoteAddr(), conn.LocalAddr())

	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	if _, err := conn.Write(cmd(1, 0x0005)); err != nil {
		fmt.Fprintf(os.Stderr, "write CFG2: %v\n", err)
		os.Exit(1)
	}
	cfg2, err := readFrame(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read CFG2: %v\n", err)
		os.Exit(1)
	}
	stn, pmuID, format, ph, an, dg, rate := decodeCFG2(cfg2)
	fmt.Printf("CFG2 size=%d station=%q pmu_idcode=%d format=0x%04X ph=%d an=%d dg=%d data_rate=%d\n",
		len(cfg2), stn, pmuID, format, ph, an, dg, rate)
	fmt.Printf("format bits: phasor_float=%v rectangular=%v analog_float=%v freq_float=%v\n",
		format&0x0002 != 0, format&0x0001 != 0, format&0x0004 != 0, format&0x0008 != 0)

	useID := uint16(1)
	if *idcodeFlag != 0 {
		useID = uint16(*idcodeFlag)
	} else if pmuID != 0 {
		useID = pmuID
	}
	fmt.Printf("sending DATA_ON with idcode=%d\n", useID)
	time.Sleep(200 * time.Millisecond)
	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	if _, err := conn.Write(cmd(useID, 0x0002)); err != nil {
		fmt.Fprintf(os.Stderr, "write DATA_ON: %v\n", err)
		os.Exit(1)
	}

	deadline := time.Now().Add(*wait)
	got := 0
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		raw, err := readFrame(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				fmt.Printf("… waiting for TCP data (%s left)\n", time.Until(deadline).Round(time.Second))
				continue
			}
			fmt.Printf("TCP read ended: %v (got=%d)\n", err, got)
			break
		}
		got++
		ft := raw[1] & 0x70
		fmt.Printf("TCP frame #%d type=0x%02X size=%d idcode=%d head=%x\n",
			got, ft, len(raw), binary.BigEndian.Uint16(raw[4:]), raw[:min(16, len(raw))])
	}

	if got == 0 {
		fmt.Println("no TCP data frames received after DATA_ON")
		fmt.Println("note: if PMU Connection Tester is open, close it — many PMUs allow only one DATA client")
		os.Exit(2)
	}
	fmt.Printf("SUCCESS: %d TCP frames\n", got)
}
