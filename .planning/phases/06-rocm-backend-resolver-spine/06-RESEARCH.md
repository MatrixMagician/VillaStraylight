# Phase 6: ROCm Backend + Resolver Spine - Research

**Researched:** 2026-06-05
**Domain:** Go control-plane backend-seam extension (polymorphic resolver + per-backend residency proof) for an opt-in ROCm/HIP llama.cpp backend on AMD Strix Halo (gfx1151)
**Confidence:** HIGH (seam shape, call sites, ROCm log/residency format, image digest all verified against the live tree and authoritative sources)

## Summary

Phase 6 is a **pure-Go, off-hardware refactor** of an existing, well-factored v1.0 seam. There are no new Go module dependencies, no I/O, no host calls in the package under change — every deliverable is unit-testable with fixtures. The work is four tightly-coupled moves on `internal/inference/`: (1) add a one-method `ResidencyProof() ResidencyMarkers` accessor to the `Backend` interface; (2) parameterize the **two** existing Vulkan-hardcoded scrape paths (`scrapeOffloadLog` in `offload.go` and `scrapeLoadTensorsVulkan` in `running_offload.go`) by that marker set so Vulkan stays byte-identical and ROCm slots in; (3) add `backend_rocm.go` (digest-pinned image, `/dev/kfd`+`/dev/dri`, render group, ordered HSA/HIPBLASLT env, same mandatory flags) as the sibling of `backend_vulkan.go`; (4) add `BackendFor(name) (Backend, error)` and re-route the 7 hardcoded `VulkanBackend()` call sites through it.

The single most important grounding fact: **there are TWO scrape functions to parameterize, not one.** The CONTEXT.md and ARCHITECTURE.md call out `running_offload.go`'s `scrapeLoadTensorsVulkan`, but `offload.go`'s `scrapeOffloadLog` (the start-time `Validate` path, validate.go:122) ALSO hardcodes Vulkan device detection (`"ggml_vulkan:"`, `"- Vulkan"`, the software-renderer reject). A planner that touches only `running_offload.go` will leave the start-time offload-assert Vulkan-only and silently fail ROCm residency on the `install`/`villa inference` path. Both must be threaded through `ResidencyMarkers`.

**Primary recommendation:** Implement exactly the ARCHITECTURE.md Pattern-1 design — a 3-field `ResidencyMarkers` struct returned by `Backend.ResidencyProof()`, consumed by renamed `scrapeOffloadLog(stderr, m)` and `scrapeLoadTensorsResidency(journal, m)`; `combineOffload`/`Verdict`/typed-Unknown vocabulary untouched. Pin the ROCm image to the **already-resolved digest** `sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89` (resolved on this dev box 2026-06-05 via `skopeo inspect`), eliminating the TODO-digest fallback path entirely.

## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** `BackendFor` lives in `internal/inference`. Signature `func BackendFor(name string) (Backend, error)`.
- **D-02:** Fail-closed on unknown backend (actionable error, never silent Vulkan fallback). Empty string → `"vulkan"` default. `"vulkan"` and `"rocm"` are the only accepted values this milestone.
- **D-03:** Each of the 7 call sites (`cmd/villa/{install,lifecycle,status,dashboard,model,inference}.go` + `internal/status/status.go`) resolves via `BackendFor(cfg.Backend)` and surfaces the error through its existing error path. v1.0 suite stays green under the Vulkan default.
- **D-04:** Extend `Backend` with `ResidencyProof()` returning a backend-owned descriptor (device token, model-buffer phrase, fault-string, sysfs busy signal). Each backend file owns its marker literals — they stay behind the seam.
- **D-05:** Refactor `running_offload.go` to be parameterized by the descriptor rather than hardcoding `"Vulkan0"`. Combine discipline + typed-Unknown contract reused verbatim. Vulkan behavior byte-identical (existing Vulkan offload tests stay green).
- **D-06:** ROCm proof asserts a `ROCm0` device-buffer line + `offloaded N/N` (N==M) + non-zero sysfs `gpu_busy_percent` during a real decode + absence of `Memory access fault by GPU node`. Silent/partial CPU fallback = FAIL; unevaluable signal = WARN.
- **D-07:** Build the pure parse/verdict logic + table-test fixtures + grep-gate NOW (off-hardware). Live-only signals (gpu_busy during decode, live fault-scan) wired in as inputs now, exercised on hardware in Phase 8.
- **D-08:** `backend_rocm.go` carries `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4` pinned by `@sha256:…`. Never nightlies. If digest unresolvable off-hardware, pin tag with marked TODO-digest constant + a failing-until-real-digest test. *(Resolution: digest IS resolvable on this dev box — see Standard Stack; the TODO fallback is not needed.)*
- **D-09:** ROCm `ContainerArgs` = delta over Vulkan: add `--device /dev/kfd` AND `--device /dev/dri`, the `render` group via `--group-add`, ordered env `HSA_OVERRIDE_GFX_VERSION=11.5.1` then `ROCBLAS_USE_HIPBLASLT=1`; keep `-ngl 999 -fa 1 --no-mmap` and loopback-only host publish. (Rendered-unit byte-golden is Phase 7.)
- **D-10:** Positive-presence grep-gate for `ROCm0`/HIP markers in `backend_rocm.go` + extend negative seam gate to the rocm image token.

### Claude's Discretion

- Exact descriptor struct name/shape for `ResidencyProof()` and how the parameterized scraper is factored (single function vs per-signal helpers) — provided Vulkan stays byte-identical and ROCm markers stay behind the seam.
- Exact error type/wording for the fail-closed unknown-backend path, as long as it is actionable and routed through each call site's existing error handling.

### Deferred Ideas (OUT OF SCOPE)

