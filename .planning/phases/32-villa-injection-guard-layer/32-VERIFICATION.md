---
phase: 32-villa-injection-guard-layer
verified: 2026-06-19T00:00:00Z
status: passed
score: 15/15 must-haves verified
behavior_unverified: 0
overrides_applied: 0
re_verification: # No previous VERIFICATION.md — initial verification
  previous_status: none
---

# Phase 32: Villa Injection Guard Layer Verification Report

**Phase Goal:** Fetched web content reaching the model is sanitized, Unicode-normalized, provenance-fenced as untrusted-data-not-instructions, and screened by a heuristic classifier that flags injection attempts — honestly surfaced as "reduces and flags, does not eliminate."
**Verified:** 2026-06-19
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Raw HTML reduced to plain text, all markup stripped (bluemonday StrictPolicy) + entity-decoded | ✓ VERIFIED | `sanitize.go:26` package-level `bluemonday.StrictPolicy()`; `sanitize.go:38-40` `strings.TrimSpace(html.UnescapeString(strictPolicy.Sanitize(rawHTML)))` |
| 2 | Invisible/zero-width removed, bidi neutralized, NFKC applied — BEFORE fence/classify | ✓ VERIFIED | `normalize.go:59-66` `norm.NFKC.String(s)` then `transform.String(invisibleRemover, folded)`; named bidi/zero-width set + Cf catch-all at lines 29-38 |
| 3 | Content wrapped in crypto/rand-nonced fence, closing delimiter non-forgeable | ✓ VERIFIED | `fence.go:35-41` `rand.Read` per-fetch nonce; same nonce on open+close tags (lines 56-60); fail-closed propagates error on rand failure |
| 4 | Pure-Go heuristic classifier returns `Verdict{Detected,Rules}`, flag-not-block (never drops content) | ✓ VERIFIED | `classify.go:131-152` returns `Verdict`; `TestClassifyDoesNotDrop` PASS (input unchanged); signature cannot return content |
| 5 | websafe compiles with zero guard stubs; CGO_ENABLED=0 static build succeeds | ✓ VERIFIED | `guard_stubs.go` absent (deleted); `CGO_ENABLED=0 go build ./cmd/villa` → BUILD OK |
| 6 | Held-out adversarial + benign corpus exist as checked-in JSON | ✓ VERIFIED | `testdata/corpus_inject.json` (35 samples), `corpus_benign.json` (39 samples) |
| 7 | Recall ≥ frozen threshold AND precision ≥ frozen threshold over production ordering | ✓ VERIFIED | `TestClassifyRecall` PASS (minRecall=0.90), `TestClassifyPrecision` PASS (minPrecision=0.95); both score `classify(normalize(sanitize(sample)))` = production order |
| 8 | Benign corpus includes legitimate non-Latin sample + article ABOUT injection | ✓ VERIFIED | 3 `meta-article-*` + 3 `non-latin-*` samples (arabic, cjk, japanese) in `corpus_benign.json` |
| 9 | "injection-safe"/"immune"/"blocks injection" appear nowhere in package — grep-ban enforced | ✓ VERIFIED | `TestNoInjectionSafeCopy` PASS (directory-walk, scanned > 0 files); ban regex `honesty_test.go:30` |
| 10 | Package doc says "reduces and flags, does not eliminate" + markdown-image residual documented as NOT closed | ✓ VERIFIED | `doc.go:13-30` honesty posture + markdown-image residual "NOT claimed closed"; `TestMarkdownImageResidualDocumented` PASS |
| 11 | fetchOne runs sanitize → normalize → classify → fence (sanitize-first) | ✓ VERIFIED | `websafe.go:169-185` `clean := sanitize(body); clean = normalize(clean); verdict := classify(clean); ...; fence(clean)` |
| 12 | Classifier verdict is USED — stored on Page, not discarded | ✓ VERIFIED | `websafe.go:193` `Page{..., Verdict: verdict}`; Page.Verdict field `websafe.go:47` |
| 13 | Page carries Verdict; fenced text is content regardless of verdict (flag-not-block) | ✓ VERIFIED | `websafe.go:193` Content=fenced always; verdict separate field; flag-not-block comment line 168 |
| 14 | Title routed through same sanitize+normalize defang as body | ✓ VERIFIED | `websafe.go:182-183` `title := normalize(sanitize(extractTitle(body)))`; title verdict folded via `mergeVerdicts` (WR-01) |
| 15 | /load metadata carries additive `guard{detected,rules}`; page_content/metadata/source/title not renamed | ✓ VERIFIED | `loader.go:137-144` additive `guard` key reading `p.Verdict`; `TestLoadMetadataGuard` + `TestLoadMetadataGuardAlwaysPresent` PASS asserting existing tags intact |

