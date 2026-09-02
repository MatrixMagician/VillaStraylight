# Architecture

## System overview

VillaStraylight is a single static Go CLI, `villa` (`cmd/villa`), that acts as the
**control plane** for a strictly-local AI stack on an AMD Strix Halo (gfx1151) /
Fedora host. It detects the host hardware, recommends a memory-fitting
model/quant/context from a versioned catalog, gates installs behind a host-readiness
preflight, renders rootless **Podman Quadlet** units from a single config source of
truth, and orchestrates two integrated OSS containers, **llama.cpp `llama-server`**
(OpenAI-compatible inference, **ROCm 7.2.4 by default with a Vulkan RADV
fallback**) and **Open WebUI** (chat), plus a native, loopback-only Go **control
dashboard**. The Go code is the orchestrator only; the AI services are integrated
upstream images, never rebuilt. The architectural style is a **layered pipeline of pure
cores behind injectable host seams**: every package that makes a decision (`detect`,
`recommend`, `preflight`, `inference.BackendFor`, `orchestrate.Render`,
`orchestrate.Reconcile`, `status`, `modelswap`, `backendswap`, `bench`,
`residentset`) is a pure, table-testable library that returns a typed value; all
host-touching effects (sysfs reads, `podman`, `systemctl`, HTTP probes, downloads,
file writes) are injected as function seams or confined to a small number of
clearly-marked impure files. The command layer (`cmd/villa`) is the only place that
prints, maps verdicts to exit codes, and calls `os.Exit`.

As of **v1.1**, backend choice is a first-class, polymorphic seam. A single resolver,
`inference.BackendFor(cfg.Backend)`, maps the persisted `backend` string
(`""`/`"rocm"` → ROCm 7.2.4, `"rocm-6.4.4"` / `"rocm-6.4.4-rocwmma"` → the additive
digest-pinned ROCm 6.4.4 variants, `"vulkan"` → the Vulkan RADV fallback) to a
`Backend` implementation and is the **only** place a concrete backend is chosen.
Because the empty string resolves to ROCm, `inference.IsROCmFamily`, the single
enumeration of the ROCm-name set that routes the ROCm preflight gate, counts `""` as
ROCm-family too; the two must agree or an unset config would run ROCm while skipping
its bring-up gate. It **fails closed**: an
unknown/typo'd value returns an actionable error rather than silently defaulting to a
privileged backend. Every inference call site depends on the `Backend` interface, so
flipping `backend = "rocm"` in `config.toml` re-routes image, device-passthrough args,
runtime flags, and offload-residency markers with no other change.

A defining cross-cutting contract is **typed-Unknown**: every detected signal is a
`detect.Bytes`/`detect.Bool`/`detect.Int`/`detect.Str` carrying a `Known` flag and
provenance, so "could not measure" is always distinct from a legitimate zero. This
propagates into the PASS / WARN / FAIL verdict vocabulary shared by `preflight`,
`inference`, `status`, `backendswap`, and `bench`: an unevaluable check degrades to
WARN rather than a false pass or a false hard-block. A defining v1.1 guarantee is that
this verdict is **backend-neutral**: offload-assert keys on the active backend's
`ResidencyProof()` markers, so CPU fallback is positively classified FAIL regardless of
backend, never a false-green.

As of **v1.8**, a pin is likewise two values rather than one. Every component the
stack runs is pinned by digest, and those pins used to be compile-time constants, so
"what villa vetted" and "what this host runs" could not differ and could not be
distinguished. They are now separate: the **vetted** pin is compiled into `pins.Table`
and cannot be absent, the **effective** pin lives in `pinstate` on the host and
routinely is, and `pinresolve` is the single place they meet. `villa update` is the
verb that moves one to the other, transactionally, proving each subsystem before and
after it changes it. The same typed-Unknown discipline governs its report: a check
that could not be conducted is a `Reject`, which is a different claim from "you are up
to date" and is never rendered as one.

## Component diagram

