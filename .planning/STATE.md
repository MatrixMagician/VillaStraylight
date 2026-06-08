---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: Operability
status: executing
stopped_at: Phase 17 UI-SPEC approved
last_updated: "2026-06-08T16:48:07.714Z"
last_activity: 2026-06-08 -- Phase 17 execution started
progress:
  total_phases: 6
  completed_phases: 5
  total_plans: 19
  completed_plans: 18
  percent: 83
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-07 after starting v1.2)

**Core value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box. v1.2 extends the bar to "and stays operable, recoverable, and measurable over time."
**Current focus:** Phase 17 — guided-tui-install-capstone

## Current Position

Phase: 17 (guided-tui-install-capstone) — EXECUTING
Plan: 3 of 3
Status: Ready to execute
Progress: [██████████] 100% (plans) — phase-gate UAT remains
Last activity: 2026-06-08 -- Phase 17 execution started

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

- Total plans completed: 44 (across v1.0 + v1.1)
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
- [14-01]: benchstore SavedReport JSONL contract frozen (savedReportSchemaVersion=1, schema_version LAST field, record.golden frozen BEFORE any live writer); pp/tg persist as SEPARATE fields, no blended key; VoidExhausted/Reason round-trip (BENCH-03).
- [14-01]: Comparable iff model+quant+ctx+host match; backend DELIBERATELY allowed to differ (cross-backend compare is the point); UNKNOWN host (HostGfxID=="") => not comparable (no false-equal). benchstore imports NEITHER inference NOR detect (SeamGrepGate green) (BENCH-04).
- [14-01]: SavedSpec PERSISTS the fixed benchPrompt reproducibility constant — the saved record is a SUPERSET of `bench --json` (no prompt key there) for reproducibility; the value is an in-repo constant, never user content (T-14-02). Compare reads the primary measured side (Single for single-mode, AB.B for ab); not-comparable folded into CompareResult.Comparable=false with zero deltas.
- [14-02]: BENCH-03 write-hook fires in runBench AFTER render on BOTH exitPass and exitWarn paths (persist-always A5 — void-exhausted runs still recorded). The write is loud-but-non-fatal: a benchstore error is a stderr WARN that NEVER changes the measurement's exit code (T-14-05). Single persists mode=single, --ab persists ONE mode=ab record.
- [14-02]: Fingerprint captured at the cmd tier from config (model/quant/ctx) + `.Known`-guarded detect.Probe() (host gfx/kernel) — UNKNOWN host fact serializes to the empty sentinel, never fabricated (T-14-04); benchstore receives plain strings and imports no detect (SeamGrepGate green). For --ab the fingerprint backend axis is res.AB.From (presentation only, not a comparability blocker).
- [14-02]: liveBenchstoreDeps append seam: assert-inside-dir → MkdirAll 0700 → OpenFile(O_APPEND|O_CREATE|O_WRONLY, 0600) → Write (never write-whole-file/truncate); ReadAll returns (nil,nil) on absent store (wired now for Plan 03). Path + traversal guard re-resolved as LOCAL cmd-tier copies (benchstore's are unexported).
- [14-03]: BENCH-04 read-only `villa bench --compare`/`--list` (new cmd/villa/bench_compare.go). runBenchCompare loads via benchstore.Load, auto-selects the two most-recent comparable reports (selectComparePair, A8 v1), runs the pure Compare guard. Exit mapping: comparable→0, not-comparable→2, <2 reports→1 (remediation) — IDENTICAL in --json mode (not-comparable returns 2 even though comparable:false was emitted; no-false-green T-14-04). Flag-exclusivity rejects --ab/--ab-target/--reps/--warmup/--n-predict + --compare&&--list at the cobra boundary (read-only enforced T-14-06). Void side is advisory: a comparable pair with a void side STILL prints the delta + exits 0 but flags that side not-authoritative (RESEARCH Q3/A5). --compare --json golden (cmd/villa/testdata/bench-compare.json.golden) frozen: comparable+void AND not-comparable cases; pp/tg deltas SEPARATE keys, a_void_exhausted/b_void_exhausted flags, no blended key.
- [Phase ?]: 15-02: CounterSample sibling + ScrapeCounters reuse the bounded /metrics scrape (no second request); absent counter => Known=false, never a fabricated 0 (D-05/D-06)
- [Phase ?]: Plan 15-03: status.Report evolved by ONE append-only Usage *usage.UsageTotals omitempty field; reportSchemaVersion 1->2; CLI reads usage.json read-only (D-07 sole-writer)
- [Phase ?]: 15-04: dashboard /api/metrics is the SOLE usageMu-guarded writer of usage.json; in-section model identity (D-07/Pitfall 2)
- [Phase ?]: 15-04: dashboard surfaces cumulative totals via the SAME status.Report.usage field (no new endpoint, D-10); typed-Unknown muted UI, never a fabricated 0
- [Phase ?]: Backup manifest carries store schema versions as plain ints; real usage/bench values supplied by cmd-tier via Plan-02 accessors (16-01).
- [Phase ?]: Build-stamped villa version via -ldflags -X main.version from git describe; backup manifest villa_version source (16-01, D-09).
- [16-02]: villa backup ships (BAK-01) — pure Backup() over Deps does quiesce(stop OWUI)->podman volume export->assemble single .tar(manifest+config+volume+usage+single bench-reports.jsonl)->defer best-effort restart (D-05); model weights excluded, identities recorded for re-pull.
- [16-02]: podman volume I/O is a SHARED cmd-tier fixed-arg seam (cmd/villa/podman_volume.go: volumeExportArgs/volumeImportArgs/podmanVolume/requirePodman) cloned from uninstall.go podmanVolumeRm — NO new impure module (D-02); internal/backup stays exec-free (SeamGrepGate green).
- [16-02]: store schema versions sourced via NEW exported accessors usage.SchemaVersion()/benchstore.SavedReportSchemaVersion() (mirror-guarded vs the unexported consts) so the manifest can never desync; OWUI volume name via orchestrate.OpenWebUIVolumeName(); both image digests seam-sourced (no literal — D-10).
- [16-02]: archive 0600, output traversal-guarded against its parent, corrupt partial removed on failed write; default name villa-backup-<timestamp>.tar (FS-safe, no ':'). --json deferred (D-13). On-hardware gfx1151 round-trip UAT deferred to phase gate.
- [16-03]: villa restore ships (BAK-02/BAK-03) — pure Restore() clones backendswap.Run: read+verify (fail-closed BLOCK on checksum mismatch / incompatible manifest schema, ZERO side effects) -> WARN-and-confirm skew (--yes/--force bypass) -> capture-before-mutate -> quiesce -> clean-recreate-before-import (VolumeRm not-found-tolerant -> ReconcileAndWrite Quadlet recreate from restored config -> EnsureVolume explicit create -> VolumeImport) on apply AND rollback -> offload-asserting prove -> verbatim rollback with honest complete/incomplete reporting.
- [16-03]: clean-recreate-before-import is the load-bearing fix (podman volume import MERGES + does NOT auto-create, RESEARCH HIGH) — stale chats/webui.db never leak; a test asserts VolumeRm<ReconcileAndWrite<EnsureVolume<VolumeImport on the forward path and a 2nd clean-recreate on rollback re-importing the CAPTURED tar.
- [16-03]: config restored via config.SaveVilla (new config.Parse([]byte) turns the archive's config.toml into the source-of-truth VillaConfig); data-dir artifacts via atomic usage.WriteFileAtomic 0600/0700; liveRestoreProve composes ROCm preflight + the proven Phase-8 liveProve residency assert (health-200-but-residency-FAIL -> non-pass -> rollback); no new outbound (D-12). internal/backup stays exec/inference/detect-free (SeamGrepGate green). On-hardware UAT (clean-recreate-no-merge / same-host round-trip / live-SQLite quiesce / cross-version skew / cross-host best-effort) deferred to phase gate.
- [Phase ?]: Pinned huh/lipgloss/termenv direct; bubbletea v1.3.6 stays indirect to avoid v2 leak (D-11)

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

Last session: 2026-06-08T16:48:00.897Z
Stopped at: Phase 17 UI-SPEC approved
Resume file: .planning/phases/17-guided-tui-install-capstone/17-UI-SPEC.md

## Operator Next Steps

- **Phase 12 complete** ✓ — ROCM-ALT-01 shipped, verified (UAT 7/7), and secured (10/10 threats closed). On-hardware verdict: capability works; `rocm-6.4.4` does NOT recover Δtg — Vulkan stays the tg default (never auto-switched, as designed).
- **Plan the next phase:** `/gsd-discuss-phase 13` then `/gsd-plan-phase 13` (`villa doctor` — pure composition of preflight + status + residency proof + drift; OWN golden, do NOT mutate `status.Report`; offload-asserting).
- Consider folding the deferred PR-#2 finding (shared `rocmpolicy` leaf package, graphmind memory 10e784d6) into a later phase.
