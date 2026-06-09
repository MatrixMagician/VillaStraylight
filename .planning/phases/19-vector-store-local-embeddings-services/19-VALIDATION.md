---
phase: 19
slug: vector-store-local-embeddings-services
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-09
---

# Phase 19 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (standard `testing`, table-driven + golden fixtures) |
| **Config file** | none — Go modules, `make check` = vet + test |
| **Quick run command** | `go test ./internal/orchestrate/... ./internal/memory/... ./internal/config/...` |
| **Full suite command** | `make check` (`go vet ./...` + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched package(s)
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

> Filled by the planner — every task maps to an automated `go test` (render golden,
> seam-gate, config byte-identical, reconcile) or a `checkpoint:human-verify` for the
> two on-hardware proofs (Qdrant writability; kyuz0 `/v1/embeddings` 768-dim health).

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 19-XX-XX | XX | N | INFRA-01/02 / PRIV-04 | T-19-XX / — | container-DNS/loopback only; no host port | golden/unit | `go test ./internal/orchestrate/...` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- Existing Go test infrastructure (`go test`, golden fixtures under `internal/orchestrate/render_test.go`, `internal/inference/seam_test.go`) covers all phase requirements. New golden fixtures for the `qdrant`/`embed` rendered units are added in-plan (refreeze with `-update`), not as separate Wave 0 framework installs.

*No framework install needed — Go toolchain already present.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Qdrant writable on a rootless named `:Z` volume (no UID/SELinux failure) survives reboot | INFRA-01 / SC#2 | Needs a live Fedora rootless-Podman host + reboot | `villa install` with `memory_enabled=true`; write a point to `villa-qdrant:6333`; reboot; confirm the collection persists |
| `villa-embed` `/v1/embeddings` returns a 768-length vector offline on the pinned kyuz0 digest (guards llama.cpp #15406) | INFRA-02 / PRIV-04 / SC#3 | Needs the pinned image running on-GPU with no network | Offline curl `POST /v1/embeddings` against `villa-embed:8080`; assert `len(data[0].embedding) == 768`, no outbound network |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
