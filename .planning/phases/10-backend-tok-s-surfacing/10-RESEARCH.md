# Phase 10: Backend + tok/s Surfacing - Research

**Researched:** 2026-06-06
**Domain:** Go control-plane read/surfacing capstone — composing existing read-models (status, recommend, detect, dashboard) into backend-aware output. No new behavior, no new deps.
**Confidence:** HIGH (every claim verified against the on-disk source this session)

## Summary

Phase 10 is a **pure read/surfacing composition** over code that already exists and is already tested. There is **no new switching, benchmarking, scraping, or detection logic** to write — Phases 6-9 built every primitive this phase consumes. The work is: (1) append four typed-optional fields to the shared `internal/status.Report` (active backend, image, live tok/s, ROCm-readiness tri-state) plus a `schema_version`; (2) append a `ROCmAdvice` enum + Note to `recommend.Recommendation` plus a `schema_version`, derived purely from the `HostProfile.rocm_readiness` already in hand; (3) wire the tok/s seam in `liveStatusDeps` by reusing `metrics.ScrapeMetrics`; (4) render all of it in the `villa status` table and the vanilla-JS dashboard; (5) re-freeze the two `--json` goldens (`status.json.golden`, `recommend.golden.json`) exactly once as reviewed pure-addition diffs. `detect.golden.json` stays byte-identical.

**The single most important correction to the phase brief:** Open Question #1 (the "previously-hardcoded `VulkanBackend()` correctness fix") is **already done**. `internal/status/status.go:179` resolves `inference.BackendFor(cfg.Backend)` and `status.go:239` feeds `Markers: backend.ResidencyProof()` into the residency verdict. There is **no Vulkan-default left in the read-model** — a ROCm install already asserts `ROCm0` because the markers come from the resolved backend. SC#1's "correctness fix" was landed in Phase 6's re-route of the residency input; Phase 10 must **verify and assert** this with a ROCm-config test, not change the wiring. `grep "VulkanBackend()"` across all non-test `.go` returns exactly one hit: its own definition in `backend_vulkan.go:63`. Zero call sites remain.

**Primary recommendation:** Treat this as an **append-only struct-extension + render + golden-refreeze** exercise. Add fields at the tail of `Report` and `Recommendation`; source backend identity from the already-resolved `backend.Name()`/`backend.Image()` (mirror `backendShowEntry`); reuse `metrics.ScrapeMetrics` for tok/s via a new `Deps` seam; fold the five `ROCmReadiness` Bools worst-wins with unknown-wins-over-not-ready; derive `ROCmAdvice` purely inside `Pick`. Re-freeze goldens with `go test ./cmd/villa/ -run <GoldenTest> -update`. Keep every backend-marker literal behind the inference seam (the `TestSeamGrepGate` walks `cmd/villa` and `internal/` — `backend.Name()` returns `"vulkan"`/`"rocm"` which are NOT gated tokens, but `ROCm0`/`HSA_OVERRIDE_GFX_VERSION`/`Memory access fault`/image tags ARE).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Active backend identity (name+image) | API/Backend (`internal/status.Report`) | — | Single authoritative surface (D-01); both CLI and dashboard read the same `Report`. Sourced from resolved `inference.Backend`. |
| Live tok/s collection | API/Backend (`internal/metrics`) | — | Already the dashboard's proven collector; CLI reuses it via a `status.Deps` seam wired in the cmd layer (`liveStatusDeps`). |
| tok/s labeling by backend | Browser/Client (dashboard JS) + CLI renderer | API composes the two reads | Dashboard composes `/api/status` (identity) + `/api/metrics` (number); identity is NOT duplicated into `metricsView` (D-01). |
| ROCm-readiness tri-state fold | API/Backend (`internal/status`, pure helper) | — | Folded from `detect.ROCmReadiness` (consumed, never recomputed, D-04). Pure function, table-testable. |
| ROCm advice derivation | API/Backend (pure `recommend.Pick`) | — | No I/O — `Pick` already takes `HostProfile`. Advice derives from `rocm_readiness` in hand (D-05). |
| Schema version bump | API/Backend (struct field) | — | Additive `schema_version` int on `Report` + `Recommendation` (D-07); detect stays at 2. |
| Rendering (table + badge) | CLI renderer + Browser/Client | — | Presentation only; no read-model logic in `cmd/villa` renderers or dashboard JS. |

## Standard Stack

**No new libraries.** This phase uses only what is already vendored. Per `CLAUDE.md` and project memory: **NO new Go deps for v1.1**. `go.mod` must be unchanged after this phase.

