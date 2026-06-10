---
phase: 22-control-plane-fit-host-gate
reviewed: 2026-06-10T17:07:37Z
depth: deep
files_reviewed: 25
files_reviewed_list:
  - cmd/villa/backend.go
  - cmd/villa/dashboard.go
  - cmd/villa/doctor.go
  - cmd/villa/doctor_test.go
  - cmd/villa/inference.go
  - cmd/villa/install.go
  - cmd/villa/install_test.go
  - cmd/villa/model.go
  - cmd/villa/preflight.go
  - cmd/villa/preflight_test.go
  - cmd/villa/recommend.go
  - cmd/villa/recommend_test.go
  - cmd/villa/status.go
  - cmd/villa/testdata/doctor-memory.json.golden
  - cmd/villa/testdata/doctor-memory-pass.golden
  - cmd/villa/testdata/doctor-memory-residency-fail.golden
  - cmd/villa/testdata/recommend.golden.json
  - internal/doctor/doctor.go
  - internal/doctor/doctor_test.go
  - internal/memory/footprint.go
  - internal/memory/footprint_test.go
  - internal/preflight/checks_memory.go
  - internal/preflight/checks_memory_test.go
  - internal/recommend/recommend.go
  - internal/recommend/recommend_test.go
findings:
  critical: 1
  warning: 7
  info: 7
  total: 15
status: issues_found
---

# Phase 22: Code Review Report

**Reviewed:** 2026-06-10T17:07:37Z
**Depth:** deep
**Files Reviewed:** 25
**Status:** issues_found

## Summary

Phase 22 (envelope reservation before chat-model fit, MEM-PRE host gates, doctor memory fold + MEM-DOC-residency under-load proof) is structurally sound on its headline invariants: the schema 1→2 bump is append-only with `recommend.golden.json` as the sole re-frozen golden (`status.json.golden` untouched, guarded by `TestPickOverrideWeightInvariance`); `checks_memory.go` contains no `exec.Command` (only the package `runTool` seam); the down-rank/supersession predicates are correctly conjunctive (`Status==WARN` AND ID/service match), so a confident FAIL is never suppressed — verified in both `doctor.Aggregate` and its tests; the reservation clamps at 0 with no uint64 wraparound; doctor never writes units (`unitDirReadOnly`, stat-only `DriftPlan`).

Deep cross-module tracing did surface one Critical defect: `doctor.Aggregate` folds an **errored** `status.Report` (zero-value `LoopbackOnly=false`) into a confident BLOCK-class "privacy breach" FAIL — reachable on any host where the status read-model errors (e.g. fresh host, `cfg.Model` unresolvable), fabricating a confident FAIL from an unevaluable signal in direct violation of the typed-Unknown honesty rule. Seven warnings follow, mostly around timing/semantics of the new under-load proof, the unbounded `podman` probe, and a dry-run contract breach in the install gate.

## Critical Issues

### CR-01: doctor fabricates a confident "loopback privacy breach" FAIL when the status read-model errors

**File:** `internal/doctor/doctor.go:236-247` (fold site); wired at `cmd/villa/doctor.go:222`
**Issue:** `status.Run` returns an errored `Report` (with `err` set via `Report.Err()`, **no** Services, and zero-value `LoopbackOnly=false`) on any internal failure — config load, `ModelFile` resolution, `BackendFor`, or `Render` (`internal/status/status.go:286-310`). `doctor.Aggregate` never consults `report.Err()`; it tests `!report.LoopbackOnly` and emits a BLOCK-tier FAIL: *"a published port binds a non-loopback address (privacy breach, PRIV-01)"* → `Overall=FAIL` → exit 1.

This is reachable in a perfectly ordinary state: on a never-installed host (or any host whose `cfg.Model` is absent from the catalog), `liveModelFile` errors (`cmd/villa/lifecycle.go:210-213`), `status.Run` returns the errored zero report, and `villa doctor` confidently reports a **privacy breach** with a "re-run `villa install`" remediation. `villa status` handles this same condition honestly (`runStatus` checks `report.Err()` and prints the real error, `cmd/villa/status.go:72-75`); doctor converts the identical unevaluable signal into a fabricated confident FAIL — the exact failure mode the project's typed-Unknown discipline forbids ("never a FAIL fabricated from a signal that could not be evaluated").
**Fix:**
```go
// in Aggregate, before folding LoopbackOnly/Services:
report := d.StatusReport()
if err := report.Err(); err != nil {
    findings = append(findings, Finding{
        ID:          "stack",
        Name:        "Running-stack read-model",
        Tier:        tierWarn,
        Status:      statusWarn,
        Detail:      "the running-stack state could not be evaluated",
        Remediation: "fix the reported condition, then re-run `villa doctor`: " + err.Error(),
        Provenance:  "status.Run error",
        Raw:         err.Error(),
    })
} else {
    if !report.LoopbackOnly { /* existing loopback FAIL */ }
    for _, s := range report.Services { /* existing health/offload fold */ }
}
```
Add a core test: a stubbed `StatusReport` returning an errored zero report must yield WARN (not the loopback FAIL).

