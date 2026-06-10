# Phase 21: Conversational Recall Indexer - Research

**Researched:** 2026-06-10
**Domain:** Open WebUI REST integration (chats → Knowledge indexing), Go control-plane verb + pure core, XDG flat-file state
**Confidence:** HIGH — every load-bearing API claim was verified against the PINNED OWUI digest's bundled backend source (`podman exec villa-openwebui`, read-only) and/or confirmed by live read-only REST probes on the gfx1151 box (OWUI v0.9.6, `ghcr.io/open-webui/open-webui@sha256:7f1b0a1a…`)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 (access path):** Read past chats via the **OWUI REST API over the existing loopback PublishPort** using the established fixed-arg curl seam — NOT by reading `webui.db` directly (CGO/static-binary ban; unstable schema; auth bypass = privacy violation), NOT via `podman exec`. Reuse the Phase-20 primitives in `cmd/villa/verify_memory.go`: `mintAdminToken` (signin → signup fallback; service creds `villa-verify@localhost`), `runLoopbackCurl`/`runLoopbackCurlStdin` (fixed-arg, shell-free). No new host port (D-11 carried).
- **D-02 (destination):** Index INTO a **villa-managed OWUI Knowledge collection** (e.g. "Villa Recall — Past Conversations") created/maintained via the knowledge API (`knowledge/create`, `files/` multipart upload, `knowledge/{id}/file/add` — all already driven by `driveRagUploadCite`). Chat content flows through **OWUI's own chunk→embed→Qdrant path** (villa-embed, 768-dim, multitenant collection layout per Phase-20 D-01) — retrieval is native OWUI RAG **with citations**, Phase-23 Qdrant backup covers it for free. Do NOT write vectors into Qdrant directly behind OWUI's back.
- **D-03 (retrieval):** RECALL-02 is satisfied by the recall Knowledge collection being **attached to the served model / referenced in chat**. Research MUST confirm the concrete mechanism on the pinned OWUI digest and how it behaves under `ENABLE_PERSISTENT_CONFIG=False`; if attachment requires a one-time UI/API step, villa performs it during index (API) or `docs/MEMORY.md` documents it (UI) — semantic-not-keyword retrieval is the acceptance bar (SC#2). *(Resolved in this research — see "RECALL-02 mechanism" below.)*
- **D-04 (granularity):** One **role-labeled plain-text transcript per chat** (`user:`/`assistant:` turns, title + ISO date header), deterministically named from the OWUI chat id (e.g. `villa-recall-<chat-id>.txt`). OWUI's own chunker handles splitting — never hand-roll chunking. A **changed chat re-indexes by delete-then-re-add** of its file in the Knowledge collection (clean-replace — no stale-chunk leaks, no duplicate growth).
- **D-05 (state):** Villa-side index state is a **flat JSON file under `$XDG_DATA_HOME/villa/` (e.g. `recall-state.json`)** — NEVER `config.toml`, never SQLite — mapping `chat_id → {owui_updated_at, knowledge_file_id, indexed_at}` plus the knowledge collection id and embedding model/dim recorded for the Phase-23 skew guard. Incremental = diff the live chat list (id + updated_at) against the state file: new → add, changed → delete+re-add, deleted chat → remove from Knowledge.
- **D-06 (staleness):** **Honest staleness, typed-Unknown discipline:** report indexed count, last-indexed timestamp, and stale/unindexed count computed against the LIVE chat list. If OWUI is unreachable or listing fails, report **Unknown — could not evaluate** (typed-Unknown → WARN), NEVER a stale count of 0 / a false "current". A partial index run that errored mid-way must persist what actually completed and report the remainder as stale.
- **D-07 (verb):** New **`villa recall` parent verb** with `villa recall index` (incremental by default; `--rebuild` = clean-recreate the Knowledge collection then full re-index) and `villa recall status` (honest state report, human + `--json` if cheap). Mirrors `villa verify memory`: thin cobra caller + injectable `Deps` seam; **memory disabled → refuse-with-remediation** (`exitBlocked`).
- **D-08 (core):** **ONE new pure core `internal/recall`**: diff/plan computation, transcript rendering, staleness classification — unit-testable off-hardware with injected chat lists/state. Host effects (curl drives, file I/O) stay behind the cmd-tier `Deps` seam; `orchestrate` remains the only intentionally-impure module; NO image literals or backend markers in `internal/recall` (`TestSeamGrepGate` green; no allowlist edit expected).
- **D-09 (identity):** The indexer indexes **the operator's chats** on this single-operator box. Research MUST confirm the listing/read mechanism on the pinned digest and pick the **least-privilege path that reaches the operator's chats**. Strictly local; credentials never leave the box; no plaintext secrets committed to config without flagging it. *(Resolved in this research — see "Identity path" below.)*

### Claude's Discretion

- Exact verb/flag spellings, JSON state schema field names, transcript format details, polling/backoff values — planner's call within D-01..D-09.
- Whether `recall status` gets `--json` golden treatment now or stays human-only until Phase 23 surfaces recall in `status.Report` — planner's call; if `--json` ships, it is a NEW contract (own schema_version), never a mutation of `status.Report` (that single evolution is reserved for Phase 23).
- Batch size / rate limiting for large chat histories (the REST drive is local; embed throughput on gfx1151 is the real constraint).

### Deferred Ideas (OUT OF SCOPE)

- Auto-reindex on a schedule (systemd timer / watch) — new capability; backlog.
- Surfacing recall rows in `status`/dashboard (`status.Report` 2→3) — **Phase 23**.
- Backup/restore of `recall-state.json` + the Qdrant volume — **Phase 23**.
- `recommend`/`preflight`/`doctor` awareness of index size/headroom — **Phase 22**.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| RECALL-01 | A `villa`-orchestrated indexer semantically indexes past conversations into the vector store (chats → Knowledge), running locally | Confirmed full chats API on the pinned digest (admin list-by-user covering archived/pinned/folders in one endpoint; full-chat GET; canonical message-chain reconstruction). Confirmed knowledge create/upload/add path — already proven by `driveRagUploadCite`. All traffic over the existing loopback PublishPort; vectors flow through OWUI→villa-embed→Qdrant (zero new outbound). |
| RECALL-02 | The assistant retrieves relevant past-chat content *by meaning* into the current conversation's context | Confirmed the model-attachment mechanism: a Model DB row with `id == <served base model id>`, `base_model_id=null`, `meta.knowledge=[{type:"collection",id:<kb>}]` makes OWUI's middleware inject the recall KB into **every** chat completion server-side (UI **and** REST). Unaffected by `ENABLE_PERSISTENT_CONFIG=False` (DB data, not a ConfigVar). REST-verifiable. |
| RECALL-03 | Incremental/re-index is `villa`-controllable with honest state (no silent staleness) | `updated_at` (epoch seconds, indexed DB column) confirmed as the change signal on the list endpoint; `knowledge/{id}/file/remove?delete_file=true` confirmed to clean vectors (by file_id AND hash) for the delete-then-re-add path; `knowledge/{id}/reset` confirmed as the id-preserving `--rebuild` primitive. State-file schema + staleness algebra specified below; typed-Unknown discipline mapped to existing patterns. |
</phase_requirements>

## Summary

Every load-bearing unknown flagged in CONTEXT.md is now resolved against the **pinned digest itself** (the live container's bundled backend source is definitive for v0.9.6) plus targeted read-only live probes. Three headline results:

1. **Identity (D-09) is resolved:** the seeded service account `villa-verify@localhost` is **role=admin on the live box** (verified by live signin), `ENABLE_ADMIN_CHAT_ACCESS` defaults to `True` (plain env var, not PersistentConfig, unset in the live container), and `GET /api/v1/chats/list/user/{user_id}` is an admin endpoint that returns a user's **complete** chat universe (archived included by hard-coded `include_archived=True`; no folder/pinned filtering) with `{id,title,updated_at,created_at}` summaries. Meanwhile the alternative — operator API keys — is **dead on this deployment**: `ENABLE_API_KEYS` defaults to `False` AND is a PersistentConfig var, so enabling it would require a new env key and a golden re-freeze. **Recommendation: admin JWT via the existing `mintAdminToken` seam + the admin chats endpoints.** No new secret, no new env, no golden change.

2. **RECALL-02 mechanism (D-03) is resolved and it is REST-verifiable:** OWUI resolves model-attached knowledge **server-side** in `process_chat_payload` (`model.info.meta.knowledge` items are appended to the request's `files` for every non-native-FC completion) — unlike MEM-01 memory injection, which was frontend-mediated. A Model DB row whose `id` equals the served base-model id (with `base_model_id=null`) overrides the live base model and carries `meta.knowledge`. Model rows are database **data**, not PersistentConfig — `ENABLE_PERSISTENT_CONFIG=False` does not touch them. villa attaches the recall KB idempotently during `recall index` via `POST /api/v1/models/create` / `model/update`.

3. **The update path is leak-free as designed (D-04):** `POST /api/v1/knowledge/{id}/file/remove` (with default `delete_file=true`) removes the file from the KB, deletes its vectors by `file_id` **and** by content hash, drops the per-file vector collection, and deletes the file row. For `--rebuild`, use `POST /api/v1/knowledge/{id}/reset` (clears files+vectors but **keeps the KB id**, preserving the model attachment) — NOT `DELETE /{id}/delete`, which also strips the KB from every model's `meta.knowledge`.

**Primary recommendation:** Build `villa recall index|status` as a thin cobra caller over a new pure `internal/recall` core (Plan/RenderTranscript/Staleness + cloned atomic XDG store), generalizing the proven `verify_memory.go` REST primitives, using the admin chats endpoints for listing/reading, `knowledge file/remove + file/add` for clean-replace, `knowledge reset` for rebuild, and an idempotent model-knowledge attach step each run so retrieval survives `villa model swap`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Chat listing/reading, transcript upload, KB maintenance, model attachment | `cmd/villa` Deps seam (fixed-arg curl over loopback) | OWUI REST API | D-01: REST over the existing PublishPort; host effects stay in the cmd tier per the seam convention |
| Diff/plan (add/update/delete), transcript rendering, staleness classification | `internal/recall` (pure core) | — | D-08: decision logic is pure, off-hardware testable |
| Incremental state persistence (`recall-state.json`) | `internal/recall` (store clone of `internal/usage` atomic writer) | XDG data dir | D-05: flat JSON under `$XDG_DATA_HOME/villa/`, 0600/0700, atomic temp+rename |
| Chunking, embedding, vector storage, retrieval, citations | Open WebUI (container) → villa-embed → Qdrant | — | D-02: OWUI's own upload→embed→store path; never re-implement; never write Qdrant directly |
| Enablement gate (memory off → refuse) | `cmd/villa` + `internal/memory.Decide` | `internal/config` | D-07: mirrors the `villa verify memory` gated-verb precedent (but BLOCKS instead of exiting 0 — an explicit index request cannot honestly no-op) |
| Operator documentation | `docs/MEMORY.md` (extend) | — | CONTEXT canonical ref: append recall verbs + retrieval enable-path |

## Standard Stack

### Core

Zero new Go dependencies (v1.3 INTEGRATION rule: zero new first-party libraries). Everything is stdlib + existing internal packages + the OWUI REST API.

| Component | Version | Purpose | Why Standard |
|-----------|---------|---------|--------------|
| Open WebUI REST API | v0.9.6 @ `sha256:7f1b0a1a…` (pinned, live on box) | Chats list/read, Knowledge CRUD, file upload/process, model attach | The integration contract; all shapes verified against this digest's bundled source `[VERIFIED: live container source]` |
| `cmd/villa/verify_memory.go` primitives | in-repo | `mintAdminToken`, `runLoopbackCurl(Stdin)`, `pollFileProcessed`, knowledge create/upload/add sequence | ~80% of the indexer's I/O surface, proven on-hardware in Phase 20 `[VERIFIED: codebase]` |
| `internal/usage` store pattern | in-repo | Atomic XDG flat-file persistence template for `recall-state.json` (clone, don't import — established clone discipline) | v1.2 precedent: 0600/0700, traversal guard vs fixed root, temp+rename `[VERIFIED: codebase]` |
| `internal/memory.Decide` | in-repo | Fail-closed enablement gate for the recall verbs | Phase-18 gate pattern `[VERIFIED: codebase]` |
| Go stdlib (`encoding/json`, `os/exec`, `time`, `strings`) | Go 1.26.2 | JSON marshal, fixed-arg curl exec, timestamps | Existing toolchain `[VERIFIED: codebase]` |

### Supporting

| Component | Purpose | When to Use |
|-----------|---------|-------------|
| `cmd/villa/verify.go` cobra/Deps wiring | Template for `newRecall()` / `recallDeps` | Verb registration in `root.go`, injectable seams, return-not-Exit run body |
| `discoverChatModel` (`verify_memory.go:329`) | Resolve the SERVED model id (GGUF filename) for the attachment step | Every `recall index` run (attachment must track the current served model) |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Admin endpoint `GET /chats/list/user/{id}` | Per-user token + `GET /api/v1/chats/` | Self-list EXCLUDES archived always, excludes folder/pinned chats unless flags set; operator token hand-off needs `ENABLE_API_KEYS` (default False, PersistentConfig → new env + golden re-freeze) or a password in villa's hands. Strictly worse on this deployment. `[VERIFIED: live container source]` |
| villa state-file diff (D-05) | OWUI `POST /knowledge/{id}/sync/diff` (SHA-256 manifest diff, exists on this digest) | sync/diff is filename/checksum-based and designed for directory sync; it cannot carry villa's `owui_updated_at` semantics or honest-staleness reporting. D-05 locks the state-file approach; sync/diff noted for awareness only. |
| delete-then-re-add (D-04) | `POST /knowledge/{id}/file/update` or `POST /files/{id}/data/content/update` (re-embed in place) | In-place update exists, but D-04 locks clean-replace (v1.2 BAK lesson); delete path is confirmed leak-free, so no reason to deviate. |
| `knowledge/{id}/reset` for `--rebuild` | `DELETE /knowledge/{id}/delete` + re-create | Delete strips the KB id from EVERY model's `meta.knowledge` and changes the KB id → attachment must be redone and state invalidated. Reset keeps the id; strictly better. `[VERIFIED: live container source]` |

**Installation:** none — no new packages.

## Package Legitimacy Audit

No external packages are installed by this phase (v1.3 rule: zero new first-party Go libraries; integration is REST-only against already-pinned, already-running container images).

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Confirmed API Surface (pinned digest v0.9.6) — the planner plans directly from this

All routes below were read from `/app/backend/open_webui/routers/*.py` inside the live pinned container (read-only) and, where marked, exercised by live read-only REST probes. Base URL: `http://127.0.0.1:<chat_port>` (existing PublishPort, D-11). Auth: `Authorization: Bearer <JWT>` from the existing `mintAdminToken` seam.

### Identity & users (D-09)

| Endpoint | Auth | Notes |
|----------|------|-------|
| `POST /api/v1/auths/signin` `{email,password}` → `{token,…}` | — | Existing seam; JWT works for all API calls regardless of `ENABLE_API_KEYS` `[VERIFIED: live REST probe]` |
| `GET /api/v1/auths/` | Bearer | Returns session user incl. `role`. Live: `villa-verify@localhost` → **`role:"admin"`** `[VERIFIED: live REST probe]` |
| `GET /api/v1/users/?page=1` | admin | `{users:[{id,email,role,name,…}]}`. Live box: `villa-verify@local.test` (admin, the onboarded operator) + `villa-verify@localhost` (admin, service) `[VERIFIED: live REST probe]` |

- `ENABLE_ADMIN_CHAT_ACCESS`: `os.getenv('ENABLE_ADMIN_CHAT_ACCESS','True')` — plain env read (config.py:2983), **not** PersistentConfig, **unset** in the live container → effective `True`. `[VERIFIED: live container source + printenv]`
- `ENABLE_API_KEYS`: default `False` AND a `ConfigVar` (PersistentConfig) — with `ENABLE_PERSISTENT_CONFIG=False` and villa not setting it, sk-API-keys are OFF in this deployment. `[VERIFIED: live container source]`

### Chats — listing & reading (RECALL-01/03)

| Endpoint | Auth | Shape / semantics |
|----------|------|-------------------|
| `GET /api/v1/chats/list/user/{user_id}?page=N` (optional `order_by`, `direction`, `query`) | **admin** + `ENABLE_ADMIN_CHAT_ACCESS` | **The recommended list endpoint.** `include_archived=True` hard-coded; NO folder/pinned filtering → one endpoint = the user's complete chat universe. 60/page, `page` defaults to 1; default order `updated_at DESC, id`. Items: `{id, title, updated_at, created_at, last_read_at}` — **epoch SECONDS** ints `[VERIFIED: live container source + live REST probe]` |
| `GET /api/v1/chats/` (= `/list`) | any user (own chats) | Excludes archived ALWAYS; excludes folder-chats and pinned unless `include_folders=true&include_pinned=true`; no `page` → returns ALL, with `page` → 60/page. Kept for reference; NOT recommended (incomplete universe) `[VERIFIED: live container source]` |
| `GET /api/v1/chats/{id}` | owner, OR **admin** + `ENABLE_ADMIN_CHAT_ACCESS` | Full `ChatResponse`: `{id, user_id, title, chat:{…}, updated_at, created_at, archived, pinned, meta, folder_id, …}` `[VERIFIED: live container source + live REST probe]` |
| `GET /api/v1/chats/all` | own chats only | NDJSON bulk export of the session user's chats. Not usable for the admin-reads-operator path; per-chat GET is incremental-friendly anyway |

**Pagination termination:** request pages until a page returns fewer than 60 items (or empty).

**Chat JSON structure** (live-probed on the box):

```text
chat (dict): { id, title, models, files, tags, timestamp, history, messages }
chat.history: { currentId: "<msg-id>", messages: { "<msg-id>": {...}, ... } }
message: { id, parentId, childrenIds, role: "user"|"assistant", content,
           timestamp (epoch sec), model/models, [done, usage, followUps, output] }
```

- **Canonical linear transcript = walk the `parentId` chain from `history.currentId` back to the root, then reverse** — exactly what OWUI's own `get_message_list` (utils/misc.py:72) does, including a visited-set cycle guard. `[VERIFIED: live container source]`
- **Do NOT use the flat `chat.messages` list** — it is a frontend-maintained current-branch view and was stale/partial on the live probe (len 1 vs a full history). `[VERIFIED: live REST probe]`
- Assistant `content` may embed reasoning blocks: `<details type="reasoning" done="true" …>…</details>` — **strip these before rendering the transcript** (indexing chain-of-thought bloats embeddings and pollutes retrieval). `[VERIFIED: live REST probe]`

### Knowledge & files (D-02/D-04)

| Endpoint | Notes |
|----------|-------|
| `POST /api/v1/knowledge/create` `{name, description, access_grants?}` → `{id,…}` | Admin or `workspace.knowledge` permission. Default private (owner = creator). Side effect: embeds KB name/description metadata via the embedding engine → **requires villa-embed reachable at create time** `[VERIFIED: live container source]` |
| `GET /api/v1/knowledge/{id}` → `KnowledgeFilesResponse` (incl. `files[]` metadata) | Reconcile actual KB contents vs `recall-state.json` (drift detection) |
| `POST /api/v1/files/` multipart (`file=@-;filename=…`, query `process=true&process_in_background=true` defaults) → `{id}` | Existing seam (`runLoopbackCurlStdin`); poll `GET /api/v1/files/{id}/process/status` until `completed` (existing `pollFileProcessed`) |
| `POST /api/v1/knowledge/{id}/file/add` `{file_id}` | Adds + embeds into the KB collection (existing seam) |
| `POST /api/v1/knowledge/{id}/file/remove` `{file_id}` (query `delete_file=true` default) | **Confirmed leak-free:** removes from KB, deletes vectors by `file_id` AND by content `hash`, drops the `file-{id}` vector collection, deletes the file row (file-delete branch requires file owner or admin — villa's service account is admin) `[VERIFIED: live container source]` |
| `POST /api/v1/knowledge/{id}/reset` | Drops the KB's vector collection + clears its file list but **KEEPS the KB id** → the correct `--rebuild` primitive (model attachment survives) `[VERIFIED: live container source]` |
| `DELETE /api/v1/knowledge/{id}/delete` | Deletes KB + vectors AND removes the KB from every model's `meta.knowledge`. Do NOT use for rebuild `[VERIFIED: live container source]` |

### RECALL-02 mechanism (D-03 — RESOLVED)

**How a Knowledge collection reaches a NEW chat on this digest:** OWUI's chat middleware (`utils/middleware.py:2486`) reads `model.info.meta.knowledge`; when set (and `function_calling != 'native'` — villa never enables native FC), the knowledge items are appended to the request's `files` **server-side, on every completion** — UI chats AND bare `POST /api/chat/completions`. Retrieval then runs the same citation-bearing path proven in Phase 20 (`sources` field, A6). `[VERIFIED: live container source]`

**How villa attaches it to the served model:** a Model DB row with `id == <served base model id>` and `base_model_id = null` overrides the live base model — `utils/models.py:144-150` merges `model['info'] = custom_model.model_dump()` onto the model served by the OpenAI connection (villa-llama's GGUF-filename id, discovered via the existing `discoverChatModel`). `ModelMeta` is `extra='allow'`, so `knowledge` rides as an extra field. `[VERIFIED: live container source]`

```jsonc
// POST /api/v1/models/create   (or POST /api/v1/models/model/update if the row exists)
{
  "id": "<served-model-id from GET /api/models>",   // e.g. the GGUF filename
  "base_model_id": null,
  "name": "<same served-model id or friendly name>",
  "meta": {
    "knowledge": [ { "type": "collection", "id": "<recall-kb-id>", "name": "Villa Recall — Past Conversations" } ]
  },
  "params": {},
  "is_active": true
}
```

- The `{type:"collection", id:…}` item shape is the SAME shape proven on-hardware by `driveRagUploadCite`'s per-chat `files` param; middleware passes non-legacy items through verbatim into `files`. `[VERIFIED: live container source]`
- `create` 401s with MODEL_ID_TAKEN if the row exists; `model/update` 401s NOT_FOUND if it doesn't → the idempotent attach step is: `GET /api/v1/models/model?id=…` → exists ? update (merge `knowledge` into existing meta, preserving other fields) : create. Admin role suffices for both.
- **PersistentConfig interaction: NONE.** Model rows, knowledge collections, files, and chats are database DATA — not `ConfigVar`s. `ENABLE_PERSISTENT_CONFIG=False` does not affect them, and no env key changes → **no golden re-freeze in this phase.** `[VERIFIED: live container source]`
- **Retrieval-time access check** (`retrieval/utils.py:~1317`): a collection item only retrieves if the requesting user is **admin OR the KB owner OR holds a read access-grant** — otherwise it is *silently skipped*. Both live accounts are admin, so this is green today; see Pitfall 4 for the future-non-admin caveat.

### Identity path — concrete recommendation (D-09 resolved)

**Use the admin JWT path with the existing service-account creds.** Rationale chain:
1. `villa-verify@localhost` is **already admin** on the live box `[VERIFIED: live REST probe]` — and on a fresh box `mintAdminToken`'s signup-first-user fallback creates it as admin. No new credential, no new secret in config.
2. `ENABLE_ADMIN_CHAT_ACCESS` default `True` (plain env, unaffected by PersistentConfig) gates both the list-by-user and the cross-user chat GET.
3. The operator-token alternative is strictly worse on this deployment: sk-API-keys are disabled (`ENABLE_API_KEYS=False`, PersistentConfig — enabling = new env key + golden re-freeze), and a password-based per-user signin would put the operator's password in villa's hands.
4. Least-privilege framing: villa already mints this exact admin token for `villa verify memory` (Phase 20, SECURITY 16/16 closed) — recall adds READ access to chats under the SAME posture on a strictly-local single-operator box. Document the widened read scope in `docs/MEMORY.md`.

**Whose chats:** enumerate `GET /api/v1/users/?page=…`; **default = index every user's chats except the `villa-verify@localhost` service account itself** (the operator may have any account name; the service account is the only identity villa can deterministically exclude; on this single-operator box "all human users" == the operator). The state file records `user_id` per chat. An optional `--user <email>` narrowing flag is planner's discretion.

## Recommended `recall-state.json` schema (D-05)

```jsonc
{
  "schema_version": 1,                       // own contract; independent of status.Report
  "knowledge_id": "<owui-kb-uuid>",
  "knowledge_name": "Villa Recall — Past Conversations",
  "embedding_model": "nomic-embed-text-v1.5", // Phase-23 skew guard (from config at index time)
  "embedding_dim": 768,                       // Phase-23 skew guard
  "last_index_started_at": "2026-06-10T12:00:00Z",   // RFC3339 UTC
  "last_index_completed_at": "2026-06-10T12:03:21Z", // zero/absent ⇒ last run incomplete
  "chats": {
    "<chat_id>": {
      "user_id": "<owui-user-uuid>",
      "owui_updated_at": 1781041047,          // epoch seconds, as returned by the list API
      "file_id": "<owui-file-uuid>",          // the transcript file currently in the KB
      "indexed_at": "2026-06-10T12:01:07Z"
    }
  }
}
```

- **Counts/ids/timestamps only — no titles, no content** (mirrors `internal/usage`'s counts-only-by-construction discipline; `recall status` doesn't need titles for honest counts, and titles are chat content leaking into a host file). Planner may add titles for UX but must then treat the file as content-bearing in SECURITY.md.
- Persistence: clone `internal/usage`'s store shape — own `schema_version`, fail-closed `Load` (absent/corrupt/unknown-schema ⇒ empty typed-Unknown state, never a fabricated index), atomic `WriteFileAtomic` (0600/0700, traversal guard vs the fixed `$XDG_DATA_HOME/villa` root), injected byte-I/O `Deps`.
- **Partial-run honesty (D-06):** write the state file after each chat completes (or small batches) AND on the error-exit path — `last_index_completed_at` is only stamped on a clean full pass, so `recall status` can distinguish "complete as of T" from "partial run, remainder stale".

**Staleness algebra** (pure, in `internal/recall`):
- inputs: live list `L` = `{(chat_id, updated_at)}` per indexed user (or typed-Unknown if any list call failed), state `S`.
- `indexed = |S.chats|`; `new = {id ∈ L : id ∉ S}`; `changed = {id : L.updated_at > S.owui_updated_at}`; `deleted = {id ∈ S : id ∉ L}`; `stale = |new| + |changed| + |deleted|`.
- `L` Unknown ⇒ stale = **Unknown — could not evaluate** (WARN), `indexed`/`last_indexed` still reported from the state file (they're villa-side truths).
- Attachment state is part of honest status: `GET /api/v1/models/model?id=<served>` → recall KB present in `meta.knowledge`? → `attached | missing | unknown` (unknown when OWUI/model discovery fails).

## Architecture Patterns

### System Architecture Diagram

```text
                       villa recall index / status
                                  │
                 ┌────────────────┴───────────────────┐
                 │  cmd/villa/recall.go (thin cobra)   │
                 │  gate: memory.Decide → refuse/block │
                 └────────────────┬───────────────────┘
                                  │ recallDeps (injectable func fields)
        ┌──────────────┬──────────┴─────────────┬──────────────────┐
        ▼              ▼                        ▼                  ▼
  listUsers/      getChat(id)            uploadTranscript /   readState /
  listChats       (full body)            removeFile / reset   writeState (atomic)
        │              │                        │                  │
        └── fixed-arg curl over 127.0.0.1:<chat_port> ──┘   $XDG_DATA_HOME/villa/
                       │   (existing PublishPort, D-11)       recall-state.json
                       ▼
              ┌──────────────────┐    chunk→embed→store    ┌────────────┐
              │   Open WebUI     │ ───────────────────────▶│ villa-embed │ 768-dim
              │ (pinned v0.9.6)  │                          └────────────┘
              │ chats DB · files │ ──────── vectors ──────▶ ┌────────────┐
              │ knowledge · model│   open-webui_knowledge   │   Qdrant   │
              │  meta.knowledge ─┼── injected into every    │(multitenant)│
              └──────────────────┘   chat completion (RAG)  └────────────┘
                       ▲
        new chat (UI or REST) — retrieval by meaning + `sources` citations

  PURE CORE internal/recall (no I/O):
    Plan(liveChats, state)        → {adds, updates, deletes}
    RenderTranscript(chat)        → role-labeled text (currentId parent-chain walk)
    Staleness(live|Unknown, state)→ typed report (indexed / last / stale / Unknown)
    Load/Save state               → fail-closed, own schema_version
```

### Recommended layout

```text
internal/recall/
├── recall.go        # Plan diff core + types (ChatRef{ID, UserID, UpdatedAt}, PlanResult)
├── transcript.go    # RenderTranscript: parent-chain walk, reasoning-block strip, header
├── staleness.go     # Staleness classification (typed-Unknown), attachment state fold
├── store.go         # recall-state.json load/save (clone of usage.go atomic discipline)
└── *_test.go
cmd/villa/
├── recall.go        # newRecall()/newRecallIndex()/newRecallStatus(), recallDeps, run* bodies
└── recall_live.go   # (or in recall.go) liveRecallDeps: curl drives generalized from verify_memory.go
```

### Pattern 1: Generalize, don't duplicate, the Phase-20 REST drive
**What:** Lift `mintAdminToken`, `runLoopbackCurl(Stdin)`, `pollFileProcessed`, `discoverChatModel` into shared cmd-tier helpers used by both `verify memory` and `recall` (they're already package `main`; a small refactor of call sites, no behavior change).
**When:** First plan, before new endpoints are added.

### Pattern 2: Idempotent attach-on-index
**What:** Every `recall index` run ends by asserting the attachment: discover the served model id (`GET /api/models`), `GET /api/v1/models/model?id=…`, create-or-update the row so `meta.knowledge` contains the recall KB (merge — never clobber other meta keys an operator may have set).
**Why:** the Model row is keyed by the served model id (GGUF filename) — after `villa model swap` the NEW id has no row and recall silently detaches. Re-asserting each run + reporting `attached/missing/unknown` in `recall status` keeps SC#2 honest across swaps.

### Pattern 3: Transcript rendering mirrors `get_message_list`
**What (per D-04):**
```text
# <title>
# <ISO-8601 date of created_at> — Open WebUI chat <chat-id>

user: <content>
assistant: <content with <details type="reasoning"…>…</details> stripped>
...
```
Walk `history.messages` from `history.currentId` via `parentId` (visited-set cycle guard, exactly like OWUI's `get_message_list`), reverse to chronological order. Skip non-user/assistant roles. A chat with no reconstructable messages is SKIPPED (recorded as skipped in the run summary, not silently dropped).

### Pattern 4: Gated verb that BLOCKS when memory is off
**What:** Unlike `verify memory` (memory off ⇒ exit 0 "nothing to verify"), `recall index`/`status` with memory off must `exitBlocked` with remediation ("enable memory_enabled and run villa install first") — D-07: an explicit user request to index cannot honestly no-op.

### Anti-Patterns to Avoid
- **Writing vectors into Qdrant directly** — invisible to OWUI, breaks citations, violates the multitenant layout OWUI owns (all KBs live in ONE physical collection `open-webui_knowledge` with `tenant_id` payloads — verified in `qdrant_multitenancy.py`). D-02 hard ban.
- **Using `chat.messages` (flat list) for the transcript** — stale frontend-branch view; use the `history.currentId` chain.
- **`DELETE /knowledge/{id}/delete` for `--rebuild`** — strips the KB from all models' `meta.knowledge` and changes the id; use `reset`.
- **Clobbering the Model row's meta on attach** — read-merge-write; the operator may have UI-set description/capabilities on the served model.
- **Reporting stale=0 when OWUI is unreachable** — typed-Unknown WARN, always (D-06).
- **Parallel uploads to "speed up" bulk index** — serializes anyway at villa-embed (`RAG_EMBEDDING_BATCH_SIZE` default 1) and contends with the chat model on the shared gfx1151 envelope; index sequentially.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Chunking/embedding/vector storage | Any Go-side splitter/embedder | OWUI `files/` upload → `knowledge/file/add` pipeline | D-02; OWUI owns chunk size, prefixes (`search_document:`), multitenant layout |
| Vector cleanup on update/delete | Direct Qdrant deletes | `knowledge/{id}/file/remove?delete_file=true` | Confirmed to delete by file_id AND hash + drop the `file-` collection `[VERIFIED]` |
| Full rebuild | KB delete + recreate | `knowledge/{id}/reset` | Preserves KB id → preserves model attachment |
| Retrieval into new chats | Prompt stuffing / custom injector | Model `meta.knowledge` attachment | Server-side, citation-bearing, REST-verifiable `[VERIFIED]` |
| Linear thread reconstruction | Ad-hoc ordering of the messages map | Mirror `get_message_list` (currentId → parentId chain + cycle guard) | It's OWUI's own canonical semantics for "the conversation" |
| Atomic state persistence | New file-writing code from scratch | Clone `internal/usage` WriteFileAtomic/Load/Save shape | Proven 0600/0700/traversal-guard/temp+rename discipline |

**Key insight:** this phase is REST choreography + a pure diff — every "hard" sub-problem (embedding, retrieval, vector hygiene, thread semantics) already has an owner; villa's only novel logic is the plan/staleness algebra and the transcript renderer, both pure.

## Common Pitfalls

### Pitfall 1: The self-list endpoint silently misses chats
**What goes wrong:** `GET /api/v1/chats/` excludes archived chats ALWAYS and excludes folder-organized + pinned chats by default — an indexer built on it under-indexes while reporting "current".
**How to avoid:** use `GET /api/v1/chats/list/user/{user_id}` (admin), which hard-codes `include_archived=True` and applies no folder/pinned filters. `[VERIFIED: live container source]`
**Warning signs:** indexed count < chats visible in the UI sidebar + archived view.

### Pitfall 2: `villa model swap` silently detaches recall
**What goes wrong:** the attachment Model row is keyed by the served model id (the GGUF filename). A model swap serves a new id with no row → `meta.knowledge` no longer applies → RECALL-02 silently stops working while the index looks green.
**How to avoid:** idempotent attach-on-index (Pattern 2) + `recall status` reports attachment state (`attached/missing/unknown`) as part of honest staleness. Document in `docs/MEMORY.md`: "after a model swap, run `villa recall index` (or `status`) to re-assert retrieval."
**Warning signs:** `recall status` green but a new chat shows no `knowledge_search`/`sources`.

### Pitfall 3: Stale Model rows from PREVIOUS served models accumulate
**What goes wrong:** after several swaps, old ids keep their rows; harmless for retrieval but `models/create` returns MODEL_ID_TAKEN if villa re-swaps back to an old id and tries create instead of update.
**How to avoid:** the attach step is GET → update-or-create (never blind create). Cleanup of old rows is NOT required (out of scope; harmless).

### Pitfall 4: Retrieval access-check silently skips the KB for non-admin users
**What goes wrong:** at retrieval time the requesting user must be admin, KB owner, or hold a read grant — otherwise the collection item is skipped with NO error. Both live accounts are admin today, so this is invisible until a non-admin operator account appears.
**How to avoid:** note in `docs/MEMORY.md`; optionally (planner discretion) create the KB with a public-read access grant. Not a blocker for this box. `[VERIFIED: live container source]`

### Pitfall 5: Reasoning blocks and huge transcripts bloat the index
**What goes wrong:** assistant messages on this box embed `<details type="reasoning">…</details>` thought blocks (live-probed); indexing them wastes embed throughput and degrades retrieval relevance. Very long chats also stress the 60s `pollFileProcessed` timeout (chunked at CHUNK_SIZE=1000 chars, embedded ONE chunk per request — `RAG_EMBEDDING_BATCH_SIZE` default 1 applies since villa doesn't set it and PersistentConfig is off).
**How to avoid:** strip reasoning `<details>` blocks in `RenderTranscript`; make the per-file processing timeout generous and/or size-aware (e.g. base 60s + per-KiB allowance — planner's call); index sequentially.
**Warning signs:** `file processing: timed out` on long chats; retrieval returning reasoning fragments.

### Pitfall 6: `updated_at` bumps on non-content changes
**What goes wrong:** rename/pin/move/tag operations bump `updated_at` → spurious re-index of unchanged content.
**Why it's accepted:** delete+re-add is idempotent and cheap at this scale; a content-hash optimization is unnecessary complexity. Document as known behavior (re-index is correct, just occasionally redundant).

### Pitfall 7: `knowledge/create` needs villa-embed up
**What goes wrong:** KB creation embeds the KB's name/description metadata via the embedding engine — with villa-embed down, first-run index fails confusingly at the create step.
**How to avoid:** the index run's first action after the gate should be a cheap reachability sequence (OWUI `/health` over loopback; failures → refuse-with-remediation naming the service), mirroring the existing proof seams' error details.

### Pitfall 8: Partial-run dishonesty
**What goes wrong:** an index run that dies at chat 40/100 but only writes state at the end loses 39 chats' worth of truth — next `status` over-reports staleness (acceptable) or, worse, a final-write-only success path could claim completeness it didn't earn.
**How to avoid:** D-06 discipline — persist state incrementally (after each chat), stamp `last_index_completed_at` ONLY on a clean full pass; `status` distinguishes complete vs partial runs.

## Code Examples

### Listing the operator's chats (admin path)

```bash
# Source: /app/backend/open_webui/routers/chats.py:515 (pinned digest, read on-box)
curl -sf -H "Authorization: Bearer $TOK" \
  "http://127.0.0.1:3000/api/v1/chats/list/user/$USER_ID?page=1"
# → [ {"id":"146356a2-…","title":"…","updated_at":1781041047,"created_at":1781040998,"last_read_at":null}, … ]
# 60/page; loop pages until short page. updated_at/created_at are epoch SECONDS.
```

### Reading one chat and reconstructing the thread (pure-core input)

```bash
# Source: routers/chats.py:935 + utils/misc.py:72 (get_message_list)
curl -sf -H "Authorization: Bearer $TOK" "http://127.0.0.1:3000/api/v1/chats/$CHAT_ID"
# → {"id":…, "user_id":…, "title":…, "updated_at":…, "archived":…, "chat":{
#      "history":{"currentId":"<mid>","messages":{"<mid>":{"id","parentId","childrenIds",
#                  "role","content","timestamp",…}, …}}, "messages":[…stale branch view…]}}
```

```go
// internal/recall/transcript.go — mirror get_message_list: currentId → parentId chain
func linearThread(messages map[string]chatMsg, currentID string) []chatMsg {
    var out []chatMsg
    seen := map[string]bool{}
    for id := currentID; id != "" && !seen[id]; {
        m, ok := messages[id]
        if !ok {
            break
        }
        seen[id] = true
        out = append(out, m)
        id = m.ParentID
    }
    // reverse → chronological
    for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
        out[i], out[j] = out[j], out[i]
    }
    return out
}
```

### Clean-replace of a changed chat (D-04)

```bash
# Source: routers/knowledge.py:865 (file/remove — vectors cleaned by file_id AND hash)
curl -sf -X POST "http://127.0.0.1:3000/api/v1/knowledge/$KB_ID/file/remove?delete_file=true" \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOK" \
  -d "{\"file_id\":\"$OLD_FILE_ID\"}"
# then re-add: POST /api/v1/files/ (multipart, stdin) → poll /process/status → POST …/file/add
```

### Rebuild (id-preserving)

```bash
# Source: routers/knowledge.py:1024 (reset keeps the KB id → model attachment survives)
curl -sf -X POST "http://127.0.0.1:3000/api/v1/knowledge/$KB_ID/reset" \
  -H "Authorization: Bearer $TOK"
```

### Attaching the recall KB to the served model (RECALL-02)

```bash
# Source: routers/models.py:221/639 + utils/models.py:144 + utils/middleware.py:2486
SERVED=$(curl -sf -H "Authorization: Bearer $TOK" http://127.0.0.1:3000/api/models \
         | jq -r '.data[0].id')   # villa: existing discoverChatModel
curl -sf -X POST http://127.0.0.1:3000/api/v1/models/create \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOK" -d @- <<EOF
{"id":"$SERVED","base_model_id":null,"name":"$SERVED","params":{},"is_active":true,
 "meta":{"knowledge":[{"type":"collection","id":"$KB_ID","name":"Villa Recall — Past Conversations"}]}}
EOF
# If MODEL_ID_TAKEN: GET /api/v1/models/model?id=$SERVED, merge knowledge into meta,
# POST /api/v1/models/model/update with the merged form.
```

### RECALL-02 verification request (no per-chat files param — the attachment must do the work)

```bash
# A NEW chat: plain completion, NO "files" key. If meta.knowledge is wired, OWUI injects
# the recall KB server-side and the response carries top-level "sources" (A6).
curl -sf -X POST http://127.0.0.1:3000/api/chat/completions \
  -H "Content-Type: application/json" -H "Authorization: Bearer $TOK" \
  -d "{\"model\":\"$SERVED\",\"stream\":false,
       \"messages\":[{\"role\":\"user\",\"content\":\"<semantic paraphrase of a planted past-chat topic>\"}]}"
```

## State of the Art

| Old Approach (rejected) | Current Approach (this phase) | Why |
|--------------|------------------|--------|
| Read `webui.db` SQLite directly | OWUI REST over loopback | D-01: CGO ban, unstable schema, auth posture |
| Per-chat `#` reference / user does manual attach | Model `meta.knowledge` attachment, villa-asserted | Server-side, zero per-chat friction, REST-verifiable; `#` reference remains available to users as a bonus, not the mechanism |
| OWUI native `type:"chat"` context item (exists on this digest: attach a chat directly to a request) | Knowledge collection index | The chat-item is per-request, not semantic search over ALL history; noted as interesting but it does not satisfy "retrieve by meaning across all past chats" |
| sk-API-key hand-off for identity | Admin JWT via existing seam | `ENABLE_API_KEYS=False` + PersistentConfig on this deployment |

**Deprecated/outdated:** the older `meta.knowledge` legacy shapes (`collection_name`/`collection_names`) are still handled by the middleware but should not be emitted — use the modern `{type:"collection", id}` item.

## Runtime State Inventory

> Not a rename/refactor phase, but the phase CREATES new runtime state; inventory for Phase-23 awareness:

| Category | Items | Action Required |
|----------|-------|------------------|
| Stored data | NEW: recall KB (OWUI DB row + vectors in `open-webui_knowledge` under its tenant_id); NEW: one OWUI file row + `file-…` artifacts per indexed chat; NEW: Model row keyed by served model id | Created/maintained by `recall index`; covered by the Phase-23 Qdrant-volume backup for vectors; OWUI DB rows live in the existing openwebui volume |
| Live service config | None — no OWUI env changes (attachment is DB data, not config) → **no golden re-freeze** | none |
| OS-registered state | None | none |
| Secrets/env vars | None new — reuses the existing fixed service-account creds posture (Phase 20 accepted) | Flag widened read scope (chats) in SECURITY review |
| Build artifacts | None | none |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Embed throughput on gfx1151 is sufficient for a sequential bulk index of a realistic chat history within minutes (768-dim nomic Q8_0; chunks embedded 1/request at CHUNK_SIZE=1000 chars) `[ASSUMED — Phase-19 measured single-request latency only]` | Pitfall 5 | Bulk index is slow, not wrong; mitigate with generous timeouts + progress output; measured in the on-hardware wave |
| A2 | The 60s `pollFileProcessed` timeout needs raising/size-awareness for long transcripts `[ASSUMED]` | Pitfall 5 | Spurious FAILs on big chats; tune on-hardware |
| A3 | Merging `meta.knowledge` into an existing Model row via `model/update` preserves UI-set fields when the full prior meta is echoed back (read-merge-write) `[ASSUMED from ModelForm shape — extra='allow' echoes extras]` | Pattern 2 | Operator-set model description/capabilities lost; verify in the on-hardware wave by diffing the row before/after |
| A4 | `villa verify memory`'s `/api/chat/completions` drive does not persist chat rows (so verification doesn't pollute the index) `[ASSUMED — completions endpoint writes no Chat row in the inspected code path; not live-proven]` | Identity path | Harmless noise chats get indexed; exclude service-account users by default anyway |

## Open Questions

1. **Should `recall status --json` ship now?**
   - What we know: it's cheap (the staleness report is already a typed struct); it must be its OWN schema-versioned contract (CONTEXT discretion note).
   - Recommendation: ship human-only OR `--json` with `schema_version:1` and a golden — planner's call; do NOT touch `status.Report`.
2. **Per-chat transcript size cap?**
   - What we know: no OWUI-side max (FILE_MAX_SIZE unset); huge chats still chunk fine but embed slowly.
   - Recommendation: no cap in v1; report per-chat timing in the index run output so the operator sees outliers.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Live OWUI @ pinned digest | all REST drives | ✓ (probed) | v0.9.6 `sha256:7f1b0a1a…` | — (refuse-with-remediation when down) |
| villa-embed / villa-qdrant / villa-llama | embed path, served-model discovery | ✓ (podman ps) | pinned digests | — |
| Host `curl` | fixed-arg REST seam | ✓ (probed live) | — | — |
| rootless podman + systemd --user | stack lifecycle | ✓ | 5.8.2 | — |
| Go toolchain | build/tests | ✓ (repo builds routinely) | 1.26.2 | — |
| Admin-capable token | chats list/read, model attach | ✓ — `villa-verify@localhost` is **role=admin** (live-verified) | — | signup-first-user fallback on a fresh box (existing seam) |

**Missing dependencies with no fallback:** none.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven; fake `*Deps`; golden via `-update` flag where applicable) |
| Config file | none needed (`Makefile` targets exist) |
| Quick run command | `go test ./internal/recall/... ./cmd/villa/ -count=1` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| RECALL-01 | Plan diff (new/changed/deleted), transcript render (chain walk, reasoning strip, header), index run drives add/update/remove via injected Deps in order | unit | `go test ./internal/recall/ -run 'TestPlan|TestRenderTranscript' -count=1` + `go test ./cmd/villa/ -run TestRunRecallIndex -count=1` | ❌ Wave 0 (new files) |
| RECALL-01 | Memory-off gate refuses with remediation (exitBlocked) | unit | `go test ./cmd/villa/ -run TestRecallGate -count=1` | ❌ Wave 0 |
| RECALL-01 | Seam gate stays green (no image/backend literals in `internal/recall`) | existing gate | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ exists |
| RECALL-02 | Attach step is idempotent create-or-update, read-merge-write of meta | unit (fake Deps) | `go test ./cmd/villa/ -run TestRecallAttach -count=1` | ❌ Wave 0 |
| RECALL-02 | New chat retrieves planted past-chat content BY MEANING with `sources` citation, NO per-chat files param | on-hardware (manual wave; semantic paraphrase ≠ keywords) | scripted curl sequence in the verification plan | ❌ on-hw wave |
| RECALL-03 | Staleness algebra: counts, typed-Unknown on unreachable OWUI, partial-run truth, deleted-chat handling | unit | `go test ./internal/recall/ -run TestStaleness -count=1` | ❌ Wave 0 |
| RECALL-03 | State store: fail-closed load (absent/corrupt/future-schema ⇒ empty), atomic write, 0600 | unit | `go test ./internal/recall/ -run TestStore -count=1` | ❌ Wave 0 |
| RECALL-03 | Live incremental: edit chat → re-run → file replaced; delete chat → re-run → removed; `recall status` honest before/after; OWUI stopped → Unknown WARN | on-hardware | verification-plan checklist | ❌ on-hw wave |

### Sampling Rate
- **Per task commit:** `go test ./internal/recall/... ./cmd/villa/ -count=1`
- **Per wave merge:** `make check`
- **Phase gate:** `make check` green + on-hardware verification wave (live box, real chats) before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/recall/recall_test.go` — Plan diff (RECALL-01/03)
- [ ] `internal/recall/transcript_test.go` — chain walk, reasoning strip, skip-empty (RECALL-01)
- [ ] `internal/recall/staleness_test.go` — typed-Unknown, partial run (RECALL-03)
- [ ] `internal/recall/store_test.go` — fail-closed load, atomic write (RECALL-03)
- [ ] `cmd/villa/recall_test.go` — gate, run bodies with fake Deps, attach idempotency (RECALL-01/02)
- Framework install: none (stdlib testing)

## Security Domain

> `security_enforcement: true`, ASVS L1.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Existing `mintAdminToken` JWT seam; token held in memory only, never persisted; fixed service creds = the Phase-20 accepted posture (SECURITY 16/16) — **the NEW delta is widened READ scope (operator chats); record it as an explicit accepted risk on the single-operator box** |
| V3 Session Management | yes | Fresh JWT per run; no token caching to disk |
| V4 Access Control | yes | Admin endpoints gated by `ENABLE_ADMIN_CHAT_ACCESS` (default True, env-pinned semantics); retrieval-time KB access enforced by OWUI (admin/owner/grant) — document the non-admin-user silent-skip |
| V5 Input Validation | yes | All curl args fixed (no shell interpolation, T-20-09 carried); JSON bodies via `encoding/json` marshal only; state-file load fails CLOSED on corrupt/unknown schema; chat ids/file ids are path segments composed from API-returned values only |
| V6 Cryptography | no | none needed (loopback-only, no new secrets) |
| V8 Data Protection | yes | `recall-state.json` 0600/dir 0700 under XDG data root with traversal guard; recommended counts/ids-only schema (no chat content/titles in the state file); transcripts transit loopback only, stored only inside OWUI's existing volumes |
| V10 Malicious Code / SSRF | yes | All URLs composed from config-resolved loopback base; no user-supplied URLs |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Shell injection via chat titles/content in curl args | Tampering | Content travels via `-d @`/stdin marshaled JSON and fixed args; never interpolated into argv strings beyond fixed URL+id composition |
| State-file tampering → fabricated "current" index | Tampering/Repudiation | Fail-closed load; staleness recomputed against the LIVE list every status call — state alone can never claim currency |
| Privacy: chat content leaving the box | Information Disclosure | Loopback-only REST; vectors in local Qdrant; no new outbound (zero-outbound already runtime-proven in Phase 20); no new host port |
| Privacy: chat content in villa-side files | Information Disclosure | State file = ids/timestamps only (recommended); transcripts streamed via stdin to curl (no temp files), mirroring `runLoopbackCurlStdin` |
| Silent retrieval failure presented as green | Repudiation | `recall status` reports attachment state + typed-Unknown; on-hw wave asserts a REAL semantic retrieval with citation |

## Sources

### Primary (HIGH confidence)
- Live pinned OWUI container source (read-only `podman exec`, digest `sha256:7f1b0a1a…`, v0.9.6): `routers/chats.py`, `routers/knowledge.py`, `routers/files.py`, `routers/models.py`, `models/chats.py`, `models/models.py`, `models/knowledge.py`, `utils/middleware.py`, `utils/models.py`, `utils/misc.py`, `retrieval/utils.py`, `retrieval/vector/dbs/qdrant_multitenancy.py`, `config.py` — definitive for the deployed version
- Live read-only REST probes on the box (signin/role, users list, admin chat list shape, full chat JSON structure)
- Repo: `cmd/villa/verify_memory.go`, `cmd/villa/verify.go`, `internal/usage/usage.go`, `internal/memory/memory.go`, `internal/config/villaconfig.go`
- `.planning/phases/20-*/20-03-SUMMARY.md` (A5 token mint, A6 `sources`, service accounts), `18-DECISIONS.md` (D-08 model/dim/prefixes, D-09 env contract)

### Secondary (MEDIUM confidence)
- none needed — all claims grounded in primary sources

### Tertiary (LOW confidence)
- Assumptions A1–A4 (throughput, timeout sizing, meta-merge echo, completions-no-chat-row) — flagged for the on-hardware wave

## Metadata

**Confidence breakdown:**
- API surface & shapes: HIGH — read from the pinned digest's own source + live probes
- Identity path (D-09): HIGH — service-account admin role live-verified; flag defaults read from deployed config.py
- RECALL-02 mechanism (D-03): HIGH — middleware + model-merge code paths read end-to-end; REST-verifiable
- Throughput/timeout tuning: MEDIUM — estimates pending the on-hardware wave (A1/A2)

**Research date:** 2026-06-10
**Valid until:** indefinitely for the PINNED digest (the source inspected IS the deployed code); re-verify only if the OWUI digest is ever re-pinned
