---
gsd_state_version: 1.0
milestone: v1.1
milestone_name: ROCm Opt-In Backend
status: planning
stopped_at: Phase 11 context gathered
last_updated: "2026-06-06T21:37:37.562Z"
last_activity: 2026-06-06 -- Phase 11 added (v1.1 tech debt)
progress:
  total_phases: 6
  completed_phases: 5
  total_plans: 14
  completed_plans: 14
  percent: 83
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-03)

**Core value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box.
**Current focus:** Phase 10 — backend-tok-s-surfacing

## Current Position

Phase: 11 (address-v1-1-tech-debt) — NOT PLANNED
Plan: 0 of 0
Status: Phase added — ready for planning
Next: Phase 11 (address-v1-1-tech-debt) — rocm_readiness detect probes + doc reconciliation; not yet planned (`/gsd-plan-phase 11`).
Last activity: 2026-06-06 -- Phase 11 added (v1.1 tech debt)

## Performance Metrics

**Velocity:**

- Total plans completed: 19
- Average duration: 34 min
- Total execution time: 2.2 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 3 | - | - |
| 02 | 3 | - | - |
| 4 | 3 | - | - |
| 6 | 3 | - | - |
| 07 | 3 | - | - |

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
| Phase 06 P01 | 25 min | 3 tasks | 11 files |
| Phase 06 P02 | 5min | 3 tasks | 13 files |
| Phase 06 P03 | 25 min | 3 tasks | 7 files |
| Phase 07 P01 | 3 min | 3 tasks | 5 files |
| Phase 07 P02 | 4min | 3 tasks | 7 files |
| Phase 07 P03 | 14 min | 3 tasks | 8 files |
| Phase 08 P01 | 14 min | 3 tasks | 4 files |
| Phase 08 P02 | 18min | 2 tasks | 3 files |
| Phase 09 P01 | 6 min | 1 tasks | 2 files |
| Phase 09 P02 | 4 min | 2 tasks | 3 files |
| Phase 09 P03 | 4 min | 2 tasks | 4 files |
| Phase 10 P01 | 18min | 3 tasks | 5 files |
| Phase 10 P02 | ~3m | 2 tasks | 5 files |
| Phase 10 P03 | 3min | 2 tasks | 2 files |

## Accumulated Context

### Roadmap Evolution

