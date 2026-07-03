# Phase 34: Web-Search Surfacing (LANDS LAST) - Research

**Researched:** 2026-06-21
**Domain:** Go control-plane read-model surfacing (status / dashboard / doctor / backup) over an existing, proven web-search feature set (Phases 29–33)
**Confidence:** HIGH (surfacing phase over an existing codebase; every claim verified against the live source tree, not external docs)

## Summary

This is a pure **surfacing** phase: it reads a finished, verified feature (web search, Phases 29–33) and exposes it across four existing read-models — `villa status`/`--json`, the dashboard, `villa doctor`, and `villa backup`/`restore` — adding **no new web-search behavior**. The codebase has strong, repeatedly-applied precedents for every piece of work here: the `status.Report` append-only tail-field + single-golden-re-freeze (memory v2→v3, agent v3→v4), the hidden-until-data dashboard panel (`memory-panel`/`agent-panel`), doctor's own schema bump + tri-state findings, and the backup optional-entry pattern (`crush.json`/`recall-state.json`). The dominant engineering risk is *contract discipline*, not novel logic.

There is exactly **ONE genuinely new artifact**: a small persisted **verify-search result** (verdict + timestamp). `villa verify search` (`cmd/villa/verify_search.go`) currently does **not** persist anything — it runs the proof and exits. The outbound-bounded indicator across status/dashboard/doctor MUST derive from this cached result (never a config bool, never re-running the heavy netns/nft proof on a status poll). The exact store should clone `internal/recall/store.go` verbatim (fail-closed Load, atomic 0600/0700 write, own schema_version, `$XDG_DATA_HOME/villa/<file>.json`). This is the load-bearing honesty property of the whole phase.

A second important finding: the **guard-verdict counters** (strip/flag/quarantine) have **no persisted/aggregate source today**. The `metadata.guard` sub-key (`internal/websafe/loader.go:142`) is emitted **per-request** in the in-container `/load` HTTP response and is never aggregated host-side. The status block's guard counters therefore have no existing data source to read. This is a planning decision point (see Open Questions Q1) — likely the honest answer is to **omit guard counters when no source exists** (typed-Unknown / no row, per the UI-SPEC's "omit when absent" rule) rather than fabricate a counter aggregation pipeline, which would be NEW behavior and out of scope.

**Primary recommendation:** Clone `internal/recall/store.go` for a new `verify-search-state.json` persisted artifact (written by `villa verify search`, read read-only by status/doctor/dashboard). Bump `status.Report` 4→5 with a single tail-appended `*WebSearchInfo` field gated on `cfg.WebSearchEnabled`, re-freeze the three `status*.json.golden` files once. Bump `internal/doctor/doctor.go` `reportSchemaVersion` 2→3 with nil-safe web-search seams mirroring the agent fold. Add searxng/websafe as dedicated non-GPU health rows (qdrant/embed precedent — NOT the generic chat-endpoint probe). Add a `EntrySearxngSettings` optional backup entry mirroring `EntryCrushConfig`. Resolve the guard-counter source question before planning.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `web_search` block in `--json` | status core (`internal/status`) | command tier (`cmd/villa/status.go` live wiring) | Config-is-truth read-model; same core feeds CLI + dashboard (no fork) |
| Persisted verify-search result | NEW store package (clone `internal/recall`) | `cmd/villa/verify_search.go` (writer) | One mutable artifact; pure store + injected byte-I/O seam |
| searxng/websafe health rows | status core | command tier (dedicated health seams) | Non-GPU managed services — own health probe like qdrant/embed, NOT generic chat probe |
| Outbound-bounded indicator | status core (reads cached verify) | NEW store (read seam) | Derived from real cached verify result; never a config bool, never live re-probe |
| Dashboard Web Search panel | dashboard assets (`internal/dashboard/assets/dashboard.js`/`.html`/`.css`) | status core (`report.web_search`) | Hidden-until-data, fed by existing `/api/status` poll — no new endpoint/fetch |
| doctor web-search checks | doctor core (`internal/doctor`) | command tier (nil-safe seams) | doctor's own schema bump; composes status read-model + new proofs |
| Residency-under-search-load check | command tier (live proof seam) | doctor core (consumes Verdict opaquely) | Offload-assert under load; clone `runAgentResidencyUnderLoad` drive→sample→join |
| backup/restore web-search config | backup core (`internal/backup`) | command tier (optional-entry wiring) | Optional archive entry mirroring `crush.json`; manifest schema bump |

## Project Constraints (from CLAUDE.md)

These are AUTHORITATIVE and treated with the same force as locked decisions:

- **Config is the single source of truth.** Quadlet units regenerated from config, never hand-edited. The `web_search` block identity (enabled/addrs/ports) comes from `cfg`, not from re-derived state.
- **`--json`/dashboard contracts are byte-frozen by golden tests.** Evolve **append-only + schema-bump**; refreeze intentionally with `go test … -update`. New tagged fields go ABOVE `SchemaVersion` (which stays last).
- **Honesty-by-construction / typed-Unknown.** Missing/unevaluable → WARN/Unknown, NEVER a fabricated value. The outbound-bounded indicator shows green ONLY for a real recent verify PASS; stale/absent → gray Unknown, never green, never red.
- **Offload-asserting (silent CPU fallback = FAIL).** The under-search-load residency check must FAIL on a confident CPU fallback, never false-green.
- **Loopback-only binds.** Dashboard binds `127.0.0.1`; searxng/websafe publish NO host port (container-DNS only, PRIV-01).
- **No shell interpolation.** All host commands are fixed-arg `exec.Command`.
- **XSS-safe dashboard.** Every server/web-derived value via `textContent`, never `innerHTML` — especially the row-7 query/URL provenance (the most attacker-influenceable surface in the panel).
- **Inference seam grep-gate (`TestSeamGrepGate`).** Backend marker strings (`ROCm0`/`Vulkan0`/`HSA_OVERRIDE…`/image tags) must stay behind `internal/inference` + `internal/orchestrate`; the gate walks `internal/` AND `cmd/villa`. The residency-under-load seam consumes `inference.Verdict` OPAQUELY (Status/Detail/Remediation only) and resolves images only via `orchestrate.*Image()` accessors.
- **Dashboard binary trap.** After `make build`, dashboard code changes require `systemctl --user restart villa-dashboard.service`.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Area 1 — status/--json web_search block (schema 4→5)**
- ONE append-only `web_search` block; `status.Report` `schema_version` bumps 4→5 with a SINGLE golden re-freeze (mirror the memory v3 / agent v4 v-bump precedents — the "off" output differs from v4 only in `schema_version`).
- Block fields: enabled state; `villa-searxng` + `villa-websafe` health rows; guard-verdict counters (strip/flag/quarantine from Phase 32); last-query freshness; outbound-bounded indicator (Area 2).
- Web-search-OFF: output differs from the v4 contract ONLY in `schema_version` (preserves byte-identical-when-off, mirroring the agent-off precedent).

