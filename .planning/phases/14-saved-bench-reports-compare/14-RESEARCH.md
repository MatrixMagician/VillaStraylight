# Phase 14: Saved Bench Reports + `--compare` - Research

**Researched:** 2026-06-07
**Domain:** Local append-only persistence (JSONL) of a byte-frozen result contract + read-only comparison with a comparability guard, inside an established pure-core/injectable-seam Go codebase.
**Confidence:** HIGH (this phase is almost entirely composition of in-tree, already-shipped patterns; the only external surface — Go stdlib `encoding/json` + XDG path resolution — is already used identically elsewhere in this repo and verified by reading the code.)

## Summary

Phase 14 adds two capabilities on top of the shipped `villa bench` machinery: (1) **BENCH-03** — persist every bench run as a versioned saved report under `$XDG_DATA_HOME/villa/`, with pp/tg tok/s kept structurally separate and residency-void state recorded; and (2) **BENCH-04** — `villa bench --compare` that lists/views saved reports and prints pp/tg deltas, gated by a comparability fingerprint (model/quant/host) that refuses to emit deltas across non-comparable runs.

This is **not** a research-heavy phase in the "find the right library" sense. Every needed mechanism already exists in-tree and was read during this research: the XDG path + 0600/0700 + path-traversal discipline (`internal/config/villaconfig.go`), the `$XDG_DATA_HOME/villa/...` resolver (`cmd/villa/model.go:modelsDir`), the pure-core + injectable-`Deps` seam (`internal/bench`, `internal/doctor`), the byte-frozen golden + `-update` freeze (`cmd/villa/testdata/*.golden`), and the schema-version-as-last-field append-only contract (`internal/detect/profile.go`). The work is to **compose those patterns into a new `internal/benchstore` pure core** plus a thin cmd-tier write hook and a `--compare`/list/view surface — and, critically, to **freeze the on-disk JSONL record format with `schema_version` from day one via its own golden before any real report is written** (the ROADMAP implementation note; on-disk format is a migration-incurring contract).

**Primary recommendation:** Create a new pure `internal/benchstore` package owning the saved-report record type (`schema_version` as the FIRST or LAST stable field), the JSONL encode/decode, the comparability fingerprint + comparison logic, and an injected `Deps` write seam — and a thin `cmd/villa/bench.go` write hook that fires AFTER a successful `villa bench` run plus a `villa bench --compare` (and `villa bench --list`) read-only surface. Freeze the record format with an own golden (`benchstore`-resident, schema 1) in the FIRST plan/wave, before any writer path is exercised. `benchstore` MUST NOT import `internal/inference` or `internal/detect` (seam-gate + import-purity); the fingerprint fields arrive as plain strings captured at the cmd tier.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Saved-report record type + `schema_version` contract | `internal/benchstore` (pure core) | — | Contract logic with no host I/O; mirrors how `internal/bench` owns `BenchSpec`/`Result` and `internal/detect` owns `HostProfile`+`SchemaVersion`. |
| JSONL encode / decode (append, read-all) | `internal/benchstore` (pure logic) + injected `Deps` for the actual file read/write | cmd-tier `live*Deps` wiring | Pure core does the marshal/parse; the byte-level append + read is an injected `func` field so it's testable off-hardware (mirrors `bench.Deps`, `doctor.Deps`). |
| XDG path resolution + 0600/0700 + traversal guard | cmd-tier `live*Deps` (or a `benchstore` path helper mirroring `config`) | — | Host filesystem concern. `cmd/villa/model.go:modelsDir` and `internal/config` already establish the exact pattern. |
| Comparability fingerprint (model/quant/host) | `internal/benchstore` (pure) | cmd-tier captures the host fields from `detect.HostProfile` and passes them as plain strings | Fingerprint *matching* is pure value comparison; *sourcing* the host facts is a cmd-tier concern so `benchstore` never imports `detect`. |
| pp/tg delta computation (kept separate) | `internal/benchstore` (pure) | — | Same separation discipline as `internal/bench` `abResult` — two metrics, never blended. |
| Write trigger (after a `bench` run completes) | cmd-tier `cmd/villa/bench.go` | `internal/benchstore` write `Deps` | The write hook fires in `runBench` after a successful, non-`--ab` (and/or `--ab`) run; the bench core stays pure/unaware. |
| `--compare` / `--list` / view presentation + exit mapping | cmd-tier `cmd/villa/bench.go` | `internal/benchstore` read+compare | Printing/exit codes live only in the cmd tier (project convention). |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `encoding/json` | Go 1.26.2 (in-tree) | Marshal/parse each saved-report record as one JSON object per line (JSONL) | Already the project's JSON contract tool (`--json` golden contracts everywhere). No new dependency. `[VERIFIED: codebase grep — encoding/json used in cmd/villa/bench.go, model.go, detect golden tests]` |
| Go stdlib `os` / `path/filepath` | Go 1.26.2 (in-tree) | XDG path resolution, append-write (`os.OpenFile` with `O_APPEND|O_CREATE|O_WRONLY`, 0600), dir create (0700), read-all | Exactly how `internal/config` and `cmd/villa/model.go` already resolve `$XDG_DATA_HOME/villa/...` and write 0600/0700. `[VERIFIED: codebase grep]` |
| Go stdlib `bufio` | Go 1.26.2 (in-tree) | `bufio.Scanner` to read JSONL line-by-line on `--compare`/`--list` | Standard JSONL read idiom; no dependency. `[ASSUMED — standard idiom, not yet present in this repo]` |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| Go stdlib `testing` + golden fixtures | in-tree | Freeze the JSONL record format + the `--compare`/`--list` `--json` output via `testdata/*.golden` and the shared `*update` flag | The ONLY test framework in this repo (table-driven + golden, no third-party assert/mock). `[VERIFIED: codebase — cmd/villa/*_test.go, internal/orchestrate/render_test.go]` |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Flat JSONL (one record per line, append-only) | One JSON file per run (`<timestamp>-<fingerprint>.json`) | ROADMAP implementation note explicitly says **"Flat JSONL persistence"** — locked by the roadmap. JSONL is append-friendly (no read-modify-write), trivially listable, and a corrupt trailing line doesn't poison earlier records. Per-file would multiply path-traversal surface and dir-scan cost. Recommend JSONL. |
| `encoding/json` per-line | `database/sql` + SQLite | Massive overdependency for an append-only log; would add CGO risk (the single-static-binary / `CGO_ENABLED=0` constraint forbids cgo-SQLite). Rejected. |
| Storing `detect.HostProfile` (typed-Optional) verbatim in the record | Capture a small set of plain-string host facts as the fingerprint | Embedding `detect.*` types would force `benchstore` to import `detect` (breaks import purity + complicates the seam gate) AND drag the whole Optional JSON shape into the frozen on-disk contract. Capture only the comparability-relevant host fields as plain strings at the cmd tier. |

