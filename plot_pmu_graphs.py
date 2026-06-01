#!/usr/bin/env python3
"""Plot live PMU trends for multiple simulators from the PDC conversation API."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import math

import matplotlib.pyplot as plt
from matplotlib.animation import FuncAnimation
from matplotlib.dates import DateFormatter
import requests


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser(description="Live PMU plots from /conversation/state")
    ap.add_argument("--state-url", default="http://127.0.0.1:2112/conversation/state", help="PDC state endpoint")
    ap.add_argument("--pmus", default="PMU-SIM-1,PMU-SIM-2,PMU-SIM-3", help="Comma-separated PMU names to plot")
    ap.add_argument("--refresh-ms", type=int, default=1000, help="Refresh interval for plot updates")
    return ap.parse_args()


def ts_ms_to_dt(values: list[int]) -> list[datetime]:
    return [datetime.fromtimestamp(v / 1000.0, tz=timezone.utc).astimezone() for v in values]


def phasor_xy(magnitude: float, angle_deg: float) -> tuple[float, float]:
    angle_rad = math.radians(angle_deg)
    return magnitude * math.cos(angle_rad), magnitude * math.sin(angle_rad)


def draw_phasor_diagram(ax: plt.Axes, pmu: dict, pmu_name: str) -> bool:
    ph = pmu.get("lastPhasor") if isinstance(pmu, dict) else None
    if not isinstance(ph, dict):
        return False

    vectors = [
        ("VA", ph.get("va", {}), "#F87171", 3.2),
        ("VB", ph.get("vb", {}), "#FBBF24", 3.2),
        ("VC", ph.get("vc", {}), "#60A5FA", 3.2),
        ("IA", ph.get("ia", {}), "#22D3EE", 2.2),
    ]

    parsed: list[tuple[str, float, float, float, float, str, float]] = []
    for label, item, color, width in vectors:
        if not isinstance(item, dict):
            continue
        try:
            mag = float(item.get("magnitude", 0.0))
            ang = float(item.get("angleDeg", 0.0))
        except (TypeError, ValueError):
            continue
        x, y = phasor_xy(mag, ang)
        parsed.append((label, mag, ang, x, y, color, width))

    if not parsed:
        return False

    vmax = max(max(abs(v[3]), abs(v[4]), abs(v[1])) for v in parsed)
    if vmax <= 0:
        vmax = 1.0
    limit = vmax * 1.25

    # Draw phasor plane with concentric magnitude rings and orthogonal axes.
    ax.set_xlim(-limit, limit)
    ax.set_ylim(-limit, limit)
    ax.set_aspect("equal", adjustable="box")
    ax.axhline(0, color="#334155", linewidth=1.0)
    ax.axvline(0, color="#334155", linewidth=1.0)

    ring_count = 4
    for i in range(1, ring_count + 1):
        r = limit * i / ring_count
        ring = plt.Circle((0, 0), r, color="#334155", fill=False, alpha=0.3, linewidth=0.8)
        ax.add_patch(ring)

    for label, mag, ang, x, y, color, width in parsed:
        ax.arrow(
            0,
            0,
            x,
            y,
            width=limit * 0.006,
            head_width=limit * 0.04,
            head_length=limit * 0.06,
            length_includes_head=True,
            color=color,
            linewidth=width,
            alpha=0.92,
        )
        ax.text(
            x * 1.06,
            y * 1.06,
            f"{label}: {mag:.1f} @ {ang:.1f}°",
            color=color,
            fontsize=8,
            ha="center",
            va="center",
        )

    ax.set_title(f"Phasor Diagram ({pmu_name})", loc="left", fontsize=11, pad=8, color="#E5E7EB")
    ax.set_xlabel("Real axis", color="#CBD5E1")
    ax.set_ylabel("Imag axis", color="#CBD5E1")
    ax.tick_params(colors="#94A3B8", labelsize=8)
    ax.grid(True, color="#334155", alpha=0.18, linewidth=0.6)
    return True


def main() -> int:
    args = parse_args()
    wanted = [x.strip() for x in args.pmus.split(",") if x.strip()]

    plt.style.use("dark_background")
    fig = plt.figure(figsize=(17.5, 9.2))
    gs = fig.add_gridspec(2, 3, hspace=0.28, wspace=0.2)
    trend_axes = [
        fig.add_subplot(gs[0, 0]),
        fig.add_subplot(gs[0, 1]),
        fig.add_subplot(gs[0, 2]),
        fig.add_subplot(gs[1, 0]),
    ]
    phasor_ax = fig.add_subplot(gs[1, 1:3])

    fig.patch.set_facecolor("#0b1220")
    metrics = [
        ("mw", "MW", "MW"),
        ("rocof", "ROCOF", "Hz/s"),
        ("frequency", "Frequency (Hz) live trend", "Hz"),
        ("mvar", "MVAR", "MVAR"),
    ]
    colors = {
        "PMU-SIM-1": "#7EB26D",
        "PMU-SIM-2": "#6ED0E0",
        "PMU-SIM-3": "#EAB839",
    }
    linestyles = {
        "PMU-SIM-1": "-",
        "PMU-SIM-2": "--",
        "PMU-SIM-3": "-.",
    }
    time_formatter = DateFormatter("%H:%M:%S")

    session = requests.Session()

    def update(_frame: int) -> None:
        try:
            response = session.get(args.state_url, timeout=2)
            response.raise_for_status()
            payload = response.json()
        except Exception as exc:
            fig.suptitle(f"PDC Live Trends - fetch error: {exc}")
            return

        pmu_map = {pmu.get("name", ""): pmu for pmu in payload.get("pmus", [])}

        for ax, (metric_key, title, unit) in zip(trend_axes, metrics):
            ax.clear()
            ax.set_facecolor("#111827")
            for spine in ax.spines.values():
                spine.set_color("#334155")
            plotted = False
            for name in wanted:
                pmu = pmu_map.get(name)
                if not pmu:
                    continue
                trends = pmu.get("trends", [])
                if not trends:
                    continue

                xs = ts_ms_to_dt([int(p["ts"]) for p in trends if "ts" in p and metric_key in p])
                ys = [float(p[metric_key]) for p in trends if "ts" in p and metric_key in p]
                if not xs or not ys:
                    continue

                ax.plot(
                    xs,
                    ys,
                    label=name,
                    linewidth=2.0,
                    color=colors.get(name, None),
                    linestyle=linestyles.get(name, "-"),
                )
                plotted = True

            ax.set_title(title, loc="left", fontsize=11, pad=8, color="#E5E7EB")
            ax.set_ylabel(unit, color="#CBD5E1")
            ax.tick_params(colors="#94A3B8", labelsize=8)
            ax.xaxis.set_major_formatter(time_formatter)
            ax.grid(True, color="#334155", alpha=0.35, linewidth=0.8)
            if plotted:
                ax.legend(loc="lower left", fontsize=8, frameon=False, ncol=min(3, len(wanted)))
            else:
                ax.text(0.5, 0.5, "Waiting for data", color="#64748B", ha="center", va="center", transform=ax.transAxes)

        trend_axes[2].set_xlabel("Local time")
        trend_axes[3].set_xlabel("Local time")

        phasor_ax.clear()
        phasor_ax.set_facecolor("#111827")
        for spine in phasor_ax.spines.values():
            spine.set_color("#334155")

        phasor_name = next((name for name in wanted if name in pmu_map), None)
        if phasor_name is None and pmu_map:
            phasor_name = sorted(pmu_map.keys())[0]

        drew = False
        if phasor_name:
            drew = draw_phasor_diagram(phasor_ax, pmu_map.get(phasor_name, {}), phasor_name)
        if not drew:
            phasor_ax.set_title("Phasor Diagram", loc="left", fontsize=11, pad=8, color="#E5E7EB")
            phasor_ax.text(0.5, 0.5, "Waiting for phasor data", color="#64748B", ha="center", va="center", transform=phasor_ax.transAxes)
            phasor_ax.set_xticks([])
            phasor_ax.set_yticks([])

        fig.autofmt_xdate()
        fig.suptitle(f"PDC Live Trends Dashboard  |  {datetime.now().strftime('%H:%M:%S')}", color="#F8FAFC", fontsize=14)

    anim = FuncAnimation(fig, update, interval=max(250, args.refresh_ms), cache_frame_data=False)
    # Keep a strong reference so matplotlib does not garbage-collect the animation.
    _ = anim
    plt.tight_layout(rect=(0, 0, 1, 0.96))
    plt.show()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())