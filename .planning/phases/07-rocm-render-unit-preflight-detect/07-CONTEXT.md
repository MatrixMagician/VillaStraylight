# Phase 7: ROCm Render Unit + Preflight/Detect - Context

**Gathered:** 2026-06-06
**Status:** Ready for planning

> Interactive discuss (default mode). All four gray areas were selected and
> decided; the user took the recommended option on every crux and secondary
> choice (logged in `07-DISCUSSION-LOG.md`). Decisions are HOW-to-implement
> refinements of the locked ROADMAP success criteria — no scope added.

<domain>
## Phase Boundary

Deliver the three **off-hardware** pieces the Phase 8 switch verb gates on, all
as pure deltas over the proven v1.0/Phase-6 primitives:

1. **ROCM-03** — render the ROCm `villa-llama.container` Quadlet unit as a pure
   delta over the Vulkan unit (digest-pinned `kyuz0:rocm-7.2.4`, `/dev/kfd` +
   `/dev/dri` passthrough, `render` group, ordered `HSA_OVERRIDE_GFX_VERSION=11.5.1`
   + `ROCBLAS_USE_HIPBLASLT=1` env, `-ngl 999 -fa 1 --no-mmap`), frozen by a **new
   ROCm byte-golden** with the **Vulkan golden byte-for-byte unchanged** (proves
   additivity).
2. **PRE-06** — a reusable **refuse-with-remediation ROCm preflight verdict**
   driven by externalized (`go:embed`-updatable) version ranges + denylist,
   biased not to over-block a genuinely-working host.
3. **DET-04** — `villa detect --json` ROCm-readiness fields appended after the
   existing GPU block, schema-bumped, never reordering the frozen v1.0 contract.

Both preflight and detect are **off-hardware** this phase: pure parse/verdict
logic + table-test fixtures. Live ROCm offload / HSA-override behavior is
exercised on real gfx1151 hardware in Phase 8 (the switch verb).

**Out of scope (later phases):** the on-running-install `villa backend set`
switch verb + transactional rollback + live generation-probe/residency cutover
(Phase 8, on-hardware); `villa bench` A/B (Phase 9); backend/tok-s surfacing in
`status`/dashboard/`recommend` (Phase 10).

</domain>

<decisions>
## Implementation Decisions

### ROCm preflight shape (PRE-06)
- **D-01:** **Extend the existing preflight pipeline.** Add the ROCm checks as
  `TierBlock` `CheckResult`s exposed via a new pure entrypoint (e.g.
  `RunROCm(profile)` / `RunROCmWithPolicy`) in `internal/preflight` — reusing the
  existing `CheckResult` / `Tier` (BLOCK=exit1, WARN=exit2) / `Status`
  vocabulary, the `pass`/`warn`/`fail` constructors, exit-code mapping, and
  remediation field. Do **not** build a separate ROCm verdict type. Phase 8's
  switch verb calls `RunROCm` and refuses on any `StatusFail`.
- **D-02:** **Confident known-bad → FAIL (refuse); unevaluable signal → WARN.**
  Only a positively-detected bad state refuses: firmware **exactly** `20251125`,
  a `rocm7-nightlies` image request, kernel `< 6.18.4`, missing HSA override, or
  `rocminfo` present but **not** gfx1151. A missing/unparseable BLOCK-tier signal
  (e.g. firmware date unreadable, `rocminfo` absent off-hardware) **degrades to
  `StatusWarn`**, never FAIL — the existing D-15 typed-Unknown discipline,
  honoring PRE-06's "biased not to over-block a genuinely-working host."
- **D-03:** **Surface a CLI path this phase.** Expose the verdict off-hardware via
  `villa preflight --backend rocm` (renders the ROCm `CheckResult` table) **and**
  as the reusable function the Phase-8 switch gate consumes — so it is testable
  and demoable now without waiting for the switch verb. The standalone host
  preflight (`Run`) stays WARN-only and unchanged.

