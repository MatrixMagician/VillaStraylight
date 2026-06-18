---
phase: 30
slug: owui-native-search-wiring
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-18
---

# Phase 30 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` (table-driven + byte-for-byte golden fixtures) |
| **Config file** | none — `go test` built in; goldens under `internal/orchestrate/testdata/` |
| **Quick run command** | `go test ./internal/orchestrate/ ./internal/config/` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/orchestrate/ ./internal/config/`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite (`make check`) must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 30-01-xx | 01 | 1 | SRCH-02 | — | New `WebSearchResultCount` field; web-search keys omitted on-disk when off | unit | `go test ./internal/config/` | ✅ | ⬜ pending |
| 30-01-xx | 01 | 1 | SRCH-02 | T-30 (SSRF deferred to P31) | Web-search env group appends only when `WebSearchEnabled`; URL composed from config, no re-typed literal | unit/golden | `go test ./internal/orchestrate/ -run TestRenderOpenWebUI` | ✅ | ⬜ pending |
| 30-01-xx | 01 | 1 | SRCH-02 | — | `ENABLE_PERSISTENT_CONFIG=False` emitted exactly once and LAST when memory OR web-search on | golden | `go test ./internal/orchestrate/ -run TestRenderOpenWebUITelemetryFrozen` | ✅ | ⬜ pending |
| 30-01-xx | 01 | 1 | SRCH-02 (SC#4) | — | Drift test binds each web-search env KEY to its orchestrate accessor (env-name churn fails build) | unit | `go test ./internal/orchestrate/ -run TestRenderOpenWebUITelemetryFrozen` | ✅ | ⬜ pending |
| 30-01-xx | 01 | 1 | SRCH-02 (SC#4) | — | Search-off render byte-identical to v1.4 (negative test updated for new `searxngFixtureInput` on-render) | golden | `go test ./internal/orchestrate/ -run TestRenderByteIdenticalWhenWebSearchOff` | ✅ | ⬜ pending |
| 30-01-xx | 01 | 1 | SRCH-02 | — | No re-typed host/image literal leaks to callers | unit | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- Existing infrastructure covers all phase requirements. The orchestrate golden + frozen-telemetry tests already exist (`TestRenderOpenWebUITelemetryFrozen`, `TestRenderByteIdenticalWhenWebSearchOff`); this phase EXTENDS them with a web-search-on case and a new `villa-openwebui.container.websearch.golden`. No new framework, no Wave 0 install.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Operator opts into web search per-query via OWUI's native UI toggle and tunes result count | SRCH-03 (SC#2) | Requires a live OWUI container + browser; the native toggle is OWUI's own UI, not villa code | On-hardware UAT: `villa` install with `web_search_enabled=true`, open OWUI at 127.0.0.1:3000, toggle web search on a query, confirm SearXNG-sourced results appear |
| Honest no-results behavior — never a fabricated cited answer | SRCH-03 (SC#3) | Behavioral/model-level; depends on a real no-results query | On-hardware UAT: issue a query that returns zero SearXNG results with web search on; confirm the model does NOT fabricate a cited answer |

---

## Validation Sign-Off

- [ ] All tasks have automated verify or are listed under Manual-Only (SRCH-03 SC#2/SC#3 are inherently manual/UAT)
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (none — existing infra)
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
