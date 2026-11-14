package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runRecommendRaw executes `villa recommend <args>` against the fixture profile
// and returns the output and the command error.
func runRecommendRaw(t *testing.T, args ...string) (string, error) {
	t.Helper()
	prev := recommendProbe
	recommendProbe = fixtureProfile
	t.Cleanup(func() { recommendProbe = prev })
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"recommend"}, args...))
	err := root.Execute()
	return buf.String(), err
}

// TestRecommendEnvelopeWithSaveIsRefusedAndWritesNothing guards that a simulated
// pick can never reach config.toml: the refusal precedes every write and names
// the way out.
func TestRecommendEnvelopeWithSaveIsRefusedAndWritesNothing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	out, err := runRecommendRaw(t, "--envelope", "64GiB", "--save")
	if err == nil {
		t.Fatalf("--envelope with --save succeeded:\n%s", out)
	}
	if !strings.Contains(err.Error(), "--save") || !strings.Contains(err.Error(), "simulated") {
		t.Errorf("refusal %q does not explain itself", err)
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "villa", "config.toml")); !os.IsNotExist(statErr) {
		t.Errorf("config.toml exists after a refused save (stat err %v)", statErr)
	}
	if strings.Contains(out, "Recommended:") {
		t.Error("a recommendation was rendered before the refusal")
	}
}

// TestRecommendEnvelopeRefusesMalformedSizes guards the fail-closed parse at the
// command: a bad value is an error, never a silent fall back to the detected envelope.
func TestRecommendEnvelopeRefusesMalformedSizes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, v := range []string{"abc", "0", "-4GiB", "64"} {
		if out, err := runRecommendRaw(t, "--envelope", v); err == nil {
			t.Errorf("--envelope %q accepted:\n%s", v, out)
		}
	}
}

// TestRecommendEnvelopeIsStatedAsSimulated guards the plain statement in both
// surfaces, and that a normal run carries the marker as false.
func TestRecommendEnvelopeIsStatedAsSimulated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	run := func(args ...string) string {
		out, err := runRecommendRaw(t, args...)
		if err != nil {
			t.Fatalf("recommend %v: %v\n%s", args, err, out)
		}
		return out
	}

	table := run("--envelope", "32GiB")
	if !strings.Contains(table, "SIMULATED") || !strings.Contains(table, "32.000 GiB") {
		t.Errorf("table does not state the simulated envelope:\n%s", table)
	}
	if strings.Contains(run(), "SIMULATED") {
		t.Error("a normal run claims to be simulated")
	}

	var sim, normal map[string]any
	if err := json.Unmarshal([]byte(run("--envelope", "32GiB", "--json")), &sim); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(run("--json")), &normal); err != nil {
		t.Fatal(err)
	}
	if sim["simulated_envelope"] != true || normal["simulated_envelope"] != false {
		t.Errorf("simulated_envelope = %v / %v, want true / false", sim["simulated_envelope"], normal["simulated_envelope"])
	}
	if sim["usable_envelope_bytes"] != float64(32<<30) {
		t.Errorf("usable_envelope_bytes = %v, want 32 GiB", sim["usable_envelope_bytes"])
	}
}
