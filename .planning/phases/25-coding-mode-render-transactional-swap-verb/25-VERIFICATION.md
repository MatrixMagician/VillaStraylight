---
phase: 25-coding-mode-render-transactional-swap-verb
verified: 2026-06-13T00:00:00Z
status: passed
score: 9/9 must-haves verified
overrides_applied: 0
---

# Phase 25: Coding-Mode Render & Transactional Swap Verb Verification Report

**Phase Goal:** User can flip the running stack into a tool-calling-ready coding mode and back via an explicit transactional verb — chat model restored on exit, residency proven under load, addon-off renders byte-identical to v1.3.
**Verified:** 2026-06-13
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

Merged from ROADMAP Success Criteria (SC1-3) + PLAN frontmatter must_haves (CMODE-01 + CMODE-02).

| #   | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | (SC1/CMODE-01) Addon-OFF render + config-on-disk byte-identical to v1.3 | ✓ VERIFIED | `git diff --stat v1.3 -- internal/orchestrate/testdata/` shows ONLY `villa-llama-coding.container.golden` added (21 insertions); off-path goldens unmodified. `git diff v1.3 -- villa-llama.container.golden` is empty. Config: `marshalVilla` zeroes `CoderModel/Quant/AgentCtx` when `!CodingMode` (villaconfig.go:270-274); `TestCodingModeSaveOmitsKeysWhenDisabled` green. |
| 2 | (SC1/CMODE-01) Addon-ON renders `--jinja` + single `-c <agent_ctx>` + sampling + `--cache-reuse` only when cache_reuse_safe | ✓ VERIFIED | New golden Exec line: `... -c 65536 ... --jinja --temp 0.7 --top-p 0.8 --top-k 20 --repeat-penalty 1.05 --cache-reuse 256` — single `-c` at agent ctx. `appendCodingModeArgs` gates `--cache-reuse` on `if cm.CacheReuseSafe` (fail-closed). Render sets `spec.ContextLen = in.CoderAgentCtx` (render.go:101-104). `go test ./internal/orchestrate -run TestRender`, `TestCacheReuseGate` green. |
| 3 | (CMODE-01) Flag literals live ONLY in internal/inference; TestSeamGrepGate enforces in BOTH maps | ✓ VERIFIED | `grep --jinja\|--cache-reuse\|--repeat-penalty` finds them ONLY in `backend_vulkan.go` (shared `appendCodingModeArgs`; ROCm calls same helper, backend_rocm.go:109). `codingModeFlagPattern()` present in both `patterns` (seam_test.go:63) and `cmdPatterns` (seam_test.go:177). `TestSeamGrepGate` green. |
| 4 | (CMODE-02/SC2) Enter: capture → cutover → under-load prove → verbatim rollback on failure | ✓ VERIFIED | `codingmode.Run`: capture strictly before mutate; `v := d.Prove(...)`; `if v.Status != ProveStatusPass { return rolledBack(...) }` (codingmode.go:365-368). `TestEnter`, `TestProveFailRollback` (asserts `bytes.Equal(rec.restored, priorUnitBytes)`), `TestMutateRollback` green. Live Prove = PollHealth + GenerationProbe + RunningOffloadVerdict (coding-mode.go:89,101,139). |
| 5 | (CMODE-02/SC2/D-09) Silent CPU fallback / ready-but-residency-FAIL → rollback (idle-green never green) | ✓ VERIFIED | Cutover gates ONLY on `ProveStatusPass`; any other verdict rolls back verbatim (codingmode.go:366). Own `ProveStatusPass` const (codingmode.go:53) — core imports neither inference nor detect. On-hardware: idle `villa status` OFFLOAD WARN (typed-Unknown, F-3 documented), authoritative gate = cutover Prove PASS under load (25-02-SUMMARY). |
| 6 | (CMODE-02/SC3/D-08) Exit: chat model restored under same transactional discipline | ✓ VERIFIED | Exit runs identical capture→mutate→prove→rollback frame; durable-chat-model invariant (cfg.Model never overwritten; coder served from cfg.CoderModel; exit clears coder fields → unit reverts). `TestExitRestoresChat` green. On-hardware drill 4: chat restored, Exec line byte-identical v1.3, config coding keys dropped. |
| 7 | (CMODE-02/SC3/D-06) Mode never auto-flips — explicit verb only | ✓ VERIFIED | `TestNoAutoFlipStructuralGuard` walks cmd/villa + internal/, asserts `CodingMode = true\|false` mutated nowhere outside `codingmode.Run`. Verb registered in root.go:36 (`newCodingMode()`). Binary `villa coding-mode --help` shows enter/exit subcommands. Green. |
| 8 | (CMODE-02) Same-state enter/exit is a clean NoOp, zero side effects | ✓ VERIFIED | `TestNoOpEnterAlreadyCoding`, `TestNoOpExitAlreadyChat` green. On-hardware drills 3 + 4a: "already in coding/chat mode — no change", exit 0. |
| 9 | (CMODE-02) codingmode core is pure + Deps-injected, imports NEITHER inference NOR detect | ✓ VERIFIED | `go list -deps ./internal/codingmode` shows only `internal/config` among first-party deps. Import block imports `context` + `internal/config` only. Defines own `ProveStatusPass`/`ProveVerdict`. |

