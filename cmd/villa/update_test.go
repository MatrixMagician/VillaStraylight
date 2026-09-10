package main

// update_test.go drives `villa update --check` through runUpdate, the same body the
// cobra RunE calls, so the exit codes and the printed wording are what is asserted
// rather than a helper behind them.
//
// The exit codes are the scriptable product here. `if villa update --check; then`
// must read as "you are current", and a Reject must never take the same code as
// "nothing to do".

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/manifest"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/updatefetch"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

// updateHarness drives one runUpdate call and captures both streams.
type updateHarness struct {
	out, errOut bytes.Buffer
	saved       *pinstate.State
	// stackRunning defaults false, which is the stopped-stack refusal — the state
	// an apply test has to opt OUT of rather than into, so a test that forgets gets
	// the safe answer.
	stackRunning bool
	flowDeps     updateflow.Deps
}

// run invokes the check path with an injected fetch result.
func (h *updateHarness) run(t *testing.T, cfg config.VillaConfig, state pinstate.State, fetched updatefetch.Fetched, fetchErr error, args []string, flags updateFlags) int {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetOut(&h.out)
	cmd.SetErr(&h.errOut)
	cmd.SetContext(context.Background())

	deps := updateDeps{
		StackRunning:   func() bool { return h.stackRunning },
		FlowDeps:       func(context.Context) updateflow.Deps { return h.flowDeps },
		ReferencedRefs: func() map[string]bool { return nil },
		LoadConfig:     func() (config.VillaConfig, error) { return cfg, nil },
		LoadPinState:   func() (pinstate.State, error) { return state, nil },
		SavePinState:   func(s pinstate.State) error { h.saved = &s; return nil },
		Fetch:          func(context.Context) (updatefetch.Fetched, error) { return fetched, fetchErr },
		Now:            func() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) },
		VillaVersion:   func() string { return "v1.7" },
	}
	return runUpdate(cmd, args, deps, flags)
}

// text is everything printed, across both streams.
func (h *updateHarness) text() string { return h.out.String() + h.errOut.String() }

// signedManifest builds a manifest the verifier would accept, if the binary carried
// the matching key. It returns the bytes, the signature, and the key.
func signedManifest(t *testing.T, mutate func(*manifest.Document)) (updatefetch.Fetched, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	doc := manifest.FromTable(11, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if mutate != nil {
		mutate(&doc)
	}
	data, err := manifest.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	sig, err := manifest.Sign(priv, data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return updatefetch.Fetched{Manifest: data, Signature: sig, Host: updatefetch.Host()}, pub
}

// TestCheckWithNoManifestIsARejectThatCannotReadAsUpToDate is the most important
// assertion in this file.
//
// The cost of the misreading is a user believing they are current for months, so
// the output has to NAME the misreading and refuse it. Testing for the exact
// sentence is deliberate: it is the one string in the verb whose absence would be
// invisible in every other test, since a Reject with cheerful wording still exits 1.
func TestCheckWithNoManifestIsARejectThatCannotReadAsUpToDate(t *testing.T) {
	var h updateHarness
	code := h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{},
		updatefetch.Fetched{}, nil, nil, updateFlags{check: true})

	if code != exitBlocked {
		t.Errorf("exit = %d, want %d — could-not-check must not share an exit code with 'nothing to do'", code, exitBlocked)
	}
	got := h.text()
	if !strings.Contains(got, "Could not check") {
		t.Errorf("the output does not say villa could not check:\n%s", got)
	}
	if !strings.Contains(got, `This is not "you are up to date"`) {
		t.Errorf("the output does not refuse the up-to-date misreading:\n%s", got)
	}
	if strings.Contains(strings.ToLower(got), "up to date.") || strings.Contains(got, "everything is current") {
		t.Errorf("the Reject contains reassuring wording that reads as being current:\n%s", got)
	}
}

// TestARejectStatesTheFromRegistriesCostAtThePointOfUse: the fingerprint cost has
// to be where the user chooses, not only in the docs. A user reading the Reject is
// exactly the user about to reach for the opt-in.
func TestARejectStatesTheFromRegistriesCostAtThePointOfUse(t *testing.T) {
	var h updateHarness
	h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{}, updatefetch.Fetched{}, nil, nil, updateFlags{check: true})

	got := h.text()
	if !strings.Contains(got, "--from-registries") {
		t.Errorf("the Reject does not mention the opt-in:\n%s", got)
	}
	if !strings.Contains(got, "which addons") {
		t.Errorf("the Reject does not state that --from-registries reveals which addons are enabled:\n%s", got)
	}
}

