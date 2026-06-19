---
gsd_state_version: 1.0
milestone: v1.5
milestone_name: Web Search
current_phase: 31
current_phase_name: grounded-fetch-embed-grounding
status: executing
stopped_at: "Phase 30 COMPLETE — on-hardware UAT PASSED (SC#2 grounded via D-06 BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL direct-inject fix; SC#3 no-fabrication). Ready to plan Phase 31."
last_updated: "2026-06-19T17:13:57.510Z"
last_activity: 2026-06-19
last_activity_desc: Phase 31 execution started
progress:
  total_phases: 6
  completed_phases: 2
  total_plans: 9
  completed_plans: 6
  percent: 33
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-18 — milestone v1.5 Web Search started; SearXNG moved from Out of Scope to Active)

**Core value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box. v1.2 extended the bar to "and stays operable, recoverable, and measurable over time." v1.3 extended it to "and remembers the user and their documents across chats — strictly local." v1.4 extended it to "and gives the operator a strictly-local terminal coding agent, wired to a fit-guarded coding model." v1.5 extends it to "and can ground answers in live web search when the operator opts in — accurate, up-to-date, with malicious-site prompt injection defended-in-depth and outbound provably bounded."
**Current focus:** Phase 31 — grounded-fetch-embed-grounding

## Current Position

Phase: 31 (grounded-fetch-embed-grounding) — EXECUTING
Plan: 2 of 4
Status: Ready to execute
Last activity: 2026-06-19 — Phase 31 execution started

## Performance Metrics

**Velocity:**

- Total plans completed: 102 (v1.0 + v1.1: 48; v1.2: 19; v1.3: 20)
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
| 25 | 2 | - | - |
| 27 | 6 | - | - |
| 29 | 3 | - | - |

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
| Phase 25 P01 | 35min | 3 tasks | 11 files |
| Phase 25 P02 | ~40min | 2 of 3 tasks (2 auto + 1 on-hardware checkpoint OPEN) | 5 files |
| Phase 26 P01 | ~22min | 3 tasks (TDD) | 8 files |
| Phase 27 P27-01 | ~40 min | 3 tasks | 10 files |
| Phase 27 P27-03 | ~5 min | 2 tasks (Task 1 TDD) | 3 files |
| Phase 27 P27-05 | ~4 min | 2 tasks (TDD; gap-closure CR-01+WR-05) | 5 files |
| Phase 27 P27-06 | ~12 min | 3 tasks (TDD; gap-closure WR-01+WR-06) | 6 files |
| Phase 28 P02 | 30m | 2 tasks | 11 files |
| Phase 28 P03 | ~40 min | 2 tasks | 9 files |
| Phase 31 P01 | ~25m | 3 tasks | 6 files |

## Accumulated Context

### Roadmap Evolution

- **v1.5 Web Search roadmap created (2026-06-18): Phases 29–34 mapped from 19 requirements** (SRCH/GROUND/GUARD/PRIV/SURF); numbering CONTINUES from v1.4 (last phase 28). Structure follows the research-reconciled six-phase build order verbatim: SearXNG service FIRST (29), OWUI native-search wiring (30), grounded fetch → embed grounding via `villa-websafe` + SSRF + ephemeral collection + ctx-fit (31), villa injection guard layer — sanitize/normalize/fence/classify (32), egress-bounding + `villa verify search` inverse-framed negative-control-first (33), surfacing + the single `status.Report` 4→5 contract bump LAST (34). Coverage: Phase 29 (SRCH-01/04), 30 (SRCH-02/03), 31 (GROUND-01/02/03 + GUARD-01/05), 32 (GUARD-02/03/04), 33 (PRIV-07/08/09), 34 (SURF-04/05/06/07). 100% coverage, no orphans, no duplicates. **Research flags:** Phases 31 (fetch path / OWUI external-loader contract + SSRF), 32 (adversarial injection corpus + must-WIN precision/recall eval), 33 (rootless-netns nft inverse-framing mechanics) need `/gsd-plan-phase --research-phase`; 29/30/34 follow established v1.3/v1.4 patterns.
- v1.4 Coding Agent roadmap created (2026-06-12): Phases 24–28 mapped from 17 requirements; shipped 2026-06-15, audit `tech_debt`.
- v1.3 Memory & Knowledge roadmap created (2026-06-09): Phases 18–23 mapped from 22 requirements; shipped 2026-06-11, audit PASSED.
- v1.2 Operability roadmap created (2026-06-07): Phases 12–17 mapped from 13 requirements, research-converged build order preserved.
- Phase 11 added (2026-06-06): Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation (v1.1, shipped).

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. v1.5 roadmap decisions (research-grounded, frozen into success criteria):

