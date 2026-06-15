---
phase: 28-agent-surfacing-contracts
reviewed: 2026-06-15T00:00:00Z
depth: deep
files_reviewed: 13
files_reviewed_list:
  - cmd/villa/doctor.go
  - cmd/villa/status.go
  - cmd/villa/backup.go
  - cmd/villa/restore.go
  - internal/doctor/doctor.go
  - internal/status/status.go
  - internal/backup/manifest.go
  - internal/backup/backup.go
  - internal/backup/restore.go
  - internal/backup/deps.go
  - internal/metrics/llamacpp.go
  - internal/dashboard/assets/dashboard.js
  - internal/dashboard/assets/dashboard.html
findings:
  critical: 0
  warning: 4
  info: 3
  total: 7
status: issues_found
---

# Phase 28: Code Review Report

**Reviewed:** 2026-06-15
**Depth:** deep
**Files Reviewed:** 13
**Status:** issues_found

## Summary

Phase 28 surfaces the already-built coding agent through the existing status / doctor /
backup / restore / dashboard cores. The big invariants hold cleanly:

- **Single byte-frozen contract bump:** `status.Report.reportSchemaVersion` is bumped
  3→4 exactly once; `Coding *CodingInfo` (omitempty) is append-only above `SchemaVersion`.
  The doctor core keeps its OWN separate schema (v2) — no second status bump. Correct.
- **Seam lock holds:** no backend marker literal (`ROCm0`/`Vulkan0`/`HSA_OVERRIDE`/device
  paths/image tags) leaks into the changed code paths. The two `Vulkan0` hits are in a
  pre-existing cobra `Long` help string (status.go) and in explanatory comments
  (doctor.go) flagged as NON-markers. `inference.Verdict` is consumed opaquely.
- **Security posture intact:** no shell interpolation (fixed-arg `exec.CommandContext`
  with constant prompt + `agentBinPath()`, never PATH lookup); manifest/config parsing is
  fail-closed; dashboard agent values render via `textContent` (XSS-safe); no new outbound
  network (agent seams reuse the bounded loopback `/metrics` scrape); the agent binary is
  EXCLUDED from backup archives (identity-only `ExcludedAgent`), like model weights.
- Build is green (`go build ./...`).

The defects below are correctness / honesty-by-construction issues, not invariant breaches.
The most consequential is WR-01: the new derived agent-residency surface ignores the
memory reservation, so it can display a wrong "swap"/"shared" mode on a memory-enabled
host — a fabricated-by-omission value the project's honesty doctrine forbids.

## Warnings

### WR-01: Derived agent residency ignores the memory reservation → wrong swap/shared on a memory-on host

**File:** `cmd/villa/status.go:336-347`
**Issue:** `liveAgentResidency` recomputes the coder fit with
`recommend.Pick(profile, cat, recommend.Overrides{}, recommend.MemoryInputs{})` — i.e.
`MemoryInputs{}` with `Enabled=false`. In `recommend.Pick` an enabled memory stack first
reserves the embedding footprint and shrinks the envelope (`envelope -= reservation`)
BEFORE `pickCoder(c, envelope)` runs (internal/recommend/recommend.go:179-195). Every
OTHER live caller passes the real memory inputs
(`recommend.MemoryInputs{Enabled: cfg.MemoryEnabled, EmbeddingModel: cfg.EmbeddingModel}` —
see backend.go:395, dashboard.go:267, inference.go:147, recommend.go:234). Because this
new seam hard-codes memory-off, on a memory-enabled host the coder fit is computed against
the FULL (un-reserved) envelope, so the residency it surfaces in `villa status --json`
(`coding.residency`) and the dashboard Agent panel can be **optimistically wrong** (e.g.
"shared" when the post-reservation reality is "swap"). The whole point of this seam, per
its own doc comment and `CodingInfo.Residency`, is "NEVER fabricated" — a value computed
against the wrong envelope is exactly a fabricated state.
**Fix:**
```go
func liveAgentResidency() string {
    cat, _, err := catalog.Load(modelCatalogPath)
    if err != nil {
        return ""
    }
    profile := detect.Probe()
    if !profile.UsableEnvelopeBytes.Known {
        return ""
    }
    cfg, err := config.LoadVilla()
    if err != nil {
        return "" // typed-Unknown rather than a memory-blind guess
    }
    rec := recommend.Pick(profile, cat, recommend.Overrides{},
        recommend.MemoryInputs{Enabled: cfg.MemoryEnabled, EmbeddingModel: cfg.EmbeddingModel})
    return rec.Coder.Residency
}
```