```mermaid
graph TD
    CLI["cmd/villa (cobra CLI)<br/>detect · recommend · preflight · model · install<br/>up · down · restart · logs · status · dashboard<br/>backend · bench · update · uninstall"]

    CLI --> detect["internal/detect<br/>HostProfile probe (typed-Unknown)<br/>+ ROCm readiness"]
    CLI --> catalog["internal/catalog<br/>embedded model catalog + fit dims"]
    CLI --> recommend["internal/recommend<br/>Pick: memory-fitting model (pure)"]
    CLI --> preflight["internal/preflight<br/>host-readiness checks (pure)<br/>+ ROCm gate (rocm-policy.json)"]
    CLI --> download["internal/download<br/>verified resumable GGUF pull"]
    CLI --> config["internal/config<br/>config.toml store (0600)"]
    CLI --> orchestrate["internal/orchestrate<br/>Render → Reconcile → WriteUnits + systemd seam"]
    CLI --> modelswap["internal/modelswap<br/>guarded swap core"]
    CLI --> residentset["internal/residentset<br/>resident-set admission control (pure)"]
    CLI --> backendswap["internal/backendswap<br/>transactional capture→prove→rollback"]
    CLI --> bench["internal/bench<br/>honest A/B core (pure)"]
    CLI --> status["internal/status<br/>read-model aggregation"]
    CLI --> dashboard["internal/dashboard<br/>loopback dashboard"]
    CLI --> updateflow["internal/updateflow<br/>per-subsystem transaction (pure)<br/>prove current → capture → mutate → prove → commit"]
    CLI --> updatecheck["internal/updatecheck<br/>the read-only report (pure)"]
    CLI --> pinresolve["internal/pinresolve<br/>vetted vs effective, resolved"]

    recommend --> detect
    recommend --> catalog
    preflight --> detect
    CLI --> resolver["inference.BackendFor(name)<br/>single fail-closed resolver"]
    resolver --> brc["backendROCm<br/>(7.2.4, default)"]
    resolver --> bvk["backendVulkan<br/>(RADV, fallback)"]
    bvk -.implements.-> inference["internal/inference<br/>Backend / Runner / ResidencyProof seam"]
    brc -.implements.-> inference
    orchestrate --> inference
    orchestrate --> config
    modelswap --> catalog
    modelswap --> config
    modelswap --> download
    modelswap --> orchestrate
    residentset -.cmd composes.-> recommend
    residentset -.cmd composes.-> orchestrate
    backendswap -.cmd composes.-> orchestrate
    backendswap -.cmd composes.-> config
    bench -.--ab composes.-> backendswap
    status --> orchestrate
    status --> inference
    status --> detect
    dashboard --> status
    dashboard --> modelswap
    dashboard --> metrics["internal/metrics<br/>llama-server /metrics + /slots scrape"]

    pinresolve --> pins["internal/pins<br/>VETTED pins, compiled in"]
    pinresolve --> pinstate["internal/pinstate<br/>EFFECTIVE pins + retained previous"]
    updatecheck --> pinresolve
    updatecheck --> manifestverify["internal/manifestverify<br/>ed25519 + serial + expiry + allowlist"]
    manifestverify --> manifest["internal/manifest<br/>pin-manifest wire format"]
    updatecheck -.composes.-> updatefetch["internal/updatefetch<br/>the ONE outbound GET"]
    updateflow --> pinstate
    updateflow -.cmd composes.-> orchestrate
    updateflow -.cmd composes.-> prune["internal/prune<br/>reference-counted image removal"]
    updateflow -.cmd composes.-> snapshotprune["internal/snapshotprune<br/>data-snapshot retention"]
    orchestrate --> pinresolve

    orchestrate -.writes.-> quadlet["~/.config/containers/systemd<br/>*.container/.network/.volume"]
    orchestrate -.systemctl --user.-> systemd["rootless user systemd"]
    inference -.podman run.-> llama["llama-server (Vulkan/ROCm, gfx1151)"]
    systemd -.-> owui["Open WebUI"]
    systemd -.-> dashsvc["villa dashboard service"]
```

