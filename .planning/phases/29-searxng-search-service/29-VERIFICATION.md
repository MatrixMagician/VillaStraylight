---
phase: 29-searxng-search-service
verified: 2026-06-18T00:00:00Z
status: human_needed
score: 5/6 must-haves verified
behavior_unverified: 1
overrides_applied: 0
behavior_unverified_items:
  - truth: "A real format=json query against a running villa-searxng returns parseable JSON results[] (SC#2 live readiness — readiness is the actual query, never a health-200)"
    test: "On the live gfx1151 host, after wave merge + `make build`, set web_search_enabled=true and run `villa install`. Observe the readiness step."
    expected: "Install prints `search service ready: real format=json query returned N result(s)` with N>=1 (the live liveSearxngProof round-trip over villa.network). `ss -ltnp` and `podman port villa-searxng` confirm NO host port is published (container-DNS only). A FAIL must print the refuse-with-remediation message and a non-zero exit, never a false-green."
    why_human: "The live container round-trip cannot run from an isolated worktree — it requires the full searxng stack rendered + started via `villa install` on the gfx1151 host. The proof LOGIC (evalSearxngProof / liveSearxngProof via the runProbeCurl podman seam) is present and fully unit-tested off-hardware via injected probes, but the end-to-end query against a live engine was deliberately not executed and was recorded by the executor as an explicit on-hardware UAT checkpoint, not a fabricated pass."
human_verification:
  - test: "On the live gfx1151 host, set web_search_enabled=true and run `villa install`; observe the SearXNG readiness step and the published-port surface."
    expected: "Prints `search service ready: real format=json query returned N result(s)` (N>=1) from the live format=json round-trip; `ss -ltnp` / `podman port villa-searxng` show NO host port (container-DNS-only on villa.network). FAIL path refuses-with-remediation and exits non-zero, never false-green."
    why_human: "Live container round-trip; needs the stack up on real hardware. Proof logic is implemented and unit-tested off-hardware, but the live query was not executed."
---

# Phase 29: SearXNG Search Service Verification Report

**Phase Goal:** The operator has a local SearXNG metasearch service running as a managed Quadlet unit on `villa.network`, returning parseable JSON results from a bounded, auditable set of upstream engines — the premise that nothing grounds without.
**Verified:** 2026-06-18
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SC#1 — `villa-searxng.container` comes up on `villa.network`, container-DNS-only, no host port, digest-pinned behind a seam-locked const | ✓ VERIFIED | Golden `villa-searxng.container.golden`: `Network=villa.network`, `ContainerName=villa-searxng`, no `PublishPort`. Image `ghcr.io/searxng/searxng@sha256:ed29454…` pinned as `const searxngImage` in `internal/orchestrate/searxng.go:40`, allowlisted in `internal/inference/seam_test.go:134`. Tests `TestSearxngUnitNoPublishPort`, `TestSearxngUnitNoSecretLeak`, `TestSeamGrepGate` PASS. `.volume` clause DECIDED N/A by design (stateless private JSON instance: limiter:false, image_proxy:false, no valkey — settings supplied via read-only bind; documented `29-01-PLAN.md` `<plan_decisions>`). |
| 2 | SC#2a — `settings.yml` rendered from config with `search.formats:[html,json]`, generated `secret_key` (via env, not in 0644 file), `limiter:false` | ✓ VERIFIED | `searxng-settings.yml.golden`: `formats:[html,json]`, `limiter:false`, `image_proxy:false`, `secret_key:""` (live value via `$SEARXNG_SECRET` from 0600 EnvironmentFile). `GenerateSearxngSecret` uses `crypto/rand` (`villaconfig.go:353`). Tests `TestRenderSearxngSettings`, `TestRenderSearxngSecretEnv`, `TestGenerateSearxngSecretUsesCryptoRand` PASS. |
| 3 | SC#2b — a real `format=json` query returns parseable JSON results (readiness is the actual query, never a health-200) | ⚠️ PRESENT_BEHAVIOR_UNVERIFIED | Proof LOGIC present + fully unit-tested: `evalSearxngProof` (pure verdict, fail-closed on empty/unreachable) + `liveSearxngProof` (real `GET /search?…&format=json` via `runProbeCurl` podman seam over villa.network, fixed-arg `--data-urlencode`, cold-start retry). Tests `TestSearxngProofPassOnRealResults`, `…FailOnUnreachable`, `…FailOnAllEmpty`, `…ToleratesTransientEngineFailure`, `…ColdStartRetry`, `…RetryGivesUp` PASS. The LIVE round-trip against a running container was NOT executed (requires `villa install` on gfx1151 host) — routed to human verification. |
| 4 | SC#3 — vetted SUBSET of upstream engines (bounded, auditable), not the full default set (SRCH-04) | ✓ VERIFIED | `use_default_settings.engines.keep_only` = `[duckduckgo, brave, wikipedia, wikidata]` (single-source `searxngEngines` slice, `searxng.go:95`). Golden confirms the bounded 4-engine keep_only. Test `TestSearxngEngineAllowlist` PASS. |
| 5 | SC#4 — with web search OFF, the rest of the stack renders byte-identical to v1.4 (additive + gated) | ✓ VERIFIED | Render gated on `in.Cfg.WebSearchEnabled` (`render.go:222`); when false no searxng unit is appended. 13 pre-existing goldens unchanged (15 total = 13 + 2 new). Test `TestRenderByteIdenticalWhenWebSearchOff` PASS; config self-heals only endpoint fields, never the bool (`TestWebSearchEnabledNotSelfHealed`). |
| 6 | Secret never lands in the 0644 unit — injected via `EnvironmentFile=` pointing at a 0600 env file | ✓ VERIFIED | Unit references `EnvironmentFile=%h/.config/villa/searxng/searxng.env`, no inline secret literal (`searxng.container.tmpl`). Writers `WriteSearxngSecretEnv`/`WriteSearxngSettings` write atomically (temp→fsync→rename) at mode 0600, dir 0700, traversal-guarded (`assertInsideDir`). Tests `TestWriteSearxngFilesMode`, `TestWriteSearxngTraversalRefused`, `TestSearxngUnitNoSecretLeak` PASS. |

