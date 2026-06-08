---
phase: quick-260608-pyp
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - cmd/villa/tui_theme.go
  - cmd/villa/tui_theme_test.go
  - cmd/villa/install_wizard.go
  - cmd/villa/install_wizard_test.go
  - cmd/villa/install.go
  - cmd/villa/install_test.go
autonomous: true
requirements: [INSTALL-01]
must_haves:
  truths:
    - "The wizard footer renders the contracted explicit key map (↑/↓ · Tab · Enter · y/n · Esc), first-party-owned, not huh's default labels"
    - "The step-2 Select advance affordance surfaces the contracted 'use this model' CTA in the footer help (Enter-to-advance IS the confirm)"
    - "Review and preflight-gap detail lines are indented at the 2-cell `block` token under their heading"
    - "Declining a privileged BLOCK-tier preflight fix in the wizard emits the exact contracted BLOCK-gap-declined error copy"
  artifacts:
    - path: "cmd/villa/tui_theme.go"
      provides: "villaKeyMap() first-party huh KeyMap with contracted footer help labels + the blockIndent token"
      contains: "func villaKeyMap"
    - path: "cmd/villa/install_wizard.go"
      provides: "block-indented preflightSummary/reviewBlock; form built WithKeyMap(villaKeyMap())"
      contains: "WithKeyMap"
    - path: "cmd/villa/install.go"
      provides: "contracted BLOCK-gap-declined copy on the wizard-decline path"
      contains: "to override (auditable)"
  key_links:
    - from: "cmd/villa/install_wizard.go buildWizardForm"
      to: "cmd/villa/tui_theme.go villaKeyMap"
      via: "huh.NewForm(...).WithKeyMap(villaKeyMap())"
      pattern: "WithKeyMap\\(villaKeyMap"
---

<objective>
Close the four remaining Phase-17 UI-SPEC warnings from 17-UI-REVIEW.md (the empty-state BLOCKER + typed-Unknown advisory were already fixed in quick task 260608-ppy / commit 583b1ee — do NOT touch those). All four are COMMAND-TIER presentation only: an explicit first-party footer key map (Pillar 2), the 2-cell `block` detail-line indentation (Pillar 5), the contracted step-2 "use this model" advance CTA (Pillar 1/6), and the contracted BLOCK-gap-declined error copy (Pillar 1).

Purpose: bring the rendered TUI surface into byte-level conformance with the approved 17-UI-SPEC.md design contract so the checker sign-off (Dimensions 1/2/5) flips to PASS.
Output: a first-party `villaKeyMap()` + `blockIndent` token in tui_theme.go; block-indented `preflightSummary`/`reviewBlock` and keymap-wired form in install_wizard.go; the contracted decline copy in install.go; table-driven assertions on the rendered strings (no live TTY).
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
</execution_context>

<context>
@.planning/phases/17-guided-tui-install-capstone/17-UI-SPEC.md
@.planning/phases/17-guided-tui-install-capstone/17-UI-REVIEW.md
@cmd/villa/tui_theme.go
@cmd/villa/install_wizard.go
@cmd/villa/install.go

# Constraints (from CLAUDE.md, non-negotiable):
# - COMMAND-TIER presentation ONLY. Add NO decision logic to any internal/* core; the
#   wizard stays a pure collector. NO backend/image literal (TestSeamGrepGate walks cmd/villa).
# - gofmt -l ./cmd/villa must be clean; CGO_ENABLED=0 go build ./cmd/villa exit 0;
#   TestSeamGrepGate green; make check (vet + full suite) green.
# - The dependency is locked to charmbracelet/huh v1.0.0 (D-11). Do NOT add a new dep.
#   The huh KeyMap API is github.com/charmbracelet/huh.{KeyMap,NewDefaultKeyMap};
#   help labels come from github.com/charmbracelet/bubbles/key.WithHelp("key","label").
#   bubbles/key is already a transitive dep of huh (no go.mod change).
# - No golden/--json/byte-frozen test asserts the old footer/indent/copy (verified:
#   no testdata fixture references "Use this model"/"BLOCK:"/"Review —"). So NO -update
#   refreeze is expected. If a build surfaces an unexpected golden diff, refreeze it
#   INTENTIONALLY with `go test ./... -update` and note exactly which golden + why.

