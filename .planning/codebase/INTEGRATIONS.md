# External Integrations

**Analysis Date:** 2026-06-07

VillaStraylight is a control plane: Go orchestrates OSS containers and reads the host; it does not embed the AI services. Every external touchpoint below is either a loopback HTTP call to a managed container, a fixed-arg subprocess (`podman` / `systemctl` / host probes), a host filesystem/sysfs read, or an outbound model/image pull. **Privacy posture:** strictly local by default; first-party components emit zero telemetry; the only outbound traffic is image pulls (Podman) and model GGUF pulls (Hugging Face).

## APIs & External Services

**Inference engine — llama.cpp `llama-server` (OpenAI-compatible):**
- Run as a rootless Podman container (`villa-llama`), published loopback-only at `http://127.0.0.1:8080` (container-internal `0.0.0.0:8080`). Endpoint helper: `endpointURL()` in `internal/inference/backend_vulkan.go`.
- Started with mandatory Strix Halo flags `-ngl 999 -fa 1 --no-mmap -lv 4 --metrics` (`llamaServerFlags`, `internal/inference/backend_vulkan.go`); `--metrics` enables the Prometheus endpoint.
- Endpoints consumed by `villa`:
  - `GET /health` - readiness polling (`internal/inference/probe.go`, `runner_podman.go`).
  - `POST /v1/chat/completions` - real chat probe to prove the path end-to-end, and the bench's measured completion (`internal/llm/openai.go`, `internal/inference/probe.go`, `internal/bench/bench.go`). Reads the llama.cpp `/v1` `timings` extension (pp/tg) for honest tokens/s.
  - `GET /metrics` - Prometheus text gauges for the dashboard Performance panel (`internal/metrics/llamacpp.go`, `ScrapeMetrics`).
  - `GET /slots` - narrow per-slot view (context/KV signal, `is_processing` count); deliberately never reads prompt/sampling params (`internal/metrics/llamacpp.go`, `ScrapeSlots`).
- Integration contract: OpenAI-compatible API. llama.cpp performs NO auth; Open WebUI's connection key is a fixed `sk-no-key-required` sentinel (not a secret).
- Auth: none (loopback, no key).

**Chat UI — Open WebUI:**
- Run as a rootless Podman container (`villa-openwebui`), published loopback-only at `127.0.0.1:3000` -> container `:8080` (`openWebUIPublishPort`, `internal/orchestrate/openwebui.go`).
- Reaches inference over the `villa.network` Podman DNS name (`OPENAI_API_BASE_URL=http://villa-llama:8080/v1`), NOT localhost — keeps the chat target in lockstep with the inference unit's `ContainerName`.
- Telemetry kill-set frozen by a golden test (`ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`, `OFFLINE_MODE=True`, `ENABLE_VERSION_UPDATE_CHECK=False`, `HF_HUB_OFFLINE=1`). `ENABLE_OLLAMA_API=False`, `ENABLE_OPENAI_API=True`, `WEBUI_AUTH=True` (local admin persisted in the durable volume).
- Auth: Open WebUI's own local admin account (first-visit setup), persisted in the `villa-openwebui` named volume at `/app/backend/data:Z`.

## Data Storage

**Databases:**
- None first-party. Open WebUI manages its own SQLite/data inside its named Podman volume (`villa-openwebui` -> `/app/backend/data`, `:Z` private SELinux label). `internal/orchestrate/openwebui.go`.

**File Storage:**
- Local filesystem only.
  - CLI config: `$XDG_CONFIG_HOME/villa/config.toml` (mode 0600, dir 0700). `internal/config/villaconfig.go`.
  - Model GGUFs: a host models directory (XDG-based, created 0700), bind-mounted read-only into the inference container at `/models` (`:ro,z`). Download lands here. `internal/download/download.go`, `internal/inference/backend_vulkan.go`.
  - Generated Quadlet units: `~/.config/containers/systemd/` (e.g. `villa-llama.container`, `villa-network.network`, `villa-openwebui.container`/`.volume`). Rendered from `internal/orchestrate/quadlet/*.tmpl`.

**Caching:**
- None first-party.

## Authentication & Identity

**Auth Provider:**
- None for first-party services on the loopback. The control dashboard is loopback-only with a same-origin guard on mutating `/api` routes (`requireSameOrigin`, `internal/dashboard/middleware.go`); no login.
- Open WebUI owns its own local admin auth (`WEBUI_AUTH=True`).
- llama.cpp endpoint is unauthenticated (loopback); the Open WebUI -> inference key is the fixed `sk-no-key-required` sentinel.

## Monitoring & Observability

**Error Tracking:**
- None (no external error/telemetry service — privacy constraint).

**Logs:**
- Container/service logs read via the user journal: `journalctl --user -u <service> --no-pager`, bounded by `io.LimitReader` (256 KiB). `villa logs` and the status GPU-residency scrape use this. `internal/orchestrate/systemd.go` (`JournalText`, `ResidencyJournal`).
- Transient bench/probe runs capture bounded llama-server stderr directly from the `podman run` child (8 KiB cap). `internal/inference/runner_podman.go`.
- Dashboard backend uses chi's `middleware.Logger` for request logging (local stdout only).

