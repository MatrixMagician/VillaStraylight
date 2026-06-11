---
phase: 20-open-webui-memory-rag-wiring-offline-lockdown
plan: 02
subsystem: testing
tags: [rag, zero-outbound, proof-seam, openwebui, qdrant, embeddings, podman, curl, cobra, privacy]

# Dependency graph
requires:
  - phase: 19-vector-store-local-embeddings-services
    provides: "the Phase-19 proof seam (memoryProof/evalMemoryProof/liveMemoryProof/runProbeCurl), orchestrate.EmbedImage() helper-image accessor, villa.network + container-DNS-only villa-embed/villa-qdrant"
  - phase: 20-open-webui-memory-rag-wiring-offline-lockdown (Plan 01)
    provides: "OWUI memory/RAG env wiring (D-09 block) behind the orchestrate seam — the RAG path under test"
provides:
  - "Pure evalRagSmoke core: negative-control-first runtime zero-outbound RAG-smoke verdict (unit-testable off-hardware via injected probes)"
  - "liveRagSmoke seam: REST RAG drive over the loopback PublishPort + a fixed-arg negative-control external egress probe over villa.network"
  - "`villa verify memory` subcommand: gated on persisted memory_enabled, refuse-with-remediation on FAIL via an injectable Deps seam"
affects: [phase-20-plan-03, recall-indexer, surfacing-backup]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Honesty-by-construction proof seam (verdict type + pure core + injected probes + fixed-arg exec) extended to a runtime egress proof"
    - "Negative-control-first ordering: egress proven blocked BEFORE the drive is trusted (false-green prevention)"

key-files:
  created:
    - cmd/villa/verify_memory.go
    - cmd/villa/verify_memory_test.go
    - cmd/villa/verify.go
  modified:
    - cmd/villa/root.go

key-decisions:
  - "Reused memoryProof as the RAG-smoke verdict type (one verdict type, no new ragProof struct) — planner's discretion per the plan."
  - "Loopback REST drive uses a NEW host-side fixed-arg curl runner (runLoopbackCurl/runLoopbackCurlStdin), distinct from runProbeCurl, because the OWUI PublishPort is a host loopback bind not reachable from inside villa.network."
  - "Egress negative-control reuses runProbeCurl over villa.network against https://huggingface.co/ (the canonical OWUI runtime exfil target); reachability ⇒ NOT blocked ⇒ FAIL."
  - "Admin token mint tries signin then falls back to signup (first-user-becomes-admin on a fresh DB); citation parse checks both sources and citations at top level and per-choice — token/citation-field specifics confirmed on-hardware in Plan 03 (A5/A6)."
  - "`villa verify memory` is a dedicated verb (on-hardware by nature), not a per-install gate — memory OFF exits 0 (nothing to verify), memory ON FAIL → exitBlocked."

patterns-established:
  - "Negative-control-first runtime proof: a probe that could not run, or an external host reachable, is a FAIL — never a silent skip."
  - "Async file-processing poll treats a timeout as an ERROR (the RAG path did not complete), never a skip (Pitfall 6)."

requirements-completed: [KB-01, KB-02, KB-03, MEM-03, PRIV-05]

# Metrics
duration: ~14min
completed: 2026-06-09
---

# Phase 20 Plan 02: Runtime Zero-Outbound RAG Smoke Proof Summary

**A pure negative-control-first `evalRagSmoke` core + a `liveRagSmoke` REST-drive/egress-probe seam + a gated `villa verify memory` subcommand that proves a real document upload retrieves + cites with ZERO outbound — never a false-green.**

## Performance

- **Duration:** ~14 min
- **Completed:** 2026-06-09
- **Tasks:** 3 (Task 1 TDD)
- **Files modified:** 4 (3 created, 1 modified)