**Score:** 9/9 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/inference/inference.go` | RunSpec.CodingMode + CodingModeSpec | ✓ VERIFIED | Optional `*CodingModeSpec` field; nil = byte-identical off path. |
| `internal/inference/backend_vulkan.go` | ContainerArgs coding delta (`--jinja`) | ✓ VERIFIED | `appendCodingModeArgs` shared helper; flag literals live here only. |
| `internal/inference/backend_rocm.go` | Symmetric ROCm delta | ✓ VERIFIED | Calls same `appendCodingModeArgs(args, spec.CodingMode)` (line 109). |
| `internal/config/villaconfig.go` | coding fields omitempty + omit-when-off | ✓ VERIFIED | `coding_mode,omitempty` + coder_* dropped when off (line 270-274); `coder_agent_ctx,omitzero` (TOML int gotcha fix). |
| `internal/orchestrate/render.go` | D-05 config→descriptor single point | ✓ VERIFIED | Sets spec.CodingMode + ContextLen=CoderAgentCtx only when non-nil (render.go:101-104). |
| `internal/orchestrate/testdata/villa-llama-coding.container.golden` | NEW append-only golden | ✓ VERIFIED | Exists; full delta; only added vs v1.3 (no existing golden mutated). |
| `internal/codingmode/codingmode.go` | Pure transactional core | ✓ VERIFIED | Run/Deps/Result/ProveVerdict/ProveStatusPass; literal-free; imports only config. |
| `cmd/villa/coding-mode.go` | enter\|exit noun + liveCodingModeDeps | ✓ VERIFIED | All seams wired; liveCodingProve twin (ConfigContext=AgentCtx); marker-free. |
| `cmd/villa/root.go` | registers newCodingMode() | ✓ VERIFIED | root.go:36. |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| render.go | inference RunSpec.CodingMode | descriptor pass + ContextLen override | ✓ WIRED | render.go:101-104. |
| backend ContainerArgs | rendered Exec line | appendCodingModeArgs delta | ✓ WIRED | Golden Exec confirms flags flow through. |
| liveCodingModeDeps | codingmode.Run | wires all host seams | ✓ WIRED | LoadConfig/SaveConfig/CaptureUnit/ReconcileAndWrite/RestoreUnit/Restart/Prove/ResolveCoder/Pull. |
| Prove seam | inference.RunningOffloadVerdict under load | liveCodingProve, ConfigContext=AgentCtx | ✓ WIRED | coding-mode.go:139,167; PollHealth+GenerationProbe+RunningOffloadVerdict. |
| codingmode.Run enter | modelswap forward ordering | recommend.Pick().Coder + Pull composition | ✓ WIRED | ResolveCoder backed by recommend.Pick (coding-mode.go:199); Pull=download. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Binary builds | `go build -o /tmp/villa-verify ./cmd/villa` | exit 0 | ✓ PASS |
| Verb registered with enter/exit | `villa coding-mode --help` | shows enter + exit subcommands, transactional description | ✓ PASS |
| go vet clean | `go vet ./...` | exit 0 | ✓ PASS |
| Full suite green | `go test ./...` | all packages ok | ✓ PASS |
| Seam gate | `go test ./internal/inference -run TestSeamGrepGate` | PASS | ✓ PASS |
| Core transactional tests | `go test ./internal/codingmode -run 'TestEnter\|...'` | 9 PASS | ✓ PASS |
| Structural guard + cmd tests | `go test ./cmd/villa -run 'TestNoAutoFlip\|TestCodingMode'` | 8 PASS | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| CMODE-01 | 25-01 | Coding-mode render delta behind seams; addon-off byte-identical to v1.3 | ✓ SATISFIED | Truths 1-3; REQUIREMENTS.md:24 `[x] Complete`. |
| CMODE-02 | 25-02 | Transactional enter/exit verb composing modelswap; chat restored on exit | ✓ SATISFIED | Truths 4-9; REQUIREMENTS.md:25 `[x] Complete`. |

No orphaned requirements: REQUIREMENTS.md maps only CMODE-01/CMODE-02 to Phase 25, both claimed by plans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | None (no TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER in any phase-25 file) | — | — |

### Human Verification Required

None. The single Manual-Only verification (on-hardware enter→prove→exit, CMODE-02) was already executed on the gfx1151 box and PASSED (25-02-SUMMARY "On-Hardware Acceptance (Task 3) — PASSED"): enter swap with residency proven 4 ways (cutover Prove + journal 49/49 layers + GTT 52GiB + real generation `RESIDENCY_OK`), symmetric exit (chat restored, byte-identical), NoOp drills, and the rollback drill (forced prove-FAIL → verbatim restore, exit 1). The idle-state OFFLOAD WARN is the documented F-3 typed-Unknown idle-scrape behavior, not a residency failure — authoritative gate is the cutover Prove which passed under load.

### Gaps Summary

No gaps. All 9 must-haves verified against the codebase at every level (exists, substantive, wired, data-flows). The strongest off-path evidence — `git diff --stat v1.3 -- testdata/` showing ONLY the new coding golden added — directly proves CMODE-01's byte-identity contract. The transactional discipline (capture→cutover→prove→rollback, gated solely on ProveStatusPass) is enforced by 14 Deps-driven tests plus the on-hardware acceptance. Marker discipline (codingmode imports neither inference nor detect; flag literals only in the inference seam; TestSeamGrepGate covering both internal/ and cmd/villa walks) is intact and CI-enforced. Two SUMMARY-documented deviations (`omitzero` for the zero-int config field; quoted-literal anchoring of the seam-gate regex) are correct fixes for the plan's own load-bearing invariants, both backed by passing tests.

---

_Verified: 2026-06-13_
_Verifier: Claude (gsd-verifier)_
