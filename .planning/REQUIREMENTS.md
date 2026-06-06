# Requirements: VillaStraylight

**Defined:** 2026-06-03
**Core Value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box.

## v1 Requirements

Milestone 1 — "Core platform." Each maps to a roadmap phase. All are hypotheses until shipped and validated.

### Hardware Detection (DET)

- [x] **DET-01**: User can run `villa detect` to see detected CPU model/arch, iGPU (e.g. gfx1151 / RDNA 3.5), total RAM, and usable unified-memory (GTT) envelope
- [x] **DET-02**: Detection reports GPU-backend availability — Vulkan ICD present + iGPU enumerated via `/dev/dri`, and whether ROCm is installed
- [x] **DET-03**: Detection computes the usable GTT/unified-memory envelope (not the BIOS VRAM carve-out), accounting for kernel-version behavior (e.g. kernel ≥ 6.16.9 auto-maps the pool)

### Preflight (PRE)

- [x] **ROCM-01**: An opt-in ROCm/HIP `llama-server` inference backend implemented behind the existing `Backend` interface, selected by a `backend` config field — Vulkan RADV remains the default; no backend specifics leak to callers.
- [x] **ROCM-02**: The ROCm backend is offload-asserting via a HIP residency proof — asserts the `ROCm0` device buffer line + `offloaded N/N layers` (N==M) + non-zero sysfs `gpu_busy_percent` during a real decode + absence of `Memory access fault by GPU node`; a silent/partial CPU fallback is a FAIL. A grep-gate test prevents a refactor dropping the HIP marker strings.
- [x] **ROCM-03**: The ROCm Quadlet unit renders the correct delta over the Vulkan unit — digest-pinned `kyuz0:rocm-7.2.4` image (never nightlies), `/dev/kfd` + `/dev/dri` passthrough, `render` group, `HSA_OVERRIDE_GFX_VERSION=11.5.1` + `ROCBLAS_USE_HIPBLASLT=1` env, and `-ngl 999 -fa 1 --no-mmap` flags; frozen by a new byte-golden with the Vulkan golden unchanged (proves additivity).
- [x] **ROCM-04**: The single polymorphic resolver `BackendFor(cfg.Backend)` routes every inference-backend call site through config (replacing the 7 hardcoded `VulkanBackend()` sites), so backend choice is honored consistently across install, lifecycle, status, model, and dashboard paths.

### Recommendation Engine (REC)

