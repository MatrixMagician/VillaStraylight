---
description: Backend literals and pinned images stay behind their seams
globs: ["internal/**/*.go", "cmd/villa/**/*.go"]
priority: 60
prompt: ["backend", "rocm", "vulkan", "quadlet", "offload", "pins", "pinstate", "seam"]
---
Backend marker strings — `ROCm0`, `Vulkan0`, `HSA_OVERRIDE…`, container image tags and digests, device
paths — live in `internal/inference` (and `internal/detect/gpu_amd.go`) and nowhere else.
`TestSeamGrepGate` walks both `internal/` and `cmd/villa` and fails the build on a leaked literal.

`inference.BackendFor(name)` is the only place a config `backend` string becomes a concrete
implementation. Everything else depends on the `Backend` interface.

Rendered units derive their image through `livePinnedRender`, never a constant and never a direct
`orchestrate.Render` call from a cmd verb — a test fails the build if one does. A pin is two values:
the vetted pin compiled into `pins.Table` and the effective pin in `pinstate`. They are separate
packages because they fail differently; `pinresolve` is where they meet.

Offload is offload-asserting, never liveness. A silent or partial CPU fallback is a FAIL through
`ResidencyProof`, never a false green.
