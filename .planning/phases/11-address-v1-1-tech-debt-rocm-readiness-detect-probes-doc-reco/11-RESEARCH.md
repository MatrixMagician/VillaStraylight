# Phase 11: Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation - Research

**Researched:** 2026-06-06
**Domain:** Go host-fact detection (typed-Optional probes), no-false-green discipline, Fedora `rpm` querying, GSD doc-frontmatter reconciliation
**Confidence:** HIGH (this is a code-grounded cleanup phase; every load-bearing claim is verified against the live tree + this host)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 (firmware probe source):** Probe the installed **linux-firmware build date** via `exec.Command` (fixed args, no shell interpolation), consistent with detect's existing `exec.Command` usage (`internal/detect/gpu_amd.go:243` `runTool`). Parse the date stamp; return **KnownBool only when the date parses**, otherwise `UnknownBool(reason, raw)` (rpm absent / package not installed / unparseable → honest UNSET, never a fabricated false — no-false-green D-08).
- **D-02 (floor/deny compare placement):** Do the floor/deny comparison **behind a detect-local seam in `gpu_amd.go`** (e.g. `firmwareDatePolicyOK(date)`), mirroring `kernelMeetsROCmFloor` / `rocmImagePolicyOK`, so `readiness_rocm.go` carries **no firmware-version literal**. Policy values are the same as preflight's `rocm-policy.json` (`firmwareFloor: 20260110`, `firmwareDeny: ["20251125"]`); **research decides** share-vs-duplicate (no detect→preflight import cycle, no literal in `readiness_rocm.go`). FAIL semantics: Known date in denylist OR below floor → `KnownBool(false)`; Known clear date → `KnownBool(true)`.
- **D-03 (HSA-override viability):** Derive viability from **already-Known host facts**, not a runtime experiment: iGPU is **gfx1151** AND ROCm substrate is present (rocminfo enumerated / kernel floor met). All source facts Known-good → `KnownBool(true)`; Known non-gfx1151 / absent substrate → `KnownBool(false)`; any source fact Unknown → `UnknownBool`. **No container run, no env mutation, no side effects.** Reuse the facts already threaded into `computeROCmReadiness`.
- **D-04 (schema/golden):** **No new fields, no schema bump, no golden re-freeze.** `firmware_date_ok` / `hsa_override_viable` already exist (schema 2). `hostProfileSchemaVersion` stays **2** and `cmd/villa/testdata/detect.golden.json` stays **byte-identical**. **Research must confirm** the detect golden is fixture-driven (not live-host derived); if any harness derives it from the live host, the new probes MUST be injectable/seam-stubbed.
- **D-05 (doc reconciliation):** Fix **exactly the audit-named set**, each corrected only after confirming the claim against its phase **VERIFICATION.md**: add `requirements-completed` SUMMARY frontmatter (DET-04→07-03, BSET-01/02/03→08-01/08-02, BENCH-02→09-02/09-03, REC-05→10-02); correct 06-REVIEW.md stale frontmatter citing the CR-01 fix commit `499644e`; refresh the stale REQUIREMENTS.md ROCM-02 tracking note. Re-confirm the audit's 3-source cross-check passes. **No prose rewriting** beyond frontmatter + the one stale note.
- **D-06 (two-tier verification):** Off-hardware (CI/unit) proves wiring + no-false-green (Unknown→UNSET golden byte-identical; Known-good→`KnownBool(true)`; Known-bad→`KnownBool(false)`). On-hardware UAT confirms live gfx1151 reports `firmware_date_ok`/`hsa_override_viable` Known + the Phase-10 readiness badge reads `ready`. UAT-gated like Phases 8–10. Item 2 verified purely off-hardware.

### Claude's Discretion

- Exact firmware-date parse format and the precise `rpm` query string / fallback probe source (fixed-args, side-effect-free, degrades to `UnknownBool` honestly per D-01).
- Whether firmware floor/deny values are shared from preflight's embedded `rocm-policy.json` or duplicated detect-side (D-02) — provided no detect→preflight import cycle and no literal in `readiness_rocm.go`.
- Exact helper names / file placement for the two probe bodies and the `gpu_amd.go` firmware seam (seam/grep-gate discipline holds).
- Precise frontmatter key spelling for SUMMARY tags (match the existing `requirements-completed` convention in green SUMMARYs).

### Deferred Ideas (OUT OF SCOPE)

- The **~13 advisory hardening warnings** (P8 ×5, P7 ×4 + MesaFloor unconsumed, P6 ×4) — separate hardening effort.
- The **Nyquist VALIDATION.md draft reconciliation** (P6/P8/P9/P10 `nyquist_compliant:false`) — run `/gsd-validate-phase N` separately.
- Any **new detect fields, schema bump, or golden re-freeze**; any **new ROCm capability**.
- Turning preflight's `checkROCmFirmware`/`checkROCmHSA` into probe-consumers (natural follow-up, not this phase).
</user_constraints>

<phase_requirements>
## Phase Requirements

No formal new REQ-IDs. This phase closes **residual sub-clauses** of two already-satisfied requirements.