- [x] **BSET-01**: `villa backend set rocm|vulkan` swaps the inference unit on a *running* install (save-before-restart, regenerate `villa-llama.container`, daemon-reload, restart only `villa-llama.service`) — fit-guarded against the memory envelope; refuses-with-remediation rather than guessing.
- [x] **BSET-02**: The switch is transactional and rollback-safe — it captures the exact prior working unit/config before mutating, gates cutover on a real generation-probe readiness check + residency proof for the new backend, and auto-rolls back to the verbatim captured working unit on any failure (failed bring-up is a no-op to the user's stack).
- [x] **BSET-03**: `villa backend show` reports the active backend; the configured model/quant/context is preserved across a switch (model = config; refuse, don't re-pick); `--dry-run` previews the switch without mutating.

### Inference (INF)

- [x] **PRE-06**: A reusable ROCm-specific preflight verdict gates ROCm bring-up — confirms `rocminfo`/gfx1151, enforces kernel floor (≥6.18.4), blocks the known-bad `linux-firmware-20251125` (≥20260110 good), requires the HSA override, and refuses `rocm7-nightlies`; expressed as externalized (`go:embed`-updatable) version policy (ranges + denylist) and refuses-with-remediation, biased to not over-block a genuinely-working host.
- [x] **DET-04**: `villa detect` reports ROCm readiness (additive fields on the host profile / `--json`, schema-bumped, never reordering the frozen v1.0 contract).

### Orchestration (ORCH)

- [x] **ORCH-01**: `villa` generates Podman Quadlet units (`.container`/`.network`/`.volume`) for the stack from config
- [x] **ORCH-02**: Generated units pass the GPU device (`/dev/dri`) plus the required group (`keep-groups`) and SELinux settings so the container reaches the iGPU
- [x] **ORCH-03**: Services start on boot via Quadlet `[Install]` + linger and survive logout
- [x] **ORCH-04**: Services run rootless and publish only to loopback by default

### CLI Lifecycle (CLI)

- [x] **CLI-01**: `villa install` runs end-to-end setup (detect → recommend → generate units → pull model → bring up) and is idempotent / re-runnable
- [x] **CLI-02**: `villa up` / `down` / `restart` control the whole stack and individual services
- [x] **CLI-03**: `villa status` shows an aggregated health table (unit active-state + container health + llama.cpp `/health` + Open WebUI reachability)
- [x] **CLI-04**: `villa logs [service]` shows and can follow (`-f`) per-service logs
- [x] **CLI-05**: `villa config show` displays the effective config; editing config regenerates the affected units
- [x] **CLI-06**: `villa uninstall` cleanly removes units and volumes, with a choice to keep or remove downloaded models
- [x] **CLI-07**: Post-install output prints the chat + dashboard URL and a health summary

### Model Management (MODEL)

- [x] **MODEL-01**: `villa model list` shows available (catalog) and currently-loaded models, distinguishing the two
- [x] **MODEL-02**: `villa model pull` downloads a model with checksum/size verification and resumable, atomic write (all shards present)
- [x] **MODEL-03**: `villa model swap` switches the loaded model — regenerating the inference unit args (model path, `-c`, `-ngl`) and restarting it
- [x] **MODEL-04**: Install auto-pulls a recommended default model (optionally a small bootstrap model first) so the first chat works immediately

### Chat (CHAT)

- [x] **CHAT-01**: Open WebUI runs as a container wired to the local OpenAI-compatible inference endpoint
- [x] **CHAT-02**: User can open Open WebUI in the browser and chat with the local model with no extra configuration
- [x] **CHAT-03**: Open WebUI data (chat history, settings) persists across restarts via a durable volume

### Control Dashboard (DASH)

### v1.1.x / operability

- **BENCH-03**: `villa bench --compare` one-shot flip→bench→flip-back, and a saved bench report artifact (md/JSON).
- **BAK-01**: Backup / restore — config first, then Open WebUI volume snapshots.
- **DOCTOR-01**: `villa doctor` deep diagnostics / self-heal hints (reuse preflight logic post-install).
- **USAGE-01**: Cumulative token/throughput usage tracking ("Token Spy"-style).
- **INSTALL-01**: Interactive guided (TUI) install alongside the one-shot flag-driven install.
- **ROCM-ALT-01**: `rocm-6.4.4` (or newer stable) as a documented opt-in alternate image for TG-heavy models where 7.2.4 underperforms — selected/validated via `villa bench`.

### Milestone 2 — Memory & Search

- Qdrant persistent memory; SearXNG search; OpenCode coding-agent wiring (deferred — see PROJECT.md Out of Scope).

### Future

- Metal / Apple-Silicon as a third `Backend`; voice (Whisper/Kokoro); agents/orchestration; image generation.
- ROCm perf-tuning knobs (hipBLASLt / rocWMMA-FA / batch) exposed as first-class advanced config.

- **VOICE-01**: Voice — Whisper (STT) + Kokoro (TTS)
- **AGENT-01**: Agents / workflows
- **PLAT-01**: macOS / Apple Silicon / Metal backend (and other GPU backends) behind the inference interface
- **REMOTE-01**: Authenticated remote access (designed so v1's local-only gateway extends without rework)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Building a custom chat UI | Superseded by Open WebUI (mature, multi-model, RAG-ready); rebuilding wastes the control-plane focus. Earlier Go chat scaffold is reference-only. |
| Rebuilding AI services in Go | Go is the control plane only; reimplementing inference/chat is enormous and pointless — orchestrate OSS containers. |
| Cloud / hybrid model fallback | Directly violates the "zero data leaving the box" core value. Local-only, not a mode. |
| Image generation (ComfyUI) | Not requested for this product. |
| Multiple simultaneous loaded models / auto-swap router | Unified-memory envelope rarely fits two large models; adds a routing layer. One loaded model at a time; explicit swap. |
| Docker / Docker-Compose dependency | Fedora-native, rootless Podman Quadlets are a core differentiator and stronger security posture. |
| Multi-platform support (macOS/Metal, NVIDIA, Intel) in v1 | Nail Fedora + Strix Halo first; multi-backend multiplies test surface. Kept behind an interface for later. |
| Remote access / multi-user auth in v1 | Breaks the strictly-local posture; auth + exposure is its own security project (deferred, not designed out). |

## Traceability

| REQ-ID | Phase | Status |
|--------|-------|--------|
| ROCM-01 | Phase 6 | Complete |
| ROCM-02 | Phase 6 | Complete (residency engine + descriptor + gpu_busy fold in 06-01; ROCm0 markers + grep-gate in 06-02; verified 06-VERIFICATION.md) |
| ROCM-04 | Phase 6 | Complete |
| ROCM-03 | Phase 7 | Complete |
| PRE-06 | Phase 7 | Complete |
| DET-04 | Phase 7 | Complete |
| BSET-01 | Phase 8 | Complete |
| BSET-02 | Phase 8 | Complete |
| BSET-03 | Phase 8 | Complete |
| BENCH-01 | Phase 9 | Pending |
| BENCH-02 | Phase 9 | Pending |
| REC-05 | Phase 10 | Pending |
| DASH-06 | Phase 10 | Pending |

| Requirement | Phase | Status |
|-------------|-------|--------|
| DET-01 | Phase 1 | Complete |
| DET-02 | Phase 1 | Complete |
| DET-03 | Phase 1 | Complete |
| PRE-01 | Phase 1 | Complete |
| PRE-02 | Phase 1 | Complete |
| PRE-03 | Phase 1 | Complete |
| PRE-04 | Phase 1 | Complete |
| PRE-05 | Phase 1 | Complete |
| REC-01 | Phase 1 | Complete |
| REC-02 | Phase 1 | Complete |
| REC-03 | Phase 1 | Complete |
| REC-04 | Phase 1 | Complete |
| INF-01 | Phase 2 | Complete |
| INF-02 | Phase 2 | Complete |
| INF-03 | Phase 2 | Complete |
| MODEL-02 | Phase 2 | Complete |
| ORCH-01 | Phase 3 | Complete |
| ORCH-02 | Phase 3 | Complete |
| ORCH-03 | Phase 3 | Complete |
| ORCH-04 | Phase 3 | Complete |
| CLI-01 | Phase 3 | Complete |
| CLI-02 | Phase 3 | Complete |
| CLI-03 | Phase 3 | Complete |
| CLI-04 | Phase 3 | Complete |
| CLI-05 | Phase 3 | Complete |
| CLI-06 | Phase 3 | Complete |
| CLI-07 | Phase 3 | Complete |
| MODEL-01 | Phase 3 | Complete |
| MODEL-03 | Phase 3 | Complete |
| PRIV-01 | Phase 3 | Complete |
| PRIV-03 | Phase 3 | Complete |
| CHAT-01 | Phase 4 | Complete |
| CHAT-02 | Phase 4 | Complete |
| CHAT-03 | Phase 4 | Complete |
| MODEL-04 | Phase 4 | Complete |
| PRIV-02 | Phase 4 | Complete |
| DASH-01 | Phase 5 | Pending |
| DASH-02 | Phase 5 | Pending |
| DASH-03 | Phase 5 | Pending |
| DASH-04 | Phase 5 | Pending |
| DASH-05 | Phase 5 | Pending |

**Coverage:**

- v1 requirements: 38 total
- Mapped to phases: 38 (100%)
- Unmapped: 0 ✓

**Per-phase counts:**

- Phase 1 (Hardware Foundation & Preflight Gate): 12 — DET-01..03, PRE-01..05, REC-01..04
- Phase 2 (GPU-Validated Inference Slice): 4 — INF-01..03, MODEL-02
- Phase 3 (Orchestrated Install & Lifecycle): 15 — ORCH-01..04, CLI-01..07, MODEL-01, MODEL-03, PRIV-01, PRIV-03
- Phase 4 (Chat Integration): 5 — CHAT-01..03, MODEL-04, PRIV-02
- Phase 5 (Control Dashboard): 5 — DASH-01..05

---
*Requirements defined: 2026-06-03*
*Last updated: 2026-06-03 after roadmap creation (traceability mapped)*