Note on the GPU backend seam: backend selection is funneled through the single
`inference.BackendFor(name)` resolver (`internal/inference/backend.go`), which is
fail-closed; every other site depends on the `Backend` interface, never on a concrete
`backendVulkan`/`backendROCm`. The only files permitted to hold imperative backend
literals (the container image digest, `/dev/dri` device passthrough, `--group-add
keep-groups`, the loopback publish, the mandatory `llama-server` flags, and the ROCm
`HSA_OVERRIDE_GFX_VERSION` override) are the per-backend implementations
`internal/inference/backend_vulkan.go` and `internal/inference/backend_rocm.go`, plus
the AMD detection seam `internal/detect/gpu_amd.go`. A future Metal backend slots in as
a third sibling `Backend` implementation behind `BackendFor` without touching callers.

## Data flow

The canonical flow is the `villa install` lifecycle, `install.Run` in
`internal/install/flow.go`, which composes the pure cores in order and returns a
typed `Result` (ADR-0005). Since v1.6 install is **transactional** (ADR-0003): it
captures the prior state before the first mutation and restores it if a mutation
or a proof fails, so a failed install never leaves a running-but-unproven stack.
The whole flow, gate resolution, plan assembly, the mutate-and-start ORDER and the
transaction, lives in `internal/install`; the command tier (`cmd/villa/install.go`)
wires the live host into `install.Deps` and renders the narration lines the flow
emits through its `Emit` seam.

1. **Detect**: `detect.Probe()` reads `/proc/meminfo`, `/sys/class/drm`,
   `/sys/module/ttm/parameters/pages_limit`, `/proc/sys/kernel/osrelease`, and the AMD
   GPU seam to assemble a `detect.HostProfile` (CPU/arch, iGPU name + gfx id, Vulkan
   ICD, DRI nodes, ROCm presence, total RAM, and the usable GTT/unified-memory
   envelope). It never errors and never panics: every field degrades to a typed
   Unknown.
2. **Recommend**: `recommend.Pick(profile, catalog, overrides)` (a pure function)
   chooses the largest auto-eligible model whose `weight_bytes + kv_cache@ctx +
   headroom ≤ usable_envelope`. It skips bootstrap and `unified_memory_safe:false`
   entries, defaults the backend to `rocm` (falling back to `vulkan` only when the
   host is confidently not ROCm-ready), re-validates manual overrides, and
   degrades to a conservative RAM-fraction floor (or refuses) when the envelope is
   Unknown. Every term of the fit inequality is surfaced on the `Recommendation`.
3. **Preflight gate**: `preflight.RunWithResources(profile, req)` runs the
   host-readiness checks against the concrete model's `weights + KV + headroom`
   requirement: Vulkan iGPU present, podman rootless ready, user lingering, SELinux
   container-device boolean, free disk/memory floors, kernel and firmware baselines.
   Each check is BLOCK- or WARN-tier; a BLOCK-tier check that cannot be evaluated
   downgrades to WARN. BLOCK gaps are offered as **per-step consented privileged
   host-prep** (`setsebool`, `loginctl enable-linger`), never run silently, and
   overridable with `--force`.
3a. **Resolve gates**: `install.ResolveGates` folds the persisted config with this
   run's opt-in flags into one answer for the four optional subsystems, read by the
   preflight gate, the pre-stage step, the render and the proofs. A flag turns a
   subsystem ON and persists that choice; nothing on the command line turns one OFF.
3b. **Capture**: the prior config, the unit files, and which services were running.
   Model weights are deliberately NOT captured, so a failed install leaves them and a
   retry does not re-download tens of gigabytes.
4. **Ensure model + persist config**: the recommended GGUF is auto-pulled if absent
   via `internal/download` (HEAD-verify → resumable `.part` → per-shard SHA256 →
   atomic rename), then the chosen `model/quant/ctx/backend` plus the
   dashboard/chat defaults are written to `config.toml` via the 0600 traversal-guarded
   `config.SaveVilla`, **before** any unit work, so config is the single source of
   truth.
5. **Render**: `orchestrate.Render(RenderInput)` (pure) builds five Quadlet units,
   the inference `.container`, `villa.network`, `villa-models.volume`, and the Open
   WebUI `.container` + `.volume`. The inference unit's imperative fields are obtained
   **through** `Backend.Image()` and `Backend.ContainerArgs(spec)` and mapped to
   Quadlet keys, never re-typed.
