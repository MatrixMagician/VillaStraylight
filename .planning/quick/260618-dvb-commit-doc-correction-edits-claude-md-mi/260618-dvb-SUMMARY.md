---
id: 260618-dvb
type: quick
status: complete
completed: 2026-06-18
---

# Quick Task 260618-dvb — Summary

**Outcome:** Corrected the shipped-milestone records in two doc files to reflect
the true release history (v1.0 → v1.4, all tagged on `main`). Docs-only; no code
changed.

## Changes

- **`CLAUDE.md`**
  - Source-of-truth list: `shipped milestone history (v1.0, v1.1)` → `(v1.0 through v1.4)`.
  - `**Shipped:**` line now enumerates all five milestones: v1.0 MVP, v1.1 ROCm
    Opt-In Backend, v1.2 Operability, v1.3 Memory & Knowledge, v1.4 Coding Agent.
- **`.planning/MILESTONES.md`**
  - v1.2 heading: removed the stale `> **Release status:** … pending via /gsd-ship`
    blockquote and changed `Completed:` → `Shipped:` (tag `v1.2` exists, 2026-06-08).
  - v1.1 heading: `Shipped: 2026-06-06` → `Shipped: 2026-06-07`, matching the
    `v1.1` git-tag creatordate. Timeline/proof dates (2026-06-06) deliberately
    left intact — they record when the work/benchmarks happened, not the tag.

## Verification

- `git diff` limited to the four intended hunks across the two files.
- Tag dates cross-checked: `git for-each-ref --sort=creatordate refs/tags` →
  v1.0 2026-06-05, v1.1 2026-06-07, v1.2 2026-06-08, v1.3 2026-06-11, v1.4 2026-06-15.
- `.planning/graphs/*` pre-existing working-tree changes left untouched/unstaged.

## Notes

- Origin: discovered while reconciling the MemPalace temporal KG against git tags.
  The KG and a graphmind context memory now record the same corrected history.
- No dedicated `## v1.0 MVP` section exists in MILESTONES.md (history runs v1.4→v1.1);
  left as-is — a content addition, not a correction, pending user direction.
