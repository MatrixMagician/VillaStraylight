---
phase: 24
slug: coder-fit-math-catalog-on-hardware-model-qualification
status: approved
nyquist_compliant: true
wave_0_complete: true
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
| 24-01-T1 | 24-01 | 1 | CODER-01 | T-24-01 | revision-pinned shard URLs (never `/resolve/main/`); fail-closed defaults (absent role ⇒ chat, absent cache_reuse_safe ⇒ false) | unit (tdd) | `go test ./internal/catalog/... -run 'Load\|Schema\|Coder\|Seed' -count=1` | ✅ `internal/catalog/catalog_test.go` (new tests in task) | ⬜ pending |
| 24-01-T2 | 24-01 | 1 | CODER-01 | T-24-02, T-24-03 | external coder entries refused-with-warning + seed fallback, never clamped; 1 MiB bounded reader + DisallowUnknownFields retained | unit (tdd) | `go test ./internal/catalog/... -count=1 && make check` | ✅ `internal/catalog/catalog_test.go` + new `testdata/schema3-external.json` | ⬜ pending |
| 24-02-T1 | 24-02 | 2 | CODER-02 | T-24-04, T-24-05 | coder fit locked to catalog `agent_ctx` (no `--ctx` leak); coder block stamped on all paths incl. refusals | unit (tdd) | `go test ./internal/recommend/... -count=1` | ✅ new `internal/recommend/coder_test.go` + `recommend_test.go` | ⬜ pending |
| 24-02-T2 | 24-02 | 2 | CODER-02 | T-24-05 | schema-3 `--json` contract frozen append-only; exactly ONE golden re-freeze | golden | `go test ./cmd/villa -run Recommend -count=1 && make check` | ✅ `cmd/villa/recommend_test.go` + `testdata/recommend.golden.json` | ⬜ pending |
| 24-03-T1 | 24-03 | 2 | CODER-03 | T-24-08 | sha256-verified Crush tarball (hard abort on mismatch); GGUFs sha256-verified via internal/download | scripted gate | `echo "0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9  /tmp/crush-qual-download/crush_0.76.0_Linux_x86_64.tar.gz" \| sha256sum --check && /tmp/crush-qual/crush --version && ls ~/.local/share/villa/models/ \| grep -c "Qwen3-Coder"` | ✅ produced by task (qualify.sh, crush.json, staged artifacts) | ⬜ pending |
| 24-03-T2 | 24-03 | 2 | CODER-03 | T-24-07 | served by pinned digest (build 9496 proof); offload-asserting, never liveness | scripted evidence gate | `grep -v '^#' …/qualification/qwen3-coder-30b-a3b/server-version.txt \| grep -q "9496" && grep -qE "offloaded [0-9]+/[0-9]+" …/kv-gtt.txt && grep -qE "^VERDICT: (PASS\|FAIL)" …/verdict.md` | ✅ produced by task (evidence dir) | ⬜ pending |
| 24-03-T3 | 24-03 | 2 | CODER-03 | T-24-07 | both Next entries get explicit verdicts; live chat stack restored green (offload asserted) | scripted evidence gate | `grep -qE "^VERDICT: (PASS\|FAIL)" …/qwen3-coder-next-q4/verdict.md && grep -qE "^VERDICT: (PASS\|FAIL)" …/qwen3-coder-next-q3/verdict.md && ./villa status` | ✅ produced by task (evidence dirs) | ⬜ pending |
| 24-03-T4 | 24-03 | 2 | CODER-03 | T-24-09 | blocking human review of evidence chain before reconciliation; /tmp-scoped Crush confirmed (D-08) | checkpoint:human-verify | `ls …/qualification/*/verdict.md \| grep -c verdict` (3 expected) + operator approval | ✅ verdict files from T2/T3 | ⬜ pending |
| 24-04-T1 | 24-04 | 3 | CODER-01, CODER-03 | T-24-14, T-24-15 | cache_reuse_safe is a literal copy of probe verdicts; no FAIL entry shipped; no `/resolve/main/` in coder shards | unit + scripted gate | `make check && python3 -c "…assert coders…assert all('/resolve/main/' not in s['url'] …)…"` (full gate in 24-04 Task 1) | ✅ `internal/catalog/catalog_test.go` (reconciled values) | ⬜ pending |
| 24-04-T2 | 24-04 | 3 | CODER-03 | T-24-13 | D-11 decision recorded with evidence citations BEFORE the freeze | scripted gate | `grep -qi "^Decision:" …/24-TOOLBOX-DECISION.md && grep -qi "qwen3-coder-30b-a3b" …/24-QUALIFICATION-EVIDENCE.md` | ✅ produced by task | ⬜ pending |
| 24-04-T3 | 24-04 | 3 | CODER-01, CODER-03 | T-24-13 | catalog freeze ratified against the evidence chain at a blocking checkpoint | checkpoint:human-verify | `make check` + operator approval | ✅ artifacts from 24-04 T1/T2 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*(`…` abbreviates `.planning/phases/24-coder-fit-math-catalog-on-hardware-model-qualification` — full commands live in each plan's `<automated>` element.)*

**Sampling continuity:** every task carries an `<automated>` verify; no task relies on manual-only verification (checkpoints 24-03-T4 and 24-04-T3 pair an automated gate with the human review). Max gap between automated verifies: 0 tasks.

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements (go test + golden `-update` discipline already shipped). No MISSING verifies — no Wave 0 scaffold tasks required (`wave_0_complete: true` by vacuous satisfaction).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Agent-in-the-loop qualification loop | CODER-03 | Requires live gfx1151 host, podman, real model weights, real Crush run | Per 24-RESEARCH.md qualification protocol (serve by digest with `--jinja` → KV/GTT measurement → curl tool-call smoke → `crush run --yolo` read→edit→verify → cache-reuse probe) |
| KV footprint measurement at agent ctx | CODER-03 | On-hardware GTT/metrics observation | sysfs GTT delta + llama-server `KV buffer size` log line at agent_ctx |
| Toolbox re-pin evidence | CODER-03/SC#4 | Inspect pinned image vintage on host | `podman run` by digest + llama-server version/parser greps per RESEARCH |

*Note: each manual verification above is still gated by an automated evidence-file check (24-03-T2/T3 greps) — the manual part is producing the evidence on hardware, not asserting it.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none exist — existing infra suffices)
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-12 (planner revision pass — per-task map filled from plans 24-01..24-04)
