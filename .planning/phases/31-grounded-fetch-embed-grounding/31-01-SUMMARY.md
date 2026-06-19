---
phase: 31-grounded-fetch-embed-grounding
plan: 01
subsystem: internal/websafe
tags: [ssrf, fetch, owui-external-loader, page_content, pure-core, security]
requires:
  - "Go 1.26.2 stdlib (net, net/http, net/netip, crypto/subtle, context, io)"
provides:
  - "internal/websafe.SafeClient(Bounds) — SSRF-guarded *http.Client (connect-time Control + per-hop CheckRedirect)"
  - "internal/websafe.Loader/NewLoader/Load — bounded, skip-and-continue batch fetch core (sole producer of page_content)"
  - "internal/websafe.Deps{Client} — injected fetch seam (live client wired in Plan 03)"
  - "internal/websafe.Bounds/DefaultBounds — conservative per-fetch resource limits"
  - "internal/websafe.Server/NewServer/Handler/HandleLoad — verified OWUI external-loader HTTP contract on /load"
  - "internal/websafe.LoadRequest{urls}/LoadResponse{page_content,metadata} — verified contract types"
  - "internal/websafe guard seam (sanitize/normalize/fence/classify) — Phase-32 GUARD-02/03/04 stubs"
affects:
  - "Plan 31-03 (imports this core: liveWebsafeDeps wires SafeClient; cmd serves Handler() at the /load path)"
tech-stack:
  added: []
  patterns:
    - "pure core + injected Deps func-field seam (network injected; orchestrate stays the only impure module)"
    - "net.Dialer.Control connect-time IP validation (TOCTOU-safe SSRF; defeats DNS rebinding)"
    - "skip-and-continue batch + honest non-nil empty-on-all-fail (D-06 honesty)"
    - "always-200 partial array for the OWUI raise_for_status contract"
key-files:
  created:
    - internal/websafe/ssrf.go
    - internal/websafe/websafe.go
    - internal/websafe/guard_stubs.go
    - internal/websafe/loader.go
    - internal/websafe/ssrf_test.go
    - internal/websafe/websafe_test.go
  modified: []
decisions:
  - "Bounds + DefaultBounds live in ssrf.go (where SafeClient consumes them) since the SSRF client and the fetch core share them; DefaultBounds = 2 MiB / 10s / concurrency 4 / 5 redirects."
  - "Deps is a struct holding an injected *http.Client (not a Get func) — matches the verified RESEARCH safeClient seam and keeps the core unit-testable with a stub client."
  - "The OWUI route is the single const loadPath = \"/load\"; Plan 03's EXTERNAL_WEB_LOADER_URL must end in this path (single source of truth)."
  - "authOK uses crypto/subtle constant-time compare; an empty configured secret accepts any villa.network caller (documented posture; Plan 03 supplies a real crypto/rand secret)."
  - "HTML->text + <title> extraction kept deliberately simple per RESEARCH 'Don't Hand-Roll' — full sanitize (bluemonday) is Phase 32; no new dependency added."
metrics:
  duration: ~25m
  completed: 2026-06-19
  tasks: 3
  files: 6
  tests: 29
status: complete
---

# Phase 31 Plan 01: internal/websafe pure fetch core + SSRF guard + OWUI contract glue — Summary

Built `internal/websafe` — the pure, off-hardware-unit-testable fetch core that is the sole producer of OWUI `page_content` (GUARD-01) behind a comprehensive connect-time SSRF guard (GUARD-05) and the verified OWUI external-loader HTTP contract (GROUND-01 page-production path), with the network fetch injected via a `Deps` func-field seam so the core stays pure (orchestrate remains the only impure module).

## What was built

