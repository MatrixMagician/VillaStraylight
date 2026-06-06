# Phase 7: ROCm Render Unit + Preflight/Detect - Research

**Researched:** 2026-06-06
**Domain:** Go pure render/parse/verdict logic + Podman Quadlet unit generation + byte-golden/schema test mechanics (OFF-HARDWARE)
**Confidence:** HIGH

## Summary

Phase 7 is almost entirely a **read-the-existing-code** phase: three deltas over proven v1.0/Phase-6 primitives, all exercised off-hardware via table tests + byte goldens. The dominant research finding is that the actual code is **ahead of, and slightly diverges from,** what CONTEXT.md D-09 literally says — and the planner must reconcile that divergence or the byte-golden freeze will be wrong.

The single most important correction: **D-09 says only "`AddDevice` becomes `[]string`", but `backend_rocm.go.ContainerArgs` emits THREE render-affecting deltas that `parseContainerArgs` does not currently handle** — a second `--device` (kfd), a **second `--group-add` (render)**, and **two `--env` flags** that the parser has no case for and silently drops today. The repo's own `.planning/research/ARCHITECTURE.md` (line 79) already calls this out: `parseContainerArgs` must additionally collect a second `--group-add` and an ordered `Environment=` block (`containerView.Env []envPair`, mirroring the Open WebUI template's `{{range .Env}}Environment=` pattern). Rendering the ROCm unit by changing only `AddDevice` (the literal D-09 wording) would produce a unit **missing the HSA override + hipBLASLt env and the render group** — i.e. a non-functional unit that still passes a naive golden. The planner MUST treat "multi-device" as "multi-device **+ multi-group-add + env block**."

Everything else is mechanical and low-risk: the ROCm image digest is **already a real resolved sha256** (not a TODO), so the byte-golden can be frozen this phase with no digest-resolution task. The preflight pipeline, `CheckResult`/`Tier`/`Status` vocabulary, `pass`/`warn`/`fail` constructors, and typed-Unknown→WARN downgrade already model exactly what PRE-06 needs. The detect contract is guarded by a `cmd/villa/testdata/detect.golden.json` byte-golden + a `hostProfileSchemaVersion` constant + a round-trip test — append + bump 1→2 + re-freeze the golden is a known, repeatable motion.

**Primary recommendation:** Plan ROCM-03 as a **render-seam delta with THREE parser changes** (devices→`[]string`, group-add→`[]string`, **new ordered `Env []struct{Key,Value}`**), not just devices; freeze a new `villa-llama.rocm.container.golden` while keeping `villa-llama.container.golden` byte-identical (the Vulkan single-element/empty-env paths must render the identical existing lines). Plan PRE-06 as a new `RunROCm(profile)` entrypoint returning `[]CheckResult` from a `go:embed`'d `rocm-policy.json`, with the v1.0 `floors.go` constants migrated into that same policy as a behavior no-op. Plan DET-04 as an appended nested `rocm_readiness` typed-Optional object + `hostProfileSchemaVersion` 1→2 + re-frozen `detect.golden.json`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| ROCm image/device/group/env literals | `internal/inference/backend_rocm.go` (the seam) | — | Already encoded in Phase 6; the ONLY file allowed to know these (grep-gated). Phase 7 reads, never re-types. |
| Map seam args → Quadlet keys | `internal/orchestrate/render.go` (`parseContainerArgs`) | `quadlet/container.tmpl` | Pure renderer; consumes the seam, emits text. D-09 + the env/group-add deltas land here. |
| ROCm fitness verdict | `internal/preflight` (`RunROCm`) | `cmd/villa/preflight.go` (CLI surface, exit codes) | Pure library never exits/prints; CLI maps to exit codes + table. |
| Externalized version policy | `internal/preflight/rocm-policy.json` (`go:embed`) | `floors.go` (migrated constants) | One authoritative data source; loader populates the `Floor`/policy struct. |
| ROCm-readiness facts | `internal/detect/profile.go` (`HostProfile.rocm_readiness`) | `internal/detect/gpu_amd.go` (probes, the seam) | Backend-neutral struct holds typed-Optionals; any new ROCm probe lives behind the gpu_amd seam. |

## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** ROCm preflight EXTENDS the existing `internal/preflight` pipeline — new pure entrypoint (e.g. `RunROCm(profile)` / `RunROCmWithPolicy`) returning `TierBlock` `CheckResult`s, reusing `CheckResult`/`Tier`(BLOCK=exit1, WARN=exit2)/`Status`, the `pass`/`warn`/`fail` constructors, exit-code mapping, and the remediation field. NO separate ROCm verdict type. Phase 8's switch verb calls `RunROCm` and refuses on any `StatusFail`.
- **D-02:** Confident known-bad → `StatusFail` (refuse): firmware **exactly** `20251125`; a `rocm7-nightlies` image request; kernel `< 6.18.4`; missing HSA override; or `rocminfo` present but **not** gfx1151. A missing/unparseable BLOCK-tier signal (firmware unreadable, `rocminfo` absent off-hardware) **degrades to `StatusWarn`**, never FAIL (D-15 typed-Unknown). Bias against over-blocking.
- **D-03:** Surface via `villa preflight --backend rocm` (renders the ROCm `CheckResult` table off-hardware) AND as the reusable Phase-8 switch-gate function. Standalone host preflight (`Run`) stays WARN-only and **unchanged**.
- **D-04:** Embedded JSON policy file `internal/preflight/rocm-policy.json` loaded via `go:embed` into the `Floor`/policy struct: kernel-floor range (≥6.18.4), firmware floor (≥20260110) + firmware **denylist** (`20251125`), image **denylist** (`rocm7-nightlies`), required `HSA_OVERRIDE_GFX_VERSION` value.
- **D-05:** MIGRATE existing v1.0 `floors.go` constants (`KernelFloor`/`KernelTested`/`FirmwareFloor`/`FirmwareDeny`/`MesaFloor`) into the same embedded policy. Migration MUST keep current v1.0 preflight outputs/goldens **byte-identical** (constants → loaded data is a behavior no-op).
- **D-06:** Nested `rocm_readiness` object appended to `HostProfile` AFTER the GPU block: `hsa_override_viable`, `firmware_date_ok`, `kernel_floor_ok`, `rocminfo_gfx1151`, `image_policy_ok`. Top-level `ROCmPresent` stays where it is.
- **D-07:** Bump `hostProfileSchemaVersion` 1→2, additive only — existing fields keep names/types/order; the new object is strictly appended. The frozen v1.0 `--json` golden gains the nested object (re-frozen once).
- **D-08:** Each readiness field is a typed-Optional (`value.go` `Bool`/`Str`) so "couldn't detect" → null/unset, distinct from a real `false`.
- **D-09:** `containerView.AddDevice` becomes `[]string`; `parseContainerArgs` collects ALL `--device` tokens (not last-wins); `container.tmpl` ranges emitting one `AddDevice=` per device. Vulkan 1-element slice MUST render the identical single line (Vulkan golden byte-identical is the proof). Device order must match `backend_rocm.go` `ContainerArgs` order. Defensive check asserts `len(AddDevice) > 0`.

