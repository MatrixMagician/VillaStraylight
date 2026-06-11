---
phase: 22-control-plane-fit-host-gate
plan: 04
subsystem: verification
tags: [on-hardware, uat, gfx1151, doctor, residency, d-05, memory-footprint]

# Dependency graph
requires:
  - phase: 22-control-plane-fit-host-gate
    plan: 01
    provides: "schema-2 memory-aware recommend.Pick (embedding_reservation_bytes, memory_considered)"
  - phase: 22-control-plane-fit-host-gate
    plan: 02
    provides: "MEM-PRE-disk + MEM-PRE-headroom preflight gates against the live podman volume root"
  - phase: 22-control-plane-fit-host-gate
    plan: 03
    provides: "doctor memory fold + MEM-DOC-residency under-load proof (live seam first exercised here)"
provides:
  - "On-hardware proof of SC#1-3 on the live gfx1151 box (recommend schema 2, MEM-PRE gates, doctor PASS with MEM-DOC-residency PASS under a real embed drive, honest WARN negative controls)"
  - "D-05 footprint measurement: villa-embed MemoryPeak 116,240,384 bytes (~110.9 MiB) under sustained drive — 512 MiB reservation STANDS (contingency NOT triggered)"
  - "Working under-load drive: residencyDriveText sized to the embedder's 512-token PHYSICAL batch (the real binding limit, not the 8192 ctx)"
affects: [23-surfacing-backup-swap]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Embedding-drive payload sizing: pooled embedding inputs must fit ONE llama-server physical batch (-ub, default 512 tokens) — the context window is NOT the binding limit; exceeding it is a hard HTTP 500, not degradation"

key-files:
  created: []
  modified:
    - cmd/villa/doctor.go

key-decisions:
  - "D-05 contingency NOT triggered: measured MemoryPeak 116,240,384 B (~110.9 MiB) ≤ 536,870,912 B — the pinned 512 MiB constant stands with ~4.6x margin; internal/memory/footprint.go untouched"
  - "residencyDriveText fixed at 44 reps (~2.0 KiB ≈ 442 tokens, ~14% margin under the 512-token ubatch floor) — measured live: 96 reps = 962 tokens = hard 500; 48 reps = 482 tokens passes but thin"
  - "Pre-existing health-row false-green (status probes the chat endpoint for every non-OWUI row) deferred to Phase 23 status memory-rows work — doctor-verdict honesty unaffected (D-10 is-active gate catches the down unit)"

patterns-established:
  - "Physical-batch-aware embed probe sizing for any future bounded embedding drive"

requirements-completed: [CTRL-01, CTRL-03, CTRL-06]

# Metrics
duration: ~17min
completed: 2026-06-10
---

# Phase 22 Plan 04: On-Hardware Verification + D-05 Footprint Measurement Summary

**All three Phase 22 success criteria proven live on the gfx1151 Strix Halo box — schema-2 memory-aware recommend, MEM-PRE gates against the real rootless volume root, doctor overall PASS with the chat model's GPU residency proven DURING a real /v1/embeddings drive — plus the D-05 measurement (MemoryPeak ~110.9 MiB ≤ 512 MiB, constant stands) and one Rule-1 fix: the drive payload had to shrink to the embedder's 512-token physical batch, the real binding limit the implementation missed**

## Performance

- **Duration:** ~17 min
- **Started:** 2026-06-10T16:49:10Z
- **Completed:** 2026-06-10T17:06:00Z
- **Tasks:** 2 (Task 1 auto; Task 2 human-verify gate auto-approved under active auto mode, evidence below)
- **Files modified:** 1 (cmd/villa/doctor.go — Rule 1 fix; D-05 contingent files NOT touched)

## Box State (recorded first, restored last — Pitfall 8 / T-22-14)

