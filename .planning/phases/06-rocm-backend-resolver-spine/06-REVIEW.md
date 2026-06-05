---
phase: 06-rocm-backend-resolver-spine
reviewed: 2026-06-06T00:00:00Z
depth: deep
files_reviewed: 19
files_reviewed_list:
  - cmd/villa/dashboard.go
  - cmd/villa/inference.go
  - cmd/villa/install.go
  - cmd/villa/lifecycle.go
  - cmd/villa/model.go
  - cmd/villa/status.go
  - internal/inference/backend.go
  - internal/inference/backend_rocm.go
  - internal/inference/backend_rocm_test.go
  - internal/inference/backend_vulkan.go
  - internal/inference/inference.go
  - internal/inference/offload.go
  - internal/inference/offload_test.go
  - internal/inference/running_offload.go
  - internal/inference/running_offload_test.go
  - internal/inference/seam_test.go
  - internal/inference/validate.go
  - internal/inference/validate_test.go
  - internal/status/status.go
findings:
  critical: 1
  warning: 4
  info: 3
  total: 8
status: issues_found
---

# Phase 6: Code Review Report

**Reviewed:** 2026-06-06
**Depth:** deep (cross-file: Backend seam, BackendFor resolver wiring across the 8 re-routed call sites, parameterized offload/running_offload scrapers, gpu_busy fold, ROCm container args)
**Files Reviewed:** 19
**Status:** issues_found

## Summary

The phase is well-executed against its stated invariants. I independently verified the load-bearing claims:

- **Build + tests + vet pass** (`go build ./...`, 217 tests across the three changed packages, `go vet` clean).
- **Fail-closed resolver:** `BackendFor` returns `(nil, error)` on an unknown value and is correctly wired through all live deps sites (inference.go, status.go, install.go, lifecycle.go, model.go, dashboard.go reuses status.go) — every site treats the error as a FAIL/block, never a silent Vulkan default.
- **Byte-identical-Vulkan invariant holds:** the running-offload provenance string is reproduced byte-for-byte (`"journald load_tensors Vulkan0 residency + point-in-time mem_info_gtt_used floor"`) and the `status.json.golden` test stays green; the busy fold is skipped for Vulkan (busy always Unknown), so no Vulkan verdict moves.
- **Digest pins are real** 64-hex `@sha256:` pins (Vulkan + ROCm), and the seam grep gate keeps the ROCm image/device literals inside `backend_rocm.go`.
- **Catalog-resolution / no-shell-interpolation** discipline preserved for model args; loopback-only host publish preserved on both backends.

The one BLOCKER is a serialization-contract corruption in the gpu_busy re-fold path (ROCm only), which I reproduced. It does not currently flip a status incorrectly, and no live caller populates the busy signal until Phase 8, but the logic is shipped, tested as status-correct, and silently mangles the `--json` `sysfs_offload` / `gtt_delta_bytes` fields plus the human `Detail` for the exact path this phase exists to wire.

## Critical Issues

### CR-01: gpu_busy re-fold overwrites the GTT-floor sysfs signal, zeroes `gtt_delta_bytes`, and nests the detail string (ROCm/busy path)

**File:** `internal/inference/running_offload.go:319-324` (the re-fold), `internal/inference/running_offload.go:284-292` (`verdictAsResult`), `internal/inference/offload.go:298-321` (`combineOffload`)

**Issue:** When `GPUBusyPercent.Known` is true, `RunningOffloadVerdict` re-combines the already-combined verdict with the busy signal:

```go
v = combineOffload(verdictAsResult(v), gpuBusyFloor(in.GPUBusyPercent))
```

`combineOffload` unconditionally sets `v.SysfsOffload = sysfs.Signal` and `v.GTTDeltaBytes = sysfs.DeltaBytes` from its *second* argument. On the re-fold the second argument is the **busy** result, so:

- `SysfsOffload` in the `--json` contract is overwritten with the busy signal (`Source="gpu_busy_percent"`) — the real point-in-time GTT-floor signal is **lost** from the serialized contract.
- `GTTDeltaBytes` is overwritten with `gpuBusyFloor`'s `DeltaBytes`, which is always `0` — the recorded GTT-used floor value (documented as the A1 calibration record) is **dropped to 0**.
- `Detail` is rebuilt by `joinDetail` over `verdictAsResult(v)` (whose `Detail` is *already* the combined "offload proven (log + sysfs) — log: ...; sysfs: ..." string), producing a duplicated, nested detail.

Reproduced with a Known busy=42 reading on a ROCm residency-PASS journal:

```
SysfsOffload.Source = "gpu_busy_percent"   (want the GTT-floor source)
GTTDeltaBytes       = 0                      (want ~23068672000, the GTT used floor)
Detail              = "offload proven (log + sysfs) — log: offload proven (log + sysfs) — log: ROCm0 model buffer ... ; sysfs: ... ; sysfs: gpu_busy_percent 42% ..."
```

