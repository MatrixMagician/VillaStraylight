---
description: Reuse-first implementation
priority: 90
---
Trace the real flow before changing it, then decide whether the requested behavior needs new code at
all. Reach in this order: an existing `internal/*` core or `Deps` seam that already does the job, the
standard library, an already-direct dependency. This module has four direct dependencies on purpose —
adding a fifth needs an argument, not a convenience.

When code must change, fix the smallest shared root cause rather than guarding each caller. Delete
code when the behavior survives without it. Skip speculative abstractions, knobs, and configuration
for values that never change; an interface with one implementation is a smell here, not a shape.

Keep what protects the user: validation at trust boundaries, fail-closed handling of hand-edited
config and unknown backend strings, and the honesty invariants (a typed `Unknown` degrading to WARN is
not the same claim as a confident negative degrading to FAIL — never collapse them).

Run the smallest focused check that exercises what you changed. `make check` (vet + test + test-race)
is the pre-commit gate, and `make build-static` covers the CGO-free build that `make check` does not.
Do not claim a gate result you have not read.
