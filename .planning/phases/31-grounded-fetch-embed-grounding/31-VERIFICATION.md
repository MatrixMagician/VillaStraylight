---
phase: 31-grounded-fetch-embed-grounding
verified: 2026-06-19T00:00:00Z
status: passed
score: 5/5 must-haves verified
behavior_unverified: 0
overrides_applied: 0
residuals: # Non-blocking, recorded honestly (carried from 31-UAT.md)
  - "Clean-replace cadence of open-webui_web-search not independently re-verified (OWUI-managed; isolation from durable stores IS proven live). Candidate for a follow-up probe."
  - "Install idempotency: a units-unchanged re-install won't re-provision a deleted websafe.env (consistent with the shipped searxng pattern, Phase 29; normal single-pass opt-in unaffected)."
---

# Phase 31: Grounded Fetch → Embed Grounding Verification Report

**Phase Goal:** With search on, the operator asks a current-events/research question and gets an answer grounded in fetched pages with inline citations to live URLs — the fetch path established and resource-bounded (SSRF-guarded, ephemeral, ctx-reserved) before any guard policy is layered on.
**Verified:** 2026-06-19
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (5 ROADMAP Success Criteria = GROUND-01/02/03 + GUARD-01/05)

| # | Truth (SC / Requirement) | Status | Evidence |
|---|--------------------------|--------|----------|
| 1 | **GUARD-01 / SC#1** — villa-websafe is OWUI's `WEB_LOADER_ENGINE=external` fetch path and the sole producer of `page_content` | ✓ VERIFIED | `internal/websafe/loader.go` `HandleLoad` is the only `page_content` emitter; every byte passes `Loader.fetchOne` (incl. stubbed guard seam). OWUI env sets `WEB_LOADER_ENGINE=external` + `EXTERNAL_WEB_LOADER_URL` (`openwebui.go:252-253`). Tests `TestRenderOpenWebUIExternalLoaderWiring`, `TestLoadHappyPath` pass. **Live UAT:** `POST /load {example.com}` → 200, 1 item, `metadata.source=https://example.com`, `page_content` len 284. |
| 2 | **GROUND-01 / SC#2** — search-on research question returns an answer grounded in fetched pages with inline citations to live URLs (v1.3 RAG + `sources` reused verbatim) | ✓ VERIFIED (behavioral, on-hardware) | State/runtime-dependent → proven by **live UAT**: Q "latest Podman version" → "Podman 5.8.3, released June 12 2026" + CVE-2026-44517 (both AFTER the model's Jan-2026 cutoff ⇒ necessarily grounded), inline cites `[2,4]`, `sources` carries live URLs (github releases, podman.io, versionlog.com). BYPASS=False + retrieval-fix key confirmed at pinned OWUI digest (commit e347850, 4bdd822). |
| 3 | **GROUND-02 / SC#3** — fetched content in a dedicated ephemeral collection, never the durable memory/document-KB store | ✓ VERIFIED (isolation proven on-hardware) | **Live UAT:** after query Qdrant shows `open-webui_web-search`=34 pts, distinct from `open-webui_memories`=0, `open-webui_knowledge`=15, `open-webui_files`=12 — web content never entered the durable stores. Clean-replace cadence is OWUI-managed (villa adds no Qdrant calls, Pitfall 2) — recorded as a non-blocking residual; the GROUND-02 isolation bar is met. |
| 4 | **GROUND-03 / SC#4** — recommend reserves a web-search ctx budget BEFORE the chat fit; residency offload-asserted under search load (silent/partial CPU fallback = FAIL) | ✓ VERIFIED | `recommend.Pick` subtracts `webRes` from the envelope BEFORE `pickBest/pickOverride` via saturating add (`recommend.go:241-246`); schema 3→4 (`recommendSchemaVersion=4`), `WebSearchReservationBytes` append-only. Tests `TestPickCoderUsesPostReservationEnvelope`, `TestWebSearchReservation`, `TestPickWebSearchReservation` pass; golden `cmd/villa/testdata/recommend.golden.json` re-frozen with `web_search_reservation_bytes`. **Live UAT:** `villa status` under in-flight web-search query → `villa-llama.service active ready OFFLOAD PASS` (rocm), no CPU fallback. |
| 5 | **GUARD-05 / SC#5** — SSRF guard: resolve-and-validate target IP (reject loopback/link-local/169.254.169.254/internal villa-* hosts), re-check after every redirect, http(s) scheme allowlist | ✓ VERIFIED | `internal/websafe/ssrf.go`: connect-time `net.Dialer.Control` validates the CONNECTED IP (defeats DNS-rebinding TOCTOU), per-hop `CheckRedirect` (cap ≤5 + scheme + host re-check), comprehensive `rejectPrefixes` + `ipRejected`/`hostRejected`. Tests `TestSSRFRejectSet`, `TestControlConnectTime`, `TestRedirectRevalidation` (3 subtests) pass. **Live UAT:** `POST /load` with `169.254.169.254` + `villa-qdrant:6333` → 200, **0 items** (both rejected, skip-and-continue). |

