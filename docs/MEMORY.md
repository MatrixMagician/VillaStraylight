<!-- generated-by: gsd-doc-writer -->
# Memory & Knowledge (v1.3)

VillaStraylight wires Open WebUI's **native Memory** and **RAG (document knowledge)**
to a strictly-local vector stack: `villa-qdrant` (the vector store) and `villa-embed`
(a dedicated llama-server embedding the `nomic-embed-text-v1.5` model, 768-dim). Memory
is **off by default** and opt-in per install — when you enable it, `villa` regenerates
the Open WebUI container unit with the memory/RAG environment block (see
[Offline-lockdown environment](#offline-lockdown-environment)) and reconciles the stack.

Everything in this document runs **on the box** — the assistant remembers facts and
answers from your uploaded documents with **zero data leaving the host**. The runtime
proof of that claim is [`villa verify memory`](#proving-zero-outbound-with-villa-verify-memory).

> **Enabling memory.** Set `memory_enabled=true` (via `villa recommend --save`, the
> install wizard, or by editing `config.toml`) and run `villa install`. Memory off is
> byte-identical to a v1.2 install; nothing changes until you opt in.

---

## What the memory stack gives you

| Capability | Backed by | Requirement |
|------------|-----------|-------------|
| Remember a user-stated fact across separate chats | OWUI native Memory (`ENABLE_MEMORIES=True`) | MEM-01 |
| Explicitly save a message / fact to Memory | OWUI native Memory | MEM-02 |
| Automatic LLM-assisted memory extraction (opt-in) | Native Function Calling / community filter | MEM-03 |
| View, edit, delete stored memories; a deleted memory stops being injected | OWUI native Memory | MEM-04 |
| Upload a document and get a **cited** answer from it | OWUI native RAG → `villa-embed` + Qdrant | KB-01/02/03 |
| Runtime proof that retrieval reaches **no external host** | `villa verify memory` | PRIV-05 |

`villa` integrates these OSS features — it does not rebuild them. The control plane only
wires the environment and proves the privacy boundary.

---

## Personalized memory: save, recall, view/edit/delete

Open the chat UI at `http://127.0.0.1:3000`.

### Save a fact (MEM-02)

Memory is **manual** by default. You add a memory in one of two ways:

- **Per-message:** tell the assistant a fact, then use the message action to save it
  to Memory.
- **Directly:** *Settings → Personalization → Memory* → add a memory.

### Recall across chats (MEM-01)

Open a **new** chat and ask something that depends on the saved fact. The stored memory
is injected into the conversation context, so the assistant answers using it without you
restating it. Memory injection is cross-chat by construction — it is not scoped to a
single conversation.

### View, edit, delete (MEM-04)

*Settings → Personalization → Memory* lists every stored memory. You can:

- **View** the full set of memories.
- **Edit** a memory — the edited text is what gets injected from then on.
- **Delete** a memory. **A deleted memory stops being injected** — open a fresh chat
  after deleting and confirm the assistant no longer uses it. (This is the SC#1 honesty
  property: deletion is real, not cosmetic.)

---

## Enabling automatic memory extraction

**Automatic LLM-assisted memory extraction is DEFAULT-OFF**, and `villa` keeps it off
deliberately.

**Why default-off (D-07).** Automatic extraction asks the model to decide, on every turn,
what is worth remembering and to call memory tools to store it. On a **small local model**
that proactive function-calling is **unreliable** — it produces noisy, missed, or wrong
memories. So `villa` ships with only the **manual** memory store enabled
(`ENABLE_MEMORIES=True`) and leaves the automatic path for you to turn on consciously.

**There is no environment toggle for auto-extraction.** Open WebUI's native automatic
memory is not a single env var — it is driven by one of two opt-in mechanisms, and the
"default-off" guarantee is enforced simply by `villa` **not enabling either of them**:

1. **Native Function Calling (Agentic Mode) — the built-in native path.** Enable
   Native Function Calling for a model in *Workspace → Models → Edit*. With it on, the
   model is given memory tools (`add_memory`, `search_memories`, `replace_memory_content`,
   `delete_memory`, `list_memories`) and calls them proactively. This is the modern native
   auto-memory. It is **off unless you enable it per-model.**
2. **A community Filter Function (e.g. Adaptive Memory / Auto Memory).** Installed from the
   UI function library; it LLM-extracts and stores memories each turn. These are
   community-maintained, not a `villa` dependency, and are **off unless you install and
   enable one.**

> **Caveat — Native Function Calling changes RAG injection (keep it off for citations).**
> When Native Function Calling is **on**, attached knowledge is **no longer auto-injected**;
> the model has to call retrieval tools itself, and the deterministic, citation-bearing RAG
> path breaks. For reliable document answers **with citations** (see below), keep Native
> Function Calling **off**. If you turn on auto-extraction, do it knowing it can change how
> (and whether) knowledge documents are cited.

`ENABLE_PERSISTENT_CONFIG=False` does not need to (and cannot) force auto-extraction off —
the default state is already off, and no env exists to pin it. Enabling auto-extraction is
always an explicit, reversible user action.

---

## Uploading documents (knowledge) and getting cited answers

Document upload → chunk → embed → retrieve runs **entirely through the local stack**:
chunks are embedded by `villa-embed` (`RAG_EMBEDDING_ENGINE=openai` pointed at
`http://villa-embed:8080/v1`) and stored in `villa-qdrant` (`VECTOR_DB=qdrant`). **No cloud
embedding API is called and no model is downloaded at runtime** — the offline lockdown plus
the pre-staged local embedder remove that vector (KB-03 / PRIV-05).

To get a cited answer (KB-01/02):

1. **Create a Knowledge collection** and upload a document.
2. In a chat, **attach the collection** (or reference it with `#`) and ask a question whose
   answer is only in the uploaded document.
3. The answer is built from the **retrieved chunks** and shows **visible citations /
   sources** — citations are automatic in the standard RAG pipeline; no setting gates their
   display.

> Keep **Native Function Calling** and **Full Context Mode** **off** for this path — both
> bypass the standard chunk → retrieve → inject-with-citations pipeline.

---

## Offline-lockdown environment

When `memory_enabled=true`, `villa` appends the memory/RAG environment block below to the
generated `villa-openwebui.container` unit. These are **derived constants** rendered from
`config.toml` — you change them by changing `config.toml` (or upgrading `villa`), never by
hand-editing the unit. The block is byte-frozen by a golden test; it evolves append-only.

| Environment key | Value | What it enforces |
|-----------------|-------|------------------|
| `VECTOR_DB` | `qdrant` | Use the local Qdrant vector store (never ChromaDB, which posts PostHog telemetry). |
| `QDRANT_URI` | `http://villa-qdrant:6333` | Reach Qdrant by container-DNS on `villa.network` — no host port. |
| `ENABLE_QDRANT_MULTITENANCY_MODE` | `True` | One tenant-partitioned collection (OWUI + Qdrant recommended layout); locked before any vector exists — flipping it later disconnects collections. |
| `QDRANT_COLLECTION_PREFIX` | `open-webui` | Stable collection-name prefix (substrate for the Phase-21 indexer / Phase-23 backup). |
| `RAG_EMBEDDING_ENGINE` | `openai` | Route embeddings to an OpenAI-compatible endpoint — here, the **local** `villa-embed`. |
| `RAG_OPENAI_API_BASE_URL` | `http://villa-embed:8080/v1` | The local embedder; no cloud API. |
| `RAG_OPENAI_API_KEY` | `sk-no-key-required` | Sentinel — the private `villa.network` embedder needs no real key. |
| `RAG_EMBEDDING_MODEL` | `nomic-embed-text-v1.5` | The pre-staged 768-dim embedding model. |
| `RAG_EMBEDDING_QUERY_PREFIX` | `search_query:` | nomic query task-instruction prefix (optimal retrieval). |
| `RAG_EMBEDDING_CONTENT_PREFIX` | `search_document:` | nomic document task-instruction prefix (optimal retrieval). |
| `RAG_EMBEDDING_MODEL_AUTO_UPDATE` | `False` | Never auto-download / update the embedding model at runtime (offline lockdown). |
| `ENABLE_MEMORIES` | `True` | Enable the **manual** native Memory store + cross-chat injection (MEM-01/02/04). Does **not** enable auto-extraction. |
| `ENABLE_PERSISTENT_CONFIG` | `False` | **Load-bearing.** Without it, OWUI bakes RAG/memory settings into `webui.db` on first boot and then **ignores** the env — config drifts off `config.toml`. `False` makes the rendered env win, so **config stays the single source of truth**. |

These join the OWUI block's existing telemetry/offline kill-set (`ANONYMIZED_TELEMETRY=False`,
`DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`, `OFFLINE_MODE=True`,
`ENABLE_VERSION_UPDATE_CHECK=False`, `HF_HUB_OFFLINE=1`, `WEBUI_AUTH=True`).

> **First-boot ordering matters.** Enabling memory on a previously-booted Open WebUI starts
> a fresh Qdrant store, and `ENABLE_PERSISTENT_CONFIG=False` makes the env win over any
> stale DB rows — so the rendered env is always authoritative after `villa install`.

Neither the memory env nor the proof opens a new published host port — Open WebUI keeps its
single loopback `PublishPort` (the chat UI on `127.0.0.1:3000`); Qdrant and the embedder are
container-DNS only. `villa status`'s loopback/privacy audit must stay green (D-11).

---

## Proving zero-outbound with `villa verify memory`

Install-time green is **not** sufficient for the privacy claim — the headline guarantee is a
**runtime** assertion. `villa verify memory` drives the **real** RAG path (upload → embed →
retrieve → cite) and asserts no external host is reached, using a **negative control** that
**must fail**.

### Host-egress precondition (required)

`villa verify memory` pairs the real upload-and-cite drive with a **negative-control external
probe** (a `curl` to a public host such as `https://huggingface.co/` over `villa.network`)
that **MUST fail**. For that control to be meaningful, the host's outbound to the public
internet must be **blocked for the duration of the run**:

- Block host egress with a **deny-all egress firewall rule** (firewalld / nftables) or an
  **offline NIC** for the length of the run, and/or rely on the self-contained
  `podman run --network villa <img> curl https://huggingface.co/` probe that must fail.
- This precondition is **operator-supplied** — the host firewall is not under `villa`'s
  control. (See the per-phase `user_setup`: *block host outbound for the duration of the
  `villa verify memory` run on gfx1151*.)

### Running it

```bash
villa verify memory
```

- **Gated:** with memory **off**, there is nothing to verify and the command exits `0`.
- **PASS** (memory on, egress blocked): the **negative control failed** (egress proven
  blocked — not a false-green) **AND** the real upload → embed → retrieve → cite path
  completed (the planted fact **and** a citation were returned).
- **FAIL** (`exitBlocked`, refuse-with-remediation): the external probe could not run, or an
  external host **was** reachable, or the upload/retrieve/cite drive did not complete. A
  silent skip or unevaluable result is a **FAIL**, never a false-green
  (honesty-by-construction).

### Confirming the negative control is real

To prove the gate is not vacuous: temporarily **un-block egress** and re-run — `villa verify
memory` **MUST now FAIL** the negative control (the external host became reachable). Re-block
egress before final sign-off. A negative control that always passes proves nothing.

---

## Related

- [Configuration](CONFIGURATION.md) — `config.toml`, the managed container env, and how units
  are regenerated from config.
- [Architecture](ARCHITECTURE.md) — where `orchestrate`, the memory render path, and the
  proof seams live.
