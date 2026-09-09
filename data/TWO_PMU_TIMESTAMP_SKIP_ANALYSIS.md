# Two-PMU Capture — Simple Summary

**Files:** `last_1000_combined_raw.csv`, `last_1000_combined_parsed.csv`  
**PMUs:** pmu-1 and pmu-2 (500 frames each)

---

## Main point

Both PMUs send data steadily at **20 fps** (~every 50 ms).  
But about **every 7 seconds**, each stream **skips one full second** in its timestamp — 20 missing time slots — then continues.

---

## What gets skipped

Each jump drops **one whole second** (20 timestamps at 50 ms steps: `.00` to `.95`).

| Jump | pmu-1 skips | pmu-2 skips |
|------|-------------|-------------|
| 1 | `10:51:46` | `10:51:45` |
| 2 | `10:51:53` | `10:51:52` |
| 3 | `10:52:00` | `10:51:59` |
| 4 | `10:52:07` | `10:52:06` |

- **4 jumps** per stream → **80 missing timestamps** each  
- pmu-2 skips the second **1 second before** pmu-1

**Example (pmu-1, jump 1):**  
Last seen `10:51:45.95` → next seen `10:51:47.00`  
Missing: entire second `10:51:46` (`.00`, `.05`, `.10` … `.95`)

---

## Why alignment is partial

| | Count |
|--|-------|
| Timestamps where **both** PMUs have data | 417 |
| Only pmu-1 | 83 |
| Only pmu-2 | 83 |
| Match rate | ~72% |

Because each stream skips a **different** second, many timestamps don’t line up across PMUs.

---

## Capture-time difference (matching timestamps only)

For the **417 timestamps** where both PMUs have data, we compared `captured_at` (when PDC received each frame):

| Metric | Value |
|--------|-------|
| Average difference | **~115 ms** |
| Median | ~116 ms |
| Range | 97 – 139 ms |

pmu-2 is typically received **~115 ms before** pmu-1 for the same PMU timestamp.

---

## One-liner

> Frames arrive fine at 20 fps, but each PMU periodically skips labelling one full second of timestamps — and the two streams skip offset seconds — so only ~72% of timestamps match across PMUs.
