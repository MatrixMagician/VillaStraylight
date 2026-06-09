---
phase: 20
slug: open-webui-memory-rag-wiring-offline-lockdown
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-09
---

# Phase 20 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard `testing`; table-driven + byte-for-byte golden fixtures) |
| **Config file** | none — `Makefile` targets (`make test`, `make check`) |
| **Quick run command** | `go test ./internal/orchestrate/... ./cmd/villa/...` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds (off-hardware; the runtime zero-outbound proof is on-hardware/manual) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/orchestrate/... ./cmd/villa/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds (off-hardware tests)

---

## Per-Task Verification Map

> Populated by the planner from PLAN.md tasks. Off-hardware Go tests cover the env-block
> render + golden re-freeze + seam gate; the runtime zero-outbound RAG proof (PRIV-05/SC#4)
> and the OWUI Memory/KB behavioural criteria (SC#1–3) are on-hardware verification-wave items.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 20-01-01 | 01 | 1 | INFRA-03 | — | OWUI env block renders D-09 keys only when `memory_enabled=true`; byte-identical off | golden | `go test ./internal/orchestrate/...` | ❌ W0 | ⬜ pending |
| 20-01-02 | 01 | 1 | INFRA-03 | — | `ENABLE_PERSISTENT_CONFIG=False` present in the memory-on env block | golden/unit | `go test ./internal/orchestrate/...` | ❌ W0 | ⬜ pending |
| 20-01-03 | 01 | 1 | PRIV-05 | — | telemetry-frozen test is memory-aware; seam gate green (no re-typed literals) | unit | `go test ./internal/orchestrate/... ./internal/inference/...` | ❌ W0 | ⬜ pending |
| 20-0X-XX | — | 2 | PRIV-05 | — | runtime firewalled upload→retrieve reaches no external host; negative-control external probe FAILS | manual/on-hw | on-hardware proof (`evalMemoryProof` extension) | ❌ W0 | ⬜ pending |
| 20-0X-XX | — | 2 | MEM-01..04 | — | cross-chat memory inject; manual save; view/edit/delete; deleted memory stops injecting | manual/on-hw | OWUI Memory UI/API | n/a | ⬜ pending |
| 20-0X-XX | — | 2 | KB-01..03 | — | upload → local embed (villa-embed) + Qdrant retrieve + visible citations, fully offline | manual/on-hw | OWUI Knowledge UI/API | n/a | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/orchestrate/testdata/villa-openwebui.container.memory.golden` — new golden for the memory-on env block (memory-off golden stays byte-identical)
- [ ] Memory-aware update to `TestRenderOpenWebUITelemetryFrozen` (env-line count parameterized by `memory_enabled`)
- [ ] Runtime zero-outbound RAG smoke proof harness extending the Phase-19 `evalMemoryProof` seam (pure core unit-testable off-hardware via injected probes)

*Existing `go test` infrastructure otherwise covers phase requirements.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Cross-chat memory recall + view/edit/delete; deleted memory stops injecting | MEM-01/02/04 | OWUI native UI/agent behaviour; needs a live model + OWUI DB | On-hardware: enable `ENABLE_MEMORIES`, state a fact, open a new chat, confirm injection; save a message to memory; view/edit/delete; confirm deleted memory no longer injected |
| Automatic memory extraction toggle is opt-in/default-off | MEM-03 | Native Function-Calling / community Filter is a per-model UI / function toggle, not a single env | On-hardware: confirm auto-extraction is OFF by default; document + exercise the user enable path |
| Document upload → cited answer, fully local & offline | KB-01/02/03 | Needs a live OWUI + villa-embed + Qdrant + chat model | On-hardware: upload a doc to a Knowledge collection, ask a question, confirm visible citations; confirm chunk/embed/retrieve hit only villa-embed + Qdrant (no cloud, no model download) |
| Runtime firewalled zero-outbound document-upload smoke test | PRIV-05 | Requires host-layer egress block + live RAG path; OWUI cannot use `--network none` | On-hardware: block host egress, drive a real upload→retrieve, assert no external host reached; pair with a negative-control external probe that MUST fail (else false-green) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