### Version policy externalization (PRE-06)
- **D-04:** **Embedded JSON policy file.** Add an `internal/preflight` policy file
  (e.g. `rocm-policy.json`) loaded via `go:embed` into the `Floor`/policy struct,
  carrying: kernel-floor range (≥6.18.4), firmware floor (≥20260110) + firmware
  **denylist** (`20251125`), image **denylist** (`rocm7-nightlies`), and the
  required `HSA_OVERRIDE_GFX_VERSION` value. Matches the SC "go:embed-updatable
  ranges + denylist" wording; a floor/denylist entry is correctable without
  reshaping check logic.
- **D-05:** **Migrate the existing Vulkan/host floors into the same embedded
  policy** (currently the Go constants in `internal/preflight/floors.go`:
  `KernelFloor`, `KernelTested`, `FirmwareFloor`, `FirmwareDeny`, `MesaFloor`) so
  there is one authoritative externalized source. The migration must keep the
  current v1.0 preflight check outputs/goldens byte-identical (constants → loaded
  data is a behavior no-op for the existing checks).

### detect --json readiness shape (DET-04)
- **D-06:** **Nested `rocm_readiness` object**, appended to `HostProfile` **after**
  the existing GPU block — a single nested object (e.g. `hsa_override_viable`,
  `firmware_date_ok`, `kernel_floor_ok`, `rocminfo_gfx1151`, `image_policy_ok`).
  One clean append point with room for future ROCm fields; the dashboard reads one
  sub-tree. Top-level `ROCmPresent` stays where it is (already in the v1.0
  contract).
- **D-07:** **Bump `hostProfileSchemaVersion` 1 → 2**, additive only: existing
  fields keep their names, types, and order; the new object is strictly appended.
  The frozen v1.0 `--json` golden gains the nested object (re-frozen once); no
  existing field is renamed or reordered.
- **D-08:** **Each readiness field is a typed-Optional** (the `value.go`
  `Bool`/`Str` types like the rest of `HostProfile`) so "couldn't detect"
  serializes as null/absent — distinct from a real `false`. Honors the D-16
  detect contract and lets `recommend`/dashboard tell unknown from known-bad. A
  probe that can't run off-hardware (e.g. `rocminfo` not installed) yields the
  Optional's unset value, not `false`.

### Multi-device unit rendering (ROCM-03)
- **D-09:** **`AddDevice` becomes a `[]string`; the template ranges over it.**
  `parseContainerArgs` (`internal/orchestrate/render.go`) collects **all**
  `--device` tokens (not last-wins) into `containerView.AddDevice []string`, and
  `quadlet/container.tmpl` emits one `AddDevice=` line per element. A 1-element
  slice (Vulkan, `/dev/dri` only) must render the **identical single line** —
  proven by the **Vulkan golden staying byte-for-byte unchanged**. ROCm renders
  two lines (`/dev/kfd` then `/dev/dri`, in `ContainerArgs` order). Update the
  defensive all-fields-non-empty check to assert `len(AddDevice) > 0`.

### Claude's Discretion
- Exact entrypoint/struct names (`RunROCm` vs `RunROCmWithPolicy`, the policy
  loader/struct shape, the `rocm_readiness` Go type name) — planner/executor's
  call, provided D-01..D-09 hold.
- Exact JSON key spellings inside `rocm_readiness` and the embedded
  `rocm-policy.json` schema, provided fields are typed-Optional (D-08) and the
  policy carries the ranges + both denylists (D-04).
- Whether the ROCm device order in the rendered unit is `kfd,dri` or `dri,kfd` —
  must match `backend_rocm.go` `ContainerArgs` order exactly (the renderer reads
  from the seam, never re-orders), and is frozen by the new ROCm golden.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 7: ROCm Render Unit + Preflight/Detect" — goal,
  the 3 locked success criteria (ROCm byte-golden + Vulkan golden unchanged;
  refuse-with-remediation preflight from externalized ranges/denylist; detect
  `--json` readiness fields appended + schema-bumped), and the spine-first
  ordering rationale (lines 29, 63–74).
