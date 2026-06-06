# Phase 10: Backend + tok/s Surfacing - Context

**Gathered:** 2026-06-06
**Status:** Ready for planning

> `--auto` discuss. All six gray areas were auto-selected and decided with the
> recommended option on every choice (logged in `10-DISCUSSION-LOG.md`). Every
> decision is a HOW-to-implement refinement of the locked ROADMAP Phase-10
> success criteria — no scope added. This is the **last** v1.1 phase: the
> byte-frozen `--json` goldens re-freeze exactly once here.

<domain>
## Phase Boundary

Make the three first-party read surfaces **backend-aware** as a single,
append-only contract change — the milestone's surfacing capstone (REC-05,
DASH-06):

1. **`villa status` + dashboard `/api/status`** (shared `internal/status`
   read-model) — show the **active backend (with image tag)**, **live
   token-generation tok/s labeled by backend**, and a **ROCm-readiness
   indicator**. Includes the correctness fix: the offload/residency verdict must
   reflect the *configured* backend (assert `ROCm0` on a ROCm install, not a
   hardcoded `Vulkan0`).
2. **Control dashboard UI** (`internal/dashboard/assets/dashboard.{html,css,js}`,
   vanilla JS, no framework) — render the active backend + image, label the
   existing Performance-panel tok/s by backend, and show the ROCm-readiness
   indicator. (`UI hint: yes`.)
3. **`villa recommend`** (`internal/recommend`) — honest ROCm advice
   ("ready / worth trying / verify with bench") for the pick, **Vulkan stays the
   recommended default**, **never** promising a guaranteed speed-up and **never**
   auto-switching.

All additions are **append-only** with re-frozen goldens reviewed as pure-addition
diffs — no existing field reordered or retagged.

**Out of scope (explicitly):** any new switching/bench logic (Phase 8/9 own it —
this phase only *reads* and *composes*); `villa bench --compare` + saved report
artifact (BENCH-03, v1.1.x backlog); the `rocm-6.4.4` alternate image
(ROCM-ALT-01, v1.1.x backlog); cumulative usage tracking (USAGE-01); a custom
chat UI (out of scope project-wide).

</domain>

<decisions>
## Implementation Decisions

### Active-backend single source of truth (DASH-06)
- **D-01:** Append the active backend **name + image tag** to the shared
  `internal/status.Report` as the **single authoritative active-backend surface**.
  Both `villa status` (human table + `--json`) and the dashboard `/api/status`
  read it — no second source. Mirror the existing `cmd/villa/backend.go`
  `backendShowEntry{Backend, Image}` shape and source the values from the
  **resolved** `inference.Backend.Name()` + `inference.Backend.Image()` (via
  `BackendFor(cfg.Backend)`), never a literal. The dashboard Performance panel
  labels its tok/s row using the backend from the **status poll** — do NOT
  duplicate backend identity into the `/api/metrics` `metricsView` (keep tok/s in
  metrics, identity in status; the UI composes them).

### Status offload/residency correctness (DASH-06 SC#1)
- **D-02:** The status read-model's offload verdict must key on the **resolved
  backend's** `ResidencyProof()` markers, so a ROCm install asserts `ROCm0` +
  ROCm fault strings rather than a hardcoded `Vulkan0`. Phase 6 already re-routed
  the inference *endpoint* through `BackendFor(cfg.Backend)` (no
  `VulkanBackend()` call sites remain outside its own definition); **research must
  pin the exact site** where the residency markers feed `status.Deps` and confirm
  they come from the resolved backend, not a Vulkan default. This is the
  "correctness fix on a ROCm install" SC, not new behavior on Vulkan (the Vulkan
  path stays byte-identical).

### Live tok/s in `villa status` (DASH-06 SC#1)
- **D-03:** `villa status` gains live token-generation tok/s by reusing the SAME
  collector the dashboard uses — `internal/metrics.ScrapeMetrics(endpoint)` /
  `PerfSnapshot` / `IsGenerating` (already public, already dashboard-proven). Add
  a **typed-optional** tok/s figure to `status.Report` populated in
  `liveStatusDeps` (new `Deps` seam member so `status_test.go` stubs it). Honor
  the established honesty discipline: **never a fabricated 0** — an idle snapshot
  or a failed/absent `/metrics` scrape serializes as unset/omitted (typed-Unknown),
  mirroring the dashboard `metricsView` `Available`/`Idle` semantics (Pitfall 3 /
  D-11). The figure is labeled by the active backend (D-01).

