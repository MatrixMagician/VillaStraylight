# Phase 22: Control-Plane Fit + Host Gate - Context

**Gathered:** 2026-06-10
**Status:** Ready for planning

> Captured in `--auto` mode (single pass, via `/gsd-progress --next --auto`).
> Each decision below was auto-selected as the recommended option, grounded in
> ROADMAP Phase 22 (goal + 3 success criteria), REQUIREMENTS CTRL-01/03/06,
> the pinned Phase-18 spike decisions (D-08 footprint), and the Phase-19
> on-hardware finding that `villa-embed` serves on CPU (no GPU passthrough).
> Review before planning.

<domain>
## Phase Boundary

Make the **control plane memory-stack-aware** so the v1.3 stack "runs healthy
after install" on the gfx1151 unified-memory envelope:

1. **CTRL-01** — `villa recommend` reserves the embedding-model footprint in the
   unified-memory fit math *before* the chat-model fit (envelope shrinks first),
   surfaced as an append-only `recommend` field + schema bump.
2. **CTRL-06** — `villa preflight` gates host fitness for the memory stack
   (vector-index disk space, embedder memory headroom) with refuse-with-remediation.
3. **CTRL-03** — `villa doctor` folds memory-stack health into its existing
   PASS/WARN/FAIL contract: services up, vector-disk/headroom, and an
   **offload-asserting residency proof that the chat model survives an
   embedding/import workload** (silent/partial CPU fallback = FAIL, never false-green).

**In scope:**
- `internal/recommend` fit-math change + append-only `Recommendation` field(s) +
  `recommendSchemaVersion` bump + recommend golden re-freeze (this is the
  *recommend* contract — distinct from the `status.Report` 2→3 evolution
  reserved for Phase 23).
- New preflight check(s) for the memory stack, active only when `memory_enabled=true`.
- New doctor findings for the memory stack, including the under-load residency proof.
- On-hardware verification on the live Strix Halo box (real embed load, real
  GTT scrape).

**Out of scope (later phases / deferred — do NOT build here):**
- `status`/dashboard memory rows, `status.Report` schema 2→3, backup/restore of
  the Qdrant volume, memory-aware `model swap` — **Phase 23**.
- Giving `villa-embed` GPU passthrough (`AddDevice=/dev/dri`) — orchestrate/unit
  change, not a control-plane fit change (see D-02; backlog if ever wanted).
- Any auto-remediation (auto-shrinking ctx, auto-disabling memory) — recommend
  recommends, preflight refuses-with-remediation, doctor reports; none mutate config.

</domain>

<decisions>
## Implementation Decisions

### Envelope reservation semantics (CTRL-01)
- **D-01:** The reservation happens **inside `recommend.Pick` by shrinking the
  resolved envelope BEFORE the chat-model fit** (`resolveEnvelope` result minus
  the embedding reservation), only when `memory_enabled=true` in the loaded
  config. Memory disabled → reservation is zero and the pick math is unchanged.
  The reservation value comes from the existing pure `internal/memory.Footprint(modelID)`
  (Phase 18 D-08: 512 MiB pinned for `nomic-embed-text-v1.5`) — never a literal
  re-typed in `recommend`. `Pick`'s signature grows the memory inputs explicitly
  (pure-core rule: no config loads inside the core).
  `[auto] Reservation — Q: "shrink envelope before fit vs subtract inside pickBest vs post-hoc note?" → Selected: "shrink envelope before chat-model fit, gated on memory_enabled" (recommended; SC#1 says 'envelope shrinks first')`
- **D-02:** **Typed-Unknown footprint never silently reserves 0.** If
  `memory_enabled=true` but `Footprint` returns Unknown (unrecognized embedding
  model id), reserve the **conservative default constant (512 MiB)** and append a
  DEGRADED-style note naming the unrecognized model — mirroring the existing
  degraded-envelope note pattern. Honest over optimistic: never under-reserve.
  `[auto] Unknown footprint — Q: "refuse vs reserve 0 + note vs conservative default + note?" → Selected: "conservative default reservation + honest note" (recommended; honesty-by-construction, never under-reserve)`
- **D-03:** Contract evolution is **append-only + schema bump**:
  `recommendSchemaVersion` 1→2, new field(s) surfacing the reservation (e.g.
  embedding reservation bytes + whether memory was considered) — exact field
  names planner's call. Recommend goldens re-frozen ONCE, intentionally, with
  `-update`. This does NOT touch `status.Report` (its single 2→3 evolution stays
  reserved for Phase 23 — ROADMAP's "exactly ONE byte-frozen contract evolution"
  line refers to the status/dashboard contract).
  `[auto] Contract — Q: "bump recommend schema vs notes-only surfacing?" → Selected: "append-only field + schema bump 1→2, goldens re-frozen once" (recommended; SC#1 mandates it)`

