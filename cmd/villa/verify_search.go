package main

// verify_search.go holds the TESTABLE SPINE of `villa verify search` — the PRIV-08
// bounded-outbound honesty proof. This file is the PURE, off-hardware-unit-testable
// half of the v1.4 four-layer verify harness (the live netns/nft seam and the cobra
// wiring land in Plan 02):
//
//  1. Verdict type — searchProof carries a THREE-state PASS / FAIL / REJECT status.
//     memoryProof (PASS/FAIL only) is INSUFFICIENT here: SC2 mandates a REJECT state
//     DISTINCT from FAIL — an ineffective/unconductable proof is an HONEST infra-fail
//     (REJECT), NEVER a fabricated PASS and NEVER conflated with a security FAIL.
//  2. Pure core — evalSearchVerify maps the injected probe outcomes to the verdict,
//     asserting the positive control (allowlist reachable) and the negative control
//     (canary reachable UNGUARDED) FIRST, then the inverse-framed under-bound assertion.
//     It takes only func seams, so it does ZERO host I/O (no exec, no net, no http).
//  3. Curl-exit classifier — classifySearchProbe maps a curl exit code to blocked /
//     reachable / could-not-run, cloning classifyEgressProbe's 6/7/28-vs-everything-else
//     honesty map (here the everything-else branch maps to an error → the caller REJECTs).
//  4. In-process family drivers — injectionFlagged (family b) and ssrfBlocked (family c)
//     assert the SHIPPED Phase-31/32 websafe guard in-process (no network, no live bound).
//
// The verdict→exit map (PASS→exitPass, FAIL→exitBlocked, REJECT→exitWarn), the live
// `unshare -rn` + nft bound seam, the family-(d) live secret-query driver + its
// TestSearchSecretQuery, and the cobra registration all land in Plan 02. This file is
// deliberately host-free so the load-bearing, easy-to-invert truth table — where a
// false-green is most dangerous — is pinned by unit tests FIRST.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// searchStatus is the THREE-state verdict for `villa verify search` (SC2): PASS, a
// security FAIL, or an honest-infra REJECT (the proof could not be conducted honestly —
// tooling absent, env broken, allowlist unreachable, canary already unreachable
// unguarded, or an ineffective blanket block). REJECT is DISTINCT from FAIL and must
// NEVER be a fabricated PASS. The verdict→exit map lives in Plan 02
// (PASS→exitPass, FAIL→exitBlocked, REJECT→exitWarn).
type searchStatus int

const (
	searchPass searchStatus = iota
	searchFail
	searchReject
)

// searchProof is the verify-search verdict value (shape cloned from memoryProof,
// install_memory.go:188, extended to three states). detail carries refuse-with-
// remediation text on EVERY non-PASS outcome (the preflight refuse-with-remediation
// invariant).
type searchProof struct {
	status searchStatus
	detail string
}

// reject / fail / pass are small constructors so the pure core reads as the locked
// truth-table order and every non-PASS verdict is forced to carry a detail string.
func reject(detail string) searchProof { return searchProof{status: searchReject, detail: detail} }
func fail(detail string) searchProof   { return searchProof{status: searchFail, detail: detail} }
func pass(detail string) searchProof   { return searchProof{status: searchPass, detail: detail} }

