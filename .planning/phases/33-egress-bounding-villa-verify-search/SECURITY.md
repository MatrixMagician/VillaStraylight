# SECURITY.md — Phase 33: Egress-Bounding + `villa verify search`

**Phase:** 33 — egress-bounding-villa-verify-search
**Audit date:** 2026-06-20
**ASVS Level:** L1
**Block policy:** block on high
**Threats closed:** 11 / 11 (10 mitigate + 1 accept)
**Threats open:** 0
**Adversarial stance:** FORCE — every mitigation assumed absent until a grep match / code path proved it present at the right location.

This audit verifies declared threat mitigations against the *implemented* code (read-only).
It does not scan for new vulnerabilities. Each threat resolves to CLOSED (mitigation
present), an accepted-risk entry, or OPEN. Evidence cites `file:line`.

---

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-33-01 | Repudiation / Info-disclosure (verdict math invertibility) | mitigate | CLOSED | `evalSearchVerify` locked order — positive-control-FIRST, canary-still-reachable ⇒ `fail(...)` at `cmd/villa/verify_search.go:142-144`; broken/blanket env ⇒ distinct `reject(...)` at `:117-122,128-133,139-147`. Inversion trap pinned by `TestEvalSearchVerifyInverse` ("STILL reachable under the bound"). |
| T-33-02 | Spoofing the verdict (empty/unroutable netns false-block) | mitigate | CLOSED | Architecture A: bound applied INSIDE podman's rootless-netns where `--network villa` egress flows — `applySearchBound` via `podman unshare --rootless-netns nft --file -` (`verify_search.go:645`); positive control + canary-reachable-UNGUARDED asserted first (`:114-133`); ineffective bound ⇒ FAIL, unroutable ⇒ REJECT (`:138-147`). On-hardware confirmed: PASS with genuine rule-driven block, ineffective bound ⇒ FAIL never PASS (33-VERIFICATION.md status=passed, commit 5c4360a). |
| T-33-03 | Tampering (shell / command injection) | mitigate | CLOSED | Fixed-arg `exec.Command` only; `nft --file -` reads ruleset on STDIN (`verify_search.go:645-646`); no `sh -c`/`bash -c` (non-comment grep = 0); allowlist IPs netip-validated before formatting (`resolveAllowlistIPs` `:415-432`, `netip.ParseAddr` `:422`); bridge ifname validated `^[A-Za-z0-9_.-]{1,15}$` (`nftBridgeIfPattern` `:602`, `liveBridgeInterface` `:617`); helper image only via `orchestrate.EmbedImage()` (`:471`, no re-typed literal — `TestSeamGrepGate` green); probe exec `runProbeCurlCode` fixed-arg "no shell" (`install_memory.go:392`). |
| T-33-04 | Tampering (indirect prompt injection — family b) | mitigate | CLOSED | `injectionFlagged` reuses shipped `websafe.NewLoader` guard, asserts stripped + `UNTRUSTED_WEB_CONTENT nonce=` fence + `Verdict.Detected`+Rules (`verify_search.go:211-225`); live clause drives a PLANTED page via in-process `plantedPageRoundTripper` (`:575-578`), not a benign URL (CR-02 fix, commit 9079c1f). `TestSearchLivePlantedInjectionFlagged` proves non-vacuity. Flag-not-block (no elimination claim). |
| T-33-05 | Info-disclosure (SSRF — family c) | mitigate | CLOSED | `ssrfBlocked` drives `websafe.SafeClient(DefaultBounds())` against an internal URL, true iff refused (`verify_search.go:260-267`); live clause uses `http://169.254.169.254/...` (`:581`). Family-c unit cases (`169.254.169.254`, `127.0.0.1`, `villa-*`, `localhost`, control) in `internal/websafe/ssrf_test.go` — suite green. |
| T-33-06 | Denial of service (bound left applied) | mitigate | CLOSED | Verify-time-only; `applySearchBound` returns a REAL teardown `release` (`nft delete table inet villabound` in same rootless-netns, `verify_search.go:651-657`); `boundThen` defers `release()` on EVERY exit path and downgrades a restore failure to an error ⇒ REJECT (`:534-538`). On-hardware: no `villabound` table lingers, stack connectivity retained (33-VERIFICATION.md). |
| T-33-07 | Info-disclosure (curl reachability misclassification) | mitigate | CLOSED | `curl -f` OMITTED on every probe — `grep -c '"-f"' verify_search.go` = 0; nft uses long flag `--file` (`:645`); probes use `"-s","--max-time","5"` (`:495,506,540,560`). A reachable-but-erroring host reads reachable (egress open ⇒ FAIL), never excused as REJECT. WR-02 documented at `:313`. |
| T-33-08 | Info-disclosure (lazy OWUI HF pull / telemetry) | mitigate | CLOSED | Kill-env unconditional in base env BEFORE the `if memoryEnabled` block: `HF_HUB_OFFLINE=1`, `ANONYMIZED_TELEMETRY=False`, `DO_NOT_TRACK=True`, `SCARF_NO_ANALYTICS=True`, `OFFLINE_MODE=True`, `ENABLE_VERSION_UPDATE_CHECK=False` (`internal/orchestrate/openwebui.go:158-164`). PRIV-09 regression `TestOWUIKillEnvPresentBothViewsPRIV09` asserts all six in both views (`openwebui_test.go`). On-hardware: env LIVE, no HF/CDN egress under a real web search (33-VERIFICATION.md). |
| T-33-09 | Tampering (silent scope reduction of opt-in) | mitigate | CLOSED | Gate reads already-shipped `WebSearchEnabled` (`internal/config/villaconfig.go:138`); web-off byte-identical regression `TestOWUIWebOffByteIdenticalPRIV07` reuses the v1.4 golden (no re-freeze). Assert-only honored: no Phase-33 commit touched openwebui.go / villaconfig.go / the OWUI golden (those changes are Phases 29-31). |
| T-33-10 | Info-disclosure (secret-in-query-string exfil — family d) | mitigate | CLOSED | `secretExfilURL` carries a FIXED constant token in the query string built in Go and passed as ONE fixed exec arg (`verify_search.go:284-296`) — never shell-interpolated, never logged in plaintext beyond the URL; `secretQueryBlocked` classifies via pure `classifySearchProbe` (`:317-320`); composed UNDER the bound as the 6th probe (`:558-564,595`). `secretQueryBlocked` ref count = 6 (defined + wired). `TestSearchSecretQuery` (×2) green incl. FAIL-not-PASS verdict case. On-hardware: secret CONTAINED (33-VERIFICATION.md). |
| T-33-SC | Tampering (npm/pip/cargo installs) | accept | CLOSED (accepted) | See Accepted Risks below. Zero external packages added this phase (`tech-stack.added: []` in all three SUMMARYs); host tools (nft/unshare/podman/curl) are OS-provided. `git show --stat` of Phase-33 commits shows no `go.mod`/`go.sum` change. |

