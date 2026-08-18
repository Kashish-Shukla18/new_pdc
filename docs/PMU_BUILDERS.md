# PDC for PMU builders

How this Phasor Data Concentrator (PDC) consumes IEEE C37.118 streams, what stack we use and why, what data we keep, and how to talk about it with someone who builds PMUs.

> We speak as a **PDC that consumes your stream**, not as a competing PMU.

---

## One-line summary

We built a **PDC**: we speak **IEEE C37.118** to PMUs, **buffer** with Kafka so frames aren’t lost under load, **decode** using CFG2, then **fan out** clean readings to a live dashboard, Redis (latest), and InfluxDB (history).

---

## Stack — what we use and why

| Layer | What | Why |
|--------|------|-----|
| **C37.118 over TCP/UDP** | Handshake + DATA stream | Same protocol PMUs already speak — no custom device protocol |
| **Kafka `pmu.raw.frames`** | Raw frames right after TCP | Shock absorber: parse/storage lag must not force drops at the socket |
| **Kafka `pmu.readings`** | Parsed JSON samples | Fan-out: UI and storage consume independently |
| **Go PDC** | Ingress + processor | Many concurrent PMU connections, low overhead |
| **CFG2-driven parser** | Decode DATA from CFG2 layout | Correct for real field PMUs (not a fixed simulator layout only) |
| **Redis** | Latest reading per PMU | Fast “what is now”; survives process restart (TTL) |
| **InfluxDB** | Time-series history | Trends, forensics, later analytics |
| **React + SSE** | Operator UI | Live view for operators |
| **Disk spool** | Failed Kafka/sink writes | Retry when broker/DB is briefly down |
| **CFG2 on disk** | `data/profiles/` | After restart we can still decode DATA before the next handshake |

### Common questions

**Why Kafka?**  
At tens of PMUs × ~100 samples/s, a slow Influx flush or UI must not stall TCP. Kafka decouples **ingest** from **consume**.

**Why not only InfluxDB?**  
Influx is history. Live UI and multi-consumer fan-out need a bus plus a hot latest store. **Redis = now; Influx = then.**

---

## How we work with PMU data

### Connection sequence

1. Dial PMU (TCP/UDP as configured)
2. `CMD_DATA_OFF` (clear stale stream)
3. Optional **HDR**
4. **CFG2** → register channel layout (and persist it)
5. **CMD_DATA_ON** → continuous DATA frames
6. CRC check on frames

### Per DATA frame

1. Land in **raw Kafka** if needed (ordered by PMU key/partition)
2. **Parse with CFG2** → engineering values
3. Light **quality gate** (e.g. clock skew policy)
4. Publish **parsed reading** to `pmu.readings`
5. Consumers:
   - **`pdc-dashboard`** → live SSE / operator UI
   - **`pdc-sink`** → Redis latest + Influx history

**Design point vs a naive “parse then write DB” PDC:**  
We do not block the PMU socket on storage. We **queue raw**, then **queue parsed**, so the device stream is not punished by our downstream latency.

```
PMU ──C37.118──► Ingress ──► Kafka pmu.raw.frames
                                    │
                                    ▼
                              Processor (CFG2 parse)
                                    │
                                    ▼
                              Kafka pmu.readings
                           ┌────────┴────────┐
                           ▼                 ▼
                     pdc-dashboard      pdc-sink
                     (live UI/SSE)   Redis + InfluxDB
```

---

## What data we have after parse

From a DATA frame + CFG2 we build a **`Reading`**.

### Identity / time

- PMU name, IDCODE
- SOC + FRACSEC → timestamp
- Time quality / leap-second flags (MSG_TQ)
- Frame size, sync, CRC validity

### Status

- Full **STAT** decode (data error, CFG change, trigger, sync unlock, TQ, etc.)
- Digital status word(s)

### Measurements

- Named **phasors** (magnitude/angle and rectangular where applicable)
- **Frequency**, frequency deviation, **ROCOF**
- **Analogs** from CFG
- Digitals

### Derived (computed from phasors)

- Active / reactive / apparent power (MW / MVAR / MVA)
- Power factor
- Sequence components (V+, V−, V0)
- Voltage imbalance %
- Phase angle differences (VAB / VBC / VCA style)

### Where each copy lives

| Data | Where |
|------|--------|
| Raw C37.118 frame | Kafka `pmu.raw.frames` |
| Full parsed sample | Kafka `pmu.readings` → Redis + InfluxDB |
| Live operator view | In-memory bus + SSE (seeded from Redis on restart) |
| CFG layout | Memory + `data/profiles/*.json` |

---

## What this is better at (concentration / ops)

Better as a **concentrator and operator path**, not “better than your PMU”:

1. **Protects the stream** — raw Kafka buffer before heavy work
2. **Standards-correct decode** — CFG2-driven, not a hard-coded channel map
3. **Multi-consumer** — one parsed stream → UI + storage without a second TCP session to the PMU
4. **Restart resilience** — CFG2 on disk + Redis latest
5. **Operator visibility** — React dashboard + Prometheus metrics
6. **Scale path** — can split `ingress` / `processor` modes later

### Do not overclaim

- Local compose Kafka is typically **RF=1** (dev)
- Full multi-PMU soak testing waits on real devices
- Some Connectivity UI numbers may be illustrative, not measured network RTT
- We do **not** claim a full IEEE PDC output stream to another PDC unless that is explicitly implemented

---

## 60-second explanation

Your PMU sends C37.118. We terminate that session as a PDC.  
We publish **raw frames** to Kafka so processing delay does not drop packets.  
We decode with **CFG2**, then publish **parsed readings** to a second Kafka topic.  
One consumer group feeds the **live dashboard**; another writes **Redis** (latest) and **InfluxDB** (history).  
CFG2 is also saved to disk so after a PDC restart we can keep decoding.  
Relative to a single-process parse-and-write PDC, we optimize for **no-drop buffering** and **independent consumers** of the same synchrophasor stream.

---

## FAQ for PMU builders

**Do you modify our frames?**  
No. We store/forward raw, then decode a copy.

**Do you support CFG1 / CFG3?**  
Decode is driven from **CFG2** (common PDC path). HDR is optional.

**What reporting rate do you support?**  
Whatever CFG2 `DATA_RATE` advertises; architecture is aimed at ~100 sps × many PMUs.

**How do you handle time sync?**  
We use frame SOC/FRACSEC and expose TQ / STAT sync bits; the quality gate can warn on clock skew.

**Can you concentrate many PMUs?**  
Yes — one session per PMU; Kafka keyed by PMU name; devices can be started/stopped from the API/UI.

**Where is the PDC output today?**  
Kafka `pmu.readings`, Redis (latest), InfluxDB (history), and the live React UI.

---

## Related docs

- [README.md](./README.md) — architecture, run modes, env vars, quick start
- [dashboard/README.md](./dashboard/README.md) — UI proxy ports and pages
