# Phase 7: ROCm Render Unit + Preflight/Detect - Pattern Map

**Mapped:** 2026-06-06
**Files analyzed:** 11 (4 CREATE, 7 MODIFY)
**Analogs found:** 11 / 11 (every file has an exact in-repo analog — this is a "copy the sibling" phase)

> Seam discipline (load-bearing): backend literals (`/dev/kfd`, `render`, `HSA_OVERRIDE…`, image strings) live ONLY in `internal/inference/*` and `internal/detect/gpu_amd.go`. `TestSeamGrepGate` fails if any appear elsewhere. Every file below READS those values through `Backend.Image()` / `Backend.ContainerArgs(spec)` or the `HostProfile`; none re-types them. The `render.go` flag tokens are assembled from fragments (`dash + "device"`) specifically to dodge the gate — keep that idiom.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/orchestrate/render.go` | service (pure renderer) | transform | self (`parseContainerArgs`) + `openwebui.go` `envPair`/`Env` | exact (extend in place) |
| `internal/orchestrate/quadlet/container.tmpl` | config (template) | transform | `quadlet/openwebui.container.tmpl` (`{{range .Env}}`) | exact |
| `internal/orchestrate/testdata/villa-llama-rocm.container.golden` | test (fixture) | transform | `testdata/villa-llama.container.golden` | exact |
| `internal/orchestrate/render_test.go` | test | transform | `TestRenderContainerGolden` + `TestRenderOpenWebUITelemetryFrozen` | exact |
| `internal/preflight/rocm-policy.json` | config (embedded data) | file-I/O (`go:embed`) | `render.go` `//go:embed quadlet/*.tmpl` + `floors.go` `Floor` struct | role-match |
| `internal/preflight/floors.go` | service (data source) | transform | self (`Floor` / `Floors()`) | exact (refactor in place) |
| `internal/preflight/checks_rocm.go` (CREATE) | service (verdict) | transform | `checks_gpu.go` (`checkVulkanIGPU`/`checkKernelFloor`/`checkFirmwareFloor`) + `preflight.go` (`Run`/`pass`/`warn`/`fail`) | exact |
| `cmd/villa/preflight.go` | controller (CLI) | request-response | self (`newPreflight`/`renderPreflight`) | exact (add `--backend` flag) |
| `internal/detect/profile.go` | model | CRUD (struct contract) | self (`HostProfile` + typed-Optionals) | exact (append field + bump) |
| `internal/detect/` rocm readiness (CREATE/MODIFY) | service (compute) | transform | `gpu_amd.go` (`parseGfxID`/`rocmPresent`) + `value.go` (`KnownBool`/`UnknownBool`) | exact |
| `cmd/villa/testdata/detect.golden.json` | test (fixture) | CRUD | self + `cmd/villa/detect_test.go` `fixtureProfile()` | exact (re-freeze) |

---

## Pattern Assignments

### `internal/orchestrate/render.go` (renderer, transform) — D-09 + the two undocumented deltas

**Analog:** self + `internal/orchestrate/openwebui.go`

**CRITICAL (RESEARCH Pitfall 1):** D-09's literal wording ("`AddDevice` becomes `[]string`") is INCOMPLETE. `backend_rocm.go.ContainerArgs` emits THREE render-affecting deltas the parser must handle: multiple `--device`, a SECOND `--group-add` (`render`), and TWO `--env` flags that have **no case today and are silently dropped**. Implement all three or the ROCm unit is non-functional (CPU fallback) while a naive golden still passes.

**1. `containerView` scalar → slice fields** (render.go:37-47, current):
```go
type containerView struct {
	ContainerName string
	Image         string
	Network       string
	AddDevice     string   // ← becomes []string
	GroupAdd      string   // ← becomes []string (currently last-wins, drops a group)
	PublishPort   string
	Volume        string
	PodmanArgs    string
	Exec          string
	// ← add: Env []envPair   (envPair already defined in openwebui.go:46-49)
	// ← consider: BackendLabel string  (Description= delta — see Open Question A1 below)
}
```

