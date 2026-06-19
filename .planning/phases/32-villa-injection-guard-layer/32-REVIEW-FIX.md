---
phase: 32-villa-injection-guard-layer
fixed_at: 2026-06-19T00:00:00Z
review_path: .planning/phases/32-villa-injection-guard-layer/32-REVIEW.md
iteration: 1
findings_in_scope: 4
fixed: 4
skipped: 0
deferred: 3
status: all_fixed
---

# Phase 32: Code Review Fix Report

**Source review:** `.planning/phases/32-villa-injection-guard-layer/32-REVIEW.md`
**Iteration:** 1
**Scope:** Critical + Warning findings (CR-01, WR-01, WR-02, WR-03). Info findings
(IN-01, IN-02, IN-03) are out of scope for this `--fix` pass (IN-02 was incidentally
addressed as part of the WR-01 fix; see below).

**Summary:**
- Findings in scope: 4 (1 Critical, 3 Warning)
- Fixed: 4
- Skipped: 0
- Deferred (out of scope): 3 Info

**Verification (all green):**
- `go test -race ./internal/websafe/...` — 73 passed
- `go test -race ./...` — 1472 passed across 25 packages (full suite under the race detector)
- `make check` — exit 0 (now includes the new `test-race` gate)
- `CGO_ENABLED=0 go build ./...` — exit 0 (single static binary preserved; `-race` is test-only CGO)
- Must-WIN eval frozen thresholds STILL pass with full margin: **recall 35/35 = 1.00** (≥ 0.90), **precision 39/39 = 1.00** (≥ 0.95). `minRecall`/`minPrecision` consts unchanged.
- `gofmt` — every file changed by these fixes is clean (two pre-existing unformatted test files, `normalize_test.go` and `sanitize_test.go`, were NOT touched by this pass and remain as-is).

## Fixed Issues

### CR-01 (BLOCKER): `normalize` shared a stateful `transform.Chain` across concurrent fetch goroutines

**Files modified:** `internal/websafe/normalize.go`, `Makefile`, `.github/workflows/ci.yml`
**Commit:** `cb610f7`
**Applied fix:** Replaced the package-level stateful `transform.Chain(norm.NFKC,
runes.Remove(...))` with the stateless string forms: `norm.NFKC.String(s)` followed by
`transform.String(invisibleRemover, folded)` over a package-level `runes` Transformer.
No shared mutable transformer is raced on. The CR-02 never-return-empty invariant is
preserved (on transform error it returns the NFKC-folded fallback, never `""`).
Added a `-race` gate so this class of bug cannot silently regress: a new
`make test-race` target folded into `make check`, and a dedicated CI race step. Both
enable CGO for the **test run only** — the shipped binary stays CGO-free (the existing
`CGO_ENABLED=0` build gates are unchanged). Confirmed the race reproduced pre-fix
(`go test -race` FAILED on 3 tests) and is gone post-fix.

### WR-02: `fence` failed OPEN to a predictable zero nonce on `crypto/rand.Read` error

**Files modified:** `internal/websafe/fence.go`, `internal/websafe/websafe.go`, `internal/websafe/fence_test.go`, `internal/websafe/websafe_test.go`
**Commit:** `41a749f` (committed jointly with WR-01 — both interleave in `websafe.go`/`websafe_test.go`)
**Applied fix:** `newNonce` now returns `(string, error)` and propagates a
`crypto/rand.Read` failure instead of emitting the constant `"0000000000000000"` nonce.
`fence` returns `(string, error)`; on a nonce error `fetchOne` fails the fetch CLOSED
(`return Page{}, err`), so the page is omitted (skip-and-continue, honest partial)
rather than shipping a forgeable constant-nonce fence. Consistent with the package's
fail-closed-on-untrusted-input invariant. Updated `fence_test.go` and the
`fetchOneGuard` test helper for the new signature.

### WR-01: title injection surface — `metadata.title` reached unfenced/unclassified and from inside HTML comments

**Files modified:** `internal/websafe/websafe.go`, `internal/websafe/verdict.go`, `internal/websafe/websafe_test.go`
**Commit:** `41a749f` (joint with WR-02)
**Applied fix:** (a) `extractTitle` now blanks HTML comment spans (`<!-- ... -->`) before
scanning via a new length-preserving `blankComments` helper, so a `<title>` hidden in a
comment is no longer scraped over the real document title (this also keeps the CR-01
length-preserving index invariant intact). It now requires a tag terminator after
`<title` (`>` or whitespace), so `<titlebar>` no longer matches — this incidentally
closes IN-02. (b) The normalized title is now run through the classifier and OR-merged
into the page verdict via a new `mergeVerdicts` helper, so title-borne injection
(e.g. a forged `[/UNTRUSTED_WEB_CONTENT]` close, or `ignore previous instructions` in a
`<title>`) is flagged in `metadata.guard`. The title remains verbatim in
`metadata.title` (flag-not-block) because it is a human-facing citation label; fencing
it would corrupt the displayed citation. Added regression tests for title-injection
flagging, the commented-title decoy, the forged-fence-close title, and the
`<titlebar>` non-match.

### WR-03: classifier `system:` / `assistant:` rules precision-fragile

**Files modified:** `internal/websafe/classify.go`, `internal/websafe/classify_test.go`, `internal/websafe/testdata/corpus_benign.json`
**Commit:** `9bf4838`
**Applied fix:** Moved the bare `system:` / `assistant:` markers out of the plain
`Contains` phrase list into a line-anchored matcher (`matchLineLeadingRole` /
`lineLeading`): they now fire ONLY at a turn-start position (start-of-text, after a
newline, or after a whitespace/delimiter run) and no longer match a role word embedded
mid-sentence ("Operating System: Linux", "voice assistant: enabled"). Added a `user:`
marker for symmetry. Added 7 benign corpus samples that legitimately contain
"System:"/"assistant:" in ordinary prose so the frozen precision gate genuinely
stresses this rule, plus a per-function `classify_test` for the line-anchor split.
Frozen thresholds unchanged; recall and precision both stay at 1.00.

## Deferred (out of scope — Info findings, not part of `--fix`)

### IN-01: benign `metadata.guard.rules` serializes as `null`, not omitted
`loader.go` builds the guard sub-key as a raw `map[string]any` with `"rules":
p.Verdict.Rules`, bypassing the `omitempty` tag so a benign page emits
`"rules":null`. Harmless for OWUI (ignores unknown keys). Not fixed this pass.

### IN-02: `<title>` open-tag match did not require a terminator
**Effectively addressed** as a side effect of the WR-01 fix — `extractTitle` now
requires a tag terminator after `<title`, so `<titlebar>`/`<titlexyz>` no longer match
(covered by `TestTitleInjectionFlagged/non-title-prefixed-element-not-matched`). No
separate commit; folded into `41a749f`.

### IN-03: `Page.Source` is the raw input URL, not the post-redirect final URL
A citation-honesty nuance (SSRF is still enforced per-hop). Not a security hole;
deferred.

---

_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
