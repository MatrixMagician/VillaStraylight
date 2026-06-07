---
phase: 14-saved-bench-reports-compare
verified: 2026-06-07T00:00:00Z
status: human_needed
score: 12/12 must-haves verified
overrides_applied: 0
human_verification:
  - test: "On real gfx1151 hardware, run `villa bench` once on vulkan and once on rocm (same model/quant/ctx/host), then `villa bench --compare`"
    expected: "Two saved JSONL records persist under $XDG_DATA_HOME/villa/bench-reports.jsonl with non-fabricated host_gfx_id captured from detect.Probe(); --compare prints a real cross-backend Δpp/Δtg (exit 0); --list shows both runs"
    why_human: "Requires live AMD Strix Halo GPU + rootless Podman llama-server to produce genuine pp/tg timings and a real (non-empty) host_gfx_id fingerprint; cannot be measured off-hardware. All deterministic logic is verified via injected-Deps tests + binary spot-checks with synthetic records."
---

# Phase 14: Saved Bench Reports + `--compare` Verification Report

**Phase Goal:** Users can persist every `villa bench` run as a versioned saved report under `$XDG_DATA_HOME/villa/` and compare saved reports over time / across models — pp and tg tok/s kept separate (never blended) with a comparability guard that refuses deltas across non-comparable runs. Pairs with Phase 12 to prove the Δtg recovery.
**Verified:** 2026-06-07
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `villa bench` persists a versioned saved report under `$XDG_DATA_HOME/villa/`, recording pp+tg separately and residency-void state (BENCH-03) | ✓ VERIFIED | `persistBenchReport` (bench.go:823) fires after every measurement via `benchstoreWrite`→`benchstore.Append`; `SavedReport` (benchstore.go:49) carries `prompt_per_sec`/`predicted_per_sec` separate + `void_exhausted`/`reason` + `schema_version:1`. Store path `$XDG_DATA_HOME/villa/bench-reports.jsonl` (benchStoreLocation:271). |
| 2 | User can list/view saved reports (`villa bench --list`) (BENCH-04) | ✓ VERIFIED | `--list` flag (bench.go:528) → `runBenchCompare` list branch → `renderBenchList` (bench_compare.go:217). Binary spot-check: table with index/captured_at/model/quant/backend/pp/tg/void, exit 0. |
| 3 | `villa bench --compare` shows pp/tg deltas between saved reports (BENCH-04) | ✓ VERIFIED | `runBenchCompare` compare branch → `benchstore.Compare` → `renderCompareDelta` (bench_compare.go:243) prints Δpp and Δtg on SEPARATE lines. Binary spot-check (vulkan→rocm comparable): Δpp +30.00, Δtg +15.00, exit 0. |
| 4 | `--compare` refuses deltas across mismatched model/quant/host, labeling "not comparable" not a misleading delta (BENCH-04) | ✓ VERIFIED | `Comparable` guard (benchstore.go:153); `renderCompareNotComparable` (bench_compare.go:235). Binary spot-checks: differing model → "not comparable: differing fields: [model]", exit 2; empty host_gfx_id → "[host]", exit 2 (no false-equal). |
| 5 | New `internal/benchstore` package; JSONL carries `schema_version:1` frozen by golden; `Load` fail-closed on unparseable AND unknown-schema | ✓ VERIFIED | Package present; `record.golden` freezes the schema-1 line; `Load` (benchstore.go:254) `continue`s on unmarshal error AND on `r.SchemaVersion != savedReportSchemaVersion` (line 277). Tests: `TestLoadSkipsCorruptLine`, `TestLoadSkipsUnknownSchemaVersion`. |
| 6 | `internal/benchstore` imports NEITHER `internal/inference` NOR `internal/detect`; host fingerprint captured at cmd tier (TestSeamGrepGate green) | ✓ VERIFIED | grep of benchstore.go imports → NONE. `captureBenchFingerprint` (bench.go:380) builds the fingerprint at cmd tier from `detect.Probe()` `.Known`-guarded plain strings. `TestSeamGrepGate` passes. |
| 7 | pp/tg tok/s structurally separate in records AND deltas (no blended throughput key) | ✓ VERIFIED | `SavedSide`/`SavedAB`/`CompareResult` use `prompt_per_sec`/`predicted_per_sec`/`delta_*` only. `TestNoBlendedKey` (benchstore) + `TestBenchCompareNoBlendedKey`/`TestBenchJSONNoBlendedKey` (cmd) assert absence of `tok_per_sec`/`throughput`. |
| 8 | Comparability: comparable iff model+quant+ctx+host match, backend may differ; UNKNOWN host ⇒ not comparable; void side in comparable pair flagged "not authoritative" | ✓ VERIFIED | `Comparable` (benchstore.go:153) checks Model/Quant/Ctx/HostGfxID, excludes Backend; empty HostGfxID → diff "host". `renderCompareDelta` marks void side "[not authoritative — residency void]". Tests: `TestComparableMatrix`, `TestUnknownHost`, `TestBenchCompareVoidSide`. |
| 9 | Exit mapping 0 (ok) / 2 (not comparable) / 1 (error), identical in --json mode | ✓ VERIFIED | `runBenchCompare` returns exitPass/exitWarn/exitBlocked; JSON branch (bench_compare.go:166) returns exitWarn on `!cr.Comparable` even after emitting the contract. Binary spot-checks: comparable=0, not-comparable=2 (human & --json), no-reports=1, flag-combo=1. |
| 10 | XDG write-path guard is meaningful (validates resolved store under resolved data-home root) — fix 4377e4b | ✓ VERIFIED | `benchAssertStoreUnderRoot` (bench.go:301) rejects empty/non-absolute root and `..`-escaping store, validating store vs the resolved `$XDG_DATA_HOME` root (not its own parent). Tests: `TestBenchstoreWriteConfinedToDataDir`, `TestBenchstoreWriteRejectsNonAbsoluteXDG`. |
| 11 | Write failure / config-load failure is LOUD-but-NON-FATAL (never changes exit code); WR-04 zeroed-fingerprint skip | ✓ VERIFIED | `persistBenchReport` (bench.go:823) skips-with-WARN on `config.LoadVilla` error (avoids zeroed fingerprint) and WARNs (not fatal) on write error. Tests: `TestBenchWriteNonFatal`, `TestBenchPersistSkipsOnConfigLoadError`. |
| 12 | `--compare`/`--list` are read-only; reject combination with --ab/--ab-target/-n/--warmup at cobra boundary; --ab persists ONE record | ✓ VERIFIED | Flag-exclusivity guard (bench.go:471-481) rejects live-measurement flag combos and mutual --compare/--list. `--ab` persists one `mode=ab` record (`savedReportFromResult`). Tests: `TestBenchCompareFlagExclusive`, `TestBenchCompareReadOnly`, `TestBenchPersistABOneRecord`. |

