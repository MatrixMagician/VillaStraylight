---
phase: 30-owui-native-search-wiring
plan: 02
subsystem: orchestrate
tags: [orchestrate, owui, web-search, searxng, env-only, byte-identical-off, golden, drift-test]
requires:
  - "Plan 01: VillaConfig.WebSearchResultCount int field (toml:web_search_result_count,omitzero, default 3)"
provides:
  - "buildOpenWebUIView extended with web-search inputs (webSearchEnabled, searxngAddr, searxngPort, webSearchResultCount)"
  - "WebSearchEnabled-gated OWUI native web-search env block (ENABLE_WEB_SEARCH=True, WEB_SEARCH_ENGINE=searxng, SEARXNG_QUERY_URL composed, WEB_SEARCH_RESULT_COUNT)"
  - "Single trailing ENABLE_PERSISTENT_CONFIG=False emit gated on memoryEnabled || webSearchEnabled (D-04 refactor)"
  - "villa-openwebui.container.websearch.golden (frozen search-on env block, the SC#1 frozen set)"
  - "SC#4 drift guard extended to web-search keys (TestRenderOpenWebUITelemetryFrozen websearch-on case)"
affects:
  - "Phase 31 (villa-websafe fetch path) and Phase 32 (guard layer) build on this native-wiring base; this plan adds NO guard/prompt/fetch path"
tech-stack:
  added: []
  patterns:
    - "Second WebSearchEnabled-gated env-append group in buildOpenWebUIView, independent of the memory group (append-only)"
    - "Single trailing ENABLE_PERSISTENT_CONFIG=False emit gated on memoryEnabled || webSearchEnabled"
    - "SEARXNG_QUERY_URL composed via fmt.Sprintf from config host:port (no re-typed villa-searxng/8080 literal — TestSeamGrepGate stays green)"
key-files:
  created:
    - internal/orchestrate/testdata/villa-openwebui.container.websearch.golden
  modified:
    - internal/orchestrate/openwebui.go
    - internal/orchestrate/render.go
    - internal/orchestrate/render_test.go
    - internal/orchestrate/searxng_test.go
decisions:
  - "D-01: web-search inputs threaded as plain extra params (matches the existing memoryEnabled bool flag style, simpler than a struct)"
  - "D-04: ENABLE_PERSISTENT_CONFIG=False refactored OUT of the memory block to a single trailing emit gated on memory||websearch (load-bearing for both ConfigVar groups)"
  - "Open Question 1 RESOLVED: memory-on golden stays byte-identical after the single-emit refactor (line position unchanged) — NOT re-frozen"
metrics:
  duration: ~4m (autonomous tasks only; on-hardware UAT pending)
  completed: 2026-06-18
  tasks: 2 of 3 (Task 3 is a blocking on-hardware UAT — PENDING human verification)
  files: 5
requirements: [SRCH-02, SRCH-03]
status: awaiting-human-verification
---

# Phase 30 Plan 02: OWUI Native Search Wiring Summary

> **STATUS: autonomous portion COMPLETE; on-hardware UAT (Task 3) PENDING human
> verification.** Tasks 1 & 2 (code, golden, drift/byte-identical tests, `make
> check`) are done and committed. Task 3 is a `checkpoint:human-verify` blocking
> on-hardware UAT on the gfx1151 dev box and has NOT been performed or self-approved
> — SRCH-03 SC#2/SC#3 remain unverified until the operator signs off. Do NOT treat
> this plan as fully complete until that UAT passes.

Wired Open WebUI to the already-rendered (Phase 29) local SearXNG via OWUI's
**native** web search — env-only, behind the orchestrate seam — by extending
`buildOpenWebUIView` with a `WebSearchEnabled`-gated ordered env block (verified
key names at the pinned OWUI digest rev `02dc3e68`) and refactoring
`ENABLE_PERSISTENT_CONFIG=False` to a SINGLE trailing emit gated on
`memoryEnabled || webSearchEnabled`. Froze the result in a new search-on golden and
extended the bidirectional telemetry-frozen drift test with a web-search-on case.

## What Was Built

