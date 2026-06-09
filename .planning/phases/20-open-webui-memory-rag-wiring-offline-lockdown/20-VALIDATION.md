---
phase: 20
slug: open-webui-memory-rag-wiring-offline-lockdown
status: approved
nyquist_compliant: true
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
> render + golden re-freeze + seam gate + the `evalRagSmoke` pure core + the docs/MEMORY.md
> content check; the runtime zero-outbound RAG proof (PRIV-05/SC#4) and the OWUI Memory/KB
> behavioural criteria (SC#1–3) are on-hardware verification-wave checkpoints (manual by
> nature — they need a live model + populated OWUI DB + host-egress control). Every
> implementation (auto) task carries an `<automated>` verify; the on-hardware rows are
> checkpoints, not missing automation.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 20-01-01 | 01 | 1 | INFRA-03 | — | OWUI env block renders D-09 keys only when `memory_enabled=true`; byte-identical off | golden | `go test ./internal/orchestrate/...` | ❌ W0 | ⬜ pending |
| 20-01-02 | 01 | 1 | INFRA-03 | — | `ENABLE_PERSISTENT_CONFIG=False` present in the memory-on env block | golden/unit | `go test ./internal/orchestrate/...` | ❌ W0 | ⬜ pending |
| 20-01-03 | 01 | 1 | PRIV-05 | — | telemetry-frozen test is memory-aware; seam gate green (no re-typed literals) | unit | `go test ./internal/orchestrate/... ./internal/inference/...` | ❌ W0 | ⬜ pending |
| 20-02-01 | 02 | 1 | KB-01/02/03, PRIV-05 | T-20-07 | pure `evalRagSmoke` core: negative-control-first; silent skip = FAIL | unit | `go test ./cmd/villa/... -run 'EvalRagSmoke' -count=1` | ❌ W0 | ⬜ pending |
| 20-02-02 | 02 | 1 | KB-01/02/03, PRIV-05 | T-20-08/09 | `liveRagSmoke` REST drive + fixed-arg negative-control egress probe; no re-typed image literal | unit/build | `go build ./cmd/villa/... && go vet ./cmd/villa/...` | ❌ W0 | ⬜ pending |
| 20-02-03 | 02 | 1 | KB-01/02/03, MEM-03, PRIV-05 | T-20-10 | `villa verify memory` gated on memory_enabled, refuse-with-remediation, no new port | build/help | `go build ./cmd/villa/... && villa verify memory --help` | ❌ W0 | ⬜ pending |
| 20-03-01 | 03 | 2 | MEM-01..04, KB-01..03, PRIV-05 | T-20-14 | docs/MEMORY.md documents save/view/edit/delete, auto-extraction enable-path (default-off), offline-lockdown env table, `villa verify memory` + egress precondition | doc/content-check | `test -f docs/MEMORY.md && grep -q 'Enabling automatic memory extraction' docs/MEMORY.md && grep -q 'ENABLE_PERSISTENT_CONFIG' docs/MEMORY.md && grep -q 'verify memory' docs/MEMORY.md && grep -q 'ENABLE_MEMORIES' docs/MEMORY.md` | n/a | ⬜ pending |
| 20-03-02 | 03 | 2 | INFRA-03 | — | live re-render/reconcile: `villa install` emits the D-09 memory-on env block (incl. ENABLE_PERSISTENT_CONFIG=False); OWUI healthy | manual/on-hw | on-hardware (`villa install` re-render proof) | n/a | ⬜ pending |
| 20-03-03 | 03 | 2 | MEM-01/02/03/04 | T-20-14 | cross-chat memory inject; manual save; view/edit/delete; deleted memory stops injecting; auto-extraction default-off + toggleable | manual/on-hw | OWUI Memory UI/API | n/a | ⬜ pending |
| 20-03-04 | 03 | 2 | KB-01/02/03 | — | upload → local embed (villa-embed) + Qdrant retrieve + visible citations, fully offline; A5/A6 resolved | manual/on-hw | OWUI Knowledge UI/API | n/a | ⬜ pending |
| 20-03-05 | 03 | 2 | PRIV-05 | T-20-12/13/15 | runtime firewalled upload→retrieve reaches no external host; negative-control external probe FAILS; gate FAILs when egress open; no new host port | manual/on-hw | on-hardware (`villa verify memory`, egress-blocked) | n/a | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/orchestrate/testdata/villa-openwebui.container.memory.golden` — new golden for the memory-on env block (memory-off golden stays byte-identical)
- [ ] Memory-aware update to `TestRenderOpenWebUITelemetryFrozen` (env-line count parameterized by `memory_enabled`)
- [ ] `cmd/villa/verify_memory_test.go::TestEvalRagSmoke` — pure-core unit test for `evalRagSmoke` (negative-control-first, injected probes, off-hardware)

*Existing `go test` infrastructure otherwise covers phase requirements.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live re-render/reconcile with memory on | INFRA-03 | Needs a live Quadlet/systemd host; `villa install` touches the user systemd manager | On-hardware: set `memory_enabled=true`, run `villa install`, confirm the rendered `villa-openwebui.container` carries the D-09 env block (incl. `ENABLE_PERSISTENT_CONFIG=False`) and OWUI is healthy |
| Cross-chat memory recall + view/edit/delete; deleted memory stops injecting | MEM-01/02/04 | OWUI native UI/agent behaviour; needs a live model + OWUI DB | On-hardware: enable `ENABLE_MEMORIES`, state a fact, open a new chat, confirm injection; save a message to memory; view/edit/delete; confirm deleted memory no longer injected |
| Automatic memory extraction toggle is opt-in/default-off | MEM-03 | Native Function-Calling / community Filter is a per-model UI / function toggle, not a single env | On-hardware: confirm auto-extraction is OFF by default; document + exercise the user enable path |
| Document upload → cited answer, fully local & offline | KB-01/02/03 | Needs a live OWUI + villa-embed + Qdrant + chat model | On-hardware: upload a doc to a Knowledge collection, ask a question, confirm visible citations; confirm chunk/embed/retrieve hit only villa-embed + Qdrant (no cloud, no model download) |
| Runtime firewalled zero-outbound document-upload smoke test | PRIV-05 | Requires host-layer egress block + live RAG path; OWUI cannot use `--network none` | On-hardware: block host egress, drive a real upload→retrieve, assert no external host reached; pair with a negative-control external probe that MUST fail (else false-green) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved

> Nyquist note: every Wave-1 implementation (auto) task (20-01-01..03, 20-02-01..03) and the
> off-hardware doc task (20-03-01) carries an `<automated>` verify. The remaining Plan-03 rows
> (20-03-02..05) are on-hardware human-verify checkpoints by nature — SC#4 explicitly requires
> a RUNTIME assertion on a live host with a host-egress precondition, which cannot be a pure
> unit test. These are checkpoints, not missing automation, so `nyquist_compliant: true` holds.
> `wave_0_complete: false` because execution has not yet run.