# EXACT contracted strings to copy VERBATIM (from 17-UI-SPEC.md):
# - Interaction Contract key map (line 158-ish): ↑/↓ (and k/j) · Tab/Shift+Tab · Enter · y/n · Esc/Ctrl+C
# - Copywriting: Step-2 confirm CTA = "Use this model" (advance from the Select)
# - Copywriting: Error state — BLOCK gap declined =
#     BLOCK: <check name>. <remediation>. Run the suggested command, or re-run with --no-tui --force to override (auditable).
# - Spacing token `block` = "1 blank row + 2-cell left indent" for indented detail lines under a heading.
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: First-party footer key map + step-2 "use this model" CTA</name>
  <files>cmd/villa/tui_theme.go, cmd/villa/tui_theme_test.go, cmd/villa/install_wizard.go</files>
  <behavior>
    - villaKeyMap() returns a *huh.KeyMap derived from huh.NewDefaultKeyMap() with the
      footer HELP LABELS overridden so the rendered footer matches the contract; the
      KEYS themselves are left at huh defaults (we change help labels, not bindings).
    - Test: the Select Submit/Next binding's Help().Desc renders "use this model" (the
      contracted step-2 CTA advance affordance — Enter-to-advance IS the confirm).
    - Test: navigation bindings carry the contracted footer vocabulary — Select Up.Help().Key
      contains "↑", Down "↓"; a field Next/Prev help reflects Tab/Shift+Tab; Confirm
      Accept.Help().Key == "y" and Reject == "n"; Quit/abort help reflects esc/ctrl+c.
    - Test: villaKeyMap() is a pure func (no env, no TTY) — callable in CI with no terminal.
  </behavior>
  <action>
    In tui_theme.go add `func villaKeyMap() *huh.KeyMap`. Start from `huh.NewDefaultKeyMap()`,
    then re-bind the help LABELS on the navigation bindings to match 17-UI-SPEC.md's
    Interaction Contract footer. Use github.com/charmbracelet/bubbles/key — for each binding
    call `key.WithKeys(<keep the default keys>)` + `key.WithHelp(<key glyph>, <label>)` to
    rewrite only the help text. Set these labels verbatim from the contract:
      - Select.Up help "↑/k" → "up"; Select.Down help "↓/j" → "down".
      - Select.Next AND Select.Submit help label → "use this model" (this is warning #3:
        the contracted Step-2 confirm CTA. The Select advances on Enter; the footer help
        is where "Use this model" renders. Lowercase "use this model" to match the
        project's lowercase-command-aware footer voice; the contract's titlecase "Use this
        model" is the row label — keep the footer token consistent with huh's faint help
        casing. If the executor judges the exact titlecase "Use this model" is required to
        satisfy the contract literally, use that casing instead — EITHER renders the
        contracted CTA; pick one and assert it.)
      - Note.Next / Confirm.Next help → "enter" / "next" (Enter advances).
      - Confirm.Accept help "y"/"yes", Confirm.Reject help "n"/"no" (the y/n contract).
      - Prev bindings (shift+tab) help → "back"; Quit (ctrl+c) + any esc help → "cancel".
    Return the assembled *huh.KeyMap. This is the ONLY place footer wording is owned.
    Then in install_wizard.go buildWizardForm, change the final builder from
    `huh.NewForm(groups...).WithTheme(villaTheme(in.colorEnabled))` to additionally
    `.WithKeyMap(villaKeyMap())` so the form renders the first-party footer. Leave
    WithShowHelp at its default (help shown) — do NOT disable the footer.
    NO backend/image literal anywhere (TestSeamGrepGate). Pure presentation; no decision logic.
  </action>
  <verify>
    <automated>cd /home/oliverh/repos/github/MatrixMagician/VillaStraylight && go test ./cmd/villa/ -run 'TestVillaKeyMap|TestStepTwoCTA|TestSeamGrepGate' -count=1</automated>
  </verify>
  <done>villaKeyMap() exists and is wired via WithKeyMap; the Select advance help renders the contracted "use this model" CTA; navigation/confirm/abort help labels match the contract vocabulary; tests pass; TestSeamGrepGate green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: 2-cell `block` indentation for review + preflight detail lines</name>
  <files>cmd/villa/tui_theme.go, cmd/villa/install_wizard.go, cmd/villa/install_wizard_test.go</files>
  <behavior>
    - A `blockIndent` constant (2 spaces, the `block` token) is defined once in tui_theme.go.
    - preflightSummary(): every per-check detail row is prefixed with blockIndent so the
      glyph/word/name column is indented 2 cells under the "Preflight results" heading.
    - reviewBlock(): every `key: value` line (model/backend/will pull/will write/will start)
      is prefixed with blockIndent so the review detail lines sit 2 cells under the
      "Review — villa will install:" heading.
    - Test: preflightSummary(<one PASS check>, ascii=true) — each non-empty line
      strings.HasPrefix "  " (2 spaces); the existing glyph/word/name content is unchanged
      after the indent.
    - Test: reviewBlock(<wizardInput with a fake backend>) — every line HasPrefix "  ";
      the "will pull:" line still renders backend.Image() via the accessor (no literal).
    - Existing wizard accessible-driver / no-fit / advisory tests still pass (the indent
      is additive; assertions that use strings.Contains on inner content keep matching).
  </behavior>
  <action>
    In tui_theme.go add `const blockIndent = "  "` (2 cells = the UI-SPEC `block` token;
    document it cites 17-UI-SPEC.md Spacing Scale `block` = 2-cell left indent).
    In install_wizard.go preflightSummary: prefix each emitted row with blockIndent — i.e.
    write `blockIndent` before the glyph in the per-check `fmt.Fprintf(&b, "%s %s  %s", ...)`
    so it becomes a 2-cell-indented detail line; keep the marker-gap (1 cell glyph→word)
    and the inter-row `\n` join exactly as-is. Do the same in reviewBlock: prefix each
    `fmt.Fprintf(&b, "model:      ...")` / backend / will-pull / will-write / will-start
    line with blockIndent. Do NOT indent the Note TITLE (the heading stays flush; only the
    Description detail lines indent — that IS the `block` hierarchy). Keep all existing
    text/accessors byte-identical apart from the leading 2 spaces. NO backend/image literal
    (reviewBlock keeps in.backend.Name()/Image()). Pure presentation.
  </action>
  <verify>
    <automated>cd /home/oliverh/repos/github/MatrixMagician/VillaStraylight && go test ./cmd/villa/ -run 'TestPreflightSummaryBlockIndent|TestReviewBlockIndent|TestInstallWizard' -count=1</automated>
  </verify>
  <done>blockIndent defined once; preflightSummary and reviewBlock detail lines are 2-cell-indented under their headings; new HasPrefix assertions pass; prior wizard tests still pass; no image literal leaked.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Contracted BLOCK-gap-declined error copy on the wizard-decline path</name>
  <files>cmd/villa/install.go, cmd/villa/install_test.go</files>
  <behavior>
    - When a privileged BLOCK-tier preflight gap is DECLINED via the wizard's collected
      consent (consents[c.ID] == false) AND the gap stays unmet (no --force), the install
      emits the EXACT contracted copy:
        BLOCK: <check name>. <remediation>. Run the suggested command, or re-run with --no-tui --force to override (auditable).
      with <check name> = c.Name and <remediation> = c.Remediation (or remediationCommand
      fallback when Remediation is empty — match the existing remediation-forward rule).
    - Test: drive runInstall on the wizard path with a fake wizard returning a declined
      consent for a BLOCK-tier check; assert stderr contains the verbatim contracted line
      (with the real check name + remediation substituted) and the exit code stays
      exitBlocked, zero host mutation (existing seam call-count assertions unchanged).
    - Test: the --force override path still degrades to exitWarn and does NOT emit the
      decline copy as a hard block (the copy is the declined-without-force state only).
  </behavior>
  <action>
    In install.go resolveGap, on the wizard-decline branch (the
    `if decision, recorded := consents[c.ID]; consents != nil && recorded { if !decision {...} }`
    block, currently printing "  declined — run the command above..."), ADD the contracted
    BLOCK-gap-declined line to errOut BEFORE returning false:
      fmt.Fprintf(errOut, "BLOCK: %s. %s. Run the suggested command, or re-run with --no-tui --force to override (auditable).\n", c.Name, blockRemediation(c))
    where blockRemediation(c) returns c.Remediation when non-empty else remediationCommand(c, ...)
    (reuse the existing remediationCommand for the fallback; resolve username via the same
    path resolveGap already uses — it already computes cmdStr). Keep the existing terse
    "  declined …" hint too (it is the actionable next-step), OR fold it into the contracted
    line — prefer EMITTING THE CONTRACTED LINE VERBATIM and keeping behavior (return false →
    caller treats as block → exitBlocked unless --force). This is the ONLY copy change; do
    NOT alter the exit-code mapping, the --force degrade-to-WARN, or the non-interactive
    branch wording. Pure presentation copy; no decision-logic change; no internal/* edit.
    Verify TestSeamGrepGate stays green (no image literal added).
  </action>
  <verify>
    <automated>cd /home/oliverh/repos/github/MatrixMagician/VillaStraylight && go test ./cmd/villa/ -run 'TestWizardBlockDeclinedCopy|TestInstallWizardPathRunsGateOnce|TestSeamGrepGate' -count=1</automated>
  </verify>
  <done>The wizard-decline path emits the verbatim contracted "BLOCK: <name>. <remediation>. Run the suggested command, or re-run with --no-tui --force to override (auditable)." line; exit stays exitBlocked with zero mutation; --force still degrades to WARN; tests pass; seam gate green.</done>
</task>

</tasks>

<verification>
Run from the repo root after all three tasks:
- `gofmt -l ./cmd/villa` → no output (clean).
- `CGO_ENABLED=0 go build ./cmd/villa` → exit 0 (static-binary constraint, D-11/SC#4).
- `go test ./cmd/villa/ -run TestSeamGrepGate -count=1` → PASS (no backend/image literal leaked into cmd/villa).
- `make check` → vet + full suite green.
- If ANY golden/--json test reports a diff (none expected — no fixture references the old
  footer/indent/copy), refreeze INTENTIONALLY with `go test ./... -update` and note exactly
  which golden changed and why in the SUMMARY. An UNEXPECTED golden diff is a signal to stop
  and re-check, not to blindly -update.
</verification>

<success_criteria>
- Footer renders the contracted explicit key map (first-party villaKeyMap, wired via WithKeyMap).
- Step-2 advance surfaces the contracted "use this model" CTA in the footer help.
- Review + preflight-gap detail lines are 2-cell `block`-indented under their headings.
- The wizard BLOCK-gap-decline path emits the exact contracted error copy; exit/mutation contract unchanged.
- gofmt clean; CGO_ENABLED=0 build exit 0; TestSeamGrepGate green; make check green.
- Zero internal/* edits; zero new dependency; zero backend/image literal in cmd/villa.
</success_criteria>

<output>
Create `.planning/quick/260608-pyp-fix-remaining-phase-17-ui-spec-warnings-/260608-pyp-SUMMARY.md` when done, noting:
- The four warnings closed and the exact contracted strings emitted.
- Decision on warning #3: IMPLEMENTED (not waived) — the footer-help CTA route, no 17-UI-SPEC.md amendment.
- Whether any golden was refrozen (expected: none).
</output>
