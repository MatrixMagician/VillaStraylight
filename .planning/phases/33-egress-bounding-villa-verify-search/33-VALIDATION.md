---
phase: 33
slug: egress-bounding-villa-verify-search
status: finalized
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-19
finalized: 2026-06-20
---

# Phase 33 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard library `testing`) |
| **Config file** | none — Go modules; `Makefile` targets |
| **Quick run command** | `go test ./cmd/villa/... -run VerifySearch` |
| **Full suite command** | `make check` (vet + `go test ./...`, incl. `-race`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./cmd/villa/... -run VerifySearch`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

> Finalized against the actually-implemented tests (audited 2026-06-20). All off-hardware
> coverage is green and non-vacuous; the live netns/nft attach mechanics legitimately remain
> the on-hardware human checkpoint (see Manual-Only Verifications).

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test(s) | Automated Command | Status |
|---------|------|------|-------------|------------|-----------------|---------|-------------------|--------|
| 33-verdict | 01 | 1 | PRIV-08 | T-33-01/02 | pure 3-state inverse-framing core: canary-reachable-unguarded→blocked-under-bound; canary-STILL-reachable-under-bound ⇒ **FAIL** (inversion trap, never fabricated PASS); empty-netns trap (canary already down) ⇒ REJECT; blanket block (allowlist also dropped) ⇒ REJECT — both REJECT classes distinct from FAIL | `TestEvalSearchVerify` (12-row truth table), `TestEvalSearchVerifyInverse` (isolated trap: non-PASS + "STILL reachable" detail) | `go test ./cmd/villa/ -run 'TestEvalSearchVerify\|TestEvalSearchVerifyInverse' -count=1` | ✅ green |
| 33-classifier | 01 | 1 | PRIV-08 | T-33-02 | curl-exit classifier: 6/7/28⇒blocked, exit 0⇒reachable, any other/never-started⇒error (caller REJECTs), NEVER blocked=true on a could-not-run path | `TestClassifySearchProbe` (7-row, never-false-block invariant) | `go test ./cmd/villa/ -run TestClassifySearchProbe -count=1` | ✅ green |
| 33-family-b | 01 | 1 | PRIV-08 | T-33-04 | planted-injection page → stripped+fenced+flagged via shipped websafe guard (in-process, no network); live wiring proven non-vacuous (CR-02) + benign page NOT flagged | `TestSearchInjectionFlagged`, `TestSearchLivePlantedInjectionFlagged`, `TestSearchLiveInjectionFlagsBenignFalse` | `go test ./cmd/villa/ -run 'TestSearch.*Injection' -count=1` | ✅ green |
| 33-family-c | 01 | 1 | PRIV-08 | T-33-05 | SSRF internal-host (169.254.169.254/127.0.0.1/villa-*/localhost) refused via shipped ssrf.go guard | `TestSearchSSRF` (cmd), `TestSSRFInternalHostCase` (websafe) | `go test ./cmd/villa/ -run TestSearchSSRF -count=1 && go test ./internal/websafe/ -run TestSSRFInternalHostCase -count=1` | ✅ green |
| 33-family-d | 02 | 2 | PRIV-08 | T-33-10 | secret-in-query exfil: a REAL probe (not vacuous) — secret token in canary query string; the **(false,nil)⇒FAIL** verdict case is pinned end-to-end (secret reached canary ⇒ FAIL, never PASS); contained=6/7/28, escaped=exit-0, could-not-run⇒FAIL | `TestSearchSecretQuery` (6-row incl. exit-0⇒(false,nil)), `TestSearchSecretQueryDrivesFailNotPass` (verdict-level (false,nil)⇒FAIL), `TestSecretExfilURLCarriesTokenInQuery` | `go test ./cmd/villa/ -run 'TestSearchSecretQuery\|TestSecretExfil' -count=1` | ✅ green |
| 33-cobra | 02 | 2 | PRIV-08 | T-33-03 | registered under `verify`; web-search-OFF exits 0 ("nothing to verify", not silent skip); PASS→0/FAIL→1/REJECT→2 exit map (no 4th code) | `TestVerifySearchRegistered`, `TestRunVerifySearchGate`, `TestRunVerifySearchExit` | `go test ./cmd/villa/ -run 'TestVerifySearchRegistered\|TestRunVerifySearch' -count=1` | ✅ green |
| 33-bound-render | 02/03 | 2/3 | PRIV-08 | T-33-03 | nft ruleset (arch A, fixed-arg, no shell): forward-hook iifname-scoped drop, BOTH-family (v4+v6) accepts, catch-all drop AFTER accepts (order load-bearing); bridge-ifname validator rejects nft/shell syntax; curl --resolve pin (WR-04 TOCTOU) | `TestNftBoundRuleset`, `TestNftBridgeIfPattern`, `TestResolveCurlPin` | `go test ./cmd/villa/ -run 'TestNft\|TestResolveCurlPin' -count=1` | ✅ green |
| 33-json | 02 | 2 | PRIV-08 | — | byte-frozen `--json` schema-v1 contract; run path honors --json (schema+verdict to stdout, no stderr) | `TestVerifySearchJSON` (golden), `TestVerifySearchJSONRunPath` | `go test ./cmd/villa/ -run TestVerifySearchJSON -count=1` | ✅ green |
| 33-seam-gate | 02/03 | 2/3 | PRIV-08 | T-33-03 | no leaked backend/image literal in verify_search.go (helper image via orchestrate.EmbedImage only) | `TestSeamGrepGate` | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ green |
| 33-priv-regress | 02 | 2 | PRIV-07, PRIV-09 | T-33-08/09 | regression assertions (assert-only, never re-add): web-search-OFF OWUI render byte-identical to v1.4; six outbound-kill env keys (HF_HUB_OFFLINE + telemetry kills) present in BOTH web-off and web-on goldens | `TestOWUIWebOffByteIdenticalPRIV07`, `TestOWUIKillEnvPresentBothViewsPRIV09` | `go test ./internal/orchestrate/ -run 'TestOWUIWebOffByteIdenticalPRIV07\|TestOWUIKillEnvPresentBothViewsPRIV09' -count=1` | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red.*
*Critical: per RESEARCH, the OWUI env block and VillaConfig schema were NOT modified — PRIV-07/PRIV-09 are already shipped; this phase PROVES them by regression assertion (`git diff --exit-code` on openwebui.go/villaconfig.go/the OWUI goldens is clean).*

### Audit notes (2026-06-20)

- **Non-vacuity confirmed.** The two load-bearing false-green hazards are pinned at the verdict level, not merely at the probe layer: the inversion trap (`TestEvalSearchVerifyInverse` asserts non-PASS **and** the `"STILL reachable under the bound"` detail) and the family-(d) `(false,nil)⇒FAIL` case (`TestSearchSecretQueryDrivesFailNotPass` asserts `searchFail` and explicit `!= searchPass` with every other clause held good). CR-02's live family-(b) non-vacuity is pinned by `TestSearchLivePlantedInjectionFlagged` (live clause CAN flag) + `TestSearchLiveInjectionFlagsBenignFalse` (a benign page is NOT flagged, so the clause is discriminating, not always-true).
- **No off-hardware gaps found; no tests added.** Off-hardware coverage is genuinely complete. The implementation evolved past the draft map (architecture A finalized on-hardware in Plan 03 with `nftBoundRuleset`/`liveBridgeInterface`/`resolveCurlPin`/`nftBridgeIfPattern`, plus the WR-02/03/04 fixes); the renderable/pure portions of that bound are unit-covered (`TestNftBoundRuleset`/`TestNftBridgeIfPattern`/`TestResolveCurlPin`). The live netns attach + teardown remain the on-hardware human checkpoint by nature (Pitfall 1: an empty `unshare -rn` netns blocks trivially — the negative control can only be disproven where egress actually flows).
- **Full suite:** `go test ./cmd/villa/... ./internal/orchestrate/... ./internal/websafe/...` — 746 passed, 0 failed.

---

## Wave 0 Requirements

- [x] `cmd/villa/verify_search.go` + `verify_search_test.go` — 3-state verdict core + inverse-framing unit tests
- [x] `cmd/villa/testdata/verify-search.json.golden` — `verify search --json` golden fixture (schema v1)
- [x] No framework install needed — `go test` built in

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| The live rootless-netns + nft egress bound actually blocks an off-allowlist canary while leaving SearXNG/websafe reachable, with guaranteed teardown | PRIV-08 | requires the live Strix Halo host (`unshare -rn` / `nft` / podman netns); the negative control must run where egress genuinely flows (loopback-only netns trivially "blocks" for the wrong reason) | On-hardware bound-mechanics task: run `villa verify search`; confirm PASS with a routable allowlist + canary-reachable-unguarded control; confirm an unroutable/ineffective bound yields REJECT (exit 2), not PASS; confirm netns is torn down |
| Web search under the bound makes no HuggingFace pull (weights pre-staged: `nomic-embed-text-v1.5`) | PRIV-09 | requires live OWUI + network observation | Enable web search, run a query, confirm no outbound HF pull beyond SearXNG upstreams + result fetches |

*The pure verdict core + families (b)/(c)/(d) + the nft ruleset renderer + the PRIV-07/09 regression guards all have automated unit coverage; the live netns bound mechanics (architecture A, finalized on-hardware in Plan 03 Task 1) are exercised end-to-end by the on-hardware human checkpoint (Plan 03 Task 2). This split is expected, not a coverage gap — the empty-netns false-block (Pitfall 1) can only be disproven where container egress genuinely flows.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (live netns mechanics flagged manual/on-hardware)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter
- [x] Off-hardware coverage complete and non-vacuous (audited 2026-06-20); live netns mechanics legitimately remain the on-hardware human checkpoint

**Approval:** approved (Nyquist audit 2026-06-20) — off-hardware coverage complete; PRIV-08 live netns proof + PRIV-09 no-HF-pull UAT carried to Plan 03 Task 2 (blocking-human checkpoint) by design.
