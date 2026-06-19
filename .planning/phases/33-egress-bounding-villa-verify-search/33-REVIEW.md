---
phase: 33-egress-bounding-villa-verify-search
reviewed: 2026-06-19T00:00:00Z
depth: deep
files_reviewed: 6
files_reviewed_list:
  - cmd/villa/verify_search.go
  - cmd/villa/verify_search_json.go
  - cmd/villa/verify.go
  - cmd/villa/verify_search_test.go
  - internal/orchestrate/openwebui_test.go
  - internal/websafe/ssrf_test.go
findings:
  critical: 2
  warning: 4
  info: 3
  total: 9
status: issues_found
---

# Phase 33: Code Review Report

**Reviewed:** 2026-06-19
**Depth:** deep
**Files Reviewed:** 6
**Status:** issues_found

## Summary

Phase 33 ships `villa verify search` — the PRIV-08 bounded-outbound honesty proof. The
**pure verdict core (`evalSearchVerify`) is excellent and correct**: the inverse-framing
order is genuinely non-invertible, canary-still-reachable-under-bound is FAIL (never PASS),
the empty-netns / blanket-block / broken-env cases REJECT distinctly, and the unit tests
(`TestEvalSearchVerify`, `TestEvalSearchVerifyInverse`, `TestSearchSecretQueryDrivesFailNotPass`)
pin the load-bearing truth table directly. The 3-state→exit mapping, the `--json`
byte-frozen contract, the no-shell-interpolation discipline (`nft --file -` on STDIN,
fixed-arg `exec.Command`, netip-validated allowlist IPs, constant secret token as one URL
arg), and the PRIV-07/09 regression guards are all sound. `go build` and the 42 search
tests pass.

However, the **live host seam (`liveSearchVerify`) does not actually exercise the property
the pure core verifies**, and two of the six probes are wired against inputs that cannot
demonstrate the asserted result. The pure spine is honest; the live composition that feeds
it is not — which is precisely the false-green hazard SC2 forbids, just relocated from the
verdict math to the probe wiring.

## Critical Issues

### CR-01: The transient egress bound is applied in a throwaway netns the probes never enter — the bound has ZERO effect on the canary probe

**File:** `cmd/villa/verify_search.go:495-515` (`applySearchBound`), composed in `boundThen` at `418-454`

**Issue:** `applySearchBound` applies the nft ruleset via `unshare -rn nft --file -`. `unshare -rn` creates a *new, anonymous* network namespace, runs `nft` inside it, and that namespace is **destroyed the moment the `unshare` process exits** (which is before the function returns — `cmd.CombinedOutput()` waits for exit at line 511). The returned `release` is a no-op (line 514), and the comment at 506-508 concedes the netns "auto-tears-down when the process exits."

The subsequent under-bound probes (`boundThen` lines 435, 440, 447) run via `runProbeCurlCode`, which is `podman run --rm --network villa …` (`install_memory.go:384-392`). That podman container runs in the **host network stack / the `villa` podman network** — an entirely different namespace from the destroyed `unshare -rn` netns. The nft `table inet villabound` drop policy was applied to a namespace that no longer exists and that the probe never enters.

Consequence: under the "bound," the off-allowlist canary is probed with **no firewall in effect**. On a normal host with egress, the canary curl returns exit 0 (reachable) → `canaryReach == true` → the pure core correctly returns **FAIL** ("canary STILL reachable under the bound"). So `villa verify search` will FAIL on every healthy host *not because the bound is ineffective, but because the bound was never applied to the probe's network at all*. The proof is structurally incapable of producing a PASS. Worse, if the host happens to have no egress, the negative control at step 2 (`canaryUnguarded`) REJECTs first — so there is no host configuration under which this command can return PASS. The honesty core is intact, but the live mechanism it composes is a no-op firewall.

The code comments (493-494, 506-508) explicitly defer the "exact rootless-netns attach point (architecture A vs B)" to "Plan 03, on-hardware." This means the seam shipped in this phase is a **known-non-functional placeholder**, not a working bound. That must not merge as a passing verify command.

**Fix:** The bound and the probe MUST share one network namespace. Either (A) run the probe *inside* the same persistent netns the rules were applied to:

```go
// apply rules to a NAMED, persistent netns, then run curl inside THAT netns:
//   ip netns add villabound33
//   ip netns exec villabound33 nft --file -        (ruleset on STDIN)
//   ip netns exec villabound33 curl -s --max-time 5 <canary>
// release() = ip netns del villabound33   (REAL teardown, not a no-op)
```

or (B) use podman's rootless-netns / `--network ns:/var/run/netns/villabound33` so the `podman run` probe joins the bounded namespace. Until the probe demonstrably runs *behind* the applied ruleset, do not let the verb claim a PASS. If on-hardware finalization is genuinely required, the command should REJECT ("bound mechanism not finalized") rather than ship a placeholder that always FAILs.

### CR-02: Family-(b) injection probe in the live path is wired against the real allowlist URL (benign Wikipedia), which cannot be stripped+fenced+flagged — guarantees a spurious FAIL

