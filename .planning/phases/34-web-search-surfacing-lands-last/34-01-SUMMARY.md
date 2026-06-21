---
phase: 34-web-search-surfacing-lands-last
plan: 01
subsystem: verify-search-state-store
tags: [surfacing, store, verifystate, SURF-04, honesty-by-construction]
status: complete
requires:
  - "internal/recall/store.go (clone-don't-import discipline source)"
  - "cmd/villa/verify_search_json.go verdictName vocabulary (PASS/FAIL/REJECT)"
  - "cmd/villa/verify_search.go runVerifySearch run path + searchVerifyDeps seam"
provides:
  - "package internal/verifystate"
  - "verifystate.State{schema_version,verdict,checked_at}"
  - "verifystate.Deps{WriteAll,ReadAll,Now} injectable byte-I/O seam"
  - "verifystate.Save(d, s) — version-stamping"
  - "verifystate.Load(d) — fail-closed"
  - "verifystate.VerifyStatePath() — $XDG_DATA_HOME/villa/verify-search-state.json"
  - "verifystate.WriteFileAtomic(path, data) — traversal-guarded 0600 atomic writer"
  - "verifystate.SchemaVersion() — accessor mirroring verifyStateSchemaVersion=1"
  - "searchVerifyDeps.persistFn seam + liveVerifyStatePersist live wiring"
  - "verdict vocabulary persisted: PASS / FAIL / REJECT"
affects:
  - "Plans 03/04/05 read verifystate.Load / verifystate.State to derive the outbound-bounded indicator (with a freshness check in the status core — never a config bool)"
tech_stack:
  added: []
  patterns:
    - "Store discipline: fail-closed Load + version-stamping Save + atomic 0600/0700 temp+rename writer (cloned, not imported)"
    - "Injectable byte-I/O Deps seam (pure core, off-hardware-testable)"
    - "Best-effort side effect that never alters the verb's exit code (RestartWarning posture)"
key_files:
  created:
    - internal/verifystate/store.go
    - internal/verifystate/store_test.go
  modified:
    - cmd/villa/verify_search.go
    - cmd/villa/verify_search_test.go
decisions:
  - "verifystate clones the recall-store discipline verbatim but imports NOTHING from internal/recall (clone-don't-import: local assertInsideDir / WriteFileAtomic copies)."
  - "Save stamps schema_version itself (verifyStateSchemaVersion=1) — a caller-supplied schema is never trusted (T-34-01)."
  - "The persist is a best-effort cmd-tier side effect via a new persistFn seam; it NEVER alters runVerifySearch's exit code (the proof verdict is authoritative) and stays OUT of the pure evalSearchVerify core. A nil seam is a no-op."
  - "Verdict vocabulary persisted is the verdictName() set (PASS/FAIL/REJECT); the freshness PASS->bounded gate lives downstream in the status core (Plan 03), so a stale PASS cannot read as current."
metrics:
  duration: 4min
  completed: "2026-06-21"
  tasks: 2
  files: 4
---

# Phase 34 Plan 01: Persisted verify-search-state store Summary

A new `internal/verifystate` package persists the LAST real `villa verify search` verdict + timestamp to `$XDG_DATA_HOME/villa/verify-search-state.json`, cloning the `internal/recall` store discipline verbatim (fail-closed Load, version-stamping Save, traversal-guarded atomic 0600 writer); `villa verify search` writes it best-effort after the proof runs without ever altering its exit code (SURF-04).

## What Was Built

### Task 1 — `internal/verifystate` store package (commit `a79cf10`)
- `State{SchemaVersion int, Verdict string, CheckedAt string}` (schema v1) — verdict + timestamp ONLY, no query/URL/fetched content (T-34-02).
- `Deps{WriteAll, ReadAll, Now}` injectable byte-I/O seam — pure core, fully off-hardware-testable.
- `Save(d, s)` stamps `verifyStateSchemaVersion=1` itself (never trusts a caller schema), marshals, writes via the seam.
- `Load(d)` fails CLOSED: absent (`nil,nil`) / corrupt / future-schema ⇒ empty `State`, no error, no panic — NEVER a fabricated PASS (T-34-01). A nil `ReadAll` seam or a real read error stay REAL errors.
- `VerifyStatePath()` ⇒ `$XDG_DATA_HOME/villa/verify-search-state.json` with the recall `~/.local/share/villa` → `/var/tmp/villa` fallbacks.
- `WriteFileAtomic(path, data)` — local `assertInsideDir` traversal guard vs the FIXED `storeRootDir()` (WR-05), 0700 dir, 0600 temp + rename, temp cleaned on every error branch (T-34-04).
- `SchemaVersion()` accessor mirrors the unexported const so downstream readers can never desync.
- `TestStore` (6 subtests, TDD RED→GREEN): fail-closed Load, version-stamping round-trip + re-stamp-on-forged-schema, SchemaVersion-mirrors-const, no-content-keys, XDG path + traversal guard, atomic writer (mode/no-temp-residue/traversal-refusal/failed-rename-cleanup).

