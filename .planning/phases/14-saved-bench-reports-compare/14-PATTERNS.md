# Phase 14: Saved Bench Reports + `--compare` - Pattern Map

**Mapped:** 2026-06-07
**Files analyzed:** 6 (3 new, 3 modified/extended)
**Analogs found:** 6 / 6 (every new file has an in-tree exact-or-role analog; this phase is composition, not invention)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/benchstore/benchstore.go` (NEW) | service (pure core + injected I/O seam) | file-I/O (append-only JSONL) + transform (compare) | `internal/bench/bench.go` (pure core + `Deps`); `internal/config/villaconfig.go` (path safety/marshal) | role-match (compose two analogs) |
| `internal/benchstore/benchstore_test.go` (NEW) | test | transform/round-trip | `internal/detect/profile_test.go` (schema-version assert + round-trip) | exact (pattern) |
| `cmd/villa/testdata/benchstore-record.golden` (NEW) | config (frozen contract fixture) | file-I/O | `cmd/villa/testdata/bench.json.golden` | exact (pattern) |
| `cmd/villa/testdata/bench-compare.json.golden` (NEW) | config (frozen contract fixture) | file-I/O | `cmd/villa/testdata/bench.json.golden` | exact (pattern) |
| `cmd/villa/bench.go` (MODIFIED) | controller (cobra surface + `live*Deps` wiring + render/exit) | request-response + file-I/O write-hook | `cmd/villa/bench.go` itself (`liveBenchDeps`, `runBench`, `benchEntry`); `cmd/villa/model.go:modelsDir` (XDG resolver) | exact (extend in place) |
| `cmd/villa/bench_test.go` (MODIFIED) | test | golden + stub-Deps | `cmd/villa/bench_test.go` itself (golden freeze + no-blended assert) | exact (extend in place) |

**Key constraint reminder (CLAUDE.md / RESEARCH Pitfall 2):** `internal/benchstore` MUST NOT import `internal/inference` or `internal/detect`. Fingerprint host fields arrive as plain strings captured at the cmd tier and passed in — exactly how `internal/bench` imports neither and takes backend markers only through the injected `Measure` verdict. `TestSeamGrepGate` (`internal/inference/seam_test.go`) walks all of `internal/` + `cmd/villa` and fails on a leaked backend/marker literal.

## Pattern Assignments

### `internal/benchstore/benchstore.go` (pure core + injected file-I/O seam)

This file composes THREE in-tree analogs: the schema-version contract (`detect.HostProfile`), the pp/tg-separate result + `Deps`-of-func-fields (`internal/bench`), and the path-safety/marshal discipline (`internal/config`).

**Analog A — schema_version as last field, append-only** (`internal/detect/profile.go:3-11, 58-60`):
```go
// hostProfileSchemaVersion is the self-version of the HostProfile contract.
// Bump it whenever a field's meaning changes incompatibly ...
const hostProfileSchemaVersion = 2
// ...
	// SchemaVersion is the HostProfile contract self-version. It MUST stay the
	// LAST field of HostProfile (append-only discipline; new fields go above it).
	SchemaVersion int `json:"schema_version"`
