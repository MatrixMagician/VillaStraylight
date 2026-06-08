---
phase: quick-260608-ppy
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - cmd/villa/install.go
  - cmd/villa/install_wizard.go
  - cmd/villa/install_wizard_test.go
autonomous: true
requirements:
  - 17-UI-REVIEW-FIX-1
  - 17-UI-REVIEW-FIX-2
must_haves:
  truths:
    - "The wizard no-fit path emits the contracted empty-state copy including the usable-GiB envelope figure and the --no-tui parity note."
    - "The screen-1 detected-host Note appends the contracted typed-Unknown advisory ONLY when at least one host fact is not Known."
    - "make check stays green; CGO_ENABLED=0 go build ./cmd/villa exits 0; TestSeamGrepGate stays green."
  artifacts:
    - path: "cmd/villa/install.go"
      provides: "Contracted empty-state copy on the no-fit branch (replaces the generic substitute)."
    - path: "cmd/villa/install_wizard.go"
      provides: "Typed-Unknown advisory note appended by detectedHostSummary when any fact is not Known."
    - path: "cmd/villa/install_wizard_test.go"
      provides: "Table-driven assertions for both contracted strings (present/absent)."
  key_links:
    - from: "cmd/villa/install_wizard.go:detectedHostSummary"
      to: "detect.HostProfile typed-Unknown accessors (.Known)"
      via: "conditional advisory append"
      pattern: "could not be probed"
---

<objective>
Close the two phase-17 UI-SPEC copy gaps surfaced by 17-UI-REVIEW.md: the missing
empty-state "no fitting model" string (BLOCKER) and the missing typed-Unknown
advisory note (WARNING). Both are COMMAND-TIER presentation copy in cmd/villa/ only.

Purpose: bring the install surface into byte-exact agreement with the approved
17-UI-SPEC.md design contract for these two contracted strings.

Output: updated install.go no-fit copy + install_wizard.go detectedHostSummary,
with table-driven tests asserting both contracted strings verbatim.

SCOPE GUARD — fix ONLY these two gaps. The other four 17-UI-REVIEW warnings
(footer keymap, block indentation, step-2 "Use this model" CTA, BLOCK-declined
copy) are explicitly OUT OF SCOPE. Do not touch them.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/phases/17-guided-tui-install-capstone/17-UI-REVIEW.md
@.planning/phases/17-guided-tui-install-capstone/17-UI-SPEC.md
@cmd/villa/install.go
@cmd/villa/install_wizard.go
@cmd/villa/install_wizard_test.go