**Score:** 15/15 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/websafe/sanitize.go` | bluemonday StrictPolicy + entity decode | ✓ VERIFIED | Substantive, wired into fetchOne |
| `internal/websafe/normalize.go` | NFKC + invisible/bidi strip | ✓ VERIFIED | Stateless transformers (CR-01 fix), wired |
| `internal/websafe/fence.go` | crypto/rand nonced fence, fail-closed | ✓ VERIFIED | WR-02 fail-closed nonce, wired |
| `internal/websafe/verdict.go` | Verdict type + mergeVerdicts | ✓ VERIFIED | Type + WR-01 merge helper, wired |
| `internal/websafe/classify.go` | heuristic rule families → Verdict | ✓ VERIFIED | WR-03 line-anchored role markers, wired |
| `internal/websafe/websafe.go` | rewired fetchOne + Page.Verdict | ✓ VERIFIED | sanitize→normalize→classify→fence ordering |
| `internal/websafe/loader.go` | /load metadata.guard threading | ✓ VERIFIED | Additive guard key |
| `internal/websafe/doc.go` | honesty copy + residual doc | ✓ VERIFIED | Both present, pinned by tests |
| `testdata/corpus_inject.json` | ≥30 adversarial samples | ✓ VERIFIED | 35 samples |
| `testdata/corpus_benign.json` | ≥30 benign incl. non-Latin + meta | ✓ VERIFIED | 39 samples, both categories present |
| `classify_eval_test.go` | must-WIN frozen-threshold eval | ✓ VERIFIED | minRecall/minPrecision consts, production ordering |
| `honesty_test.go` | grep-ban + residual pin | ✓ VERIFIED | Both tests PASS |
| `loader_test.go` | metadata.guard + unchanged keys | ✓ VERIFIED | Asserts contract integrity |
| `go.mod` | bluemonday v1.0.27 direct dep | ✓ VERIFIED | Builds; bluemonday imported |

### Key Link Verification

| From | To | Via | Status |
|------|----|----|--------|
| sanitize.go | bluemonday | `bluemonday.StrictPolicy()` package var | ✓ WIRED |
| normalize.go | x/text/unicode/norm + runes | NFKC.String + runes.Remove (stateless) | ✓ WIRED |
| classify.go | verdict.go | returns `Verdict{...}` | ✓ WIRED |
| websafe.go fetchOne | classify.go | `verdict := classify(clean)` | ✓ WIRED |
| loader.go | websafe.go | `p.Verdict` → metadata.guard | ✓ WIRED |
| classify_eval_test.go | corpus + classify | `classify(normalize(sanitize(sample)))` | ✓ WIRED |
| cmd/villa/websafe.go | websafe.NewLoader/NewServer | end-to-end /load path | ✓ WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| must-WIN recall gate | `go test -run TestClassifyRecall` | PASS (≥0.90) | ✓ PASS |
| must-WIN precision gate | `go test -run TestClassifyPrecision` | PASS (≥0.95) | ✓ PASS |
| grep-ban honesty | `go test -run TestNoInjectionSafeCopy` | PASS | ✓ PASS |
| markdown-image residual pin | `go test -run TestMarkdownImageResidualDocumented` | PASS | ✓ PASS |
| /load metadata.guard contract | `go test -run TestLoadMetadataGuard` | PASS | ✓ PASS |
| flag-not-block (no drop) | `go test -run TestClassifyDoesNotDrop` | PASS | ✓ PASS |
| data-race (CR-01) full tree | `CGO_ENABLED=1 go test -race ./...` | EXIT=0, no DATA RACE | ✓ PASS |
| static build | `CGO_ENABLED=0 go build ./cmd/villa` | BUILD OK | ✓ PASS |
| seam grep-gate unbroken | `go test -run TestSeamGrepGate` | PASS | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| GUARD-02 | 32-01, 32-03 | Sanitize + normalize before fencing | ✓ SATISFIED | Truths 1,2,11; REQUIREMENTS.md:28 `[x]` |
| GUARD-03 | 32-01, 32-03 | Nonced provenance fence | ✓ SATISFIED | Truths 3,11; REQUIREMENTS.md:29 `[x]` |
| GUARD-04 | 32-01, 32-02, 32-03 | Heuristic classifier, flag-not-block, honest copy, markdown-image residual | ✓ SATISFIED | Truths 4,7,9,10,12,15; REQUIREMENTS.md:30 `[x]` |

All three requirement IDs from plan frontmatter are accounted for in REQUIREMENTS.md (Phase 32, Complete). No orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | None | — | No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER or stub patterns in modified non-test files |

### Review Findings (32-REVIEW-FIX.md: all_fixed) — Confirmed in Code

| Finding | Fix | Confirmed |
|---------|-----|-----------|
| CR-01 (BLOCKER) data race on shared transform.Chain | normalize uses stateless `norm.NFKC.String` + per-call runes transformer | ✓ `normalize.go:40-66`; `-race ./...` green |
| WR-01 title injection surface | title sanitize+normalize+classify, `mergeVerdicts` fold | ✓ `websafe.go:182-183`, `verdict.go:25-37` |
| WR-02 fence fail-open on rand error | propagate error, fetchOne omits page | ✓ `fence.go:35-41,51-61` |
| WR-03 role-marker precision | line-leading anchoring | ✓ `classify.go:87-125` |

### Human Verification Required

None. All four Success Criteria are observable in code and exercised by passing automated gates (eval thresholds, grep-ban, residual pin, race detector, static build). No state-transition/cancellation invariants require runtime human checks beyond what the corpus eval and race gate already exercise.

### Gaps Summary

No gaps. The phase goal is genuinely achieved in the shipped code:
- Sanitize (bluemonday StrictPolicy) + Unicode normalize (NFKC + invisible/bidi strip) run before fencing, in the load-bearing production order sanitize→normalize→classify→fence (`websafe.go:169-185`).
- Per-fetch crypto/rand nonce fence, fail-closed on rand error (`fence.go`).
- Pure-Go heuristic classifier returns a flag-not-block Verdict that is USED (stored on Page, threaded into /load metadata.guard additively without renaming verified contract tags).
- The must-WIN recall (≥0.90) / precision (≥0.95) eval passes over the production ordering against frozen consts; benign corpus includes non-Latin + meta-articles so over-flagging is measured.
- Honesty copy ("reduces and flags, does not eliminate") present and grep-ban-enforced; markdown-image exfil documented as a known residual NOT claimed closed.
- CR-01 data race fixed; `go test -race ./...` green; the -race gate is in `make check` (Makefile:60) and CI (ci.yml:40).

---

_Verified: 2026-06-19_
_Verifier: Claude (gsd-verifier)_
