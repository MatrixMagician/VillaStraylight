# Phase 10: Backend + tok/s Surfacing - Pattern Map

**Mapped:** 2026-06-06
**Files analyzed:** 13 (6 source modified, 1 served-unchanged, 3 dashboard assets, 6 golden/test files)
**Analogs found:** 13 / 13 (all in-repo; this is an append-only surfacing capstone, zero greenfield)

> Every new field/renderer this phase adds has a **direct in-repo analog already in
> production**. No new package, no new dependency, no new probe. The executor's job is
> tail-append + render + golden re-freeze, mirroring patterns Phases 5-9 established.
> The "correctness fix" (SC#1) is **already wired** (status.go:179/239) — Phase 10
> *proves* it with a ROCm-config test, it does not change the wiring.

---

## File Classification

| Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---------------|------|-----------|----------------|---------------|
| `internal/status/status.go` | model (read-model struct) | request-response / transform | `internal/detect/profile.go` (Phase-7 tail-append to frozen struct) | exact |
| `cmd/villa/status.go` | controller (cobra + live wiring) | request-response | existing `liveStatusDeps` / `liveProps` / `renderStatusTable` in same file | exact (self-analog) |
| `internal/metrics/llamacpp.go` | service (collector) | streaming/scrape | **REUSE-AS-IS** — `ScrapeMetrics`/`IsGenerating`/`ScrapeSlots` (no edit) | reuse source |
| `internal/recommend/recommend.go` | model + pure derivation | transform (pure) | `buildRecommendation`/`pickBest` Notes+Backend population in same file | exact (self-analog) |
| `cmd/villa/recommend.go` | controller (renderer) | request-response | existing `renderRecommendTable` notes/backend rendering in same file | exact (self-analog) |
| `internal/detect/readiness_rocm.go` | model (READ source) | transform | **CONSUME-ONLY** — `ROCmReadiness` typed-Optional Bools (no edit) | read source |
| `internal/dashboard/api.go` | controller (handler) | request-response | `handleStatus` serves `Report` verbatim (no edit needed) | reuse source |
| `internal/dashboard/assets/dashboard.js` | component (vanilla JS) | event-driven (poll) | `renderPerformance`/`renderGPU` busy badge / `metricRow` / `renderHealth` | exact |
| `internal/dashboard/assets/dashboard.html` | component (markup) | static | `#health-panel`/`#health-rows` (JS-appended, likely no structural change) | exact |
| `internal/dashboard/assets/dashboard.css` | config (styles) | static | `.badge-ready/warn/unknown` + `--status-*` (already defined — **zero new CSS**) | exact |
| `cmd/villa/status_test.go` | test (golden + stub) | — | `TestStatusJSONGolden` + `newStatusDeps` stub | exact (self-analog) |
| `cmd/villa/recommend_test.go` | test (golden) | — | `TestRecommendJSONGolden` | exact (self-analog) |
| `internal/{status,recommend}_test.go`, `dashboard/api_test.go` | test (table/unit) | — | `fixtureProfile` (detect_test.go) + existing table tests | exact |

---

## Pattern Assignments

### `internal/status/status.go` (model, transform) — D-01/D-03/D-04/D-07

**Analog:** `internal/detect/profile.go` (Phase-7 tail-append discipline) + `value.go` (typed-Optional `Bool`).

**Tail-append to a frozen struct** — analog `profile.go:53-60` (how Phase 7 appended `ROCmReadiness` + `SchemaVersion` as the LAST tagged fields, "nothing above it moved"):
```go
// internal/detect/profile.go:53-60 — the EXACT append shape to mirror on Report:
	// ROCmReadiness ... appended AFTER the GPU block (D-06) as a strictly additive
	// contract change; nothing above it moved.
	ROCmReadiness ROCmReadiness `json:"rocm_readiness"`

	// SchemaVersion ... MUST stay the LAST field (append-only; new fields go above it).
	SchemaVersion int `json:"schema_version"`
```

**Apply to `Report` (status.go:89-104):** append AFTER `Overall` (status.go:98, the current last *tagged* field) and BEFORE the trailing unexported `err error` (status.go:103, which has no json tag and never serializes). Order of new tagged fields: `Backend`, `Image`, `GenTokensPerSec`, `ROCmReadiness`, then `SchemaVersion` last.

**Typed-optional tok/s (no fabricated 0)** — analog `internal/dashboard/api.go:47` (`LatencyMS *float64 json:"latency_ms,omitempty"` — the established honest-optional idiom):
```go
// api.go:44-47 — the *float64+omitempty idiom to copy for GenTokensPerSec:
	LatencyMS *float64 `json:"latency_ms,omitempty"`
```

**Consume-don't-recompute tri-state fold** — analog: the `detect.Bool{Known,Value}` discipline (`value.go:63-74`) + the worst-wins `bump` fold (`status.go:291-312`). NEW pure helper `foldROCmReadiness` reads `detect.ROCmReadiness` (profile.go:70-88), checking `b.Known` FIRST so any unevaluable signal short-circuits to `unknown` (no-false-green). The five Bools to read: `HSAOverrideViable, FirmwareDateOK, KernelFloorOK, RocminfoGfx1151, ImagePolicyOK`.

**Source backend identity from the resolver (D-01)** — `status.Run` already resolves `backend, err := inference.BackendFor(cfg.Backend)` at **status.go:179**. Set `report.Backend = backend.Name()` and `report.Image = backend.Image()` from that ALREADY-RESOLVED value (the same accessors `backendShowEntry` uses). Never a literal.

**SC#1 correctness — ALREADY WIRED, do not change:** status.go:239 already feeds `Markers: backend.ResidencyProof()` (the resolved backend) into `RunningOffloadVerdict`. A ROCm install already asserts its own markers. Phase 10's task is a *proving test*, not a wiring edit.

**New `Deps` seam members (status.go:109-143)** — mirror the shape of existing seams (`Props func(endpoint string) *inference.PropsInfo`, `Endpoint func() string`). Add:
```go
// New Deps members, declared in internal/status, wired in cmd/villa liveStatusDeps:
	GenTokensPerSec func(endpoint string) *float64   // tok/s seam (D-03)
	ROCmReadiness   func() detect.ROCmReadiness       // readiness probe seam (D-04)
```
> Note: `status.Run` does NOT currently call `detect.Probe()`; readiness must arrive via the new seam so `internal/status` stays pure and `status_test.go` can stub it (mirror how `Props`/`GTTUsed` are injected, never called directly).

---

### `cmd/villa/status.go` (controller) — D-03

**Analog:** the existing `liveProps` HTTP-probe seam (status.go:275-301) and the `liveStatusDeps` wiring block (status.go:151-175); table rows in `renderStatusTable` (status.go:98-132).

**tok/s seam — REUSE the collector, do not rebuild** — wire into `liveStatusDeps` (the `endpoint` is already backend-resolved at status.go:150). Mirror the nil-on-failure discipline of `liveProps`:
```go
// New status.Deps member wired in liveStatusDeps, reusing internal/metrics verbatim:
GenTokensPerSec: func(endpoint string) *float64 {
	snap, ok := metrics.ScrapeMetrics(endpoint)   // llamacpp.go:106 — bounded, dashboard-proven
	if !ok {
		return nil // 404/transport error → typed-Unknown (omitted), mirrors liveProps nil
	}
	slots, _ := metrics.ScrapeSlots(endpoint)      // llamacpp.go:141
	if !metrics.IsGenerating(snap, slots) {        // llamacpp.go:170
		return nil // idle: gauges are stale snapshots → omit, NEVER a fabricated 0 (D-03)
	}
	v := snap.GenTokensPerSec
	return &v
},
ROCmReadiness: func() detect.ROCmReadiness { return detect.Probe().ROCmReadiness },
```
Add `"github.com/MatrixMagician/VillaStraylight/internal/metrics"` to the import block (status.go:14-22).

**Table rows** — analog `renderStatusTable` tabwriter rows (status.go:100, 113, 121). Append rows in the same `fmt.Fprintf(tw, "label\t%s\n", ...)` idiom:
```go
// status.go:100/113/121 idiom to mirror for the new rows:
	fmt.Fprintf(tw, "overall\t%s\n", r.Overall)
	fmt.Fprintf(tw, "loopback-only\t%s\n", strconv.FormatBool(r.LoopbackOnly))
```
Render `backend  <name>` always; gate `image  <tag>` behind the existing `withProvenance` (-v) flag (status.go:122) per the compact-table recommendation; render the tok/s row only when non-nil (omit on nil, never `0.0`); render `rocm-readiness  <tri-state>`.

---

### `internal/recommend/recommend.go` (model, pure transform) — D-05/D-07

**Analog:** `buildRecommendation` (recommend.go:198-222) Backend+Notes population; the `defaultBackend = "vulkan"` const (recommend.go:24); the append-to-Notes idiom (recommend.go:85-90).

**Append-only enum + Note, derived purely** — `Pick` already takes `detect.HostProfile` (recommend.go:74); read `p.ROCmReadiness` in hand. The pick is set at recommend.go:203-206 / :24 — add advice fields AFTER, never touch `rec.Backend`:
```go
// recommend.go:203-219 — the assignment ORDER to preserve (Backend stays vulkan):
	backend := m.BackendDefault
	if backend == "" {
		backend = defaultBackend   // ":24 — vulkan, REC-04 unchanged"
	}
	return Recommendation{
		...
		Backend: backend,          // <- never reassigned by advice
		...
	}
```
Append to the `Recommendation` struct (recommend.go:29-54) AFTER `Alternatives` (the current last field): `ROCmAdvice string json:"rocm_advice,omitempty"`, `ROCmNote string json:"rocm_note,omitempty"`, `SchemaVersion int json:"schema_version"` (last). Reuse the SAME fold logic as `status.foldROCmReadiness` so the two surfaces agree (`ready→worth-trying`, `unknown→verify-with-bench`, `not-ready→withheld + blocker Note`).

**Honesty constraint (Pitfall 3 / on-hardware UAT Δtg −11.15):** the Note MUST contain "verify"/"villa bench" and MUST NOT contain "faster"/"guaranteed"/"speed-up". Recommended copy:
> `ROCm: worth trying for prompt-heavy workloads — token generation may not improve (and can regress vs vulkan). Verify on your model with: villa bench --ab`

---

### `cmd/villa/recommend.go` (controller, renderer) — D-05

**Analog:** `renderRecommendTable` (recommend.go:128-180) — the Notes loop (recommend.go:165-167) and the backend line (recommend.go:141-142).
```go
// recommend.go:141-142 + 165-167 — the rendering idiom to mirror for the advice line:
	fmt.Fprintf(w, "Recommended: %s  (quant %s, ctx %d, backend %s)\n", ...)
	for _, n := range rec.Notes {
		fmt.Fprintf(w, "  - %s\n", n)
	}
```
Render the `ROCmAdvice`/`ROCmNote` after the Notes loop, gated `if rec.ROCmAdvice != ""`. `renderRecommend` (recommend.go:119-126) already encodes `rec` as JSON verbatim, so the new tagged fields surface in `--json` automatically.

---

### `internal/metrics/llamacpp.go` (service, scrape) — REUSE, NO EDIT

**Public, dashboard-proven collector** consumed by both the existing dashboard (`handleMetrics`, api.go:73) and now the CLI. `ScrapeMetrics` (llamacpp.go:106), `IsGenerating` (llamacpp.go:170), `ScrapeSlots` (llamacpp.go:141), `PerfSnapshot.GenTokensPerSec` (llamacpp.go:43). Bounded by `scrapeTimeout` (2s) + `maxScrapeBody` (64 KiB io.LimitReader) — the CLI reuse inherits these bounds (Security: DoS mitigated). **Do not add fields; do not query the removed KV-cache gauge.**

---

### `internal/detect/readiness_rocm.go` + `profile.go` (READ source) — CONSUME, NO EDIT

`detect.ROCmReadiness` (profile.go:70-88) is the five typed-Optional Bools to FOLD. `computeROCmReadiness` (readiness_rocm.go:20) already populated them in Phase 7. **Phase 10 adds NO detect field; `hostProfileSchemaVersion` stays 2; `detect.golden.json` stays byte-identical (Pitfall 6).** Any edit under `internal/detect/` is a warning sign of scope creep.

---

### `internal/dashboard/api.go` (handler) — D-01, NO/MINIMAL EDIT

`handleStatus` (api.go:17-20) serializes the shared `Report` verbatim — it gains the new backend/image/tok-s/readiness fields **for free** the moment they land on `Report`. **Do NOT add backend identity to `metricsView` (api.go:39-67)** — identity lives in status, the number in metrics; the JS composes them (D-01). The only test addition: assert `/api/status` carries the new fields and `metricsView` JSON shape is UNCHANGED.

---

### `internal/dashboard/assets/dashboard.js` (component, poll) — UI-SPEC elements 1-3

**Analog A — `metricRow` + tok/s composition** (dashboard.js:49-61, 170): append the `(backend)` suffix to the existing generation row, gated so idle/unavailable branches (dashboard.js:151,159,163) are untouched:
```js
// dashboard.js:170 — the row to extend (number from /api/metrics, label from /api/status):
perfBody.appendChild(metricRow("generation",
  (m.gen_tokens_per_sec || 0).toFixed(1) + " tok/s" +
  (lastBackend ? " (" + lastBackend + ")" : "")));   // lastBackend stashed in poll()
```

**Analog B — stash backend in the status poll** (dashboard.js:452-456): mirror the existing `.then(report)` body; add `lastBackend = report.backend;` (module-scoped var declared near dashboard.js:31, like `switching`) and a `renderBackend(report.backend, report.image, report.rocm_readiness)` call after `renderHealth`.

**Analog C — readiness badge = the GPU busy_available honest-Unknown precedent** (dashboard.js:217-230): copy this exact branch shape (`document.createElement` + `textContent`, XSS-safe), mapping the tri-state to the EXISTING badge classes:
```js
// dashboard.js:223-227 — the gray-badge "Unavailable" precedent to mirror for unknown:
} else {
  var badge = document.createElement("span");
  badge.className = "badge badge-unknown";
  badge.textContent = "Unavailable";
  ...
}
```
Mapping (UI-SPEC + healthClass idiom dashboard.js:72-89): `ready`→`badge badge-ready` "ROCm ready"; `not-ready`→`badge badge-warn` "ROCm not ready"; `unknown`/absent→`badge badge-unknown` "ROCm readiness unknown" + optional `mutedP` caption (dashboard.js:40). Backend-absent → `healthLabel` default "unavailable" gray badge (dashboard.js:82-88).

**Analog D — health rows for backend/image** (dashboard.js:117-141 `renderHealth`): build `.health-row` rows with `document.createElement` + `textContent` exactly as `renderHealth` does; image tag uses `.health-detail` (monospace tabular-nums per UI-SPEC).

---

### `internal/dashboard/assets/dashboard.{html,css}` — likely ZERO structural / ZERO new CSS

**HTML:** `#health-panel`/`#health-rows` already exist (UI-SPEC; rows are JS-appended). No structural change required (optional `<div id="backend-rows">` is executor discretion).

**CSS:** the readiness badge reuses **already-defined** classes — confirmed present: `.badge-ready/warn/down/unknown` (dashboard.css:165-168) backed by `--status-ready/warn/unknown` (dashboard.css:16-19); `.health-row`/`.health-detail` (dashboard.css:126-141); `.metric-value` tabular-nums (dashboard.css:182). **Add no new color, size, weight, or spacing token** (UI-SPEC locked). Only add a rule if an existing class genuinely cannot be composed.

---

### Golden + table tests — D-06 re-freeze discipline

**Analog:** `TestStatusJSONGolden` (status_test.go:214-244) and `TestJSONGolden` (detect_test.go:55) — the shared `*update` flag pattern (`var update = flag.Bool("update", ...)` defined ONCE in detect_test.go:13, shared across the package test binary).

**The -update mechanism to mirror** (status_test.go:226-243):
```go
golden := filepath.Join("testdata", "status.json.golden")
if *update {
	_ = os.WriteFile(golden, out.Bytes(), 0o644)
	return
}
want, _ := os.ReadFile(golden)
if !bytes.Equal(out.Bytes(), want) { t.Errorf(...) }
```

**Re-freeze commands** (run once each, review pure-addition diff):
```bash
go test ./cmd/villa/ -run TestStatusJSONGolden -update
go test ./cmd/villa/ -run TestRecommendJSONGolden -update   # confirmed name (recommend_test.go:39)
go test ./cmd/villa/                                          # full run, goldens must match
git diff cmd/villa/testdata/detect.golden.json               # MUST be empty (Pitfall 6)
```

**Stub analog for the new tok/s/readiness seams:** `newStatusDeps` (status_test.go:49-83) — add `GenTokensPerSec`/`ROCmReadiness` stub knobs the same way `Props`/`GTTUsed`/`DashboardHealth` are stubbed (generating→value, idle→nil, unavailable→nil; readiness fixture all-unset→unknown, all-good→ready, one-Known-bad→not-ready).

**Fixture analog for the readiness fold table:** `fixtureProfile` (detect_test.go:18-50) — note the existing `ROCmReadiness` block (detect_test.go:41-47) deliberately mixes Known + Unknown Bools; reuse that exact construction style for the fold's table cases.

**Recommend honesty assertion:** new table test mirroring `recommend_test.go` style — assert `rec.Backend == "vulkan"` regardless of advice value, and the Note contains "verify"/"bench" and NOT "faster"/"guaranteed".

---

## Shared Patterns

### Typed-Unknown / no-false-green (applies to: status tok/s, status readiness, recommend advice)
**Source:** `internal/detect/value.go:63-74` (`Bool{Known,Value}` + `UnknownBool`) and `internal/dashboard/api.go:47` (`*float64`+`omitempty`).
```go
// value.go:71-74 — the Known-first discipline every fold must honor:
func KnownBool(v bool, src string) Bool   { return Bool{Value: v, Known: true, Source: src} }
func UnknownBool(reason, raw string) Bool { return Bool{Known: false, Source: reason, Raw: raw} }
```
Check `.Known` BEFORE `.Value`; an unevaluable signal is `unknown`/omitted, NEVER a confident `not-ready`/`0.0`.

### Resolver, never literal (applies to: status backend/image, status residency markers)
**Source:** `internal/inference/inference.go:54-72` (`Backend` interface) + `cmd/villa/backend.go:249` (`backendShowEntry{Backend: backend.Name(), Image: backend.Image()}`).
```go
// backend.go:244-249 — the canonical "what's running" identity to mirror in status:
backend, err := inference.BackendFor(cfg.Backend)
...
entry := backendShowEntry{Backend: backend.Name(), Image: backend.Image()}
```
All backend identity flows through `BackendFor(cfg.Backend)`; `backend.Name()`/`Image()`/`ResidencyProof()` are values returned by the seam — using them is correct, pasting a literal is not.

### Append-only + one-time golden re-freeze (applies to: Report, Recommendation)
**Source:** `internal/detect/profile.go:53-60` (the Phase-7 precedent) + `cmd/villa/status_test.go:226-243` (the `-update` mechanism). New tagged fields at the tail, `SchemaVersion` last, golden regenerated once and reviewed as a pure-addition diff.

### Bounded reuse, no new collector (applies to: CLI tok/s)
**Source:** `internal/metrics/llamacpp.go` (`scrapeTimeout` 2s + `maxScrapeBody` 64 KiB io.LimitReader). The CLI reuse inherits the bounds — no new scraper, no new attack surface.

### Seam-gate discipline — CRITICAL (applies to: ALL surfacing code)
**Source:** `internal/inference/seam_test.go:34-44` (`TestSeamGrepGate` walks `internal/` AND `cmd/villa`).
**Gated literals that MUST NOT appear in status/recommend/detect-consumer/dashboard code:**
`kyuz0`, `docker.io/`, `server-vulkan`, `rocm-7.2.4`, `rocm7-nightlies`, `--device /dev/dri`, `--group-add`, `keep-groups`, and (per RESEARCH/UI-SPEC) the device/abort markers `ROCm0`, `HSA_OVERRIDE_GFX_VERSION`, `Memory access fault`.
**Allowed:** the bare word "ROCm"/"rocm" in a badge label or enum value; `backend.Name()` ("vulkan"/"rocm") and `backend.Image()` (the runtime string returned by the seam). Do NOT document a marker in a comment in the consuming layer (Pitfall 5).

---

## No Analog Found

| File | Reason |
|------|--------|
| — | None. Every file has a production in-repo analog. This phase is deliberately scheduled last so it composes finished primitives. If a task feels like new logic (new switching, new bench, new probe, auto-switch), it is scope creep — recheck the deferred list (BENCH-03, ROCM-ALT-01, USAGE-01). |

---

## Metadata

**Analog search scope:** `internal/status`, `internal/detect`, `internal/recommend`, `internal/metrics`, `internal/inference`, `internal/dashboard` (+ assets), `cmd/villa` (source + tests).
**Files read this session:** status.go, cmd status.go, profile.go, readiness_rocm.go, value.go, recommend.go, cmd recommend.go, backend.go, llamacpp.go, api.go, dashboard.js, dashboard.css (badge/health excerpts), inference.go (Backend interface), seam_test.go, detect_test.go, status_test.go.
**Pattern extraction date:** 2026-06-06
