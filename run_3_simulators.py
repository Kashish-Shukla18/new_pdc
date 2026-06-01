#!/usr/bin/env python3
"""Start three PMU simulator processes with distinct ports and idcodes."""

from __future__ import annotations

import signal
import subprocess
import sys
import time
from pathlib import Path


SIMS = [
    {"name": "PMU-SIM-1", "tcp": 4712, "udp": 4713, "idcode": 7734, "fnom": 50, "rate": 50, "freq_bias": 0.0, "phase_shift_deg": 0.0, "mw_bias": 0.0, "mvar_bias": 0.0, "oscillation_scale": 1.00},
    {"name": "PMU-SIM-2", "tcp": 4722, "udp": 4723, "idcode": 7735, "fnom": 50, "rate": 50, "freq_bias": 0.015, "phase_shift_deg": 4.0, "mw_bias": 6.0, "mvar_bias": -3.0, "oscillation_scale": 0.92},
    {"name": "PMU-SIM-3", "tcp": 4732, "udp": 4733, "idcode": 7736, "fnom": 50, "rate": 50, "freq_bias": -0.012, "phase_shift_deg": -6.0, "mw_bias": -5.0, "mvar_bias": 4.0, "oscillation_scale": 1.10},
]


def start_simulators() -> list[subprocess.Popen]:
    base_dir = Path(__file__).resolve().parent
    stimulator = base_dir / "stimulator.py"
    procs: list[subprocess.Popen] = []

    for sim in SIMS:
        cmd = [
            sys.executable,
            str(stimulator),
            "--host",
            "127.0.0.1",
            "--tcp-port",
            str(sim["tcp"]),
            "--udp-port",
            str(sim["udp"]),
            "--idcode",
            str(sim["idcode"]),
            "--fnom",
            str(sim["fnom"]),
            "--rate",
            str(sim["rate"]),
            "--freq-bias",
            str(sim["freq_bias"]),
            "--phase-shift-deg",
            str(sim["phase_shift_deg"]),
            "--mw-bias",
            str(sim["mw_bias"]),
            "--mvar-bias",
            str(sim["mvar_bias"]),
            "--oscillation-scale",
            str(sim["oscillation_scale"]),
        ]

        proc = subprocess.Popen(cmd, cwd=str(base_dir))
        procs.append(proc)
        print(f"started {sim['name']} on tcp:{sim['tcp']} udp:{sim['udp']} idcode:{sim['idcode']} pid:{proc.pid}")

    return procs


def stop_simulators(procs: list[subprocess.Popen]) -> None:
    for proc in procs:
        if proc.poll() is None:
            proc.terminate()

    deadline = time.time() + 5
    for proc in procs:
        if proc.poll() is None:
            remaining = max(0.0, deadline - time.time())
            try:
                proc.wait(timeout=remaining)
            except subprocess.TimeoutExpired:
                proc.kill()


def main() -> int:
    procs = start_simulators()

    def _handle_signal(_sig: int, _frame) -> None:
        print("stopping simulators...")
        stop_simulators(procs)
        raise SystemExit(0)

    signal.signal(signal.SIGINT, _handle_signal)
    signal.signal(signal.SIGTERM, _handle_signal)

    print("all simulators running. press Ctrl+C to stop.")
    try:
        while True:
            time.sleep(1)
            for proc in procs:
                if proc.poll() is not None:
                    print(f"simulator pid {proc.pid} exited with code {proc.returncode}")
                    stop_simulators(procs)
                    return 1
    except KeyboardInterrupt:
        print("stopping simulators...")
        stop_simulators(procs)
        return 0


if __name__ == "__main__":
    raise SystemExit(main())