**Installation:** No new dependencies. (`go.mod` unchanged — all stdlib.)

**Version verification:** No external packages added; nothing to verify on a registry. Go toolchain is the in-tree 1.26.2 (`go.mod`). `[VERIFIED: codebase — go.mod, CLAUDE.md Technology Stack]`

## Package Legitimacy Audit

> This phase adds **no external packages** — it uses only the Go standard library already in use across the repo. The Package Legitimacy Gate is therefore N/A.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| (none) | — | — | — | — | — | No external packages installed |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| BENCH-03 | `villa bench` persists each run as a versioned saved report under `$XDG_DATA_HOME/villa/`, pp/tg separate (never blended), recording residency-void state. | On-disk JSONL record (§ On-Disk Format) with `schema_version` from day one, pp/tg as separate fields mirroring `bench.Stats`/`benchSide`, `VoidExhausted`+`Reason` persisted; XDG path + 0600/0700 mirrors `internal/config` + `cmd/villa/model.go:modelsDir`; write seam injected (§ Write Seam). |
| BENCH-04 | `villa bench --compare` compares saved reports, gated by a comparability guard (same model/quant/host fingerprint) refusing deltas across non-comparable runs. | Comparability fingerprint (§ Env/Host Fingerprint), read-only `--compare`/`--list`/view surface (§ `--compare` Semantics), pp/tg deltas kept separate, "not comparable" label instead of a misleading delta. |

## Architecture Patterns

### System Architecture Diagram

```
                       villa bench [flags]                         villa bench --compare / --list
                              │                                              │
                              ▼                                              ▼
      ┌───────────────────────────────────────┐          ┌──────────────────────────────────────┐
      │ cmd/villa/bench.go : runBench          │          │ cmd/villa/bench.go : runBenchCompare  │
      │  (existing live measure → bench.Run)   │          │  (NEW read-only path)                 │
      └───────────────────────────────────────┘          └──────────────────────────────────────┘
                              │                                              │
              bench.Result (pp/tg separate,                                  │ read all records
              VoidExhausted/Reason)                                          ▼
                              │                          ┌──────────────────────────────────────┐
                              ▼  capture fingerprint     │ benchstore.Load(Deps) → []SavedReport │
      ┌───────────────────────────────────────┐         │  (pure parse via injected ReadAll)    │
      │ build benchstore.SavedReport:          │         └──────────────────────────────────────┘
      │  schema_version, captured_at,          │                            │
      │  BenchSpec, pp/tg stats, void state,    │                            ▼
      │  fingerprint{model,quant,host...}      │         ┌──────────────────────────────────────┐
      └───────────────────────────────────────┘         │ benchstore.Compare(a, b) (pure)        │
                              │                          │  - if !Comparable(a.FP, b.FP):         │
                              ▼                          │      → NotComparable (refuse deltas)   │
      ┌───────────────────────────────────────┐         │  - else: ΔPP = b.PP-a.PP (separate)    │
      │ benchstore.Append(Deps, report) (pure  │         │          ΔTG = b.TG-a.TG (separate)    │
      │  marshal) → injected AppendLine seam    │         └──────────────────────────────────────┘
      └───────────────────────────────────────┘                            │
                              │                                            ▼
                              ▼                          cmd-tier renders table / --json (frozen golden);
      $XDG_DATA_HOME/villa/bench-reports.jsonl           "not comparable" label instead of a delta
      (0600 file, 0700 dir, traversal-guarded, append-only)
```

Data flow to trace: a `villa bench` run produces a `bench.Result` (pp/tg already separate, `VoidExhausted`/`Reason` set); the cmd tier folds that plus the captured comparability fingerprint into a `benchstore.SavedReport` and appends it as one JSONL line via the injected write seam. Later, `villa bench --compare` reads all records via the injected read seam, runs the pure comparability guard, and either prints per-metric deltas or the "not comparable" label.

### Recommended Project Structure

```
internal/benchstore/
├── benchstore.go        # SavedReport type + schema_version const, Fingerprint type,
│                        #   Comparable(), Compare()/Delta result type, Deps seam,
│                        #   Append()/Load() pure cores (marshal/parse), path helper
├── benchstore_test.go   # schema_version stability, fingerprint comparable/not-comparable
│                        #   matrix, pp/tg never-blended, void round-trip, JSONL parse
└── (no embedded assets)

cmd/villa/
├── bench.go             # + write hook in runBench (after success), + --compare/--list
│                        #   flags + runBenchCompare + liveBenchstoreDeps wiring + render
└── bench_test.go        # + write-hook fires test, + --compare render + own --json golden

internal/benchstore/testdata/
└── record.golden                  # ONE frozen JSONL record (schema 1) — the on-disk contract

cmd/villa/testdata/
└── bench-compare.json.golden      # frozen --compare --json output (deltas + not-comparable)
```

### Pattern 1: Pure core owns the contract type + schema_version (mirror `detect.HostProfile`)

**What:** The saved-report record type, its `schema_version`, and the JSONL marshal/parse live in `internal/benchstore` as pure logic. `schema_version` is a stable field (the in-tree convention is `SchemaVersion` as the LAST field, append-only — new fields go above it).
**When to use:** Always, for this phase — it is the BENCH-03 contract.
**Example:**
```go
// Source: pattern from internal/detect/profile.go (hostProfileSchemaVersion + last-field SchemaVersion)
package benchstore

// savedReportSchemaVersion is the self-version of the on-disk saved-report
// contract. Bump ONLY on an incompatible change; new fields are appended above
// SchemaVersion (append-only). Frozen by internal/benchstore/testdata/record.golden from day one.
const savedReportSchemaVersion = 1

// SavedReport is ONE persisted bench run — the byte-frozen JSONL record. pp and
// tg are SEPARATE fields end-to-end (never a blended tok/s). VoidExhausted/Reason
// carry the residency-void honesty. Fingerprint is the comparability key.
type SavedReport struct {
	CapturedAt   string      `json:"captured_at"`   // RFC3339; injected clock (deterministic in tests)
	Mode         string      `json:"mode"`          // "single" or "ab"
	Spec         SavedSpec   `json:"spec"`          // the reproducible BenchSpec (full)
	Single       *SavedSide  `json:"single,omitempty"`
	AB           *SavedAB    `json:"ab,omitempty"`
	VoidExhausted bool       `json:"void_exhausted"`
	Reason       string      `json:"reason,omitempty"`
	Fingerprint  Fingerprint `json:"fingerprint"`
	SchemaVersion int        `json:"schema_version"` // MUST stay last (append-only)
}
```

