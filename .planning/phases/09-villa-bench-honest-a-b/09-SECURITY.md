---
phase: 9
slug: villa-bench-honest-a-b
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-06
register_authored_at_plan_time: true
---

# Phase 9 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> **Result: SECURED — 11/11 threats CLOSED (10 mitigate + 1 accept), block_on: high.**
> Audit type: mitigation verification (register authored at plan-time; verify-only, no net-new threat scan).
> Privacy posture: strictly local, loopback-only (127.0.0.1), no telemetry; outbound = image/model pulls only.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| villa control plane → llama-server `/v1` (loopback 127.0.0.1) | bench issues a non-streaming completion to the already-audited local inference endpoint | numeric `timings` only (no prompt/sampling readback) |
| bench core ↔ injected `Deps` | every host-touching action (Measure / Switch / Restore) is an injected seam; the pure core crosses no real boundary | none (pure core) |
| `<backend>` `--ab` target → `inference.BackendFor` | untrusted CLI arg crosses into the fail-closed backend allowlist (same as `backend set`) | backend name string (validated) |
| `--ab` Switch/Restore → `backendswap.Run` | the ONLY mutation path; transactional rollback owned by the already-threat-reviewed Phase-8 core | quadlet/systemd backend state |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation (evidence) | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-09-01 | Information disclosure | `llm.Complete` response parsing | mitigate | `internal/llm/openai.go:68-93,142-199` — decodes ONLY the 6-numeric-field `Timings`; no prompt/sampling readback. Test `openai_test.go:78-114`. | closed |
| T-09-02 | Denial of service | unbounded non-200 body from a wedged server | mitigate | `internal/llm/openai.go:182-184` — `io.LimitReader(resp.Body, 2048)`. Test `openai_test.go:190-211` (8192-byte body bounded). | closed |
| T-09-03 | Tampering / Availability | `--ab` leaving stack on non-default backend (core) | mitigate | `internal/bench/bench.go:253-258` — `defer d.Restore(ctx, orig)` BEFORE the flip (265) → success/error/void-exhaustion/panic all restore. Test `bench_test.go:285-314`. | closed |
| T-09-04 | Repudiation / Integrity | a void (CPU-fallback) run counted as a slow pass | mitigate | `internal/bench/bench.go:210-221` — `resident==false` excluded from Kept, never substituted; `<MinResident` → VoidExhausted WARN. Tests `bench_test.go:218-260`. | closed |
| T-09-05 | Tampering (project invariant) | backend-marker literal leaking into `internal/bench` | mitigate | `internal/bench/bench.go:30-35` imports neither `inference` nor `detect`; markers only via injected Measure. `seam_test.go` TestSeamGrepGate Walk 1 green. | closed |
| T-09-06 | Spoofing / Validation | `--ab` backend target (untrusted CLI arg) | mitigate | `cmd/villa/bench.go:76-79` → fail-closed `inference.BackendFor` (`backend.go:21-30`); bounded-int guard `bench.go:308-311`. Test `bench_test.go:145-170`. | closed |
| T-09-07 | Information disclosure | bench render + `--json` output | mitigate | `cmd/villa/bench.go:362-399` + `testdata/bench.json.golden` — only numeric timings + Kept/Void + backend names + reproducible conditions; no prompt/message bodies. Tests: golden freeze + no-blended-key. | closed |
| T-09-08 | Denial of service | wedged/CPU-fallback server hanging a run forever | mitigate | `cmd/villa/bench.go:101-102,162-166` — `context.WithTimeout(spec.Timeout=5m)`; timed-out run is VOID, never an infinite wait. | closed |
| T-09-09 | Tampering / Availability | `--ab` leaving stack on non-default backend (cmd layer) | mitigate | `cmd/villa/bench.go:226-269` — Switch/Restore delegate to `backendswap.Run` (LOCKED Phase-8 transactional core); failed restore is LOUD + propagated. Tests `bench_test.go:273-341`. | closed |
| T-09-10 | Tampering (project invariant) | backend-marker literal leak in `cmd/villa/bench.go` | mitigate | `cmd/villa/bench.go` literal-free; markers only via `BackendFor(target).ResidencyProof()` (182). `seam_test.go:107-142` TestSeamGrepGate Walk 2 green. | closed |
| T-09-SC | Tampering (supply chain) | npm/pip/cargo/go installs | accept | No new package installs; `go.mod` last touched at Phase-1 commit `7a9b769`; working tree clean. Stdlib + existing repo packages only. | closed (accepted) |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

### Post-execution quick-task review — 260606-p3a (commits a210a7e, 8aa9c90, cb25a32)

The single added line `res.Backend = benchConfiguredBackend()` in `runBench` (`cmd/villa/bench.go`, guarded by `if !ab`) was re-reviewed:

- **T-09-07:** no weakening — `benchConfiguredBackend` reads ONLY `config.LoadVilla().Backend` (a backend NAME, non-sensitive config; `""` on error), not prompt/sampling data. Single-mode now matches the `--ab` path, which already emitted backend names.
- **T-09-03 / T-09-09:** unaffected — the `if !ab` guard leaves the flip/restore path untouched (it labels sides from `res.AB.From/To`, never `res.Backend`).
- **T-09-10:** no marker-literal introduced; TestSeamGrepGate Walk 2 (cmd/villa) still green.
- The pure `internal/bench` core was unchanged (stays config-unaware).

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-09-01 | T-09-SC | No new dependencies introduced this phase — stdlib + existing in-tree (pre-reviewed) packages only; `go.mod`/`go.sum` byte-unchanged. Supply-chain surface is unchanged from prior phases. | O Hingst | 2026-06-06 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-06 | 11 | 11 | 0 | gsd-security-auditor (verify-mitigations mode) |

### Verification commands (all green)
- `go test ./internal/inference/ -run 'TestSeamGrepGate'` → 2 passed (T-09-05, T-09-10)
- `go test ./internal/bench/` → 7 passed (T-09-03, T-09-04)
- `go test ./cmd/villa/ -run TestBench` → 15 passed (T-09-06, T-09-07, T-09-08, T-09-09)
- `go test ./internal/llm/ -run TestComplete` → 8 passed (T-09-01, T-09-02)
- `git status --porcelain go.mod go.sum` → clean (T-09-SC)

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-06
