import argparse
import logging
import math
import socket
import struct
import threading
import time
from typing import Optional

# ─────────────────────────────────────────────────────────────────────────────
# CRC-CCITT  (polynomial X^16 + X^12 + X^5 + 1, seed 0xFFFF, no final mask)
# ─────────────────────────────────────────────────────────────────────────────
def crc_ccitt(data: bytes) -> int:
    crc = 0xFFFF
    for byte in data:
        temp  = (crc >> 8) ^ byte
        crc   = (crc << 8) & 0xFFFF
        quick = temp ^ (temp >> 4)
        crc  ^= quick
        crc  ^= (quick << 5)  & 0xFFFF
        crc  ^= (quick << 12) & 0xFFFF
    return crc


# ─────────────────────────────────────────────────────────────────────────────
# Helpers
# ─────────────────────────────────────────────────────────────────────────────
def _soc_fracsec(ts: float, time_base: int) -> tuple:
    """Split a Unix timestamp into (SOC uint32, FRACSEC uint32).

    FRACSEC bits 31-24 = time-quality byte (0x00 = locked to UTC).
    FRACSEC bits 23-0  = fraction count (count / time_base = seconds fraction).
    """
    soc       = int(ts)
    frac_sec  = ts - soc
    frac_cnt  = round(frac_sec * time_base) & 0x00FFFFFF
    fracsec   = frac_cnt  # time-quality byte = 0x00 (locked, best)
    return soc, fracsec


def _pack_frame(sync: int, idcode: int, soc: int, fracsec: int, payload: bytes) -> bytes:
    """Wrap payload in the standard C37.118 frame and append CRC."""
    framesize = 2 + 2 + 2 + 4 + 4 + len(payload) + 2   # SYNC+FS+IDC+SOC+FRAC+payload+CHK
    header    = struct.pack('>HHHII', sync, framesize, idcode, soc, fracsec)
    body      = header + payload
    chk       = crc_ccitt(body)
    return body + struct.pack('>H', chk)


# ─────────────────────────────────────────────────────────────────────────────
# PMU configuration constants (all overridable via constructor)
# ─────────────────────────────────────────────────────────────────────────────
_STATION_NAME  = b'PMU_SIM_IITK    '   # 16 bytes ASCII
_IDCODE        = 7734
_TIME_BASE     = 1_000_000              # 1 µs resolution  (IEC 61850 compatible = 2^24)

# FORMAT word: bit3=1(FREQ float), bit2=1(analog float), bit1=1(phasor float), bit0=0(rectangular)
_FORMAT        = 0x000E

_PHNMR  = 4   # VA, VB, VC, IA
_ANNMR  = 2   # MW, MVAR
_DGNMR  = 1   # one 16-bit digital word (16 breaker bits)


