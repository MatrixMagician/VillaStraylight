# Phase 17: Guided TUI Install (Capstone) - Context

**Gathered:** 2026-06-08
**Status:** Ready for planning

<domain>
## Phase Boundary

A guided interactive terminal install wizard that composes the **finished** pipeline
`detect → recommend → confirm/adjust → preflight-gate → install` with confirmation/consent
steps. **Pure presentation** — it adds zero decision logic to any pure core; all fit /
preflight / backend decisions come from the existing `recommend.Pick`, `internal/preflight`,
and `inference.BackendFor`. The wizard lives entirely in the command tier (`cmd/villa/`);
no `internal/*` core may import the TUI library. It is TTY-gated and degrades gracefully to
the existing flag-driven install path off-TTY or via `--no-tui`. The binary remains a single
static **CGO-free** build (`CGO_ENABLED=0`).

**In scope:** the presentation/wizard layer over `villa install`, confirm/adjust of the
recommendation (within the memory envelope), in-wizard preflight gap surfacing + consent,
final review/confirm before mutation, graceful fallback, the one new dependency
(`charmbracelet/huh` v1.0.0), and a small shared `lipgloss` theme.

**Out of scope (new capabilities → their own phase):** any new install/recommend/preflight
*logic*; changing the backend default or auto-switching; a non-install TUI (dashboard TUI,
model-management TUI); remote/web install; new config fields.
</domain>

<decisions>
## Implementation Decisions

### Invocation surface
- **D-01:** The TUI is the **default experience of `villa install`** on an interactive
  terminal. Bare `villa install` on a TTY launches the wizard. `--no-tui` opts out to today's
  flag-driven path. There is **no** new `villa setup` subcommand and **no** `--tui` opt-in
  flag — one command, progressively enhanced. (Matches the ROADMAP `--no-tui` wording.)

### Confirm / adjust depth
- **D-02:** At the confirm/adjust step the user can **pick from memory-fitting alternatives**:
  the wizard shows the recommended model/quant/ctx, plus a list of other catalog picks that
  fit the **detected envelope**, all re-validated through `recommend.Pick`. The user selects;
  the wizard computes nothing itself.
- **D-03:** Backend stays **Vulkan by default and is never auto-switched** — it changes only
  on explicit user action (consistent with the project-wide ROCm-is-opt-in rule). The wizard
  is not the place to silently flip backends.

### Preflight gap handling inside the wizard
- **D-04:** Non-privileged **automated fixes auto-run** with a visible notice; **privileged
  (sudo) fixes always require explicit per-item consent** before villa runs them (reuse the
  existing `promptConsent` / `interactive` seam semantics). villa never silently runs a
  privileged command.
- **D-05:** **Auto-run of safe fixes applies to BOTH paths** (TUI *and* the existing
  flag-driven path), unifying behavior. ⚠️ **This changes today's flag-path behavior**, which
  currently offers safe (non-privileged) fixes on y/N rather than auto-running them. See the
  contract-risk note under Deferred / planner constraints.
- **D-06:** Unmet **BLOCK** gaps still cannot proceed without `--force`; the wizard preserves
  the preflight **0/2/1 (PASS/BLOCK/WARN) exit contract** unchanged.

### Final apply
- **D-07:** Before any host mutation (image pull, Quadlet unit write, service start) the
  wizard shows a **final review screen** (chosen model/quant/ctx/backend + what will be
  pulled/written/started) with **one explicit "Install" confirm**. Matches villa's
  "never silently mutate" posture.

### Graceful fallback / degradation
- **D-08:** Fall back to the flag-driven path when **stdin/stdout is not a TTY**, OR `--json`
  is set, OR `--no-tui` is passed. Flags stay first-class in the fallback path.
- **D-09:** Honor **`NO_COLOR` / `TERM=dumb`** by degrading the **theme** (color/styling),
  **not** the whole wizard — a no-color terminal still gets the guided flow, just unstyled.

### Visual / branding scope
- **D-10:** Ship a small shared **villa `lipgloss` theme**: an accent color, consistent
  status coloring (**BLOCK=red / WARN=amber / PASS=green**), and a **step progress indicator**
  (e.g. "Step 2/5"). One theme file in the command tier; subject to D-09 NO_COLOR degradation.

### Dependency / build constraints (locked by ROADMAP)
- **D-11:** `charmbracelet/huh` **v1.0.0** is the **only** new first-party dependency —
  pure-Go / CGO-free. It transitively pins the **stable** `bubbletea v1.3.6` /
  `lipgloss v1.1.0` (NOT `charm.land/bubbletea/v2`). Confined to `cmd/villa/`; **no pure core
  may import it.** CI must verify the `CGO_ENABLED=0` static build.