- **`internal/orchestrate/openwebui.go`** — `buildOpenWebUIView` signature extended
  with `webSearchEnabled bool, searxngAddr string, searxngPort int,
  webSearchResultCount int` (D-01: plain params, matching the existing
  `memoryEnabled` flag style). A new `if webSearchEnabled { ... }` block appends the
  four VERIFIED native keys in order — `ENABLE_WEB_SEARCH=True`,
  `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL=http://{addr}:{port}/search?q=<query>&format=json`
  (composed via `fmt.Sprintf` from the config-threaded host:port — NO re-typed
  literal), and `WEB_SEARCH_RESULT_COUNT={count}` (via `strconv.Itoa`). The
  `<query>` token is OWUI's literal placeholder, kept verbatim. Added `strconv`
  import.
- **Single-emit refactor (D-04, load-bearing)** — `ENABLE_PERSISTENT_CONFIG=False`
  was removed from inside the memory block and is now emitted exactly ONCE, LAST,
  under `if memoryEnabled || webSearchEnabled`. The file/func doc comments were
  rewritten to describe the single-emit form and the extension of the T-20-01
  rationale to the web-search ConfigVars. Deprecated names (the old `RAG_*` family)
  are described by concept only, not as literal env-key strings, so the
  byte-identical-off negative test's literals are not polluted.
- **`internal/orchestrate/render.go`** — the OWUI render call threads
  `in.Cfg.WebSearchEnabled / SearxngAddr / SearxngPort / WebSearchResultCount`. No
  new const (config is the single source of truth).
- **`internal/orchestrate/testdata/villa-openwebui.container.websearch.golden`** (new)
  — freezes the 11 unchanged base env lines + the 4 web-search keys + a single
  trailing `ENABLE_PERSISTENT_CONFIG=False`. The only deliberate re-freeze.
- **`internal/orchestrate/render_test.go`** — added the `websearch-on` third case to
  `TestRenderOpenWebUITelemetryFrozen` (the SC#4 drift guard binding every
  web-search key + the count check to `buildOpenWebUIView` output); added
  `TestRenderOpenWebUIWebSearchContainerGolden` (golden freeze),
  `TestRenderOpenWebUIWebSearchConfigDriven` (WR-01: custom addr+port surfaces in
  `SEARXNG_QUERY_URL`), and `TestRenderOpenWebUIPersistentConfigSingleEmit` (exactly
  one, last, for both web-on/memory-off and memory-on/web-off).
- **`internal/orchestrate/searxng_test.go`** — set `WebSearchResultCount: 3` in
  `searxngFixtureInput()` so the rendered unit matches the frozen golden and the
  drift-case literal `3` (Render does not normalize config; the fixture must carry
  the value explicitly).