## CI/CD & Deployment

**Hosting:**
- Self-hosted single binary on the user's Fedora host. No cloud/PaaS.

**Orchestration runtime (the real "deploy"):**
- Rootless Podman v5 via Quadlet `.container`/`.network`/`.volume` units + user systemd. `villa` writes units, runs `systemctl --user daemon-reload`, then `start`/`enable`/`stop`/`restart`/`disable`, and `loginctl enable-linger`/`disable-linger` for boot-survival. All fixed-arg `exec.Command`, never a shell. `internal/orchestrate/systemd.go`, `cmd/villa/install.go`, `lifecycle.go`, `uninstall.go`.
- Transient inference runs (probe/bench) use `podman run --rm` directly via fixed-arg `exec.Command`. `internal/inference/runner_podman.go`.

**CI Pipeline:**
- None detected in-repo (no `.github/workflows`, etc.). Local quality gates only: `make check` (`go vet` + `go test`) and optional `golangci-lint` (`.golangci.yml`).

## Container Control Plane (Podman)

- **Podman is driven via the rootless `podman` CLI**, NOT the REST socket and NOT the `containers/podman/v5` bindings (deliberate, to preserve the single-static-binary goal). All invocations are fixed-arg `exec.Command("podman", …)` (`internal/inference/runner_podman.go`) with `exec.LookPath` probing and bounded output capture.
- Preflight verifies the rootless Podman environment (`internal/preflight/checks_podman.go`), GPU device access (`checks_gpu.go`), SELinux (`checks_selinux.go`), linger (`checks_linger.go`), resources (`checks_resources.go`), and ROCm policy (`checks_rocm.go`).
- SELinux enablement for device access is a consented privileged step: `setsebool -P container_use_devices=true` (`cmd/villa/install_hostprep.go`).

## Host Probes (read-only hardware/OS integrations)

- `github.com/jaypipes/ghw` - CPU/arch (`internal/detect/cpu.go`) and total physical memory (`internal/detect/memory.go`), root-less.
- sysfs / procfs reads (`internal/detect/memory.go`, `gpu_amd.go`):
  - `/sys/class/drm/<amd card>/device/mem_info_gtt_total` / `mem_info_gtt_used` / `mem_info_vram_used` - unified-memory (GTT) ceiling and live usage (vendor 0x1002 card discovery, never `card0`).
  - `/sys/class/drm/<amd card>/device/gpu_busy_percent` - best-effort iGPU utilization (amd-smi/rocm-smi report N/A on gfx1151).
  - `/sys/module/ttm/parameters/pages_limit`, `/proc/meminfo` (MemAvailable), `/proc/sys/kernel/osrelease` (kernel version), `/dev/dri` enumeration, `/usr/share/vulkan/icd.d/radeon_icd.x86_64.json` (RADV ICD).
- Fixed-arg host tools, each `exec.LookPath`-probed and degrading to typed-Unknown when absent (`internal/detect/gpu_amd.go`):
  - `vulkaninfo --summary` - real-GPU device name + Mesa driver version (skips llvmpipe/software renderers).
  - `rocminfo` - gfx target id (e.g. `gfx1151`) and ROCm presence.
  - `rpm -q --qf %{VERSION} linux-firmware` - firmware date stamp for ROCm policy.

## Environment Configuration

**First-party env vars:**
- The `villa` control plane does NOT read runtime env for its own config — it reads `config.toml` (TOML, not env). The legacy `.env.example` (`VS_*` vars) belongs to the superseded `web/` scaffold and is not part of v1.1.

**Container env vars `villa` SETS (into Quadlet units / `podman run`):**
- Inference (ROCm only): `HSA_OVERRIDE_GFX_VERSION=11.5.1`, `ROCBLAS_USE_HIPBLASLT=1` (`internal/inference/backend_rocm.go`).
- Open WebUI: the connection + telemetry-kill set listed under "Chat UI" above (`internal/orchestrate/openwebui.go`).

**Secrets location:**
- No secrets stored or required. The only key-shaped value is the fixed non-secret sentinel `sk-no-key-required`. Download code deliberately never logs the signed-CDN redirect URL (only the canonical HF URL). `internal/download/download.go`.

## Webhooks & Callbacks

**Incoming:**
- None. The control dashboard exposes only loopback read-only `GET` routes plus one same-origin-guarded `POST /api/models/switch` (`internal/dashboard/server.go`).

**Outgoing:**
- None to third parties beyond resource acquisition:
  - **Image pulls** via Podman (Docker Hub `docker.io/kyuz0/...`, GHCR `ghcr.io/open-webui/...`).
  - **Model pulls** via Go stdlib `net/http` from Hugging Face (`https://huggingface.co/.../resolve/main/*.gguf`), following the signed-CDN redirect; HEAD-verify `X-Linked-Size`/`X-Linked-Etag`, stream + incremental SHA256, atomic rename. `internal/download/download.go`, `internal/catalog/seed.json`.

---

*Integration audit: 2026-06-07*
