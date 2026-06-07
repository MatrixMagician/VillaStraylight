---
phase: 14-saved-bench-reports-compare
reviewed: 2026-06-07T00:00:00Z
depth: deep
files_reviewed: 5
files_reviewed_list:
  - cmd/villa/bench.go
  - cmd/villa/bench_compare.go
  - cmd/villa/bench_test.go
  - internal/benchstore/benchstore.go
  - internal/benchstore/benchstore_test.go
findings:
  critical: 0
  warning: 5
  info: 4
  total: 9
status: issues_found
---

# Phase 14: Code Review Report

**Reviewed:** 2026-06-07
**Depth:** deep
**Files Reviewed:** 5
**Status:** issues_found

## Summary

Phase 14 adds the saved-bench-report persistence layer (`internal/benchstore`) plus the
read-only `villa bench --compare` / `--list` surface. The architecture is clean: the pure
`benchstore` core is genuinely detect/inference-free, the on-disk schema is golden-frozen,
pp/tg stay structurally separate end-to-end, the comparability guard correctly refuses on
an UNKNOWN host (no false-equal), `Load` fails closed per line, and the JSON/human exit
mapping (0/2/1) is consistent. Tests are thorough.

No BLOCKER-class defects were proven. The findings below are correctness and robustness
gaps. The two most material are: (1) the on-disk read path performs **no `schema_version`
validation**, so a future/hand-edited v2 record is silently parsed and compared as v1
(fail-open against the very contract the golden exists to protect); and (2) the live
path-traversal guard is **structurally inert** — it checks a constant store path against
its own parent directory and can never reject anything, contradicting its stated purpose.

## Warnings

### WR-01: `Load` performs no `schema_version` validation — future/hand-edited records silently parsed as v1

**File:** `internal/benchstore/benchstore.go:251-280`
**Issue:** `Load` unmarshals every JSONL line into a `SavedReport` and appends it with no
check that `r.SchemaVersion == savedReportSchemaVersion`. The whole point of the
golden-frozen `schema_version` (=1) contract is to gate migrations, but on read it is
inert. A record written by a future `villa` (schema_version=2 with reshaped fields) — or a
hand-edited line that is JSON-valid but semantically wrong — is loaded as if it were v1 and
fed straight into `Compare`, which can then emit a misleading pp/tg delta from
mis-mapped fields. The project context explicitly calls out "JSONL parsing must fail closed
on malformed/hand-edited lines (never emit a misleading delta)"; an unknown schema version
is exactly that case and is currently fail-open.
**Fix:** Skip (fail-closed) any line whose `SchemaVersion` is not a recognized/supported
value, mirroring the per-line skip already used for unparseable lines:
```go
var r SavedReport
if err := json.Unmarshal(line, &r); err != nil {
    continue
}
if r.SchemaVersion != savedReportSchemaVersion {
    continue // unknown/future schema — skip, do not silently treat as v1
}
out = append(out, r)
```
(If forward-compat reads are desired later, gate on `r.SchemaVersion <= savedReportSchemaVersion`
and add an explicit migration — but never parse an unknown version as the current one.)

### WR-02: Live path-traversal guard is structurally inert (cannot ever reject)

**File:** `cmd/villa/bench.go:303-323` (and the unused `internal/benchstore/assertInsideDir`, benchstore.go:298-318)
**Issue:** `liveBenchstoreDeps` computes `store := benchReportsStorePath()` and
`dir := filepath.Dir(store)`, then `AppendLine` calls `benchAssertInsideDir(store, dir)`.
Because `store` is by construction a direct child of `dir` (e.g. `$XDG/villa/bench-reports.jsonl`
under `$XDG/villa`) and neither value derives from any external/untrusted input, `filepath.Rel(dir, store)`
is always `bench-reports.jsonl` — the guard can never produce a `..` and therefore can never
reject. It is defense-theater, not a working traversal guard. Separately, the pure core's
`benchstore.assertInsideDir` (the "real" guard the comment references) is **never wired into
the live append path** at all — only exercised by a unit test. The actual untrusted vector
that matters here is `$XDG_DATA_HOME` itself (an attacker-controlled env var could point the
store anywhere), and that is not validated by either guard.
**Fix:** Either (a) remove the inert local guard and its misleading comment so it does not
imply protection that is not there, or (b) make it meaningful by validating the resolved
`store`/`dir` against a fixed trusted root (e.g. assert `$XDG_DATA_HOME` is absolute and the
final path stays under the resolved data home). At minimum, do not claim T-14-01 traversal
protection from a check that is provably a no-op.

### WR-03: Store-path resolver duplicated across the seam boundary with drift risk

**File:** `cmd/villa/bench.go:263-272` vs `internal/benchstore/benchstore.go:287-296`
**Issue:** `benchReportsStorePath` (cmd) and `benchReportsPath` (benchstore) are byte-for-byte
duplicates of the XDG resolution logic (XDG_DATA_HOME → ~/.local/share → /var/tmp), and
`benchAssertInsideDir` (cmd) duplicates `assertInsideDir` (benchstore). The comments
acknowledge the duplication as deliberate (avoiding an import), but the two copies are the
canonical store-location contract and can silently diverge: if one is edited (e.g. fallback
order changed) the live writer and the documented/tested resolver disagree, and reads/writes
target different files with no test catching it. The benchstore copy is also partly dead
(`benchReportsPath` and `assertInsideDir` are unexported and unused by any caller in the
shipped path — see WR-02).
**Fix:** Export the resolver from `benchstore` (e.g. `benchstore.ReportsPath()`) and have the
cmd tier call it, OR add a test asserting the two resolvers return identical paths for the
same env. Importing `benchstore` for a path helper does not pull in `detect`/`inference`, so
the seam-grep constraint is not violated by reuse.

