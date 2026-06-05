# Roadmap: VillaStraylight

## Overview

VillaStraylight is a control plane + managed workload: a single Go binary that inspects an AMD Strix Halo (gfx1151) Fedora host, recommends a hardware-fitting model/quant/context, generates rootless Podman Quadlet units, and brings up llama.cpp (Vulkan) inference + Open WebUI chat + a read-only control dashboard — with zero data leaving the box. The roadmap is risk-ordered exactly as all four research streams independently converged: lock the pure, testable core (detection + recommendation + a hard preflight gate) first; then validate the single biggest technical unknown — that `llama-server` actually offloads to the iGPU through the rootless `/dev/dri` + unified-memory gauntlet — as an early vertical slice before any orchestration machinery is built on top of it; then turn that proven manual run into a managed, idempotent, boot-survivable stack with model management and privacy gates; wire Open WebUI so the first chat works immediately; and finally add the dashboard as a read-model over the same internal API `villa status` already consumes. The north star throughout is "runs healthy after install" — every phase ends in something runnable and verifiable on the real hardware.

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Hardware Foundation & Preflight Gate** - Pure, testable `villa detect`/`recommend` over a usable-GTT envelope, plus a non-optional host-prep preflight that blocks unsafe installs *(all 3 plans complete — ready for verification)*
- [x] **Phase 2: GPU-Validated Inference Slice** - Backend seam + Vulkan llama-server proven to actually offload to the gfx1151 iGPU, OpenAI-compatible on loopback, with offload-asserting health and a near-max-context probe (completed 2026-06-04)
- [x] **Phase 3: Orchestrated Install & Lifecycle** - Config-as-source-of-truth, Quadlet generation, model management, and the idempotent, boot-survivable, loopback-only `villa` lifecycle verbs (completed 2026-06-04)
- [x] **Phase 4: Chat Integration** - Open WebUI wired to local inference by container DNS, telemetry killed, durable data, default-model auto-pull so the first chat works with no configuration (completed 2026-06-05)
- [x] **Phase 5: Control Dashboard** - Read-only web dashboard over the same internal API as `villa status`: health, tok/s + latency, iGPU/unified-memory usage, model switching, and a chat link (completed 2026-06-05)

## Phase Details

### Phase 1: Hardware Foundation & Preflight Gate

**Goal**: As a privacy-conscious Strix Halo owner, I want to run `villa detect`/`recommend` for a correct hardware profile + a memory-fitting model recommendation and a preflight gate that refuses unsafe installs, so that I avoid silent-CPU-fallback and OOM before anything is installed.
**Mode:** mvp
**Depends on**: Nothing (first phase)
**Requirements**: DET-01, DET-02, DET-03, REC-01, REC-02, REC-03, REC-04, PRE-01, PRE-02, PRE-03, PRE-04, PRE-05
**Success Criteria** (what must be TRUE):

  1. `villa detect` prints a `HostProfile` showing CPU/arch, the gfx1151 iGPU, Vulkan ICD + `/dev/dri` enumeration (and whether ROCm is present), total RAM, and the usable GTT/unified-memory envelope (the real ceiling, not BIOS VRAM), correctly accounting for kernel version (e.g. ≥ 6.16.9 auto-map).
  2. `villa recommend` prints a model + quantization + context length + backend (Vulkan default for gfx1151) that satisfies `model_bytes + KV-cache@context + headroom ≤ usable_envelope`, read from a versioned JSON catalog honoring the `unified_memory_safe` flag, and the user can override model/quant/context.
  3. Preflight reports pass/warn/fail with a remediation hint per check for: Vulkan ICD + iGPU enumeration, kernel/Mesa/firmware floor (externalized thresholds), Podman rootless-ready (subuid/subgid) + `systemd --user`, user lingering, and free disk (≥ model) + free memory (≥ envelope).
  4. A failing preflight check blocks install (or explicitly warns) rather than allowing a silent container crash; detection that fails to parse a tool degrades to a conservative "unknown", never to 0 or a crash.

**Plans**: 3 plansPlans:
**Wave 1**

- [x] 01-01-PLAN.md — Walking skeleton + `villa detect` slice (cobra scaffold, typed Unknown contract, sysfs GTT envelope, HostProfile --json contract)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01-02-PLAN.md — `villa recommend` slice (versioned go:embed catalog, GQA fit-math, overrides, degraded floor, TOML config + --save)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 01-03-PLAN.md — `villa preflight` slice (reusable preflight package, BLOCK/WARN tiering, exit codes 0/2/1, --force override summary)

### Phase 2: GPU-Validated Inference Slice

