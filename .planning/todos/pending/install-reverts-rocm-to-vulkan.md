---
id: install-reverts-rocm-to-vulkan
type: bug
status: pending
created: 2026-06-14
source: Phase 27 Plan 04 on-hardware acceptance
severity: medium
component: cmd/villa/install.go
reproduced_on_hardware: 2026-06-15  # confirmed again during Phase 28 UAT: `villa install --coding-agent` clobbered backend rocm->vulkan on gfx1151; restored via `villa backend set rocm`
---

# `villa install` reverts a persisted `backend=rocm` to Vulkan

## What

`runInstall` recomputes the recommendation with `rec := d.pick(profile, recommend.Overrides{})`
(no overrides) and then does `cfg.Backend = rec.Backend` (`cmd/villa/install.go:432`).
`recommend.Pick` always defaults to **Vulkan** (ROCm is strictly opt-in and is never
auto-recommended), so any re-run of `villa install` (including `villa install --coding-agent`)
**silently discards a persisted `villa backend set rocm` opt-in** and rewrites the
`villa-llama` unit + `config.toml` to Vulkan.

This is a **config-is-source-of-truth violation**: the persisted `backend` field is the
authority for `villa up`/`restart`/`backend`, but `install` overwrites it from a fresh
recommendation instead of honoring the loaded value.

## Evidence (Phase 27 Plan 04, gfx1151)

On a box with `backend = "rocm"` (villa-llama on ROCm 7.2.4 `@sha256:2da150c1…`),
`villa install --coding-agent` rendered the **Vulkan RADV** unit, wrote it, and persisted
`backend = "vulkan"`. The runtime stayed ROCm only incidentally — `systemctl --user start`
no-op'd the already-active service, so the container was not recreated until a later
stop/start. Restoring required `villa backend set rocm`.

## Expected

`villa install` should preserve a persisted non-default `backend` (e.g. carry the loaded
`cfg.Backend` into `recommend.Overrides`, or skip the `cfg.Backend = rec.Backend` overwrite
when a valid persisted backend exists), so a re-install is a true no-op on a ROCm-opted box.

## Fix sketch

- Load `cfg` (already done), and when `cfg.Backend` is a valid non-empty value, pass it as an
  override into `pick()` or guard the `cfg.Backend = rec.Backend` assignment so it does not
  clobber an explicit opt-in.
- Add a regression test: install on a `backend=rocm` config renders the ROCm unit and a
  re-install is a no-op (`plan.Changed == 0`).

## Scope

Out of Phase-27 scope (Phase 27 is the agent addon + verify). Fix in a maintenance pass.