**Score:** 5/6 truths verified (1 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/orchestrate/searxng.go` | image const + views + builders + unit/path names (164 lines) | ✓ VERIFIED | `searxngImage` const (digest-pinned), `SearXNGImage()`, `buildSearxngView`, `buildSettingsYml`, `SearxngEngines()`, secret-env helpers. Image literal seam-locked. |
| `internal/orchestrate/quadlet/searxng.container.tmpl` | `EnvironmentFile=` (no inline secret), settings mount, no host port | ✓ VERIFIED | Renders `EnvironmentFile={{.SecretEnvFile}}`, `Volume={{.SettingsMount}}` (`:ro,Z`), no PublishPort. |
| `internal/orchestrate/quadlet/searxng-settings.yml.tmpl` | formats/limiter/image_proxy + engine keep_only | ✓ VERIFIED | `keep_only` ranges `.Engines`; `formats:[html,json]`, `limiter:false`, `image_proxy:false`, `secret_key:""`. |
| `internal/config/villaconfig.go` | WebSearchEnabled gate + Searxng* fields + crypto-rand secret + normalize | ✓ VERIFIED | Gate + `SearxngAddr/Port/Secret`, defaults (villa-searxng:8080), normalize zeroes fields when off, `GenerateSearxngSecret` via crypto/rand. |
| `internal/orchestrate/searxng_settings_write.go` | atomic 0600 traversal-guarded writers (148 lines) | ✓ VERIFIED | `WriteSearxngSettings`/`WriteSearxngSecretEnv` + shared `writeSearxngFile` + `atomicWriteMode`; dir 0700, file 0600; `assertInsideDir` reused. |
| `cmd/villa/install_searxng.go` | evalSearxngProof + liveSearxngProof (198 lines) | ✓ VERIFIED | Pure verdict + live `format=json` seam over villa.network, fixed-arg curl, bounded cold-start retry, fail-closed. |
| `internal/orchestrate/testdata/villa-searxng.container.golden` | byte-frozen unit | ✓ VERIFIED | Present, matches render. |
| `internal/orchestrate/testdata/searxng-settings.yml.golden` | byte-frozen settings | ✓ VERIFIED | Present, matches render. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `render.go` | `searxng.go` | `WebSearchEnabled`-gated branch calling `buildSearxngView` | ✓ WIRED | `render.go:222` gates append on `in.Cfg.WebSearchEnabled`. |
| `searxng.go` | `villaconfig.go` | searxng name/port/secret from resolved config (WR-01) | ✓ WIRED | `buildSearxngView(in.Cfg.SearxngAddr)`; identity derives from config, not a local const. |
| `seam_test.go` | `searxng.go` | isSeam allowlist extended | ✓ WIRED | `rel == "orchestrate/searxng.go"` (line 134); `TestSeamGrepGate` PASS. |
| `searxng_settings_write.go` | `reconcile.go` | reuses `assertInsideDir` + atomic-write discipline | ✓ WIRED | `assertInsideDir(target, dir)` + `atomicWriteMode` mirror reconcile.go. |
| `searxng_settings_write.go` | `searxng.go` | writes bytes from `RenderSearxngSettings`/`RenderSearxngSecretEnv` (single source) | ✓ WIRED | Writers persist the pure-render output verbatim. |
| `install.go` | `install_searxng.go` | `WebSearchEnabled`-gated start + plan-presence + `searxngProofFn` | ✓ WIRED | `install.go:787` gate + `planHasUnit(SearXNGContainerUnitName())` before start; proof at `:877`, refuse-with-remediation on FAIL. |
| `install.go` | `searxng_settings_write.go` | writes settings.yml + 0600 secret env before start | ✓ WIRED | `WriteSearxngSettings`/`WriteSearxngSecretEnv` invoked before `start(searxngServiceName)`. |
| `install_searxng.go` | `install_memory.go` | reuses `runProbeCurl` + `EmbedImage()` | ✓ WIRED | Helper image from `orchestrate.EmbedImage()` (no re-typed literal). |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Affected-package test suite | `go test ./internal/orchestrate/ ./internal/config/ ./cmd/villa/ ./internal/inference/` | 702 passed (4 packages) | ✓ PASS |
| SC#2 proof logic + install wiring | `go test ./cmd/villa/ -run 'TestSearxngProof\|TestInstallWebSearchWiring'` | 11 passed | ✓ PASS |
| SC#1/SC#3/SC#4 render invariants | `go test ./internal/orchestrate/ -run 'TestRenderByteIdenticalWhenWebSearchOff\|TestSearxngEngineAllowlist\|TestSearxngUnitNoPublishPort\|TestSearxngUnitNoSecretLeak'` | 4 passed | ✓ PASS |
| SC#2 live `format=json` round-trip against running container | `villa install` (web search on) on gfx1151 host | not runnable from worktree | ? SKIP → human verification |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| SRCH-01 | 29-01, 29-02, 29-03 | SearXNG Quadlet unit on villa.network (container-DNS, no host port, digest-pinned), settings.yml from config, readiness via real format=json query | ✓ SATISFIED (live-query readiness pending on-hardware UAT) | Truths 1,2,4,6 VERIFIED; truth 3 (live query) PRESENT_BEHAVIOR_UNVERIFIED → human. REQUIREMENTS.md marks SRCH-01 Pending — consistent with the outstanding on-hardware readiness confirmation. |
| SRCH-04 | 29-01 | Vetted subset of upstream engines, not the full default set | ✓ SATISFIED | Truth/SC#3 VERIFIED — `keep_only` 4-engine allowlist. REQUIREMENTS.md marks SRCH-04 Complete. |

Both PLAN-declared requirement IDs (SRCH-01, SRCH-04) are accounted for. No orphaned requirements: REQUIREMENTS.md maps only SRCH-01 + SRCH-04 to Phase 29 (SRCH-02/03 are Phase 30).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | No unreferenced TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER in any modified file | — | Clean |

Code review (`29-REVIEW.md`): 0 critical, 5 warning, 4 info. All hard invariants traced PASS. The 5 warnings are non-blocking robustness/forward-dependency notes (see Forward Dependencies below); none contradict a Phase 29 success criterion.

### Forward Dependencies (noted, NOT Phase 29 gaps)

- **WR-01 — No opt-in path enables web search yet.** `WebSearchEnabled` is never set true in this phase. This is correct-by-design: SC#4 requires the web-search-OFF render to be byte-identical to v1.4, so an off-by-default gate is the contract. The enable wiring (OWUI native web search) is SRCH-02 → Phase 30; the explicit opt-in (PRIV-07) is Phase 33. Recorded so it is not lost.
- **WR-02 — `secret_key:""` relies on the container entrypoint substituting `$SEARXNG_SECRET`.** This is the very behavior the on-hardware live-query UAT confirms; not separately actionable in this phase.

### Human Verification Required

#### 1. On-hardware SearXNG live readiness (SC#2 live `format=json` round-trip)

**Test:** On the live gfx1151 host, after wave merge + `make build`, set `web_search_enabled=true` and run `villa install`.
**Expected:** Readiness step prints `search service ready: real format=json query returned N result(s)` with N>=1 (the live `liveSearxngProof` round-trip over villa.network). `ss -ltnp` and `podman port villa-searxng` confirm NO host port is published (container-DNS only). A failure path must refuse-with-remediation and exit non-zero — never a false-green.
**Why human:** The live container round-trip cannot run from an isolated worktree; it needs the full stack rendered + started via `villa install` on real hardware. The proof logic is present and fully unit-tested off-hardware via injected probes; only the live query is outstanding. Recorded by the executor as an explicit on-hardware UAT checkpoint (`29-03-SUMMARY.md`), not a fabricated pass.

### Gaps Summary

No gaps. All four ROADMAP success criteria are achieved in the codebase at the render/config/wiring/proof-logic level, with comprehensive byte-frozen goldens and behavioral tests (702 + 11 + 4 PASS). The sole outstanding item is the SC#2 LIVE `format=json` query round-trip, which is real-but-pending on-hardware UAT — the proof code itself is present, wired, and unit-tested fail-closed. Per the project's "offload-asserting, never liveness" principle, this live round-trip is the genuine readiness signal and must be confirmed on the gfx1151 host before SRCH-01 flips to Complete. The `.volume` clause of SC#1 is deliberately satisfied as intentionally-N/A (a stateless private JSON-only instance needs no writable volume), documented in the plan — not an omission.

---

_Verified: 2026-06-18_
_Verifier: Claude (gsd-verifier)_