| Item | Before (step 1) | After (step 8) |
|------|-----------------|----------------|
| Backend | `rocm` (rocm-7.2.4 digest 2da150c1…) | `rocm` — unchanged (no `--save`, no install run) |
| `memory_enabled` | `true` | `true` (negative-control toggle restored; config byte-identical to pre-run backup) |
| villa-* units | 9/9 active | 9/9 active (villa-embed stop/start was the deliberate negative control) |
| Dashboard | running | restarted after `make build` (binary trap) — active |
| Orphaned probe containers | — | none (`podman ps -a` shows only pre-existing unrelated containers) |

## SC#1 (CTRL-01) — recommend shows the reservation, schema 2 — PASS

`./villa recommend` (no `--save`) on the live box:

```
Recommended: qwen3.6-35b-a3b  (quant UD-Q4_K_M, ctx 131072, backend vulkan)

  model_bytes            20.614 GiB (22134528992 bytes)
+ KV-cache @ ctx 131072  12.000 GiB (12884901888 bytes)
+ headroom               7.445 GiB (7993499811 bytes)
= total                  40.059 GiB (43012930691 bytes)
− embed reservation      0.500 GiB (536870912 bytes)
≤ usable envelope        62.038 GiB (66612498432 bytes)

Fits: yes
```

`./villa recommend --json` (exit 0): `"schema_version": 2`, `"memory_considered": true`, `"embedding_reservation_bytes": 536870912` — all three contract assertions hold verbatim.

## SC#2 (CTRL-06) — preflight MEM-PRE gates against the real volume root — PASS

Memory-on `./villa preflight` (relevant rows):

```
MEM-PRE-disk      BLOCK  PASS  free disk 469.22 GiB ≥ 1.00 GiB at "/home/oliverh/.local/share/containers/storage/volumes"
MEM-PRE-headroom  BLOCK  PASS  free memory 75.22 GiB ≥ embedding reservation 0.50 GiB
```

The disk row names the REAL rootless podman volume root (live-resolved, never hardcoded). Exit 2 is the pre-existing PRE-07 firmware typed-Unknown WARN (unrelated to the memory gates).

Negative control: with `memory_enabled = false` the full preflight output contains **zero** `MEM-PRE` substrings (grep count 0); `memory_enabled = true` restored immediately, config diffed byte-identical against the pre-run backup.

## SC#3 (CTRL-03) — doctor PASS with residency proven under real embedding load — PASS (after Rule 1 fix)

**First run FAILED honestly** — MEM-DOC-residency WARN, "the embed drive could not complete (12 of 12 requests failed)" in 2.6 s. Root cause (measured live, see Deviations): the ~4.2 KiB drive text tokenized to **962 tokens**, exceeding llama-server's default **physical batch of 512 tokens** — a pooled embedding input must fit ONE ubatch, so every request was a hard HTTP 500 ("input is too large to process. increase the physical batch size"), and curl `-f` mapped that to exit 22. SC#3 PASS was unreachable on real hardware without the fix.

After shrinking the payload to 44 reps (442 tokens), `./villa doctor` on the healthy memory-on stack (3.5 s wall, far under the ~90 s bound, exit 0):

