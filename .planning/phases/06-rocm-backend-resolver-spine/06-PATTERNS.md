# Phase 6: ROCm Backend + Resolver Spine - Pattern Map

**Mapped:** 2026-06-05
**Files analyzed:** 14 (4 new Go, 5 new testdata, 5 modified)
**Analogs found:** 14 / 14 (every file has a direct in-tree analog — this is the SPINE phase)

> All backend literals (image, devices, env, flags, markers) live ONLY in
> `internal/inference/` (+ `internal/detect/gpu_amd.go`). Every NEW/MODIFIED Go file
> stays inside that seam; the 7 call sites depend on the `Backend` interface only.
> The entire phase is pure-Go, off-hardware, fixture-tested (no new module deps).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/inference/backend_rocm.go` (NEW) | backend impl | transform (spec→args) | `internal/inference/backend_vulkan.go` | exact (sibling) |
| `internal/inference/inference.go` (MOD) | interface contract | n/a | self (extend `Backend`) | exact |
| `internal/inference/offload.go` (MOD) | service (pure parser) | transform (stderr→verdict) | self (`scrapeOffloadLog`) | exact |
| `internal/inference/running_offload.go` (MOD) | service (pure parser) | transform (journal→verdict) | self (`scrapeLoadTensorsVulkan`) | exact |
| `BackendFor()` resolver (NEW; in `inference.go` or `backend.go`) | resolver/factory | request-response (string→Backend) | `VulkanBackend()` ctor + `BackendFor` Pattern-2 in RESEARCH | role-match |
| `internal/inference/backend_rocm_test.go` (NEW) | test | n/a | `internal/inference/inference_test.go` | exact |
| `internal/inference/offload_test.go` (MOD) | test | n/a | self + `running_offload_test.go` | exact |
| `internal/inference/running_offload_test.go` (MOD) | test | n/a | self | exact |
| `internal/inference/seam_test.go` (MOD) + `TestROCmMarkerPresence` (NEW) | test (grep-gate) | n/a | `seam_test.go:TestSeamGrepGate` | exact (inverse polarity) |
| `testdata/load_tensors_rocm.txt` (NEW) | fixture | n/a | `testdata/load_tensors_vulkan.txt` | exact |
| `testdata/load_tensors_rocm_cpu.txt` (NEW) | fixture | n/a | `testdata/load_tensors_cpu.txt` | exact |
| `testdata/load_tensors_rocm_fault.txt` (NEW) | fixture | n/a | `testdata/load_tensors_cpu.txt` (no Vulkan analog) | role-match |
| `testdata/rocm_devinfo_pass.stderr` (NEW) | fixture | n/a | `testdata/radv_devinfo_pass.stderr` | exact |
| `testdata/rocm_offloaded_partial.stderr` (NEW) | fixture | n/a | `testdata/offloaded_zero.stderr` | role-match |
| **7 call sites** (cmd/villa + internal/status) (MOD) | callers | request-response | each currently calls `inference.VulkanBackend()` directly | exact |

---

## Pattern Assignments

### `internal/inference/backend_rocm.go` (NEW — backend impl, transform)

**Analog:** `internal/inference/backend_vulkan.go` (read in full). Copy structure; swap image + device/env deltas. Keep everything else verbatim.

**Digest-pin constant + provenance comment** — mirror `backend_vulkan.go:15-19` style (record `skopeo`/`podman pull` date + RESEARCH legitimacy note). The resolved digest is in RESEARCH (`sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89`). Vulkan exemplar:
```go
// vulkanImage is the digest-pinned kyuz0 Strix-Halo Vulkan RADV image (CLAUDE.md
// prescribed; RESEARCH §Package Legitimacy Audit: approved, pin digest ...).
// Resolved on the dev box 2026-06-04 via `podman pull ...`.
const vulkanImage = "docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555c45864352a4077528836988d448e9f030fbab9f7376ea1c603ac7aad"
```
→ ROCm: `const rocmImage = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89"` (NEVER `rocm7-nightlies`).

