#!/usr/bin/env python3
"""Update IP for all registered PMUs in InfluxDB."""
import json
import os
import sys
import urllib.request

INFLUX_URL = os.getenv("INFLUX_URL", "http://127.0.0.1:8087")
INFLUX_TOKEN = os.getenv("INFLUX_TOKEN", "my-super-secret-token")
INFLUX_ORG = os.getenv("INFLUX_ORG", "pdc-org")
INFLUX_BUCKET = os.getenv("INFLUX_BUCKET", "synchrophasor")
NEW_IP = sys.argv[1] if len(sys.argv) > 1 else "172.24.108.1"


def flux_query(query: str) -> str:
    req = urllib.request.Request(
        f"{INFLUX_URL}/api/v2/query?org={INFLUX_ORG}",
        data=query.encode(),
        headers={
            "Authorization": f"Token {INFLUX_TOKEN}",
            "Content-Type": "application/vnd.flux",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.read().decode()


def write_pmu(pmu: dict) -> None:
    import time

    ts = int(time.time() * 1_000_000_000)
    name = pmu["name"].replace(",", "\\,").replace(" ", "\\ ")
    fields = [
        f"ip={json.dumps(pmu['ip'])}",
        f"port={int(pmu.get('port', 4712))}i",
        f"idcode={int(pmu.get('idcode', 0))}i",
        f"protocol={json.dumps(pmu.get('protocol', 'tcp'))}",
        f"timeout_sec={int(pmu.get('timeout_sec', 30))}i",
        f"reconnect_sec={int(pmu.get('reconnect_sec', 10))}i",
        f"region={json.dumps(pmu.get('region', ''))}",
        f"lat={float(pmu.get('lat', 0.0))}",
        f"lon={float(pmu.get('lon', 0.0))}",
        "active=1i",
    ]
    line = f"pmu_config,name={name} {','.join(fields)} {ts}"

    req = urllib.request.Request(
        f"{INFLUX_URL}/api/v2/write?org={INFLUX_ORG}&bucket={INFLUX_BUCKET}&precision=ns",
        data=line.encode(),
        headers={
            "Authorization": f"Token {INFLUX_TOKEN}",
            "Content-Type": "text/plain; charset=utf-8",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        resp.read()


def parse_csv(csv_text: str) -> list[dict]:
    """Parse Influx annotated CSV into PMU dicts."""
    lines = [ln for ln in csv_text.splitlines() if ln and not ln.startswith("#")]
    if len(lines) < 2:
        return []

    header = lines[0].split(",")
    idx = {name: i for i, name in enumerate(header)}
    pmus: dict[str, dict] = {}

    for row in lines[1:]:
        cols = row.split(",")
        if len(cols) < len(header):
            continue
        name = cols[idx.get("name", -1)] if "name" in idx else ""
        if not name:
            continue
        pmu = pmus.setdefault(name, {"name": name})
        for field in ("ip", "port", "idcode", "protocol", "timeout_sec", "reconnect_sec", "region", "lat", "lon", "active"):
            if field not in idx:
                continue
            val = cols[idx[field]]
            if val == "":
                continue
            if field in ("port", "idcode", "timeout_sec", "reconnect_sec", "active"):
                pmu[field] = int(float(val))
            elif field in ("lat", "lon"):
                pmu[field] = float(val)
            else:
                pmu[field] = val

    return [p for p in pmus.values() if p.get("active", 1) != 0]


def main() -> None:
    query = f'''
from(bucket:"{INFLUX_BUCKET}")
  |> range(start: 0)
  |> filter(fn: (r) => r._measurement == "pmu_config")
  |> pivot(rowKey:["_time", "name"], columnKey: ["_field"], valueColumn: "_value")
  |> group(columns: ["name"])
  |> sort(columns: ["_time"], desc: true)
  |> limit(n: 1)
'''
    csv_text = flux_query(query)
    pmus = parse_csv(csv_text)
    if not pmus:
        print("No registered PMUs found in InfluxDB.")
        print("Raw response preview:")
        print(csv_text[:800])
        return

    for pmu in pmus:
        old_ip = pmu.get("ip", "")
        pmu["ip"] = NEW_IP
        write_pmu(pmu)
        print(f"updated {pmu['name']}: {old_ip}:{pmu.get('port')} -> {NEW_IP}:{pmu.get('port')}")

    print(f"\nDone. Updated {len(pmus)} device(s) to IP {NEW_IP}.")
    print("Restart PDC or POST each device to /api/pmus for live receivers to reconnect.")


if __name__ == "__main__":
    main()