### Pattern 2: Injected write/read `Deps` seam (mirror `bench.Deps` / `doctor.Deps`)

**What:** The actual byte-level file append and read-all are `func` fields on a `Deps` struct, so `benchstore_test.go` drives the whole flow with an in-memory buffer — no `$XDG_DATA_HOME` touched. The pure core does marshal/parse; the seam does I/O.
**When to use:** Always — this is how every host-touching core in this repo stays testable off-hardware (project convention "Pure-core + injectable-seam").
**Example:**
```go
// Source: pattern from internal/bench/bench.go (Deps of func fields) + internal/config (0600/0700)
type Deps struct {
	// AppendLine appends one already-marshaled JSONL line (with trailing '\n')
	// to the saved-report store. Live wiring opens $XDG_DATA_HOME/villa/
	// bench-reports.jsonl O_APPEND|O_CREATE|O_WRONLY 0600 under a 0700 dir,
	// traversal-guarded (mirror config.assertInsideDir). nil-safe in tests.
	AppendLine func(line []byte) error
	// ReadAll returns the raw store bytes (all JSONL lines) for --compare/--list.
	// Live wiring os.ReadFile's the store; returns (nil,nil) when absent (no
	// reports yet is not an error — read-only by default, like config.LoadVilla).
	ReadAll func() ([]byte, error)
	// Now supplies the capture timestamp so tests are deterministic.
	Now func() time.Time
}
```

### Pattern 3: Byte-frozen golden, append-only evolution (mirror `cmd/villa/testdata/bench.json.golden`)

**What:** Freeze ONE representative JSONL record (`internal/benchstore/testdata/record.golden`) and the `--compare --json` output (`bench-compare.json.golden`) with the shared package-level `*update` flag. The record golden is the on-disk **contract** — it must be frozen in the FIRST plan/wave, BEFORE any real writer path runs, so the format can never silently drift and incur a migration (the explicit ROADMAP note).
**When to use:** Always — `--json`/dashboard/on-disk contracts are byte-frozen in this repo.
**Example:**
```go
// Source: cmd/villa/bench_test.go (golden + *update freeze; *update declared in detect_test.go)
golden := filepath.Join("testdata", "record.golden") // under internal/benchstore/
if *update {
	_ = os.WriteFile(golden, got, 0o644)
	t.Logf("updated %s", golden)
	return
}
want, err := os.ReadFile(golden) // run with -update to create
if !bytes.Equal(got, want) {
	t.Errorf("saved-report record does not match golden ...")
}
```

### Pattern 4: Comparability guard refuses, never fabricates (mirror offload-asserting / fail-closed posture)

**What:** `Compare(a, b)` first checks `Comparable(a.Fingerprint, b.Fingerprint)`. On a mismatch it returns a typed `NotComparable` outcome (with which fields differ) — the cmd tier prints "not comparable" and the differing fields, NEVER a numeric delta. This mirrors the codebase's offload-asserting / fail-closed honesty (a misleading green is worse than an honest refusal).
**When to use:** Always for `--compare` — it is SC#4.

### Anti-Patterns to Avoid

