# VillaStraylight

## What This Is

VillaStraylight is a self-hosted, local AI server stack for privacy-conscious power users who want a ChatGPT/Claude-class experience running entirely on their own hardware. A single Go CLI (`villa`) auto-detects the host hardware, recommends suitable models and configuration, generates Podman Quadlet units, and orchestrates a stack of OSS AI services (local inference, chat UI, and a control dashboard) — initially tuned for AMD Strix Halo on Fedora Workstation 44+, with macOS/Apple Silicon planned later.

## Core Value

**Run a capable local AI workspace that "just works" after install** — hardware-aware setup that picks the right models and config so inference, chat, and the control dashboard come up healthy on the user's machine, with zero data leaving the box.

## Current State

**v1.2 Operability shipped 2026-06-08** (PR #3 merged to `main`, merge commit `91c2ae2` — `--merge` preserving history, mirroring v1.0/v1.1; tag `v1.2` pushed on the merge commit; phase branches 12–17 deleted). All 6 phases (12–17) complete; milestone audit **PASSED** — 13/13 requirements satisfied, 5/5 cross-phase integration flows wired, 6/6 phases Nyquist-compliant. Post-merge `/code-review` found+fixed 4 issues (atomic commits `49d5324`/`6ae2077`/`8dcc2c1`/`2ed1699`); CI green. VillaStraylight is now an operable, recoverable, measurable daily-driver: `villa doctor` gives a one-shot read-only health diagnosis (preflight + status + residency-proof + drift, 0/1/2 exit, offload-asserting) (P13); saved bench reports + `villa bench --compare` persist and compare runs behind a comparability guard (P14); cumulative reset-aware token usage is tracked counts-only and surfaced append-only in `status` + dashboard (P15); `villa backup`/`restore` give a self-describing local archive (weights excluded) with transactional skew-warning restore (P16); and a `charmbracelet/huh` guided TUI install composes the finished pipeline byte-identically to the flag path, TTY-gated with `--no-tui` fallback, single static CGO-free binary (P17). An alternate digest-pinned `rocm-6.4.4` backend shipped behind the seam (P12) — its perf hypothesis honestly disproven on-hardware (Vulkan stays the tg default). ~36.9k Go LOC, full suite green, CGO-free static build gated in CI. Vulkan RADV remains the default. All three shipped milestones (v1.0, v1.1, v1.2) are on `main` and tagged.

**v1.1 ROCm Opt-In Backend shipped 2026-06-06** (tag `v1.1`). All 6 phases (6–11) complete; milestone audit `tech_debt` (13/13 requirements satisfied, 0 critical blockers, highest-value debt closed inline by Phase 11). ROCm now ships as a strictly opt-in second inference backend behind the `BackendFor` resolver with a HIP residency proof (P6), a byte-frozen Quadlet render delta + refuse-with-remediation preflight + detect-readiness fields (P7), a transactional `villa backend set` switch with verbatim rollback (P8, 4/4 on-hardware UAT), an honest A/B `villa bench` (P9, live Δpp +4.84 / Δtg −11.15), and backend-aware `recommend`/`status`/dashboard surfacing (P10). Phase 11 made the `rocm_readiness` firmware/HSA detect probes real (live-verified on the gfx1151 host — `villa status` badge reads `ready`, backend `rocm`) and reconciled documentation drift. Vulkan RADV remains the default. 129 Go files, ~26.3k LOC, full suite green (16 packages).

**v1.0 MVP shipped 2026-06-05** (tag `v1.0`, PR #1 → `main`, merge `57f8ef7`). All 5 phases live-verified on real AMD Strix Halo (gfx1151) hardware: `villa` detects the host, recommends a memory-fitting model, installs a rootless Podman Quadlet stack (llama.cpp Vulkan inference + Open WebUI chat), and serves a read-only control dashboard — strictly local, zero telemetry. Phases 4 & 5 STRIDE-secured.

> **Note (release hygiene):** v1.1 reached `main` via PR #2 (merge `2e22d1f`) and is tagged `v1.1` on that merge commit — mirroring v1.0's PR #1 / tag-on-`main` pattern. Both shipped milestones are on `main`.

## Current Milestone: v1.3 Memory & Knowledge (local RAG)

**Goal:** Give VillaStraylight a strictly-local memory system so the assistant remembers the user across chats and can recall past conversations and uploaded documents — integrated, not rebuilt (Go stays control-plane only).

**Target features:**
- **Personalized memory** — facts the assistant remembers about the user across all chats (Open WebUI Memory), injected into future conversations.
- **Conversational recall (RAG)** — semantic retrieval over past chat history.
- **Document knowledge base** — RAG over user-uploaded documents (and optionally chats), with citations.
- **Capture: both modes** — automatic LLM-assisted extraction *and* explicit user save/pin, with edit/delete controls.
- **Delivery: integrate + orchestrate** — new rootless Quadlet services for a local vector DB (e.g. Qdrant) + a local embeddings path; wire Open WebUI's native RAG/Memory to them; `villa` recommends / installs / monitors / backs-up the memory stack. No custom Go RAG engine.

**Key context:** Embeddings must run locally (zero-outbound); exact vector DB + embedding model/runtime are research items. Must preserve all non-negotiables — strictly local, zero telemetry, single static CGO-free binary, config as single source of truth, `orchestrate` the only impure module, byte-frozen contracts evolve append-only + schema bump. v1.2 backup/restore should extend to cover the new memory volumes.

**Candidate themes deferred again (not in v1.3):**
- **Search & agents:** SearXNG local search (SRCH-01), OpenCode coding-agent wiring (CODE-01).
- **Access:** authenticated remote / multi-user access (REMOTE-01), ROCm perf-tuning knobs — hipBLASLt / rocWMMA-FA / batch (ROCM-TUNE-01).
- **Carryover tech debt (non-blocking):** extract a shared `rocmpolicy` leaf package (deferred PR-#2 finding); investigate `rocm-6.4.4-rocwmma` residency FAIL on gfx1151; optional re-pin of the drifted `rocm-6.4.4` rolling tag.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- [x] Hardware auto-detection: identifies CPU/arch, GPU, and total/unified memory on the host — *Validated in Phase 1: `villa detect` reads ghw + amdgpu sysfs on the live Strix Halo host; usable envelope = GTT `mem_info_gtt_total` (62.5 GiB), not MemTotal*
- [x] Recommendation engine: maps detected hardware to a recommended model + quantization + context length + service config — *Validated in Phase 1: `villa recommend` shows the `model_bytes + KV@ctx + headroom ≤ envelope` fit math over a versioned go:embed catalog*
- [x] Non-optional preflight gate refuses/warns before unsafe install (reusable `internal/preflight`, exit 0/2/1) — *Validated in Phase 1*
- [x] llama.cpp `llama-server` inference running on the AMD Strix Halo iGPU via the Vulkan backend, exposing an OpenAI-compatible API — *Validated in Phase 2: real Vulkan-RADV iGPU offload (log + sysfs dual-assert), loopback OpenAI API, live chat completion*
- [x] Single `villa` Go CLI installs and controls the whole stack (install, up/down, status, model management) — *Validated in Phase 3: full lifecycle verb set, idempotent install, config-as-source-of-truth*
- [x] Podman Quadlet (systemd) orchestration: Go generates and manages `.container`/`.network`/`.volume` units, rootless, start-on-boot — *Validated in Phase 3*
- [x] Open WebUI integrated as the chat front end, wired to the local inference API — *Validated in Phase 4: container-DNS wiring to `http://villa-llama:8080/v1`, telemetry killed, durable `:Z` volume; live browser chat + restart persistence confirmed on the gfx1151 host (UAT 4/4)*
- [x] Strictly local operation: all services bind to localhost/LAN; no telemetry; only outbound traffic is image/model downloads during install/update — *Validated through Phase 5: PRIV-01/02/03 loopback + no-telemetry gates, `villa status` loopback-only assertion, SECURED audits (Phase 4 12/12, Phase 5 19/19 STRIDE, threats_open=0)*
- [x] Control dashboard showing service health, performance metrics, token/throughput usage, and available/loaded models — *Validated in Phase 5: read-only dashboard as a read-model over the same internal API as `villa status`; amdgpu-sysfs GPU panel, 409-guarded model switch, chat link; all 5 UAT items live-verified on the gfx1151 host*
- [x] Runs on Fedora Workstation 44+ on AMD Strix Halo — *Validated v1.0: continuously exercised on the live gfx1151 host through all 5 phases, incl. reboot boot-survival*
- [x] Full ROCm preflight + detection (rocminfo/gfx1151, kernel floor, firmware-20251125 block, HSA override) — refuse-with-remediation — *Validated Phase 7 (off-hardware) + Phase 11 (on-hardware): `villa preflight --backend rocm` refuses only on positively-known-bad with named remediation, driven by a `go:embed` `rocm-policy.json`; `villa detect --json` appends a `rocm_readiness` block (schema 1→2, append-only). Phase 11 made the firmware/HSA probes real — live gfx1151 host reports `ready`.*
- [x] ROCm/HIP inference backend, opt-in, behind the existing `Backend` interface, offload-asserting (no false-green CPU fallback) — *Validated v1.1 (Phase 6): `backend_rocm.go` (digest-pinned `rocm-7.2.4`, kfd+dri, render group, HSA→hipBLASLt env, ROCm0 residency markers) selected via the single `BackendFor()` resolver that fails closed; a silent/partial CPU fallback is a FAIL. Vulkan stays the default, byte-identical.*
- [x] `villa backend set rocm|vulkan` switches the inference backend on a running install (fit-guarded, readiness-polled, rollback-safe) — *Validated v1.1 (Phase 8): transactional `internal/backendswap` capture→mutate→prove→rollback; cutover gated on a real generation-probe + residency proof; auto-rolls back verbatim on any failure. 4/4 on-hardware UAT incl. forced CPU-fallback rollback + bounded 5m timeout.*
- [x] `villa bench` proves the Vulkan-vs-ROCm throughput delta — *Validated v1.1 (Phase 9): honest A/B reports prompt-processing and token-generation tok/s separately (never blended), warmup-discarded, N-rep median+stddev, residency-void-gated; `--ab` composes the Phase-8 switch. Live proof-of-value Δpp +4.84 / Δtg −11.15 — ROCm wins pp, regresses tg.*
- [x] Backend-aware `detect`/`recommend` (ROCm readiness + advice; Vulkan stays default) — *Validated v1.1 (Phase 10): `recommend` derives honest ROCm advice ("worth trying / verify with bench / withheld") purely from `rocm_readiness`, never reassigns the backend, never promises a speed-up.*
- [x] Dashboard + `villa status` surface the active backend and live tok/s — *Validated v1.1 (Phase 10): active backend + image tag, live tok/s labeled by backend, tri-state ROCm-readiness badge — append-only, schema-bumped, goldens re-frozen once; `status`'s previously-hardcoded `VulkanBackend()` now reflects the configured backend.*
- [x] `rocm-6.4.4` alternate ROCm image option for TG-heavy models (ROCM-ALT-01) — *Validated v1.2 (Phase 12): two digest-pinned, fail-closed, seam-locked backends (`rocm-6.4.4` + `-rocwmma`) selectable via `BackendFor`, gated by `rocm-policy.json`, honestly benchable via `bench --ab-target`. On-hardware UAT (gfx1151, 2026-06-07): SC#1–4 all PASS as engineering deliverables (incl. a correct offload-asserting residency FAIL + verbatim rollback for `-rocwmma`, and surviving a same-day rolling-tag drift via the content-addressed pin). **Honest perf outcome: `rocm-6.4.4` does NOT recover the v1.1 Δtg −11.15 — Vulkan still leads tg by ~11.68 tok/s; Vulkan stays the tg default, never auto-switched.** The capability + honest measurement shipped; the perf premise it tested is false on this host/model.*
- [x] Saved benchmark reports + `villa bench --compare` (BENCH-03/BENCH-04) — *Validated v1.2 (Phase 14): each `villa bench` run persists one versioned JSONL saved report under `$XDG_DATA_HOME/villa/bench-reports.jsonl` (`schema_version=1`, golden-frozen, pp/tg kept SEPARATE, residency-void recorded, `.Known`-guarded host fingerprint — never fabricated); `bench --compare`/`--list` are read-only, comparability-guarded (model+quant+ctx+host match, backend may differ, refuses on unknown host — no false-equal), 0/2/1 exit. On-hardware UAT (gfx1151, qwen3.6-35b-a3b, 2026-06-07): clean cross-backend round-trip — two records (rocm + vulkan), real `host_gfx_id=gfx1151`, `--compare` printed Δpp −6.58 / Δtg +10.39 (vulkan>rocm) on separate lines, exit 0; independently reproduces the Phase-12 ~+11.68 Δtg finding with genuine timings.*
- [x] `villa doctor` health diagnosis (DOCTOR-01/02/03) — *Validated v1.2 (Phase 13): pure `internal/doctor.Aggregate` composes the shipped preflight gate + status read-model + per-service offload Verdict + config-vs-disk drift into one doctor-owned PASS/WARN/FAIL Report with its OWN `schema_version=1` golden (never mutates `status.Report`); preflight-mirroring 0/1/2 exit, remediation on every non-PASS, offload-FAIL dominates a health-200 (no false-green), ROCm residency-supersession down-ranks typed-Unknown host-prep WARNs so a residency-proven opt-in ROCm install reaches exit 0. On-hardware UAT 3/3 (gfx1151): healthy→0, induced CPU fallback→1, hand-touched unit drift→2.*
- [x] Cumulative usage tracking (USAGE-01/02) — *Validated v1.2 (Phase 15): pure `internal/usage` reset-aware per-model `Fold` over monotonic `_total` counters, counts-only (no prompt/response content), XDG atomic store; surfaced via ONE append-only `status.Report.usage` field (`reportSchemaVersion` 1→2, golden re-frozen once) and the dashboard `/api/metrics` as the SOLE mutex-guarded writer; zero new outbound (reuses the existing bounded loopback `/metrics` scrape). On-hardware UAT 4/4 (gfx1151): live monotonic growth, reset-aware persistence across an `llama-server` restart, no-new-socket, dashboard render.*
- [x] Backup / restore (BAK-01/02/03) — *Validated v1.2 (Phase 16): pure `internal/backup` builds a self-describing single-`.tar` archive (config + Open WebUI volume + usage + bench; model weights EXCLUDED, identities recorded for re-pull) with a SHA-256 manifest; `villa restore` mirrors `backendswap` transactional discipline (capture → quiesce → clean-recreate-before-import → offload-asserting prove → verbatim rollback) with version-skew WARN-and-confirm and fail-closed BLOCK on checksum / incompatible schema. podman volume I/O via a shared cmd-tier fixed-arg seam (orchestrate stays the only impure module). On-hardware UAT 4/5 (gfx1151) + 1 documented cross-host best-effort limitation; UAT found+fixed a real regression (WR-05, fix 8eb2526).*
- [x] Guided TUI install (INSTALL-01/02) — *Validated v1.2 (Phase 17): a `charmbracelet/huh` 5-screen wizard wired into `villa install` as a PURE COLLECTOR over the finished detect→recommend→preflight→backend pipeline — computes nothing itself, threads consent into the SINGLE existing `gateInstall`, writes a `config.toml` byte-identical to the flag path; bypassed by `--no-tui`/`--json`/non-TTY, NO_COLOR/`TERM=dumb` degradable. The milestone's ONLY new dependency, command-tier-only; binary stays a single static CGO-free build (gated in `.github/workflows/ci.yml`; bubbletea pinned v1.3.6, no v2 leak). On-hardware UAT 3/3 (gfx1151), zero mutation.*
- [x] Local vector store + embeddings stack orchestrated as rootless Quadlet services (INFRA-01/02/04, PRIV-04) — *Validated v1.3 (Phases 18–19): digest-pinned Qdrant + dedicated `villa-embed` llama-server (nomic-embed-text-v1.5 Q8_0, 768-dim) rendered behind the orchestrate managed-service seam on `villa.network`, container-DNS only (NO host port); embedding GGUF pre-staged at install (the only sanctioned outbound window); readiness proof asserts offline 768-dim `/v1/embeddings` + Qdrant writable. On-hardware install PASS (gfx1151, 2026-06-09).*
- [x] Personalized memory: assistant remembers user facts across chats (Open WebUI Memory), strictly local (MEM-01) — *Validated v1.3 (Phase 20): env-only D-09 wiring behind the orchestrate seam with mandatory `ENABLE_PERSISTENT_CONFIG=False`; cross-chat injection confirmed through the real web UI on the gfx1151 host (2026-06-10 UAT): saved fact recalled in a new chat, no recall after delete (injection is frontend-mediated in OWUI v0.9.6).*
- [x] Document knowledge base: RAG over uploaded documents with citations (KB-01/02/03) — *Validated v1.3 (Phase 20): real REST upload→embed (villa-embed/768-dim)→Qdrant→cited chat answer on-hardware; citation field = top-level `sources`. Fully local.*
- [x] Capture both ways + edit/delete (MEM-02/03/04) — *Validated v1.3 (Phase 20): explicit save/list/semantic-query/update/delete all confirmed live; automatic extraction exists but is DEFAULT-OFF (SC#2 — store provably empty until a deliberate save) with the enable path documented in `docs/MEMORY.md`.*
- [x] Local embeddings only — zero new outbound beyond image/model pulls (PRIV-05) — *Validated v1.3 (Phase 20): runtime firewalled zero-outbound smoke test `villa verify memory` — negative-control-first (egress-open run FAILs exit 1, proving the gate is real); under a scoped nft egress block the full upload→cite path PASSes exit 0 ("document upload retrieved + cited with zero outbound"). Full offline/telemetry env lockdown frozen by golden + telemetry test.*

### Active

<!-- v1.3 Memory & Knowledge — requirements scoped in .planning/REQUIREMENTS.md (REQ-IDs assigned there). -->

- [ ] Conversational recall: semantic retrieval over past chat history
- [ ] `villa` control-plane support for the memory stack: recommend / install / status+dashboard surfacing / backup-restore coverage

_Full, testable REQ-ID list lives in `.planning/REQUIREMENTS.md`._

### Out of Scope

<!-- Explicit boundaries with reasoning. -->

- **macOS / Apple Silicon / Metal backend** — **PERMANENTLY out of scope** (decided 2026-06-09). VillaStraylight commits to AMD Strix Halo on Fedora/Linux as its target; the `Backend` interface keeps a Metal port *architecturally possible* but it is no longer a planned milestone goal.
- **SearXNG search** — deferred to a later milestone (search & agents)
- **OpenCode (local-model coding agent) wiring** — deferred to a later milestone (search & agents)
- **Voice (Whisper STT / Kokoro TTS)** — explicitly future
- **Agents / agent orchestration (e.g. n8n, custom)** — explicitly future
- **Image generation (ComfyUI) and other DreamServer extras** — not requested for this product
- **Building a custom chat UI** — superseded by Open WebUI; the earlier Go chat scaffold is reference/dev-only, reused opportunistically (e.g. gateway pieces) at most
- **Remote access / multi-user auth** — out of v1; strictly-local posture, designed so authenticated remote access can be added later without rework
- **Rebuilding AI services in Go** — Go is the control plane only; inference/chat/etc. are existing OSS containers

## Context

- **Hardware target:** AMD Strix Halo (Ryzen AI Max) — RDNA 3.5 integrated GPU (gfx1151), large unified/shared memory pool (commonly 64–128 GB). Inference performance hinges on unified-memory configuration and GPU backend choice.
- **GPU access:** On Fedora/Linux today, the Vulkan backend of llama.cpp is the most reliable path for Strix Halo's iGPU; ROCm/HIP for gfx1151 is newer and less stable, kept as an optional alternate backend.
- **Reference prior art:** DreamServer (Light-Heart-Labs) — a similar Strix Halo stack, but Docker-Compose-based with a `dream` CLI and Open WebUI + Qdrant + SearXNG + Perplexica + Whisper/Kokoro + agents. VillaStraylight's distinct angle: **Go control plane, Podman Quadlets, Fedora-native, privacy + performance first.** DreamServer's model/memory-envelope recommendations (e.g. UD-Q4_K_M quants for unified memory; model selection by 64/96/124 GB tiers) are a useful starting reference for the recommendation engine.
- **Existing code:** A small single-binary Go chat-proxy scaffold exists in this repo from earlier this session (OpenAI-compatible streaming + embedded React UI). The project is treated as greenfield; the scaffold may be cannibalized for gateway/streaming code but does not constrain the architecture.
- **Prior decision:** Earlier session chose Go + chi, OpenAI-compatible model connection — consistent with this larger design.

## Constraints

- **Tech stack**: Go for all first-party code (CLI, detection, orchestration, dashboard backend, gateway) — single-language, single static binary, easy self-hosted distribution.
- **Orchestration**: Podman (rootless) via Quadlet/systemd units — native to Fedora; no Docker dependency.
- **Platform (v1)**: Fedora Workstation 44+ on AMD Strix Halo only. Architecture must not hard-code assumptions that block a later macOS/Apple-Silicon/Metal backend.
- **Inference**: llama.cpp `llama-server`, Vulkan backend primary (ROCm optional) — OpenAI-compatible API as the integration contract.
- **Privacy/Security**: Strictly local by default; no telemetry from first-party components; outbound limited to image/model pulls.
- **Performance**: Setup must produce a configuration that actually runs on the detected hardware (right model size/quant/context for the memory envelope) — "runs healthy after install" is the bar.
- **Integration-first**: Reuse mature OSS (Open WebUI, llama.cpp, later Qdrant/SearXNG); build only the control plane.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go control plane; AI services are OSS containers | Avoid rebuilding mature tools; focus effort on hardware-aware orchestration | ✅ Validated (v1.0) — single static `villa` binary orchestrates llama.cpp + Open WebUI containers; no AI service rebuilt |
| llama.cpp + Vulkan as primary inference backend | Most reliable Strix Halo iGPU path on Linux; OpenAI-compatible API; matches DreamServer's llama.cpp choice | ✅ Validated (Phase 2) — RADV offload proven on live gfx1151 (log device_info + sysfs GTT delta), loopback /v1, dual-assert verdict |
| Inference behind a `Backend` interface; offload is offload-asserting (D-11), not liveness | De-risk the project's biggest unknown early; no Vulkan/Linux leak to callers; never false-green a silent CPU fallback | ✅ Validated (Phase 2 + v1.1) — `internal/inference` seam + grep-gate; the v1.1 ROCm backend exercised the seam for the first time with zero caller leakage |
| Podman Quadlets / systemd for orchestration | Native, rootless, boot-on-startup on Fedora; no Docker | ✅ Validated (v1.0) — `.container`/`.network`/`.volume` units generated + reconciled; rootless, boot-survival confirmed on reboot |
| Integrate Open WebUI for chat | Mature, multi-model, RAG-ready; don't reinvent the UI | ✅ Validated (Phase 4) — container-DNS wired to local /v1, telemetry killed, durable volume; live browser chat + restart persistence |
| Single `villa` Go CLI as install + control entry point | Cohesive self-hosted UX; one static binary | ✅ Validated (v1.0) — full lifecycle verb set, idempotent install, config-as-source-of-truth |
| Strictly local, no telemetry, no outbound (except pulls) | Privacy is a stated core value | ✅ Validated (through v1.0) — loopback-only PRIV gates, STRIDE-secured (Phase 4 12/12, Phase 5 19/19); v1.1 posture unchanged |
| v1 = "Core platform"; defer memory/search/OpenCode/voice/agents | Nail one platform end-to-end before breadth | ✅ Good — v1.0 + v1.1 shipped on the focused scope; deferred themes remain deferred |
| AMD Strix Halo / Fedora first; macOS/Metal later | Reduce surface area; one hardware target to optimize | ✅ Good — exercised continuously on the live gfx1151 host; Metal kept behind the `Backend` interface |
| Treat repo as greenfield; earlier Go chat scaffold is reference-only | New vision is far larger than the scaffold; don't let it constrain design | ✅ Good — scaffold not extended; clean `cmd/villa` + `internal/*` architecture |
| ROCm is opt-in; Vulkan RADV stays default; `recommend` advises, never auto-switches | Preserve the v1.0 "just works" bar; ROCm is fragile (firmware/kernel-sensitive); honesty over hype | ✅ Validated (v1.1) — `recommend` never reassigns backend nor promises a speed-up; live Δtg −11.15 proves the honesty constraint was warranted |
| Single polymorphic `BackendFor(cfg.Backend)` resolver, fail-closed | One resolution point so backend choice is honored across install/lifecycle/status/bench/dashboard; never silently fall back to Vulkan | ✅ Validated (Phase 6) — all runtime sites route through it; unknown config string → actionable error, not a silent default |
| Pin `rocm-7.2.4` stable; never `rocm7-nightlies` | Nightlies have a 64 GB allocation-cap bug that blocks large models | ✅ Good — digest-pinned in `backend_rocm.go`; preflight refuses a nightly image request |
| `villa backend set` is transactional (capture→prove→cutover→rollback) | A failed ROCm bring-up must be a no-op to the running stack — the "just works" bar | ✅ Validated (Phase 8) — 4/4 on-hardware UAT incl. forced CPU-fallback rollback + bounded timeout |
| `bench --ab` composes the Phase-8 switch; never re-implements switching | One switch implementation; bench measures, it doesn't orchestrate | ✅ Validated (Phase 9) — `--ab` delegates the flip to `backendswap.Run` |
| Surfacing (Phase 10) lands last; `--json`/goldens re-freeze exactly once | Append-only, schema-bumped, never reordered — protect the frozen dashboard contracts | ✅ Validated (Phase 10) — status/recommend/detect goldens re-frozen once as pure-addition diffs |
| ROCM-ALT-01: ship the alt image as a selectable, honestly-benched capability; never auto-switch; adopt only the digest the A/B proves recovers Δtg | Honesty over hype — prove the perf claim on-hardware, don't promise an unbenchmarked speed-up | ✅ Validated (Phase 12), premise disproven — on-hardware A/B shows `rocm-6.4.4` does NOT recover Δtg (Vulkan leads tg ~11.68 tok/s); capability shipped, Vulkan stays tg default. The honest A/B did exactly its job: prove, don't assume |
| v1.2 new persistence is flat JSONL/JSON under `$XDG_DATA_HOME/villa/` — never in `config.toml`, never embedded SQLite | Keep config the single source of *configuration* truth; CGO-free static binary forbids cgo-SQLite, and pure-Go SQLite is disproportionate for append-mostly data | ✅ Validated (v1.2) — benchstore (P14), usage (P15), backup manifest (P16) all flat-file; `config.toml` unchanged as config authority |
| Each v1.2 decision-logic feature gets ONE new pure `internal/*` core; host effects stay behind an orchestrate/cmd seam | Preserve "orchestrate is the only intentionally-impure module"; keep every core unit-testable off-hardware | ✅ Validated (v1.2) — `doctor`/`benchstore`/`usage`/`backup` are exec-free pure cores (SeamGrepGate green); podman volume I/O is a cmd-tier fixed-arg seam |
| `villa doctor` owns its OWN report schema and only READS `status.Report`; diagnoses + remediates-by-advice, never mutates the install | Don't evolve a second byte-frozen contract; keep mutation in explicit verbs (install / backend set / restore) | ✅ Validated (Phase 13) — doctor `schema_version=1` golden distinct from status; read-only `unitDirReadOnly` resolver creates no Quadlet dir |
| Only ONE byte-frozen contract (`status.Report`) evolves in v1.2, append-only + schema bump 1→2, golden re-frozen once | Staggered persistence rollout so contract risk is never compounded across features in flight | ✅ Validated (Phase 15) — usage field added omitempty; dashboard is the sole mutex-guarded writer of `usage.json` |
| `villa restore` mirrors `backendswap` transactional discipline with clean-recreate-before-import | `podman volume import` MERGES + does not auto-create — a failed/partial restore must never corrupt or leak stale data into the running stack | ✅ Validated (Phase 16) — capture→quiesce→clean-recreate→prove→rollback; on-hardware UAT confirmed no stale-data leak + verbatim rollback |
| `charmbracelet/huh` is the milestone's ONLY new dependency; command-tier only; binary stays single static CGO-free | A guided TUI must not break the single-static-binary constraint or leak presentation into pure cores | ✅ Validated (Phase 17) — huh confined to `cmd/villa`; `CGO_ENABLED=0` build gated in CI; bubbletea pinned v1.3.6 (no v2 leak); wizard config byte-identical to flag path |
| OWUI memory/RAG wiring is env-only behind the orchestrate seam, with `ENABLE_PERSISTENT_CONFIG=False` mandatory from first boot | PersistentConfig bakes env into `webui.db` on first boot then ignores the env — config must stay the single source of truth | ✅ Validated (Phase 20) — D-09 block conditional on `memory_enabled` (off-render byte-identical to v1.2 golden); kill switch frozen in the memory golden + live container env; config drift impossible by construction |
| Zero-outbound is proven at RUNTIME, negative-control-first — never flag-trusted | Install-time green is insufficient: OWUI lazily pulls models from HF on first RAG use; a vacuous egress check would false-green the privacy claim | ✅ Validated (Phase 20) — `villa verify memory`: egress-open run FAILs exit 1 (gate proven real), scoped-block run PASSes with a real upload→cite; auto-extraction confirmed default-off; cross-chat memory injection UI-confirmed (frontend-mediated in OWUI v0.9.6) |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-06-10 after Phase 20 (OWUI Memory/RAG wiring + offline lockdown) — INFRA-03, MEM-01..04, KB-01..03, PRIV-05 all validated on-hardware (UAT 6/6 incl. UI-confirmed cross-chat memory injection; security 16/16 threats closed); Phases 18–19 requirements folded into Validated. Remaining v1.3: Phase 21 (conversational recall indexer), Phase 22 (control-plane fit + host gate), Phase 23 (surfacing/backup/swap). Prior footer: 2026-06-09 — started milestone v1.3 Memory & Knowledge (local RAG): personalized memory + conversational recall + document KB, integrate-and-orchestrate (local vector DB + local embeddings wired into Open WebUI), both auto-extract and explicit-save capture; `villa` control-plane support incl. backup/restore coverage. Mac/Metal moved to PERMANENTLY out of scope. v1.2 reconciled to shipped/merged/tagged (PR #3, merge `91c2ae2`, tag `v1.2`). Prior footer: 2026-06-08 after v1.2 Operability milestone close — all 6 phases (12–17) complete, audit PASSED (13/13 requirements, 5/5 integration flows, 6/6 phases Nyquist-compliant); PR-to-`main` + tag `v1.2` pending via `/gsd-ship`. v1.2 delivered: `villa doctor` (DOCTOR-01/02/03), saved bench reports + `--compare` (BENCH-03/04), cumulative usage tracking (USAGE-01/02), backup/restore (BAK-01/02/03), guided TUI install (INSTALL-01/02), and the alt `rocm-6.4.4` backend (ROCM-ALT-01, perf premise honestly disproven — Vulkan stays the tg default). Full per-phase detail + audit archived under `.planning/milestones/v1.2-*`. v1.1 (Phases 6–11) + v1.0 (Phases 1–5) shipped + tagged on `main`. Next milestone scoped via `/gsd-new-milestone` (fresh REQUIREMENTS.md).*