// evalSearchVerify is the PURE inverse-framing bounded-outbound proof core (PRIV-08,
// SC2), unit-testable off-hardware via its injected probe seams — it does ZERO host I/O
// (no exec, no net, no http). Shape cloned from evalAgentVerify (verify_agent.go:62),
// extended from PASS/FAIL to PASS/FAIL/REJECT. The order is LOCKED (RESEARCH Pattern 3 /
// 33-PATTERNS.md:73-80) and is the load-bearing logic SC2 forbids inverting:
//
//  1. Positive control FIRST: an allowlisted upstream MUST be reachable in the probe
//     environment. If not (err or false), the environment cannot conduct the proof →
//     REJECT ("could not run the proof"), never a false block.
//  2. Negative control: the off-allowlist canary MUST be reachable UNGUARDED. An err →
//     REJECT (probe could not run). Reachable==false → REJECT (the empty-netns trap:
//     you cannot prove a BLOCK of something that was ALREADY unreachable). NEVER a PASS.
//  3. Apply the bound, then re-probe: the canary MUST now be UNREACHABLE. If it is STILL
//     reachable under the bound the block is INEFFECTIVE → FAIL (the inversion trap — a
//     security violation, NEVER a fabricated PASS). If the bound ALSO blocked the
//     allowlist (allowlist unreachable under bound) it is a blanket block, not an
//     allowlist → REJECT (proves nothing about bounded-not-blanket). A boundThen err →
//     REJECT (could not apply/probe the transient bound).
//  4. In-process families: planted-injection page NOT stripped+fenced+flagged → FAIL;
//     an SSRF internal-host case NOT blocked → FAIL; the secret-in-query-string exfil
//     reaching the canary under the bound (err or not-blocked) → FAIL.
//  5. Only if every clause holds → PASS, with a detail string.
//
// Why STILL-reachable is FAIL (not REJECT) while a blanket/broken env is REJECT: a
// canary that is still reachable under a constructed bound means the SECURITY property
// (bounded outbound) was DEMONSTRABLY violated — the proof ran and a guarantee failed.
// A broken/unroutable/blanket environment means the proof could not be CONDUCTED at all
// — an honest infra-fail. Conflating the two is exactly the SC2 false-green hazard.
func evalSearchVerify(
	allowlistReaches func() (bool, error), // positive control: allowlisted upstream reachable unguarded
	canaryUnguarded func() (bool, error), // negative control: off-allowlist canary reachable UNGUARDED
	boundThen func() (canaryReachable, allowlistReachable bool, err error), // under the transient bound
	injectionFlagged func() (stripped, fenced, flagged bool), // family (b)
	ssrfBlocked func() bool, // family (c)
	secretQueryBlocked func() (bool, error), // family (d)
) searchProof {
	// 1) Positive control FIRST — an allowlisted upstream must be reachable, else the
	//    probe environment is broken and cannot conduct the proof (REJECT, never a block).
	ok, err := allowlistReaches()
	if err != nil {
		return reject(fmt.Sprintf("the allowlisted-upstream positive control could not run (%v) — the probe environment is broken; cannot conduct the bounded-outbound proof. Verify network egress works on this host, then re-run `villa verify search`", err))
	}
	if !ok {
		return reject("the allowlisted upstream is not reachable in the probe environment — cannot conduct the proof (an unroutable environment must REJECT, never false-pass). Verify network egress works on this host, then re-run `villa verify search`")
	}

	// 2) Negative control — the off-allowlist canary must be reachable UNGUARDED. If it
	//    was ALREADY unreachable, there is nothing to prove a block of (the empty-netns
	//    trap): REJECT, never a fabricated PASS.
	reach, err := canaryUnguarded()
	if err != nil {
		return reject(fmt.Sprintf("the off-allowlist canary probe could not run unguarded (%v) — cannot establish the negative control; refusing to declare a bound. Verify the canary host is probeable, then re-run `villa verify search`", err))
	}
	if !reach {
		return reject("the off-allowlist canary was ALREADY unreachable unguarded — cannot prove a block of something already unreachable (the empty-netns trap). Run the probe where egress actually works, then re-run `villa verify search`")
	}

	// 3) Under the bound — the canary MUST be unreachable; the allowlist MUST stay
	//    reachable. STILL-reachable canary = ineffective block = FAIL (the inversion trap;
	//    NEVER a fabricated PASS). Allowlist-also-blocked = blanket, not allowlist = REJECT.
	canaryReach, allowReach, err := boundThen()
	if err != nil {
		return reject(fmt.Sprintf("could not apply or probe the transient egress bound (%v) — cannot conduct the proof under a bound; refusing to declare bounded. Ensure nft/unshare are available, then re-run `villa verify search`", err))
	}
	if canaryReach {
		return fail("off-allowlist canary STILL reachable under the bound — the block is INEFFECTIVE; FAILS verification (never a fabricated PASS). Fix the egress bound so off-allowlist hosts are dropped, then re-run `villa verify search`")
	}
	if !allowReach {
		return reject("the bound blocked the ALLOWLIST too — that is a blanket block, not an allowlist, and proves nothing about bounded-not-blanket egress. Widen the allowlist to the sanctioned upstreams, then re-run `villa verify search`")
	}

	// 4) In-process families (b)/(c)/(d) — any violation is a security FAIL.
	stripped, fenced, flagged := injectionFlagged()
	if !(stripped && fenced && flagged) {
		return fail(fmt.Sprintf("the planted-injection page was NOT stripped+fenced+flagged (stripped=%t, fenced=%t, flagged=%t) — the websafe guard did not defang untrusted web content; FAILS verification. Inspect internal/websafe, then re-run `villa verify search`", stripped, fenced, flagged))
	}
	if !ssrfBlocked() {
		return fail("an SSRF internal-host case was NOT blocked — the websafe SSRF guard let an internal address through; FAILS verification. Inspect internal/websafe/ssrf.go, then re-run `villa verify search`")
	}
	blk, err := secretQueryBlocked()
	if err != nil || !blk {
		return fail(fmt.Sprintf("the secret-in-query-string exfil reached the off-allowlist canary under the bound (blocked=%t, err=%v) — outbound is NOT contained; FAILS verification. Fix the egress bound, then re-run `villa verify search`", blk, err))
	}

	return pass("outbound bounded (off-allowlist canary blocked, allowlist reachable under the bound); planted injection stripped+fenced+flagged; SSRF internal-host case blocked; secret-in-query-string contained")
}