### Embed GPU posture + footprint constant (CTRL-01)
- **D-04:** **`villa-embed` stays CPU-only in v1.3** (Phase 19 shipped it with no
  `AddDevice=/dev/dri`; embed resident GTT delta ≈ 0). The ~512 MiB reservation
  still subtracts from the **unified envelope** — on Strix Halo, CPU-resident
  embed memory and GTT come from the same physical pool, so the fit math is the
  same either way. Do NOT add GPU passthrough to the embed unit in this phase.
  `[auto] Embed posture — Q: "keep CPU-only vs add GPU passthrough + measure?" → Selected: "keep CPU-only; reservation against the unified envelope" (recommended; unit change is out of scope, unified memory makes the math equivalent)`
- **D-05:** The **512 MiB constant stays the planning figure** (conservative,
  never under-reserves — D-08's own rationale). Phase-19's deferred "measure on
  hardware in Phase 22" lands as a **verification/UAT observation** (measure
  resident usage under embed load on the live box and record it in the phase
  summary), NOT as a code change to the constant — unless measurement shows the
  constant UNDER-reserves, which would be a finding requiring the constant raised.
  `[auto] Constant — Q: "refine constant from measurement vs keep conservative?" → Selected: "keep 512 MiB; measure + record during verification; raise only if under-reserving" (recommended)`

### Preflight memory-stack gate (CTRL-06)
- **D-06:** New topic-grouped check file (e.g. `internal/preflight/checks_memory.go`)
  mirroring the `checks_resources.go` pattern: injectable statfs/meminfo seams,
  BLOCK tier with refuse-with-remediation text, typed-Unknown → WARN when a
  probe can't evaluate. **Gates run ONLY when `memory_enabled=true`** — when the
  flag is off the checks are not emitted at all (a v1.2-shaped install sees
  byte-identical preflight output; consistent with how the memory stack is
  opt-in everywhere else).
  `[auto] Gate shape — Q: "always-on checks vs memory_enabled-gated; new file vs extend checks_resources?" → Selected: "new checks_memory.go, emitted only when memory_enabled=true" (recommended; opt-in discipline + topic-grouped file convention)`
- **D-07:** Two gates: **(a) vector-index disk** — free space at the rootless
  Podman volume storage root (where the Qdrant named volume lives) must clear a
  floor; **(b) embedder memory headroom** — free memory must clear the embedding
  reservation (same `memory.Footprint` source as D-01). Threshold constants and
  the exact statfs target path are planner's/researcher's call (research
  confirms the rootless volume storage path resolution — never hardcode
  `~/.local/share/containers/...` without checking `podman system info` /
  graphroot semantics). Both BLOCK on confident failure, WARN on Unknown.
  `[auto] Gates — Q: "what exactly gates: disk only vs disk + headroom?" → Selected: "both, per REQUIREMENTS CTRL-06 wording; thresholds planner's call" (recommended)`