## Warnings

### WR-01: MEM-DOC-residency "sample DURING load" invariant is timing-dependent, not enforced

**File:** `cmd/villa/doctor.go:399-442` (`runResidencyUnderLoad` drive/sample interleave)
**Issue:** The proof claims (step 3 comment) that "the sample below therefore reads the journal/GTT with an embed request in flight." Two gaps: (a) `completions` is buffered to full capacity, so the producer never blocks — if the consumer stalls, all 12 requests can complete before the sample at `completed >= residencySampleAfter` runs, sampling *after* load ended; (b) more concretely, each drive request is a fresh `podman run --rm` container with ~1s+ startup latency, and the sample fires the instant completion #2 is received — i.e. in the idle gap *between* request 2 finishing and request 3's curl actually reaching the embedder. The GTT/journal read therefore lands at best at the margin of load, weakening Pitfall 6's "must be DURING load" guarantee. A chat model evicted only while a request is actively processing could still sample PASS.
**Fix:** Gate the sample on demonstrated in-flight work rather than completion count — e.g. have the drive goroutine publish "request started" events (or maintain an atomic in-flight flag) and sample only when `completed >= residencySampleAfter && inFlight`, or take the sample from inside the drive loop between issuing request N and awaiting its result.

### WR-02: `liveVolumeRoot` shells to `podman system info` through a `runTool` with no timeout — a wedged podman hangs preflight/install/doctor

**File:** `internal/preflight/checks_memory.go:42-52`; root cause `internal/preflight/exec.go:19-27`
**Issue:** `runTool` uses `exec.Command(...).Output()` with no context/timeout. Phase 22 adds the first live `podman` invocation through this seam; a hung rootless podman (stale user socket — a state this product can itself produce) blocks `villa preflight`, `villa install`, and `villa doctor` indefinitely whenever `memory_enabled=true`. The file's comment sells the seam as "bounded ... stdout capped at maxToolOutput", but the bound is output-size only — and even that is cosmetic, since `cmd.Output()` has already buffered the entire output in memory before the `io.LimitReader` truncates a string copy. Note also `TestLiveMemoryGateOffPath`'s memory-on subtest (`cmd/villa/preflight_test.go:242-256`) executes the real `podman` on the test host through this unbounded path, so a wedged podman also hangs `go test`.
**Fix:**
```go
ctx, cancel := context.WithTimeout(context.Background(), toolTimeout) // e.g. 10s
defer cancel()
cmd := exec.CommandContext(ctx, name, args...)
```
in `runTool` (or a bounded variant used by `liveVolumeRoot`), and cap the read at the pipe level (`cmd.StdoutPipe()` + `io.LimitReader`) if the memory bound is meant to be real.

### WR-03: MEM-PRE-headroom double-counts the reservation when doctor reuses it against a RUNNING stack — false blocking fault on a healthy host

**File:** `internal/preflight/checks_memory.go:115-148` (check semantics); doctor binding `cmd/villa/doctor.go:214-216`
**Issue:** `checkEmbedHeadroom` requires `MemAvailable >= footprint` — correct as a *pre-install* gate ("is there room to start the embedder?"). Doctor (D-08 composition) runs the identical check while villa-embed is already resident, so the embedder's own consumption has already been subtracted from `MemAvailable` and the check effectively demands a *second* 512 MiB on top of the running one. On a memory-tight host a perfectly healthy running stack yields a confident BLOCK FAIL ("free memory < embedding reservation") → `villa doctor` exit 1 — a false blocking fault, the inverse of the no-false-green rule, and a break of the DOCTOR-01 "exit 0 = healthy" contract. (Unlikely on the 128 GB target box, but the gate is generic.)
**Fix:** Give `MemoryGateInput` a running-context mode (or a separate doctor-facing wrapper) that either (a) downgrades a headroom shortage to WARN with "the embedder is already running — low system headroom" detail, or (b) skips the reservation term when the embed service is active and instead checks an absolute low-memory floor.

