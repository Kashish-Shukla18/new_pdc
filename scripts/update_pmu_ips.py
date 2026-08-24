#!/usr/bin/env python3
"""Update IP for all registered PMUs in Postgres (pmu_config)."""
import os
import sys

try:
    import psycopg2
except ImportError:
    print("Install psycopg2: pip install psycopg2-binary")
    sys.exit(1)

DSN = os.getenv("POSTGRES_DSN", "postgres://pdc:pdc@127.0.0.1:5433/pdc?sslmode=disable")
NEW_IP = sys.argv[1] if len(sys.argv) > 1 else "172.24.108.1"


def main() -> None:
    conn = psycopg2.connect(DSN)
    conn.autocommit = True
    cur = conn.cursor()
    cur.execute(
        "SELECT name, ip, port FROM pmu_config WHERE active = TRUE ORDER BY name"
    )
    rows = cur.fetchall()
    if not rows:
        print("No registered PMUs found in Postgres (pmu_config).")
        return

    for name, old_ip, port in rows:
        cur.execute(
            "UPDATE pmu_config SET ip = %s, updated_at = NOW() WHERE name = %s",
            (NEW_IP, name),
        )
        print(f"updated {name}: {old_ip}:{port} -> {NEW_IP}:{port}")

    print(f"\nDone. Updated {len(rows)} device(s) to IP {NEW_IP}.")
    print("Restart PDC or POST each device to /api/pmus for live receivers to reconnect.")
    cur.close()
    conn.close()


if __name__ == "__main__":
    main()
