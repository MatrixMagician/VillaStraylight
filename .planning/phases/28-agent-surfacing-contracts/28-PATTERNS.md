# Phase 28: Agent Surfacing & Contracts - Pattern Map

**Mapped:** 2026-06-15
**Files analyzed:** 9 (5 created/modified cores + 4 cmd/asset surfaces)
**Analogs found:** 9 / 9 (every artifact has an exact in-repo precedent — surfacing-only phase)

> This phase introduces NO new mechanism. Each new artifact mirrors a proven v1.2/v1.3
> precedent. Every excerpt below is real (file:line) — copy these patterns directly.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/status/status.go` (add `Coding *CodingInfo`) | model/core | transform (read-model) | the `Memory *MemoryInfo` block in the SAME file (`status.go:149`, `MemoryInfo` at `:183`, `memoryInfo()` at `:535`) | exact |
| `cmd/villa/status.go` (human + --json render) | controller/render | request-response | the Memory/Usage render in the SAME file (`renderStatusTable` `:107`, usage loop `:131`) | exact |
| `cmd/villa/testdata/status*.golden` (re-freeze 3→4) | test fixture | byte-frozen contract | `status.json.golden` / `status-memory.json.golden` | exact |
| `internal/dashboard/assets/dashboard.{html,css,js}` (Agent panel) | component | event-driven (poll) | the Memory panel (`renderMemory` `dashboard.js:299`, shell `dashboard.html:64`) | exact |
| `internal/doctor/doctor.go` (fold agent checks) | service/core | transform (compose) | `Aggregate` + `offloadFinding`/`residencyUnderLoadFinding` (`doctor.go:184/424/456`) | exact |
| `cmd/villa/doctor.go` (live agent-check wiring) | controller | request-response | `cmd/villa/verify_agent.go` (`liveAgentVerify` `:175`, `liveVerifyAgentDeps` `:304`) | exact |
| `internal/backup/{backup,manifest,checksum}.go` (agent coverage) | service/core | file-I/O | the `ExcludedModels` BAK-01 pattern (`manifest.go:63/112`, `backup.go:70`) | exact |
| `internal/status/usage.go` surfacing (coder model id) | core | transform | the per-model `UsageTotals` core itself (`usage.go:64/74`) — no change, just surface coder id | exact |
| `internal/metrics/llamacpp.go` (cache_n/prompt_n) | core | request-response (scrape) | `ScrapeCounters` + `counterFromMap` (`llamacpp.go:191/99`) | exact |

---

## Pattern Assignments

### `internal/status/status.go` — add `Coding *CodingInfo` (SURF-01, D-01..D-04)

**Analog:** the `Memory *MemoryInfo` pointer block in the SAME file. Clone it EXACTLY.

**Pointer-block tail-append** (`status.go:141-154`) — add `Coding` directly ABOVE `SchemaVersion`, below `Memory`; nothing above moves:
```go
	// Memory is the v1.3 memory-stack summary (Phase-23 D-02): ... It is a
	// *MemoryInfo + omitempty (Pitfall 10: a non-pointer struct with omitempty still
	// serializes) so a memory-OFF install OMITS the key entirely ...
	Memory *MemoryInfo `json:"memory,omitempty"`

	// SchemaVersion is the Report contract self-version (D-07). It MUST stay the
	// LAST tagged field (append-only; new tagged fields go above it ...
	SchemaVersion int `json:"schema_version"`
```
→ New `Coding *CodingInfo \`json:"coding,omitempty"\`` goes between these two (D-01). Coding-off output is byte-identical except schema_version (D-02).

**Schema bump** (`status.go:171`): `const reportSchemaVersion = 3` → `4`. Bump ONCE at the end of the phase (D-04). Note the version-history comment block at `:162-170` — append a v4 line.

**Sidecar info type** (`MemoryInfo` at `status.go:183-191`) — clone for `CodingInfo` with snake_case + `omitempty` tails:
```go
type MemoryInfo struct {
	EmbeddingModel       string `json:"embedding_model"`
	EmbeddingDim         int    `json:"embedding_dim"`
	RecallState          string `json:"recall_state"`
	IndexedChats         int    `json:"indexed_chats,omitempty"`
	...
	EmbeddingSkew        string `json:"embedding_skew,omitempty"`
}
```
→ `CodingInfo` field set from D-03: `enabled`, `version`, `pin_match` (bool), `model`, `mode`, `residency`. Exact JSON spellings are Claude's-discretion (match the snake_case idiom), locked at plan time.

