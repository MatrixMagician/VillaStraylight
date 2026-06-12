---
phase: 24
slug: coder-fit-math-catalog-on-hardware-model-qualification
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-12
---

# Phase 24 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib `testing`, table-driven + golden fixtures) |
| **Config file** | none — Makefile targets (`make test`, `make check`) |
| **Quick run command** | `go test ./internal/catalog/... ./internal/recommend/...` |
| **Full suite command** | `make check` (go vet + go test ./...) |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/catalog/... ./internal/recommend/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner) | | | CODER-01/02/03 | | | unit/golden | `make check` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements (go test + golden `-update` discipline already shipped).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Agent-in-the-loop qualification loop | CODER-03 | Requires live gfx1151 host, podman, real model weights, real Crush run | Per 24-RESEARCH.md qualification protocol (serve by digest with `--jinja` → KV/GTT measurement → curl tool-call smoke → `crush run --yolo` read→edit→verify → cache-reuse probe) |
| KV footprint measurement at agent ctx | CODER-03 | On-hardware GTT/metrics observation | sysfs GTT delta + llama-server `KV buffer size` log line at agent_ctx |
| Toolbox re-pin evidence | CODER-03/SC#4 | Inspect pinned image vintage on host | `podman run` by digest + llama-server version/parser greps per RESEARCH |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
