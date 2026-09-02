---
status: accepted
---

# The install flow runs behind one interface, and narrates through a seam

Install was the last stack-mutating flow whose body lived in the command tier. Its
decisions had moved into `internal/install` (gate resolution, plan assembly, the
mutate-and-start sequence, the ADR-0003 transaction), but the flow that composed
them was still a 700-line cobra function that printed as it went. The flow and its
presentation were inseparable, so every test of an install invariant had to drive
the cobra command and read the outcome back out of stdout.

The update flow had already shown the other shape: `updateflow.Run(ctx, Deps,
targets)` returns a typed `Result`, and the command tier wires the live adapters
and renders. Install now has the same shape: `install.Run(ctx, Deps, Opts)` runs
the whole flow, from the config refusal to the readiness proofs and the rollback,
and returns the `Result` the expand step introduced. The command tier keeps two
jobs, wiring and rendering, and the flow's invariants are tested at the interface.

## How the flow narrates

The update flow narrates after the fact: it returns a `Result` and the command
tier prints it. Install cannot. It is the longest-running verb in the tool, and
most of its wall-clock is multi-gigabyte weight pulls; a "downloading" line that
appears after the download is no progress at all.

**Emit a typed line through a seam**, the chosen option. `Deps.Emit func(Line)`
receives each narration line as it happens, tagged with its stream. The core never
holds a writer and never prints; the command tier's adapter writes each line to
stdout or stderr. A test captures the lines into a buffer the same way, so the
contracted copy stays byte-testable at the interface.

**Hand the core `io.Writer`s**, rejected. It is the smaller diff, and `backup`
takes a writer for its archive bytes, but a writer for prose is printing by
another name. A core that prints is a core whose output cannot be tested as a
value, which is the property the whole pure-core convention exists to keep.

**Accumulate lines in the Result and render after**, rejected, for the progress
reason above. It is the right shape for `update`, whose steps are seconds; it is
the wrong shape for a verb whose steps are minutes.

## Consequences

`internal/install` now imports `inference`, `detect`, `preflight`, `catalog` and
`recall`, all of which it takes as typed values through `Deps`. It still contains
no host I/O, no `os/exec` and no image literal; the seam grep gate walks it.

The readiness verdict collapses into `install.Proof`; the three subsystem proof
verdicts stay cmd-side types because the verify family shares them, and the live
wiring adapts each to a `Proof` at the seam. The flow's host-prep gate
(`gateInstall` and its consent helpers) moves with the flow; the wizard keeps
calling the remediation helpers, now exported from the core.