**Stateless struct + compile-time assertion + constructor** (`backend_vulkan.go:55-69`):
```go
type backendVulkan struct{}
var _ Backend = backendVulkan{}
func VulkanBackend() Backend { return backendVulkan{} }
func (backendVulkan) Name() string  { return "vulkan" }
func (backendVulkan) Image() string { return vulkanImage }
```
→ ROCm: `type backendROCm struct{}`, `var _ Backend = backendROCm{}`, `func (backendROCm) Name() string { return "rocm" }`, `Image()` returns `rocmImage`. (No exported `ROCmBackend()` ctor is required — `BackendFor` is the entry point; add one only if a test helper needs it.)

**`ContainerArgs` assembly** — the SINGLE assembly point. Vulkan source (`backend_vulkan.go:81-103`):
```go
func (backendVulkan) ContainerArgs(spec RunSpec) []string {
	hostPublish := fmt.Sprintf("%s:%d:%d", hostPublishAddr, serverPort, serverPort)
	modelBind := fmt.Sprintf("%s:%s:ro,z", spec.ModelsDir, containerModelsDir)
	containerModelPath := filepath.Join(containerModelsDir, spec.ModelFile)
	args := []string{
		"run", "--rm",
		"--name", spec.ContainerName,
		"--device", "/dev/dri",
		"--group-add", "keep-groups",
		"--security-opt", "seccomp=unconfined",
		"-p", hostPublish,
		"-v", modelBind,
		vulkanImage,
		"llama-server",
		"-m", containerModelPath,
		"-c", fmt.Sprintf("%d", spec.ContextLen),
		"--host", "0.0.0.0",
		"--port", fmt.Sprintf("%d", serverPort),
	}
	args = append(args, llamaServerFlags...)
	return args
}
```
**ROCm delta (D-09):** add `"--device", "/dev/kfd"` AND keep `"--device", "/dev/dri"`; add `"--group-add", "render"` (in addition to `keep-groups`); add ORDERED env `"--env", "HSA_OVERRIDE_GFX_VERSION=11.5.1"` then `"--env", "ROCBLAS_USE_HIPBLASLT=1"`; swap `vulkanImage`→`rocmImage`. Reuse the shared `hostPublishAddr`/`serverPort`/`containerModelsDir`/`llamaServerFlags` consts and `endpointURL()` verbatim (they are backend-neutral). Full delta example: RESEARCH §"ROCm ContainerArgs delta (D-09)".

> **Off-hardware note (A3):** STACK.md says `render` group; PITFALLS.md mentions `render`+`video`. The byte-golden freeze is Phase 7 — Phase 6 only needs `ContainerArgs` correct behind the interface (assert tokens with `Contains`, not a golden). Note both groups in the plan.

**`ResidencyProof()` (D-04) — ROCm markers** (see Shared Patterns → Residency markers for the struct + both impls).

---

### `internal/inference/inference.go` (MOD — interface contract)

**Self-extend.** Add ONE method to `Backend` (`inference.go:54-65`) and define the `ResidencyMarkers` struct. Current interface:
```go
type Backend interface {
	Name() string
	Image() string
	ContainerArgs(spec RunSpec) []string
}
```
→ Add `ResidencyProof() ResidencyMarkers`. The exact struct shape is Claude's Discretion (D-04); RESEARCH §Pattern 1 proposes the load-bearing fields (`DeviceToken`, `DeviceLabel`, `StartLogDevicePrefix`, `FaultString`, `RejectSoftwareRenderer`) — it MUST cover BOTH scrape paths. Document each field with the existing doc-comment density (see the `Backend`/`RunSpec` comments). Do NOT touch `Status`/`Verdict`/`pass`/`warn`/`fail` (`inference.go:82-155`) — frozen contract.

---

### `internal/inference/running_offload.go` (MOD — pure parser, transform)