**Area 2 — outbound-bounded indicator derivation (load-bearing)**
- `villa verify search` PERSISTS its last result (verdict + timestamp) to a small state artifact; `status`/dashboard/`doctor` READ that cached result with a freshness stamp.
- NEVER a bare config bool; NEVER run the netns egress proof on a status poll / dashboard refresh (too heavy + disruptive).
- Honesty: stale or absent verify result ⇒ typed-Unknown (NOT "bounded"/PASS). Only a real, recent `villa verify search` PASS surfaces as "outbound bounded."

**Area 3 — villa doctor web-search checks (doctor's OWN schema bump)**
- Fold web-search checks into `villa doctor` on doctor's own schema bump (separate from the status 4→5 bump): searxng/websafe service readiness, guard health, egress-proof status (read the cached verify result).
- Tri-state: ready / degraded-with-reason / typed-Unknown; remediation on EVERY non-PASS (refuse-with-remediation).
- Include an offload-asserting chat-model-GPU-resident check UNDER SEARCH LOAD (a silent/partial CPU fallback under search load is a FAIL, never false-green).

**Area 4 — backup/restore web-search coverage**
- `villa backup`/`restore` cover the web-search configuration: SearXNG `settings.yml` provenance + the `WebSearchEnabled` gate, consistent with prior backup coverage.
- Fetched ephemeral web content is EXCLUDED by design.

### Claude's Discretion
- Exact Go field names/JSON keys in the `web_search` block; the cached-verify-result artifact path/format; doctor's exact schema-version value + check IDs; backup entry naming — all at the planner/executor's discretion within the decisions above.
- The dashboard panel's visual/interaction design is captured separately in the UI-SPEC.

### Deferred Ideas (OUT OF SCOPE)
- Any new web-search feature behavior (this phase only surfaces existing, proven behavior).
- v2 scope: focus modes, embedding-rerank, multi-round deep-research (per v1.5 roadmap deferrals).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SURF-04 | `villa status`/`--json` gains exactly one append-only `web_search` block (schema 4→5, single golden re-freeze): enabled state, searxng/websafe health rows, guard counters, last-query freshness, outbound-bounded indicator from the real verify result | `internal/status/status.go` Report struct + `reportSchemaVersion=4` (clone the Coding/Memory tail-field pattern at lines 152–186); the 3 goldens at `cmd/villa/testdata/status*.json.golden`; NEW persisted verify-result store (clone `internal/recall/store.go`); searxng/websafe dedicated health rows (qdrant/embed precedent, status.go:539–567); guard-counter source GAP (see Open Q1) |
| SURF-05 | Dashboard hidden-until-data XSS-safe Web Search panel — outbound visibility + bounded indicator; no new endpoint/fetch/probe | 34-UI-SPEC.md (approved); `renderAgent`/`renderMemory` precedent in `dashboard.js`; reads `report.web_search` off the existing `/api/status` poll |
| SURF-06 | `villa doctor` folds web-search checks on doctor's OWN schema bump: readiness, guard health, egress-proof status, tri-state with remediation + offload-assert under search load | `internal/doctor/doctor.go` `reportSchemaVersion=2` + nil-safe agent-fold precedent (lines 304–318); `cmd/villa/doctor.go` `runAgentResidencyUnderLoad` drive→sample→join to clone for search-load residency |
| SURF-07 | `villa backup`/`restore` cover web-search config (SearXNG settings.yml provenance + WebSearchEnabled gate); ephemeral content excluded | `internal/backup/manifest.go` `EntryCrushConfig`/`backupSchemaVersion=3`; `internal/backup/backup.go` optional-entry `sources` pattern (lines 214–227); SearXNG settings.yml at `$XDG_CONFIG_HOME/villa/searxng/settings.yml` (orchestrate/searxng_settings_write.go:50) |
</phase_requirements>

## Standard Stack

This phase adds **no external dependencies**. It uses only the existing module stack.

### Core (existing, reused)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `encoding/json` | Go 1.26.2 | Report + store marshaling | Already the contract serializer for every `--json` golden [VERIFIED: go.mod + status.go] |
| Go stdlib `os`/`path/filepath` | Go 1.26.2 | Atomic 0600 store write + XDG path resolution | The recall/usage/config store discipline [VERIFIED: recall/store.go] |
| `github.com/spf13/cobra` | v1.10.2 | `villa verify search` / `doctor` cobra wiring (already present) | Existing CLI framework [VERIFIED: go.mod via CLAUDE.md] |
| `github.com/go-chi/chi/v5` | v5.3.0 | Dashboard `/api/status` (no new route this phase) | Existing dashboard router [VERIFIED: CLAUDE.md] |
| Go stdlib `testing` + golden fixtures | Go 1.26.2 | Byte-frozen contract tests, `-update` re-freeze | The only test framework in this repo [VERIFIED: status_test.go:303] |

**No new packages. No installation step. No Package Legitimacy Audit required** (zero external packages installed).

## Architecture Patterns

### System Architecture Diagram

```
                            ┌────────────────────────────────────────┐
                            │  cfg (config.toml) — SINGLE SOURCE OF   │
                            │  TRUTH: WebSearchEnabled, Searxng*,     │
                            │  Websafe* addr/port                     │
                            └───────────────┬────────────────────────┘
                                            │
   villa verify search (heavy, on-demand)   │ reads cfg
   ──────────────────────────────────────   │
   netns/nft egress proof  ──► verdict ──────┼──► [NEW] verify-search-state.json
   (PASS/FAIL/REJECT) + ts                   │     (verdict + RFC3339 checked_at)
                                             │     $XDG_DATA_HOME/villa/...
                                             │            │ read-only
                ┌────────────────────────────┴────────────┤
                │ status core (internal/status) Run(Deps)  │
                │  builds Report.web_search ONLY when       │ reads cached verify
                │  cfg.WebSearchEnabled:                    │◄────────────────────
                │   • enabled                               │
                │   • searxng/websafe health (dedicated     │ dedicated health seams
                │     non-GPU probes, qdrant/embed pattern) │ (in-network curl, TTL-cached)
                │   • outbound_bounded (tri-state from      │
                │     cached verify; stale/absent→Unknown)  │
                │   • guard counters (SOURCE GAP — Open Q1) │
                │   • last_query_at / verify_checked_at     │
                │   • SchemaVersion 4 ──► 5                 │
                └───────┬───────────────────────┬──────────┘
                        │                        │
            CLI --json  │              /api/status (no new route)
            (3 goldens  │                        │
             re-frozen) │              dashboard.js poll() ──► renderWebSearch(report)
                        │                        │  hidden-until-data; textContent only
                        ▼                        ▼
                                    ┌──────────────────────────────┐
   villa doctor (own schema 2 ──► 3)│ doctor core Aggregate(Deps)  │
   ────────────────────────────────│  nil-safe seams (web off →    │
   composes status read-model       │  no findings, byte-identical) │
   + NEW seams:                     │   • searxng/websafe readiness │
     • egress-proof status (reads   │   • guard health              │
       cached verify result)        │   • egress-proof status       │
     • residency UNDER SEARCH LOAD  │   • residency-under-search-   │
       (offload-assert, drive→      │     load (offload FAIL        │
        sample→join)                │     dominates HTTP-200)       │
                                    └──────────────────────────────┘

   villa backup/restore
   ────────────────────
   [NEW optional entry] settings.yml (SearXNG provenance) + WebSearchEnabled gate
   (mirror EntryCrushConfig); ephemeral fetched web content EXCLUDED by design
```

### Recommended File Touch Map (no new top-level structure)
```
internal/
├── status/status.go              # +WebSearchInfo struct, +web_search field, bump 4→5,
│                                 #  +searxng/websafe dedicated rows + health seams in Deps
├── doctor/doctor.go              # +web-search findings, bump reportSchemaVersion 2→3,
│                                 #  +nil-safe web-search seams in Deps (mirror agent fold)
├── backup/manifest.go            # +EntrySearxngSettings const, bump backupSchemaVersion 3→4
├── backup/backup.go              # +SearxngSettingsPath to BackupInput + sources list
├── backup/restore.go             # +restore the settings.yml entry (mirror crush.json)
└── verifystate/ (NEW, ~clone of  # NEW persisted verify-search result store:
    internal/recall/store.go)     #  State{schema_version, verdict, checked_at}, fail-closed
                                  #  Load, atomic 0600 Save, VerifyStatePath()
cmd/villa/
├── verify_search.go              # WRITE the verify result via the new store (the ONE new write)
├── status.go (liveStatusDeps)    # wire searxng/websafe health seams + ReadVerifyState seam
├── doctor.go (liveDoctorDeps)    # wire web-search seams + runSearchResidencyUnderLoad
├── testdata/status*.json.golden  # RE-FREEZE once (3 files)
└── testdata/doctor*.json.golden  # RE-FREEZE for doctor's bump
internal/dashboard/assets/
├── dashboard.html                # +#web-search-panel section after #agent-panel (hidden)
├── dashboard.js                  # +renderWebSearch(report), panel vars, poll() call
└── dashboard.css                 # reuse existing tokens; add classes ONLY if unavoidable
```

### Pattern 1: Append-only tail-field on `status.Report` (the 4→5 bump)
**What:** Add a `*WebSearchInfo` pointer field with `json:",omitempty"` ABOVE the `SchemaVersion` field; populate it ONLY when `cfg.WebSearchEnabled`; bump `reportSchemaVersion` 4→5.
**When to use:** The single contract evolution for this phase (mirror memory v3 / agent v4 exactly).
**Example:**
```go
// Source: internal/status/status.go:152-186 (Coding/CodingInfo precedent — VERIFIED)
// In Report struct, immediately ABOVE SchemaVersion:
//
//   WebSearch *WebSearchInfo `json:"web_search,omitempty"`
//
//   SchemaVersion int `json:"schema_version"` // stays LAST
//
const reportSchemaVersion = 5 // was 4 (Phase-28 agent). v5 tail-appends web_search.

// In Run(), mirroring the cfg.AgentEnabled gate at status.go:506-508:
if cfg.WebSearchEnabled {
    report.WebSearch = webSearchInfo(cfg, d.SearxngHealth, d.WebsafeHealth, d.ReadVerifyState /*, guard source — Open Q1 */)
}
```
**Byte-identical-when-off guarantee:** With web search off, `WebSearch == nil`, the `omitempty` key is absent, and the v5 output differs from v4 ONLY in `schema_version` — exactly the memory/agent precedent (status.go:158-161).

### Pattern 2: Persisted state store (clone `internal/recall/store.go`)
**What:** A new pure store package with fail-closed `Load`, atomic 0600/0700 `Save`, its OWN `schema_version`, and a `VerifyStatePath()` resolver under `$XDG_DATA_HOME/villa`.
**When to use:** The ONE new artifact this phase needs (the cached verify result).
**Example:**
```go
// Source: internal/recall/store.go:31-156 (VERIFIED — verbatim discipline clone)
const verifyStateSchemaVersion = 1
type State struct {
    SchemaVersion int    `json:"schema_version"`
    Verdict       string `json:"verdict"`      // "PASS"/"FAIL"/"REJECT" (verdictName, verify_search_json.go:31)
    CheckedAt     string `json:"checked_at"`   // RFC3339 UTC
}
// Load fails CLOSED: absent/corrupt/future-schema ⇒ empty State (NOT a fabricated PASS).
// Save stamps SchemaVersion, marshals, writes via WriteFileAtomic(VerifyStatePath(), ...).
// WriteFileAtomic: traversal-guarded against the FIXED storeRootDir(), 0700 dir, 0600 temp+rename.
```
**Critical honesty rule:** A stale (older than a freshness window) or absent verify result MUST surface as typed-Unknown ("unavailable"), NEVER as "bounded"/green. The freshness window is at the planner's discretion (the UI-SPEC copy distinguishes "not verified" from "stale").

### Pattern 3: Dedicated non-GPU health rows for searxng/websafe (qdrant/embed precedent)
**What:** searxng/websafe ARE already rendered into units when `WebSearchEnabled=true` (render.go:244 + the searxng append), so `serviceUnits()` will produce service rows for them. But the current `Run()` loop would send them through the **generic `d.Health(endpoint)` chat probe** (status.go:569) — the SAME false-green Phase-22 fixed for qdrant/embed. They MUST get dedicated in-network health seams + their own branch.
**When to use:** Adding searxng/websafe health rows to the report.
**Example:**
```go
// Source: internal/status/status.go:539-567 (qdrant/embed VERIFIED precedent)
if svc == d.SearxngService && d.SearxngService != "" {
    ss.Health = HealthUnknown
    if d.SearxngHealth != nil {
        ss.Health = d.SearxngHealth(cfg.SearxngAddr, cfg.SearxngPort)
    }
    ss.Offload = naOffloadVerdict()
    ss.OffloadApplies = false   // non-GPU — excluded from worst-wins fold (D-12)
    ss.OffloadOK = false
    report.Services = append(report.Services, ss)
    continue
}
// ...identical branch for d.WebsafeService / d.WebsafeHealth
```
Add `SearxngService`/`WebsafeService` + `SearxngHealth`/`WebsafeHealth` to `status.Deps` (mirror `QdrantService`/`QdrantHealth` at status.go:363-371). Live wiring derives the service name via `unitServiceName(orchestrate.SearXNGContainerUnitName())` / `WebsafeContainerUnitName()` — never a typed literal.

### Pattern 4: doctor's own schema bump + nil-safe web-search fold (agent precedent)
**What:** Bump `internal/doctor/doctor.go` `reportSchemaVersion` 2→3; add nil-safe web-search seams to `doctor.Deps` consulted only when `cfg.WebSearchEnabled`; a nil seam (web off) emits NO finding so the web-off doctor output is byte-identical except the bump.
**When to use:** SURF-06.
**Example:**
```go
// Source: internal/doctor/doctor.go:304-318 (agent fold VERIFIED precedent)
// In Aggregate(), after the agent fold:
if d.SearchEgressProof != nil {       // reads cached verify result → tri-state finding
    findings = append(findings, searchEgressFinding(d.SearchEgressProof()))
}
if d.SearchResidencyUnderLoad != nil { // offload-assert under search load
    findings = append(findings, searchResidencyFinding(d.SearchResidencyUnderLoad()))
}
// + searxng/websafe readiness findings (folded from the status read-model rows, or
//   from preflight-style checks); guard health (SOURCE GAP — Open Q1).
const reportSchemaVersion = 3 // was 2 (agent fold). v3 adds web-search findings.
```
Every non-PASS finding MUST carry a `Remediation` (doctor.go:88, D-11). Tri-state via the existing PASS/WARN/FAIL grammar (doctor.go:43-50).

### Pattern 5: Residency-under-search-load proof (clone `runAgentResidencyUnderLoad`)
**What:** An offload-asserting check that drives a real search workload and samples the chat-model GPU residency MID-DRIVE (drive→settle→sample-if-still-in-flight→join), degrading to typed-Unknown when no round can be caught in flight.
**When to use:** The SURF-06 "offload-asserting chat-model-GPU-resident check UNDER SEARCH LOAD."
**Example:**
```go
// Source: cmd/villa/doctor.go:626-718 (runAgentResidencyUnderLoad VERIFIED precedent)
// Reuse: precondition gate (read-only — doctor never starts a service); drive a bounded
// search workload; sample inference.RunningOffloadVerdict over the EXACT liveStatusDeps
// input set (JournalText, Props, GTTUsed, WeightBytes, Markers=backend.ResidencyProof());
// JOIN every sampled round. A confident CPU fallback under load = FAIL (never false-green).
```
**Open question Q2:** what constitutes the "search workload" to drive the chat model under? The agent analog drives `crush run`; the memory analog drives `/v1/embeddings`. A search-load drive likely needs the chat model to run a search-augmented completion. The planner must decide the cheapest honest drive (see Open Questions).

### Pattern 6: Optional backup entry (clone `EntryCrushConfig`)
**What:** Add an `EntrySearxngSettings = "settings.yml"` manifest const + a `SearxngSettingsPath` field in `BackupInput`, wired into the `sources` list as optional (`FileMissing`-skipped); bump `backupSchemaVersion` 3→4.
**When to use:** SURF-07.
**Example:**
```go
// Source: internal/backup/manifest.go:42,58-64 + backup.go:74-80,214-227 (VERIFIED)
const EntrySearxngSettings = "searxng-settings.yml"  // optional; web-off skips it
const backupSchemaVersion = 4 // was 3. v4 adds the optional SearXNG settings entry.
// In backup.go sources (line 214):
//   {EntrySearxngSettings, in.SearxngSettingsPath, false}, // optional, FileMissing-skipped
// cmd tier sets SearxngSettingsPath ONLY on cfg.WebSearchEnabled (mirror crush gating),
//   resolved from $XDG_CONFIG_HOME/villa/searxng/settings.yml (orchestrate accessor).
```
The `WebSearchEnabled` gate itself is **already** captured by `config.toml` (the `web_search_enabled` key, villaconfig.go:138) which backup already archives as `EntryConfig` — so "the WebSearchEnabled gate" coverage is satisfied by the existing config.toml entry; the NEW entry is the `settings.yml` provenance. Restore mirrors crush.json (restore.go).

### Anti-Patterns to Avoid
- **Probing searxng/websafe via the generic chat `d.Health(endpoint)`.** That is exactly the Phase-22 false-green qdrant/embed fixed — they are non-GPU services with their OWN in-network health. Use dedicated seams.
- **Deriving outbound-bounded from `cfg.WebSearchEnabled` (a config bool).** Explicitly forbidden (Area 2). It MUST come from the cached real verify result.
- **Re-running the netns/nft egress proof on a status poll or dashboard refresh.** It needs `podman unshare --rootless-netns` + nft + a probe container and bounds real egress — far too heavy/disruptive for a ~2.5s poll. Read the cached result only.
- **Fabricating guard counters (zeros) when no source exists.** Per the UI-SPEC: absent → omit the row, never a fabricated 0 (no-false-green).
- **`innerHTML` of any server/web-derived value in the panel.** Especially the query/URL provenance — the most attacker-influenceable surface. `textContent` only.
- **Two golden re-freezes for the status block.** ONE 4→5 bump, ONE re-freeze of the three status goldens (staggered-contract-risk discipline — freeze the finished set once).
- **Re-typing a backend marker / image literal in the residency-under-load seam.** Consume `inference.Verdict` opaquely; resolve images via `orchestrate.*Image()` (TestSeamGrepGate walks cmd/villa).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Persisting the verify result | A bespoke JSON writer | Clone `internal/recall/store.go` (fail-closed Load, atomic 0600 write, traversal guard, own schema) | The store discipline (atomicity, fail-closed, mode, traversal) is already battle-tested across usage/recall/bench/config; re-rolling it risks a torn file or a fabricated-index class bug |
| searxng/websafe health probing | A new probe | The `QdrantHealth`/`EmbedHealth` in-network curl seam pattern (status.go:363-371) | TTL-cached, in-network, already proven non-false-green |
| Residency-under-load proof | A new drive→sample loop | Clone `runAgentResidencyUnderLoad` / `runResidencyUnderLoad` (doctor.go) | The in-flight-sampling discipline (sample only while verifiably in flight, else typed-Unknown) is subtle and already correct — re-rolling it reintroduces the idle-sample false-green |
| Dashboard panel rendering | New row builders / a fetch | `renderAgent`/`renderMemory` + `mutedP()`/`metricRow()`/`memoryBadgeRow()` over the existing `/api/status` poll | UI-SPEC mandates zero new fetch/endpoint and reuse of existing helpers |
| Backup optional entry | A new archive assembly path | The `EntryCrushConfig` optional-entry + `FileMissing`-skip pattern | The schema-gate + optional-skip semantics are already correct and golden-tested |
| Doctor tri-state findings | A new severity grammar | doctor's PASS/WARN/FAIL + BLOCK/WARN vocabulary + `findingFromCheck` (doctor.go:43-50) | Keeps the worst-wins fold + golden contract-independent of upstream structs |

**Key insight:** Every single surface in this phase has a direct, recent, golden-tested precedent in the same repo. The correct posture is *clone-and-adapt the nearest precedent*, not *design anew*. The only place with NO precedent (and therefore the real research risk) is the guard-counter data source (Open Q1) and the search-load drive (Open Q2).

## Runtime State Inventory

> This phase ADDS one persisted artifact and surfaces existing config. It is not a rename/refactor, but the inventory matters because of the new write + the backup coverage.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | **NEW:** `verify-search-state.json` (verdict + checked_at) under `$XDG_DATA_HOME/villa/`. Written by `villa verify search`, read read-only by status/doctor/dashboard. No prior persisted verify state exists. | Create the store (clone recall); wire the write in `cmd/villa/verify_search.go`; wire read-only seams in status/doctor live deps |
| Live service config | SearXNG `settings.yml` lives at `$XDG_CONFIG_HOME/villa/searxng/settings.yml` (0600, holds the SEARXNG_SECRET), written by `orchestrate.WriteSearxngSettings` (searxng_settings_write.go:50). NOT currently in backup. | Add as an optional backup entry (SURF-07). It is on-disk (not UI/DB-only), so a file-path source suffices |
| OS-registered state | None new. searxng/websafe are rootless Podman Quadlet units (already rendered when web search on); no Task Scheduler / launchd / systemd-name change. | None — verified by inspecting render.go (units appended on `WebSearchEnabled`) |
| Secrets/env vars | `SearxngSecret` (config), `WebsafeSecret`/`EXTERNAL_WEB_LOADER_API_KEY` (0600 env-file). These are ALREADY in config.toml (archived as EntryConfig) and the searxng.env (NOT archived). The NEW settings.yml entry contains the rendered SEARXNG_SECRET — so the backup entry inherits config.toml's 0600 sensitivity. | Confirm the settings.yml backup entry preserves 0600 on restore (mirror crush.json restore mode handling). Decide whether searxng.env also needs coverage (likely out of scope — settings.yml is the "provenance" called for) |
| Build artifacts / installed packages | None — no compiled/installed artifact carries web-search state. | None |

**Guard counters — explicitly NOT found as persisted state:** There is NO host-side aggregate counter store for strip/flag/quarantine. The `metadata.guard` sub-key is per-request, in-container, in the `/load` HTTP response (loader.go:142), and is consumed only by Open WebUI. Verified by grep across `internal/` and `cmd/` — no `quarantine`/counter store exists anywhere. This is the central planning gap (Open Q1).

## Common Pitfalls

### Pitfall 1: Guard counters have no data source
**What goes wrong:** The status block and UI-SPEC both call for `guard.{strip,flag,quarantine}` counters, but there is no persisted/aggregate counter source — only per-request `metadata.guard` inside the container `/load` response.
**Why it happens:** Phase 32 designed the guard as flag-not-block per-request (loader.go:46 comment literally says "for Phase 34 to count") but never built a counter aggregation/persistence layer.
**How to avoid:** Resolve Open Q1 BEFORE planning. The honest, in-scope option is to **omit guard-counter rows when no source exists** (typed-Unknown / no row, per the UI-SPEC "omit when absent" rule). Building a counter pipeline = NEW web-search behavior = out of scope (the phase only surfaces). Also note "quarantine" is not a current guard state — the verdict is binary `detected` + `rules` (verdict.go:15); a three-way strip/flag/quarantine taxonomy does not exist in the shipped guard.
**Warning signs:** A plan task that adds counter incrementing to the websafe service, or to OWUI, or a new metrics endpoint — all out of scope.

### Pitfall 2: searxng/websafe rows falling through to the chat-endpoint health probe
**What goes wrong:** Because both services are rendered into `serviceUnits()` when web search is on, the existing `Run()` loop reaches `ss.Health = d.Health(endpoint)` (status.go:569) and probes the CHAT endpoint for a searchengine's health — a false-green.
**Why it happens:** The loop's default branch is the inference health probe; new non-GPU services need their own branch + seam (qdrant/embed already do).
**How to avoid:** Add explicit `if svc == d.SearxngService` / `WebsafeService` branches BEFORE the default, each with a dedicated in-network health seam and `OffloadApplies=false`.
**Warning signs:** A searxng/websafe row showing the same health as villa-llama; an offload verdict on a search service.

### Pitfall 3: `last_query_at` / outbound-visibility has no source either
**What goes wrong:** The status block + UI-SPEC ask for "last query freshness" (`last_query_at`) and outbound-visibility rows (`last_query` / `last_fetched[]`). Like guard counters, there is no persisted host-side record of the last search query or fetched URLs — searches happen inside OWUI → searxng → websafe, none of which write a host-readable last-query artifact.
**Why it happens:** Same root cause as Pitfall 1 — the surfaced data was assumed to exist.
**How to avoid:** Treat these as part of Open Q1. The honest default is to OMIT these rows when no source exists (UI-SPEC already mandates omit-when-absent). Do NOT invent a query-logging pipeline (out of scope + a privacy consideration — logging fetched URLs/queries is exactly the "ephemeral web content excluded by design" the backup section forbids capturing).
**Warning signs:** A task adding query/URL logging to websafe or a new OWUI hook.

### Pitfall 4: Forgetting the dashboard restart after rebuild
**What goes wrong:** Dashboard JS/HTML changes don't appear because `villa-dashboard.service` is long-lived (serves the embedded assets from the running binary).
**Why it happens:** `villa status` runs fresh from `./villa`, but the dashboard is a persistent service.
**How to avoid:** After `make build`, `systemctl --user restart villa-dashboard.service` (CLAUDE.md GOTCHA). Verification steps for the panel must include this.
**Warning signs:** Panel not appearing despite correct code.

### Pitfall 5: Two golden re-freezes / accidental field reordering
**What goes wrong:** Adding the web_search field anywhere but immediately above `SchemaVersion`, or re-freezing goldens twice, breaks the byte-frozen append-only contract.
**Why it happens:** `SchemaVersion` MUST stay the last tagged field (status.go:163-166); the unexported `err` stays after it and never serializes.
**How to avoid:** Insert `WebSearch *WebSearchInfo` immediately above `SchemaVersion`; bump the const once; re-freeze the three status goldens once with `-update`.
**Warning signs:** A golden diff touching field ORDER, not just adding the new key + the schema_version bump.

### Pitfall 6: doctor's bump conflated with status's bump
**What goes wrong:** doctor has its OWN `reportSchemaVersion` (doctor.go:61, currently 2), INDEPENDENT of status's (currently 4). The Area 3 decision is explicit: doctor bumps on its OWN schema (separate from status 4→5).
**Why it happens:** Both are called `reportSchemaVersion` in different packages.
**How to avoid:** Bump `internal/doctor/doctor.go` 2→3 and `internal/status/status.go` 4→5 independently; re-freeze both sets of goldens.
**Warning signs:** A single schema-version change expected to cover both contracts.

## Code Examples

### Reading the cached verify result honestly in the status core
```go
// Source: internal/status/status.go:484-489 (ReadUsage/ReadRecallState nil-safe seam VERIFIED)
// + recall.Load fail-closed semantics (recall/store.go:111-132)
//
// In status.Deps:
//   ReadVerifyState func() *verifystate.State  // nil-safe; nil seam OR absent store → nil
//
// In webSearchInfo():
//   bounded := "unknown" // typed-Unknown default — NEVER green by default
//   if d.ReadVerifyState != nil {
//       if st := d.ReadVerifyState(); st != nil && st.Verdict == "PASS" && fresh(st.CheckedAt) {
//           bounded = "bounded"
//       } else if st != nil && st.Verdict != "PASS" {
//           bounded = "not-bounded"   // amber — a real recent non-PASS verdict
//       }
//       // stale or absent ⇒ stays "unknown" (gray "unavailable")
//   }
```

### Writing the verify result (the ONE new write) in cmd/villa/verify_search.go
```go
// Source: runVerifySearch (verify_search.go:699-737) — add a persist after the proof.
// The write is the ONLY mutation `villa verify search` makes; it stays out of the pure
// evalSearchVerify core (which does ZERO host I/O) and goes in the cmd-tier run path.
//
//   proof := deps.verifyFn(cmd.Context(), deps)
//   _ = verifystate.Save(liveVerifyStateDeps(), verifystate.State{
//       Verdict:   verdictName(proof.status),  // "PASS"/"FAIL"/"REJECT" (verify_search_json.go:31)
//       CheckedAt: time.Now().UTC().Format(time.RFC3339),
//   })
// A persist failure must NOT change the verb's exit code (the proof verdict is authoritative);
// surface it as a best-effort warning at most (mirror the backup RestartWarning posture).
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `status.Report` v4 (agent fold last) | v5 with `web_search` tail-field | This phase | One append-only bump, one golden re-freeze (3 files) |
| doctor `reportSchemaVersion` v2 (agent fold) | v3 (web-search fold) | This phase | Independent bump; doctor goldens re-frozen |
| backup `backupSchemaVersion` 3 (recall + crush) | 4 (SearXNG settings entry) | This phase | New optional archive entry, schema-gated on restore |
| `villa verify search` runs + exits (no persistence) | persists verdict+timestamp to a cached store | This phase | The ONE new artifact; enables honest outbound-bounded surfacing |

**Deprecated/outdated:** None. This phase introduces no replacements — it is purely additive surfacing.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Guard strip/flag/quarantine counters have NO persisted source and the honest resolution is to omit them | Pitfall 1, Open Q1 | If a counter source is expected to be built, scope is larger than "surfacing" — but building it would violate the "no new behavior" boundary. Needs user/planner confirmation. |
| A2 | `last_query_at` / outbound-visibility (searched/fetched) also have no host-side source and should be omitted when absent | Pitfall 3 | Same as A1; also a privacy consideration (logging queries/URLs conflicts with "ephemeral content excluded by design") |
| A3 | The "WebSearchEnabled gate" backup coverage is satisfied by the existing `config.toml` (EntryConfig) entry; the NEW entry is `settings.yml` provenance only | Pattern 6, SURF-07 | If a separate gate artifact is expected, an extra entry is needed (low risk — config.toml already carries `web_search_enabled`) |
| A4 | The "quarantine" guard state does not exist in the shipped guard (verdict is binary `detected` + `rules`) | Pitfall 1 | If quarantine is a real shipped state I missed, the counter taxonomy changes (verified against verdict.go — low risk) |
| A5 | The verify-state store should live under `$XDG_DATA_HOME/villa/` (recall/usage precedent), separate from config | Pattern 2 | Path is at planner discretion; wrong root only affects backup-coverage decisions |
| A6 | searxng/websafe are rendered into units (and thus appear in `serviceUnits`) only when `WebSearchEnabled=true` | Pattern 3 | Verified at render.go:208-244; if the gate differs, the dedicated-row branches still no-op safely (empty service name) |

## Open Questions

1. **What is the data source for guard counters (strip/flag/quarantine) and last-query/outbound-visibility?**
   - What we know: `metadata.guard` is per-request, in-container, in the `/load` response (loader.go:142); there is NO host-side aggregate/persisted counter or last-query store anywhere in `internal/`/`cmd/`. The verdict is binary `detected`+`rules`, not a three-way strip/flag/quarantine.
   - What's unclear: whether the phase is expected to build a counter/last-query persistence layer (which is NEW behavior, out of scope) or surface only what already exists.
   - Recommendation: **Plan to omit these rows when no source exists** (the UI-SPEC's omit-when-absent rule covers this honestly). Confirm with the user during discuss/plan that fabricating a counter pipeline is out of scope. The surfacable, real data this phase CAN honestly show: enabled state, searxng/websafe health, outbound-bounded (from cached verify), and verify freshness. Treat guard counters / last-query as honest "no source → no row."

2. **What "search workload" should the residency-under-search-load check drive?**
   - What we know: the agent analog drives `crush run` (doctor.go:626), the memory analog drives `/v1/embeddings` (doctor.go:397). A "search load" should exercise the CHAT model while a web search is in flight.
   - What's unclear: the cheapest honest drive — a search-augmented chat completion through OWUI? a direct llama-server completion with a search-tool prompt? The chat-model GPU residency is what's asserted, so the drive must keep the chat model decoding.
   - Recommendation: clone `runAgentResidencyUnderLoad`'s drive→settle→sample-if-in-flight→join shape; pick the minimal drive that keeps `villa-llama` decoding (likely a bounded chat completion). The planner should pick the drive and document the precondition gate (villa-llama + villa-searxng + villa-websafe active). Degrade to typed-Unknown when the stack isn't fully up.

3. **Freshness window for the cached verify result.**
   - What we know: Area 2 mandates stale → typed-Unknown; the UI-SPEC distinguishes "not verified" from "stale".
   - What's unclear: the exact window (hours? days?).
   - Recommendation: choose a conservative window at planner discretion (e.g. 24h); make it a single named constant in the status core so both --json and dashboard inherit it.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test (all work) | ✓ (assumed dev host) | 1.26.2 (go.mod) | — |
| rootless Podman + `systemctl --user` | residency-under-search-load proof (live, on-hardware) | ✓ on dev Strix Halo box (MEMORY.md) | v5 | proof degrades to typed-Unknown WARN off-hardware (by design) |
| nft + `podman unshare --rootless-netns` | `villa verify search` (writes the cached result) | ✓ on dev box (Phase 33 verified) | — | verify REJECTs honestly if absent; status then reads a stale/absent result → Unknown |
| AMD GPU stack (`/dev/dri`) | offload-assert under search load | ✓ on dev box | gfx1151 | off-hardware → typed-Unknown (no false-green) |

**Missing dependencies with no fallback:** None — all required tools are present on the live dev host; off-hardware paths degrade to typed-Unknown by construction.
**Missing dependencies with fallback:** All on-hardware proofs degrade to typed-Unknown WARN off-hardware (the honesty-by-construction invariant), so unit tests run anywhere.

## Validation Architecture

> nyquist_validation is `true` in `.planning/config.json` — this section is included.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven + `httptest` + byte-for-byte golden fixtures); NO third-party assertion/mocking lib |
| Config file | none (Go convention; `Makefile` drives it) |
| Quick run command | `go test ./internal/status/ ./internal/doctor/ ./internal/backup/ ./cmd/villa/ -run <Name> -x` |
| Full suite command | `make check` (`go vet ./... && go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SURF-04 | `web_search` block present when on; byte-identical-when-off except schema_version | golden + unit | `go test ./cmd/villa/ -run TestStatusJSON -x` (re-frozen) + `go test ./internal/status/ -run TestRunWebSearch -x` | ⚠️ status_test.go exists; new web-search subtest = Wave 0 |
| SURF-04 | outbound-bounded tri-state from cached verify (PASS+fresh→bounded; stale/absent→unknown) | unit | `go test ./internal/status/ -run TestWebSearchOutboundBounded -x` | ❌ Wave 0 |
| SURF-04 | searxng/websafe dedicated health rows (NOT chat probe) | unit | `go test ./internal/status/ -run TestRunSearxngWebsafeRows -x` | ❌ Wave 0 |
| SURF-04 | verify-state store fail-closed Load + atomic Save | unit | `go test ./internal/verifystate/ -run TestStore -x` | ❌ Wave 0 (new pkg) |
| SURF-04 | `villa verify search` persists the result | unit | `go test ./cmd/villa/ -run TestVerifySearchPersists -x` | ❌ Wave 0 |
| SURF-05 | panel hidden-until-data; XSS-safe (textContent); reads report.web_search | manual + (optional JS) | dashboard restart + browser check; `villa status --json` shows web_search | manual (UI-SPEC checker sign-off) |
| SURF-06 | doctor web-search findings; bump 2→3; web-off byte-identical except bump | golden + unit | `go test ./internal/doctor/ -run TestAggregateWebSearch -x` + re-frozen doctor goldens | ⚠️ doctor_test.go exists; new subtest = Wave 0 |
| SURF-06 | residency-under-search-load = offload-assert (CPU fallback → FAIL; not-in-flight → Unknown) | unit | `go test ./internal/doctor/ -run TestSearchResidencyFinding -x` | ❌ Wave 0 |
| SURF-06 | every non-PASS finding carries remediation | unit | `go test ./internal/doctor/ -run TestWebSearchFindingsHaveRemediation -x` | ❌ Wave 0 |
| SURF-07 | settings.yml optional entry present when on, skipped when off/absent; restore mirrors crush | golden + unit | `go test ./internal/backup/ -run TestBackupSearxngSettings -x` + `-run TestRestoreSearxngSettings` | ⚠️ backup_test.go/restore_test.go exist; new subtests = Wave 0 |
| SURF-07 | ephemeral fetched content NOT archived | unit (negative assertion) | `go test ./internal/backup/ -run TestBackupExcludesEphemeral -x` | ❌ Wave 0 |
| ALL | TestSeamGrepGate stays green (no leaked backend/image literal in new code) | guard | `go test ./internal/inference/ -run TestSeamGrepGate -x` | ✅ exists |

### Sampling Rate
- **Per task commit:** the targeted `-run` command for that task's package.
- **Per wave merge:** `go test ./internal/status/ ./internal/doctor/ ./internal/backup/ ./internal/verifystate/ ./cmd/villa/ ./internal/inference/`.
- **Phase gate:** `make check` green before `/gsd-verify-work`; the on-hardware residency-under-search-load + `villa verify search` persistence proven on the live Strix Halo box.

### Wave 0 Gaps
- [ ] `internal/verifystate/store_test.go` — fail-closed Load / atomic Save (clone recall/store_test.go) — covers SURF-04
- [ ] `internal/status/status_test.go` new subtests — web_search block, outbound-bounded tri-state, searxng/websafe rows — covers SURF-04
- [ ] `cmd/villa/verify_search_test.go` new subtest — persistence on each verdict — covers SURF-04
- [ ] `internal/doctor/doctor_test.go` new subtests — web-search fold, residency finding, remediation invariant — covers SURF-06
- [ ] `cmd/villa/doctor_test.go` — search-residency-under-load drive seam (clone agent-residency test) — covers SURF-06
- [ ] `internal/backup/backup_test.go` + `restore_test.go` new subtests — settings.yml entry + exclusion — covers SURF-07
- [ ] Re-freeze: `cmd/villa/testdata/status*.json.golden` (3), doctor goldens — intentional, single re-freeze each contract

## Security Domain

> `security_enforcement` is enabled (absent/true in config). This phase surfaces a privacy-critical feature, so the security posture is load-bearing.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Loopback-only binds (dashboard); searxng/websafe publish no host port (PRIV-01); the surfacing adds no new bind/route |
| V5 Input Validation / Output Encoding | yes | Dashboard XSS-safety: `textContent` for ALL server/web-derived values, especially the query/URL provenance (the most attacker-influenceable surface). No `innerHTML`. |
| V6 Cryptography / Secrets | yes | SearXNG `settings.yml` (0600, holds SEARXNG_SECRET) — the new backup entry MUST preserve 0600 on restore (mirror crush.json); never widen the mode |
| V7 Errors/Logging | yes | Do NOT log fetched URLs/search queries to a persisted host artifact (privacy + "ephemeral content excluded by design"). Outbound-visibility rows surface only what already exists, never a new log |
| V9 Communications | yes | The cached outbound-bounded indicator reflects the REAL Phase-33 egress proof; never assert "bounded" from a config bool (a false security claim) |
| V12 Files/Resources | yes | The new verify-state store: traversal-guarded against the fixed store root, atomic 0600 temp+rename (clone recall WriteFileAtomic) |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Reflected XSS via fetched-URL / search-query provenance in the dashboard panel | Tampering / Elevation | `textContent` only; UI-SPEC binding rule; the panel renders MORE untrusted data than any existing panel |
| False "bounded"/"secure" claim from a config bool or stale verify | Spoofing (of a security property) | Outbound-bounded green ONLY for a real recent verify PASS; stale/absent → gray Unknown (no false-green) |
| Secret leak via the new settings.yml backup entry (contains SEARXNG_SECRET) | Information Disclosure | Preserve 0600 on archive + restore (crush.json precedent); archive is itself 0600 |
| Persisting ephemeral web content / queries (privacy regression) | Information Disclosure | Exclude fetched ephemeral content by design (SURF-07); do not build a query/URL log to feed counters/last-query |
| Torn/forged verify-state file → fabricated "bounded" | Tampering | Fail-closed Load (corrupt/future-schema ⇒ empty ⇒ Unknown, never a fabricated PASS); atomic write |

## Sources

### Primary (HIGH confidence) — direct source-tree inspection this session
- `internal/status/status.go` — Report struct, `reportSchemaVersion=4`, Memory/Coding tail-field + nil-safe seam precedent (lines 92–186, 300–405, 430–723), qdrant/embed dedicated-row pattern (539–567)
- `internal/doctor/doctor.go` — Finding/Report, `reportSchemaVersion=2`, nil-safe agent fold (43–181, 209–355)
- `cmd/villa/doctor.go` — `runAgentResidencyUnderLoad` / `runResidencyUnderLoad` drive→sample→join (318–718)
- `cmd/villa/verify_search.go` — the full verify-search verb; confirmed NO persistence today; `verdictName` taxonomy
- `cmd/villa/verify_search_json.go` — verdict string mapping (PASS/FAIL/REJECT)
- `internal/recall/store.go` — the store-clone precedent (schema, fail-closed Load, atomic 0600 WriteFileAtomic, XDG path)
- `internal/backup/manifest.go` + `backup.go` — `EntryCrushConfig`/`backupSchemaVersion=3`, optional `sources` + `FileMissing`-skip pattern
- `internal/websafe/loader.go` (`metadata.guard` per-request, 142–145), `verdict.go` (binary detected+rules), `websafe.go`, `cmd/villa/websafe.go` — confirmed NO persisted guard counters
- `internal/orchestrate/render.go` (searxng/websafe rendered on `WebSearchEnabled`), `searxng_settings_write.go` (settings.yml at `$XDG_CONFIG_HOME/villa/searxng/`), `searxng.go`/`websafe.go` (unit-name accessors)
- `internal/config/villaconfig.go` — `WebSearchEnabled`/`Searxng*`/`Websafe*` fields
- `cmd/villa/testdata/status.json.golden` (+ status-memory, status-coding) — the 3 goldens to re-freeze; `status_test.go` `-update` mechanism
- `.planning/REQUIREMENTS.md` (SURF-04..07), `.planning/config.json` (nyquist_validation:true), `34-CONTEXT.md`, `34-UI-SPEC.md`, `CLAUDE.md`

### Secondary (MEDIUM confidence)
- None — no external sources needed (closed-world surfacing over a known codebase).

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new packages; existing module fully inventoried.
- Architecture / patterns: HIGH — every surface has a direct golden-tested in-repo precedent, verified by reading the actual source.
- Pitfalls: HIGH — the two real risks (guard-counter source gap, search-load drive) were confirmed by exhaustive grep over `internal/`+`cmd/`, not assumed.
- Open questions: MEDIUM — Q1/Q2 require a scope decision (omit-when-absent vs. build a pipeline) the planner/user must confirm; the recommended (omit) keeps the phase within "surfacing only."

**Research date:** 2026-06-21
**Valid until:** 2026-07-21 (stable — internal codebase, no fast-moving external deps)
