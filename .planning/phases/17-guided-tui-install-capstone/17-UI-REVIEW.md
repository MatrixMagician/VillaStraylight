# Phase 17 — UI Review (Terminal TUI)

**Audited:** 2026-06-08
**Baseline:** `.planning/phases/17-guided-tui-install-capstone/17-UI-SPEC.md` (approved design contract — terminal TUI surface)
**Surface:** terminal TUI (`charmbracelet/huh` 5-screen wizard, `lipgloss`/`termenv` theme). NOT a web/browser frontend.
**Screenshots:** not captured — TUI surface, no DOM/browser; Playwright/screenshots do not apply. Evidence is CODE-BASED (`cmd/villa/install_wizard.go`, `cmd/villa/tui_theme.go`, `cmd/villa/install.go`) corroborated by on-hardware rendered captures in `17-UAT.md` (SGR sequence counts, accessible-mode text, abort copy). The `:3000` HTTP 200 is the already-running Open WebUI stack, not a phase-17 dev server.

---

## Pillar Scores

| Pillar | Score | Key Finding |
|--------|-------|-------------|
| 1. Copywriting | 2/4 | Install/abort/review copy match contract verbatim, but TWO contracted copy strings are MISSING (empty-state "no fitting model", typed-Unknown "pick conservatively" note) and the step-2 CTA is not the contracted "Use this model" |
| 2. Visuals | 3/4 | Step N/M header, glyph column (`✓`/`!`/`✗` + `[OK]`/`[WARN]`/`[BLOCK]`), 5-screen sequence all present and UAT-rendered; key-hints footer is huh's default, not the contract-specified explicit key map |
| 3. Color | 4/4 | Accent + 3 status tokens exactly per contract, accent scarce/reserved, NO_COLOR=1 → 0 SGR and TERM=dumb → 0 escapes proven on hardware; flow survives (D-09) |
| 4. Typography | 3/4 | Display=bold+underline+accent, Heading=bold, Help=faint all implemented; status words bold-unconditional so they survive color strip; Heading role for in-Note sub-sections ("Will install") not separately emphasized — relies on plain text |
| 5. Spacing | 3/4 | marker-gap (1 cell) and screen-1 gutter honored; review/preflight detail lines NOT indented at the contracted 2-cell `block` indent (flat left-aligned columns) |
| 6. Experience Design | 4/4 | Full detect→model→preflight→review→confirm flow; Cancel-default focus (D-07); consent-gated privileged fixes (D-04); non-TTY/`--no-tui`/`--json` fallback + NO_COLOR degradation all proven on hardware with zero mutation |

**Overall: 19/24**

---

## Top Priority Fixes

1. **Empty-state copy missing (BLOCKER for the contract).** UI-SPEC Copywriting contracts the wizard-specific string `No catalog model fits the detected memory envelope (<N> GiB usable). Free memory or supply a larger-envelope host, then re-run villa install. (--no-tui shows the same result.)`. The implementation never emits it — the no-fit branch is `install.go:223` (`install: no fitting configuration for this host (memory envelope undeterminable — recommend refused)`), a different, generic message that fires on the flag path BEFORE the wizard is reached, and it does not state the usable-GiB envelope or the `--no-tui` parity note. **Fix:** add the contracted copy (with the computed `<N> GiB` from `profile.UsableEnvelopeBytes`) on the wizard no-fit path before `useWizard` is evaluated.

2. **Typed-Unknown advisory note missing (WARNING).** Contract requires that when any host fact is typed-Unknown the screen-1 Note carry the one-line advisory `Some host facts could not be probed; villa will pick conservatively. Run villa detect for detail.`. `detectedHostSummary` (`install_wizard.go:181-189`) renders bare per-field `unknown` tokens (`strOrUnknown`/`bytesOrUnknown`, lines 280-293) but never appends the advisory sentence. **Fix:** in `detectedHostSummary`, if any of `p.CPUModel/UsableEnvelopeBytes/IGPUName/KernelVersion` is not `Known`, append the contracted note as a faint trailing line.

3. **Step-2 confirm CTA does not match contract (WARNING).** Contract Copywriting row: step-2 confirm CTA = **"Use this model"**. The Select (`install_wizard.go:125-130`) advances on `Enter` with no labelled confirm button, so the contracted CTA string never renders (UAT shows the raw `Enter a number between 1 and 3:` accessible prompt, no "Use this model"). **Fix:** either surface "Use this model" as the advance affordance copy or document a deliberate waiver in the contract.

4. **Key-hints footer is the huh default, not the contracted explicit key map (WARNING).** The Interaction Contract specifies a footer surfacing `↑/↓·Tab·Enter·y/n·Esc`. No first-party footer/`WithShowHelp`/keymap renderer exists in `install_wizard.go` or `tui_theme.go`; the wizard relies on huh's built-in help line. Functionally correct but the footer content/wording is not first-party-owned or guaranteed to match the contract. **Fix:** assert/customize the footer key map (or waive in contract).

