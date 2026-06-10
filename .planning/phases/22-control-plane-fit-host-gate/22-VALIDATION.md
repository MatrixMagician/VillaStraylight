---
phase: 22
slug: control-plane-fit-host-gate
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-10
---

# Phase 22 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard library; table-driven + golden fixtures) |
| **Config file** | none — Makefile targets (`make test`, `make check`) |
| **Quick run command** | `go test ./internal/recommend/ ./internal/preflight/ ./internal/doctor/ ./internal/memory/` |
| **Full suite command** | `make check` (go vet + go test ./...) |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/recommend/ ./internal/preflight/ ./internal/doctor/ ./internal/memory/` (plus `./cmd/villa/` when cmd-tier files change)
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner) | | | CTRL-01 / CTRL-03 / CTRL-06 | | | unit / golden | `make check` | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — `internal/recommend`,
`internal/preflight`, `internal/doctor`, `internal/memory`, and `cmd/villa`
all have established `_test.go` suites, fake-Deps doubles, and golden-fixture
machinery (`-update` flag pattern). No new framework or harness needed.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Doctor residency proof under real embedding load | CTRL-03 | Needs the live gfx1151 box: real villa-llama + villa-embed, real GTT scrape under `/v1/embeddings` load | On the dev box with the stack up: `./villa doctor` (and `--json`); assert chat-model residency finding is PASS while embed requests run; confirm typed-Unknown WARN (not PASS) when stack is down |
| Footprint measurement (D-05) | CTRL-01 | On-hardware cgroup/GTT observation | Record villa-embed MemoryCurrent/MemoryPeak under load in the phase summary; raise the 512 MiB constant only if it under-reserves |
| Preflight vector-disk gate on real volume root | CTRL-06 | statfs target is the live rootless podman volume path | `./villa preflight` with memory on: disk/headroom rows present with remediation; with `memory_enabled=false`: rows absent (byte-identical output) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