**Populating function** (`memoryInfo()` at `status.go:535-563`) — clone as `codingInfo(cfg, ...)`. Note the typed-Unknown discipline: a nil seam / unreadable store → "unknown", never a fabricated value.

**Gated population in `Run`** (`status.go:409-411`) — clone the `if cfg.MemoryEnabled` guard:
```go
	if cfg.MemoryEnabled {
		report.Memory = memoryInfo(cfg, d.ReadRecallState)
	}
```
→ `if cfg.AgentEnabled { report.Coding = codingInfo(cfg, ...) }` (gate is `config.AgentEnabled`, `villaconfig.go:122`).

**Deps seam** — add any new agent probe as a `func` field on `status.Deps` (`status.go:240-318`); a nil seam degrades to typed-Unknown (mirrors `ReadRecallState` `:317`).

---

### `cmd/villa/status.go` — human table + --json (SURF-01)

**Analog:** the Memory/Usage rendering in the SAME file.

**--json path is automatic** (`status.go:84-88`): the encoder serializes the whole `Report` — adding `Coding` to the struct surfaces it with zero render code:
```go
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	}
```

**Human table — present-only block** (`renderStatusTable`, the Usage loop at `status.go:131-136`) — clone the "render ONLY when present, never fabricated" idiom:
```go
	if r.Usage != nil {
		for _, m := range r.Usage.Models {
			fmt.Fprintf(tw, "usage %s\tprompt %d / generated %d (cumulative)\n",
				m.Model, m.Prompt.Cumulative, m.Predicted.Cumulative)
		}
	}
```
→ Add an `if r.Coding != nil { ... }` block rendering version / pin-match / model / mode / residency rows.

**Live seam wiring** (`liveStatusDeps` `status.go:175-234`) — wire any new agent seam here, mirroring `ReadUsage: liveReadUsage` (`:222`) and `ReadRecallState: liveReadRecallState` (`:232`). The dashboard reuses `*liveStatusDeps()` verbatim, so the panel is fed for free.

**Read-only store seam precedent** (`liveReadUsage` `status.go:243-265`): absent store → nil (omitted), unreadable → nil, empty → nil. NEVER writes. Clone this shape for any agent state read.

---

### `cmd/villa/testdata/status*.golden` — single 3→4 re-freeze (D-04)

**Analog:** `status.json.golden` (coding-off variant) + `status-memory.json.golden` (the feature-on variant precedent).

The 3→4 bump is the ONE intentional re-freeze of the entire phase. Produce BOTH variants together:
- coding-off golden: byte-identical to today EXCEPT `"schema_version": 4`.
- coding-on golden: a new fixture (mirror `status-memory.json.golden`) carrying the `coding` block.

Refreeze deliberately with `go test ./cmd/villa/... -run TestStatus -update` (the package-level `var update = flag.Bool(...)` convention). Append-only contract: confirm nothing above the new `coding` key moved.

---

### `internal/dashboard/assets/dashboard.{html,css,js}` — Agent panel (SURF-01, D-05)

**Analog:** the Memory panel. Mirror it EXACTLY (the UI-SPEC is binding). NO new CSS tokens.

**HTML shell** (`dashboard.html:64-66`) — clone after the Memory panel, ships `hidden`:
```html
    <section class="panel" id="memory-panel" aria-labelledby="memory-heading" hidden>
      <h2 class="panel-heading" id="memory-heading">Memory</h2>
      <div id="memory-body"></div>
    </section>
```
→ `id="agent-panel"` / `agent-heading` "Agent" / `agent-body` (UI-SPEC shell at 28-UI-SPEC.md:143).

**CSS — reuse existing classes verbatim**, introduce nothing (`dashboard.css`): `.panel` `:107`, `.panel-heading` `:113`, `.badge` + `.badge-ready/-warn/-unknown` `:156-168`, `.metric-row`/`.metric-label`/`.metric-value` `:173-182`, `.muted` `:170`.

**JS render fn** (`renderMemory` at `dashboard.js:299-352`) — clone as `renderAgent(report)`. Hidden-until-data gate:
```js
  function renderMemory(report) {
    if (!memoryPanel || !memoryBody) { return; }
    var mem = report && report.memory;
    if (!mem) { memoryPanel.hidden = true; return; }
    memoryPanel.hidden = false;
    memoryBody.textContent = "";
    memoryBody.appendChild(metricRow("embedding model", mem.embedding_model || ""));
    ...
```
→ `var ag = report && report.coding; if (!ag) { agentPanel.hidden = true; return; }`

