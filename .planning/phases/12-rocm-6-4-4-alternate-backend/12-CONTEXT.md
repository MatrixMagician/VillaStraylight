# Phase 12: `rocm-6.4.4` Alternate Backend - Context

**Gathered:** 2026-06-07
**Status:** Ready for planning
**Mode:** `--auto` (decisions auto-resolved to the recommended option for each gray area; review before planning)

<domain>
## Phase Boundary

Add a digest-pinned, token-generation-tuned **`rocm-6.4.4`** ROCm image (and its
bench-decided `-rocwmma` variant) as alternate inference backend(s), selectable
through the proven `BackendFor` seam exactly like the existing `rocm` (7.2.4)
backend — to recover the v1.1 `villa bench --ab` Δtg −11.15 token-generation
regression. Vulkan RADV stays the default and is **never** auto-switched.

**In scope:** new digest-pinned image literal(s) behind the inference seam; a
new `BackendFor` selection key per image; `rocm-policy.json` gating + ROCm
preflight applied to the new backend(s); the seam grep-gate (`seam_test.go`
image regex) extended in the SAME commit; transactional switch + residency proof
+ `bench --ab` working against the new image(s) by reusing the shipped
`backendswap` / `bench` machinery unchanged.

**Out of scope (own phases / future):** ROCm perf-tuning knobs
(hipBLASLt/rocWMMA-FA/batch tunables — `ROCM-TUNE-01`, deferred); any change to
the Vulkan default; per-image `recommend` advice; new outbound traffic.
</domain>

<decisions>
## Implementation Decisions

### Backend identity & selection
- **D-01:** The new image is selected via **distinct `BackendFor` backend
  strings** — `rocm-6.4.4` (and `rocm-6.4.4-rocwmma`) added as new `case`s in
  `internal/inference/backend.go`. This matches ROADMAP Success Criterion #1's
  literal `villa backend set rocm-6.4.4`. NOT a sub-flag/`--image` variant of
  `rocm`.
- **D-02:** **Coexist** with the existing `rocm` backend — `rocm` keeps meaning
  ROCm **7.2.4** (unchanged digest). The new strings are additive. This is
  required by SC#3: `bench --ab` must measure the new image **against**
  rocm-7.2.4 / Vulkan, so 7.2.4 must remain selectable.
- **D-03:** Fail-closed resolver semantics are preserved — an unknown/typo'd
  backend string still returns the actionable `BackendFor` error, never a silent
  fallback. The new cases are explicit additions only.

### rocwmma variant strategy
- **D-04:** **Ship BOTH** `rocm-6.4.4` and `rocm-6.4.4-rocwmma` as selectable,
  digest-pinned, seam-locked, policy-gated backends. The point of the phase is to
  let `bench --ab` *prove* which digest recovers Δtg — that decision is made at
  **runtime by the user's benchmark**, not guessed at plan time. ("ship the one
  the A/B proves" → ship both so the A/B can be run.)
- **D-05:** Both digests are pinned from the ROADMAP research flag and
  **re-verified at implementation time** (the rolling `rocm-6.4.4` tag is
  re-pushed by kyuz0 — pin the digest, not the tag):
  - plain: `sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62`
  - `-rocwmma`: `sha256:9a97129af2c1a2f0080f234787f6978551a43e354f3eb26a8ebc868f643c0141`

### Backend delta reuse
- **D-06:** **Parameterize the proven `backendROCm` delta by image** rather than
  forking a new backend type per digest. The kfd+dri device passthrough,
  `keep-groups` rule (no redundant `--group-add render` — Phase 08 UAT CR-G1),
  seccomp, loopback host-publish, read-only model bind, mandatory llama-server
  flags, and the `ResidencyProof` markers (`ROCm0` / `- ROCm` /
  `ggml_cuda_init:` / "Memory access fault by GPU node") are shared ROCm-family
  behaviour — only the image digest differs. Research must confirm whether
  `rocm-6.4.4` needs a different `HSA_OVERRIDE_GFX_VERSION` or
  `ROCBLAS_USE_HIPBLASLT` setting than 7.2.4 (default assumption: identical
  `11.5.1` + hipBLASLt=1).