# Contract reminders (CLAUDE.md):
# - cmd/villa/ is presentation; add NO decision logic to internal/* cores.
# - No backend/image literals in cmd/villa (TestSeamGrepGate walks it).
# - gib(b uint64) lives at cmd/villa/recommend.go:194 — renders "X.XXX GiB (N bytes)".
# - UsableEnvelopeBytes is a detect.Bytes (typed-Unknown: .Known / .Value).
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Emit the contracted empty-state no-fit copy</name>
  <files>cmd/villa/install.go, cmd/villa/install_wizard_test.go</files>
  <behavior>
    - When d.pick refuses (rec.Model=="" || rec.ContextLen<=0 || rec.WeightBytes==0),
      the no-fit branch (currently install.go:~222-226) emits the EXACT contracted
      empty-state string from 17-UI-SPEC.md:195, with the `<N> GiB` placeholder
      replaced by the detected usable envelope figure (in GiB) from
      profile.UsableEnvelopeBytes:

        No catalog model fits the detected memory envelope (<N> GiB usable). Free memory or supply a larger-envelope host, then re-run villa install. (--no-tui shows the same result.)

    - <N> is the GiB figure derived from profile.UsableEnvelopeBytes. When that
      fact is typed-Unknown (!.Known), render the figure as `unknown` (NEVER a
      fabricated 0) so the sentence reads "(unknown GiB usable)".
    - Test (in install_wizard_test.go): a refusing pick produces output containing
      the contracted sentence verbatim, including the parity note
      "(--no-tui shows the same result.)" and the GiB envelope token.
    - Test: the Known-envelope case renders the numeric GiB figure (not "unknown").
  </behavior>
  <action>
    Replace the generic no-fit substitute at install.go:~222-226 (the
    "install: no fitting configuration for this host (memory envelope
    undeterminable — recommend refused)" lines) with the EXACT contracted
    empty-state copy quoted in the behavior block above and in 17-UI-SPEC.md:195.
    Copy the spec wording VERBATIM — do not paraphrase, do not add or drop words,
    keep the parenthetical "(--no-tui shows the same result.)" exactly.

    Substitute the `<N> GiB` token with the usable-envelope figure rendered from
    profile.UsableEnvelopeBytes. Reuse the existing gib helper (recommend.go:194)
    or render the GiB number directly; the contract wants a "GiB usable" figure,
    so emit just the GiB number followed by " GiB usable" (e.g. "8 GiB usable" or
    a fractional figure consistent with the existing GiB rendering). When
    profile.UsableEnvelopeBytes.Known is false, emit "unknown GiB usable" instead
    of a fabricated number — typed-Unknown must never become a confident 0.

    Keep returning exitBlocked. This branch fires on the flag-path BEFORE
    `useWizard` is evaluated, which is correct: pick refusal precedes the wizard,
    so the empty-state copy covers both paths from a single emission point. Do NOT
    duplicate the message inside the wizard.

    This is presentation copy only — add NO decision logic to internal/* and emit
    NO backend/image literal.
  </action>
  <verify>
    <automated>cd /home/oliverh/repos/github/MatrixMagician/VillaStraylight && go test ./cmd/villa/ -run 'Wizard|Install|NoFit|EmptyState' -count=1</automated>
  </verify>
  <done>The no-fit branch emits the contracted empty-state string verbatim with the usable-GiB envelope figure (or "unknown" when typed-Unknown) and the --no-tui parity note; tests assert it; exitBlocked preserved.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Append the typed-Unknown advisory note in detectedHostSummary</name>
  <files>cmd/villa/install_wizard.go, cmd/villa/install_wizard_test.go</files>
  <behavior>
    - detectedHostSummary (install_wizard.go:~181) appends, as a trailing line, the
      EXACT contracted advisory from 17-UI-SPEC.md:196:

        Some host facts could not be probed; villa will pick conservatively. Run villa detect for detail.

    - The advisory is appended ONLY when at least one of the rendered host facts is
      NOT Known (typed-Unknown), checked via the detect Optional/typed-Unknown
      accessors: p.CPUModel.Known, p.UsableEnvelopeBytes.Known, p.IGPUName.Known,
      p.KernelVersion.Known (mirror the facts detectedHostSummary already renders).
    - When ALL rendered facts are Known, the advisory is NOT appended (no trailing
      line, output unchanged from today's all-Known rendering).
    - Test: an all-Known profile produces a summary WITHOUT the advisory.
    - Test: a profile with at least one typed-Unknown fact produces a summary that
      contains the advisory string verbatim as a trailing line, AND still renders
      the bare per-field "unknown" token(s) (the advisory augments, never replaces,
      the existing strOrUnknown/bytesOrUnknown tokens).
  </behavior>
  <action>
    In detectedHostSummary (install_wizard.go:~181-189), after the existing
    backend line is written, compute whether any rendered fact is typed-Unknown:
    check .Known on the same facts the function renders — CPUModel, IGPUName,
    KernelVersion (detect.Str) and UsableEnvelopeBytes (detect.Bytes). Match the
    existing strOrUnknown / bytesOrUnknown unknown-conditions (note strOrUnknown
    also treats an empty Value as unknown).

    If any is not Known, append a newline + the EXACT contracted advisory quoted in
    the behavior block (and in 17-UI-SPEC.md:196). Copy the spec wording VERBATIM.
    Render it as a faint trailing line consistent with the existing faint/muted
    help styling used elsewhere in this file (do not introduce accent color; the
    advisory is help-tier, not status-tier).

    Keep the wizard a PURE collector — this is presentation only. Do NOT move the
    Known-check or copy into any internal/* core. Emit NO backend/image literal.
  </action>
  <verify>
    <automated>cd /home/oliverh/repos/github/MatrixMagician/VillaStraylight && go test ./cmd/villa/ -run 'DetectedHostSummary|Wizard|TypedUnknown' -count=1</automated>
  </verify>
  <done>detectedHostSummary appends the contracted advisory verbatim iff at least one rendered fact is not Known; all-Known omits it; bare "unknown" tokens still render; tests cover both branches.</done>
</task>

<task type="auto">
  <name>Task 3: Refreeze any old-string assertions and run full gates</name>
  <files>cmd/villa/install_test.go, cmd/villa/install_wizard_test.go</files>
  <action>
    Search cmd/villa for any existing test or golden/--json fixture that asserts the
    OLD generic no-fit string ("no fitting configuration for this host", "memory
    envelope undeterminable", "recommend refused") or that asserts the screen-1
    summary WITHOUT the advisory in a typed-Unknown case. If found, update it
    INTENTIONALLY to the new contracted copy — these are presentation-contract
    changes, not accidental drift. Document the refreeze in the SUMMARY.

    If a golden/--json contract literally embedded the old string, evolve it
    intentionally (append-only + schema bump where the project convention requires;
    refreeze with `go test ... -update` only for the affected golden, and confirm no
    unrelated golden changed). The no-fit copy is stderr human text, so it is most
    likely a plain-string assertion rather than a frozen golden — verify before
    using -update.

    Then run the full gate set and confirm all green.
  </action>
  <verify>
    <automated>cd /home/oliverh/repos/github/MatrixMagician/VillaStraylight && CGO_ENABLED=0 go build ./cmd/villa && make check && go test ./cmd/villa/ -run TestSeamGrepGate -count=1</automated>
  </verify>
  <done>No stale old-string assertions remain; CGO_ENABLED=0 build exits 0; make check (vet + full suite) is green; TestSeamGrepGate is green.</done>
</task>

</tasks>

<verification>
- Both contracted strings (17-UI-SPEC.md:195 empty-state, :196 typed-Unknown advisory)
  appear verbatim in the emitted output under their contracted conditions.
- Empty-state includes the usable-GiB envelope figure (or "unknown" when typed-Unknown)
  and the "(--no-tui shows the same result.)" parity note.
- Typed-Unknown advisory appears iff at least one host fact is not Known; absent when all Known.
- No decision logic added to internal/*; no backend/image literal added to cmd/villa.
- make check green; CGO_ENABLED=0 go build ./cmd/villa exit 0; TestSeamGrepGate green.
</verification>

<success_criteria>
- 17-UI-REVIEW BLOCKER (empty-state copy) closed: contracted string emitted with GiB figure + --no-tui note.
- 17-UI-REVIEW WARNING (typed-Unknown advisory) closed: contracted note appended conditionally.
- Scope held to exactly these two gaps; the other four warnings untouched.
- All gates green.
</success_criteria>

<output>
Create `.planning/quick/260608-ppy-fix-phase-17-ui-spec-copy-gaps-17-ui-rev/260608-ppy-SUMMARY.md` when done.
</output>
