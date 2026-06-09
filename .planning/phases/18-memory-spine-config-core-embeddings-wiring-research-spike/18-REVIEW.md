---
phase: 18-memory-spine-config-core-embeddings-wiring-research-spike
reviewed: 2026-06-09T00:00:00Z
depth: deep
files_reviewed: 5
files_reviewed_list:
  - internal/config/villaconfig.go
  - internal/config/villaconfig_test.go
  - internal/memory/footprint.go
  - internal/memory/memory.go
  - internal/memory/memory_test.go
findings:
  critical: 0
  warning: 3
  info: 4
  total: 7
status: issues_found
---

# Phase 18: Code Review Report

**Reviewed:** 2026-06-09
**Depth:** deep
**Files Reviewed:** 5
**Status:** issues_found

## Summary

Reviewed the Phase 18 memory-spine config core and the new pure `internal/memory`
decision package. The code is well-structured, gofmt-clean, builds, and all 29
package tests plus `TestSeamGrepGate` pass. The LOAD-BEARING seam invariant holds:
`internal/memory` imports neither `os/exec` nor any container-image literal, and the
grep gate is green over it. Typed-Unknown degradation (`Footprint`) and fail-closed
reason accumulation (`Decide`) are implemented correctly and match the recommend/
preflight idiom.

No BLOCKER-level correctness or security defects were found. However, deep
cross-file analysis surfaced two substantive design tensions worth fixing before
this code is built on by later phases:

1. The struct doc comment makes an **unconditional** "byte-identical / NOT emitted
   to disk for a non-opted-in install" (SC#1) claim, but the shared `SaveVilla`
   writer marshals the full struct and **does** emit all 7 memory keys the first
   time any existing save-bearing command (`recommend --save`, `model swap`,
   `backend set`, `restore`) runs on a v1.2-upgraded install. The guarantee only
   holds for the pure load path the new test deliberately exercises.

