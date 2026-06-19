---
phase: 33-egress-bounding-villa-verify-search
plan: 03
subsystem: cmd/villa verify search — on-hardware finalization of the rootless-netns nft egress bound (architecture A)
tags: [PRIV-08, PRIV-09, verify-search, nft-bound, rootless-netns, architecture-A, CR-01, WR-02, WR-03, WR-04, ipv6, toctou, on-hardware]
requires:
  - cmd/villa/verify_search.go (Plan 01 pure spine evalSearchVerify/classifySearchProbe + Plan 02 live seam liveSearchVerify)
  - cmd/villa/install_memory.go (runProbeCurlCode — the `podman run --network villa` probe; memoryProofNetwork="villa")
  - internal/orchestrate (EmbedImage — the helper probe image)
provides:
  - liveBridgeInterface (resolves + validates the villa bridge ifname for the bound scope)
  - nftBoundRuleset(bridgeIf, allow) — forward-hook, iifname-scoped, BOTH-family (v4+v6) egress bound (architecture A)
  - applySearchBound — applies/tears down the bound INSIDE podman's rootless-netns (real teardown, CR-01 fix)
  - resolveAllowlistIPs — now keeps BOTH v4 AND v6 allowlist IPs (WR-02/WR-03)
  - resolveCurlPin — curl --resolve pin closing the WR-04 TOCTOU window
affects:
  - cmd/villa/verify_search.go (boundThen rewired to architecture A; positive-control inversion bug fixed)
  - cmd/villa/verify_search_test.go (TestNftBoundRuleset reshaped; TestNftBridgeIfPattern + TestResolveCurlPin added)
tech-stack:
  added: []
  patterns:
    - "architecture A: apply the nft FORWARD-hook bound inside podman's rootless-netns where `--network villa` egress is forwarded — the bound and the probe share ONE namespace (CR-01)"
    - "iifname-scoped forward drop (not a blanket policy drop) so the shared rootless-netns stack keeps connectivity; established,related accept keeps in-flight conns alive"
    - "BOTH-family bound: ip daddr + ip6 daddr accepts with a trailing iifname drop catch-all so an IPv6 egress path cannot bypass a v4-only block (WR-02)"
    - "curl --resolve pin to the rule-built allowlist IP closes the rule-vs-probe DNS TOCTOU (WR-04)"
    - "real teardown via `podman unshare --rootless-netns nft delete table` on EVERY exit path (Pitfall 6 — the rootless-netns outlives the verb)"
key-files:
  created: []
  modified:
    - cmd/villa/verify_search.go
    - cmd/villa/verify_search_test.go
decisions:
  - "Chose architecture A (apply the bound inside podman's rootless-netns) over architecture B (build veth+route inside a fresh `unshare -rn` netns). A is the only mechanism where the SAME namespace carries both the nft rule AND the `--network villa` probe's forwarded egress, so the block is proven by the RULE, not an absent route. B is heavier, rootless veth-to-host plumbing is fragile, and pasta is L4 so a host FORWARD rule would not apply (33-RESEARCH Pitfall 1/2). Verified exit-0 on the live Strix Halo host."
  - "Scoped the drop by `iifname <villa-bridge>` (e.g. podman3) instead of a blanket `policy drop`/`output` hook, because the rootless-netns is SHARED by the running villa stack — a blanket drop would break the live containers. iifname-scoping bounds exactly the bridge-forwarded traffic and the established,related accept preserves in-flight connections. Verified: the running stack still had egress immediately after teardown."
  - "Drop BOTH families (the trailing `iifname … drop` catches v4 and v6) and emit ip6 daddr accepts for v6 allowlist IPs (WR-02/WR-03). On this host the villa network is v4-only (10.89.2.0/24, no v6 subnet) so the canary's v6 attempt is forwarded via pasta and was genuinely caught (curl exit 7) — confirming a v4-only block would have leaked over v6."
  - "Pin the allowlist probe with curl --resolve to the rule-built IP (WR-04) so the nft accept and the probe target the IDENTICAL address — removing the CDN-rotation TOCTOU that could spuriously REJECT a healthy host as a blanket block."
  - "Bridge ifname is taken from `podman network inspect villa --format {{.NetworkInterface}}` and validated against ^[A-Za-z0-9_.-]{1,15}$ before it is ever %q-formatted into the ruleset — defense in depth so a hostile network config cannot inject nft syntax (it is not user input, but the ruleset is data fed to `nft --file -` on STDIN, never a shell)."
metrics:
  duration_min: 50
  completed: 2026-06-20
  tasks_completed: 1
  tasks_total: 2
status: in_progress
---

# Phase 33 Plan 03: On-Hardware Finalization of the Rootless-Netns Egress Bound (Architecture A) Summary

