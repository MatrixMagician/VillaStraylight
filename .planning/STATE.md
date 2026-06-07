---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: Operability
status: ready
stopped_at: Phase 13 (villa doctor) complete & verified (15/15, UAT 3/3 live) — ready to plan Phase 14
last_updated: "2026-06-07T15:55:52.008Z"
last_activity: 2026-06-07
progress:
  total_phases: 6
  completed_phases: 2
  total_plans: 6
  completed_plans: 6
  percent: 33
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-07 after starting v1.2)

**Core value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box. v1.2 extends the bar to "and stays operable, recoverable, and measurable over time."
**Current focus:** Phase 14 — saved-bench-reports + `--compare`

## Current Position

Phase: 14
Plan: Not started
Status: Phase 13 complete & verified (villa doctor; gap-closure 13-03 shipped, UAT 3/3 live) — ready to plan Phase 14
Progress: [██░░░░] 2/6 phases complete (v1.2)
Last activity: 2026-06-07

## v1.2 Build Order (research-converged — preserve)

Seam-locked + composition features first (zero/trivial contract risk), then the two
persistence features with their byte-frozen evolutions staggered (only ONE byte-frozen
contract evolves at a time), then the destructive backup, then the TUI capstone.

1. **Phase 12 — ROCM-ALT-01**: lowest-risk; proven `BackendFor` seam; digest-pin + extend `seam_test.go` regex SAME commit; gate via `rocm-policy.json`; bench-decide plain vs `-rocwmma`.
2. **Phase 13 — DOCTOR-01/02/03**: pure composition of preflight + status + residency proof + drift; OWN golden, do NOT mutate `status.Report`; offload-asserting.
3. **Phase 14 — BENCH-03/04**: freeze new `internal/benchstore` saved-report format FIRST via its own golden; pp/tg never blended; comparability guard.
4. **Phase 15 — USAGE-01/02**: dashboard poll loop is SOLE writer of usage store; reset-aware fold; ONE append-only `status.Report` schema bump + re-freeze golden once.
5. **Phase 16 — BAK-01/02/03**: HIGHEST RISK; exclude model weights; transactional restore mirroring backendswap; `podman volume export/import` behind orchestrate/cmd seam (NOT a new impure module).
6. **Phase 17 — INSTALL-01/02**: capstone TUI; `charmbracelet/huh` v1.0.0 is the ONLY new dep (pins bubbletea v1.3.6 stable); pure presentation; TTY-gated + `--no-tui`; CGO_ENABLED=0 check.

## Research Flags (deeper planning research likely)

- **Phase 12 (ROCM-ALT-01):** re-verify the rolling `rocm-6.4.4` tag digest at impl time (`sha256:c81f30a7…150ec62`; `-rocwmma` variant `sha256:9a97129a…3c0141` is bench-decided). Build step, not research.
- **Phase 15 (USAGE-01):** confirm exact llama.cpp `/metrics` cumulative counter names (`llamacpp:prompt_tokens_total` / `tokens_predicted_total`) + reset semantics against a live `llama-server`; degrade fold to typed-Unknown if a counter is absent.
- **Phase 16 (BAK-01):** validate cross-host / post-`podman system reset` restore (UID-mapping + SELinux `:Z` repair) + decide the Open WebUI live-SQLite quiesce approach. External Podman volume mechanics MEDIUM-confidence.

## Performance Metrics

**Velocity:**

- Total plans completed: 27 (across v1.0 + v1.1)
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

## Accumulated Context

### Roadmap Evolution

- v1.2 Operability roadmap created (2026-06-07): Phases 12–17 mapped from 13 requirements, research-converged build order preserved.
- Phase 11 added (2026-06-06): Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation (v1.1, shipped).

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. Recent v1.2 roadmap decisions:

- [v1.2 Roadmap]: Build order is research-converged (all four researchers + synthesizer agreed) — seam-locked/composition first, only ONE byte-frozen contract evolution in flight at a time, destructive backup BEFORE the TUI front-end. Preserve it.
- [v1.2 Roadmap]: This is an INTEGRATION milestone, not an ecosystem one — five of six features are buildable with stdlib + existing seams. EXACTLY ONE new first-party dependency for the whole milestone: `charmbracelet/huh` v1.0.0 (Phase 17 only, command-tier only).
- [v1.2 Roadmap]: New persistence is flat JSONL/JSON under `$XDG_DATA_HOME/villa/` — NEVER in `config.toml` (stays single-source-of-configuration-truth), NEVER embedded SQLite (CGO breaks the static binary; pure-Go SQLite is a disproportionate burden for append-mostly data).
- [v1.2 Roadmap]: Each feature with decision logic gets ONE new pure `internal/*` core (`doctor`, `benchstore`, `usage`, `backup`); host effects stay behind an `orchestrate`-resident or cmd-tier seam — `orchestrate` remains the ONLY intentionally-impure module.
- [v1.2 Roadmap]: ROCM-ALT-01 never auto-switches; Vulkan stays default; ship the digest the A/B bench proves recovers Δtg −11.15 (never promise an unbenchmarked speed-up).