- Phase 11 added (2026-06-06): Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation.

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- [06-03 / A1 RESOLVED]: `runValidation`'s cfg source is `config.LoadVilla()` (the established loader; runValidation takes no cfg param, so load in-function rather than thread a signature change). All 8 non-test `VulkanBackend()` call sites now resolve via `BackendFor(cfg.Backend)`, fail-closed; the re-route is a Vulkan-default no-op (full suite green = SC#4). Deps-wiring helpers became `func() (*T, error)` to surface the resolver error.
- [v1.1 Roadmap]: Spine-first ordering — `BackendFor()` resolver + `ResidencyProof()` interface extension (Phase 6) is a hard precondition for render (7), switch (8), bench (9), surfacing (10). Off-hardware Phases 6–7 precede the on-hardware switch (8).
- [v1.1 Roadmap]: Bench (Phase 9) COMPOSES the Phase-8 `backend set` verb — it must never re-implement backend switching (explicit anti-pattern).
- [v1.1 Roadmap]: Surfacing (Phase 10) lands last so the byte-frozen `--json`/dashboard goldens re-freeze exactly once (append-only, schema-bumped, never reordered).
- [v1.1 Roadmap]: ROCm is opt-in; Vulkan RADV stays the default. `recommend` advises ("ready / worth trying / verify with bench"), never auto-switches. Pin `rocm-7.2.4` (never `rocm7-nightlies` — 64 GB cap).
- [Roadmap]: Inference behind a `Backend` interface from day one (Vulkan v1; ROCm/Metal later) — pluggability checkpoint at Phase 2 review. **v1.1 exercises this seam for the first time.**
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
- [06-01]: Backend interface extended with ResidencyProof() ResidencyMarkers (D-04); both offload-assert scrapes (scrapeOffloadLog start-time + scrapeLoadTensorsResidency running) are now parameterized by the descriptor — no hardcoded Vulkan0/ggml_vulkan:/- Vulkan literals remain in the scraper bodies. ROCm slots in (Plan 02) without re-rolling combineOffload/gttFloor.
- [06-01]: D-06 gpu_busy_percent folded through combineOffload via gpuBusyFloor (Known non-zero corroborates PASS, Known-zero FAILs a claimed-healthy decode, absent/Unknown is combine-neutral). CRITICAL: Unknown is neutral by SKIPPING the fold (combineOffload has NO neutral state) — a WARN would downgrade every Vulkan PASS; Vulkan supplies no busy reading so its verdict stays byte-identical.
- [06-01]: A non-empty FaultString found in the journal voids residency (FAIL) before the buffer-line switch; a start-time 0<N<M partial offload FAILs, gated on an explicit offloaded line so Vulkan auto-fit (no offloaded line) still PASSes. Provenance embeds the DeviceToken so the byte-frozen status --json golden is unchanged for Vulkan.
- [Phase ?]: BackendFor fails closed on an unknown config backend (nil + actionable error), never a silent Vulkan fallback (D-02)
- [Phase ?]: ROCm backend is a Vulkan sibling file (backend_rocm.go) behind the seam; rocm-7.2.4 digest-pinned, never the nightlies tag (D-08)
- [Phase ?]: TestROCmMarkerPresence gates on ROCm0 (not ggml_cuda, which is shared with the CUDA path)
- [Phase ?]: [07-01]: ROCm villa-llama.container rendered as a pure additive delta over Vulkan (image+kfd+render-group+HSA/hipBLASLt env), byte-frozen by a new golden with the Vulkan golden unchanged. parseContainerArgs collects ALL --device/--group-add/--env tokens (D-09 was incomplete: second group-add + both env flags were silently dropped). BackendLabel keyed off Backend.Name() via a render.go label map (seam-clean, reproduces 'Vulkan RADV' exactly); Env excluded from the defensive check (Vulkan emits zero env, Pitfall 1).
- [Phase ?]: Phase 7 Plan 2: externalized ROCm version floors + denylists into a go:embed rocm-policy.json; RunROCm refuses bring-up only on confident known-bad (firmware 20251125 / nightly image / kernel <6.18.4 / wrong HSA / non-gfx1151), unevaluable degrades to WARN (PRE-06)
- [08-01]: internal/backendswap is the transactional core for `villa backend set` — capture(verbatim prior unit bytes + value-snapshot config) STRICTLY before mutate, switch ONLY on ProveStatusPass (is-active/200 alone never success, SC#3), verbatim rollback on any mutate error or non-pass verdict (BSET-02), best-effort bounded re-ready with honest incomplete-rollback reporting (Pitfall 5). ProveVerdict + ProveStatusPass='pass' are LOCAL (no inference/detect import) so the core stays literal-free of backend markers; the cmd layer maps inference.StatusPass in. Fit-guard FIRST then ROCm preflight refuse-with-remediation against the PRESERVED model (BSET-01); same-backend is a clean NoOp; refusals fire zero seams.
- [08-01]: inference now EXPORTS PollHealth(ctx,endpoint,timeout)/GenerationProbe(ctx,endpoint,modelID) — thin wrappers over the private pollHealth/chatProbe that probe the ALREADY-running server with NO --rm container (Validate's --rm container is why liveProve cannot use it). Closes the Plan-02 liveProve BLOCKER. TestSeamGrepGate now also walks cmd/villa with the backend-marker subset (GOOS/image/device/ROCm0-HSA-fault); the podman-process pattern is EXCLUDED from the cmd/villa walk because cmd/villa is the legitimate OS-orchestration tier (lifecycle/uninstall fixed-arg podman calls).
- [Phase ?]: [07-03]: villa detect --json gains a nested rocm_readiness object appended after the GPU block with hostProfileSchemaVersion bumped 1->2 (additive append-only golden re-freeze; SchemaVersion stays last). Off-hardware undetectable signals (rocminfo_gfx1151, firmware_date_ok, hsa_override_viable) serialize as UnknownBool/UNSET, never a real false (D-08). image_policy_ok is config-driven against the resolved image, not a host probe (Pitfall 5). detect imports neither inference nor preflight (cycles), so the ROCm image-tag + 6.18.4 kernel-floor literals are mirrored behind the gpu_amd.go seam and the version comparator re-expressed there; readiness_rocm.go stays literal-free (TestSeamGrepGate green).
- [Phase ?]: [08-02]: villa backend set/show noun + liveProve cutover gate — liveProve composes EXPORTED inference.PollHealth (bounded by proveTimeout=5m load_tensors-hang guard) + inference.GenerationProbe (tokens>0) + RunningOffloadVerdict over BackendFor(target).ResidencyProof() markers, sampling detect.GPUBusyPercent() DURING the decode via goroutine+ticker max-keep (D-07 closed) so a silent CPU fallback FAILs+rolls back; maps ONLY inference.StatusPass to backendswap.ProveStatusPass (SC#3). runBackendSet returns the int exit (Refused/Err/RolledBack->1, Switched/NoOp->0); --dry-run previews fit/preflight side-effect-free. liveBackendSwapDeps clones liveSwapDeps render/reconcile/write + capture/restore via traversal-guarded orchestrate.WriteUnits. backend.go literal-free of markers (cmd/villa-walking TestSeamGrepGate).
- [Phase ?]: [09-01]: llm.Complete is the honest measurement leaf — a non-streaming stream:false /v1 completion returning the server-computed per-request timings block (pp/tg already separated) that StreamChat discards; completeResponse deserializes ONLY the numeric timings (T-09-01), non-200 bounded by io.LimitReader 2048 (T-09-02), fixed max_tokens/seed/temperature on the wire for reproducibility, stdlib-only/go.mod unchanged, literal-free of backend markers.
- [Phase ?]: [09-02] internal/bench is the pure honest-benchmark core: warmup-discard, residency void-gate (resident==false excluded/counted, never a slow pass), bounded void-exhaustion WARN below MinResident, separated pp/tg median+stddev; print-free/exit-free, cobra layer (09-03) owns presentation.
- [Phase ?]: [09-02] The --ab flip composes backendswap.Run via injected Switch/Restore (LOCKED, never re-implemented); defer Restore(orig) registered BEFORE the flip so every exit path restores; package imports no inference/detect, markers arrive only via Measure verdict (seam gate green).
- [Phase ?]: [09-03] villa bench noun wires llm.Complete + internal/bench: liveMeasure is a liveProve clone (residency gate, during-decode GPUBusyPercent sampling, spec.Timeout load_tensors-hang guard) swapping GenerationProbe for llm.Complete; resident ONLY for inference.StatusPass (CPU-fallback completion is VOID, not a slow pass). Plain `bench` benches only the running backend (zero flips, SC#1); --ab delegates Switch/Restore to backendswap.Run via the SAME liveBackendSwapDeps wiring (LOCKED) and restores the original (SC#3). Spec rides the live Measure closure (the LOCKED core threads its own context.Background()); the no-endpoint reachability pre-check is a package-level `var benchEndpointReachable` indirection (NOT a new bench.Deps field). Exit map: no-endpoint->exitBlocked, void-exhaustion->exitWarn, clean->exitPass. --json (bench.json.golden) carries separate prompt_per_sec/predicted_per_sec (+stddevs) per side + per-metric delta (Phase-10 read contract); bench.go literal-free of markers (TestSeamGrepGate green).
- [Phase ?]: Phase 10-01: status Report schema_version=1; backend/image/tok-s/rocm-readiness tail-appended; golden re-frozen once as pure-addition diff (DASH-06)
- [Phase ?]: 10-02: villa recommend surfaces typed ROCmAdvice (worth-trying/verify-with-bench/withheld) derived purely in Pick from rocm_readiness; Backend stays vulkan, advice never auto-switches and never promises a speed-up (honesty Δtg −11.15, points to villa bench)
- [Phase ?]: 10-02: recommend.golden.json re-frozen once as pure tail-addition (schema_version:1); detect + status goldens byte-identical; go.mod frozen

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
| 260606-p3a | Fix villa bench single-mode backend label: name the measured backend in human header + --json single.backend (Phase-9 UAT minor gap; --ab + pp/tg-separate contract unchanged) | 2026-06-06 | 8aa9c90 | [260606-p3a-fix-villa-bench-single-mode-backend-labe](./quick/260606-p3a-fix-villa-bench-single-mode-backend-labe/) |

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| *(none)* | | | |

## Session Continuity

Last session: 2026-06-06T21:37:37.556Z
Stopped at: Phase 11 context gathered
Resume file: .planning/phases/11-address-v1-1-tech-debt-rocm-readiness-detect-probes-doc-reco/11-CONTEXT.md

## Operator Next Steps

- Plan the first v1.1 phase with `/gsd-plan-phase 6` (ROCm Backend + Resolver Spine — off-hardware, spine for the milestone).
- Phases 8 and 9 carry on-hardware research flags (see Blockers/Concerns) — consider `--research-phase` when reaching them.