### Claude's Discretion

- Exact entrypoint/struct names (`RunROCm` vs `RunROCmWithPolicy`, policy loader/struct shape, the `rocm_readiness` Go type name).
- Exact JSON key spellings inside `rocm_readiness` and the embedded `rocm-policy.json` schema (provided fields are typed-Optional per D-08 and the policy carries ranges + both denylists per D-04).
- Whether the ROCm device order in the rendered unit is `kfd,dri` or `dri,kfd` — **must match `backend_rocm.go` `ContainerArgs` order exactly** (renderer never re-orders), frozen by the new ROCm golden. **(Research note: the order in code TODAY is `kfd` THEN `dri` — see below.)**

### Deferred Ideas (OUT OF SCOPE)

- `villa backend set rocm|vulkan` switch verb + transactional rollback + live generation-probe/residency cutover — **Phase 8** (BSET-01/02/03, on-hardware).
- Live ROCm offload / HSA-override / `load_tensors`-hang behavior on real gfx1151 — **Phase 8**.
- `villa bench` honest A/B (separate pp/tg tok/s) — **Phase 9** (BENCH-01/02).
- Backend-aware `recommend` + dashboard/`status` active-backend + live tok/s + ROCm-readiness indicator — **Phase 10** (REC-05, DASH-06); consumes the `rocm_readiness` object frozen here.
- `checkMesaFloor` wiring (the `floors.go` `MesaFloor` TODO) — pre-existing v1.0 deferral; migrate the constant into the policy (D-05) but **keep it unconsumed**.

## Phase Requirements

| ID | Description (verbatim from REQUIREMENTS.md) | Research Support |
|----|---------------------------------------------|------------------|
| ROCM-03 | The ROCm Quadlet unit renders the correct delta over the Vulkan unit — digest-pinned `kyuz0:rocm-7.2.4` (never nightlies), `/dev/kfd` + `/dev/dri` passthrough, `render` group, `HSA_OVERRIDE_GFX_VERSION=11.5.1` + `ROCBLAS_USE_HIPBLASLT=1` env, and `-ngl 999 -fa 1 --no-mmap`; frozen by a new byte-golden with the Vulkan golden unchanged. | `backend_rocm.go.ContainerArgs` already emits all of this (real digest). `parseContainerArgs` needs: devices→`[]string`, group-add→`[]string`, NEW ordered `Env`. `container.tmpl` ranges over each. Quadlet supports multiple `AddDevice=`/`GroupAdd=`/`Environment=` (cited). |
| PRE-06 | A reusable ROCm preflight verdict — confirms `rocminfo`/gfx1151, kernel floor (≥6.18.4), blocks `linux-firmware-20251125` (≥20260110 good), requires HSA override, refuses `rocm7-nightlies`; externalized `go:embed` version policy (ranges + denylist), refuses-with-remediation, biased not to over-block. | `preflight.go` `CheckResult`/`Tier`/`Status`/`pass`/`warn`/`fail` + D-15 downgrade already model the verdict. New `RunROCm` returns `[]CheckResult`. `go:embed` of `rocm-policy.json`; migrate `floors.go`. **ID-collision caveat below.** |
| DET-04 | `villa detect` reports ROCm readiness (additive fields on host profile / `--json`, schema-bumped, never reordering the frozen v1.0 contract). | `HostProfile` + typed-Optional `value.go` primitives. Append nested `rocm_readiness`, bump `hostProfileSchemaVersion` 1→2, re-freeze `cmd/villa/testdata/detect.golden.json`. |

## Standard Stack

No new external packages. This phase uses **only the Go standard library** (`embed`, `encoding/json`, `text/template`, `testing`) plus the already-vendored `github.com/spf13/cobra` (CLI) and `github.com/MatrixMagician/VillaStraylight/internal/*`.

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `embed` (stdlib) | Go ≥1.16 | `go:embed` the `rocm-policy.json` (D-04) and existing `quadlet/*.tmpl` | Already used in `render.go` (`//go:embed quadlet/*.tmpl`). Same idiom for policy JSON. |
| `text/template` (stdlib) | — | Render `container.tmpl` with `{{range}}` over devices/groups/env | Already the render mechanism; Open WebUI tmpl already uses `{{range .Env}}`. |
| `encoding/json` (stdlib) | — | Marshal `HostProfile`/`CheckResult`; parse `rocm-policy.json` | Already the detect/preflight JSON contract mechanism. |

**Installation:** none — `go build` only. No `npm`/`pip`/`cargo`; no Package Legitimacy Audit needed (no external packages added). The two container images referenced are **already pinned in code** (Phase 6) and are not installed by this phase.

## Package Legitimacy Audit

**Not applicable** — Phase 7 installs no external packages. The two container image digests it references are already resolved and pinned in `internal/inference/` from Phase 2/6:

| Image | Digest | Where | Status |
|-------|--------|-------|--------|
| `kyuz0/amd-strix-halo-toolboxes:vulkan-radv` | `sha256:9a74e555…ac7aad` | `backend_vulkan.go:19` | Pinned (Phase 2) |
| `kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4` | `sha256:2da150c1f0252f383b0b400f6cfa6630d3d34cf7c57132fe8445393b40531a89` | `backend_rocm.go:26` | **Real, resolved 2026-06-05 via `skopeo inspect`** — NOT a TODO placeholder. |

> **Answer to open question #4 (digest state):** The ROCm digest is a **genuine resolved sha256**, not a Phase-6 TODO. The byte-golden freeze does NOT depend on a digest-resolution task. `Image()` returns this constant; the renderer reads it through the seam.

## Architecture Patterns

### System Architecture Diagram

```
                         ┌───────────────────────────────────────────────┐
RenderInput{Backend} ───▶│ orchestrate.Render                             │
  (Backend = BackendFor  │   ├─ spec := RunSpec{...}                      │
   ("rocm"|"vulkan"))    │   ├─ cv := parseContainerArgs(                 │
                         │   │      Backend.Image(),  ◀── seam (literal)   │
                         │   │      Backend.ContainerArgs(spec)) ◀── seam  │
                         │   │        walks flags: --device* → []string    │
                         │   │                     --group-add* → []string  │ ★ NEW
                         │   │                     --env* → []Env{Key,Val}   │ ★ NEW
                         │   │                     -p/-v/--security-opt      │
                         │   │                     exec = args after image  │
                         │   └─ ExecuteTemplate(container.tmpl, cv)         │
                         │        {{range .AddDevice}}AddDevice=…{{end}}    │ ★ range
                         │        {{range .GroupAdd}}GroupAdd=…{{end}}      │ ★ range
                         │        {{range .Env}}Environment=…{{end}}        │ ★ range
                         └──────────────┬────────────────────────────────────┘
                                        ▼
                   villa-llama.container text  ──▶ golden compare
                   (Vulkan → existing golden BYTE-IDENTICAL;
                    ROCm → NEW golden, 2nd AddDevice + GroupAdd=render + 2 env lines)

detect.Probe() ──▶ HostProfile{ …GPU block…, rocm_readiness{…}, SchemaVersion:2 } ──▶ --json golden
preflight.RunROCm(profile) ──▶ []CheckResult ──▶ cmd: villa preflight --backend rocm (table + exit code)
   policy ◀── go:embed rocm-policy.json (migrated floors.go + ROCm ranges/denylists)
```

★ = the render deltas this phase introduces. Note **two** are NOT in the literal D-09 wording (group-add list, env block); D-09 mentions only devices. See "Common Pitfalls / Pitfall 1."

### Pattern 1: Seam-read renderer (never re-type backend literals)
**What:** `render.go` obtains every imperative value through `in.Backend.Image()` and `in.Backend.ContainerArgs(spec)` and maps it to a Quadlet key. It never writes `/dev/kfd`, `render`, `HSA_OVERRIDE…`, or the image string itself.
**When to use:** Every render change in this phase. The grep-gate (`internal/inference/seam_test.go`, `TestSeamGrepGate`) fails if a backend literal appears outside `internal/inference` + `internal/detect/gpu_amd.go`.
**Example (existing flag-fragment trick to dodge the grep gate, from `render.go`):**
```go
// Source: internal/orchestrate/render.go:149-155 (verified, in-repo)
const dash = "--"
var (
    flDevice   = dash + "device"
    flGroupAdd = dash + "group" + "-add" // assembled so the gate doesn't flag the bare token
    flSecOpt   = dash + "security-opt"
    flName     = dash + "name"
)
// NEW for Phase 7: add flEnv = dash + "env" and a collecting case.
```

### Pattern 2: `{{range}}` over an ordered slice in the template (Open WebUI precedent)
**What:** The Open WebUI container template already emits a variable-length env block from an ordered slice. Reuse the identical idiom for ROCm devices/groups/env so a 1-element (Vulkan) slice renders one line and a 2-element (ROCm) slice renders two — order preserved from `ContainerArgs`.
**Example (existing, verified):**
```go
// Source: internal/orchestrate/quadlet/openwebui.container.tmpl:13
{{range .Env}}Environment={{.Key}}={{.Value}}
{{end}}
```
**Whitespace caveat (load-bearing for byte-goldens):** the Open WebUI tmpl's `{{range}}…{{end}}` block places a trailing newline structure that the planner must replicate EXACTLY in `container.tmpl` so the Vulkan golden (which has `AddDevice=/dev/dri` followed by `GroupAdd=keep-groups` on the next line with no blank line) stays byte-identical. Regenerate goldens with `go test ./... -update` and diff the Vulkan golden to confirm zero change.

### Pattern 3: Typed-Optional readiness facts (no false-green)
**What:** Each `rocm_readiness` field is a `detect.Bool`/`detect.Str` so "couldn't detect off-hardware" serializes as `{"known":false,…}`, distinct from a real `false`. Mirrors `ROCmPresent`, `IGPUGfxID`, etc.
**Example (existing primitive, verified):**
```go
// Source: internal/detect/value.go:71-74
func KnownBool(v bool, src string) Bool { return Bool{Value: v, Known: true, Source: src} }
func UnknownBool(reason, raw string) Bool { return Bool{Known: false, Source: reason, Raw: raw} }
```