// TestNeverCheckedIsItsOwnStateInTheReject: a host that has never asked and a host
// whose last check was long ago are different facts, and only one of them is
// staleness. Printing a blank timestamp for the first would be worse than either.
func TestNeverCheckedIsItsOwnStateInTheReject(t *testing.T) {
	var never, stale updateHarness
	never.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{}, updatefetch.Fetched{}, nil, nil, updateFlags{check: true})
	stale.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{CheckedAt: "2026-04-02T09:12:44Z"},
		updatefetch.Fetched{}, nil, nil, updateFlags{check: true})

	if !strings.Contains(never.text(), "never completed an update check") {
		t.Errorf("a never-checked host does not say so:\n%s", never.text())
	}
	if !strings.Contains(stale.text(), "2026-04-02") {
		t.Errorf("a previously-checked host does not report when:\n%s", stale.text())
	}
	if strings.Contains(stale.text(), "never completed") {
		t.Errorf("a previously-checked host reads as never checked:\n%s", stale.text())
	}
}

// TestARejectRecordsNoCheck: recording a timestamp for a check that failed would
// make status report freshness villa does not have, which is the same lie as "you
// are up to date" wearing a different hat.
func TestARejectRecordsNoCheck(t *testing.T) {
	var h updateHarness
	h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{}, updatefetch.Fetched{}, nil, nil, updateFlags{check: true})
	if h.saved != nil {
		t.Errorf("a failed check recorded CheckedAt = %q; status would then report freshness villa does not have", h.saved.CheckedAt)
	}
}

// TestANetworkFailureIsARejectNotACrash: being offline is a could-not-check like
// any other, reported through the same path so it cannot read as "up to date"
// either.
func TestANetworkFailureIsARejectNotACrash(t *testing.T) {
	var h updateHarness
	code := h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{},
		updatefetch.Fetched{}, context.DeadlineExceeded, nil, updateFlags{check: true})

	if code != exitBlocked {
		t.Errorf("exit = %d, want %d for an unreachable endpoint", code, exitBlocked)
	}
	if !strings.Contains(h.text(), `This is not "you are up to date"`) {
		t.Errorf("an offline check does not refuse the up-to-date misreading:\n%s", h.text())
	}
}

// TestABuildWithNoKeyRefusesRatherThanReportingCurrent: the binary carries no
// verification key yet, so every real manifest is refused. The important property
// is that this exits 1 and reads as could-not-check, never 0.
func TestABuildWithNoKeyRefusesRatherThanReportingCurrent(t *testing.T) {
	fetched, _ := signedManifest(t, nil)

	var h updateHarness
	code := h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{}, fetched, nil, nil, updateFlags{check: true})

	if code == exitPass {
		t.Fatal("a manifest villa cannot verify was reported as 'you are current'")
	}
	if code != exitBlocked {
		t.Errorf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(h.text(), `This is not "you are up to date"`) {
		t.Errorf("an unverifiable manifest does not refuse the up-to-date misreading:\n%s", h.text())
	}
}

// TestApplyWithNoManifestTakesTheSameRejectPath: villa does not know what to apply,
// and "could not check" is the honest answer whichever verb asked. Inventing a
// second wording for it would let one of the two drift into something that reads
// like "nothing to do".
func TestApplyWithNoManifestTakesTheSameRejectPath(t *testing.T) {
	var h updateHarness
	code := h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{}, updatefetch.Fetched{}, nil, nil, updateFlags{})

	if code != exitBlocked {
		t.Errorf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(h.text(), `This is not "you are up to date"`) {
		t.Errorf("apply with no manifest does not refuse the up-to-date misreading:\n%s", h.text())
	}
}

// TestFromRegistriesStatesItsCostEvenWhenUnimplemented: the flag exists in the help
// and the cost travels with it, so a user cannot opt in without meeting the reason
// not to.
func TestFromRegistriesStatesItsCostEvenWhenUnimplemented(t *testing.T) {
	var h updateHarness
	h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{}, updatefetch.Fetched{}, nil, nil,
		updateFlags{check: true, fromRegistries: true})

	if !strings.Contains(h.text(), "which addons you have enabled") {
		t.Errorf("--from-registries does not state its fingerprint cost:\n%s", h.text())
	}
}

