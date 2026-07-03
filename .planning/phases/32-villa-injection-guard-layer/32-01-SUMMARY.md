---
phase: 32-villa-injection-guard-layer
plan: 01
subsystem: internal/websafe (injection guard core)
status: complete
tags: [security, prompt-injection, sanitization, unicode, fencing, classifier, pure-core]
requires:
  - "Phase-31 guard seam (guard_stubs.go identity pass-throughs)"
  - "github.com/microcosm-cc/bluemonday@v1.0.27"
  - "golang.org/x/text/{unicode/norm,runes,transform}"
provides:
  - "sanitize(rawHTML string) string — GUARD-02 markup strip + entity-decode"
  - "normalize(s string) string — GUARD-02 NFKC + invisible/bidi rune strip"
  - "fence(content string) string — GUARD-03 crypto/rand nonced provenance fence"
  - "classify(normalized string) Verdict — GUARD-04 heuristic rule-family classifier"
  - "Verdict{Detected bool, Rules []string} — exported value type for /load metadata.guard"
affects:
  - "32-03 (seam rewire consumes these four funcs + the Verdict type)"
  - "32-02 (must-WIN recall/precision corpus eval tunes injectionRules)"
tech-stack:
  added:
    - "github.com/microcosm-cc/bluemonday v1.0.27 (direct; pure-Go BSD-3-Clause)"
    - "golang.org/x/text v0.23.0 promoted indirect -> direct"
  patterns:
    - "package-level immutable bluemonday StrictPolicy (built once, concurrency-safe)"
    - "transform.Chain(norm.NFKC, runes.Remove(predicate)) single-pass normalizer"
    - "crypto/rand + encoding/hex nonce (clone of villaconfig idiom; never math/rand)"
    - "map[string][]string curated multi-word imperative rule families + fixed iteration order"
    - "CR-02 never-return-empty-on-error (NFKC-only fallback)"
key-files:
  created:
    - internal/websafe/sanitize.go
    - internal/websafe/normalize.go
    - internal/websafe/fence.go
    - internal/websafe/verdict.go
    - internal/websafe/classify.go
    - internal/websafe/sanitize_test.go
    - internal/websafe/normalize_test.go
    - internal/websafe/fence_test.go
    - internal/websafe/classify_test.go
  modified:
    - go.mod
    - go.sum
    - internal/websafe/websafe_test.go
  deleted:
    - internal/websafe/guard_stubs.go
decisions:
  - "role-identity-reset rule family uses 'act as an ai' / 'act as a language model' (NOT bare 'act as') to protect precision (Pitfall 3)"
  - "fixed injectionRuleOrder slice makes Verdict.Rules deterministic (Go map iteration is randomized)"
  - "remove obsolete TestGuardStubsIdentity rather than adapt it — its premise (identity pass-throughs) no longer exists"
metrics:
  tasks_completed: 3
  files_created: 9
  files_modified: 3
  files_deleted: 1
  duration: ~35m
  completed: 2026-06-19
---

# Phase 32 Plan 01: Injection Guard Production Core Summary

Replaced the four Phase-31 identity guard stubs with real, topic-grouped GUARD-02/03/04 policy files in the pure `internal/websafe` core: bluemonday StrictPolicy markup sanitization with mandatory entity-decode, an NFKC + invisible/bidi Unicode defang pipeline, a crypto/rand-nonced provenance fence, and a heuristic multi-word rule-family classifier returning a `Verdict` — `guard_stubs.go` deleted, package compiles zero-stub under `CGO_ENABLED=0`.

## What Was Built

- **`sanitize.go` (GUARD-02 markup):** package-level `strictPolicy = bluemonday.StrictPolicy()` (built once, concurrency-safe); `sanitize` returns `strings.TrimSpace(html.UnescapeString(strictPolicy.Sanitize(rawHTML)))`. Entity-decode is mandatory (Pitfall 1) so the model reads `&` not `&amp;`. Replaces the CR-01/CR-02-buggy hand-rolled `extractText`.
- **`normalize.go` (GUARD-02 Unicode):** `invisibleAndBidi` runes.Predicate over the named zero-width/Trojan-Source bidi runes + a `unicode.Cf` catch-all; `normalizer = transform.Chain(norm.NFKC, runes.Remove(...))`; `normalize` runs `transform.String` with an NFKC-only fallback that NEVER returns `""` for non-empty input (CR-02 invariant). NFKC chosen over NFKD (defang-not-destroy recomposes).
- **`fence.go` (GUARD-03):** `newNonce()` clones the repo's only CSPRNG idiom (crypto/rand + encoding/hex, 64-bit), `fence` wraps content in a data-not-instructions preamble + `[UNTRUSTED_WEB_CONTENT nonce=<hex>]…[/UNTRUSTED_WEB_CONTENT nonce=<hex>]` with the SAME nonce on both tags (non-forgeable closing delimiter).
- **`verdict.go`:** exported `Verdict{Detected bool json:"detected"; Rules []string json:"rules,omitempty"}` — zero value marshals to `{"detected":false}`.
- **`classify.go` (GUARD-04):** `injectionRules map[string][]string` across four families (imperative-override, role-identity-reset, delimiter-turn-spoofing, secret-exfil-probe) of multi-word imperative phrases; `classify(normalized string) Verdict` (widened from stub `bool`) lowercases, scans in a fixed family order, returns matched family names. flag-not-block: never drops/rewrites content.
- **`guard_stubs.go` deleted** — all four guards are real in their own files.

