---
id: 260618-dvb
type: quick
status: complete
created: 2026-06-18
description: Commit doc-correction edits — CLAUDE.md milestone lines + MILESTONES.md v1.2 note and v1.1 date
---

# Quick Task 260618-dvb: Doc-correction edits (milestone records)

## Context

While reconciling MemPalace's temporal KG against the git tags this session, two
planning/doc files were found to misstate the shipped-milestone history. The repo
has shipped through **v1.4** (tags `v1.0`..`v1.4` on `main`), but the docs still
read as if only v1.0/v1.1 shipped and carried a stale v1.2 "pending /gsd-ship"
note. The edits were applied to the working tree; this task records and commits
them with GSD tracking.

## Tasks

### Task 1 — Correct milestone records in CLAUDE.md and MILESTONES.md

- **files:** `CLAUDE.md`, `.planning/MILESTONES.md`
- **action:**
  - `CLAUDE.md` line 13: `shipped milestone history (v1.0, v1.1)` → `(v1.0 through v1.4)`
  - `CLAUDE.md` line 24 (`**Shipped:**`): expand `v1.0 MVP and v1.1 (ROCm Opt-In Backend)` to enumerate all five shipped milestones (v1.0 MVP, v1.1 ROCm Opt-In Backend, v1.2 Operability, v1.3 Memory & Knowledge, v1.4 Coding Agent).
  - `.planning/MILESTONES.md` v1.2 heading: drop the stale `> **Release status:** … pending via /gsd-ship` blockquote and change `Completed:` → `Shipped:` (the `v1.2` tag now exists, creatordate 2026-06-08).
  - `.planning/MILESTONES.md` v1.1 heading: `Shipped: 2026-06-06` → `Shipped: 2026-06-07` to match the `v1.1` git tag creatordate. Timeline / proof-of-value dates (2026-06-06) left intact — those are work/measurement dates, not the tag date.
- **verify:** `git diff -- CLAUDE.md .planning/MILESTONES.md` shows exactly the four hunks above; tag dates cross-checked with `git for-each-ref --sort=creatordate refs/tags`.
- **done:** Both files committed; no code touched; `.planning/graphs/*` working-tree noise left unstaged.

## must_haves

- **truths:** Shipped milestones are v1.0 (2026-06-05), v1.1 (2026-06-07), v1.2 (2026-06-08), v1.3 (2026-06-11), v1.4 (2026-06-15), all tagged on `main`.
- **artifacts:** Corrected `CLAUDE.md` and `.planning/MILESTONES.md`.
- **key_links:** `CLAUDE.md`, `.planning/MILESTONES.md`.
