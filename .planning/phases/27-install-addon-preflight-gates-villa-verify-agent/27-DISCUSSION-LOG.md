# Phase 27 Discussion Log

**Date:** 2026-06-14
**Mode:** discuss-phase (interactive, default mode)
**Areas selected:** all four (Addon opt-in & gate surface; Pre-stage scope; verify agent egress proof; llama-down control + uninstall scope)

For human reference only — not consumed by downstream agents. The canonical output is `27-CONTEXT.md`.

---

## Area 1 — Addon opt-in & gate surface
- **Options presented:** Mirror memory addon (persisted config field + `--coding-agent` flag, one install verb) / Separate `villa install agent` subcommand / Flag-only no persisted field.
- **Selected:** Mirror memory addon (Recommended).
- **Notes:** Keeps config as single source of truth; addon-off renders byte-identical; reuses `install_memory.go` gate pattern. → D-01.

## Area 2 — Pre-stage scope in the sanctioned outbound window
- **Options presented:** Recommended residency entry only / All 3 qualified entries / Recommended + `--coder-model` override.
- **Selected:** Recommended residency entry only (Recommended).
- **Notes:** Mirrors `nomicEmbedShard` single-shard; minimizes outbound window + disk; idempotent presence-skip; binary via Phase-26 seam. Multi-entry/override → deferred. → D-02..D-04.

## Area 3 — `villa verify agent` egress proof mechanics
- **Options presented:** Non-interactive `crush run` tool-call round-trip under egress-block + negative-control-first / Minimal PONG completion only / Packet-capture assertion.
- **Selected:** Non-interactive `crush run` round-trip (Recommended).
- **Notes:** Reuses `verify_memory.go` four-layer seam; PASS/FAIL only; negative control asserted FIRST; real tool-call path, not a bare completion. → D-05..D-07.

## Area 4 — llama-down control + uninstall scope
- **Q4a llama-down control — Selected:** Second control folded inside `villa verify agent` (Recommended). Verdict = ctrl1.pass && ctrl2.failed-as-expected. One verb proves both PRIV-06 clauses. → D-08.
- **Q4b uninstall scope — Selected:** Binary + config removed always; staged coder GGUF governed by existing keep/remove-models flag (default-keep); config.toml left in place (Recommended). → D-10.

## Deferred ideas
- Multi-entry / `--coder-model` override pre-staging.
- Agent surfacing (Phase 28).
- villa-driven agent upgrade/re-pin verb.

## Claude's discretion (handed to planner/researcher)
- Config field name; disk/envelope BLOCK thresholds; cloud-credential env allowlist; `verify_agent.go` layout + on-host egress-block mechanism; the `crush run` task that forces a tool-call round-trip; plan/wave decomposition.