Finalized `liveSearchVerify`'s egress-bound seam to **architecture A** — applying the nft bound INSIDE podman's rootless-netns where `--network villa` container egress is actually forwarded — making `villa verify search` produce a **genuine on-hardware PASS** (the canary is reachable unguarded and blocked by the RULE, not an absent route). Resolves CR-01 (no-op bound), WR-02 (IPv6 bypass), WR-03 (v6 allowlist), WR-04 (DNS TOCTOU), and a positive-control inversion bug surfaced on-hardware. **Task 2 (the operator on-hardware checkpoint) is PENDING** — PRIV-08 is NOT yet fully closed.

## What Was Built (Task 1)

**Architecture A, finalized.** The Plan-02 seam shipped a known-non-functional placeholder: `applySearchBound` applied the ruleset via `unshare -rn nft` to a *throwaway anonymous netns* that was destroyed when the process exited, while the probes ran via `podman run --rm --network villa` in an *entirely different* namespace. The nft drop had **zero effect on the probe** (CR-01) — the canary was always probed UNGUARDED, so the verb could never PASS.

The fix puts the bound and the probe in **one shared namespace**:

- **`applySearchBound`** now applies the ruleset via `podman unshare --rootless-netns nft --file -` (ruleset on STDIN, fixed-arg, no shell). The returned `release` performs a **real teardown** (`podman unshare --rootless-netns nft delete table inet villabound`) on every exit path — the rootless-netns outlives the verb (it owns the running stack), so teardown is mandatory (Pitfall 6 / T-33-06).
- **`nftBoundRuleset(bridgeIf, allow)`** renders a **FORWARD-hook** drop scoped to the villa bridge interface: `iifname "<bridge>" ct state established,related accept`, one `ip daddr`/`ip6 daddr <ip> accept` per validated allowlist IP (BOTH families), then a trailing `iifname "<bridge>" drop` catch-all.
- **`liveBridgeInterface`** resolves the bridge ifname via `podman network inspect villa --format {{.NetworkInterface}}` and validates it against `^[A-Za-z0-9_.-]{1,15}$`.
- **`resolveAllowlistIPs`** now keeps BOTH v4 and v6 (WR-02/WR-03).
- **`resolveCurlPin`** builds a `curl --resolve host:443:<ip>` pin (prefers v4) so the rule and probe target the identical address (WR-04 TOCTOU close).

## Architecture Chosen: A (and why)

| | Architecture A (CHOSEN) | Architecture B (rejected) |
|---|---|---|
| Where the bound lives | podman's rootless-netns (has the villa bridge + default route via pasta-mirrored host iface) | a fresh `unshare -rn` netns with hand-built veth + default route |
| Does the probe traverse it | YES — `--network villa` egress is forwarded through this exact namespace | only if veth-to-host plumbing works (rootless, fragile) |
| Block proven by | the nft RULE (canary reachable unguarded first) | risk of being proven by an absent/broken route |
| Verdict | exit-0 verified on the live Strix Halo host | heavier; pasta is L4 so host FORWARD rules differ |

Architecture A is the only mechanism where the same namespace carries both the rule and the forwarded probe egress — the load-bearing requirement that makes the negative control real (33-RESEARCH Pitfall 1/2, Open Q2 resolved).

## On-Host Observations (prototyping + the real verb)

Helper image: `orchestrate.EmbedImage()` (vulkan-radv toolbox, has curl). Canary = `https://huggingface.co/` (off-allowlist). Allowlist = `en.wikipedia.org` (SearXNG general engine).

**Negative/positive controls (no bound):**
- canary HF via `--network villa` → HTTP 200, exit 0 (**reachable UNGUARDED** ✓)
- allowlist Wikipedia → HTTP 301, exit 0 (**reachable UNGUARDED** ✓)

**Under the architecture-A bound (iifname-scoped forward drop):**
- canary HF (v4) → curl exit 28 (timeout = **blocked by the rule** ✓)
- canary HF (v6, `-6`) → curl exit 7 (failed connect = **blocked**; confirms a v4-only block would have leaked over v6 — WR-02 is real ✓)
- family-(d) secret-in-query `?exfil=VILLA-SEARCH-EXFIL-CANARY-7741` → curl exit 28 (**secret did NOT reach the canary** ✓)
- allowlist Wikipedia (pinned via `--resolve`) → HTTP 301, exit 0 (**stays reachable — allowlist, not blanket block** ✓)

**Teardown / no residue:**
- `nft delete table inet villabound` → exit 0; `podman unshare --rootless-netns nft list tables | grep villabound` → none after every run.
- Post-teardown, a `--network villa` container curled HF → 200 (**stack connectivity restored** ✓). The 7 running villa containers stayed Up throughout.

**The real verb (`./villa verify search`):**
- `EXIT=0`, line: `outbound bounded (off-allowlist canary blocked, allowlist reachable under the bound); planted injection stripped+fenced+flagged; SSRF internal-host case blocked; secret-in-query-string contained`
- `./villa verify search --json` → `{"schema":1,"verdict":"PASS",...}`, EXIT=0.

