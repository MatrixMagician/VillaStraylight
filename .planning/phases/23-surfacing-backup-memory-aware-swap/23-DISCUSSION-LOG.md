# Phase 23: Surfacing, Backup & Memory-Aware Swap - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-10
**Phase:** 23-surfacing-backup-memory-aware-swap
**Areas discussed:** Status/dashboard surfacing shape, Single contract evolution mechanics, Backup/restore of the Qdrant volume, Memory-aware model swap
**Mode:** `--auto` (single pass; recommended option auto-selected for every question; no interactive prompts)

---

## Status/dashboard surfacing shape (CTRL-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse non-GPU ServiceStatus rows | villa-qdrant/villa-embed as `Services` rows via the OWUI/dashboard-row pattern (OffloadApplies=false), gated on memory_enabled | ✓ |
| New top-level Memory section only | Separate memory block, no service rows | |
| Dashboard-specific probes | Dashboard probes services itself | |

**Choice:** Reuse the existing non-GPU row fold + tail-appended memory fields (active embedding model + recall-index summary); dashboard folds the same status core.
**Notes:** Recall summary included under the same single 2→3 bump per the Phase-21 deferral — no second evolution is available later.

---

## Single contract evolution mechanics (CTRL-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Unconditional 2→3 bump | Version describes the contract shape; memory fields omitempty | ✓ |
| Bump only when memory enabled | schema_version varies with config | |

**Choice:** Unconditional `reportSchemaVersion` 2→3; goldens re-frozen once with `-update`.

---

## Backup/restore of the Qdrant volume (CTRL-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Extend internal/backup core | Optional Qdrant-volume + recall-state.json entries; seam-sourced volume name; manifest gains model+dimension fields; clean-recreate-before-import; skew WARN+confirm | ✓ |
| Parallel memory-backup path | Separate backup verb/archive for the memory stack | |
| Probe collection dim at restore | Ask Qdrant instead of recording dimension in manifest | |

**Choice:** Extend the existing core; dimension recorded in the manifest at backup time; restore mirrors the image-skew confirm precedent; old backups stay restorable.

---

## Memory-aware model swap (CTRL-05)

| Option | Description | Selected |
|--------|-------------|----------|
| Guard config-applied paths + assert chat-swap invariant | `recall index` refuses on confident dimension/model mismatch (via the Phase-21 recall-state stamp); install/up WARN with remediation; chat swap leaves memory units/collections byte-identical (asserted by test); no auto-reindex | ✓ |
| New `model swap --embedding` verb | Sanctioned embedding-swap surface with guided re-index | |
| Auto-reindex on mismatch | Guard mutates by rebuilding the index | |

**Choice:** Guard where config takes effect; no new verb; never auto-reindex.

---

## Claude's Discretion

- Exact Report/manifest field names and JSON keys; golden fixture layout; dashboard panel layout.
- Whether `backupSchemaVersion` bumps for the append-only manifest growth.
- Exact verb set carrying the D-10 WARN beyond the `recall index` refusal; remediation strings.
- Recall-state read seam shape inside status (Deps func vs store loader behind Deps).
- Plan sequencing (CTRL-02/04/05 largely independent; on-hardware verification last).

## Deferred Ideas

- `villa model swap --embedding` verb — backlog (new capability).
- Auto-reindex (scheduled / on swap / on restore) — backlog.
- GPU passthrough for villa-embed — backlog (carried from Phase 22).
- Reranker/hybrid, SearXNG, multi-user/remote — v2.
