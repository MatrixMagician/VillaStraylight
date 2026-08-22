---
status: accepted
---

# A failed install rolls back to the prior stack, and a first install leaves nothing behind

The three swap flows are transactional: they capture the prior state, mutate, prove,
and restore verbatim on any failure, reporting honestly when a rollback could not
complete. Install was the one stack-mutating flow outside that discipline. It proves
just as thoroughly — memory, search readiness and the coding agent all have proof
seams — but a failure part-way through left whatever it had already written and
started in place. The word "rollback" did not appear in the install path.

That asymmetry is not defensible. An install that renders units, starts four
services, and then fails its residency proof leaves a running-but-unproven stack: the
exact false-green state ADR-0001 exists to prevent, reached through the one door that
was not guarded.

Install now captures before mutating and restores on failure, like the swaps.

## What "restore" means on a host that had no prior stack

The swap cores always have a prior state to restore to, because a swap presupposes
something already running. A first install does not. That difference forces a
decision the swaps never had to make.

**Restore to nothing** — the chosen option. On a host with no prior install, a failed
install stops the services it started, removes the units it wrote, and deletes the
config it persisted, leaving the host as it found it.

**Preserve the partial state for diagnosis** — rejected. It reads as the more helpful
option and is the more dangerous one. A half-installed stack is indistinguishable
from a healthy one to every ordinary check: the units exist, the services are active,
`/health` answers. A later `villa up`, or a reboot, would bring up a stack that never
passed its proof, and the operator would have no signal that anything was wrong. The
diagnostic value is available without the risk, because the failure detail and the
remediation are printed at the point of failure, and the model weights — the
expensive part — are deliberately NOT removed, so a retry does not re-download them.

**Ask the operator** — rejected. The decision must hold in a non-interactive run, and
a prompt at the moment of failure is the worst time to ask.

## Consequences

Rollback is best-effort and reports honestly when it is incomplete, exactly as the
swap cores do. A rollback step that itself fails must never be presented as a clean
restoration: the result says the stack is in an indeterminate state and names what
could not be undone, because a wrong "restored" claim is worse than an honest
"partially restored".

The captured state is the config, the unit files, and which services were running.
Model weights and container images are NOT captured or removed. They are large,
expensive to re-acquire, and inert on their own — an unused GGUF on disk harms
nothing, while re-downloading tens of gigabytes after a transient failure is a real
cost. This mirrors the backup path, which excludes model weights for the same reason
and re-stages them on restore.

A re-install over an existing stack restores that prior stack verbatim on failure,
which means a failed upgrade leaves the working install running rather than a broken
one.