**2. The `envPair` type to reuse — DO NOT redefine** (openwebui.go:46-49, same package):
```go
type envPair struct {
	Key   string
	Value string
}
```
It is an ordered slice deliberately (map order is non-deterministic and breaks byte-goldens). The new `containerView.Env` is the SAME `[]envPair`.

**3. Flag-walk to extend** (render.go:149-190, current — note the grep-gate-dodging fragment assembly):
```go
const dash = "--"
var (
	flDevice   = dash + "device"
	flGroupAdd = dash + "group" + "-add"
	flSecOpt   = dash + "security-opt"
	flName     = dash + "name"
	// ← ADD: flEnv = dash + "env"
)
for i := 0; i < len(flags); i++ {
	switch flags[i] {
	case flDevice:
		if i+1 < len(flags) { cv.AddDevice = flags[i+1]; i++ }   // ← append: cv.AddDevice = append(cv.AddDevice, flags[i+1])
	case flGroupAdd:
		if i+1 < len(flags) { cv.GroupAdd = flags[i+1]; i++ }     // ← append (was last-wins!)
	// ← ADD case flEnv: split flags[i+1] on FIRST "=" into envPair{Key,Value}, append to cv.Env
	case flSecOpt:
		if i+1 < len(flags) { cv.PodmanArgs = flSecOpt + " " + flags[i+1]; i++ }
	case "-p", "--publish": /* … */
	case "-v", "--volume":  /* … */
	case flName: i++
	}
}
```
Split `--env` value on the FIRST `=` (the value `HSA_OVERRIDE_GFX_VERSION=11.5.1` itself contains no second `=`, but `ROCBLAS_USE_HIPBLASLT=1` etc. — use `strings.Cut(v, "=")`).

**4. Defensive all-fields check to update** (render.go:195-198, current):
```go
if cv.AddDevice == "" || cv.GroupAdd == "" || cv.PublishPort == "" ||
	cv.Volume == "" || cv.PodmanArgs == "" || cv.Exec == "" {
	return containerView{}, fmt.Errorf("orchestrate: ContainerArgs missing a required mapped field: %+v", cv)
}
```
→ change device/group to slice checks: `len(cv.AddDevice) > 0 && len(cv.GroupAdd) > 0`. Keep scalar checks for the others. **Do NOT** require `len(cv.Env) > 0` — Vulkan legitimately has zero env (RESEARCH Pitfall 1).

**Seam constraint:** the `--device`/`--group-add`/`--env` VALUES (`/dev/kfd`, `render`, `HSA_OVERRIDE…`) come from `ContainerArgs` — never type them in render.go. Only the flag NAME fragments are local, exactly as today.

---

### `internal/orchestrate/quadlet/container.tmpl` (template, transform) — D-09 range

**Analog:** `quadlet/openwebui.container.tmpl:13` — the EXACT `{{range}}` whitespace to mirror

**The precedent to copy byte-for-byte** (openwebui.container.tmpl:13-14):
```gotemplate
{{range .Env}}Environment={{.Key}}={{.Value}}
{{end}}
```
The newline sits INSIDE the range (after `{{.Value}}`), `{{end}}` flush-left on its own line, immediately before the next section header. Replicate this exact form for `AddDevice`, `GroupAdd`, and `Env`.

**Current container.tmpl lines to convert** (container.tmpl:10-11):
```gotemplate
AddDevice={{.AddDevice}}
GroupAdd={{.GroupAdd}}
```
→
```gotemplate
{{range .AddDevice}}AddDevice={{.}}
{{end}}{{range .GroupAdd}}GroupAdd={{.}}
{{end}}{{range .Env}}Environment={{.Key}}={{.Value}}
{{end}}
```