**Goal**: `llama-server` provably runs the selected model on the real gfx1151 iGPU via Vulkan with GPU offload actually engaged, exposing an OpenAI-compatible API on loopback — sitting behind a backend interface so no Vulkan/Linux assumption leaks to callers. This validates the project's single biggest technical risk as an early vertical slice, before any orchestration is built on it.
**Mode:** mvp
**Depends on**: Phase 1
**Requirements**: INF-01, INF-02, INF-03, MODEL-02
**Success Criteria** (what must be TRUE):

  1. A model resolves, downloads with SHA256/size verification, resumes if interrupted, atomically renames on success, and is rejected if any shard is missing — yielding a verified GGUF on disk for inference to load.
  2. `llama-server` serves `/v1` bound to `127.0.0.1` with the mandatory Strix Halo flags (`-ngl 999 -fa 1 --no-mmap`), and a real chat completion returns tokens from the iGPU.
  3. The health check asserts GPU offload actually happened (offloaded layers > 0 / Vulkan device line in startup logs) — not merely that the server responds — and a near-max-context probe surfaces OOM/long-context hangs at validation time rather than in use.
  4. The inference backend sits behind a `Backend` interface (runner-and-backend-neutral, abstracting container-vs-native runner); review confirms no `runtime.GOOS=="linux"` or `/dev/dri` assumption has leaked outside `internal/inference/` and the AMD detection file.

**Plans**: 3 plans
Plans:
**Wave 1** *(parallel — no file overlap)*

- [x] 02-01-PLAN.md — `villa model pull` slice: catalog schema v2 (real verified HF URL/SHA256/size + corrected dims) + resumable/atomic/per-shard-verified downloader (MODEL-02)
- [x] 02-02-PLAN.md — inference seam slice: Runner/Backend interfaces + Vulkan backend (digest-pinned image, mandatory flags, keep-groups, loopback) + dual offload assert (log-scrape + sysfs GTT/VRAM delta) + grep-gate (INF-01, INF-03)

**Wave 2** *(blocked on Wave 1)*

- [x] 02-03-PLAN.md — proven-run slice: health/readiness + reused internal/llm chat probe + context-ceiling stress probe + offload-asserting `villa inference run|validate` verb (exit 0/2/1, --json) + on-hardware gate (INF-01, INF-02, INF-03)

### Phase 3: Orchestrated Install & Lifecycle

**Goal**: `villa install` runs end-to-end (detect → recommend → generate units → pull model → bring up) idempotently and re-runnably, turning Phase 2's manual run into rootless, loopback-only, boot-survivable managed services driven entirely from a single config source of truth — with the full `villa` lifecycle verb set.
**Mode:** mvp
**Depends on**: Phase 2
**Requirements**: ORCH-01, ORCH-02, ORCH-03, ORCH-04, CLI-01, CLI-02, CLI-03, CLI-04, CLI-05, CLI-06, CLI-07, MODEL-01, MODEL-03, PRIV-01, PRIV-03
**Success Criteria** (what must be TRUE):

  1. `villa install` generates `.container`/`.network`/`.volume` Quadlet units from config, passes `/dev/dri` + `GroupAdd=keep-groups` + correct SELinux/volume settings so the container reaches the iGPU, and is idempotent — a second run is a no-op (`--dry-run` prints the rendered units).
  2. Services run rootless, publish only to `127.0.0.1` by default, start on boot via Quadlet `[Install] WantedBy=default.target` + `loginctl enable-linger`, and survive a real logout/reboot.
  3. `villa up`/`down`/`restart` control the whole stack and individual services; `villa status` shows an aggregated health table (unit active-state + container health + llama.cpp `/health`); `villa logs [service] [-f]` follows per-service logs; `villa config show` displays effective config and editing it regenerates the affected units.
  4. `villa model list` distinguishes available (catalog) from loaded; `villa model swap` regenerates the inference unit args (model path, `-c`, `-ngl`) and restarts it; `villa uninstall` cleanly removes units and volumes with a keep-or-remove-models choice.
  5. `villa status` asserts the privacy posture — every service binds loopback only (verifiable via `ss -tlnp`), nothing on `0.0.0.0` unless explicitly opted in, and "no telemetry; outbound = image/model pulls only" — and post-install output prints the chat + dashboard URL with a health summary.