### ROCm-readiness indicator (DASH-06)
- **D-04:** Surface a **tri-state** readiness indicator (`ready` /
  `not-ready` / `unknown`) **folded from the existing detect `rocm_readiness`
  sub-tree** (Phase 7 D-06: `hsa_override_viable`, `firmware_date_ok`,
  `kernel_floor_ok`, `rocminfo_gfx1151`, `image_policy_ok`). **Consume, never
  recompute** — read the typed-Optional fields and fold worst-wins. Honor
  no-false-green (Phase 7 D-08): any unevaluable signal → `unknown`, never a
  fabricated `not-ready`. Off-hardware most fields are unset, so the honest
  indicator is `unknown` there. Appended to `status.Report`; rendered as a
  Health-panel row (CLI) + a badge (dashboard).

### `recommend` ROCm advice (REC-05)
- **D-05:** Add a typed **`ROCmAdvice`** field (string enum:
  `ready` / `worth-trying` / `verify-with-bench`; unset/empty when not
  applicable) to `recommend.Recommendation`, plus a human-readable **Note**.
  Derived **purely** from `HostProfile.rocm_readiness` inside the pure `Pick`
  function (no new I/O — `Pick` already takes the `HostProfile`). The recommended
  `Backend` **stays `vulkan`** (REC-04 unchanged); advice **never** changes the
  pick and **never** auto-switches. **Honesty constraint:** the ROCm win is
  prompt-processing-weighted while token-gen is ~flat (may regress vs 6.4.4) —
  the advice MUST say "verify with `villa bench`" and MUST NOT promise a
  guaranteed speed-up. Advice mapping (recommended; research to finalize the
  exact thresholds): all readiness signals Known-good → `worth-trying` (+ "verify
  with bench"); any Known-bad signal → not advised, name the blocker in the Note;
  any unknown → `verify-with-bench`.

### Schema-bump + golden re-freeze discipline (DASH-06 SC#3 / REC-05)
- **D-06:** All new fields are **strictly appended at the tail** of their structs;
  no existing field is renamed, retyped, reordered, or retagged. Each affected
  `--json` golden is **re-frozen exactly once** as a reviewed pure-addition diff:
  `cmd/villa/testdata/status.json.golden` and
  `cmd/villa/testdata/recommend.golden.json`. **`detect` gains no new fields** this
  phase (it is *consumed*, not extended), so `hostProfileSchemaVersion` stays at
  the Phase-7 value (2) and `detect.golden.json` stays byte-identical.
- **D-07:** To satisfy SC#3's "bumped schema version" literally for the two
  contracts that *do* grow: add an explicit **additive `schema_version` integer**
  to `status.Report` and `recommend.Recommendation` (the first version that
  includes the new fields). This is itself a tail-appended additive field.
  *Alternative for research to weigh:* if a `schema_version` on a
  previously-unversioned contract is judged a heavier change than the golden
  additive-diff guard already provides, fall back to "golden re-freeze is the
  contract guard, no version field" — but the default is the explicit additive
  version, since the SC names a version bump.

### Claude's Discretion
- Exact Go field names / json key spellings for the new `status.Report` additions
  (backend, image, tok/s, rocm-readiness indicator) and the `Recommendation`
  `ROCmAdvice` enum spelling — planner/executor's call, provided D-01..D-07 hold
  (typed-optional for tok/s, tri-state for readiness, append-only, honest-unknown).
- The precise dashboard CSS/markup for the backend label, image tag, and
  readiness badge — match the existing panel/metric-row idiom in
  `dashboard.{css,js}`; no new framework (D-01 of Phase 5 stands).
- Whether the CLI human table shows the image tag inline or only under `-v`
  (verbose) — presentation detail, provided `--json` carries it unconditionally.
- The exact worst-wins fold order for the tri-state readiness indicator, provided
  unknown never masquerades as not-ready (D-04).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 10: Backend + tok/s Surfacing" — goal + the 3
  locked success criteria (active backend+image+tok/s+readiness; honest recommend
  advice, Vulkan default, no auto-switch; append-only fields + bumped schema
  version + goldens re-frozen as pure-addition diffs) and the spine-first ordering
  rationale ("surfaces last so goldens re-freeze once").
- `.planning/REQUIREMENTS.md` — REC-05 + DASH-06 (traceability rows; the ROADMAP
  Phase-10 success criteria are the authoritative spec text). Also the v1.1.x
  backlog that bounds scope: BENCH-03, ROCM-ALT-01, USAGE-01.
- `.planning/PROJECT.md` — milestone goal + key decisions (ROCm opt-in, Vulkan
  stays default, refuse/advise over silent auto-switch, strictly-local/no-telemetry).

### Prior phases this composes (do NOT re-litigate)
- `.planning/phases/07-rocm-render-unit-preflight-detect/07-CONTEXT.md` — D-06/D-07/
  D-08: the `rocm_readiness` nested typed-Optional object + `hostProfileSchemaVersion`
  1→2 + no-false-green discipline this phase **consumes** for the readiness indicator
  (D-04) and recommend advice (D-05).
- `.planning/phases/08-*/` (BSET-01/02/03) — `villa backend show` /
  `backendShowEntry{Backend,Image}` shape to mirror (D-01); the switch verb the
  advice points users to (never invoked from recommend/status).
- `.planning/phases/09-*/` (BENCH-01/02) — `villa bench` is what the recommend
  advice tells users to run to verify ("verify with bench", D-05); the honest
  pp-vs-tg framing that forbids promising a speed-up.

### Stack / honesty constraints
- `CLAUDE.md` §"Recommended Models", §"What NOT to Use" — Vulkan RADV is the
  default; ROCm opt-in; never auto-select correctness-flagged paths; the pp-weighted
  / tg-flat reality behind the "no guaranteed speed-up" advice rule (D-05).
- `.planning/research/SUMMARY.md`, `.planning/research/PITFALLS.md` — v1.1 ROCm
  pp-vs-tg volatility and the typed-Unknown / no-false-green honesty pattern.

### Code to extend / mirror
- `internal/status/status.go` — `Report` struct (the frozen `--json` contract to
  append to, D-01/D-03/D-04/D-07), `Deps`, `Run`, `Aggregate`, the worst-wins fold.
- `cmd/villa/status.go` — `runStatus` (renderer + exit map), `renderStatusTable`
  (add backend/image/tok-s/readiness rows), `liveStatusDeps` (already resolves
  `BackendFor(cfg.Backend)`; add the `metrics.ScrapeMetrics` wiring for tok/s, D-03).
- `cmd/villa/testdata/status.json.golden` — re-freeze once (D-06).
- `cmd/villa/backend.go` — `backendShowEntry{Backend,Image}` + `newBackendShow`
  (the active-backend shape + `Backend.Name()`/`Image()` accessors to reuse, D-01).
- `internal/inference/inference.go` — `Backend` interface (`Name()`, `Image()`,
  `ResidencyProof()`); `internal/inference/backend.go` `BackendFor` resolver.
- `internal/metrics/llamacpp.go` — `ScrapeMetrics`/`PerfSnapshot`/`IsGenerating`/
  `ScrapeSlots`/`ActiveSlots` (the dashboard-proven tok/s collector to reuse for the
  CLI, D-03).
- `internal/recommend/recommend.go` — `Recommendation` struct + pure `Pick`
  (append `ROCmAdvice` + Note, derive from `HostProfile.rocm_readiness`, D-05/D-07);
  `cmd/villa/recommend.go` `renderRecommend`/`renderRecommendTable` (render the advice);
  `cmd/villa/testdata/recommend.golden.json` — re-freeze once (D-06).
- `internal/detect/readiness_rocm.go` — `computeROCmReadiness` + `ROCmReadiness`
  type (the sub-tree to fold, never recompute, D-04).
- `internal/dashboard/api.go` — `handleStatus` (serves `Report`; gains the new
  fields automatically) + `metricsView`/`handleMetrics` (tok/s source; do NOT add
  backend here, D-01).
- `internal/dashboard/assets/dashboard.{html,css,js}` — the vanilla-JS panels:
  `renderPerformance` (label tok/s by backend), the Health panel (active backend +
  image + readiness badge). `internal/dashboard/embed.go` (`go:embed all:assets`).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`backendShowEntry{Backend, Image}` + `Backend.Name()`/`Image()`** — the active
  backend identity already has a shape and accessors (Phase 8); D-01 reuses them so
  status/dashboard match `villa backend show` exactly.
- **`internal/metrics` collector** — `ScrapeMetrics`/`PerfSnapshot`/`IsGenerating`
  is public and already drives the dashboard Performance panel; the CLI tok/s (D-03)
  is a straight reuse, not a new scraper.
- **`detect.ROCmReadiness` sub-tree** — Phase 7 already computed the five
  typed-Optional readiness fields; D-04 folds them, D-05 reads them — no new probes.
- **Shared `internal/status` read-model** — `villa status` and `/api/status` already
  fold the SAME `Report` via `status.Run`; appending fields to `Report` surfaces them
  on both surfaces at once (one change, two consumers).

### Established Patterns
- **Append-only + golden re-freeze** — v1.0/v1.1 freeze each `--json` contract with a
  golden; a new field is a tail append + a reviewed one-time re-freeze (Phase 7 D-07
  did this for detect). D-06 applies it to status + recommend.
- **Typed-Unknown / no-false-green** — unevaluable = unset/omitted, distinct from a
  real false/zero; applies to tok/s (D-03, never a fabricated 0) and the readiness
  indicator (D-04, unknown ≠ not-ready).
- **Pure-core + cmd-layer I/O split** — `internal/recommend.Pick` is pure (advice
  derives from the in-hand `HostProfile`, D-05); `internal/status` stays pure with
  live wiring injected via `Deps` in `liveStatusDeps` (D-03); printing/exit stays in
  `cmd/villa`.
- **Resolver, never literal** — backend identity/markers come from
  `BackendFor(cfg.Backend)` (D-01/D-02); no `VulkanBackend()` literal remains in a
  call site.

### Integration Points
- `status.Report` ← new backend/image (D-01), tok/s (D-03), readiness indicator
  (D-04), schema_version (D-07) → consumed by `villa status` table+JSON AND the
  dashboard `/api/status` poll → dashboard JS panels.
- `liveStatusDeps` ← `metrics.ScrapeMetrics(endpoint)` for the tok/s seam (D-03);
  endpoint already derived from the resolved backend.
- `recommend.Pick` ← `HostProfile.rocm_readiness` → `ROCmAdvice` + Note (D-05) →
  `renderRecommendTable` + `recommend.golden.json`.
- Dashboard `renderPerformance` ← backend label from the `/api/status` poll (D-01),
  tok/s from `/api/metrics` (unchanged).

</code_context>

<specifics>
## Specific Ideas

- Mirror `villa backend show`'s `{backend, image}` exactly in the status surface so
  a user sees the same backend+image string in `status`, the dashboard, and
  `backend show` — one canonical rendering of "what's running."
- The dashboard Performance tok/s row should read e.g. "generation 12.3 tok/s
  (vulkan)" — the backend label comes from the status poll, the number from
  `/api/metrics`; when idle/unavailable, keep the existing honest copy ("Idle — no
  active generation." / "unavailable") rather than appending a backend to a fake 0.
- recommend advice copy must be honest and bench-pointing, e.g.
  "ROCm: worth trying for prompt-heavy workloads — token-gen may not improve;
  verify on your model with `villa bench --ab`." Never "ROCm is faster."
- Readiness badge: green `ROCm ready` only when every Known signal is good; gray
  `ROCm readiness unknown` off-hardware / when any signal is unevaluable; amber/red
  `ROCm not ready` only on a confidently-detected blocker (name it).

</specifics>

<deferred>
## Deferred Ideas

- **`villa bench --compare` one-shot flip→bench→flip-back + saved md/JSON report**
  — BENCH-03, v1.1.x backlog. This phase only *points* users to `villa bench`.
- **`rocm-6.4.4` alternate image for TG-heavy models** — ROCM-ALT-01, v1.1.x
  backlog; selected/validated via `villa bench`, not surfaced as auto-advice here.
- **Cumulative token/throughput usage tracking ("Token Spy")** — USAGE-01, v1.1.x;
  this phase shows a *live* tok/s snapshot, not historical accumulation.
- **Auto-switching to ROCm based on advice** — explicitly forbidden (REC-05 / D-05);
  advice never mutates config or units.
- **New detect ROCm probes (real firmware-date / HSA-override on-hardware checks)**
  — Phase 7 left `firmware_date_ok`/`hsa_override_viable` as off-hardware-unset;
  turning them into real probes is a separate detect-hardening effort, not Phase 10
  (Phase 10 consumes whatever readiness reports, honestly).

None of these were raised as new scope — the `--auto` pass stayed within the phase
boundary.

</deferred>

---

*Phase: 10-backend-tok-s-surfacing*
*Context gathered: 2026-06-06*
