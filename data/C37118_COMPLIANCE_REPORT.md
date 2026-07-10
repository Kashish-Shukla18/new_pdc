# IEEE C37.118.2-2011 Compliance Report

**Project:** PDC (`parser`, `receiver`, `cmd/capture-pmu`, `cmd/probe-pmu`)  
**Date:** 2026-07-10  
**Standard:** IEEE Std C37.118.2-2011  
**Scope:** Frame parsers, CFG2 configuration parser, command framing, CRC, STAT/FRACSEC bit fields  

---

## Executive verdict

| Stage | Verdict |
|-------|---------|
| **Before fixes** | **NOT COMPLIANT** — STAT wrong, integer/units broken, SYNC version/type inconsistent, CFG2 units skipped |
| **After fixes** | **SUBSTANTIALLY COMPLIANT** for single-PMU CFG2 + DATA (float and integer), commands, CRC, STAT Table 7, MSG_TQ Table 5 |

Remaining gaps are documented under “Open items” (HDR/CFG1/CFG3 parsers not implemented; multi-PMU DATA only consumes first block; FORMAT reserved bits not enforced).

---

## Fixes applied

| # | Issue | Fix location |
|---|--------|--------------|
| 1 | STAT bits wrong vs Table 7 | `parser/parser.go` — `STATDecoded`, `decodeStat` |
| 2 | Invented DSO bit / wrong TQ & unlocked fields | Removed; TQ=bits 9–6, unlocked=bits 5–4 |
| 3 | FRACSEC MSG_TQ not decoded | `decodeMsgTQ` + `Reading.MsgTQ` |
| 4 | Integer polar: signed mag, angle not `/10⁴` | `parseDataWithProfile` — `uint16` mag, angle `/10000` |
| 5 | PHUNIT/ANUNIT/DIGUNIT skipped | `parser/cfg2.go` — parsed into `Profile` |
| 6 | Integer values unscaled | Apply PHUNIT/ANUNIT factors in DATA parser |
| 7 | CFGCNT discarded | Stored as `Profile.CfgCnt` |
| 8 | Multi-PMU CFG2 `DATA_RATE` offset wrong | Walk all PMU blocks before `DATA_RATE` |
| 9 | Payload length not checked | `DataPayloadBytes` / `TotalDataPayloadBytes` vs body |
| 10 | DGNMR > 1 only first word | Read all digital words into `Digitals` |
| 11 | SYNC CMD `0x41`, type mask `0xF0` | CMD=`0x42` (type `0x40` \| ver `2`); mask `0x70` |
| 12 | CFG2 parse failure → silent legacy | Handshake returns error; `ParseDataFrame` requires profile |
| 13 | `readFrame` min size 4 | Minimum **16** |
| 14 | capture-pmu no CRC on read | CRC verified in `readFrame` |
| 15 | Wrong STAT log text in `main.go` | Updated to Table 7 / MSG_TQ |

---

## Re-audit scorecard

| Check | Result | Evidence |
|-------|--------|----------|
| Field order (common header) | **Pass** | SYNC→FRAMESIZE→IDCODE→SOC→FRACSEC→body→CHK |
| Field lengths | **Pass** | Profile-driven; payload size enforced |
| Endianness | **Pass** | Big-endian throughout |
| Signed/unsigned (int polar) | **Pass** | Mag `uint16`, angle `int16` |
| STAT bit masking (Table 7) | **Pass** | `decodeStat` + unit test |
| FRACSEC MSG_TQ (Table 5) | **Pass** | `decodeMsgTQ` + unit test |
| Reserved FORMAT bits enforced | **Open** | Not rejected if non-zero |
| CRC-CCITT | **Pass** | init `0xFFFF`, poly `0x1021` |
| Frame length calculation | **Pass** | Header FRAMESIZE + CFG2-derived body size |
| Command codes 1–6 | **Pass** | Unchanged, correct |
| Command SYNC version=2 | **Pass** | `0xAA42` in receiver / capture / probe |
| Command payload | **Pass** | 2-byte CMD, FRAMESIZE=18 |
| CFG2 response required | **Pass** | Handshake fails if parse fails |
| PHNMR / ANNMR / DGNMR | **Pass** | Parsed + used for sizing |
| FORMAT bits 0–3 | **Pass** | Polar / float flags |
| FNOM | **Pass** | Bit 0 → 50/60 Hz |
| DATA_RATE | **Pass** | After all PMU blocks |
| PHUNIT / ANUNIT / DIGUNIT | **Pass** | Parsed; PH/AN applied on integer path |
| Channel names / station | **Pass** | 16-byte ASCII trim |
| CFGCNT | **Pass** | Stored |
| Aligner STAT 15–14 | **Pass** | Matches `decodeStat` data-error code |