### Claude's Discretion
- Exact screen decomposition/order within the `detect → recommend → confirm → gaps → review`
  flow (so long as it composes the named cores and honors D-07's final confirm).
- Whether `--dry-run` is exposed as a wizard preview screen or only via the flag/fallback path.
- Internal seam shape for testing the wizard off-hardware (follow the existing `installDeps` /
  `interactive`/`consent` seam pattern; the planner/researcher decides exact form).
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` — Phase 17 section: goal, 4 success criteria, the `huh` v1.0.0 /
  bubbletea v1.3.6 / lipgloss v1.1.0 pin, "command tier only / no core import", CGO-free CI check.
- `.planning/REQUIREMENTS.md` — INSTALL-01 (guided TUI composes the pipeline, presentation
  only) and INSTALL-02 (graceful non-TTY / `--no-tui` degradation, single static CGO-free binary);
  Out-of-scope row: "CGO-based TUI toolkit" rejected (`huh` is pure-Go).

### Existing pipeline to compose (the wizard wraps these — does not reimplement)
- `cmd/villa/install.go` — the install command the wizard fronts: `newInstall`/`runInstall`,
  the `installDeps` seam (`interactive func() bool`, `consent func(prompt string) bool`,
  `stdinIsInteractive`, `promptConsent`), `gateInstall`/`resolveGap`/`offerNonBlockingGap`/
  `runGapFix`/`hasAutomatedFix` (gap + consent logic to reuse), 0/2/1 exit mapping,
  `--json`/`--dry-run`/`--force` handling, `printPostInstall`.
- `cmd/villa/recommend.go` — confirm/adjust source: `recommend --model/--quant/--ctx` override
  re-validation against the envelope (D-07 in that file), `--save` persistence (D-20).
- `cmd/villa/detect.go` — host probe surface feeding step 1.
- `cmd/villa/preflight.go` — preflight gate surface feeding the gap step.
- `internal/recommend/recommend.go` — `Pick()` (memory-fit; source of the "alternatives" list in D-02).
- `internal/preflight/` — `CheckResult` BLOCK/WARN tiers + remediation (the gap data the wizard renders).
- `internal/inference/backend.go` — `BackendFor` (backend resolution; never auto-switched, D-03).

### Conventions / architecture (project-wide rules the wizard must respect)
- `CLAUDE.md` — "Config is the single source of truth", pure-core + injectable-seam, byte-frozen
  `--json`/golden contracts, no shell interpolation, loopback-only, seam grep-gate.
- `.planning/codebase/ARCHITECTURE.md`, `.planning/codebase/CONVENTIONS.md`,
  `.planning/codebase/STRUCTURE.md` — command-tier vs pure-core boundary, `live*Deps` wiring,
  `fake*Deps` test doubles, golden-test discipline.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `installDeps` seam (`cmd/villa/install.go:75`): already carries `interactive func() bool`
  and `consent func(prompt string) bool`, wired live via `liveInstallDeps` (`stdinIsInteractive`,
  `promptConsent`). The wizard reuses this seam — TTY-gating and privileged consent are NOT new.
- `gateInstall`/`resolveGap`/`offerNonBlockingGap`/`runGapFix`/`hasAutomatedFix`
  (`install.go:442–578`): the preflight gap-resolution + automated-fix machinery the wizard's
  gap screen drives (D-04/D-05). `hasAutomatedFix` already distinguishes safe vs privileged.
- `recommend.Pick` + catalog: source of the memory-fitting alternative list (D-02) — no new
  fit logic needed.

### Established Patterns
- Pure-core + injectable-seam: the wizard is command-tier presentation; mirror the `installDeps`
  + `live*Deps()` / `fake*Deps` pattern so the whole flow is testable off-hardware (no live TTY).
- Byte-frozen `--json`/golden contracts: D-05 (auto-run safe fixes on the flag path) likely
  touches `install_test.go` expectations / any install golden — evolve append-only and re-freeze
  intentionally; do not let the TUI add a new uncovered output contract silently.
- Seam grep-gate (`TestSeamGrepGate`): backend marker literals stay behind `internal/inference`
  — the wizard must render backend names via existing accessors, not re-typed literals.

### Integration Points
- New TUI code under `cmd/villa/` (e.g. an install-wizard file) invoked from `newInstall`/
  `runInstall` when interactive-and-not-`--no-tui`/`--json`; otherwise the existing flag path runs.
- `go.mod` gains exactly one direct dependency (`charmbracelet/huh` v1.0.0) + its stable
  transitive `bubbletea`/`lipgloss` pins; `CGO_ENABLED=0` build check added to CI/Makefile.
</code_context>

<specifics>
## Specific Ideas

- "Step 2/5"-style progress indicator and BLOCK=red / WARN=amber / PASS=green status coloring
  (D-10), themed but NO_COLOR-degradable (D-09).
- Final review-and-confirm screen as the single explicit gate before host mutation (D-07).
</specifics>

<deferred>
## Deferred Ideas

- **Contract-risk note for researcher/planner (not deferred work — a constraint):** D-05
  unifies "auto-run non-privileged fixes" across BOTH the TUI and the existing flag-driven
  path. This **changes current `villa install` flag-path behavior** (today it offers safe fixes
  on y/N). The planner MUST: (a) update affected `install_test.go` expectations and any install
  golden append-only + re-freeze intentionally; (b) confirm the preflight **0/2/1 exit contract**
  and the "never silently run a privileged command" rule remain intact (privileged fixes still
  consent-gated); (c) verify no `--json`/non-interactive regression (auto-run must still respect
  non-interactive mode).
- A non-install TUI surface (dashboard TUI, model-management TUI) — out of scope; would be its
  own phase.
- `--dry-run` rendered as a rich in-wizard preview — left to Claude's discretion this phase; can
  be a follow-up if desired.

None of the above expands Phase 17 scope.
</deferred>

---

*Phase: 17-Guided TUI Install (Capstone)*
*Context gathered: 2026-06-08*