Vulkan is unaffected (busy always Unknown → fold skipped → byte-identical golden preserved), which is why the existing tests do not catch it: `TestRunningServerBusySignalFold` / `TestRunningServerROCmBusySignal` assert only `v.Status`, never the `SysfsOffload`/`GTTDeltaBytes`/`Detail` fields the re-fold corrupts. Status precedence is still correct in the cases tested, so this is silent contract rot, not a wrong verdict — but it ships now and surfaces the moment Phase 8 wires the live busy read.

**Fix:** Do not route the busy signal through `combineOffload` as the sysfs slot. Either (a) make the busy fold a status-only escalation that preserves the carried signals/delta:

```go
if in.GPUBusyPercent.Known {
    busy := gpuBusyFloor(in.GPUBusyPercent)
    // Escalate status with combineOffload's precedence, but KEEP the original
    // log/sysfs signals + GTT delta in the contract.
    folded := combineOffload(verdictAsResult(v), busy)
    v.Status = folded.Status
    v.Detail = folded.Detail        // and fix the nesting below
    v.Remediation = folded.Remediation
    v.Raw = firstNonEmpty(v.Raw, busy.Raw)
    // v.SysfsOffload and v.GTTDeltaBytes are left as the residency+floor values.
    provenance += " + gpu_busy_percent corroboration"
}
```

…or (b) give `combineOffload` a 3-signal variant that carries the busy signal in a dedicated field. Separately, fix the `Detail` nesting: `verdictAsResult(v)` should not feed an already-`joinDetail`'d string back into `joinDetail`. Add a test asserting `SysfsOffload.Source`, `GTTDeltaBytes`, and a non-nested `Detail` on the Known-busy ROCm path.

## Warnings

### WR-01: Start-time ROCm scrape test feeds a journald-timestamp fixture through the stderr scraper — the `ggml_cuda_init:` branch can never fire, so the test passes for the wrong reason

**File:** `internal/inference/offload_test.go:93` (case `"rocm cpu fallback → WARN"` uses `load_tensors_rocm_cpu.txt`); interacts with `internal/inference/offload.go:113`

**Issue:** `scrapeOffloadLog` is the **start-time** scrape over Runner *stderr* (no journald prefix). The `StartLogDevicePrefix` branch uses `strings.HasPrefix(line, m.StartLogDevicePrefix)`. The ROCm CPU fixture `load_tensors_rocm_cpu.txt` is a *journald-format* file whose lines start with `Jun 05 12:00:01 strix villa-llama[1234]: ggml_cuda_init: ...`, so `HasPrefix(line, "ggml_cuda_init:")` is always false. The case yields WARN because no device and no offloaded line are seen — but it never exercises the `ggml_cuda_init:` device-init parse the comment claims it covers, and it mixes a journald fixture into the stderr scraper. A real ROCm CPU-fallback stderr (`ggml_cuda_init: no ROCm devices found...` with no timestamp prefix) would still WARN here only because that line contains no `=` — the test does not prove that.

**Fix:** Use a stderr-shaped fixture (no journald timestamp prefix) for the start-time ROCm CPU case, mirroring `rocm_devinfo_pass.stderr`. Confirm a `ggml_cuda_init: ... = ...` shaped line (if any real build emits one) is handled as intended, or document that the ROCm start-time device proof comes only from the `- ROCm0` device_info label, not the `ggml_cuda_init:` prefix.

### WR-02: Running scrape cannot detect a runtime partial offload on its own — it relies entirely on the GTT floor backstop

**File:** `internal/inference/running_offload.go:157-182`

**Issue:** Unlike `scrapeOffloadLog` (which gained the `0<N<M` partial-FAIL rule, offload.go:169), `scrapeLoadTensorsResidency` has no partial-offload concept. A runtime journal with a small `ROCm0 model buffer size = 373.71 MiB` line *plus* a large `CPU_Mapped model buffer size = 41037.94 MiB` line returns PASS (`sawDeviceBuffer && deviceMiB > 0`), ignoring `sawCPUBuffer`. The only thing that catches "most weights actually landed on CPU" is the GTT floor (`used >= weight`). That backstop is sound when `WeightBytes > 0`, but when the weight is unknown (`weightBytes==0` → floor degrades to WARN, running_offload.go:220) the combined verdict for a genuine partial offload is WARN, not FAIL — a silent-CPU-fallback that the dual assert is meant to catch slips to WARN.