Earlier (v1.0 / v1.1) decisions retained below.

- [Roadmap]: Inference behind a `Backend` interface from day one; the single `BackendFor()` resolver is the only polymorphism point, fail-closed.
- [Roadmap]: Config is the single source of truth; Quadlet units are derived/regenerated, never hand-edited.
- [Roadmap]: `--json`/dashboard contracts are byte-frozen; evolve append-only + schema bump, re-freeze goldens exactly once.
- [Roadmap]: Offload-asserting — a silent/partial CPU fallback is a FAIL, never a false-green; backend marker literals stay behind the `internal/inference` seam (`TestSeamGrepGate`).
- [Roadmap]: `bench --ab` composes the `backend set` switch — never re-implements switching.
- [v1.1]: ROCm is opt-in; Vulkan RADV stays the default; `recommend` advises, never auto-switches. Digest-pin ROCm images (never the nightlies tag — 64 GB cap).
- [v1.1]: `villa backend set` is transactional (capture→prove→cutover→rollback); is-active/200 alone is never success.
- [Phase ?]: [12-01]: Two additive ROCm 6.4.4 backends (plain + rocwmma) ship as image-parameterized backendROCm variants behind BackendFor; rocm still means 7.2.4 (coexistence); IsROCmFamily is the single ROCm-name enumeration; both digests re-verified live via skopeo 2026-06-07.
- [Phase ?]: D-08 closed: every literal rocm comparison routes through inference.IsROCmFamily (cmd/villa backend.go + preflight.go)
- [Phase ?]: SC#2: preflight.RunROCmForImage threads BackendFor(target).Image() so the policy deny-list evaluates the real digest, not an empty-image WARN
- [Phase ?]: Seam regex anchored to image context so the gate catches image literals but not bare backend-name config values
- [Phase 12 — UAT 2026-06-07, gfx1151]: **Hypothesis DISPROVEN** — `rocm-6.4.4` does NOT recover the v1.1 Δtg −11.15 regression. On-hardware A/B: Vulkan leads tg by ~11.68 tok/s over rocm-6.4.4 (≈ rocm-7.2.4). Vulkan remains the correct tg default; ROCm wins pp slightly. The honest A/B did its job — prove, don't assume. Capability shipped correctly (selectable, gated, residency-proven, benchable); the perf premise it tested is false on this host/model.
- [Phase 12 — UAT]: `rocm-6.4.4-rocwmma` is non-functional on this host/model — residency FAILED (load_tensors hang / CPU-fallback stall) and rolled back verbatim. The offload-asserting FAIL + transactional rollback worked exactly as designed (a real honest FAIL, never a false-green). Ships selectable but does not come up here.
- [Phase ?]: doctor defines its OWN Report (reportSchemaVersion=1); status.Report only READ, never extended (D-02)
- [Phase ?]: doctor worst-wins maps to authoritative shipped preflight tiers — BLOCK/offload FAIL→exit 1, WARN/drift/typed-Unknown→exit 2 (D-04/Pitfall 1, NOT inverted ROADMAP prose)
- [Phase ?]: offload FAIL folded as BLOCK-class FAIL dominating HealthReady — no false-green over a health-200 (Pitfall 3)
- [Phase ?]: villa doctor exit mapping reuses authoritative preflight constants (FAIL=1, drift/WARN=2, healthy=0) — not inverted (D-04)
- [Phase ?]: unitDirReadOnly resolves the Quadlet dir without creating it — doctor is strictly read-only (no MkdirAll/WriteUnits)
- [13-03]: Residency-supersession in doctor.Aggregate — a proven ROCm-family offload StatusPass (IsROCmFamily + OffloadApplies gate) down-ranks the three typed-Unknown ROCm host-prep WARNs (ROCM-PRE-firmware/-hsa/-image) so a healthy opt-in ROCm install reaches exit 0 (closes 13-UAT Test 1 / restores DOCTOR-01).
- [13-03]: No-false-green preserved — the down-rank predicate is the (superseded-ID AND Status==statusWarn) CONJUNCTION; a confident StatusFail on the SAME IDs still folds to FAIL (pure ID-set match forbidden). Findings stay VISIBLE (rank-suppressed, not deleted); no serialized field, schema_version stays 1.
- [13-03]: Only the three structural typed-Unknown checks are superseded; Probe-driven ROCM-PRE-gfx/-kernel are NOT (the supersession is correctly narrow, never over-firing on host-prep signals).
- [13-03]: Option B nil-safe doctor.Deps.RunROCmImage seam — liveDoctorDeps binds preflight.RunROCmForImage(BackendFor(cfg.Backend).Image()) for ROCm-family backends (nil for vulkan) so a denied RUNNING image is a confident FAIL; the image literal stays behind the inference seam (TestSeamGrepGate green).

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