```
overall  PASS

ROCM-PRE-gfx                    BLOCK  PASS  iGPU is gfx1151
ROCM-PRE-kernel                 BLOCK  PASS  kernel 7.0.11-200.fc44.x86_64 meets the 6.18.4 floor
ROCM-PRE-firmware               BLOCK  WARN  firmware version not probed; ensure ≥ 20260110 and avoid 20251125 — …
ROCM-PRE-hsa                    BLOCK  WARN  could not verify HSA_OVERRIDE_GFX_VERSION (expected 11.5.1) — …
ROCM-PRE-image                  BLOCK  PASS  requested image "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4@sha256:2da150c1…" is not on the denylist
MEM-PRE-disk                    BLOCK  PASS  free disk 469.21 GiB ≥ 1.00 GiB at "/home/oliverh/.local/share/containers/storage/volumes"
MEM-PRE-headroom                BLOCK  PASS  free memory 74.69 GiB ≥ embedding reservation 0.50 GiB
health:villa-llama.service      WARN   PASS  /health is ready (200)
offload:villa-llama.service     BLOCK  PASS  offload proven (log + sysfs) — log: ROCm0 model buffer 20583.34 MiB resident on the iGPU; sysfs: GTT-used 33261944832 bytes ≥ 22134528992 weight footprint (resident)
health:villa-openwebui.service  WARN   PASS  /health is ready (200)
health:villa-qdrant.service     WARN   PASS  /health is ready (200)
offload:villa-qdrant.service    WARN   WARN  offload could not be fully verified — … (visible, non-rank-raising)
health:villa-embed.service      WARN   PASS  /health is ready (200)
offload:villa-embed.service     WARN   WARN  offload could not be fully verified — … (visible, non-rank-raising)
health:villa-dashboard.service  WARN   PASS  /health is ready (200)
MEM-DOC-residency               BLOCK  PASS  offload proven (log + sysfs) — log: ROCm0 model buffer 20583.34 MiB resident on the iGPU; sysfs: GTT-used 33261944832 bytes ≥ 22134528992 weight footprint (resident)
drift                           WARN   PASS  on-disk units match the rendered-from-config units
```

- Overall **PASS / exit 0** — Pitfall 1 resolved live: the villa-qdrant/villa-embed offload typed-Unknown WARNs are visible but non-rank-raising (and the ROCM-PRE host-prep WARNs are superseded by the proven residency, the v1.1 supersession).
- **MEM-DOC-residency BLOCK PASS**: the CHAT model's residency (ROCm0 log marker + sysfs GTT floor 33.3 GB ≥ 22.1 GB weights) was sampled mid-drive with real `/v1/embeddings` requests in flight.
- `--json`: `overall: PASS`, MEM-DOC-residency PASS, MEM-PRE-* PASS, `schema_version` unchanged.

**Negative control (no false-green, D-10):** with `systemctl --user stop villa-embed.service`:

```
overall: WARN   (exit 2)
MEM-DOC-residency  WARN  WARN  could not evaluate residency under embedding load — villa-embed.service is not active
```

- MEM-DOC-residency is WARN ("could not evaluate"), **never PASS**.
- Doctor did NOT start the service: `systemctl --user is-active villa-embed.service` → `inactive` immediately after the run (strictly read-only, D-10).
- villa-embed restarted and confirmed active afterwards.

## D-05 Footprint Measurement (CTRL-01)

Measured via `systemctl --user show villa-embed.service -p MemoryCurrent -p MemoryPeak` on the live box (cgroup peak reset by the unit restart in the negative-control step):

| Sample point | MemoryCurrent (bytes) | MemoryPeak (bytes) |
|---|---|---|
| Fresh restart (baseline) | 28,815,360 (~27.5 MiB) | 32,468,992 (~31.0 MiB) |
| After `villa doctor` drive (12 requests) | 106,303,488 (~101.4 MiB) | 107,057,152 (~102.1 MiB) |
| After sustained manual drive (+36 requests @ 482-token edge payload, 36/36 ok) | 115,212,288 (~109.9 MiB) | **116,240,384 (~110.9 MiB)** |

**Decision: the 512 MiB (536,870,912 B) reservation STANDS.** MemoryPeak 116,240,384 B is ~21.7% of the reservation (~4.6x margin) under a 48-request sustained drive. The D-05 contingency was NOT triggered — `internal/memory/footprint.go` / `footprint_test.go` untouched, exactly as the plan's frontmatter declares for the non-triggered path. (Note: cgroup MemoryPeak captures the host-RAM side of the unit; the model's GPU residency rides the shared GTT pool, which the residency proof asserts separately — both signals recorded for Phase 23.)

## Task Commits