# ─────────────────────────────────────────────────────────────────────────────
# Frame builders
# ─────────────────────────────────────────────────────────────────────────────
def build_cfg2_frame(soc: int, fracsec: int,
                     idcode: int, fnom: int, data_rate: int) -> bytes:
    """Build a Configuration Frame 2 (CFG-2, SYNC=0xAA31)."""

    # ── fixed header section ──────────────────────────────────────────────
    body  = struct.pack('>I', _TIME_BASE)              # TIME_BASE
    body += struct.pack('>H', 1)                        # NUM_PMU = 1

    # ── PMU block (repeated once) ─────────────────────────────────────────
    body += _STATION_NAME[:16].ljust(16, b' ')          # STN (16 bytes)
    body += struct.pack('>H', idcode)                   # IDCODE
    body += struct.pack('>H', _FORMAT)                  # FORMAT
    body += struct.pack('>H', _PHNMR)                   # PHNMR
    body += struct.pack('>H', _ANNMR)                   # ANNMR
    body += struct.pack('>H', _DGNMR)                   # DGNMR

    # Channel names: 16 bytes each
    ch_names = [b'VA', b'VB', b'VC', b'IA', b'MW', b'MVAR']
    for n in ch_names:
        body += n.ljust(16, b' ')[:16]
    # 16 digital channel labels (one per bit of the digital word)
    breaker_labels = [f'BKR_{i:02d}'.encode() for i in range(1, 17)]
    for lbl in breaker_labels:
        body += lbl.ljust(16, b' ')[:16]

    # PHUNIT (4 bytes × PHNMR)
    # MSB: 0=voltage, 1=current  |  LS 3 bytes: scale in 1e-5 V or A per bit
    # For floating-point mode the 24-bit scale is informational only.
    v_scale = int((132_000 / math.sqrt(3)) * 1e5 / 32768) & 0xFFFFFF
    i_scale = int(1_000 * 1e5 / 32768) & 0xFFFFFF
    for _ in range(3):
        body += struct.pack('>I', (0x00 << 24) | v_scale)   # voltages
    body += struct.pack('>I', (0x01 << 24) | i_scale)       # current IA

    # ANUNIT (4 bytes × ANNMR): rms analog, unity scale
    for _ in range(_ANNMR):
        body += struct.pack('>I', 0x01_000001)   # MSB=1 (rms), scale=1

    # DIGUNIT (4 bytes × DGNMR): normal-state mask = 0, valid-bits mask = 0xFFFF
    body += struct.pack('>HH', 0x0000, 0xFFFF)

    # FNOM: bit0=1 → 50 Hz, bit0=0 → 60 Hz
    fnom_word = 0x0001 if fnom == 50 else 0x0000
    body += struct.pack('>H', fnom_word)

    # CFGCNT
    body += struct.pack('>H', 0)

    # DATA_RATE (signed: positive = fps, negative = seconds-per-frame)
    body += struct.pack('>h', data_rate)

    return _pack_frame(0xAA31, idcode, soc, fracsec, body)


def build_header_frame(soc: int, fracsec: int, idcode: int) -> bytes:
    """Build a human-readable Header Frame (SYNC=0xAA11)."""
    info = (
        f"IIT Kanpur PMU Simulator | IEEE C37.118.2-2011\n"
        f"Station : {_STATION_NAME.decode().strip()}\n"
        f"IDCODE  : {idcode}\n"
        f"Channels: VA VB VC IA | MW MVAR | DIG×16\n"
        f"Format  : Floating-point rectangular phasors\n"
    ).encode('ascii')
    return _pack_frame(0xAA11, idcode, soc, fracsec, info)