**DOM helpers (reuse, do not re-roll):** `metricRow` (`dashboard.js:62`, mono value rows), `mutedP` (`:53`, empty/Unknown copy), `memoryBadgeRow` (`:274`, label+badge), `groupThousands` (`:85`, token totals). All set via `textContent` (XSS-safe — never HTML interpolation).

**Honesty badge mapping** (the Memory skew/recall idiom, `dashboard.js:326-351`): pin mismatch / drift → amber `memoryBadgeRow(..., "mismatch", "warn")` (NEVER red); typed-Unknown (cache unevaluable, pin unevaluable) → gray `(..., "unavailable", "unknown")`. See UI-SPEC state table (28-UI-SPEC.md:154-163) for the exact rows.

**Per-model usage reuse** (`renderCumulativeUsage` `dashboard.js:419-462`): key the coder model's totals out of `report.usage.models[report.coding.model]`; no entry → `mutedP("No usage recorded yet")` (NOT a fabricated 0). Reuse this fn's selection logic verbatim.

**Poll wiring** (`dashboard.js:758`) — add `renderAgent(report)` beside `renderMemory(report)` in the `/api/status` `.then`; the SAME poll feeds it (no new fetch/endpoint, D-05). On the `.catch` path do NOT call it (last-good content under stale dimming).

---

### `internal/doctor/doctor.go` + `cmd/villa/doctor.go` — agent checks (SURF-02, D-06/D-07)

**Analog (core):** the `Aggregate` composition + the opaque-Verdict finding mappers in `doctor.go`.

**Add nil-safe Deps seams** (`doctor.go:117-156`) — clone `ResidencyUnderLoad func() inference.Verdict` (`:155`) for the agent checks (tool-call round-trip, under-load residency). The contract is binding: "NIL-SAFE: when nil ... NO finding is emitted at all — never a PASS-by-default (no-false-green)."

**Compose in Aggregate** (`doctor.go:275-277`) — clone the residency-under-load fold:
```go
	if d.ResidencyUnderLoad != nil {
		findings = append(findings, residencyUnderLoadFinding(d.ResidencyUnderLoad()))
	}
```

**Offload-FAIL-dominates mapper** (`residencyUnderLoadFinding` `doctor.go:456-479` / `offloadFinding` `:424-447`) — clone this switch for the agent residency check. THIS is D-07 (honesty dominance) made concrete:
```go
	switch v.Status {
	case inference.StatusPass:
		f.Tier = tierBlock; f.Status = statusPass
	case inference.StatusFail:
		// Confident CPU fallback under load = a real fault (BLOCK FAIL) — never a
		// false-green over a healthy-looking stack.
		f.Tier = tierBlock; f.Status = statusFail
		f.Remediation = nonEmpty(v.Remediation, "...")
	default: // StatusWarn — could not be EVALUATED
		f.Tier = tierWarn; f.Status = statusWarn
		f.Remediation = nonEmpty(v.Remediation, "...")
	}
```
Consume `inference.Verdict` OPAQUELY (Status/Detail/Remediation only) — never type Vulkan0/ROCm0/image literals (TestSeamGrepGate). For binary/version + config drift checks, fold `internal/agent/drift.go` results via the `findingFromCheck` precedent (`doctor.go:376`) — see `agent.Drift`/`ConfigDrift` (`internal/agent/drift.go:66-112`).

**Analog (cmd wiring):** `cmd/villa/verify_agent.go`. REUSE its probes — do not re-roll.
- The tool-call round-trip driver: `liveAgentToolCallProbe` (referenced `verify_agent.go:307` as `agentTaskFn`) — the read→edit `crush run` round-trip.
- The under-load residency proof reuses `inference.RunningOffloadVerdict` (the `liveProve` seam) exactly as `status.Run` calls it (`status.go:475-485`).
- Wire the new `liveDoctorDeps()` closure mirroring `liveVerifyAgentDeps` (`verify_agent.go:304-311`) and `liveStatusDeps` — gate on `liveLoadedAgentEnabled` (`verify_agent.go:306/349`) so an agent-off stack emits no agent findings.