5. **Review/preflight detail lines not indented at the `block` 2-cell indent (WARNING).** Spacing scale `block` = "1 blank row + 2-cell left indent" for indented detail lines under a heading. `reviewBlock` (`install_wizard.go:238-246`) and `preflightSummary` (216-233) emit flat, left-aligned `key: value` / glyph rows with no 2-cell gutter. **Fix:** prefix detail/gap lines with the 2-cell indent so the visual hierarchy matches the spacing token.

6. **Confirm CTA wording drift on privileged gaps (minor).** Contract implies per-item consent on the exact privileged command; implementation uses `Affirmative("Run it")`/`Negative("Skip")` (`install_wizard.go:153-154`). Acceptable (remediation-forward), but the gap-declined error-state copy `BLOCK: <check name>. <remediation>. Run the suggested command, or re-run with --no-tui --force to override (auditable).` is not rendered by the wizard on decline — the contract error string is absent.

---

## Detailed Findings

### Pillar 1: Copywriting (2/4)

**Met (verbatim against contract):**
- Wizard abort copy is byte-exact: `Install cancelled — no changes were made. Re-run villa install, or villa install --no-tui for the flag-driven path.` (`install.go:265`; rendered live in UAT Test 1/Test 2 on Ctrl+C).
- Final destructive confirm copy exact: `Install: this will pull images, write Quadlet units, and start services. Proceed?` with `Affirmative("Install")`/`Negative("Cancel")` (`install_wizard.go:167-171`).
- Review heading exact: `Review — villa will install:` with `model:`, `backend:`, `will pull:`, `will write:`, `will start:` lines (`install_wizard.go:165, 238-246`). Backend is always `vulkan` via accessor, never auto-flipped (D-03 honored).

**Gaps (BLOCKER/WARNING):**
- **BLOCKER:** Empty-state "no fitting model" contract string absent (see Top Fix 1). The substitute at `install.go:223` omits the usable-GiB figure and the `--no-tui` parity note.
- **WARNING:** Typed-Unknown advisory note absent (Top Fix 2) — only bare `unknown` tokens render.
- **WARNING:** Step-2 CTA "Use this model" never rendered (Top Fix 3).
- **WARNING:** BLOCK-gap-declined error copy (`BLOCK: <check name>. <remediation>. Run the suggested command, or re-run with --no-tui --force to override (auditable).`) not emitted by the wizard on decline (Top Fix 6).

Two of six contracted copy strings missing, plus the step-2 CTA drift → 2/4 (notable gaps, contract partially met).

### Pillar 2: Visuals (3/4)

- 5-screen sequence implemented exactly as the Layout table: Note (detected host) → Select (model) → Note+Confirms (preflight) → Note+Confirm (review) → install/result (`buildWizardForm`, `install_wizard.go:114-176`). UAT Test 1 rendered Screen 1/5 and Screen 2/5 live on gfx1151.
- Step N/M indicator present on every screen title via `stepHeader(N,5,...)` (`tui_theme.go:139-148`); UAT confirmed "Step 1/5" with the current step in accent (SGR `38;5;105`).
- Glyph column per Iconography table: `✓`/`!`/`✗` with `[OK]`/`[WARN]`/`[BLOCK]` ASCII fallback (`statusGlyph`, `tui_theme.go:113-133`); ascii path driven by `!in.colorEnabled` (`install_wizard.go:115`).
- **WARNING:** the key-hints footer is huh's built-in default rather than the contract-specified explicit key map (Top Fix 4) — no first-party footer renderer. Glyph column + screen sequence are strong; the footer ownership gap holds this to 3/4.

### Pillar 3: Color (4/4)

- All five color tokens match the contract values exactly: accent `{63,105}`, muted `{244,245}`, pass `{34,42}`, warn `{130,214}`, block `{160,196}` (`tui_theme.go:30-39`).
- Accent reserved to the contracted elements only: focused title, NoteTitle, SelectSelector, and the current Step digit (`villaTheme`, `tui_theme.go:84-89`; `stepHeader`, 144-147). No decorative accent.
- Status colors reserved exclusively to preflight rows + result line via `statusStyle` (`tui_theme.go:96-108`), always co-occurring with a bold word + glyph.
- D-09 degradation proven on hardware (UAT Test 2): `NO_COLOR=1` → **0 color SGR sequences** (foreground fully stripped, `colorEnabled()` false → `termenv.Ascii`), `TERM=dumb` → **0 escapes at all** (huh accessible mode), flow still completes. No gradients/bg-fills/24-bit-only colors. This is a clean 4/4.

### Pillar 4: Typography (3/4)

