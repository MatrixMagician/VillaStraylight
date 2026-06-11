---
phase: 22
slug: control-plane-fit-host-gate
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-10
---

# Phase 22 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| config.toml → recommend core | hand-edited untrusted TOML flows into fit math via cmd-tier load | model id, ctx, memory_* fields |
| recommend output → install resource gate | a wrong reservation propagates into the install BLOCK gate | reservation bytes |
| config.toml → preflight gating | hand-edited untrusted TOML decides whether gates run and which model id feeds the floor | memory_enabled, embedding_model |
| podman exec output → statfs path | host-tool output becomes a filesystem path probed by statfs | volume-root path string |
| config.toml → embed-load drive | config-sourced embed model/addr/port flow into a podman/curl exec | model id, container addr:port |
| container journal/sysfs → residency verdict | untrusted service output feeds the PASS/WARN/FAIL decision | journal text, GTT bytes |
| doctor (read-only diagnosis) → host state | a diagnostic that mutates services would violate its contract | systemd unit state |
| verification commands → live operator box | UAT runs against the operator's real running stack and config | backend choice, unit state, config.toml |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-22-01 | DoS | recommend.Pick reservation | mitigate | `internal/recommend/recommend.go:195-207` conservative default on typed-Unknown (never 0 when enabled); `:169-173` clamp-to-0 (no uint64 wrap); `:377` `addSaturating` (WR-07); single-source constant `internal/memory/footprint.go:37-42` | closed |
| T-22-02 | Tampering | EmbeddingModel config string | mitigate | flows only into map lookup (`internal/memory/footprint.go:49-57`), fmt note text, preflight floor lookup, `json.Marshal` body (`cmd/villa/doctor.go:424-428`) — no exec/shell/path use | closed |
| T-22-03 | Repudiation | install pick seam | mitigate | `cmd/villa/install.go:1027` threads `liveLoadedMemoryInputs()`; `:281` MinMemBytes includes reservation; all 7 production `Pick` call sites verified (status deliberately zero-value to keep golden frozen) | closed |
| T-22-04 | Info integrity | recommend --json contract | mitigate | `recommendSchemaVersion = 2` (recommend.go:32); append-only fields above SchemaVersion; targeted golden re-freeze carries both keys + schema 2 | closed |
| T-22-05 | Tampering | liveVolumeRoot podman invocation | mitigate | `internal/preflight/checks_memory.go:43` via `runTool` only; `exec.go:30-51` fixed-arg, 8 KiB LimitReader, 10 s timeout (WR-02); output used only as statfs path; TestSeamGrepGate green | closed |
| T-22-06 | DoS | host under-provisioned for memory stack | mitigate | BLOCK-tier FAIL + remediation on confident disk/headroom shortage (`checks_memory.go:106-111`, `:159-161`); conservative floor on Unknown footprint; install refuses pre-stack (`install.go:290-291`) | closed |
| T-22-07 | Repudiation | unevaluable probe → false verdict | mitigate | resolver/statfs failure → WARN with provenance (`checks_memory.go:91-104`); Unknown MemAvailable → WARN (`:140-146`); all branches table-test pinned | closed |
| T-22-08 | Elevation | broken config silently enabling gates | mitigate | fail-soft load → memory-off (`cmd/villa/preflight.go:31-37`, `cmd/villa/recommend.go:207-213`); off-path byte-identicality guarded by frozen preflight goldens | closed |
| T-22-09 | Tampering | embed-load drive args | mitigate | body via `json.Marshal` (`cmd/villa/doctor.go:424-428`); `runProbeCurl` fixed-arg `exec.CommandContext` (`cmd/villa/install_memory.go:322-330`), no shell | closed |
| T-22-10 | Repudiation | silent CPU fallback under load | mitigate | D-09 mapping: StatusFail → BLOCK FAIL, default → WARN, nil seam → no finding (`internal/doctor/doctor.go:494-517`, `:292-294`); drive errors degrade PASS→WARN; down-rank predicate suppresses only WARN, never FAIL (pinned by `TestMemoryOffloadFailNotSuppressed`); WR-04 fail-closed on unrecognized verdict | closed |
| T-22-11 | DoS | load proof hammers/hangs/leaks | mitigate | N=12 requests, 10 s/req, 60 s parent timeout (`cmd/villa/doctor.go:308-320`, `:437`); `--rm` containers; in-flight sample joined before return (`:484`) | closed |
| T-22-12 | Tampering | "read-only" doctor mutating state | mitigate | D-10 precondition gate degrades to WARN (`doctor.go:382-401`); zero systemctl mutation calls; live-proven by embed-down negative control (unit inactive post-run, re-proven in 22-UAT) | closed |
| T-22-13 | Info Disclosure | new probe traffic leaving the box | mitigate | all drive traffic on villa.network container DNS, no host port (`install_memory.go:210-214`, `:324-325`); helper image already pulled; zero new outbound | closed |
| T-22-14 | Tampering | operator state during verification | mitigate | record-first/restore-last evidenced in 22-04-SUMMARY Box State: backend rocm unchanged, no `--save`, config byte-identical, 9/9 units active | closed |
| T-22-15 | Repudiation | verification greener than observed | mitigate | verbatim outputs in 22-04-SUMMARY; mandatory negative controls executed (memory-off absence, embed-down WARN); first-run honest FAIL documented; re-executed live in 22-UAT | closed |
| T-22-16 | DoS | sustained drive destabilizing live box | accept | bounded 12-req/60 s drive; live: 3.5 s wall, MemoryPeak ~110.9 MiB (UAT re-measure 216.6 MiB) vs ~75 GiB free, no orphaned containers | closed |
| T-22-SC | Tampering | package installs | accept | zero new packages: all four SUMMARY frontmatters `tech-stack.added: []`, `go.mod` untouched | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-22-01 | T-22-16 | Doctor's embedding-load drive is bounded (12 requests, 10 s/req, 60 s overall, `--rm` transient containers) against a 125 GiB host with ~75 GiB free; measured impact trivial (3.5 s wall, ≤216.6 MiB embed peak vs 512 MiB reservation) | operator (plan-time disposition, UAT-confirmed) | 2026-06-10 |
| AR-22-02 | T-22-SC | Zero new packages this phase — supply-chain surface unchanged (`go.mod` untouched, `tech-stack.added: []` across all summaries) | operator (plan-time disposition) | 2026-06-10 |

*Accepted risks do not resurface in future audit runs.*

---

## Unregistered Flags (informational, none blocking)

1. **Pre-existing per-row health false-green** — `internal/status/status.go:376` probes the single chat endpoint for every non-OWUI row, so a stopped villa-embed still renders `health … PASS`. Pre-existing since Phase 19; doctor verdict honesty unaffected (Active state + MEM-DOC-residency catch it → exit 2). Carried to Phase 23 (status schema 2→3 memory rows) — assign a threat ID in the Phase 23 register. Graphmind memory `527de579`.
2. **WR-05 dry-run privileged host-prep** — `--dry-run` could execute the wizard's privileged host-prep consent path; found in post-summary code review (no register entry), fixed in `1fe19f1` (`cmd/villa/install.go:301-302, 692, 766, 827`). Elevation surface appeared during implementation without a threat mapping — register-at-plan-time discipline note for future phases.
3. **WR-02/WR-04/WR-06/WR-07 review fixes** map to existing threats (T-22-05/07, T-22-10, T-22-10, T-22-01 respectively) — all verified present.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-10 | 17 | 17 | 0 | gsd-security-auditor (146 pinning tests green across recommend/memory/preflight/doctor; TestSeamGrepGate green) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-10