```
**Replicate:** `const savedReportSchemaVersion = 1`; put `SchemaVersion int json:"schema_version"` as the LAST field of `SavedReport`; new fields append above it; bump only on incompatible change. Freeze BEFORE the first writer runs (ROADMAP note).

**Analog B — `Deps` of func fields, pure core does marshal/parse** (`internal/bench/bench.go:81-105`):
```go
type Deps struct {
	Measure func(ctx context.Context) (t RunTimings, resident bool, detail string, err error)
	Switch  func(ctx context.Context, target string) error
	Restore func(ctx context.Context, original string) error
	LoadConfig func() (config.VillaConfig, error)
	OnSideStart func(side string, spec BenchSpec)
}
```
**Replicate** (RESEARCH Pattern 2): a `benchstore.Deps` whose `AppendLine func(line []byte) error`, `ReadAll func() ([]byte, error)`, and `Now func() time.Time` are injected; the pure core marshals/parses, the seam does the byte I/O. Note the file is print-free and exit-free — return typed values (`SavedReport`, `CompareResult`, `NotComparable`), never print/`os.Exit`.

**Analog C — pp/tg kept structurally separate, per-metric delta** (`internal/bench/bench.go:110-135, 302-311`):
```go
type Stats struct {
	MedianPP float64; StddevPP float64
	MedianTG float64; StddevTG float64
	Kept int; Void int
}
// ...
func abResult(orig, target string, a, b Stats) ABResult {
	return ABResult{
		From: orig, To: target, A: a, B: b,
		DeltaPP: b.MedianPP - a.MedianPP, // pp delta
		DeltaTG: b.MedianTG - a.MedianTG, // tg delta — SEPARATE, never blended
	}
}
```
**Replicate:** persist `prompt_per_sec`/`predicted_per_sec` (+ stddevs) as separate fields; `Compare` returns `DeltaPP`/`DeltaTG` as two figures. NEVER a `tok_per_sec`/`throughput` field (the no-blended golden grep, below, enforces it). Persist the already-computed `bench.Stats` values — benchstore stores, never recomputes (RESEARCH Don't Hand-Roll).

**Comparability guard — refuse, never fabricate** (RESEARCH Pattern 4 / Code Examples; mirrors the no-false-green posture):
```go
func Comparable(a, b Fingerprint) (bool, []string) {
	var diff []string
	if a.Model != b.Model { diff = append(diff, "model") }
	if a.Quant != b.Quant { diff = append(diff, "quant") }
	if a.HostGfxID == "" || b.HostGfxID == "" || a.HostGfxID != b.HostGfxID {
		diff = append(diff, "host") // UNKNOWN host = not comparable (no false-equal)
	}
	return len(diff) == 0, diff // backend deliberately NOT a blocker (that IS the comparison)
}
```
On mismatch, `Compare` returns a typed `NotComparable{DifferingFields}` — the cmd tier prints the differing fields and NO delta. (Backend-blocking decision is A3 — open for discuss-phase, but research strongly recommends backend differs freely.)

---

### `internal/benchstore` path-safety helper / OR cmd-tier `liveBenchstoreDeps`

The XDG path resolution + 0600/0700 + traversal guard is the `config` + `model.go` composition. The actual file touch lives in an injected seam (wired at the cmd tier), keeping `benchstore`'s logic pure.

**Analog — XDG_DATA_HOME resolver with fallbacks** (`cmd/villa/model.go:112-124`):
```go
func modelsDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa", "models")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa", "models")
	}
	return filepath.Join("/var/tmp", "villa", "models")
}
```
**Replicate:** a `benchReportsPath()` resolving `$XDG_DATA_HOME/villa/bench-reports.jsonl` with the identical `~/.local/share` → `/var/tmp` fallback chain (single JSONL store, not a per-run dir).

**Analog — traversal guard + 0600 file / 0700 dir + chmod-even-if-preexisting** (`internal/config/villaconfig.go:24-27, 150-178, 221-240`):
```go
const configFileMode os.FileMode = 0o600
const configDirMode  os.FileMode = 0o700
// ...
	if err := assertInsideDir(path, dir); err != nil { return err }
	if err := os.MkdirAll(dir, configDirMode); err != nil { ... }
	if err := os.WriteFile(path, data, configFileMode); err != nil { ... }
	if err := os.Chmod(path, configFileMode); err != nil { ... } // tighten even if pre-existing
