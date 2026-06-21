---
phase: 34
slug: web-search-surfacing-lands-last
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-21
---

# Phase 34 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib `testing`; table-driven + golden fixtures) |
| **Config file** | none — toolchain built in (`go.mod`, Go 1.26) |
| **Quick run command** | `go test ./internal/status/ ./internal/backup/ ./internal/doctor/` |
| **Full suite command** | `make check` (vet + `go test ./...`) |
| **Estimated runtime** | ~60 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick run command for the touched package(s)
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner / validate-phase) | — | — | SURF-04..07 | — | — | golden/unit | `make check` | — | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- Existing infrastructure covers all phase requirements (stdlib `go test` + golden fixtures; no framework install needed).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Offload-asserting chat-model GPU-residency under search load | SURF-06 | Requires live gfx1151 host + running stack under a real search workload | On the Strix Halo host: enable web search, drive a search query, run `villa doctor` and confirm the residency-under-load check PASSes (no silent CPU fallback) |
| `villa verify search` egress proof (rootless-netns nft block) | SURF-04 (indicator source) | Requires on-hardware netns + nft | On-hardware `villa verify search` PASS produces the cached result the indicator reads |

*Automated coverage handles the JSON/golden/backup/doctor-schema contracts; the two host-only behaviors above are manual-on-hardware.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
