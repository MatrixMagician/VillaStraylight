---
description: An unset backend must mean ROCm in both places
globs: ["internal/inference/**", "internal/catalog/**", "internal/recommend/**", "internal/config/**"]
priority: 10
prompt: ["backend", "backendfor", "catalog", "seed.json", "default backend"]
---
ROCm 7.2.4 is the default inference backend; Vulkan RADV is the fallback (`villa backend set vulkan`).

`BackendFor("")` and `IsROCmFamily("")` must BOTH resolve to ROCm. If only one does, an unset config
runs ROCm while skipping the ROCm preflight gate — the host ends up on a backend whose floors were
never checked.

`internal/catalog/seed.json`'s per-entry `backend_default` OVERRIDES `recommend.defaultBackend`. The
two are separate declarations of the same intent, so a change to either without the other silently
splits the answer. Keep them in step.
