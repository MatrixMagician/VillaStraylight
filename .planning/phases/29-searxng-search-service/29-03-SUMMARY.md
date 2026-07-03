---
phase: 29-searxng-search-service
plan: 03
subsystem: cmd/villa (install flow)
tags: [searxng, web-search, readiness-proof, install, format-json, fixed-arg, fail-closed]
requires:
  - "29-01: SearXNGContainerUnitName(), SearXNGImage(), SearXNGSecretEnvFilePath(), RenderSearxngSettings, RenderSearxngSecretEnv, cfg.SearxngAddr/SearxngPort/SearxngSecret/WebSearchEnabled, config.GenerateSearxngSecret"
  - "29-02 (WAVE-MERGE): orchestrate.WriteSearxngSettings(name,text) error + orchestrate.WriteSearxngSecretEnv(name,text) error — referenced by install.go, resolve at wave merge"
provides:
  - "evalSearxngProof (pure verdict) + liveSearxngProof (real format=json query via runProbeCurl) + searxngProof/searxngProofInput/searxngResult types + searxngServiceName + liveLoadedWebSearchEnabled"
  - "install.go WebSearchEnabled-gated start + readiness-proof blocks (fail-closed, refuse-with-remediation)"
affects:
  - "cmd/villa/install.go (install flow), cmd/villa/install_test.go (fake Deps wiring)"
tech-stack:
  added: []
  patterns:
    - "Readiness-by-real-query (clone of memoryProof): pure eval*Proof verdict + injected probe, live*Proof seam reusing runProbeCurl over villa.network, install.go gating block"
    - "Bounded cold-start retry in the pure verdict core; live seam supplies the inter-retry delay"
key-files:
  created:
    - "cmd/villa/install_searxng.go"
    - "cmd/villa/install_searxng_test.go"
  modified:
    - "cmd/villa/install.go"
    - "cmd/villa/install_test.go"
decisions:
  - "Cold-start retry lives in the PURE evalSearxngProof (bounded, deterministic, fast in tests); liveSearxngProof supplies only the inter-attempt time.After delay + ctx cancellation — keeps the verdict unit-testable off-hardware while the live seam owns wall-clock waiting."
  - "PASS condition = parseable JSON with >=1 result OR number_of_results>0 (hasAnswer); an all-empty 200 (every engine timed out) is FAIL (Open Q2). A transient single-engine unresponsive_engines entry with a real result still PASSES (tolerated)."
  - "loadedWebSearchEnabled seam is NIL-SAFE (cfg.WebSearchEnabled only set when the seam is non-nil) so any pre-Phase-29 test double / wiring keeps a v1.4 byte-identical install (no start, no proof, no writes)."
metrics:
  duration: "~12m"
  completed: "2026-06-18"
  tasks: 2
  files: 4
status: complete
---

# Phase 29 Plan 03: SearXNG Readiness Proof + Install Wiring Summary

Real `format=json` SearXNG readiness proof (parses `results[]`, never a health-200) cloned from the shipped `memoryProof` seam, wired into the install flow gated on the persisted `WebSearchEnabled` — failing closed with remediation when the search service can't answer.

## What was built

**Task 1 — `evalSearxngProof` (pure) + `liveSearxngProof` (real query):** `cmd/villa/install_searxng.go` mirrors `install_memory.go`. `evalSearxngProof(probe)` is the PURE verdict core with a BOUNDED cold-start retry loop: it re-issues the injected probe up to `searxngProofRetries` (3) times, returns `StatusPass` as soon as a genuine answer arrives (`hasAnswer()` = `len(results)>0 || number_of_results>0`), and declares `StatusFail` only after the retries are exhausted — a probe error → FAIL naming `systemctl --user status villa-searxng.service` + re-run `villa install`; an all-empty parseable response → FAIL ("returned no results — every upstream engine may have timed out"). `liveSearxngProof(ctx, in)` reuses `runProbeCurl` VERBATIM (helper image from `orchestrate.EmbedImage()`, no re-typed literal) to issue `GET /search` with FIXED args `-sf -G <url> --data-urlencode q=<probe> --data-urlencode format=json` (no shell interpolation, no host port), `json.Unmarshal`s into `searxngResult`, and supplies the inter-retry `time.After` delay (with `ctx` cancellation). Readiness is the REAL query parsing `results[]` — there is NO `/healthz` path.

