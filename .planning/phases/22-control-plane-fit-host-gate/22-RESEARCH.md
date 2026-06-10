# Phase 22: Control-Plane Fit + Host Gate - Research

**Researched:** 2026-06-10
**Domain:** Go control-plane fit math + host gating (recommend / preflight / doctor) over the v1.3 memory stack on rootless Podman / gfx1151 unified memory
**Confidence:** HIGH (all integration points read from the codebase this session; every host-dependent claim verified live on the target Strix Halo dev box)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Reservation happens **inside `recommend.Pick` by shrinking the resolved envelope BEFORE the chat-model fit** (`resolveEnvelope` result minus the embedding reservation), only when `memory_enabled=true` in the loaded config. Memory disabled → reservation is zero and the pick math is unchanged. Reservation value comes from the existing pure `internal/memory.Footprint(modelID)` (Phase 18 D-08: 512 MiB pinned for `nomic-embed-text-v1.5`) — never a literal re-typed in `recommend`. `Pick`'s signature grows the memory inputs explicitly (pure-core rule: no config loads inside the core).
- **D-02:** **Typed-Unknown footprint never silently reserves 0.** If `memory_enabled=true` but `Footprint` returns Unknown (unrecognized embedding model id), reserve the **conservative default constant (512 MiB)** and append a DEGRADED-style note naming the unrecognized model — mirroring the existing degraded-envelope note pattern. Honest over optimistic: never under-reserve.
- **D-03:** Contract evolution is **append-only + schema bump**: `recommendSchemaVersion` 1→2, new field(s) surfacing the reservation (e.g. embedding reservation bytes + whether memory was considered) — exact field names planner's call. Recommend goldens re-frozen ONCE, intentionally, with `-update`. This does NOT touch `status.Report` (its single 2→3 evolution stays reserved for Phase 23).
- **D-04:** **`villa-embed` stays CPU-only in v1.3** (Phase 19 shipped it with no `AddDevice=/dev/dri`; embed resident GTT delta ≈ 0). The ~512 MiB reservation still subtracts from the **unified envelope** — on Strix Halo, CPU-resident embed memory and GTT come from the same physical pool. Do NOT add GPU passthrough to the embed unit in this phase.
- **D-05:** The **512 MiB constant stays the planning figure**. Phase-19's deferred "measure on hardware in Phase 22" lands as a **verification/UAT observation** (measure resident usage under embed load on the live box and record it in the phase summary), NOT as a code change to the constant — unless measurement shows the constant UNDER-reserves, which would be a finding requiring the constant raised.
- **D-06:** New topic-grouped check file (e.g. `internal/preflight/checks_memory.go`) mirroring the `checks_resources.go` pattern: injectable statfs/meminfo seams, BLOCK tier with refuse-with-remediation text, typed-Unknown → WARN when a probe can't evaluate. **Gates run ONLY when `memory_enabled=true`** — when the flag is off the checks are not emitted at all (a v1.2-shaped install sees byte-identical preflight output).
- **D-07:** Two gates: **(a) vector-index disk** — free space at the rootless Podman volume storage root (where the Qdrant named volume lives) must clear a floor; **(b) embedder memory headroom** — free memory must clear the embedding reservation (same `memory.Footprint` source as D-01). Threshold constants and the exact statfs target path are planner's/researcher's call (never hardcode `~/.local/share/containers/...` without checking `podman system info` / graphroot semantics). Both BLOCK on confident failure, WARN on Unknown.
- **D-08:** Doctor (when `memory_enabled=true`) adds memory-stack findings via its existing Deps-fold pattern: **services-up rows** for `villa-qdrant` + `villa-embed` (reuse the systemd/health seams the install/status paths already use), the **vector-disk/headroom checks** (reuse the Phase-22 preflight checks through doctor's existing preflight fold — no duplicate logic), all folded into the existing PASS/WARN/FAIL exit contract. Memory disabled → no memory findings emitted (mirror D-06).
- **D-09:** The **residency proof drives a REAL embedding workload** and asserts the CHAT model stays GPU-resident under it: generate N embedding requests against `villa-embed` `/v1/embeddings` (reuse the Phase-19/20 proof mechanism in `cmd/villa/install_memory.go` — `evalMemoryProof`'s reach path; villa-embed has NO host port, so the drive goes through the proven container-network mechanism, fixed-arg, shell-free), while scraping the chat model's residency with the existing `internal/inference` offload machinery (`RunningOffload` / GTT-delta + log-scrape). **Semantics:** confident partial/full CPU fallback of the CHAT model during embed load = **FAIL**; unevaluable signal (services down, scrape failed, memory stack not running) = **typed-Unknown WARN ("could not evaluate")** — never a false-green PASS.
- **D-10:** Doctor's under-load proof is **opt-in via the existing doctor invocation shape** (it already runs host-touching probes); if the chat stack or memory stack is down, the proof degrades to typed-Unknown WARN rather than starting services itself — doctor diagnoses, it never mutates state.

### Claude's Discretion

- Exact new `Recommendation` field names/JSON keys, check IDs (PRE-xx numbering), remediation strings, threshold constants (disk floor), N embed requests and request payload for the load proof, golden fixture layout — planner's call within D-01..D-10.
- Whether the preflight headroom check reuses `checkResources`'s meminfo seam or gets its own — planner's call (no duplicate probe logic either way).
- Sequencing of plans (recommend fit vs preflight vs doctor) — planner's call; recommend fit (CTRL-01) has no dependency on the other two.

### Deferred Ideas (OUT OF SCOPE)

- `status`/dashboard memory rows + `status.Report` 2→3 + golden re-freeze — **Phase 23**.
- Qdrant-volume backup/restore + dimension-in-manifest skew WARN — **Phase 23**.
- Memory-aware `villa model swap` (dimension guard) — **Phase 23**.
- GPU passthrough for `villa-embed` (and re-measuring a GPU-resident embed footprint) — backlog.
- Auto-remediation (auto-shrink ctx / auto-disable memory on unfit hosts) — backlog.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CTRL-01 | `villa recommend` reserves the embedding-model footprint in the unified-memory fit math *before* the chat-model fit, so the recommended config never OOMs or silently CPU-falls-back on gfx1151 | §"CTRL-01: Fit-Math Integration" — exact insertion point in `Pick` (after `resolveEnvelope`, before `pickBest`/`pickOverride`), `memory.Footprint` consumption, the 7-call-site signature ripple, append-only field + `recommendSchemaVersion` 1→2, exactly one golden (`cmd/villa/testdata/recommend.golden.json`) re-frozen |
| CTRL-03 | `villa doctor` includes memory-stack health checks (services up, offload-asserting residency under embedding load, vector-disk/headroom), folded into its existing PASS/WARN/FAIL exit contract | §"CTRL-03: Doctor Fold + Residency-Under-Load Proof" — Deps growth pattern, reuse of `runProbeCurl` reach + `RunningOffloadVerdict` assert, the live-verified "doctor already WARNs with memory on" pitfall and its doctor-side resolution path, doctor report/golden evolution rules |
| CTRL-06 | `villa preflight` gates host fitness for the memory stack (disk space for the vector index, memory headroom for the embedder) with refuse-with-remediation | §"CTRL-06: Preflight Memory Gate" — `checks_memory.go` skeleton mirroring `checks_resources.go`, the live-verified statfs target resolution (`podman system info --format '{{.Store.VolumePath}}'`), gating wiring in `cmd/villa/preflight.go` + the install path, floor sizing math |
</phase_requirements>

## Summary

This phase is pure **integration over already-shipped seams** — zero new Go dependencies, zero new container images, zero new outbound. Every building block exists: `memory.Footprint` (the 512 MiB typed-Bytes reservation), `resolveEnvelope`/`Pick` (the fit math to shrink), `checks_resources.go` (the statfs/meminfo check pattern to mirror), `doctor.Aggregate` (the Deps-fold to extend), `RunningOffloadVerdict` + `gttFloor` (the residency assert to reuse), and `runProbeCurl` (the proven fixed-arg container-network reach to villa-embed). The work is wiring them together while honoring three frozen-contract disciplines: recommend's golden re-freezes exactly once (schema 1→2), `status.Report` is untouched, and `TestSeamGrepGate` stays green (nothing new needs an image or marker literal — all are reachable via existing accessors).

Two load-bearing facts were established **on the live target box** this session. First, the rootless-Podman volume storage root resolves via `podman system info --format '{{.Store.VolumePath}}'` → `~/.local/share/containers/storage/volumes` (podman 5.8.2), with `podman volume inspect villa-qdrant --format '{{.Mountpoint}}'` as the per-volume variant — and the existing `existingAncestor` statfs walk already handles a not-yet-created path, so the disk gate can statfs the resolved root even pre-install. Second — the most important planning discovery — **`villa doctor` with `memory_enabled=true` ALREADY exits WARN today on a perfectly healthy stack**: `status.Run` derives service rows from rendered units, so villa-qdrant/villa-embed get `OffloadApplies=true` and typed-Unknown offload WARN verdicts that doctor folds into its worst-wins rank (verified live: `offload:villa-qdrant.service WARN`, `offload:villa-embed.service WARN`, overall WARN). SC#3's "folds into the existing PASS/WARN/FAIL contract" cannot be honestly satisfied unless doctor handles these non-GPU services doctor-side (the status-side N/A pattern is explicitly Phase 23). The planner must include this in the doctor plan.

A third useful observation de-risks D-05: `villa-embed`'s live cgroup footprint is **MemoryCurrent ≈ 469 MiB / MemoryPeak ≈ 473 MiB** — the weights (146 MB) + fully-allocated 8192-token KV (~302 MiB math) land just **under** the 512 MiB reservation even after real serving. The STATE.md "constrain embed ctx ≈ 512" flag appears unnecessary on current evidence; the verification measurement (D-05) should confirm peak stays < 512 MiB under a sustained embed drive.

**Primary recommendation:** Three independent plan tracks — (1) recommend fit + schema bump (pure core + 7 call-site thread-through + one golden re-freeze), (2) `checks_memory.go` + cmd-tier gating (preflight verb + install path), (3) doctor Deps growth (memory service rows, preflight-check fold, under-load residency proof, AND the non-GPU offload N/A handling) — closed by an on-hardware UAT plan on this box.

## Project Constraints (from CLAUDE.md)

| Directive | Impact on this phase |
|-----------|---------------------|
| Pure-core + injectable-seam; cores never `os.Exit`/print | `Pick` reservation, check classification, doctor fold, proof verdict mapping all stay pure; config loads + podman/statfs/journald live in cmd tier / Deps |
| Config is the single source of truth | `memory_enabled`/`embedding_model` read via `config.LoadVilla()` in the cmd tier, threaded into cores as explicit inputs (D-01) |
| `TestSeamGrepGate` walks `internal/` + `cmd/villa` | No image/device/marker literal in recommend/preflight/doctor; use `orchestrate.EmbedImage()`, `orchestrate.QdrantVolumeName()`, `inference.BackendFor(...).ResidencyProof()` |
| `--json`/dashboard contracts byte-frozen; append-only + schema bump; refreeze with `-update` | recommend golden re-frozen once (1→2); `status.json.golden` MUST NOT change; doctor render goldens unchanged unless fixtures change |
| Offload is offload-asserting, never liveness | Under-load proof: confident CPU fallback = FAIL; unevaluable = WARN; never false-green (D-09) |
| No shell interpolation; fixed-arg `exec.Command` | Embed-load drive reuses `runProbeCurl` (fixed args, JSON body marshaled, model id config-resolved) |
| Typed-Unknown degradation (never bare 0 / never panic) | D-02 conservative-default reservation + note; check WARN on unevaluable probes |
| Dashboard binary trap | Verification on the live box must `make build` + restart `villa-dashboard.service` if dashboard behavior is checked (dashboard calls `recommend.Pick` at cmd/villa/dashboard.go:268) |
| GSD workflow; `make check` pre-commit gate | `go vet` + full `go test ./...` green per task |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Embedding-footprint reservation math | Pure core (`internal/recommend`) | — | Fit math is pure & table-testable; `Pick` signature grows explicit memory inputs (D-01) |
| Footprint constant / typed-Unknown source | Pure core (`internal/memory.Footprint`) | — | Single source (D-01/D-02); never re-typed elsewhere |
| Reading `memory_enabled`/`embedding_model` | Command tier (`cmd/villa/*.go`) | — | Pure-core rule: no config loads inside cores; cmd tier loads + threads |
| Vector-disk / headroom check classification | Pure core (`internal/preflight/checks_memory.go`) | — | Mirrors `checkResources`: classification pure, probes injected |
| Resolving the podman volume storage root | Command tier / live seam | `internal/preflight` (consumes a path string) | `podman system info` is a host exec; the check takes a path + statfs seam |
| Memory-stack doctor findings + worst-wins fold | Pure core (`internal/doctor`) | — | Composition-only; new findings normalize into the existing `Finding` grammar |
| Embed-load drive (N × `/v1/embeddings`) | Command tier (`cmd/villa`, reusing `runProbeCurl`) | Container network (`villa.network`) | villa-embed has no host port; the proven `podman run --rm --network villa --entrypoint curl` reach is a cmd-tier live seam |
| Chat-model residency assert under load | Pure core (`internal/inference.RunningOffloadVerdict`) | Command tier (journal/GTT reads) | Existing machinery reused verbatim; cmd tier supplies `JournalText`/`GTTUsedBytes`/`Markers` |
| Exit-code mapping & rendering | Command tier | — | Existing `renderPreflight`/`renderDoctor`/`renderRecommend` patterns |
| On-hardware verification | Live host (this box) | — | gfx1151 + running memory stack is the only place the under-load proof is real |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib (`syscall.Statfs`, `os/exec`, `encoding/json`, `testing`) | Go 1.26.2 | statfs probe, fixed-arg podman exec, JSON contracts, table+golden tests | Already the project's only test/probe toolset `[VERIFIED: go.mod + internal/preflight/checks_resources.go]` |
| `github.com/spf13/cobra` | v1.10.2 | existing verbs (`recommend`/`preflight`/`doctor`) gain no new flags by default | Already wired `[VERIFIED: go.mod]` |

**Zero new dependencies.** The v1.3 roadmap pins "zero new first-party Go libraries" `[CITED: .planning/STATE.md → v1.3 Build Order]`, and every capability this phase needs exists in-repo (inventory below).

### Supporting (in-repo reuse inventory — all verified by reading the source this session)

| Existing asset | Location | Reused for |
|----------------|----------|------------|
| `memory.Footprint(modelID) detect.Bytes` | `internal/memory/footprint.go:38` | D-01 reservation + D-07b headroom floor (single source) |
| `memory.Decide(cfg) Decision` | `internal/memory/memory.go:49` | Fail-closed enablement-and-fields-valid gate (recall verbs precedent) |
| `resolveEnvelope` / `Pick` / `pickBest` / `pickOverride` / `finalizeRecommendation` | `internal/recommend/recommend.go:123,210,272,154`, `envelope.go:45` | CTRL-01 insertion points |
| `recommendSchemaVersion = 1`, `SchemaVersion` last-field rule | `internal/recommend/recommend.go:29,100-102` | D-03 bump 1→2; new fields go ABOVE `SchemaVersion` |
| `checkResources` + `statfsFunc` seam + `existingAncestor` + `warn/fail/pass` helpers | `internal/preflight/checks_resources.go:28,47,85`, `preflight.go:171-184` | The exact pattern `checks_memory.go` mirrors (D-06) |
| `p.MemAvailableBytes` (typed `detect.Bytes` from `/proc/meminfo` MemAvailable) | `internal/detect/profile.go:29` | D-07b headroom probe — reuse the HostProfile field, no new meminfo seam needed |
| `doctor.Deps` func-fields + `Aggregate` + `Finding`/`statusRank` | `internal/doctor/doctor.go:117,171,83,146` | D-08 fold; new findings normalize into the existing grammar |
| `inference.RunningOffloadVerdict` + `RunningOffloadInput` + `gttFloor` | `internal/inference/running_offload.go:327,40,234` | D-09 residency assert (verbatim reuse — never re-roll) |
| `runProbeCurl` (fixed-arg `podman run --rm --network villa --entrypoint curl EmbedImage()`) | `cmd/villa/install_memory.go:322` | D-09 embed-load drive (villa-embed has no host port) |
| `liveLoadedMemoryEnabled` (fail-soft persisted gate) | `cmd/villa/install_memory.go:138` | Pattern for cmd-tier memory gating in preflight/doctor/recommend |
| `liveStatusDeps` residency wiring (`sys.ResidencyJournal`, `detect.GTTUsedBytes`, `backend.ResidencyProof()`, `liveWeightBytes`) | `cmd/villa/status.go:168-196,400` | Live inputs for the under-load `RunningOffloadInput` |
| `orchestrate.QdrantVolumeName()` / `QdrantContainerUnitName()` / `EmbedContainerUnitName()` / `EmbedImage()` | `internal/orchestrate/memory.go:106,114,117,47` | Volume identity for the disk gate; service unit names for doctor rows; helper image — no re-typed literals |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Growing `Pick`'s signature (D-01, locked) | A `PickWithMemory` wrapper keeping `Pick` as a zero-reservation shim | D-01 explicitly locks "Pick's signature grows the memory inputs" — a wrapper risks call sites silently staying memory-blind; do NOT use |
| `podman system info --format '{{.Store.VolumePath}}'` | `podman volume inspect villa-qdrant --format '{{.Mountpoint}}'` | volume inspect fails pre-install (volume absent); `system info` works always; use VolumePath primary, with the typed-Unknown WARN on exec failure |
| Driving embed load via `runProbeCurl` per request | A Go `net/http` client | villa-embed publishes NO host port (PRIV-01/SC#4) — host HTTP cannot reach it; the container-network curl is the proven path `[VERIFIED: cmd/villa/install_memory.go + podman ps shows no published port]` |

**Installation:** none — no new packages.

## Package Legitimacy Audit

This phase installs **no external packages** (no new Go modules, no new container images — the only image touched is the already-pinned, on-hardware-verified `EmbedImage()` accessor reused as the curl helper). No legitimacy check required.

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
                          config.toml  (memory_enabled, embedding_model, embedding_dim,
                                        qdrant_addr/port, embed_addr/port — SINGLE SOURCE)
                                │  config.LoadVilla()  (cmd tier ONLY)
        ┌───────────────────────┼───────────────────────────────┐
        ▼                       ▼                               ▼
┌─ villa recommend ─┐   ┌─ villa preflight ─┐          ┌─ villa doctor ─────────────┐
│ cmd/villa/        │   │ cmd/villa/        │          │ cmd/villa/doctor.go        │
│ recommend.go      │   │ preflight.go      │          │ liveDoctorDeps()           │
│  memory inputs ──►│   │  memory gating ──►│          │  + memory svc names        │
└───────┬───────────┘   └───────┬───────────┘          │  + memory preflight fold   │
        ▼                       ▼                      │  + under-load proof seam   │
┌ internal/recommend┐   ┌ internal/preflight┐          └──────┬─────────────────────┘
│ Pick:             │   │ checks_memory.go  │                 ▼
│  resolveEnvelope  │   │ (NEW, mirrors     │        ┌ internal/doctor.Aggregate ┐
│   │ envelope      │   │  checks_resources)│        │ existing findings          │
│   ▼ −Footprint ◄──┼───┼── memory.Footprint│        │ + svc-up rows (qdrant/embed)│
│  pickBest/Override│   │ disk: statfs at   │        │ + memory disk/headroom fold │
│  fit verdict      │   │  podman VolumePath│        │ + residency-under-load      │
│  +new fields,     │   │ headroom: profile │        │ + non-GPU offload N/A       │
│   schema 1→2      │   │  MemAvailableBytes│        │ worst-wins → PASS/WARN/FAIL │
└───────────────────┘   └───────────────────┘        └──────┬──────────────────────┘
                                                            │ under-load proof (D-09)
                              ┌─────────────────────────────┴───────────────┐
                              ▼ drive (cmd tier, fixed-arg)                 ▼ assert (pure)
                   podman run --rm --network villa              inference.RunningOffloadVerdict(
                    --entrypoint curl EmbedImage()                JournalText: villa-llama ResidencyJournal,
                    → N × POST villa-embed:8080/v1/embeddings     GTTUsedBytes: detect.GTTUsedBytes(),  ← read DURING load
                      (container-DNS only, no host port)          WeightBytes: liveWeightBytes(cfg),
                                                                  Markers: BackendFor(cfg.Backend).ResidencyProof())
                                                                → PASS / WARN(unevaluable) / FAIL(CPU fallback)
```

### Recommended Project Structure (edits, no new packages)

```
internal/recommend/recommend.go    # Pick signature + reservation + new Recommendation fields + schema bump
internal/recommend/envelope.go     # (optionally) reservation-aware envelope helper
internal/preflight/checks_memory.go        # NEW — D-06/D-07 checks (pure classification + injected seams)
internal/preflight/checks_memory_test.go   # NEW — table tests
internal/preflight/preflight.go    # new exported runner/entry for memory checks (emission gated by caller)
internal/doctor/doctor.go          # Deps growth + memory findings + non-GPU offload handling
cmd/villa/recommend.go             # thread memory inputs; render reservation line
cmd/villa/preflight.go             # load config fail-soft; append memory checks when enabled
cmd/villa/doctor.go                # liveDoctorDeps growth: svc names, memory checks, load-proof seam
cmd/villa/install.go               # install path also gates memory checks when enabled (same emission rule)
cmd/villa/status.go | model.go | inference.go | backend.go | dashboard.go  # Pick call-site thread-through
cmd/villa/testdata/recommend.golden.json   # re-frozen ONCE with -update (schema 2)
```

---

### CTRL-01: Fit-Math Integration (the exact mechanics)

**Insertion point** `[VERIFIED: internal/recommend/recommend.go:123-147]`: `Pick` currently does `resolveEnvelope(p)` → degraded note → `pickOverride`/`pickBest`. The reservation subtracts from `envelope` immediately after `resolveEnvelope` succeeds and BEFORE the degraded note / pick calls, so every downstream consumer (`headroomBytes(envelope)`, the OOM guard `total > envelope`, `UsableEnvelopeBytes`) sees the shrunken value — exactly SC#1's "envelope shrinks first".

**Memory inputs (D-01: explicit signature growth).** The pure core needs: whether memory is enabled, and the embedding model id. Recommended shape: a small value struct (e.g. `MemoryInputs{Enabled bool; EmbeddingModel string}`) as a 4th `Pick` parameter — zero value = memory off = byte-identical math. The core calls `memory.Footprint(in.EmbeddingModel)` itself (pure→pure import is fine; `internal/memory` already imports only `config`+`detect` and is seam-gate-clean `[VERIFIED: internal/memory/footprint.go header + 18-02 decision]`).

**Typed-Unknown branch (D-02):** `Footprint` returns `detect.Bytes`; on `.Known=false` with memory enabled, reserve a conservative default. The 512 MiB default constant for this branch must ALSO come from a single source — recommended: export it from `internal/memory` (e.g. `memory.ConservativeFootprintBytes()` or a doc'd exported const) rather than re-typing `512 << 20` in recommend; `embedFootprints` currently holds the value privately `[VERIFIED: internal/memory/footprint.go:29-31]`. Note template: mirror the existing degraded-envelope note (`recommend.go:136-139`).

**Edge case — reservation ≥ envelope:** subtract with a guard (`if reservation >= envelope` → envelope 0 / refuse-style note). On a degraded 50%-of-RAM floor this still cannot underflow silently; never wrap a uint64.

**New `Recommendation` fields (D-03, names planner's call):** appended ABOVE `SchemaVersion` (last-field rule, `recommend.go:100-102`). Minimum honest surface: reservation bytes + a memory-considered marker (e.g. `embedding_reservation_bytes uint64` + `memory_considered bool`, or reservation with `omitempty` semantics — but note: a plain `uint64` with 0 when off keeps the off-path JSON shape changed only by the new keys; either way the golden re-freezes once and `schema_version: 2` flags the growth). The memory-off table render must stay byte-identical (only print a reservation line when reserved > 0).

**Call-site ripple (all 7 production sites must compile + decide what to pass)** `[VERIFIED: grep this session]`:

| Call site | Context | What to thread |
|-----------|---------|----------------|
| `cmd/villa/recommend.go:60` | the verb itself | persisted config (already loads `config.LoadVilla()` for catalog path — reuse, fail-soft to memory-off) |
| `cmd/villa/install.go:985` | install's pick seam (re-runs recommend) | persisted config — install MUST be memory-aware or the installed config skips the reservation |
| `cmd/villa/status.go:405` (`liveWeightBytes`) | derives chat WeightBytes for the GTT floor | memory inputs change fit verdict, NOT `WeightBytes` of an explicit `--model` override (`pickOverride`/`buildRecommendation` weight terms are envelope-independent `[VERIFIED: recommend.go:272-330]`) — passing zero-value memory keeps `status.json.golden` byte-identical; passing real config is also weight-safe. Recommend: pass zero-value to provably not perturb the frozen status path |
| `cmd/villa/model.go:331`, `cmd/villa/inference.go:144`, `cmd/villa/backend.go:392`, `cmd/villa/dashboard.go:268` | fit re-validation paths | planner's call; honest default = thread persisted config (fail-soft) so swap/bench fit checks see the same envelope the user was recommended |

**Goldens affected:** exactly `cmd/villa/testdata/recommend.golden.json` (re-frozen once via `go test ./cmd/villa -run TestRecommend -update` — the package-level `-update` flag lives in `cmd/villa/detect_test.go:13` `[VERIFIED]`). `status.json.golden`, `doctor*.golden`, orchestrate goldens: untouched. `internal/recommend` uses table tests, not goldens — new cases added, none re-frozen.

**Install interplay (note for planner):** install's resource gate computes `MinMemBytes = rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes` `[VERIFIED: cmd/villa/install.go:268-270]` — this does NOT include the embed reservation. The D-07b preflight headroom check covers the embedder separately; whether install's `MinMemBytes` should ALSO add the reservation when memory is on is planner's call (adding it is more honest; both checks reading `memory.Footprint` keeps one source).

---

### CTRL-06: Preflight Memory Gate (statfs target — VERIFIED on the live box)

**Volume storage root resolution (D-07a).** Verified live on the target host (Fedora 44, rootless podman 5.8.2) `[VERIFIED: on-hardware probes this session]`:

```
podman system info --format '{{.Store.GraphRoot}}'   → /home/oliverh/.local/share/containers/storage
podman system info --format '{{.Store.VolumePath}}'  → /home/oliverh/.local/share/containers/storage/volumes
podman volume inspect villa-qdrant --format '{{.Mountpoint}}'
                                                     → .../storage/volumes/villa-qdrant/_data
```

**Recommended statfs target:** `Store.VolumePath` from `podman system info` — it exists on any podman host (even before `villa-qdrant` is created, when `volume inspect` would fail), it is the filesystem the named volume's `_data` lives under, and the existing `existingAncestor` walk (`checks_resources.go:47`) makes statfs robust even if the path doesn't exist yet. Resolution is a host exec → it lives behind an injectable seam (a `volumeRootFn func() (string, bool)` mirroring `statfsFunc`), fixed-arg `exec.Command("podman", "system", "info", "--format", "{{.Store.VolumePath}}")`, with exec failure → typed-Unknown WARN ("could not resolve the podman volume storage root"). Never hardcode `~/.local/share/containers/...` (D-07); the deterministic fallback used by `[ASSUMED]`-grade tooling is graphroot+`/volumes`, but the seam makes that unnecessary.

**Check file shape (D-06).** `checks_memory.go` mirrors `checks_resources.go` exactly `[VERIFIED: internal/preflight/checks_resources.go:85-130]`:

- Pure check funcs taking `(p detect.HostProfile, <thresholds>, <injected seams>)` returning `CheckResult` via the package `pass/warn/fail` helpers.
- **Disk gate:** `statfs(volumeRoot)` free bytes ≥ disk floor → PASS; confident `<` → `fail` (TierBlock) with remediation ("free up space under the podman volume storage root <path>; the Qdrant vector index grows with indexed chats/documents"); statfs/resolution failure → `warn` (TierBlock, downgraded — D-15 discipline).
- **Headroom gate:** `p.MemAvailableBytes.Known && p.MemAvailableBytes.Value ≥ memory.Footprint(embeddingModel)` (Unknown footprint → the same conservative default as D-02). Reuses the HostProfile field — no new meminfo probe (the "reuse `checkResources`'s meminfo seam" discretion resolves naturally: the seam IS the profile field).

**Check IDs.** Existing namespace: `PRE-01..PRE-07` (PRE-04 = disk+memory) and `ROCM-PRE-{gfx,kernel,firmware,hsa,image}` `[VERIFIED: grep this session]`. The ROCm precedent (a topic prefix for an opt-in subsystem) fits best: recommend `MEM-PRE-disk` / `MEM-PRE-headroom` — collision-free, self-describing in doctor's findings table, and avoids renumbering the frozen PRE-xx sequence. Planner's call per discretion.

**Emission gating (D-06 — the load-bearing wiring detail).** `preflight.Run`/`RunWithResources` take NO config and are called from two places `[VERIFIED]`:

1. `cmd/villa/preflight.go:47-63` — the standalone verb. It does NOT currently load config. It must gain a fail-soft `config.LoadVilla()` (mirror `liveLoadedMemoryEnabled`, `install_memory.go:138` — load error → memory-off → byte-identical v1.2 output) and append the memory checks ONLY when enabled. Do not change `Run`'s signature/output for the off path — D-06 requires byte-identical output when off, and `preflight-pass.golden`/`preflight-warn.golden` freeze the render.
2. `cmd/villa/install.go:268-273` — install's `runChecks` seam (`preflight.RunWithResources`). Install already knows `MemoryEnabled` (the `loadedMemoryEnabled` seam); the same appended-when-enabled rule applies so an opted-in install refuses-with-remediation before bringing up the stack.

Cleanest core shape: a new exported `RunMemory(p detect.HostProfile, in MemoryGateInput) []CheckResult` (or similar) in `internal/preflight` that BOTH callers append when enabled — emission decision in the cmd tier, classification in the core. Doctor then reuses the SAME function through its fold (D-08, no duplicate logic).

**Disk floor sizing (planner's call; grounded math).** Live reality: the Qdrant volume `_data` is currently **4.2 MB** with the full Phase-21 recall index + Phase-20 KB resident; the volume filesystem has **~469 GiB free** `[VERIFIED: du/df this session]`. Sizing model: a 768-dim float32 vector ≈ 3 KiB raw; with HNSW graph + payload overhead ≈ ~1.5–2× → ~5–6 KiB/chunk; **1 GiB ≈ ~180k–230k chunks** — years of single-user chat/document indexing `[ASSUMED: derived from Qdrant's published capacity-planning formula memory ≈ vectors × dim × 4 B × 1.5; disk likewise weights+graph]`. A **1 GiB BLOCK floor** is therefore generous-but-meaningful (catches a nearly-full disk, never blocks a sane host); 2 GiB if the planner wants margin for Qdrant WAL/snapshots. Keep it a named const with the WR-02/WR-03 style comment (floor, not envelope).

---

### CTRL-03: Doctor Fold + Residency-Under-Load Proof

**⚠ Live-verified pitfall — doctor already WARNs with memory on.** `status.Run` derives service rows from `serviceUnits(rendered units)` `[VERIFIED: internal/status/status.go:272-280,348-393]`; with memory on, `villa-qdrant.container`/`villa-embed.container` render → rows with `OffloadApplies=true` → `RunningOffloadVerdict` over THEIR journals → typed-Unknown WARN (no `load_tensors` device line; villa-embed runs at default verbosity). Doctor folds every `OffloadApplies` row (`doctor.go:214-222`) → two WARN findings. **Verified on this box right now:**

```
./villa status --json → villa-qdrant.service offload_applies=true status WARN; villa-embed.service same; overall WARN
./villa doctor --json → offload:villa-qdrant.service WARN WARN; offload:villa-embed.service WARN WARN; overall: WARN
```

So a healthy memory-on install can NEVER reach doctor PASS today. SC#3 ("folds memory-stack health into its existing PASS/WARN/FAIL exit contract") is unsatisfiable without addressing this. The status-side fix (N/A offload rows, schema 2→3) is **explicitly Phase 23** `[CITED: 22-CONTEXT.md Deferred + STATE.md Blockers]`. The doctor-side resolution that stays inside Phase 22 scope: doctor's `Deps` gains the memory service unit names (cmd tier supplies them from `orchestrate.QdrantContainerUnitName()`/`EmbedContainerUnitName()` → `.service`, mirroring how `status.Deps.OWUIService` names a service without literals), and `Aggregate` treats those services' offload findings as N/A (skip the offload finding, or emit it down-ranked like the ROCm supersession precedent at `doctor.go:284-295` — keep the health finding either way). This touches only doctor's OWN report (allowed), not `status.Report`. **The planner must decide skip-vs-downrank explicitly; silently leaving doctor WARN-forever fails SC#3's spirit and makes the new memory findings unverifiable (overall would be WARN regardless).**

**Deps growth (D-08/D-09).** Doctor's existing seams: `Probe`, `LoadConfig`, `StatusReport`, `DriftPlan`, `Backend`, `RunROCmImage` `[VERIFIED: doctor.go:117-143]`. New func-fields (nil-safe like `RunROCmImage` — nil → no memory findings, memory-off behavior byte-identical):

- `MemoryEnabled bool` (or fold via `LoadConfig` — but a plain field mirrors `Backend` and keeps the core config-load-free at Aggregate time).
- `MemoryServiceNames []string` (or two fields) — for services-up rows AND the offload-N/A predicate.
- `RunMemoryChecks func(detect.HostProfile) []preflight.CheckResult` — binds the CTRL-06 checks (preflight fold reuse, D-08; normalized via the existing `findingFromCheck`).
- `ResidencyUnderLoad func() inference.Verdict` (or a small typed result) — the D-09 proof, live-wired in cmd tier.

**Services-up rows:** the health rows for villa-qdrant/villa-embed ALREADY appear via the status fold (verified live: `health:villa-qdrant.service`, `health:villa-embed.service`, PASS) — note for the planner: D-08's "services-up rows" may already be satisfied by the existing fold; what's genuinely new is the disk/headroom findings + the under-load proof + the offload-N/A handling. Avoid duplicating health rows.

**The under-load proof (D-09) — live wiring recipe (all pieces exist):**

1. **Precondition gate (D-10):** chat stack healthy + memory stack active + memory enabled; anything down → one typed-Unknown WARN finding ("could not evaluate residency under embedding load — <which precondition>"), never start services, never FAIL.
2. **Drive:** N × `runProbeCurl(ctx, orchestrate.EmbedImage(), "-sf", "-X", "POST", "http://<embedAddr>:<embedPort>/v1/embeddings", "-H", "Content-Type: application/json", "-d", body)` — body marshaled like `liveMemoryProof` (`install_memory.go:231-244`), model id config-resolved. Sizing N + payload is discretion; a meaningful load = several requests with near-`embedContextLen`-scale inputs (e.g. 8–16 requests of a few-KiB repeated text, sequential is fine — the point is sustained allocator pressure, not throughput). Bound each with a context timeout (the recall indexer measured ~1.3 s/chat on this box `[CITED: STATE.md Phase-21 entry]`; a 30–60 s overall budget is ample). Run the drive concurrently (goroutine) or interleaved so step 3 reads DURING load.
3. **Assert (verbatim reuse):** `inference.RunningOffloadVerdict(RunningOffloadInput{JournalText: sys.ResidencyJournal(villa-llama), Props: …, GTTUsedBytes: detect.GTTUsedBytes(), WeightBytes: liveWeightBytes(cfg), Markers: inference.BackendFor(cfg.Backend).ResidencyProof(), …})` — exactly the `liveStatusDeps` wiring (`cmd/villa/status.go:189-196`). The dynamic signal is the **GTT floor** (`gttFloor`, `running_offload.go:234`): if the chat weights get evicted under embed pressure, `mem_info_gtt_used < WeightBytes` → confident FAIL. The journal scrape re-proves the load-time residency line. Map per D-09: Verdict FAIL → BLOCK-tier FAIL finding; Verdict WARN → typed-Unknown WARN; PASS → PASS. Note villa-embed is CPU-only so it adds ~0 GTT itself `[VERIFIED: 19-03-SUMMARY.md + live measurement]` — the proof is about the CHAT model surviving the unified-memory pressure.
4. **Seam cleanliness:** markers come only via `BackendFor(...).ResidencyProof()` in the cmd tier; helper image only via `EmbedImage()`; check IDs / service names are not marker literals (doctor already embeds `ROCM-PRE-*` ID strings safely, `doctor.go:62-66`). `TestSeamGrepGate` stays green with zero allowlist changes.

**Doctor contract evolution (research question 4).** Doctor owns `reportSchemaVersion = 1` (`doctor.go:54`) with the same last-field/append-only rule. **Findings are data, not schema** — adding new finding IDs does NOT require a schema bump; a bump is needed only if the `Report`/`Finding` STRUCTS gain tagged fields (none required by this design). The cmd-tier goldens (`doctor.json.golden`, `doctor-pass/warn/rocm-superseded.golden`) are render goldens driven by injected fixture Reports `[VERIFIED: cmd/villa/doctor_test.go + testdata listing]` — they change only if their fixtures change; new memory-on fixtures/goldens are added alongside, existing ones untouched. Precedent: v1.2 phases 12–17 evolved doctor by adding findings (ROCm Option B image gate, supersession) at schema 1 with no bump `[VERIFIED: doctor.go history comments]`.

---

### Pattern 1: Envelope-shrink-first reservation (sketch)

```go
// internal/recommend/recommend.go — inside Pick, after resolveEnvelope (D-01/D-02)
envelope, degraded, ok := resolveEnvelope(p)
if !ok { /* existing refusal — unchanged */ }

var notes []string
reservation := uint64(0)
if mem.Enabled {
    fp := memory.Footprint(mem.EmbeddingModel)
    if fp.Known {
        reservation = fp.Value
    } else {
        reservation = memory.ConservativeFootprintBytes() // exported single source — never re-type 512<<20
        notes = append(notes, fmt.Sprintf(
            "RESERVED CONSERVATIVELY: no pinned footprint for embedding model %q; reserving %s before the chat-model fit (D-02).",
            mem.EmbeddingModel, humanGiB(reservation)))
    }
    if reservation >= envelope { /* envelope = 0 → existing no-fit path refuses honestly */ }
    envelope -= reservation // SC#1: the envelope shrinks BEFORE pickBest/pickOverride
}
// existing degraded note + pickOverride/pickBest follow, sized against the shrunken envelope
```

### Pattern 2: checks_memory.go disk gate (sketch, mirrors checkResources)

```go
// internal/preflight/checks_memory.go — D-06/D-07a
func checkVectorDisk(volumeRoot string, rootOK bool, floor uint64, statfs statfsFunc) CheckResult {
    const id, name = "MEM-PRE-disk", "Vector-index disk space"
    remediation := "free up space on the filesystem holding the podman volume storage root; the Qdrant vector index lives there and grows with indexed chats/documents"
    if !rootOK {
        return warn(id, name, TierBlock, "could not resolve the rootless podman volume storage root", remediation, "podman system info --format {{.Store.VolumePath}}", "")
    }
    free, ok := statfs(volumeRoot) // liveStatfs walks existingAncestor — pre-install paths are fine
    if !ok { return warn(id, name, TierBlock, fmt.Sprintf("could not statfs %q", volumeRoot), remediation, "syscall.Statfs", "") }
    if free < floor {
        return fail(id, name, fmt.Sprintf("free disk %s at %q < required %s for the vector index", humanGiB(free), volumeRoot, humanGiB(floor)), remediation, "syscall.Statfs:"+volumeRoot, "")
    }
    return pass(id, name, TierBlock, fmt.Sprintf("free disk %s ≥ %s at %q", humanGiB(free), humanGiB(floor), volumeRoot), "syscall.Statfs:"+volumeRoot)
}
```

### Anti-Patterns to Avoid

- **Re-typing the 512 MiB constant in recommend/preflight** — `memory.Footprint` (+ an exported conservative-default accessor) is the only source (D-01/D-02; the existing single-source discipline: `EmbedGGUFFilename()` precedent).
- **Gating memory checks inside `preflight.Run`** by loading config in the core — config loads stay in the cmd tier (pure-core rule); the core exposes the checks, callers decide emission.
- **Re-rolling offload math for the under-load proof** — `RunningOffloadVerdict`/`gttFloor`/`combineOffload` are reused verbatim; the phase adds only the LOAD around the existing assert (CONTEXT Reusable Assets).
- **Touching `status.Report` / `status.json.golden`** — the 2→3 evolution is Phase 23's single contract change; `liveWeightBytes` must keep producing identical weights (pass zero-value memory inputs there, or prove weight-invariance in a test).
- **Doctor starting services to make the proof evaluable** — D-10: degrade to typed-Unknown WARN. (A transient `podman run --rm` probe container is the established read-only-diagnosis precedent from the install proof — acceptable; `systemctl start` is not.)
- **Treating `villa-embed`'s own WARN offload as the residency signal** — the proof asserts the CHAT model (villa-llama); villa-embed is CPU-by-design (D-04).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Free-disk probe | shelling to `df` / parsing output | `statfsFunc` seam + `liveStatfs` (`checks_resources.go:34`) | locale-proof, already walks `existingAncestor` for absent paths (T-03-01 precedent) |
| Free-memory probe | new `/proc/meminfo` parser | `HostProfile.MemAvailableBytes` (typed `detect.Bytes`) | already probed with provenance + typed-Unknown |
| Reaching villa-embed | host HTTP client / publishing a port | `runProbeCurl` (`install_memory.go:322`) | no host port exists (PRIV-01/SC#4); fixed-arg, seam-clean, proven on-hardware in 19-03/20-02/21 |
| Residency assertion | bespoke GTT/journal logic | `inference.RunningOffloadVerdict` + `gttFloor` | the no-false-green math is shipped, golden-guarded, backend-neutral via `ResidencyMarkers` |
| Footprint value | a literal in recommend/preflight | `memory.Footprint` | single source; typed-Unknown on miss drives D-02 |
| Volume identity / helper image / service names | string literals | `orchestrate.QdrantVolumeName()/QdrantContainerUnitName()/EmbedContainerUnitName()/EmbedImage()` | accessors exist precisely so later phases never re-type them `[VERIFIED: orchestrate/memory.go:106-117]` |
| Check rendering / exit mapping | new render paths | `renderPreflight`/`renderDoctor` + `exitPass/Warn/Blocked` | byte-frozen render goldens; the authoritative exit constants live in `cmd/villa/preflight.go:19-23` |

**Key insight:** this phase has zero novel algorithms — every risk is in WIRING discipline (which goldens re-freeze, which call sites thread inputs, which tier loads config), so the plans should be structured around contract checkpoints, not implementation difficulty.

## Common Pitfalls

### Pitfall 1: Doctor is already WARN with memory on (false-WARN-forever)
**What goes wrong:** the new memory findings land but doctor overall stays WARN on a healthy stack, making SC#3 unverifiable and PASS unreachable.
**Why:** status rows for villa-qdrant/villa-embed carry `OffloadApplies=true` typed-Unknown WARN verdicts (verified live this session).
**How to avoid:** doctor-side N/A handling keyed on Deps-supplied memory service names (skip or down-rank the offload finding; keep health). Do NOT touch `status.Report`.
**Warning signs:** on-hardware UAT can't produce `overall: PASS` with everything green.

### Pitfall 2: Golden blast radius
**What goes wrong:** `-update` run package-wide re-freezes goldens that must NOT change (status, preflight render, bench…).
**How to avoid:** re-freeze with a targeted `-run TestRecommend…` filter; `git status cmd/villa/testdata/` must show exactly the intended file(s); CI gate `make check` after.
**Warning signs:** `git status --porcelain` shows >1 golden modified after the recommend bump.

### Pitfall 3: Pick signature ripple breaks a frozen path
**What goes wrong:** threading real config into `liveWeightBytes` (status path) or the dashboard's pick changes a frozen output, or a call site silently passes the wrong zero value and stays memory-blind (install!).
**How to avoid:** explicit per-call-site decision table (see CTRL-01 section); a test asserting `WeightBytes` is reservation-invariant for `--model` overrides; install's pick MUST receive the persisted memory inputs.
**Warning signs:** `status.json.golden` or dashboard golden diffs; an opted-in install that recommends the same ctx as memory-off.

### Pitfall 4: Memory-off byte-identicality broken
**What goes wrong:** a v1.2-shaped install sees new preflight rows, a new recommend note, or new doctor findings.
**How to avoid:** every emission gated on enabled (D-06/D-08); existing goldens (`preflight-pass.golden`, `doctor.json.golden`, recommend table output for off-path) double as the regression net — only the recommend JSON golden changes (new fields + schema 2; values zero/false when off).
**Warning signs:** any existing render golden diff other than recommend's.

### Pitfall 5: Config load in the wrong tier / not fail-soft
**What goes wrong:** `preflight` core imports config-loading, or the verb hard-errors on an unreadable config.
**How to avoid:** mirror `liveLoadedMemoryEnabled` (`install_memory.go:138`): cmd tier, load error → memory-off (a broken config never silently enables gates), `memory.Decide` for field validity where needed.
**Warning signs:** `villa preflight` exit-code change on a host with no config file.

### Pitfall 6: GTT floor misread during the load proof
**What goes wrong:** the proof reads GTT before/after instead of DURING load, or trusts the static journal alone — missing a transient eviction.
**How to avoid:** read `detect.GTTUsedBytes()` while the embed drive is in flight (goroutine/interleave); the floor (`used ≥ chat WeightBytes`) is the dynamic signal; remember `mem_info_gtt_used` is HOST-WIDE (CR-03 comment) so other GPU users can only make the floor pass spuriously — acceptable for a floor, but record provenance.
**Warning signs:** proof PASSes with villa-llama stopped (precondition gate missing).

### Pitfall 7: Unbounded/heavy embed drive
**What goes wrong:** the doctor proof hammers villa-embed with huge payloads, hangs without timeouts, or leaves transient containers behind on ctrl-C.
**How to avoid:** fixed N, bounded per-request context (`exec.CommandContext` — already how `runProbeCurl` works), `--rm` containers, total proof budget ≤ ~60 s; payload sized to be meaningful (multi-KiB), not adversarial (embed server ctx is 8192, `embedContextLen` const).
**Warning signs:** doctor wall-time blowup; orphaned `podman ps -a` entries after an aborted run.

### Pitfall 8: install re-runs recommend (live-box verification hazard)
**What goes wrong:** verification on this box runs `villa install`/`recommend --save` and reverts the operator's ROCm backend to vulkan (known gotcha, CONTEXT Specifics).
**How to avoid:** UAT checklist notes current backend first (`villa backend …`), restores `villa backend set rocm` after, exactly as 19-03 did. The box is currently on ROCm 7.2.4 `[VERIFIED: systemctl list-units this session]`.

## Code Examples

(See Patterns 1–2 above for the reservation and disk-gate sketches.) The under-load proof's live seam, composed entirely from existing pieces:

```go
// cmd/villa/doctor.go — live wiring sketch for Deps.ResidencyUnderLoad (D-09/D-10)
// Sources: install_memory.go runProbeCurl/liveMemoryProof; status.go liveStatusDeps wiring.
residencyUnderLoad := func() inference.Verdict {
    // (1) D-10 precondition gate: memory enabled+valid, villa-llama/-embed active → else typed-Unknown WARN, never start anything.
    // (2) drive: fire N fixed-arg embed requests over villa.network (no host port):
    //     runProbeCurl(ctx, orchestrate.EmbedImage(), "-sf", "-X", "POST",
    //         fmt.Sprintf("http://%s:%d/v1/embeddings", cfg.EmbedAddr, cfg.EmbedPort),
    //         "-H", "Content-Type: application/json", "-d", string(marshaledBody))
    //     in a goroutine so step (3) samples DURING load.
    // (3) assert: the exact liveStatusDeps inputs, re-read mid-drive:
    return inference.RunningOffloadVerdict(inference.RunningOffloadInput{
        JournalText:  residencyJournal,          // sys.ResidencyJournal for villa-llama (status.go:189-192)
        GTTUsedBytes: detect.GTTUsedBytes(),     // point-in-time, sampled under load
        WeightBytes:  liveWeightBytes(cfg),      // chat model footprint — the gttFloor reference
        Markers:      backend.ResidencyProof(),  // via inference.BackendFor(cfg.Backend) — seam-clean
        ConfigModel:  modelFile, ConfigContext: cfg.Ctx,
    })
}
```

## State of the Art

| Old Approach (pre-Phase-22) | Current Approach (this phase) | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `recommend` fit math is chat-model-only; embed footprint invisible | envelope shrinks by `memory.Footprint` before fit; surfaced append-only at schema 2 | Phase 22 | the NON-NEGOTIABLE THREAT in STATE.md (OOM / silent CPU fallback under import load) closes |
| preflight knows nothing of the memory stack | `checks_memory.go` disk + headroom gates, emitted only when opted in | Phase 22 | "runs healthy after install" extends to the memory stack |
| doctor folds memory services accidentally (WARN-forever via typed-Unknown offload) | deliberate memory findings + non-GPU N/A handling + under-load proof | Phase 22 | doctor PASS becomes reachable and meaningful on memory-on installs |
| 512 MiB footprint = unmeasured estimate (MEDIUM, D-08) | live evidence: villa-embed cgroup ≈ 469 MiB current / 473 MiB peak; D-05 verification re-measures under sustained drive | measured 2026-06-10 | constant validated (does not under-reserve on current evidence); recorded in phase summary per D-05 |

**Deprecated/outdated:** nothing removed; the STATE.md research flag "constrain embed ctx ≈ 512" is **superseded by measurement** (see Open Question 1) — KV at the rendered `-c 8192` is already inside the 512 MiB reservation.

## Runtime State Inventory

Not a rename/refactor/migration phase — omitted by design (greenfield wiring into existing seams; no stored identifiers change).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | 1.26.x (`go.mod` 1.26.2) | — |
| rootless Podman | volume-root probe, embed drive | ✓ | 5.8.2 | typed-Unknown WARN path covers absence on other hosts |
| `villa` binary + full stack | on-hardware UAT | ✓ | villa-llama (ROCm 7.2.4), villa-embed, villa-qdrant, villa-openwebui, villa-dashboard all `active` | — |
| `villa-qdrant` named volume | disk-gate ground truth | ✓ | mountpoint `…/storage/volumes/villa-qdrant/_data` (4.2 MB used) | `Store.VolumePath` works pre-creation |
| Disk headroom on volume FS | UAT realism | ✓ | ~469 GiB free on `/home` | — |
| Host memory | headroom-gate ground truth | ✓ | MemTotal 125.1 GiB; MemAvailable ≈ 82 GiB | — |
| gfx1151 GTT sysfs (`mem_info_gtt_used`) | under-load proof | ✓ (status offload PASS for villa-llama live) | kernel ≥ 6.18.4 | typed-Unknown WARN off-hardware |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none — this IS the target host.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven + byte-frozen goldens + injected `fake*Deps`); no third-party assertion libs |
| Config file | none (convention-based); `-update` flag for goldens (`cmd/villa/detect_test.go:13`) |
| Quick run command | `go test ./internal/recommend/ ./internal/preflight/ ./internal/doctor/ -count=1` |
| Full suite command | `make check` (`go vet ./... && go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CTRL-01 | envelope shrinks first; D-02 conservative default + note; memory-off byte-identical; schema 2 last-field | unit + golden | `go test ./internal/recommend/ -count=1` + `go test ./cmd/villa/ -run 'Recommend' -count=1` | ✅ (`recommend_test.go` both tiers; new cases added in-plan) |
| CTRL-06 | disk/headroom BLOCK-FAIL / Unknown-WARN / PASS; emission gated; remediation non-empty | unit + render golden | `go test ./internal/preflight/ -count=1` + `go test ./cmd/villa/ -run 'Preflight' -count=1` | ❌ `internal/preflight/checks_memory_test.go` (created with the plan — not Wave 0; pattern file `checks_resources_test.go` exists) |
| CTRL-03 | memory findings fold; non-GPU offload N/A; proof FAIL/WARN/PASS semantics; memory-off identical | unit + render golden | `go test ./internal/doctor/ -count=1` + `go test ./cmd/villa/ -run 'Doctor' -count=1` | ✅ (`doctor_test.go` both tiers; new fixtures in-plan) |
| seam invariant | no leaked image/marker literal | unit (grep gate) | `go test ./internal/inference/ -run TestSeamGrepGate -count=1` | ✅ |
| SC#3 on-hardware | real embed drive + GTT scrape on gfx1151; D-05 footprint measurement | manual-only (justified: requires live GPU + running stack) | operator UAT per plan checklist (`systemctl --user show villa-embed.service -p MemoryPeak` for D-05) | n/a |

### Sampling Rate
- **Per task commit:** the affected package's `go test ./internal/<pkg>/ -count=1`
- **Per wave merge:** `make check`
- **Phase gate:** `make check` green + targeted golden diff audit (`git status cmd/villa/testdata/` shows only `recommend.golden.json` + any NEW fixtures) before `/gsd-verify-work`

### Wave 0 Gaps
None — existing test infrastructure (table tests, fake Deps, golden harness with `-update`) covers all phase requirements; new `_test.go` files land alongside their implementation per project convention (tdd_mode false).

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | no new auth surface (loopback/container-DNS only; no new ports) |
| V3 Session Management | no | n/a |
| V4 Access Control | no | doctor/preflight are read-only local diagnostics |
| V5 Input Validation | yes | `memory.Decide` fail-closed on hand-edited config; podman/curl args FIXED (no shell); model id config-resolved into a JSON-marshaled body, never interpolated into a command line |
| V6 Cryptography | no | n/a |

### Known Threat Patterns for this phase (STRIDE)

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Command injection via config-sourced embed model/addr into the curl drive | Tampering | fixed-arg `exec.CommandContext`; JSON body via `json.Marshal` (the `liveMemoryProof` precedent, T-19-10) |
| False-green: doctor reports healthy while chat model silently CPU-falls-back under import load | Repudiation/Info-integrity | offload-asserting proof (D-09): confident fallback = FAIL; unevaluable = WARN; never PASS-by-default (`RunningOffloadVerdict` discipline) |
| Resource exhaustion: doctor's load proof DoSes the box / hangs | DoS | bounded N, per-request context timeouts, total budget, `--rm` transient containers |
| Privacy: new probe traffic leaves the box | Information Disclosure | all probe traffic stays on `villa.network` / loopback; zero new outbound; no telemetry (PRIV-01..03 posture unchanged) |
| State mutation by a "read-only" diagnosis | Tampering | doctor never `systemctl start`s anything (D-10); only transient `podman run --rm` probe containers (established install-proof precedent) |
| Under-reserving memory → OOM kills under embedding load | DoS | conservative reservation, typed-Unknown → conservative default (D-02), headroom BLOCK gate (D-07b) |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Qdrant disk sizing math (~5–6 KiB per 768-dim chunk incl. HNSW/payload overhead; 1 GiB ≈ ~180–230k chunks) | CTRL-06 floor sizing | Floor too low/high — low risk: the floor is a coarse BLOCK gate, and the live index is 4.2 MB against 469 GiB free; any 1–2 GiB constant is safe on the target |
| A2 | `podman system info --format '{{.Store.VolumePath}}'` is stable across podman 5.x (verified at 5.8.2 on the target; field is part of podman's public info API) | CTRL-06 statfs target | Probe seam returns not-ok → check degrades to typed-Unknown WARN (designed-for failure mode, D-07) |
| A3 | `MemoryPeak=473 MiB` cgroup reading reflects real serving load since last service start (~8 h uptime; the Phase-21 indexer ran through this service) | D-05 evidence | If peak predates a restart, the D-05 verification drive re-measures anyway — the phase plan already mandates the live measurement |

All other claims in this document are `[VERIFIED]` against the codebase or live host this session, or `[CITED]` from `.planning/` artifacts.

## Open Questions

1. **Embed context 8192 vs the STATE.md "ctx ≈ 512" flag.**
   - What we know: `embedContextLen = 8192` is a pinned orchestrate render const `[VERIFIED: orchestrate/memory.go:90]`; KV at f16 ≈ 36,864 B/token × 8192 ≈ 302 MiB; live cgroup peak 473 MiB < 512 MiB reservation.
   - What's unclear: peak under a sustained, large-payload drive (the D-05 measurement).
   - Recommendation: do NOT change the const in this phase (an orchestrate unit change + golden re-freeze, outside CONTEXT's in-scope list); let the D-05 measurement decide — only if peak > 512 MiB does the planner choose between raising the constant (in-scope per D-05) vs flagging a ctx-shrink for a later phase. Note: shrinking ctx to 512 would cap chunk size and risks the OWUI RAG chunking path — don't do it casually.
2. **Should install's `MinMemBytes` (install.go:268-270) also add the embed reservation when memory is on?**
   - What we know: the D-07b preflight headroom check covers the embedder independently; install's gate currently sums only chat terms.
   - Recommendation: planner's call; adding `+ reservation` when enabled is one line and more honest, with `memory.Footprint` as the shared source. Either way, document the choice in the plan.
3. **Doctor's non-GPU offload handling: skip vs down-rank-but-visible.**
   - What we know: both fit doctor's own report; the ROCm supersession (`doctor.go:284-295`) is the down-rank precedent; skipping is simpler and matches status's OWUI N/A precedent.
   - Recommendation: down-rank-but-visible mirrors the established "visible but non-rank-raising" honesty pattern; skip is acceptable if the health row remains. Planner decides; either resolves Pitfall 1.

## Sources

### Primary (HIGH confidence)
- Codebase reads this session: `internal/recommend/{recommend,envelope}.go`, `internal/preflight/{preflight,checks_resources}.go` (+ check-ID grep), `internal/doctor/doctor.go`, `cmd/villa/{recommend,preflight,doctor,install_memory,status,install}.go` (targeted), `internal/inference/running_offload.go`, `internal/memory/{footprint,memory}.go`, `internal/orchestrate/memory.go`, `internal/config/villaconfig.go` (fields), `cmd/villa/testdata/` goldens.
- Live target-host probes (Fedora 44 / gfx1151 / podman 5.8.2): `podman system info` (GraphRoot/VolumePath), `podman volume inspect villa-qdrant`, `podman volume ls`, `df`, `du`, `/proc/meminfo`, `systemctl --user list-units 'villa-*'`, `systemctl --user show villa-embed.service -p MemoryCurrent -p MemoryPeak`, `podman stats`, `./villa status --json`, `./villa doctor --json`.
- `.planning/` artifacts: 22-CONTEXT.md, ROADMAP.md Phase 22, REQUIREMENTS.md CTRL-01/03/06, STATE.md research flags, 18-DECISIONS.md (D-08), 19-03-SUMMARY.md (CPU-only embed hand-off).

### Secondary (MEDIUM confidence)
- Qdrant capacity-planning formula (memory ≈ vectors × dim × 4 B × 1.5) — from prior-phase research lineage + training knowledge, used only for the discretionary disk-floor sizing (tagged A1).

### Tertiary (LOW confidence)
- none.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new deps; every reused asset read at source this session.
- Architecture: HIGH — integration points line-anchored; the doctor-WARN pitfall and volume-root resolution verified live on the target host.
- Pitfalls: HIGH — Pitfall 1 reproduced live; golden/call-site blast radii enumerated by grep.
- Sizing constants (disk floor): MEDIUM — derived math, explicitly discretionary (D-07).

**Research date:** 2026-06-10
**Valid until:** ~2026-07-10 (stable in-repo seams; re-verify only the live-box state — running services, podman version — if verification is delayed)
