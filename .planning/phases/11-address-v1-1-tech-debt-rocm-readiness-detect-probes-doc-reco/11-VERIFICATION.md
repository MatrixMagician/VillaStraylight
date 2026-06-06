---
phase: 11-address-v1-1-tech-debt-rocm-readiness-detect-probes-doc-reco
verified: 2026-06-06T22:25:10Z
status: passed
score: 12/12 must-haves verified
overrides_applied: 0
---

# Phase 11: Address v1.1 Tech Debt — rocm_readiness Detect Probes + Doc Reconciliation Verification Report

**Phase Goal:** Make `internal/detect/readiness_rocm.go`'s `firmwareDateOK()` / `hsaOverrideViable()` real probes (KnownBool on-hardware, honest UNSET off-hardware) so the Phase-10 ROCm-readiness badge reads `ready` on a live ROCm host (closes the DASH-06 SC#1 residual + the DET-04 readiness fields), and reconcile the audit-named documentation drift (6 missing SUMMARY `requirements-completed` tags + stale 06-REVIEW prose Status line + REQUIREMENTS.md ROCM-02 note).
**Verified:** 2026-06-06T22:25:10Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
| --- | --- | --- | --- |
| 1 | `firmwareDateOK()` returns a real KnownBool when firmware probes successfully (no longer unconditional UnknownBool) | ✓ VERIFIED | `readiness_rocm.go:59-64` — `if !date.Known { UnknownBool } else KnownBool(firmwareDatePolicyOK(date.Value), ...)`. `TestFirmwareDateOK` green (clear/floor → true). |
| 2 | `hsaOverrideViable()` returns a real KnownBool derived from gfx-id + ROCm substrate (no longer unconditional UnknownBool) | ✓ VERIFIED | `readiness_rocm.go:76-81` — `if !gfxID.Known { UnknownBool } else KnownBool(gfxID.Value=="gfx1151" && rocmPresent.Value, ...)`. `TestHSAOverrideViable` green. |
| 3 | Off-hardware (gfx-id Unknown / rpm absent) both fields stay UNSET — no fabricated false (no-false-green, D-08) | ✓ VERIFIED | Both probes guard on `!known → UnknownBool`. `TestFirmwareDateOK` "unprobed firmware" case + `TestHSAOverrideViable` "gfx-id unknown" case assert `wantKnown=false`. Extended `TestComputeROCmReadinessOffHardware` green. |
| 4 | A Known denylisted (20251125) or sub-floor (<20260110) firmware date yields KnownBool(false) | ✓ VERIFIED | `firmwareDatePolicyOK` (gpu_amd.go:412-419): denylist-wins then `compareVersionSegments(date, "20260110")>=0`. Test cases "denylisted date"/"sub-floor not denied" → `wantValue=false`. |
| 5 | A Known clear firmware date (>=20260110, not denied) yields KnownBool(true) | ✓ VERIFIED | Test cases "clear date >= floor" (20260519) + "exactly at floor" (20260110) → `wantValue=true`. Confirmed live on host (20260519 → true). |
| 6 | `cmd/villa/testdata/detect.golden.json` stays byte-identical — TestJSONGolden passes WITHOUT -update (D-04) | ✓ VERIFIED | `go test ./cmd/villa/ -run TestJSONGolden` green without -update; `git diff --stat cmd/villa/testdata/detect.golden.json` shows ZERO changes. Golden is fixture-driven. |
| 7 | [ON-HARDWARE UAT] On live gfx1151 ROCm host, `villa detect --json` shows firmware_date_ok:true + hsa_override_viable:true, and the status/dashboard ROCm-readiness badge reads `ready` — closes DASH-06 SC#1 residual | ✓ VERIFIED (live) | **This verification host IS a live gfx1151 ROCm host** (rocminfo: Name=gfx1151, AMD RYZEN AI MAX+ 395). `villa detect --json` rocm_readiness: `hsa_override_viable {value:true,known:true}`, `firmware_date_ok {value:true,known:true}` (all 5 leaves Known=true). `villa status` → `rocm-readiness  ready` / `backend rocm`; `status --json` → `rocm_readiness = "ready"`. |
| 8 | All six audit-named SUMMARYs carry a requirements-completed tag matching their VERIFICATION-confirmed REQ-ID | ✓ VERIFIED | 07-03=[DET-04], 08-01=[BSET-01,BSET-02], 08-02=[BSET-03], 09-02=[BENCH-02], 09-03=[BENCH-02], 10-02=[REC-05]. Each count=1; format matches green-SUMMARY convention. |
| 9 | 06-REVIEW.md prose Status line reads resolved (not issues_found), consistent with frontmatter | ✓ VERIFIED | `grep -c issues_found` = 0; line 42 `**Status:** resolved`. Frontmatter untouched. |
| 10 | Each tag added only after confirming SATISFIED against sibling VERIFICATION.md (evidence-first, D-05) | ✓ VERIFIED | 11-02-SUMMARY cites per-tag VERIFICATION lines (07:80, 08:85-87, 09:104, 10:86). Cross-referenced against actual VERIFICATION coverage rows. |
| 11 | REQUIREMENTS.md ROCM-02 note corrected only if genuinely stale; otherwise no-edit-needed (Open Q1) | ✓ VERIFIED | Confirmed: line 88 is the `## Out of Scope` section (custom chat UI), NOT a ROCM-02 note. Actual ROCM-02 entries (line 19 requirement, line 104 traceability) are accurate. "No edit needed" was the correct disposition — no fabricated edit. |
| 12 | The audit 3-source frontmatter cross-check no longer flags tagging lag | ✓ VERIFIED | All six tagging-lag SUMMARYs now carry their tag; REVIEW prose reconciled; ROCM-02 confirmed accurate. The three sources (REQUIREMENTS traceability, VERIFICATION coverage, SUMMARY frontmatter) are now consistent. |