def build_data_frame(soc: int, fracsec: int, idcode: int,
                     t: float, fnom: int,
                     freq_bias: float = 0.0,
                     phase_shift_deg: float = 0.0,
                     mw_bias: float = 0.0,
                     mvar_bias: float = 0.0,
                     oscillation_scale: float = 1.0) -> bytes:
    """Build a Data Frame (SYNC=0xAA01) with simulated synchrophasors."""

    # ── simulate grid signals ─────────────────────────────────────────────
    v_nom       = 132_000 / math.sqrt(3)      # phase-to-neutral RMS, volts
    i_nom       = 1_000.0                     # amps

    # Slow frequency oscillation (±0.05 Hz at 0.05 Hz beat) to feel "live"
    freq_dev    = oscillation_scale * 0.05 * math.sin(2 * math.pi * 0.05 * t)
    f_actual    = fnom + freq_bias + freq_dev
    rocof       = oscillation_scale * 0.05 * 2 * math.pi * 0.05 * math.cos(2 * math.pi * 0.05 * t)

    # Accumulated phase offset from nominal (integral of freq_dev over time)
    # ≈ 0.05 / (2π×0.05) × sin(…) = 0.159 × sin(…) radians
    phase_off   = math.radians(phase_shift_deg) + (
        oscillation_scale * (0.05 / (2 * math.pi * 0.05)) * math.sin(2 * math.pi * 0.05 * t)
    )

    # 3-phase angles (balanced, RMS magnitudes)
    ang_a =  phase_off
    ang_b =  phase_off - 2 * math.pi / 3
    ang_c =  phase_off + 2 * math.pi / 3

    # Phasors in rectangular floating-point
    def rect(mag, ang):
        return mag * math.cos(ang), mag * math.sin(ang)

    va_r, va_i = rect(v_nom, ang_a)
    vb_r, vb_i = rect(v_nom, ang_b)
    vc_r, vc_i = rect(v_nom, ang_c)
    # Current IA: lagging VA by 25° (power-factor ≈ 0.91 lag)
    ia_r, ia_i = rect(i_nom, ang_a - math.radians(25))

    # MW/MVAR slow swing
    mw   = 100.0 + mw_bias + 10.0 * math.sin(2 * math.pi * 0.02 * t)
    mvar = 40.0  + mvar_bias + 5.0 * math.cos(2 * math.pi * 0.02 * t)

    # Digital word: BKR_01 and BKR_02 closed
    digital = 0x0003

    # ── STAT word ────────────────────────────────────────────────────────
    # Bits 15-14=00 (data OK), Bit 13=0 (synced), Bit 12=0 (time-sorted),
    # Bits 8-6=010 (PMU_TQ < 1 µs), all else 0
    stat = 0b0000_0000_1000_0000  # PMU_TQ bits [8:6]=010

    # ── pack payload ─────────────────────────────────────────────────────
    payload  = struct.pack('>H',   stat)
    payload += struct.pack('>ff',  va_r, va_i)
    payload += struct.pack('>ff',  vb_r, vb_i)
    payload += struct.pack('>ff',  vc_r, vc_i)
    payload += struct.pack('>ff',  ia_r, ia_i)
    payload += struct.pack('>f',   f_actual)      # FREQ  (float, Hz)
    payload += struct.pack('>f',   rocof)          # DFREQ (float, Hz/s)
    payload += struct.pack('>f',   mw)             # ANALOG MW
    payload += struct.pack('>f',   mvar)           # ANALOG MVAR
    payload += struct.pack('>H',   digital)        # DIGITAL

    return _pack_frame(0xAA01, idcode, soc, fracsec, payload)


def bytes_to_hex(packet: bytes) -> str:
    """Render packet bytes as uppercase hex pairs separated by spaces."""
    return ' '.join(f'{b:02X}' for b in packet)


# ─────────────────────────────────────────────────────────────────────────────
# Command parser
# ─────────────────────────────────────────────────────────────────────────────
def parse_command(raw: bytes) -> Optional[int]:
    """Return the CMD word (int) if raw bytes contain a valid command frame."""
    try:
        if len(raw) < 18:
            return None
        sync, fsize, idcode = struct.unpack_from('>HHH', raw, 0)
        if sync != 0xAA41:
            logging.debug("parse_command: invalid sync 0x%04X", sync)
            return None
        if len(raw) < fsize:
            logging.debug("parse_command: frame too short %d < %d", len(raw), fsize)
            return None
        # verify CRC
        received_crc = struct.unpack_from('>H', raw, fsize - 2)[0]
        computed_crc = crc_ccitt(raw[:fsize - 2])
        if received_crc != computed_crc:
            logging.warning("parse_command: CRC mismatch (expected 0x%04X, got 0x%04X)", received_crc, computed_crc)
            return None
        cmd = struct.unpack_from('>H', raw, 14)[0]
        logging.debug("parse_command: decoded CMD 0x%04X", cmd)
        return cmd
    except Exception as e:
        logging.exception("parse_command exception: %s", e)
        return None