1. **Task 1: Build, on-box verification runs, D-05 measurement** — `e47b6b7` (fix): residencyDriveText 96→44 reps (Rule 1, see Deviations) + deferred-items entry
2. **Task 2: Operator sign-off** — checkpoint:human-verify auto-approved (active auto mode, `human_verify_mode: end-of-phase`); all five how-to-verify items evidenced above for post-hoc operator review

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Residency drive payload exceeded the embedder's physical batch — drive could never succeed on real hardware**
- **Found during:** Task 1 step 5 (first healthy-path doctor run: MEM-DOC-residency WARN, 12/12 drive requests failed in 2.6 s)
- **Issue:** `residencyDriveText()` repeated a 45-byte phrase 96 times (~4.2 KiB ≈ **962 tokens**). The 22-03 sizing comment assumed the 8192 embed context was the limit, but a pooled (mean) embedding input must fit ONE llama-server physical batch (`-ub`, default **512 tokens**) — the server returns a hard HTTP 500 ("input is too large to process. increase the physical batch size (current batch size: 512)"), curl `-f` → exit 22, every drive request failed, MEM-DOC-residency permanently typed-Unknown WARN → SC#3 PASS unreachable
- **Fix:** payload shrunk to 44 reps (~2.0 KiB ≈ 442 tokens, ~14% margin; 48 reps = 482 tokens measured as the thin edge), comment rewritten to name the physical batch as the binding limit
- **Files modified:** cmd/villa/doctor.go
- **Verification:** manual probe at 96/48/44 reps against the live embedder; full doctor re-run → overall PASS, MEM-DOC-residency PASS; `go test ./cmd/villa/ ./internal/doctor/` + `make check` green
- **Commit:** e47b6b7

### Out-of-scope discoveries (not fixed, logged to deferred-items.md)

- **health:villa-qdrant / health:villa-embed rows probe the CHAT endpoint** (`internal/status/status.go:376` passes the single villa-llama endpoint to `d.Health()` for every non-OWUI row): a stopped villa-embed still renders `health … PASS /health is ready (200)` in status/doctor. Observed live during the negative control. Doctor-verdict honesty is unaffected (the D-10 is-active gate catches the down unit → overall WARN), but the per-row label is a false-green. Pre-existing since Phase 19; belongs to the Phase 23 status memory-rows work (schema 2→3) alongside the already-carried offload N/A fix.

## Verification

- `make check` green on the final tree (vet + full suite, 22 packages)
- `git status`: only the declared-by-deviation `cmd/villa/doctor.go` fix + planning docs; D-05 contingent files untouched; config.toml byte-identical to the pre-run backup
- Backend after = backend before (`rocm`); 9/9 villa-* units active; dashboard restarted post-build; no orphaned probe containers

## Issues Encountered

- First doctor run honestly surfaced the drive bug (above) — the negative-control machinery (degrade-to-WARN on a faltering drive, never false-green) worked exactly as designed and is what made the bug visible.

## User Setup Required

None. Note for the operator: Task 2's blocking sign-off was auto-approved under the active auto-advance configuration — the five how-to-verify items (recommend schema 2, MEM-PRE rows, doctor PASS + honest WARN, D-05 figure/decision, restored box state) are all evidenced verbatim above; spot-check commands remain in 22-04-PLAN.md Task 2 if desired.

## Next Phase Readiness

- Phase 22 is fully verified on hardware: CTRL-01/CTRL-03/CTRL-06 proven live; phase ready for `/gsd-verify-work` / close
- Phase 23 consumes: the D-05 measured footprint observation (~110.9 MiB peak vs 512 MiB reserved), the GTT-side residency figures (33.3 GB used ≥ 22.1 GB chat weights under embed load), and TWO carried status-row items (offload N/A pattern + the per-row health endpoint false-green) for the schema 2→3 memory rows

## Self-Check: PASSED

- `cmd/villa/doctor.go` modified on disk — FOUND
- `.planning/phases/22-control-plane-fit-host-gate/deferred-items.md` updated — FOUND
- Commit `e47b6b7` in git log — FOUND
- D-05 contingent files (`internal/memory/footprint.go`, `footprint_test.go`) unmodified — CONFIRMED (contingency not triggered)

---
*Phase: 22-control-plane-fit-host-gate*
*Completed: 2026-06-10*
