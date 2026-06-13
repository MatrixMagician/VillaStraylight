---
phase: 26
slug: agent-delivery-core-lockdown-launcher
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-13
---

# Phase 26 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

**Scope:** `internal/agent` (Crush coding-agent delivery core — pin policy + checksum
gate + deterministic `crush.json` renderer + drift detector + install seam) and the
`villa code` launcher verb (`cmd/villa/code.go`, registered in `cmd/villa/root.go`).

**Verification method:** each declared mitigation was located in the implementation
(file:line) AND its guarding test/gate was run green. Documentation/intent was not
accepted as evidence. Register authored at plan time across all three plans
(26-01/02/03) → verify-mitigations-exist mode (no retroactive scan). Implementation
files were not modified.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| GitHub release CDN → host filesystem | Install seam streams + extracts the third-party Crush tarball to `$XDG_DATA_HOME/villa/bin`. | Untrusted third-party tarball bytes (verified SHA-256 before extraction). |
| villa process → crush process | `villa code` execs the binary, handing it the lockdown env. | Constant-literal env vars + explicit binary path (no PATH/shell). |
| user PATH → launcher | A user-installed `crush` on PATH must NOT be exec'd (D-05). | Untrusted PATH entries (ignored for the launch binary). |
| config.toml → rendered crush.json | villa-controlled values rendered into a file Crush `$(...)`-expands at load. | villa-controlled config values (metachar-free literals only). |
| embedded crush-policy.json → core | Build-time `//go:embed` data, compiled in — NOT runtime attacker input. | Trusted build-time constants. |
| installed binary → drift detector | The pinned `binarySha256` is the trust anchor for binary-drift. | Hash of the on-disk binary (report-only comparison). |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-26-01 | Tampering | downloaded Crush tarball (supply chain) | mitigate | `VerifyTarball` — size-then-`strings.EqualFold` SHA-256 (`crypto/sha256`), refuse-with-remediation (`internal/agent/policy.go:88-104`; `TestChecksumGate` green) | closed |
| T-26-02 | EoP/Tampering | rendered crush.json values (`$(...)` exec) | mitigate | Every rendered value a fixed metachar-free literal; global-only render (`render.go:24-45,177-183`; `TestRenderNoMetachars` green) | closed |
| T-26-03 | Info Disclosure | cloud-model fallback / extra providers | mitigate | Exactly one openai-compat provider + `disable_default_providers:true` + non-empty `models[]` (`render.go:158-172`; golden:6; `TestRenderContract` green) | closed |
| T-26-04 | Info Disclosure | telemetry phone-home | mitigate | `disable_metrics=true` + `disable_provider_auto_update=true` (`render.go:159-160`; `TestRenderContract:231` green) | closed |
| T-26-05 | Tampering | malformed embedded policy | accept | Build-time panic on malformed `//go:embed` — no runtime attacker path (`policy.go:73-79`; `TestPolicyLoadPanicsOnMalformed` green) | closed |
| T-26-06 | Tampering | silent binary/config drift | mitigate | `DetectDrift` report-only (no auto-correct); binary-hash compare; config-ABSENT distinct (`drift.go:80-117`; 3 drift tests green) | closed |
| T-26-SC | Tampering | charmbracelet/crush release pin | mitigate | Pinned `v0.76.0` + tarball SHA-256 + size; zero new go.mod deps (`crush-policy.json:2-11`) | closed |
| T-26-07 | Tampering | install seam — unverified tarball reaching extraction | mitigate | `Install` calls `VerifyTarball` (line 65) BEFORE `extractCrushBinary` (line 73) (`install.go:52-78`; mismatch-refuses test green) | closed |
| T-26-08 | Tampering | tar extraction path traversal | mitigate | stdlib `archive/tar`; `assertInsideBinDir` rejects `..`/absolute (`install.go:85-125,155-172`; traversal-rejected test green) | closed |
| T-26-09 | EoP | launching the wrong crush (PATH hijack) | mitigate | `syscall.Exec` of explicit `agentBinPath()`; `exec.LookPath` only for LSP refs, never the launch binary (`code.go:181-187,217-219`) | closed |
| T-26-10 | Info Disclosure | telemetry/autoupdate phone-home at launch | mitigate | `lockdownEnv` appends `CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`, `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1` (`agent.go:51-55,229-236`; `TestCodeLockdownEnv` green) | closed |
| T-26-11 | Tampering | silent drift auto-correction at launch | mitigate | Drift surfaced + exits without writing; only write path is config-ABSENT first-run render (`agent.go:181-201`, `code.go:95-98`; drift tests green) | closed |
| T-26-12 | Tampering | command injection via launched args/env | mitigate | Fixed-arg `syscall.Exec`, no shell; constant-literal env; no user interpolation (`code.go:183`) | closed |
| T-26-13 | EoP | `villa code` auto-flipping coding mode | mitigate | `code.go` reads-never-assigns `CodingMode`; OFF is a WARN; `TestNoAutoFlipStructuralGuard` green | closed |
| T-26-14 | Tampering | pinning a hash from an unverified binary | mitigate | Verify-before-extract; binary hash derived only from the SHA-256-verified tarball (`install.go:62-67`; 26-03-SUMMARY:26-30) | closed |
| T-26-15 | Spoofing | a fabricated binarySha256 value | mitigate | `binarySha256` = `4fd811f6…42b4`, byte-identical to recorded command output; `TestPolicyLoad` asserts hash + rejects sentinel (`crush-policy.json:8`) | closed |
| T-26-16 | Info Disclosure | a launched agent phoning home (runtime egress) | accept | Deferred to Phase 27 (PRIV-06); on-hardware launch observed honest (no fetch). Active instrumented egress proof out of scope here | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-26-01 | T-26-05 | The pin policy is `//go:embed`-compiled into the binary (build-time data), not parsed from any attacker-reachable runtime path. A malformed embed is a build-time programming error caught by `loadCrushPolicy`'s panic at startup; no entry point lets an attacker supply these bytes. Confirmed `policy.go:12-21` `//go:embed`, no runtime read of an external policy file. | gsd-security-auditor (--auto) | 2026-06-13 |
| AR-26-02 | T-26-16 | Phase 26 ships config kill switches (T-26-03/04) + launch-env lockdown (T-26-10) and recorded an honest on-hardware launch observation (no telemetry/autoupdate fetch). The active, instrumented runtime-egress negative-control + llama-down proofs are scoped to Phase 27 (PRIV-06). Deferral declared in 26-03-PLAN.md; on-hardware caveat recorded honestly in 26-03-SUMMARY.md:96-98 (no fabricated pass). | gsd-security-auditor (--auto) | 2026-06-13 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-13 | 17 | 17 | 0 | gsd-security-auditor (`/gsd-secure-phase 26 --auto`) |

### Gates run (this audit)
- `go test ./internal/agent/` → 32 passed.
- `go test ./internal/inference -run TestSeamGrepGate` → passed (no backend-marker leak into `internal/agent` or `cmd/villa`).
- `go test ./cmd/villa -run 'TestNoAutoFlipStructuralGuard|TestCode'` → 7 passed.
- `go build ./...` → success.

### Note for downstream (not a blocker)
26-03 recorded a real D-12 defect found+fixed on-hardware: `agent.Run` originally
exec'd internally, swallowing the coding-off/first-run/LSP WARNs before they printed.
The shipped code reflects the fix — `Run` returns `Result.ReadyToLaunch`+`LaunchEnv`
(`agent.go:220-222`) and the single `d.Launch` is performed by `runCode` after printing
Warnings (`code.go:102-113`). This relocates the launch (T-26-09/10/12 surface) into
`code.go`, where it was verified above.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-13