## Tasks Completed

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Extend buildOpenWebUIView + single-emit ENABLE_PERSISTENT_CONFIG refactor; thread render.go | bd55051 | internal/orchestrate/openwebui.go, render.go (+ test compile-fix call sites in render_test.go, searxng_test.go) |
| 2 | Freeze websearch golden; drift + config-driven + single-emit tests | 73fc399 | internal/orchestrate/render_test.go, testdata/villa-openwebui.container.websearch.golden |
| 3 | **On-hardware UAT (checkpoint:human-verify, blocking)** | **PENDING** | — (not automatable off-hardware; SRCH-03 SC#2/SC#3) |

## Verification

- `go build ./internal/orchestrate/` — success.
- `go test ./internal/inference/ -run TestSeamGrepGate` — green (no host/image/device
  literal leaked; the SearXNG host:port is config-composed, which the gate does not match).
- `go vet ./internal/orchestrate/` — clean.
- `go test ./internal/orchestrate/ ./internal/config/` — green, including the new
  `TestRenderOpenWebUIWebSearchContainerGolden`, `TestRenderOpenWebUIWebSearchConfigDriven`,
  `TestRenderOpenWebUIPersistentConfigSingleEmit`, and the extended
  `TestRenderOpenWebUITelemetryFrozen` websearch-on case.
- `make check` (vet + full `go test ./...`) — green across all 24 packages.
- **Open Question 1 RESOLVED (render-and-diff, not blind -update):**
  `TestRenderOpenWebUIMemoryContainerGolden` (memory-on) and
  `TestRenderOpenWebUIContainerGolden` (memory-off) pass WITHOUT re-freezing; `git
  diff` confirms both `villa-openwebui.container.golden` and
  `villa-openwebui.container.memory.golden` are byte-identical (unchanged). The
  single-emit refactor leaves `ENABLE_PERSISTENT_CONFIG=False` last in the same
  position for memory-on. Only the new websearch golden was `-update`d.
- `TestRenderByteIdenticalWhenWebSearchOff` — green (5 units off / 6 on, searxng
  unit strictly appended); unaffected by the OWUI env change (it checks only unit
  count/order).

### Pitfall 5 audit (searxng_test.go)

Audited every test in `searxng_test.go` that renders `searxngFixtureInput()`:
`TestRenderSearxng`, `TestSearxngUnitNoSecretLeak`, `TestSearxngUnitNoPublishPort`,
and `TestRenderSearxngIsConfigDriven` all inspect the **villa-searxng** unit (not
the OWUI unit), and `TestRenderByteIdenticalWhenWebSearchOff` checks only unit
count/order. **None assert OWUI env text under that fixture**, so none broke from
the new OWUI web-search block. No stale-fixture expectation needed updating beyond
adding `WebSearchResultCount: 3` to the fixture itself (required so the rendered
unit matches the frozen golden / drift-case literal).

## On-Hardware UAT (Task 3) — PENDING

This is a **blocking `checkpoint:human-verify`** on the gfx1151 dev box. It was NOT
performed or self-approved. SRCH-03 SC#2 (per-query native toggle gates search) and
SC#3 (honest no-results, no fabricated citation) remain UNVERIFIED until the
operator signs off. Steps the operator must run:

1. `make build`, then install/reconfigure with `web_search_enabled=true` in
   config.toml (or the opt-in path), then `systemctl --user restart
   villa-openwebui.service` so the regenerated unit's env takes effect.
2. Confirm env reached the container:
   `podman inspect villa-openwebui --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -E 'WEB_SEARCH|ENABLE_PERSISTENT_CONFIG'`
   — expect `ENABLE_WEB_SEARCH=True`, `WEB_SEARCH_ENGINE=searxng`, a
   `SEARXNG_QUERY_URL` pointing at `villa-searxng:8080`, `WEB_SEARCH_RESULT_COUNT=3`,
   and exactly one `ENABLE_PERSISTENT_CONFIG=False`.
3. At http://127.0.0.1:3000: confirm the native per-session Web Search toggle
   appears; toggle ON for a current-event query and confirm SearXNG-sourced results
   ground the reply; toggle OFF and confirm no search runs (SRCH-03 / SC#2).
4. With web search ON, ask an obscure zero-result query and confirm the model does
   NOT fabricate a cited answer (OWUI's native search→embed→retrieve→inject path; no
   villa guard — SRCH-03 / SC#3).

## Deviations from Plan

None — plan executed exactly as written for the autonomous portion. The only
implementation choices were the plan-sanctioned discretion points:

- **D-01 input style:** plain extra params (not a `webSearchRenderInput` struct) —
  the plan explicitly permits either; plain params match the existing
  `memoryEnabled` flag and are simplest.
- **Fixture field placement (test infra, not a deviation):** added
  `WebSearchResultCount: 3` to `searxngFixtureInput()`. `Render` does not call
  `normalizeVilla`, so the fixture must carry the default explicitly for the
  rendered unit to show `WEB_SEARCH_RESULT_COUNT=3` and match both the golden and
  the drift-case literal `3`. This is the established fixture pattern (the memory
  fixture likewise populates its config explicitly).

## Known Stubs

None.

## Threat Flags

None — no new network/auth/file surface beyond what the plan's `<threat_model>`
anticipated. The web-search env appends ONLY when `WebSearchEnabled`; off-render is
byte-identical to v1.4 (proven). `SEARXNG_QUERY_URL` carries no credential (composed
from config host:port only); the SearXNG secret stays in the 0600 `searxng.env`
(Phase-29), never touched here. URL composed via `fmt.Sprintf` — no shell
interpolation; `<query>` is OWUI's literal placeholder, never interpolated by villa.

## Self-Check: PASSED

- FOUND: internal/orchestrate/openwebui.go (modified — web-search block + single-emit)
- FOUND: internal/orchestrate/render.go (modified — threads 4 inputs)
- FOUND: internal/orchestrate/render_test.go (modified — drift + 3 new tests)
- FOUND: internal/orchestrate/searxng_test.go (modified — fixture WebSearchResultCount:3)
- FOUND: internal/orchestrate/testdata/villa-openwebui.container.websearch.golden (created)
- FOUND commit: bd55051 (feat — Task 1)
- FOUND commit: 73fc399 (test — Task 2)