## Accomplishments
- Pure `evalRagSmoke(egressBlocked, uploadCite, wantFact)` core: asserts the egress negative-control FIRST (a probe that could not run OR an external host reachable → FAIL), then the upload-and-cite path (upload error / missing fact / no citation → FAIL); all-good → PASS. No WARN, no skip — an unevaluable result is a FAIL (D-10, PRIV-05).
- `liveRagSmoke` seam: a fixed-arg negative-control external probe (`curl -sf --max-time 5 https://huggingface.co/`) over villa.network via the existing `runProbeCurl` (helper image from `orchestrate.EmbedImage()`, no re-typed image literal), plus a loopback REST RAG drive (signin/signup → knowledge/create → files/ multipart → poll process/status → file/add → plain chat/completions with `files:[{type:collection,id}]`) over the existing PublishPort — no new host port (D-11).
- `villa verify memory` subcommand: thin cobra caller + injectable `verifyMemoryDeps` seam (mirrors doctor.go), gated on persisted `memory_enabled`, refuse-with-remediation (`exitBlocked`) on FAIL, registered in `root.go`.
- Full off-hardware unit coverage: `TestEvalRagSmoke` (6 cases) + `TestEvalRagSmokeNegativeControlFirst` (spy proves uploadCite never runs when the negative control fails) + `TestRunVerifyMemoryGate` (gate + FAIL/PASS exit mapping). `make check` green; `TestSeamGrepGate` green.

## Task Commits

Each task was committed atomically:

1. **Task 1: Pure evalRagSmoke core + injected-probe tests** - `b44aa17` (feat; TDD RED→GREEN written atomically — test + core in one commit)
2. **Task 2: liveRagSmoke seam (REST drive + negative-control egress probe)** - `0e4e3fd` (feat)
3. **Task 3: Wire `villa verify memory` (gated, refuse-with-remediation)** - `99ced77` (feat; includes TestRunVerifyMemoryGate)

## Files Created/Modified
- `cmd/villa/verify_memory.go` - `ragSmokeInput` struct, the pure `evalRagSmoke` core, `liveRagSmoke` + the loopback REST drive (`driveRagUploadCite`/`mintAdminToken`/`pollFileProcessed`/`parseChatAnswerAndCitation`) and fixed-arg host-side curl runners.
- `cmd/villa/verify_memory_test.go` - `TestEvalRagSmoke`, `TestEvalRagSmokeNegativeControlFirst`, `TestRunVerifyMemoryGate`.
- `cmd/villa/verify.go` - `newVerify()` parent + `newVerifyMemory()` subcommand, `verifyMemoryDeps` seam, `liveVerifyMemoryDeps()`, `runVerifyMemory()`.
- `cmd/villa/root.go` - registered `newVerify()` in the command tree.

## Decisions Made
- Reused `memoryProof` as the RAG-smoke verdict type (one verdict type across the proof seams).
- Added a dedicated host-side fixed-arg curl runner for the loopback drive (the PublishPort is a host bind, not reachable from inside villa.network); the egress negative control stays on `runProbeCurl` over villa.network.
- Admin-token mint and citation-field parsing are structured to defer the exact token path / citation field name to the Plan-03 on-hardware confirmation (A5/A6) while keeping the code green and seam-clean now.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
- A scratch `net/http` import was added then removed during Task 2 (the loopback drive uses `os/exec`+`curl`, not an in-process HTTP client, to keep every invocation fixed-arg and shell-free). Resolved before the Task 2 commit; build/vet clean.

## Known Stubs
None blocking the plan goal. Two values are deliberately deferred to the Plan-03 on-hardware step and documented inline as such (not silent placeholders):
- `mintAdminToken` tries signin then signup — the on-hardware step seeds/confirms the admin account (Open Question 1 / A5).
- `parseChatAnswerAndCitation` checks BOTH candidate citation fields (`sources`, `citations`) at the top level and per-choice — the exact field name is confirmed on-hardware in Plan 03 (A6). This is defensive, not a stub: a green still requires a real citation to be present.
The live drive's planted question/fact (`ragSmokeQuestion`/`ragSmokeWantFact`) are the seam contract the verification wave seeds the matching document against.

## Next Phase Readiness
- The pure core + seam + verb are ready; Plan 03 runs the on-hardware firewalled drive: seed the planted doc, supply the host-egress block precondition, confirm the admin-token mint path (A5) and the citation field name (A6), and capture the runtime PASS.
- No new host port, no new dependency, `TestSeamGrepGate` green — privacy/seam invariants intact.

## Self-Check: PASSED

---
*Phase: 20-open-webui-memory-rag-wiring-offline-lockdown*
*Completed: 2026-06-09*
