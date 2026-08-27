---
status: accepted
---

# Offload is proven by two independent signals, never assumed

A llama.cpp server that has silently fallen back to the CPU is healthy by every
ordinary measure: the process runs, the port listens, `/health` returns 200, and
completions come back, just an order of magnitude slower. Because a false green
here defeats the whole point of the product, a residency proof requires **both**
the startup-log scrape (a real GPU device selected, software renderers rejected,
layers offloaded > 0) and the amdgpu sysfs GTT-used delta to pass; either signal
alone is foolable.

## Considered options

**A liveness probe** (process up, port open, `/health` 200), rejected: this is
exactly the check that CPU fallback passes.

**One signal, the log scrape alone**, rejected: the server can log a GPU device
and still offload nothing, and the log format drifts as the upstream image
rebuilds on llama.cpp master.

**One signal, the sysfs GTT delta alone**, rejected: GTT-used moves for reasons
unrelated to our model, so a delta on its own proves nothing about *this* server.

## Consequences

Each signal degrades to a typed Unknown, which is a **Warn** and is distinct
from a confidently-false signal, which is a **Fail**. Uncertainty is never
reported as either success or failure: an unreadable stderr or a missing sysfs
file must not manufacture a pass, and must not manufacture a failure either.

The sysfs half is a calibrated threshold against the model's on-disk weight, not
a boolean, so it carries tuning constants that a future host or quantisation may
need to move. The observed delta is recorded in the `--json` output precisely so
those constants can be re-derived from real runs rather than guessed.

This makes the proof the expensive part of every swap and of `villa doctor`: a
backend swap, model swap, or coding-mode change must run the model under load to
prove residency before it may commit, and rolls back verbatim when it cannot.
That cost is deliberate and should not be optimised away by trusting a cheaper
signal.