6. **Reconcile**: `orchestrate.Reconcile(units, unitDir)` (pure) does a sha256
   render-vs-disk diff, classifying each unit Changed or Unchanged. An empty Changed
   slice is a true no-op, the idempotency core.
7. **Write + start**: `orchestrate.WriteUnits` writes each changed unit atomically
   (sibling temp + fsync + `os.Rename`, refusing any path outside the unit dir), then
   the `orchestrate.Systemd` seam runs `systemctl --user daemon-reload`, starts
   `villa-llama.service`, then `villa-openwebui.service`. The native control-dashboard
   unit is reconciled separately into `~/.config/systemd/user`, enabled for
   boot-survival, and started.
8. **Readiness poll + proofs**: the command layer polls the loopback inference
   endpoint's `/health` (503 = still loading → keep polling; timeout → WARN, never a
   confident FAIL), then runs the enabled subsystems' readiness proofs, then prints
   the inference endpoint, the chat URL, and the dashboard URL.
9. **Rollback on failure**: any failure after the capture, INCLUDING a failed proof,
   restores the host: services install started are stopped, units it wrote are
   restored or removed, and the config is restored or removed. On a clean host it
   restores to nothing; on a re-install the prior stack is restored, so a failed
   upgrade leaves the working install running. A rollback step that itself fails is
   reported honestly as incomplete rather than claimed as a clean restoration.

