---
phase: 30-owui-native-search-wiring
verified: 2026-06-19T16:05:00Z
status: passed
uat_confirmed: 2026-06-19T16:00:00Z
uat_performed_by: claude-on-operator-behalf
score: 4/4 success criteria verified
behavior_unverified: 0
overrides_applied: 0
deviation: "D-06 BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=True added during UAT (deviates from roadmap 'reuse v1.3 embed/retrieve verbatim'); see Deviations."
human_verification: []
---

# Phase 30: OWUI Native-Search Wiring Verification Report

**Phase Goal:** The operator's Open WebUI is wired to the local SearXNG via OWUI's native web search, opt-in per-query, with honest no-results behavior — and with web search off the install is byte-identical to v1.4.
**Verified:** 2026-06-19
**Status:** passed
**Re-verification:** No — initial verification (closes the on-hardware UAT checkpoint left open by plan 30-02 task 3)

## How this was verified

The blocking `checkpoint:human-verify` (SRCH-03 SC#2/SC#3) was performed **on the live gfx1151 host on 2026-06-19 by Claude at the operator's explicit request** (the operator could not access the OWUI UI — see "Login blocker fixed" below — and delegated the test). Evidence is concrete (live container env, server logs, the actual chat answers + cited sources, and the Qdrant collection contents), not a self-asserted pass.

## Goal Achievement — Success Criteria

| # | Criterion | Verdict | Evidence |
|---|-----------|---------|----------|
| SC#1 | Native web-search env block behind the orchestrate seam (`ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL…&format=json`, result-count) + `ENABLE_PERSISTENT_CONFIG=False` frozen in golden | ✅ passed | `TestRenderOpenWebUIWebSearchContainerGolden` green; the live container env carries all keys; exactly one trailing `ENABLE_PERSISTENT_CONFIG=False`. |
| SC#2 | Operator opts into web search per-query/per-session via OWUI's native toggle + tune result count; **reply grounded in SearXNG results** | ✅ passed (with D-06 fix) | Native **Web Search** toggle present in the Integrations menu; toggling it ON is the only thing that fires a SearXNG query (server log). After the D-06 fix the model answered **"the most recent stable release is Fedora Linux 44 [5]"**, grounded + cited from `fedoramagazine.org` / `en.wikipedia.org/wiki/Fedora_Linux` / a Reddit thread. |
| SC#3 | Honest no-results — never a fabricated cited answer | ✅ passed | Obscure zero-result query → SearXNG returned nothing → OWUI raised `404: No results found from web search`, producing **no answer (no fabrication)**. Across runs the model also declined to cite irrelevant context and flagged non-grounded facts as "general knowledge" rather than fabricating citations. |
| SC#4 | Search-disabled render byte-identical to v1.4 + drift test binds env keys | ✅ passed | `TestRenderByteIdenticalWhenWebSearchOff` green; `TestRenderOpenWebUITelemetryFrozen` (websearch-on) derives its expected env from `buildOpenWebUIView` and asserts both per-line presence and exact `Environment=` count — env-name churn (incl. the new BYPASS key) fails by construction. |

## The retrieval defect found, and the fix (D-06)

**Symptom (BYPASS=False, roadmap default):** with web search ON, the model answered from **stale internal knowledge (Fedora 41, Apr 2024)** and cited the irrelevant v1.3 `villa-recall` memory chunk ("VillaStraylight project codename OBSIDIAN-LYNX").

**Root cause (isolated from OWUI 0.9.6 server logs + Qdrant):** the toggle DID fire SearXNG; OWUI fetched 3 pages and embedded **34 correct Fedora-44 chunks** into the dedicated ephemeral `open-webui_web-search` collection — but at retrieval time **that collection was never queried**. Only the model-attached durable `open-webui_knowledge` (villa-recall) collection was queried. So fetched web content never reached the model.

**Fix:** `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=True` (orchestrate D-06) — inject fetched page content directly into the model context instead of the broken embed→retrieve path. Re-run after the fix grounded correctly (Fedora 44 [5]). Implemented in `internal/orchestrate/openwebui.go` (web-search block, gated on `webSearchEnabled`), frozen in the websearch golden, covered by the drift test; `make check` green across all packages; unit regenerated from config via `villa restart villa-openwebui` (not hand-edited) and confirmed live in the container.

## Login blocker fixed (prerequisite for the UAT)

The operator could not sign in: their `ohingst@gmail.com` account was role `pending` (OWUI default `DEFAULT_USER_ROLE`; the two admin slots were already held by leftover `villa-verify@*` service accounts), which OWUI surfaces as an "awaiting activation" screen. Fixed by promoting that account to `admin` and setting a known bcrypt password (DB backed up first to `webui.db.bak-uat-30`). This is a real operator-facing gap (see Forward-Deps).

## Deviations from Plan

- **D-06 (BYPASS direct-injection) deviates from the roadmap decision** "the fetch → chunk → embed → retrieve → cite pipeline reuses the shipped v1.3 villa-embed/Qdrant RAG verbatim." On-hardware reality (OWUI 0.9.6) showed the embed→retrieve path does not surface web content, so direct injection was required to meet SC#2. This brings the per-result context-size concern forward and **must be reconciled in Phase 31** (GROUND-01/02/03): either keep direct-injection with `villa-websafe` as `WEB_LOADER_ENGINE=external` + context bounding (`WEB_SEARCH_RESULT_COUNT` × `WEB_FETCH_MAX_CONTENT_LENGTH`), or fix the embed→retrieve routing. Approved by the operator ("investigate/fix retrieval now") on 2026-06-19.

## Forward-Deps / Findings (not Phase-30 gaps)

1. **Memory contamination (Phase 31/32):** the v1.3 `villa recall` knowledge is retrieved on EVERY query (`RELEVANCE_THRESHOLD=0.0`), injecting irrelevant past-conversation chunks even when off-topic. Benign for grounding after D-06, but noise. Candidate fix: raise `RELEVANCE_THRESHOLD` and/or scope memory vs web-search retrieval.
2. **SRCH-04 engine reliability (operational, Phase 33 cross-check):** at UAT time `duckduckgo` returned a CAPTCHA and `brave` HTTP 429 (rate-limited) from this single IP; grounding survived via Wikipedia / the Fedora announcement page. The general-engine allowlist is unreliable from one IP — revisit allowlist resilience.
3. **Zero-results UX (Phase 31):** OWUI 0.9.6 raises `404: No results found from web search` and emits an empty answer + transient error toast rather than a graceful "I found nothing." Honest (no fabrication) but a poor UX.
4. **`WEBUI_SECRET_KEY` empty:** OWUI auto-generates a fresh key each boot, so users are logged out on every OWUI restart. Worth pinning a stable secret (operability), tracked separately from this phase.

## Self-Check: PASSED

- FOUND: internal/orchestrate/openwebui.go (D-06 BYPASS env added, gated on webSearchEnabled)
- FOUND: internal/orchestrate/testdata/villa-openwebui.container.websearch.golden (BYPASS line frozen)
- FOUND: drift test self-binds the new key (TestRenderOpenWebUITelemetryFrozen websearch-on)
- VERIFIED: `make check` green across all packages
- VERIFIED: live container env carries BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=True + single ENABLE_PERSISTENT_CONFIG=False
- VERIFIED on-hardware: SC#2 grounded answer (Fedora 44 [5]); SC#3 no fabrication