- `.planning/REQUIREMENTS.md` — ROCM-03 (ROCm Quadlet render delta + byte-golden),
  PRE-06 (reusable ROCm preflight verdict, externalized policy, refuse-with-
  remediation, don't over-block), DET-04 (detect ROCm readiness, schema-bumped,
  append-only).
- `.planning/PROJECT.md` — milestone goal, key decisions (ROCm opt-in, pin
  `rocm-7.2.4`, Vulkan stays default, refuse-with-remediation over silent degrade).

### Prior phase (the spine this builds on)
- `.planning/phases/06-rocm-backend-resolver-spine/06-CONTEXT.md` — D-08/D-09:
  `backend_rocm.go` image constant + `ContainerArgs` (device/group/env deltas)
  that the renderer consumes; the seam discipline this phase must not violate.
- `.planning/phases/06-rocm-backend-resolver-spine/06-VERIFICATION.md` — what
  Phase 6 already proved (resolver, residency, grep-gates) so Phase 7 doesn't
  re-litigate it.

### Stack / ROCm specifics
- `CLAUDE.md` §"Technology Stack" / §"What NOT to Use" / §"Version Compatibility"
  — ROCm image, `HSA_OVERRIDE_GFX_VERSION=11.5.1`, `ROCBLAS_USE_HIPBLASLT=1`,
  `--no-mmap -fa 1 -ngl 999`, nightlies 64 GB cap, kernel ≥6.18.4,
  avoid `linux-firmware-20251125` (≥20260110 good).
- `.planning/research/STACK.md`, `.planning/research/PITFALLS.md`,
  `.planning/research/ARCHITECTURE.md`, `.planning/research/SUMMARY.md` — v1.1 ROCm
  research (image digest pinning, gfx1151 sharp edges, kfd/SELinux notes).

### Code to mirror / extend
- `internal/orchestrate/render.go` — the **pure renderer**: `Render`,
  `parseContainerArgs` (the single-device → `[]string` change, D-09), the
  defensive all-fields-non-empty check; consumes the backend seam only.
- `internal/orchestrate/quadlet/container.tmpl` — the container template that must
  range over `AddDevice` (D-09).
- `internal/orchestrate/render_test.go` (`TestRenderContainerGolden`) +
  `internal/orchestrate/testdata/villa-llama.container.golden` — the Vulkan golden
  to keep byte-identical; pattern for the new ROCm golden.
- `internal/preflight/preflight.go` — `Tier`/`Status`/`CheckResult`,
  `pass`/`warn`/`fail`, `Run`/`RunWithResources` (the pipeline to extend, D-01).
- `internal/preflight/checks_gpu.go` — `checkVulkanIGPU`/`checkKernelFloor`/
  `checkFirmwareFloor` (the check style + provenance/remediation to mirror).
- `internal/preflight/floors.go` — the version-floor constants to migrate into the
  embedded JSON policy (D-04/D-05).
- `internal/detect/profile.go` — `HostProfile` + `hostProfileSchemaVersion` (the
  frozen `--json` contract to append to + bump, D-06/D-07).
- `internal/detect/value.go` — the `Str`/`Bool`/`Bytes`/`Int` typed-Optional
  primitives the `rocm_readiness` fields use (D-08).
- `internal/inference/backend_rocm.go` — authoritative source of the ROCm image +
  `ContainerArgs` device/group/env order the renderer reads (must not re-type).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`render.go` / `parseContainerArgs`** — already maps every imperative field out
  of the seam's `ContainerArgs` slice; the only change is single `--device` →
  collect-all (`[]string`) + template range. The Vulkan path renders unchanged.
- **Preflight pipeline** — `CheckResult`/`Tier`/`Status` already model BLOCK
  (refuse, exit 1) vs WARN (exit 2) with a typed-Unknown→WARN downgrade; ROCm
  checks slot in as `TierBlock` results with no new verdict machinery.