**Whitespace caveat (RESEARCH Pitfall 2 — the make-or-break of ROCM-03):** the Vulkan golden currently has `AddDevice=/dev/dri` then `GroupAdd=keep-groups` on the very next line with NO blank line and NO `Environment=` line. The range conversion MUST keep that byte-identical. Mechanism: after editing, run `go test ./internal/orchestrate/... -update` then `git diff internal/orchestrate/testdata/villa-llama.container.golden` MUST show ZERO change. If it changes, fix the TEMPLATE, never the golden. (Empty `.Env` must render zero lines, not a blank line.)

**Open Question A1 — `[Unit] Description=`:** container.tmpl:3 hardcodes `Description=VillaStraylight llama.cpp inference (Vulkan RADV)`. The ROCm unit's description would be wrong. Recommended (RESEARCH §Open Questions): add a seam-sourced `BackendLabel` field to `containerView` from `Backend.Name()` ("rocm"/"vulkan") and interpolate it — keeps it grep-gate-clean, single template. Confirm the Vulkan golden's Description line is unchanged after. Fallback if whitespace can't be reconciled: a second `container.rocm.tmpl` (RESEARCH A2).

---

### `internal/orchestrate/testdata/villa-llama-rocm.container.golden` (CREATE)

**Analog:** `testdata/villa-llama.container.golden` (mirror its structure so a reviewer diffs the two units and sees exactly the device/group/env/image delta — the unit IS the documentation).

