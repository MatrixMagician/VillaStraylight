package subsystem

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestGatesAnswerTheConfigFlags pins each gate to its config field. These are the
// mappings every caller now depends on, so they are asserted once here rather than
// re-derived at each read site.
func TestGatesAnswerTheConfigFlags(t *testing.T) {
	cases := []struct {
		kind Kind
		set  func(*config.VillaConfig)
		key  string
	}{
		{Memory, func(c *config.VillaConfig) { c.MemoryEnabled = true }, "memory_enabled"},
		{WebSearch, func(c *config.VillaConfig) { c.WebSearchEnabled = true }, "web_search_enabled"},
		{Agent, func(c *config.VillaConfig) { c.AgentEnabled = true }, "agent_enabled"},
		{CodingMode, func(c *config.VillaConfig) { c.CodingMode = true }, "coding_mode"},
	}
	for _, tc := range cases {
		t.Run(tc.kind.String(), func(t *testing.T) {
			off := config.VillaConfig{}
			if On(off, tc.kind) {
				t.Errorf("%v reads on for a zero config; every optional subsystem defaults OFF", tc.kind)
			}
			on := config.VillaConfig{}
			tc.set(&on)
			if !On(on, tc.kind) {
				t.Errorf("%v reads off despite its flag being set", tc.kind)
			}
			if tc.kind.ConfigKey() != tc.key {
				t.Errorf("ConfigKey = %q, want %q — remediation text tells the operator what to edit", tc.kind.ConfigKey(), tc.key)
			}
		})
	}
}

// TestGatesAreIndependent: turning one subsystem on must not turn another on. A
// shared-flag bug here would enable a whole stack the operator never opted into,
// which is exactly the failure the scattered reads made hard to see.
func TestGatesAreIndependent(t *testing.T) {
	for _, k := range All {
		cfg := config.VillaConfig{}
		switch k {
		case Memory:
			cfg.MemoryEnabled = true
		case WebSearch:
			cfg.WebSearchEnabled = true
		case Agent:
			cfg.AgentEnabled = true
		case CodingMode:
			cfg.CodingMode = true
		}
		enabled := Enabled(cfg)
		if len(enabled) != 1 || enabled[0] != k {
			t.Errorf("enabling %v turned on %v; the gates must be independent", k, enabled)
		}
	}
}

// TestNamedAccessorsAgreeWithOn: the four convenience accessors and the enumerable
// form must never disagree, or a caller's choice of spelling would change the answer.
func TestNamedAccessorsAgreeWithOn(t *testing.T) {
	cfg := config.VillaConfig{
		MemoryEnabled:    true,
		WebSearchEnabled: false,
		AgentEnabled:     true,
		CodingMode:       false,
	}
	pairs := []struct {
		kind  Kind
		named bool
	}{
		{Memory, MemoryOn(cfg)},
		{WebSearch, WebSearchOn(cfg)},
		{Agent, AgentOn(cfg)},
		{CodingMode, CodingModeOn(cfg)},
	}
	for _, p := range pairs {
		if got := On(cfg, p.kind); got != p.named {
			t.Errorf("%v: On = %v but the named accessor = %v", p.kind, got, p.named)
		}
	}
}

// TestEnabledPreservesAllOrder: reporters render subsystems in a stable order, so a
// map-backed implementation (which would randomise it) must not creep in.
func TestEnabledPreservesAllOrder(t *testing.T) {
	cfg := config.VillaConfig{MemoryEnabled: true, WebSearchEnabled: true, AgentEnabled: true, CodingMode: true}
	got := Enabled(cfg)
	if len(got) != len(All) {
		t.Fatalf("Enabled returned %d of %d subsystems", len(got), len(All))
	}
	for i, k := range All {
		if got[i] != k {
			t.Errorf("Enabled[%d] = %v, want %v — the order must match All", i, got[i], k)
		}
	}
}

