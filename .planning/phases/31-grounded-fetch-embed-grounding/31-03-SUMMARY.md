---
phase: 31-grounded-fetch-embed-grounding
plan: 03
subsystem: orchestrate+cmd
tags: [websafe, owui-external-loader, quadlet, ssrf, bind-mount, seam, ground-01, ground-02, guard-01, byte-identical-off]

# Dependency graph
requires:
  - phase: 31-grounded-fetch-embed-grounding/01
    provides: "internal/websafe SafeClient/Loader/NewServer/Handler + the /load contract (wired into the serve cmd)"
  - phase: 31-grounded-fetch-embed-grounding/02
    provides: "recommend.WebSearchInputs + config WebsafeAddr/WebsafePort/WebLoaderSecret/HostVillaPath + GenerateWebLoaderSecret (consumed by render + cmd wiring)"
  - phase: 29-searxng-service
    provides: "searxng.go managed-service skeleton (image const + seam allowlist + secret-env-file + view/builder) cloned for websafe.go"
  - phase: 30-owui-native-search-wiring
    provides: "buildOpenWebUIView webSearchEnabled env block (extended append-only here)"
provides:
  - "internal/orchestrate villa-websafe managed-service render: WebsafeImage()/WebsafeContainerUnitName()/WebsafeSecretEnvFilePath()/RenderWebsafeSecretEnv + buildWebsafeView (host-binary :ro,z bind-mount, config-resolved DNS, no host port)"
  - "OWUI external-loader wiring: WEB_LOADER_ENGINE=external + config-composed EXTERNAL_WEB_LOADER_URL(/load) + BYPASS True->False + ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True + bearer via 0600 EnvironmentFile on the OWUI unit"
  - "RenderInput.HostVillaPath (os.Executable() threaded at all 7 production render sites)"
  - "hidden 'villa websafe-serve' cobra cmd (live SafeClient + container-env bearer) registered in the tree; villa-websafe.service derived automatically by serviceUnits"
  - "production recommend.Pick web-search ctx reservation wiring (webSearchInputsFrom/liveLoadedWebSearchInputs threaded into the production Pick sites)"
