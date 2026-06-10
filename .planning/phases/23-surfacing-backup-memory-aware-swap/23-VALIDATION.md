---
phase: 23
slug: surfacing-backup-memory-aware-swap
status: ready
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-10
---

# Phase 23 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib `testing`; table-driven + golden fixtures, no third-party assert) |
| **Config file** | none — `Makefile` targets exist |
| **Quick run command** | `go test ./internal/status ./internal/backup ./internal/modelswap ./internal/recall ./cmd/villa` |
| **Full suite command** | `make check` (go vet + go test ./...) |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick run command (affected packages)
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 23-01 T1 | 23-01 | 1 | CTRL-02 | T-23-01, T-23-05 | per-row health false-green fixed; typed-Unknown skew (never green for unevaluated) | unit (table-driven, fake Deps) | `go test ./internal/recall ./internal/status` | ✅ extend existing | ⬜ pending |
| 23-01 T2 | 23-01 | 1 | CTRL-02 | T-23-03, T-23-04 | fixed-arg seam-locked probes; 15s TTL bounds podman churn | unit (hermetic fakes) | `go test ./cmd/villa -run 'TestLiveQdrantHealth\|TestLiveEmbedHealth\|TestMemoryHealthTTL\|TestLiveReadRecallState' && go vet ./cmd/villa` | ✅ extend existing | ⬜ pending |
| 23-01 T3 | 23-01 | 1 | CTRL-02 | T-23-02 | exactly ONE deliberate golden re-freeze (D-04); v2→v3 delta inspected | golden + full suite | `make check` | ✅ re-freeze + new fixture | ⬜ pending |
| 23-02 T1 | 23-02 | 1 | CTRL-04 | T-23-06, T-23-08 | quiesce-before-export ordering; embedding skew WARN; typed-Unknown old manifests | unit (fakeDeps ordering assert) | `go test ./internal/backup -run 'TestBackup\|TestCompareSkew\|TestBuildManifest' && go test ./cmd/villa -run 'TestVolumeExists\|TestBackup'` | ✅ extend existing | ⬜ pending |
| 23-02 T2 | 23-02 | 1 | CTRL-04 | T-23-07, T-23-09, T-23-10, T-23-11 | clean-recreate both ways; 2×2 matrix zero-touch cells; rollback symmetry; existing readAndVerify guards | unit (fakeDeps matrix + failure injection) | `go test ./internal/backup -run TestRestore && make check` | ✅ extend existing | ⬜ pending |
| 23-03 T1 | 23-03 | 2 | CTRL-02 | T-23-12, T-23-13 | XSS-safe DOM (createElement+textContent only); typed-Unknown gray badges | build + vet + syntax | `go build ./... && go vet ./internal/dashboard && node --check internal/dashboard/assets/dashboard.js` | ✅ existing assets | ⬜ pending |
| 23-03 T2 | 23-03 | 2 | CTRL-02 | T-23-14 | passthrough only — no new endpoint/fetch/probe; loopback posture unchanged | unit (httptest) | `go test ./internal/dashboard && make check` | ✅ extend existing | ⬜ pending |
| 23-04 T1 | 23-04 | 2 | CTRL-05 | T-23-17 | chat swap leaves memory units byte-identical; exactly one Restart | unit (invariant, byte-equality) | `go test ./internal/orchestrate -run TestRender && go test ./internal/modelswap` | ✅ extend existing | ⬜ pending |
| 23-04 T2 | 23-04 | 2 | CTRL-05 | T-23-15, T-23-16, T-23-18 | fail-closed refusal before stamp overwrite; read-only install WARN; no auto-reindex | unit (exit-code + zero-mutation fakes) | `go test ./cmd/villa -run 'TestRecallIndex\|TestInstallMemory' && make check` | ✅ extend existing | ⬜ pending |
| 23-05 T1 | 23-05 | 3 | CTRL-02/04/05 | T-23-19, T-23-20, T-23-01 (closure) | safety backup first; stopped-embed negative control; box restored | live on-hardware drill | `./villa status --json \| grep -c '"schema_version": 3' && make check` | ✅ (live box) | ⬜ pending |
| 23-05 T2 | 23-05 | 3 | CTRL-02 | — | operator visual sign-off (dashboard rendering) | manual-only (checkpoint:human-verify) | MISSING by design — see Manual-Only Verifications; all automatable assertions ran in 23-05 T1 | — | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — `internal/status`,
`internal/backup`, `internal/modelswap`, `internal/recall`, and `cmd/villa`
already have table-driven test files and golden fixtures (`testdata/*.golden*`)
that the new tests extend. Golden re-freezes happen intentionally with
`go test … -update` exactly once per evolved contract. No Wave 0 scaffolding
needed (`wave_0_complete: true` reflects pre-existing infrastructure).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live status/dashboard memory rows on real stack | CTRL-02 | Needs running villa-qdrant/villa-embed + dashboard service | `make build`, restart `villa-dashboard.service`, check `villa status --json` + dashboard panel on the Strix Halo box |
| Real backup/restore of populated Qdrant volume | CTRL-04 | Needs a populated volume (post-Phase-21 index) + podman | `villa backup` → `villa restore` drill; verify clean-recreate + skew WARN path |
| Embedding-dimension swap drill | CTRL-05 | Needs live recall state + OWUI KB | Edit config embedding model, observe `recall index` refusal + remediation; `--rebuild` path |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (23-05 T2 is a blocking `checkpoint:human-verify` — manual by design, listed in Manual-Only Verifications; its automatable assertions run in 23-05 T1)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none — existing infra)
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ready — planner revision pass, 2026-06-10