- **SSRF guard (`ssrf.go`)** — `rejectPrefixes` netip reject-set (loopback v4/v6, all RFC1918, link-local incl. 169.254.169.254 metadata, CGNAT, ULA, "this network", v4-mapped-v6), `ipRejected` (unmaps Is4In6 + stdlib Is* predicates + prefix set, fail-closed on invalid), `hostRejected` (defense-in-depth name reject: localhost / `villa-` prefix / `.network` + `.localhost` suffix, case-insensitive), `control` (the `net.Dialer.Control` hook validating the CONNECTED IP after DNS / before connect — defeats DNS-rebinding TOCTOU), and `SafeClient(Bounds)` wiring Control into the transport + an overall Timeout + a `CheckRedirect` that caps the chain at MaxRedirects, rejects non-http(s) redirect schemes, and rejects internal redirect hosts. `Bounds`/`DefaultBounds` also live here (shared by client + core).
- **Fetch core (`websafe.go`)** — `Deps{Client}` seam, `Page{Content,Source,Title}`, `Loader`/`NewLoader`, `fetchOne` (url.Parse + http(s) allowlist + hostRejected + `context.WithTimeout` + non-2xx reject + `io.LimitReader` body cap + guard-seam pipe), and `Load` (bounded-concurrency `chan struct{}` semaphore sized `min(len(urls), MaxConcurrent)`, skip-and-continue, input-order survivors, **non-nil empty slice on all-fail / empty input** — honest no-results, never fabricated). Simple HTML->text + `<title>` extraction.
- **Guard stubs (`guard_stubs.go`)** — `sanitize`/`normalize`/`fence` identity pass-throughs + `classify` no-detection, each documented as the Phase-32 GUARD-02/03/04 seam. Posture comment never claims injection immunity.
- **OWUI contract glue (`loader.go`)** — `LoadRequest{urls}`/`LoadResponse{page_content,metadata}` (exact verified tags), `Server`/`NewServer`/`Handler()` serving the single `/load` route, `authOK` (constant-time Bearer; empty secret = accept-any), and `HandleLoad` (401 unauth before any fetch, 400 on malformed/oversize bounded body, **ALWAYS 200 with a partial/empty non-nil array**, mapping each `Page` to `{page_content, metadata.source/title}` where `source` → OWUI's `sources` citation field).

## Tasks → commits

| Task | Name | Commit |
| ---- | ---- | ------ |
| 1 | SSRF guard (connect-time IP, per-hop redirect, scheme + host reject) | 2574e0c |
| 2 | Fetch core + Phase-32 guard stubs (bounded, skip-and-continue, Deps seam) | 05d5c8c |
| 3 | OWUI external-loader contract glue (Bearer auth, always-200 partial array) | 0cd3d00 |

## Verification results

- `go test ./internal/websafe/` — green (29 test cases incl. subtests across SSRF reject-set, redirect re-validation, connect-time control, host reject, fetch bounds/truncation/timeout, skip-and-continue, all-fail empty, guard-stub identity, contract, auth, malformed/oversize body, always-200).
- `go vet ./internal/websafe/` — clean. `go build ./...` — clean (nothing else broke).
- `grep -rn 'injection-safe' internal/websafe/` — clean (posture grep-ban honored).
- `go.mod`/`go.sum` unchanged — **stdlib only**, no new dependency (`bluemonday` is Phase 32).
- TestSeamGrepGate (`internal/inference`, walks `internal/`) — green: package is image/host-literal free.
- No file deletions in any of the three commits.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `cap` shadowed the builtin in `Load`**
- **Found during:** Task 2
- **Issue:** the concurrency-limit local was named `cap`, shadowing the Go builtin (a staticcheck/readability smell).
- **Fix:** renamed to `limit`.
- **Files modified:** internal/websafe/websafe.go
- **Commit:** 05d5c8c (fixed before the task commit)

**2. [Rule 3 - Blocking] Posture grep-ban token in guard_stubs comment**
- **Found during:** Task 3 verification
- **Issue:** the guard_stubs posture comment contained the literal phrase `injection-safe` (inside a negation), which trips the plan's `grep -rn 'injection-safe'` posture grep-ban.
- **Fix:** reworded to "do NOT confer immunity to prompt injection" — same posture, banned token removed.
- **Files modified:** internal/websafe/guard_stubs.go
- **Commit:** 0cd3d00

## Notes for Plan 03

- Wire the live client with `SafeClient(DefaultBounds())` into `Deps{Client}`; build the `Loader` with the same `Bounds`.
- Serve `Server.Handler()`; the route is fixed at `/load` (const `loadPath`) — `EXTERNAL_WEB_LOADER_URL` must end in `/load`.
- Supply a real crypto/rand Bearer secret to `NewServer` (the empty-secret accept-any path is a documented fallback, not the recommended posture).

## Self-Check: PASSED
- internal/websafe/ssrf.go — FOUND
- internal/websafe/websafe.go — FOUND
- internal/websafe/guard_stubs.go — FOUND
- internal/websafe/loader.go — FOUND
- internal/websafe/ssrf_test.go — FOUND
- internal/websafe/websafe_test.go — FOUND
- commit 2574e0c — FOUND
- commit 05d5c8c — FOUND
- commit 0cd3d00 — FOUND
