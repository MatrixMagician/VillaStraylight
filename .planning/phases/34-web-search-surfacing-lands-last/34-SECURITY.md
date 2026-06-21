---
phase: 34-web-search-surfacing-lands-last
audit_date: 2026-06-21
asvs_level: L1
block_policy: block on high
status: SECURED
threats_closed: 19
threats_total: 19
threats_open: 0
---

# SECURITY.md — Phase 34: Web-Search Surfacing (lands last)

**Phase:** 34 — web-search-surfacing-lands-last
**Audit date:** 2026-06-21
**ASVS Level:** L1
**Block policy:** block on high
**Threats closed:** 19 / 19 (all mitigate)
**Threats open:** 0
**Diff audited:** `840756b..HEAD` (branch `gsd/phase-34-web-search-surfacing-lands-last`)
**Adversarial stance:** FORCE — every mitigation assumed absent until a grep match / code path proved it present at the right location.

This audit verifies declared threat mitigations against the *implemented* code (read-only).
It does not scan for new vulnerabilities. Each threat resolves to CLOSED (mitigation
present at the right location) or OPEN. Evidence cites `file:line`. Implementation files
were not modified.

---

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|-----------|----------|-------------|--------|----------|
| T-34-01 | Tampering (torn/forged verify-state file → fabricated "bounded") | mitigate | CLOSED | Fail-closed `Load` (`internal/verifystate/store.go:97-118`): absent (`ReadAll`⇒nil,nil) ⇒ empty State (`:105-107`); corrupt/unparseable ⇒ empty State, no panic (`:109-111`); `schema_version != verifyStateSchemaVersion` ⇒ empty State, future schema never reinterpreted (`:112-116`). Atomic temp+rename write so no partial file is observed (`WriteFileAtomic:184-208`). `Save` stamps `s.SchemaVersion` itself, never trusts the caller (`:83`). `TestStore` green. |
| T-34-02 | Information Disclosure (state file on disk) | mitigate | CLOSED | `storeFileMode = 0o600` / `storeDirMode = 0o700` enforced by `WriteFileAtomic` (`store.go:47-50,181,191,211`); file holds only `{schema_version,verdict,checked_at}` — no query/URL/fetched content (`State` struct `:57-61`, package doc `:11-14`). |
| T-34-03 | Spoofing (persisted verdict feeding the indicator) | mitigate | CLOSED | Verdict written verbatim from the real proof: `verifystate.State{Verdict: verdictName(proof.status), CheckedAt: time.Now().UTC().Format(RFC3339)}` (`cmd/villa/verify_search.go:744-747`); the PASS→"bounded" freshness check lives in the status core (Plan 03), so a stale PASS cannot read as current. Persist is best-effort — `persistFn` error never changes the exit code (`:743-751`). |
| T-34-04 | Elevation / path traversal (WriteFileAtomic target) | mitigate | CLOSED | `assertInsideDir(path, storeRootDir())` guards every write against the FIXED store root (`store.go:147-164,178`); rejects `..`/absolute escapes; fixed-arg path, no shell interpolation. Live write resolves `VerifyStatePath()` so legitimate writes never reject (`verify_search.go:368-372`). |
| T-34-05 | Information Disclosure (settings.yml backup entry → SEARXNG_SECRET) | mitigate | CLOSED | Restore forces 0600 via `writeSearxngSettings`→`WriteSearxngSettings` at `searxngSettingsFileMode = 0o600`, dir 0700, never widened (`internal/orchestrate/searxng_settings_write.go:30-34,60-64,130`; `internal/backup/restore.go:466-477`). On-disk mode test asserts exactly 0600. |
| T-34-06 | Information Disclosure (privacy — persisting ephemeral web content) | mitigate | CLOSED | Only the settings.yml CONFIG entry crosses; "NO entry is ever added for fetched/ephemeral web content" (`internal/backup/backup.go:242`). `TestBackupExcludesEphemeral` (`backup_test.go:632`) asserts no archive entry references a query/URL/fetch log via a banned-substring scan (`:648-656`). |
| T-34-07 | Tampering (restored settings.yml integrity) | mitigate | CLOSED | settings.yml verified through the SAME `readAndVerify` SHA-256 pass as every member — "a per-entry SHA-256 mismatch wraps ErrChecksumMismatch" (`restore.go:585-589`); the entry is only collected after verification (`collect[EntrySearxngSettings]` `:723-724`); prior-file capture enables verbatim rollback (`captureFile` `:262`). |
| T-34-08 | Spoofing (outbound_bounded indicator) | mitigate | CLOSED | `webSearchInfo` derives the tri-state from the cached verify State with a freshness gate (`internal/status/status.go:883-914`): default `OutboundUnknown` (`:886`); nil seam/absent store ⇒ unknown (`:888-894`); unparseable/stale/**future** timestamp ⇒ unknown via `age < 0 || age > verifyFreshnessWindow` clamp (WR-01 fix, commit 1397696, `:899-905`); "bounded" only for `Verdict=="PASS"` AND fresh (`:908-909`). NEVER from `cfg.WebSearchEnabled`. `TestWebSearchOutboundBounded` asserts the no-false-green case. |
| T-34-09 | Tampering / false-green health (villa-searxng / villa-websafe rows) | mitigate | CLOSED | Dedicated in-network seams `SearxngHealth`/`WebsafeHealth` consulted in explicit per-service branches (`status.go:676-700`), which `continue` before the generic `d.Health(endpoint)` fall-through (`:708`); nil seam → `HealthUnknown` (the qdrant/embed precedent, never the Phase-22 chat false-green). |
| T-34-10 | Information Disclosure (privacy — source-gap fields) | mitigate | CLOSED | `WebSearchInfo` ships only `{enabled, outbound_bounded, verify_checked_at}` (`status.go:285-289`); guard counters / last_query / fetched-URLs OMITTED with a documented scope-limit comment (`:280-284`) — surfacing them would require a query/URL log conflicting with "ephemeral content excluded by design". |
| T-34-11 | Tampering (byte-frozen status contract) | mitigate | CLOSED | Single append-only 4→5 bump; `WebSearch *WebSearchInfo` tail-appended ABOVE `SchemaVersion` which stays last (`status.go:162-176`); `reportSchemaVersion = 5` (`:200`, only non-comment match). ONE golden re-freeze of the three `status*.json.golden` (each +1 line: schema bump / web_search block). `TestStatus*` green against re-frozen goldens. |
| T-34-12 | Spoofing (doctor egress-proof finding) | mitigate | CLOSED | `searchEgressFinding` is tri-state from the cached verify result (`internal/doctor/doctor.go:641`); the live `liveSearchEgressProof` over `verifystate.Load` applies the SAME freshness clamp incl. the future-timestamp lower bound `time.Since(checked) < 0 || > searchVerifyFreshnessWindow` (`cmd/villa/doctor.go:630-634`, WR-01 fix); stale/absent ⇒ typed-Unknown WARN with remediation, never config-bool-derived. |
| T-34-13 | Repudiation / false-green (residency-under-search-load) | mitigate | CLOSED | `runSearchResidencyUnderLoad` is offload-asserting (`cmd/villa/doctor.go:687`): precondition gate (chat + villa-searxng + villa-websafe active via accessors `:692-693`) → `agentUnevaluable` typed-Unknown WARN, never starts a service; samples `inference.RunningOffloadVerdict` ONLY while a round is verifiably in flight (`:491-506`); confident CPU fallback → FAIL dominating HTTP-200; no round in flight → typed-Unknown. `TestSearchResidencyFinding` proves the FAIL-not-masked case. |
| T-34-14 | Information Disclosure / Elevation (leaked backend marker / image literal) | mitigate | CLOSED | `inference.Verdict` consumed opaquely; images via `orchestrate.*Image()`, unit names via `orchestrate.*ContainerUnitName()`. `grep ROCm0\|Vulkan0\|HSA_OVERRIDE` in `cmd/villa/doctor.go` = 0; the 3 matches in `cmd/villa/status.go:57` / `internal/doctor/doctor.go:19,72` are explanatory COMMENTS, not re-typed literals. Authoritative `TestSeamGrepGate` (walks `internal/` + `cmd/villa`) PASSES. |
| T-34-15 | Tampering (doctor's own byte-frozen contract) | mitigate | CLOSED | Independent append-only `reportSchemaVersion = 3` (`internal/doctor/doctor.go`, doctor's OWN const, not conflated with status's 5); single `doctor.json.golden` re-freeze (+1 line, schema bump only); nil seams keep web-off byte-identical except the bump (`TestAggregateWebSearch`). |
| T-34-16 | Tampering / Elevation (reflected XSS — row-7 provenance + all panel values) | mitigate | CLOSED | `renderWebSearch` (`internal/dashboard/assets/dashboard.js:472-509`) builds every value via `memoryBadgeRow`/`metricRow`/`mutedP` — all `createElement + textContent` (`:278-284`). ZERO `innerHTML` lines added this phase (`git diff … | grep '^+' innerHTML` = 0); the 6 pre-existing `innerHTML` matches are a static no-server-data literal (`:154`) + comments. |
| T-34-17 | Spoofing (dashboard outbound-bounded indicator) | mitigate | CLOSED | Tri-state mirrors the status core: `bounded`→green badge-ready, `not-bounded`→amber badge-warn, else→gray "unavailable" + "run villa verify search" caption (`dashboard.js:481-505`); never green/red by default — the green claim rides the freshness-gated status field (T-34-08). Human-verify checkpoint step 4 (Plan 05 Task 2) proves the no-false-green path. |
| T-34-18 | Information Disclosure (privacy — fabricated last-query / fetched-URL rows) | mitigate | CLOSED | Omit-when-absent by design: source-gap rows render ONLY if the status core ships them; it does not (T-34-10), so the renderer never fabricates a 0/"never"/placeholder (`dashboard.js:506-508`); `verify_checked_at` rendered only when present (`:486-488,497-499`). |
| T-34-19 | Tampering (off-render pixel-identity) | mitigate | CLOSED | Panel ships `hidden` in the static shell (`dashboard.html`, `#web-search-panel … hidden`); `renderWebSearch` re-hides when `report.web_search` is absent (`dashboard.js:474-478`); called from poll()'s `.then` after `renderAgent`, NOT the `.catch` (`:915-919`); no new fetch/endpoint/probe (reads the existing `/api/status`). Web-off dashboard pixel-identical to v1.4. |

---

## Disposition Detail — the central trap (T-34-01 / T-34-08 / T-34-12)

The load-bearing false-green hazard this phase forbids is a fabricated "bounded"
outbound claim from a torn/forged/stale/config-derived signal. Verified mitigation chain
in implemented code:

1. **Fail-closed Load (T-34-01).** `verifystate.Load` (`store.go:97-118`) returns an
   EMPTY State — never a fabricated PASS — for an absent, corrupt, OR future-schema file.
   `Save` stamps the schema_version itself (`:83`); the writer is atomic temp+rename so no
   partial file is ever read.
2. **Indicator derives from the cache, never a config bool (T-34-08).** `webSearchInfo`
   reads `ReadVerifyState()` (the cached `verifystate.State`), defaults to `"unknown"`, and
   is built ONLY inside the `cfg.WebSearchEnabled` gate but its VALUE never comes from that
   bool (`status.go:883-914`). `TestWebSearchOutboundBounded` includes the explicit case
   that `WebSearchEnabled=true` with no cached PASS yields `outbound_bounded != "bounded"`.
3. **Freshness gate with future-timestamp clamp (WR-01, commit 1397696).** Both the status
   core (`status.go:899`) and doctor (`cmd/villa/doctor.go:631`) clamp the lower bound:
   `age < 0 || age > window`. A PASS stamped in the future by a skewed/forged clock has a
   negative age (never `> window`), so without the lower-bound clamp it would read as fresh
   — the clamp is what keeps the no-false-green invariant. This was the single code-review
   finding (WR-01), resolved in `1397696` and verified present here.
4. **Doctor egress-proof tri-state (T-34-12).** The cached verify result maps to PASS only
   for a fresh PASS; stale/absent/future ⇒ typed-Unknown WARN with remediation
   ("run villa verify search"); never config-bool-derived.
5. **Residency offload-asserting (T-34-13).** A confident CPU fallback under search load is
   a FAIL that dominates a healthy HTTP-200; in-flight-only sampling means not-in-flight ⇒
   typed-Unknown, never an idle-sampled false-green.

---

## Review-Finding Disposition (cross-check)

The phase code review (34-REVIEW.md) raised one finding; it is resolved in the audited code:

| Finding | Disposition | Verified in code |
|---------|-------------|------------------|
| WR-01 (future-dated verify timestamp could read as fresh) | FIXED (commit 1397696) | lower-bound clamp `age < 0` in `status.go:899` and `time.Since(checked) < 0` in `cmd/villa/doctor.go:631`; regression cases added to `status_test.go` and `doctor_test.go` |

---

## Accepted Risks Log

None new this phase.

### Carried (not introduced this phase)

- **Supply-chain (analogue of T-33-SC):** No npm/pip/cargo/go-module installs occurred.
  All five plan SUMMARYs declare `tech-stack.added: []`; `git diff 840756b..HEAD -- go.mod
  go.sum` is empty. Host tools (nft/unshare/podman/curl) and OSS container images are
  unchanged. Residual = the pre-existing, already-audited dependency set.
- **Known residual (governing claim):** Per ROADMAP/STATE, *"Safe from injection" is NOT
  claimed.* The websafe guard reduces/flags injection only; the browser-side
  markdown-image exfil channel is a documented known residual out of scope for this
  surfacing phase. T-34-16's mitigation is XSS-prevention on the dashboard DOM
  (textContent-only) — it does not claim to eliminate upstream content-channel risk.

---

## Unregistered Flags

None. All five Phase-34 SUMMARYs carry a `## Threat Flags` section (34-02 titles it
`## Threat Mitigations Applied`), each declaring "None" — no new endpoint, auth path, or
trust boundary beyond the registered threats. The phase adds:

- one read-only 0600 verdict+timestamp file (T-34-01..04),
- one optional 0600 backup entry + verified restore (T-34-05..07),
- a read-only append-only status field + two in-network health probes (T-34-08..11),
- two read-only doctor findings + a bounded in-network chat drive (T-34-12..15),
- a hidden-until-data dashboard panel reading the existing poll (T-34-16..19).

No new outbound port, no new fetch/endpoint/probe, no dependency. Every new surface maps
to a registered threat.

---

## Verification Commands (re-runnable)

```
# fail-closed store + persist (T-34-01..04)
go test ./internal/verifystate/ ./cmd/villa/ -run 'TestStore|TestVerifySearch' -count=1
# backup 0600 + ephemeral-exclude + SHA-256 restore (T-34-05..07)
go test ./internal/backup/ -run 'TestBackupSearxngSettings|TestBackupExcludesEphemeral|TestRestoreSearxngSettings' -count=1
# status no-false-green + dedicated health rows + schema-5 goldens (T-34-08..11)
go test ./internal/status/ ./cmd/villa/ -run 'TestWebSearchOutboundBounded|TestRunSearxngWebsafeRows|TestRunWebSearch|TestStatus' -count=1
# doctor tri-state egress + offload-asserting residency + schema-3 golden (T-34-12..15)
go test ./internal/doctor/ ./cmd/villa/ -run 'TestAggregateWebSearch|TestSearchResidencyFinding|TestWebSearchFindingsHaveRemediation|TestDoctor' -count=1
# dashboard XSS / off-pixel-identity (T-34-16..19)
go test ./internal/dashboard/ -count=1
grep -c innerHTML internal/dashboard/assets/dashboard.js   # added this phase: 0
# seam gate — no leaked backend/image literal (T-34-14)
go test ./internal/inference/ -run TestSeamGrepGate -count=1
# no dependency change (supply-chain)
git diff 840756b..HEAD -- go.mod go.sum                    # empty
# full gate
make check
```

All commands above were executed during this audit and pass (`make check`: GREEN).

---

_Audited read-only against implemented code on branch `gsd/phase-34-web-search-surfacing-lands-last`._
_Implementation files were not modified. Auditor: gsd-secure-phase (FORCE stance)._