# ─────────────────────────────────────────────────────────────────────────────
# TCP client handler
# ─────────────────────────────────────────────────────────────────────────────
class TCPClientHandler(threading.Thread):
    """Handles one connected TCP client in its own thread."""

    CMD_DATA_OFF = 0x0001
    CMD_DATA_ON  = 0x0002
    CMD_HDR      = 0x0003
    CMD_CFG1     = 0x0004
    CMD_CFG2     = 0x0005

    def __init__(self, conn: socket.socket, addr, pmu: 'PMUSimulator'):
        super().__init__(daemon=True)
        self.conn       = conn
        self.addr       = addr
        self.pmu        = pmu
        self.sending    = False
        self.alive      = True
        self.lock       = threading.Lock()
        self.stream_key = f"tcp_{addr[0]}_{addr[1]}"
        self.name       = f"TCPHandler-{addr[0]}:{addr[1]}"

    def run(self):
        logging.info("TCP client connected: %s:%d", *self.addr)
        sender = threading.Thread(target=self._send_loop, daemon=True)
        sender.start()
        self.conn.settimeout(0.5)
        buf = b''

        while self.alive:
            try:
                chunk = self.conn.recv(1024)
                if not chunk:
                    break
                buf += chunk
                while len(buf) >= 4:
                    fsize = struct.unpack_from('>H', buf, 2)[0]
                    if len(buf) < fsize:
                        break
                    frame_bytes = buf[:fsize]
                    buf = buf[fsize:]
                    cmd = parse_command(frame_bytes)
                    if cmd is not None:
                        logging.info("[%s:%d] Received CMD: 0x%04X", *self.addr, cmd)
                        self._handle_cmd(cmd)
            except socket.timeout:
                pass
            except (ConnectionResetError, BrokenPipeError, OSError):
                break

        self.alive = False
        sender.join(timeout=0.1)
        self.conn.close()
        logging.info("TCP client disconnected: %s:%d", *self.addr)

    def _send_loop(self):
        while self.alive:
            if not self.sending:
                time.sleep(0.005)
                continue

            frame = self.pmu.get_next_data_frame(self.stream_key)
            if frame is None:
                time.sleep(min(max(self.pmu.seconds_until_next_frame(self.stream_key), 0.001), 0.005))
                continue

            try:
                self.conn.sendall(frame)
            except (BrokenPipeError, OSError):
                self.alive = False
                return

    def _handle_cmd(self, cmd: int):
        ts = time.time()
        soc, fracsec = _soc_fracsec(ts, _TIME_BASE)
        idcode = self.pmu.idcode

        if cmd == self.CMD_DATA_ON:
            logging.info("[%s:%d] CMD: Data ON", *self.addr)
            self.pmu.arm_stream(self.stream_key)
            self.sending = True

        elif cmd == self.CMD_DATA_OFF:
            logging.info("[%s:%d] CMD: Data OFF", *self.addr)
            self.sending = False

        elif cmd in (self.CMD_CFG1, self.CMD_CFG2):
            logging.info("[%s:%d] CMD: Send CFG-%d", *self.addr, cmd - 3)
            frame = build_cfg2_frame(soc, fracsec, idcode,
                                     self.pmu.fnom, self.pmu.data_rate)
            try:
                self.conn.sendall(frame)
            except OSError:
                pass

        elif cmd == self.CMD_HDR:
            logging.info("[%s:%d] CMD: Send Header", *self.addr)
            frame = build_header_frame(soc, fracsec, idcode)
            try:
                self.conn.sendall(frame)
            except OSError:
                pass

        else:
            logging.debug("[%s:%d] Unknown CMD: 0x%04X", *self.addr, cmd)

    def stop(self):
        self.alive = False


