---
phase: 12-rocm-6-4-4-alternate-backend
plan: 02
subsystem: inference-routing
tags: [rocm, family-predicate, preflight-gate, backend-label, image-policy, seam-grep-gate]
requires:
  - "inference.IsROCmFamily (12-01 single ROCm-name enumeration)"
  - "inference.BackendFor(\"rocm-6.4.4\"|\"rocm-6.4.4-rocwmma\") (12-01 resolver cases)"
  - "preflight.RunROCmWithPolicy (existing image-parameterized gate)"
provides:
  - "preflight.RunROCmForImage(p, image) — threads the resolved target digest into the policy gate (SC#2)"
  - "Family-aware PreflightROCm closure + preflight --backend router (every ROCm name gated identically, D-08)"
  - "orchestrate.backendLabel honest ROCm-family Description labels (Pitfall 2)"
  - "detect.rocm644ImageTag + widened rocmImagePolicyOK (honest 6.4.4 readiness, Pitfall 3)"
affects:
  - "12-03 (bench --ab against the new backends; the routing/labels/readiness it relies on are now family-aware)"
tech-stack:
  added: []
  patterns:
    - "Anti-literal routing: every == \"rocm\" / != \"rocm\" comparison asks IsROCmFamily (D-08)"
    - "Thread the resolved target image into the policy gate so the deny-list evaluates the real digest (SC#2)"
    - "Image-CONTEXT anchored seam regex: matches IMAGE literals (:tag / tag@), not bare config-VALUE names"
    - "Additive golden (new fixture), never a refreeze of the byte-frozen Vulkan/7.2.4 goldens (D-09)"
key-files:
  created:
    - "internal/orchestrate/testdata/villa-llama-rocm-6.4.4.container.golden (additive)"
  modified:
    - "internal/preflight/checks_rocm.go (RunROCmForImage wrapper)"
    - "internal/preflight/checks_rocm_test.go (evaluate/deny tests)"
    - "cmd/villa/backend.go (family-aware PreflightROCm threading b.Image())"
    - "cmd/villa/backend_test.go (live family-aware closure test)"
    - "cmd/villa/preflight.go (IsROCmFamily router + widened --backend help)"
    - "internal/orchestrate/render.go (backendLabel 6.4.4 arms)"
    - "internal/orchestrate/render_test.go (backendLabel + additive 6.4.4 golden tests)"
    - "internal/detect/gpu_amd.go (rocm644ImageTag + widened rocmImagePolicyOK)"
    - "internal/detect/gpu_amd_test.go (rocmImagePolicyOK 6.4.4 cases)"
    - "internal/inference/seam_test.go (image-context-anchored ROCm tag regex)"
decisions:
  - "Anchored the seam regex ROCm-tag alternatives to image context (:tag / tag@) so the gate catches real image literals but not the legitimate bare config-value names the plan required in render.go/preflight.go; kyuz0|docker.io/ remain un-anchored backstops"
  - "Tested the LIVE PreflightROCm closure via liveBackendSwapDeps() (off-hardware Probe yields Unknowns → WARN/PASS, never false FAIL) rather than refactoring the closure out — no architectural change"
  - "Added the optional additive 6.4.4 render golden (proves the honest ROCm label at the render level); left resolvedROCmImage() untouched so detect.golden.json stays byte-frozen (D-09)"
metrics:
  duration: "~20 min"
  completed: "2026-06-07"
  tasks: 2
  files_changed: 11
---

# Phase 12 Plan 02: ROCm-Family Routing + Meaningful Policy Gate Summary