### WR-04: `renderDoctor` verdict→exit mapping fails open on an unrecognized Overall

**File:** `cmd/villa/doctor.go:100-114`
**Issue:** `switch r.Overall { case "FAIL": ...; case "WARN": ...; default: return exitPass }` — any malformed/empty `Overall` (a future Aggregate bug, a hand-built Report, a JSON-roundtripped fixture) silently maps to exit 0 (healthy). The sibling mapping in `renderInference` (`cmd/villa/inference.go:207-214`) deliberately fails closed (`default: exitBlocked`). For a health-verdict command, defaulting to "healthy" is the wrong defensive direction.
**Fix:**
```go
switch r.Overall {
case "FAIL": ... return exitBlocked
case "WARN": return exitWarn
case "PASS": return exitPass
default:     return exitBlocked // unknown verdict is never healthy
}
```

### WR-05: `villa install --dry-run` can execute privileged host-prep — dry-run zero-side-effect contract breach

**File:** `cmd/villa/install.go:302-344` (wizard + `gateInstall` precede the dry-run return at line 398)
**Issue:** The command help and the step-5 comment promise "--dry-run ... writes nothing (no pull, no config write)" / "a dry run has zero side effects (ORCH SC#1)". But `useWizard` (line 302) ignores `opts.dryRun`, and `gateInstall` → `resolveGap`/`offerNonBlockingGap` run *before* the dry-run early return: on an interactive TTY with a BLOCK/WARN gap, `--dry-run` launches the wizard, prompts "Run \`setsebool -P ...\` now?", and on consent **executes** `d.setsebool()`/`d.enableLinger()` — persistent privileged host mutation under a flag sold as side-effect-free. `TestInstallDryRunWritesNothing` doesn't cover this (it uses pass checks + non-interactive deps), so the gap is untested.
**Fix:** Thread `opts.dryRun` into the gate: in `useWizard` add `&& !opts.dryRun`, and in `resolveGap`/`offerNonBlockingGap` treat `opts.dryRun` like the non-interactive branch (print the command, never prompt, never run the seam). Add a test: dry-run + interactive + consenting stub must record zero `seboolCalls`/`lingerCalls`.

### WR-06: `runResidencyUnderLoad` passes the model *id* as `ConfigModel` where every sibling caller passes the resolved GGUF filename — and omits Props entirely despite claiming the "EXACT liveStatusDeps input set"

**File:** `cmd/villa/doctor.go:430-439`
**Issue:** The doc comment (step 3) claims the sample evaluates `RunningOffloadVerdict` over "the EXACT liveStatusDeps input set." It does not: (a) `ConfigModel: cfg.Model` is the catalog id, while `liveProve` (`cmd/villa/backend.go:83,164`) and the status core pass the catalog-resolved GGUF *filename* (`liveModelFile`); (b) no `Props` is passed at all, so the `/props` config-identity drift overlay (`internal/inference/running_offload.go:365-371`) is silently disabled for this proof. Today (a) is dead input *because of* (b) — `propsDrift` returns "" on nil props — but the moment someone wires Props in (to actually match liveStatusDeps, as the comment claims), the id-vs-filename mismatch makes `sameModelPath` fail and every under-load PASS permanently degrades to a spurious drift WARN.
**Fix:** Either resolve `ConfigModel` via `liveModelFile(cfg)` (mirroring `liveProve`) and wire `Props: liveProps(endpoint)` to honor the stated contract, or pass empty `ConfigModel`/zero `ConfigContext` and amend the comment to document that the drift overlay is intentionally out of scope for the under-load sample.

### WR-07: absurd `--ctx` override can wrap `kvCacheBytes` and defeat the D-07 fit re-validation

**File:** `internal/recommend/recommend.go:356-366` (`pickOverride`/`effectiveCtx`); math at `internal/recommend/kv.go:15-25`
**Issue:** `effectiveCtx` accepts any positive `ov.Ctx` (an `int`, 64-bit here). `kvCacheBytes` multiplies five uint64 terms with no overflow check; for the test catalog's "mid" model the multiplier is ~196,608, so `--ctx` values around 9.4e13+ wrap mod 2^64 and can produce a *small* `total` → `Fits=true`. The override path's whole promise is "re-validated against the envelope ... never a silent OOM" — overflow silently defeats that guard and the fit verdict feeds `inference.go`'s ceiling stress math and the rendered unit's `-c`. Self-inflicted and exotic, but it is precisely the guard this code documents.
**Fix:** Clamp/validate the override before sizing, e.g. in `effectiveCtx`/`pickOverride`: reject (note + `Fits=false`) any `ov.Ctx` above a sane ceiling (e.g. 16M tokens), or compute the product with `bits.Mul64` and treat a non-zero high word as not-fitting.

