# Phase 27: Install Addon, Preflight Gates & `villa verify agent` - Pattern Map

**Mapped:** 2026-06-14
**Files analyzed:** 9 (5 new, 4 modified)
**Analogs found:** 9 / 9 (all exact — this is a composition phase; every new file mirrors a proven anchor seam-for-seam)

This is a COMPOSITION phase. There are NO no-analog files. Every new file is a clone of an existing `cmd/villa` anchor with the noun swapped (`embed`→`coder`, `memory`→`agent`, `rag`→`agent`). The three reuse anchors are `install_memory.go`, `verify_memory.go`, `uninstall.go`; the consumed cores are `internal/agent` (Phase 26), `internal/recommend/coder.go` (Phase 24), `internal/preflight`, `internal/download`, `internal/config`.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/villa/install_agent.go` (NEW) | command seam (addon install) | file-I/O + request-response | `cmd/villa/install_memory.go` | exact |
| `cmd/villa/verify_agent.go` (NEW) | command seam (runtime proof) | event-driven / request-response | `cmd/villa/verify_memory.go` | exact |
| `cmd/villa/preflight_agent.go` (NEW, or fold into install_agent.go) | command seam (preflight tiers) | transform (verdict) | `internal/preflight/preflight.go` tiers + `cmd/villa/install.go` `runMemoryChecks` fold | exact |
| `cmd/villa/install.go` (MODIFY) | command (lifecycle verb) | request-response | own memory-addon fold (lines 299-301, 443-450, 527-544) | exact (self) |
| `cmd/villa/verify.go` (MODIFY) | command (verb tree) | request-response | own `newVerifyMemory()` registration (line 75) | exact (self) |
| `cmd/villa/uninstall.go` (MODIFY) | command (ordered teardown) | file-I/O | own `uninstallDeps` ordered seam (lines 52-85) | exact (self) |
| `internal/config/villaconfig.go` (MODIFY) | model (config struct) | CRUD | own `MemoryEnabled` field (line 62) | exact (self) |
| `cmd/villa/install_agent_test.go` + `verify_agent_test.go` + uninstall/install test extensions (NEW/MODIFY) | test | table-driven | `install_memory_test.go` / `verify_test.go` / `install_test.go:1389` single-source test | exact |
| consumed: `internal/agent/{install,render,policy,drift}.go`, `internal/recommend/coder.go` | service/model | — | NO CHANGES (composed, not modified) | n/a |

---

## Pattern Assignments

### `cmd/villa/install_agent.go` (NEW — command seam, file-I/O + request-response)

**Analog:** `cmd/villa/install_memory.go`

Mirror the four memory seams: shard pre-stage source → presence-skip → ensure-download → readiness verdict. The ONE structural delta from the memory analog: the coder GGUF is a **catalog entry** (resolve `shards[0]` from the `pickCoder`-selected entry), NOT a hard-coded literal like `nomicEmbedShard`. This makes D-02 (pick selects) and D-04 (single source) hold by construction.

**Shard-pre-stage pattern** — analog `install_memory.go:44-117`. The memory analog uses a hard-coded literal:
```go
// install_memory.go:56 — the ANTI-pattern for coder (do NOT clone a coderShard literal)
var nomicEmbedShard = catalog.Shard{ URL:..., Filename:..., SHA256:..., SizeBytes: 146146432 }
```
Instead resolve from the picked entry (research §1). `recommend.Recommendation.Coder` is `CoderFit` (`internal/recommend/coder.go:28`); `CoderFit.Model` is the picked coder id. The catalog model carries `Shards []catalog.Shard`:
```go
func coderShardFor(rec recommend.Recommendation, cat catalog.Catalog) (catalog.Shard, bool) {
    for _, m := range cat.Models {
        if m.ID == rec.Coder.Model && len(m.Shards) > 0 {
            return m.Shards[0], true // single-shard coder entries (catalog FROZEN, Phase 24; A4)
        }
    }
    return catalog.Shard{}, false // no coder fit / not found → refuse-with-remediation
}
```

**Presence-skip pattern** — clone `liveEmbedModelPresent` (`install_memory.go:93-102`): `os.Stat` + size-match → present, never re-pull. Parameterize by the resolved shard (research §2):
```go
func liveCoderModelPresent(modelsDir string, sh catalog.Shard) bool {
    fi, err := os.Stat(filepath.Join(modelsDir, sh.Filename))
    if err != nil { return false }
    return fi.Size() >= 0 && uint64(fi.Size()) == sh.SizeBytes
}
```

**Ensure-download pattern (single sanctioned outbound window)** — clone `liveEnsureEmbedModel` (`install_memory.go:104-117`): MkdirAll 0700, wrap shard in a single-shard `catalog.CatalogModel`, call the `pullFn` seam (`== download.PullModel`, `internal/download/download.go:64`, signature `PullModel(ctx, m catalog.CatalogModel, modelsDir string) error`).

**Agent binary install pattern** — compose the Phase-26 seam, never re-implement (research §3). `agent.Install(asset agent.CrushAsset, r io.Reader, binDir string) (string, error)` (`internal/agent/install.go:52`) does checksum-before-extract. Resolve the pinned asset + URL via the policy loader (`internal/agent/policy.go`: `CrushPolicy{Version, Assets map[string]CrushAsset, URLTmpl}`, key `"linux/amd64"`, `{asset}` placeholder in `URLTmpl`). NOTE: `loadCrushPolicy()` is currently **unexported** — the plan must export a loader (e.g. `agent.LoadCrushPolicy()`) or add an `agent.InstallPinned(r, binDir)` wrapper. Bin dir target is `agentBinDir()` and the placed binary is `agentBinPath()` (`cmd/villa/code.go:205,217` → `$XDG_DATA_HOME/villa/bin/crush`). REUSE these, do not re-derive.

**`crush.json` render pattern** — compose `agent.Render(cfg config.VillaConfig, probes []agent.LSPProbe) ([]byte, []agent.Warning, error)` (`internal/agent/render.go:136`) + the `agent.Deps.WriteConfig func(b []byte) error` seam (`internal/agent/agent.go:255,273`); `agent.Run(d Deps)` (`agent.go:99`) already does first-run write. The global config path is `crushConfigPath()` (`cmd/villa/code.go:194` → `~/.config/crush/crush.json`). The plan should render `permissions.allowed_tools` (view/edit/write) + `options.disabled_tools` (fetch/agentic_fetch/download/sourcegraph) — confirm field support in the security pass (Pitfall 3, Pitfall 5).

**Tool-call readiness verdict (pure core, PASS/FAIL only)** — clone `evalMemoryProof` (`install_memory.go:204-236`). Reuse the `memoryProof{status preflight.Status; detail string}` Verdict shape (`install_memory.go:187`) OR a parallel `agentProof` of the same shape. A health-200 NEVER reaches this core — the only input is the real tool-call result (research §4):
```go
func evalAgentProof(toolCall func() (edited bool, err error)) agentProof {
    edited, err := toolCall()
    if err != nil { return agentProof{preflight.StatusFail, "...could not complete a tool-call round-trip (" + err.Error() + ")... re-run `villa install --coding-agent`"} }
    if !edited { return agentProof{preflight.StatusFail, "...ran but did not perform the tool-call edit..."} }
    return agentProof{preflight.StatusPass, "tool-call round-trip (read→edit→result) completed against the local endpoint"}
}
```
The live tool-call driver execs the villa-owned binary fixed-arg (`agentBinPath()`, `crush run "<prompt>"`), planting a TOKEN_A file and asserting it now contains TOKEN_B + exit 0 (Open Q1 — confirm payload on-hardware, bound with a timeout = FAIL).

---

### `cmd/villa/verify_agent.go` (NEW — command seam, negative-control-first runtime proof)

**Analog:** `cmd/villa/verify_memory.go` (mirror the four-layer seam EXACTLY).

**Layer 1 — Verdict type:** reuse `memoryProof{status preflight.Status; detail string}` (`install_memory.go:187`), PASS/FAIL only, no WARN (D-06).

**Layer 2 — pure negative-control-FIRST core:** clone `evalRagSmoke` (`verify_memory.go:70-102`). Fold BOTH controls into one verdict (research §6):
```go
func evalAgentVerify(
    egressBlocked func() (bool, error),                  // ctrl1 negative control FIRST
    agentTask func() (completed bool, err error),        // ctrl1 real task under egress block
    llamaDownTask func() (answered bool, err error),     // ctrl2 same task, villa-llama STOPPED
) memoryProof {
    blocked, err := egressBlocked()
    if err != nil { return memoryProof{preflight.StatusFail, "could not run the egress negative-control probe..."} }
    if !blocked { return memoryProof{preflight.StatusFail, "egress is NOT blocked: an external host was reachable..."} }
    completed, err := agentTask()
    if err != nil || !completed { return memoryProof{preflight.StatusFail, "the agent task did not complete under the egress block..."} }
    answered, _ := llamaDownTask() // an error here is the EXPECTED inference-down outcome
    if answered { return memoryProof{preflight.StatusFail, "the agent ANSWERED with villa-llama stopped — silent cloud-model fallback detected; FAILS verification"} }
    return memoryProof{preflight.StatusPass, "zero-outbound agent task completed; no cloud fallback (llama-down control failed as expected)"}
}
```
This mirrors `evalRagSmoke`'s exact structure: negative control returns FAIL on err (cannot run) and FAIL on `!blocked` (reachable) BEFORE the real task is trusted (`verify_memory.go:72-84`).

**Layer 3 — live seam:** clone `liveRagSmoke` (`verify_memory.go:142-160`). The egress probe is verbatim (research §7):
```go
helperImage := orchestrate.EmbedImage() // internal/orchestrate/memory.go:47 — seam accessor, NEVER a re-typed image literal (TestSeamGrepGate walks cmd/villa)
egressBlocked := func() (bool, error) {
    _, err := runProbeCurl(ctx, helperImage, "-sf", "--max-time", "5", egressNegativeControlHost) // const "https://huggingface.co/" (verify_memory.go:110)
    return err != nil, nil
}
```
`agentTask`/`llamaDownTask` are host-side `crush run` drivers using `agentBinPath()` fixed-arg (NOT `runProbeCurl` — those drive a host binary, not an in-network curl; mirror `runLoopbackCurl` host-process discipline at `verify_memory.go:422-443`). `llamaDownTask` requires stopping `villa-llama.service` via an injected systemd seam (`orchestrate.NewSystemd().Stop`), then restoring.

**Layer 4 — fixed-arg exec:** reuse `runProbeCurl` verbatim (`install_memory.go:350-369`: `podman run --rm --network villa --entrypoint curl <helperImage> <args...>`). No shell, no new cap-root tooling (D-07).

---

### `cmd/villa/preflight_agent.go` (NEW or folded — command seam, verdict transform)

**Analog:** `internal/preflight/preflight.go` tier helpers + `cmd/villa/install.go` memory-check fold.

Return `[]preflight.CheckResult` appended to `checks[]` via a nil-safe seam (research Pattern 2), exactly like memory:
```go
// install.go:299-301 (the precedent to mirror)
if d.loadedMemoryEnabled() && d.runMemoryChecks != nil {
    checks = append(checks, d.runMemoryChecks(profile)...)
}
```
Use the `preflight` builders: `CheckResult{ID, Name, Tier, Status, Detail, Remediation, Provenance}` (`preflight.go:86-105`). Tiers: `TierBlock`/`TierWarn` (`preflight.go:31-39`); statuses `StatusPass`/`StatusWarn`/`StatusFail` (`preflight.go:55-66`). Helpers `pass`/`warn`/`fail` are package-private to `preflight` — the cmd-tier check builders construct `CheckResult` literals directly (mirror how memory checks are built), OR add exported agent-check helpers in `internal/preflight` if a new check ID needs the tier machinery.

- **disk BLOCK:** staged coder shard `SizeBytes` + binary `asset.Size`. The install disk path already statfs's via `preflight.ResourceReq.MinDiskBytes` + `liveStatfs` (`install.go:288-292`) — reuse that machinery; do not hand-roll statfs.
- **post-coder envelope BLOCK:** drive from `rec.Coder` (`CoderFit`, `internal/recommend/coder.go:28-48`): read `Fits`/`TotalBytes`/`WeightBytes`/`KVCacheBytes`/`HeadroomBytes`/`Residency` — NEVER re-derive (anti-pattern). `Fits == false` (Residency "shared") is the BLOCK basis at agent-profile ctx.
- **cloud-credential WARN:** `os.LookupEnv` over the fixed allowlist (research §"Cloud-credential WARN allowlist": ANTHROPIC/OPENAI/OPENROUTER/GEMINI/GOOGLE_GENERATIVE_AI/GROQ/XAI/MISTRAL/DEEPSEEK/AZURE_OPENAI/CRUSH keys). Presence → WARN naming the var(s) + the neutralization, never BLOCK (D-09). Typed-Unknown (unprobeable) → WARN.

---

### `cmd/villa/install.go` (MODIFY — fold `--coding-agent` flag + persisted gate)

**Analog:** its own memory-addon fold pattern.

- Add `--coding-agent` flag in `newInstall()` (mirror `--no-tui` at `install.go:254-255`): when set, override+persist `cfg.AgentEnabled = true` before the gate.
- Add a `loadedAgentEnabled func() bool` seam to `installDeps` (mirror `loadedMemoryEnabled`, `install.go:174`; live `liveLoadedMemoryEnabled` at `install_memory.go:140-146` — fail-soft to false).
- Add a `runAgentChecks func(detect.HostProfile, recommend.Recommendation) []preflight.CheckResult` seam, appended at the memory-fold site (`install.go:299-301`). NOTE: agent checks need `rec` (for `rec.Coder` + staged size), unlike `runMemoryChecks(profile)` which takes only profile.
- Gate the pre-stage/install/render/readiness block on `cfg.AgentEnabled && !opts.dryRun`, mirroring the embed pre-stage block (`install.go:443-450`) and the memory start/proof blocks (`install.go:527-544`, `556-580`).
- Persist `cfg.AgentEnabled` via the existing `saveConfig` seam (`install.go:457`).
- **Addon-off byte-identical:** the field is `,omitempty` so a memory-off / agent-off render is unchanged (D-01, Pitfall 6). Assert this by extending `install_test.go`.

---

### `cmd/villa/verify.go` (MODIFY — register the new subcommand)

**Analog:** its own `newVerifyMemory()` registration. Add `cmd.AddCommand(newVerifyAgent())` next to line 75. Clone the `newVerifyMemory()`/`runVerifyMemory()`/`verifyMemoryDeps`/`liveVerifyMemoryDeps()` quartet (`verify.go:42-142`) into the verify-agent file (or keep the thin cobra wiring here and the logic in `verify_agent.go`, matching the verify_memory split). Gate on the persisted `AgentEnabled` (memory-off exits 0 with a clear message — `verify.go:113-116`).

---

### `cmd/villa/uninstall.go` (MODIFY — agent teardown ordering)

**Analog:** its own `uninstallDeps` ordered seam (lines 52-85; ordering IS the contract).

Add two ALWAYS-removed seams (research §5):
```go
type uninstallDeps struct {
    // ... existing ...
    removeAgentBinary func() error // ALWAYS removes agentBinPath() ($XDG_DATA_HOME/villa/bin/crush); idempotent (absent = ok)
    removeCrushConfig func() error // ALWAYS removes crushConfigPath() (~/.config/crush/crush.json); idempotent
}
```
- Wire live in `liveUninstallDeps()` (`uninstall.go:284-306`) reusing `agentBinPath()`/`crushConfigPath()` from `code.go` (DRY) with a traversal-guarded `os.Remove` tolerating `os.IsNotExist` (mirror `removeUnitFileLive`, `uninstall.go:311-320`, and its `assertUnitInsideDir` guard at 325-342).
- Insert the removals in the ordered teardown body (`runUninstall`, after step 5 or alongside the dashboard teardown — choose a deterministic position; ordering is asserted by `uninstall_test.go`).
- **Staged coder GGUF:** NO new seam — it lives in `modelsDir()`, governed by the EXISTING `removeModels`/keep-models choice (`uninstall.go:401-409`, `resolveModelChoice` 244-256). D-10.
- **config.toml:** NO seam (LEFT in place — `uninstall.go:235`). The deliberate-absence invariant (lines 49-51) holds.

---

### `internal/config/villaconfig.go` (MODIFY — `[agent] enabled` gate field)

**Analog:** the `MemoryEnabled` field (line 62) — mirror exactly:
```go
// AgentEnabled gates the v1.4 coding-agent addon. Default false (D-01); ,omitempty so an
// agent-off install is byte-identical on disk. NOT self-healed in normalizeVilla (a
// meaningful explicit toggle, like MemoryEnabled / CodingMode).
AgentEnabled bool `toml:"agent_enabled,omitempty"`
```
Append-only (mirrors the v1.3 memory-stack + v1.4 coding-mode field discipline, lines 52-112). Do NOT add it to `normalizeVilla` self-healing. Note the precedent of the `CodingMode`/`CoderModel`/`CoderAgentCtx` fields (lines 99-112) which the served `-m` path derives from — the readiness/verify endpoint rides the coding-mode `--jinja` unit.

---

## Shared Patterns

### Verdict type (Verdict / PASS-FAIL honesty)
**Source:** `cmd/villa/install_memory.go:187` (`memoryProof{status preflight.Status; detail string}`)
**Apply to:** `evalAgentProof` (install readiness) and `evalAgentVerify` (runtime proof). PASS/FAIL only — no WARN, no silent skip; a timeout/unevaluable = FAIL (honesty-by-construction).

### Negative-control-FIRST egress proof
**Source:** `cmd/villa/verify_memory.go:70-84` (`evalRagSmoke`) + `:147-152` (`liveRagSmoke` egress probe) + `runProbeCurl` (`install_memory.go:350-369`)
**Apply to:** `verify_agent.go` ctrl1. Assert the external host is unreachable (`egressNegativeControlHost`, `verify_memory.go:110`) BEFORE trusting the agent task. `helperImage := orchestrate.EmbedImage()` — the seam accessor, NEVER a re-typed image literal.

### Pure-core + injectable-seam + `live*Deps`
**Source:** `cmd/villa/verify.go:42-64` (`verifyMemoryDeps`/`liveVerifyMemoryDeps`), `install.go:82-215` (`installDeps`)
**Apply to:** all new command files. Host effects (download, install, `crush run`, podman/curl, systemd stop) are injected `func` fields; pure verdict cores are unit-testable off-hardware; live wiring is a `live*Deps()` closure.

### Fixed-arg exec, no shell
**Source:** `install_memory.go:350-369` (`runProbeCurl`), `verify_memory.go:422-443` (`runLoopbackCurl`), `internal/agent/install.go` (no shell `tar`)
**Apply to:** every host command in the new files. The coder id is catalog-resolved; the `crush run` payload uses constant prompts + planted files (no metachars).

### Single-source filename discipline
**Source:** `cmd/villa/install_test.go:1389-1392` (`TestEmbedGGUFFilenameSingleSource`) + `orchestrate.EmbedGGUFFilename()` (`internal/orchestrate/memory.go:58`)
**Apply to:** the coder GGUF. Mirror with `TestCoderShardSingleSource`: the staged shard filename (`coderShardFor(rec,cat).Filename`) must equal the served `-m` path derived from the same catalog entry (D-04). Since both derive from the picked catalog `shards[0]`, the assertion is "resolves to one entry," not "two literals match."

### Traversal-guarded idempotent removal
**Source:** `cmd/villa/uninstall.go:311-342` (`removeUnitFileLive` + `assertUnitInsideDir`)
**Apply to:** `removeAgentBinary`/`removeCrushConfig` — guard the path inside its XDG dir, tolerate `os.IsNotExist` for idempotent re-uninstall.

### Checksum-before-extract / verified download (don't hand-roll)
**Source:** `internal/agent/install.go:52` (`Install`) + `internal/agent/policy.go:88` (`VerifyTarball`), `internal/download/download.go:64` (`PullModel`)
**Apply to:** binary + GGUF staging. Compose; never re-implement the size-then-SHA256 verify, atomic rename, or tar extraction.

---

## No Analog Found

None. Every file maps to an exact analog (the defining property of this composition phase).

---

## Seam-Gate Caution (cross-cutting)

`TestSeamGrepGate` (`internal/inference/seam_test.go`) walks BOTH `internal/` and `cmd/villa`. The new files must NOT contain any backend-marker literal (image tag, `Vulkan0`/`ROCm0`, device arg). The `runProbeCurl` helper image MUST come from `orchestrate.EmbedImage()`. Run `go test ./internal/inference/ -run TestSeamGrepGate` + `make check` per task. Phase 27 changes NO golden contract (Phase 28 owns surfacing); agent-off render stays byte-identical (D-01, Pitfall 6).

## Metadata

**Analog search scope:** `cmd/villa/` (install_memory.go, verify_memory.go, verify.go, uninstall.go, install.go, code.go), `internal/agent/`, `internal/recommend/coder.go`, `internal/preflight/preflight.go`, `internal/download/download.go`, `internal/config/villaconfig.go`, `internal/orchestrate/memory.go`
**Files scanned:** ~14
**Pattern extraction date:** 2026-06-14