**Score:** 5/5 truths verified (0 present, behavior-unverified). The two behavior-dependent truths (GROUND-01 grounding, GROUND-03 offload-under-load) are proven by recorded on-hardware UAT evidence rather than presence alone.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/websafe/ssrf.go` | SSRF reject-set + connect-time Control + per-hop CheckRedirect + SafeClient | ✓ VERIFIED | `control`, `ipRejected`, `hostRejected`, `SafeClient`, `rejectPrefixes`, `DefaultBounds` all present + substantive (167 lines); wired into `SafeClient` consumed by cmd tier. |
| `internal/websafe/websafe.go` | Loader core + Deps func-field seam + fetchOne (scheme allowlist, LimitReader, timeout, bounded concurrency, skip-and-continue) | ✓ VERIFIED | `type Deps`, `Loader`, `fetchOne`, bounded-concurrency `Load` (204 lines). Pure core, client injected. |
| `internal/websafe/loader.go` | OWUI external-loader contract types + handler (Bearer auth, always-200 partial array) | ✓ VERIFIED | `LoadRequest`/`LoadResponse` (`page_content`/`metadata`), `Server`, `HandleLoad`, constant-time `authOK`. Always-200 + skip-and-continue confirmed by `TestLoaderAlways200`. |
| `internal/websafe/guard_stubs.go` | Phase-32 pass-through hooks (sanitize/normalize/fence/classify) | ✓ VERIFIED | Identity stubs present + documented; seam exists, no policy claimed (correct posture). |
| `internal/recommend/recommend.go` | WebSearchInputs + webSearchReservation + WebSearchReservationBytes (schema 3→4) | ✓ VERIFIED | All symbols present; reservation subtracted before fit. |
| `internal/config/villaconfig.go` | WebsafeAddr/Port/WebLoaderSecret/HostVillaPath + GenerateWebLoaderSecret + omit-when-off + addr/port self-heal (secret/path NOT self-healed) | ✓ VERIFIED | Fields present with `omitempty`/`omitzero`; `GenerateWebLoaderSecret` uses crypto/rand; off-zeroing at 413-416; secret + host path explicitly excluded from self-heal. |
| `internal/orchestrate/websafe.go` | websafeImage const + accessor + websafeView/buildWebsafeView/buildWebsafeExec | ✓ VERIFIED | Distroless digest `@sha256:b669b9df…89400` pinned (placeholder replaced, matches UAT); image literal behind orchestrate seam. |
| `internal/orchestrate/quadlet/websafe.container.tmpl` | villa-websafe unit (bind-mount + Exec, no PublishPort) | ✓ VERIFIED | No `PublishPort` (PRIV-01), `Volume` bind-mount, `Exec`, `EnvironmentFile` (0600 secret). |
| `cmd/villa/websafe.go` | hidden `villa websafe-serve` cmd + runWebsafe + liveWebsafeDeps (SafeClient wired) | ✓ VERIFIED | `Hidden: true`, `websafe.NewServer`, `SafeClient(DefaultBounds())` wired; registered in cobra tree. |
| `.planning/phases/31-.../31-UAT.md` | on-hardware UAT evidence record | ✓ VERIFIED | All SCs PASS, A1 confirmed, dated 2026-06-19 on gfx1151. |

### Key Link Verification

| From | To | Via | Status |
|------|----|----|--------|
| `websafe.go` Loader | `ssrf.go` SafeClient | injected `net.Dialer.Control` + `CheckRedirect` client | ✓ WIRED — `liveWebsafeDeps` injects `websafe.SafeClient(DefaultBounds())`. |
| `loader.go` handler | `websafe.go` `.Load` | maps `[]Page` → `[{page_content, metadata{source,title}}]` | ✓ WIRED — `HandleLoad` calls `s.loader.Load`. |
| `recommend.go` Pick | `webSearchReservation` | envelope shrinks by reservation+webRes before fit (saturating) | ✓ WIRED — `recommend.go:241-246`, no uint64 wrap. |
| `config.go` marshalVilla | new web fields | omit-when-off zeroing (byte-identical-off) | ✓ WIRED — fields zeroed when WebSearchEnabled false (413-416). |
| `render.go` | `websafe.go` buildWebsafeView | render unit inside WebSearchEnabled branch, threading HostVillaPath + WebsafeAddr/Port | ✓ WIRED — `render.go:222,240`; off-render emits no villa-websafe unit (`searxng_test.go:233`). |
| `openwebui.go` buildOpenWebUIView | cfg.WebsafeAddr/Port | EXTERNAL_WEB_LOADER_URL composed via fmt.Sprintf (no re-typed literal) | ✓ WIRED — `openwebui.go:252-253`; `TestRenderOpenWebUIExternalLoaderURLConfigDriven` passes. |
| `cmd/villa/websafe.go` liveWebsafeDeps | websafe SafeClient + Server | live Control client into core, serves HandleLoad | ✓ WIRED. |
| `orchestrate/websafe.go` websafeImage | dev-box distroless RepoDigest | placeholder replaced with real @sha256 before golden freeze | ✓ WIRED — digest matches UAT record. |
| `openwebui.go` retrieval-fix key | OWUI config.py at pinned digest | ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS confirmed present + grounding | ✓ WIRED — `openwebui.go:270`; A1 confirmed live at env.py:688 / config.py:1541. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full build | `go build ./...` | success | ✓ PASS |
| Full suite (single run) | `go test ./...` | no failures (`make check` exit 0) | ✓ PASS |
| Inference seam gate | `go test ./internal/inference -run TestSeamGrepGate` | ok | ✓ PASS |
| SSRF + loader named tests | `go test ./internal/websafe -run 'TestSSRF\|TestControl\|TestRedirect\|TestLoad...'` | all PASS | ✓ PASS |
| Reservation-before-fit | `go test ./internal/recommend -run 'WebSearch\|Reservation'` | all PASS | ✓ PASS |
| Install secret-env writer | `go test ./cmd/villa -run TestInstallWebSearchWiring` | PASS | ✓ PASS |
| Byte-identical-off render | `TestRenderByteIdenticalWhenMemoryOff` + `searxng_test.go:233` off-assert | PASS | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| GUARD-01 | 31-01/03/04 | villa-websafe sole producer of page_content | ✓ SATISFIED | Truth #1 |
| GUARD-05 | 31-01 | SSRF guard (resolve-and-validate, redirect re-check, scheme allowlist) | ✓ SATISFIED | Truth #5 |
| GROUND-01 | 31-01/02/03/04 | Grounded answer with inline citations to live URLs | ✓ SATISFIED | Truth #2 |
| GROUND-02 | 31-02/03/04 | Dedicated ephemeral collection, never durable store | ✓ SATISFIED | Truth #3 |
| GROUND-03 | 31-02/03/04 | recommend ctx-reservation before fit + offload-assert | ✓ SATISFIED | Truth #4 |

No orphaned requirements: REQUIREMENTS.md maps exactly GROUND-01/02/03 + GUARD-01/05 to Phase 31, all claimed by plans and verified.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | No TBD/FIXME/XXX in any phase-modified file | — | None |

Guard stubs (`guard_stubs.go`) are identity pass-throughs — intentional Phase-32 seam (documented, no "injection-safe" claim made), not a stub anti-pattern.

### Human Verification Required

None. The two behavior-dependent truths (GROUND-01 grounding, GROUND-03 offload-under-load) that presence checks cannot prove were verified by the recorded on-hardware UAT (`31-UAT.md`, gfx1151, 2026-06-19), which is contemporaneous, command-level evidence — not a SUMMARY narrative.

### Residuals (non-blocking)

1. **Clean-replace cadence** of `open-webui_web-search` not independently re-verified (OWUI-managed; isolation from durable stores IS proven). Follow-up probe candidate.
2. **Install idempotency:** a units-unchanged re-install won't re-provision a deleted `websafe.env` (consistent with shipped searxng, Phase 29). A UAT-surfaced install gap (websafe secret-env not provisioned on the reconcile path) was found and FIXED (commits ca9909f, cf6806f); normal single-pass opt-in writes units + secret together.

### Gaps Summary

None. All 5 ROADMAP success criteria (GROUND-01/02/03, GUARD-01/05) are delivered, wired, and proven — pure-core SSRF + loader with passing behavioral tests, byte-identical-off render/golden discipline intact, seam gate green, `make check` green (1423+ tests), and the load-bearing runtime claims (grounded cited answer post-cutoff, ephemeral isolation, offload-under-load) confirmed on the live gfx1151 box. The two recorded residuals are non-blocking and consistent with shipped patterns.

---

_Verified: 2026-06-19_
_Verifier: Claude (gsd-verifier)_
