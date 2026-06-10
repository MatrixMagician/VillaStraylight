# Phase 22: Control-Plane Fit + Host Gate - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-10
**Phase:** 22-control-plane-fit-host-gate
**Areas discussed:** Envelope reservation semantics, Embed GPU posture + footprint constant, Preflight memory-stack gate, Doctor memory fold + residency proof
**Mode:** `--auto` (single pass, invoked via `/gsd-progress --next --auto`) — all areas auto-selected, recommended option chosen per question.

---

## Envelope reservation semantics (CTRL-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Shrink envelope before chat-model fit | `Pick` subtracts the embedding reservation from the resolved envelope before any fit math, gated on `memory_enabled` | ✓ |
| Subtract inside `pickBest` | Per-candidate subtraction during fit scoring | |
| Post-hoc note | Recommend unchanged; only a note warns about embed memory | |

**Choice:** Shrink envelope first — SC#1 wording ("envelope shrinks first") makes this the contract, not a preference.
**Sub-decisions:** Unknown footprint → conservative 512 MiB default + honest note (never silently reserve 0); `recommendSchemaVersion` 1→2 append-only + goldens re-frozen once (distinct from the Phase-23 `status.Report` evolution).

---

## Embed GPU posture + footprint constant

| Option | Description | Selected |
|--------|-------------|----------|
| Keep villa-embed CPU-only; reserve against unified envelope | No unit change; 512 MiB constant stays; measure + record during verification | ✓ |
| Add GPU passthrough + measure GPU-resident footprint | Orchestrate/unit change; new measurement basis | |

**Choice:** CPU-only stays — unit changes are out of phase scope; on Strix Halo unified memory the reservation math is equivalent either way. Constant raised only if on-hardware measurement shows under-reservation.

---

## Preflight memory-stack gate (CTRL-06)

| Option | Description | Selected |
|--------|-------------|----------|
| New `checks_memory.go`, emitted only when `memory_enabled=true` | Mirrors `checks_resources.go` seams; opt-in discipline; byte-identical v1.2 output when off | ✓ |
| Extend `checks_resources.go` in place | Mixes memory-stack and base-stack concerns | |
| Always-on checks | Emits memory noise for non-memory installs | |

**Choice:** New topic-grouped file, opt-in gated. Two gates: vector-disk floor at the rootless volume storage root (research resolves the path properly) + embedder headroom from `memory.Footprint`. BLOCK on confident failure, typed-Unknown → WARN.

---

## Doctor memory fold + residency-under-load proof (CTRL-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse preflight checks via existing fold + service rows + real embed-load residency proof | Composition over re-implementation; real `/v1/embeddings` load via the proven install-proof reach; existing `RunningOffload` assert | ✓ |
| Standalone duplicate checks in doctor | Duplicates probe logic | |
| Synthetic memory pressure / static check only | Doesn't prove the SC#3 "under embedding/import workload" claim | |

**Choice:** Fold + real load. Semantics: confident chat-model CPU fallback under load = FAIL; unevaluable = typed-Unknown WARN; doctor never starts/mutates services to run the proof.

---

## Claude's Discretion

- Field names/JSON keys, check IDs, remediation strings, disk-floor threshold, embed request count/payload, golden layout.
- Whether headroom check shares `checkResources`'s meminfo seam or gets its own.
- Plan sequencing (CTRL-01 independent of CTRL-03/06).

## Deferred Ideas

- Phase 23: status/dashboard memory rows (schema 2→3), Qdrant backup/restore + skew WARN, memory-aware model swap.
- Backlog: GPU passthrough for villa-embed; auto-remediation of unfit hosts.