**Self-refactor (D-05) — byte-identical Vulkan behavior required.** Parameterize `scrapeLoadTensorsVulkan` by `ResidencyMarkers`; rename to `scrapeLoadTensorsResidency(journal string, m ResidencyMarkers)`.

Current hardcoded token (`running_offload.go:69-78`):
```go
const (
	loadTensorsPrefix = "load_tensors:"
	vulkanDeviceToken = "Vulkan0"   // ← becomes m.DeviceToken
	bufferSizePhrase  = "model buffer size"
)
```
`loadTensorsPrefix` + `bufferSizePhrase` are backend-NEUTRAL — keep them as consts. Only `vulkanDeviceToken` becomes `m.DeviceToken`. Current device match (`running_offload.go:114`): `if strings.Contains(line, vulkanDeviceToken)` → `if strings.Contains(line, m.DeviceToken)`. (Pitfall 2: `"ROCm0"` will NOT match `"ROCm_Host"` — direct port is correct; add a fixture line with both to lock it.)

**Add the fault scan (D-06)** BEFORE the buffer-line switch (`running_offload.go:123`): `if m.FaultString != "" && strings.Contains(journal, m.FaultString) { return ...FAIL... }`. Empty `FaultString` (Vulkan) is a no-op → Vulkan stays byte-identical.

**Thread the markers in** (`running_offload.go:40-59`): add `Markers ResidencyMarkers` to `RunningOffloadInput`. `RunningOffloadVerdict` (`running_offload.go:217-218`) passes `in.Markers`. **Do NOT touch** `combineOffload`, `gttFloor`, `parseBufferMiB`, `propsDrift`, the typed-Unknown discipline, or the `RunningOffloadVerdict` provenance/combine flow — all reused verbatim (D-05).

---

### `internal/inference/offload.go` (MOD — pure parser, transform) ⚠️ THE #1 TRAP

**RESEARCH's single most-emphasized fact: there are TWO scrape paths, not one.** The start-time `scrapeOffloadLog` (called from `validate.go:122`, the `install`/`villa inference` path) is ALSO Vulkan-hardcoded and MUST be threaded, or ROCm residency silently never matches on the start-time path.

Rename `scrapeOffloadLog(stderr)` → `scrapeOffloadLog(stderr string, m ResidencyMarkers)`. Replace the three hardcoded literals:
- `offload.go:109` — `strings.HasPrefix(line, "ggml_vulkan:")` → `m.StartLogDevicePrefix` (gate on non-empty).
- `offload.go:123` — `strings.Index(line, "- Vulkan")` → `m.DeviceLabel`.
- Software-renderer reject (`offload.go:98,138-146`) — gate the `noteDevice`/`softwareDevice` reject on `m.RejectSoftwareRenderer` (true for Vulkan, false for ROCm — ROCm has no llvmpipe analog).

**Partial-offload gap (Pitfall 3, D-06).** Today `offload.go:148-155` FAILs only on `offloaded == 0`. For ROCm, `0 < N < M` (partial) MUST also FAIL. Add the `N<M → FAIL` rule, GATED so Vulkan's auto-fit no-offloaded-line PASS is unaffected. `parseOffloadedLayers` (`offload.go:184-205`) already returns `(offloaded, total)` — the data is present.

