---
phase: 27-install-addon-preflight-gates-villa-verify-agent
fixed_at: 2026-06-14T00:00:00Z
review_path: .planning/phases/27-install-addon-preflight-gates-villa-verify-agent/27-REVIEW.md
iteration: 1
findings_in_scope: 3
fixed: 3
skipped: 0
status: all_fixed
---

# Phase 27: Code Review Fix Report

**Fixed at:** 2026-06-14
**Source review:** .planning/phases/27-install-addon-preflight-gates-villa-verify-agent/27-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 3 (WR-01, WR-02, WR-03 — the 4 Info findings were OUT of scope and not touched)
- Fixed: 3
- Skipped: 0

All fixes were applied in an isolated git worktree, each committed atomically,
and validated with the full `go vet ./...` + `go test ./...` suite (1253 tests
across 24 packages, all green) plus the honesty-critical `TestSeamGrepGate`
(internal/inference) and the WR-01/WR-02 truth-table tests. `gofmt` clean on all
touched files.

## Fixed Issues

### WR-02: Egress external probe uses `-f`, so a reachable-but-erroring host is classified as infra-failure, not "not blocked"

**Files modified:** `cmd/villa/verify_agent.go`
**Commit:** e2f2bc6
**Status:** fixed (requires human verification — behavioral change on an honesty-critical security path)

**Applied fix:** Dropped curl's `-f` (fail-on-HTTP-error) flag from the external
negative-control probe (`runProbeCurlCode(ctx, helperImage, "-s", "--max-time",
"5", egressNegativeControlHost)`), reasoning recorded in an expanded code comment.

**Why this is the right shape (and strictly better, never weaker):**
- The phase verifier confirmed the *prior* behaviour was already FAIL-SAFE: a
  reachable-but-HTTP-erroring host (curl exit 22 under `-f`) routed through
  `classifyEgressProbe`'s `default → infra-error → FAIL` branch and could NEVER
  produce a false PASS or `blocked=true`. So the requirement was to IMPROVE
  honesty/precision without weakening the fail-closed property.
- With `-f` removed, ANY HTTP response (including 4xx/5xx — rate-limit, captcha,
  transient outage) returns curl exit 0 ⇒ the existing `externalErr == nil`
  branch of `classifyEgressProbe` ⇒ `blocked=false` ⇒ `evalAgentVerify` FAILs
  with the precise "egress is NOT blocked: an external host was reachable"
  message. A demonstrably-reachable host now correctly reads as egress-OPEN
  (the security FAIL it should be), instead of being excused as a probe-infra
  problem (the WR-02 false-negative).
- **Fail-closed preserved:** a genuine block still surfaces as curl exit 6/7/28
  → the `curlExitCouldNotResolve/FailedToConnect/OperationTimeout` cases →
  `blocked=true`; a broken probe environment still surfaces via `sanityErr` or
  an unclassified exit → infra FAIL. A reachable-erroring host can NO LONGER
  read as "blocked".
- **`classifyEgressProbe` was NOT changed** — its truth table (locked by
  `TestClassifyEgressProbe`) already maps exit 0 → not-blocked. The fix lives
  entirely in what the external probe *sends*, so the pure-classifier tests stay
  green and the cardinal "infra failure must never read as blocked" invariant is
  untouched.
- **Seam gate:** no new backend/host literal in `cmd/villa`; the in-network
  endpoint, helper image, and target host all stay behind their existing
  accessors. `TestSeamGrepGate` passes.

**Human-verification note:** this changes the runtime behaviour of an honesty
gate (PRIV-06). The logic is straightforward and fully covered by the existing
classifier truth-table tests, but a maintainer should confirm on-hardware that
`curl -s` (without `-f`) against the negative-control host returns exit 0 for an
HTTP response when egress is open, as expected, before the phase proceeds.

### WR-01: `runProbeCurlCode` exit-code extraction is untested on the honesty-critical path

