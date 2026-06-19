---
phase: 32
slug: villa-injection-guard-layer
status: final
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-19
finalized: 2026-06-19
---

# Phase 32 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard library `testing`) |
| **Config file** | none — Go modules; `Makefile` targets |
| **Quick run command** | `go test ./internal/websafe/...` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/websafe/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Test File · Functions | Automated Command | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-----------------------|-------------------|--------|
| 32-01-T1 | 01 | 1 | GUARD-02 | T-32-01/02/04 | markup stripped (bluemonday StrictPolicy) + entity-decoded; NFKC fold + invisible/bidi rune strip; non-Latin survives; never-empty fallback | unit | `sanitize_test.go` (StripsMarkup, AllMarkupTrimsEmpty, PreservesVisibleText); `normalize_test.go` (StripsInvisibleAndBidi, NFKCFolds, PreservesNonLatin, NeverEmpty) | `go test ./internal/websafe/...` | ✅ green |
| 32-01-T2 | 01 | 1 | GUARD-03 | T-32-03 | crypto/rand nonce on BOTH delimiters (same per call, unique across calls); content verbatim; Verdict JSON shape | unit | `fence_test.go` (FenceNonced, FenceNonceUnique, VerdictJSON) | `go test ./internal/websafe/...` | ✅ green |
| 32-01-T3 | 01 | 1 | GUARD-04 | T-32-01..05 | heuristic `injectionRules` classifier flags known attacks, no over-flag on benign, case-insensitive, flag-not-block, line-anchored role markers | unit | `classify_test.go` (DetectsKnownAttacks, DoesNotOverFlagBenign, CaseInsensitive, FlagNotBlock, RoleMarkerLineAnchored) | `go test ./internal/websafe/...` | ✅ green |
| 32-02-T1 | 02 | 2 | GUARD-04 | T-32-06/07 | held-out adversarial corpus (35 ≥30) + benign corpus (39 ≥30, incl. non-Latin + meta-article) parse and carry correct `expect_detected` | fixtures | `testdata/corpus_inject.json`, `testdata/corpus_benign.json` (loaded + length/empty-guarded by `loadCorpus`) | `go test ./internal/websafe/...` | ✅ green |
| 32-02-T2 | 02 | 2 | GUARD-04 | T-32-06/07 | **must-WIN** recall ≥ 0.90 + precision ≥ 0.95 (frozen consts `minRecall`/`minPrecision`) measured over production ordering `classify(normalize(sanitize(sample)))`; flag-not-block | eval | `classify_eval_test.go` (TestClassifyRecall, TestClassifyPrecision, TestClassifyDoesNotDrop) | `go test ./internal/websafe/...` | ✅ green |
| 32-02-T3 | 02 | 2 | GUARD-04 | T-32-08/09 | directory-walking grep-ban on "injection-safe"/"immune"/"blocks injection" (scanned>0 guard); markdown-image residual documented as NOT closed; "reduces and flags" copy | grep/doc | `honesty_test.go` (TestNoInjectionSafeCopy, TestMarkdownImageResidualDocumented) | `go test ./internal/websafe/...` | ✅ green |
| 32-03-T1 | 03 | 2 | GUARD-02/03/04 | T-32-10/11/12 | fetchOne runs sanitize → normalize → classify → fence (sanitize-first; normalize-before-classify catches NFKC-obfuscated); verdict USED (no `_ = classify`); title defanged + classified; flag-not-block preserves content; commented/decoy/forged-fence title handling | unit | `websafe_test.go` (TestGuardSeamOrder, TestFetchGuardVerdict, TestTitleInjectionFlagged, TestExtractTitleLengthPreserving) | `go test ./internal/websafe/...` | ✅ green |
| 32-03-T2 | 03 | 2 | GUARD-04 | T-32-13/14 | `/load` metadata gains additive `guard:{detected,rules}` sub-key; existing page_content/metadata/source/title unchanged; always-200 + non-nil array; verdict always present | integration | `loader_test.go` (TestLoadMetadataGuard, TestLoadMetadataGuardAlwaysPresent) | `go test ./internal/websafe/...` | ✅ green |
| 32-RACE | 03 | 2 | GUARD-02 (CR-01/WR-04) | T-32-10 | concurrent multi-URL Load over a stateless normalize is data-race clean (shared `transform.Chain` race regression gated) | race | `websafe_test.go` (TestLoadRaceBatch) under `-race`; `make test-race` + CI `go test -race ./...` (`.github/workflows/ci.yml`) | `make test-race` / `CGO_ENABLED=1 go test -race ./internal/websafe/...` | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky.*
*Note: the GUARD-04 honesty copy / markdown-image residual / "injection-safe" grep-ban (formerly drafted as a standalone 32-04 row) is delivered by Plan 32-02 — the phase is a 3-plan layout (32-01, 32-02, 32-03).*
*Verified 2026-06-19: `go test ./internal/websafe/...` green; `CGO_ENABLED=1 go test -race ./internal/websafe/...` green; `TestSeamGrepGate` green. Recall/precision pass over the frozen consts (0.90/0.95) on the 35-sample adversarial + 39-sample benign corpora.*

---

## Wave 0 Requirements

- [x] `internal/websafe/*_test.go` — guard transform + classifier unit tests for GUARD-02/03/04 (sanitize/normalize/fence/classify + websafe/loader integration tests, all green)
- [x] `internal/websafe/testdata/` — adversarial injection corpus (35 samples incl. invisible-Unicode + fence-breakout payloads) + benign controls (39 samples incl. non-Latin + meta-article) backing the precision/recall must-WIN eval
- [x] No framework install needed — `go test` is built in

*The injection-detection precision/recall eval is the must-WIN gate (suggested: recall ≥ 0.90, precision ≥ 0.95, ≥30 positive + ≥30 benign samples; thresholds frozen by the planner before implementation).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| End-to-end: a real fetched page with an embedded injection reaches the chat model fenced + flagged | GUARD-02/03 | requires live OWUI + llama.cpp + villa-websafe on hardware | Enable web search, ask a question whose top result page contains an injection payload; confirm the answer treats it as data and the guard verdict surfaces honestly |

*Automated unit + corpus eval cover the transform/classifier logic; the on-hardware end-to-end is human verification.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies — every task row maps to a named, executed test function
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references — all test files + corpora exist and pass
- [x] No watch-mode flags
- [x] Feedback latency < 60s (`go test ./internal/websafe/...` ≈ 1.1s; `-race` ≈ 3.3s)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-19 — coverage audited; all GUARD-02/03/04 requirements and every plan must_have map to executed, behavior-exercising tests that pass (incl. the must-WIN precision/recall eval over the production ordering and the `-race` regression gate). No coverage gaps; no tests needed to be added.