// TestModuleIsLoadBearing is the deletion test the memory module used to fail.
//
// The point of routing gates through here is that deleting this package would break
// callers. This asserts the property directly: the flags are read through the gate
// across the tiers, not around it. A direct predicate read of a gating flag outside
// this package (and outside config, which defines the fields) means the module has
// started sliding back toward decorative.
func TestModuleIsLoadBearing(t *testing.T) {
	// Predicate reads only: an `if cfg.MemoryEnabled`, a `&&`, a `return c.AgentEnabled`.
	// Assignments (`cfg.MemoryEnabled = ...`) are how a gate gets SET, which is a
	// different operation and legitimately touches the field.
	flags := regexp.MustCompile(`\b(cfg|c)\.(MemoryEnabled|WebSearchEnabled|AgentEnabled|CodingMode)\b\s*(?:[^=]|$)`)
	predicate := regexp.MustCompile(`\bif\b|&&|\|\||\breturn\b`)

	repoRoot := filepath.Join("..", "..")
	var bypasses []string

	for _, dir := range []string{"cmd/villa", "internal"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, rerr := filepath.Rel(repoRoot, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			// This package defines the gates; config defines the fields.
			if strings.HasPrefix(rel, "internal/subsystem/") || strings.HasPrefix(rel, "internal/config/") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for i, line := range strings.Split(string(data), "\n") {
				code := line
				if idx := strings.Index(code, "//"); idx >= 0 {
					code = code[:idx] // a comment may name a field freely
				}
				if flags.MatchString(code) && predicate.MatchString(code) {
					bypasses = append(bypasses, rel+":"+itoa(i+1))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	if len(bypasses) > 0 {
		t.Errorf("subsystem gates were read directly instead of through this module:\n  %s\n"+
			"Use subsystem.MemoryOn / WebSearchOn / AgentOn / CodingModeOn so the gate has one answer.",
			strings.Join(bypasses, "\n  "))
	}
}

// TestAlwaysOnSubsystemsAreOnForAZeroConfig guards the honesty of the always-on
// answer. Inference and chat have no config bool, so "is inference on?" must be
// true even for a config that has never been written — an install renders both
// units unconditionally, and a false here would let a caller skip the one
// subsystem the stack cannot run without.
func TestAlwaysOnSubsystemsAreOnForAZeroConfig(t *testing.T) {
	zero := config.VillaConfig{}
	for _, k := range []Kind{Inference, Chat} {
		if !On(zero, k) {
			t.Errorf("%v reads off for a zero config; it is on by construction", k)
		}
		if !k.AlwaysOn() {
			t.Errorf("%v.AlwaysOn() = false; it has no gate to turn off", k)
		}
		if key := k.ConfigKey(); key != "" {
			t.Errorf("%v.ConfigKey() = %q, want empty — there is no flag to edit", k, key)
		}
	}
}

// TestEveryNamesAllSubsystemsExactlyOnce is what the pin table depends on: a list
// it can key entries by. A duplicate would double-count a component and a gap would
// make a pinned component unnameable, so both are checked rather than assumed.
func TestEveryNamesAllSubsystemsExactlyOnce(t *testing.T) {
	seen := map[Kind]bool{}
	for _, k := range Every {
		if seen[k] {
			t.Errorf("Every names %v twice", k)
		}
		seen[k] = true
		if k.String() == "unknown" {
			t.Errorf("Every names a Kind with no String(): %d", int(k))
		}
	}
	for _, k := range All {
		if !seen[k] {
			t.Errorf("Every omits the optional subsystem %v", k)
		}
	}
	if !seen[Inference] || !seen[Chat] {
		t.Error("Every omits an always-on subsystem")
	}
}

// TestAllStaysTheOptionalSet is the decision guard for the widening.
//
// All was deliberately NOT widened, because Enabled walks it and every caller of
// Enabled asks "which addons did the operator turn on?". Widening it would make
// status, doctor and install each report inference and chat as enabled addons, with
// no compile error to catch it. If a future change widens All, this test fails and
// forces the caller audit that decision needs.
func TestAllStaysTheOptionalSet(t *testing.T) {
	for _, k := range All {
		if k.AlwaysOn() {
			t.Errorf("All contains the always-on subsystem %v; Enabled would report it as an enabled addon", k)
		}
	}
	// Enabled over a fully-enabled config must still return exactly the optional
	// set, never the always-on pair.
	cfg := config.VillaConfig{MemoryEnabled: true, WebSearchEnabled: true, AgentEnabled: true, CodingMode: true}
	if got := len(Enabled(cfg)); got != len(All) {
		t.Errorf("Enabled returned %d subsystems for a fully-enabled config, want %d", got, len(All))
	}
}

// TestKindValuesAreStable pins the iota numbering. A Kind is a map key in the pin
// state store, so an inserted member would silently renumber every value already
// written to disk and re-point one subsystem's effective pin at another.
func TestKindValuesAreStable(t *testing.T) {
	want := map[Kind]int{Memory: 0, WebSearch: 1, Agent: 2, CodingMode: 3, Inference: 4, Chat: 5}
	for k, n := range want {
		if int(k) != n {
			t.Errorf("%v = %d, want %d — a member was inserted rather than appended, renumbering stored values", k, int(k), n)
		}
	}
}

// TestOwnedStateIsTheWholeMapping asserts EVERY subsystem's answer, not just the
// two that say yes.
//
// A spot-check on chat and memory would pass against a version that returned true
// for everything, which is the failure that matters: snapshotting inference would
// try to export a volume it does not own, and snapshotting the models volume would
// copy tens of gigabytes of weights `update` is explicitly not responsible for. The
// negatives are the assertion.
func TestOwnedStateIsTheWholeMapping(t *testing.T) {
	want := map[Kind]string{
		Inference:  "",
		Chat:       "villa-openwebui",
		Memory:     "villa-qdrant",
		WebSearch:  "",
		Agent:      "",
		CodingMode: "",
	}
	if len(want) != len(Every) {
		t.Fatalf("the mapping covers %d subsystems but Every names %d — a new subsystem must declare whether it owns state", len(want), len(Every))
	}
	for _, k := range Every {
		vol, owns := k.StateVolume()
		wantVol, ok := want[k]
		if !ok {
			t.Fatalf("%v is not in the expected mapping", k)
		}
		if owns != (wantVol != "") {
			t.Errorf("%v OwnsPersistentState = %v, want %v", k, owns, wantVol != "")
		}
		if vol != wantVol {
			t.Errorf("%v StateVolume = %q, want %q", k, vol, wantVol)
		}
		if owns != k.OwnsPersistentState() {
			t.Errorf("%v: StateVolume and OwnsPersistentState disagree", k)
		}
	}
}

// TestStatefulWalksTheDeclaration: Stateful() is exactly the subsystems that own
// state, in Every order, and never anything else.
//
// The ORDER matters because the update flow applies subsystems in Every order, so
// a stateful list in a different order would snapshot in an order the apply never
// uses and make a test's ordering assertion meaningless.
func TestStatefulWalksTheDeclaration(t *testing.T) {
	got := Stateful()
	if len(got) != 2 {
		t.Fatalf("Stateful() = %v, want exactly chat and memory", got)
	}
	if got[0] != Chat || got[1] != Memory {
		t.Errorf("Stateful() = %v, want [chat memory] — Every order, chat before memory", got)
	}
	for _, k := range got {
		if !k.OwnsPersistentState() {
			t.Errorf("Stateful() names %v, which does not own persistent state", k)
		}
	}
}

// TestReadOnlyMountsAreNotOwnedState is the negative gate on the declaration.
//
// villa-embed and villa-llama mount the models volume read-only and villa-websafe
// bind-mounts the villa binary read-only. None is state its subsystem owns, and
// naming any of them here would point a snapshot at model weights `update` must
// never touch. This fails if the models volume ever appears in the mapping.
func TestReadOnlyMountsAreNotOwnedState(t *testing.T) {
	forbidden := map[string]string{
		"villa-models": "the shared model store is mounted read-only and holds weights update never touches",
	}
	for _, k := range Every {
		vol, owns := k.StateVolume()
		if !owns {
			continue
		}
		if why, bad := forbidden[vol]; bad {
			t.Errorf("%v declares %q as owned state: %s", k, vol, why)
		}
	}
	if Inference.OwnsPersistentState() {
		t.Error("inference declares owned state; its models mount is read-only")
	}
	if WebSearch.OwnsPersistentState() {
		t.Error("web search declares owned state; the websafe binary mount is read-only and SearXNG's settings are a read-only bind")
	}
}

// itoa avoids pulling strconv in for one call in a test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