**Score:** 12/12 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `internal/detect/gpu_amd.go` | firmwareDateProbe / firmwareDatePolicyOK / isYYYYMMDD + rocmFirmwareFloor/Deny consts | ✓ VERIFIED | All present (lines 366,368,379,395,412); fixed-arg `runTool("rpm",...)`; literals carry CLAUDE.md citation; compiles + vets. |
| `internal/detect/readiness_rocm.go` | real firmwareDateOK(date Str) + hsaOverrideViable(gfxID Str, rocmPresent Bool) bodies | ✓ VERIFIED | Both rewritten (lines 59,76); no unconditional UnknownBool; no firmware literal; no os.Getenv call (only doc comment). |
| `internal/detect/detect.go` | extended computeROCmReadiness call threading rocmPresent + firmwareDateProbe() | ✓ VERIFIED | Line 38: `computeROCmReadiness(gpu.gfxID, kernel, gpu.rocmPresent, firmwareDateProbe(), resolvedROCmImage())`. |
| `internal/detect/readiness_rocm_test.go` | TestFirmwareDateOK + TestHSAOverrideViable + 5 updated call sites + extended off-hardware test | ✓ VERIFIED | 2 new table tests with real assertions; all 5 call sites 5-arg; off-hardware regression extended. |
| 6 SUMMARYs (07-03/08-01/08-02/09-02/09-03/10-02) | requirements-completed frontmatter | ✓ VERIFIED | All present, count=1 each, correct REQ-IDs. |
| `06-REVIEW.md` | prose Status corrected to resolved | ✓ VERIFIED | No issues_found remains; frontmatter untouched. |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| detect.go Probe() | computeROCmReadiness | passes gpu.rocmPresent + firmwareDateProbe() | ✓ WIRED | Exact pattern matched at detect.go:38. |
| readiness_rocm.go firmwareDateOK | firmwareDatePolicyOK (gpu_amd.go seam) | policy compare; no firmware literal in readiness_rocm.go | ✓ WIRED | `firmwareDatePolicyOK` referenced; grep for `2026xxxx/2025xxxx` in readiness_rocm.go returns nothing. |
| each SUMMARY requirements-completed tag | sibling VERIFICATION.md coverage row | evidence-first confirmation | ✓ WIRED | Per-tag VERIFICATION lines cited and cross-checked. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| --- | --- | --- | --- | --- |
| rocm_readiness.firmware_date_ok | firmwareDate Str | `firmwareDateProbe()` → `rpm -q --qf %{VERSION} linux-firmware` | Yes (live: 20260519 → KnownBool true) | ✓ FLOWING |
| rocm_readiness.hsa_override_viable | gfxID + rocmPresent | `probeGPU` (rocminfo) | Yes (live: gfx1151 + rocm present → KnownBool true) | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Live detect produces Known firmware/HSA verdicts | `go run ./cmd/villa detect --json` | firmware_date_ok:{true,known}; hsa_override_viable:{true,known}; all 5 leaves Known=true | ✓ PASS |
| Status badge reads ready on ROCm host | `go run ./cmd/villa status` | `rocm-readiness  ready` / `backend rocm` | ✓ PASS |
| Status JSON fold | `go run ./cmd/villa status --json` | `rocm_readiness = "ready"` | ✓ PASS |
| Probes off-source still UNSET | `go test ./internal/detect/ -run TestComputeROCmReadinessOffHardware` | green | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` declared or implied for this phase. Verification used Go test suite + live `villa detect`/`villa status` invocations (recorded above). N/A.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| DET-04 | 11-01 | `villa detect` reports ROCm readiness fields | ✓ SATISFIED | firmware_date_ok + hsa_override_viable now probe-real; live host shows Known=true; off-hardware honest UNSET. REQUIREMENTS.md:32/108 already Complete (Phase 7); this phase closes the residual probe-real sub-clause. |
| DASH-06 | 11-01 | Dashboard/status ROCm-readiness badge | ✓ SATISFIED | SC#1 residual closed: live badge reads `ready` (non-`unknown`). REQUIREMENTS.md:115 Complete. |
| tech-debt | 11-02 | Cross-cutting doc reconciliation | ✓ SATISFIED | 6 SUMMARY tags added, 06-REVIEW prose fixed, ROCM-02 confirmed accurate (no edit). Not a formal REQ-ID (cross-cutting, as scoped). |

No new formal REQ-IDs expected for this phase (per goal scope — residual sub-clauses + cross-cutting tech-debt). No orphaned requirements: every plan requirement ID accounted for. ROADMAP Phase 11 declares Requirements: DET-04, DASH-06 — both satisfied.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| (none) | — | — | — | No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER in any modified detect source file. No os.Getenv call (only doc comment naming the forbidden read). No `sh -c` introduced. No detect→preflight import. |

### Human Verification Required

None. The single item the plan deferred as MANUAL UAT (live badge=ready on a gfx1151 ROCm host) was verifiable directly because the verification host IS a live gfx1151 ROCm host (AMD RYZEN AI MAX+ 395, rocminfo Name=gfx1151, linux-firmware 20260519). The on-hardware behavior was confirmed live: `firmware_date_ok:true`, `hsa_override_viable:true`, `rocm-readiness ready`, `backend rocm`. No off-hardware-only residual remains.

### Gaps Summary

No gaps. Both plan goals are achieved and verified against the codebase, not the SUMMARY narrative:

- **Plan 11-01 (probes):** The two `readiness_rocm.go` stubs are genuinely real — guarded `KnownBool`/`UnknownBool` derivations backed by a fixed-arg `rpm` firmware probe and a policy seam in `gpu_amd.go`. No-false-green discipline (D-08) holds (Unknown sources → UNSET). The detect golden is byte-identical (D-04) — no re-freeze. Full suite green (16 packages). Crucially, the on-hardware success criterion was confirmed LIVE on this gfx1151 ROCm host: all five rocm_readiness leaves are Known=true and the status badge reads `ready`.
- **Plan 11-02 (doc reconciliation):** All six audit-named SUMMARYs carry their VERIFICATION-confirmed `requirements-completed` tag (evidence-first), the stale 06-REVIEW prose Status line reads `resolved` (frontmatter untouched), and the ROCM-02 disposition is correctly recorded as "no edit needed" — the audit's line-88 pointer is imprecise (it's the Out-of-Scope section); the real ROCM-02 entries (lines 19, 104) are accurate. No fabricated edit.

All four phase-11 commits (cb229cb, abc8bea, 8be00e0, a39f42b) exist; change scope is exactly the intended file set.

---

_Verified: 2026-06-06T22:25:10Z_
_Verifier: Claude (gsd-verifier)_
