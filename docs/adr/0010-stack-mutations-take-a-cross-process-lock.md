---
status: accepted
---

# Stack mutations take a cross-process lock, not just a per-process one

Four cores mutate the running stack under a capture→mutate→prove→rollback
transaction: `backendswap` (backend/speculation/tools-mode), `codingmode`, and
`modelswap`. `internal/dashboard` already serialised its own model-switch handler
against itself with `swapMu sync.Mutex` (`internal/dashboard/server.go`), so two
near-simultaneous `POST /api/models/switch` requests could never race.

That mutex protects nothing across a process boundary. `villa backend set`,
`villa speculation set`, `villa tools-mode enter|exit` and `villa coding-mode
enter|exit` each run as a separate CLI process; `villa-dashboard.service` is a
long-lived server. Each of the CLI cores captures the config, mutates, proves,
and — on any failure — restores the captured config verbatim with `SaveConfig
(priorCfg)`. If a dashboard-initiated model switch persists its own config change
DURING one of those windows, the CLI's rollback overwrites it with the config it
captured before the dashboard's write ever happened, silently reverting a switch
the operator just watched succeed. `swapMu` never sees this: it is a different
process's memory.

## Decision

Every stack-mutating core takes an advisory `flock` (`syscall.Flock`, no new
dependency) on one file, `.stacklock`, in the villa config directory, held around
the mutate+prove window (capture through rollback). The lock lives in a new
package, `internal/stacklock`, with two entry points:

- **`Acquire` (blocking)**: `LOCK_EX` with no `LOCK_NB`. Every CLI verb uses this.
  A stack mutation's mutate+prove window is seconds; a second CLI invocation, or
  a concurrent dashboard switch, waits for it rather than being refused for
  something that will clear itself shortly.
- **`TryAcquire` (non-blocking)**: `LOCK_EX|LOCK_NB`, returning `ErrBusy`
  immediately when another process holds it. The dashboard's HTTP handler uses
  this, composed with its EXISTING `swapMu.TryLock()`-then-409 shape: `swapMu`
  still excludes a second in-process request without touching the filesystem;
  `TryAcquire` extends that same "busy → 409, never block" contract across the
  process boundary. Blocking inside an HTTP handler risks a hung request whose
  client has already timed out and retried, doubling the wait for no benefit.

Both acquire the SAME file, so a CLI holder excludes a dashboard `TryAcquire` (it
gets `ErrBusy` immediately) and a dashboard holder excludes a CLI `Acquire` (it
waits).

## Alternatives considered

**A lock file per core** (`.stacklock.backend`, `.stacklock.model`, …), rejected.
The race is between ANY two stack-mutating cores — a `backend set` rollback can
just as easily stomp a `model swap`'s config write as another `backend set`'s —
so a per-core lock would exclude a core from itself while leaving it exposed to
every sibling core, which is the exact hole this ADR closes.

**A lock keyed to `config.toml` itself** (locking the file the cores actually
read/write), rejected. It conflates "excluded from mutating" with "excluded from
reading", and `LoadConfig`/`SaveVilla` are called from places (`villa status`,
`doctor`) that must never block behind a multi-second mutate+prove window. A
dedicated `.stacklock` file locks only the WINDOW, never the read path.

**A retry-with-backoff loop instead of a blocking flock**, rejected for the CLI
side. `syscall.Flock`'s blocking mode already does this at the kernel level with
no polling, no chosen interval to get wrong, and no dependency.

## Consequences

`internal/stacklock` is Linux/Unix-only (`syscall.Flock`), consistent with the
project's Fedora-only v1 constraint; it is noted in the package comment rather
than built behind a tag, since nothing else in this module supports another OS
yet.

The lock is advisory: nothing but the four cores' own callers ever takes it, and a
process that crashes while holding it releases it automatically when the kernel
closes its file descriptors — there is no separate liveness check and none is
needed, because `flock`'s release-on-close IS the liveness check.

A holder that hangs (not crashes) after acquiring — a proof that never returns,
for instance — blocks every other stack mutation for as long as it hangs. This is
accepted as the same shape every `Acquire` call already has (the caller's own
proof step can take as long as the residency drive protocol allows), not a new
failure mode `stacklock` introduces.
