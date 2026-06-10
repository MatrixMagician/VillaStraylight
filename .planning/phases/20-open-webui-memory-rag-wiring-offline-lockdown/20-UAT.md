---
status: complete
phase: 20-open-webui-memory-rag-wiring-offline-lockdown
source: 20-01-SUMMARY.md, 20-02-SUMMARY.md, 20-03-SUMMARY.md
started: 2026-06-10T09:53:21Z
updated: 2026-06-10T10:16:30Z
---

## Current Test

[testing complete]

## Tests

### 1. Memory-wired Open WebUI comes up healthy (INFRA-03)
expected: With memory_enabled=true, the live villa-openwebui container env carries the full D-09 block incl. ENABLE_PERSISTENT_CONFIG=False; OWUI /health is green; villa status shows loopback-only, no telemetry, no new host port.
result: pass
evidence: Re-verified live 2026-06-10 — all 12 D-09 env keys present in the running container (VECTOR_DB=qdrant, QDRANT_URI=http://villa-qdrant:6333, RAG_OPENAI_API_BASE_URL=http://villa-embed:8080/v1, RAG_EMBEDDING_MODEL=nomic-embed-text-v1.5, ENABLE_MEMORIES=True, ENABLE_PERSISTENT_CONFIG=False, …); /health {"status":true}; villa status loopback-only true, "no telemetry; outbound = image/model pulls only". Note — villa-qdrant/villa-embed show OFFLOAD WARN: non-GPU-service N/A pattern is Phase 23 scope, not a Phase 20 criterion.

### 2. Document upload → cited local answer (KB-01/02/03)
expected: Uploading a document embeds it via villa-embed/nomic (768-dim) into Qdrant; asking a question whose answer exists only in that document returns the planted fact WITH a citation — fully local.
result: pass
evidence: Proven live 2026-06-10 inside the `villa verify memory` PASS (the proof drives the real REST upload→embed→poll→chat path and only greens when the planted fact is retrieved AND cited); also verified independently in 20-03 Task 4 ("PURPLE-MONGOOSE-9AA7BD [1]" cited).

### 3. Runtime firewalled zero-outbound proof — villa verify memory (PRIV-05)
expected: Egress OPEN → FAIL exit 1 (negative control catches reachable external host). Egress BLOCKED → "document upload retrieved + cited with zero outbound", exit 0.
result: pass
evidence: Run live 2026-06-10 with rebuilt binary (v1.2-63-g3047e5d): egress open → "runtime zero-outbound RAG proof FAILED: egress is NOT blocked…" exit 1; with nft drop applied inside the rootless netns (table inet villaverify, forward hook, non-RFC1918/loopback/link-local daddr drop) → "document upload retrieved + cited with zero outbound" exit 0. Egress restored after (table deleted; container probe to huggingface.co over villa network → HTTP 200). GOTCHA: `fwd` is a reserved nftables keyword — chain must use another name (used `villaforward`); ruleset must be piped via `nft -f -` stdin (file paths unreadable inside `podman unshare --rootless-netns`).

### 4. Memory save / view / edit / delete (MEM-02, MEM-04)
expected: A user memory can be saved, listed, updated, and deleted; after delete the store returns to 0.
result: pass
evidence: Full lifecycle live 2026-06-10 via /api/v1/memories: add → list (1) → semantic query returns it (distance ≈0.903, embedded via villa-embed) → update (content changed) → delete (true) → list [].

### 5. Auto-extraction is default-off (MEM-03, SC#2)
expected: Without explicit opt-in, no memories appear from ordinary chatting; store stays empty until a memory is deliberately saved.
result: pass
evidence: Store was [] on 2026-06-10 before any manual save, despite prior chats on this OWUI (usage counters non-zero, 20-03 KB chats + this session's chats). No memory filter/Native Function Calling installed by villa.

### 6. Cross-chat memory injection via the UI (MEM-01, SC#1)
expected: With Settings → Personalization → Memory ON, a saved fact is recalled in a NEW chat; after deleting the memory, a fresh chat no longer recalls it.
result: pass
evidence: Driven through the real web UI with Playwright on 2026-06-10 (the 20-03 pending item — injection is frontend-mediated in OWUI v0.9.6). Seeded memory "The operator's cat is named Wintermute", ui.memory=true; NEW chat asked "What is my cat's name?" → "Based on your provided context, your cat's name is Wintermute." (fact never stated in that chat). Deleted the memory → fresh chat, same question → "I don't actually know your cat's name!…" — injection stops. Test chats deleted afterward; ui.memory left ON for the villa-verify@localhost service account.

## Summary

total: 6
passed: 6
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none]
