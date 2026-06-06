---
phase: 9
slug: villa-bench-honest-a-b
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-06
---

# Phase 9 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `09-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (table tests + golden files) |
| **Config file** | none — `go test` |
| **Quick run command** | `go test ./internal/bench/... ./cmd/villa/ -run Bench -count=1` |
| **Full suite command** | `go test ./... -count=1` |
| **Estimated runtime** | ~30 seconds (full suite); quick run ~5s |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/bench/... ./cmd/villa/ -run Bench -count=1 && go vet ./...`
- **After every plan wave:** Run `go test ./... -count=1`
- **Before `/gsd-verify-work`:** Full suite must be green **+ on-hardware UAT** (real `villa bench` + `villa bench --ab` on gfx1151, residency-checked delta)
- **Max feedback latency:** ~30 seconds (automated); on-hardware UAT is manual

---

## Per-Task Verification Map

| Req ID | Behavior | Test Type | Automated Command | File Exists | Status |
|--------|----------|-----------|-------------------|-------------|--------|
| BENCH-01 | pp/tg reported as TWO separate figures; never blended | unit | `go test ./internal/bench/ -run TestSeparatePPTG -count=1` | ❌ W0 | ⬜ pending |
| BENCH-01 | non-resident (CPU-fallback) run is VOID, excluded from stats | unit | `go test ./internal/bench/ -run TestVoidNonResident -count=1` | ❌ W0 | ⬜ pending |
| BENCH-01 | `--ab` composes `backendswap.Run` (never re-implements switching) + RESTORES original | unit | `go test ./cmd/villa/ -run TestBenchABRestoresOriginal -count=1` | ❌ W0 | ⬜ pending |
| BENCH-02 | warmup run discarded; not counted in stats | unit | `go test ./internal/bench/ -run TestWarmupDiscarded -count=1` | ❌ W0 | ⬜ pending |
| BENCH-02 | median + stddev correct over known inputs (per metric) | unit | `go test ./internal/bench/ -run TestStats -count=1` | ❌ W0 | ⬜ pending |
| BENCH-02 | identical BenchSpec applied to both `--ab` sides | unit | `go test ./internal/bench/ -run TestIdenticalSpecBothSides -count=1` | ❌ W0 | ⬜ pending |
| BENCH-02 | insufficient resident runs ⇒ honest WARN, not a confident delta | unit | `go test ./internal/bench/ -run TestVoidExhaustionWarn -count=1` | ❌ W0 | ⬜ pending |
| BENCH-01/02 | `--json` shape frozen (Phase 10 reads tok/s from it) | golden | `go test ./cmd/villa/ -run TestBenchJSON -count=1` | ❌ W0 | ⬜ pending |
| (seam) | `cmd/villa/bench.go` literal-free of backend markers | grep-gate | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ extend | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/bench/bench_test.go` — fake-`Deps` recorder; warmup-discard, void-gate, median/stddev, identical-spec, void-exhaustion (covers BENCH-01/02)
- [ ] `cmd/villa/bench_test.go` — stubbed Deps: exit mapping, `--ab` restores original, `--json` golden
- [ ] `cmd/villa/testdata/bench.json.golden` — frozen `--json` shape
- [ ] (if used) `internal/llm/openai_test.go` extension — `Complete` parses the `timings` block from a fixture response body

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Per-model pp/tg Vulkan-vs-ROCm delta magnitude | BENCH-01/02 | The volatile value bench exists to measure — requires real gfx1151 + loaded model | Run `villa bench` then `villa bench --ab` on-hardware; confirm pp/tg reported separately, residency-checked, delta within noise band |
| SELinux `/dev/kfd` behavior on ROCm `--ab` side | BENCH-01 | Host-policy dependent | During `--ab`, confirm ROCm side reaches GPU residency (no CPU fallback void) |
| `/v1` `timings` block present on running server | BENCH-01 | Server-build dependent | One-line probe: non-streaming `/v1/chat/completions` returns `timings.predicted_per_second` |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