### Core (all existing, in-repo)
| Package | Purpose | Why Standard |
|---------|---------|--------------|
| `internal/status` | Shared read-model `Report` + `Run`/`Aggregate` | Both `villa status` and dashboard `/api/status` fold the SAME struct (DASH-01). |
| `internal/metrics` | `ScrapeMetrics`/`PerfSnapshot`/`IsGenerating`/`ScrapeSlots`/`ActiveSlots` | Dashboard-proven tok/s collector — reuse verbatim for the CLI (D-03). |
| `internal/recommend` | Pure `Pick` + `Recommendation` | Advice derives purely from the `HostProfile` already passed (D-05). |
| `internal/detect` | `ROCmReadiness` typed-Optional `Bool` sub-tree + `Str`/`Bool` value types | Consumed for the readiness indicator + advice; **no new detect fields** (D-06). |
| `internal/inference` | `Backend` interface (`Name()`, `Image()`, `ResidencyProof()`) + `BackendFor` resolver | Backend identity + residency markers, fail-closed (D-01/D-02). |
| stdlib `encoding/json`, `text/tabwriter`, `net/http` | JSON contracts, CLI table, bounded probes | Already the established pattern across every command. |

**Installation:** none. Run `go build ./... && go vet ./... && go test ./...` to confirm zero `go.mod` drift.

## Package Legitimacy Audit

> Not applicable — this phase installs **no external packages**. `go.mod` is frozen for v1.1 (project constraint). slopcheck/registry verification is moot: every dependency is first-party `internal/*` or Go stdlib. If a planner or executor proposes any `go get`, that is a scope violation — reject it.

## Architecture Patterns

### System Architecture Diagram

```
                          config.toml (cfg.Backend)
                                   │
                                   ▼
                    inference.BackendFor(cfg.Backend)         ── fail-closed resolver
                                   │
              ┌────────────────────┼─────────────────────────┐
              ▼                    ▼                          ▼
      backend.Name()        backend.Image()         backend.ResidencyProof()
      ("vulkan"/"rocm")     (digest-pinned tag)     (Vulkan0/ROCm0 markers — SEAM-ONLY)
              │                    │                          │
              └─────────┬──────────┘                          ▼
                        ▼                             RunningOffloadVerdict
          status.Report.Backend / .Image          (already wired, status.go:239)
                        │                                      │
   detect.Probe() ──► HostProfile.rocm_readiness               │
                        │  (5 typed-Optional Bools)            │
                        ▼                                      │
          foldROCmReadiness() ──► ready/not-ready/unknown      │
                        │                                      │
   metrics.ScrapeMetrics(endpoint) ──► PerfSnapshot ──► tok/s typed-optional
                        │                                      │
                        ▼                                      ▼
        ┌───────────────────────────────────────────────────────────┐
        │   internal/status.Report  (ONE frozen --json struct)        │
        │   + Backend + Image + GenTokensPerSec? + ROCmReadiness       │
        │   + SchemaVersion  (all TAIL-APPENDED)                       │
        └───────────────┬───────────────────────────┬─────────────────┘
                        │                            │
            villa status (table+JSON)        dashboard GET /api/status
                        │                            │
                        ▼                            ▼
              renderStatusTable              dashboard.js poll():
              (new rows)                     renderHealth + backend label
                                             + readiness badge; stash backend
                                             so renderPerformance(/api/metrics)
                                             labels tok/s "(vulkan)" / "(rocm)"

   recommend.Pick(HostProfile, catalog, ov)
        │  (Backend stays "vulkan" — REC-04 unchanged)
        ▼
   Recommendation + ROCmAdvice(enum) + Note + SchemaVersion  ── derived purely
        │                                                       from rocm_readiness
        ├──► villa recommend (table + JSON)
        └──► recommend.golden.json (re-frozen once)
```

### Recommended Project Structure (files touched — no new packages)
```
internal/status/status.go        # +4 fields on Report, + foldROCmReadiness helper, populate in Run
internal/status/status_test.go   # + ROCm-config residency assertion, tok/s stub, readiness fold table
internal/recommend/recommend.go  # + ROCmAdvice type + advice() + SchemaVersion; populate in Pick/buildRecommendation
internal/recommend/recommend_test.go # + advice derivation table (ready/worth-trying/verify-with-bench/blocked)
cmd/villa/status.go              # + tok/s seam in liveStatusDeps (metrics.ScrapeMetrics); new table rows
cmd/villa/status_test.go         # + tok/s Deps stub; golden refreeze
cmd/villa/recommend.go           # render ROCmAdvice + Note in renderRecommendTable
cmd/villa/recommend_test.go      # golden refreeze
cmd/villa/testdata/status.json.golden    # RE-FROZEN ONCE (pure-addition diff)
cmd/villa/testdata/recommend.golden.json # RE-FROZEN ONCE (pure-addition diff)
cmd/villa/testdata/detect.golden.json    # UNCHANGED — assert byte-identical
internal/dashboard/assets/dashboard.{html,css,js}  # backend label, image, readiness badge, tok/s label
internal/dashboard/*_test.go     # assert /api/status carries new fields; no metricsView change
```