**Score:** 12/12 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/benchstore/benchstore.go` | SavedReport + schema const, Fingerprint, Comparable/Compare, Append/Load, Deps seam, path resolver, traversal guard | ✓ VERIFIED | 332 lines; all exports present; detect/inference-free. |
| `internal/benchstore/benchstore_test.go` | schema stability, no-blended grep, void round-trip, comparable matrix, unknown-host, delta, append-via-seam, fail-closed | ✓ VERIFIED | 12 test funcs incl. `TestLoadSkipsUnknownSchemaVersion`, `TestSchemaVersionIsLastField`. |
| `internal/benchstore/testdata/record.golden` | one frozen schema-1 JSONL record | ✓ VERIFIED | Contains `schema_version`, separate pp/tg, `void_exhausted`, fingerprint. |
| `cmd/villa/bench.go` | write hook, fingerprint capture (.Known-guarded), liveBenchstoreDeps (O_APPEND 0600/0700, guarded), benchstoreWrite indirection | ✓ VERIFIED | All present; `benchstore` imported; guard validates against resolved root. |
| `cmd/villa/bench_compare.go` | read-only --compare/--list, comparability render, 0/2/1 exit, --json contract | ✓ VERIFIED | 263 lines; never calls bench.Run/swap/write. |
| `cmd/villa/testdata/bench-compare.json.golden` | frozen --compare --json: comparable delta + not-comparable refusal | ✓ VERIFIED | Both shapes present; `comparable`/`differing_fields`/separate `delta_*`/per-side void flags. |
| `cmd/villa/bench_test.go` | write-hook, fingerprint, non-fatal, ab-one-record, compare-render, refusal, exclusivity, golden, no-blended | ✓ VERIFIED | 20+ phase-14 test funcs; all pass. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `benchstore.Append` | `Deps.AppendLine` | injected func field | ✓ WIRED | benchstore.go:240 calls `d.AppendLine(line)`. |
| `benchstore.Compare` | `Comparable(a.Fp, b.Fp)` | guard gates delta | ✓ WIRED | benchstore.go:202 gates delta on Comparable. |
| `bench.go runBench` | `benchstore.Append` via liveBenchstoreDeps | post-success write hook | ✓ WIRED | `persistBenchReport`→`benchstoreWrite`→`benchstore.Append`. |
| `bench.go fingerprint` | `detect.Probe().IGPUGfxID.Value` | .Known-guarded plain string | ✓ WIRED | captureBenchFingerprint bench.go:380-397. |
| `bench_compare.go` | `benchstore.Load` + `benchstore.Compare` | read → guard → render | ✓ WIRED | runBenchCompare bench_compare.go:131,157. |
| `--compare/--list flags` | cobra flag-exclusivity | reject live-measurement combos | ✓ WIRED | bench.go:471-481. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| List saved reports | `bench --list` (2 synthetic records) | table, exit 0 | ✓ PASS |
| Comparable cross-backend delta | `bench --compare` (vulkan/rocm, same fp) | Δpp +30.00 Δtg +15.00, exit 0 | ✓ PASS |
| Not-comparable (model) | `bench --compare` (different model) | "not comparable: [model]", exit 2 | ✓ PASS |
| Not-comparable (unknown host) | `bench --compare` (empty host_gfx_id) | "not comparable: [host]", exit 2 (no false-equal) | ✓ PASS |
| No/insufficient reports | `bench --compare` (empty store) | remediation, exit 1 | ✓ PASS |
| Flag exclusivity | `bench --compare --ab` | read-only refusal, exit 1 | ✓ PASS |
| JSON exit parity | `bench --compare --json` (not comparable) | exit 2 | ✓ PASS |

### Probe Execution

No conventional `scripts/*/tests/probe-*.sh` probes declared for this phase. Verification used `go test` + binary behavioral spot-checks instead.

| Check | Command | Result | Status |
|-------|---------|--------|--------|
| Full test suite (both packages) | `go test ./internal/benchstore/... ./cmd/villa/...` | 228 passed, exit 0 | PASS |
| Seam grep gate | `go test ./internal/inference -run TestSeamGrepGate` | passed | PASS |
| Build | `go build ./...` | success | PASS |
| Vet | `go vet ./internal/benchstore/... ./cmd/villa/...` | no issues | PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| BENCH-03 | 14-01, 14-02 | `villa bench` persists versioned saved report under `$XDG_DATA_HOME/villa/`, pp/tg separate, records residency-void | ✓ SATISFIED | Truths 1,5,6,7,10,11; persistBenchReport + benchstore.Append. |
| BENCH-04 | 14-01, 14-03 | `villa bench --compare` gated by comparability guard refusing deltas across non-comparable runs | ✓ SATISFIED | Truths 2,3,4,8,9,12; Comparable/Compare + runBenchCompare. |

Both REQUIREMENTS.md IDs for Phase 14 are accounted for; no orphaned requirements (REQUIREMENTS.md maps only BENCH-03/BENCH-04 to Phase 14, both claimed by plans).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | none | — | No TBD/FIXME/XXX, no TODO/HACK/placeholder, no blended throughput key in modified files. |

### Code Review Status

14-REVIEW.md raised WR-01..05 + IN-01..04. The three that touched locked invariants are **resolved in shipped code**:
- **WR-01** (Load no schema validation) → fixed: `Load` rejects `SchemaVersion != 1` (benchstore.go:277), tested.
- **WR-02** (inert traversal guard) → fixed (4377e4b): `benchAssertStoreUnderRoot` validates store vs resolved data-home root, tested.
- **WR-04** (swallowed config error → mislabeled record) → fixed: skip-with-WARN on config-load failure (bench.go:824), tested.

WR-03 (resolver duplication), WR-05 (auto-select most-recent pair only), IN-01..04 are documented non-blocking minor/info items — they do not break any phase-14 goal truth.

### Human Verification Required

#### 1. On-hardware cross-backend saved-report UAT (gfx1151)

**Test:** On a real AMD Strix Halo host, run `villa bench` once on vulkan then once on rocm (same model/quant/ctx/host), then `villa bench --compare` and `villa bench --list`.
**Expected:** Two records persist to `$XDG_DATA_HOME/villa/bench-reports.jsonl` with a genuine (non-empty) `host_gfx_id` from `detect.Probe()`; `--compare` shows a real cross-backend Δpp/Δtg (exit 0); `--list` shows both. This pairs with Phase 12 to prove the Δtg recovery.
**Why human:** Requires live GPU + rootless Podman llama-server to produce genuine pp/tg timings and a real host fingerprint. All deterministic logic is verified off-hardware via injected-Deps tests and binary spot-checks with synthetic records.

### Gaps Summary

No automated gaps. All 12 must-have truths are VERIFIED in the shipped code: the new `internal/benchstore` pure core (detect/inference-free, golden-frozen schema-1, fail-closed Load), the cmd-tier write hook with a meaningful XDG traversal guard and loud-but-non-fatal error handling, and the read-only `--compare`/`--list` surface with structurally-separate pp/tg, the model+quant+ctx+host comparability guard (UNKNOWN host ⇒ not comparable, no false-equal), and the 0/2/1 exit mapping identical in `--json`. Build, vet, full test suite (228 passing) and the seam grep gate are all green; binary behavioral spot-checks confirm every exit path. The single remaining item is the on-hardware (gfx1151) cross-backend UAT — a known manual item, not an automated gap.

---

_Verified: 2026-06-07_
_Verifier: Claude (gsd-verifier)_