### Task 2 — wire the persist into `villa verify search` (commit `adf8217`)
- Added `persistFn func(verifystate.State) error` to `searchVerifyDeps`.
- `liveVerifyStatePersist` wraps `verifystate.Save` over a `WriteAll` that calls `verifystate.WriteFileAtomic(verifystate.VerifyStatePath(), …)`; defaulted in `liveVerifySearchDeps`.
- `runVerifySearch` persists `State{Verdict: verdictName(proof.status), CheckedAt: time.Now().UTC().Format(time.RFC3339)}` after `deps.verifyFn(...)`. The error is discarded for the exit code (best-effort warning to stderr only, suppressed in `--json` mode); nil seam = no-op. The pure `evalSearchVerify` core is untouched.
- `TestVerifySearchPersists`: PASS/FAIL/REJECT verdict round-trip via the capture seam + persist-failure-does-not-change-exit + nil-persistFn-is-safe.

## Exported Symbols (for Plans 03/04/05 read-seam wiring)

| Symbol | Signature | Purpose |
|--------|-----------|---------|
| `verifystate.State` | `struct{SchemaVersion int; Verdict string; CheckedAt string}` | the persisted document |
| `verifystate.Deps` | `struct{WriteAll func([]byte) error; ReadAll func() ([]byte, error); Now func() time.Time}` | injectable byte-I/O seam |
| `verifystate.Save` | `func(Deps, State) error` | version-stamping write |
| `verifystate.Load` | `func(Deps) (State, error)` | fail-closed read |
| `verifystate.VerifyStatePath` | `func() string` | `$XDG_DATA_HOME/villa/verify-search-state.json` |
| `verifystate.WriteFileAtomic` | `func(string, []byte) error` | traversal-guarded 0600 atomic writer |
| `verifystate.SchemaVersion` | `func() int` | `1` (mirrors the unexported const) |

**Persisted verdict vocabulary:** `"PASS"`, `"FAIL"`, `"REJECT"` (from `verdictName`, `cmd/villa/verify_search_json.go:31`).

**Downstream honesty contract (Plan 03):** derive the outbound-bounded indicator from `verifystate.Load`, NEVER from `cfg.WebSearchEnabled`. A `Verdict=="PASS"` only reads as "bounded" when ALSO fresh (the freshness window const lives in the status core); a stale / absent / corrupt store ⇒ empty State ⇒ typed-Unknown ("unavailable"), never green.

## Verification

- `go test ./internal/verifystate/ -run TestStore` — 7 passed.
- `go test ./cmd/villa/ -run TestVerifySearchPersists` — pass (PASS/FAIL/REJECT + persist-failure + nil-seam).
- `go test ./cmd/villa/ -run TestVerifySearch` and `-run TestRunVerifySearch` — pass (persist did not change exit codes).
- `go test ./internal/inference/ -run TestSeamGrepGate` — pass (no backend-literal leak in the new cmd-tier code).
- `make check` (vet + full test) — green across all packages.

### Acceptance gates
- `grep 'verifyStateSchemaVersion = 1' internal/verifystate/store.go` ✓
- `grep 'func VerifyStatePath' internal/verifystate/store.go` ✓ + literal `verify-search-state.json` present ✓
- `grep -c 'internal/recall' internal/verifystate/store.go` = 0 ✓ (comments reworded to "the recall store"; zero import of internal/recall)
- `grep -c 'verifystate.Save\|persistFn' cmd/villa/verify_search.go` ≥ 1 ✓
- pure `evalSearchVerify` body contains NO verifystate reference (persist lives only in the run path) ✓

## Deviations from Plan

None — plan executed exactly as written.

One clarification applied within the plan's intent: the acceptance gate `grep -c 'internal/recall' … returns 0` initially matched three doc-comment mentions of the analog package. The intent is "no IMPORT of internal/recall"; the attribution comments were reworded to "the recall store" so the gate passes while the clone-don't-import provenance stays documented. Tracked as a wording adjustment, not a logic change.

## Known Stubs

None.

## Threat Flags

None — the new surface (a 0600 verdict+timestamp file) is fully covered by the plan's threat register (T-34-01..04); no new endpoint, auth path, or trust boundary was introduced.

## Self-Check: PASSED

- Created files verified present: `internal/verifystate/store.go`, `internal/verifystate/store_test.go`, `cmd/villa/verify_search.go`, `cmd/villa/verify_search_test.go`, `34-01-SUMMARY.md`.
- Commits verified in history: `a79cf10` (Task 1), `adf8217` (Task 2).