### WR-04: `persistBenchReport` and `liveMeasure` swallow `config.LoadVilla` errors, risking a silently mislabeled saved record

**File:** `cmd/villa/bench.go:784` (`cfg, _ := config.LoadVilla()`)
**Issue:** `persistBenchReport` ignores the error from `config.LoadVilla()`. On a load failure
`cfg` is the zero `VillaConfig`, so `captureBenchFingerprint` records `Model=""`, `Quant=""`,
`Ctx=0`. That report is then persisted as a real, comparable-looking record with an empty
model/quant fingerprint. An empty-model record can later be auto-selected by `selectComparePair`
and compared against another empty-model record (both `Model==""`, `Quant==""`) — they will be
deemed comparable on those axes (only `HostGfxID==""` saves it via the UNKNOWN-host rule). The
honesty posture is "never fabricate identity"; persisting a zeroed fingerprint as if it were a
real measurement subject is a quieter version of the same problem, and it is written with no
warning. Note the same swallow exists for the configured-backend label seam, but there it is
explicitly tolerated; here it pollutes the durable store.
**Fix:** On a config load error, either skip persistence with a loud non-fatal WARN (consistent
with the existing write-failure handling) or stamp an explicit "unknown" marker the
comparability guard treats as not-comparable. Do not silently persist a zeroed fingerprint:
```go
cfg, err := config.LoadVilla()
if err != nil {
    fmt.Fprintf(errOut, "bench: WARNING — skipping saved report: cannot load config for fingerprint: %v\n", err)
    return
}
```

### WR-05: `--compare` auto-selects only the most-recent comparable PAIR, silently ignoring older comparable reports that contradict it

**File:** `cmd/villa/bench_compare.go:104-115` (`selectComparePair`)
**Issue:** `selectComparePair` returns the first comparable pair scanning newest-first and
stops. This is documented as the v1 auto-selection, but it has an honesty edge: if the two
most-recent reports happen to be comparable but were both run under, say, a transient bad
config (or one is void-exhausted), the command renders an authoritative-looking delta from
them and never surfaces that other, equally-comparable historical reports exist with very
different numbers. The user gets a confident delta with no indication it is one arbitrary pair
out of many. Combined with WR-04, an auto-selected pair could even be two zeroed-fingerprint
records. The void side IS flagged (good), but pair-selection provenance is invisible.
**Fix:** This is a UX/honesty gap, not a crash. At minimum, print which two reports (by
`captured_at` / index) were auto-selected so the delta is attributable, and note when more
than two comparable reports exist (e.g. "comparing reports #4 and #7; 3 other comparable
reports present — see `villa bench --list`"). Consider letting the user pin a pair in a later
iteration.

## Info

### IN-01: `liveMeasure` is entirely unreachable from the deep-test path and untested off-hardware

**File:** `cmd/villa/bench.go:76-197`
**Issue:** `liveMeasure` (the real per-run measurement) is wired only into `liveBenchDeps.Measure`
and is never exercised by any test in `bench_test.go` — every test stubs `Measure`. Its goroutine
+ ticker + `select` sampling loop, the `ErrNoTimings` void branch, and the residency-verdict fold
are correctness-critical but unverified here. This is consistent with the seam pattern, but the
function carries real logic (e.g. the busy-sample max-keeping, the deadline void path) that has no
unit coverage in this phase's deliverables.
**Fix:** Consider a focused test that injects a fake `llm` client + `detect` seams (or extract the
sample loop into a pure helper) so the void/deadline branches are asserted off-hardware.

### IN-02: `renderCompareDelta` re-derives `benchCompareSideOf(a)`/`(b)` already computed by the caller path

**File:** `cmd/villa/bench_compare.go:243-245` vs `benchCompareEntryFrom:184-185`
**Issue:** The human and JSON render paths independently call `benchCompareSideOf` on the same
two reports. Harmless today (the function is pure), but it is duplicated work and a divergence
risk if one path later mutates a side. Minor.
**Fix:** Fold the two sides once in `runBenchCompare` and pass them into both renderers.

### IN-03: `NewSavedReport` constructor is effectively unused in the live write path

**File:** `internal/benchstore/benchstore.go:128-131`
**Issue:** `NewSavedReport` is only called from tests; the live path builds `SavedReport`
directly and relies on `Append` to stamp `SchemaVersion`. The constructor's doc comment even
acknowledges callers "may also build the struct directly." Two stamping paths (`NewSavedReport`
and `Append`) is mild redundancy; if a future call site uses the struct literal + `Marshal`
(not `Append`), it would emit `schema_version: 0`.
**Fix:** Either route all persistence through `NewSavedReport` (and have `Marshal` reject an
unstamped report), or drop the constructor and document `Append` as the sole stamping point.

### IN-04: `--list --json` emits raw `SavedReport` records including the full `prompt` and `reason` strings

**File:** `cmd/villa/bench_compare.go:71-74, 142-143`
**Issue:** `benchListEntry` serializes the untouched `[]SavedReport`, which includes `Spec.Prompt`
(the fixed benchmark prompt) and `Reason`. The prompt is a fixed in-repo constant (no user
content, so not a leak per the T-14-02 design), but emitting the full internal record shape via
`--list --json` makes the list contract implicitly equal to the on-disk contract — any future
on-disk field becomes part of the public `--list --json` surface automatically. There is a list
golden? No — unlike `--compare`, `--list --json` has no golden freezing it, so this surface can
drift silently.
**Fix:** Add a golden for `--list --json` (as `--compare` has) so the list contract is frozen,
or project the list into an explicit DTO rather than echoing `SavedReport` verbatim.

---

_Reviewed: 2026-06-07_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
