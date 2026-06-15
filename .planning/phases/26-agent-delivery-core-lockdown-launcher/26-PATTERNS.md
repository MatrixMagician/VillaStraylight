# Phase 26: Agent Delivery Core & Lockdown Launcher - Pattern Map

**Mapped:** 2026-06-13
**Files analyzed:** 8 new + 2 modified
**Analogs found:** 10 / 10 (every new file maps to a shipped first-party analog — zero new patterns invented)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/agent/policy.go` + `crush-policy.json` | config (embedded policy) | transform (decode embed) | `internal/preflight/floors.go` + `rocm-policy.json` | exact |
| `internal/agent/render.go` | service (pure renderer) | transform (cfg → bytes) | `internal/codingmode/codingmode.go` (pure-core shape) + Pattern-3 stdlib JSON | role-match |
| `internal/agent/version.go` | utility | transform (compare) | `internal/preflight/floors.go` `compareVersions`/`splitVersion` | exact (clone) |
| `internal/agent/drift.go` | service (pure comparator) | transform (bytes/hashes → DriftReport) | `internal/codingmode/codingmode.go` (typed Result) | role-match |
| `internal/agent/agent.go` | model (Deps + typed Result/Report) | request-response | `internal/codingmode/codingmode.go` `Deps`/`Result` | exact (clone shape) |
| `cmd/villa/code.go` | route (thin cobra caller + liveAgentDeps) | request-response | `cmd/villa/coding-mode.go` | exact |
| binary download/verify seam (wired in `code.go` / Phase 27) | service | file-I/O (stream-hash-verify-rename) | `internal/download/download.go` | role-match |
| `cmd/villa/root.go` (MODIFIED) | config (verb registration) | — | `cmd/villa/root.go:35-36` `newCodingMode()` | exact |
| XDG dir resolution (config dir / data dir helpers in `code.go`) | utility | — | `internal/config/villaconfig.go:201-219` + `internal/recall/store.go:141-149` | exact |
| rendered-`crush.json` golden fixture (`internal/agent/testdata/*.golden`) | test | — | orchestrate rendered-unit goldens (NEW append-only fixture) | role-match |

## Pattern Assignments

### `internal/agent/policy.go` + `crush-policy.json` (config, embedded policy)

**Analog:** `internal/preflight/floors.go` (lines 73-117) + `internal/preflight/rocm-policy.json`

Clone this verbatim shape. The embed is build-time data (NOT runtime input) → panic-on-malformed is correct.

**Embed + decode pattern** (`floors.go:73-117`):
```go
//go:embed rocm-policy.json
var rocmPolicyBytes []byte

type ROCmPolicy struct {
	KernelFloor   string   `json:"kernelFloor"`
	FirmwareDeny  []string `json:"firmwareDeny"`
	// ...
}

func loadROCmPolicy() ROCmPolicy {
	var p ROCmPolicy
	if err := json.Unmarshal(rocmPolicyBytes, &p); err != nil {
		panic(fmt.Sprintf("preflight: malformed embedded rocm-policy.json: %v", err))
	}
	return p
}
```

**Apply as** (per RESEARCH Pattern 1; field names are Claude's discretion):
```go
//go:embed crush-policy.json
var crushPolicyBytes []byte

type CrushPolicy struct {
	Version string                `json:"version"`     // "v0.76.0"
	Assets  map[string]CrushAsset `json:"assets"`      // key "linux/amd64"
	URLTmpl string                `json:"urlTemplate"`
}
type CrushAsset struct {
	Name         string `json:"name"`         // crush_0.76.0_Linux_x86_64.tar.gz
	SHA256       string `json:"sha256"`       // TARBALL checksum: 0f66114171270485763ffbc96f63403de5d598124c4f3841bc478c3a3a0d1ec9
	Size         uint64 `json:"size"`         // 25155696
	BinarySHA256 string `json:"binarySha256"` // EXTRACTED-binary hash (Pitfall 6 / Q2) — computed on-hardware at pin time
}
func loadCrushPolicy() CrushPolicy { /* json.Unmarshal; panic on malformed */ }
```

> **Pitfall 6 / Open-Q2 (load-bearing):** the `checksums.txt` SHA-256 (`0f66...1ec9`) is over the `.tar.gz`, NOT the extracted `crush` binary. The policy MUST also carry `binarySha256` for D-14 binary-drift, computed once by extracting the verified tarball on the Strix Halo box. Do NOT compare the installed binary hash against the tarball checksum.

`rocm-policy.json` is a flat JSON object (see `internal/preflight/rocm-policy.json`, 9 lines) — mirror that minimalism.

---

### `internal/agent/version.go` (utility, compare)

**Analog:** `internal/preflight/floors.go` (lines 141-206) — `compareVersions` + `splitVersion`

Clone **verbatim**. Returns -1/0/+1, tolerates distro suffixes (`6.18.9-300.fc44` → `[6 18 9]`), never panics. Use to compare a pinned `version` against `crush --version` output (drift / presence).

```go
func compareVersions(a, b string) int { /* floors.go:146-169 */ }
func splitVersion(v string) []int     { /* floors.go:173-206 — stops each segment at first non-digit */ }
```

---

### `internal/agent/agent.go` (model — `Deps` + typed `Result`/`DriftReport`)

**Analog:** `internal/codingmode/codingmode.go` (lines 38-44, 123-205)

Clone the **shape** (NOT the transactional state machine — Phase 26 has no capture/prove/rollback). Key conventions to copy:

1. **Package doc declares the seam + literal-free discipline** (`codingmode.go:1-38`): explicitly state `internal/agent` imports NEITHER `internal/inference` NOR `internal/detect`, holds NO backend marker tokens, and that all host effects are injected `func` fields. `TestSeamGrepGate` walks `internal/` — a leaked image tag/`Vulkan0`/device arg fails CI.

2. **`Deps` struct = injected host-effect `func` fields** (`codingmode.go:127-165`). Each field is a one-line-doc'd closure the live wiring fills:
```go
type Deps struct {
	LoadConfig  func() (config.VillaConfig, error)   // source of truth
	LookPath    func(bin string) (string, bool)      // LSP probe (D-10) — exec.LookPath, references only
	ReadConfig  func() ([]byte, error)               // on-disk crush.json (config-drift input, D-14)
	HashBinary  func() (string, bool, error)         // sha256 of $XDG_DATA_HOME/villa/bin/crush (binary-drift, D-14)
	WriteConfig func(b []byte) error                 // render output → ~/.config/crush/crush.json (0600)
	Launch      func(env []string) error             // syscall.Exec the villa-owned binary with lockdown env (D-11)
	// (Download/Extract install seam is Phase-27-wired; the verify helper lives here)
}
```

3. **Typed `Result`/`DriftReport`** (`codingmode.go:169-205`) — return a typed value, NEVER `os.Exit`, NEVER print. Carry a `Reason` (refuse-with-remediation), a discrete `BinaryDrift bool` / `ConfigDrift bool`, `BinaryAbsent bool`, and a `Warnings []Warning` for LSP-missing (D-10). The cobra caller maps these to exit codes + messages.

4. **Locally-defined value sentinels** (`codingmode.go:46-62`) — declare any status/mode constants locally so the package keeps zero `inference`/`detect` imports.

---

### `internal/agent/render.go` (service, pure renderer)

**Analog:** pure-core discipline from `codingmode.go` + config-as-source-of-truth + stdlib `encoding/json` (RESEARCH Pattern 3).

- `Render(cfg config.VillaConfig, probes []lspProbe) ([]byte, []Warning, error)` — pure, deterministic bytes. `crush.json` is a DERIVED artifact of `config.toml` (same role Quadlet units play vs config — never the authority; drift is flagged, never auto-corrected).
- **Determinism is load-bearing (Pitfall 4):** config-drift = compare on-disk vs freshly-rendered. Prefer ordered structs over maps; fix `MarshalIndent` indent + trailing newline; or compare parsed-semantic (unmarshal both → `reflect.DeepEqual`) and golden-freeze the choice.
- **LSP render is WARN-on-absence, never BLOCK** (RESEARCH Code Examples, D-10) — typed-Unknown degradation, identical posture to `floors.go`'s WARN tier:
```go
for _, pr := range probes {
	if pr.Found {
		out[pr.Key] = lspEntry{Command: pr.Command} // fixed literal — NO $() (Pitfall 1)
	} else {
		warns = append(warns, Warning{Code: "lsp_missing", Msg: "... install it ... (e.g. `go install golang.org/x/tools/gopls@latest`)"})
	}
}
```
- **No backend literals, no `-c`/sampling flags** — those live behind `internal/inference` (Phase 25). The renderer knows only a loopback URL + a model id.
- Frozen schema (top-level `$schema`/`options`/`providers`/`lsp`/`permissions`; exactly one `openai-compat` provider at `http://127.0.0.1:8080/v1`; explicit non-empty `models[]` with `villa-` prefixed id) is in RESEARCH lines 153-200 — treat that as the contract.

