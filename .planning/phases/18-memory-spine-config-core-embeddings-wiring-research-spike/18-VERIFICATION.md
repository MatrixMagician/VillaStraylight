---
phase: 18-memory-spine-config-core-embeddings-wiring-research-spike
verified: 2026-06-09T00:00:00Z
status: passed
score: 6/6 must-haves verified
overrides_applied: 0
---

# Phase 18: Memory Spine (config core + pure decision core + research spike) Verification Report

**Phase Goal:** `villa` has a pure memory-decision core and config fields that make the memory stack opt-in and config-driven, and the version-sensitive integration choices (embeddings runtime, exact Open WebUI env keys, embedding model + footprint) are resolved and pinned before any unit or env block is frozen.
**Verified:** 2026-06-09
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

This phase has three roadmap Success Criteria. All three are verified against the
real codebase (not SUMMARY claims), plus the scope-discipline boundary and the
code-review fix (commit `579e575`) the task brief flagged for confirmation.

### Observable Truths

| #   | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | SC#1 — user can set `memory_enabled` (+ embedding model / dim / Qdrant+embed addrs/ports) in `config.toml`; loads into `VillaConfig` | ✓ VERIFIED | 7 memory fields on `VillaConfig` (`internal/config/villaconfig.go:62-82`), all TOML-tagged; `defaultConfig()` is the single home of literals (`:96-103`). |
| 2 | SC#1 — existing v1.2 install stays BYTE-IDENTICAL until opt-in (default off, self-healing/defaulted fields) | ✓ VERIFIED | `MemoryEnabled` defaults `false`; `normalizeVilla` self-heals zero/empty fields from `defaultConfig()` (`:132-162`), never widens a bind. `TestMemoryByteIdentical` + `TestMemorySaveOmitsKeysWhenDisabled` PASS. |
| 3 | SC#1 fix (579e575) holds — `marshalVilla` drops the 7 `memory_*` keys when `memory_enabled=false` on every save-bearing command | ✓ VERIFIED | `marshalVilla` zeroes the 7 fields when `!c.MemoryEnabled`, so `,omitempty`/`,omitzero` tags drop them (`:226-236`); shared by `SaveVilla`+`SaveVillaTo`. `TestMemorySaveOmitsKeysWhenDisabled` asserts memory-off save writes NO key and memory-on save persists all 7. |
| 4 | SC#2 — `internal/memory` pure core computes Footprint / Decide / RenderView with NO host I/O; no `os/exec`, no image literal; `TestSeamGrepGate` green | ✓ VERIFIED | `memory.go` imports only `internal/config`; `footprint.go` only `internal/detect` (no `os/exec`, no image literal in non-comment code). `TestSeamGrepGate` PASS over `internal/memory`; `go test ./internal/memory/` 18 tests PASS. |
| 5 | SC#3 — embeddings-runtime (D-07), embedding model+footprint (D-08), exact OWUI env keys (D-09 incl. mandatory `ENABLE_PERSISTENT_CONFIG=False`) RECORDED as decisions later phases consume | ✓ VERIFIED | `18-DECISIONS.md` records D-07/D-08/D-09 with pinned values, rationale, downstream-consumer phase, and evidence pointers to `18-RESEARCH.md`. `ENABLE_PERSISTENT_CONFIG=False` marked MANDATORY/LOAD-BEARING. |
| 6 | SC#3 — `ENABLE_QDRANT_MULTITENANCY_MODE` marked choice-pending Phase 20 (unmade decision unambiguous, not omitted) | ✓ VERIFIED | `18-DECISIONS.md:86,113-115` — "CHOICE PENDING — Phase 20, before any vectors exist." |