# ─────────────────────────────────────────────────────────────────────────────
# PMU Simulator core
# ─────────────────────────────────────────────────────────────────────────────
class PMUSimulator:
    """
    Core PMU simulator.

    Maintains a high-precision frame ticker so that get_next_data_frame()
    returns a new frame exactly when the next reporting interval is due,
    or None if it's not time yet.  Callers should poll in a tight loop or
    sleep(0) between polls.
    """

    def __init__(self, idcode: int = _IDCODE, fnom: int = 50,
                 data_rate: int = 50, verbose: bool = False,
                 freq_bias: float = 0.0, phase_shift_deg: float = 0.0,
                 mw_bias: float = 0.0, mvar_bias: float = 0.0,
                 oscillation_scale: float = 1.0):
        self.idcode    = idcode
        self.fnom      = fnom
        self.data_rate = data_rate
        self.verbose   = verbose
        self.freq_bias = freq_bias
        self.phase_shift_deg = phase_shift_deg
        self.mw_bias = mw_bias
        self.mvar_bias = mvar_bias
        self.oscillation_scale = oscillation_scale

        self._interval = 1.0 / data_rate    # seconds between frames
        now            = time.time()
        self._frame_n  = 0
        self._t0       = now                # wall-clock reference for signals
        self._lock     = threading.Lock()
        self._next_ts_by_stream = {
            'tcp': math.ceil(now * data_rate) / data_rate,
            'udp': math.ceil(now * data_rate) / data_rate,
        }

    def get_next_data_frame(self, stream: str = 'tcp') -> Optional[bytes]:
        """Return a data frame for one stream if its scheduled time has arrived, else None."""
        now = time.time()
        with self._lock:
            next_ts = self._next_ts_by_stream.get(stream, math.ceil(now * self.data_rate) / self.data_rate)
            if now < next_ts:
                return None
            ts        = next_ts
            self._next_ts_by_stream[stream] = next_ts + self._interval
            self._frame_n += 1

        soc, fracsec = _soc_fracsec(ts, _TIME_BASE)
        t_rel        = ts - self._t0
        frame        = build_data_frame(
            soc,
            fracsec,
            self.idcode,
            t_rel,
            self.fnom,
            freq_bias=self.freq_bias,
            phase_shift_deg=self.phase_shift_deg,
            mw_bias=self.mw_bias,
            mvar_bias=self.mvar_bias,
            oscillation_scale=self.oscillation_scale,
        )

        if self.verbose and (self._frame_n <= 3 or self._frame_n % self.data_rate == 0):
            logging.info("Frame #%6d  SOC=%d  size=%d bytes  "
                         "FRACSEC=0x%08X  ts=%.6f",
                         self._frame_n, soc, len(frame), fracsec, ts)
        return frame

    def seconds_until_next_frame(self, stream: str = 'tcp') -> float:
        with self._lock:
            next_ts = self._next_ts_by_stream.get(stream, math.ceil(time.time() * self.data_rate) / self.data_rate)
            return max(next_ts - time.time(), 0.0)

    def arm_stream(self, stream: str = 'tcp'):
        with self._lock:
            now = time.time()
            self._next_ts_by_stream[stream] = math.ceil(now * self.data_rate) / self.data_rate


# ─────────────────────────────────────────────────────────────────────────────
# TCP server
# ─────────────────────────────────────────────────────────────────────────────
def run_tcp_server(host: str, port: int, pmu: PMUSimulator):
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind((host, port))
    srv.listen(10)
    srv.settimeout(1.0)
    logging.info("TCP server listening on %s:%d", host, port)

    handlers = []
    while True:
        try:
            conn, addr = srv.accept()
            h = TCPClientHandler(conn, addr, pmu)
            h.start()
            handlers.append(h)
            # prune dead handlers
            handlers = [h for h in handlers if h.is_alive()]
        except socket.timeout:
            pass
        except KeyboardInterrupt:
            break

    for h in handlers:
        h.stop()
    srv.close()


# ─────────────────────────────────────────────────────────────────────────────
# UDP spontaneous broadcast
# ─────────────────────────────────────────────────────────────────────────────
def run_udp_server(host: str, port: int, pmu: PMUSimulator):
    """
    Spontaneous mode: sends data frames at the configured rate
    to the broadcast address on the UDP port.
    Clients simply bind to port 4713 and receive without sending commands.
    """
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
    dest = ('<broadcast>', port)
    logging.info("UDP spontaneous broadcast → port %d", port)

    try:
        while True:
            frame = pmu.get_next_data_frame('udp')
            if frame is not None:
                sock.sendto(frame, dest)
            else:
                time.sleep(0.0002)   # 200 µs spin — keeps CPU usage low
    except KeyboardInterrupt:
        pass
    finally:
        sock.close()


