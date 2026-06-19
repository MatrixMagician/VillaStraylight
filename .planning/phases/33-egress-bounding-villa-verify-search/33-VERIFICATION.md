---
phase: 33-egress-bounding-villa-verify-search
verified: 2026-06-19T22:47:25Z
status: human_needed
score: 8/10 must-haves verified
behavior_unverified: 2
overrides_applied: 0
behavior_unverified_items:
  - truth: "`villa verify search` proves bounded outbound under a REAL rootless-netns nft block — off-allowlist canary reachable unguarded, unreachable under the bound; an ineffective block REJECTs (SC2 / PRIV-08)"
    test: "On the live Strix Halo host with web_search_enabled=true: run `./villa verify search` and confirm exit 0 (PASS), then (via `--json`) confirm the off-allowlist canary was reachable UNGUARDED and UNREACHABLE under the bound while the allowlisted upstream stayed reachable. Make the bound ineffective (e.g. add the canary IP to the allowlist) and confirm REJECT/FAIL (exit nonzero) — never a green PASS."
    expected: "PASS (exit 0) with a genuinely reachable-unguarded → blocked-under-bound canary and a still-reachable allowlist; an ineffective/unroutable bound returns REJECT (exit 2) or FAIL (exit 1), never a fabricated PASS."
    why_human: "The live bound seam (applySearchBound) is a documented non-functional placeholder: nft is applied in a throwaway `unshare -rn` netns that the `podman run --network villa` probes never enter (CR-01, BLOCKER). Off-hardware the proof is structurally incapable of returning PASS. The real attach point (arch A vs B) and the live PASS are the explicit deliverable of the autonomous:false Plan 33-03 on-hardware human checkpoint, which has not run. Presence + wiring of the pure core is verified; the live bounded-outbound PROPERTY can only be exercised on real hardware."
  - truth: "Family (d) secret-in-query-string exfil is blocked under the REAL bound on hardware; OWUI makes NO outbound HuggingFace pull under a real web search (PRIV-08 family-d + PRIV-09 effectiveness)"
    test: "On hardware: confirm (via `villa verify search --json`) the secret-bearing canary request was BLOCKED under the bound (the secret did not reach the canary). Run a REAL web search through Open WebUI that triggers grounding while watching outbound — confirm grounding works AND no outbound HuggingFace pull occurs (HF_HUB_OFFLINE=1 + telemetry kill env is effective). Confirm no `villabound` nft table lingers after the verb exits."
    expected: "The secret-bearing request is dropped under the bound; a real web search grounds successfully with zero outbound HF pull; the bound is verify-time-only and torn down cleanly."
    why_human: "Family (d) is wired into the same live bound as the canary probe, so it shares CR-01's non-functional placeholder and cannot be exercised off-hardware (the family-(d) UNIT test passes via an injected fake curl exit, but the live nft block is not real off-host). PRIV-09's env presence is verified by regression (kill-env in both goldens, byte-identical web-off render); the EFFECTIVENESS of the kill under a live search is an on-hardware observation. Both are the Plan 33-03 checkpoint deliverable."
human_verification:
  - test: "Live `villa verify search` PASS with a real nft bound (negative-control-first, inverse-framed): canary reachable unguarded → unreachable under the bound, allowlist stays reachable; ineffective bound REJECTs/FAILs (PRIV-08 / SC2)."
    expected: "PASS exit 0 on the healthy web-search-enabled stack; REJECT (exit 2) / FAIL (exit 1) — never a fabricated PASS — on an ineffective/unroutable bound."
    why_human: "Live bound seam is a documented non-functional placeholder (CR-01); the real rootless-netns attach point and live PASS are the Plan 33-03 on-hardware blocking checkpoint (not yet run). Cannot be exercised off-hardware."
  - test: "Family (d) secret-in-query exfil blocked under the real bound; PRIV-09 no-outbound-HF-pull under a real web search; bound tears down with no `villabound` residue."
    expected: "Secret-bearing request dropped under the bound; real grounding works with zero HF pull; clean teardown."
    why_human: "Shares CR-01's placeholder bound; PRIV-09 effectiveness is a runtime observation. Plan 33-03 checkpoint deliverable."
---

# Phase 33: Egress-Bounding + `villa verify search` Verification Report

**Phase Goal:** The operator can prove — negative-control-first, inverse-framed, under a real rootless-netns nft block — that web search's outbound is bounded; web search is opt-in/default-off and OWUI's lazy/background outbound is killed, so the only sanctioned runtime outbound is SearXNG upstreams + result-page fetches.
**Verified:** 2026-06-19T22:47:25Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