### Doctor memory fold + residency-under-load proof (CTRL-03)
- **D-08:** Doctor (when `memory_enabled=true`) adds memory-stack findings via
  its existing Deps-fold pattern: **services-up rows** for `villa-qdrant` +
  `villa-embed` (reuse the systemd/health seams the install/status paths already
  use), the **vector-disk/headroom checks** (reuse the Phase-22 preflight checks
  through doctor's existing preflight fold — no duplicate logic), all folded into
  the existing PASS/WARN/FAIL exit contract. Memory disabled → no memory
  findings emitted (mirror D-06).
  `[auto] Doctor fold — Q: "new standalone checks vs fold preflight + service rows?" → Selected: "reuse preflight checks via the existing fold + service-up rows" (recommended; composition over re-implementation)`
- **D-09:** The **residency proof drives a REAL embedding workload** and asserts
  the CHAT model stays GPU-resident under it: generate N embedding requests
  against `villa-embed` `/v1/embeddings` (reuse the Phase-19/20 proof mechanism
  in `cmd/villa/install_memory.go` — `evalMemoryProof`'s reach path; villa-embed
  has NO host port, so the drive goes through the proven container-network
  mechanism, fixed-arg, shell-free), while scraping the chat model's residency
  with the existing `internal/inference` offload machinery (`RunningOffload` /
  GTT-delta + log-scrape, per the no-silent-CPU-fallback constraint).
  **Semantics:** confident partial/full CPU fallback of the CHAT model during
  embed load = **FAIL**; unevaluable signal (services down, scrape failed,
  memory stack not running) = **typed-Unknown WARN ("could not evaluate")** —
  never a false-green PASS.
  `[auto] Residency proof — Q: "real embed load vs synthetic memory pressure vs static check?" → Selected: "real /v1/embeddings load via the proven Phase-19/20 drive + existing RunningOffload assert" (recommended; SC#3 says 'under embedding/import workload')`
- **D-10:** Doctor's under-load proof is **opt-in via the existing doctor
  invocation shape** (it already runs host-touching probes); if the chat stack
  or memory stack is down, the proof degrades to typed-Unknown WARN rather than
  starting services itself — doctor diagnoses, it never mutates state.
  `[auto] Doctor mutation — Q: "doctor starts services to test vs degrade to Unknown?" → Selected: "never mutate; degrade honestly" (recommended; doctor is read-only by design)`

### Claude's Discretion
- Exact new `Recommendation` field names/JSON keys, check IDs (PRE-xx numbering),
  remediation strings, threshold constants (disk floor), N embed requests and
  request payload for the load proof, golden fixture layout — planner's call
  within D-01..D-10.
- Whether the preflight headroom check reuses `checkResources`'s meminfo seam or
  gets its own — planner's call (no duplicate probe logic either way).
- Sequencing of plans (recommend fit vs preflight vs doctor) — planner's call;
  recommend fit (CTRL-01) has no dependency on the other two.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` → **Phase 22** section — goal + 3 success criteria
  (SC#1 envelope-shrinks-first + append-only schema bump; SC#2 preflight
  refuse-with-remediation; SC#3 doctor fold + offload-asserting residency under
  embedding load).
- `.planning/REQUIREMENTS.md` — **CTRL-01, CTRL-03, CTRL-06** definitions.
- `.planning/PROJECT.md` — v1.3 milestone goal; "runs healthy after install" bar;
  out-of-scope list.

### Prior-phase contracts this phase builds on
- `.planning/phases/18-memory-spine-config-core-embeddings-wiring-research-spike/18-DECISIONS.md`
  — **D-08 pinned**: `nomic-embed-text-v1.5`, 768-dim, Q8_0, **~512 MiB
  conservative footprint reservation** ("feeds the D-03 fit math"; Phase 22 is
  the named downstream consumer for CTRL-01).
- `.planning/phases/19-vector-store-local-embeddings-services/19-03-SUMMARY.md`
  — **on-hardware finding:** `villa-embed` has NO GPU passthrough (no
  `AddDevice=/dev/dri`), serves on CPU, embed resident GTT delta ≈ 0; the
  512 MiB conservative reservation remains the planning figure "pending a
  GPU-resident embed measurement in Phase 22 (CTRL-01)" → resolved by D-04/D-05.
- `.planning/phases/21-conversational-recall-indexer/21-CONTEXT.md` — deferred
  item routed here: "recommend/preflight/doctor awareness of index size/headroom
  — Phase 22"; the vector disk being gated includes the recall Knowledge index
  living in the Qdrant volume.

### Code touchpoints (reuse/extend; primary edit sites)
- `internal/recommend/recommend.go` — `Pick` (:123), `recommendSchemaVersion = 1`
  (:29), `finalizeRecommendation`, `pickBest`/`pickOverride`; `envelope.go` —
  `resolveEnvelope` (:45). The reservation subtracts after envelope resolution,
  before fit.
- `internal/memory/footprint.go` — `Footprint(modelID) detect.Bytes` (:38),
  `embedFootprints` pinned map — the ONLY footprint source (D-01/D-02).
- `internal/config/villaconfig.go` — `MemoryEnabled`, `EmbeddingModel`,
  `EmbeddingDim` fields the cmd tier reads and passes into the pure cores.
- `internal/preflight/checks_resources.go` — PRE-04 disk+memory check: the
  statfs/meminfo seam + BLOCK/WARN + remediation pattern `checks_memory.go`
  mirrors; `internal/preflight/preflight.go` — check registration/runner.
- `internal/doctor/doctor.go` — `Deps` func-fields, `Aggregate` (:171), the
  preflight fold + typed-Unknown WARN discipline, doctor's OWN Report/golden;
  `cmd/villa/doctor.go` — `liveDoctorDeps` (:156), rendering, exit mapping.
- `internal/inference/running_offload.go` — `RunningOffloadInput` (:40),
  `gttFloor` (:234) — the existing offload-assert machinery the residency proof
  re-uses (log-scrape + GTT-delta; CPU fallback = FAIL).
- `cmd/villa/install_memory.go` — `evalMemoryProof` — the proven mechanism for
  reaching `villa-embed` `/v1/embeddings` (no host port) that the doctor load
  proof reuses.
- `cmd/villa/recommend.go` + `cmd/villa/testdata/*.golden*` — recommend `--json`
  golden(s) to re-freeze once with `-update` after the schema bump.
- `internal/inference/seam_test.go` — `TestSeamGrepGate`: no image/marker
  literals may leak into `recommend`/`preflight`/`doctor` cores.

### External
- Rootless Podman volume storage location semantics (graphroot /
  `podman system info`) — research confirms how to resolve the statfs target for
  the vector-disk gate (D-07); never hardcode the path blindly.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/memory.Footprint` — pinned, typed-Bytes footprint source; already
  unit-tested at 512 MiB for the pinned model. CTRL-01 consumes it as-is.
- `recommend.Pick`'s degraded-envelope note pattern — the template for the
  D-02 "reserved conservatively for unrecognized model" note.
- `checks_resources.go` statfs/meminfo injectable seams — the disk/headroom
  gate is the same shape pointed at a different path + floor.
- Doctor's preflight fold + `statusRank` severity merge — memory checks slot in
  without new aggregation logic.
- `RunningOffload`/`gttFloor` + the Backend marker seam — the under-load
  residency assert already exists; the phase only adds the LOAD (embed drive)
  around it.
- `evalMemoryProof` (install path) — proven container-network reach to
  villa-embed with no host port.

### Established Patterns
- **Pure core + injectable seam** — fit math and check classification stay pure;
  config loads and host probes live in the cmd tier / Deps.
- **Typed-Unknown degradation** — unevaluable ≠ failing; WARN with provenance,
  never false-green, never silent 0-reservation.
- **Append-only + schema-bump golden evolution** — recommend goldens re-frozen
  once, intentionally; `status.Report` untouched (Phase 23).
- **Opt-in discipline** — `memory_enabled=false` keeps recommend/preflight/doctor
  output byte-identical to v1.2 behavior.
- **Refuse-with-remediation** — every non-PASS check carries actionable text.

### Integration Points
- config (`MemoryEnabled`/`EmbeddingModel`) → cmd tier → `recommend.Pick`
  (envelope shrink) / preflight checks / doctor findings.
- `memory.Footprint` → recommend reservation AND preflight headroom floor
  (single source, no duplicated constant).
- Doctor load proof → villa-embed `/v1/embeddings` (container network) +
  `internal/inference` residency scrape on villa-llama.
- Phase 23 consumes: the schema-bump precedent and the measured-footprint
  observation recorded in this phase's summary.

</code_context>

<specifics>
## Specific Ideas

- ROADMAP SC#1 wording is the ordering contract: "reserves … *before* the
  chat-model fit (envelope shrinks first)" — the reservation must be visible in
  the fit math, not a post-hoc note.
- The live Strix Halo box IS the dev/test host — the doctor residency-under-load
  proof and the D-05 footprint measurement run for REAL during verification
  (per the standing on-hardware verification convention).
- `villa install` re-runs recommend and reverts a ROCm backend choice to Vulkan
  (known gotcha) — Phase 22 changes recommend math; verification on the live box
  should re-check that an install/recommend pass doesn't surprise the running
  stack (restore `backend set rocm` afterward if the box was on ROCm).

</specifics>

<deferred>
## Deferred Ideas

- `status`/dashboard memory rows + `status.Report` 2→3 + golden re-freeze —
  **Phase 23**.
- Qdrant-volume backup/restore + dimension-in-manifest skew WARN — **Phase 23**.
- Memory-aware `villa model swap` (dimension guard) — **Phase 23**.
- GPU passthrough for `villa-embed` (and re-measuring a GPU-resident embed
  footprint) — backlog; not needed while the CPU-only posture holds.
- Auto-remediation (auto-shrink ctx / auto-disable memory on unfit hosts) — new
  capability; backlog if ever wanted.

</deferred>

---

*Phase: 22-control-plane-fit-host-gate*
*Context gathered: 2026-06-10*
