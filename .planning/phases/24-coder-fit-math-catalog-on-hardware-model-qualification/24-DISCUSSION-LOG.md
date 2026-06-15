# Phase 24: Coder Fit Math, Catalog & On-Hardware Model Qualification - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-12
**Phase:** 24-coder-fit-math-catalog-on-hardware-model-qualification
**Mode:** `--auto` — all gray areas auto-selected; recommended option chosen per question, no interactive prompts
**Areas discussed:** Catalog schema-3 shape, Coder fit stage & residency derivation, On-hardware qualification protocol, Toolbox re-pin decision handling

---

## Catalog schema-3 shape

| Option | Description | Selected |
|--------|-------------|----------|
| Append-only optional fields on `CatalogModel` | `role` (absent ⇒ chat), `agent_ctx`, `cache_reuse_safe` (absent ⇒ false), sampling preset, `template_provenance`; revision-pinned shard URLs | ✓ |
| Separate coder sub-catalog / parallel models array | New top-level array for coder entries | |
| Role as required field on all entries | Backfill `role:"chat"` onto existing entries | |

**Auto-selection rationale:** Append-only optional fields keep existing chat entries byte-untouched (SC#1 demands it) and follow the shipped schema-evolution discipline; a parallel array or required-field backfill would touch frozen entries.

---

## Coder fit stage & residency derivation

| Option | Description | Selected |
|--------|-------------|----------|
| Fit at catalog-declared per-entry `agent_ctx`; standalone inequality; `swap` if best coder fits post-reservation envelope, else `shared`; always-stamped `coder` block above `SchemaVersion` | Residency purely an output of fit math at the ctx that will be rendered; D-03 unconditional-stamp precedent | ✓ |
| Global agent ctx constant (e.g. 64k) for all coder entries | Simpler but violates fit-at-rendered-ctx when entries differ | |
| Additive fit (coder + chat must co-fit) | Models co-residency, which is deferred to v2 | |
| `coder` block omitempty when no coder fits | Hides the honest refusal shape; breaks D-03 precedent | |

**Auto-selection rationale:** STATE.md Phase-24 risk note and PITFALLS #4 require fit at the agent's RENDERED ctx; swap semantics mean the chat model is unloaded, so the inequality is standalone; the v1.3 D-03 memory fields set the unconditional-stamp precedent.

---

## On-hardware qualification protocol

| Option | Description | Selected |
|--------|-------------|----------|
| Dev-time real-agent loop: locally-fetched Crush v0.76.0 (qualification tool only) → llama-server `--jinja` on pinned image; multi-step read→edit→verify task; KV measured via /metrics + GTT delta | Real agent-in-the-loop per CODER-03; measurement replaces computed estimates before freeze | ✓ |
| Scripted curl-driven multi-turn tool-call harness only | No real agent in the loop — weaker than the SC demands | |
| Defer qualification until Phase 26 ships pinned Crush | Catalog would freeze unqualified — forbidden by SC#3 | |

**Auto-selection rationale:** SC#3 says "benchmark scores alone never qualify an entry" and demands a real multi-step agent-in-the-loop loop; using Crush ad hoc as a dev-time qualification tool satisfies this without pre-empting Phase 26's pinned delivery. The box is the live gfx1151 dev host, so qualification runs for real.

---

## Toolbox re-pin decision handling

| Option | Description | Selected |
|--------|-------------|----------|
| Evidence-first decision before catalog freeze: check pinned digest's llama.cpp vintage vs PR #16095 + Feb-2026 parser fixes + per-model `--cache-reuse`; record as numbered decision; any re-pin digest-pinned; Next ships only if proven, else deferred honestly | SC#4 verbatim, deletion-over-hope | ✓ |
| Re-pin preemptively to latest toolbox digest | Unjustified churn; violates evidence-first discipline | |
| Ship Qwen3-Coder-Next on documented upstream support claims | "Shipped on hope" — explicitly forbidden by SC#3 | |

**Auto-selection rationale:** SC#4 requires the decision recorded with evidence before freeze; v1.1 standing decision requires digest pins; D-12 keeps the honest-deletion posture.

---

## Claude's Discretion

- Exact Go field/JSON key names for new catalog + recommend fields (existing conventions; `SchemaVersion` stays last).
- Golden test organization for the two schema-3 re-freezes.
- Exact qualification task script/steps and evidence-capture format.
- Human-readable CLI rendering of the coder block.

## Deferred Ideas

- Co-resident `villa-coder` unit (CODER-V2-01, 128 GB fit-gated v2 stretch).
- Qdrant/villa-embed MCP semantic code search (CODER-V2-02, behind a must-WIN numeric eval).
- KV-cache quantization as default (only ever catalog-declared + benched, per entry).
- Render delta / swap verb / agent delivery / addon / surfacing — Phases 25–28.