- Emphasis scale implemented: Display = `Bold(true).Underline(true)` + accent on Focused.Title/NoteTitle (`tui_theme.go:84,86`); Help = faint via mutedColor on ShortDesc/FullDesc (87-88).
- Status words bold UNCONDITIONALLY (`statusStyle`, `tui_theme.go:97`) so BLOCK/WARN/PASS stay scannable after color strip — exactly the contract's "survive NO_COLOR" rule; UAT confirms legibility in TERM=dumb plain mode.
- **WARNING:** the Heading role (bold, default-fg, for in-screen sub-sections like the review block's implicit "will install" grouping) is not separately emphasized — `reviewBlock`/`preflightSummary` emit plain body text with no bold sub-headings, so the 4-role hierarchy collapses toward 3 roles inside Notes. Hierarchy still survives via the accented NoteTitle + glyph column, hence 3/4 not lower.

### Pillar 5: Spacing (3/4)

- marker-gap (1 cell between glyph and word) honored: `"%s %s  %s"` glyph/word/name (`install_wizard.go:224`).
- Screen-1 host facts use aligned label columns (`CPU:`, `memory:`, etc., `install_wizard.go:183-187`) — legible, fixed-width, 80-col safe.
- step token sits inline on the header with a 1-cell gap before the title (`stepHeader(...) + " Detected host"`, `install_wizard.go:120`) — matches the "no extra blank row" exception.
- **WARNING:** the `block` token (2-cell left indent for detail lines under a heading) is NOT applied — review lines (`reviewBlock`, 238-246) and preflight gap rows (`preflightSummary`, 216-233) are flush-left, no gutter indent. Vertical rhythm relies on `\n` joins only. Functional at 80 cols but the contracted indentation hierarchy is missing → 3/4.

### Pillar 6: Experience Design (4/4)

- Full flow composed without added decision logic (pure presentation, INSTALL-01): `buildWizardForm` presents `rec`/`checks`/`backend`; the single `gateInstall` (`install.go:280`) consumes collected consent — probe/pick/runChecks/gate each run once (verified `TestInstallWizardPathRunsGateOnce`).
- **Cancel-default focus (D-07):** `doInstall` starts `false` so focus defaults to the safe "Cancel" (`liveWizard`, `install_wizard.go:71`; confirm at 167-171); asserted by test and noted in UAT Test 1.
- **Consent gating (D-04):** privileged gaps get a per-item Confirm defaulting to decline (`val: false`, `install_wizard.go:147`); decline → seam 0 invocations (UAT full walk-through declined PRE-03, no sudo ran; `TestInstallWizardPathRunsGateOnce` consent-denied case).
- **Fallback (D-08/D-01):** `useWizard := d.interactive() && !opts.json && !opts.noTUI && d.stdoutIsTTY()` (`install.go:245`); `--no-tui`/`--json`/non-TTY all take the flag path with byte-identical config (UAT Test 3: md5 `546aaa0…` identical across `--no-tui`, non-TTY, piped-stdin).
- Abort returns `exitBlocked` with no mutation (`install.go:266`); 0/2/1 exit contract preserved (D-06).
- One non-blocking observation from UAT (TERM=dumb Ctrl+C delivers hard SIGINT rather than the graceful abort copy) is a library accessible-mode artifact, no mutation either way — cosmetic, does not lower the score.

No web/registry/component-registry surface exists (terminal TUI); supply-chain safety is the huh-only/CGO-free/seam-confined constraint, verified by Plan-01 CI + `TestSeamGrepGate`. Clean 4/4.

---

## Registry Safety

Not applicable — terminal TUI, no `components.json`/shadcn/component registry. Dependency-supply safety is governed by D-11 (huh v1.0.0 only new dep, pure-Go, `CGO_ENABLED=0`, confined to `cmd/villa/`, no `internal/*` import) and `TestSeamGrepGate` (no backend/image literal in TUI code) — both verified green in the Plan-01/02/03 summaries. Registry audit: 0 third-party UI blocks (not a registry surface).

---

## Files Audited

- `cmd/villa/install_wizard.go` — 5-screen huh wizard: `liveWizard`, `buildWizardForm`, `modelOptions`, `preflightSummary`, `reviewBlock`, `detectedHostSummary`, `privilegedGap`, `strOrUnknown`/`bytesOrUnknown`.
- `cmd/villa/tui_theme.go` — `villaTheme`, `colorEnabled`, `stdoutIsTTY`, `statusStyle`, `statusGlyph`, `stepHeader`, color tokens.
- `cmd/villa/install.go` — `useWizard` gate (line 245), wizard branch (246-278), abort copy (265), no-fit copy (223-224), `gateInstall` single-gate consumption (280).
- `.planning/phases/17-guided-tui-install-capstone/17-UI-SPEC.md` — design contract (audit baseline).
- `.planning/phases/17-guided-tui-install-capstone/17-UAT.md` — on-hardware rendered evidence (SGR counts, accessible-mode text, abort copy, NO_COLOR/dumb degradation).
- `.planning/phases/17-guided-tui-install-capstone/17-CONTEXT.md`, `17-01/02/03-SUMMARY.md` — decisions and build record.
