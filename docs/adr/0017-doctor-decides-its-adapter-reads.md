---
status: accepted
---

# Doctor decides from the loaded config; its adapter only reads

`internal/doctor` was a fold over findings, and every decision that gave the fold
its inputs was made in `liveDoctorDeps` (`cmd/villa/doctor.go`), a 240-line
constructor. It loaded the config, then wired a seam only when a subsystem was on
(`AgentToolCall`, `SearchResidencyUnderLoad`, `RunSandboxChecks` and eight others),
so a nil seam meant "off" and the core's `if d.X != nil` was the gate. The same
constructor decided config-vs-disk drift (an absent unit dir is not drift, and the
render takes the villa path the installed websafe unit records, #141), the SBX-02
sandbox network scan, tools-mode drift (`liveToolsDrift`, in `tools_mode.go`) and
agent drift (`liveAgentDrift`). None of that could be tested without a config file
and a unit dir on disk, so the tests asserted only `d.Seam != nil`
(`TestLiveDoctorDepsWires*`). ADR-0012 moved decisions out of cobra bodies; this
moves them out of the adapter that sits beside them. Reading it to move it also
found three faults, recorded below.

## Decision

- **`doctor.Aggregate(cfg, Deps)` takes the loaded config and decides.** Whether a
  subsystem's findings exist is `subsystem.MemoryOn/AgentOn/WebSearchOn/SandboxOn`
  of `cfg`, asked once in the fold. `newDoctor` loads the config once and hands it
  to both `liveDoctorDeps` and `Aggregate`, and `liveDoctorDeps` points the status
  run's `LoadConfig` at it, so one doctor run sees one config. A `Deps` seam that a
  subsystem's absence used to leave nil is now always wired.

- **The seams are raw reads.** `UnitDirExists`, `ReadUnit(name)`,
  `RenderUnits(cfg, hostVilla)` and `RunningVilla` back the drift plan, the
  moved-binary finding, SBX-02 and TMD-01; `AgentBinarySHA`, `ReadCrushConfig`
  and `RenderCrushConfig` back agent drift; `IsActive` backs the embedder's
  active-state; `ReadVerifyState` backs the search egress finding. The rules over
  them live in `internal/doctor/decide.go`: an absent unit dir is a typed-Unknown
  WARN and never drift; an absent unit file is drift, as `orchestrate.Reconcile`
  reads it; the units are rendered with the mount the installed websafe unit
  records, else the running binary; the tool-calling token is derived from the
  inference seam (`toolsFlagToken` and `unitCarriesToolsFlag` moved here from
  `cmd/villa`). The comparison is a small loop in the module rather than
  `orchestrate.Reconcile`, because that function reads the directory itself.

- **`liveDoctorDeps` keeps reads, the four drivers and the preflight bindings.**
  The drivers are the proofs that generate a real workload: embed load, agent tool
  call, agent residency and search residency. They stay seams, are constructed
  unconditionally, and fire only when `Aggregate` calls them for a subsystem that
  is on. `RunMemoryChecks` (`preflight.RunMemory`) and `RunSandboxChecks` stay
  seams because those gates' own probes read the host; the sandbox one is built on
  first use, since it resolves the sandbox image pin. The ROCm gate is no longer a
  seam: the core resolves the image with `inference.BackendFor` and calls
  `preflight.RunROCmForImage`. A backend that `IsROCmFamily` claims and
  `BackendFor` refuses used to abort the verb before the fold; it is now a BLOCK
  `backend` finding, so the refusal is in the report.

- **The search egress finding no longer depends on the status report (#266).**
  Doctor reads the last `villa verify search` result itself and asks
  `status.WebSearchSection` (the exported `webSearchInfo`) for the tri-state, so
  it and `villa status` still apply one freshness rule. When the report errors,
  a fresh non-PASS verdict is a BLOCK FAIL and a fresh PASS is a PASS; an
  unreadable, absent or stale result is a WARN that says which, and names the
  report error. The detail again quotes the verdict, which the tri-state had
  dropped.

- **SBX-02 reads the inference proxy unit, not villa-llama.** Since ADR-0011
  villa-llama joins `villa.network` only and `villa-inferproxy` is the unit on
  both networks, so the check for `Network=villa-sandbox` in the inference unit
  failed on every sandbox-on host. It now scans the proxy unit; the finding names
  it. The v1.11 spec's SBX-02 row still says "the inference unit".

- **`status.Run` guards a nil `Probe`**, as it guards every other optional seam:
  an unprobed profile folds to unknown readiness.

- **The dashboard builds its inference client once per scrape.** `Metrics`,
  `Slots` and `CounterSample` each loaded `config.toml` per call, up to three
  loads per `/api/metrics` request. The server calls them separately, so a scrape
  has no boundary in `cmd/villa`; `scrapeClient` shares one load among reads
  within `scrapeClientTTL` (1 s), under the 2.5 s poll, so the key written by
  `villa up` still reaches the next scrape (#253).

## Rejected

**A `Host` interface with `ReadUnit`, `Hash` and the rest as methods.** It is the
same set of reads with one implementation and one fake, and the repo's `Deps`
shape already is that; an interface adds a name and nothing a test needs.

**Move only the drift plan and leave the gating in the adapter.** The gating is
the larger half of the constructor, and it is what made the tests assert nil-ness.
Moving one half leaves a caller that can still get the other wrong by wiring.

**Have `Aggregate` load the config itself through a seam.** Doctor's callers
already hold the config (`newDoctor` needs it to build the proofs), and a load
inside the fold would give a run two configs, the fault ADR-0016 removed from
status.

**Keep reading the egress answer from the status report and add a fallback for
the errored case.** It keeps two paths to one finding. The report never carried
what the finding needs when it errors, and the read is one call to a store the
status wiring already exposes.

## Consequences

The `villa doctor --json` goldens are byte-identical. `TestLiveDoctorDepsWires*`
are deleted; `internal/doctor` has table tests over a fake unit dir for each
subsystem on and off (asserting the proof is driven then and only then), drift
present, absent, unit-dir-missing and moved-binary, SBX-02 joined and not,
tools drift and agent drift. `cmd/villa` keeps end-to-end checks over a temp XDG
home and a real unit dir, written against the old adapter before the move and
unchanged after it except for the two defects.

The `search-egress` and SBX-02 wording changed; no golden covers either. ADR-0016
said an errored status report yields no `search-egress` finding and that doctor's
gating move was left to #258; both are superseded here.

A `Deps` built by hand must now wire every seam a subsystem's fold calls, since
nothing is nil-gated; `newDoctorDeps` in the tests wires them all. The doctor
test doubles set `cfg`, not a seam, to turn a subsystem on.