- [v1.5 Roadmap]: **Integrate, not rebuild — exactly ONE new search container.** SearXNG (digest-pinned, on `villa.network`, container-DNS-only, no host port) wired into OWUI's *native* web search; the fetch → chunk → embed → retrieve → cite pipeline reuses the shipped v1.3 villa-embed/Qdrant RAG **verbatim** (no new embedder, vector DB, or citation plumbing — OWUI's top-level `sources` field). The differentiator is the guard layer in front of the embed, not the embed itself.
- [v1.5 Roadmap]: **`villa-websafe` is the sole producer of `page_content`** via OWUI's released `WEB_LOADER_ENGINE=external` + `EXTERNAL_WEB_LOADER_URL` seam — a tiny Go HTTP service over a pure, unit-testable `internal/websafe` core (network fetch injected as a `Deps` func), keeping "orchestrate is the only intentionally-impure module." Every byte embedded or shown to the model passes through it (fetch → sanitize → normalize → fence → classify), with ZERO OWUI rebuild.
- [v1.5 Roadmap]: **Two governing claims, never inverted.** *"Outbound is bounded"* is provable and IS proven — `villa verify search` clones the v1.4 verify-agent four-layer nft/netns harness but **inverse-framed** (off-allowlist canary reachable *unguarded*, blocked *under* the bound; an ineffective block is REJECTED, never a fabricated PASS). *"Safe from injection" is NOT solvable and is NEVER claimed* — the guard reduces/flags only (combined defenses ~73%→~9%, never to zero); the browser-side markdown-image exfil channel is **documented as a known residual**, not closed. Grep-ban "injection-safe" copy.
- [v1.5 Roadmap]: **Classifier is a heuristic Go tripwire, not a model — for v1.5.** PromptGuard is DeBERTa (cannot run on `llama-server`); the rules-vs-model decision is settled to heuristic-rules-for-v1.5. The DeBERTa/PromptGuard sidecar is deferred as GUARD-V2-01 behind a pre-declared must-WIN precision/recall eval (it would add a Python runtime/container, breaking the single-static-binary discipline). The guard is pure Go, CGO-free (`bluemonday` StrictPolicy is the non-negotiable core).
- [v1.5 Roadmap]: **Strip/normalize/fence BEFORE the classifier; normalize invisible/bidi/zero-width/homoglyph Unicode BEFORE fencing.** Fences are one layer (a hint, not a boundary) — defense-in-depth + Unicode normalization + least privilege; the egress bound is the real backstop that contains exfil even on a guard miss.
- [v1.5 Roadmap]: **Fetched web content lives in a dedicated ephemeral Qdrant collection** (clean-replace / bounded lifetime), NEVER the operator's durable memory/document-KB store — untrusted ephemeral content must not poison long-term recall (GROUND-02). `recommend` reserves a web-search ctx budget (result-count × page-size) BEFORE the chat fit so a search-enabled envelope cannot silently CPU-fall-back; residency is offload-asserted under search load (GROUND-03).
- [v1.5 Roadmap]: **Opt-in / default-OFF; off-render byte-identical to v1.4.** Web search is an explicit addon (PRIV-07, mirrors the v1.4 coding agent); the zero-outbound install is unchanged when off. OWUI's lazy/background outbound (HF pulls, telemetry) is killed (`HF_HUB_OFFLINE` + telemetry kill switches) and any web-search weights pre-staged, so the only sanctioned runtime outbound is SearXNG upstreams + result-page fetches (PRIV-09).
- [v1.5 Roadmap]: **EXACTLY ONE byte-frozen contract evolves** — `status.Report` 4→5, append-only `web_search` block, single golden re-freeze, landed LAST in Phase 34. `doctor` owns its own schema bump (independent). `recommend`'s ctx-reservation evolution (Phase 31) is a separate contract landing once where the fit math changes. The **outbound-bounded indicator derives from the real `villa verify search` result, never a bare config bool** (SURF-04).
- [v1.5 Roadmap]: **SSRF guard is a property of the fetcher** (Phase 31) — resolve-and-validate target IP (reject loopback / link-local / `169.254.169.254` / internal `villa-*` hosts), re-check after every redirect, http(s)-scheme allowlist; the netns/firewall allowlist is the backstop. **No agentic browsing, no link-following beyond depth=1, no cloud search/extraction APIs** (read-only fetch+embed+cite only).