# ─────────────────────────────────────────────────────────────────────────────
# Entry point
# ─────────────────────────────────────────────────────────────────────────────
def main():
    ap = argparse.ArgumentParser(
        description='IEEE C37.118.2-2011 PMU Simulator',
        formatter_class=argparse.ArgumentDefaultsHelpFormatter
    )
    ap.add_argument('--host',     default='0.0.0.0',  help='Bind address')
    ap.add_argument('--tcp-port', type=int, default=4712, help='TCP port')
    ap.add_argument('--udp-port', type=int, default=4713, help='UDP broadcast port')
    ap.add_argument('--idcode',   type=int, default=_IDCODE, help='PMU IDCODE (1–65534)')
    ap.add_argument('--fnom',     type=int, default=50,  choices=[50, 60],
                    help='Nominal system frequency (Hz)')
    ap.add_argument('--rate',     type=int, default=50,
                    help='Data reporting rate (frames/s). '
                         'Standard values for 50 Hz: 10,25,50; for 60 Hz: 10,12,15,20,30,60')
    ap.add_argument('--no-tcp',   action='store_true', help='Disable TCP server')
    ap.add_argument('--no-udp',   action='store_true', help='Disable UDP broadcast')
    ap.add_argument('--verbose',  action='store_true', help='Log one summary line per second')
    ap.add_argument('--print-one-packet', action='store_true',
                    help='Print one data packet in hex and exit')
    ap.add_argument('--freq-bias', type=float, default=0.0,
                    help='Static frequency bias in Hz applied to this simulator')
    ap.add_argument('--phase-shift-deg', type=float, default=0.0,
                    help='Static phase shift in degrees applied to this simulator')
    ap.add_argument('--mw-bias', type=float, default=0.0,
                    help='Static active power offset in MW applied to this simulator')
    ap.add_argument('--mvar-bias', type=float, default=0.0,
                    help='Static reactive power offset in MVAR applied to this simulator')
    ap.add_argument('--oscillation-scale', type=float, default=1.0,
                    help='Scale factor for oscillating components to separate traces')
    args = ap.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format='%(asctime)s [%(levelname)s] %(message)s',
        datefmt='%H:%M:%S'
    )

    pmu = PMUSimulator(
        idcode    = args.idcode,
        fnom      = args.fnom,
        data_rate = args.rate,
        verbose   = args.verbose,
        freq_bias = args.freq_bias,
        phase_shift_deg = args.phase_shift_deg,
        mw_bias = args.mw_bias,
        mvar_bias = args.mvar_bias,
        oscillation_scale = args.oscillation_scale,
    )

    if args.print_one_packet:
        ts = time.time()
        soc, fracsec = _soc_fracsec(ts, _TIME_BASE)
        packet = build_data_frame(
            soc,
            fracsec,
            pmu.idcode,
            0.0,
            pmu.fnom,
            freq_bias=pmu.freq_bias,
            phase_shift_deg=pmu.phase_shift_deg,
            mw_bias=pmu.mw_bias,
            mvar_bias=pmu.mvar_bias,
            oscillation_scale=pmu.oscillation_scale,
        )
        print(bytes_to_hex(packet))
        return

    ts = time.time()
    soc, fracsec = _soc_fracsec(ts, _TIME_BASE)
    startup_packet = build_data_frame(
        soc,
        fracsec,
        pmu.idcode,
        0.0,
        pmu.fnom,
        freq_bias=pmu.freq_bias,
        phase_shift_deg=pmu.phase_shift_deg,
        mw_bias=pmu.mw_bias,
        mvar_bias=pmu.mvar_bias,
        oscillation_scale=pmu.oscillation_scale,
    )
    print(bytes_to_hex(startup_packet))

    logging.info("═══════════════════════════════════════════════════")
    logging.info(" IEEE C37.118.2-2011  PMU Simulator  — IITK")
    logging.info("═══════════════════════════════════════════════════")
    logging.info(" IDCODE    : %d",  pmu.idcode)
    logging.info(" f_nominal : %d Hz", pmu.fnom)
    logging.info(" Rate      : %d fps  (T = %.1f ms)", pmu.data_rate, 1000/pmu.data_rate)
    logging.info(" Phasors   : VA VB VC (132 kV L-N) + IA (1 kA)")
    logging.info(" Analogs   : MW, MVAR")
    logging.info(" Digital   : 16-bit breaker word")
    logging.info(" Format    : IEEE float, rectangular")
    logging.info(" Biases    : freq=%+.4fHz phase=%+.2fdeg MW=%+.2f MVAR=%+.2f scale=%.2f",
                 pmu.freq_bias, pmu.phase_shift_deg, pmu.mw_bias, pmu.mvar_bias, pmu.oscillation_scale)
    if not args.no_tcp:
        logging.info(" TCP       : %s:%d  (commanded mode)", args.host, args.tcp_port)
    if not args.no_udp:
        logging.info(" UDP       : broadcast :%d  (spontaneous)", args.udp_port)
    logging.info("═══════════════════════════════════════════════════")

    threads = []

    if not args.no_tcp:
        t = threading.Thread(
            target=run_tcp_server,
            args=(args.host, args.tcp_port, pmu),
            daemon=True, name='TCPServer'
        )
        t.start()
        threads.append(t)

    if not args.no_udp:
        t = threading.Thread(
            target=run_udp_server,
            args=(args.host, args.udp_port, pmu),
            daemon=True, name='UDPServer'
        )
        t.start()
        threads.append(t)

    if not threads:
        logging.error("Both TCP and UDP are disabled — nothing to do.")
        return

    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        logging.info("Shutting down PMU simulator…")