### Pattern 4: `go:embed` JSON policy loaded once, test-overridable
**What:** Embed `rocm-policy.json` at package scope; parse into the policy struct in a package-level `var` or `init`. Keep the check functions accept a policy parameter (like `Floors()` returns a `Floor` value today) so tests inject a synthetic policy without touching the embedded file.
**Recommended shape:**
```go
// Source: idiomatic Go embed (CITED: pkg.go.dev/embed) + in-repo Floor pattern
//go:embed rocm-policy.json
var rocmPolicyBytes []byte

func loadROCmPolicy() ROCmPolicy { /* json.Unmarshal(rocmPolicyBytes, …); panic on malformed embed */ }

// RunROCm uses the embedded default; RunROCmWithPolicy(p, pol) takes an injected
// policy for table tests — exactly mirroring Run / RunWithResources today.
```
**Behavior-no-op proof for D-05 migration:** keep `Floors()` returning the SAME values, now sourced from the embedded JSON instead of constants. The existing `checks_gpu_test.go` (`TestCheckKernelFloor`, `TestCheckFirmwareFloorIsWarnAdvisory`, `TestCompareVersions`) and any preflight goldens must pass **unchanged** — that is the migration's acceptance test. Seed `rocm-policy.json` with the verbatim current values: kernel `6.18.4`, kernelTested `6.18.9`, mesa `25.0.0`, firmware `20260110`, firmwareDeny `20251125`.

### Anti-Patterns to Avoid
- **Rendering the ROCm unit by changing only `AddDevice`** (the literal D-09 text). It omits the render group and the HSA/hipBLASLt env → a non-functional unit that a naive golden still "freezes." Handle devices **and** group-add **and** env.
- **Last-wins flag parsing.** Today `parseContainerArgs` overwrites `cv.GroupAdd` on each `--group-add` — for ROCm that silently drops `keep-groups` OR `render` depending on order. Collect into slices.
- **Re-ordering devices/env in the renderer.** Order is the seam's (`ContainerArgs`), frozen by the golden. Renderer preserves slice order.
- **Editing the v1.0 detect/preflight goldens to "fix" them during the floors migration.** If a v1.0 golden changes, the migration is not a no-op — fix the loader, not the golden.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Version comparison (kernel/firmware floors) | A new comparator | `floors.go.compareVersions` / `splitVersion` (existing, tested by `TestCompareVersions`) | Already handles distro suffixes (`6.18.9-300.fc44`) and malformed input by erring toward WARN. |
| Verdict vocabulary (BLOCK/WARN, PASS/WARN/FAIL, exit codes, remediation) | A ROCm-specific verdict type | `preflight.CheckResult` + `Tier`/`Status` + `pass`/`warn`/`fail` (D-01) | The CLI exit-code mapping (`cmd/villa/preflight.go renderPreflight`) and `--force` already consume `[]CheckResult`. |
| "Couldn't detect" vs real-false | A `*bool` / sentinel | `detect.Bool`/`detect.Str` typed-Optionals (D-08) | The whole D-16/D-15 no-false-green contract is built on these; `--json` consumers depend on the `known` field. |
| Multi-value unit keys | String-join devices into one `AddDevice=` line | Multiple `AddDevice=`/`GroupAdd=`/`Environment=` lines (Quadlet supports repeats — cited) | Podman parses repeated keys; a comma-joined single line is NOT valid Quadlet device syntax. |
| Golden update harness | A new `-update` flow | The existing `goldenCompare` (`render_test.go`) / `TestJSONGolden` (`detect_test.go`) `-update` pattern | Both packages already have `var update = flag.Bool("update", …)`; run `go test ./... -update`. |

**Key insight:** Phase 7 adds essentially **zero new algorithms** — it is wiring three already-present mechanisms (seam-read render, typed-verdict pipeline, typed-Optional profile) to ROCm data that Phase 6 already encoded. The risk is entirely in **fidelity** (byte-golden additivity, schema append-only, behavior-no-op migration), not in novel logic.

## Common Pitfalls