doctor owns its OWN schema (`reportSchemaVersion = 1`, `doctor.go:54`) — independent of status; do NOT bump it for these additive findings unless its own golden contract changes (it likely does — bump append-only and refreeze `doctor.json.golden`).

---

### `internal/backup/{backup,manifest,checksum}.go` — agent coverage (SURF-03, D-08)

**Analog:** the BAK-01 `ExcludedModels` weights pattern — config goes IN, identity-recorded binary stays OUT.

**Agent config INTO the archive** (`manifest.go:41-49` entry names, `backup.go:64-68` source paths) — add an `EntryCrushConfig = "crush.json"` entry alongside `EntryConfig`/`EntryUsage`, sourced from `internal/agent` config path (`~/.config/crush/crush.json`, see `agent.go:270`). It is read, checksummed (`checksum.sum` `:26`), and added as an `EntryChecksum` (`manifest.go:53`) like every other archive member. Gate it on `cfg.AgentEnabled` and skip-when-absent via `FileMissing` (`backup.go:104-106`) — mirror the optional qdrant/recall entries so a memory/agent-off archive stays layout-stable.

**Agent binary IDENTITY-RECORDED + EXCLUDED** (`ExcludedModel` `manifest.go:63-68`, `ExcludedModels` field `:112`) — clone this exclude+identity mechanism for the agent binary:
```go
type ExcludedModel struct {
	ID     string `json:"id"`
	Quant  string `json:"quant"`
	Ctx    string `json:"ctx"`
	Source string `json:"source"`
}
```
→ Record the agent binary's sha256 + version/pin in the manifest (a new `ExcludedAgent` field or reuse the identity-record shape), exclude the bytes. The sha256 primitive already exists: `internal/agent/install.go:177 sha256Hex` (and `backup/checksum.go:26 sum`). The pin SHA is the `CrushAsset` identity used by `agent.Install` (`install.go:52`); the on-disk binary lives at `binDir/crush` (`crushBinaryName` `install.go:33`).

**Restore re-stages like weights** (D-08): refuse-with-remediation on identity drift — reuse the `checksum.verify` + `ErrChecksumMismatch` fail-closed BLOCK (`checksum.go:20-47`) and the restore read+verify pass that consumes `ExcludedModels` for re-pull reporting.

backup owns its OWN `backupSchemaVersion = 2` (`manifest.go:33`) — bump to 3 append-only (add the agent fields ABOVE `SchemaVersion`, `manifest.go:125-127`) with the same fail-closed-on-newer gate (`m.SchemaVersion <= backupSchemaVersion`). Not golden-frozen.

---

### `internal/status/usage.go` — coder per-model attribution (USAGE-03, D-09)

**Analog:** the usage core IS the analog — no change to the engine. The coder is just another model id.

The store is already per-model keyed (`UsageTotals.Models map[string]ModelUsage`, `usage.go:74-77`; `ModelUsage.Model`, `:64`). Surfacing work is in the dashboard/status layer (above): select `report.usage.models[coder model id]`. Ensure the coder model surfaces as a distinct served model id in the usage write path (the dashboard is the sole writer). No new accounting path, no `usageSchemaVersion` bump.

The `Sample`/`foldCounter` reset-aware fold (`usage.go:83-118`) already handles per-model counters with typed-Unknown `Known` flags — reuse as-is.

---

### `internal/metrics/llamacpp.go` — cache effectiveness (USAGE-04, D-10)

**Analog:** `ScrapeCounters` + `counterFromMap` in the SAME file — clone for `cache_n`/`prompt_n`.

**Counter name literals stay HOME here** (`llamacpp.go:57-60`) — add the cache/prompt timing metric names alongside (the grep gate keeps metric literals in this package):
```go
const (
	mPromptTokensTotal    = "llamacpp:prompt_tokens_total"
	mPredictedTokensTotal = "llamacpp:tokens_predicted_total"
)
```

**Typed-Unknown counter read** (`counterFromMap` `llamacpp.go:99-105`) — clone its presence + finiteness guard for `cache_n`/`prompt_n`:
```go
func counterFromMap(m map[string]float64, name string) (uint64, bool) {
	v, ok := m[name]
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > maxCounterValue {
		return 0, false // typed-Unknown, never a fabricated count (D-05)
	}
	return uint64(v), true
}
```