// TestTheFromRegistriesHelpTextCarriesTheCost: the ticket requires the cost in the
// HELP TEXT, not only in the docs, because help is where a user meets the flag.
func TestTheFromRegistriesHelpTextCarriesTheCost(t *testing.T) {
	cmd := newUpdate()
	flag := cmd.Flags().Lookup("from-registries")
	if flag == nil {
		t.Fatal("--from-registries is not a flag")
	}
	for _, want := range []string{"reveals", "addons"} {
		if !strings.Contains(strings.ToLower(flag.Usage), want) {
			t.Errorf("the --from-registries help does not contain %q:\n%s", want, flag.Usage)
		}
	}
}

// TestCheckWorksAgainstAStoppedStack is the asymmetry with apply, asserted rather
// than documented: nothing in the check path touches a running service, so a
// stopped stack changes nothing about the outcome.
func TestCheckWorksAgainstAStoppedStack(t *testing.T) {
	// The harness wires no systemd, no podman and no probe seam at all. If the
	// check path reached for a running service it could not compile, let alone
	// pass — which is the property, expressed structurally.
	var h updateHarness
	code := h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{}, updatefetch.Fetched{}, nil, nil, updateFlags{check: true})
	if code != exitBlocked {
		t.Errorf("exit = %d; the check ran to a verdict without a running stack, which is the point", code)
	}
	if !strings.Contains(cmdLongText(t), "STOPPED stack") {
		t.Error("the help text does not state that --check works on a stopped stack")
	}
}

// cmdLongText returns the update command's long help.
func cmdLongText(t *testing.T) string {
	t.Helper()
	return newUpdate().Long
}

// TestUnknownSubsystemTeachesTheModel: "qdrant" is a real thing a user knows the
// name of, so the useful reply says where it lives and why it moves with its
// sibling — not just that the word was wrong.
func TestUnknownSubsystemTeachesTheModel(t *testing.T) {
	var h updateHarness
	code := h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{}, updatefetch.Fetched{}, nil,
		[]string{"qdrant"}, updateFlags{check: true})

	if code != exitBlocked {
		t.Errorf("exit = %d, want %d for an unknown subsystem", code, exitBlocked)
	}
	got := h.text()
	for _, want := range []string{"part of the memory subsystem", "verify memory proves Qdrant and the embedder together", "villa update memory"} {
		if !strings.Contains(got, want) {
			t.Errorf("the error does not teach the subsystem model (missing %q):\n%s", want, got)
		}
	}
	if !strings.Contains(got, "inference, chat, memory, search, agent") {
		t.Errorf("the error does not list the valid subsystems:\n%s", got)
	}
}

// TestEverySubsystemNameIsAccepted: the names printed in the error must be the
// names the parser takes, or villa contradicts itself in consecutive lines.
func TestEverySubsystemNameIsAccepted(t *testing.T) {
	for _, name := range []string{"inference", "chat", "memory", "search", "agent", "sandbox"} {
		if _, ok := subsystemByName(name); !ok {
			t.Errorf("%q is listed as a valid subsystem but the parser refuses it", name)
		}
	}
	if _, ok := subsystemByName("qdrant"); ok {
		t.Error("a container name was accepted as a subsystem")
	}
}