| ID | Description | Research Support |
|----|-------------|------------------|
| DASH-06 (SC#1 residual) | "…and a ROCm-readiness indicator" — the badge must read non-`unknown` on a live ROCm host (ROADMAP SC#1, line 130). Today it stays `unknown` because two underlying facts are never probed. | Making `firmwareDateOK()`/`hsaOverrideViable()` real KnownBool on-hardware flips `foldROCmReadiness` (status.go:161) and `deriveROCmAdvice` (recommend.go:174) from `unknown` to `ready` with **zero change to the Phase-10 fold** — both already consume worst-wins over the 5 signals. |
| DET-04 (fields probe-real) | `villa detect` reports ROCm readiness — fields `firmware_date_ok` / `hsa_override_viable` currently permanently UNSET. | profile.go:73,76 declares both as `Bool` json `hsa_override_viable`/`firmware_date_ok`; readiness_rocm.go:56,63 are the two stubs to make real. |
</phase_requirements>

## Summary

This is a **small, sharply-scoped post-milestone cleanup** with two independent workstreams: (1) make two detect probes real, (2) reconcile six-plus pieces of documentation drift. Both are HIGH-confidence because every fact is verifiable in the live tree, and the host this research ran on (a Fedora 44 gfx1151 box with `rpm` and `rocminfo` present) directly confirms the firmware-probe mechanics.

The firmware probe is trivial in shape: `rpm -q --qf '%{VERSION}' linux-firmware` returns a clean `YYYYMMDD` numeric stamp (verified live: `20260519`) directly comparable to the policy floor `20260110` and denylist entry `20251125`. The HSA-override probe needs **no host I/O at all** — it is a pure derivation over facts `computeROCmReadiness` already has (gfx-id + kernel + ROCm-present). The single highest-risk correctness point (D-04, golden stability) resolves cleanly: the golden is **fixture-driven** (`cmd/villa/detect_test.go:18 fixtureProfile()`), never derived from `detect.Probe()`, so the new probes cannot perturb it. The only Probe()-touching test (`profile_test.go:13`) asserts "no panic + Arch Known" and does not snapshot firmware/HSA fields.

The doc reconciliation is evidence-checked and verified ready: all six requirements are confirmed `✓ SATISFIED` in their VERIFICATION.md coverage tables, the exact `requirements-completed: [REQ-ID]` key format is established in green SUMMARYs (06-01/06-02/07-01/07-02/09-01/10-01/10-03), and the 06-REVIEW frontmatter `status:`/`critical:` fields turn out to be **already correct** — the genuinely-stale string is the prose `**Status:** issues_found` in the markdown body (line 42). The REQUIREMENTS.md "ROCM-02 note" pointer is the one genuinely ambiguous item and is flagged as an Open Question with a concrete recommendation.

**Primary recommendation:** Add `firmwareDateProbe()` + `firmwareDatePolicyOK(date)` as detect-local helpers in `gpu_amd.go` using `runTool("rpm", "-q", "--qf", "%{VERSION}", "linux-firmware")`; **duplicate** the two firmware policy values detect-side (the established `kernelMeetsROCmFloor` precedent) to avoid a detect→preflight cycle; make both `firmwareDateOK()` and `hsaOverrideViable()` accept their source facts as parameters (thread gfx-id + kernel + rocm-present from `computeROCmReadiness`) so they stay table-testable and Probe() wires the live facts; keep the golden byte-identical (fixture-driven, confirmed). For docs, insert the `requirements-completed` key once per SUMMARY and fix the one stale REVIEW prose line.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| linux-firmware date probe (exec `rpm`) | Backend seam (`internal/detect/gpu_amd.go`) | — | All host-tool execution + ROCm/firmware literals live behind the seam; mirrors `igpuGfxID()`/`runTool` (gpu_amd.go:239,424). |
| firmware floor/deny compare | Backend seam (`gpu_amd.go`) | — | Policy-value literals must stay out of backend-neutral `readiness_rocm.go` (D-02); mirrors `kernelMeetsROCmFloor` (gpu_amd.go:305). |
| HSA-override viability derivation | Readiness compute (`readiness_rocm.go`) | Backend seam (gfx-id source) | Pure fold over already-Known facts, no new I/O (D-03); same shape as `rocminfoGfx1151`/`kernelFloorOK`. |
| readiness sub-tree assembly | Readiness compute (`readiness_rocm.go`) | Orchestrator (`detect.go:36`) | `computeROCmReadiness` is the single assembly point; `Probe()` threads the live facts. |
| badge fold (consume readiness) | Status / Recommend (`status.go`, `recommend.go`) | Dashboard (`dashboard.js`) | UNCHANGED — already worst-wins over 5 signals; flips to `ready` automatically once leaves go Known. |
| doc frontmatter reconciliation | `.planning/` artifacts | — | Pure metadata edits; no code tier involved. |

## Standard Stack

This phase introduces **zero new dependencies**. It extends existing stdlib + in-tree patterns.

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `os/exec` | go 1.26.2 | Fixed-arg host-tool execution via the existing `runTool` helper | Already the detect probe mechanism (gpu_amd.go:239); `rpm` query is the same shape as `rocminfo`/`vulkaninfo`. [VERIFIED: go.mod, gpu_amd.go] |
| Go stdlib `strings`/`strconv` | go 1.26.2 | Parse + numeric-compare the `YYYYMMDD` stamp | Mirrors `splitNumericSegments`/`compareVersionSegments` (gpu_amd.go:314,340). [VERIFIED: gpu_amd.go] |

**No installation step.** No `npm`/`pip`/`cargo`; no Package Legitimacy Audit needed (no external packages added). [VERIFIED: go.mod — module github.com/MatrixMagician/VillaStraylight, go 1.26.2]

### Host tooling (runtime probe targets, not Go deps)
| Tool | Present on dev host | Behavior when absent | Notes |
|------|---------------------|----------------------|-------|
| `rpm` | ✓ `/usr/bin/rpm` | `runTool` → `ok=false` → `UnknownBool` (honest UNSET) | Fedora-native; the firmware-date source. [VERIFIED: live host probe] |
| `rocminfo` | ✓ `/usr/bin/rocminfo` | `igpuGfxID()` → `UnknownStr` → `rocminfo_gfx1151` UNSET → HSA derivation UNSET | Already wired; HSA viability reuses its result. [VERIFIED: live host probe] |

## Architecture Patterns

### System Architecture Diagram

```
                         villa detect / status / recommend / dashboard
                                          │
                              detect.Probe()  (detect.go:17)
                                          │
            ┌─────────────────────────────┼──────────────────────────────┐
            │                             │                               │
      probeGPU()                    kernelVersion()             resolvedROCmImage()
   (gpu_amd.go:45)              (detect.go:30)                  (gpu_amd.go:276)
   ├─ igpuGfxID()  ─── gfx-id Str ──┐         │ kernel Str                 │
   ├─ rocmPresent() ─ rocmPresent Bool ─┐     │                            │
   └─ [NEW] firmwareDateProbe() ─ date Str ─┐ │                            │
                                          │ │ │                            │
                                          ▼ ▼ ▼                            ▼
                         computeROCmReadiness(gfxID, kernel, rocmPresent?, firmwareDate?, resolvedImage)
                                       (readiness_rocm.go:20)
                                          │
        ┌──────────────┬──────────────┬──┴───────────┬──────────────┬──────────────┐
        ▼              ▼              ▼               ▼              ▼              ▼
  hsaOverrideViable  firmwareDateOK  kernelFloorOK  rocminfoGfx1151  imagePolicyOK
   [NEW: derive]     [NEW: compare    (existing)      (existing)       (existing)
   gfx1151 + ROCm     date via                                       
   substrate Known    firmwareDatePolicyOK seam                      
   → KnownBool]       in gpu_amd.go]                                 
        │              │              │               │              │
        └──────────────┴──────────────┴───────────────┴──────────────┘
                                          │
                              ROCmReadiness sub-tree (5 typed-Optional Bool)
                                          │
                          HostProfile.ROCmReadiness  (json: rocm_readiness)
                                          │
        ┌─────────────────────────────────┼─────────────────────────────────┐
        ▼                                 ▼                                   ▼
  foldROCmReadiness               deriveROCmAdvice                  dashboard badge
  (status.go:161)                 (recommend.go:174)                (dashboard.js)
  UNCHANGED worst-wins            UNCHANGED worst-wins              UNCHANGED tri-state
        │                                 │                                   │
        ▼                                 ▼                                   ▼
  badge: unknown→ready            advice: verify-bench→ready        badge: unknown→ready
  (DASH-06 SC#1 residual closed)
```

**Decision points:** each leaf returns `KnownBool` only when its source fact is `Known`; any `Unknown` source → `UnknownBool` → the fold short-circuits to `unknown` (no-false-green). Off-hardware (rpm absent OR rocminfo absent) the badge stays honestly `unknown`.

### Recommended Project Structure (files touched — no new files required for probes)

```
internal/detect/
├── gpu_amd.go          # ADD: firmwareDateProbe() + firmwareDatePolicyOK(date) seam + 2 firmware policy consts
├── readiness_rocm.go   # EDIT: firmwareDateOK(date Str) + hsaOverrideViable(gfxID,kernel,rocmPresent) bodies; thread facts in computeROCmReadiness
├── readiness_rocm_test.go  # ADD: Known-good / Known-bad / Unknown table cases for both probes
├── detect.go           # EDIT: Probe() threads gpu.rocmPresent + new firmware probe into computeROCmReadiness
└── value.go            # UNCHANGED (Bool/Str/KnownBool/UnknownBool already exist)
cmd/villa/
├── detect_test.go      # UNCHANGED — fixtureProfile() keeps both fields UnknownBool (golden stays byte-identical)
└── testdata/detect.golden.json  # UNCHANGED — byte-identical (D-04)
.planning/phases/...    # EDIT: 6 SUMMARY frontmatters + 06-REVIEW prose + REQUIREMENTS.md note
```

### Pattern 1: Detect-local seam for ROCm policy literals (D-02)
**What:** Keep every firmware-version literal in `gpu_amd.go`, never in backend-neutral `readiness_rocm.go`.
**When to use:** Any ROCm/firmware/kernel literal the readiness compute needs.
**Example (the established precedent — duplicate the value detect-side, do NOT import preflight):**
```go
// Source: internal/detect/gpu_amd.go:293-307 (the kernel-floor precedent to mirror)
const rocmKernelFloorTarget = "6.18.4"
func kernelMeetsROCmFloor(kernelVersion string) bool {
    return compareVersionSegments(kernelVersion, rocmKernelFloorTarget) >= 0
}
// NEW firmware analog (recommended):
const rocmFirmwareFloor = "20260110"          // mirrors rocm-policy.json firmwareFloor
var rocmFirmwareDeny = []string{"20251125"}   // mirrors rocm-policy.json firmwareDeny
func firmwareDatePolicyOK(date string) bool {
    for _, bad := range rocmFirmwareDeny {
        if date == bad { return false }       // denylist match → confident false
    }
    return compareVersionSegments(date, rocmFirmwareFloor) >= 0  // below floor → false
}
```
> **Import-cycle confirmation:** `internal/preflight` imports `internal/detect` (checks_gpu.go:6, checks_resources.go:9). Therefore `detect` importing `preflight` would create a cycle and is **forbidden**. `detect` imports `preflight`: **NONE** (verified). Duplication detect-side is the only valid option and is already the established convention (kernel floor, image tag). [VERIFIED: codebase grep]

### Pattern 2: Fixed-arg host probe via `runTool` (D-01)
**What:** Execute `rpm` with a fixed argument slice (never `sh -c`), bounded output, degrade to Unknown.
**Example:**
```go
// Source: internal/detect/gpu_amd.go:239-250 (runTool — fixed args, threat T-01-01)
// NEW probe (recommended), living in gpu_amd.go:
func firmwareDateProbe() Str {
    out, ok := runTool("rpm", "-q", "--qf", "%{VERSION}", "linux-firmware") // fixed args
    if !ok {
        return UnknownStr("rpm query for linux-firmware failed or rpm absent", capRaw(out))
    }
    date := strings.TrimSpace(out)
    if !isYYYYMMDD(date) { // all-digit, length 8 guard
        return UnknownStr("linux-firmware version not a parseable YYYYMMDD stamp", capRaw(out))
    }
    return KnownStr(date, "rpm -q --qf %{VERSION} linux-firmware")
}
```
> **Live verification of the probe output on this host:**
> `rpm -q --qf '%{VERSION}' linux-firmware` → `20260519`
> `rpm -q linux-firmware` → `linux-firmware-20260519-1.fc44.noarch`
> The `%{VERSION}` field is exactly the `YYYYMMDD` stamp — same format as policy floor `20260110` and deny `20251125`. Numeric compare via the existing `compareVersionSegments` works (it splits on `.`, and a bare 8-digit string is one segment compared as an int — but note 8-digit dates exceed nothing problematic; see Pitfall 2). [VERIFIED: live host probe 2026-06-06]

### Pattern 3: Pure derivation over already-Known facts (D-03, no I/O)
**What:** `hsaOverrideViable` is a substrate-readiness statement derived from facts in hand, not a runtime experiment.
**Example:**
```go
// Recommended signature change — thread the source facts so it's pure + table-testable:
func hsaOverrideViable(gfxID Str, rocmPresent Bool) Bool {
    if !gfxID.Known || !rocmPresent.Known {
        return UnknownBool("HSA override viability unevaluable (gfx id or rocm substrate unknown)", gfxID.Raw)
    }
    viable := gfxID.Value == "gfx1151" && rocmPresent.Value
    return KnownBool(viable, "gfx1151 + rocm substrate present (HSA 11.5.1 applies)")
}
```
> **Note (D-03 source-fact choice):** "ROCm substrate present" is best sourced from `rocmPresent` (gpu_amd.go:359 `KnownBool` — rocminfo on PATH) which is **always Known** (it's a confident true/false, never Unknown). To preserve the no-false-green intent that off-hardware → UNSET, gate viability on **gfx-id Known** (rocminfo enumerated a device). Off-hardware rocminfo absent → `igpuGfxID` Unknown → `gfxID.Known==false` → UNSET. This keeps the golden fixture (gfx-id Unknown) producing UNSET. The planner should pick exactly which substrate facts gate Known-ness; `gfxID.Known` is the cleanest single gate. [ASSUMED — see Assumptions Log A1]

### Anti-Patterns to Avoid
- **Importing `internal/preflight` from `internal/detect`** to share policy values — creates an import cycle (preflight→detect already exists). Duplicate the values detect-side.
- **Putting a firmware-version literal in `readiness_rocm.go`** — violates D-02 self-discipline (keep it behind the gpu_amd.go seam). Note: `TestSeamGrepGate` would NOT actually catch a version-number literal (see Common Pitfall 3), so the discipline is convention, not enforced — follow it anyway.
- **Running a container / mutating env** to "test" HSA viability — D-03 forbids side effects; derive from host facts only.
- **Re-freezing `detect.golden.json`** — D-04 forbids it; the golden must stay byte-identical (it will, because it's fixture-driven).
- **Green-washing a SUMMARY tag** without checking its VERIFICATION.md — D-05 requires the evidence check first (all six already verified — see below).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Run `rpm` safely | Custom `os/exec` + `sh -c` | Existing `runTool(name, args...)` (gpu_amd.go:239) | Fixed-arg (threat T-01-01), bounded output (T-01-02), LookPath guard, returns `ok=false` cleanly on absent binary. |
| Numeric version compare | New comparator | Existing `compareVersionSegments`/`splitNumericSegments` (gpu_amd.go:314,340) | Already suffix-tolerant, panic-free, mirrors preflight semantics. |
| Typed Optional Bool | New result type | `Bool` + `KnownBool`/`UnknownBool` (value.go:63-74) | The no-false-green spine; consumers already fold on `.Known`. |
| Worst-wins badge fold | New fold | `foldROCmReadiness` (status.go:161) + `deriveROCmAdvice` (recommend.go:174) | UNCHANGED — they already consume the 5 signals; making leaves Known flips the badge for free. |
| Test injection seam | New mock framework | Thread facts as function params (chosen) OR package-level `var fn = func(){}` (the codebase pattern, e.g. bench.go:465 `benchConfiguredBackend`, uninstall.go:361 `podmanVolumeRm`) | Both are in-tree idioms; parameter-threading is purest for table tests. |

**Key insight:** This phase is almost entirely *wiring existing primitives* — the only genuinely new logic is the `YYYYMMDD` parse guard and the firmware floor/deny compare, both ~10 lines mirroring existing helpers.

## Runtime State Inventory

> This phase is **not** a rename/refactor/migration. It adds two read-only host probes and edits planning docs. No stored data, live-service config, OS-registered state, secrets, or build artifacts carry a renamed string.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — verified: probes are read-only host inspection; no datastore keys involved. | none |
| Live service config | None — verified: no Quadlet/systemd/container config changes; detect is pure host inspection. | none |
| OS-registered state | None — verified: no task/unit registration; `rpm`/`rocminfo` are queried read-only. | none |
| Secrets/env vars | None — verified: `hsaOverrideViable` derives from host facts and does NOT read or mutate `HSA_OVERRIDE_GFX_VERSION` (the render unit owns that env, Phase 7). No secret touched. | none |
| Build artifacts | None — verified: no `go:generate`, no embed change, no package rename; the golden stays byte-identical so no regen artifact. | none |

## Common Pitfalls

### Pitfall 1: Assuming the new probe perturbs the detect golden
**What goes wrong:** Fear that the live `rpm` probe makes `villa detect --json` differ from `detect.golden.json` on the dev host.
**Why it happens:** Misreading the golden test as live-host-derived.
**How to avoid:** The golden is **fixture-driven** — `cmd/villa/detect_test.go:55 TestJSONGolden` renders `fixtureProfile()` (a hand-built `HostProfile`, lines 18-50), which sets `FirmwareDateOK`/`HSAOverrideViable` to explicit `UnknownBool`. It never calls `detect.Probe()`. The golden stays byte-identical with **no** seam-stub required. [VERIFIED: cmd/villa/detect_test.go]
**Warning signs:** Any plan task that says "re-freeze golden" or "stub the probe for the golden test" — both are unnecessary and D-04 forbids the re-freeze.

### Pitfall 2: `compareVersionSegments` with an 8-digit single segment
**What goes wrong:** `20260519` is a single numeric segment; the existing comparator builds an int via `n = n*10 + digit`. `int` on this platform is 64-bit so `20260519` is fine, but a malformed huge stamp could be a concern.
**Why it happens:** The comparator was designed for dotted kernel versions (small ints), not 8-digit dates.
**How to avoid:** It still works correctly for 8-digit YYYYMMDD (well within int64). Add an `isYYYYMMDD` guard (len==8, all digits) BEFORE comparing so a garbage `%{VERSION}` (e.g. an epoch-prefixed or git-snapshot string) degrades to `UnknownStr` rather than a misleading compare. [VERIFIED: gpu_amd.go:340 splitNumericSegments logic]
**Warning signs:** A firmware build with a non-date VERSION (rawhide snapshot tags) — must yield UNSET, not a confident false.

### Pitfall 3: Believing `TestSeamGrepGate` enforces the firmware-literal discipline
**What goes wrong:** Planner assumes putting a firmware version in `readiness_rocm.go` will fail CI.
**Why it happens:** CONTEXT.md D-02 says "so `TestSeamGrepGate` stays green," implying enforcement.
**How to avoid:** The gate (`internal/inference/seam_test.go:34`) matches only four imperative patterns in `internal/`: `runtime.GOOS`, container **image** literals (`kyuz0|docker.io/|server-vulkan|rocm-7.2.4|rocm7-nightlies`), container **device** args, and `podman` invocations. It does **NOT** match firmware date stamps or `HSA_OVERRIDE_GFX_VERSION` within `internal/` (the HSA/marker pattern applies only to the `cmd/villa` walk). So a firmware literal in `readiness_rocm.go` would **not** trip the gate. **Keep the literal in gpu_amd.go anyway** (D-02 self-discipline) — but the planner should not rely on the grep-gate as the guardrail; this is a code-review/convention point. [VERIFIED: internal/inference/seam_test.go]
**Warning signs:** A verification step that claims "TestSeamGrepGate proves no firmware literal leaked" — it doesn't; that property holds by convention.

### Pitfall 4: HSA viability reading the actual env var
**What goes wrong:** Implementing `hsaOverrideViable` by checking `os.Getenv("HSA_OVERRIDE_GFX_VERSION")`.
**Why it happens:** The name suggests "is the override set."
**How to avoid:** D-03 + CONTEXT specifics: viability is a *substrate-readiness* statement ("this host is a gfx1151 where 11.5.1 applies and ROCm enumerates it"), NOT "the env var is currently exported." The render unit owns that env (Phase 7). Derive from `gfxID == "gfx1151" && rocm substrate present`. No `os.Getenv`, no env mutation. [VERIFIED: CONTEXT.md specifics]
**Warning signs:** Any `os.Getenv` / `os.Setenv` referencing `HSA_OVERRIDE_GFX_VERSION` in detect — also a `cmd/villa` seam-gate violation if it leaked there.

### Pitfall 5: Tagging a SUMMARY before checking VERIFICATION.md
**What goes wrong:** Adding `requirements-completed` for a requirement that isn't actually verified.
**How to avoid:** All six are pre-confirmed `✓ SATISFIED` (see Doc-Reconciliation Evidence table below). The plan should still cite the VERIFICATION line per edit (D-05 evidence-first). [VERIFIED: each phase VERIFICATION.md]

## Doc-Reconciliation Evidence (D-05) — verified, with exact edits

### Confirmed satisfaction (the D-05 evidence gate — all PASS)
| REQ | SUMMARY to tag | VERIFICATION.md line confirming SATISFIED |
|-----|----------------|-------------------------------------------|
| DET-04 | 07-03-SUMMARY.md | 07-VERIFICATION.md:80 `✓ SATISFIED` |
| BSET-01 | 08-01-SUMMARY.md | 08-VERIFICATION.md:85 `SATISFIED (logic)` |
| BSET-02 | 08-01-SUMMARY.md | 08-VERIFICATION.md:86 `SATISFIED (logic)` |
| BSET-03 | 08-02-SUMMARY.md | 08-VERIFICATION.md:87 `SATISFIED (logic)` |
| BENCH-02 | 09-02-SUMMARY.md AND 09-03-SUMMARY.md | 09-VERIFICATION.md:104 `✓ SATISFIED` (mapped to 09-02, 09-03) |
| REC-05 | 10-02-SUMMARY.md | 10-VERIFICATION.md:86 `✓ SATISFIED` |

> **Note on BSET-01/02 mapping:** the audit lists 08-01 AND 08-02 for both BSET-01/02 (verified across plans). The cleanest tagging that matches the VERIFICATION mapping: 08-01 gets `[BSET-01, BSET-02]`, 08-02 gets `[BSET-03]`. The planner may instead mirror the audit's exact phrasing (08-01/02 jointly). Either satisfies the 3-source check; recommend per-plan primary-ownership tagging consistent with how 09-02/09-03 both carry BENCH-02.

### Exact frontmatter key format (the convention to replicate)
```yaml
requirements-completed: [ROCM-02]                 # 06-01-SUMMARY.md:49
requirements-completed: [ROCM-01, ROCM-02, ROCM-04]  # 06-02-SUMMARY.md:51
requirements-completed: [ROCM-03]                 # 07-01-SUMMARY.md:47
requirements-completed: [PRE-06]                  # 07-02-SUMMARY.md:49
```
[VERIFIED: grep of green SUMMARYs]

### Insertion anchor per target SUMMARY (two layouts exist)
| SUMMARY | Frontmatter layout | Where to insert `requirements-completed` | Tag |
|---------|--------------------|------------------------------------------|-----|
| 07-03 | nested `metrics:` block (lowercase) | top-level key, sibling to `affects`/`decisions`, before `metrics:` | `[DET-04]` |
| 08-01 | `# Metrics` markdown header (line 48) | top-level key, before the `# Metrics` line | `[BSET-01, BSET-02]` |
| 08-02 | `# Metrics` markdown header (line 48) | before `# Metrics` | `[BSET-03]` |
| 09-02 | nested `metrics:` block | before `metrics:` | `[BENCH-02]` |
| 09-03 | nested `metrics:` block | before `metrics:` | `[BENCH-02]` |
| 10-02 | nested `metrics:` block | before `metrics:` | `[REC-05]` |

> All six currently have `requirements-completed` count = **0** (verified) so there is no risk of a duplicate key. [VERIFIED: grep -c per file]

### 06-REVIEW.md — the actual stale string (audit was imprecise)
- **Frontmatter is ALREADY correct:** line 31 `status: resolved`, line 27 `critical: 0`, and a `resolution.cr-01` block citing `499644e`. [VERIFIED: 06-REVIEW.md:25-35]
- **The genuinely stale string is the prose body:** line 42 `**Status:** issues_found`. This contradicts the (correct) frontmatter `status: resolved`.
- **Recommended edit:** change the prose `**Status:** issues_found` → `**Status:** resolved` (CR-01 fixed in `499644e`, regression-guarded by `TestRunningServerBusyFoldPreservesContract`). This is the one-line truthful correction. Do NOT touch the frontmatter (already right). [VERIFIED: 06-REVIEW.md]

### REQUIREMENTS.md ROCM-02 note — AMBIGUOUS, see Open Question 1
The audit cites "line ~88," but line 88 is in the Out-of-Scope table (unrelated). The ROCM-02 references are line 19 (requirement def, accurate) and line 104 (tracking table: `Complete (residency engine…verified 06-VERIFICATION.md)` — also accurate). No genuinely-stale ROCM-02 note exists at/near line 88. **Recommendation: treat as an Open Question** — the planner/human should confirm intent before editing (see Open Questions). [VERIFIED: grep of all ROCM-02 mentions in REQUIREMENTS.md]

## Code Examples

### Threading the facts in `computeROCmReadiness` (the wiring change)
```go
// Source: internal/detect/readiness_rocm.go:20 (current) + detect.go:36 (caller)
// CURRENT:
func computeROCmReadiness(gfxID Str, kernel Str, resolvedImage string) ROCmReadiness { ... }
//   called: computeROCmReadiness(gpu.gfxID, kernel, resolvedROCmImage())

// RECOMMENDED (thread the two new source facts so both probes are pure + testable):
func computeROCmReadiness(gfxID Str, kernel Str, rocmPresent Bool, firmwareDate Str, resolvedImage string) ROCmReadiness {
    return ROCmReadiness{
        HSAOverrideViable: hsaOverrideViable(gfxID, rocmPresent),
        FirmwareDateOK:    firmwareDateOK(firmwareDate),
        KernelFloorOK:     kernelFloorOK(kernel),
        RocminfoGfx1151:   rocminfoGfx1151(gfxID),
        ImagePolicyOK:     rocmImagePolicyOK(resolvedImage),
    }
}
// detect.go Probe() then passes: gpu.gfxID, kernel, gpu.rocmPresent, firmwareDateProbe(), resolvedROCmImage()
// firmwareDateOK becomes a pure compare over the Str (no I/O); firmwareDateProbe() (gpu_amd.go) does the exec.
```
> This keeps **all I/O in gpu_amd.go** (firmwareDateProbe via runTool) and **all literals in gpu_amd.go** (firmwareDatePolicyOK), while readiness_rocm.go stays a pure fold — exactly the existing `kernelFloorOK(kernel)` shape. [VERIFIED: readiness_rocm.go, detect.go]

### firmwareDateOK pure-compare body (readiness_rocm.go)
```go
// firmwareDateOK reports whether the linux-firmware date clears the policy.
// KnownBool only when the date Str is Known; else UnknownBool (no-false-green).
func firmwareDateOK(date Str) Bool {
    if !date.Known {
        return UnknownBool("firmware date not probed (rpm absent or unparseable)", date.Raw)
    }
    return KnownBool(firmwareDatePolicyOK(date.Value), "firmware date vs floor/denylist")
}
```

### Existing consumers that flip automatically (NO change needed — proof)
```go
// status.go:161 foldROCmReadiness — already folds all 5 incl. FirmwareDateOK/HSAOverrideViable
// recommend.go:174 deriveROCmAdvice — already folds the same 5 signals
// Both: any !Known → "unknown" wins; all Known-good → "ready"/"worth-trying".
// Making the two leaves Known on-hardware is sufficient to flip the badge.  [VERIFIED]
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `firmwareDateOK()` / `hsaOverrideViable()` return unconditional `UnknownBool` (stubs) | Real probes: `rpm` firmware-date query + pure gfx1151/substrate derivation | This phase | Badge reads `ready` on live ROCm host; DASH-06 SC#1 residual closed |
| Firmware date "not probed in Phase 1" (preflight degrades to WARN) | Detect now probes it (preflight unchanged this phase) | This phase (detect only) | Preflight consuming the probed fact is explicitly deferred |

**Deprecated/outdated:**
- The two stub comments in `readiness_rocm.go:52-65` ("not probed … advisory only off-hardware") become inaccurate once probes are real — update them to describe the live behavior + the off-hardware UNSET path.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The cleanest single Known-ness gate for `hsaOverrideViable` is `gfxID.Known` (off-hardware rocminfo absent → gfx-id Unknown → UNSET, preserving golden + no-false-green). Using `rocmPresent` alone would NOT preserve UNSET because `rocmPresent` is always Known. | Pattern 3 / D-03 | LOW — if the planner gates on `rocmPresent.Value` only, off-hardware (rocminfo absent → rocmPresent Known=false) would yield `KnownBool(false)` instead of UNSET, breaking no-false-green AND the off-hardware test expectation. Must gate on `gfxID.Known`. Flag for plan-check. |
| A2 | `rpm -q --qf '%{VERSION}'` is the firmware-date source on all target Fedora 44 hosts (verified on this dev host). | D-01 | LOW — if a host uses a non-rpm firmware install, the probe returns UNSET (honest), not wrong. Acceptable degradation. |

## Open Questions

1. **What is the "stale REQUIREMENTS.md ROCM-02 note" (audit's line ~88)?**
   - What we know: The audit (`v1.1-MILESTONE-AUDIT.md:126`) names "REQUIREMENTS.md line 88 ROCM-02 tracking note stale." Line 88 is in the Out-of-Scope table (unrelated). ROCM-02 appears at line 19 (def — accurate) and line 104 (tracking — accurate). [VERIFIED]
   - What's unclear: No genuinely-stale ROCM-02 note exists at the cited location. The pointer may be a line-number drift from an earlier REQUIREMENTS.md revision, or the audit may have meant a different ID (e.g. the DET-04 line-32 or DASH-06 line-115 tracking entries that don't yet reflect the now-real probes).
   - **Recommendation:** Add a `checkpoint:human-verify` (or a discuss note) asking the user to identify the exact stale line before editing. If none can be identified, the truthful outcome is "no edit needed — the ROCM-02 entries are accurate," and the plan should record that the audit pointer was imprecise. Do NOT invent an edit to satisfy a stale pointer. This is the only genuinely-uncertain item in the phase.

2. **BSET-01/02 tagging granularity (08-01 vs 08-02).**
   - What we know: VERIFICATION maps both to 08-01 AND 08-02. [VERIFIED]
   - What's unclear: Whether to tag each plan with its primary requirement or mirror the joint mapping.
   - **Recommendation:** Tag per primary ownership (08-01: `[BSET-01, BSET-02]`, 08-02: `[BSET-03]`) consistent with how BENCH-02 is jointly tagged on 09-02/09-03. Either form passes the 3-source check; this is low-stakes.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | go 1.26.2 | — |
| `rpm` | firmware-date probe (D-01) | ✓ | /usr/bin/rpm | Probe → `UnknownBool` (honest UNSET) on absence |
| `rocminfo` | HSA-viability + gfx-id source (D-03) | ✓ | /usr/bin/rocminfo | gfx-id Unknown → readiness UNSET on absence |
| live gfx1151 ROCm host | on-hardware UAT (D-06) | ✓ (this dev host is gfx1151) | — | UAT-gated; off-hardware tier proves wiring independently |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none on this host; on a non-Fedora CI runner `rpm`/`rocminfo` absence degrades both probes to UNSET (the off-hardware contract — golden stays green).

## Validation Architecture

> nyquist_validation is **enabled** (`.planning/config.json` workflow.nyquist_validation: true).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table tests + `-update` goldens) |
| Config file | none — `go test` discovers `*_test.go` |
| Quick run command | `go test ./internal/detect/...` |
| Full suite command | `go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DET-04 (firmware probe Known-good) | Known clear date → `KnownBool(true)` | unit | `go test ./internal/detect/ -run TestFirmwareDate -x` | ❌ Wave 0 (add cases to readiness_rocm_test.go) |
| DET-04 (firmware probe Known-bad) | Denylist date (20251125) OR sub-floor → `KnownBool(false)` | unit | `go test ./internal/detect/ -run TestFirmwareDate -x` | ❌ Wave 0 |
| DET-04 (firmware probe Unknown) | rpm absent / unparseable → UNSET | unit | `go test ./internal/detect/ -run TestFirmwareDate -x` | ❌ Wave 0 |
| DET-04 (HSA viability Known-good) | gfx1151 + substrate → `KnownBool(true)` | unit | `go test ./internal/detect/ -run TestHSAOverride -x` | ❌ Wave 0 |
| DET-04 (HSA viability Known-bad) | non-gfx1151 → `KnownBool(false)` | unit | `go test ./internal/detect/ -run TestHSAOverride -x` | ❌ Wave 0 |
| DET-04 (HSA viability Unknown) | gfx-id Unknown → UNSET | unit | `go test ./internal/detect/ -run TestHSAOverride -x` | ❌ Wave 0 |
| D-04 (golden byte-identical) | `villa detect --json` over fixture == golden | golden | `go test ./cmd/villa/ -run TestJSONGolden` | ✅ exists (must stay green, no -update) |
| D-04 (off-hardware no-false-green) | fixture Unknowns → UNSET | unit | `go test ./internal/detect/ -run TestComputeROCmReadinessOffHardware` | ✅ exists (extend to assert both new probes still UNSET with Unknown inputs) |
| DASH-06 SC#1 (badge fold) | all-Known-good → `ready` | unit | `go test ./internal/status/ -run TestFoldROCmReadiness` | ✅ exists (status_test.go:279-293 already covers good/bad/unset) |
| D-05 (doc cross-check) | 6 SUMMARYs tagged, REVIEW prose fixed | manual/grep | `grep -rl requirements-completed .planning/phases/{07,08,09,10}-*` | manual |

### Sampling Rate
- **Per task commit:** `go test ./internal/detect/...` (fast, the changed package)
- **Per wave merge:** `go test ./internal/detect/... ./cmd/villa/... ./internal/status/... ./internal/recommend/...`
- **Phase gate:** `go test ./...` green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/detect/readiness_rocm_test.go` — add `TestFirmwareDateOK` (Known-good/Known-bad-deny/Known-bad-subfloor/Unknown) and `TestHSAOverrideViable` (gfx1151+substrate/non-gfx1151/Unknown) table cases; covers DET-04 probe wiring.
- [ ] `internal/detect/readiness_rocm_test.go` — extend `TestComputeROCmReadinessOffHardware` to assert both new probes still UNSET when their source facts are Unknown (no-false-green regression guard).
- No framework install needed (Go stdlib `testing` in use). No new test file required — extend the existing `readiness_rocm_test.go`.

## Security Domain

> security_enforcement is **enabled** (workflow.security_enforcement: true; security_asvs_level: 1; security_block_on: high).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth surface in this phase. |
| V3 Session Management | no | No sessions. |
| V4 Access Control | no | No access-control change. |
| V5 Input Validation | yes | The `rpm` stdout (`%{VERSION}`) is untrusted tool output: bound it (`runTool` already caps at `maxToolOutput` 8 KiB, gpu_amd.go:19) and validate the format (`isYYYYMMDD` guard) before numeric compare. |
| V6 Cryptography | no | No crypto. |

### Known Threat Patterns for {Go detect probe shelling to rpm}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Command injection via shell interpolation | Tampering / EoP | Use `runTool` fixed-arg `exec.Command` (NEVER `sh -c`) — gpu_amd.go:243 already does this; the firmware probe inherits it. No user-controlled arg in the rpm query (the package name is a constant literal). |
| Untrusted-output memory exhaustion | DoS | `runTool` bounds output to `maxToolOutput` (8 KiB) — gpu_amd.go:245-248. |
| Confidently-wrong firmware verdict (false-green) | (data integrity) | No-false-green: unparseable/absent → `UnknownBool` (UNSET), never a fabricated pass/fail. This is the project's core invariant (D-08) and the security-relevant correctness control here. |
| Reading/leaking the HSA env secret | Information disclosure | `hsaOverrideViable` MUST NOT read `os.Getenv("HSA_OVERRIDE_GFX_VERSION")` — derive from host facts only (Pitfall 4). No env value is read or logged. |

No `security_block_on: high` findings anticipated: the probes are read-only, fixed-arg, bounded, and add no network/auth/secret surface.

## Sources

### Primary (HIGH confidence — live codebase + this host)
- `internal/detect/readiness_rocm.go` — the two stubs (lines 56,63) + the KnownBool/UnknownBool mirror templates (rocminfoGfx1151:34, kernelFloorOK:45).
- `internal/detect/gpu_amd.go` — seam home: `runTool`:239, `igpuGfxID`:424, `rocmPresent`:359, `kernelMeetsROCmFloor`:305 (the duplication precedent), `compareVersionSegments`:314, `resolvedROCmImage`:276, `rocmImagePolicyOK`:282.
- `internal/detect/detect.go:36` — the single `computeROCmReadiness` call site (Probe orchestrator).
- `internal/detect/profile.go:70-88` — `ROCmReadiness` struct (fields already exist, schema stays 2).
- `internal/detect/value.go:58-74` — `Bool`/`KnownBool`/`UnknownBool` typed-Optional.
- `cmd/villa/detect_test.go:18-80` — `fixtureProfile()` + `TestJSONGolden` (PROVES the golden is fixture-driven → D-04 safe).
- `internal/detect/readiness_rocm_test.go` + `profile_test.go` — existing tests to extend; profile_test.go:13 Probe() asserts no-panic only.
- `internal/status/status.go:161` + `internal/recommend/recommend.go:174` — the unchanged worst-wins folds that flip automatically.
- `internal/preflight/floors.go:46,52,80-137` + `rocm-policy.json` — `firmwareFloor 20260110`, `firmwareDeny [20251125]`, the go:embed; CONFIRMS detect must duplicate (preflight→detect import direction).
- `internal/inference/seam_test.go:34` — `TestSeamGrepGate` actual matched patterns (firmware literal NOT enforced; Pitfall 3).
- Live host probes (2026-06-06): `rpm -q --qf '%{VERSION}' linux-firmware` → `20260519`; `rocminfo` present; `go version go1.26.2`.
- VERIFICATION.md coverage tables: 07:80, 08:85-87, 09:104, 10:86 — all `SATISFIED` (D-05 gate).
- Green SUMMARY frontmatters: 06-01:49, 06-02:51, 07-01:47, 07-02:49 — `requirements-completed` format.
- `06-REVIEW.md:25-42` — frontmatter already `resolved`/`critical:0`; prose line 42 is the stale string.
- `.planning/ROADMAP.md:126-148` — DASH-06 goal + SC#1 (the badge residual).
- `.planning/config.json` — nyquist_validation + security_enforcement both true.

### Secondary (MEDIUM confidence)
- `.planning/v1.1-MILESTONE-AUDIT.md` — authoritative tech-debt list; note its "line ~88" REQUIREMENTS pointer is imprecise (Open Question 1).

### Tertiary (LOW confidence)
- None — no external/web sources needed; this phase is fully code-grounded.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new deps; all stdlib + in-tree helpers verified.
- Architecture: HIGH — every seam, caller, and consumer read directly; the wiring change is a 1-line signature extension + 2 helper bodies mirroring existing code.
- Pitfalls: HIGH — golden-safety, import-cycle, and grep-gate scope all verified against the live tree (each corrected a CONTEXT.md assumption).
- Doc reconciliation: HIGH for the 6 SUMMARYs + 06-REVIEW (evidence-confirmed); MEDIUM for the REQUIREMENTS ROCM-02 note (Open Question 1 — audit pointer imprecise).

**Research date:** 2026-06-06
**Valid until:** 2026-07-06 (stable — code-grounded; only the linux-firmware version on the host will drift, which the probe handles by design)