- **Importing `internal/inference` or `internal/detect` into `internal/benchstore`:** breaks import purity, and any backend-marker literal pulled in would risk the `TestSeamGrepGate` walk (which covers all of `internal/` + `cmd/villa`). The fingerprint host fields arrive as plain strings captured at the cmd tier (the same way `bench` receives backend markers only through the injected `Measure` verdict).
- **Read-modify-write of the whole store on each save:** defeats the append-only JSONL choice and races other writers. Append one line.
- **Blending pp and tg anywhere** (a single "tok/s" field in the record or delta): violates BENCH-03/04 and the repo's structural separation. Keep two fields end-to-end.
- **Letting the bench core write the report:** `internal/bench` is print-free, exit-free, and import-minimal by design; the write hook lives in the cmd tier, fed by the pure `bench.Result` + captured fingerprint.
- **Freezing the record golden AFTER writing real reports:** the ROADMAP note is explicit — lock the format first, or you inherit a migration.
- **Storing prompt/response content beyond the reproducible spec:** the existing `bench --json` deliberately carries only the spec (seed/temp/n_predict/prompt-is-fixed) — keep the same discipline (no captured generated text).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| XDG data dir resolution | A bespoke `$HOME/.local/share` joiner | Mirror `cmd/villa/model.go:modelsDir` (XDG_DATA_HOME → UserHomeDir/.local/share → /var/tmp fallback) | Already correct and consistent with `villa model`'s data dir; one canonical resolver shape. |
| Path-traversal-safe write | Ad-hoc string checks | Mirror `config.assertInsideDir` (clean+abs+`filepath.Rel` `..` check) + 0600/0700 | Proven in-tree guard; reuse the exact shape. |
| JSON marshal/parse | A hand-rolled serializer | `encoding/json` with explicit `json:` tags + golden freeze | The project's universal contract tool. |
| Median/stddev for stored stats | Recompute in benchstore | Persist the already-computed `bench.Stats` values (MedianPP/StddevPP/MedianTG/StddevTG) the cmd tier already holds | `internal/bench` already computed them; benchstore stores, never recomputes. |
| Deterministic timestamps in tests | `time.Now()` inline | Inject `Deps.Now` | Keeps the record golden reproducible (same discipline as bench's stubbed Measure). |

**Key insight:** Nearly every primitive this phase needs is already implemented and battle-tested in this exact repo. The risk is NOT building the wrong thing — it's *re-implementing* an in-tree primitive slightly differently. Compose `config`'s write discipline, `model.go`'s XDG resolver, `bench`'s Deps/stats, and `detect`'s schema-version contract.

## On-Disk Format (BENCH-03 detail)

**File:** single append-only `$XDG_DATA_HOME/villa/bench-reports.jsonl` (default `~/.local/share/villa/bench-reports.jsonl`; `/var/tmp/villa/...` last-resort fallback, mirroring `modelsDir`). One JSON object per line. `[CITED: ROADMAP Phase 14 implementation note — "Flat JSONL persistence"]` `[VERIFIED: codebase — cmd/villa/model.go:modelsDir XDG resolution]`

**XDG resolution on this Fedora target:** `XDG_DATA_HOME` is typically unset on Fedora Workstation, so it falls through to `os.UserHomeDir()/.local/share/villa/` (confirmed present on the dev box: `~/.local/share/` exists). Tests set `t.Setenv("XDG_DATA_HOME", t.TempDir())` exactly as `cmd/villa/model_test.go` already does. `[VERIFIED: codebase grep — model_test.go uses t.Setenv("XDG_DATA_HOME", t.TempDir())]`

**Record fields (schema 1):**

| Field (JSON) | Type | Source | Notes |
|--------------|------|--------|-------|
| `schema_version` | int (=1) | `benchstore` const | Frozen day one; last field; bump = incompatible change |
| `captured_at` | string (RFC3339) | injected `Deps.Now` | Deterministic in tests |
| `mode` | string | `bench.Result` | "single" or "ab" (matches existing `benchEntry.mode`) |
| `spec` | object | `bench.BenchSpec` | Full reproducible spec: `reps`, `warmup`, `prompt`, `n_predict`, `seed`, `temp`, `timeout`(or omit), `min_resident`, `ab_target` |
| `single` | object? | `bench.Stats` | pp/tg SEPARATE: `prompt_per_sec`, `prompt_per_sec_stddev`, `predicted_per_sec`, `predicted_per_sec_stddev`, `kept`, `void`, `backend` |
| `ab` | object? | `bench.ABResult` | `from`, `to`, `a`{side}, `b`{side}, `delta_prompt_per_sec`, `delta_predicted_per_sec` |
| `void_exhausted` | bool | `bench.Result.VoidExhausted` | Residency-void honesty (BENCH-03 requires it persisted) |
| `reason` | string (omitempty) | `bench.Result.Reason` | The void-exhaustion explanation |
| `fingerprint` | object | cmd-tier capture | Comparability key (see next section) |

The `single`/`ab` shapes can reuse the existing `benchSide`/`benchAB` JSON tag layout from `cmd/villa/bench.go` so the on-disk numbers are byte-for-byte what `bench --json` already emits — minimizing new contract surface. Decide in planning whether `benchstore` re-declares its own structs (preferred for import purity — `benchstore` must not import `cmd/villa`) or whether the cmd tier maps the existing `benchSide`/`benchAB` into `benchstore` types at the write boundary (cleaner; recommended). `[VERIFIED: codebase — cmd/villa/bench.go benchSide/benchAB JSON tags]`

**`schema_version` embedding + golden freeze:** declare `const savedReportSchemaVersion = 1` in `benchstore`; assert it in a test (mirror `internal/detect/profile_test.go`'s `SchemaVersion != hostProfileSchemaVersion` guard); freeze ONE full record via `internal/benchstore/testdata/record.golden` with the shared `*update` flag BEFORE any writer runs against a real store (ROADMAP note). `[VERIFIED: codebase — internal/detect/profile_test.go; cmd/villa/bench_test.go golden/-update]`

## Env/Host Fingerprint (BENCH-04 comparability detail)

The fingerprint is the comparability key. ROADMAP/REQUIREMENTS specify **model / quant / host fingerprint**. Recommended composition (all captured at the cmd tier as **plain strings**, so `benchstore` never imports `detect`):

| Fingerprint field | Source | Why it gates comparability |
|-------------------|--------|----------------------------|
| `model` | `config.VillaConfig.Model` | Different model → tok/s not comparable. |
| `quant` | `config.VillaConfig.Quant` | Different quant → different compute/memory profile → not comparable. |
| `ctx` | `config.VillaConfig.Ctx` | Context length materially changes pp/tg; recommend including (HIGH value, LOW cost). `[ASSUMED — REQUIREMENTS says "model/quant/host"; ctx is a defensible addition; confirm in discuss-phase]` |
| `backend` | the benched backend (`bench.Result.Backend` / `AB.From`/`To`) | NOTE: backend is what `--compare` often WANTS to vary (vulkan vs rocm on the SAME model/quant/host is the whole point — proving Δtg recovery). **Backend should be RECORDED but is a presentation axis, not necessarily a comparability-blocking field.** Decide in discuss-phase: the comparability guard should block on model/quant/host mismatch but PERMIT a backend difference (that's the intended comparison). `[ASSUMED — design choice; the Phase-12-pairing goal "prove the Δtg recovery" implies cross-backend compare must be allowed]` |
| host: `igpu_gfx_id` | `detect.HostProfile.IGPUGfxID` (`.Value` string, e.g. "gfx1151") | Different GPU → not comparable. |
| host: `kernel_version` | `detect.HostProfile.KernelVersion.Value` | Driver/kernel changes shift throughput; a defensible host-identity component. `[ASSUMED — "host" is unspecified granularity; recommend gfx_id as the primary host key, kernel as secondary; confirm in discuss-phase]` |
| host: `usable_envelope_bytes` or `total_ram_bytes` | `detect.HostProfile` | Memory envelope identity; optional. `[ASSUMED — granularity choice]` |

**Critical design question for discuss-phase:** the exact host-attribute set and whether `backend` is comparability-blocking. The strong recommendation, grounded in the Phase-12 pairing goal ("the alt-image bench runs are the first saved reports that prove the regression recovery"), is: **comparable iff `model` AND `quant` AND host-key (`igpu_gfx_id`) match; backend is allowed to differ (that IS the comparison); a backend-equal/model-or-host-differs pair is "not comparable."** The fingerprint must capture the host facts as plain strings at the cmd tier from `detect.HostProfile` (or a Probe seam), never by importing `detect` into `benchstore`.

**Optional-field gotcha:** `detect.HostProfile` fields are typed-Optional (`Str`/`Bytes` with `Known bool`). At capture time, read `.Value` (and guard `.Known`); persist a plain string. An UNKNOWN host fact (off-hardware) should serialize as an explicit empty/`"unknown"` sentinel so two unknown fingerprints don't falsely compare equal — decide the sentinel in planning (recommend: treat any UNKNOWN host field as "not comparable" rather than equal, preserving the no-false-green posture). `[VERIFIED: codebase — internal/detect/value.go Str/Bytes carry Known bool]`

## Write Seam (BENCH-03 detail)

**Where the write fires:** in `cmd/villa/bench.go:runBench`, AFTER a successful run, before returning the exit code. The pure `bench.Run` stays untouched (it must remain print-free/import-minimal). The cmd tier already holds everything needed: `res bench.Result` (pp/tg, VoidExhausted/Reason), the spec, the configured backend (`benchConfiguredBackend`), and config (model/quant/ctx). It additionally probes/loads the host fingerprint.

**Persistence policy decisions for discuss-phase/planning:**
- **Always persist, or only on a clean (non-void-exhausted) run?** Recommend persist ALWAYS (including void-exhausted runs) but with `void_exhausted: true` recorded — a void run is itself a meaningful data point and BENCH-03 says "recording residency-void state." `[ASSUMED — recommend persist-always; confirm]`
- **Persist `--ab` runs as one record (with the `ab` block) or two single records?** Recommend ONE record with the `ab` block (matches `bench --json`'s `mode:"ab"` shape), since an A/B pair is the canonical "prove the Δtg recovery" artifact. `[ASSUMED]`
- **Failure to write = ?** A write failure must NOT fail the bench (the measurement already succeeded and was printed). Recommend: warn-to-stderr, keep exit code from the measurement (mirror how bench's failed-restore is LOUD-but-non-fatal). `[ASSUMED — recommend non-fatal warn; confirm]`

**Seam wiring (`liveBenchstoreDeps`):** a cmd-tier constructor (mirroring `liveBenchDeps`) wires `Deps.AppendLine` to an `os.OpenFile(store, O_APPEND|O_CREATE|O_WRONLY, 0600)` under a 0700 dir (MkdirAll), traversal-guarded; `Deps.ReadAll` to `os.ReadFile` (returning empty on absent); `Deps.Now` to `time.Now`. Tests pass a `Deps` backed by a `bytes.Buffer`.

## `--compare` Semantics (BENCH-04 detail)

**CLI surface — recommendation: flags on the existing `bench` noun (not subcommands).** The roadmap/requirements phrase it as `villa bench --compare`. Recommend:
- `villa bench --compare` — compare saved reports (default: the two most recent comparable reports, OR most-recent-of-each-backend for the same model/quant/host). Read-only; does NOT run a benchmark, does NOT touch the backend (distinct from `--ab`, which flips live). `[CITED: ROADMAP — "--compare is read-only and distinct from live --ab"]`
- `villa bench --list` — list saved reports (index, captured_at, model/quant/backend, pp/tg, void state) so the user can see/select. (SC#2 "list and view saved bench reports.")
- Selection: decide in discuss-phase whether `--compare` takes explicit indices/ids (e.g. `--compare 3,7`) or auto-selects. Recommend supporting BOTH: bare `--compare` auto-picks a sensible pair; optional selectors refine it. `[ASSUMED — UX choice; confirm in discuss-phase]`
- `--json` on `--compare`/`--list` for the machine contract (frozen golden), consistent with every other verb.

**Mutual-exclusivity validation:** `--compare`/`--list` are read-only and must reject combination with `--ab`/`--ab-target`/`-n`/`--warmup` (the live-measurement flags) as a usage error at the cobra boundary (mirror the existing `--ab-target requires --ab` check). `[VERIFIED: codebase — cmd/villa/bench.go RunE flag-combo validation]`

**Comparison output (pp/tg separate, guard-gated):**
1. Load all records via `benchstore.Load(Deps)`.
2. Select the pair (auto or by selector).
3. `benchstore.Compare(a, b)`:
   - If `!Comparable(a.Fingerprint, b.Fingerprint)` → return `NotComparable{DifferingFields: [...]}`. The cmd tier prints `not comparable: model differs (qwen3 vs llama3)` (or similar) and NO delta. Exit: recommend `exitWarn` (2) — an honest non-comparison, not a hard failure. `[ASSUMED — exit mapping; confirm]`
   - Else compute `DeltaPP = b.MedianPP - a.MedianPP` and `DeltaTG = b.MedianTG - a.MedianTG` as SEPARATE figures (mirror `bench.abResult`), render Δpp / Δtg on separate lines, never a blended number.
4. If a selected record is itself `void_exhausted`, surface that in the comparison (its band is not authoritative) — recommend labeling the side and still allowing the delta but flagged, OR refusing; decide in discuss-phase. `[ASSUMED]`

**Exit-code mapping (recommended):** comparable delta printed → `exitPass` (0); not-comparable → `exitWarn` (2); no/insufficient saved reports → `exitBlocked` (1) with remediation ("run `villa bench` first"). Mirrors the existing 0/2/1 contract used by bench/doctor/preflight. `[VERIFIED: codebase — exitPass/exitWarn/exitBlocked convention]`

## Runtime State Inventory

> Greenfield-additive within an existing codebase (no rename/refactor/migration). The relevant "state" question for THIS phase is the on-disk format itself.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | NEW store `$XDG_DATA_HOME/villa/bench-reports.jsonl` — does not exist yet (no prior bench persistence). No migration of existing data. | Create with `schema_version` from day one; freeze format BEFORE first write (ROADMAP note) so no future migration is incurred. |
| Live service config | None — `--compare` is read-only and never touches Quadlet/systemd/backend. | None. |
| OS-registered state | None. | None. |
| Secrets/env vars | Reads `XDG_DATA_HOME` (already read by `villa model`); no secrets. | None. |
| Build artifacts | New `internal/benchstore` package + goldens; no stale artifacts. | None. |

**The canonical risk:** once a real `bench-reports.jsonl` exists in any user's `~/.local/share/villa/`, the record format is a contract that cannot be changed without a migration. This is exactly why the ROADMAP mandates freezing the golden FIRST. `[CITED: ROADMAP Phase 14 implementation note]`

## Common Pitfalls

### Pitfall 1: Freezing the on-disk golden after writing real reports
**What goes wrong:** A field is renamed/reordered after `bench-reports.jsonl` already has rows → silent contract drift → either a migration or unreadable old rows.
**Why it happens:** Treating the JSONL like ephemeral output rather than a contract.
**How to avoid:** Freeze `internal/benchstore/testdata/record.golden` (schema 1) + the `savedReportSchemaVersion` const-assert test in the FIRST plan/wave, before any live writer path. New fields append above `schema_version`; incompatible change = version bump.
**Warning signs:** A planned task writes reports before a golden exists.

### Pitfall 2: `benchstore` importing `internal/inference` or `internal/detect`
**What goes wrong:** Pulls backend-marker literals (or `detect`'s Optional types) into the pure core; risks `TestSeamGrepGate` (it walks ALL of `internal/`), couples the on-disk format to `detect`'s schema, and breaks core purity.
**Why it happens:** Wanting the host fingerprint "right there."
**How to avoid:** Fingerprint fields are plain strings captured at the cmd tier and PASSED IN — `benchstore` imports nothing backend-specific (mirror how `internal/bench` deliberately imports neither `inference` nor `detect`).
**Warning signs:** An `import "github.com/MatrixMagician/VillaStraylight/internal/detect"` line appears in `internal/benchstore`.

### Pitfall 3: Blending pp and tg in the record or the delta
**What goes wrong:** A single "tok/s" field collapses two metrics that mean different things → violates BENCH-03/04 and a project invariant.
**Why it happens:** Convenience of one number.
**How to avoid:** Two fields end-to-end (`prompt_per_sec` / `predicted_per_sec` and `delta_prompt_per_sec` / `delta_predicted_per_sec`), exactly as `bench.Stats`/`benchSide`/`benchAB` already do. Add a golden grep test that fails on any blended key (the existing `bench_test.go` already has a "no blended tok/s key" assertion to clone).
**Warning signs:** A field named `tok_per_sec` / `throughput` without a pp/tg qualifier.

### Pitfall 4: False-equal fingerprints from UNKNOWN host facts
**What goes wrong:** Two off-hardware runs both serialize host fields as empty → compare equal → a misleading delta across genuinely-unknown hosts.
**Why it happens:** Optional `Known=false` flattening to `""`.
**How to avoid:** Treat any UNKNOWN host fingerprint field as a comparability-BLOCKER (not equal) — preserves the no-false-green posture. Use an explicit sentinel and guard `.Known` at capture.
**Warning signs:** Comparing two records produced off-hardware emits a delta.

### Pitfall 5: Write failure silently failing (or failing) the bench
**What goes wrong:** A persistence error either crashes a successful measurement or is swallowed with no trace.
**Why it happens:** Treating the write as load-bearing for the bench.
**How to avoid:** The measurement already succeeded and printed — a write failure is a LOUD stderr warning, non-fatal (mirror bench's failed-restore-is-loud-but-non-fatal idiom).
**Warning signs:** `runBench` returns `exitBlocked` because the store couldn't be written.

### Pitfall 6: Path-traversal / loose perms on the new store
**What goes wrong:** Store written world-readable or outside the XDG dir.
**How to avoid:** Reuse `config.assertInsideDir` shape + 0600 file / 0700 dir, exactly as `internal/config` and `cmd/villa/model.go` already do.

## Code Examples

### Append one JSONL record (live seam wiring)
```go
// Source: pattern from internal/config/villaconfig.go (0600/0700 + traversal guard)
// + cmd/villa/model.go:modelsDir (XDG_DATA_HOME resolution). Lives in cmd/villa.
func liveBenchstoreDeps() benchstore.Deps {
	store := benchReportsPath() // $XDG_DATA_HOME/villa/bench-reports.jsonl
	dir := filepath.Dir(store)
	return benchstore.Deps{
		AppendLine: func(line []byte) error {
			if err := assertInsideDir(store, dir); err != nil { return err }
			if err := os.MkdirAll(dir, 0o700); err != nil { return err }
			f, err := os.OpenFile(store, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil { return err }
			defer f.Close()
			_, err = f.Write(line) // line already ends with '\n'
			return err
		},
		ReadAll: func() ([]byte, error) {
			b, err := os.ReadFile(store)
			if os.IsNotExist(err) { return nil, nil } // no reports yet ≠ error
			return b, err
		},
		Now: time.Now,
	}
}
```

### Pure comparability guard (in benchstore)
```go
// Source: separation pattern from internal/bench/bench.go abResult (per-metric, never blended)
func Comparable(a, b Fingerprint) (bool, []string) {
	var diff []string
	if a.Model != b.Model { diff = append(diff, "model") }
	if a.Quant != b.Quant { diff = append(diff, "quant") }
	if a.HostGfxID == "" || b.HostGfxID == "" || a.HostGfxID != b.HostGfxID {
		diff = append(diff, "host") // unknown host = not comparable (no false-green)
	}
	return len(diff) == 0, diff // backend deliberately NOT a blocker (that's the comparison)
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `villa bench` output is ephemeral (printed, not saved) | Each run persisted as a versioned JSONL saved report | This phase (14) | Enables longitudinal/cross-model comparison; introduces a new on-disk contract. |
| Comparison only via live `--ab` (flips the backend) | Read-only `--compare` over saved reports + comparability guard | This phase (14) | Compare across TIME and across models without flipping a live backend; honest refusal on non-comparable pairs. |

**Deprecated/outdated:** none — purely additive.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Single append-only `bench-reports.jsonl` (vs per-run files) | On-Disk Format | Low — ROADMAP says "flat JSONL"; per-file would be a different layout but same record. |
| A2 | `ctx` included in the comparability fingerprint | Env/Host Fingerprint | Medium — REQUIREMENTS says "model/quant/host"; adding ctx tightens comparability (defensible). Confirm in discuss-phase. |
| A3 | `backend` is RECORDED but NOT a comparability-blocking field (cross-backend compare is the point) | Env/Host Fingerprint, `--compare` Semantics | High — if backend must match to be comparable, the Phase-12-pairing "prove Δtg recovery" use case breaks. STRONGLY recommend allowing backend to differ; confirm in discuss-phase. |
| A4 | Host fingerprint key = `igpu_gfx_id` (primary), kernel/envelope secondary/optional | Env/Host Fingerprint | Medium — "host" granularity unspecified. gfx_id is the obvious identity; confirm the full set. |
| A5 | Persist ALWAYS (incl. void-exhausted runs, flagged) | Write Seam | Low-Medium — BENCH-03 says record residency-void state; persisting void runs is consistent. |
| A6 | `--ab` runs persist as ONE record with an `ab` block | Write Seam | Low — matches `bench --json` mode:"ab". |
| A7 | Write failure is non-fatal (loud stderr warn, keep measurement exit code) | Write Seam | Low — consistent with bench's failed-restore idiom. |
| A8 | CLI surface = flags on `bench` (`--compare`/`--list`) not subcommands | `--compare` Semantics | Low — ROADMAP literally says `villa bench --compare`. |
| A9 | Exit mapping: comparable→0, not-comparable→2, no-reports→1 | `--compare` Semantics | Low — mirrors the repo's 0/2/1 contract. |
| A10 | UNKNOWN host fingerprint field ⇒ not comparable (sentinel, no false-equal) | Pitfalls / Fingerprint | Low — preserves no-false-green posture. |

## Open Questions (RESOLVED)

1. **Exact host-attribute set for the fingerprint + whether `backend` blocks comparability.** — RESOLVED: see A2/A3/A4/A10 — comparable iff model+quant+ctx+host(gfx id) match; backend deliberately allowed to differ; UNKNOWN host fact ⇒ not comparable
   - What we know: REQUIREMENTS says "model/quant/host"; the Phase-12 pairing goal implies cross-backend compare must be allowed.
   - What's unclear: the precise host fields and the backend-blocking decision.
   - Recommendation: model + quant + `igpu_gfx_id` block; backend differs freely; surface in discuss-phase (A2/A3/A4).

2. **`--compare` selection UX (auto-pick vs explicit indices/ids).** — RESOLVED: A8 — bare `--compare` auto-selects the two most-recent comparable reports
   - What we know: SC#2 requires list+view; SC#3 requires `--compare` deltas.
   - What's unclear: whether bare `--compare` auto-selects a pair or requires selectors.
   - Recommendation: support both (bare = sensible auto pair; optional selectors). Confirm in discuss-phase (A8 detail).

3. **Treatment of a void-exhausted record inside `--compare`.** — RESOLVED: A5 — persist always; in `--compare`, still show the delta but flag the void side as not-authoritative
   - Recommendation: still show the delta but flag the void side as not-authoritative; decide in discuss-phase.

## Environment Availability

> No new external tools/services. `--compare` is fully off-hardware (read-only file I/O). The write hook runs after a real `villa bench`, which already requires the running stack — unchanged by this phase.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go stdlib (`encoding/json`, `os`, `bufio`, `path/filepath`) | benchstore | ✓ | Go 1.26.2 | — |
| `$XDG_DATA_HOME` (env, optional) | store path | ✓ (falls back to `~/.local/share`) | — | `~/.local/share/villa` then `/var/tmp/villa` |
| Running inference stack | only the WRITE path (a real `villa bench`); NOT `--compare` | n/a off-hardware | — | `--compare`/`--list` need only saved files |

**Missing dependencies with no fallback:** none
**Missing dependencies with fallback:** XDG_DATA_HOME unset → `~/.local/share/villa` (standard on this Fedora target).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table-driven + byte-for-byte goldens; shared package-level `*update` flag declared in `detect_test.go`) — no third-party assert/mock |
| Config file | none (standard `go test`) |
| Quick run command | `go test ./internal/benchstore/... ./cmd/villa/... -run Bench -count=1` |
| Full suite command | `make check` (`go vet ./...` + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| BENCH-03 | `savedReportSchemaVersion == 1` and record golden is byte-stable | unit/golden | `go test ./internal/benchstore -run SchemaVersion -count=1` ; `go test ./cmd/villa -run RecordGolden -count=1` | ❌ Wave 0 |
| BENCH-03 | pp/tg persisted as SEPARATE keys; NO blended tok/s key in the record | unit/golden grep | `go test ./cmd/villa -run NoBlended -count=1` (clone existing bench_test no-blended assertion) | ❌ Wave 0 |
| BENCH-03 | `void_exhausted`/`reason` round-trip (write→read) preserves residency-void state | unit | `go test ./internal/benchstore -run VoidRoundTrip -count=1` | ❌ Wave 0 |
| BENCH-03 | Append writes one JSONL line per run; store grows append-only; 0600/0700 + traversal guard | unit (buffer + tmp XDG) | `go test ./cmd/villa -run BenchstoreWrite -count=1` (`t.Setenv("XDG_DATA_HOME", t.TempDir())`) | ❌ Wave 0 |
| BENCH-03 | Write hook FIRES after a successful `villa bench` run (and is non-fatal on write error) | unit (stub Deps) | `go test ./cmd/villa -run BenchWriteHook -count=1` | ❌ Wave 0 |
| BENCH-04 | Comparable matrix: same model+quant+host ⇒ comparable; each mismatch ⇒ not-comparable with the differing field named | table unit | `go test ./internal/benchstore -run Comparable -count=1` | ❌ Wave 0 |
| BENCH-04 | UNKNOWN host fingerprint ⇒ NOT comparable (no false-equal) | unit | `go test ./internal/benchstore -run UnknownHost -count=1` | ❌ Wave 0 |
| BENCH-04 | `Compare` deltas are per-metric (Δpp, Δtg) and never blended | unit | `go test ./internal/benchstore -run CompareDelta -count=1` | ❌ Wave 0 |
| BENCH-04 | `--compare` on a non-comparable pair prints "not comparable" + NO delta; exit 2 | cmd (stub ReadAll) | `go test ./cmd/villa -run CompareNotComparable -count=1` | ❌ Wave 0 |
| BENCH-04 | `--compare --json` byte-stable golden (comparable + not-comparable) | golden | `go test ./cmd/villa -run CompareGolden -count=1` | ❌ Wave 0 |
| BENCH-04 | `--compare`/`--list` reject combination with `--ab`/`-n` (read-only) at the cobra boundary | cmd | `go test ./cmd/villa -run CompareFlagExclusive -count=1` | ❌ Wave 0 |
| BENCH-04 | No saved reports ⇒ `--compare` refuses with remediation, exit 1 | cmd | `go test ./cmd/villa -run CompareNoReports -count=1` | ❌ Wave 0 |
| (invariant) | `TestSeamGrepGate` stays green — `internal/benchstore` leaks no backend marker | existing | `go test ./internal/inference -run SeamGrepGate -count=1` | ✅ exists |

### Sampling Rate
- **Per task commit:** `go test ./internal/benchstore/... ./cmd/villa/... -count=1`
- **Per wave merge:** `make check`
- **Phase gate:** `make check` green (incl. `TestSeamGrepGate`) before `/gsd-verify-work`; the record golden frozen in the first wave.

### Wave 0 Gaps
- [ ] `internal/benchstore/benchstore_test.go` — schema-version stability, comparable matrix, unknown-host, void round-trip, per-metric delta (BENCH-03/04)
- [ ] `internal/benchstore/testdata/record.golden` — frozen schema-1 JSONL record (the on-disk contract; FIRST wave)
- [ ] `cmd/villa/testdata/bench-compare.json.golden` — frozen `--compare --json` (comparable + not-comparable)
- [ ] `cmd/villa/bench_test.go` additions — write-hook fires, `--compare`/`--list` render + flag-exclusivity + no-reports + not-comparable (BENCH-03/04)
- [ ] Framework install: none — stdlib `testing` already in use.

## Security Domain

> `security_enforcement` is absent in `.planning/config.json` → treated as ENABLED.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Local single-user CLI; no auth surface. |
| V3 Session Management | no | No sessions. |
| V4 Access Control | yes (filesystem) | Store written 0600 under a 0700 dir; traversal-guarded (`config.assertInsideDir` shape). |
| V5 Input Validation | yes | `--compare` selectors are bounded ints/ids validated at the cobra boundary (mirror existing `--reps`/`--ab-target` validation); a malformed/corrupt JSONL line is skipped or fails closed, never panics. |
| V6 Cryptography | no | No secrets, no crypto; saved reports carry only numeric timings + reproducible spec + fingerprint. |
| V8 Data Protection / Privacy | yes | NO prompt/response content stored (counts-only discipline, matching `bench --json`); strictly local, no outbound — `--compare` is pure local file read. Reinforces zero-telemetry. |

### Known Threat Patterns for Go CLI + local JSONL store

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal on the store path | Tampering | `assertInsideDir` (clean+abs+`filepath.Rel` `..` check), path joined under XDG only — reuse `internal/config` guard. |
| Info disclosure via loose file perms | Information Disclosure | 0600 file / 0700 dir + `os.Chmod` even if pre-existing (mirror `config.SaveVilla`). |
| Corrupt/oversized JSONL line crashing `--compare` | Denial of Service | `bufio.Scanner` with a bounded buffer; skip-and-warn on an unparseable line (fail closed per-line, never panic); a trailing partial line never poisons earlier records. |
| Leaking generated text / prompts into the store | Information Disclosure / Privacy | Persist ONLY numeric timings + the fixed reproducible spec + fingerprint — no captured completion text (same discipline as `bench --json`). |
| Backend-marker literal leaking into `internal/benchstore` | (build invariant) | `benchstore` imports neither `inference` nor `detect`; fingerprint host fields arrive as plain strings; `TestSeamGrepGate` enforces. |

## Project Constraints (from CLAUDE.md)

- **Pure-core + injectable-seam:** `internal/benchstore` must be a pure core (no host I/O in logic); the file append/read are injected `Deps func` fields wired by a cmd-tier `live*Deps()` closure. `internal/orchestrate` stays the ONLY intentionally impure module — benchstore's I/O is confined to injected seams, not embedded in pure logic.
- **Byte-frozen contracts:** the on-disk JSONL record AND any `--compare`/`--list` `--json` output are byte-frozen via golden + `schema_version`, evolved append-only (new field above `schema_version`, bump on incompatible change). Freeze the record golden FIRST.
- **Seam grep-gate:** backend marker strings must stay behind `internal/inference` + `internal/orchestrate`. `benchstore` must not import `inference`/`detect` or carry any backend literal; `TestSeamGrepGate` walks `internal/` + `cmd/villa`.
- **Config is the single source of truth:** fingerprint model/quant/ctx come from `config.VillaConfig`; never re-derive.
- **No shell interpolation / fixed-arg only:** N/A (no exec in this phase) but the discipline holds — store paths are catalog/XDG-resolved, never interpolated from user input.
- **Single static CGO-free binary:** no cgo dependency (no SQLite) — stdlib JSONL only.
- **Offload-asserting / no false-green:** carried into comparability — an UNKNOWN host fact yields "not comparable," never a false-equal/misleading delta.
- **Thin cobra callers + pure cores:** printing/exit-mapping live in `cmd/villa/bench.go`; `benchstore` returns typed values (`SavedReport`, `CompareResult`, `NotComparable`), never prints or exits.

## Sources

### Primary (HIGH confidence)
- `internal/bench/bench.go` — BenchSpec, Stats, ABResult, Result (VoidExhausted/Reason), Deps-of-func-fields, pp/tg separation, abResult per-metric delta. (read in full)
- `cmd/villa/bench.go` — existing `villa bench` cobra surface, `liveBenchDeps`, `benchSide`/`benchAB` JSON tags, flag-combo validation, 0/2/1 exit mapping, `benchEndpointReachable`/`benchConfiguredBackend` seams. (read in full)
- `internal/config/villaconfig.go` — XDG path resolution (`os.UserConfigDir`), 0600/0700, `assertInsideDir` traversal guard, load-returns-defaults-when-absent. (read in full)
- `cmd/villa/model.go` — `modelsDir()` `$XDG_DATA_HOME/villa/...` resolution + fallbacks; `model` noun subcommand registration. (read)
- `internal/detect/profile.go` + `detect.go` — `HostProfile` fields (IGPUGfxID/KernelVersion/etc.), `hostProfileSchemaVersion` (=2) as last-field append-only contract, typed-Optional `Str`/`Bytes` (`Known bool`). (read)
- `cmd/villa/bench_test.go` + `cmd/villa/testdata/bench.json.golden` — golden + shared `*update` freeze pattern; "no blended tok/s key" assertion to clone. (read)
- `internal/inference/seam_test.go` — `TestSeamGrepGate` walks `internal/` + `cmd/villa`. (read, scope confirmed)
- `.planning/ROADMAP.md` Phase 14 + implementation note; `.planning/REQUIREMENTS.md` BENCH-03/04. (read)

### Secondary (MEDIUM confidence)
- `cmd/villa/model_test.go` — `t.Setenv("XDG_DATA_HOME", t.TempDir())` test idiom. (grep)

### Tertiary (LOW confidence)
- none — this phase required no web/external research; all findings are codebase-verified.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — stdlib-only, all patterns verified in-tree by reading the source.
- Architecture: HIGH — direct composition of `bench`/`config`/`detect`/`model.go` patterns already in the repo.
- On-disk format / fingerprint: MEDIUM-HIGH — record shape is HIGH (mirrors `bench --json`); the exact host-attribute set + backend-blocking decision are the genuine open design choices (A2/A3/A4) for discuss-phase.
- Pitfalls: HIGH — drawn from explicit in-tree invariants (seam gate, golden freeze, no-false-green, 0600/0700).

**Research date:** 2026-06-07
**Valid until:** 2026-07-07 (stable — internal codebase patterns, no fast-moving external deps).

## RESEARCH COMPLETE