**Score:** 6/6 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/config/villaconfig.go` | memory_* fields + single-source defaults + self-heal + omit-on-disabled save | ✓ VERIFIED | 7 fields, `defaultConfig()` sole literal home, `normalizeVilla` self-heal, `marshalVilla` omit path. |
| `internal/config/villaconfig_test.go` | default-off load, byte-identical, self-heal, never-widen, save-omits-keys | ✓ VERIFIED | 15 tests PASS incl. `TestMemoryByteIdentical`, `TestMemorySaveOmitsKeysWhenDisabled`. |
| `internal/memory/memory.go` | `Decide` + `RenderView` + `Decision`/`MemoryRenderInput` types | ✓ VERIFIED | Exports present; fail-closed gate with port 1..65535 bound; pure. |
| `internal/memory/footprint.go` | `Footprint(modelID) -> detect.Bytes` typed-Unknown | ✓ VERIFIED | KnownBytes(512 MiB) on hit; UnknownBytes (Known=false) on miss/empty; single constant home. |
| `internal/memory/memory_test.go` | table-driven Footprint/Decide/RenderView tests | ✓ VERIFIED | `TestFootprint`, `TestDecide`, `TestDecideAccumulatesReasons`, `TestRenderView` all PASS. |
| `18-DECISIONS.md` | canonical D-07/D-08/D-09 record w/ evidence | ✓ VERIFIED | Present; all markers (`ENABLE_PERSISTENT_CONFIG`, `nomic-embed-text-v1.5`, `villa-embed`, `CHOICE PENDING`) found. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| `normalizeVilla` | `defaultConfig()` | `d := defaultConfig()` derivation (no re-hard-coded literals) | ✓ WIRED | `villaconfig.go:133`; every memory fill derives from `d`. |
| `LoadVillaFrom`/`Parse`/`LoadVilla` | `normalizeVilla` | existing call sites self-heal memory fields for free | ✓ WIRED | `:214`, `:309`, `:324`. |
| `marshalVilla` | omit-when-disabled | zero 7 fields when `!MemoryEnabled` + omit tags | ✓ WIRED | `:226-236`; shared by both save paths. |
| `memory.Footprint` | `detect.KnownBytes`/`UnknownBytes` | typed-Unknown return | ✓ WIRED | `footprint.go:40-45`. |
| `memory.Decide`/`RenderView` | `config.VillaConfig` | consumes Plan-01 memory fields as typed input | ✓ WIRED | `memory.go:49,95`. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Config memory tests pass | `go test ./internal/config/` | 15 passed | ✓ PASS |
| SC#1 named tests pass | `go test -run 'TestMemoryByteIdentical\|TestMemorySaveOmitsKeysWhenDisabled' -v` | both `--- PASS` | ✓ PASS |
| Memory core tests pass | `go test ./internal/memory/` | 18 passed (Footprint/Decide/RenderView) | ✓ PASS |
| Seam gate green over internal/memory | `go test ./internal/inference/ -run TestSeamGrepGate` | 1 passed | ✓ PASS |
| Full suite + vet green | `make check` | `go vet ./...` + all 21 packages `ok` | ✓ PASS |
| DECISIONS grep gate | `grep -E 'ENABLE_PERSISTENT_CONFIG\|nomic-embed-text-v1.5\|villa-embed\|CHOICE PENDING'` | 18 matches, exit 0 | ✓ PASS |
| No image-literal additions in phase-18 source | `git show <commits> -- <src> \| grep image-tokens` | none in non-comment code | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| INFRA-04 | 18-01, 18-02 | Memory stack is config-driven — new `config.toml` fields (enable flag, embedding model, ports/addrs) drive later Quadlet regeneration; units never hand-edited as authority | ✓ SATISFIED | Config fields + single-source defaults + self-heal (`villaconfig.go`); pure decision core consuming those fields (`internal/memory`); decisions pinned (`18-DECISIONS.md`). Render/recommend/preflight wiring is correctly deferred to Phases 19-23 (config is the source they will read). REQUIREMENTS.md:90,121 maps INFRA-04 → Phase 18 → Complete. |

No orphaned requirements: REQUIREMENTS.md maps only INFRA-04 to Phase 18, and both plans declare `requirements: [INFRA-04]`.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No `TBD`/`FIXME`/`XXX` debt markers; no stubs; no hardcoded empty render data | ℹ️ Info | The "flagged for on-hardware refinement in Phase 19" notes on the ~512 MiB footprint constant reference a concrete downstream phase (auditable, not a debt marker). |

### Scope Discipline (phase boundary)

Confirmed via `git show --name-only` across all 4 phase-18 source commits
(`7b93ad3`, `3ae6771`, `129fd13`, `579e575`): only 5 non-planning files were
touched — `internal/config/villaconfig.go` (+test) and the new `internal/memory`
package (+test). ZERO container-image literals introduced (seam gate green). NO
Quadlet unit (`internal/orchestrate/quadlet/*.tmpl`), NO OWUI env-block
(`openwebui.go`), and NO `recommend`/`preflight`/`doctor` call site touched —
those land in Phases 19-23 as designed. Phase stayed in lane.

### Human Verification Required

None. This phase is pure-Go decision logic + a documentation artifact, fully
verifiable by the test suite and static inspection. No visual/runtime/external
behavior to confirm by hand.

### Gaps Summary

No gaps. All three Success Criteria are met against the actual codebase:

- **SC#1** — config is opt-in and default-off; the byte-identical guarantee holds
  on the realistic save path (the WR-01 review gap fixed in `579e575` is confirmed
  in `marshalVilla` and pinned by `TestMemorySaveOmitsKeysWhenDisabled`).
- **SC#2** — `internal/memory` is a pure core (no `os/exec`, no image literal);
  `TestSeamGrepGate` and `go test ./internal/memory/` are green.
- **SC#3** — D-07/D-08/D-09 are durably recorded with evidence pointers, the
  mandatory `ENABLE_PERSISTENT_CONFIG=False` is flagged, and the unmade
  multitenancy decision is explicitly marked CHOICE PENDING — Phase 20.

`make check` (vet + full suite, 21 packages) is green. INFRA-04 is accounted for.

---

_Verified: 2026-06-09_
_Verifier: Claude (gsd-verifier)_
