---
phase: 07-rocm-render-unit-preflight-detect
verified: 2026-06-06T00:00:00Z
status: passed
score: 9/9 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  note: initial verification (no prior VERIFICATION.md)
---

# Phase 7: ROCm Render Unit / Preflight Verdict / Detect Readiness — Verification Report

**Phase Goal:** The ROCm Quadlet unit renders correctly as a pure delta over the Vulkan unit, and a reusable ROCm preflight verdict + detect-readiness fields can tell (off-hardware) whether a host is fit for ROCm — refusing with remediation rather than silently degrading. These are the two "is this unit/host valid" pieces the switch verb gates on.
**Verified:** 2026-06-06
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #   | Truth                                                                                                                                                                              | Status     | Evidence |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------- |
| 1   | ROCm install renders `villa-llama.container` with digest-pinned `kyuz0:rocm-7.2.4`, `/dev/kfd`+`/dev/dri`, `render` group, HSA+hipBLASLt env, `-ngl 999 -fa 1 --no-mmap`         | ✓ VERIFIED | `villa-llama-rocm.container.golden` lines 8 (`rocm-7.2.4@sha256:2da150c1…`), 10-11 (kfd+dri), 12-13 (keep-groups+render), 14-15 (HSA=11.5.1 + ROCBLAS_USE_HIPBLASLT=1), 19 (`-ngl 999 -fa 1 --no-mmap`). `TestRenderROCmContainerGolden` + `TestRenderROCmEnvGroupFrozen` PASS. |
| 2   | The Vulkan `villa-llama.container` golden is byte-for-byte unchanged (additivity proof)                                                                                          | ✓ VERIFIED | `git log` shows the Vulkan golden last touched by Phase-5 commit `92d53cd`; Phase 7 never modified it. Single `AddDevice=/dev/dri`, single `GroupAdd=keep-groups`, zero `Environment=`. `TestRenderContainerGolden` PASS. Working tree clean for that file. |
| 3   | `parseContainerArgs` collects ALL `--device`, ALL `--group-add`, ALL `--env` (none dropped)                                                                                      | ✓ VERIFIED | `render.go:184-201` — device/group/env cases `append`; env split on first `=`. `TestParseContainerArgsMultiValue` PASS (ROCm multi-value + Vulkan single-device/zero-env). |
| 4   | `villa preflight --backend rocm` refuses with remediation on confident known-bad (firmware==20251125, nightly image, kernel<6.18.4, HSA missing, non-gfx1151)                    | ✓ VERIFIED | `checks_rocm.go` 5 `checkROCm*` funcs each `fail()` only on positively-known-bad with named remediation. `TestRunROCmKnownBadProfileFails`, `TestRunROCm{Gfx,Kernel,Firmware,HSA,Image}` PASS. |
| 5   | Unevaluable BLOCK-tier signal degrades to WARN, NEVER Fail (biased not to over-block)                                                                                            | ✓ VERIFIED | Every check `warn()`s on `!Known`. `TestRunROCmOffHardwareNoFalseFail` (zero StatusFail off-hardware) + `TestPreflightBackendROCmOffHardware` (exit 2, never 1) PASS. |
| 6   | Version ranges + denylists live in a `go:embed` `rocm-policy.json`; migrated v1.0 floors are a behavior no-op                                                                     | ✓ VERIFIED | `rocm-policy.json` carries floors + `firmwareDeny`/`imageDeny`/`requiredHSAOverride`. `floors.go` `//go:embed` + `loadROCmPolicy()`. `TestLoadROCmPolicyMatchesV1Floors`, `TestFloorsSourcedFromPolicy`, `TestPolicyCarriesROCmDenylists` PASS; existing kernel/firmware/compare tests pass unchanged. |
| 7   | `villa detect --json` reports nested `rocm_readiness` appended AFTER the GPU block, schema bumped 1→2, v1.0 contract never reordered                                              | ✓ VERIFIED | `profile.go:11` `hostProfileSchemaVersion = 2`; `ROCmReadiness` at line 56 after the GPU/floor block, `SchemaVersion` stays last (line 60). `detect.golden.json` lines 86-113 confirm order. `TestHostProfileJSONRoundTrips` (schema==2) + `TestJSONGolden` PASS. |
| 8   | Each `rocm_readiness` field is a typed-Optional; off-hardware undetectable signals serialize UNSET (Known=false), distinct from a real false                                     | ✓ VERIFIED | `detect.golden.json`: `hsa_override_viable`/`firmware_date_ok`/`rocminfo_gfx1151` all `known:false`; `kernel_floor_ok`/`image_policy_ok` `known:true`. `TestComputeROCmReadinessOffHardware` PASS. |
| 9   | The re-frozen `detect.golden.json` diff is purely additive (schema 1→2 + appended object, no key moved/renamed)                                                                  | ✓ VERIFIED | Golden shows v1 key order intact, `rocm_readiness` appended before `schema_version`. REVIEW confirmed append-only field order directly. |