---

### `internal/agent/drift.go` (service, pure comparator)

**Analog:** `codingmode.go` typed-Result shape; the comparator itself is novel-but-trivial (RESEARCH Pattern 3 / D-14).

- `DetectDrift(in DriftInput) DriftReport` — handed **bytes/hashes + a freshly-rendered reference**; pure compare, zero I/O (the live reads are injected via `Deps.ReadConfig`/`Deps.HashBinary`).
- Two signals, both **surfaced with remediation, NEVER auto-corrected** (D-14): (a) binary drift = installed binary SHA-256 ≠ policy `binarySha256` (Pitfall 6); (b) config drift = on-disk `crush.json` ≠ freshly-rendered.

---

### `cmd/villa/code.go` (route, thin cobra caller + `liveAgentDeps()`)

**Analog:** `cmd/villa/coding-mode.go` — clone the whole structure.

**File-doc + marker discipline** (`coding-mode.go:21-37`): open with a doc comment stating the verb's role AND that the file is LITERAL-FREE of backend markers (`TestSeamGrepGate` walks `cmd/villa`). `villa code` does NOT render units and does NOT auto-flip coding mode (honor `TestNoAutoFlipStructuralGuard`, D-12).

**Thin cobra constructor + body-returns-int** (`coding-mode.go:230-247, 271-275`):
```go
func newCode() *cobra.Command {
	return &cobra.Command{
		Use:   "code",
		Short: "...",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runCode(cmd, liveAgentDeps())
			os.Exit(code)
			return nil
		},
	}
}
// runCode returns the int (no os.Exit in body) so tests assert output+code without a subprocess:
func runCode(cmd *cobra.Command, d *agent.Deps) int { res := agent.Run(*d, ...); /* switch res → exit + Fprintf */ }
```

