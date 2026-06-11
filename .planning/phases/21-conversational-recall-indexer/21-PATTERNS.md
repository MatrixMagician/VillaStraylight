# Phase 21: Conversational Recall Indexer - Pattern Map

**Mapped:** 2026-06-10
**Files analyzed:** 11 new/modified files
**Analogs found:** 11 / 11 (every new file has an in-repo analog; no "no analog" gaps)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/recall/recall.go` (Plan diff core + types) | service (pure core) | transform (diff/plan) | `internal/modelswap/modelswap.go` (typed-Result ordering core) + `internal/usage/usage.go` Fold | role-match (strong) |
| `internal/recall/transcript.go` (RenderTranscript) | utility (pure renderer) | transform | `internal/usage/usage.go` `Fold`/`foldCounter` (pure, copy-not-mutate) + RESEARCH `linearThread` example | role-match |
| `internal/recall/staleness.go` (typed-Unknown classification) | service (pure core) | transform | `internal/memory/memory.go` `Decide` (typed Decision, reason accumulation) | exact (same shape: cfg/state in → typed verdict out) |
| `internal/recall/store.go` (`recall-state.json`) | model/store | file-I/O | `internal/usage/usage.go` (whole file — clone, don't import) | exact |
| `internal/recall/*_test.go` (recall/transcript/staleness/store) | test | — | `internal/usage/usage_test.go`, `internal/memory/memory_test.go` (table-driven, stdlib testing) | exact |
| `cmd/villa/recall.go` (newRecall/newRecallIndex/newRecallStatus + recallDeps + run bodies) | controller (cobra verb) | request-response (CLI) | `cmd/villa/verify.go` (gated verb + Deps + return-not-Exit) | exact |
| `cmd/villa/recall_live.go` (liveRecallDeps REST drives) | service (live seam) | request-response (REST over loopback) | `cmd/villa/verify_memory.go` (the whole REST drive) | exact |
| `cmd/villa/recall_test.go` | test | — | `cmd/villa/verify_memory_test.go` (gate spy + fake Deps) | exact |
| `cmd/villa/root.go` (register `newRecall()`) | config (command registration) | — | itself, line 35-36 | exact (one-line edit) |
| `cmd/villa/verify_memory.go` (lift shared helpers for reuse) | service (refactor in place) | request-response | itself — helpers are already package `main`; no move needed unless splitting into a shared file | exact |
| `docs/MEMORY.md` (append recall section) | doc | — | itself — existing `##` section structure (e.g. "Proving zero-outbound with `villa verify memory`" at line 165) | exact |

---

## Pattern Assignments

### `cmd/villa/recall.go` (controller, CLI request-response)

**Analog:** `cmd/villa/verify.go` (142 lines — read it whole before writing)

**Imports pattern** (`cmd/villa/verify.go:22-31`):
```go
import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)
```
(goimports grouping: stdlib / cobra / project — enforced by `.golangci.yml`.)

**Deps struct + live wiring pattern** (`cmd/villa/verify.go:42-64`) — copy this shape for `recallDeps`/`liveRecallDeps`:
```go
type verifyMemoryDeps struct {
	// loadedMemoryEnabled is the AUTHORITATIVE memory gate source — the PERSISTED
	// config.LoadVilla().MemoryEnabled (live: liveLoadedMemoryEnabled, failing soft to
	// false so a broken config never silently claims memory is on). Reused from install.
	loadedMemoryEnabled func() bool
	loadedConfig func() config.VillaConfig
	ragSmokeFn func(ctx context.Context, in ragSmokeInput) memoryProof
}

func liveVerifyMemoryDeps() verifyMemoryDeps {
	return verifyMemoryDeps{
		loadedMemoryEnabled: liveLoadedMemoryEnabled,
		loadedConfig:        liveLoadedConfig,
		ragSmokeFn:          liveRagSmoke,
	}
}
```
`liveLoadedConfig` / `liveLoadedMemoryEnabled` already exist at `cmd/villa/install_memory.go:124` / `:138` — reuse, do not duplicate.

**Parent verb + subcommand pattern** (`cmd/villa/verify.go:67-101`):
```go
func newVerify() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Validate a running villa stack (runtime proofs)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newVerifyMemory())
	return cmd
}

func newVerifyMemory() *cobra.Command {
	return &cobra.Command{
		Use:   "memory",
		Short: "...",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runVerifyMemory(cmd, args, liveVerifyMemoryDeps()))
			return nil
		},
	}
}
```
For recall: `newRecall()` parent with `newRecallIndex()` + `newRecallStatus()` children; flags (`--rebuild`, optional `--json`) are local cobra flags on the subcommand, like `var update`-style locals — NOT new persistent root flags.

**Return-not-Exit run body + gate + exit mapping** (`cmd/villa/verify.go:109-142`):
```go
func runVerifyMemory(cmd *cobra.Command, _ []string, deps verifyMemoryDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	if !deps.loadedMemoryEnabled() {
		fmt.Fprintln(out, "verify memory: the memory stack is not enabled (memory_enabled=false) — nothing to verify. ...")
		return exitPass
	}
	cfg := deps.loadedConfig()
	...
	proof := deps.ragSmokeFn(cmd.Context(), in)
	if proof.status == preflight.StatusFail {
		fmt.Fprintf(errOut, "verify memory: ... FAILED: %s\n", proof.detail)
		return exitBlocked
	}
	fmt.Fprintf(out, "verify memory: %s\n", proof.detail)
	return exitPass
}
```
**CRITICAL DELTA (D-07 / Pattern 4 in RESEARCH):** `verify memory` exits 0 when memory is off; `recall index`/`status` must instead **return `exitBlocked` with remediation** ("enable memory_enabled and run villa install first") — an explicit index request cannot honestly no-op. Keep everything else (Fprintln to `cmd.OutOrStdout()`, stderr for remediation, returned int) identical.

**Exit constants** — reuse the AUTHORITATIVE constants at `cmd/villa/preflight.go:20-22`:
```go
exitPass    = 0 // all checks pass
exitWarn    = 2 // passed with warnings (or an overridden block)
exitBlocked = 1 // an un-overridden BLOCK check failed
```

**Loopback address constant pattern** (`cmd/villa/verify.go:37`):
```go
const verifyMemoryLoopbackAddr = "127.0.0.1"
```
Reuse this constant (or mirror it); base URL composition is `fmt.Sprintf("http://%s:%d", addr, cfg.ChatPort)` (`verify_memory.go:154`) — never `:port` / never user-supplied.

---

### `cmd/villa/recall_live.go` (live REST seam, request-response over loopback)

**Analog:** `cmd/villa/verify_memory.go` — the indexer's I/O surface is ~80% these proven helpers. All are already package `main`; **call them, do not copy them**. New endpoints (chats list/get, file/remove, reset, models create/update) follow the exact same idiom.

**Fixed-arg curl runners — REUSE AS-IS** (`cmd/villa/verify_memory.go:419-440`):
```go
func runLoopbackCurl(ctx context.Context, curlArgs ...string) ([]byte, error) {
	return runLoopbackCurlStdin(ctx, "", curlArgs...)
}

func runLoopbackCurlStdin(ctx context.Context, stdin string, curlArgs ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "curl", curlArgs...) // fixed args; no shell
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, stderr.String())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
```

**Token mint — REUSE AS-IS** (`mintAdminToken`, `verify_memory.go:274-322`): signin → signup fallback with fixed JSON creds (`villa-verify@localhost`), token extracted via anonymous struct unmarshal. Token held in memory only.

**Served-model discovery — REUSE AS-IS** (`discoverChatModel`, `verify_memory.go:329-348`) for the attach step (the served id is the GGUF filename).

**Processing poll — REUSE/EXTEND** (`pollFileProcessed`, `verify_memory.go:353-380`): deadline loop, 1s tick, `select` on `ctx.Done()`, timeout is an ERROR never a skip. The 60s `ragSmokeProcessTimeout` (`:115`) may need a size-aware/generous variant for long transcripts (RESEARCH A2) — parameterize the timeout rather than re-rolling the loop.

**New-endpoint idiom — copy the GET/POST shape** (`verify_memory.go:187-199`, knowledge/create):
```go
kOut, err := runLoopbackCurl(ctx,
	"-sf", "-X", "POST", base+"/api/v1/knowledge/create",
	"-H", "Content-Type: application/json", "-H", auth,
	"-d", string(kBody),
)
if err != nil {
	return "", false, fmt.Errorf("knowledge/create: %w", err)
}
var kResp struct {
	ID string `json:"id"`
}
if jerr := json.Unmarshal(kOut, &kResp); jerr != nil || kResp.ID == "" {
	return "", false, fmt.Errorf("knowledge/create returned no id (%v): %s", jerr, string(kOut))
}
```
Per-step rules visible here, apply to every new endpoint (chats list `GET /api/v1/chats/list/user/{id}?page=N`, chat get, `file/remove?delete_file=true`, `knowledge/{id}/reset`, `models/create` / `models/model/update`):
- Bodies built via `json.Marshal(map[string]any{...})` only — never string interpolation of content.
- URLs composed `base + "/api/v1/..." + apiReturnedID` — ids are API-returned values, never user input.
- Every error wrapped with the endpoint name: `fmt.Errorf("knowledge/file/add: %w", err)`.
- Empty/missing id in a 200 body is an ERROR carrying the raw body for diagnosis.

**Multipart upload via stdin — REUSE the shape** (`verify_memory.go:205-218`): `runLoopbackCurlStdin(ctx, docText, "-sf", "-X", "POST", base+"/api/v1/files/", "-H", auth, "-F", "file=@-;filename=...;type=text/plain")` — transcript content never touches a temp file or the shell. Recall's filename is deterministic: `villa-recall-<chat-id>.txt`.

**Doc-comment convention** (`verify_memory.go:16-42`): every file opens with a long file-level comment stating its role, the decision IDs it implements (D-xx, RECALL-xx), and the honesty invariants. Mirror this in both new files.

---

### `internal/recall/recall.go` (pure plan/diff core, transform)

**Analog:** `internal/modelswap/modelswap.go` (typed-Result core) for the index-run orchestration shape; `internal/usage/usage.go` `Fold` for pure copy-not-mutate diff math.

**Typed Result over exit codes** (`internal/modelswap/modelswap.go:51-77`):
```go
type Result struct {
	Refused    bool   // rejected with zero side effects
	Reason     string // refusal/error explanation
	Err        error  // non-refusal failure
	FailedStep string // names the step Err occurred at
	...
}
```
Copy this discipline for the index-run summary: a typed struct (`PlanResult` with adds/updates/deletes; a run result with per-step `FailedStep`, skipped chats recorded — never silently dropped), the cmd tier maps it to exit codes + printing.

**Ordering-is-the-contract Run shape** (`modelswap.go:85-142`): numbered steps, each failure short-circuits with `Result{Err: err, FailedStep: "..."}` and no further side effects:
```go
func Run(d Deps, name string) Result {
	// (1) Resolve ... Unknown → refuse, zero side effects.
	m, ok := d.ResolveCatalog(name)
	if !ok {
		return Result{Refused: true, Unknown: true, Reason: "unknown model", ToModel: name}
	}
	// (2) ... refuse BEFORE any side effect
	...
	if err := d.Pull(m); err != nil {
		return Result{Err: err, FailedStep: "pull", ToModel: m.ID}
	}
	...
}
```
For recall index the analogous ordering is: gate → reachability → ensure KB → list users/chats → Plan diff → per-chat (render → remove-old → upload → poll → add → **persist state incrementally**) → attach model → stamp `last_index_completed_at` only on a clean full pass (D-06/Pitfall 8).

**Pure-fold copy-not-mutate** (`internal/usage/usage.go:126-158`): `Fold` returns an updated copy, never mutates the input map (`out.Models = make(...); for k, v := range prior.Models { out.Models[k] = v }`). Plan/Staleness must be pure the same way: injected live list + state in, typed plan out, no I/O.

**Package doc-comment pattern** (`usage.go:1-21` / `modelswap.go:1-15`): open with `// Package recall ...` stating purity, the seam, and the decision IDs (D-04/D-05/D-06/D-08).

---

### `internal/recall/staleness.go` (typed-Unknown classification)

**Analog:** `internal/memory/memory.go` `Decide` (lines 36-75) — typed verdict + accumulated reasons, fail-closed, never panics.

**Typed-decision shape** (`internal/memory/memory.go:36-54`):
```go
type Decision struct {
	Enabled bool
	Valid   bool
	Reasons []string
}

func Decide(cfg config.VillaConfig) Decision {
	if !cfg.MemoryEnabled {
		return Decision{Enabled: false, Valid: true}
	}
	var reasons []string
	if cfg.EmbeddingModel == "" {
		reasons = append(reasons, "embedding_model is empty (...)")
	}
	...
	return Decision{Enabled: true, Valid: len(reasons) == 0, Reasons: reasons}
}
```
Copy for the staleness report: a typed struct where **Unknown is a distinct state, not a zero** — e.g. `StaleCount` paired with `StaleKnown bool` (or a typed enum mirroring `attached | missing | unknown`). The algebra from RESEARCH: `new = L∖S`, `changed = updated_at > owui_updated_at`, `deleted = S∖L`; `L` Unknown ⇒ stale Unknown (WARN) while `indexed`/`last_indexed` still report from the state file (villa-side truths). Every refusal/warn string follows memory.go's refuse-with-reason style (the reason names the field AND why it matters).

Note: the recall verbs' enablement gate itself should literally call `memory.Decide(cfg)` (cmd tier) — don't re-validate memory fields in `internal/recall`.

---

### `internal/recall/transcript.go` (pure renderer)

**Analog:** purity discipline from `internal/usage` (no I/O, no os/exec, no literals); the chain-walk algorithm is specified in `21-RESEARCH.md` "Code Examples" (`linearThread`: walk `history.currentId` → `parentId` with a visited-set cycle guard, reverse to chronological). Strip `<details type="reasoning"...>...</details>` blocks; skip non-user/assistant roles; a chat with no reconstructable messages is SKIPPED and recorded, never silently dropped. Header format per D-04: title + ISO date + chat id, then `user:` / `assistant:` turns. Input is a typed struct parsed by the caller — keep `encoding/json` tags on the chat-shape types here (pure), the curl bytes arrive via the Deps seam.

---

### `internal/recall/store.go` (recall-state.json, file-I/O)

**Analog:** `internal/usage/usage.go` — **clone the whole store discipline** (established clone-don't-import rule, stated at `usage.go:243`: "This is a LOCAL copy of the config/benchstore guard shape — config's is unexported and importing config solely for it would widen this pure core's deps").

**Own schema version** (`usage.go:36-43`):
```go
const recallSchemaVersion = 1

func SchemaVersion() int { return recallSchemaVersion } // Phase-23 manifest reader-of-record
```

**Modes** (`usage.go:48-51`):
```go
const (
	storeFileMode os.FileMode = 0o600
	storeDirMode  os.FileMode = 0o700
)
```

**Injectable byte-I/O Deps** (`usage.go:165-173`):
```go
type Deps struct {
	WriteAll func(data []byte) error
	ReadAll  func() ([]byte, error) // (nil, nil) when no store exists yet
	Now      func() time.Time
}
```

**Fail-closed Load** (`usage.go:193-214`) — copy verbatim semantics:
```go
func Load(d Deps) (UsageTotals, error) {
	if d.ReadAll == nil {
		return UsageTotals{}, fmt.Errorf("usage: Load: nil ReadAll seam")
	}
	data, err := d.ReadAll()
	if err != nil {
		return UsageTotals{}, fmt.Errorf("usage: read store: %w", err)
	}
	if len(data) == 0 {
		return UsageTotals{}, nil // absent store ⇒ empty typed-Unknown
	}
	var t UsageTotals
	if err := json.Unmarshal(data, &t); err != nil {
		return UsageTotals{}, nil // corrupt ⇒ fail closed to empty, never a panic
	}
	if t.SchemaVersion != usageSchemaVersion {
		return UsageTotals{}, nil // unknown/future schema — fail closed
	}
	return t, nil
}
```
For recall the fail-closed empty state means "nothing indexed" — never a fabricated index (RESEARCH state schema, D-05).

**Save stamps the version** (`usage.go:176-186`): `t.SchemaVersion = usageSchemaVersion; json.Marshal; d.WriteAll(data)`.

**XDG root resolver + path** (`usage.go:222-239`):
```go
func storeRootDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa")
	}
	return filepath.Join("/var/tmp", "villa")
}

func RecallStatePath() string { return filepath.Join(storeRootDir(), "recall-state.json") }
```

**Atomic writer + traversal guard** (`usage.go:244-315`) — clone `assertInsideDir` (guards against the FIXED store root, not the path's own parent — WR-05) and `WriteFileAtomic` (mkdir 0700 → CreateTemp same-dir → Chmod 0600 → Write → Close → Rename → chmod-tighten; temp cleaned up on every error branch). Wire it as the live `WriteAll` in `liveRecallDeps`.

**Content discipline** (`usage.go:18-20` D-11 precedent): counts/ids/timestamps only — no chat titles or content in the state file (RESEARCH recommends; if planner adds titles, SECURITY.md must treat the file as content-bearing). `usage_test.go` asserts this via a JSON-key denylist — replicate that test idea.

---

### `cmd/villa/recall_test.go` + `internal/recall/*_test.go` (tests)

**Analog:** `cmd/villa/verify_memory_test.go` (read whole; 207 lines).

**Gate test with proof spy** (`verify_memory_test.go:145-165`) — copy for "memory off → exitBlocked, drive never runs":
```go
t.Run("memory off exits 0 without running the proof", func(t *testing.T) {
	proofRan := false
	deps := verifyMemoryDeps{
		loadedMemoryEnabled: func() bool { return false },
		loadedConfig:        func() config.VillaConfig { return config.DefaultVillaConfig() },
		ragSmokeFn: func(context.Context, ragSmokeInput) memoryProof {
			proofRan = true
			return memoryProof{status: preflight.StatusPass}
		},
	}
	cmd := newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if code := runVerifyMemory(cmd, nil, deps); code != exitPass {
		t.Errorf("memory-off exit = %d, want exitPass (%d)", code, exitPass)
	}
	if proofRan {
		t.Errorf("the proof must NOT run when memory is off")
	}
})
```
(Recall's expected code flips to `exitBlocked` per D-07 — same harness: `cobra.Command` with `SetContext(context.Background())`, `SetOut`/`SetErr` buffers, assert returned int + stderr non-empty on refusal.)

**Table-driven pure-core test** (`verify_memory_test.go:28-96`): cases slice with name + inputs + `wantStatus`, injected func closures, assert every FAIL carries a non-empty remediation detail. Use for Plan/Staleness/Transcript tables.

**Ordering/short-circuit spy test** (`verify_memory_test.go:103-131`): boolean spy proves a later step never ran after an earlier refusal — reuse for "delete-before-re-add ordering" and "attach runs after index".

**Test doc-comments**: every test function opens with a comment naming the invariant guarded and the decision ID (see `verify_memory_test.go:15-27`) — a project convention, not optional.

---

### `cmd/villa/root.go` (modify — registration)

**Edit site** (`cmd/villa/root.go:35-36`): append `newRecall()` to the existing `root.AddCommand(...)` list:
```go
root.AddCommand(newDetect(), newRecommend(), newPreflight(), newModel(), newInference(), newInstall(),
	newUp(), newDown(), newRestart(), newLogs(), newConfig(), newStatus(), newDoctor(), newVerify(), newDashboard(), newBackend(), newBench(), newBackup(), newRestore(), newUninstall())
```

### `cmd/villa/verify_memory.go` (modify — share, don't fork)

`mintAdminToken`, `runLoopbackCurl(Stdin)`, `pollFileProcessed`, `discoverChatModel` are package-`main` funcs already callable from `cmd/villa/recall*.go` — the minimum change is **zero edits** (just call them). If the planner moves them to a shared file (e.g. `cmd/villa/owui_client.go`) for clarity, it must be a pure move with no behavior change and `verify memory` tests staying green. The hard-coded `ragSmokeProcessTimeout` (`verify_memory.go:115`) is the one piece recall likely parameterizes (size-aware timeout, RESEARCH A2) — prefer adding a timeout parameter over a second poll loop.

### `docs/MEMORY.md` (modify — append)

Append a new `##` section (e.g. "Indexing past conversations with `villa recall`") following the existing section style (verb-titled `##` with `###` sub-steps, see lines 165-207 "Proving zero-outbound with `villa verify memory`" / "Running it"). Must document: the recall verbs, the retrieval enable-path, the post-`model swap` re-assert gotcha (Pitfall 2), the widened admin read scope, and keep the default-off auto-extraction guidance intact (CONTEXT canonical ref).

---

## Shared Patterns

### Exit-code mapping
**Source:** `cmd/villa/preflight.go:20-22` (`exitPass=0`, `exitBlocked=1`, `exitWarn=2`)
**Apply to:** both recall run bodies. Run bodies return the int; only the cobra `RunE` calls `os.Exit` (`verify.go:96-99`).

### Memory gate (fail-closed enablement)
**Source:** `internal/memory/memory.go:49` `Decide(cfg)` + `cmd/villa/install_memory.go:124/:138` (`liveLoadedConfig` / `liveLoadedMemoryEnabled`)
**Apply to:** `recall index` AND `recall status`. Delta vs `verify memory`: refuse with `exitBlocked` + remediation, never exit-0 no-op (D-07).

### Fixed-arg, shell-free host exec
**Source:** `cmd/villa/verify_memory.go:419-440` (`runLoopbackCurl(Stdin)`)
**Apply to:** every REST call. JSON bodies via `json.Marshal` only; ids in URLs are API-returned; content travels via stdin (`-F file=@-`) — T-20-09 carried.

### Error wrapping with step names
**Source:** throughout `verify_memory.go` (e.g. `:193` `fmt.Errorf("knowledge/create: %w", err)`) and `modelswap.go` `FailedStep`
**Apply to:** all new endpoint drives and the index-run result. Every non-PASS outcome carries a refuse-with-remediation detail naming what to check (e.g. `verify_memory.go:91`).

### Typed-Unknown honesty
**Source:** `internal/usage/usage.go:83-90` (`Known bool` per counter; unknown ⇒ no fold, no write) and `memory.Decide`'s reason accumulation
**Apply to:** staleness (`Unknown ≠ 0`), attachment state (`attached|missing|unknown`), partial-run state persistence.

### Atomic XDG persistence
**Source:** `internal/usage/usage.go:222-315` (`storeRootDir`, `assertInsideDir`, `WriteFileAtomic`)
**Apply to:** `recall-state.json` only. Clone into `internal/recall/store.go` (clone-don't-import is the established rule, `usage.go:243`).

### Seam grep gate
**Source:** `internal/inference/seam_test.go` `TestSeamGrepGate` (walks `internal/` + `cmd/villa`)
**Apply to:** `internal/recall` must contain NO image/backend literals; loopback URLs and OWUI endpoint paths are fine (precedent: `verify_memory.go:104-110` explicitly notes URL constants are not gate-relevant). No allowlist edit expected (D-08).

### File/package doc-comments with decision IDs
**Source:** `verify_memory.go:16-42`, `usage.go:1-21`, `modelswap.go:1-15`
**Apply to:** every new file. State role, purity/impurity, and the D-xx/RECALL-xx IDs implemented.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| — | — | — | None. Every new file has a strong in-repo analog; only the transcript chain-walk algorithm is novel, and RESEARCH supplies it verbatim (`21-RESEARCH.md` "Code Examples" `linearThread`). |

## Metadata

**Analog search scope:** `cmd/villa/`, `internal/usage/`, `internal/memory/`, `internal/modelswap/` (analogs pre-identified by CONTEXT.md/RESEARCH.md; confirmed by direct read)
**Files scanned:** 7 read in full (`verify_memory.go`, `verify.go`, `usage.go`, `memory.go`, `root.go`, `modelswap.go`, `verify_memory_test.go`) + targeted greps for `liveLoaded*` seams, exit constants, `memoryProof` (`install_memory.go:159`), and `docs/MEMORY.md` headings
**Pattern extraction date:** 2026-06-10