- **`floors.go`** — already holds `KernelFloor`/`FirmwareFloor`/`FirmwareDeny` as
  data with a written intent to lift into embedded JSON; D-04/D-05 realize it.
- **`HostProfile` + `value.go` Optionals** — `ROCmPresent`, `IGPUGfxID`,
  `KernelVersion` already exist; `rocm_readiness` is a typed-Optional append.

### Established Patterns
- **Pure-library + cmd-layer I/O split** — `internal/preflight`,
  `internal/orchestrate`, `internal/detect` never do `os.Exit`/printing; the
  ROCm verdict, render, and readiness logic stay pure and fixture-tested.
  Off-hardware this phase = parse + verdict only.
- **Byte-golden additivity** — a new backend's unit is frozen by a NEW golden
  while the existing golden stays byte-identical (proves the change is a pure
  delta). Same discipline as Phase 6's grep-gates.
- **Typed-Unknown / no-false-green** — unevaluable signal = WARN/Optional-unset,
  distinct from a confident bad (FAIL/known-false). Applies to both preflight
  (D-02) and detect (D-08).
- **Externalized version data** — floors live as data, not inlined into checks, so
  they can be corrected/loaded without reshaping logic (D-04).

### Integration Points
- `orchestrate.RenderInput.Backend` already feeds the resolved backend into
  `Render`; rendering the ROCm unit needs only the `BackendFor("rocm")` path +
  the multi-device renderer change.
- The Phase-8 switch verb will call the `RunROCm` preflight verdict (D-01/D-03)
  and rely on the ROCm golden render — both produced here.
- `villa detect --json` consumers (dashboard, Phase-10 `recommend`) read the new
  `rocm_readiness` object; schema bump (D-07) lets them detect a mismatch.

</code_context>

<specifics>
## Specific Ideas

- ROCm device line shape to fixture/golden against: two `AddDevice=` lines,
  `/dev/kfd` then `/dev/dri` (matching `backend_rocm.go` `ContainerArgs` order),
  plus `GroupAdd=render` and the ordered `HSA_OVERRIDE_GFX_VERSION=11.5.1` /
  `ROCBLAS_USE_HIPBLASLT=1` env in the rendered `[Container]` section.
- Mirror the Vulkan golden's structure for the new ROCm golden so a reviewer can
  diff the two units and see exactly the device/group/env/image delta — the unit
  IS the documentation of the backend difference.
- Preflight remediation strings should name the exact fix (e.g. "kernel 6.18.x <
  floor 6.18.4 — update kernel", "linux-firmware-20251125 is denied — install
  ≥20260110", "rocm7-nightlies caps allocation at 64 GB — use rocm-7.2.4"),
  matching the existing `checks_gpu.go` provenance/remediation tone.

</specifics>

<deferred>
## Deferred Ideas

- **`villa backend set rocm|vulkan` switch verb + transactional rollback + live
  generation-probe/residency cutover** — Phase 8 (BSET-01/02/03, on-hardware).
  Phase 7's preflight verdict + ROCm render are its inputs.
- **Live ROCm offload / HSA-override / `load_tensors`-hang behavior on real
  gfx1151** — exercised in Phase 8; Phase 7 proves the off-hardware logic only.
- **`villa bench` honest A/B (separate pp/tg tok/s)** — Phase 9 (BENCH-01/02).
- **Backend-aware `recommend` advice + dashboard/`status` active-backend + live
  tok/s + ROCm-readiness indicator** — Phase 10 (REC-05, DASH-06); consumes the
  `rocm_readiness` object frozen here.
- **`checkMesaFloor` wiring** (the `floors.go` `MesaFloor` TODO) — pre-existing
  v1.0 deferral, unrelated to ROCm; leave unwired (don't risk a cross-namespace
  version compare). Migrate the constant into the policy file (D-05) but keep it
  unconsumed.

None of these were raised as new scope — discussion stayed within the phase
boundary.

</deferred>

---

*Phase: 7-rocm-render-unit-preflight-detect*
*Context gathered: 2026-06-06*