### Policy gating & preflight applicability
- **D-07:** Keep `internal/preflight/rocm-policy.json` as a deny-list + floors
  policy (kernel/firmware/mesa floors, `imageDeny`, required HSA override). Pin
  the new digests in the seam; add an explicit allow/deny entry only if research
  shows the rolling tag needs it. A request that fails a floor is refused with
  named remediation — never silently downgraded (SC#2).
- **D-08:** **Generalize the ROCm-preflight predicate.** The live
  `PreflightROCm` closure in `cmd/villa/backend.go` currently gates on
  `cfg.Backend != "rocm"` (exact string). It must fire for **all ROCm-family
  backends** (`rocm`, `rocm-6.4.4`, `rocm-6.4.4-rocwmma`) so the new backends get
  the same refuse-with-remediation gate. Introduce a single "is this a ROCm
  backend" predicate (likely in `internal/inference`) and route the preflight +
  any other `== "rocm"` checks through it. Audit for other literal `"rocm"`
  comparisons that must become family-aware.

### Surfacing & seam gate
- **D-09:** **No new surfacing logic.** `status`/dashboard already display the
  active backend + image tag via `BackendFor(cfg.Backend).Image()`, so the new
  image surfaces automatically. `recommend` advice stays generic (derives from
  `rocm_readiness`; makes no per-image speed promises — the honesty constraint
  from v1.1's Δtg −11.15). Confirm goldens only need an append-only refresh if a
  new enum value appears; otherwise leave frozen contracts untouched.
- **D-10:** **Extend `internal/inference/seam_test.go`'s image regex in the SAME
  commit** that introduces the new image literal(s), so a leaked `rocm-6.4.4`
  literal fails `TestSeamGrepGate` immediately (SC#4). Add `rocm-6\.4\.4` to the
  "container image literal" pattern (and the `cmdPatterns` copy). Keep
  `TestROCmMarkerPresence` green for the parameterized markers.

### Claude's Discretion
- Exact resolver string naming for the rocwmma variant (`rocm-6.4.4-rocwmma`
  assumed; planner may shorten if it keeps the `villa backend set` UX clean and
  the seam regex tight).
- Whether the ROCm-family predicate lives in `internal/inference` (preferred,
  keeps backend knowledge behind the seam) vs a small helper in `config` —
  planner decides, but it must not leak image literals out of the seam.
- Whether the shared ROCm delta is refactored into one image-parameterized
  `backendROCm{image, markers}` struct or kept as thin sibling structs — planner
  picks the lowest-churn option that keeps the seam grep-gate green.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` → "Phase 12: `rocm-6.4.4` Alternate Backend" — Goal, 4
  Success Criteria, and the **Research flag** (digest re-verification + the two
  pinned `sha256` digests; `-rocwmma` is bench-decided).
- `.planning/REQUIREMENTS.md` → **ROCM-ALT-01** (and the Out-of-Scope row
  excluding ROCm perf-tuning knobs / `ROCM-TUNE-01`).
- `.planning/PROJECT.md` → Key Decisions rows: "ROCm is opt-in; Vulkan stays
  default; recommend advises, never auto-switches", "Single polymorphic
  `BackendFor` resolver, fail-closed", "Pin `rocm-7.2.4` stable; never nightlies",
  "`villa backend set` is transactional", "`bench --ab` composes the switch".

### Inference seam (where ALL new literals must live)
- `internal/inference/backend.go` — `BackendFor` resolver (add the new case(s);
  preserve fail-closed default).
- `internal/inference/backend_rocm.go` — the ROCm delta to parameterize by image
  (device args, HSA/hipBLASLt env, `ResidencyProof` markers).
- `internal/inference/backend_vulkan.go` — the shared-delta reference (default
  backend; stays byte-identical).
- `internal/inference/seam_test.go` — `TestSeamGrepGate` image regex (extend SAME
  commit) + `TestROCmMarkerPresence` (positive marker gate).

### Policy & preflight gate
- `internal/preflight/rocm-policy.json` — image deny-list + kernel/firmware/mesa
  floors + required HSA override (the gating policy).
- `internal/preflight/floors.go` — `go:embed` policy loader.
- `internal/preflight/preflight.go` (+ `RunROCm`) — the BLOCK/WARN ROCm gate.
- `cmd/villa/backend.go` — `villa backend set` verb + the `PreflightROCm` /
  `cfg.Backend != "rocm"` closure to generalize (D-08).

### Reused machinery (compose, do NOT re-implement)
- `internal/backendswap/backendswap.go` — transactional capture→prove→cutover→
  rollback (used as-is for the new backend's switch).
- `internal/bench/bench.go` — honest A/B pp/tg core; `--ab` composes
  `backendswap.Run` (used as-is to prove Δtg recovery).
- `CLAUDE.md` → "Inference seam grep-gate" gotcha + "Container Images
  Standardized On" table (add the new image row).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`BackendFor` resolver** (`internal/inference/backend.go`): single
  polymorphism point — new backends slot in as additional `case`s with zero
  caller changes (the 8 call sites already route through it).
- **`backendROCm` delta** (`backend_rocm.go`): the entire kfd+dri / keep-groups /
  HSA / hipBLASLt / `ResidencyProof` machinery is reusable; the new backend is a
  digest swap over it (D-06).
- **`backendswap.Run`** + **`bench --ab`**: transactional switch and honest A/B
  already work for any `Backend`; selecting the new string flows through them
  unchanged — SC#1 and SC#3 are largely satisfied by reuse.
- **`rocm-policy.json` + `RunROCm`**: the refuse-with-remediation gate is reusable
  for SC#2; only the predicate that decides "is this a ROCm target" must widen.

### Established Patterns
- **Seam-lock + grep-gate (same-commit rule):** any new image/device/marker
  literal lives only in `internal/inference` (+ `internal/detect/gpu_amd.go`) and
  the `seam_test.go` regex is extended in the same commit (CLAUDE.md; SC#4).
- **Digest-pin, never tag** (Pitfall 12 / T-6-04): the rolling tag is silently
  rebuilt; pin `@sha256:…` and re-verify at implementation time (D-05).
- **Fail-closed resolver** (D-02 of v1.1): unknown backend string → actionable
  error, never silent Vulkan fallback.
- **Append-only / byte-frozen contracts:** `--json`/dashboard goldens evolve
  append-only with a schema bump; only refresh if a new enum value is added (D-09).
- **Offload-asserting:** the switch/bench gate on a real generation probe +
  residency proof; a silent/partial CPU fallback is a FAIL, never false-green.

### Integration Points
- `internal/inference/backend.go` (resolver case) → drives every install /
  lifecycle / status / bench / dashboard site automatically.
- `cmd/villa/backend.go` `PreflightROCm` closure → generalize the `"rocm"`
  literal to a ROCm-family predicate (D-08).
- `CLAUDE.md` image table + `seam_test.go` regex → updated in lockstep with the
  new literal.
</code_context>

<specifics>
## Specific Ideas

- The whole phase is a **delta over shipped v1.1 machinery** — the lowest-risk
  v1.2 phase by design (research-converged build order placed it first precisely
  because the `BackendFor` / `backendswap` / `bench --ab` seams already exist and
  carry zero/trivial contract risk).
- The user benchmarks plain vs `-rocwmma` vs `rocm-7.2.4` vs Vulkan with
  `villa bench --ab` and keeps whichever recovers Δtg — the product ships the
  *capability to choose*, not a pre-judged winner.
- Strictly-local posture and zero-telemetry are unchanged; the only new outbound
  is the additional image pull (consistent with existing image/model pulls).
</specifics>

<deferred>
## Deferred Ideas

- **ROCm perf-tuning knobs** (hipBLASLt / rocWMMA-FA / batch tunables) —
  `ROCM-TUNE-01`, explicitly deferred beyond v1.2 (REQUIREMENTS.md Future).
  Phase 12 ships the `-rocwmma` *image* as a selectable backend, NOT tunable
  flags.
- **Per-image `recommend` advice** (e.g. "use rocm-6.4.4 for TG-heavy models") —
  `recommend` stays image-agnostic to preserve the honesty constraint; revisit
  only if usage data later justifies it.
- **Retiring rocm-7.2.4** once 6.4.4 proves a universal win — not this phase;
  7.2.4 must stay for the A/B baseline.

</deferred>

---

*Phase: 12-rocm-6-4-4-alternate-backend*
*Context gathered: 2026-06-07*