---

## Disposition Detail — the central trap (T-33-02)

The load-bearing false-green hazard SC2 forbids is a fabricated PASS from an
unroutable/empty netns. Verified mitigation chain in implemented code:

1. **Bound applied where egress actually flows (architecture A).** `applySearchBound`
   applies the nft ruleset inside podman's rootless-netns via
   `podman unshare --rootless-netns nft --file -` (`verify_search.go:645`) — the same
   namespace the `--network villa` probe container's egress is forwarded through. The
   earlier Plan-02 placeholder (throwaway `unshare -rn` netns, CR-01 BLOCKER) was
   replaced on-hardware (commit 5c4360a) and is no longer present in the code.
2. **Canary proven reachable UNGUARDED first (positive control).** `canaryUnguarded`
   probes the off-allowlist canary (`https://huggingface.co/`) with no bound; an
   already-unreachable canary ⇒ REJECT, never PASS (`verify_search.go:127-133`).
3. **Block proven by the RULE.** `boundThen` re-probes under the bound; a canary STILL
   reachable ⇒ `fail(...)` (`:142-144`); the allowlist blocked too ⇒ `reject(...)`
   blanket-block (`:145-147`); apply/probe error ⇒ `reject(...)` (`:139-141`).
4. **Both-family bound (WR-02).** `inet` table with `ip daddr` + `ip6 daddr` accepts and
   a trailing `iifname … drop` catch-all (`nftBoundRuleset:390-407`) so an IPv6 path
   cannot bypass a v4-only block; `resolveAllowlistIPs` keeps both families (`:415-432`).
