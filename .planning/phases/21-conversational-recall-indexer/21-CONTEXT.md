# Phase 21: Conversational Recall Indexer - Context

**Gathered:** 2026-06-10
**Status:** Ready for planning

> Captured in `--auto` mode (single pass). Each decision below was auto-selected
> as the recommended option, grounded in ROADMAP Phase 21 (goal + 3 success
> criteria), REQUIREMENTS RECALL-01..03, the live Phase-19/20 stack (villa-qdrant +
> villa-embed + the wired OWUI on the gfx1151 host), and the Phase-20 REST-drive
> code that already exercises the exact OWUI APIs this indexer needs
> (`cmd/villa/verify_memory.go`). Review before planning.

<domain>
## Phase Boundary

A **`villa`-orchestrated, strictly-local indexer** that turns the user's past Open
WebUI chat history into a searchable **Knowledge collection** (chats → Knowledge)
so the assistant retrieves relevant past-chat content **by meaning** in new
conversations — with **incremental re-index under explicit `villa` control** and
**honest staleness reporting** (indexed count / last-indexed / stale count; never
silently stale).

**In scope:**
- A new `villa` command surface that (a) indexes past chats into a villa-managed
  OWUI Knowledge collection and (b) reports the index's honest current state.
- Chat content flows through **OWUI's own upload→embed→store path** (villa-embed
  768-dim + Qdrant over `villa.network`) — no new outbound, no new host port.
- Villa-side incremental state (what's indexed, as-of-when) persisted as a flat
  file under `$XDG_DATA_HOME/villa/` (v1.2 persistence rule).
- The retrieval half (RECALL-02): relevant past-chat content reaches a NEW chat's
  context by meaning — via the recall Knowledge collection being attached/referenced;
  research confirms the exact OWUI mechanism and any `ENABLE_PERSISTENT_CONFIG=False`
  interaction.

**Out of scope (later phases / deferred — do NOT build here):**
- `recommend` footprint reservation, `preflight` gates, `doctor` memory checks — **Phase 22**.
- `status`/dashboard memory/recall rows (`status.Report` schema 2→3), backup of the
  Qdrant volume + recall state, memory-aware swap — **Phase 23**.
- Any auto-reindex daemon/systemd timer — RECALL-03 is explicit-control; a timer is
  a new capability (backlog).
- Building a custom RAG/retrieval engine in Go — integrate OWUI Knowledge, never
  re-implement chunking/embedding/retrieval.

</domain>

<decisions>
## Implementation Decisions

### Chat-history access path (RECALL-01)
- **D-01:** Read past chats via the **OWUI REST API over the existing loopback
  PublishPort** using the established fixed-arg curl seam — NOT by reading
  `webui.db` directly (embedded SQLite is banned: CGO breaks the static binary;
  OWUI's internal schema is unstable and unversioned; bypassing OWUI auth is a
  privacy posture violation), and NOT via `podman exec`. Reuse the Phase-20
  primitives in `cmd/villa/verify_memory.go`: `mintAdminToken` (signin → signup
  fallback; service creds `villa-verify@localhost`), `runLoopbackCurl`/
  `runLoopbackCurlStdin` (fixed-arg, shell-free). No new host port (D-11 carried).
  `[auto] Access path — Q: "REST API vs direct webui.db vs podman exec?" → Selected: "OWUI REST over loopback, fixed-arg curl seam" (recommended)`

### Index destination + retrieval mechanism (RECALL-01/02)
- **D-02:** Index INTO a **villa-managed OWUI Knowledge collection** (e.g.
  "Villa Recall — Past Conversations") created/maintained via the knowledge API
  (`knowledge/create`, `files/` multipart upload, `knowledge/{id}/file/add` —
  all already driven by `driveRagUploadCite`). Chat content therefore flows
  through **OWUI's own chunk→embed→Qdrant path** (villa-embed, 768-dim,
  multitenant collection layout per Phase-20 D-01) — retrieval is native OWUI
  RAG **with citations**, and the Phase-23 Qdrant backup covers it for free.
  Do NOT write vectors into Qdrant directly behind OWUI's back (invisible to
  OWUI, breaks citations, violates the multitenancy layout OWUI owns).
  `[auto] Destination — Q: "OWUI Knowledge collection vs direct Qdrant writes?" → Selected: "villa-managed Knowledge collection via OWUI API" (recommended; roadmap says chats → Knowledge)`