// TestEveryComponentHasASubsystemAUserCanName: a component in the table that no
// argument can select would be unreachable from the CLI.
func TestEveryComponentHasASubsystemAUserCanName(t *testing.T) {
	for _, e := range pins.Table() {
		found := false
		for _, name := range []string{"inference", "chat", "memory", "search", "agent", "sandbox"} {
			if k, ok := subsystemByName(name); ok && k == e.Subsystem {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("component %q belongs to subsystem %v, which no CLI argument selects", e.Component, e.Subsystem)
		}
	}
}

// TestStatusNeverTriggersACheck is the on-command rule at the surface that would
// break it.
//
// status is polled by the dashboard, so a check there would be network access on a
// UI refresh loop — telemetry-shaped even with an innocent payload. The seam status
// is given is a store READ, and this asserts the wiring stays that way.
func TestStatusNeverTriggersACheck(t *testing.T) {
	src := readCmdSource(t, "status.go")
	for _, forbidden := range []string{"updatefetch.Fetch", "updatefetch.LiveDeps", "manifestverify.Verify"} {
		if strings.Contains(src, forbidden) {
			t.Errorf("status.go references %q; status is polled by the dashboard, so a check there is network access on a UI refresh loop", forbidden)
		}
	}
}

// TestOnlyTheUpdateVerbFetches: no other command may check opportunistically, or
// villa's unqualified zero-telemetry claim stops being true.
func TestOnlyTheUpdateVerbFetches(t *testing.T) {
	for _, name := range cmdGoFiles(t) {
		if name == "update.go" {
			continue
		}
		if strings.Contains(readCmdSource(t, name), "updatefetch.") {
			t.Errorf("%s reaches for the update fetch seam; checks are strictly on-command and belong only to `villa update`", name)
		}
	}
}

// readCmdSource reads a file in cmd/villa with comments stripped, so a doc comment
// may name a forbidden symbol freely.
func readCmdSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var b strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// cmdGoFiles lists the non-test Go sources in cmd/villa.
func cmdGoFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestPrintRejectNamesDivergedPins is the fix for #198: the Reject claimed every
// component was running the pin villa vetted, even when the pin state store
// recorded an effective pin that differs (an out-of-band `villa backend set`, a
// `villa sandbox build`). The three cases run the real reject path end to end —
// LoadPinState through printReject — because the wrongness lives in what the
// resolver reports, not just in how printReject formats a field it is handed.
func TestPrintRejectNamesDivergedPins(t *testing.T) {
	entry, ok := pins.Lookup(pins.OpenWebUI)
	if !ok {
		t.Fatal("pins.OpenWebUI is not in the table")
	}
	vetted := entry.Vetted().Ref

	// This is today's text, captured from the unmodified code with the same
	// inputs as the "no divergence" and "equal effective" cases below: an empty
	// state resolves every component to its vetted pin.
	const wantPlain = "Could not check for updates.\n\n" +
		"  No pin manifest is available. Villa is using the pins compiled into this binary.\n\n" +
		"  This is not \"you are up to date\" — villa could not determine anything.\n\n" +
		"Your stack is running the pins villa shipped with, which were vetted on\n" +
		"gfx1151 hardware. Nothing is wrong; nothing was checked.\n\n" +
		"  villa update --check --from-registries\n" +
		"      Ask each registry directly instead. This contacts one endpoint per\n" +
		"      installed component, which reveals to those registries which addons\n" +
		"      you have enabled. The manifest check does not.\n\n" +
		"Villa has never completed an update check on this host.\n"
	const bannedPlainSentence = `running the pins villa shipped with, which were vetted`

	cases := []struct {
		name  string
		state pinstate.State
		want  string // exact match when set
	}{
		{
			name:  "no recorded pins is not divergence",
			state: pinstate.State{},
			want:  wantPlain,
		},
		{
			name: "a recorded effective pin equal to vetted is not divergence",
			state: pinstate.State{Pins: map[string]pinstate.Effective{
				string(pins.OpenWebUI): {Ref: vetted},
			}},
			want: wantPlain,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h updateHarness
			h.run(t, config.VillaConfig{Backend: "vulkan"}, tc.state, updatefetch.Fetched{}, nil, nil, updateFlags{check: true})
			if got := h.text(); got != tc.want {
				t.Errorf("output =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}

	t.Run("a diverged effective pin is named with both short refs", func(t *testing.T) {
		var h updateHarness
		h.run(t, config.VillaConfig{Backend: "vulkan"}, pinstate.State{Pins: map[string]pinstate.Effective{
			string(pins.OpenWebUI): {Ref: "ghcr.io/open-webui/open-webui:main@sha256:deadbeef00112233445566778899aabbccddeeff00112233445566778899aa"},
		}}, updatefetch.Fetched{}, nil, nil, updateFlags{check: true})

		got := h.text()
		if !strings.Contains(got, string(pins.OpenWebUI)) {
			t.Errorf("the Reject does not name the diverged component %q:\n%s", pins.OpenWebUI, got)
		}
		if !strings.Contains(got, proseRef("ghcr.io/open-webui/open-webui:main@sha256:deadbeef00112233445566778899aabbccddeeff00112233445566778899aa")) {
			t.Errorf("the Reject does not state the running ref:\n%s", got)
		}
		if !strings.Contains(got, proseRef(vetted)) {
			t.Errorf("the Reject does not state the vetted ref:\n%s", got)
		}
		if strings.Contains(got, bannedPlainSentence) {
			t.Errorf("the Reject still claims every pin is vetted despite the divergence:\n%s", got)
		}
	})
}
