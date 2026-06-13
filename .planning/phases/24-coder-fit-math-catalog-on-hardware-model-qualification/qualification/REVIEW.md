# Operator Evidence Review — Plan 24-03 (CODER-03)

**Checkpoint:** Task 4 — Operator review of qualification evidence + restored stack
**Gate:** `checkpoint:human-verify` (blocking; never auto-approved — gates a catalog freeze on real-hardware truth)
**Reviewed:** 2026-06-13
**Operator decision:** **APPROVED** — evidence released to plan 24-04 (reconciliation + D-11 toolbox decision record).

## What was reviewed

Three complete on-hardware qualification evidence directories on the gfx1151 Strix Halo
dev box (Fedora 44, rootless podman 5.8.2), each served BY DIGEST
`sha256:9a74e555…` (build 9496 / commit 94a220cd6 — never the drifted `vulkan-radv`
tag at build 9579):

| Entry | agent_ctx | VERDICT | Offload | Measured KV vs computed | Cache-reuse probe |
|-------|-----------|---------|---------|--------------------------|-------------------|
| `qwen3-coder-30b-a3b` | 65536 | **PASS** | 49/49 | 6144.00 MiB = 6.00 GiB (exact) | true (cache_n=135, no warning) |
| `qwen3-coder-next-q4` | 131072 | **PASS** | 49/49 | 3072.00 MiB = 3.00 GiB (exact; n_layers:12 hybrid encoding validated) | true* (cache_n=58, no warning) |
| `qwen3-coder-next-q3` | 131072 | **PASS** | 49/49 | 3072.00 MiB = 3.00 GiB (exact) | true* (cache_n=58, no warning) |

`*` Cache-reuse on the Next hybrids came back **true** against the stated probe
criteria (no incompatibility warning AND turn-2 `cache_n > 0`) — but via
**recurrent-state context checkpoints** (75.376 MiB DeltaNet state restored on turn 2),
NOT GQA `n_cache_reuse` chunk reuse. The catalog `cache_reuse_safe` claim for the Next
entries is deliberately left to plan **24-04** to ratify (FINDING A3). The flag is
demonstrably harmless on build 9496.

## Review steps performed (per the checkpoint how-to-verify)

1. Opened each `qualification/<entry-id>/verdict.md`; confirmed every `VERDICT: PASS`
   line is supported by cited evidence — real read→edit→verify agent tool use (not
   narrated XML), offload N/N (49/49), server version build 9496.
2. Spot-checked `qwen3-coder-30b-a3b/crush-transcript.txt`: the agent ran `go test ./...`,
   read the failing test + source, fixed the seeded `i < n` → `i <= n` off-by-one, and
   re-ran `go test ./...` green.
3. Ran `./villa status`: chat stack green, OFFLOAD asserted (not just active) — the
   live stack was restored after qualification (`villa-llama.service` restarted).
4. Confirmed `/tmp/crush-qual` and `/tmp/qual-repo` hold the only Crush artifacts —
   nothing installed into PATH or shipped (D-08; `command -v crush` empty).

## Findings carried into plan 24-04

- **A2 — DeltaNet recurrent-state constant measured directly:** RS buffer = 301.50 MiB
  (4 slots × ~75.4 MiB/seq), ctx- and quant-independent (identical on Q4 and Q3 runs).
  Candidate to fold into `min_envelope_bytes` if material — 24-04 call.
- **A3 — cache-reuse mechanism nuance:** reuse via context checkpoints, not KV shifting;
  `cache_reuse_safe` for the Next entries to be ratified in 24-04 with this nuance recorded.
- **Harness deviation:** Crush v0.76.0 rejects `--yolo` with the `run` subcommand;
  auto-accept implemented via config `permissions.allowed_tools` (strictly tighter than
  `--yolo` — no agent/fetch/download auto-accept; T-24-09 unchanged).

**Disposition:** All three entries PASS on real hardware. Evidence set is released to
plan 24-04. Nothing is shipped on hope from this plan — failures (none here) would have
been recorded precisely for D-10/D-12 disposition.