// classifySearchProbe is the PURE curl-exit classifier the live (Plan 02) bound probe
// delegates its reachability verdict to — the live podman/curl exec is not driveable
// off-hardware, but this is. It clones classifyEgressProbe (verify_agent.go:131); the
// ONLY semantic difference is that an unclassified failure here is surfaced as an error
// the CALLER maps to REJECT (not FAIL), matching the three-state taxonomy.
//
//   - sanityOrControlErr != nil: the in-network/positive sanity probe failed, so the
//     probe environment is wholesale-broken → ERROR (the caller REJECTs "could not run
//     the proof"). NEVER blocked=true — that is exactly the false-green this forbids.
//   - exit 0 (externalErr == nil): the host answered → reachable (blocked=false).
//   - a curl CONNECTION/TIMEOUT exit (6/7/28): the host is genuinely unreachable →
//     blocked=true (the desired, proven-blocked outcome).
//   - any OTHER non-zero exit (e.g. 127 curl-absent) or a container that never started:
//     "the probe could not run" → ERROR (caller REJECTs), NEVER a false block.
func classifySearchProbe(sanityOrControlErr error, externalExitCode int, externalErr error) (blocked bool, err error) {
	if sanityOrControlErr != nil {
		return false, fmt.Errorf("the verify-search probe environment is broken: the positive sanity control failed (%w) — verify host egress and the helper image, then re-run `villa verify search`", sanityOrControlErr)
	}
	if externalErr == nil {
		// Exit 0 — the host answered. Reachable, not blocked.
		return false, nil
	}
	switch externalExitCode {
	case curlExitCouldNotResolve, curlExitFailedToConnect, curlExitOperationTimeout:
		// A real connection/timeout failure → the host is genuinely unreachable.
		return true, nil
	default:
		// Non-zero but not a connection/timeout code (e.g. 127 curl-absent), or a
		// container that never started (-1). "The probe could not run" → REJECT-bound
		// error, NEVER a false block.
		return false, fmt.Errorf("the verify-search reachability probe could not run (exit %d: %w) — this is NOT proof of a block; verify the helper image has curl and the bound netns is reachable, then re-run `villa verify search`", externalExitCode, externalErr)
	}
}

// injectionFlagged is the in-process family-(b) driver (PRIV-08): it asserts the SHIPPED
// Phase-32 websafe guard defangs an UNTRUSTED web page WITHOUT any network or live bound.
// It builds a websafe.Loader over the injected (test-stub) *http.Client, fetches the
// planted URL, and reports whether the produced Page was:
//
//   - stripped: the active markup tag (e.g. <script>) was removed from Page.Content;
//   - fenced:   the [UNTRUSTED_WEB_CONTENT nonce=…] provenance fence is present;
//   - flagged:  the heuristic classifier set Page.Verdict.Detected with named Rules.
//
// It reuses the guard via websafe.NewLoader (never re-implements detection/sanitize),
// and DefaultBounds() supplies the conservative resource limits. The planted-injection
// page itself is a TEST input (it lives in verify_search_test.go), injected via client.
func injectionFlagged(client *http.Client, activeMarkup, plantedURL string) (stripped, fenced, flagged bool) {
	loader := websafe.NewLoader(websafe.Deps{Client: client}, websafe.DefaultBounds())
	pages := loader.Load(context.Background(), []string{plantedURL})
	if len(pages) == 0 {
		// The guard fail-closed and omitted the page (skip-and-continue). With nothing
		// produced we cannot assert strip+fence+flag — report all false so the pure core
		// FAILs the family rather than silently treating an omitted page as success.
		return false, false, false
	}
	p := pages[0]
	stripped = !strings.Contains(p.Content, activeMarkup)
	fenced = strings.Contains(p.Content, "UNTRUSTED_WEB_CONTENT nonce=")
	flagged = p.Verdict.Detected && len(p.Verdict.Rules) > 0
	return stripped, fenced, flagged
}

// ssrfBlocked is the in-process family-(c) driver (PRIV-08): it asserts the SHIPPED
// GUARD-05 SSRF guard REFUSES an internal-host URL via the exported wiring, WITHOUT
// reaching any real network. It drives websafe.SafeClient(DefaultBounds()) — whose
// connect-time Control hook rejects internal/reserved IPs and whose CheckRedirect
// rejects internal hostnames — against the given internal URL and returns true iff the
// request is REFUSED (an error, no successful response). A nil error (the request
// somehow succeeded) means the guard did NOT block → false.
func ssrfBlocked(internalURL string) bool {
	client := websafe.SafeClient(websafe.DefaultBounds())
	resp, err := client.Get(internalURL)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return err != nil
}