**Thread the markers in:** `validate.go:122` calls `scrapeOffloadLog(stderr)`. `ValidateInput` (`validate.go:34-58`) currently carries a `Runner` (not a `Backend`) and builds the spec via `spec(in)` (`validate.go:145`). **Plan decision needed:** add a `Backend`/`Markers` field to `ValidateInput` (or derive markers from the Runner's backend) so `scrapeOffloadLog` gets `in.<...>.ResidencyProof()`. Confirm the cfg/backend source in-plan (see Assumption A1).

---

### `BackendFor(name string) (Backend, error)` (NEW — resolver/factory, request-response)

**Analog:** the `VulkanBackend()` constructor (`backend_vulkan.go:61-63`) for the construction shape; RESEARCH §Pattern 2 for the resolver body. Lives in `internal/inference` (D-01) — `inference.go` or a new `backend.go`. Fail-closed (D-02):
```go
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
Consumes `VillaConfig.Backend` (`internal/config/villaconfig.go`, default `"vulkan"`). Exact error wording is Claude's Discretion (D-02) — must be actionable.

> **Keep `VulkanBackend()` exported.** Six test helpers inject it directly (`cmd/villa/status_test.go:35`, `internal/status/status_test.go:30`, `internal/dashboard/api_test.go`, `inference_test.go:12-13,22,67,76`, `internal/orchestrate/render_test.go`). Removing it breaks the no-op guarantee. Only the 7 NON-test sites re-route.

---

### 7 Call Sites (MOD — callers, request-response)

**Analog:** each currently calls `inference.VulkanBackend()` directly; re-route to `BackendFor(cfg.Backend)` and surface the error through the EXISTING error path. Verified line numbers (live tree):

| # | File:line | Current | Error path |
|---|-----------|---------|------------|
| 1 | `cmd/villa/install.go:234` | `RenderInput{Backend: inference.VulkanBackend(), …}` | existing render-fail handling (`return exitBlocked`) |
| 2 | `cmd/villa/install.go:632` | `NewContainerRunner(VulkanBackend(), …).Endpoint()` | resolve `b` once in deps-wiring helper, return err up |
| 3 | `cmd/villa/lifecycle.go:66` | `RenderInput{Backend: VulkanBackend(), …}` | `return nil, "", fmt.Errorf("resolve backend: %w", err)` |
| 4 | `cmd/villa/status.go:135` | `NewContainerRunner(VulkanBackend(), …).Endpoint()` | resolve in `liveStatusDeps` |
| 5 | `cmd/villa/dashboard.go:130` | `NewContainerRunner(VulkanBackend(), …).Endpoint()` | reuse `liveStatusDeps`'s single resolution |
| 6 | `cmd/villa/model.go:361` | `RenderInput{Backend: VulkanBackend(), …}` (closure) | `return false, err` (closure returns `(bool, error)`) |
| 7 | `cmd/villa/inference.go:113` | `backend := inference.VulkanBackend()` | existing `inference.Verdict{Status: FAIL, …}` refusal (cf. line 127) — confirm cfg source (A1) |
| — | `internal/status/status.go:180` | `RenderInput{Backend: VulkanBackend(), …}` | `return Report{Overall: StatusFail.String(), …, err: err}` |

> Note: install.go has 2 sites; the "7 `VulkanBackend()` call sites" = 6 cmd/villa files (install counted once in CONTEXT, 2 physical) + `internal/status/status.go`. Re-route ALL 8 physical occurrences; the v1.0 suite must stay green under the Vulkan default (SC#4 no-op proof: `go test ./...`).

---

### Test files (NEW + MOD)

**`backend_rocm_test.go` (NEW)** — analog `inference_test.go` (read in full). Mirror:
- `TestContainerArgsCarryMandatoryFlags` (`inference_test.go:66-79`) → `TestROCmContainerArgs`: assert `joined` contains `--device /dev/kfd`, `--device /dev/dri`, `--group-add render`, `HSA_OVERRIDE_GFX_VERSION=11.5.1`, `ROCBLAS_USE_HIPBLASLT=1`, `-ngl 999`, `-fa 1`, `--no-mmap`. Use the `strings.Join(args, " ")` + `Contains` pattern (NOT a byte-golden — that's Phase 7).
- `inference_test.go:76-77` digest-pin guard → `TestROCmImageDigestPinned`: assert `Image()` contains `@sha256:` (+ 64-hex). Passes now (real digest).
- `TestBackendFor`: `""`/`"vulkan"` → vulkan, `"rocm"` → rocm, unknown → non-nil actionable error.

**`offload_test.go` / `running_offload_test.go` (MOD)** — analog `running_offload_test.go:27-60` table-test + `readFixture(t, "...")` + `t.TempDir()`/`detect.GTTUsedBytesForTest` fixture pattern. Add ROCm marker-driven cases (driven by `backendROCm{}.ResidencyProof()`); leave EVERY existing Vulkan case unchanged (regression proof). Fixture helper `readFixture` is shared across the two test files.

**`seam_test.go` (MOD) + `TestROCmMarkerPresence` (NEW)** — analog `TestSeamGrepGate` (`seam_test.go:34-84`), inverse polarity. The existing negative gate's `container image literal` regex `kyuz0|docker\.io/|server-vulkan` (`seam_test.go:39`) ALREADY seam-binds the rocm image (it contains `kyuz0` + `docker.io/`) — VERIFY, don't duplicate (Pitfall 5). Optionally add `rocm-7.2.4|rocm7-nightlies` for explicit intent. The NEW positive gate:
```go
func TestROCmMarkerPresence(t *testing.T) {
	data, err := os.ReadFile("backend_rocm.go")
	if err != nil { t.Fatalf("read backend_rocm.go: %v", err) }
	src := string(data)
	for _, marker := range []string{"ROCm0", "HSA_OVERRIDE_GFX_VERSION", "/dev/kfd"} {
		if !strings.Contains(src, marker) {
			t.Errorf("backend_rocm.go missing required ROCm marker %q", marker)
		}
	}
}
```
> Gate on `ROCm0` — NOT `ggml_cuda` (shared with the CUDA path, ambiguous; Pitfall, Anti-Pattern).

---

### testdata fixtures (NEW)

Analog the existing `testdata/` files (read shapes from `load_tensors_vulkan.txt`, `load_tensors_cpu.txt`, `radv_devinfo_pass.stderr`, `offloaded_zero.stderr`). Exact ROCm line shapes in RESEARCH §"ROCm residency PASS fixture" and §Specific Ideas:

| Fixture | Asserts | Key line |
|---------|---------|----------|
| `load_tensors_rocm.txt` | running PASS (N==M) | `load_tensors: ROCm0 model buffer size = N MiB` (N>0) + `offloaded 65/65` + include a `ROCm_Host` line to lock Pitfall 2 |
| `load_tensors_rocm_cpu.txt` | running FAIL (CPU fallback) | only `CPU model buffer size`, no `ROCm0` |
| `load_tensors_rocm_fault.txt` | running FAIL (fault dominates) | `Memory access fault by GPU node` present even alongside a partial ROCm0 line (Pitfall 4) |
| `rocm_devinfo_pass.stderr` | start-time PASS | `ggml_cuda_init: found 1 ROCm devices` + `- ROCm0 :` device line (A2: prefix is MEDIUM-confidence) |
| `rocm_offloaded_partial.stderr` | start-time FAIL (N<M) | `offloaded 1/65 layers to GPU` (Pitfall 3) |

---

## Shared Patterns

### Residency markers (D-04) — the seam-bound descriptor
**Source:** RESEARCH §Pattern 1; new `ResidencyMarkers` in `inference.go`; impls in `backend_vulkan.go` + `backend_rocm.go`.
**Apply to:** `backend_rocm.go`, `backend_vulkan.go` (`ResidencyProof()`), both scrape paths.
Each backend owns its literals; the neutral scrapers key on the struct. Vulkan MUST reproduce today's exact behavior (`DeviceToken:"Vulkan0"`, `DeviceLabel:"- Vulkan"`, `StartLogDevicePrefix:"ggml_vulkan:"`, `FaultString:""`, `RejectSoftwareRenderer:true`). ROCm: `"ROCm0"`, `"- ROCm"`, `"ggml_cuda_init:"`, `"Memory access fault by GPU node"`, `false`.

### Typed-Unknown / no-false-green (D-09)
**Source:** `internal/detect` (`Bool`/`Bytes`/`Int`), used throughout `offload.go`/`running_offload.go`.
**Apply to:** every new ROCm signal. An unevaluable signal → `detect.UnknownBool(...)` → WARN; a confidently-false offload → `detect.KnownBool(false, ...)` → FAIL. Never an ad-hoc bool/error pair.

### Combine discipline — DO NOT re-roll
**Source:** `combineOffload` (`offload.go:278-301`).
**Apply to:** the ROCm running + start-time verdicts. Any FAIL→FAIL; else any Unknown→WARN; else PASS. Reuse verbatim (D-05, Don't Hand-Roll).

### Pure-library + cmd-layer I/O split
**Source:** package doc (`inference.go:1-24`) — "NEVER calls os.Exit and NEVER prints."
**Apply to:** all `internal/inference` changes. Journald/HTTP/sysfs I/O stays in `cmd/villa` + `internal/status`; ROCm logic accepts text/bytes, returns `Verdict`.

### sysfs busy/GTT readers — reuse, don't rebuild
**Source:** `detect.GPUBusyPercent()`/`GPUBusyPercentForTest(drmRoot)` (`internal/detect/gpu_amd.go`), `detect.GTTUsedBytes[ForTest]`.
**Apply to:** the ROCm residency input. Wire `GPUBusyPercent` as an INPUT now (WARN-capable corroboration only — Open Q2), live decode-time read is Phase 8. Fixture via a temp drmRoot with a `gpu_busy_percent` file (mirror `running_offload_test.go:33-37` `mem_info_gtt_used` pattern).

### Digest-pin provenance comment
**Source:** `backend_vulkan.go:15-19`.
**Apply to:** `backend_rocm.go` const — record the resolve date (2026-06-05) + RESEARCH legitimacy note. Re-verify the digest at the Phase 7 byte-golden freeze (the `:rocm-7.2.4` tag auto-rebuilds).

---

## No Analog Found

None. Every file has a direct in-tree analog. The two role-match (not exact) cases:

| File | Role | Reason |
|------|------|--------|
| `testdata/load_tensors_rocm_fault.txt` | fixture | No Vulkan fault-string fixture exists (Vulkan uses software-renderer reject, not a fault string); shape from RESEARCH/STACK.md. |
| `testdata/rocm_offloaded_partial.stderr` | fixture | `offloaded_zero.stderr` covers N==0; the new `0<N<M` partial case is a new rule (Pitfall 3) with no exact analog. |

---

## Open Plan Decisions (flagged, not blocking)

- **A1:** `cmd/villa/inference.go:113` cfg/backend source — confirm the cfg variable in-plan (it builds the backend around `detect.Probe`+`recommend.Pick`). Low risk, test-covered.
- **`ValidateInput` threading:** carries a `Runner`, not a `Backend`. Add a `Backend`/`Markers` field (or derive from the Runner's backend) so the start-time `scrapeOffloadLog` gets `ResidencyProof()`. Executor's call.
- **A2:** `ggml_cuda_init:` start-time prefix is MEDIUM-confidence (off-hardware analog) — the running-path `ROCm0` buffer line is the HIGH-confidence primary proof; start-time is corroboration. Confirm on hardware Phase 8.
- **A3:** `render` vs `render`+`video` group for `/dev/kfd` — note both; frozen Phase 7, validated Phase 8.

## Metadata

**Analog search scope:** `internal/inference/` (all .go + testdata), `internal/detect/gpu_amd.go`, `internal/config/villaconfig.go`, `internal/status/status.go`, `cmd/villa/{install,lifecycle,status,dashboard,model,inference}.go`.
**Files read in full:** `backend_vulkan.go`, `inference.go`, `offload.go`, `running_offload.go`, `seam_test.go`, `inference_test.go`, `running_offload_test.go` (head), `validate.go` (ValidateInput).
**Verification:** all 8 `VulkanBackend()` occurrences + line numbers confirmed against the live tree via grep.
**Pattern extraction date:** 2026-06-05