### Pitfall 1: D-09 under-specifies the render delta (env + group-add are missing from the literal text)
**What goes wrong:** A planner who follows D-09's literal wording ("`AddDevice` becomes `[]string`") changes ONLY the device handling. The rendered ROCm unit then has the right two `AddDevice=` lines but **no `GroupAdd=render`** (last-wins keeps only one group-add) and **no `Environment=HSA_OVERRIDE_GFX_VERSION=11.5.1` / `ROCBLAS_USE_HIPBLASLT=1`** (the parser has no `--env` case — those tokens are silently dropped today). The unit would CPU-fall-back on real hardware (Phase 8) and the golden would still pass.
**Why it happens:** `parseContainerArgs` (verified, render.go:160-190) has cases for `--device`, `--group-add`, `--security-opt`, `-p/--publish`, `-v/--volume`, `--name` — but **none for `--env`**. And `--group-add` is last-wins (`cv.GroupAdd = flags[i+1]`), so ROCm's `keep-groups` THEN `render` collapses to one value.
**How to avoid:** Treat ROCM-03 as THREE parser changes (the repo's own `.planning/research/ARCHITECTURE.md:79` confirms this): (1) `AddDevice []string` collecting all `--device`; (2) `GroupAdd []string` collecting all `--group-add`; (3) NEW `Env []struct{Key,Value}` collecting all `--env` (split each `K=V` on first `=`). Range over all three in `container.tmpl`. Update the defensive check to `len(AddDevice) > 0 && len(GroupAdd) > 0` (env may legitimately be empty for Vulkan, so do NOT require `len(Env) > 0`).
**Warning signs:** The new ROCm golden lacks `Environment=` lines, or has only one `GroupAdd=`. Add a focused assertion (mirroring `TestRenderOpenWebUITelemetryFrozen`) that the ROCm unit contains exactly the two expected env lines and `GroupAdd=render`.

### Pitfall 2: Vulkan golden whitespace drift from the new `{{range}}` blocks
**What goes wrong:** Converting `AddDevice={{.AddDevice}}` (single line) to `{{range .AddDevice}}AddDevice={{.}}\n{{end}}` changes trailing-newline behavior; the Vulkan golden gains/loses a blank line → no longer byte-identical → ROCM-03 "Vulkan unchanged" criterion fails.
**Why it happens:** `text/template` `{{range}}…{{end}}` newline handling is fiddly; the Open WebUI tmpl solved it one specific way (`{{range .Env}}…\n{{end}}` with the `{{end}}` flush against `[Service]`).
**How to avoid:** Mirror the Open WebUI tmpl's exact `{{range}}` formatting. After editing `container.tmpl`, run `go test ./internal/orchestrate/... -update` then **`git diff internal/orchestrate/testdata/villa-llama.container.golden` must show ZERO change**. If it changes, the empty/single-element render path is wrong — fix the template, never the golden.
**Warning signs:** `git status` shows the Vulkan golden modified after `-update`.

### Pitfall 3: PRE-06 requirement-ID vs CheckResult-ID collision
**What goes wrong:** The existing `checks_gpu.go` already uses `CheckResult.ID = "PRE-06"` for the **kernel floor** check and `"PRE-07"` for the firmware floor — these are NOT the v1.1 requirement IDs (REQUIREMENTS.md only defines PRE-01..04 for v1.0 and PRE-06 for the NEW ROCm verdict). A naive ROCm check that emits `ID:"PRE-06"` would duplicate the existing kernel-floor result's ID inside `RunROCm`, and any test asserting "one result per ID" (cf. `TestRunReturnsOneResultPerRequirement`) would be ambiguous.
**Why it happens:** v1.0 reused the PRE-0x numbering as internal check IDs before the v1.1 requirement IDs were assigned; the two namespaces overlap by coincidence.
**How to avoid:** The planner should pick a **distinct, non-colliding CheckResult.ID scheme for the ROCm checks** (e.g. `ROCM-PRE-kernel`, `ROCM-PRE-firmware`, `ROCM-PRE-gfx`, `ROCM-PRE-hsa`, `ROCM-PRE-image`, or `PRE-06a/b/c…`) and map them collectively to the PRE-06 **requirement**. Do NOT renumber the existing `checks_gpu.go` IDs (that would churn v1.0 goldens/tests — out of scope). Document the requirement-ID→check-ID mapping in the plan. Note that `RunROCm` is a **separate** result slice from `Run`, so the IDs only need to be unique within the ROCm slice and not clash with downstream expectations.
**Warning signs:** A `RunROCm` test that filters by `ID == "PRE-06"` returns two results.

### Pitfall 4: Off-hardware, almost every ROCm BLOCK signal is unevaluable → must WARN, not FAIL
**What goes wrong:** On the CI/dev box (no gfx1151, no `rocminfo`), a ROCm check that treats "rocminfo absent" as a confident known-bad would FAIL — over-blocking, violating PRE-06's "biased not to over-block" and D-02. The phase is explicitly off-hardware.
**Why it happens:** The detect layer reports `ROCmPresent = KnownBool(false, "rocminfo not on PATH")` and `IGPUGfxID = UnknownStr(…)` off-hardware. "rocminfo not installed" is a confident false for *presence* but is **unevaluable** for *gfx1151-ness*.
**How to avoid:** Apply D-02/D-15 precisely: only a **positively-detected** bad state FAILs — `rocminfo` present AND gfx id known AND ≠ gfx1151; firmware date **known** AND == 20251125; kernel **known** AND < 6.18.4; image request **known** to be nightlies; HSA override **known** absent. Any `Known=false` underlying fact → `StatusWarn` ("could not verify"). Table-test BOTH branches per check (known-bad→FAIL, unknown→WARN) the way `TestCheckVulkanIGPU` already does.
**Warning signs:** `RunROCm(detect.Probe())` on the CI host returns any `StatusFail`.

### Pitfall 5: `image_policy_ok` / nightlies denylist has no host signal off-hardware
**What goes wrong:** `image_policy_ok` and the "refuses `rocm7-nightlies`" check have no *host* fact to read — the image is a *config/request* input, not a probed host property. A planner might try to probe the host for it and find nothing.
**Why it happens:** The nightlies denylist gates the **requested image**, not the machine. `backend_rocm.go` hardcodes the safe `rocm-7.2.4` digest, so for the in-tree backend the answer is always "OK" — the denylist exists for a *future* config-supplied image override.
**How to avoid:** Drive the image-policy check from the **resolved backend image string** (or a config field), not a host probe. For DET-04's `image_policy_ok` field, compute it against the configured/resolved image (the in-tree ROCm image passes; a `rocm7-nightlies` tag would set it false). For PRE-06, the check input is the image the switch would render. Document that this is a config-driven, not host-driven, signal.

## Code Examples

### Existing `parseContainerArgs` flag walk (the THREE cases to add/change)
```go
// Source: internal/orchestrate/render.go:160-198 (verified, in-repo). CURRENT shape:
for i := 0; i < len(flags); i++ {
    switch flags[i] {
    case flDevice:
        if i+1 < len(flags) { cv.AddDevice = flags[i+1]; i++ }   // ← becomes append to []string
    case flGroupAdd:
        if i+1 < len(flags) { cv.GroupAdd = flags[i+1]; i++ }     // ← becomes append to []string (last-wins today!)
    // NO case flEnv today → HSA_OVERRIDE / ROCBLAS env are SILENTLY DROPPED. ← add flEnv, split K=V
    case flSecOpt: /* … */
    case "-p", "--publish": /* … */
    case "-v", "--volume": /* … */
    case flName: i++
    }
}
// Defensive check today requires AddDevice/GroupAdd/PublishPort/Volume/PodmanArgs/Exec non-empty.
// Phase 7: change to len(AddDevice)>0 && len(GroupAdd)>0 (+ keep the scalar fields); Env may be empty (Vulkan).
```

### Exact current Vulkan golden (the byte-frozen baseline — open question #3 answered)
```
# Source: internal/orchestrate/testdata/villa-llama.container.golden (verified, in-repo)
[Container]
ContainerName=villa-llama
Image=docker.io/kyuz0/amd-strix-halo-toolboxes:vulkan-radv@sha256:9a74e555c45864352a4077528836988d448e9f030fbab9f7376ea1c603ac7aad
Network=villa.network
AddDevice=/dev/dri
GroupAdd=keep-groups
PublishPort=127.0.0.1:8080:8080
Volume=/home/villa/.local/share/villa/models:/models:ro,z
PodmanArgs=--security-opt seccomp=unconfined
Exec=llama-server -m /models/qwen3-35b-a3b-moe-64.gguf -c 131072 --host 0.0.0.0 --port 8080 -ngl 999 -fa 1 --no-mmap -lv 4 --metrics
```
**The ROCm golden's delta over this** (from `backend_rocm.go.ContainerArgs`, order verified):
- `Image=` → the `rocm-7.2.4@sha256:2da150c1…531a89` image.
- `AddDevice=/dev/kfd` THEN `AddDevice=/dev/dri` (kfd FIRST — that is the code order; the discretion in D-09 is resolved by the code).
- `GroupAdd=keep-groups` THEN `GroupAdd=render` (two lines).
- two new lines: `Environment=HSA_OVERRIDE_GFX_VERSION=11.5.1` then `Environment=ROCBLAS_USE_HIPBLASLT=1` (that order).
- `PodmanArgs=--security-opt seccomp=unconfined` unchanged; `PublishPort`/`Volume`/`Exec` unchanged (same flags, same ctx).
- `Description=` differs (Vulkan tmpl hardcodes "Vulkan RADV"). **Decision for the planner:** the description string lives in `container.tmpl` as a literal, not the seam — rendering an accurate ROCm description needs either a tmpl field or a second template. Simplest seam-clean option: add a `Description`/`BackendLabel` field to `containerView` sourced from `Backend.Name()` (not a literal). Flag this; it affects whether one template serves both backends or the ROCm unit gets its own.

### ROCm `ContainerArgs` — exact device/group/env/image order (open question #4 answered)
```go
// Source: internal/inference/backend_rocm.go:64-84 (verified, in-repo)
args := []string{
    "run", "--rm",
    "--name", spec.ContainerName,
    "--device", "/dev/kfd",          // device #1 (kfd FIRST)
    "--device", "/dev/dri",          // device #2
    "--group-add", "keep-groups",    // group #1
    "--group-add", "render",         // group #2
    "--security-opt", "seccomp=unconfined",
    "--env", "HSA_OVERRIDE_GFX_VERSION=11.5.1",  // env #1
    "--env", "ROCBLAS_USE_HIPBLASLT=1",          // env #2
    "-p", hostPublish,
    "-v", modelBind,
    rocmImage,                       // image token (digest-pinned, REAL sha256)
    "llama-server", "-m", containerModelPath, "-c", …, "--host", "0.0.0.0", "--port", …,
}
args = append(args, llamaServerFlags...) // -ngl 999 -fa 1 --no-mmap -lv 4 --metrics
```
> Note: ROCm reuses `seccomp=unconfined` but does **NOT** currently emit `--security-opt label=disable`. PITFALLS.md R7 (MEDIUM) flags that rootless `/dev/kfd` may need `label=disable` on Fedora. That is a **Phase 8 on-hardware** concern (live SELinux AVC). Phase 7 freezes whatever `ContainerArgs` emits today — do NOT add `label=disable` speculatively; if Phase 8 needs it, it is added at the seam and re-freezes the golden then. (Open item flagged below.)

### detect `--json` contract freeze mechanism (open question #6 answered)
```go
// Source: cmd/villa/detect_test.go:44-69 (verified) + internal/detect/profile_test.go (verified)
// Two guards freeze the contract:
//  1. cmd/villa/testdata/detect.golden.json — byte-golden of renderDetect(fixtureProfile(), json=true).
//     fixtureProfile() in detect_test.go hardcodes SchemaVersion:1 → MUST bump to 2 (D-07) and re-run -update.
//  2. internal/detect/profile_test.go asserts p.SchemaVersion == hostProfileSchemaVersion + round-trips
//     the typed-Optional shape (Raw never serialized).
// There is NO separate schema-version golden file; the version is just the int constant.
// DET-04 motion: (a) add rocm_readiness struct after the GPU block in profile.go;
//   (b) hostProfileSchemaVersion = 2; (c) wire the new fields in fixtureProfile() (mix Known/Unknown
//   to lock both shapes); (d) `go test ./cmd/villa/... -update`; (e) review the golden diff is
//   APPEND-ONLY (no existing field moved/renamed — eyeball the json key order).
```

## State of the Art

No fast-moving external state for this phase — it is internal Go + Podman Quadlet, both stable.

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Single scalar `AddDevice`/`GroupAdd`, env ignored in renderer | Slices + env block ranged in template | This phase (ROCM-03) | Enables multi-device/multi-group/env backends without reshaping the seam. |
| Version floors as Go constants (`floors.go`) | `go:embed`'d `rocm-policy.json` | This phase (D-04/D-05) | A bad firmware/image can be denied by editing data, not rebuilding logic. |
| `hostProfileSchemaVersion = 1` | `= 2` | This phase (D-07) | Dashboard/recommend can detect the v1.1 ROCm-readiness contract. |

**Deprecated/outdated:** none introduced. The `MesaFloor` constant remains intentionally **unwired** (pre-existing v1.0 deferral) — migrate it into the policy JSON but do NOT add a consuming check (CONTEXT.md Deferred Ideas).

## Runtime State Inventory

> Phase 7 is pure render/parse/verdict + test fixtures. It writes **no** runtime state, registers nothing with the OS, and migrates no stored data. The `floors.go`→`rocm-policy.json` "migration" (D-05) is a **source-code refactor**, not a data/runtime migration — there is no stored or live copy of those constants anywhere off-disk.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — verified by inspecting the three packages; no DB/datastore touched. | none |
| Live service config | None — no n8n/systemd/external service config written this phase (rendering produces text returned to the caller; install/run is Phase 8). | none |
| OS-registered state | None — no Task Scheduler/systemd unit registered; `Render` returns unit TEXT, it does not write/enable units. | none |
| Secrets/env vars | The `HSA_OVERRIDE_GFX_VERSION`/`ROCBLAS_USE_HIPBLASLT` are **container env literals in the seam**, not host secrets/SOPS keys. No secret renamed. | none |
| Build artifacts | `go:embed rocm-policy.json` is compiled INTO the binary — there is no stale external artifact; a `go build` re-embeds it. Existing goldens are test fixtures, re-frozen via `-update`. | rebuild only |

**Canonical question — "after every file is updated, what runtime systems still have the old string cached?"** → **Nothing.** This phase produces in-memory text + compiled-in data + test fixtures only.

## Environment Availability

> Phase 7 is code/config/test only — pure off-hardware logic. Per Step 2.6 skip condition: **SKIPPED for runtime deps** (no live ROCm/`rocminfo`/`skopeo` needed — the digest is already resolved). The only "dependency" is the Go toolchain already in use.

| Dependency | Required By | Available | Version | Fallback |
|------------|-------------|-----------|---------|----------|
| Go toolchain (`go test`, `embed`) | build + goldens | ✓ (project already builds) | ≥1.16 for `go:embed` | — |
| `rocminfo` / gfx1151 hardware | NOT required this phase (off-hardware) | ✗ (and that's fine) | — | All ROCm-readiness signals degrade to typed-Unknown/WARN off-hardware by design (D-02/D-08). |

**Missing dependencies with no fallback:** none. **Missing with fallback:** `rocminfo`/hardware — by design, off-hardware unknowns are WARN/unset, not failures.

## Validation Architecture

> `workflow.nyquist_validation` not disabled in config → section included.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table tests + `-update` goldens) |
| Config file | none — `go test` |
| Quick run command | `go test ./internal/orchestrate/... ./internal/preflight/... ./internal/detect/... ./cmd/villa/...` |
| Full suite command | `go test ./...` |
| Golden refreeze | `go test ./internal/orchestrate/... ./cmd/villa/... -update` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ROCM-03 | ROCm unit renders kfd+dri, render group, HSA/HIPBLASLT env, rocm digest | golden | `go test ./internal/orchestrate/ -run RenderROCmContainerGolden` | ❌ Wave 0 (new test + golden) |
| ROCM-03 | Vulkan golden BYTE-IDENTICAL after template change | golden | `go test ./internal/orchestrate/ -run RenderContainerGolden` | ✅ (`TestRenderContainerGolden`) — must stay green |
| ROCM-03 | ROCm env/group present (intent guard, à la telemetry-frozen) | unit | `go test ./internal/orchestrate/ -run RenderROCmEnvGroupFrozen` | ❌ Wave 0 |
| PRE-06 | known-bad→FAIL, unknown→WARN per signal (gfx/kernel/firmware/hsa/image) | table | `go test ./internal/preflight/ -run RunROCm` | ❌ Wave 0 |
| PRE-06 | `floors.go`→policy migration is byte-no-op | unit/golden | `go test ./internal/preflight/ ./cmd/villa/ -run 'CheckKernelFloor|CheckFirmwareFloor|Preflight'` | ✅ (existing must stay green) |
| PRE-06 | embedded `rocm-policy.json` parses + carries ranges/denylists | unit | `go test ./internal/preflight/ -run ROCmPolicy` | ❌ Wave 0 |
| DET-04 | `rocm_readiness` appended, schema=2, append-only | golden | `go test ./cmd/villa/ -run JSONGolden` | ✅ (`TestJSONGolden`) — re-frozen once |
| DET-04 | readiness fields are typed-Optional (unset≠false) | unit | `go test ./internal/detect/ -run RoundTrip` | ✅ (`TestHostProfileJSONRoundTrips`) — extend |

### Sampling Rate
- **Per task commit:** the per-package quick run for the package touched.
- **Per wave merge:** `go test ./...`.
- **Phase gate:** `go test ./...` green (esp. the two MUST-stay-green goldens: Vulkan container + v1.0 preflight) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/orchestrate/testdata/villa-llama.rocm.container.golden` — new ROCm golden (after the template + parser change land).
- [ ] `internal/orchestrate/render_test.go` — `TestRenderROCmContainerGolden` (uses `inference.BackendFor("rocm")` fixture input) + an env/group intent guard.
- [ ] `internal/preflight/rocm-policy.json` (embedded) + `internal/preflight/rocm_checks.go` (`RunROCm`/`RunROCmWithPolicy`) + `rocm_checks_test.go` (table: known-bad/unknown per signal).
- [ ] `internal/preflight/policy_test.go` — assert the embedded JSON loads + equals the migrated floor values (no-op proof).
- [ ] `cmd/villa/preflight.go` — wire `--backend rocm` flag to call `RunROCm` (D-03); `cmd/villa/preflight_test.go` cases.
- [ ] `internal/detect/profile.go` — `rocm_readiness` struct + `hostProfileSchemaVersion = 2`; update `fixtureProfile()` in `detect_test.go`; re-freeze `detect.golden.json`.
- [ ] Framework install: none — Go `testing` already in use.

## Security Domain

> `security_enforcement` not disabled → included. Phase 7 is off-hardware logic; the security surface is small but real (a strictly-local, zero-telemetry product).

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V5 Input Validation | yes | `rocm-policy.json` is a trusted, `go:embed`'d (compiled-in) file — not user input — so it is not an injection vector; still validate on load and `panic`/fail-closed on a malformed embed (a build-time error, never a runtime parse of attacker data). |
| V5 (tool output parsing) | yes | `rocminfo`/`vulkaninfo` output is already bounded (`maxToolOutput` 8 KiB) and parsed with fixed-arg `exec` (no `sh -c`) in `gpu_amd.go` — reuse, do not re-roll. |
| V6 Cryptography | no | No crypto introduced. Image integrity is via the **pinned sha256 digest** (already in code) — supply-chain control, not a Phase-7 task. |
| V2/V3/V4 (auth/session/access) | no | No auth surface; CLI is local-only. |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Floating ROCm image tag reintroduces nightlies 64 GB cap / telemetry | Tampering | Pinned digest (in code); the `rocm7-nightlies` denylist (PRE-06) refuses a nightlies request. |
| `--security-opt label=disable` weakens SELinux separation if added as default | Elevation of Privilege / Info Disclosure | **NOT added this phase.** PITFALLS R7: prefer a narrow policy; `label=disable` only as a reviewed Phase-8 fallback scoped to the one container. Phase 7 freezes the current (no label-disable) seam output. |
| Container env injection via model/path | Tampering | Already mitigated: exec args are fixed slices, never shell-interpolated (T-02-08, verified in both backends). |
| Over-blocking a working host (denial of the product to the user) | Denial of Service (self-inflicted) | D-02/D-15 bias: only confident known-bad FAILs; unknown→WARN. |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The planner should add a `Description`/`BackendLabel` field (sourced from `Backend.Name()`) or a second template so the ROCm unit's `[Unit] Description=` isn't the literal "Vulkan RADV". | Code Examples / golden delta | If ignored, the ROCm golden's Description line is wrong/misleading; cosmetic but visible in the golden. Low functional risk, but it IS a render delta the golden captures. |
| A2 | One `container.tmpl` can serve both backends via `{{range}}` + a Description field; a separate `container.rocm.tmpl` is not required. | Pattern 2 | If template whitespace can't be made to keep Vulkan byte-identical AND render ROCm, a second template is the fallback (more duplication, still works). |
| A3 | `--security-opt label=disable` is NOT added this phase (Phase 8 on-hardware decides). | Security / Code Examples | If the kyuz0 image/Fedora rootless actually requires it for the unit to even render correctly, that's a seam change — but it's a *runtime* need (SELinux AVC at run), not a render-correctness need, so deferring is sound. |
| A4 | The ROCm-check `CheckResult.ID`s should use a distinct scheme to avoid the `PRE-06`/`PRE-07` collision with existing `checks_gpu.go` IDs; existing IDs are NOT renumbered. | Pitfall 3 | If a downstream consumer expects ROCm checks under literal `PRE-06`, the mapping must be documented; low risk since `RunROCm` is a separate slice. |
| A5 | `image_policy_ok` / nightlies denial is a config/request-driven signal, not a host probe (the in-tree image always passes). | Pitfall 5 | If the planner expects a host probe for it, they'll find no signal; framing it as config-driven is correct per the requirement (gates the *requested* image). |

## Open Questions

1. **`[Unit] Description=` for the ROCm unit (A1).**
   - What we know: `container.tmpl` hardcodes "VillaStraylight llama.cpp inference (Vulkan RADV)". The seam exposes `Backend.Name()` ("rocm"/"vulkan").
   - What's unclear: one template + a `Description`/label field, or a second template.
   - Recommendation: add a seam-sourced `BackendLabel` field to `containerView` (keeps it grep-gate-clean) and interpolate it into `Description=`; single template. Confirm the Vulkan golden's Description line is unchanged after.

2. **Whether one template can keep Vulkan byte-identical while adding ROCm ranges (A2).**
   - Recommendation: try single-template first (mirror Open WebUI `{{range}}` whitespace); if `-update` mutates the Vulkan golden and it can't be massaged back, split to `container.rocm.tmpl`.

3. **`label=disable` for kfd (A3, PITFALLS R7, MEDIUM).** Deferred to Phase 8 (on-hardware SELinux). Phase 7 freezes today's seam output. Flag so Phase 8 knows the golden re-freezes if the seam gains it.

## Sources

### Primary (HIGH confidence)
- In-repo source (verified by reading this session): `internal/orchestrate/render.go`, `quadlet/container.tmpl`, `quadlet/openwebui.container.tmpl`, `render_test.go`, `testdata/villa-llama.container.golden`; `internal/inference/backend_rocm.go`, `backend_vulkan.go`; `internal/preflight/preflight.go`, `floors.go`, `checks_gpu.go`, `checks_gpu_test.go`, `preflight_test.go`; `internal/detect/profile.go`, `value.go`, `gpu_amd.go`, `profile_test.go`, `testdata/rocminfo-present.txt`; `cmd/villa/preflight.go`, `detect.go`, `detect_test.go`.
- `.planning/REQUIREMENTS.md` (ROCM-03/PRE-06/DET-04 verbatim), `.planning/phases/07-…/07-CONTEXT.md` (D-01..D-09), `.planning/research/PITFALLS.md` (R7 kfd/SELinux, externalized policy), `.planning/research/ARCHITECTURE.md` (line 79 — the env+group-add render delta).
- [Podman `podman-systemd.unit(5)` Quadlet docs](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) — `AddDevice=`/`GroupAdd=`/`Environment=` may each be listed multiple times; `SecurityLabelDisable=` maps to `--security-opt label=disable`. **[CITED]**

### Secondary (MEDIUM confidence)
- `.planning/research/PITFALLS.md` R7 source links: Red Hat AI Inference Server (Podman+ROCm `--device=/dev/kfd --device=/dev/dri --group-add keep-groups --security-opt=label=disable`) — HIGH for the kfd+SELinux pattern; relevant only to deferred Phase-8 SELinux.

### Tertiary (LOW confidence)
- none load-bearing.

## Metadata

**Confidence breakdown:**
- Standard stack (stdlib only, no new packages): HIGH — verified no external dep; both container digests already pinned in code.
- Architecture / render delta: HIGH — read every relevant file; the env+group-add gap is confirmed in code AND in the repo's own ARCHITECTURE.md.
- Preflight pipeline reuse: HIGH — `CheckResult`/`Tier`/`Status`/constructors read directly; the ID-collision caveat is a verified observation, not a guess.
- Detect contract freeze: HIGH — `detect.golden.json` + `hostProfileSchemaVersion` + round-trip test all read.
- Quadlet multi-key semantics: HIGH — official Podman docs cited.
- `label=disable`/SELinux at runtime: MEDIUM — but explicitly OUT of Phase 7 scope (Phase 8 on-hardware).

**Research date:** 2026-06-06
**Valid until:** ~30 days (internal Go + stable Podman Quadlet; the only volatility is the kyuz0 image, already digest-pinned).
