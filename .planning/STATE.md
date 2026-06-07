---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: Operability
status: executing
stopped_at: Phase 12 Plan 03 off-hardware tasks complete; on-hardware UAT (Task 4) pending operator
last_updated: "2026-06-07T11:20:00.000Z"
last_activity: 2026-06-07 -- Phase 12 Plan 03 autonomous tasks executed (--ab-target); on-hardware checkpoint reached
progress:
  total_phases: 6
  completed_phases: 0
  total_plans: 3
  completed_plans: 2
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-07 after starting v1.2)

**Core value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box. v1.2 extends the bar to "and stays operable, recoverable, and measurable over time."
**Current focus:** Phase 12 — rocm-6-4-4-alternate-backend

## Current Position

Phase: 12 (rocm-6-4-4-alternate-backend) — EXECUTING (Plan 03 off-hardware done; on-hardware UAT pending)
Plan: 3 of 3
Status: AWAITING on-hardware UAT — Plan 03 Task 4 (checkpoint:human-verify, gate=blocking-human). ROCM-ALT-01 stays OPEN.
Progress: [░░░░░░] 0/6 phases complete (v1.2)
Last activity: 2026-06-07 -- Phase 12 Plan 03 `--ab-target` (Option A) implemented + tested off-hardware; on-hardware switch/residency/Δtg checkpoint reached

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

- Total plans completed: 21 (across v1.0 + v1.1)
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

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
| Phase 12 P01 | 15 | 3 tasks | 6 files |
| Phase 12 P02 | ~20 min | 2 tasks | 11 files |
| Phase 12 P03 | ~20 min | 3 of 4 tasks (Task 4 on-hardware pending) | 4 files |

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

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

- Worth folding into v1.2 planning (esp. Phase 12): the deferred PR-#2 review findings — extracting a shared `rocmpolicy` leaf package (graphmind memory 10e784d6).

### Blockers/Concerns

[Issues that affect future work]

- Phase 16 (BAK) is the highest-risk feature: rootless-Podman UID-mapping/SELinux mangle, torn live-SQLite snapshots, accidental model-weight sweep, version-skew restore. Mitigate with `podman volume export/import` + transactional discipline + model-weight exclusion + manifest skew WARN.
- Phase 15 (USAGE) must not become telemetry: counts-only, no content, no new outbound, single writer (dashboard poller), loopback-only, bounded growth.
- Phase 13 (DOCTOR) must inherit offload-asserting discipline — a green doctor over a silent CPU fallback is worse than no doctor.
- On-hardware validation remains the dominant verification path (gfx1151). Phases 15 + 16 need live-host confirmation steps.

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

Last session: 2026-06-07T11:20:00.000Z
Stopped at: Phase 12 Plan 03 — off-hardware tasks complete (commits 9fcd5d3, c049e31); on-hardware UAT (Task 4, checkpoint:human-verify) pending operator on the gfx1151 host.
Resume: run the on-hardware UAT steps in `.planning/phases/12-rocm-6-4-4-alternate-backend/12-03-SUMMARY.md` → "On-Hardware Checkpoint", then reply "on-hardware verified" with the residency-proof result + bench Δtg figures to close ROCM-ALT-01 / Phase 12.

## Operator Next Steps

- **v1.2 roadmap created** ✓ — 13/13 requirements mapped to Phases 12–17; research-converged build order preserved; STATE + REQUIREMENTS traceability updated.
- **Plan the first phase:** `/gsd-plan-phase 12` (`rocm-6.4.4` Alternate Backend). Re-verify the rolling-tag digest at implementation time; extend `seam_test.go`'s image regex in the same commit.
- Consider folding the deferred PR-#2 finding (shared `rocmpolicy` leaf package, graphmind memory 10e784d6) into Phase 12 planning.