2. `Decide`'s fail-closed validation of empty/zero memory fields is effectively
   **unreachable for configs loaded through the normal path**, because
   `normalizeVilla` self-heals every one of those fields to its default *before*
   `Decide` is ever called. The two invariants ("treat zero as unset → silently
   fill" vs "memory-on + missing field → refuse-with-reason") collide, and the
   self-heal wins for the hand-edited-config input `Decide` names as its boundary.

The remaining items are minor robustness and clarity notes.

## Warnings

### WR-01: Struct doc claims a byte-identical guarantee the shared `SaveVilla` writer breaks

**File:** `internal/config/villaconfig.go:52-57` (and `:61`, `:96-101`)
**Issue:**
The memory-field block doc states the fields "are NOT emitted to disk for a
non-opted-in install (byte-identical guarantee, SC#1/D-05)." This is only true for
the *pure load path*. `SaveVilla` / `SaveVillaTo` marshal the full `VillaConfig`
via `toml.Marshal`, and the memory fields have **non-zero defaults** (`768`,
`"villa-qdrant"`, `"villa-embed"`, ports `6333`/`8080`, model id). BurntSushi/toml
has no `omitempty`, so every save emits all 7 keys. Empirically confirmed: saving
`DefaultVillaConfig()` writes `memory_enabled`, `embedding_model`, `embedding_dim`,
`qdrant_addr`, `qdrant_port`, `embed_addr`, `embed_port`.

Real-world trigger: a v1.2 user upgrades to the v1.3 binary and runs any
save-bearing command (`recommend --save`, `villa model swap`, `villa backend set`,
`villa restore`). `LoadVilla` seeds the memory defaults, the command calls
`SaveVilla(c)`, and the on-disk config silently gains 7 memory keys the user never
opted into. The struct comment promises the opposite.

The new test `TestMemoryByteIdentical` does NOT catch this — by design it avoids
calling Save (see its own comment at `villaconfig_test.go:353-359`). So the
guarantee is asserted only for a path that was never going to violate it, while the
realistic save path is untested and does violate the stated invariant.

**Fix:** Either (a) scope the doc comment to the truth — "the load path is
read-only and never mutates a non-opted-in file; the shared SaveVilla writer DOES
re-emit the memory defaults once a save-bearing command runs, which is acceptable
because the values are inert defaults" — or (b) if true byte-identity across saves
is required, gate emission (e.g. a custom marshal that omits the memory block when
`MemoryEnabled == false`, or `toml:",omitempty"` plus zero-defaults resolved at
read time). Add a test that calls `SaveVillaTo` with a default (memory-off) config
and asserts the on-disk bytes against the chosen contract, so the guarantee is
verified on the path that can actually break it.

### WR-02: `Decide`'s fail-closed field validation is unreachable after `normalizeVilla`

**File:** `internal/memory/memory.go:40-66` vs `internal/config/villaconfig.go:131-161`
**Issue:**
`Decide` is documented as "the validation BOUNDARY between an untrusted (possibly
hand-edited) config.VillaConfig and the rest of the memory stack" and refuses
memory-on configs with empty `embedding_model`/`qdrant_addr`/`embed_addr` or
zero `embedding_dim`/`qdrant_port`/`embed_port`. But any config that reaches
`Decide` through the normal entry points (`LoadVilla`, `LoadVillaFrom`, `Parse`)
has already passed through `normalizeVilla`, which self-heals every one of those
exact fields from zero/empty to its default. So a hand-edited
`memory_enabled = true` config with all those fields blanked is *silently repaired*
before `Decide` sees it. Confirmed empirically: `Parse` of such a config followed
by `Decide` yields `{Enabled:true, Valid:true, Reasons:[]}` — the refusal never
fires for the untrusted-config input `Decide` names as its reason for existing.

The two invariants conflict: `normalizeVilla` ("treat zero/empty as unset → fill
default") pre-empts `Decide` ("memory-on + missing field → refuse-with-reason").
`Decide` can only return `Valid:false` for an in-memory struct constructed without
going through normalize — which is not how configs arrive in production.

This is not a crash or security hole, but the fail-closed gate is largely dead code
for its stated primary input, and a later phase wiring `Decide` into install/up will
get a false-green on a malformed hand-edit because the self-heal masked it.

**Fix:** Decide which layer owns the contract and make it coherent. Options:
(a) document `Decide` as a defense-in-depth gate for direct/in-memory construction
and note that loaded configs are pre-normalized (lower the doc's "untrusted
hand-edited" claim); or (b) if `Decide` is meant to catch hand-edits, do NOT
self-heal the memory fields in `normalizeVilla` when `MemoryEnabled == true` — let
the blanks survive so `Decide` can refuse them with a reason. Add a test that runs a
malformed memory-on config through the *real* `LoadVillaFrom` → `Decide` chain (not
a hand-built struct) and asserts the intended outcome, so the interaction is pinned.

### WR-03: `Decide` accepts out-of-range ports (> 65535)

**File:** `internal/memory/memory.go:55-56`, `:61-62`
**Issue:**
Port validation is `cfg.QdrantPort <= 0` / `cfg.EmbedPort <= 0` only. A hand-edited
`qdrant_port = 999999` or `embed_port = 70000` passes `Decide` as valid, then flows
through `RenderView` into orchestrate, where it composes an unusable endpoint URL
that fails only at container/connect time — exactly the kind of late, opaque failure
the refuse-with-reason gate exists to prevent up front. The dim check has the same
shape (`<= 0` only), though an over-large dim is caught later by the Phase-23 swap
guard; the port has no such downstream backstop in the pieces handed to orchestrate.
**Fix:** Tighten the port checks to a valid TCP range, e.g.:
```go
if cfg.QdrantPort <= 0 || cfg.QdrantPort > 65535 {
    reasons = append(reasons, "qdrant_port must be in 1..65535 ...")
}
if cfg.EmbedPort <= 0 || cfg.EmbedPort > 65535 {
    reasons = append(reasons, "embed_port must be in 1..65535 ...")
}
```
Add table cases for the upper-bound rejection.

## Info

### IN-01: `TestRenderView` URL/host:port check has a redundant, weaker sub-condition

**File:** `internal/memory/memory_test.go:180`
**Issue:** `if strings.Contains(v, "://") || strings.Contains(v, ":")` — the second
clause (`":"`) already matches everything the first clause (`"://"`) would, so the
`"://"` test is dead. The assertion still works for the current bare-DNS values, but
the OR is misleading and a `host:port` value would be reported as the generic case,
not the URL case the message distinguishes. Harmless, but the test reads as if it
checks two things when it checks one.
**Fix:** Drop the redundant `"://"` clause, or split into two separate asserts with
distinct messages if both shapes are meant to be reported differently.

### IN-02: `Footprint` Unknown reason has a trailing-space artifact for empty input

**File:** `internal/memory/footprint.go:42`
**Issue:** `"memory: no footprint known for embedding model "+modelID` produces a
reason ending in a trailing space when `modelID == ""` (the empty-string Unknown
case the test exercises). Cosmetic only — the test asserts `Source != ""`, which
holds.
**Fix:** Special-case the empty id, e.g. `"... for empty embedding model id"` when
`modelID == ""`, or quote the id: `fmt.Sprintf("... model %q", modelID)`.

### IN-03: `EmbeddingDim` is documented LOAD-BEARING but only `> 0` is validated

**File:** `internal/memory/memory.go:49-51`, `internal/config/villaconfig.go:65-68`
**Issue:** The dim is described as the pinned anchor whose change "corrupts existing
Qdrant vectors," yet `Decide` accepts any positive dim. This is acceptable for
Phase 18 (the memory-aware swap guard is explicitly deferred to Phase 23 per the
struct doc), but flagging it so the deferral is tracked and not silently lost: a
positive-but-mismatched dim is a real future corruption vector with no current
guard.
**Fix:** No change required this phase. Ensure the Phase-23 swap guard is the place
that pins dim equality against the persisted/embedded-vector dimension, and
cross-reference it from this comment so the deferral is auditable.

### IN-04: `RenderView` does no validation by contract — relies on caller ordering

**File:** `internal/memory/memory.go:83-95`
**Issue:** `RenderView` is documented to skip validation ("callers gate with Decide
first"). That is a reasonable pure-core split, but nothing enforces the ordering: a
future caller can call `RenderView` without `Decide` and hand orchestrate an
empty/invalid endpoint piece. No defect today (no production caller exists yet —
this is the spike phase), but the implicit contract is a latent foot-gun once
wiring lands.
**Fix:** When the Phase-22/23 caller is written, ensure it calls `Decide` and
checks `Valid` before `RenderView`; consider a doc note or a debug-time assertion.
Optionally, return `(MemoryRenderInput, bool)` mirroring the Decide result so the
ordering is harder to skip. No action needed in Phase 18.

---

_Reviewed: 2026-06-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