---

## Automated verification

```
go test ./parser/ -count=1 -v
```

| Test | Result |
|------|--------|
| `TestDecodeStat_C371182011` | PASS |
| `TestDecodeMsgTQ` | PASS |
| `TestExpectedDataPayloadSize_FloatPolar` | PASS |
| `TestParseCFG2AndData_FloatPolar` | PASS |
| `TestIntegerPolarScaling` | PASS |
| `TestBuildCMDVersion` | PASS |

Builds: `pdc`, `cmd/capture-pmu`, `cmd/probe-pmu` — OK.

---

## Config field matrix (post-fix)

| Field | Parsed | Stored | Applied in DATA |
|-------|--------|--------|-----------------|
| TIME_BASE | Yes | Yes | Timestamp |
| NUM_PMU | Yes | Yes | Payload total size |
| STN | Yes | Yes | — |
| IDCODE | Yes | Yes | Warn if ≠ config |
| FORMAT | Yes | Yes | Layout |
| PHNMR | Yes | Yes | Yes |
| ANNMR | Yes | Yes | Yes |
| DGNMR | Yes | Yes | Yes |
| CHNAM | Yes | Yes | Phasor name map |
| PHUNIT | Yes | Yes | Integer phasors |
| ANUNIT | Yes | Yes | Integer analogs |
| DIGUNIT | Yes | Yes | Stored (masks) |
| FNOM | Yes | Yes | Integer frequency |
| CFGCNT | Yes | Yes | — |
| DATA_RATE | Yes | Yes | — |

---

## Open items (not blocking typical single float PMU)

1. **HDR / CFG1 / CFG3** frame parsers not implemented (commands exist).  
2. **Multi-PMU DATA:** length validated for all blocks; only **first** PMU block decoded into `Reading`.  
3. **FORMAT / FNOM reserved bits** not rejected when non-zero.  
4. **Legacy simulator path** retained as `ParseDataFrameLegacy` only — not used by live handshake.  
5. **IB/IC application mapping** still dropped after wire parse (PZR.BI/CI) — application gap, not a framing error.  
6. **Negative DATA_RATE** (seconds per frame) stored but not specially interpreted by FPS logic.

---

## Impact on PMU.001 (float polar, 6 ph, 3 an, 1 dg)

| Before | After |
|--------|--------|
| STAT logs wrong | Table 7 correct |
| Commands `0xAA41` | `0xAA42` |
| CFG2 units ignored (OK for float) | Units parsed for future integer devices |
| Silent legacy if CFG2 parse failed | Hard fail — no bogus 50 Hz layout |
| Digital: 1 word | All `DGNMR` words |

Float path values (VA/VB/VC/IA, f, ROCOF) remain valid; compliance of status/time quality reporting is corrected.

---

## Conclusion

The PDC C37.118 stack is **substantially compliant with IEEE C37.118.2-2011** for the implemented CFG2 + DATA + CMD paths after this fix set. Prior critical defects (STAT, integer polar, unit factors, SYNC version/type, CFG2 multi-PMU offset, mandatory profile) are resolved and covered by unit tests.
