---
phase: 8
slug: villa-backend-set-switch-verb-rollback
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-06
---

# Phase 8 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| CLI arg → core/resolver | The `<backend>` target string crosses into `Run`/`liveBackendSwapDeps`/`liveProve`; validated by the fail-closed `inference.BackendFor` allowlist (vulkan/rocm/empty), never interpolated into a shell or path. | untrusted user input (low sensitivity) |
| core → filesystem (Quadlet dir) | Capture/restore read+write `villa-llama.container` bytes via the traversal-guarded `orchestrate.WriteUnits` (`assertInsideDir`). | unit file bytes (config, non-secret) |
| core → systemd (user manager) | daemon-reload / restart cross into the rootless user systemd via fixed-arg `orchestrate.Systemd` seams (no shell). | control commands |
| liveProve → running server | `inference.PollHealth` + `inference.GenerationProbe` + journald residency read probe the ALREADY-restarted `villa-llama.service` over its loopback endpoint; verdict markers sourced only from `BackendFor(target).ResidencyProof()`. | loopback HTTP, journald text (local only) |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-8-01 | Tampering | capture-before-mutate ordering in `Run` | mitigate | Capture (step 4) strictly precedes SaveConfig/ReconcileAndWrite/Restart; `TestCaptureBeforeMutate` (capIdx < save/write), `TestCaptureFailureRefuses` (backendswap.go:179-183, :234-243). | closed |
| T-8-02 | Denial of Service | failed/degraded switch leaving a broken stack | mitigate | Verbatim rollback restores exact prior unit+config and re-readies; `TestRollbackVerbatim`, `TestMutateErrorRollsBack` assert byte-equal restore (backendswap.go:191-210). | closed |
| T-8-03 | Spoofing (of health) | cutover gate trusting is-active/200 | mitigate | `Run` switches ONLY on `Prove.Status == ProveStatusPass`; any non-pass rolls back; `TestActiveNotSuccess` (backendswap.go:249-252). | closed |
| T-8-04 | Tampering | backend marker literals leaking into the core | mitigate | backendswap imports only `context`+`config`, literal-free of ROCm0/HSA/fault/image tokens; markers via injected `Prove` seam; `TestSeamGrepGate` walks `internal/` (seam_test.go:85). | closed |
| T-8-05 | Tampering | rollback partial-failure presented as clean | accept→report | Best-effort rollback accumulates errors and reports honestly ("rolled back, but the restore did not fully complete…") while keeping `RolledBack:true`; `TestRollbackIncompleteReported` (backendswap.go:214-231). | closed |
| T-8-06 | Spoofing (of health) | a degraded ROCm backend faking a successful cutover | mitigate | `liveProve` requires PollHealth ready AND GenerationProbe tokens>0 AND `RunningOffloadVerdict==StatusPass` (ROCm0 buffer + non-zero gpu_busy DURING decode + no Memory access fault), fed concrete `liveWeightBytes`/`liveModelFile`; CPU-fallback/0%-busy/fault → FAIL → rollback (backend.go:100-174). **Verified on-hardware (UAT Test 2).** | closed |
| T-8-07 | Denial of Service | unbounded `load_tensors` hang wedging the CLI | mitigate | `proveTimeout = 5*time.Minute` bounds the whole prove via `context.WithTimeout`; deadline expiry → FAIL → rollback, never infinite (backend.go:41, :94-106). **Verified on-hardware (UAT Test 3: tripped at 5m01s).** | closed |
| T-8-08 | Tampering | command injection via the `<backend>` arg | mitigate | `inference.BackendFor` fail-closed allowlist; fixed-arg `systemctl`/`journalctl` via `orchestrate.Systemd` (`exec.Command(name, args...)`, no shell) (systemd.go:68-76, :131-132). | closed |
| T-8-09 | Tampering | path traversal on unit capture/restore | mitigate | CaptureUnit reads inside `quadletUnitDir()`; RestoreUnit writes via `orchestrate.WriteUnits` → `assertInsideDir` rejects `..`/abs escapes (backend.go:414-420, :462-469; reconcile.go:52-63, :104-121). | closed |
| T-8-10 | Information disclosure | loopback→exposed port regression on re-render | accept | Host publish is `127.0.0.1` for both backends; PublishPort derived from ContainerArgs; `TestRenderedPublishLoopbackOnly` asserts `127.0.0.1:8080:8080` and no `0.0.0.0:` (render_test.go:326-336). Out of this phase's mutation surface; posture unchanged. | closed |
| T-8-11 | Tampering | ROCm0/HSA/fault literals leaking into cmd/villa | mitigate | `cmd/villa/backend.go` literal-free; markers arrive only via `backend.ResidencyProof()`; `TestSeamGrepGate` walk-2 over `cmd/villa` (seam_test.go:107-142). | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-8-01 | T-8-05 | Rollback is best-effort; a half-restored stack is reported honestly rather than masked as success. Verified by `TestRollbackIncompleteReported`. | O Hingst | 2026-06-06 |
| AR-8-02 | T-8-10 | The loopback/PublishPort privacy posture is unchanged by this phase's mutation surface and is guarded by the byte-frozen render golden + loopback assertion. | O Hingst | 2026-06-06 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-06 | 11 | 11 | 0 | gsd-security-auditor (verify-mitigations mode) |

Audit result: **## SECURED** — all 11 plan-time threats verified CLOSED with concrete file:line / passing-test evidence. No unregistered threat flags (SUMMARY 08-02 declared none; verified). The CR-G1 on-hardware blocker (commit f3eaedb) is confirmed fixed at HEAD and does not introduce a new threat (it removed a podman-illegal `--group-add render`; capture/restore and allowlist guards intact).

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-06