**`Result`→exit mapping** (`coding-mode.go:271-328`): branch on typed Result fields, `Fprintf(errOut, ...)` for refuse/drift remediation, `Fprintf(out, ...)` for success/WARN, return `exitPass`/`exitBlocked`. Pre-`exec` flow (D-13): binary absent → remediation pointing at the Phase-27 install addon (graceful, not a crash); drift found → surface + remediation, EXIT; coding-mode OFF → WARN pointing at `villa coding-mode enter` but still launch (D-12).

**`liveAgentDeps()` closure** (clone `liveCodingModeDeps`, `coding-mode.go:337-477`): fills each `Deps` `func` field with the real host impl:
```go
func liveAgentDeps() *agent.Deps {
	return &agent.Deps{
		LoadConfig:  config.LoadVilla,
		LookPath:    func(bin string) (string, bool) { p, err := exec.LookPath(bin); return p, err == nil },
		ReadConfig:  func() ([]byte, error) { return os.ReadFile(crushConfigPath()) },
		HashBinary:  func() (string, bool, error) { /* sha256 of agentBinPath() */ },
		WriteConfig: func(b []byte) error { /* MkdirAll 0700 + WriteFile 0600, traversal-guarded */ },
		Launch:      func(env []string) error { /* syscall.Exec(agentBinPath(), args, env) — fixed-arg, no shell */ },
	}
}
```

---

### binary download/verify seam (`internal/download` reuse; install wired in Phase 27)

**Analog:** `internal/download/download.go` (lines 64-131) — the SHA-256 stream-hash-verify-then-rename discipline.

Reuse the **checksum-BEFORE-place** posture for the agent tarball (D-03, AGENT-01). Phase 26 ships the verify helper/seam; Phase 27 wires the full install. Copy the verify-then-atomic-rename core (`download.go:114-130`):
```go
if uint64(written) != asset.Size { _ = os.Remove(partPath); return err("size mismatch") }
gotSum := hex.EncodeToString(h.Sum(nil))
if !strings.EqualFold(gotSum, asset.SHA256) {        // EqualFold compare — fail-closed
	_ = os.Remove(partPath)
	return err("checksum mismatch — refusing to install an unverified Crush binary")
}
// only now: extract tar.gz → $XDG_DATA_HOME/villa/bin/crush (traversal-guarded)
```
Also reuse `assertInsideDir` (`download.go:251-268`) to confine tar extraction to `$XDG_DATA_HOME/villa/bin` (`archive/tar`+`compress/gzip`, no shell `tar`). Verify BEFORE extraction; never extract an unverified artifact.

---

### `cmd/villa/root.go` (MODIFIED — register the verb)

**Analog:** `cmd/villa/root.go:35-36` — append `newCode()` to the `root.AddCommand(...)` list (sibling to `newCodingMode()`). One-line change.

---

### XDG path helpers (in `cmd/villa/code.go` live layer)

**Analogs:**
- **Config dir** (`crush.json` at `~/.config/crush/crush.json`): use `os.UserConfigDir()` exactly as `internal/config/villaconfig.go:201-208` (`villaConfigDir`), then `filepath.Join(base, "crush", "crush.json")`.
- **Data dir** (binary at `$XDG_DATA_HOME/villa/bin/crush`): clone the `storeRootDir()` fallback chain `internal/recall/store.go:141-149`:
```go
func agentBinDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" { return filepath.Join(x, "villa", "bin") }
	if home, err := os.UserHomeDir(); err == nil { return filepath.Join(home, ".local", "share", "villa", "bin") }
	return filepath.Join("/var/tmp", "villa", "bin")
}
```
`villa code` execs `filepath.Join(agentBinDir(), "crush")` EXPLICITLY — never a PATH lookup (D-05).

