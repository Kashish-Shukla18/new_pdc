# Five-PMU Capture — Burst + Timestamp Skip Pattern

**File:** `audit_parsed.csv` (1500 frames, combined arrival order)  
**PMUs:** pmu-1 … pmu-5 (~300 frames each)  
**Stamp window:** `2026-09-07T04:58:43Z` → `04:58:54Z`  
**CFG DATA_RATE:** 50 fps (20 ms) on all streams  
**PDC:** single `pdc.exe`, serial parse per PMU (dump order matches SOC/fracsec)

---

## Main point

Each stream is **not** a smooth 50 Hz drip.

1. **Burst on the wire:** frames often arrive as a **pair** (near-zero gap), then the socket idles ~**46–50 ms** (`tcp_wait`), then another pair.  
2. **Skip in timestamps:** about every **~3 seconds**, SOC jumps **+2** after `.98` → next label is `(sec+2).00`. One full second of stamps is missing.  
3. Inventory **~33–34 / 50** matches this: dense stamps are 50 fps, but skips remove ~⅓ of the timeline → wall receive ~33 fps.  
4. **PDC is not dropping frames** (`received == parsed`, handler/CRC/quality drops = 0). Long `tcp_wait` means waiting on the device.

---

## Pattern A — Burst (arrival / `captured_at`)

Typical cycle for one PMU:

```text
[frame] [frame]  ---- ~46–50 ms quiet ----  [frame] [frame]  ---- quiet ----
   ↑ pair (burst)                              ↑ pair
```

| PMU   | Frames | Wall FPS | Burst size 1 | Burst size 2 | Burst size 3 | Gaps &lt;2 ms | Gaps 40–60 ms | Median quiet gap |
|-------|--------|----------|--------------|--------------|--------------|--------------|---------------|------------------|
| pmu-1 | 300    | 32.9     | 124          | 85           | 2            | 112          | 180           | 46.4 ms          |
| pmu-2 | 301    | 33.0     | 125          | 85           | 2            | 113          | 181           | 46.4 ms          |
| pmu-3 | 299    | 32.9     | 113          | 90           | 2            | 112          | 180           | 46.4 ms          |
| pmu-4 | 300    | 32.9     | 120          | 87           | 2            | 112          | 181           | 46.4 ms          |
| pmu-5 | 300    | 32.9     | 105          | 90           | 5            | 112          | 181           | 46.4 ms          |

- **Burst size** = frames sharing the same `captured_at` (same millisecond batch).  
- **Quiet gap** ≈ what shows up as live `tcp_wait` ~47 ms (last hop).  
- During continuous stamp stretches, device labels still step **+20 ms** (true 50 fps spacing).

---

## Pattern B — Timestamp skip (SOC / fracsec)

Every skip looks the same:

```text
….98  →  (sec+2).00
SOC N → SOC N+2     (should have been N+1)
```

Example (pmu-1):

```text
04:58:45.98  →  04:58:47.00
Missing: entire second 04:58:46 (.00 … .98) = 50 stamp slots at 20 ms
```

### Skips in this dump

| PMU   | Missed seconds (UTC)     | Jump examples |
|-------|--------------------------|---------------|
| pmu-1 | `:46`, `:49`, `:52`      | `.45.98→.47`, `.48.98→.50`, `.51.98→.53` |
| pmu-2 | `:47`, `:50`, `:53`      | offset **+1 s** vs pmu-1 |
| pmu-3 | `:44`, `:47`, `:50`      | aligned with pmu-2 on later skips |
| pmu-4 | `:46`, `:49`, `:52`      | **same as pmu-1** |
| pmu-5 | `:47`, `:50`, `:53`      | **same as pmu-2** |

Two phase groups (staggered by 1 second):

- **Group A:** pmu-1, pmu-4  
- **Group B:** pmu-2, pmu-3, pmu-5  

When Group A skips a second, Group B usually has it — and the reverse.

---

## Second-by-second grid

`50` = full second (50 samples). `.` = no stamps for that PMU that second. Other numbers = partial (window edge).

```text
UTC sec    pmu1  pmu2  pmu3  pmu4  pmu5
04:58:43      .     .     2     .     .
04:58:44     10     .     .    18     .
04:58:45     50    47    50    50    42
04:58:46      .    50    50     .    50
04:58:47     50     .     .    50     .
04:58:48     50    50    50    50    50
04:58:49      .    50    50     .    50
04:58:50     50     .     .    50     .
04:58:51     50    50    50    50    50
04:58:52      .    50    47     .    50
04:58:53     40     .     .    32     .
04:58:54      .     4     .     .     8
```

---

## Why inventory shows ~33/50 and n-way match is weak

| Metric | Value |
|--------|--------|
| Dense stamp rate (between skips) | **50 fps** |
| Wall / inventory rate | **~33 fps** |
| Rough cause | `50 × (1 − ~⅓ skip time) ≈ 33` |
| Unique stamps (any PMU) | 468 |
| Stamps present on **all 5** | **142 (~30%)** |

Staggered skips mean most timestamps are missing on at least one PMU → poor time alignment across the fleet.

---

## What this is / is not

| | |
|--|--|
| **Is** | Device/sim send pattern: bursty TCP delivery + periodic SOC+2 label skips |
| **Is not** | PDC parse overload, Docker/Kafka throttle, or handler pool drops |
| **Already fixed on PDC** | Out-of-order dump from parallel per-frame parse (now sequential per PMU) |

---

## One-liner

> All five PMUs burst frames in pairs (~50 ms quiet between bursts) and every ~3 s skip labelling one full second of timestamps (two staggered groups); PDC receives ~33 fps with no drops — CFG’s 50 fps is only true inside the non-skip stretches.