affects: [31-04-offload-assert-and-golden-freeze, 33-egress-verify, 34-surfacing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Managed-service image literal behind the orchestrate seam (isSeam allowlist extended SAME commit as the const)"
    - "Gated, append-only, byte-identical-off render (web-off identical to v1.4)"
    - "0600 secret via EnvironmentFile (bearer never in the 0644 unit) on BOTH the websafe AND the OWUI unit"
    - "Hidden serve subcommand + Deps func-field seam (return-not-Exit body, off-network tested)"
    - "Golden-freeze deferral (search-ON OWUI golden t.Skip-deferred to on-hardware Plan 04 confirmation)"

key-files:
  created:
    - internal/orchestrate/websafe.go
    - internal/orchestrate/quadlet/websafe.container.tmpl
    - internal/orchestrate/websafe_test.go
    - internal/orchestrate/testdata/villa-websafe.container.golden
    - cmd/villa/websafe.go
    - cmd/villa/websafe_test.go
  modified:
    - internal/orchestrate/render.go
    - internal/orchestrate/orchestrate.go
    - internal/orchestrate/openwebui.go
    - internal/orchestrate/quadlet/openwebui.container.tmpl
    - internal/orchestrate/openwebui_test.go
    - internal/orchestrate/render_test.go
    - internal/orchestrate/searxng_test.go
    - internal/inference/seam_test.go
    - cmd/villa/root.go
    - cmd/villa/lifecycle.go
    - cmd/villa/recommend.go
    - cmd/villa/install.go
    - cmd/villa/inference.go
    - cmd/villa/backend.go
    - cmd/villa/status.go
    - cmd/villa/dashboard.go
    - cmd/villa/model.go
    - cmd/villa/coding-mode.go
    - cmd/villa/doctor.go
    - cmd/villa/restore.go

decisions:
  - "Tasks 1 and 2 committed together (e9f0e3d): they share render.go/render_test.go/openwebui.go (the OWUI signature change is consumed by render.go's call site), so a per-task split would have produced a non-buildable intermediate commit. Committed as one atomic, buildable render-layer unit; Task 3 (cmd) committed separately (29db0bd)."
  - "Distroless image left as the gcr.io/distroless/static-debian12@sha256:RESOLVE_ON_HARDWARE placeholder per the plan; Plan 04 resolves the real RepoDigest on the dev box (after confirming `file ./villa` is static, CGO_ENABLED=0) before freezing the search-ON golden."
  - "Web-search REQUIRES memory-on (planner DECISION, option a): the RAG keys are NOT duplicated into the web-search block — they already render when both are on. The actionable refuse gate (web search without memory) is documented; enforcement in the install/opt-in path lands with the activation flow (Plan 04 / Phase 33) — see Deferred."
  - "The search-ON OWUI golden freeze is DEFERRED to Plan 04 (t.Skip with a clear message): ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS is the A1 HIGH-risk unknown that must be confirmed to exist + ground at the pinned digest on-hardware before the golden freezes. The drift test still binds every web-search key by construction now."
  - "Production Pick + RenderInput wiring done as a Rule 2/3 deviation (flagged by 31-02 SUMMARY + the orchestrator note): without it the GROUND-03 reservation never applies when web search is on and the bind-mount path is empty. Two deliberate weight-only probes stay zero-input (envelope-independent, golden-frozen)."

metrics:
  duration: ~55min
  completed: 2026-06-19
  tasks: 3
  files: 26
  commits: 2
status: complete
---

# Phase 31 Plan 03: Grounded-fetch render + OWUI external-loader wiring + hidden websafe-serve — Summary

**Wires the Plan-01 SSRF-guarded fetch core and the Plan-02 config fields into the running stack: renders a gated, seam-locked, byte-identical-off `villa-websafe` Quadlet unit (host binary bind-mounted read-only, exec `villa websafe-serve`, container-DNS only, no host port), points OWUI at it as the external web loader with BYPASS flipped False + the retrieval-fix key + a 0600 bearer EnvironmentFile, and adds the hidden `villa websafe-serve` subcommand serving the verified `/load` contract behind a live SafeClient — with the search-ON OWUI golden freeze deferred to Plan 04's on-hardware confirmation.**

## What was built

### Task 1+2 — orchestrate render layer (commit e9f0e3d)

- **`internal/orchestrate/websafe.go`** — cloned the searxng.go managed-service skeleton: `websafeImage` (digest-pinned distroless, `RESOLVE_ON_HARDWARE` placeholder) + `WebsafeImage()`; `websafeContainerUnitName` + `WebsafeContainerUnitName()` (install-flow fail-closed gate); the 0600 secret-env-file consts + `WebsafeSecretEnvFilePath()` + `websafeSecretEnvBody`; `websafeView` + `buildWebsafeView` (config-resolved DNS, `villa.network`, no PublishPort, host-binary `:ro,z` read-only bind-mount, fixed-token Exec) + `buildWebsafeExec` (`/usr/local/bin/villa websafe-serve --host 0.0.0.0 --port <port>`, no shell interpolation).
- **`quadlet/websafe.container.tmpl`** — the unit (EnvironmentFile + binary Volume + Exec; no PublishPort).
- **`render.go`** — append `villa-websafe` inside the existing `WebSearchEnabled` branch, STRICTLY after searxng, never mutating the shared slice; `RenderWebsafeSecretEnv` mirrors `RenderSearxngSecretEnv`. **`orchestrate.go`** — `RenderInput.HostVillaPath`.
- **`inference/seam_test.go`** — `orchestrate/websafe.go` added to the `isSeam` allowlist in the SAME commit as the image const (the distroless literal would otherwise trip the container-image regex). `TestSeamGrepGate` green.
- **`openwebui.go`** — `buildOpenWebUIView` gains `websafeAddr/websafePort`; appends `WEB_LOADER_ENGINE=external` + config-composed `EXTERNAL_WEB_LOADER_URL=http://<addr>:<port>/load`, flips `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` True→False, adds `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True`; carries the bearer via a gated 0600 `EnvironmentFile=` on the OWUI unit (`SecretEnvFile` view field + `{{if .SecretEnvFile}}` template guard → byte-identical-off). **`openwebui.container.tmpl`** updated.
- **Tests** — `websafe_test.go` (golden + seam-locked image + config-driven DNS/port + no-secret-leak + no-host-port + EnvironmentFile contract + secret-env render); `openwebui_test.go` (external-loader keys + config-driven URL + bearer-via-EnvironmentFile, all asserting search-OFF carries none); `searxng_test.go` fixture extended with the websafe fields + `HostVillaPath`; `TestRenderByteIdenticalWhenWebSearchOff` extended (web-on adds searxng THEN websafe; off = exactly the 5 v1.4 units); telemetry-frozen websearch-on case threads the websafe args. `villa-websafe.container.golden` frozen.

### Task 3 — hidden serve cmd + production wiring (commit 29db0bd)

- **`cmd/villa/websafe.go`** — `newWebsafe()` (Hidden cobra `websafe-serve`, `--host`/`--port`), `runWebsafe` (return-not-Exit; builds the `internal/websafe` Loader+Server, serves `Handler()` via the injected Serve seam), `liveWebsafeDeps` (live `SafeClient(DefaultBounds())` + bearer from the container env `EXTERNAL_WEB_LOADER_API_KEY` + `http.ListenAndServe`). Registered in `root.go`.
- **`cmd/villa/websafe_test.go`** — off-network via the Deps seam: verified OWUI contract round-trip ({urls} POST → `[{page_content, metadata{source,title}}]` 200 against an httptest upstream), Bearer enforced (401), serve-error → exitBlocked, Hidden + registered, and the lifecycle verify (`serviceUnits`/`managedServices` derive `villa-websafe.service` automatically — NO lifecycle.go edit needed).

## Tasks → commits

| Task | Name | Commit |
| ---- | ---- | ------ |
| 1+2 | villa-websafe render + OWUI external-loader env wiring (shared files; atomic render-layer unit) | e9f0e3d |
| 3 | hidden `villa websafe-serve` cmd + lifecycle verify + production Pick/HostVillaPath wiring | 29db0bd |

## Verification results

- `go test ./internal/orchestrate/ ./internal/inference/ ./cmd/villa/` — green (render goldens incl. the frozen websafe golden, the seam gate with the new allowlist entry, the cmd contract/auth/hidden/lifecycle tests).
- `go build ./...` — clean. `go test ./...` — all packages green. `make check` (vet + full suite) — green.
- Web-OFF render byte-identical to v1.4 — `TestRenderByteIdenticalWhenWebSearchOff` (5 units, no villa-websafe, no new OWUI keys, no OWUI EnvironmentFile).
- Seam leak grep `grep -rn 'villa-websafe@\|distroless' cmd/villa internal --include=*.go | grep -v 'orchestrate/websafe.go' | grep -v _test.go` → CLEAN (no image-literal leak outside the seam).
- The search-ON OWUI golden freeze is correctly DEFERRED to Plan 04 (`TestRenderOpenWebUIWebSearchContainerGolden` t.Skip); byte-identical-off goldens unchanged.

## Deviations from Plan

### Auto-fixed / scope follow-through

**1. [Rule 2 - Missing critical functionality] Production web-search reservation + bind-mount path wiring**
- **Found during:** Task 3 (and explicitly flagged by 31-02-SUMMARY "Next Phase Readiness" + the orchestrator's plan note).
- **Issue:** Plan 02 left all production `recommend.Pick` call sites passing zero-value `WebSearchInputs{}` (web-off) and the new `RenderInput.HostVillaPath` unthreaded at production render sites. Without this, enabling web search would NOT apply the GROUND-03 ctx reservation (silent CPU-fallback risk) and the villa-websafe bind-mount would render with an empty host path (broken unit).
- **Fix:** Added `webSearchInputsFrom(cfg)` + `liveLoadedWebSearchInputs()` (mirror `liveLoadedMemoryInputs`, fail-soft) and threaded them into the production Pick sites (recommend/inference/backend/status/dashboard/model/coding-mode/install). Added `hostVillaPath()` (`os.Executable()`) and threaded `HostVillaPath` into all 7 production RenderInput sites (install/lifecycle/model/backend/coding-mode/doctor/restore). The two deliberate weight-only probes (status `WeightBytes`, coding weight) were left zero-input by design (envelope-independent, golden-frozen).
- **Files modified:** cmd/villa/{recommend,inference,backend,status,dashboard,model,coding-mode,install,lifecycle,doctor,restore}.go
- **Verification:** web-off render byte-identical (no production path sets WebSearchEnabled, so the new fields/inputs are inert until config flips it); full suite + make check green.
- **Commit:** 29db0bd

**2. [Process] Tasks 1 and 2 committed as one atomic commit**
- **Found during:** commit planning.
- **Issue:** Tasks 1 and 2 share render.go, render_test.go, and openwebui.go (Task 2's `buildOpenWebUIView` signature change is consumed by Task 1's render.go call site), so a per-task split would have produced a non-buildable intermediate commit.
- **Fix:** Committed Tasks 1+2 together as one atomic, buildable render-layer unit (e9f0e3d) with a message documenting both; Task 3 committed separately (29db0bd).

## Known deferrals (by design, per plan)

- **Search-ON OWUI golden freeze → Plan 04:** `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS` is the A1 HIGH-risk unknown; must be confirmed to exist + actually ground at the pinned digest on-hardware before the golden freezes. The drift test (`TestRenderOpenWebUITelemetryFrozen` websearch-on) still binds every web-search key by construction now.
- **Distroless digest → Plan 04:** `RESOLVE_ON_HARDWARE` placeholder; Plan 04 resolves the RepoDigest on the dev box (after confirming the binary is static, CGO_ENABLED=0) and re-freezes the websafe golden.
- **"Web search requires memory-on" refuse gate enforcement:** the DECISION is documented (the RAG keys already render when both are on); wiring the actionable preflight/opt-in refuse message lands with the activation/opt-in flow (Plan 04 / Phase 33 PRIV-07/08/09), per the CONTEXT out-of-scope split.

## Self-Check: PASSED

- internal/orchestrate/websafe.go — FOUND
- internal/orchestrate/quadlet/websafe.container.tmpl — FOUND
- internal/orchestrate/testdata/villa-websafe.container.golden — FOUND
- cmd/villa/websafe.go — FOUND
- cmd/villa/websafe_test.go — FOUND
- commit e9f0e3d — FOUND
- commit 29db0bd — FOUND