- Worth folding into v1.2 planning (esp. Phase 12): the deferred PR-#2 review findings — extracting a shared `rocmpolicy` leaf package (graphmind memory 10e784d6).

### Blockers/Concerns

[Issues that affect future work]

- Phase 16 (BAK) is the highest-risk feature: rootless-Podman UID-mapping/SELinux mangle, torn live-SQLite snapshots, accidental model-weight sweep, version-skew restore. Mitigate with `podman volume export/import` + transactional discipline + model-weight exclusion + manifest skew WARN.
- Phase 15 (USAGE) must not become telemetry: counts-only, no content, no new outbound, single writer (dashboard poller), loopback-only, bounded growth.
- Phase 13 (DOCTOR) must inherit offload-asserting discipline — a green doctor over a silent CPU fallback is worse than no doctor.
- On-hardware validation remains the dominant verification path (gfx1151). Phases 15 + 16 need live-host confirmation steps.
- [Phase 12 follow-up, non-blocking] `rocm-6.4.4-rocwmma` residency FAIL on gfx1151 — investigate whether it's a bounded-timeout tuning issue (older 8.04 GB / 4-month image) or genuine gfx1151 incompatibility. Ships selectable but does not come up on this host/model.
- [Phase 12 follow-up, non-blocking] Rolling-tag drift: the live `rocm-6.4.4` tag re-pushed to `sha256:44f115e0…` (≠ pinned `c81f30a7…`) same-day. The pin stays valid/reproducible (content-addressed); a future re-pin could capture the newer build if desired.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260604-wh1 | Fix F-3: villa status OFFLOAD WARN — emit -lv 4 residency line + invocation-scoped ResidencyJournal | 2026-06-04 | d401a52 | [260604-wh1-fix-f-3-villa-status-offload-warn-emit-l](./quick/260604-wh1-fix-f-3-villa-status-offload-warn-emit-l/) |
| 260605-d2q | Fix Makefile build target to produce villa binary (repoint build/run to ./cmd/villa, drop legacy web/scaffold targets) | 2026-06-05 | b3a4419 | [260605-d2q-fix-makefile-build-target-to-produce-vil](./quick/260605-d2q-fix-makefile-build-target-to-produce-vil/) |
| 260605-fast | fix(status): render OFFLOAD N/A for non-GPU services in human table (Phase-4 UAT Test 4 cosmetic gap; --json contract unchanged) | 2026-06-05 | e5fc1fc | — |
| 260605-tuv | Fix villa uninstall: drop unsupported podman volume rm --ignore flag (exit 125), surface stderr, tolerate missing volume, add regression tests | 2026-06-05 | 228a4c0 | [260605-tuv-fix-villa-uninstall-drop-unsupported-pod](./quick/260605-tuv-fix-villa-uninstall-drop-unsupported-pod/) |
| 260606-p3a | Fix villa bench single-mode backend label: name the measured backend in human header + --json single.backend (Phase-9 UAT minor gap; --ab + pp/tg-separate contract unchanged) | 2026-06-06 | 8aa9c90 | [260606-p3a-fix-villa-bench-single-mode-backend-labe](./quick/260606-p3a-fix-villa-bench-single-mode-backend-labe/) |

## Deferred Items

Items acknowledged at milestone close on 2026-06-06 (v1.1):

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| quick_task | 260606-p3a-fix-villa-bench-single-mode-backend-label | Complete (commit 8aa9c90; task-status frontmatter reads `unknown` — tag lag only, work is done and in Quick Tasks Completed) | v1.1 close |

## Session Continuity

Last session: 2026-06-07T16:10:00.000Z
Stopped at: Completed 13-03-PLAN.md (Phase 13 plans complete)
Resume file: None

## Operator Next Steps

- **Phase 12 complete** ✓ — ROCM-ALT-01 shipped, verified (UAT 7/7), and secured (10/10 threats closed). On-hardware verdict: capability works; `rocm-6.4.4` does NOT recover Δtg — Vulkan stays the tg default (never auto-switched, as designed).
- **Plan the next phase:** `/gsd-discuss-phase 13` then `/gsd-plan-phase 13` (`villa doctor` — pure composition of preflight + status + residency proof + drift; OWN golden, do NOT mutate `status.Report`; offload-asserting).
- Consider folding the deferred PR-#2 finding (shared `rocmpolicy` leaf package, graphmind memory 10e784d6) into a later phase.