### WR-02: `CrushConfigRestored` reports a restore that never happened (false-green)

**File:** `internal/backup/restore.go:421,466-478` and `cmd/villa/restore.go:166-167,261-267`
**Issue:** On the restore forward path the crush.json entry is written only when
`ex.crushPresent && in.CrushConfigDestPath != ""` (restore.go:421). But the returned
`Result.CrushConfigRestored` is set to `ex.crushPresent` unconditionally (restore.go:472)
— it does NOT account for the destination being unset. `CrushConfigDestPath` is wired ONLY
when the CURRENT install is agent-on (`cfg.AgentEnabled`, restore.go cmd:261). So when an
agent-ON archive is restored onto an agent-OFF current install, the archive's crush.json is
silently skipped (not written) yet `CrushConfigRestored` is `true`, and the cmd tier prints
`"coding agent: crush.json restored"` (restore.go:166-167). That is a false claim — the file
was present in the archive but NOT applied. Note the restored config.toml (with
`agent_enabled=true`) IS applied via SaveConfig, so the post-restore install believes the
agent is enabled while its crush.json was never restored — an inconsistent state reported as
success.
**Fix:** Set `CrushConfigRestored` to reflect the actual write, and report honestly when an
agent-on entry was present but skipped:
```go
crushWritten := ex.crushPresent && in.CrushConfigDestPath != ""
// ...
CrushConfigRestored: crushWritten,
```
Additionally surface a `CrushConfigSkipped` flag (entry present but no destination) so the
cmd tier can warn: "archive carried crush.json but the current install is agent-off — re-run
`villa install --coding-agent` then restore, or it will not be applied."

### WR-03: Agent residency-under-load sample is not guaranteed to be taken under load

**File:** `cmd/villa/doctor.go:627-648` (`runAgentResidencyUnderLoad`)
**Issue:** The memory residency-under-load proof goes to deliberate lengths to sample only
while a request is *verifiably in flight* (`residencySampleAfter` completions + an async
launch joined after sampling; documented as phase-22 WR-01). The new agent equivalent drops
that rigor: it launches the `crush run` probe once in a goroutine and takes the GTT/journal
sample immediately (doctor.go:632-646). If the probe exits fast — e.g. the binary errors,
exits non-zero, or completes a trivial round-trip before the sample reads `sd.GTTUsed()` /
`sd.Props()` — the residency is sampled with the coder **idle**, not under load. An idle
sample can mask a CPU-fallback-under-load (the exact silent degradation this seam exists to
catch), turning a real FAIL into a PASS — a false-green. The probe's own error is
intentionally discarded (`_, _ = liveAgentToolCallProbe(ctx)()`), so a fast-failing probe
leaves no signal that the "under load" precondition was never met.
**Fix:** Mirror the memory proof's in-flight discipline: drive multiple sequential tool-call
rounds, sample only after at least one completes AND while the next is launched-and-joined,
and degrade to a typed-Unknown WARN (`agentUnevaluable`) when the drive could not keep a
request in flight long enough to sample (rather than returning the idle-sampled verdict).

### WR-04: Cache-effectiveness percentage is unbounded (can exceed 100%)

