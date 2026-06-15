# Phase 28: Agent Surfacing & Contracts - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-15
**Phase:** 28-agent-surfacing-contracts
**Mode:** `--auto` (single pass; recommended default auto-selected for every area)
**Areas discussed:** Status block representation, Status block fields, Dashboard Agent panel, Doctor checks & dominance, Backup coverage & binary identity, Usage & cache-effectiveness

---

## Status `coding` block representation

| Option | Description | Selected |
|--------|-------------|----------|
| Pointer block, omitted when disabled (mirror v1.3 Memory `,omitempty`) | Hidden-until-data; coding-off byte-identical except schema version | ✓ |
| Always-present block with `enabled: false` | Block always serialized; larger off-state diff | |

**Choice:** Mirror the v1.3 `Memory *MemoryInfo,omitempty` pointer-block precedent (D-01/D-02).
**Notes:** `reportSchemaVersion` 3→4 happens ONCE at end of phase, coding-on/off goldens refrozen together (D-04).

---

## Status block field set

| Option | Description | Selected |
|--------|-------------|----------|
| ROADMAP SC1 set: enabled, version+pin-match, model, mode, residency | Append-only within block | ✓ |
| Minimal: enabled + model only | Drops diagnosability signals | |

**Choice:** Full SC1 field set (D-03).

---

## Dashboard Agent panel

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror Memory panel, hidden-until-data | Rendered only when `coding` block present | ✓ |
| Always-rendered panel with empty state | Shows shell when agent disabled | |

**Choice:** Mirror the Memory panel hidden-until-data treatment (D-05).

---

## Doctor checks & dominance ordering

| Option | Description | Selected |
|--------|-------------|----------|
| Fold into `internal/doctor`; reuse verify-agent round-trip + RunningOffloadVerdict; FAIL dominates HTTP-200 | Honesty-by-construction, no false-green | ✓ |
| New standalone command; HTTP-200 as health | Re-rolls probes; risks false-green | |

**Choice:** Fold into existing doctor core; offload/residency FAIL dominates (D-06/D-07).

---

## Backup coverage & binary identity

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror BAK-01: config in archive, binary identity-recorded + excluded | Same as model-weights pattern | ✓ |
| Include binary bytes in archive | Bloats archive; diverges from weights pattern | |

**Choice:** Mirror BAK-01 exclude + manifest identity-record (D-08).

---

## Usage & cache-effectiveness

| Option | Description | Selected |
|--------|-------------|----------|
| Per-model via v1.2 usage core; cache `cache_n/prompt_n` ratio with typed-Unknown degradation | Reuses existing accounting + metrics scrape | ✓ |
| New usage path for the agent | Duplicates v1.2 core | |

**Choice:** Reuse v1.2 usage core (coder = distinct model); cache ratio from metrics scrape, Unknown when absent (D-09/D-10).

---

## Claude's Discretion

- Exact JSON field names within the `coding` block, panel layout/CSS, human-readable status table
  wording — match the Memory block/panel idiom; lock at plan time (append-only/byte-frozen once golden).

## Deferred Ideas

- **`install-reverts-rocm-to-vulkan`** (todo, score 0.6) — reviewed, NOT folded. An `install.go`
  backend-persistence bug (different domain; self-tagged maintenance pass). Keep as a standalone
  `/gsd-quick`/`/gsd-debug` fix, not Phase 28 scope.