- **D-03:** RECALL-02 (retrieval into a NEW chat) is satisfied by the recall
  Knowledge collection being **attached to the served model / referenced in chat**.
  Research MUST confirm the concrete mechanism on the pinned OWUI digest
  (model-level knowledge attachment via API vs per-chat `#` reference vs default
  RAG over all knowledge) and how it behaves under `ENABLE_PERSISTENT_CONFIG=False`;
  if attachment requires a one-time UI/API step, villa performs it during index
  (API) or `docs/MEMORY.md` documents it (UI) — semantic-not-keyword retrieval is
  the acceptance bar (SC#2).
  `[auto] Retrieval — Q: "model-attached knowledge vs per-chat reference?" → Selected: "attach villa's recall collection to the served model; research confirms exact mechanism" (recommended)`

### Chat→document granularity + re-index semantics (RECALL-01/03)
- **D-04:** One **role-labeled plain-text transcript per chat** (`user:`/
  `assistant:` turns, title + ISO date header), deterministically named from the
  OWUI chat id (e.g. `villa-recall-<chat-id>.txt`). OWUI's own chunker handles
  splitting — never hand-roll chunking. A **changed chat re-indexes by
  delete-then-re-add** of its file in the Knowledge collection (clean-replace,
  mirroring the v1.2 clean-recreate-before-import lesson — no stale-chunk leaks,
  no duplicate growth).
  `[auto] Granularity — Q: "per-chat transcript vs per-message documents?" → Selected: "per-chat transcript, delete+re-add on change" (recommended)`

### Incremental state + honest staleness (RECALL-03)
- **D-05:** Villa-side index state is a **flat JSON file under
  `$XDG_DATA_HOME/villa/` (e.g. `recall-state.json`)** — NEVER `config.toml`
  (config = configuration truth only), never SQLite — mapping
  `chat_id → {owui_updated_at, knowledge_file_id, indexed_at}` plus the knowledge
  collection id and embedding model/dim recorded for the Phase-23 skew guard.
  Incremental = diff the live chat list (id + updated_at) against the state file:
  new → add, changed → delete+re-add, deleted chat → remove from Knowledge.
  `[auto] State — Q: "state file vs re-derive from OWUI each run vs full re-index always?" → Selected: "flat JSON state under XDG data dir, diff-driven incremental" (recommended; v1.2 persistence rule)`
- **D-06:** **Honest staleness, typed-Unknown discipline:** the status surface
  reports indexed count, last-indexed timestamp, and stale/unindexed count
  computed against the LIVE chat list. If OWUI is unreachable or listing fails,
  report **Unknown — could not evaluate** (typed-Unknown → WARN), NEVER a stale
  count of 0 / a false "current". A partial index run that errored mid-way must
  persist what actually completed and report the remainder as stale.
  `[auto] Staleness — Q: "how to report when OWUI is down?" → Selected: "typed-Unknown WARN, never false-current" (recommended; honesty-by-construction)`

### Command surface + core layout (RECALL-01/03)
- **D-07:** New **`villa recall` parent verb** with `villa recall index`
  (incremental by default; `--rebuild` = clean-recreate the Knowledge collection
  then full re-index) and `villa recall status` (honest state report, human +
  `--json` if cheap). Mirrors the `villa verify memory` precedent: thin cobra
  caller + injectable `Deps` seam; **memory disabled → refuse-with-remediation**
  (`exitBlocked`, "enable memory_enabled and run villa install first") — an
  explicit user request to index cannot honestly no-op.
  `[auto] Verb — Q: "villa recall parent vs flags on existing verbs?" → Selected: "villa recall index/status parent verb" (recommended)`
- **D-08:** **ONE new pure core `internal/recall`** (the v1.2/v1.3 rule: each
  decision-logic feature gets one pure `internal/*` core): diff/plan computation
  (what to add/update/delete), transcript rendering, staleness classification —
  all unit-testable off-hardware with injected chat lists/state. Host effects
  (curl drives, file I/O) stay behind the cmd-tier `Deps` seam; `orchestrate`
  remains the only intentionally-impure module; NO image literals or backend
  markers in `internal/recall` (`TestSeamGrepGate` stays green; no allowlist edit
  expected).
  `[auto] Core — Q: "where does the logic live?" → Selected: "pure internal/recall core + cmd-tier seam" (recommended)`

### Whose chats / auth identity (RECALL-01, privacy)
- **D-09:** The indexer indexes **the operator's chats** on this single-operator
  box. Research MUST confirm the listing/read mechanism on the pinned OWUI digest:
  per-user token (`GET /api/v1/chats/` for the authenticated user) vs admin-scoped
  listing — and pick the **least-privilege path that reaches the operator's chats**.
  The service-account approach from Phase 20 (seeded `villa-verify@localhost`)
  only sees ITS OWN chats — if the operator's chats live under a different OWUI
  user, the indexer needs that user's token (research decides: documented token
  hand-off vs admin endpoint). Strictly local either way; credentials never leave
  the box; no plaintext secrets committed to config without flagging it in planning.
  `[auto] Identity — Q: "service account vs operator token vs admin endpoint?" → Selected: "operator's chats via least-privilege confirmed path; research resolves the token question" (recommended — genuine research flag)`