5. **TOCTOU close (WR-04).** `resolveCurlPin` pins the allowlist probe to the rule-built
   IP via `curl --resolve` (`:440-455`, applied at `:551-553`) so the nft accept and the
   probe target the identical address.

On real hardware (33-VERIFICATION.md, status=passed, human_verified 2026-06-20): genuine
canary block, ineffective bound ⇒ FAIL (never a fabricated PASS), clean teardown.

---

## Review-Finding Dispositions (cross-check)

The phase code review (33-REVIEW.md) raised 2 critical + 4 warning findings; all are
resolved in the audited code:

| Finding | Disposition | Verified in code |
|---------|-------------|------------------|
| CR-01 (no-op bound — throwaway netns) | FIXED in 33-03 (commit 5c4360a) | architecture A live at `verify_search.go:638-658`; placeholder gone |
| CR-02 (family-b probed benign URL) | FIXED (commit 9079c1f) | planted page via `plantedPageRoundTripper` `:575-578` |
| WR-01 (mutable package-global secret state) | FIXED (commit 384ed9c) | request-scoped local `secretUnderBound` `:481-485` |
| WR-02 (IPv6 bypass) | RESOLVED in 33-03 | both-family bound `nftBoundRuleset:396-403` |
| WR-03 (v6 allowlist dropped) | RESOLVED in 33-03 | `resolveAllowlistIPs` keeps v6 `:420-427` |
| WR-04 (rule-vs-probe DNS TOCTOU) | RESOLVED in 33-03 | `resolveCurlPin --resolve` pin `:440-455,551` |

---

## Accepted Risks Log

| ID | Risk | Rationale | Owner | Review trigger |
|----|------|-----------|-------|----------------|
| T-33-SC | Supply-chain risk from package-manager installs during this phase | No npm/pip/cargo/go-module installs occurred this phase. All three plan SUMMARYs declare `tech-stack.added: []`; host tools (nft, unshare, podman, curl) are OS-provided, not vendored. No `go.mod`/`go.sum` change in any Phase-33 commit. Residual risk = the pre-existing, already-audited dependency set (unchanged). | Phase owner | Any future addition of a dependency or a bundled host helper binary to the verify-search path. |

### Known residual (carried, not introduced this phase)

Per ROADMAP/STATE governing claim: *"Safe from injection" is NOT claimed.* The websafe
guard reduces/flags injection only (never to zero); the browser-side markdown-image exfil
channel is a documented known residual, out of scope for Phase 33's egress-bounding work.
T-33-04's mitigation is explicitly flag-not-block and asserts no elimination.

---

## Unregistered Flags

None. No Phase-33 SUMMARY contains a `## Threat Flags` section, and no new attack surface
was introduced beyond the registered threats (the verify-search verb is read-only on
config, opens no new host port, adds no dependency, and the only host-mutating action —
the transient nft bound — is the explicitly-modeled T-33-03/T-33-06 surface with verified
fixed-arg exec and guaranteed teardown).

---

## Verification Commands (re-runnable)

```
# no shell interpolation (T-33-03)            -> 0
grep -vE '^[[:space:]]*//' cmd/villa/verify_search.go | grep -cE 'sh -c|bash -c'
# curl -f omitted (T-33-07/WR-02)             -> 0
grep -c '"-f"' cmd/villa/verify_search.go
# no leaked image literal (T-33-03)
go test ./internal/inference/ -run TestSeamGrepGate -count=1
# family + verdict + regression suite
go test ./cmd/villa/ -run 'TestEvalSearchVerify|TestClassifySearchProbe|TestSearch|TestVerifySearch|TestRunVerifySearch|TestNftBound|TestResolveCurlPin' -count=1
go test ./internal/orchestrate/ -run 'TestOWUIKillEnv|TestOWUIWebOff|TestRenderOpenWebUI' -count=1
go test ./internal/websafe/ -run 'TestSSRF|TestHostRejected|TestControl' -count=1
```

All commands above were executed during this audit and pass.

---

_Audited read-only against implemented code. Implementation files were not modified._
_Auditor: gsd-secure-phase (FORCE stance)._
