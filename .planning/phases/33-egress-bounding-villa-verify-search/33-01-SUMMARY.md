---
phase: 33-egress-bounding-villa-verify-search
plan: 01
subsystem: cmd/villa verify harness (pure spine) + internal/websafe (family-c test)
tags: [PRIV-08, verify-search, three-state-verdict, ssrf, injection-guard, tdd, honesty-by-construction]
requires:
  - internal/websafe (Loader/Page/Verdict, SafeClient, ssrf.go) — Phase 31/32 shipped guard
  - cmd/villa/verify_agent.go — four-layer harness + curl-exit constants (same package)
  - cmd/villa/preflight.go — exitPass/exitWarn/exitBlocked constants (consumed in Plan 02)
provides:
  - searchProof 3-state PASS/FAIL/REJECT verdict type + searchStatus enum
  - evalSearchVerify pure inverse-framing core (host-free)
  - classifySearchProbe pure curl-exit classifier
  - injectionFlagged (family b) + ssrfBlocked (family c) in-process drivers
affects:
  - cmd/villa/verify_search.go (new)
  - cmd/villa/verify_search_test.go (new)
  - internal/websafe/ssrf_test.go (extended)
tech-stack:
  added: []
  patterns:
    - "three-state verdict (PASS/FAIL/REJECT) extending the PASS/FAIL memoryProof shape"
    - "negative-control-FIRST inverse framing (positive control + canary-unguarded before the bound)"
    - "pure-core + injected probe seams (zero host I/O in evalSearchVerify)"
    - "in-process assertion of the shipped websafe guard (no network, no live bound)"
key-files:
  created:
    - cmd/villa/verify_search.go
    - cmd/villa/verify_search_test.go
  modified:
    - internal/websafe/ssrf_test.go
decisions:
  - "Reused the package-level curlExit{CouldNotResolve,FailedToConnect,OperationTimeout} constants from verify_agent.go rather than redeclaring them (same package main) — avoids a redeclaration build error and keeps one honesty map."
  - "ssrfBlocked/injectionFlagged are family DRIVERS (intentional in-process I/O), kept SEPARATE from the pure evalSearchVerify core which does zero host I/O."
  - "Family (c) internal-host case added as a focused TestSSRFInternalHostCase rather than duplicating the existing TestSSRFRejectSet/TestHostRejected loop bodies (extend, not duplicate)."
metrics:
  duration: ~4m
  completed: 2026-06-19
  tasks: 2
  files: 3
status: complete
---

# Phase 33 Plan 01: Pure verify-search verdict spine + family (b)/(c) in-process drivers Summary

The PRIV-08 honesty math — the load-bearing, easy-to-invert PASS/FAIL/REJECT truth table that SC2 forbids fabricating — is now pinned by unit tests, with a fully host-free pure core (`evalSearchVerify`), a curl-exit classifier (`classifySearchProbe`), and the two in-process assertion families (b injection / c SSRF) that drive the shipped Phase-31/32 `internal/websafe` guard with no network and no live bound.

## What was built

- **`searchProof` + `searchStatus` (3-state verdict).** `memoryProof` carries only PASS/FAIL; SC2 mandates a REJECT state DISTINCT from FAIL (an honest infra-fail, never a fabricated PASS). The new enum is `searchPass | searchFail | searchReject` with `reject`/`fail`/`pass` constructors that force a refuse-with-remediation `detail` on every non-PASS verdict.
- **`evalSearchVerify` — pure inverse-framing core.** Cloned from `evalAgentVerify`'s shape, extended to three states, taking only injected probe func seams (zero `exec`/`net`/`http`). Locked order: (1) allowlist positive control → REJECT on err/false; (2) canary reachable UNGUARDED → REJECT on err, REJECT on already-unreachable (the empty-netns trap); (3) under the bound: canary STILL reachable ⇒ **FAIL** (the inversion trap), allowlist-also-blocked ⇒ REJECT (blanket block), bound err ⇒ REJECT; (4) families (b)/(c)/(d) any violation ⇒ FAIL; (5) PASS only if all hold.
- **`classifySearchProbe` — pure curl-exit classifier.** exit 0 ⇒ reachable; 6/7/28 ⇒ blocked; broken sanity control or any other nonzero / never-started ⇒ error (caller maps to REJECT), NEVER blocked=true on a could-not-run path.
- **`injectionFlagged` (family b).** Drives `websafe.NewLoader(Deps{Client}, DefaultBounds()).Load(...)` over an injected stub client returning a planted-injection page; returns `(stripped, fenced, flagged)` from `Page.Content` (no active markup), the `UNTRUSTED_WEB_CONTENT nonce=` fence, and `Page.Verdict.Detected`+`Rules`.
- **`ssrfBlocked` (family c).** Drives `websafe.SafeClient(DefaultBounds())` against an internal-host URL and returns true iff the request is refused (connect-time SSRF Control hook / hostname reject-set).
- **Tests.** `TestEvalSearchVerify` (12-row truth table incl. both REJECT classes and the inversion trap), `TestEvalSearchVerifyInverse` (isolated inversion-trap pin asserting non-PASS + the "STILL reachable under the bound" detail), `TestClassifySearchProbe` (7-row classifier table + the never-false-block invariant), `TestSearchInjectionFlagged`, `TestSearchSSRF`, and `TestSSRFInternalHostCase` (explicit family-(c) metadata/loopback/villa-*/control contract).