**Fix:** When `sawDeviceBuffer && deviceMiB > 0 && sawCPUBuffer`, at minimum record the CPU-buffer presence in the detail, and consider keying a partial-residency FAIL on the device-vs-CPU buffer ratio (the running analog of the start-time `0<N<M` rule) so the residency signal itself can FAIL a runtime partial offload independent of the GTT floor.

### WR-03: `seccomp=unconfined` is retained verbatim for the ROCm backend without re-justifying it against the added `/dev/kfd` exposure

**File:** `internal/inference/backend_rocm.go:67-71`

**Issue:** The ROCm backend adds `--device /dev/kfd` (the AMD KFD compute device) on top of `/dev/dri`, and keeps `--security-opt seccomp=unconfined`. KFD is a broad ioctl surface; combining an unconfined seccomp profile with direct `/dev/kfd` access widens the container's kernel attack surface meaningfully beyond the Vulkan `/dev/dri`-only case. The code comment cites the kyuz0 "documented minimum" for Vulkan but does not re-evaluate whether unconfined seccomp is still the minimum once `/dev/kfd` is exposed. This is a rootless container (mitigating), and CLAUDE.md prescribes the kyuz0 image, so it is defensible — but the security posture of the kfd+unconfined combination should be an explicit, documented decision, not inherited silently.

**Fix:** Document (in the backend_rocm.go header or SECURITY.md) the explicit rationale for `seccomp=unconfined` *together with* `/dev/kfd`, citing the upstream requirement. If the kyuz0 ROCm image runs under a constrained seccomp profile, prefer that over `unconfined`. At minimum, confirm the rootless boundary is the intended containment and record it.

### WR-04: `runValidation` re-derives the backend/config independently of the live deps it shares a contract with

**File:** `cmd/villa/inference.go:113-138`

**Issue:** `runValidation` loads config and resolves the backend itself (`config.LoadVilla` + `inference.BackendFor`) rather than reusing the single resolution path the other live-wiring sites use. The header comment acknowledges this ("runValidation takes no cfg today, so load it here rather than invent a signature change"). It is fail-closed and correct today, but it is a second backend-resolution point — exactly the kind of drift the "single polymorphism point" design (backend.go header) warns against. A future change to how config/backend is resolved must remember to update this site too.

**Fix:** Thread the resolved backend (or the loaded `VillaConfig`) in through the `validateFn` seam so there is one resolution point, or add a focused test asserting `runValidation` rejects an unknown `backend` value the same way `liveStatusDeps`/`liveInstallDeps` do (currently the validate path's fail-closed branch at inference.go:129-137 has no direct test).

## Info

### IN-01: Redundant `fmt.Sprintf("%s", ...)` over a Stringer

**File:** `cmd/villa/status.go:109`

**Issue:** `offloadCell = fmt.Sprintf("%s", s.Offload.Status)` formats a single value that already implements `String()`. `s.Offload.Status.String()` is clearer and avoids the reflection-based format path. `go vet` does not flag it, but `gosimple`/`staticcheck` (S1025) would.

**Fix:** `offloadCell = s.Offload.Status.String()`.

### IN-02: `gpuBusyFloor`'s `!busy.Known` branch returns a PASS-typed result that is never reached

**File:** `internal/inference/running_offload.go:255-265`

**Issue:** `gpuBusyFloor` documents (and implements) a defensive PASS-equivalent return for `!busy.Known`, but `RunningOffloadVerdict` only calls it inside `if in.GPUBusyPercent.Known` (running_offload.go:320), so the `!busy.Known` branch is dead under the sole caller. This is intentional defensive code, but it is currently unreachable and untested via the real path — a future caller that folds it unconditionally would get a PASS-typed Unknown that, through `combineOffload`, cannot downgrade but could still corrupt the sysfs/delta fields per CR-01.

**Fix:** Keep the guard as documented, but add a brief test (or an assertion comment) pinning that the only caller gates on `Known`, so the dead branch's contract is locked.

### IN-03: GTT-floor `DeltaBytes` is repurposed to carry the absolute used-bytes, not a delta

**File:** `internal/inference/running_offload.go:232,239` (sets `DeltaBytes: used.Value`)

**Issue:** In the running path, `OffloadResult.DeltaBytes` is set to the absolute `mem_info_gtt_used` value (a point-in-time floor), while in the start-time path the same field is a true before/after *delta* (offload.go:275). The shared field name `DeltaBytes` and the JSON `gtt_delta_bytes` therefore mean different things depending on which path produced the verdict. This is documented in comments but is a naming hazard for downstream `--json` consumers (and is the field CR-01 zeroes out).

**Fix:** No behavior change required; consider a doc note on the `Verdict.GTTDeltaBytes` JSON field clarifying it is "before/after delta (start-time) or point-in-time used (running)" so dashboard/calibration consumers do not misinterpret it.

---

_Reviewed: 2026-06-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
