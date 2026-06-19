---
phase: 31
slug: grounded-fetch-embed-grounding
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-19
---

# Phase 31 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard library `testing`; table-driven + golden) |
| **Config file** | none — `go.mod` at repo root |
| **Quick run command** | `go test ./internal/websafe/... ./internal/recommend/... ./internal/orchestrate/...` |
| **Full suite command** | `make check` (go vet + go test ./...) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick run command for the touched package(s)
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD (planner fills) | — | — | GROUND-01/02/03, GUARD-01/05 | T-31-* | SSRF reject + skip-and-continue + ephemeral isolation | unit / golden | `go test ./...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

> The planner populates this map per task. Anchors from RESEARCH.md → Validation Architecture:
> - **Pure-core unit** (off-hardware): `internal/websafe` fetch/SSRF reject-set (resolve-then-validate + connect-time IP check + per-hop redirect re-check + scheme allowlist) via injected `Deps`; the always-200 skip-and-continue contract; `recommend` web-RAG reservation math.
> - **Golden/drift** (off-hardware): orchestrate render of the `villa-websafe` Quadlet unit (gated on `WebSearchEnabled`, byte-identical-off) + the OWUI external-loader env block (`WEB_LOADER_ENGINE=external`, `EXTERNAL_WEB_LOADER_URL`, BYPASS/retrieval lever) + a drift test binding each env KEY to its accessor; `recommend` golden re-freeze for the reservation field.
> - **On-hardware UAT** (genuinely manual): live grounded answer with inline citations to live URLs (GROUND-01); the BYPASS=False + `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS` retrieval-fix confirmation (RESEARCH A1, blocking before golden freeze); ephemeral-collection isolation from durable stores (GROUND-02); offload-assert under search load (GROUND-03).

---

## Wave 0 Requirements

- [ ] Test stubs for the new `internal/websafe` core (SSRF reject-set table; loader request/response contract)
- [ ] `recommend` reservation test extension (web-search envelope fit)
- [ ] orchestrate golden fixtures for the `villa-websafe` unit + OWUI external-loader env

*Planner confirms exact Wave 0 set; existing `internal/orchestrate` + `internal/recommend` test infrastructure covers most needs.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live grounded answer with inline citations to live URLs | GROUND-01 | Requires a real SearXNG + OWUI + llama stack and a live current-events query | `./villa install` with `web_search_enabled=true`; ask a current-events question with search on; confirm answer cites live result URLs via OWUI `sources` |
| Retrieval-fix lever (BYPASS=False + `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS`) actually grounds | GROUND-01 (RESEARCH A1) | OWUI v0.9.6 retrieval bug #25585; exact env key must be confirmed at the pinned digest on-hardware before freezing the golden | On-hardware: toggle the lever, confirm fetched content is retrieved+cited (not silently embedded-but-unqueried) |
| Ephemeral collection isolation from durable memory/doc-KB | GROUND-02 | Requires inspecting live Qdrant collections | After a web query, confirm fetched content is NOT in the durable memory/doc-KB collection |
| Offload-asserted residency under search load | GROUND-03 | Requires live GPU + search load | `villa status` OFFLOAD under an active search query; a silent/partial CPU fallback is a FAIL |

---

## Validation Sign-Off

- [ ] All tasks have automated verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter (after planner fills the per-task map)

**Approval:** pending