- ROCm Quadlet unit rendering + byte-golden + ROCm preflight/detect — **Phase 7** (ROCM-03, PRE-06, DET-04).
- `villa backend set rocm|vulkan` switch verb + transactional rollback + live generation-probe/residency cutover — **Phase 8** (BSET-01/02/03, on-hardware).
- `villa bench` honest A/B (separate pp/tg tok/s) — **Phase 9** (BENCH-01/02).
- Backend-aware `recommend` + dashboard/`status` active-backend + live tok/s — **Phase 10** (REC-05, DASH-06).
- Live `gpu_busy_percent`-during-decode + live fault-scan exercise — logic built in Phase 6, validated on real gfx1151 hardware in Phase 8.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ROCM-01 | Opt-in ROCm/HIP `llama-server` backend behind the existing `Backend` interface, selected by `backend` config field; no specifics leak to callers. | `backend_rocm.go` mirrors `backend_vulkan.go` (Standard Stack + Code Examples); `BackendFor` resolves config string; seam grep-gate proves no leak. |
| ROCM-02 | ROCm backend offload-asserting via HIP residency proof (`ROCm0` buffer line + `offloaded N/N` (N==M) + non-zero `gpu_busy_percent` during decode + absence of `Memory access fault by GPU node`); CPU fallback = FAIL; grep-gate prevents dropping HIP markers. | Exact log line shapes verified (Architecture Patterns); `ResidencyMarkers` threads the tokens through BOTH scrape paths; busy/fault wired as inputs now (D-07). |
| ROCM-04 | Single polymorphic resolver `BackendFor(cfg.Backend)` routes every inference-backend call site (replacing the 7 hardcoded `VulkanBackend()` sites). | All 7 sites located with their error paths (Architecture Patterns → Call-Site Re-Route Map); smallest-blast-radius re-route documented. |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Config string → concrete `Backend` resolution | `internal/inference` (seam) | — | D-01: the one polymorphism point; all backend literals already live here. |
| ROCm image/device/env/flag literals | `internal/inference/backend_rocm.go` | — | Seam discipline (SC#4): backend literals never leak to callers. |
| Per-backend residency marker set | `internal/inference` (`Backend` iface + impls) | — | D-04: markers are backend-owned, consumed by neutral scrape logic. |
| Offload-assert parse/verdict (pure) | `internal/inference` (`offload.go`, `running_offload.go`) | — | Pure-library: accepts text/bytes, returns `Verdict`; no I/O. |
| sysfs `gpu_busy_percent` read (live) | `internal/detect` (`gpu_amd.go`) | `cmd/villa` (wiring) | Already exists (`GPUBusyPercent`/`GPUBusyPercentForTest`); seam-allowed location. |
| journald / HTTP / sysfs I/O | `cmd/villa` + `internal/status` | — | Pure/cmd split: I/O stays in the cmd layer (status.go reads journal, calls the pure verdict). |
| Config `Backend` field (source of truth) | `internal/config` | — | Already present, default `"vulkan"`; no schema change needed. |

## Standard Stack

### Core

This phase adds **no new Go module dependencies.** It uses only stdlib (`bufio`, `strings`, `strconv`, `fmt`, `errors`/`fmt.Errorf`) and existing internal packages (`internal/detect` for typed-Unknown `Bool`/`Bytes`/`Int`). Go 1.26.2 (from go.mod). `[VERIFIED: go.mod]`

The only external artifact added is a **container image** (consumed at runtime by podman, not a Go dependency):

| Artifact | Pin | Purpose | Why Standard |
|----------|-----|---------|--------------|
| ROCm inference image | `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89` | Opt-in higher-throughput HIP `llama-server` on gfx1151 | Same trusted community-reference family as the already-shipped Vulkan image (`kyuz0/amd-strix-halo-toolboxes`); kyuz0 README labels `rocm-7.2.4` "Latest stable 7.x build, kernel 6.18.4+ patch." Never nightlies (64 GB cap). `[VERIFIED: skopeo inspect 2026-06-05]` `[CITED: github.com/kyuz0/amd-strix-halo-toolboxes]` |

**Digest resolution (done — TODO-fallback NOT needed):**
```
$ skopeo inspect --format '{{.Digest}}' docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4
sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89
```
Resolved 2026-06-05 on the dev box. Mirror the `backend_vulkan.go:15-19` comment style (record the resolve date + the RESEARCH legitimacy note) in `backend_rocm.go`. The existing Vulkan pin (`sha256:9a74e555…`) is unchanged.

**Recommendation on the TODO-digest guard test (D-08):** Since the digest is resolved, pin the real digest directly. Still add a cheap **format-guard test** asserting `BackendFor("rocm").Image()` contains `@sha256:` and a 64-hex-char digest (mirrors `inference_test.go:76-77`'s `TestImageDigestPinned` for Vulkan). This protects against a future hand-edit dropping the pin — it passes now (real digest) rather than failing-until-resolved.

### Supporting

| Component | Already exists | Use |
|-----------|----------------|-----|
| `detect.GPUBusyPercent()` / `GPUBusyPercentForTest(drmRoot)` | `internal/detect/gpu_amd.go:364-369` | The sysfs busy signal source for the ROCm residency proof. Returns typed-Unknown `detect.Int`. Wire as an **input** to the verdict in Phase 6 (descriptor names the signal); the live decode-time read happens in Phase 8. |
| `detect.GTTUsedBytes` / `GTTUsedBytesForTest` | `internal/detect` | The backend-agnostic GTT floor signal, reused verbatim (resident weights move GTT regardless of Vulkan vs HIP). |
| `detect.Bool` / `Bytes` / `Int` (typed-Unknown) | `internal/detect` | The Known/Unknown vocabulary every signal already uses. |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `kyuz0:rocm-7.2.4` | `kyuz0:rocm-6.4.4` | Documented opt-in alternate for TG-heavy models where 7.2.4 regresses; deferred to Phase 9 bench (ROCM-ALT-01, out of scope). |
| `kyuz0:rocm-7.2.4` | `ghcr.io/ggml-org/llama.cpp:server-rocm` | Cleaner supply-chain provenance but loses gfx1151-specific patches/rocWMMA tuning. Not the v1.1 default. |
| 3-field `ResidencyMarkers` struct | A matcher closure (`func(line) bool`) per backend | Closure is harder to fixture/golden and would push logic out of the neutral scraper. The struct keeps the scrape pure and the markers declarative — preferred (Claude's Discretion D-04). |

## Package Legitimacy Audit

No Go packages are installed this phase (stdlib + existing internal only). `slopcheck` (`/home/oliverh/.local/bin/slopcheck`) targets package registries (npm/PyPI), not container images, so it does not apply to the ROCm image. Container image legitimacy is established by **provenance + digest pin**:

| Artifact | Registry | Provenance | Digest pinned | Disposition |
|----------|----------|------------|---------------|-------------|
| `kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4` | docker.io | Same org as the already-shipped, already-audited Vulkan image (`vulkan-radv`); kyuz0 is the de-facto Strix Halo community reference (CLAUDE.md HIGH source) | `sha256:2da150c1f025…` (resolved 2026-06-05) | Approved — pin digest |

**Packages removed due to slopcheck [SLOP]:** none (no Go packages added).
**Packages flagged [SUS]:** none.

## Architecture Patterns

### System Architecture Diagram

```
   config.toml (backend = "vulkan" | "rocm")
        │
        ▼
   cfg.Backend  ──►  inference.BackendFor(name) (Backend, error)   ◄── NEW (D-01/D-02)
        │                    │  fail-closed on unknown; "" → vulkan
        │                    ▼
        │        ┌───────────────────────────┐
        │        │  Backend interface        │
        │        │   Name / Image            │
        │        │   ContainerArgs(spec)     │
        │        │   ResidencyProof() ◄── NEW (D-04) │
        │        └───────────────────────────┘
        │            ▲                 ▲
        │   backendVulkan{}      backendROCm{}  ◄── NEW backend_rocm.go (D-08/D-09)
        │   Vulkan0 markers      ROCm0 markers (behind seam)
        │
        ├──► 7 call sites: install, lifecycle, status, dashboard,
        │    model, inference (cmd/villa) + internal/status/status.go
        │       (each: VulkanBackend() → BackendFor(cfg.Backend), surface err)  (D-03/ROCM-04)
        │
        ▼
   OFFLOAD-ASSERT (pure, two entry points — BOTH parameterized by ResidencyProof()):
   ┌──────────────────────────────────────────────────────────────────┐
   │ START-TIME  validate.go → scrapeOffloadLog(stderr, markers)       │  ◄── offload.go (also Vulkan-hardcoded today!)
   │ RUNNING     status.go   → scrapeLoadTensorsResidency(journal, m)  │  ◄── running_offload.go
   │     + offloadSysfsDelta / gttFloor (backend-agnostic, reused)     │
   │     + gpu_busy_percent input (wired now, live-read Phase 8)       │
   │              │                                                     │
   │              ▼  combineOffload(log, sysfs)  ── UNCHANGED (D-05)    │
   │          Verdict {PASS | WARN | FAIL}  ── frozen contract         │
   └──────────────────────────────────────────────────────────────────┘
```

### Recommended Project Structure (changes only)

```
internal/inference/
├── inference.go            # MODIFIED: + ResidencyProof() on Backend; + ResidencyMarkers struct
├── backend.go (NEW or in inference.go)  # + BackendFor(name) resolver (D-01)
├── backend_vulkan.go       # MODIFIED: implement ResidencyProof() returning Vulkan0 markers
├── backend_rocm.go         # NEW: backendROCm{} — image, kfd+dri, render group, HSA/HIPBLASLT env, ResidencyProof() ROCm0 markers
├── offload.go              # MODIFIED: scrapeOffloadLog(stderr, m) keys off markers (NOT hardcoded "ggml_vulkan:"/"- Vulkan")
├── running_offload.go      # MODIFIED: scrapeLoadTensorsVulkan → scrapeLoadTensorsResidency(journal, m)
├── seam_test.go            # MODIFIED: extend negative gate to rocm image token; (or new file) positive ROCm-marker gate
├── backend_rocm_test.go    # NEW: ContainerArgs assertions (kfd+dri, render group, ordered env, flags), image-digest-pin guard
├── offload_test.go         # MODIFIED: add ROCm fixtures + marker-driven cases; Vulkan cases UNCHANGED
├── running_offload_test.go # MODIFIED: same — ROCm residency fixtures alongside Vulkan
└── testdata/
    ├── load_tensors_rocm.txt        # NEW: ROCm0 N/N PASS fixture
    ├── load_tensors_rocm_cpu.txt    # NEW: CPU-only fallback FAIL fixture
    ├── load_tensors_rocm_fault.txt  # NEW: "Memory access fault by GPU node" FAIL fixture
    ├── rocm_devinfo_pass.stderr     # NEW: start-time stderr ROCm device PASS
    └── rocm_offloaded_partial.stderr# NEW: offloaded 1/65 partial → FAIL
```

### Pattern 1: `ResidencyMarkers` descriptor + parameterized scrape (answers Q1 + Q2)

**What:** A small backend-owned struct returned by `Backend.ResidencyProof()`; the two scrape functions take it as a parameter instead of hardcoding `"Vulkan0"`.

**Descriptor shape** (extend the ARCHITECTURE.md proposal to cover BOTH scrape paths and the D-06 signals):

```go
// ResidencyMarkers is the per-backend marker set the (backend-neutral) offload
// scrape keys on. Each backend file owns its literals; callers never name them.
type ResidencyMarkers struct {
    // DeviceToken is the load_tensors buffer-line device token: "Vulkan0" | "ROCm0".
    DeviceToken string
    // DeviceLabel is the device_info enumeration prefix used by the START-TIME
    // scrapeOffloadLog: "- Vulkan" | "- ROCm". (Vulkan also recognizes the old
    // "ggml_vulkan:" line; ROCm's analog is "ggml_cuda_init: found N ROCm devices".)
    DeviceLabel string
    // StartLogDevicePrefix is the older single-line device enumeration prefix for
    // the start-time path ("ggml_vulkan:" | "ggml_cuda_init:"). Empty disables it.
    StartLogDevicePrefix string
    // FaultString is the journal abort marker that voids residency even if a buffer
    // line is present: "" for Vulkan (no analog used), "Memory access fault by GPU node"
    // for ROCm (D-06). A non-empty FaultString found in the journal → FAIL.
    FaultString string
    // The CPU-only fallback line ("CPU model buffer size") is GENERIC and already
    // handled. The Vulkan software-renderer reject keeps using
    // detect.IsSoftwareRendererName verbatim (Vulkan-only; ROCm has no llvmpipe analog).
    RejectSoftwareRenderer bool // true for Vulkan, false for ROCm
}
```

> **Naming-collision note for the grep-gate (D-10):** ROCm's start-time device-init line is `ggml_cuda_init: found N ROCm devices` — the HIP backend reuses the CUDA code path's log prefix. The positive grep-gate should assert on `ROCm0` (and optionally `ROCm_Host`) which are unambiguous, NOT on `ggml_cuda` which is shared. `[CITED: github.com/ollama/ollama/issues/14855; github.com/ROCm/ROCm/issues/5534]`

**Vulkan markers** (must reproduce today's exact behavior — byte-identical, D-05):
```go
func (backendVulkan) ResidencyProof() ResidencyMarkers {
    return ResidencyMarkers{
        DeviceToken:            "Vulkan0",
        DeviceLabel:            "- Vulkan",
        StartLogDevicePrefix:   "ggml_vulkan:",
        FaultString:            "", // Vulkan path uses software-renderer reject, not a fault string
        RejectSoftwareRenderer: true,
    }
}
```

**ROCm markers:**
```go
func (backendROCm) ResidencyProof() ResidencyMarkers {
    return ResidencyMarkers{
        DeviceToken:            "ROCm0",
        DeviceLabel:            "- ROCm",
        StartLogDevicePrefix:   "ggml_cuda_init:", // HIP reuses the cuda-init log prefix
        FaultString:            "Memory access fault by GPU node",
        RejectSoftwareRenderer: false,
    }
}
```

**Critical refactor scope (answers Q1):** BOTH scrape functions are Vulkan-hardcoded today and BOTH must be threaded:
- `running_offload.go:218` — `scrapeLoadTensorsVulkan(in.JournalText)` → `scrapeLoadTensorsResidency(in.JournalText, in.Markers)`. The constants `vulkanDeviceToken = "Vulkan0"` / `bufferSizePhrase` / `loadTensorsPrefix` (lines 74-78) become driven by `m.DeviceToken` (keep `loadTensorsPrefix`/`bufferSizePhrase` generic — they are backend-neutral). Add the `FaultString` scan (D-06): if `m.FaultString != "" && strings.Contains(journal, m.FaultString)` → FAIL before the buffer-line switch.
- `offload.go:74` — `scrapeOffloadLog(stderr)` → `scrapeOffloadLog(stderr, m)`. Replace the hardcoded `strings.HasPrefix(line, "ggml_vulkan:")` (line 109) and `strings.Index(line, "- Vulkan")` (line 123) with `m.StartLogDevicePrefix` / `m.DeviceLabel`. Gate the software-renderer reject on `m.RejectSoftwareRenderer`.

**Thread the markers into the input structs:** `RunningOffloadInput` gains a `Markers ResidencyMarkers` field; `ValidateInput` already carries a `Backend` (validate.go builds `spec` from it) so `scrapeOffloadLog` can read `in.Backend.ResidencyProof()` directly. **Callers never name a backend** — `status.go:228` passes `backend.ResidencyProof()` (where `backend` is now `BackendFor(cfg.Backend)`); `validate.go:122` passes `in.Backend.ResidencyProof()`.

**When to use:** This is THE design. `combineOffload`/`Verdict`/`Status`/typed-Unknown are frozen and untouched (D-05).

### Pattern 2: `BackendFor` resolver — smallest-blast-radius re-route (answers Q3)

```go
// BackendFor maps a config backend string to its concrete Backend. It is the single
// polymorphism point (D-01). Fail-closed: an unrecognized value is an actionable
// error, never a silent Vulkan fallback (D-02). Empty → vulkan (config defaults to
// "vulkan"; empty only arises from a hand-edited file).
func BackendFor(name string) (Backend, error) {
    switch name {
    case "", "vulkan":
        return backendVulkan{}, nil
    case "rocm":
        return backendROCm{}, nil
    default:
        return nil, fmt.Errorf("unknown inference backend %q: set backend = \"vulkan\" (default) or \"rocm\" in config.toml", name)
    }
}
```

**Call-Site Re-Route Map (all 7 — answers Q3 + ROCM-04).** Two shapes exist; both already have an error path:

| # | File:line | Current | Re-route | Error path |
|---|-----------|---------|----------|------------|
| 1 | `cmd/villa/install.go:234` | `RenderInput{Backend: inference.VulkanBackend(), …}` | `b, err := BackendFor(cfg.Backend)` then `Backend: b` | `fmt.Fprintf(errOut, "install: …"); return exitBlocked` (matches existing render-fail handling) |
| 2 | `cmd/villa/install.go:632` | `NewContainerRunner(VulkanBackend(), …).Endpoint()` | resolve `b` once in `liveInstallDeps`; pass `b` | this is a deps-wiring helper; resolve before constructing deps, return error up |
| 3 | `cmd/villa/lifecycle.go:66` | `RenderInput{Backend: VulkanBackend(), …}` | `b, err := BackendFor(cfg.Backend)` | `return nil, "", fmt.Errorf("resolve backend: %w", err)` (matches surrounding `%w` style) |
| 4 | `cmd/villa/status.go:135` | `NewContainerRunner(VulkanBackend(), …).Endpoint()` | resolve in `liveStatusDeps` | deps-wiring; surface via the deps constructor |
| 5 | `cmd/villa/dashboard.go:130` | `NewContainerRunner(VulkanBackend(), …).Endpoint()` | reuse `liveStatusDeps`'s resolved endpoint | dashboard already folds `liveStatusDeps`; single resolution point |
| 6 | `cmd/villa/model.go:361` | `RenderInput{Backend: VulkanBackend(), …}` (inside `ReconcileAndWrite` closure) | `b, err := BackendFor(c.Backend)` | `return false, err` (closure already returns `(bool, error)`) |
| 7 | `cmd/villa/inference.go:113` | `backend := inference.VulkanBackend()` | `backend, err := BackendFor(cfg-source.Backend)` — note: inference.go reads via `recommend.Pick`/`detect.Probe`, confirm the cfg source in plan | return the existing `inference.Verdict{Status: FAIL, …}` refusal shape (mirrors the CR-02 refusal at line 127) |
| — | `internal/status/status.go:180` | `RenderInput{Backend: VulkanBackend(), …}` | `b, err := BackendFor(cfg.Backend)` | `return Report{Overall: inference.StatusFail.String(), …, err: err}` (matches the surrounding ModelFile/Render error returns) |

**Test-helper impact (answers Q3 — the v1.0 suite stays green):** Six TEST files construct `inference.VulkanBackend()` directly (`cmd/villa/status_test.go:35`, `internal/status/status_test.go:30`, `internal/dashboard/api_test.go:29`, `internal/inference/inference_test.go:12-13,22,67,76`, `internal/orchestrate/render_test.go:24`). These pass a `Backend` value into `RenderInput`/`NewContainerRunner` — they do NOT call `BackendFor`. **Keep `VulkanBackend()` as an exported constructor** (do not remove it); the re-route changes only the 7 *non-test* call sites to go through `BackendFor`. The test helpers continue to inject `VulkanBackend()` directly, so they stay green unchanged — this is exactly why SC#4's "behavior no-op under the Vulkan default" holds. `[VERIFIED: grep of live tree]`

### Pattern 3: Wiring `gpu_busy_percent` as an input now, live-read Phase 8 (answers Q5)

The sysfs busy reader already exists: `detect.GPUBusyPercent()` (live host root) and `detect.GPUBusyPercentForTest(drmRoot)` (injected fixture root), `internal/detect/gpu_amd.go:357-396`. It returns typed-Unknown `detect.Int`. For Phase 6:

- Add a `GPUBusyPercent detect.Int` field to the residency input struct(s) and a verdict rule keyed on the descriptor: the busy signal is a corroborating signal (like the GTT floor). A known non-zero busy during a decode corroborates PASS; a known-zero busy during a claimed-healthy decode is a FAIL signal (R1); an Unknown busy degrades to WARN — same combine discipline.
- In Phase 6, fixtures inject the busy reading via `GPUBusyPercentForTest` against a temp drmRoot containing a `gpu_busy_percent` file (mirror the existing `mem_info_gtt_used` fixture pattern in `running_offload_test.go:33-37`). The *live, decode-time* read (`GPUBusyPercent()` called from `cmd/villa` mid-generation) is Phase 8's job — Phase 6 proves the verdict logic, not the timing.
- **Keep it secondary, not the primary gate** (STACK.md): the `load_tensors ROCm0` line is the primary residency proof; busy% and GTT floor corroborate. Do not let an Unknown busy (idle server, ROCm #6035 N/A) FAIL a load-time PASS.

### Anti-Patterns to Avoid

- **Parameterizing only `running_offload.go`:** leaves `offload.go`'s start-time `scrapeOffloadLog` Vulkan-only → ROCm `install`/`villa inference` offload-assert silently never matches → false WARN/FAIL on a healthy ROCm load. Thread BOTH.
- **Grepping `Vulkan0` when ROCm is active (or vice-versa):** false-FAILs a healthy load. The descriptor exists precisely to assert the correct token per active backend. `[CITED: research/STACK.md:106,148]`
- **Removing `VulkanBackend()`:** breaks six test helpers and the no-op guarantee. Keep it; only re-route the 7 non-test sites.
- **Grep-gating on `ggml_cuda`:** shared with the CUDA path, ambiguous. Gate on `ROCm0`.
- **A TODO-digest constant:** unnecessary — the digest is resolved (`sha256:2da150c1f025…`). Pin it real.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| PASS/WARN/FAIL combine | A new ROCm verdict combiner | `combineOffload` verbatim (offload.go:278) | D-05 — re-rolling the offload math risks diverging the typed-Unknown contract. |
| Typed-Unknown signals | ad-hoc bool/error pairs | `detect.Bool` / `Bytes` / `Int` | The anti-silent-fallback contract is already encoded; reuse it. |
| sysfs busy/GTT read | new `/sys` parser | `detect.GPUBusyPercent[ForTest]`, `detect.GTTUsedBytes[ForTest]` | vendor-0x1002 card discovery + typed-Unknown already handled, never `card0`. |
| Image digest resolution at runtime | podman-pull-and-parse in Go | Resolve once at dev time (`skopeo inspect`), pin the literal | Reproducibility + the offload-assert both require a frozen image (STACK.md). |
| Buffer-MiB / offloaded-N/M parsing | new line parsers | `parseBufferMiB` (running_offload.go:153), `parseOffloadedLayers` (offload.go:184) | Already battle-tested against the Vulkan fixtures; the line shape is identical bar the device token. |

**Key insight:** Phase 6 is ~90% reuse. The new code is one struct, one resolver, one backend file, and ~5 fixtures. Everything load-bearing (combine, typed-Unknown, sysfs readers, line parsers) already exists and is fixture-proven — the risk is *divergence*, not *invention*.

## Common Pitfalls

### Pitfall 1: The hidden second scrape path (start-time `offload.go`)
**What goes wrong:** Planner reads CONTEXT.md/ARCHITECTURE.md (which name `running_offload.go`) and parameterizes only the running path. The start-time `Validate` path (validate.go:122 → offload.go `scrapeOffloadLog`) stays Vulkan-hardcoded.
**Why it happens:** The two scrape functions live in different files and the docs emphasize the running one.
**How to avoid:** Treat `offload.go:scrapeOffloadLog` AND `running_offload.go:scrapeLoadTensorsVulkan` as a single refactor unit, both keyed on `ResidencyMarkers`. Add a ROCm fixture for BOTH (`rocm_devinfo_pass.stderr` for start-time, `load_tensors_rocm.txt` for running).
**Warning signs:** ROCm fixtures only exist for the running path; `offload_test.go` has no ROCm case.

### Pitfall 2: `ROCm0` vs `ROCm_Host` — asserting the wrong buffer
**What goes wrong:** `load_tensors: ROCm_Host model buffer size = …` is the staging buffer, NOT proof of device residency. Asserting on `ROCm_Host` (or a substring match that catches both) false-greens a CPU-staged load.
**Why it happens:** Both lines contain "ROCm" and "model buffer size".
**How to avoid:** `DeviceToken = "ROCm0"` and match the exact token. The existing `scrapeLoadTensorsVulkan` uses `strings.Contains(line, vulkanDeviceToken)` — `"ROCm0"` will NOT match `"ROCm_Host"` (different next char), so a direct port is correct, but add a fixture line with `ROCm_Host` present alongside `ROCm0` to lock it. `[CITED: research/STACK.md:101]`

### Pitfall 3: Partial offload (`offloaded 1/65`) treated as PASS
**What goes wrong:** ROCm partial offload logs `offloaded N/M` with N<M and a small `ROCm0` buffer; if only the buffer>0 check runs, it passes. D-06 requires N==M.
**How to avoid:** The running path keys on the buffer line (N>0) and the start path keys on `parseOffloadedLayers`. For ROCm ensure the start-time path's N==M rule is exercised by a `rocm_offloaded_partial.stderr` fixture (offloaded 1/65 → FAIL). The existing offload.go logic already FAILs on `offloaded 0/N`; verify N<M (not just 0) also FAILs — **today's `scrapeOffloadLog` only FAILs on `offloaded==0`, not on `0<N<M`.** This is a real gap: for ROCm, partial must FAIL. Add the N<M→FAIL rule (gated so Vulkan's auto-fit no-offloaded-line PASS is unaffected). `[VERIFIED: offload.go:149-172]`

### Pitfall 4: Fault string scanned but server later answers (false PASS)
**What goes wrong:** `Memory access fault by GPU node` appears mid-load, the runtime spills to CPU, the server answers 200. Buffer line may still show a partial ROCm0 alloc.
**How to avoid:** D-06 — scan the journal/stderr for `m.FaultString` FIRST; any presence → FAIL regardless of buffer line. Fixture `load_tensors_rocm_fault.txt`. `[CITED: research/PITFALLS.md R1; ROCm #5824/#6146]`

### Pitfall 5: Breaking the negative seam gate by adding the rocm image token outside the seam
**What goes wrong:** The extended `TestSeamGrepGate` "container image literal" pattern (`kyuz0|docker\.io/|server-vulkan`) already catches the rocm image (it contains `kyuz0` and `docker.io/`). Adding a NEW pattern is unnecessary for the image; the existing one already binds it. But the planner must ensure the rocm digest/token appears ONLY in `backend_rocm.go`.
**How to avoid:** The existing regex `kyuz0|docker\.io/` already seam-binds the rocm image — **verify, don't duplicate.** D-10's "extend the negative gate to the rocm image token" is satisfied by confirming the existing pattern covers it (it does); optionally add `rocm-7.2.4|rocm7-nightlies` to make the intent explicit. The NEW work is the **positive** gate (assert `ROCm0` present in `backend_rocm.go`). `[VERIFIED: seam_test.go:39]`

## Code Examples

### Positive-presence grep-gate (D-10, ROADMAP SC#3)
```go
// Source: pattern mirrors seam_test.go:TestSeamGrepGate (inverse polarity)
func TestROCmMarkerPresence(t *testing.T) {
    data, err := os.ReadFile("backend_rocm.go")
    if err != nil {
        t.Fatalf("read backend_rocm.go: %v", err)
    }
    src := string(data)
    // The HIP residency markers a refactor must not silently drop.
    for _, marker := range []string{"ROCm0", "HSA_OVERRIDE_GFX_VERSION", "/dev/kfd"} {
        if !strings.Contains(src, marker) {
            t.Errorf("backend_rocm.go missing required ROCm marker %q — the HIP residency/device proof would silently break", marker)
        }
    }
}
```

### ROCm ContainerArgs delta (D-09) — sibling of backend_vulkan.go:81
```go
// Source: derived from backend_vulkan.go ContainerArgs + research/STACK.md:158, ARCHITECTURE.md:165
func (backendROCm) ContainerArgs(spec RunSpec) []string {
    hostPublish := fmt.Sprintf("%s:%d:%d", hostPublishAddr, serverPort, serverPort)
    modelBind := fmt.Sprintf("%s:%s:ro,z", spec.ModelsDir, containerModelsDir)
    containerModelPath := filepath.Join(containerModelsDir, spec.ModelFile)
    args := []string{
        "run", "--rm",
        "--name", spec.ContainerName,
        "--device", "/dev/kfd",          // NEW vs Vulkan (compute node)
        "--device", "/dev/dri",          // shared with Vulkan (render node)
        "--group-add", "render",         // render group for kfd (D-09)
        "--group-add", "keep-groups",
        "--security-opt", "seccomp=unconfined",
        "--env", "HSA_OVERRIDE_GFX_VERSION=11.5.1",  // ORDERED: override first (D-09)
        "--env", "ROCBLAS_USE_HIPBLASLT=1",          // then hipBLASLt
        "-p", hostPublish,
        "-v", modelBind,
        rocmImage,
        "llama-server",
        "-m", containerModelPath,
        "-c", fmt.Sprintf("%d", spec.ContextLen),
        "--host", "0.0.0.0",
        "--port", fmt.Sprintf("%d", serverPort),
    }
    args = append(args, llamaServerFlags...) // -ngl 999 -fa 1 --no-mmap … (shared)
    return args
}
```
> Note: env ordering and the exact device/group token set are FROZEN into a byte-golden in **Phase 7** (ROCM-03). Phase 6 only needs `ContainerArgs` correct behind the interface; assert the tokens with a `Contains`-style test (mirror `inference_test.go`), not a byte-golden.

### ROCm residency PASS fixture (`load_tensors_rocm.txt`)
```
# Source: line shapes from research/STACK.md:90-95, ROCm #5534, llama.cpp #19745/#15538
Jun 05 12:00:01 strix villa-llama[1234]: ggml_cuda_init: found 1 ROCm devices:
Jun 05 12:00:01 strix villa-llama[1234]:   Device 0: AMD Radeon Graphics, gfx1151 (0x1151), VMM: no
Jun 05 12:00:03 strix villa-llama[1234]: load_tensors: offloaded 65/65 layers to GPU
Jun 05 12:00:03 strix villa-llama[1234]: load_tensors:        CPU model buffer size =   315.32 MiB
Jun 05 12:00:03 strix villa-llama[1234]: load_tensors:  ROCm_Host model buffer size =   512.00 MiB
Jun 05 12:00:03 strix villa-llama[1234]: load_tensors:      ROCm0 model buffer size = 21504.49 MiB
Jun 05 12:00:05 strix villa-llama[1234]: main: server is listening on http://0.0.0.0:8080
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Hardcoded `vulkanDeviceToken = "Vulkan0"` in scrape | `Backend.ResidencyProof()` marker set | This phase | Backend-neutral scrape; ROCm/Metal slot in. |
| 7 literal `VulkanBackend()` call sites | `BackendFor(cfg.Backend)` | This phase | Config-driven backend selection (the spine). |
| `scrapeOffloadLog` FAILs only on `offloaded 0/N` | also FAIL on `0<N<M` (partial) | This phase | D-06 partial-offload-is-FAIL for ROCm. |

**Deprecated/outdated:**
- `rocm7-nightlies` image: 64 GB allocation cap; never use (REQUIREMENTS out-of-scope, CLAUDE.md). `rocm-7.2.4` stable only.

## Runtime State Inventory

Phase 6 is a pure-code, off-hardware refactor — it does not rename anything, register OS state, or migrate stored data. Per category:
- **Stored data:** None — no datastore keys/IDs change.
- **Live service config:** None — Phase 6 does not render or restart any unit (ROCm render is Phase 7; switch is Phase 8). The `villa-llama.container` on disk is untouched.
- **OS-registered state:** None.
- **Secrets/env vars:** The HSA/HIPBLASLt env values are container-internal literals in `backend_rocm.go`, not host env vars or secrets. None to migrate.
- **Build artifacts:** None — no package rename, no go.mod change.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `cmd/villa/inference.go`'s backend resolution reads `cfg.Backend` from a loaded config (the cfg source at line 113 needs confirmation in-plan; it currently constructs the backend before/around `detect.Probe`+`recommend.Pick`). | Pattern 2 site #7 | Re-route uses the wrong cfg source → backend not honored on `villa inference`. Low risk (one site, test-covered) but the planner must confirm the cfg variable. |
| A2 | ROCm start-time device-init log prefix is `ggml_cuda_init:` (HIP reuses the CUDA path). | Pattern 1 | If the kyuz0 build emits a different prefix, the start-time ROCm device line won't match → WARN not PASS. Mitigated: the running-path `ROCm0` buffer line is the primary proof and is HIGH-confidence; start-time is corroboration. Confirm exact prefix on hardware in Phase 8. `[ASSUMED]` from ollama #14855 analog. |
| A3 | The `render` group (not `video`) is the minimal group for `/dev/kfd` in the kyuz0 image; STACK.md says `GroupAdd=render`, PITFALLS.md mentions render+video. | Code Examples | If `video` is also required, ROCm offload fails on hardware. Phase 6 is off-hardware (no functional impact); the exact group set is frozen by Phase 7's golden and validated Phase 8. Plan should note both. `[ASSUMED]` |

## Open Questions

1. **Does the running-server ROCm proof need the fault scan to be journal-wide or load-window-bounded?**
   - What we know: D-06 says "absence of `Memory access fault by GPU node`"; status.go reads a bounded journal tail.
   - What's unclear: whether an OLD fault (pre-restart) in the tail would false-FAIL a now-healthy server.
   - Recommendation: scan only the current load window (since the last "server is listening" line) — but for Phase 6 (fixtures), a whole-fixture scan is fine; flag the windowing as a Phase 8 live-read concern.

2. **Should `gpu_busy_percent` be a FAIL-capable signal or WARN-only in Phase 6?**
   - What we know: D-06 lists non-zero busy during decode as a PASS component; idle servers read ~0; ROCm #6035 makes it N/A sometimes.
   - Recommendation: WARN-capable only in Phase 6 (it can't be a confident FAIL without a live decode in progress, which is Phase 8). Wire the input + the Known-zero-during-decode→FAIL rule, but the Phase-6 fixtures should only exercise PASS-corroborate / Unknown-WARN, deferring the live FAIL to Phase 8.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | building/testing the refactor | ✓ | go 1.26.2 (go.mod) | — |
| skopeo | resolving the ROCm image digest | ✓ | (resolved digest 2026-06-05) | podman manifest inspect (also present) |
| podman | (not needed this phase — no render/run) | ✓ | 5.8.2 | — |
| ROCm hardware (gfx1151) | NOT required Phase 6 (off-hardware, fixtures only) | ✗ | — | Synthetic fixtures (D-07); live validation deferred to Phase 8 |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** ROCm hardware — fully covered by fixtures per D-07; no blocker.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table tests + `testdata/` fixtures) |
| Config file | none (go test convention) |
| Quick run command | `go test ./internal/inference/...` |
| Full suite command | `go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ROCM-01 | `BackendFor("rocm")` returns a backend with kfd+dri, render group, ordered HSA/HIPBLASLT env, rocm image | unit | `go test ./internal/inference/ -run TestROCmContainerArgs` | ❌ Wave 0 (`backend_rocm_test.go`) |
| ROCM-01 | Image is digest-pinned (`@sha256:`+64hex) | unit | `go test ./internal/inference/ -run TestROCmImageDigestPinned` | ❌ Wave 0 |
| ROCM-01/04 | `BackendFor("vulkan")` and `""` return unchanged Vulkan backend; unknown → actionable error | unit | `go test ./internal/inference/ -run TestBackendFor` | ❌ Wave 0 |
| ROCM-02 | Running ROCm residency: ROCm0 N/N → PASS, CPU-only → FAIL, fault → FAIL, empty → WARN | unit | `go test ./internal/inference/ -run TestRunningServerOffloadVerdict` (extend) | ⚠️ exists, add ROCm cases + fixtures |
| ROCM-02 | Start-time ROCm offload: device+offloaded N/N → PASS, partial N<M → FAIL, CPU → FAIL | unit | `go test ./internal/inference/ -run TestScrapeOffloadLog` (extend) | ⚠️ exists (`offload_test.go`), add ROCm cases |
| ROCM-02 | Vulkan offload byte-identical after refactor (regression) | unit | `go test ./internal/inference/` (existing Vulkan cases unchanged) | ✅ exists — must stay green |
| ROCM-02 | grep-gate: `ROCm0`/HSA/kfd present in backend_rocm.go | unit | `go test ./internal/inference/ -run TestROCmMarkerPresence` | ❌ Wave 0 |
| ROCM-02 | negative seam gate covers rocm image token | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ exists — verify pattern covers rocm |
| ROCM-04 | 7 call sites compile + v1.0 suite green under Vulkan default | integration | `go test ./...` | ✅ exists — no-op proof |

### Sampling Rate
- **Per task commit:** `go test ./internal/inference/...` (fast, the blast-radius package)
- **Per wave merge:** `go test ./...` (proves the 7-site re-route is a no-op)
- **Phase gate:** `go vet ./... && go test ./...` green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/inference/backend_rocm.go` — the backend impl (covers ROCM-01)
- [ ] `internal/inference/backend_rocm_test.go` — ContainerArgs + digest-pin tests
- [ ] `internal/inference/testdata/load_tensors_rocm.txt` — ROCm0 N/N PASS (running path)
- [ ] `internal/inference/testdata/load_tensors_rocm_cpu.txt` — CPU-only FAIL
- [ ] `internal/inference/testdata/load_tensors_rocm_fault.txt` — fault FAIL
- [ ] `internal/inference/testdata/rocm_devinfo_pass.stderr` — start-time PASS
- [ ] `internal/inference/testdata/rocm_offloaded_partial.stderr` — N<M FAIL
- [ ] Extend `offload_test.go` / `running_offload_test.go` with ROCm marker-driven cases (Vulkan cases unchanged)
- [ ] `TestROCmMarkerPresence` positive grep-gate (new test in seam_test.go or a new file)

## Security Domain

> `security_enforcement` is enabled (default). ROCm `/dev/kfd` passthrough is a real attack-surface delta — but the LIVE exposure lands in Phase 7 (render) / Phase 8 (run). Phase 6 only encodes the device tokens in `ContainerArgs` (no unit is rendered or run). Still, flag for the planner so the Phase-7/8 threat model is pre-loaded.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Seam discipline keeps backend privilege literals (`/dev/kfd`) in one auditable file (`backend_rocm.go`); grep-gate enforces it. |
| V5 Input Validation | yes | `BackendFor` fail-closed on unknown input (D-02); model path is catalog-resolved, never shell-interpolated (existing pattern). |
| V10 Malicious Code / Supply Chain | yes | ROCm image digest-pinned (`@sha256:`), same provenance as the audited Vulkan image; format-guard test prevents an un-pinned hand-edit. |
| V12 Files/Resources | yes | `/dev/kfd` + `/dev/dri` device exposure — broader than Vulkan's `/dev/dri`-only. Documented as the ROCm-only delta; off when on Vulkan. |

### Known Threat Patterns for {Go control plane + rootless Podman ROCm}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| `/dev/kfd` compute-device exposure broader than Vulkan baseline | Elevation of Privilege / Information Disclosure | Add kfd ONLY on the ROCm backend (D-09); it's absent on Vulkan; document the delta; keep render-group scope minimal. Live exposure is Phase 7/8 — Phase 6 just encodes the token. `[CITED: research/PITFALLS.md R7]` |
| Supply-chain drift via floating `:rocm-7.2.4` tag (could reintroduce nightly cap / telemetry) | Tampering | Pin `@sha256:2da150c1f025…`; never the bare tag. `[CITED: research/PITFALLS.md Security table]` |
| SELinux label-disable as a default kfd workaround | Elevation of Privilege | NOT a Phase-6 concern (no run); for Phase 7/8 prefer narrowest policy, label-disable only as reviewed fallback. Flag forward. |
| Unknown backend string silently defaulting to a privileged path | Spoofing/Tampering | `BackendFor` fail-closed (D-02) — unknown → actionable error, never a silent fallback. |

## Sources

### Primary (HIGH confidence)
- Live tree: `internal/inference/{inference,backend_vulkan,offload,running_offload,validate,seam_test}.go`, `internal/detect/gpu_amd.go`, `internal/config/villaconfig.go`, all 7 call sites + 6 test helpers — read directly, grep-verified.
- `skopeo inspect docker://docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4` → digest `sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89` (resolved 2026-06-05, this dev box).
- `.planning/research/STACK.md` §HIP Offload-Residency Proof (lines 84-108) — ROCm0/ROCm_Host/CPU buffer line shapes, dual-signal rule, device-naming contrast.
- `.planning/research/ARCHITECTURE.md` §Pattern 1 (lines 138-157) — `ResidencyMarkers` design, scrape rename, "callers never name a backend."
- CLAUDE.md §Technology Stack / §What NOT to Use — image, HSA override, HIPBLASLt, flags, nightlies cap.

### Secondary (MEDIUM-HIGH confidence)
- `.planning/research/PITFALLS.md` R1 (HIP residency proof), R7 (kfd passthrough/SELinux), Security table — verified against ROCm/llama.cpp/ollama issues dated late-2025/early-2026.
- `github.com/ROCm/ROCm` #5534 — `HSA_OVERRIDE_GFX_VERSION=11.5.1`, `ROCBLAS_USE_HIPBLASLT=1`, `ROCm0 model buffer size` / `offloaded N/N` log shape.
- `github.com/ggml-org/llama.cpp` #19745, #15538 — `ROCm0`/`ROCm_Host`/`CPU model buffer size`, offloaded line, `--no-mmap` rationale.
- `github.com/ollama/ollama` #14855 — `library=ROCm compute=gfx1151 name=ROCm0` device signature.

### Tertiary (LOW confidence — flagged in Assumptions Log)
- `ggml_cuda_init:` as the exact ROCm start-time device-init prefix (A2) — analog from ollama, confirm on hardware Phase 8.
- `render`-only vs `render+video` group for kfd (A3) — STACK.md says render; PITFALLS.md mentions both.

## Metadata

**Confidence breakdown:**
- Seam shape / call sites / re-route: HIGH — read every file and test helper directly.
- ROCm residency log format: HIGH — corroborated across STACK.md, ROCm #5534, llama.cpp #19745, ollama #14855.
- Image digest/pin: HIGH — resolved live this session.
- Start-time device-init prefix + kfd group set: MEDIUM — off-hardware analogs, flagged in Assumptions Log, confirmed Phase 8.

**Research date:** 2026-06-05
**Valid until:** 2026-07-05 for the seam/refactor design (stable internal code); re-verify the ROCm image digest at Phase 7 byte-golden freeze (the `:rocm-7.2.4` tag auto-rebuilds — STACK.md).