Earlier (v1.0–v1.4) standing decisions retained:

- [Roadmap]: Inference behind a `Backend` interface; the single `BackendFor()` resolver is the only polymorphism point, fail-closed.
- [Roadmap]: Config is the single source of truth; Quadlet units (and agent config) are derived/regenerated, never hand-edited.
- [Roadmap]: `--json`/dashboard contracts are byte-frozen; evolve append-only + schema bump, re-freeze goldens exactly once.
- [Roadmap]: Offload-asserting — a silent/partial CPU fallback is a FAIL, never a false-green; backend marker literals stay behind the `internal/inference` seam (`TestSeamGrepGate`); managed-service image literals stay behind `internal/orchestrate`.
- [v1.1]: ROCm is opt-in; Vulkan RADV stays the default; `recommend` advises, never auto-switches. Digest-pin all images (never floating/nightly tags).
- [v1.2]: New persistence is flat JSONL/JSON under `$XDG_DATA_HOME/villa/` — never `config.toml`, never embedded SQLite; each decision-logic feature gets ONE new pure `internal/*` core; orchestrate stays the only intentionally-impure module.
- [v1.3]: Zero-outbound is proven at runtime, negative-control-first — never flag-trusted (`villa verify memory` precedent extends to `villa verify search`). Reservation-before-fit in `recommend.Pick()`; OWUI memory/RAG wiring is env-only with `ENABLE_PERSISTENT_CONFIG=False` mandatory.
- [v1.4]: Agent egress proven negative-control-first under a real rootless-netns nft FORWARD block; the verify harness lives in the rootless netns (pasta proxies container egress), NOT the host main netns — the v1.5 inverse-framed `villa verify search` clones this exact mechanism.
- [Phase ?]: Phase 31-01: internal/websafe pure fetch core — net.Dialer.Control connect-time IP validation (TOCTOU-safe SSRF) + per-hop CheckRedirect; network injected via Deps{Client} seam; OWUI route fixed at /load; always-200 partial array for OWUI raise_for_status; stdlib only (bluemonday deferred to Phase 32).

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