**Bounded scrape reuse** (`ScrapeCounters` `llamacpp.go:191-220`) — reuse the SAME bounded request shape (`scrapeTimeout` client + `maxScrapeBody` LimitReader + `parsePromText`). Add NO second HTTP request / endpoint literal. The CACHE RATIO is computed in the surfacing layer (D-10): show the pct ONLY when BOTH `cache_n` and `prompt_n` are Known AND `prompt_n > 0`; otherwise the gray Unknown badge — NEVER a fabricated 0% (UI-SPEC percentage rule, 28-UI-SPEC.md:196-199). Surface it nil-on-Unknown via a `*float64`/typed-Optional, mirroring `Report.GenTokensPerSec` (`status.go:115`).

---

## Shared Patterns

### Hidden-until-data optional block/panel
**Source:** `status.go:409` (`if cfg.MemoryEnabled`), `dashboard.js:302` (`if (!mem) { panel.hidden = true; return; }`)
**Apply to:** the `coding` Report block AND the Agent panel.
Gate population on `cfg.AgentEnabled`; nil pointer + `omitempty` omits the JSON key; the panel re-hides when the key disappears. Coding-off output is byte-identical except schema_version.

### Typed-Unknown / no-false-green (honesty-by-construction)
**Source:** `counterFromMap` (`llamacpp.go:99`), `memoryInfo` "unknown" (`status.go:539`), `liveReadUsage` nil-on-absent (`status.go:243`), `residencyUnderLoadFinding` (`doctor.go:456`)
**Apply to:** EVERY new signal. Absent/unparseable → Unknown (gray badge / omitted key), never a fabricated 0 / 0% / false PASS. A confident offload/residency FAIL DOMINATES a healthy-looking HTTP-200 (D-07).

### Pure-core + injectable `Deps` seam + `live*Deps()` wiring
**Source:** `status.Deps` (`status.go:240`) + `liveStatusDeps` (`status.go:175`); `doctor.Deps` (`doctor.go:117`); `verifyAgentDeps` + `liveVerifyAgentDeps` (`verify_agent.go:285/304`)
**Apply to:** all new agent probes. Add a `func` field (nil-safe → typed-Unknown), wire the real host in a `live*Deps()` closure in `cmd/villa`. Cores never `os.Exit`/print.

### Append-only byte-frozen golden contract, single bump
**Source:** `reportSchemaVersion` history comment (`status.go:162-171`); `status.json.golden` / `status-memory.json.golden`
**Apply to:** the ONE 3→4 `status.Report` bump at the END of the phase. New tagged fields go ABOVE `SchemaVersion`; refreeze both coding-on/off variants together with `-update`. doctor (`reportSchemaVersion=1`) and backup (`backupSchemaVersion=2`) own SEPARATE versions — bump independently/append-only.

### Backend-literal seam lock (TestSeamGrepGate)
**Source:** `doctor.go:17-20` (consume `inference.Verdict` opaquely), `manifest.go:95-98` (image digests seam-sourced)
**Apply to:** all new code. Consume offload/residency Verdicts via Status/Detail/Remediation only; never type Vulkan0/ROCm0/HSA_OVERRIDE/image tags. Image/helper literals come from `inference`/`orchestrate` accessors (e.g. `orchestrate.EmbedImage()`, `verify_agent.go:176`). The gate walks both `internal/` and `cmd/villa`.

### Counts/identity-only (no content leakage)
**Source:** `ExcludedModel` identity-only (`manifest.go:63`), `usage` counts-only (`usage.go:64`), `metrics.Slot` narrow fields (`llamacpp.go:112`)
**Apply to:** the `coding` block, the manifest agent identity record, and cache signals — version/model/mode/residency/counts/ratio ONLY. No prompt/response/content text.

---

## No Analog Found

None. Every Phase-28 artifact maps 1:1 to a shipped precedent in this codebase.

---

## Metadata

**Analog search scope:** `internal/status`, `internal/doctor`, `internal/backup`, `internal/metrics`, `internal/usage`, `internal/agent`, `internal/dashboard/assets`, `cmd/villa`
**Files scanned:** status.go, cmd/villa/status.go, dashboard.{js,css,html}, api.go, doctor.go, verify_agent.go, manifest.go, checksum.go, backup.go, usage.go, llamacpp.go, agent/{drift,install,agent}.go, config/villaconfig.go
**Pattern extraction date:** 2026-06-15