**File:** `internal/status/status.go:709-713`
**Issue:** `pct := (float64(cacheN) / float64(promptN)) * 100.0` is gated on `promptN > 0`
(no div-by-zero), but `cacheN` is not bounded by `promptN`. The two counters come from
distinct llama.cpp `_total` series scraped independently (metrics.go:289-290), so a counter
skew (reset of one but not the other, or a build where `cache_n` semantics differ) can yield
`pct > 100`. The dashboard renders it verbatim (`ag.cache_effectiveness_pct.toFixed(1) + "%"`,
dashboard.js:431-433) with no clamp — unlike the GPU memory bar which clamps with
`Math.max(0, Math.min(100, …))` (dashboard.js:563). A ">100% cache effectiveness" reading is
a fabricated-looking value that undermines the honest-surface contract.
**Fix:** Either clamp at the core (`if cacheN > promptN { … typed-Unknown / cap }`) or treat
`cacheN > promptN` as an inconsistent sample and degrade to the gray Unknown badge (leave
`CacheEffectivenessPct` nil), so the surface never shows an impossible ratio.

## Info

### IN-01: Coding-agent tool-call probe runs twice per `villa doctor`

**File:** `cmd/villa/doctor.go:250-252`, consumed at `internal/doctor/doctor.go:310-315`
**Issue:** When the agent is enabled, `AgentToolCall` and `AgentResidencyUnderLoad` are bound
to separate closures that each independently execute `liveAgentToolCallProbe` (a full
read→edit `crush run` round-trip). `doctor.Aggregate` invokes both sequentially, so a single
`villa doctor` drives the coder model through two complete tool-call round-trips back-to-back
(each under its own 90s `agentProofBudget`). This is correctness-neutral but doubles the
doctor latency and load for the agent path. Performance is out of v1 review scope; noted only
because the duplication is non-obvious and a single drive could feed both findings.
**Fix:** Drive one tool-call round-trip and sample residency mid-drive (the WR-03 fix would
naturally collapse these into one drive), reusing the single completion for the tool-call
finding too.

### IN-02: Restore agent-config gate keys on the CURRENT install rather than the restored config

**File:** `cmd/villa/restore.go:261`
**Issue:** Whether crush.json is restored is decided by the CURRENT install's
`cfg.AgentEnabled`, read before the archive's config is applied. Conceptually the restored
config (from the archive) is the post-restore source of truth, so gating the crush.json
destination on the pre-restore state is the root cause of WR-02. Even after fixing the false
"restored" message, consider deriving the destination from the archive's parsed config
(`restoredCfg.AgentEnabled`) so an agent-on backup restores its agent config regardless of the
current install's posture.
**Fix:** Resolve `in.CrushConfigDestPath` unconditionally (the path is the fixed XDG crush
path, traversal-guarded at write) and let the core's `ex.crushPresent` gate decide; or wire it
from the restored config rather than the current `cfg`.

### IN-03: `agentProbeReplaced` round-trip can be spoofed by a transcript echoing TOKEN_B

**File:** `cmd/villa/install_agent.go:308-310` (reused by the doctor agent seams)
**Issue:** The success predicate is `Contains(TOKEN_B) && !Contains(TOKEN_A)` over the probe
file's post-run content. The comment already notes presence-only was tightened to a real
replace, which is good. A residual edge remains: a coder model that overwrites the probe file
with arbitrary content containing TOKEN_B but not TOKEN_A (e.g. a "done" confirmation written
INTO the file) would false-green the round-trip even though it did not perform the intended
replace. This is a pre-existing Phase-27 helper now load-bearing for the Phase-28 doctor
tool-call FAIL-dominates path, so worth a hardening note.
**Fix:** Make the probe deterministic — assert the file equals the expected post-replace
content exactly (`strings.TrimSpace(content) == TOKEN_B`) rather than substring membership.

---

_Reviewed: 2026-06-15_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