**Files modified:** `cmd/villa/install_memory.go`, `cmd/villa/install_memory_test.go`
**Commit:** bbc7b4e
**Status:** fixed

**Applied fix:** Extracted the load-bearing `errors.As(runErr, &exitErr)` →
`exitErr.ExitCode()` vs. `-1`-never-started mapping out of `runProbeCurlCode`
into a small pure helper `extractExitCode(runErr error) int`, then added
`TestExtractExitCode` that anchors it against the REAL `os/exec` runtime:
- a process that genuinely exits non-zero (`sh -c 'exit 7'`, a compile-time
  constant — no interpolation, no injection surface) must yield `7` (mirrors the
  curl 6/7/28 genuine-block path surfacing as the container process's exit code);
- a never-started binary (nonexistent command) must yield `-1` and must NOT be an
  `*exec.ExitError` (the infrastructure / "probe could not run" case the
  classifier must read as not-a-block).

This directly addresses the reviewer's concern: the WR-01 honesty fix rested on
this mapping being correct on the real host, but only the synthetic-code
classifier was tested. A future Go/exec behaviour drift in the ExitError →
ExitCode path is now caught here rather than silently miscategorizing a timeout
as a block (or vice-versa) at runtime. `runProbeCurlCode`'s external behaviour is
unchanged (it now delegates to the helper).

### WR-03: `--coding-agent` with only shared-residency coder fit hard-blocks the install with a misleading "no coder fits" message

**Files modified:** `cmd/villa/install.go`, `cmd/villa/install_test.go`, `internal/recommend/coder.go`
**Commit:** 205b90a
**Status:** fixed

**Applied fix:** Added an explicit shared-residency branch at step 6c
(`cmd/villa/install.go`) BEFORE the `coderShardFor` no-fit gate: when
`rec.Coder.Residency == recommend.ResidencyShared`, the addon refuses with a
distinct message that names the v1.4 swap-only limitation ("the coding-agent
addon currently requires a swap-residency coder fit, but this host only supports
SHARED residency ... This is a swap-only limitation, not a memory shortfall;
freeing memory will not help") instead of the misleading "free memory / use a
larger-envelope host" copy. The generic no-fit copy is reserved for a genuine
catalog miss.

**Adaptation from the reviewer's suggested condition (important):** the review
suggested keying off `rec.Coder.Fits && rec.Coder.Residency == "shared"`. I read
the recommender source (`internal/recommend/coder.go`) and confirmed that
combination is **unreachable**: `Fits=true` is set ONLY together with
`Residency="swap"` and a non-empty `Model`; the shared path (`sharedCoderFit()`)
is always `Fits=false, Residency="shared", Model=""`. So I keyed the branch off
`rec.Coder.Residency == recommend.ResidencyShared`, which is the actual reachable
case the misleading message hit. This is the corrected, semantically-accurate
form of the reviewer's intent.

To avoid a re-typed `"shared"` string literal in `cmd/villa`, I exported
`ResidencySwap`/`ResidencyShared` from `internal/recommend` (previously
unexported `residencySwap`/`residencyShared`) and updated the two internal
references plus the install caller and tests to use the constant. (These are
recommend-domain values, not backend/host markers, so `TestSeamGrepGate` is
unaffected — confirmed green.)

**Test changes:** the existing "no coder fit refuses-with-remediation" subtest
used a shared-residency fixture and asserted the old generic message; I renamed
it to "shared-residency coder fit refuses with a swap-only message, NOT
free-memory copy (WR-03)" and tightened the assertions to (a) require the
swap-only wording and (b) assert the misleading free-memory copy is ABSENT — the
cardinal WR-03 point. (A genuine-catalog-miss subtest was considered but dropped:
that path drives the real embedded catalog via `codingModelFile` and is not
hermetically reachable through the injected `agentCat` seam, so testing it here
would be fragile and is outside WR-03's scope.)

## Skipped Issues

None — all three in-scope findings were fixed.

---

_Fixed: 2026-06-14_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