## Info

### IN-01: residency-FAIL render bakes a duplicated phrase into the frozen golden

**File:** `cmd/villa/testdata/doctor-memory-residency-fail.golden:12`; join at `cmd/villa/doctor.go:125-128` + default remediation `internal/doctor/doctor.go:487`
**Issue:** Detail and fallback remediation both contain "the chat model fell back to CPU under embedding load", so the rendered row reads "...fell back to CPU under embedding load — the chat model fell back to CPU under embedding load — check...". The golden now freezes the stutter.
**Fix:** Trim the default remediation to the action only ("check the backend (`villa backend set`) and `villa logs`") and re-freeze the one golden.

### IN-02: embed-drive URL composed with Sprintf, not `net.JoinHostPort`

**File:** `cmd/villa/doctor.go:390`
**Issue:** `fmt.Sprintf("http://%s:%d/v1/embeddings", cfg.EmbedAddr, cfg.EmbedPort)` breaks for an IPv6 `embed_addr` (degrades to a WARN, so honest, but avoidably). `cfg.EmbedAddr` is hand-edited config used directly as the probe target.
**Fix:** `"http://" + net.JoinHostPort(cfg.EmbedAddr, strconv.Itoa(cfg.EmbedPort)) + "/v1/embeddings"` (and the same in `liveMemoryProof`, which this mirrors).

### IN-03: `liveProve` silently discards the ResidencyJournal error

**File:** `cmd/villa/backend.go:158`
**Issue:** `journal, _ := orchestrate.NewSystemd().ResidencyJournal(...)` — a journalctl failure degrades correctly to a typed-Unknown verdict, but the cause never reaches the prove-fail Detail, making a journald outage indistinguishable from a genuinely empty journal during rollback triage.
**Fix:** Capture the error and append it to the verdict detail or a `Raw` field when the prove fails.

### IN-04: degraded-floor note mislabels the post-reservation envelope as "the %-of-RAM floor"

**File:** `internal/recommend/recommend.go:170-180`
**Issue:** The reservation is subtracted *before* the degraded note is formatted, so with memory on the note prints `floor − reservation` while describing it as "a conservative 50%-of-RAM floor (N GiB)". Display-only inaccuracy.
**Fix:** Format the note from the pre-subtraction floor, or reword to "floor minus the embedding reservation".

### IN-05: `runModelSwap` pre-announce runs a full probe+pick purely for a cosmetic line

**File:** `cmd/villa/model.go:267-271`
**Issue:** The "pulling..." pre-announce calls `d.ResolveCatalog` + `d.IsDownloaded` + `d.Fits` (a full `detect.Probe()` + catalog load + `recommend.Pick`), all of which `modelswap.Run` immediately re-executes — doubled host probing per swap for one progress line.
**Fix:** Move the announce into `modelswap.Run` via an optional progress callback, or accept and document the duplication.

### IN-06: dead `!opts.dryRun` condition in the embed pre-stage gate

**File:** `cmd/villa/install.go:431`
**Issue:** `if cfg.MemoryEnabled && !opts.dryRun && ...` — the dry-run path returned unconditionally at step (5) (line 398-408), so `!opts.dryRun` is always true here. Harmless but misleading (suggests dry-run can reach this point).
**Fix:** Drop the condition or replace it with a comment referencing the step-5 early return.

### IN-07: persisted memory-inputs load logic duplicated between `newRecommend` RunE and `liveLoadedMemoryInputs`

**File:** `cmd/villa/recommend.go:50-57` vs `cmd/villa/recommend.go:207-213`
**Issue:** The RunE re-implements the fail-soft `MemoryInputs` derivation inline (to share the catalog-path read) while `liveLoadedMemoryInputs` exists for exactly this contract and is used by install/model/dashboard. Two sites to keep in sync if the fail-soft rule ever changes.
**Fix:** Have the RunE call `liveLoadedMemoryInputs()` (one extra config load) or extract a shared helper that returns `(catalogPath, MemoryInputs)`.

---

_Reviewed: 2026-06-10T17:07:37Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