### Pattern 1: Tail-append to a frozen struct + one-time golden re-freeze
**What:** New JSON fields go at the **tail** of the struct (after the last existing field, before any unexported `err`), preserving every existing `json:"..."` tag and order. Then regenerate the golden once.
**When to use:** Every `Report`/`Recommendation` field this phase adds (D-06).
**Caution — `Report.err`:** `internal/status.Report` has a **trailing unexported `err error`** field (status.go:103) with NO json tag — it is never serialized. New JSON fields must be appended **before** `err` in source order (Go field order doesn't affect JSON of unexported fields, but keep `err` last by convention; what matters for JSON is the order of *tagged* fields). Append new tagged fields after `Overall` (the current last tagged field, status.go:98) and put `SchemaVersion` as the final tagged field.
**Golden re-freeze command (verified mechanism):**
```bash
# The -update flag is defined once in cmd/villa/detect_test.go:13 (flag.Bool) and shared
# across the package test binary. Each golden test writes its file when -update is set.
go test ./cmd/villa/ -run TestStatusJSONGolden -update
go test ./cmd/villa/ -run TestRecommend -update    # confirm exact test name in recommend_test.go
go test ./cmd/villa/                                # then full run, goldens must match
git diff --stat cmd/villa/testdata/                 # review: status + recommend ONLY, pure additions
git diff cmd/villa/testdata/detect.golden.json      # MUST be empty
```

### Pattern 2: Typed-optional figure (no fabricated zero)
**What:** Live tok/s is meaningful only while generating; an idle/absent scrape must serialize as **omitted/unset**, never a real-looking `0.0`.
**When to use:** The `status.Report` tok/s field (D-03).
**Recommended shape:** a pointer with `omitempty` so an unset reading drops from JSON entirely:
```go
// Tail-appended to Report, before err:
// GenTokensPerSec is the live token-generation throughput (llamacpp:predicted_tokens_seconds)
// for the ACTIVE backend, populated ONLY when the server is generating (metrics.IsGenerating).
// It is *float64 + omitempty so an idle snapshot or a failed/absent /metrics scrape omits it
// entirely — a typed-Unknown, NEVER a fabricated 0.0 (D-03 / Pitfall: no-false-green).
GenTokensPerSec *float64 `json:"gen_tokens_per_sec,omitempty"`
```
This mirrors `metricsView.LatencyMS *float64 json:"latency_ms,omitempty"` (api.go:47) — the established honest-optional idiom in this codebase.

### Pattern 3: Consume-don't-recompute tri-state fold
**What:** Fold the five `detect.ROCmReadiness` Bools into `ready`/`not-ready`/`unknown` honoring no-false-green: **any Unknown → `unknown`**, never `not-ready` from an unevaluable signal.
**When to use:** The readiness indicator (D-04).
**Example (pure helper in internal/status, fully table-testable):**
```go
// ROCmReadinessIndicator is the tri-state surfaced from the detect rocm_readiness
// sub-tree. It is a string enum so the --json contract is stable and the dashboard
// badge maps it directly.
type ROCmReadinessIndicator string

const (
    ROCmReady    ROCmReadinessIndicator = "ready"      // every signal Known-good
    ROCmNotReady ROCmReadinessIndicator = "not-ready"  // at least one Known-BAD signal, all others Known
    ROCmUnknown  ROCmReadinessIndicator = "unknown"    // any signal unevaluable (off-hardware default)
)

// foldROCmReadiness reads (never recomputes) the detect sub-tree and folds worst-wins
// with UNKNOWN winning over NOT-READY (no-false-green, D-04/D-08): a single unevaluable
// signal makes the whole indicator "unknown", so off-hardware (most fields unset) the
// honest answer is "unknown", and a confidently-bad signal only yields "not-ready" when
// every other signal is Known.
func foldROCmReadiness(r detect.ROCmReadiness) ROCmReadinessIndicator {
    bools := []detect.Bool{
        r.HSAOverrideViable, r.FirmwareDateOK, r.KernelFloorOK,
        r.RocminfoGfx1151, r.ImagePolicyOK,
    }
    sawBad := false
    for _, b := range bools {
        if !b.Known {
            return ROCmUnknown // any unevaluable signal → unknown (never not-ready)
        }
        if !b.Value {
            sawBad = true
        }
    }
    if sawBad {
        return ROCmNotReady
    }
    return ROCmReady
}
```
**Fold-order note (Claude's discretion, D-04):** because `unknown` short-circuits the moment any `!Known` field is seen, order is irrelevant to correctness — `unknown` can never be masked by a later `not-ready`. This satisfies the locked invariant explicitly.

### Pattern 4: Pure advice derivation inside Pick (no I/O)
**What:** `ROCmAdvice` is derived from `HostProfile.rocm_readiness` inside `Pick` — which already receives the `HostProfile`. No new probe, no new argument.
**When to use:** REC-05 / D-05.
**Recommended enum + mapping:**
```go
type ROCmAdvice string

const (
    ROCmAdviceReady       ROCmAdvice = "ready"             // all signals Known-good
    ROCmAdviceWorthTrying ROCmAdvice = "worth-trying"      // (per D-05: all-good may also map here)
    ROCmAdviceVerifyBench ROCmAdvice = "verify-with-bench" // any unknown signal
    // empty "" when not applicable (no rocm_readiness, off-hardware refusal path)
)
```
**Mapping (D-05, recommended — planner finalizes thresholds):**
- All five signals Known-good → `worth-trying` (NOT a promise — see Note copy below)
- Any Known-**bad** signal → advice withheld (empty or a "not advised" Note naming the blocker); `Backend` still `vulkan`
- Any **unknown** signal → `verify-with-bench`

Reuse the exact same fold as `foldROCmReadiness` so status and recommend agree. Consider deriving `ROCmAdvice` from the tri-state to keep one source of truth: `ready→worth-trying`, `unknown→verify-with-bench`, `not-ready→withheld+blocker Note`.

**Honesty-safe Note copy (locked constraint — never promise a speed-up):**
> `ROCm: worth trying for prompt-heavy workloads — token generation may not improve (and can regress vs vulkan). Verify on your model with: villa bench --ab`

The on-hardware UAT (project memory 29e84c33) measured exactly this: vulkan→rocm Δpp **+4.84**, Δtg **−11.15**. The Note MUST reflect "pp may win, tg may regress, verify." **Never** "ROCm is faster."

**Proof the pick never changes:** `Pick` sets `Backend = defaultBackend` ("vulkan", recommend.go:24) or `m.BackendDefault` from the catalog — Phase 10 adds advice fields **after** that assignment and must not touch `rec.Backend`. A test must assert `rec.Backend == "vulkan"` regardless of `ROCmAdvice` value (REC-04 unchanged, no auto-switch).

### Anti-Patterns to Avoid
- **Re-resolving the backend or adding a second identity source.** `Report` is the single authoritative surface (D-01). Do NOT put backend identity into `metricsView`/`/api/metrics` — the dashboard composes identity (status) + number (metrics).
- **Recomputing `rocm_readiness`.** Read `detect.ROCmReadiness` from the probed `HostProfile`; never re-derive the five signals (D-04). Phase 10 adds NO detect fields.
- **Leaking a backend marker.** Do not write `ROCm0`, `HSA_OVERRIDE_GFX_VERSION`, `Memory access fault`, `kyuz0`, `docker.io/`, `server-vulkan`, `rocm-7.2.4`, `--device /dev/dri`, `--group-add`, or `keep-groups` into status/recommend/detect/dashboard code — `TestSeamGrepGate` (seam_test.go:34) walks `internal/` AND `cmd/villa` and fails CI. `backend.Name()` ("vulkan"/"rocm") and `backend.Image()` (the resolved string at runtime) are fine because they are **values returned by the seam**, not literals in the caller.
- **Fabricating a tok/s 0 when idle/unavailable.** Use `*float64`+`omitempty`; the renderer shows the existing honest copy ("Idle — no active generation." / "unavailable"), never `0.0 tok/s`.
- **Reordering/retagging existing golden fields.** Append-only; the diff must be pure addition.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Scrape tok/s for the CLI | A new `/metrics` parser | `metrics.ScrapeMetrics(endpoint)` + `IsGenerating` | Bounded, security-reviewed, dashboard-proven; new scraper = duplicate + drift + new attack surface. |
| Resolve the inference endpoint | Hand-build a URL | `inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()` (status.go:150 / backend.go:90) | Already backend-resolved; the established idiom in both status and prove paths. |
| Active backend identity shape | A new struct | Mirror `backendShowEntry{Backend, Image}` (backend.go:225) sourced from `backend.Name()`/`backend.Image()` | One canonical "what's running" string across `status`, dashboard, and `backend show` (D-01). |
| ROCm-readiness signals | New probes / firmware-date checks | `detect.ROCmReadiness` (already computed, schema 2) | Phase 7 owns the probes; Phase 10 consumes. New probes = ROADMAP scope violation (deferred). |
| Worst-wins / typed-Unknown | A bespoke aggregator | The `detect.Bool{Known,Value}` discipline + a 10-line fold | Matches the no-false-green pattern used everywhere; trivially table-testable. |
| Golden regeneration | Manual JSON editing | `go test ./cmd/villa/ -run <test> -update` | Hand-edited goldens drift from the encoder's exact bytes (indent, omitempty). |

**Key insight:** Phase 10 is deliberately scheduled last precisely so it composes finished primitives. If a task feels like it needs *new logic*, it is almost certainly scope creep — recheck the deferred list (BENCH-03, ROCM-ALT-01, USAGE-01, auto-switch, new detect probes).

## Runtime State Inventory

> Not a rename/refactor/migration phase — this is an additive contract change. The only persisted-contract concern is the **golden fixtures**, which are not runtime state but reviewed test artifacts:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — no datastore keys/IDs change. Verified: phase adds JSON fields only. | none |
| Live service config | None — no n8n/systemd/Quadlet unit content changes. The dashboard `.service` and `villa-llama.container` are untouched. | none |
| OS-registered state | None. | none |
| Secrets/env vars | None — no SOPS/env var names referenced or added. | none |
| Build artifacts / goldens | `cmd/villa/testdata/status.json.golden` + `recommend.golden.json` re-frozen once each (reviewed pure-addition diff); `detect.golden.json` MUST stay byte-identical. | re-run with `-update`, review diff, commit |

## Common Pitfalls

### Pitfall 1: Assuming SC#1's "correctness fix" is unfinished work
**What goes wrong:** Planner writes a task to "change the hardcoded `VulkanBackend()` in status to the resolved backend" — but it was already changed in Phase 6.
**Why it happens:** The success-criterion phrasing ("previously-hardcoded") reads like pending work.
**How to avoid:** `status.go:179` already calls `BackendFor(cfg.Backend)`; `status.go:239` already passes `Markers: backend.ResidencyProof()`. `grep VulkanBackend() *.go` (non-test) returns only the definition. The Phase-10 task is a **proving test** (ROCm-config residency asserts `ROCm0`) + appending the **visible** backend/image fields, NOT a wiring change.
**Warning signs:** A diff that touches the residency-input wiring in `status.Run`.

### Pitfall 2: `not-ready` masquerading for `unknown`
**What goes wrong:** Off-hardware, `HSAOverrideViable`/`FirmwareDateOK` are unset (Known=false). A naive `Value==false → not-ready` fold reports "ROCm not ready" on every dev machine — a false-green inversion.
**Why it happens:** Conflating Go's zero-value `false` with a confident "no".
**How to avoid:** Check `b.Known` FIRST; any `!Known` short-circuits to `unknown` (Pattern 3 / D-08). Test the off-hardware fixture explicitly expects `unknown`.
**Warning signs:** A readiness test fixture with all-unset Bools expecting `not-ready`.

### Pitfall 3: Promising a speed-up in the advice copy
**What goes wrong:** Advice says "ROCm is faster" / "switch to ROCm for better performance."
**Why it happens:** Intuition that the opt-in backend must be the fast one.
**How to avoid:** On-hardware truth (memory 29e84c33): pp wins, **tg regresses** (Δtg −11.15). Copy must be pp-weighted + bench-pointing + never a guarantee (D-05). Add a test asserting the Note contains "verify" / "villa bench" and does NOT contain "faster"/"guaranteed".
**Warning signs:** Any superlative in the Note string.

### Pitfall 4: Fabricated `0.0 tok/s` when idle
**What goes wrong:** A non-pointer `float64` tok/s field serializes `0` when idle/unavailable, shown as a real reading.
**Why it happens:** Forgetting the gauges are stale snapshots when `!IsGenerating` (metrics.go Pitfall 3 baked-in).
**How to avoid:** `*float64`+`omitempty`; populate only when `ok && metrics.IsGenerating(snap, slots)`. Mirror `metricsView.LatencyMS`.
**Warning signs:** `GenTokensPerSec float64` (non-pointer) in `Report`.

### Pitfall 5: Seam-gate CI failure from a stray marker
**What goes wrong:** Writing a comment or label like `// ROCm0 means…` in `status.go` or `dashboard.js` trips `TestSeamGrepGate`.
**Why it happens:** Documenting the markers in the consuming layer.
**How to avoid:** Refer to backend identity via `backend.Name()`/values only. The badge label can say "ROCm" (the word) — gated tokens are `ROCm0` (the device marker), `HSA_OVERRIDE_GFX_VERSION`, `Memory access fault`, and image literals, NOT the bare word "rocm"/"ROCm". Confirm against `seam_test.go:42,124`.
**Warning signs:** Any of the gated regexes appearing outside `internal/inference/` or `internal/detect/gpu_amd.go`.

### Pitfall 6: Breaking the detect golden
**What goes wrong:** Touching `detect` to "add a readiness summary" bumps `hostProfileSchemaVersion` and breaks `detect.golden.json`.
**How to avoid:** Phase 10 **consumes** `rocm_readiness`; it adds NO detect fields. `detect.golden.json` and `hostProfileSchemaVersion` (==2) stay frozen (D-06). The fold lives in `internal/status`, not `internal/detect`.
**Warning signs:** Any edit under `internal/detect/`.

## Code Examples

### Sourcing active backend identity into Report (in status.Run)
```go
// internal/status/status.go — inside Run, AFTER `backend, err := inference.BackendFor(cfg.Backend)`
// (already present at status.go:179). Source identity from the resolved backend (D-01),
// never a literal. backend.Name()/Image() are the SAME accessors backendShowEntry uses.
report.Backend = backend.Name()
report.Image = backend.Image()
report.ROCmReadiness = foldROCmReadiness(profile.ROCmReadiness) // profile from a new Deps probe seam
report.SchemaVersion = reportSchemaVersion // additive int const, e.g. 1 (first versioned)
```
> Note: `status.Run` does not currently call `detect.Probe()` — readiness must arrive via a **new `Deps` seam** (e.g. `ROCmReadiness func() detect.ROCmReadiness`) so `internal/status` stays pure and `status_test.go` can stub it. `liveStatusDeps` wires it to `detect.Probe().ROCmReadiness`.

### tok/s seam in liveStatusDeps (cmd/villa/status.go) — reuse, don't rebuild
```go
// New status.Deps seam member (declared in internal/status):
//   GenTokensPerSec func(endpoint string) *float64
// Wired in liveStatusDeps using the EXISTING collector:
GenTokensPerSec: func(endpoint string) *float64 {
    snap, ok := metrics.ScrapeMetrics(endpoint)
    if !ok {
        return nil // /metrics 404 or transport error → typed-Unknown (omitted)
    }
    slots, _ := metrics.ScrapeSlots(endpoint)
    if !metrics.IsGenerating(snap, slots) {
        return nil // idle: gauges are stale snapshots → omit, never a fabricated 0 (D-03)
    }
    v := snap.GenTokensPerSec
    return &v
},
```
`endpoint` is already the backend-resolved value (`liveStatusDeps` computes it at status.go:150). In `status.Run`, call `d.GenTokensPerSec(endpoint)` and assign to `report.GenTokensPerSec`.

### Dashboard JS: label tok/s by backend (compose two polls)
```js
// In poll(): the /api/status response now carries report.backend. Stash it so the
// independently-fetched /api/metrics render can label the rate. (D-01: identity from
// status, number from metrics — never duplicated into /api/metrics.)
.then(function (report) {
    setConnected(true);
    renderVerdict(report.overall);
    renderHealth(report.services);
    renderBackend(report.backend, report.image, report.rocm_readiness); // new panel/rows
    lastBackend = report.backend; // module-scoped, read by renderPerformance
})
// In renderPerformance(m) generation row (dashboard.js:170), append the label:
perfBody.appendChild(metricRow("generation",
    (m.gen_tokens_per_sec || 0).toFixed(1) + " tok/s" +
    (lastBackend ? " (" + lastBackend + ")" : "")));
```
Readiness badge: mirror the GPU panel's `busy_available` "Unavailable" idiom (api.go:125 / dashboard.js:217) — `ready`→green badge, `unknown`→gray badge ("ROCm readiness unknown"), `not-ready`→amber/red badge naming nothing the JS shouldn't (the indicator string is enough). Build via `document.createElement` (XSS-safe, the established pattern, dashboard.js:115-133).

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| status read-model keyed residency on a Vulkan default | residency keyed on `BackendFor(cfg.Backend).ResidencyProof()` | Phase 6 (2026-06-05) | SC#1 "correctness fix" already landed; Phase 10 only verifies + surfaces it. |
| `recommend.Recommendation` had only `Backend string` | + `ROCmAdvice` + `Note` + `SchemaVersion` (Phase 10) | this phase | Honest advice, Vulkan still default. |
| `llamacpp:kv_cache_usage_ratio` gauge | removed upstream; tok/s from `*_tokens_seconds` gauges, idle from `/slots` | pre-Phase-5 (RESEARCH A1) | Already baked into `internal/metrics`; reuse as-is, do not query the dead gauge. |

**Deprecated/outdated:** none introduced. Confirm `go.mod` unchanged.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The recommend golden test function name is matchable by `-run TestRecommend`; planner should confirm the exact name in `recommend_test.go` before scripting `-update`. | Pattern 1 | Wrong `-run` filter regenerates nothing or the wrong golden — caught immediately by a non-matching test run. LOW risk. |
| A2 | `reportSchemaVersion`/`recommendSchemaVersion` start at `1` (first versioned contract). The exact starting integer is Claude's discretion (D-07 names the field, not the value). | Code Examples | Cosmetic; any positive int satisfies "bumped schema version" so long as it's tail-appended and frozen. LOW. |
| A3 | The dashboard backend label reads `report.backend` (json key for the new `Report.Backend` field); exact json spelling is Claude's discretion (D-01/Discretion). | Dashboard JS | Must keep JS key in lockstep with the Go json tag chosen; trivially testable. LOW. |
| A4 | All-good readiness maps to `worth-trying` (not a bare `ready`) in `ROCmAdvice`, per the D-05 recommended mapping; planner finalizes whether `ready` is also surfaced as a distinct advice value. | Pattern 4 | Affects advice copy only; the honesty constraint (no speed-up promise) holds either way. LOW. |

## Open Questions

1. **Does the human `villa status` table show the image tag inline or only under `-v`?**
   - What we know: D-01 requires `--json` to carry the image unconditionally; the table presentation is Claude's discretion (CONTEXT Discretion bullet 3).
   - Recommendation: show `backend  vulkan` always; show `image  <tag>` under `-v` (verbose) to keep the default table compact, mirroring how `renderStatusTable` already gates provenance behind `withProvenance` (status.go:122). Planner's call.

2. **Exact `-update` test name for the recommend golden.**
   - What we know: detect/status golden tests are `TestStatusJSONGolden` / (detect) `TestDetect...Golden`; recommend's golden test lives in `recommend_test.go` and references `recommend.golden.json` at line 45.
   - Recommendation: `grep "func Test" cmd/villa/recommend_test.go` before scripting the `-update` invocation. Not a blocker — running the full `go test ./cmd/villa/` after `-update` proves the refreeze.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test/golden-update | ✓ (project builds today) | per go.mod | — |
| Live gfx1151 host | Verifying SC#1 ROCm residency on a *real* ROCm install + live tok/s | ✗ (off-hardware dev) | — | Off-hardware: test with a `cfg.Backend="rocm"` fixture asserting the verdict keys on `ROCm0` markers (the markers come from `backendROCm.ResidencyProof()`, exercisable without hardware). Live ROCm residency + live tok/s labeling are on-hardware UAT items. |

**Missing dependencies with no fallback:** none — every code path is unit-testable off-hardware with stubs/fixtures (the established pattern: `status_test.go` injects a full `status.Deps`).
**Missing dependencies with fallback:** live-host validation (ROCm residency surfaced in status, live tok/s rendered in dashboard, backend label correctness on a real ROCm switch) → defer to on-hardware UAT, same as Phases 8/9.

## Validation Architecture

> `.planning/config.json` not inspected for the explicit `nyquist_validation` flag during research; absent ⇒ treat as enabled. Section included.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table tests + golden files), `cobra` command harness |
| Config file | none — `go test` |
| Quick run command | `go test ./internal/status/ ./internal/recommend/` |
| Full suite command | `go build ./... && go vet ./... && go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DASH-06 | status `Report` carries backend+image (resolved, not literal) | unit | `go test ./cmd/villa/ -run TestStatus` | ✅ extend status_test.go |
| DASH-06 | ROCm-config residency asserts `ROCm0` (SC#1 proof) | unit | `go test ./internal/status/ -run ROCm` (new) | ❌ Wave 0 — add ROCm-config residency test |
| DASH-06 | live tok/s typed-optional, omitted when idle/unavailable | unit | `go test ./cmd/villa/ -run TestStatus` | ❌ Wave 0 — add tok/s Deps stub cases |
| DASH-06 | readiness tri-state fold (unknown wins over not-ready) | unit | `go test ./internal/status/ -run Readiness` (new) | ❌ Wave 0 — add fold table test |
| DASH-06 | status `--json` golden re-frozen, pure-addition diff | golden | `go test ./cmd/villa/ -run TestStatusJSONGolden` | ✅ status_test.go:214 (re-freeze) |
| REC-05 | `ROCmAdvice` derived purely from rocm_readiness; Backend stays vulkan | unit | `go test ./internal/recommend/` | ✅ extend recommend_test.go |
| REC-05 | advice Note never promises a speed-up (contains "verify"/"bench") | unit | `go test ./internal/recommend/ -run Advice` (new) | ❌ Wave 0 — add copy assertion |
| REC-05 | recommend golden re-frozen, pure-addition diff | golden | `go test ./cmd/villa/ -run <recommend golden>` | ✅ recommend_test.go:45 |
| DASH-06/REC-05 | `detect.golden.json` byte-identical (no detect change) | golden | `go test ./cmd/villa/ -run TestDetect` | ✅ detect_test.go (must stay green w/o -update) |
| DASH-06 | dashboard `/api/status` serves new fields; `metricsView` unchanged | unit | `go test ./internal/dashboard/` | ✅ extend api_test.go |
| both | seam gate: no backend markers leak into surfacing code | regression | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ seam_test.go:34 (must stay green) |

### Sampling Rate
- **Per task commit:** `go test ./internal/status/ ./internal/recommend/ ./internal/dashboard/ ./cmd/villa/ && go test ./internal/inference/ -run TestSeamGrepGate`
- **Per wave merge:** `go build ./... && go vet ./... && go test ./...`
- **Phase gate:** full suite green + `git diff cmd/villa/testdata/detect.golden.json` empty + the two re-frozen goldens reviewed as pure additions, before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/status/status_test.go` — ROCm-config residency test (cfg.Backend="rocm" → verdict keys on ROCm markers); readiness-fold table (all-unset→unknown, all-good→ready, one-Known-bad→not-ready)
- [ ] `cmd/villa/status_test.go` — tok/s `Deps` stub cases (generating→value, idle→nil, unavailable→nil)
- [ ] `internal/recommend/recommend_test.go` — advice derivation table + Backend-stays-vulkan + Note-honesty (no "faster"/"guaranteed", contains "bench")
- [ ] `internal/dashboard/*_test.go` — assert `/api/status` carries backend/image/rocm_readiness; assert `metricsView` JSON shape UNCHANGED
- [ ] Framework install: none — existing Go test infra covers all phase requirements

## Security Domain

> `security_enforcement` assumed enabled (Phase 9 was SECURED, memory 4434c727). This phase adds **read-only output fields** — no new inputs, mutations, network listeners, or auth surfaces.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth surface added; dashboard auth posture (same-origin + JSON-content-type on POST) unchanged. |
| V3 Session Management | no | No sessions. |
| V4 Access Control | no | Read-only additions; the ONE dashboard mutation (`/api/models/switch`) is untouched. |
| V5 Input Validation | minimal | No new request inputs. The tok/s number comes from the already-bounded `metrics.ScrapeMetrics` (io.LimitReader, T-05-07). Backend/image strings come from the trusted resolver, not user input. |
| V6 Cryptography | no | None. |
| V7 Errors/Logging (info disclosure) | yes | The new fields surface only: backend name, image tag, a tok/s float, and a tri-state enum — no prompts, no sampling params, no secrets. `metricsView`'s narrow field-set discipline (no `/slots` prompt leakage, T-05-08) is preserved by NOT extending it. |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Backend-marker leak outside the seam | Tampering (defense-in-depth) | `TestSeamGrepGate` walks `cmd/villa` + `internal/` (seam_test.go:34); surfacing code uses `backend.Name()` values, never gated literals. |
| False-green readiness / fabricated tok/s | Repudiation / integrity of the honest-status contract | Typed-Unknown discipline: `*float64`+`omitempty` for tok/s, `unknown`-wins fold for readiness (D-03/D-04/D-08). |
| Info disclosure via the new perf field | Information disclosure | Reuse the field-narrowed `metrics`/`metricsView` collector (T-09-01/T-05-08); surface only the gen tok/s float, never prompt/slot internals. |
| DoS via the CLI tok/s scrape | Denial of service | `ScrapeMetrics` is bounded (2s timeout + 64 KiB io.LimitReader); the CLI reuse inherits these bounds. |

No new threats expected; the existing Phase-5/9 controls cover the additive surface. Planner should still run `/gsd-secure-phase 10` after build (the milestone discipline).

## Sources

### Primary (HIGH confidence — verified in-repo this session)
- `internal/status/status.go` (read full) — `Report` struct (err is trailing unexported, status.go:103), `Run` resolves `BackendFor` at :179 and feeds `backend.ResidencyProof()` at :239, `Deps`, `Aggregate`.
- `cmd/villa/status.go` (read full) — `liveStatusDeps` resolves backend + endpoint (:146-150), table renderer, exit map.
- `internal/metrics/llamacpp.go` (read full) — `ScrapeMetrics`/`PerfSnapshot`/`IsGenerating`/`ScrapeSlots`/`ActiveSlots`.
- `internal/recommend/recommend.go` (read full) — pure `Pick`, `Recommendation`, `defaultBackend="vulkan"` (:24).
- `internal/detect/profile.go` + `readiness_rocm.go` + `value.go` — `ROCmReadiness` (5 typed-Optional Bools), `hostProfileSchemaVersion==2`, `Bool{Known,Value}`.
- `cmd/villa/backend.go` (read full) — `backendShowEntry{Backend,Image}` (:225), `backend.Name()`/`Image()` usage (:249), seam-discipline header comment.
- `internal/inference/inference.go` (:50-72) + `backend.go` (:15-40) — `Backend` interface, fail-closed `BackendFor`.
- `internal/dashboard/api.go` (read full) — `handleStatus` serves `Report` verbatim (:17), `metricsView` (:39), `gpuView` BusyAvailable honest-unknown idiom (:115).
- `internal/dashboard/assets/dashboard.js` (:100-178, :430-489) — `poll()`, `renderHealth`, `renderPerformance`, `renderGPU` badge idiom.
- `internal/inference/seam_test.go` (:34-160) — `TestSeamGrepGate` patterns + cmd/villa walk; gated tokens are `ROCm0|HSA_OVERRIDE_GFX_VERSION|Memory access fault` + image/device literals (:42,124).
- `cmd/villa/{status,recommend,detect}_test.go` — golden mechanism: `-update` flag defined `flag.Bool` in detect_test.go:13, golden tests write on `-update`.
- `grep VulkanBackend() *.go` (non-test) — single hit: `backend_vulkan.go:63` (definition only; zero call sites).

### Secondary (HIGH — project memory, on-hardware verified)
- graphmind memory `29e84c33` — Phase 9 on-hardware UAT: vulkan→rocm Δpp +4.84 / Δtg −11.15 (the honesty constraint's empirical basis).
- graphmind memory `4434c727` — Phase 9 SECURED 11/11; the typed-Unknown/no-false-green + field-narrowing controls this phase inherits.
- `10-CONTEXT.md` D-01..D-07 (locked decisions).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new deps; every package read in source.
- Architecture: HIGH — all integration points traced to file:line; SC#1 confirmed already-wired.
- Pitfalls: HIGH — each grounded in a specific file:line or on-hardware measurement.

**Research date:** 2026-06-06
**Valid until:** stable until the v1.1 code changes (no external/fast-moving deps) — effectively until Phase 10 lands.