**Score:** 9/9 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/orchestrate/testdata/villa-llama-rocm.container.golden` | ROCm byte-golden (kfd+dri, 2 groups, 2 env, rocm digest) | ✓ VERIFIED | Contains `HSA_OVERRIDE_GFX_VERSION=11.5.1`, all delta elements; frozen + intent-guarded. |
| `internal/orchestrate/render.go` | Multi-device/group/env parser (`[]string` + `[]envPair`) | ✓ VERIFIED | `AddDevice`/`GroupAdd` `[]string`, `Env []envPair`, `flEnv` case present; appends not last-wins for those three. |
| `internal/orchestrate/quadlet/container.tmpl` | `{{range}}` over AddDevice/GroupAdd/Env | ✓ VERIFIED | Lines 10-12 range; empty `.Env` renders zero lines; `{{.BackendLabel}}` Description. |
| `internal/preflight/rocm-policy.json` | go:embed policy (floors + denylists + HSA) | ✓ VERIFIED | Carries `rocm7-nightlies`, `20251125`, `11.5.1`, `6.18.4`. |
| `internal/preflight/checks_rocm.go` | `RunROCm`/`RunROCmWithPolicy`, one TierBlock check per signal | ✓ VERIFIED | 5 checks, `ROCM-PRE-*` non-colliding ids, `compareVersions` reused, known-bad→FAIL/unknown→WARN. |
| `cmd/villa/preflight.go` | `--backend rocm` routing; standalone Run unchanged | ✓ VERIFIED | Local `--backend` flag → `RunROCm` when `=="rocm"`, else `preflight.Run`. |
| `internal/detect/profile.go` | `ROCmReadiness` + schema=2, SchemaVersion last | ✓ VERIFIED | Confirmed field order and bump. |
| `internal/detect/readiness_rocm.go` | typed-Optional compute, off-hardware → UnknownBool | ✓ VERIFIED | Drives golden's UNSET signals; `image_policy_ok` config-driven. |
| `cmd/villa/testdata/detect.golden.json` | Re-frozen golden (schema 2 + rocm_readiness) | ✓ VERIFIED | Append-only diff confirmed. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| `render.go parseContainerArgs` | `backend_rocm.go ContainerArgs` | reads --device/--group-add/--env VALUES through the seam | ✓ WIRED | `TestSeamGrepGate` PASS — no backend literal leaked into render.go. |
| `container.tmpl` | `containerView.Env` | `{{range .Env}}Environment=` | ✓ WIRED | Golden renders both env lines. |
| `floors.go Floors()` | `rocm-policy.json` | go:embed + json.Unmarshal (no-op) | ✓ WIRED | `TestFloorsSourcedFromPolicy` PASS. |
| `preflight.go RunE` | `preflight.RunROCm` | `--backend rocm` branch | ✓ WIRED | `TestPreflightBackendROCmOffHardware` PASS. |
| `checks_rocm.go` | `floors.go compareVersions` | reuse tested comparator | ✓ WIRED | `checkROCmKernel` calls `compareVersions`. |
| `profile.go HostProfile` | `ROCmReadiness` | appended `json:"rocm_readiness"` after GPU block | ✓ WIRED | Golden + struct order confirm. |
| `readiness_rocm.go` | `value.go KnownBool/UnknownBool` | typed-Optional (unset != false) | ✓ WIRED | Off-hardware UNSET in golden. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Whole module builds | `go build ./...` | exit 0 | ✓ PASS |
| Full suite green | `go test ./... -count=1` | 481 passed in 17 packages, exit 0 | ✓ PASS |
| ROCm render golden + intent guard | `go test ./internal/orchestrate/ -run TestRenderROCm` | PASS | ✓ PASS |
| Off-hardware no false FAIL | `go test ./internal/preflight/ -run TestRunROCmOffHardwareNoFalseFail` | PASS | ✓ PASS |
| Exit 2 (not 1) off-hardware | `go test ./cmd/villa/ -run TestPreflightBackendROCmOffHardware` | PASS | ✓ PASS |
| Seam grep gate (no literal leak) | `go test ./internal/inference/ -run TestSeamGrepGate` | PASS | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| ROCM-03 | 07-01 | ROCm Quadlet renders correct delta over Vulkan, frozen by byte-golden, Vulkan unchanged | ✓ SATISFIED | Truths 1-3, ROCm + Vulkan goldens, render tests. |
| PRE-06 | 07-02 | Reusable refuse-with-remediation ROCm preflight verdict, externalized policy, biased not to over-block | ✓ SATISFIED | Truths 4-6, `checks_rocm.go`, policy JSON, exit-2 test. |
| DET-04 | 07-03 | `villa detect` reports ROCm readiness, schema-bumped, additive, never reordering v1.0 | ✓ SATISFIED | Truths 7-9, schema=2, append-only golden. |

All three declared requirement IDs (ROCM-03, PRE-06, DET-04) are present in REQUIREMENTS.md, each `[x]` checked and mapped to Phase 7 (Complete). No orphaned requirements — REQUIREMENTS.md maps exactly these three IDs to Phase 7 and all three are claimed by a plan.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| `internal/detect/readiness_rocm.go` | 56-58 | `firmwareDateOK()` permanently returns UnknownBool | ℹ️ Info | Documented no-false-green design (firmware date not probed off-hardware), NOT a stub — serializes UNSET, advisory-only. Future on-hardware probe replaces it. Does not affect goal. |
| `internal/preflight/floors.go` | 42 | `MesaFloor` loaded/embedded but unconsumed | ℹ️ Info | Documented v1.0 deferral (`TODO(phase-2)`); inert tested data, not a goal gap. |

No `TBD`/`FIXME`/`XXX` debt markers found in phase-modified files (grep clean). No console-only handlers, no hardcoded-empty render data, no unwired state.

### Code Review Findings vs. Goal (07-REVIEW.md: 0 critical, 4 warning, 4 info)

| Finding | Undermines goal? | Disposition |
| ------- | ---------------- | ----------- |
| WR-01: `--security-opt` last-wins (scalar, not slice) | No | Both shipped backends emit exactly ONE `--security-opt` (confirmed both goldens). ROCm unit renders correctly today. Latent trap for a hypothetical future 2nd security-opt — acceptable follow-up. |
| WR-02: firmware floor not compared (denylist-only); sub-floor-but-not-denied firmware PASSes | No | SC#2's contracted trigger is "firmware is **exactly** linux-firmware-20251125" — the denylist exact-match is precisely that contract and is implemented + tested. Off-hardware firmware is always UNSET→WARN, so no false-green ships. The sub-floor-PASS is a robustness gap on an advisory axis, not a goal failure. Recommended follow-up. |
| WR-03: `strings.Cut` `ok` ignored on `--env` | No | Both backends use `KEY=VALUE`; env renders correctly (golden lines 14-15). Latent only for a bare `--env FOO` no backend emits. Acceptable follow-up. |
| WR-04: `os.Exit` in cobra RunE | No | The exit-code logic is in the tested `renderPreflight` seam; `os.Exit` is benign today (no deferred resources). Fragility note, not a goal failure. Acceptable follow-up. |
| IN-01..IN-04 | No | Comment accuracy / discoverability / advisory-row labeling — informational. |

The reviewer's own conclusion ("No blockers found … core invariants HOLD") is consistent with this verification. None of the 4 warnings undermine any of the three success criteria; all are correctly classified as advisory follow-ups, not Phase-7 blockers.

### Human Verification Required

None. This phase freezes rendered TEXT and produces off-hardware-testable verdicts only — nothing is run against real Strix Halo hardware in Phase 7 (on-hardware `/dev/kfd`+`/dev/dri` passthrough, render-group privilege, and real firmware/HSA probes are explicitly deferred to Phase 8 per the threat model). Every success criterion is byte-golden- and table-test-verifiable off-hardware, and all such checks pass. No visual, real-time, or external-service behavior is in scope.

### Gaps Summary

No gaps. All 9 must-have truths are VERIFIED, all 9 artifacts pass (exist + substantive + wired + data-flowing), all 7 key links are WIRED, all 3 requirement IDs are SATISFIED and accounted for in REQUIREMENTS.md, the full suite is green (`go build ./...` exit 0; `go test ./... -count=1` 481 passed), and the seam grep-gate holds. The three ROADMAP success criteria are TRUE in the codebase:

1. ROCm unit renders with the exact delta (rocm-7.2.4 digest, kfd+dri, render group, HSA+hipBLASLt env, -ngl 999 -fa 1 --no-mmap), frozen by a new golden, Vulkan golden byte-for-byte unchanged (Phase 7 never touched it — last commit is Phase 5's).
2. ROCm preflight refuses-with-remediation on confident known-bad and WARNs (never FAILs) on unevaluable signals, driven by an externalized go:embed policy, exiting 2 (never 1) off-hardware.
3. `villa detect --json` appends `rocm_readiness` after the GPU block with schema bumped 1→2, typed-Optionals, append-only golden.

The 4 review warnings are advisory robustness gaps (latent/future-backend traps and one advisory-axis false-PASS that cannot fire off-hardware), none of which undermine the phase goal.

---

_Verified: 2026-06-06_
_Verifier: Claude (gsd-verifier)_