The goal decomposes into three requirement strands: **PRIV-07** (opt-in/default-off, byte-identical-when-off), **PRIV-08** (the negative-control-first inverse-framed bounded-outbound proof), and **PRIV-09** (OWUI lazy/background outbound killed). PRIV-07 and PRIV-09 are satisfied off-hardware by assert-only regression (config + env already shipped). PRIV-08's pure honesty core, curl-exit classifier, in-process families (b)/(c)/(d), cobra wiring, exit-map, and `--json` contract are all DONE and non-vacuous — but PRIV-08's *live bounded-outbound PROOF under a real nft block* is a documented non-functional placeholder off-hardware (CR-01) and is the explicit deliverable of the autonomous:false Plan 33-03 on-hardware human checkpoint, which has not run (no 33-03-SUMMARY.md; ROADMAP Wave 3 unchecked).

### Observable Truths

| #   | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1   | SC1/PRIV-07: web search opt-in/default-OFF; OWUI web-off render byte-identical to v1.4 | ✓ VERIFIED | `config.WebSearchEnabled` defaults false (villaconfig.go:138,214); `TestOWUIWebOffByteIdenticalPRIV07` compares web-off render to the existing `villa-openwebui.container.golden`; git-diff guard shows openwebui.go/villaconfig.go/golden untouched. Test green. |
| 2   | PRIV-08 pure core maps probe outcomes to PASS/FAIL/REJECT, negative-control asserted FIRST | ✓ VERIFIED | `evalSearchVerify` (verify_search.go:105-162) — positive control → REJECT, negative control → REJECT, under-bound canary-still-reachable → FAIL, blanket → REJECT, families → FAIL. `TestEvalSearchVerify*` green. |
| 3   | Canary still reachable under the bound is a FAIL, never a fabricated PASS (inversion trap) | ✓ VERIFIED | verify_search.go:141-143 returns `fail(...)`; pinned by `TestEvalSearchVerifyInverse` / "STILL reachable under the bound" case. |
| 4   | Already-unreachable canary / unreachable allowlist is a REJECT, distinct from FAIL | ✓ VERIFIED | verify_search.go:119-121,130-132,144-146 — three distinct `reject(...)` paths; pinned by REJECT-case tests. |
| 5   | Curl-exit classifier maps 6/7/28→blocked, 0→reachable, else→REJECT-bound error (never blocked=true) | ✓ VERIFIED | `classifySearchProbe` (verify_search.go:178-196); `TestClassifySearchProbe` green. |
| 6   | Planted-injection page comes back stripped + fenced + flagged via shipped websafe guard (in-process) | ✓ VERIFIED | `injectionFlagged` (verify_search.go:210-224) drives `websafe.NewLoader`; live clause uses planted page + `plantedPageRoundTripper` (CR-02 fix); `TestSearchInjectionFlagged` + `TestSearchLivePlantedInjectionFlagged` green; benign-not-flagged counter-test present. |
| 7   | SSRF internal-host cases blocked via shipped ssrf.go guard | ✓ VERIFIED | `ssrfBlocked` (verify_search.go:259-266) drives `websafe.SafeClient`; ssrf_test.go covers 169.254.169.254 / 127.0.0.1 / villa-* / localhost. websafe SSRF tests green. |
| 8   | Family (d) secret-in-query exfil is a REAL composed probe (not vacuous): defined, unit-tested, wired into evalSearchVerify | ✓ VERIFIED | `secretQueryBlocked` (verify_search.go:316-319) + `secretExfilURL` (289-295); wired as 6th probe at verify_search.go:521; `TestSearchSecretQuery` (×2 funcs) green incl. the FAIL-not-PASS verdict case. |
| 9   | PRIV-08 LIVE: bounded outbound proven under a REAL rootless-netns nft block (canary reachable unguarded → blocked under bound; ineffective → REJECT) | ⚠️ PRESENT_BEHAVIOR_UNVERIFIED | Pure core + seam present & wired, but `applySearchBound` (verify_search.go:530-550) applies nft in a throwaway `unshare -rn` netns the `podman --network villa` probes never enter (CR-01 BLOCKER, 33-REVIEW.md:49-60). Off-hardware structurally cannot PASS. Real attach point (arch A/B) + live PASS = Plan 33-03 on-hardware checkpoint (autonomous:false, not run). See Human Verification. |
| 10  | PRIV-09 EFFECTIVE: a real web search under the bound makes NO outbound HuggingFace pull | ⚠️ PRESENT_BEHAVIOR_UNVERIFIED | Kill-env presence verified by regression (truth #11 below), but EFFECTIVENESS under a live search is a runtime observation tied to the live bound (Plan 33-03 checkpoint, not run). See Human Verification. |
| 11  | PRIV-09 env presence: OWUI outbound-kill env keys present in BOTH web-off and web-on goldens, not re-added | ✓ VERIFIED | `TestOWUIKillEnvPresentBothViewsPRIV09` (openwebui_test.go:129-160) asserts all six keys in both renders; git-diff guard confirms openwebui.go untouched. Test green. |

**Score:** 9/11 truths verified (2 present, behavior-unverified). Mapped to the 7 PLAN-frontmatter + 4 ROADMAP-SC must-haves, the headline is **8/10 must-haves** (the 4 ROADMAP SCs collapse onto truths 1, 2-5, 6-8, 9-11): SC1 ✓, SC2 ⚠️ (pure-DONE/live-pending), SC3 ✓, SC4 ⚠️ (presence ✓/effectiveness-pending).

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `cmd/villa/verify_search.go` | searchProof 3-state, evalSearchVerify, classifySearchProbe, families, live seam, secretQueryBlocked, runVerifySearch | ✓ VERIFIED (with live-seam placeholder) | All symbols present; pure half complete & wired; live `applySearchBound` is a documented non-functional placeholder (CR-01) pending 33-03. |
| `cmd/villa/verify_search_test.go` | truth-table, classifier, family (b)/(d) tests, registration/gate/exit-map, secret-query | ✓ VERIFIED | `TestEvalSearchVerify`, `TestClassifySearchProbe`, `TestSearchSecretQuery`(×2), `TestSearchLivePlantedInjectionFlagged`, etc. all green. |
| `cmd/villa/verify_search_json.go` | renderVerifySearchJSON schema-v1 | ✓ VERIFIED | renderVerifySearchJSON at :45; golden `verify-search.json.golden` (schema 1) present + byte-frozen. |
| `cmd/villa/verify.go` | newVerify() AddCommand(newVerifySearch()) | ✓ VERIFIED | verify.go:77. |
| `cmd/villa/testdata/verify-search.json.golden` | byte-frozen schema-v1 contract | ✓ VERIFIED | Contains `"schema": 1`; `TestVerifySearchJSON` green. |
| `internal/orchestrate/openwebui_test.go` | PRIV-09 kill-env both goldens, PRIV-07 web-off byte-identical | ✓ VERIFIED | `TestOWUIKillEnvPresentBothViewsPRIV09` + `TestOWUIWebOffByteIdenticalPRIV07` green; assert-only (git-diff guard clean). |
| `internal/websafe/ssrf_test.go` | explicit internal-host SSRF case | ✓ VERIFIED | 169.254.169.254 present; websafe SSRF suite green. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| verify.go | verify_search.go | newVerify() AddCommand(newVerifySearch()) | ✓ WIRED | verify.go:77 |
| verify_search.go | internal/config | liveLoadedWebSearchEnabled → WebSearchEnabled gate | ✓ WIRED | install_searxng.go:42 + villaconfig.go:138 |
| verify_search.go | internal/orchestrate | orchestrate.EmbedImage() (seam-locked image) | ✓ WIRED | verify_search.go:421; SeamGrepGate green |
| verify_search.go | internal/websafe | NewLoader + SafeClient (families b/c) | ✓ WIRED | verify_search.go:211,260 |
| liveSearchVerify | secretQueryBlocked + evalSearchVerify | family-(d) composed as 6th probe | ✓ WIRED | verify_search.go:487,521 |
| liveSearchVerify | host nft/unshare bound | applySearchBound where container egress flows | ⚠️ NOT_WIRED (live) | applySearchBound applies nft in an anonymous `unshare -rn` netns the podman probes never enter (CR-01) — attach point pending 33-03 on-hardware. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Phase-33 cmd/villa suite | `go test ./cmd/villa/ -run '<phase-33 set>' -count=1` | ok 4.0s | ✓ PASS |
| Seam grep gate (no leaked image literal) | `go test ./internal/inference/ -run TestSeamGrepGate` | 1 passed | ✓ PASS |
| OWUI render + PRIV-07/09 regression | `go test ./internal/orchestrate/ -run 'TestRenderOpenWebUI|TestOWUI'` | 21 passed | ✓ PASS |
| websafe SSRF families | `go test ./internal/websafe/ -run 'TestSSRF|TestHostRejected|TestControl'` | 4 passed | ✓ PASS |
| No shell interpolation in seam | `grep -vE '^//' verify_search.go \| grep -cE 'sh -c\|bash -c'` | 0 | ✓ PASS |
| WR-02: curl -f omitted on probes | `grep -c '"-f"' verify_search.go` | 0 | ✓ PASS |
| Assert-only guard (env/config/golden untouched) | `git diff --stat openwebui.go villaconfig.go ...golden` | no changes | ✓ PASS |
| Live `villa verify search` PASS on hardware | (requires live Strix Halo + nft/unshare) | — | ? SKIP (Plan 33-03 on-hardware checkpoint) |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| PRIV-07 | 33-02 | Web search opt-in/default-OFF; install byte-identical to v1.4 when off | ✓ SATISFIED | Config default false + byte-identical web-off render regression (truth 1, 11). |
| PRIV-08 | 33-01/02/03 | `villa verify search` proves bounded outbound negative-control-first under a real nft block; planted-injection, SSRF, secret-query cases | ? PARTIAL (NEEDS HUMAN) | Pure core + families (b)/(c)/(d) + classifier + cobra DONE & non-vacuous off-hardware (truths 2-8). LIVE bounded-outbound proof under a real nft block is a non-functional placeholder (CR-01) pending the Plan 33-03 on-hardware human checkpoint (truth 9). |
| PRIV-09 | 33-02/03 | OWUI lazy/background outbound killed (HF_HUB_OFFLINE + telemetry); weights pre-staged | ✓ SATISFIED (presence) / ? NEEDS HUMAN (effectiveness) | Kill-env presence in both goldens + byte-identical web-off render verified (truth 11). Live effectiveness (no HF pull under a real search) is the 33-03 checkpoint (truth 10). |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| cmd/villa/verify_search.go | 530-550 | Documented non-functional live-bound placeholder (applySearchBound applies nft in a netns the probes never enter) | ℹ️ Info (by design) | This is the documented Plan 33-03 deliverable (CR-01), not unmarked debt — comments at 528-543 explicitly defer the attach point to on-hardware Plan 03. No TBD/FIXME/XXX debt markers found in phase-33 files. |

No unreferenced TBD/FIXME/XXX debt markers in the phase-33 files. The placeholder is explicitly attributed to the on-hardware checkpoint in code comments and 33-REVIEW-FIX.md (CR-01 DEFERRED, not skipped).

### Human Verification Required

The pure honesty core, the off-hardware harness, and the PRIV-07/PRIV-09 regression guards are verified and green. Two truths assert a RUNTIME bounded-outbound property that can only be exercised on the live Strix Halo host under a real rootless-netns nft block — they are the explicit deliverable of the **autonomous:false Plan 33-03 blocking human checkpoint** (not yet run):

1. **Live `villa verify search` PASS with a real nft bound** — On the live host with `web_search_enabled=true`: `./villa verify search` must exit 0 (PASS) with the off-allowlist canary reachable UNGUARDED and UNREACHABLE under the bound, and the allowlisted upstream still reachable. An ineffective/unroutable bound must REJECT (exit 2) / FAIL (exit 1) — never a fabricated PASS. (Resolves CR-01 / RESEARCH Open Q2 arch A vs B.)

2. **Family-(d) secret-query blocked under the real bound + PRIV-09 no-HF-pull + clean teardown** — Confirm (via `--json`) the secret-bearing canary request was blocked under the bound; run a real grounded web search and confirm grounding works with zero outbound HuggingFace pull; confirm no `villabound` nft table lingers afterward. (Resolves RESEARCH Open Q1 weight pre-staging.)

### Gaps Summary

No off-hardware code gaps. Every must-have that can be verified without the live host IS verified: the 3-state honesty core (inversion trap + both REJECT classes pinned), the curl-exit classifier, all three in-process/composed families (b)/(c)/(d) — non-vacuous after the CR-02 fix — the cobra registration/gate/exit-map, the schema-v1 `--json` golden, and the PRIV-07 (byte-identical-when-off) + PRIV-09 (kill-env present, assert-only) regressions. The off-hardware suite is green including `-race`.

The phase is NOT a PASS only because two truths assert a live bounded-outbound property that is, by design, deferred to the autonomous:false Plan 33-03 on-hardware human checkpoint. The current live bound seam is a knowingly non-functional placeholder (CR-01) that must NOT be read as a working PASS off-hardware. This is correctly classified `human_needed` — neither a false pass (the live property is unproven) nor a closeable off-hardware code gap (the real attach point requires the host). Plan 33-03 owns finalizing the rootless-netns attach point (arch A/B), the real PASS, and the no-HF-pull UAT.

---

_Verified: 2026-06-19T22:47:25Z_
_Verifier: Claude (gsd-verifier)_