**File:** `cmd/villa/verify_search.go:457-459` (the `injection` closure inside `liveSearchVerify`)

**Issue:** The live family-(b) driver is:

```go
injection := func() (stripped, fenced, flagged bool) {
    return injectionFlagged(websafe.SafeClient(websafe.DefaultBounds()), "<script>", allowlistURL)
}
```

`allowlistURL` is `"https://en.wikipedia.org/"` (line 396). `injectionFlagged` fetches that **real** URL through the live SafeClient and asserts the returned page was `stripped` (no `<script>`), `fenced`, and `flagged`. But:

- `flagged = p.Verdict.Detected && len(p.Verdict.Rules) > 0` (line 221). `classify` only sets `Detected=true` when an injection rule-family phrase matches (`internal/websafe/classify.go:151`). A benign Wikipedia homepage contains **no prompt-injection imperatives**, so `Detected=false` → `flagged=false`.
- Therefore `injectionFlagged` returns `flagged=false`, and the pure core's `!(stripped && fenced && flagged)` clause (line 149) returns **FAIL** — "the planted-injection page was NOT stripped+fenced+flagged."

The unit test `TestSearchInjectionFlagged` passes only because it injects a `stubRoundTripper` serving `plantedInjectionPage`. The *live* wiring uses a real, benign URL and a transport that actually fetches it. The live family-(b) probe is structurally incapable of returning the asserted result — it will FAIL (or, if Wikipedia is unreachable, `Load` returns an empty slice → all-false → FAIL). This is a vacuously-broken probe: it does not test the websafe guard against any planted injection; it tests whether wikipedia.org happens to look like an attack page (it does not).

**Fix:** Drive family-(b) against a **planted** injection page via an in-process stub transport (exactly as the test does), not against the live allowlist URL:

```go
injection := func() (stripped, fenced, flagged bool) {
    client := &http.Client{Transport: stubRoundTripper{body: plantedInjectionPage}}
    return injectionFlagged(client, "<script>", "https://villa.invalid/planted")
}
```

Family (b) is an *in-process guard assertion* (the doc at 197-208 says "WITHOUT any network or live bound") — it must use a controlled planted input, never the real upstream. Promote `plantedInjectionPage`/`stubRoundTripper` out of `_test.go` (or define a non-test equivalent) so the live path can use them.

## Warnings

### WR-01: `secretUnderBound` package-global is mutable cross-invocation state; the doc comment claims otherwise

**File:** `cmd/villa/verify_search.go:479-487`

**Issue:** `secretUnderBound` is a package-level `var struct{ran,blocked bool; err error}`. The comment at 481-482 asserts it is "a request-scoped value, not shared mutable state across invocations." That is factually wrong — it is process-global shared state. It is *currently* safe only because the CLI is single-shot and `liveSearchVerify` is never called concurrently or re-entrantly. But the value is also reset (`secretUnderBound.ran = false`, line 468) *after* `boundThen` is constructed but is read inside a closure; the ordering correctness relies entirely on the pure core invoking `boundThen` before the `secret` closure. This is fragile coupling between the pure core's clause order and a live global. A future reorder of `evalSearchVerify`'s family clauses (which the pure core is free to do — it knows nothing of this global) would silently break the family-(d) result plumbing.

**Fix:** Eliminate the global. Capture the secret result in a local closure variable inside `liveSearchVerify` (closures already share the lexical scope), or have `boundThen` return the secret outcome alongside the canary/allow reaches. A local `var secret struct{...}` declared in `liveSearchVerify` and closed over by both `boundThen` and the `secret` func is a one-line change that removes the package-global and makes the lifetime genuinely request-scoped.

### WR-02: Live family-(d) secret-query probe shares CR-01's broken bound — it proves nothing about containment

**File:** `cmd/villa/verify_search.go:445-451` (inside `boundThen`)

**Issue:** The secret-in-query probe (`runProbeCurlCode(..., secretExfilURL())` at line 447) runs in the same `podman run --network villa` context as the canary probe, i.e. *outside* the throwaway `unshare -rn` netns (see CR-01). So on a host with egress, the secret-bearing request **reaches the canary host** (exit 0) → `secretQueryBlocked` returns `(false, nil)` → the pure core FAILs family (d). The probe is real and the classification is correct, but because the bound never applies to it, it can only ever report "the secret escaped" on a working host. Until CR-01 is fixed, family (d) is a guaranteed-FAIL, not a containment proof.

Note also (positive): the token is correctly a fixed constant carried as one URL arg (`secretExfilURL`, lines 262-268), never shell-interpolated and never logged in plaintext beyond the URL — that part is sound.

**Fix:** Resolved transitively by CR-01 (run the probe inside the bounded netns). No separate change needed once the bound and probe share a namespace.

### WR-03: `resolveAllowlistIPs` silently drops all IPv6 addresses; an IPv6-only resolution path REJECTs with a misleading message

**File:** `cmd/villa/verify_search.go:359-378`