**Negative-control sanity (proves non-invertibility):** built an *ineffective* bound that also accepts the canary IP → the canary stayed reachable (200) under the "bound" → the pure core maps this to FAIL (canary STILL reachable), NEVER a fabricated PASS. This proves the earlier PASS was rule-driven, not an empty-netns trick.

## WR-02 / WR-03 / WR-04 Disposition

- **WR-02 (IPv6 bypass): RESOLVED.** The bound drops BOTH families via the trailing `iifname … drop` catch-all; the on-host v6 canary attempt was genuinely blocked (exit 7). The inet table covers both families and the catch-all is not v4-scoped.
- **WR-03 (v6 allowlist dropped): RESOLVED.** `resolveAllowlistIPs` keeps v6 addresses and `nftBoundRuleset` emits `ip6 daddr <v6> accept`, so an allowlist reached over v6 is not blanket-blocked.
- **WR-04 (rule-vs-probe DNS TOCTOU): RESOLVED.** `resolveCurlPin` pins the allowlist probe to the rule-built IP via `curl --resolve`, so CDN answer rotation between rule-build and probe cannot spuriously REJECT.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Positive-control reachability was inverted in `liveSearchVerify`**
- **Found during:** Task 1, first real `./villa verify search` run (REJECTed at the allowlist positive control on a host where the allowlist was plainly reachable).
- **Issue:** `allowlistReaches` returned `classifySearchProbe(...)` directly = `(blocked, err)`, but the pure core's seam expects `(REACHABLE, err)`. A reachable allowlist (`blocked=false`) therefore read as `ok=false` → REJECT on every healthy host. (`canaryUnguarded` correctly inverted with `return !blocked`; the positive control did not.) This is a pre-existing Plan-02 defect that CR-01's no-op bound masked — with the bound non-functional the verb could never reach a state where this mattered.
- **Fix:** Mirror `canaryUnguarded` — classify, propagate any error, return `!blocked` as reachability.
- **Files modified:** `cmd/villa/verify_search.go` (`allowlistReaches` closure).
- **Verified:** the verb then PASSed (exit 0) on the live host.

This was directly blocking Task 1's goal (a genuine PASS), so it is an in-scope Rule 1 auto-fix, not a separate plan.

## New Pure-Classification Edges (unit tests added)

- `TestNftBoundRuleset` reshaped to the architecture-A forward-hook + iifname + BOTH-family shape, asserting the catch-all drop comes AFTER the accepts (order is load-bearing).
- `TestNftBridgeIfPattern` — the bridge-ifname validator accepts normal podman names and rejects anything carrying nft/shell syntax (spaces, braces, newlines, quotes, over-length).
- `TestResolveCurlPin` — prefers v4, falls back to v6, emits a single fixed `--resolve` pair, yields nil for an empty allow set.

## Verification

- `make check` — GREEN (go vet + full `go test ./...`).
- `go test -race ./cmd/villa/ -count=1` — GREEN (no data race; WR-01 request-scoped local confirmed safe under -race).
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — GREEN (no leaked image/backend literal; `podman` binary name is not gated, helper image still via `EmbedImage()`).
- `grep -vE '^[[:space:]]*//' cmd/villa/verify_search.go | grep -cE 'sh -c|bash -c'` → **0** (no shell interpolation; ruleset on STDIN, all execs fixed-arg).
- `make build` → `./villa` (19.9M) produced, so the operator can run `./villa verify search` in the Task 2 checkpoint.
- On-host: `./villa verify search` → PASS (exit 0); `--json` → schema-1 PASS; no `villabound` residue; the live stack retained connectivity throughout.

## PRIV-08 / Task 2 Status — PENDING (do NOT mark PRIV-08 fully complete)

Task 1 (the `type=auto` attach-point finalization) is COMPLETE and the verb genuinely PASSes on hardware. **Task 2 — the operator-driven `checkpoint:human-verify` (gate="blocking-human")** — is **PENDING the human checkpoint**. It covers the operator-run `./villa verify search` PASS confirmation, the family-(d) secret-query no-leak confirmation against the real nft block, and the **PRIV-09 no-outbound-HuggingFace-pull UAT under a real Open WebUI web search** (RESEARCH Open Q1 weight pre-staging: default A2 "none needed" carried; an escalation if grounding breaks). Until the operator signs off Task 2, PRIV-08 is not fully closed and PRIV-09's on-hardware no-HF-pull clause is unconfirmed.

## Self-Check: PASSED

- `cmd/villa/verify_search.go`, `cmd/villa/verify_search_test.go`, and this SUMMARY all exist on disk.
- Source markers verified: `liveBridgeInterface`, `resolveCurlPin`, the architecture-A apply (`podman unshare --rootless-netns nft --file -`), and the real teardown (`nft delete table inet villabound`) are all present.
- All verification gates green (`make check`, `-race`, `TestSeamGrepGate`, shell-interp grep == 0, `make build`); the live verb PASSes with no residue.
