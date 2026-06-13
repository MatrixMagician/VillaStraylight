---
gsd_state_version: 1.0
milestone: v1.4
milestone_name: Coding Agent
status: completed
stopped_at: Phase 25 context gathered
last_updated: "2026-06-13T09:57:33.894Z"
last_activity: 2026-06-13
progress:
  total_phases: 5
  completed_phases: 1
  total_plans: 4
  completed_plans: 4
  percent: 20
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-12 — milestone v1.4 Coding Agent started)

**Core value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box. v1.2 extended the bar to "and stays operable, recoverable, and measurable over time." v1.3 extended it to "and remembers the user and their documents across chats — strictly local." v1.4 extends it to "and gives the operator a strictly-local terminal coding agent, wired to a fit-guarded coding model."
**Current focus:** Phase 24 — Coder Fit Math, Catalog & On-Hardware Model Qualification

## Current Position

Phase: 25
Plan: Not started
Status: Phase 24 complete — catalog FROZEN, D-13 toolbox keep recorded, CODER-01/02/03 closed. Next: Phase 25 (CMODE).
Last activity: 2026-06-13

Progress: [██░░░░░░░░] 20% (v1.4)

## Performance Metrics

**Velocity:**

- Total plans completed: 91 (v1.0 + v1.1: 48; v1.2: 19; v1.3: 20)
- Average duration: 34 min
- Total execution time: 2.2 hours

**By Phase (shipped):**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 3 | - | - |
| 02 | 3 | - | - |
| 04 | 3 | - | - |
| 06 | 3 | - | - |
| 07 | 3 | - | - |
| 11 | 2 | - | - |
| 12 | 3 | - | - |
| 13 | 3 | - | - |
| 14 | 3 | - | - |
| 15 | 4 | - | - |
| 16 | 3 | - | - |
| 17 | 3 | - | - |
| 18 | 2 | - | - |
| 19 | 3 | - | - |
| 20 | 3 | - | - |
| 21 | 3 | - | - |
| 22 | 4 | - | - |
| 23 | 5 | - | - |
| 24 | 4 | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
| Phase 12 P01 | 15 | 3 tasks | 6 files |
| Phase 12 P02 | ~20 min | 2 tasks | 11 files |
| Phase 12 P03 | ~20 min | 3 of 4 tasks (Task 4 on-hardware pending) | 4 files |
| Phase 13 P01 | 3min | 2 tasks | 2 files |
| Phase 13 P02 | 8min | 2 tasks | 6 files |
| Phase 13 P03 | ~38min | 3 tasks (TDD) | 5 files |
| Phase 14 P01 | 4min | 2 tasks (TDD) | 3 files |
| Phase 15 P01 | 2 min | 2 tasks | 2 files |
| Phase 15 P02 | 2m | 1 tasks | 3 files |
| Phase 15 P03 | 6 min | 2 tasks | 4 files |
| Phase 15 P04 | ~12 min | 2 tasks | 5 files |
| Phase 16 P01 | 6m | 2 tasks | 14 files |
| Phase 16 P02 | ~12m | 2 tasks | 13 files |
| Phase 16 P03 | ~22m | 2 tasks | 7 files |
| Phase 17 P01 | 14min | 2 tasks | 6 files |
| Phase 17 P02 | 22min | 2 tasks | 3 files |
| Phase 17 P03 | 18min | 2 tasks | 2 files |
| Phase 18 P01 | 4min | 3 tasks | 3 files |
| Phase 18 P02 | ~7 min | 3 tasks (TDD) | 3 files |
| Phase 19 P01 | ~14 min | 3 tasks | 10 files |
| Phase 19 P02 | 25min | 2 tasks | 3 files |
| Phase 19 P03 | ~10min | 3 tasks | 0 files |
| Phase 20 P01 | 4 min | 3 tasks | 4 files |
| Phase 20 P02 | ~14min | 3 tasks | 4 files |
| Phase 21 P01 | 10min | 3 tasks | 8 files |
| Phase 21 P02 | 15min | 3 tasks | 6 files |
| Phase 21 P03 | 25min | 3 tasks | 0 files |
| Phase 22 P01 | 9min | 2 (TDD) tasks | 13 files |
| Phase 22 P02 | 10min | 2 tasks | 6 files |
| Phase 22 P03 | 13min | 2 (TDD) tasks | 7 files |
| Phase 22 P04 | ~17min | 2 tasks | 1 files |
| Phase 23 P01 | 16 min | 3 tasks | 16 files |
| Phase 23 P02 | 18min | 2 (TDD) tasks tasks | 12 files files |
| Phase 23 P03 | 10 min | 2 tasks | 3 files |
| Phase 23 P04 | 10 min | 2 tasks | 7 files |
| Phase 23 P05 | 9 min (drill; checkpoint pending) | 1 of 2 tasks | 0 files |
| Phase 24 P01 | 9min | 2 tasks | 8 files |
| Phase 24 P02 | 7min | 2 tasks | 7 files |
| Phase 24 P03 | ~3h (on-hardware) | 4 tasks (3 auto + 1 checkpoint APPROVED) | 24 evidence + 3 files |
| Phase 24 P04 | ~6min | 3 tasks (2 auto + 1 checkpoint APPROVED) | 4 files |

