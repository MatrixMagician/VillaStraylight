# Phase 21: Conversational Recall Indexer - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-10
**Phase:** 21-conversational-recall-indexer
**Areas discussed:** Chat-history access path, Index destination + retrieval, Chat→document granularity, Incremental state + staleness, Command surface + core layout, Whose chats / auth identity
**Mode:** `--auto` (single pass; recommended option auto-selected for every question; no user prompts)

---

## Chat-history access path

| Option | Description | Selected |
|--------|-------------|----------|
| OWUI REST API over loopback | Fixed-arg curl seam, token mint — reuses proven Phase-20 drive; respects OWUI auth | ✓ |
| Read webui.db SQLite directly | Banned: CGO breaks static binary; unstable internal schema; bypasses auth | |
| podman exec into the container | Fragile, shell-adjacent, no precedent | |

**Choice:** OWUI REST over loopback (recommended default)
**Notes:** No new host port; `runLoopbackCurl`/`mintAdminToken` already on-hardware-proven.

---

## Index destination + retrieval mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| villa-managed OWUI Knowledge collection | Content flows through OWUI's own chunk→embed→Qdrant path; native citations; Phase-23 backup covers it | ✓ |
| Direct Qdrant vector writes | Invisible to OWUI, breaks citations, violates OWUI-owned multitenant layout | |

**Choice:** OWUI Knowledge collection (recommended; ROADMAP says "chats → Knowledge")
**Notes:** RECALL-02 retrieval via model-attached knowledge / chat reference — exact mechanism is a research flag (PersistentConfig=False interaction).

---

## Chat→document granularity

| Option | Description | Selected |
|--------|-------------|----------|
| Per-chat role-labeled transcript | One file per chat, deterministic name; OWUI chunker splits; delete+re-add on change | ✓ |
| Per-message documents | Fine-grained but explodes file count and loses conversational context | |

**Choice:** Per-chat transcript (recommended)

---

## Incremental state + staleness

| Option | Description | Selected |
|--------|-------------|----------|
| Flat JSON state under $XDG_DATA_HOME/villa/ | chat_id → updated_at/file_id map; diff-driven incremental; v1.2 persistence rule | ✓ |
| Re-derive from OWUI every run | No memory of file-id mapping; cannot detect deletions cleanly | |
| Full re-index every run | Simple but wasteful; defeats RECALL-03 incremental requirement | |

**Choice:** Flat JSON state file (recommended)
**Notes:** Staleness honesty: typed-Unknown WARN when OWUI unreachable; partial runs persist truth.

---

## Command surface + core layout

| Option | Description | Selected |
|--------|-------------|----------|
| `villa recall index` / `villa recall status` parent verb | Mirrors `villa verify memory` precedent; room for growth; pure `internal/recall` core + Deps seam | ✓ |
| Flags on existing verbs (`villa up --index` etc.) | Muddles lifecycle verbs with content operations | |

**Choice:** `villa recall` parent verb (recommended)
**Notes:** Memory disabled → refuse-with-remediation (explicit user request can't honestly no-op).

---

## Whose chats / auth identity

| Option | Description | Selected |
|--------|-------------|----------|
| Operator's chats via least-privilege confirmed path | Research resolves: per-user token vs admin endpoint; service account only sees its own chats | ✓ |
| Always the seeded service account | Would index the wrong (empty) chat set on this box | |
| Always admin export endpoint | Possibly over-privileged; availability on pinned digest unconfirmed | |

**Choice:** Operator's chats; mechanism is a genuine research flag (recommended)

---

## Claude's Discretion

- Verb/flag spellings, state schema field names, transcript format, poll/backoff values.
- Whether `recall status --json` ships now (own new contract) or waits for Phase 23.
- Batch sizing for large histories (embed throughput on gfx1151 is the constraint).

## Deferred Ideas

- Scheduled auto-reindex (timer/watch) — backlog.
- status/dashboard recall rows — Phase 23. Backup of state + Qdrant volume — Phase 23.
- recommend/preflight/doctor index-size awareness — Phase 22.