**Carried-in hardening (from Phase 2 code review — `02-REVIEW.md`):** Strengthen the offload-assert that `villa status` reuses — **WR-05**: on auto-fit llama.cpp builds the log signal proves only device *enumeration*, not per-layer residency (no `offloaded N/N` line); add a stronger machine-checkable proof (e.g. a higher-verbosity `load_tensors: Vulkan0 model buffer size` line, or a `/props`/API check). **CR-03**: `mem_info_gtt_used` is a host-wide counter — make the sysfs delta robust to concurrent GPU consumers. Both keep the dual-assert's independence honest as it becomes the long-lived `villa status` / dashboard contract.

**Plans**: 6 plans
Plans:
**Wave 1**

- [x] 03-01-PLAN.md — Render+reconcile core: pure `internal/orchestrate` renderer (.container/.network/.volume from Backend) + content-hash reconcile + atomic write + systemd seam + golden fixtures + extended seam grep-gate (ORCH-01/02/03/04, CLI-01, PRIV-01)

**Wave 2** *(blocked on Wave 1)*

- [x] 03-02-PLAN.md — `villa install` slice: preflight gate + consented host-prep (SELinux/linger) + render+pull+daemon-reload+start + 503-aware readiness poll + idempotent re-run + --dry-run + post-install URL (CLI-01/07, ORCH-03, PRIV-01) — on-hardware bring-up checkpoint

**Wave 3** *(blocked on Wave 2 — shared root.go)*

- [x] 03-03-PLAN.md — Lifecycle verbs slice: `up`/`down`/`restart` (stack + individual) + `logs [service] [-f]` + `config show`/`set` (CLI-02/04/05)

**Wave 4** *(blocked on Wave 3 — shared root.go)*

- [x] 03-04-PLAN.md — Offload-asserting `status` slice: running-server Verdict (residency log + /props + point-in-time GTT floor, closes WR-05/CR-03) + aggregated table + frozen `status --json` golden + loopback/no-telemetry assertion (CLI-03, PRIV-01/03)

**Wave 5** *(blocked on Wave 4 — shared root.go)*

- [x] 03-05-PLAN.md — Model management slice: `model list` (available vs loaded) + `model swap` (fit-guard, auto-pull, config-first persist, inference-only restart) (MODEL-01/03)

**Wave 6** *(blocked on Wave 5 — shared root.go)*

- [x] 03-06-PLAN.md — `villa uninstall` slice: flag-driven keep/remove-models, ordered teardown, disable-linger, leaves config.toml (CLI-06) — on-hardware teardown + boot-survival checkpoint

### Phase 4: Chat Integration

**Goal**: Open WebUI runs as a second container on the shared network, wired to the local OpenAI-compatible inference endpoint by container DNS, with upstream telemetry disabled and durable data — and install auto-pulls a recommended default model so the user can open the browser and chat immediately with no extra configuration.
**Mode:** mvp
**Depends on**: Phase 3
**Requirements**: CHAT-01, CHAT-02, CHAT-03, MODEL-04, PRIV-02
**Success Criteria** (what must be TRUE):

  1. Open WebUI runs as a container reaching `http://villa-llama:8080/v1` by container DNS over the private network (not `localhost`), localhost-published, and its reachability health check confirms a non-empty model list.
  2. After install (which auto-pulls a recommended default model, optionally a small bootstrap model first), the user opens Open WebUI in the browser and chats with the local model with zero extra configuration.
  3. Open WebUI data (chat history, settings, first-user/admin account) persists across restarts and updates via a durable named volume with correct SELinux labeling.
  4. First-party Go code sends zero telemetry and known upstream telemetry is disabled (`ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`), re-audited on image bump — the privacy posture holds end-to-end through the chat path.

**Plans**: 3 plans
Plans:
**Wave 1**

- [x] 04-01-PLAN.md — Open WebUI render slice: managed-service constants + `openwebui.container`/`.volume` templates, `Render()` 3→5 units, container-DNS wiring + telemetry-kill env + named `:Z` volume, two goldens + frozen-telemetry-env test (CHAT-01, CHAT-03, PRIV-02)

**Wave 2** *(blocked on Wave 1 — needs the 5-unit Render)*

- [x] 04-02-PLAN.md — Install bring-up slice: start `villa-openwebui.service` after inference, `ensureModel` before bring-up, post-install prints the real chat URL `http://127.0.0.1:3000` (CHAT-02, MODEL-04)

**Wave 3** *(blocked on Wave 2 — shared cmd/villa)*

- [x] 04-03-PLAN.md — Status + teardown slice: Open WebUI reachability/model-list status row (no false offload PASS, typed-Unknown→WARN), frozen `status --json` golden + 3000 loopback port, `uninstall` removes the new named volume, end-of-phase on-hardware UAT (CHAT-01, CHAT-03, PRIV-02)

**UI hint**: yes

### Phase 5: Control Dashboard