- Carryover tech debt (non-blocking, from v1.2 close): extract a shared `rocmpolicy` leaf package (deferred PR-#2 finding, graphmind memory 10e784d6); investigate `rocm-6.4.4-rocwmma` residency FAIL on gfx1151; optional re-pin of the drifted `rocm-6.4.4` rolling tag.
- Re-verify exact OWUI env-var names at the chosen `@sha256` before Phase 30/31: both `ENABLE_WEB_SEARCH`/`WEB_SEARCH_ENGINE` and the older `ENABLE_RAG_WEB_SEARCH`/`RAG_WEB_SEARCH_ENGINE` families have existed; confirm the `external` loader + `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` (research gap, SUMMARY.md → Gaps to Address).

### Blockers/Concerns

[Issues that affect future work]

- v1.5 research gap (non-blocking, resolve at execution): exact OWUI env-var names + the `external` loader contract must be pinned and re-verified at the chosen OWUI `@sha256` before Phase 30/31 (env-name churn). On-hardware result-count × page-size ctx caps that keep the chat model GPU-resident under search load are unmeasured → reserve conservatively in `recommend` (Phase 31) and offload-assert under search load (Phase 33/34).
- v1.4 carryover (non-blocking): the 27-04 `villa install` backend-revert bug is RESOLVED in v1.4 (commit `4424f13`, guard at `install.go:444-446`); the Phase-28 Nyquist gap is RESOLVED (`28-VALIDATION.md` reconstructed 2026-06-15). Phase-27 fail-safe advisories remain open (egress-probe exit-22 classification, shared-residency "no coder fits" copy, `runProbeCurlCode` real-exec test).
- v1.3 audit-recorded debt (non-blocking): literal reboot durability re-confirmation; restore-side tar streaming (WR-06); install service-name drift test; formal 20-VERIFICATION.md; Nyquist finalization for Phases 18–19.

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
| 260615-ipn | Add drift-guard test tying crush.json provider port to inference.ServerPort() (v1.4 audit WARN) | 2026-06-15 | 6eda867 | [260615-ipn-...](./quick/260615-ipn-add-drift-guard-test-tying-crush-json-pr/) |
| 260615-qmr | Add README prerequisites section documenting Strix Halo kernel parameters | 2026-06-15 | a87bb56 | [260615-qmr-...](./quick/260615-qmr-add-readme-prerequisites-section-documen/) |
| 260618-dvb | Correct shipped-milestone records (v1.0→v1.4) in CLAUDE.md + MILESTONES.md (v1.2 stale note, v1.1 tag date) | 2026-06-18 | 8110598 | [260618-dvb-...](./quick/260618-dvb-commit-doc-correction-edits-claude-md-mi/) |

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

Items acknowledged at v1.4 milestone close (2026-06-15, from `milestones/v1.4-MILESTONE-AUDIT.md`) — audit `tech_debt`, no blockers:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| bug | 27-04: `villa install` reverts a persisted `backend=rocm` to Vulkan | RESOLVED (commit `4424f13`, in v1.4; guard at `install.go:444-446` via `inference.IsROCmFamily`, covered by `TestInstallPreservesPersistedROCmBackend`) | v1.4 close |
| process | Phase 28 has no `28-VALIDATION.md` | RESOLVED (`28-VALIDATION.md` reconstructed 2026-06-15, `nyquist_compliant: true`, 0 gaps) | v1.4 close |
| artifact | Pre-close audit: Phase 28 `28-UAT.md` status `passed`, 0 pending scenarios (benign) | Documented | v1.4 close |
| artifact | Pre-close audit: quick task `260615-ipn` status `unknown` — RESOLVED in commit 6eda867 (frontmatter tag lag only) | Documented | v1.4 close |
| advisory | Phase 27 fail-safe advisories (non-blocking): egress-probe exit-22 infra-FAIL classification; shared-residency "no coder fits" copy; `runProbeCurlCode` real-exec case untested | Open | v1.4 close |
| by_design | W-1 (cosmetic): `villa uninstall` leaves config.toml untouched (D-10) → post-uninstall `status`/`doctor` surface an honest typed-Unknown | Documented | v1.4 close |

Items deferred at v1.5 roadmap creation (2026-06-18, research-recorded):

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| v2_scope | GUARD-V2-01: Model-based injection classifier (PromptGuard/DeBERTa sidecar) — only behind a pre-declared must-WIN precision/recall eval vs the v1.5 heuristic baseline (adds a Python runtime/container, breaks single-static-binary) | Deferred | v1.5 roadmap |
| v2_scope | SRCH-V2-01: Focus modes (Academic/News/Reddit) + time-range filtering beyond result-count tuning | Deferred | v1.5 roadmap |
| v2_scope | SRCH-V2-02: Embedding-based reranking of fetched results (ties to deferred RAG-Q-01 reranker — third resident model / latency on a constrained envelope) | Deferred | v1.5 roadmap |
| v2_scope | SRCH-V2-03: Multi-round "deep research / quality mode" (iterative search→read→refine) | Deferred | v1.5 roadmap |
| residual | Browser-side markdown-image zero-click exfil channel — bypasses container egress (operator's browser renders the image); documented as a known residual + mitigated where feasible (CSP/same-origin), NOT claimed closed (GUARD-04) | Documented | v1.5 roadmap |

## Session Continuity

Last session: 2026-06-19T17:13:37.009Z
Stopped at: Phase 30 COMPLETE — on-hardware UAT PASSED (SC#2 grounded via D-06 BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL direct-inject fix; SC#3 no-fabrication). Ready to plan Phase 31 (Grounded Fetch → Embed Grounding).
Resume file: .planning/phases/30-owui-native-search-wiring/30-VERIFICATION.md

## Operator Next Steps

- **Phase 29 on-hardware UAT — PASSED (2026-06-18):** `./villa install` with `web_search_enabled=true` printed `search service ready: real format=json query returned 10 result(s)`; `podman inspect villa-searxng` → `PortBindings={}` (no host port). Security review complete (`SECURITY.md`, 14/14 threats closed). Phase 29 marked complete. Next: `/gsd-plan-phase 30`.
- SRCH-04 engine allowlist (shipped as proposed): `duckduckgo, brave, wikipedia, wikidata` — the auditable outbound surface Phase 33 cross-checks. Adjust in `internal/orchestrate/searxng.go` (`keep_only`) if desired before UAT.
- Forward deps noted by review/verify (NOT Phase 29 gaps): WR-01 the opt-in enable path is later-phase (SRCH-02/Phase 30, PRIV-07/Phase 33); WR-02 container-entrypoint `$SEARXNG_SECRET` substitution is what the live UAT confirms.
- Research-phase flag reminder: Phases 31, 32, 33 warrant `/gsd-plan-phase --research-phase` (fetch path/SSRF; injection corpus + must-WIN eval; rootless-netns nft inverse-framing)