## Accumulated Context

### Roadmap Evolution

- **v1.4 Coding Agent roadmap created (2026-06-12): Phases 24–28 mapped from 17 requirements** (CODER/CMODE/AGENT/INSTALL/PRIV/SURF/USAGE); numbering CONTINUES from v1.3 (last phase 23). Structure follows the research-reconciled 5-phase shape verbatim: fit math + on-hardware model qualification FIRST (24), render + transactional swap verb (25), agent delivery core + lockdown launcher (26), install addon + preflight + `villa verify agent` (27), surfacing + the single `status.Report` 3→4 contract bump LAST (28). 100% coverage, no orphans, no duplicates. Research flags: Phases 24/26/27 need `/gsd-plan-phase --research-phase`; 25/28 follow shipped patterns.
- v1.3 Memory & Knowledge roadmap created (2026-06-09): Phases 18–23 mapped from 22 requirements; shipped 2026-06-11, audit PASSED.
- v1.2 Operability roadmap created (2026-06-07): Phases 12–17 mapped from 13 requirements, research-converged build order preserved.
- Phase 11 added (2026-06-06): Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation (v1.1, shipped).

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. v1.4 roadmap decisions:

- [v1.4 Roadmap]: **Agent of record is Crush v0.76.0, NOT OpenCode** — OpenCode is structurally unlockable to the zero-outbound posture (unconditional `models.dev` startup fetch, runtime bun/npm downloads, autoupdate default-ON, air-gap closed upstream as not-planned #2224). Crush's two outbound channels both have config kill switches villa renders AND proves at runtime — never flag-trusted.
- [v1.4 Roadmap]: **Residency = transactional swap-based coding mode** composing `internal/modelswap`; install default is shared mode (agent rides the chat endpoint). Co-resident `villa-coder` deferred to a 128 GB fit-gated v2 stretch. Residency mode is always an OUTPUT of recommend fit math at agent-profile ctx, never a preference. Mode change is an explicit verb, never automatic (ROCm precedent).
- [v1.4 Roadmap]: **Qdrant code-collection premise researched and REJECTED** (four-way research convergence): text embedder + chunking breaks code semantics + index stale on agent edits; codebase memory is agent-native (Crush LSP + ripgrep/glob + `AGENTS.md`/`CRUSH.md`). villa-qdrant/villa-embed are UNTOUCHED by v1.4. A Qdrant-backed MCP code search is v2-only, behind a pre-declared numeric eval it must WIN.
- [v1.4 Roadmap]: **Agent is a villa-managed host binary, never a container/service** — pinned release + SHA-256 via a villa-owned `go:embed` pin policy (rocm-policy pattern); `crush.json` is a derived artifact of `config.toml` (regenerated, never hand-edited; doctor flags drift, never auto-corrects).
- [v1.4 Roadmap]: **Honesty constraints frozen into success criteria**: negative-control-first egress proofs (egress-open run must FAIL), llama-down cloud-fallback control (agent working with inference down = FAIL), under-load residency in the swap prove step (idle-green is not green), addon-off renders byte-identical to v1.3, tool-call round-trip as readiness (health-200 never suffices).
- [v1.4 Roadmap]: **EXACTLY ONE byte-frozen contract evolves** — `status.Report` 3→4, append-only `coding` block, single golden re-freeze, landed in the final phase (28). Catalog 2→3 and recommend 2→3 are separate contracts landing once, early (24).

Earlier (v1.0–v1.3) standing decisions retained:

- [Roadmap]: Inference behind a `Backend` interface; the single `BackendFor()` resolver is the only polymorphism point, fail-closed.
- [Roadmap]: Config is the single source of truth; Quadlet units (and now agent config) are derived/regenerated, never hand-edited.
- [Roadmap]: `--json`/dashboard contracts are byte-frozen; evolve append-only + schema bump, re-freeze goldens exactly once.
- [Roadmap]: Offload-asserting — a silent/partial CPU fallback is a FAIL, never a false-green; backend marker literals stay behind the `internal/inference` seam (`TestSeamGrepGate`).
- [v1.1]: ROCm is opt-in; Vulkan RADV stays the default; `recommend` advises, never auto-switches. Digest-pin all images (never floating/nightly tags).
- [v1.2]: New persistence is flat JSONL/JSON under `$XDG_DATA_HOME/villa/` — never `config.toml`, never embedded SQLite; each decision-logic feature gets ONE new pure `internal/*` core; orchestrate stays the only intentionally-impure module.
- [v1.3]: Zero-outbound is proven at runtime, negative-control-first — never flag-trusted (`villa verify memory` precedent extends to `villa verify agent`). Reservation-before-fit in `recommend.Pick()`; D-09 reflect-pinned chat-swap isolation from memory units.
- [16-03]: clean-recreate-before-import is the load-bearing restore fix (podman volume import MERGES + does NOT auto-create).
- [Phase 24-01]: AgentSampling is a pointer field (*AgentSampling) so absent blocks stay absent on re-encode; chat entries emit no agent_sampling key (D-03)
- [Phase 24-01]: External coder-entry validation refuses the whole catalog (warning naming entry id + seed fallback), never clamps — bounds: agent_ctx>0, temperature (0,2], top_p (0,1], top_k>=0, repeat_penalty (0,3]
- [Phase 24-02]: Coder block computed once in Pick after the reservation shrink and threaded to finalizeRecommendation (refusal path passes the shared conservative floor); --model override of a coder id is warn-and-allow (chat KV stays effectiveCtx; only pickCoder is agent_ctx-locked)
- [Phase 24-03]: On-hardware qualification (CODER-03, D-08/D-09) APPROVED — all three coder entries PASS on the PINNED vulkan-radv digest (build 9496, NOT the drifted 9579 tag): offload 49/49, measured KV exact-match to fit math (6.00 / 3.00 / 3.00 GiB). FINDING A2: DeltaNet recurrent-state buffer = 301.50 MiB (ctx/quant-independent) — candidate min_envelope_bytes constant for 24-04. FINDING A3: cache-reuse on the Next hybrids came back TRUE via recurrent-state context checkpoints (75.376 MiB), NOT n_cache_reuse chunk reuse — cache_reuse_safe catalog claim to be RATIFIED in 24-04. Build 9579 re-pin fallback NOT needed (9496 has full Qwen3-Next arch support). Harness deviation: Crush v0.76.0 rejects --yolo with `run`; auto-accept via config permissions.allowed_tools (strictly tighter than --yolo).
- [Phase 24-04]: **D-13 (ratifies D-11): KEEP the pinned vulkan-radv digest sha256:9a74e555 (build 9496) — NO re-pin.** Fallback build 9579 not needed (9496 has full Qwen3-Next/DeltaNet arch + working Qwen3-Coder tool-call parser). Catalog FROZEN: all three coder entries `cache_reuse_safe: true` as the literal probe verdict (truth-up of A3) — **build-9496-scoped** (Next-entry reuse mechanism is recurrent-state context checkpoints, NOT GQA KV shifting; re-probe required if a future re-pin drops checkpoint support). D-10/D-12 delete-over-hope contingency NOT triggered (3/3 PASS). Recommend golden untouched since 24-02 (be8ee0e). Operator ratified the freeze at the SC#1/SC#4 blocking checkpoint. CODER-01/02/03 closed; Phase 24 COMPLETE.

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

- Carryover tech debt (non-blocking, from v1.2 close): extract a shared `rocmpolicy` leaf package (deferred PR-#2 finding, graphmind memory 10e784d6); investigate `rocm-6.4.4-rocwmma` residency FAIL on gfx1151; optional re-pin of the drifted `rocm-6.4.4` rolling tag.

### Blockers/Concerns

[Issues that affect future work]

- NON-NEGOTIABLE THREAT (Phase 27): agent phone-home at **startup** (registry/update fetches fire before first prompt) and silent cloud-model fallback — both must be closed by the negative-control-first `villa verify agent` (egress-open run FAILs; llama-down control FAILs an agent that keeps working). Kill switches are documented upstream but unproven on this host until Phase 27.
- Phase 24 risk: tool-call/jinja template landmines — GGUF artifacts must be pinned at repo+revision level (the embedded chat template is part of the artifact); the pinned `vulkan-radv` toolbox digest may need a re-pin if its llama.cpp predates Qwen3-Next arch support (PR #16095) + Feb-2026 tool-call parser fixes. `--cache-reuse` is incompatible with recurrent/hybrid models incl. the current chat model; verify for Qwen3-Coder-Next.
- Phase 24 risk: agent-scale KV (~6–12 GiB at 64–128k ctx) blows fit math anchored on chat contexts — fit must be computed at the agent's RENDERED ctx; KV-quantization only as a catalog-declared, benched choice.
- v1.3 audit-recorded debt (non-blocking, see Deferred Items): literal reboot durability re-confirmation; restore-side tar streaming (WR-06); install service-name drift test; formal 20-VERIFICATION.md; Nyquist finalization for Phases 18–19.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260604-wh1 | Fix F-3: villa status OFFLOAD WARN — emit -lv 4 residency line + invocation-scoped ResidencyJournal | 2026-06-04 | d401a52 | [260604-wh1-...](./quick/260604-wh1-fix-f-3-villa-status-offload-warn-emit-l/) |
| 260605-d2q | Fix Makefile build target to produce villa binary | 2026-06-05 | b3a4419 | [260605-d2q-...](./quick/260605-d2q-fix-makefile-build-target-to-produce-vil/) |
| 260605-fast | fix(status): render OFFLOAD N/A for non-GPU services in human table | 2026-06-05 | e5fc1fc | — |
| 260605-tuv | Fix villa uninstall: drop unsupported podman volume rm --ignore flag | 2026-06-05 | 228a4c0 | [260605-tuv-...](./quick/260605-tuv-fix-villa-uninstall-drop-unsupported-pod/) |
| 260606-p3a | Fix villa bench single-mode backend label | 2026-06-06 | 8aa9c90 | [260606-p3a-...](./quick/260606-p3a-fix-villa-bench-single-mode-backend-labe/) |
| 260608-ppy | fix phase-17 UI-SPEC copy gaps | 2026-06-08 | 583b1ee | [260608-ppy-...](./quick/260608-ppy-fix-phase-17-ui-spec-copy-gaps-17-ui-rev/) |
| 260608-pyp | fix remaining 4 phase-17 UI-SPEC warnings | 2026-06-08 | 0cbac58 | [260608-pyp-...](./quick/260608-pyp-fix-remaining-phase-17-ui-spec-warnings-/) |

## Deferred Items

Items acknowledged at v1.2 milestone close (2026-06-08):

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| tech_debt | Extract shared `rocmpolicy` leaf package (PR-#2 finding) | Open | v1.2 close |
| tech_debt | Investigate `rocm-6.4.4-rocwmma` residency FAIL on gfx1151 | Open | v1.2 close |
| tech_debt | Optional re-pin of the drifted `rocm-6.4.4` rolling tag | Open | v1.2 close |
| limitation | Backup cross-host / post-`podman system reset` restore is documented best-effort (UID-remap + SELinux `:Z` repair validated indirectly) | Documented | v1.2 close |
| tech_debt | Phase 21 Info findings IN-01..IN-04 deferred (reasoning-strip robustness, title-newline escaping, timeout-comment, Plan-snapshot regression test) — IN-01/IN-04 strongest follow-up candidates | Open | Phase 21 close |

Items recorded at v1.3 milestone close (2026-06-11, from `milestones/v1.3-MILESTONE-AUDIT.md`):

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| tech_debt | Literal `sudo reboot` durability re-confirmation of the Qdrant `:Z` volume (mechanism proxy-proven, linger enabled) | Open | v1.3 close |
| tech_debt | Restore-side streaming of volume tars (WR-06; backup-side streaming landed) | Open | v1.3 close |
| tech_debt | Drift test binding `qdrantServiceName`/`embedServiceName` (`cmd/villa/install_memory.go:67,73`) to the orchestrate accessors (sole integration-audit WARN) | Open | v1.3 close |
| process | Phase 20 has no formal VERIFICATION.md (covered by 6/6 on-hardware UAT + approved VALIDATION + SECURITY.md) | Documented | v1.3 close |
| process | Nyquist VALIDATION.md left draft for Phases 18–19 (`/gsd-validate-phase 18` / `19`) | Open | v1.3 close |

Items deferred at v1.4 roadmap creation (2026-06-12, research-recorded):

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| v2_scope | CODER-V2-01: Co-resident `villa-coder` Quadlet unit (128 GB fit-gated stretch; design shelf-ready in research/ARCHITECTURE.md) | Deferred | v1.4 roadmap |
| v2_scope | CODER-V2-02: Qdrant/villa-embed MCP semantic code search — only behind a pre-declared numeric eval it must WIN vs grep/LSP | Deferred | v1.4 roadmap |
| v2_scope | CODER-V2-03: Sandboxed/containerized agent profile for untrusted repos | Deferred | v1.4 roadmap |

## Session Continuity

Last session: 2026-06-13T09:57:33.888Z
Stopped at: Phase 25 context gathered
Resume file: .planning/phases/25-coding-mode-render-transactional-swap-verb/25-CONTEXT.md

## Operator Next Steps

- Phase 24 is complete (4/4 plans; CODER-01/02/03 closed, catalog frozen, D-13 toolbox keep recorded). Start the next phase: `/gsd-plan-phase 25` (CMODE — coding-mode unit delta + transactional swap verb; CMODE-01/CMODE-02). Phase 25 consumes the frozen entries' agent_ctx/agent_sampling/cache_reuse_safe and the D-13 decision; the cache_reuse_safe claim is build-9496-scoped (re-probe gate in 24-TOOLBOX-DECISION.md Check 3 if the toolbox is ever re-pinned).