**Task 2 — install-flow wiring:** `cmd/villa/install.go` adds a `loadedWebSearchEnabled` gate seam (PERSISTED `config.LoadVilla().WebSearchEnabled`, fail-soft to false; `liveLoadedWebSearchEnabled`), plus four wired seams (`writeSearxngSettings`, `writeSearxngSecretEnv`, `searxngProofFn`). A `WebSearchEnabled`-gated START block (after the memory stack, independent of llama/qdrant): gates the start on `planHasUnit(SearXNGContainerUnitName())` — fail closed with an INTERNAL-ERROR remediation if absent (WR-04/T-29-13); generate-and-persists the secret once if empty (Pitfall 3, `config.GenerateSearxngSecret` + `saveConfig`); writes BOTH `settings.yml` (via `RenderSearxngSettings`→`WriteSearxngSettings`) and the 0600 secret env file (via `RenderSearxngSecretEnv`→`WriteSearxngSecretEnv`, the `EnvironmentFile=` target — secret never in the 0644 unit, T-29-02) BEFORE `start(searxngServiceName)`. A `WebSearchEnabled`-gated READINESS-PROOF block calls `searxngProofFn` and returns `exitBlocked` with `install: search service not ready: …` on FAIL, folding a PASS into the verdict.

## Verification

- `go test ./cmd/villa -run "TestSearxng|TestInstallWebSearch"`: **10 tests pass** (6 proof behaviors + 4 install-flow subtests).
- `go test ./cmd/villa/...` (with a local, uncommitted compile stub for the not-yet-merged 29-02 writers): **513 pass** — no regressions in the existing install/lifecycle suites.
- `TestSeamGrepGate` (internal/inference): **green** — no backend/image literal leaked (helper image sourced from `orchestrate.EmbedImage()`).
- `go vet ./cmd/villa/`: clean. `go fmt`: clean.

### Acceptance criteria

| Criterion | Status |
|-----------|--------|
| `evalSearxngProof` + `liveSearxngProof` + `searxngServiceName` present | PASS |
| Issues `format=json` query via `runProbeCurl`, reuses `orchestrate.EmbedImage()` | PASS |
| Fixed-arg `--data-urlencode` (no URL concatenation) | PASS |
| PASS requires >=1 result OR number_of_results>0; all-empty 200 is FAIL | PASS (test) |
| No health-200 path (`grep healthz\|/health` → only a "NEVER a /healthz" comment) | PASS |
| install gates start + proof on `cfg.WebSearchEnabled` and start on `SearXNGContainerUnitName()` presence | PASS |
| `searxngProofFn` Deps field wired to `liveSearxngProof` | PASS |
| Proof FAIL → exitBlocked w/ remediation; web-search-off → neither block runs | PASS (test) |
| settings.yml + 0600 secret env written before start; secret generated-and-persisted once | PASS (test) |

## Deviations from Plan

None — plan executed exactly as written. Both tasks implemented per the `<action>` specs; the readiness proof, the gating order (secret-generate → settings write → secret-env write → start → proof), and the fail-closed remediation all match the plan.

## Cross-wave dependency (NOT a deviation — by design)

`cmd/villa/install.go` references `orchestrate.WriteSearxngSettings(name, text string) error` and `orchestrate.WriteSearxngSecretEnv(name, text string) error`, which are **Plan 29-02 deliverables** (`internal/orchestrate/searxng_settings_write.go`) built concurrently in a sibling worktree and ABSENT from this worktree's base (29-03 and 29-02 are both wave 2, `depends_on: ["29-01"]`, scheduled in parallel because they touch disjoint files — `cmd/villa/install*.go` vs `internal/orchestrate/`). In isolation `go build ./cmd/villa` reports exactly those two `undefined` symbols and nothing else. The call sites were written against 29-02's documented signature; `go build` / `make check` go fully green at WAVE MERGE (per the plan's own `<verification>`: "`make check` green at wave merge"). To verify this plan's tests in isolation, a local `internal/orchestrate/zz_searxng_writer_stub_DO_NOT_COMMIT.go` providing the two signatures was used to compile, then DELETED before staging — it was never committed (confirmed: no untracked files, no `zz_searxng` in any commit).

## On-Hardware UAT Checkpoint (NOT auto-verified — do not mark complete without it)

The plan's `<verification>` includes a live-container round-trip that CANNOT run from this isolated worktree (it needs the full searxng stack rendered + started, which depends on 29-01/29-02/29-04 being merged and `villa install` run on the gfx1151 box). Per the run's on-hardware guidance, this is recorded as a UAT checkpoint, **not** a fabricated pass:

- [ ] **On-hardware UAT (29-VALIDATION.md Manual-Only):** after wave merge + `make build`, run `villa install` with web search enabled on the live gfx1151 host; confirm the install prints `search service ready: real format=json query returned N result(s)` (the real `liveSearxngProof` round-trip, N>=1), and `ss -ltnp` / `podman port villa-searxng` confirm NO host port is published (container-DNS only over villa.network).

No real-query PASS is claimed here — the live `format=json` round-trip was NOT executed in this run; only the proof LOGIC was implemented and fully unit-tested off-hardware via injected probes.

## Self-Check: PASSED

- FOUND: cmd/villa/install_searxng.go
- FOUND: cmd/villa/install_searxng_test.go
- FOUND commit 7a18274 (feat 29-03: readiness proof)
- FOUND commit 146a3c5 (feat 29-03: install wiring)