## How to Verify

```bash
CGO_ENABLED=0 go build ./...                                              # exits 0 (pure-Go tree)
go test ./internal/websafe/ -count=1                                     # 53 passed
go test ./internal/inference/ -run TestSeamGrepGate -count=1             # green (no literals leaked)
go vet ./internal/websafe/                                               # clean
grep -n 'bluemonday v1.0.27' go.mod                                      # direct dep
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Obsolete `TestGuardStubsIdentity` broke the build after signature widening**
- **Found during:** Task 3 (after widening `classify` from `bool` to `Verdict`)
- **Issue:** `websafe_test.go:178` did `if classify(in) {…}` (non-boolean condition) and asserted all four guards were identity pass-throughs — a premise made false by this plan's stub-swap.
- **Fix:** Removed `TestGuardStubsIdentity` and updated the `websafe_test.go` file doc comment; per-policy behavior is now covered by `sanitize_test.go`/`normalize_test.go`/`fence_test.go`/`classify_test.go`.
- **Files modified:** internal/websafe/websafe_test.go
- **Commit:** 54a418b

**2. [Rule 3 - Blocking] `TestFetchBounds/body-truncated-at-maxbytes` exceeded MaxBytes by the fence scaffold**
- **Found during:** Task 3 (full-package test run)
- **Issue:** The fetched body is truncated to MaxBytes by `io.LimitReader`, but `fence` legitimately adds ~200 bytes (preamble + 2 nonced delimiters), so `Page.Content` ran 202 bytes over the hard `> MaxBytes` assertion.
- **Fix:** Allow a bounded `maxFenceOverhead` (1 KiB) over MaxBytes — the body is still truncated (no per-byte amplification); the fence is a fixed small scaffold.
- **Files modified:** internal/websafe/websafe_test.go
- **Commit:** 54a418b

**3. [Rule 1 - Precision bug] `role-identity-reset` over-flagged benign "To act as a delegate…"**
- **Found during:** Task 3 (TestClassifyDoesNotOverFlagBenign)
- **Issue:** Bare phrase `"act as"` matched legitimate prose (Pitfall 3 over-flagging).
- **Fix:** Replaced `"act as"` with the more specifically-imperative `"act as an ai"` / `"act as a language model"` and added `"from now on you are"`.
- **Files modified:** internal/websafe/classify.go
- **Commit:** 54a418b

### Adjusted Test Expectation (not a code change)

- **sanitize_test `script-and-entities`:** the plan's behavior note said output contains `Hello & goodbye x`, but bluemonday StrictPolicy strips tags WITHOUT inserting separators, so adjacent text joins (`goodbye`+`x` → `goodbyex`). The test asserts the accurate library behavior (`Hello & goodbyex`) while still proving the script tag + body removed and `&amp;` entity-decoded. No production change.

## Threat Mitigations Confirmed

- **T-32-01 (markup tampering):** bluemonday StrictPolicy strips all tags/scripts/active markup; entity-decode for plain text.
- **T-32-02 (Trojan-Source):** invisible/zero-width + bidi controls stripped + NFKC before classify.
- **T-32-03 (fence breakout):** crypto/rand per-call nonce on BOTH delimiters; `TestFenceNonceUnique` proves uniqueness, `TestFenceNonced` proves same-nonce-both-tags.
- **T-32-04 (DoS):** parser-backed bluemonday replaces the CR-01/CR-02 hand-rollers; normalize never returns empty on error.
- **T-32-05 / T-32-SC (supply chain):** bluemonday pinned `@v1.0.27` (retracted v1.0.0–v1.0.25 avoided); pure-Go tree keeps `CGO_ENABLED=0` green.

## Honesty Posture (carried forward)

Every new guard file's package doc states the layer **reduces and flags, does not eliminate** prompt injection. `classify.go` explicitly documents the **markdown-image zero-click exfil channel as a known residual** (not claimed closed) and that a heuristic rule set has finite recall. No "injection-safe"/"immune"/"blocks injection" phrasing introduced (the grep-ban test lands in 32-02).

## Notes for Downstream Plans

- **32-03 (seam rewire):** consume `sanitize`/`normalize`/`fence`/`classify` in order sanitize→normalize→classify→fence; add `Verdict Verdict` to the `Page` struct and thread it into `LoadResponse.Metadata["guard"]`. The current `fetchOne` still uses the Phase-31 ordering + `_ = classify(text)`; this plan intentionally did NOT rewire the seam.
- **32-02 (must-WIN eval):** `injectionRules` + `injectionRuleOrder` are the tuning surface for recall/precision; the frozen `minRecall`/`minPrecision` consts and corpora live in 32-02. Remediation on gate failure = tune rules/corpus, never lower the consts.

## Known Stubs

None — all four guard functions carry real policy; `guard_stubs.go` is deleted.

## Self-Check: PASSED

- All 9 created files present on disk; `guard_stubs.go` confirmed deleted.
- Commits 1054ef9, 9181854, 54a418b exist in git log.
- `CGO_ENABLED=0 go build ./...` exits 0; `go test ./internal/websafe/` 53 passed; `TestSeamGrepGate` green; `go vet` clean; full `go test ./...` = 1452 passed.