The day-to-day verbs (`up`/`down`/`restart`/`logs` in `cmd/villa/lifecycle.go`) reuse
the same Render→Reconcile→WriteUnits→Systemd core, so hand-editing `config.toml` and
re-running `up`/`restart` converges exactly the changed units. `villa status`
(`internal/status`) and the dashboard (`internal/dashboard`) fold the **same** status
read-model, never a fork, to report per-service active state, mapped `/health`, and
the running-server GPU-offload verdict (keyed on the active backend's residency markers),
with a worst-wins overall PASS/WARN/FAIL.

A second v1.1 flow is the **transactional backend switch** (`villa backend set
<rocm|rocm-6.4.4|rocm-6.4.4-rocwmma|vulkan>`, `cmd/villa/backend.go`), driven by the pure `backendswap.Run(Deps,
target)` state machine (`internal/backendswap/backendswap.go`). It clones the proven
`modelswap` forward skeleton (fit-guard first, persist-before-unit-work,
restart-inference-only) and wraps it in a transactional frame:

1. **Capture**: the verbatim prior `villa-llama.container` bytes and prior
   `VillaConfig` are snapshotted **strictly before** any mutation.
2. **Fit + ROCm preflight guard**: the preserved model is re-checked against the
   target backend's envelope, and (for ROCm) the `preflight.RunROCm*` bring-up gate
   runs; a refuse-with-remediation aborts with zero mutation.
3. **Mutate**: persist the new backend to config, then reconcile/write the inference
   unit and restart only the inference service.
4. **Prove**: the cutover is gated on an injected `Prove` verdict: a real
   generation-probe **and** a positive residency proof over the now-running server
   (`inference.PollHealth` + `GenerationProbe` + `RunningOffloadVerdict`).
5. **Rollback**: any mutate error or non-pass verdict restores the verbatim captured
   unit + config and re-readies best-effort, so a failed or degraded switch is a no-op
   to the running stack.

`backendswap` is deliberately literal-free of backend marker tokens and imports neither
`internal/inference` nor `internal/detect`; the prove verdict is `prove.Verdict`
(`internal/prove`, shared with `codingmode` and `backup` since v1.6; it imports nothing,
which is what keeps the cores free of those imports) and the real markers arrive only
through the injected `Prove` seam wired in `cmd/villa`.

The drive behind that seam is `internal/residency`: `Prove` for the idle-cutover proof and
`UnderLoad` for the doctor proofs that must sample while a real workload runs. The
Unknown-versus-negative rule is part of that module's interface: an unevaluable signal is a
Warn, a contradicted one a Fail, and `residency.Cutover` is the single named boundary where
the transactional callers collapse both to non-pass.

The **honest A/B benchmark** (`villa bench [--ab]`, `cmd/villa/bench.go`) is the pure
`bench.Run(ctx, Deps, Spec)` core (`internal/bench/bench.go`). Without `--ab` it
measures the current backend non-disruptively; with `--ab` it **composes**
`backendswap.Run` to flip backends rather than re-implementing any switching, so the
switching logic stays locked in the one transactional core. Each measured run is gated
on a residency proof (`RunningOffloadVerdict`) so only runs that genuinely executed on
the GPU count toward the median/stddev `Stats` and the comparative `ABResult`.

## Key abstractions

- **`detect.HostProfile`** + the typed `Bytes`/`Bool`/`Int`/`Str` optionals
  (`internal/detect/detect.go`, `internal/detect/value.go`), the read-only host
  description and the typed-Unknown spine that every downstream decision consumes.
- **`recommend.Pick`** (`internal/recommend/recommend.go`), the pure
  memory-fit selector; returns a `Recommendation` exposing every term of
  `weight + KV + headroom ≤ envelope`.
- **`preflight.CheckResult` / `preflight.Run` / `RunWithResources`**
  (`internal/preflight/preflight.go`), the reusable host-readiness gate; pure,
  returns typed BLOCK/WARN-tier PASS/WARN/FAIL results with remediation + provenance.
- **`inference.BackendFor`** (`internal/inference/backend.go`), the single,
  fail-closed polymorphism point mapping the config `backend` string to a `Backend`.
  `""`/`"rocm"`/`"rocm-6.4.4"`/`"rocm-6.4.4-rocwmma"` → `backendROCm`, `"vulkan"` →
  `backendVulkan`; any other value is an error, never a silent default. Every backend
  call site routes through here.
- **`inference.Backend`** and **`inference.Runner`** (`internal/inference/inference.go`)
  are the backend-neutral seam. `Backend` (`Name`/`Image`/`ContainerArgs`/`ResidencyProof`)
  and `Runner` (start/stop/health/endpoint/logs) isolate every GPU/podman literal; the
  default ROCm implementation lives in `backend_rocm.go` (image-parameterized across
  the three pinned digests), the Vulkan RADV fallback in `backend_vulkan.go`, the podman runner in `runner_podman.go`.
- **`inference.ResidencyMarkers`** (`internal/inference/inference.go`), the
  backend-owned, log-shape-only descriptor returned by `Backend.ResidencyProof()`. It
  carries only marker literals (device token e.g. `Vulkan0`/`ROCm0`, device label,
  start-log prefix, fault string, software-renderer-reject flag), so both offload-assert
  scrape paths (`offload.go`, `running_offload.go`) are parameterized by it: a new
  backend supplies its own markers without re-rolling the offload math.
- **`inference.Verdict`** + `RunningOffloadVerdict` (`internal/inference/inference.go`,
  `running_offload.go`), the dual-assert GPU-offload result (log-scrape keyed on
  `ResidencyMarkers` + sysfs GTT delta) that catches silent CPU fallback; the shared,
  backend-neutral PASS/WARN/FAIL value the CLI, dashboard, `backendswap`, and `bench`
  render.
- **`orchestrate.Render` / `Reconcile` / `WriteUnits` / `Systemd`**
  (`internal/orchestrate/render.go`, `reconcile.go`, `systemd.go`), the pure Quadlet
  renderer, the sha256 idempotency reconciler, the atomic unit writer, and the
  fixed-arg `systemctl`/`loginctl`/`journalctl` lifecycle seam.
- **`config.VillaConfig`** + `LoadVilla`/`SaveVilla` (`internal/config/villaconfig.go`)
  is the persisted `config.toml` selection (model/quant/ctx/backend + dashboard/chat
  ports), written 0600 with a path-traversal guard; the single source of truth the
  units render from.
- **`catalog.Catalog` / `catalog.Model`** (`internal/catalog/catalog.go`), the embedded,
  schema-versioned model catalog carrying the per-model KV-fit dimensions, the
  `unified_memory_safe` flag, and per-shard download metadata.
- **`status.Report` / `status.Run` / `status.Aggregate`** (`internal/status/status.go`)
  is the JSON-neutral read-model the CLI and dashboard share; folds per-service
  active/health/offload into a worst-wins overall verdict and records loopback posture.
- **`preflight.RunROCm` / `RunROCmWithPolicy`** (`internal/preflight/checks_rocm.go`),
  the ROCm bring-up gate (gfx1151 confirm, kernel/firmware floors,
  `HSA_OVERRIDE_GFX_VERSION` viability), driven by policy data in the `go:embed`-ed
  `rocm-policy.json` (`internal/preflight/floors.go`) so deny-lists are data, not code.
- **`detect.computeROCmReadiness` / `ROCmReadiness`** (`internal/detect/readiness_rocm.go`)
  is the pure, typed-Unknown ROCm-viability summary (gfx id, kernel/firmware baselines,
  override viability) surfaced by `detect` and the backend-set preflight.
- **`modelswap.Run`** (`internal/modelswap/modelswap.go`), the guarded swap core where
  ordering is the security contract: resolve-through-catalog → fit-guard refuse →
  auto-pull → persist config → reconcile/write → restart only the inference service.
- **`residentset.Admit`** + `Slot` / `Set` / `Plan` / `Policy` / `Refusal`
  (`internal/residentset/admit.go`), the admission control for holding several models
  resident at once instead of restarting the inference unit to swap one for another
  (`villa model resident ls|add|rm`). Returns `NoOp` when the workload is already
  resident, a plain `Add` when it fits, `Add`+`Evict` of least-recently-used
  non-`Primary` slots when it fits only after eviction, or a `Refusal`. It does no host
  I/O, reads no clock (`Slot.Order` is caller-supplied recency), and mutates neither the
  `Set` nor its `Slots`, so a caller may run it speculatively to preview a plan.
  Mirroring the `Render`/`Reconcile` split it is the planning half only; there is no
  impure "carry out the `Plan`" counterpart in the package. It decides what MAY join and
  nothing else: `recommend.Pick` owns what a candidate COSTS and
  `orchestrate.ResidentUnitName` owns what its unit is called. `cmd/villa/model_resident.go`
  composes all three under the shared `install` transaction and re-derives none of them.
- **`backendswap.Run`** + `Deps` / `ProveVerdict` / `Result`
  (`internal/backendswap/backendswap.go`), the pure, Deps-injected transactional
  capture→mutate→prove→rollback state machine for `villa backend set`. Imports neither
  `inference` nor `detect`; markers and the real prove verdict arrive only through the
  injected `Prove` seam wired in `cmd/villa/backend.go`.
- **`bench.Run`** + `Spec` / `Stats` / `ABResult` / `Result`
  (`internal/bench/bench.go`), the pure honest-A/B benchmark core. `--ab` composes
  `backendswap.Run` (never re-implements switching); each kept run is residency-proven so
  CPU-fallback runs are excluded from the median/stddev comparison.
- **`dashboard.Server`** (`internal/dashboard/server.go`), the loopback-only
  `net/http` control dashboard; constructed to refuse any non-loopback bind, serves a read-only
  JSON API over the shared `status` core plus the `metrics` perf scrape, with the one
  sanctioned mutation (`POST /api/models/switch`) routed through `modelswap.Run`.
- **`pins.Table`** and **`pinstate.State`** (`internal/pins/pins.go`,
  `internal/pinstate/store.go`), the two halves of what a pin is. The VETTED pin is
  compiled in and is a build-time fact that cannot be absent; the EFFECTIVE pin is on
  the host and is a runtime fact that routinely is. They live in separate packages
  because they FAIL differently, and merging them would blur a thing that cannot fail
  with a thing that does. `pinstate` also carries the retained known-good `Previous`
  per subsystem, and inverts `jsonstore`'s absent-means-empty reading in the two places
  where the zero value points the wrong way: the anti-downgrade serial floor and the
  prune reference set (ADR-0004).
- **`pinresolve.Resolve`** (`internal/pinresolve/resolve.go`), where the two meet, and
  the single answer to "what should this component run?". Pure over an injected table
  plus state; rendered units derive their image through it rather than through a
  constant, and a test fails the build if a cmd verb calls `orchestrate.Render`
  directly.
- **`manifest.Manifest`** + **`manifestverify.Verify`** (`internal/manifest`,
  `internal/manifestverify`), the signed pin-manifest wire format and the four gates it
  must clear: ed25519 signature over the **verbatim** bytes, a serial at or above the
  anti-downgrade floor, an unexpired `valid_until`, and an allowlist that lets a
  manifest supply new *values* only. It can never introduce a component, a registry
  host, a pin shape, or a URL template. A signature proves authorship, not currency,
  which is why expiry and serial are separate checks rather than implied by it.
- **`updatecheck.Run`** + **`updatefetch.Fetch`** (`internal/updatecheck/check.go`,
  `internal/updatefetch/fetch.go`), the read-only report and the one outbound request
  a check makes. The report's load-bearing value is `Reject`: "villa could not check"
  is a different claim from "you are up to date", and only one of them was observed.
- **`updateflow.Run`** + `Deps` / `Target` / `Capture` / `Outcome`
  (`internal/updateflow/updateflow.go`), the pure, Deps-injected per-subsystem
  transaction. It clones `backendswap`'s frame and adds the two things updating needs
  that a swap did not: it proves the CURRENT state first, so a pre-existing failure is
  a refusal rather than an update failure villa did not cause; and for a subsystem that
  owns persistent state (`subsystem.OwnsPersistentState`) the mutation becomes a stopped
  window, stop → snapshot → mutate → start, whose rollback restores the data as well
  as the pin. The stop is load-bearing: a volume exported from under a running service
  is a torn copy.
- **`prune.Decide`** and **`snapshotprune.Decide`** (`internal/prune/prune.go`,
  `internal/snapshotprune/snapshotprune.go`), the only code in this project that
  deletes a container image, and the only code that deletes a data snapshot. Both are
  pure decisions over the store, both refuse when the store is unreadable (an empty
  reference set reads as "delete freely", which is the one reading that could break a
  running stack), and a failure in either is a WARN rather than a rollback: the single
  place fail-soft is correct, because cleanup runs after the update has already
  succeeded.

## Directory structure rationale

The repository follows the standard Go `cmd/` + `internal/` split: every package under
`internal/` is a pure or seam-injected library with no CLI behavior, and `cmd/villa`
is the only consumer that prints and exits. This keeps decision logic exhaustively
table-testable and lets the same cores back both the CLI and the dashboard. The v1.1
`backendswap` and `bench` packages follow the same discipline: pure, Deps-injected
cores with their live host wiring confined to `cmd/villa`.

```
cmd/
  villa/              The cobra CLI: one file per subcommand (detect, recommend,
                      preflight, model, install, up/down/restart/logs, status,
                      dashboard, backend, bench, uninstall) plus root.go and the
                      live-wiring of each package's injectable seams.
internal/
  detect/             Host probe → typed-Unknown HostProfile; AMD GPU seam in
                      gpu_amd.go; ROCm-viability summary in readiness_rocm.go.
  catalog/            Embedded, schema-versioned model catalog + KV-fit dimensions.
  recommend/          Pure memory-fit model selector (Pick) over detect + catalog.
  preflight/          Pure, reusable host-readiness gate (BLOCK/WARN-tier checks);
                      ROCm bring-up gate driven by embedded rocm-policy.json.
  download/           Verified, resumable, per-shard-checksummed GGUF downloader.
  config/             config.toml store (0600, traversal-guarded); the source of truth.
  inference/          BackendFor resolver + Backend/Runner/ResidencyProof seam:
                      ROCm (default) + Vulkan backends, podman runner, offload assert.
  orchestrate/        Pure Quadlet Render + sha256 Reconcile + atomic WriteUnits +
                      systemd seam; Open WebUI managed-service render path.
  modelswap/          Guarded swap core (ordering-is-the-security-contract).
  backendswap/        Transactional backend-switch core (capture→prove→rollback).
  bench/              Pure honest-A/B benchmark core (--ab composes backendswap).
  status/             Shared read-model aggregation (CLI + dashboard, never forked).
  metrics/            Bounded llama-server /metrics + /slots scrape for the perf panel.
  dashboard/          Loopback-only control dashboard backend + embedded UI.
  llm/                OpenAI-compatible SSE + non-streaming client (the bench timings source).
```