**Goal**: A read-only web dashboard, served by the Go binary as a read-model over the same internal API `villa status` consumes, surfaces live service health, performance and GPU/unified-memory metrics, model switching, and a one-click chat link — without forking any status logic or trusting `amd-smi` on gfx1151.
**Mode:** mvp
**Depends on**: Phase 4
**Requirements**: DASH-01, DASH-02, DASH-03, DASH-04, DASH-05
**Success Criteria** (what must be TRUE):

  1. The dashboard shows live service health sourced from the same internal API as `villa status` (logic not forked), plus a one-click link to the chat UI.
  2. The dashboard shows performance metrics — generation tok/s, prompt tok/s, latency — read from llama.cpp `/metrics` + `/slots`.
  3. The dashboard shows iGPU utilization and unified-memory (VRAM + GTT) usage vs the envelope, read from amdgpu sysfs/hwmon (not `amd-smi`, which mis-reports the pool on gfx1151).
  4. The dashboard lists available + loaded models and lets the user switch the loaded model, routed back through the Orchestrator (the same path as `villa model swap`).

**Plans**: 8 plans (5 + 3 gap-closure)
Plans:
**Wave 1**

- [x] 05-01-PLAN.md — Refactor foundation: extract `internal/status` + `internal/modelswap` cores (JSON-neutral, frozen `--json` golden stays green), extend `VillaConfig` (dashboard/chat ports), add `--metrics` to inference flags (deliberate render golden) (DASH-01, DASH-02, DASH-04)

**Wave 2** *(blocked on Wave 1 — needs the shared status core + config)*

- [x] 05-02-PLAN.md — Dashboard surface slice: `internal/dashboard` chi server + `/api/status` from the shared core + embedded no-build UI shell + Health panel + poll loop + chat link + loopback bind + `villa dashboard` verb + CSRF/same-origin scaffold (DASH-01, DASH-05)

**Wave 3** *(blocked on Wave 2 — shared internal/dashboard)*

- [x] 05-03-PLAN.md — Metrics + GPU slice: `internal/metrics` `/metrics`+`/slots` collector (corrected gauges, idle gate) + `detect.GPUBusyPercent()` + `/api/metrics`+`/api/gpu` + Performance & GPU&Memory panels (memory-first, typed-Unknown) (DASH-02, DASH-03)

**Wave 4** *(blocked on Wave 3 — shared internal/dashboard)*

- [x] 05-04-PLAN.md — Models + switch slice: `/api/models` list + guarded `POST /api/models/switch` through the shared `internal/modelswap` path + Models panel + fit-aware confirm dialog (DASH-04)

**Wave 5** *(blocked on Wave 2+4 — shared cmd/villa lifecycle + internal/status; end-of-phase UAT)*

- [x] 05-05-PLAN.md — Lifecycle integration slice: native `villa-dashboard.service` render + `Systemd.Enable()` + install/up/down/restart/uninstall wiring + `villa status` dashboard row (deliberate `--json` golden update) — on-hardware UAT (DASH-01)

**Gap closure** *(UAT Test 5 — dashboard boot-survival)*

- [x] 05-06-PLAN.md — Fix `villa-dashboard.service` ExecStart: render the binary path from `os.Executable()` (EvalSymlinks+Abs) at install time instead of the fixed `%h/.local/bin/villa`, so the unit survives reboot on dev + installed binaries; regression tests + golden update (DASH-05)

- [x] 05-07-PLAN.md — Fix gap test:1b (`villa dashboard` printed `http://127.0.0.1:0`): self-heal zeroed `dashboard_port`/`chat_port`/`dashboard_addr` on load (`normalizeVilla` in both load paths) + seed both config writers from `DefaultVillaConfig()` so a saved config never carries 0 ports; regression tests (DASH-01, DASH-05)

- [x] 05-08-PLAN.md — Fix gap test:5 (dashboard inactive after reboot on re-install): hoist the native `villa-dashboard.service` reconciliation above the no-op container early return so it runs on BOTH install paths, made idempotent via a render-vs-disk compare (write/reload/enable/restart only on a diff); regression tests (DASH-01, DASH-05)

**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Hardware Foundation & Preflight Gate | 3/3 | Complete    | 2026-06-03 |
| 2. GPU-Validated Inference Slice | 3/3 | Complete    | 2026-06-04 |
| 3. Orchestrated Install & Lifecycle | 6/6 | Complete   | 2026-06-04 |
| 4. Chat Integration | 3/3 | Complete    | 2026-06-05 |
| 5. Control Dashboard | 8/8 | Complete   | 2026-06-05 |