---

### rendered-`crush.json` golden fixture (`internal/agent/testdata/`)

**Analog:** the orchestrate rendered-unit goldens + `cmd/villa/testdata/*.golden.json` discipline.

A **NEW append-only fixture** (Phase 26 introduces NO `status.Report`/dashboard contract changes — those are Phase 28). Use the package-level `var update = flag.Bool(...)` convention to refreeze intentionally. The golden pins render determinism (Pitfall 4).

## Shared Patterns

### Pure-core + injectable-seam + `live*Deps()`
**Source:** `internal/codingmode/codingmode.go` (Deps `func` fields) + `cmd/villa/coding-mode.go:337` (`liveCodingModeDeps`)
**Apply to:** all of `internal/agent` (pure cores) + `cmd/villa/code.go` (live wiring). Cores return typed values, never `os.Exit`/print; every host effect is an injected `func` field.

### Backend-marker seam discipline (`TestSeamGrepGate`)
**Source:** `internal/codingmode/codingmode.go:30-37` (package doc) + `cmd/villa/coding-mode.go:26-33`
**Apply to:** every new `internal/agent/*.go` and `cmd/villa/code.go`. NO image tags / `Vulkan0` / `ROCm0` / `HSA_OVERRIDE…` / device args. The agent core knows only a loopback URL + a model id. The gate walks `internal/` and `cmd/villa`.

### Embedded policy = build-time data, panic-on-malformed
**Source:** `internal/preflight/floors.go:73-117` + `rocm-policy.json`
**Apply to:** `internal/agent/policy.go` + `crush-policy.json`. Compiled-in → not runtime input → panic on malformed embed is correct (no attacker-controlled path).

### Checksum-before-place, fail-closed, atomic rename
**Source:** `internal/download/download.go:114-130` + `assertInsideDir` (251-268)
**Apply to:** the agent tarball install seam (D-03). `EqualFold` SHA-256 compare; on mismatch delete + refuse-with-remediation; traversal-guard the extraction dir.

### Typed-Unknown degradation → WARN; confident absence → BLOCK/refuse
**Source:** `internal/preflight/floors.go` WARN-tier doc (18-27) + RESEARCH Code Examples
**Apply to:** `render.go` (missing LSP server → WARN, D-10) vs install (checksum mismatch → hard refuse, D-03). Every non-pass path carries an actionable next step.

### Config is the single source of truth (derived artifacts)
**Source:** CLAUDE.md + orchestrate render pattern
**Apply to:** `render.go` — `crush.json` is regenerated from `config.toml`, never hand-edited as authority; drift flagged, never auto-corrected (D-14).

## No Analog Found

None. Every new file maps to a shipped first-party analog. The ONLY genuinely new knowledge is the externally-owned Crush v0.76.0 `crush.json` schema, which RESEARCH.md freezes (lines 153-200) — that is data/contract knowledge, not a code pattern.

## Open Items the Planner Must Lock (from RESEARCH, affect which analog applies)

- **Q1 (model-id / endpoint):** base_url is the FIXED constant `http://127.0.0.1:8080/v1` (`internal/inference/backend_vulkan.go:30` `serverPort = 8080` / `containerRunner.Endpoint()` `runner_podman.go:118`), NOT a `config.toml` port field. Decide: (i) add `--alias villa-<model>` to the coding-mode render delta (touches the `internal/inference` seam + a NEW append-only golden, Phase-25-style) so the served id equals `crush.json`'s id; OR (ii) keep Phase 26 render-only relying on llama.cpp single-model leniency. Option (ii) keeps Phase 26 inside the render-only analog set.
- **Q2 (binary hash):** `crush-policy.json` must carry `binarySha256` (extracted-binary hash) in addition to the tarball `sha256` — computed once on the Strix Halo box at pin time (Pitfall 6).
- **Q3 (permissions):** render `permissions` minimal/omitted (default-prompt) in Phase 26; the exact restrictive allowlist is the Phase-27 STRIDE pass. Do NOT render allow-all.

## Metadata

**Analog search scope:** `internal/preflight`, `internal/codingmode`, `internal/download`, `internal/config`, `internal/recall`, `internal/inference`, `cmd/villa`
**Files read in full:** `internal/preflight/floors.go`, `internal/codingmode/codingmode.go`, `cmd/villa/coding-mode.go`, `internal/download/download.go`, `internal/preflight/rocm-policy.json` (+ targeted reads of `villaconfig.go`, `recall/store.go`, `root.go`)
**Pattern extraction date:** 2026-06-13