### Claude's Discretion
- Exact verb/flag spellings, JSON state schema field names, transcript format
  details, polling/backoff values — planner's call within D-01..D-09.
- Whether `recall status` gets `--json` golden treatment now or stays human-only
  until Phase 23 surfaces recall in `status.Report` — planner's call; if `--json`
  ships, it is a NEW contract (own schema_version), never a mutation of
  `status.Report` (that single evolution is reserved for Phase 23).
- Batch size / rate limiting for large chat histories (the REST drive is local;
  embed throughput on gfx1151 is the real constraint).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` → **Phase 21** section — goal + 3 success criteria (SC#1
  index locally, SC#2 semantic retrieval into new chats, SC#3 villa-controlled
  incremental + honest staleness).
- `.planning/REQUIREMENTS.md` — **RECALL-01/02/03** definitions.
- `.planning/PROJECT.md` — v1.3 milestone goal; integrate-not-rebuild; out-of-scope
  (custom Go RAG engine, cloud APIs); Key Decisions (Phase 20 rows: env-only wiring,
  zero-outbound proven at runtime).

### Prior-phase contracts this phase builds on
- `.planning/phases/20-open-webui-memory-rag-wiring-offline-lockdown/20-CONTEXT.md`
  — D-01 (`ENABLE_QDRANT_MULTITENANCY_MODE=True` locked, collection layout the
  indexer inherits), D-03 (`ENABLE_PERSISTENT_CONFIG=False` mandatory — any
  retrieval-mechanism setting must respect it), D-10/D-11 (proof-seam pattern, no
  new host port).
- `.planning/phases/20-…/20-03-SUMMARY.md` — on-hardware resolved specifics:
  admin-token mint path (A5: `POST /api/v1/auths/signup` first-user / seeded
  service account `villa-verify@localhost`), citation field (A6: top-level
  `sources`), the `villa up` re-render + `villa restart villa-openwebui` gotcha,
  service accounts live on the box.
- `.planning/phases/18-memory-spine-config-core-embeddings-wiring-research-spike/18-DECISIONS.md`
  — D-08 (nomic-embed-text-v1.5, 768-dim, `search_document:`/`search_query:`
  prefixes), `QDRANT_COLLECTION_PREFIX=open-webui` ("Namespacing for the Phase-21
  indexer").
- `docs/MEMORY.md` — operator doc to EXTEND with the recall verbs + retrieval
  enable-path (keep the default-off auto-extraction guidance intact).

### Code touchpoints (reuse/extend; primary edit sites)
- `cmd/villa/verify_memory.go` — **the API drive to generalize**: `mintAdminToken`
  (:274), `driveRagUploadCite` (:172 — knowledge/create, files/ multipart,
  knowledge file/add), `pollFileProcessed` (:353), `runLoopbackCurl`/`…Stdin`
  (:419/:425 — fixed-arg, shell-free). Lift shared pieces rather than duplicating.
- `cmd/villa/verify.go` — `newVerify()`/`verifyMemoryDeps` — the thin-cobra +
  injectable-Deps + gated-verb precedent `villa recall` mirrors.
- `internal/usage/` — the v1.2 precedent for an XDG flat-file store with
  reset-aware fold logic (pattern for `recall-state.json` read/write + atomicity).
- `internal/memory/` — `Decide(cfg)` fail-closed enablement gate pattern (the
  recall gate mirrors it); `RenderView` resolved-values handoff.
- `internal/config/villaconfig.go` — memory fields (`MemoryEnabled`,
  `EmbeddingModel`, `EmbeddingDim`, chat/embed/qdrant addrs+ports) the recall verb
  reads; `chat_port` is the loopback REST base.
- `cmd/villa/root.go` — command registration site.
- `internal/inference/seam_test.go` — `TestSeamGrepGate` walks `internal/` +
  `cmd/villa`; `internal/recall` must stay literal-free.

### External (verify against the PINNED OWUI digest at research time)
- Open WebUI chats API (`/api/v1/chats/…` list/get, pagination, `updated_at`
  semantics) and knowledge API (file delete/replace within a collection) — confirm
  shapes on OWUI v0.9.6 (`ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…`),
  the digest live on the box.
- Open WebUI model-knowledge attachment mechanism (for D-03) and its
  PersistentConfig interaction.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- The whole Phase-20 loopback REST drive (`verify_memory.go`) — token mint,
  knowledge create/upload/add, processing poll, fixed-arg curl runners — is ~80%
  of the indexer's I/O surface, already proven on-hardware.
- `internal/usage` XDG store — atomic flat-file persistence pattern for
  `recall-state.json`.
- `internal/memory.Decide` — fail-closed enablement gate to mirror for the recall
  verbs.
- `villa verify memory` cobra/Deps wiring — the gated-verb template.

### Established Patterns
- **Pure core + injectable seam** — `internal/recall` computes; cmd-tier Deps do I/O.
- **Honesty-by-construction** — staleness is typed-Unknown when unevaluable; a
  partial run persists truth; never false-current (mirrors offload-assert).
- **Clean-replace over merge** — changed chats delete+re-add their Knowledge file
  (v1.2 BAK lesson applied to documents).
- **No shell interpolation; fixed-arg exec; loopback/container-DNS only; no new
  host port** — all REST traffic over the existing OWUI PublishPort.
- **One byte-frozen contract at a time** — `status.Report` evolution is reserved
  for Phase 23; any `recall status --json` is its own new versioned contract.

### Integration Points
- OWUI chats API (loopback :3000) → `internal/recall` diff → OWUI knowledge API →
  OWUI's own embed path → villa-embed (:8080/v1, 768-dim) → Qdrant
  (multitenant `open-webui`-prefixed collection) — all on `villa.network`.
- `recall-state.json` (XDG) ← indexed-state writes; → Phase-23 backup scope +
  skew guard (embedding model/dim recorded).
- `docs/MEMORY.md` ← recall section appended.

</code_context>

<specifics>
## Specific Ideas

- ROADMAP names the shape explicitly: "chats → Knowledge semantic indexer" — the
  Knowledge collection IS the index; don't invent a parallel store.
- "Honest staleness" is a first-class deliverable (SC#3), not a nice-to-have —
  design the state file so staleness is computable, and the status output so
  Unknown ≠ current.
- The milestone's biggest single phase (user explicitly chose FULL scope per
  STATE.md) — expect multiple plans; the verification wave needs the live box
  (real chats, real retrieval-by-meaning check).

</specifics>

<deferred>
## Deferred Ideas

- Auto-reindex on a schedule (systemd timer / watch) — new capability; backlog.
- Surfacing recall rows in `status`/dashboard (`status.Report` 2→3) — **Phase 23**.
- Backup/restore of `recall-state.json` + the Qdrant volume — **Phase 23**.
- `recommend`/`preflight`/`doctor` awareness of index size/headroom — **Phase 22**.

</deferred>

---

*Phase: 21-conversational-recall-indexer*
*Context gathered: 2026-06-10*
