# Plan 20-03 Summary — On-Hardware Verification (gfx1151 Strix Halo)

**Status:** Substantially verified on the live host; ONE behavioral criterion (MEM-01 cross-chat injection / SC#1) is UI-confirmation-pending.
**Host:** AMD Strix Halo gfx1151, Fedora Workstation 44 (kernel 7.0.11), rootless podman 5.8.2, backend ROCm 7.2.4. OWUI `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…` v0.9.6.
**Date:** 2026-06-09

## Tasks

### Task 1 — `docs/MEMORY.md` (off-hardware) — DONE
Committed earlier (`4ece8fd`). Operator doc with auto-extraction enable-path (default-off), offline-lockdown env table, and `villa verify memory` usage.

### Task 2 — re-render + reconcile with memory on (INFRA-03) — ✅ VERIFIED
- Used **`villa up`** (NOT `villa install`) to re-render from the existing `config.toml` (`memory_enabled=true`) — deliberately avoids the documented `install→recommend` rocm→vulkan revert. Backend stayed **rocm**, model unchanged.
- GOTCHA found: `villa up`'s "start" is a no-op on an already-running service — the OWUI container had to be recreated via **`villa restart villa-openwebui`** to pick up the new unit.
- Live OWUI container env now carries the full D-09 block: `VECTOR_DB=qdrant`, `QDRANT_URI=http://villa-qdrant:6333`, `ENABLE_QDRANT_MULTITENANCY_MODE=True`, `QDRANT_COLLECTION_PREFIX=open-webui`, `RAG_EMBEDDING_ENGINE=openai`, `RAG_OPENAI_API_BASE_URL=http://villa-embed:8080/v1`, `RAG_EMBEDDING_MODEL=nomic-embed-text-v1.5`, `RAG_EMBEDDING_QUERY_PREFIX=search_query:`, `RAG_EMBEDDING_CONTENT_PREFIX=search_document:`, `RAG_EMBEDDING_MODEL_AUTO_UPDATE=False`, `ENABLE_MEMORIES=True`, and **`ENABLE_PERSISTENT_CONFIG=False`**.
- OWUI booted healthy (`/health {"status":true}`, offline mode). `villa status`: loopback-only true, no telemetry, no new host port.

### Task 4 — document upload → cited local answer (KB-01/02/03, SC#3) — ✅ VERIFIED
Drove the real OWUI REST RAG path over loopback (no egress change):
- Uploaded a doc whose only content was a planted code; file embedded via **villa-embed/nomic (768-dim)** → stored in Qdrant (process status `completed`).
- Chat answer: *"the Villa Straylight emergency override code is PURPLE-MONGOOSE-9AA7BD **[1]**"* — retrieved from the doc **and cited**. Fully local (no cloud, no model download).
- **A5 resolved:** admin token mint = `POST /api/v1/auths/signup` (first user → admin) → Bearer token.
- **A6 resolved:** citation field = top-level **`sources`** (`{"source":{"type":"collection","id":…},"document":[…]}`).

### Task 5 — runtime firewalled zero-outbound proof (PRIV-05, SC#4) — ✅ VERIFIED GREEN
- **Egress open:** `villa verify memory` → FAIL, exit 1 (negative control catches the reachable external host — gate is real, no false-green).
- **Egress blocked:** `villa verify memory` → **"document upload retrieved + cited with zero outbound", exit 0 (PASS)**. The real RAG path completed with the planted fact + citation while the negative-control probe to huggingface.co failed.
- **Q3 resolved (egress mechanism):** a host-wide block was rejected because rootless podman egress masquerades through the host's real NIC via `pasta` — blocking it would also sever the orchestrating agent's own API connection. The correct scoped mechanism is an **nft drop on the `forward` hook inside the rootless network namespace** (`podman unshare --rootless-netns nft …`, isolated table `inet villaverify`, drop `ip daddr != {RFC1918/loopback/link-local}`). This blocks container egress only; the host default netns is untouched. Confirmed: HF unreachable + intra-bridge villa-embed still 768-dim during the block; egress fully restored after.
- Bug found + fixed on-hardware (commit `0e6514b`): `liveRagSmoke`'s `driveRagUploadCite` omitted the required `model` field on `/api/chat/completions` (OWUI → HTTP 400, curl exit 22) and sent `stream` as a string. Added `discoverChatModel` (GET `/api/models`), included it, made `stream` a bool. `make check` green.
- Auth note (operational): `mintAdminToken` uses fixed service creds `villa-verify@localhost` / `villa-verify-memory` (signin → signup-first-user). On an **already-onboarded** OWUI (an admin already exists, signup disabled) it must be **seeded** (admin `POST /api/v1/auths/add`) or the proof fails to authenticate. On a fresh OWUI the signup-first-user path works unaided.

### Task 3 — OWUI Memory (MEM-01/02/03/04, SC#1/SC#2) — PARTIAL
- **MEM-02 (save):** ✅ `POST /api/v1/memories/add`.
- **MEM-04 (view/edit/delete):** ✅ list / update / delete all confirmed; store returns to 0 after delete.
- **MEM-03 (auto-extraction default-off, SC#2):** ✅ memory store was empty before any manual save — no silent extraction occurred from the KB chat. villa never installs Native Function Calling / a memory filter.
- **Memory substrate:** ✅ `POST /api/v1/memories/query` returns the stored memory by semantic similarity (distance ≈0.92) — proves memories are embedded via villa-embed and retrievable through the local path (the core of injection).
- **MEM-01 (cross-chat injection, SC#1):** ⏳ **UI-confirmation-pending.** Injection does NOT occur via the bare `/api/chat/completions` REST path even with the per-user `ui.memory=true` toggle persisted — in OWUI v0.9.6 memory injection is frontend-mediated (the web UI assembles relevant memories into the prompt). A headless Playwright drive was inconclusive (the model completion did not render/trigger in the headless session). **Recommend a 30-second manual check:** in the OWUI UI (Settings → Personalization → Memory ON), state a fact, open a new chat, ask about it, confirm recall; then delete it and confirm it stops being injected.

## Requirements
- INFRA-03 ✅ · KB-01 ✅ · KB-02 ✅ · KB-03 ✅ · PRIV-05 ✅ · MEM-02 ✅ · MEM-03 ✅ · MEM-04 ✅
- MEM-01 ⏳ (substrate verified; cross-chat injection pending a manual UI confirm — frontend-mediated in OWUI v0.9.6)

## Artifacts touched on-hardware
- `cmd/villa/verify_memory.go` — `discoverChatModel` added; chat payload fixed (commit `0e6514b`).
- Test admin accounts created on the live OWUI: `villa-verify@local.test` (onboarded admin) and the seeded service account `villa-verify@localhost` / `villa-verify-memory` (the creds `villa verify memory` expects). **Operator: change/remove these as desired.** Test KB collections + memories were cleaned up.
