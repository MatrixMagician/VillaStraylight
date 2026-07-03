---
phase: 33-egress-bounding-villa-verify-search
fixed_at: 2026-06-19T22:43:55Z
review_path: .planning/phases/33-egress-bounding-villa-verify-search/33-REVIEW.md
iteration: 1
findings_in_scope: 2
fixed: 2
skipped: 0
deferred: 4
status: all_fixed
---

# Phase 33: Code Review Fix Report

**Fixed at:** 2026-06-19T22:43:55Z
**Source review:** .planning/phases/33-egress-bounding-villa-verify-search/33-REVIEW.md
**Iteration:** 1

**Scope note:** This fix pass was deliberately bounded to the two OFF-HARDWARE
findings (CR-02, WR-01). The remaining four findings (CR-01, WR-02, WR-03, WR-04)
are the live rootless-netns bound-mechanics attach point and its downstream
consequences — they are the explicit deliverable of **Plan 33-03** (on-hardware,
architecture A/B selection) and cannot be correctly fixed off-hardware. They are
DEFERRED to 33-03, not skipped (see "Deferred to Plan 33-03" below).

**Summary:**
- Findings in scope: 2
- Fixed: 2
- Skipped: 0
- Deferred to Plan 33-03 (on-hardware): 4

## Fixed Issues

### CR-02: Live family-(b) injection probe was vacuously broken (fetched a benign URL)

**Files modified:** `cmd/villa/verify_search.go`, `cmd/villa/verify_search_test.go`
**Commit:** 9079c1f
**Applied fix:**

The live family-(b) clause in `liveSearchVerify` previously fetched the REAL
benign allowlist URL (`https://en.wikipedia.org/`) through the live SafeClient and
asserted the page came back stripped+fenced+**flagged**. A benign page never trips
the injection classifier (`Verdict.Detected==false` → `flagged==false`), so the
clause was structurally incapable of returning the asserted result — it ALWAYS
FAILed on a healthy host (and the passing unit test only worked because it used a
planted-page stub transport, not the live wiring).

Family (b) is an in-process guard assertion (no network, no live bound), so it must
exercise a PLANTED-injection input. The fix mirrors the unit test's planted-page
approach in the live path:

- Promoted a non-test planted page + transport into `verify_search.go` (resolving
  IN-03's build-visibility blocker): `searchPlantedInjectionPage` (active `<script>`
  markup + an imperative prompt-injection sentence) and `plantedPageRoundTripper`
  (an in-process `http.RoundTripper` serving that page with NO network — the
  non-test equivalent of the test's `stubRoundTripper`).
- Rewired the live `injection` closure to drive `injectionFlagged` against the
  planted page via the stub transport (`https://villa.invalid/planted`), NOT a live
  fetch of the benign allowlist URL.
- Added `io` to the import set (used by the transport's `io.NopCloser`).

The clause is now genuinely non-vacuous: it PASSes only if the shipped websafe
guard strips+fences+flags the attack page, and FAILs if the guard misses it.

**Non-vacuity proof (off-hardware tests added):**
- `TestSearchLivePlantedInjectionFlagged` — drives the PRODUCTION planted page
  through the PRODUCTION transport and asserts stripped+fenced+flagged (the live
  clause CAN PASS).
- `TestSearchLiveInjectionFlagsBenignFalse` — asserts a benign page is NOT flagged,
  documenting exactly why the old live-benign-fetch wiring could only ever FAIL.

### WR-01: Mislabeled package-global `secretUnderBound`

**Files modified:** `cmd/villa/verify_search.go`
**Commit:** 384ed9c
**Applied fix:**

`secretUnderBound` was a package-level `var struct{ran, blocked bool; err error}`
whose doc comment falsely claimed it was "a request-scoped value, not shared
mutable state across invocations." It was in fact process-global shared state, safe
only by the accident of single-shot CLI invocation.

Eliminated the global entirely: declared `secretUnderBound` as a LOCAL variable
inside `liveSearchVerify`, closed over by both `boundThen` (writer) and the `secret`
closure (reader). Its lifetime is now genuinely scoped to a single call and cannot
leak across invocations or be raced by a concurrent caller. Removed the now-redundant
`.ran = false` reset (the local zero value is the unrun state) and corrected the doc
comments. The change is behavior-preserving — `make check` (including the
`-race` test pass) is green.

## Deferred to Plan 33-03 (on-hardware)

The following findings are NOT skipped — they are the documented deliverable of
Plan 33-03 (the on-hardware rootless-netns bound mechanics, architecture A vs B).
They cannot be correctly resolved off-hardware and were intentionally left
untouched in this pass:

- **CR-01 (BLOCKER)** — The transient egress bound is applied in a throwaway
  `unshare -rn` netns that the `podman run --network villa` probes never enter, so
  the bound has zero effect on the canary probe (the proof is structurally
  incapable of producing a PASS). The correct fix is the rootless-netns attach
  point (run the probe inside the bounded namespace, e.g. named netns +
  `ip netns exec`, or `podman --network ns:…`), which is the on-hardware Plan 33-03
  architecture decision. Guessing a mechanism off-hardware would be unverifiable.

- **WR-02** — Live family-(d) secret-query probe shares CR-01's broken bound;
  resolved transitively once the bound and probe share a namespace (CR-01).

- **WR-03** — `resolveAllowlistIPs` silently drops IPv6; a latent correctness gap
  that only manifests once CR-01's real bound is in effect. Belongs with the
  on-hardware bound work.

- **WR-04** — TOCTOU/IP-set drift between the bound-apply resolution and the
  separate curl resolution (pin via `--resolve`); latent until CR-01 is fixed and
  best validated against the real bound on-hardware.

Info findings IN-01 / IN-02 / IN-03 were not in scope. IN-03 (test seams blocking
the CR-02 live wiring) was resolved as part of the CR-02 fix by promoting a planted
page + transport into non-test code.

## Verification

- `CGO_ENABLED=0 go build ./...` — green
- `go test ./cmd/villa/... -run 'VerifySearch|SearchSecretQuery|SearchVerify'` — green
- `make check` (vet + `-race` test suite) — green
- Live family-(b) assertion proven non-vacuous off-hardware (can PASS for planted
  injection; the benign-not-flagged counter-test documents the old failure mode).

---

_Fixed: 2026-06-19T22:43:55Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