// ...
func assertInsideDir(path, dir string) error {
	absDir, _ := filepath.Abs(filepath.Clean(dir))
	absPath, _ := filepath.Abs(filepath.Clean(path))
	rel, _ := filepath.Rel(absDir, absPath)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("refusing to write %q outside %q", absPath, absDir)
	}
	return nil
}
```
**Replicate** in the `liveBenchstoreDeps().AppendLine` closure — but APPEND, not write-whole-file: `os.OpenFile(store, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)` under a `MkdirAll(dir, 0o700)`, traversal-guarded with the same `assertInsideDir` shape. `ReadAll` is `os.ReadFile` returning `(nil, nil)` on `os.IsNotExist` (no reports yet ≠ error — mirrors `config.LoadVilla` returning defaults when absent, `villaconfig.go:130-136`). Note: `assertInsideDir` is currently unexported in `internal/config`; benchstore should carry its own copy of the guard shape (do not export config's, do not import config for it).

---

### `cmd/villa/bench.go` (MODIFIED — write hook + `--compare`/`--list` surface)

Extend the existing file in place. Three additions, each with an in-file analog.

**(1) Write hook in `runBench` after success** — analog is the existing `runBench` tail (`cmd/villa/bench.go:435-484`). The write fires AFTER the measurement is printed, is NON-FATAL on error (loud stderr warn, keep the measurement's exit code), mirroring the failed-restore-is-loud-but-non-fatal idiom already in `liveBenchDeps.Restore` (`bench.go:230-243`):
```go
// existing loud-but-non-fatal precedent to clone for a write failure:
if err := benchBackendSwap(original); err != nil {
	fmt.Fprintf(os.Stderr, "bench: WARNING — failed to restore original backend %q: %v\n...", original, err)
	return err
}
```
The cmd tier already holds everything to build the record: `res bench.Result` (pp/tg, `VoidExhausted`/`Reason`), `spec`, `benchConfiguredBackend()`, and `config.LoadVilla()` (model/quant/ctx). It additionally captures the host fingerprint from `detect` and passes plain strings into `benchstore` (NEVER let `benchstore` import `detect`).

**(2) Fingerprint capture (Optional `.Known` guard)** — `detect.HostProfile` fields are typed-Optional (`internal/detect/value.go:31-39`: `Str{Value string; Known bool; Source string}`). At capture, read `.Value` only when `.Known`; an UNKNOWN host fact serializes to a sentinel that makes the pair NOT comparable (RESEARCH Pitfall 4 — no false-equal):
```go
// cmd tier (bench.go), NOT benchstore:
hp := detect.Probe()
gfx := ""
if hp.IGPUGfxID.Known { gfx = hp.IGPUGfxID.Value }
fp := benchstore.Fingerprint{Model: cfg.Model, Quant: cfg.Quant, HostGfxID: gfx /* ctx, kernel per A2/A4 */}
```

**(3) `--compare`/`--list` read-only flags + exit mapping** — analog is the existing flag-combo validation and `--json`/render/exit mapping in `newBench` + `runBench`:
- Flag mutual-exclusivity precedent (`cmd/villa/bench.go:316-327`): `--ab-target requires --ab`. Replicate: `--compare`/`--list` reject combination with the live-measurement flags (`--ab`, `--ab-target`, `-n`, `--warmup`) at the cobra boundary.
- `--json` encode precedent (`bench.go:466-475`): `json.NewEncoder(out); enc.SetIndent("", "  ")`. Replicate for the `--compare`/`--list` machine contract (own golden).
- Exit mapping (`cmd/villa/preflight.go:20-22`): `exitPass=0`, `exitWarn=2`, `exitBlocked=1`. Replicate (RESEARCH A9): comparable delta → `exitPass`; not-comparable → `exitWarn`; no/insufficient saved reports → `exitBlocked` with remediation ("run `villa bench` first").
- Render `--compare` deltas on SEPARATE lines (clone `renderBench`'s `Δpp`/`Δtg` two-line block, `bench.go:586-588`):
```go
fmt.Fprintf(w, "  Δpp tok/s: %+8.2f\n", e.AB.DeltaPromptPerSec)
fmt.Fprintf(w, "  Δtg tok/s: %+8.2f\n", e.AB.DeltaPredictedPerSec)
```
- Reuse the testable indirection idiom: `benchRun`/`benchEndpointReachable`/`benchConfiguredBackend` are package-level `var`s so tests drive them without a live host. Add a `liveBenchstoreDeps()` constructor (mirroring `liveBenchDeps`, `bench.go:206-245`) and keep the write/read wiring behind it.

---

### `cmd/villa/testdata/benchstore-record.golden` + `bench-compare.json.golden` (NEW frozen contracts)

**Analog — golden freeze with the shared `*update` flag** (`cmd/villa/bench_test.go:463-480`; `*update` declared once in `cmd/villa/detect_test.go:13`):
```go
golden := filepath.Join("testdata", "bench.json.golden")
if *update {
	if err := os.MkdirAll("testdata", 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil { t.Fatal(err) }
	t.Logf("updated %s", golden); return
}
want, err := os.ReadFile(golden) // run with -update to create
if !bytes.Equal(out.Bytes(), want) { t.Errorf("does not match golden ...") }
```
**Replicate** for BOTH new goldens. `benchstore-record.golden` = ONE schema-1 JSONL record produced via an injected deterministic `Deps.Now` — this is the on-disk CONTRACT, frozen in the FIRST plan/wave BEFORE any live writer (ROADMAP note / RESEARCH Pitfall 1). `bench-compare.json.golden` = the `--compare --json` output covering both a comparable delta and a not-comparable refusal.

---

### `internal/benchstore_test.go` + `cmd/villa/bench_test.go` additions

**Analog — schema-version const assertion** (`internal/detect/profile_test.go:15-17`):
```go
if p.SchemaVersion != hostProfileSchemaVersion {
	t.Errorf("SchemaVersion = %d, want %d", p.SchemaVersion, hostProfileSchemaVersion)
}
```
**Replicate:** assert `savedReportSchemaVersion == 1` and that a round-tripped record preserves it (BENCH-03).

**Analog — no-blended-key golden grep** (`cmd/villa/bench_test.go:488-498`):
```go
for _, blended := range [][]byte{[]byte("tok_per_sec"), []byte("tokens_per_sec")} {
	if bytes.Contains(data, blended) {
		t.Errorf("golden contains a blended tok/s key %q — pp and tg MUST stay SEPARATE ...", blended)
	}
}
```
**Replicate:** run the same grep over `benchstore-record.golden` and `bench-compare.json.golden`.

**Analog — temp XDG in tests** (`cmd/villa/model_test.go:58, 84`): `t.Setenv("XDG_DATA_HOME", t.TempDir())` — use for the live `AppendLine`/`ReadAll` write-path test (0600/0700 + traversal, append-grows). Pure-core tests instead back `Deps` with a `bytes.Buffer` (no XDG touched), exactly as `bench_test.go` stubs the `bench.Deps` func fields.

## Shared Patterns

### Path safety (0600 file / 0700 dir + traversal guard)
**Source:** `internal/config/villaconfig.go:24-27, 157-176, 223-240`
**Apply to:** the `liveBenchstoreDeps().AppendLine` closure (the ONLY writer). Use `O_APPEND|O_CREATE|O_WRONLY 0600` under `MkdirAll(dir, 0700)`, traversal-guarded; chmod-even-if-preexisting. Carry a local copy of the `assertInsideDir` shape in benchstore (config's is unexported; do not import config solely for it).

### Injected Deps seam (pure core, testable off-hardware)
**Source:** `internal/bench/bench.go:81-105` (Deps of func fields); `cmd/villa/bench.go:206-245` (`liveBenchDeps` constructor)
**Apply to:** `benchstore.Deps` (`AppendLine`/`ReadAll`/`Now`) + a `liveBenchstoreDeps()` constructor in `cmd/villa/bench.go`. Tests pass a buffer-backed `Deps`.

### Byte-frozen golden + schema_version (append-only)
**Source:** `cmd/villa/bench_test.go:463-480` (golden + `*update`); `internal/detect/profile.go:11, 58-60` (last-field schema const)
**Apply to:** `benchstore-record.golden` (on-disk record), `bench-compare.json.golden` (`--compare --json`), and the `savedReportSchemaVersion` const. Freeze the RECORD golden in the first wave.

### pp/tg structural separation (never blended)
**Source:** `internal/bench/bench.go:110-135` (Stats/ABResult); `cmd/villa/bench.go:397-416` (benchSide/benchAB JSON tags); `cmd/villa/bench_test.go:488-498` (no-blended grep)
**Apply to:** the record fields, the `Compare` delta, and a cloned no-blended grep test over the new goldens.

### Honest refusal / no false-green
**Source:** repo-wide offload-asserting posture; `internal/bench` void-exhaustion WARN
**Apply to:** the comparability guard — UNKNOWN host fingerprint ⇒ NOT comparable; non-comparable pair ⇒ typed `NotComparable` + NO delta, exit `exitWarn` (2). A misleading green delta is worse than an honest refusal.

### Exit-code mapping (0/2/1)
**Source:** `cmd/villa/preflight.go:20-22`
**Apply to:** `--compare`/`--list`: comparable → 0, not-comparable → 2, no-reports → 1 (with remediation).

## No Analog Found

None. Every new file maps to an in-tree exact-or-role analog; this phase is composition of shipped patterns, not invention. The only genuinely new surface — `encoding/json` JSONL line read via `bufio.Scanner` — is a Go-stdlib standard idiom (not yet present in-tree) with no risk; bound the scanner buffer and skip-and-warn on a corrupt line (RESEARCH Security Domain, fail-closed per-line, never panic).

## Metadata

**Analog search scope:** `internal/config/`, `internal/bench/`, `internal/detect/`, `cmd/villa/` (bench.go, model.go, preflight.go, *_test.go), `cmd/villa/testdata/`
**Files scanned:** 8 source files + 1 golden read for structure
**Pattern extraction date:** 2026-06-07

## PATTERN MAPPING COMPLETE

**Phase:** 14 - Saved Bench Reports + `--compare`
**Files classified:** 6
**Analogs found:** 6 / 6

### Coverage
- Files with exact analog (pattern reused verbatim): 5
- Files with role-match analog (compose two analogs): 1 (`benchstore.go`)
- Files with no analog: 0

### Key Patterns Identified
- Pure `internal/benchstore` core composes THREE in-tree analogs: schema_version-as-last-field (`detect.HostProfile`), `Deps`-of-func-fields + pp/tg separation (`internal/bench`), and 0600/0700 + traversal-guard + XDG resolver (`internal/config` + `model.go:modelsDir`).
- On-disk JSONL record is a byte-frozen contract: `savedReportSchemaVersion=1`, golden frozen in the FIRST wave BEFORE any live writer (ROADMAP note), evolved append-only.
- `benchstore` imports NEITHER `inference` NOR `detect` — fingerprint host facts arrive as plain strings captured at the cmd tier (`detect.Probe()` `.Known`-guarded); `TestSeamGrepGate` enforces.
- pp/tg stay two fields end-to-end (record + delta); a cloned no-blended-key golden grep guards it; comparability refuses (typed `NotComparable`, no delta) on mismatch/unknown-host (no false-green).
- The write hook lives in `cmd/villa/bench.go:runBench` (after success, loud-but-non-fatal on write error); `--compare`/`--list` are read-only with 0/2/1 exit mapping and flag-exclusivity at the cobra boundary.

### File Created
`.planning/phases/14-saved-bench-reports-compare/14-PATTERNS.md`

### Ready for Planning
Pattern mapping complete. The planner can reference these analog files + line ranges directly in each plan's action section.