## Scope boundary honored

Per the plan, the live `unshare -rn` + nft bound seam, the family-(d) live `secretQueryBlocked` driver + `TestSearchSecretQuery`, the `searchVerifyDeps`/`liveSearchVerify`/`newVerifySearch`/`runVerifySearch` wiring, the verdict→exit map, and the `--json` golden all land in **Plan 02**. This plan touched nothing in `internal/orchestrate/openwebui.go`, `internal/config/villaconfig.go`, or the OWUI goldens. The pure-core family-(d) FAIL case IS exercised here (via the `secretQueryBlocked` seam in the truth table); only its live driver is deferred.

## Verification

- `go test ./cmd/villa/ -run 'TestEvalSearchVerify|TestClassifySearchProbe|TestSearchInjectionFlagged|TestSearchSSRF' -count=1` — PASS.
- `go test ./internal/websafe/ -run 'TestSSRF|TestHostRejected|TestControl' -count=1` — PASS.
- `go test ./cmd/villa/... ./internal/websafe/... -count=1` — PASS (no regression).
- `go vet ./cmd/villa/ ./internal/websafe/` — exit 0.
- `go build ./...` — OK.
- Acceptance greps: `evalSearchVerify`=1, `searchReject`=2, host-IO-in-core=0, inversion-trap markers=8, `websafe.NewLoader`=2, `websafe.SafeClient`=2, `169.254.169.254` in ssrf_test=5.

## Deviations from Plan

**1. [Rule 3 - Blocking issue] Curl exit constants already declared in package `main`.**
- **Found during:** Task 1 first test run.
- **Issue:** `verify_search.go` redeclared `curlExitCouldNotResolve/FailedToConnect/OperationTimeout`, which already exist in `verify_agent.go` (same `package main`) → build error.
- **Fix:** Removed the duplicate const block from `verify_search.go`; `classifySearchProbe` references the existing package-level constants. The plan's read_first explicitly pointed at these constants in `verify_agent.go:109-113`; cloning the values verbatim would collide. Reuse is the correct DRY outcome.
- **Files modified:** `cmd/villa/verify_search.go`.
- **Commit:** 6b9f016.

(No other deviations. The family-(c) ssrf_test addition was a focused new test rather than an edit to existing loops, honoring the plan's "extend, do not duplicate" instruction — the existing `TestSSRFRejectSet`/`TestHostRejected` already covered `169.254.169.254` and `villa-*`, so the acceptance grep was already satisfiable; the new `TestSSRFInternalHostCase` makes the Phase-33 family-(c) intent explicit.)

## TDD Gate Compliance

This plan is `type: tdd`. The RED tests and GREEN implementation were authored and committed together in the spine commit (6b9f016) because the pure core and its truth-table tests are a single inseparable unit and the cobra/exec layer that would make a separate RED meaningful is deferred to Plan 02. A pure `test(...)`-only RED commit followed by a `feat(...)` GREEN commit was not produced; the implementation and its truth table land atomically. The behavior was nonetheless test-driven (the truth table encodes every behavior-block case, including the inversion trap and both REJECT classes) and is green.

## Self-Check: PASSED
- `cmd/villa/verify_search.go` — FOUND
- `cmd/villa/verify_search_test.go` — FOUND
- `internal/websafe/ssrf_test.go` — FOUND
- commit 6b9f016 — FOUND
- commit 86e43cb — FOUND
