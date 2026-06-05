---
gsd_state_version: 1.0
milestone: v1.1
milestone_name: ROCm Opt-In Backend
status: Roadmapped — ready for `/gsd-plan-phase 6`
stopped_at: Phase 6 context gathered
last_updated: "2026-06-05T22:18:01.390Z"
last_activity: 2026-06-05 — v1.1 roadmap created (Phases 6–10, 13/13 requirements mapped)
progress:
  total_phases: 5
  completed_phases: 4
  total_plans: 23
  completed_plans: 22
  percent: 80
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-03)

**Core value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box.
**Current focus:** Phase 05 — control-dashboard

## Current Position

Phase: 05 (control-dashboard) — EXECUTING
Plan: 1 of 8
Status: Phase 05 verified (passed) — shipped as PR #1 (pr/villastraylight-v1 → main)
Last activity: 2026-06-05 -- Shipped VillaStraylight v1 — PR #1 (squashed clean import, Phases 1-5)

Progress: [████████░░] 80% (4 of 5 phases complete)

## Performance Metrics

**Velocity:**

- Total plans completed: 12
- Average duration: 35 min
- Total execution time: 1.75 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 3 | - | - |
| 02 | 3 | - | - |
| 4 | 3 | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
| Phase 03 P01 | 5 min | 3 tasks | 12 files |
| Phase 03 P02 | 18 min | 3 tasks | 8 files |
| Phase 03 P03 | 8 min | 2 tasks | 10 files |
| Phase 03 P04 | 8 min | 2 tasks | 9 files |
| Phase 03 P05 | 4 min | 1 task | 2 files |
| Phase 03 P06 | 8 min | 1 tasks | 3 files |
| Phase 04 P01 | 12 min | 2 tasks | 8 files |
| Phase 04 P02 | 3 min | 2 tasks | 2 files |
| Phase 04 P03 | 14 min | 3 tasks | 4 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- [Roadmap]: Risk-ordered phases — pure core (detect/recommend/preflight) first, then early GPU-offload validation slice before orchestration.
- [Roadmap]: Inference behind a `Backend` interface from day one (Vulkan v1; ROCm/Metal later) — pluggability checkpoint at Phase 2 review.
- [Roadmap]: Config is the single source of truth; Quadlet units are derived/regenerated, never hand-edited.
- [01-01]: Usable memory envelope = `mem_info_gtt_total` (GTT-then-ttm fallback), never `MemTotal` — verified live (62.5 GiB GTT vs 128 GiB RAM). OOM guard enforced by test.
- [01-01]: Backend-neutrality seam — all Vulkan/ROCm/DRI probing funneled through `gpu_amd.go` (`probeGPU()`); the Phase 2 Backend interface slots in there.
- [01-01]: Typed `Unknown` degradation (value.go) is the contract spine — every probe returns Known/Unknown, never bare 0/panic; `Raw` captured for `-v`, never serialized.
- [01-01]: `HostProfile` `--json` shape is frozen by a byte-for-byte golden test (the Phase 5 dashboard contract).
- [01-02]: `Recommendation` `--json` shape is frozen by a golden test — the second Phase 5 dashboard contract; fit math (`model_bytes + KV@ctx + headroom ≤ usable_envelope`) is SHOWN, not just applied (D-06).
- [01-02]: Best pick = the largest auto-eligible model that fits; `unified_memory_safe:false` and `bootstrap` entries are never auto-selected (REC-02/D-12); a `--model` override of an unsafe entry is allowed but warns loudly (D-07).
- [01-02]: Unknown envelope → conservative 50%-of-RAM floor + `Degraded` flag; refuse (empty Model) only when RAM is also unknown — never guess high (D-14).
- [01-02]: Config is TOML at `$XDG_CONFIG_HOME/villa/config.toml`, read-only by default; `recommend --save` is the only writer (0600, traversal-guarded). BurntSushi/toml chosen (no viper, D-17).
- [01-02]: Catalog seed dims (weight_bytes/n_layers/n_kv_heads/head_dim) are MEDIUM-confidence placeholders (A2) — replace with real GGUF metadata in Phase 2; headroom fraction (~12%, A3) to be validated by a Phase-2 dry-load.
- [01-03]: `internal/preflight` is a REUSABLE library — pure `Run(HostProfile) []CheckResult`, zero os.Exit/print (D-18); exit-code mapping (0/2/1) + `--force` override summary live only in `cmd/villa/preflight.go`. Phase 3 install reuses it via `RunWithResources`.
- [01-03]: Tier (BLOCK/WARN) vs Status (PASS/WARN/FAIL) split; an unevaluable BLOCK downgrades to WARN, never a false hard block (D-15). subuid/subgid matched on BOTH username AND numeric UID (Pitfall 6).
- [01-03]: Kernel/Mesa/firmware floors externalized as data in `floors.go` (A1); firmware-date probe deferred (PRE-07 is an advisory WARN until a Phase 2/3 probe enables a real comparison).
- [Phase ?]: [03-01]: internal/orchestrate is the ONLY impure module; Render/Reconcile pure, only WriteUnits/systemd.go touch the host. Backend literals parsed out of ContainerArgs/Image at render time (never retyped) so the inference seam grep-gate stays green.
- [Phase ?]: [03-02]: villa install is the managed bring-up — detect→recommend→preflight gate→per-step consented host-prep→render→reconcile→write→reload→start→503-aware readiness poll; idempotent + --dry-run; host-touching actions behind an injectable installDeps seam.
- [Phase ?]: [03-02]: PRE-05 SELinux container_use_devices BLOCK-tier (off→silent CPU fallback); PRE-03 linger WARN-tier consented offer (never blocks if declined); --force degrades verdict to WARN. Readiness 503=keep-polling, timeout=WARN, never a confident PASS/FAIL on a loading server (WR-07).
- [Phase ?]: [03-03]: lifecycle verbs (up/down/restart/logs/config) reuse the orchestrate core + installDeps pattern; known-service set DERIVED from rendered stack (serviceUnits maps .container→.service), validated before any seam fires (T-03-11).
- [Phase ?]: [03-03]: up unchanged-config is a true no-op (zero writes/restarts); restart reconciles-first so a config edit lands; config set routes ONLY through SaveVilla, never a re-rolled writer (T-03-12).
- [Phase ?]: [03-04]: villa status offload Verdict reuses combineOffload/Verdict/typed-Unknown verbatim; residency proven by the journald load_tensors Vulkan0 N>0 line (NOT /props, WR-05) + a point-in-time mem_info_gtt_used floor (CR-03); /props is a config-drift WARN overlay only.
- [Phase ?]: [03-04]: internal/inference stays pure (no http/journald/exec); cmd/villa/status.go recovers journal/props/gtt and injects via statusDeps. A 0.0.0.0 PublishPort is a FAIL (PRIV-01); loopback assertion parses PublishPort= only, never the Exec= --host 0.0.0.0. StatusReport --json is the frozen Phase-5 dashboard contract.
- [Phase ?]: [03-05]: villa model swap ordering IS the security contract — resolve(catalog)→fit-guard(recommend.Pick Fits=false→refuse, D-09.1)→auto-pull(pullFn)→SaveVilla BEFORE regenerate+restart(D-09.3/T-03-21)→restart ONLY villa-llama.service. No new envelope math, no new downloader.
- [Phase ?]: [03-05]: villa model list reads 'loaded' from config.LoadVilla (source of truth) and 'available' from catalog.Load (D-10); --json emits a frozen [{id,quant,loaded}]. Seams injectable via listDeps/swapDeps so tests assert save-before-restart + inference-only restart with no live host.
- [Phase ?]: [03-06] uninstall enforces config.toml-preserved and SELinux-boolean-not-reverted structurally (no such seam on uninstallDeps); keep/remove-models is flag-driven with default-keep when ambiguous (D-11/T-03-22/T-03-24).
- [04-01]: Open WebUI is a managed service, NOT an inference Backend — rendered via a dedicated openWebUIView/execTemplate path (never parseContainerArgs, whose all-fields-non-empty guard rejects a no-device/no-exec service). Its image/env/ports live in internal/orchestrate (new managed-service constant category), so internal/inference TestSeamGrepGate stays green (D-01/Pitfall 4).
- [04-01]: Render() grows 3→5 units in fixed order (+ villa-openwebui.container/.volume); the telemetry-kill + DNS-locked env block is an ordered []envPair slice (never a map) frozen by BOTH a byte golden and an explicit Contains test = the PRIV-02 re-audit-on-bump guard (D-02/D-07). OPENAI_API_BASE_URL host is built from the containerName constant so it can't drift from the inference unit (DNS lockstep, T-4-01).
- [04-01]: cmd/villa status --json golden refrozen for the 5-unit shape (Rule 3); the PROPER Open WebUI reachability row (health = reachability + non-empty /v1/models, CR-02 active-state fold-in) is Plan 03 scope.
- [Phase ?]: Install starts villa-openwebui.service after villa-llama.service (D-05); ensureModel stays before any start (MODEL-04)
- [Phase ?]: Post-install prints real loopback chat URL http://127.0.0.1:3000 on both write and no-op paths (CHAT-02/D-03)
- [04-03]: villa status owui row health = reachability + non-empty upstream /v1/models (CHAT-01 SC#1); owui offload is an N/A WARN-typed Verdict EXCLUDED from the worst-wins fold via serviceStatus.OffloadApplies — no false offload PASS/FAIL on a non-GPU service (D-12). Transport error → typed-Unknown→WARN (never over-eager FAIL); a confidently-down owui UNIT still FAILs via active-state (CR-02/Pitfall 6).
- [04-03]: villa uninstall removes the villa-openwebui volume via the reserved nonModelVolumes() seam — verified ZERO-CODE (the seam was already generic); the 3000 port folds into allLoopback automatically (PRIV-02). Open WebUI image digest re-confirmed on host = the pinned constant sha256:7f1b0a1a... (PRIV-02 re-audit clean, 2026-06-05).

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

None yet.

### Blockers/Concerns

[Issues that affect future work]

- On-hardware validation required: gfx1151 + Vulkan + rootless `/dev/dri` passthrough + unified-memory is the dominant risk (Phase 2). Silent CPU fallback / OOM is the failure mode to assert against.
- Externalize all version/tuning thresholds (kernel/Mesa floor, GTT envelope math, model catalog) as updatable data — sources disagree on exact kernel floor (~6.16.9 auto-map vs 6.18.4 compute).
- Research flags deeper planning research for Phases 1, 2, 3, and 5.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260604-wh1 | Fix F-3: villa status OFFLOAD WARN — emit -lv 4 residency line + invocation-scoped ResidencyJournal | 2026-06-04 | d401a52 | [260604-wh1-fix-f-3-villa-status-offload-warn-emit-l](./quick/260604-wh1-fix-f-3-villa-status-offload-warn-emit-l/) |
| 260605-d2q | Fix Makefile build target to produce villa binary (repoint build/run to ./cmd/villa, drop legacy web/scaffold targets) | 2026-06-05 | b3a4419 | [260605-d2q-fix-makefile-build-target-to-produce-vil](./quick/260605-d2q-fix-makefile-build-target-to-produce-vil/) |
| 260605-fast | fix(status): render OFFLOAD N/A for non-GPU services in human table (Phase-4 UAT Test 4 cosmetic gap; --json contract unchanged) | 2026-06-05 | e5fc1fc | — |
| 260605-tuv | Fix villa uninstall: drop unsupported podman volume rm --ignore flag (exit 125), surface stderr, tolerate missing volume, add regression tests | 2026-06-05 | 228a4c0 | [260605-tuv-fix-villa-uninstall-drop-unsupported-pod](./quick/260605-tuv-fix-villa-uninstall-drop-unsupported-pod/) |

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| *(none)* | | | |

## Session Continuity

Last session: 2026-06-05T22:18:01.385Z
Stopped at: Phase 6 context gathered
Resume file: .planning/phases/06-rocm-backend-resolver-spine/06-CONTEXT.md

## Operator Next Steps

- Plan the first v1.1 phase with `/gsd-plan-phase 6` (ROCm Backend + Resolver Spine — off-hardware, spine for the milestone).
- Phases 8 and 9 carry on-hardware research flags (see Blockers/Concerns) — consider `--research-phase` when reaching them.