**Issue:** `resolveAllowlistIPs` keeps only `ip.Is4()` addresses (line 370) and errors if none remain (line 375). On a host whose resolver returns only AAAA records for `en.wikipedia.org` (IPv6-only network, or DNS64/NAT64 environments), this returns "resolved to no usable IPv4 address" → the bound cannot be built → REJECT. Meanwhile the `nft` table is `inet` (line 341, covers both families) and the comment at 371 acknowledges "the inet table also covers v6, but we pin v4 upstreams." The allowlist is thus v4-only by construction, but the canary probe (`egressNegativeControlHost`) may resolve and connect over v6 — meaning even with a *correct* bound (post-CR-01), a v6 canary path would not be matched by the v4-only `ip daddr` accept rules and the allowlist itself could be unreachable over v6 under the drop policy. This is a latent correctness gap once CR-01 is fixed.

**Fix:** Emit both `ip daddr` (v4) and `ip6 daddr` (v6) accept rules, collecting `ip.Is6()` addresses too:

```go
for _, ip := range allow {
    if ip.Is4() {
        fmt.Fprintf(&b, "        ip daddr %s accept\n", ip.String())
    } else {
        fmt.Fprintf(&b, "        ip6 daddr %s accept\n", ip.String())
    }
}
```

and keep both families in `resolveAllowlistIPs`. Otherwise the canary and allowlist must be forced onto v4 (`curl -4`) for the proof to be coherent.

### WR-04: Allowlist host (`en.wikipedia.org`) is resolved at bound-apply time but probed by a *separate* curl resolution — TOCTOU + IP-set drift can blanket-block the allowlist

**File:** `cmd/villa/verify_search.go:418-441`

**Issue:** `boundThen` resolves `en.wikipedia.org` → IP set A (line 419), builds nft accept rules for set A, then later runs `curl https://en.wikipedia.org/` (line 440) which performs its **own** DNS resolution → IP set B. For a large CDN-fronted host (Wikipedia is behind multiple anycast/edge IPs that rotate), set B may not be a subset of set A. Under the `policy drop` ruleset, any allowlist connection to an IP in B\A is dropped → `allowReach == false` → the pure core REJECTs as a "blanket block" (line 143-144). The proof becomes flaky: it can intermittently REJECT on a perfectly healthy host purely due to DNS answer rotation between the two resolutions. (This is latent until CR-01 is fixed, but it is a real design defect in the probe composition.)

**Fix:** Resolve once and force curl to that pinned IP via `--resolve en.wikipedia.org:443:<ip>` (and `--resolve` for the canary symmetrically), so the rule set and the probe target the identical address. Alternatively probe by the resolved IP directly. Pin the resolution to remove the TOCTOU window between rule-build and probe.

## Info

### IN-01: Dead/unused honesty branch in the live path (acceptable, but worth a note)

**File:** `cmd/villa/verify_search.go:401,408,436,441` and `classifySearchProbe:178-180`

**Issue:** Every live call to `classifySearchProbe` passes `nil` for `sanityOrControlErr`, so the sanity branch (178-180) is never exercised in production — only in `TestClassifySearchProbe`. Unlike `liveAgentVerify` (verify_agent.go:210-212), the search path has no positive *in-network* sanity probe before the negative control; it relies solely on the step-1 allowlist positive control. This is defensible (the comment at 287-288 explains it), but it means the `sanityOrControlErr` parameter is pure test surface in this file. Consider documenting that the positive control at `evalSearchVerify` step 1 is the sole environment check, so the unused parameter isn't mistaken for live coverage.

### IN-02: `liveSearchVerify`'s `loadedConfig`/`loadedWebSearchEnabled` seams are declared but the allowlist is hard-coded

**File:** `cmd/villa/verify_search.go:303-308,333,419`

**Issue:** `searchVerifyDeps.loadedConfig` is documented as resolving "the allowlist host(s) the bound permits" (line 304), but `liveSearchVerify` ignores `deps.loadedConfig` entirely and hard-codes `searchAllowlistHost = "en.wikipedia.org"` (line 333). The config seam is wired (`liveLoadedConfig`) but never read in the proof. Either consume the config-resolved allowlist (so the bound matches what `install` actually permits for SearXNG engines) or drop the unused seam to avoid implying config-drives-allowlist when it does not. As-is, a host whose sanctioned upstreams differ from Wikipedia would be proving the wrong allowlist.

### IN-03: Test seams (`stubRoundTripper`, `plantedInjectionPage`) live only in `_test.go`, blocking correct CR-02 live wiring

**File:** `cmd/villa/verify_search_test.go:261-277`

**Issue:** The planted-injection stub transport and page constant the live path needs (per CR-02 fix) currently exist only in the test file and are therefore invisible to `verify_search.go`. Fixing CR-02 requires promoting a planted-page transport into non-test code. Flagging so the CR-02 fix is not blocked by build visibility. (The websafe guard assertions in `ssrf_test.go` and the PRIV-07/09 guards in `openwebui_test.go` are correct and meaningful — `TestOWUIKillEnvPresentBothViewsPRIV09` asserts all six kill-env keys in both views, and `TestOWUIWebOffByteIdenticalPRIV07` reuses the existing golden via `goldenCompare` rather than re-freezing it. No issues there.)

---

_Reviewed: 2026-06-19_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