# ─────────────────────────────────────────────────────────────────────────────
# Quick self-test (run with --test flag)
# ─────────────────────────────────────────────────────────────────────────────
def self_test():
    """Verify CRC and frame construction without opening sockets."""
    print("Running self-test…")

    # CRC test vectors from Annex B of the standard
    assert crc_ccitt(b'ABCD')   == 0xBFFA, "CRC vector 1 failed"
    assert crc_ccitt(b'123456') == 0x2EF4, "CRC vector 2 failed"
    assert crc_ccitt(b'abc')    == 0x514A, "CRC vector 3 failed"
    print("  ✓ CRC-CCITT vectors pass")

    ts = 1_149_580_800.0 + 0.016667   # example from Annex D
    soc, fracsec = _soc_fracsec(ts, _TIME_BASE)
    assert soc == 1_149_580_800
    assert abs(fracsec - 16667) < 2   # within ±2 µs rounding
    print("  ✓ SOC/FRACSEC split pass")

    pmu = PMUSimulator(idcode=7734, fnom=60, data_rate=30)
    cfg = build_cfg2_frame(soc, fracsec, 7734, 60, 30)
    # verify CRC embedded in frame
    fsize = struct.unpack_from('>H', cfg, 2)[0]
    assert len(cfg) == fsize
    rx_crc = struct.unpack_from('>H', cfg, fsize-2)[0]
    assert crc_ccitt(cfg[:fsize-2]) == rx_crc
    print("  ✓ CFG-2 frame CRC pass  (size=%d bytes)" % fsize)

    df = build_data_frame(soc, fracsec, 7734, 0.0, 60)
    fsize2 = struct.unpack_from('>H', df, 2)[0]
    rx_crc2 = struct.unpack_from('>H', df, fsize2-2)[0]
    assert crc_ccitt(df[:fsize2-2]) == rx_crc2
    print("  ✓ Data frame CRC pass   (size=%d bytes)" % fsize2)

    print("\nAll self-tests passed ✓")


if __name__ == '__main__':
    import sys
    if '--test' in sys.argv:
        self_test()
    else:
        main()