Generalized every literal `"rocm"` comparison to `inference.IsROCmFamily` (D-08),
threaded the resolved target image into the preflight policy gate so it evaluates the
ACTUAL 6.4.4 digest against `imageDeny` instead of WARNing on an empty string (SC#2),
and widened `backendLabel` + detect `rocmImagePolicyOK` so a 6.4.4 unit renders an
honest ROCm Description and a 6.4.4 host reports honest readiness — all green
off-hardware via `make check`, with the byte-frozen Vulkan/7.2.4 goldens untouched.

## What Was Built

- **`preflight.RunROCmForImage(p, image)`** (`checks_rocm.go`): a thin sibling of
  `RunROCm` that calls `RunROCmWithPolicy(p, loadROCmPolicy(), image)` — passing the
  RESOLVED target image so `checkROCmImage` evaluates the real digest against
  `imageDeny` (no empty-image WARN bypass). `RunROCm` itself is unchanged (the
  standalone host-prep path keeps the empty-image WARN). No allow-list added —
  `imageDeny` is a substring denylist and `rocm-6.4.4` is not denied (D-07).
- **Family-aware `PreflightROCm`** (`cmd/villa/backend.go`): replaced
  `cfg.Backend != "rocm"` with `!inference.IsROCmFamily(cfg.Backend)`, then resolves
  `inference.BackendFor(cfg.Backend)` (fail-closed → `(false, err)`) and iterates
  `preflight.RunROCmForImage(detect.Probe(), b.Image())`, refusing on the first
  `StatusFail` with that check's `Detail` (SC#2).
- **Family-aware preflight router** (`cmd/villa/preflight.go`): replaced
  `if backend == "rocm"` with `if inference.IsROCmFamily(backend)` (added the
  `inference` import), so `villa preflight --backend rocm-6.4.4` (and `-rocwmma`)
  routes to the ROCm gate; widened the `--backend` flag help + Long text + comments
  to name the new options.
- **Honest `backendLabel`** (`orchestrate/render.go`): added `case "rocm-6.4.4"` →
  "ROCm 6.4.4 (HIP)" and `case "rocm-6.4.4-rocwmma"` → "ROCm 6.4.4 rocWMMA (HIP)";
  `case "rocm"` (7.2.4) and the Vulkan `default` are byte-unchanged. A 6.4.4 unit no
  longer falls through to the misleading "Vulkan RADV" default (Pitfall 2).
- **Honest 6.4.4 readiness** (`detect/gpu_amd.go`): added `const rocm644ImageTag =
  "rocm-6.4.4"` (the ONE allowed non-inference seam file) and widened the
  `rocmImagePolicyOK` stable arm to `strings.Contains(image, rocmStableImageTag) ||
  strings.Contains(image, rocm644ImageTag)` (the substring also covers `-rocwmma`).
  The nightly-deny arm and the Unknown default are unchanged; `resolvedROCmImage()` is
  untouched (keeps `detect.golden.json` byte-frozen, D-09).
- **Additive golden** (`testdata/villa-llama-rocm-6.4.4.container.golden`): a NEW
  fixture rendered through the resolver, asserting the honest "ROCm 6.4.4 (HIP)"
  Description + the 6.4.4 digest + the identical kfd/dri/keep-groups/HSA/hipBLASLt
  delta. The Vulkan + rocm-7.2.4 goldens stay byte-unchanged.
- **Tests**: `RunROCmForImage` evaluate(PASS)/deny(FAIL) + match-RunROCmWithPolicy;
  a LIVE `PreflightROCm` family-aware closure test (short-circuit vulkan, gate all
  three ROCm names off-hardware without a false FAIL); `backendLabel` all-arm table;
  `rocmImagePolicyOK` 6.4.4 / -rocwmma / nightly / unrecognized cases; the additive
  6.4.4 render golden.

## Tasks Completed

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Family-predicate routing + thread resolved image into the preflight gate | 1de5c3d | checks_rocm.go, checks_rocm_test.go, cmd/villa/backend.go, cmd/villa/backend_test.go, cmd/villa/preflight.go |
| 2 | Widen backendLabel + detect rocmImagePolicyOK; additive 6.4.4 golden | 196c557 | render.go, render_test.go, gpu_amd.go, gpu_amd_test.go, seam_test.go, villa-llama-rocm-6.4.4.container.golden |

## Verification Results

- `go test ./internal/preflight/ ./cmd/villa/ -run 'RunROCm|Preflight|Backend' -count=1` → 37 pass, exit 0.
- `go test ./internal/orchestrate/ ./internal/detect/ -count=1` → 103 pass, exit 0.
- `go test ./internal/inference/ -run 'TestSeamGrepGate|TestROCmMarkerPresence' -count=1` → pass, exit 0.
- `make check` (`go vet ./...` + `go test ./...`) → all packages OK, exit 0 (the predicate refactor broke no caller).
- No remaining literal comparison: `grep -rn 'Backend == "rocm"\|Backend != "rocm"\|backend == "rocm"' cmd/villa internal | grep -v '_test.go' | grep -v '//'` → no matches.
- Byte-frozen goldens: `git diff --stat` over `villa-llama.container.golden`, `villa-llama-rocm.container.golden`, `detect.golden.json` → empty (unchanged).
- `RunROCmForImage(probe, <rocm-6.4.4 digest>)` → image check PASS (digest evaluated, not a "no image requested" WARN); `RunROCmForImage(probe, <rocm7-nightlies>)` → image check FAIL (denied).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Seam-gate regex flagged legitimate bare config-value names**
- **Found during:** Task 2 (running `TestSeamGrepGate` after widening `backendLabel`).
- **Issue:** The plan's two hard constraints conflicted in practice. The 12-01 seam
  regex `rocm-6\.4\.4` was intended to catch the image TAG, but it is a plain substring
  and also matched the legitimate bare backend-NAME config values the plan REQUIRED in
  non-seam files: `case "rocm-6.4.4":` / `case "rocm-6.4.4-rocwmma":` in
  `orchestrate/render.go` and the `rocm-6.4.4` mentions in `cmd/villa/preflight.go`'s
  `--backend` help text + comments. The critical constraint correctly noted the label
  strings ("ROCm 6.4.4 (HIP)", capital R + space) do not match, but did not anticipate
  the lowercase `case` arms / help text. `TestSeamGrepGate` failed on render.go + preflight.go.
- **Fix:** Anchored the seam regex's ROCm-tag alternatives to IMAGE context — match
  `:rocm-6.4.4` / `rocm-6.4.4@` (a `:` tag-separator prefix or an `@sha256` digest
  suffix) instead of the bare tag. A real container image literal
  (`docker.io/kyuz0/…:rocm-6.4.4@sha256:…`) still matches; a bare config-value NAME does
  not. The `kyuz0|docker.io/` alternatives remain UN-anchored backstops, so any real
  image string (always `docker.io/kyuz0/…`) still trips the gate regardless of tag — the
  gate's actual protection (no image LITERAL leaks the seam) is fully preserved. This is
  the same seam-clean class distinction `IsROCmFamily` already relies on (a config-VALUE
  name is not an image/device imperative — cf. bench.go's `vulkan`/`rocm` consts).
- **Files modified:** `internal/inference/seam_test.go` (committed in Task 2).
- **Commit:** 196c557.

### Plan-vs-reality note (not a behavior deviation)

- The plan's Task-1 cmd behavior test described asserting the `PreflightROCm` closure
  returns `(true,"")` for vulkan and runs the gate for rocm-6.4.4. The live closure is
  built inside `liveBackendSwapDeps()` (it calls `detect.Probe()`), not exposed as a
  standalone symbol. Rather than refactor it out (Rule 4 / architectural — avoided), the
  test calls `liveBackendSwapDeps()` and exercises the real closure: off-hardware
  `detect.Probe()` returns typed-Unknowns, so every ROCm signal degrades to WARN/PASS
  (never a false StatusFail) — the test asserts the vulkan short-circuit and that all
  three ROCm-family names route through the gate (resolving a real digest) without a
  false FAIL. The fake-Deps `PreflightROCm` in the existing recorder tests is left as-is
  (it tests the cobra exit-mapping, not the live gate).

## TDD Gate Compliance

Both tasks were `tdd="true"`. RED→GREEN was followed for each:
- **Task 1:** RED — added `TestRunROCmForImage*` → `undefined: RunROCmForImage` build
  fail; GREEN — added the wrapper + rewired the two cmd call-sites → 37 targeted tests pass.
- **Task 2:** RED — added `TestBackendLabelROCmFamily` (rocm-6.4.4 → "Vulkan RADV") +
  `TestRocmImagePolicyOK644` (rocm-6.4.4 → Known=false) failing; GREEN — widened both
  switches → all pass. The additive golden was generated via `-update` after the label
  arms landed (additive, reviewed; the frozen goldens stayed byte-unchanged).
No separate REFACTOR step was needed.

## Authentication Gates

None.

## Known Stubs

None. Every ROCm-family backend resolves to its pinned digest, runs the same
refuse-with-remediation gate against its real image, renders an honest label, and
reports honest readiness. No placeholder/empty values.

## Threat Flags

None. No new network endpoints, auth paths, or trust-boundary surface beyond the
already-modeled set. T-12-04 (preflight skipped for a new ROCm name) and T-12-05
(denied image passing unevaluated) are now MITIGATED by `IsROCmFamily` routing +
`RunROCmForImage` threading the real digest; T-12-06 (misleading label/readiness)
mitigated by the widened `backendLabel`/`rocmImagePolicyOK`. The seam-regex anchoring
narrows a CI false-positive without weakening the no-image-literal-leak guarantee.

## Self-Check: PASSED

- FOUND: internal/preflight/checks_rocm.go (RunROCmForImage)
- FOUND: cmd/villa/backend.go (inference.IsROCmFamily + RunROCmForImage with b.Image())
- FOUND: cmd/villa/preflight.go (inference.IsROCmFamily router)
- FOUND: internal/orchestrate/render.go (ROCm 6.4.4 arms)
- FOUND: internal/detect/gpu_amd.go (rocm644ImageTag + widened rocmImagePolicyOK)
- FOUND: internal/orchestrate/testdata/villa-llama-rocm-6.4.4.container.golden
- FOUND commit: 1de5c3d (Task 1)
- FOUND commit: 196c557 (Task 2)