**Delta over the Vulkan golden** (from `backend_rocm.go.ContainerArgs:64-82`, order verified — kfd FIRST resolves D-09's discretion):
- `Image=` → `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89` (backend_rocm.go:26)
- `AddDevice=/dev/kfd` THEN `AddDevice=/dev/dri` (two lines, kfd first)
- `GroupAdd=keep-groups` THEN `GroupAdd=render` (two lines)
- `Environment=HSA_OVERRIDE_GFX_VERSION=11.5.1` THEN `Environment=ROCBLAS_USE_HIPBLASLT=1` (two new lines, that order)
- `PublishPort` / `Volume` / `PodmanArgs` / `Exec` unchanged (same flags, same spec)
- `Description=` → ROCm label (per A1 resolution)

Do NOT hand-write this — generate it via `-update` after the template/parser land, then eyeball it against the delta above.

---

### `internal/orchestrate/render_test.go` (test, transform)

**Analog:** `TestRenderContainerGolden` (render_test.go:77-84) for the golden compare; `TestRenderOpenWebUITelemetryFrozen` (render_test.go:179-204) for the intent guard.

**Golden-compare pattern to copy** (render_test.go:77-84):
```go
func TestRenderContainerGolden(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil { t.Fatalf("Render: %v", err) }
	c := unitByName(t, units, "villa-llama.container")
	goldenCompare(t, "villa-llama.container.golden", c.Text)
}
```
New `TestRenderROCmContainerGolden`: build a `RenderInput` whose `Backend` is the ROCm backend (use `inference.BackendFor("rocm")` — `VulkanBackend()` has a constructor but ROCm is selected via `BackendFor`, backend.go:21), render, `goldenCompare(t, "villa-llama-rocm.container.golden", c.Text)`.

**Vulkan-unchanged proof:** `TestRenderContainerGolden` must stay green (no code change needed — that IS the additivity proof).

**Intent guard pattern to copy** (render_test.go:193-203 — count + full-set match, not subset):
```go
env := buildOpenWebUIView().Env
for _, p := range env {
	want := "Environment=" + p.Key + "=" + p.Value
	if !strings.Contains(c.Text, want) { t.Errorf(...) }
}
got := strings.Count(c.Text, "Environment=")
if got != len(env) { t.Errorf(...) }
```
New `TestRenderROCmEnvGroupFrozen`: assert the ROCm unit contains exactly the two expected `Environment=` lines AND `GroupAdd=render` (guards against the Pitfall-1 silent-drop even if the golden were regenerated wrong).

**`fixtureInput()` to adapt** (render_test.go:22-29): same fields, `Backend: <rocm backend>`, `Cfg.Backend: "rocm"`.

---

### `internal/preflight/rocm-policy.json` (CREATE) + `floors.go` (MODIFY) — D-04/D-05

**Analog (embed idiom):** `render.go:19-20`:
```go
//go:embed quadlet/*.tmpl
var quadletFS embed.FS
```
→ in preflight:
```go
//go:embed rocm-policy.json
var rocmPolicyBytes []byte
```

**Analog (the struct + loader to mirror):** `floors.go:53-77` — `Floor` struct + `Floors()` accessor.

**D-05 migration (behavior NO-OP — this is the acceptance test):** seed `rocm-policy.json` with the VERBATIM current constants so existing checks render byte-identical output:
```
KernelFloor   = "6.18.4"     (floors.go:16)
KernelTested  = "6.18.9"     (floors.go:21)
MesaFloor     = "25.0.0"     (floors.go:36 — migrate but keep UNWIRED, Deferred)
FirmwareFloor = "20260110"   (floors.go:40)
FirmwareDeny  = "20251125"   (floors.go:46)
```
Plus the NEW ROCm policy fields (D-04): firmware denylist (`["20251125"]`), image denylist (`["rocm7-nightlies"]`), required `HSA_OVERRIDE_GFX_VERSION` value (`"11.5.1"`).

Keep `Floors()` returning the SAME values, now `json.Unmarshal`'d from the embedded bytes instead of constants. **Proof:** `internal/preflight/checks_gpu_test.go` (`TestCheckKernelFloor`, `TestCheckFirmwareFloorIsWarnAdvisory`, `TestCompareVersions`) and any preflight golden MUST pass UNCHANGED. If a v1.0 golden changes, the loader is wrong — fix the loader, never the golden (RESEARCH anti-pattern). Panic/fail-closed on a malformed embed (build-time error, never runtime attacker data — Security V5).

`compareVersions`/`splitVersion` (floors.go:84-144) are already tested and handle distro suffixes — REUSE, do not re-roll (Don't Hand-Roll).

---

### `internal/preflight/checks_rocm.go` (CREATE) — D-01/D-02 `RunROCm`

**Analog:** `checks_gpu.go` (the check style + typed-Unknown→WARN downgrade) + `preflight.go` (`Run`/`pass`/`warn`/`fail` constructors).

**Entrypoint pattern to mirror** (preflight.go:142-168 — `Run`/`RunWithResources`):
```go
func Run(p detect.HostProfile) []CheckResult {
	return RunWithResources(p, ResourceReq{...})
}
func RunWithResources(p detect.HostProfile, req ResourceReq) []CheckResult {
	return []CheckResult{ checkVulkanIGPU(p), /* … */ }
}
```
→ `RunROCm(p detect.HostProfile) []CheckResult` delegating to `RunROCmWithPolicy(p, loadROCmPolicy())` (mirrors `Run`→`RunWithResources` so table tests inject a synthetic policy). Returns one `TierBlock` `CheckResult` per ROCm signal (gfx / kernel / firmware / hsa / image).

**Constructors to reuse — DO NOT build a new verdict type** (preflight.go:170-184):
```go
func pass(id, name string, tier Tier, detail, provenance string) CheckResult {...}
func warn(id, name string, tier Tier, detail, remediation, provenance, raw string) CheckResult {...}
func fail(id, name string, detail, remediation, provenance, raw string) CheckResult {...} // forces TierBlock
```

**The known-bad-FAIL / unknown-WARN check shape to copy** (checks_gpu.go:22-74 `checkVulkanIGPU` — the canonical D-02/D-15 template):
```go
icdKnown := icd.Known
if !icdKnown && !driKnown {                 // unevaluable → WARN, never FAIL
	return warn(id, name, TierBlock, "could not verify …", remediation, ...)
}
if icdKnown && icd.Value == "" {            // confident known-bad → FAIL
	return fail(id, name, "…absent", remediation, icd.Source, icd.Raw)
}
// partially verified → WARN; both good → pass
```

**Per-signal mapping (D-02, RESEARCH Pitfall 4 — off-hardware almost everything is Unknown→WARN):**
| Check | FAIL only when… | else WARN/PASS |
|-------|-----------------|----------------|
| gfx1151 | `ROCmPresent` true AND `IGPUGfxID.Known` AND value ≠ "gfx1151" | `IGPUGfxID.Known==false` → WARN |
| kernel floor | `KernelVersion.Known` AND `< policy.Kernel` (use `compareVersions`) | unknown → WARN |
| firmware deny | firmware date `Known` AND `== "20251125"` | not probed in Phase 1 → WARN (mirror `checkFirmwareFloor:115-130`) |
| HSA override | override `Known` absent | unknown → WARN |
| image policy | requested image `Known` to be `rocm7-nightlies` | config-driven, in-tree image passes (Pitfall 5) |

**CheckResult.ID collision (RESEARCH Pitfall 3 — load-bearing):** `checks_gpu.go` already uses literal `ID="PRE-06"` (kernel, line 83) and `"PRE-07"` (firmware, line 118). Do NOT reuse those in `RunROCm` — pick a distinct non-colliding scheme (e.g. `ROCM-PRE-gfx`, `ROCM-PRE-kernel`, `ROCM-PRE-firmware`, `ROCM-PRE-hsa`, `ROCM-PRE-image`) all mapping to the PRE-06 requirement. Do NOT renumber the existing IDs (churns v1.0 goldens — out of scope). `RunROCm` is a SEPARATE result slice from `Run`, so IDs only need uniqueness within the ROCm slice.

**Image-policy signal (RESEARCH Pitfall 5):** drive `image_policy_ok` / the nightlies check from the RESOLVED backend image string (config/request), NOT a host probe — the in-tree `rocm-7.2.4` digest always passes; the denylist gates a future config-supplied override.

---

### `cmd/villa/preflight.go` (CLI controller, request-response) — D-03 `--backend rocm`

**Analog:** self — `newPreflight` (preflight.go:30-47) + `renderPreflight` (preflight.go:53-93).

**Command builder to extend** (cmd/villa/preflight.go:30-47):
```go
func newPreflight() *cobra.Command {
	return &cobra.Command{
		Use:   "preflight",
		...
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := detect.Probe()
			results := preflight.Run(profile)   // ← branch on --backend: rocm → preflight.RunROCm(profile)
			code := renderPreflight(cmd.OutOrStdout(), results, jsonOut, verbose, force)
			os.Exit(code)
			return nil
		},
	}
}
```

**Flag-wiring pattern:** the global flags `jsonOut`/`verbose`/`force` are PERSISTENT flags registered on root (root.go:30-32, e.g. `pf.BoolVar(&force, "force", …)`). `--backend` is a LOCAL preflight flag — add it inside `newPreflight` via `cmd.Flags().StringVar(&backend, "backend", "", "…")` (a local string var), then in `RunE` select `preflight.RunROCm(profile)` when `backend == "rocm"`, else the unchanged `preflight.Run(profile)`. The standalone host path (`Run`) stays WARN-only and UNCHANGED (D-03).

**`renderPreflight` is reused as-is** — it already maps `[]CheckResult` → exit code (1/2/0) and applies `--force`; `RunROCm` returns the same `[]CheckResult` type so no rendering change is needed. New `cmd/villa/preflight_test.go` cases assert the `--backend rocm` table + that off-hardware it yields exit 2 (WARN), never exit 1 (RESEARCH Pitfall 4).

---

### `internal/detect/profile.go` (model, CRUD) — D-06/D-07 append + bump

**Analog:** self — the `HostProfile` struct + `hostProfileSchemaVersion`.

**Bump** (profile.go:6):
```go
const hostProfileSchemaVersion = 1   // → 2 (D-07)
```

**Append AFTER the GPU block** (profile.go:33, after `ROCmPresent`, before the memory-envelope block — D-06 "appended after the existing GPU block"). Add a nested struct, all fields typed-Optional (value.go), with explicit `json:"rocm_readiness"`:
```go
// after the GPU block, additive only — no existing field renamed/reordered (D-07)
ROCmReadiness ROCmReadiness `json:"rocm_readiness"`
```
with:
```go
type ROCmReadiness struct {
	HSAOverrideViable Bool `json:"hsa_override_viable"`
	FirmwareDateOK    Bool `json:"firmware_date_ok"`
	KernelFloorOK     Bool `json:"kernel_floor_ok"`
	RocminfoGfx1151   Bool `json:"rocminfo_gfx1151"`
	ImagePolicyOK     Bool `json:"image_policy_ok"`
}
```
(Exact key spellings/type name are Claude's discretion per D-06; fields MUST be typed-Optional per D-08. JSON keys above match D-06's listed names.)

**Append-only discipline:** existing fields keep names/types/order; `SchemaVersion` stays the LAST field. Eyeball the regenerated golden diff is purely additive (RESEARCH detect-freeze note).

---

### `internal/detect/` rocm readiness compute (CREATE/MODIFY) — D-08 typed-Optional

**Analog:** `gpu_amd.go` (`parseGfxID:339-355`, `rocmPresent:263-268` — the seam where ROCm/AMD literals are allowed) + `value.go` (the Optional constructors).

**The typed-Optional primitives to use** (value.go:70-74) — unset ≠ false (D-08, no-false-green):
```go
func KnownBool(v bool, src string) Bool  { return Bool{Value: v, Known: true,  Source: src} }
func UnknownBool(reason, raw string) Bool { return Bool{Known: false, Source: reason, Raw: raw} }
```
Compute each `rocm_readiness` field FROM existing `HostProfile` facts (gfx id, kernel version) + the embedded policy. A probe that can't run off-hardware (`rocminfo` absent → `IGPUGfxID` Unknown) yields `UnknownBool(...)`, NOT `KnownBool(false, …)` (RESEARCH Pattern 3 / Pitfall 4). Any new ROCm-specific probe MUST live behind the `gpu_amd.go` seam (mirror `igpuGfxID()`/`rocmPresent()`), not in backend-neutral files.

**Image-policy field:** config/request-driven, computed against the resolved image string, not a host probe (Pitfall 5).

---

### `cmd/villa/testdata/detect.golden.json` (re-freeze) — D-07

**Analog:** self + `cmd/villa/detect_test.go` `fixtureProfile()` (detect_test.go:18-39) + `TestJSONGolden` (detect_test.go:44-69).

**Re-freeze motion:**
1. `fixtureProfile()` hardcodes `SchemaVersion: 1` (detect_test.go:37) → bump to `2`.
2. Add the `rocm_readiness` fields to `fixtureProfile()`, MIXING Known and Unknown values (lock both serialized shapes — like `IGPUGfxID: detect.UnknownStr(...)` at line 25 vs the `Known` fields around it).
3. `go test ./cmd/villa/... -update`.
4. Review the golden diff is APPEND-ONLY: `schema_version` flips `1`→`2` and a `rocm_readiness` object appears AFTER the GPU block; NO existing key moved/renamed (eyeball JSON key order against detect.golden.json:1-87).

**Round-trip guard to extend:** `internal/detect/profile_test.go` `TestHostProfileJSONRoundTrips` (profile_test.go:26) + the `SchemaVersion == hostProfileSchemaVersion` assert (profile_test.go:15, :55) — extend the fixture so the new Optionals round-trip (Raw never serialized).

---

## Shared Patterns

### Seam-read (never re-type backend literals)
**Source:** `render.go:13-17` doc + `gpu_amd.go:12-15` seam comment; gate is `internal/inference/seam_test.go` `TestSeamGrepGate`.
**Apply to:** render.go (read via `ContainerArgs`/`Image`), checks_rocm.go (read via `HostProfile`), detect readiness (any ROCm probe behind `gpu_amd.go`).
The grep-gate-dodging fragment idiom (render.go:149-155): assemble flag names from pieces (`dash + "group" + "-add"`) so the bare token never appears as a contiguous literal. Add `flEnv = dash + "env"` the same way.

### Typed-Optional / no-false-green (D-08/D-15)
**Source:** `value.go:70-74` (`KnownBool`/`UnknownBool`); applied in `checks_gpu.go:36-68` (unevaluable→WARN, known-bad→FAIL).
**Apply to:** every `checks_rocm.go` check and every `rocm_readiness` field. Unknown underlying fact → WARN / Optional-unset, NEVER FAIL / `false`. Table-test BOTH branches per signal (mirror `TestCheckVulkanIGPU`).

### Verdict vocabulary reuse (D-01 — no new types)
**Source:** `preflight.go:30-105` (`Tier`/`Status`/`CheckResult`) + `:170-184` (`pass`/`warn`/`fail`).
**Apply to:** checks_rocm.go. `RunROCm` returns `[]CheckResult`; `cmd/villa/preflight.go renderPreflight` already maps it to exit codes 1/2/0 and `--force`.

### Byte-golden additivity (the phase's correctness contract)
**Source:** `render_test.go` `goldenCompare:53-73` (`-update` harness) + `detect_test.go` `TestJSONGolden:44-69`.
**Apply to:** new ROCm container golden (NEW file) while the Vulkan golden stays BYTE-IDENTICAL; re-frozen `detect.golden.json` (append-only). Refreeze command: `go test ./internal/orchestrate/... ./cmd/villa/... -update`. If an EXISTING golden changes, the delta is not additive — fix the code, not the golden.

### `go:embed` data (D-04)
**Source:** `render.go:19-20` (`//go:embed quadlet/*.tmpl`).
**Apply to:** `rocm-policy.json` (`//go:embed rocm-policy.json` → `[]byte` → `json.Unmarshal` into the policy struct; panic on malformed embed = build-time error, Security V5). Keep checks taking a policy PARAMETER (`RunROCmWithPolicy`) so tests inject a synthetic policy without touching the embedded file — mirrors `Run`/`RunWithResources`.

---

## No Analog Found

None. Every file in this phase extends or mirrors an existing in-repo file. This is a "copy the sibling, change the delta" phase; the planner can reference concrete line numbers above rather than RESEARCH.md generic patterns. The only genuinely new file with no exact same-package precedent is `rocm-policy.json` (data), whose loader pattern is the `//go:embed` + `Floor`-struct combination cited above.

---

## Metadata

**Analog search scope:** `internal/orchestrate/` (render.go, openwebui.go, quadlet/*.tmpl, render_test.go, testdata/), `internal/preflight/` (preflight.go, checks_gpu.go, floors.go), `internal/detect/` (profile.go, value.go, gpu_amd.go, detect_test/profile_test), `internal/inference/` (backend_rocm.go, backend.go, backend_vulkan.go), `cmd/villa/` (preflight.go, detect_test.go, root.go, testdata/).
**Files scanned (read in full):** 14.
**Key cross-cutting constraints surfaced:** (1) D-09 under-specifies — env + second group-add are silently dropped today (RESEARCH Pitfall 1); (2) Vulkan golden whitespace must stay byte-identical through the `{{range}}` conversion (Pitfall 2); (3) CheckResult.ID `PRE-06`/`PRE-07` already taken in checks_gpu.go — ROCm needs a distinct scheme (Pitfall 3); (4) off-hardware every ROCm BLOCK signal is Unknown→WARN, never FAIL (Pitfall 4); (5) image-policy is config-driven, not a host probe (Pitfall 5).
**Pattern extraction date:** 2026-06-06
```