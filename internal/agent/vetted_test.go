package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestParseAgentVersion guards the version parse: the first semver-looking token of
// `--version` output wins, whatever surrounds it, and output with none is not a
// version.
func TestParseAgentVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"2.1.317 (Claude Code)\n", "2.1.317", true},
		{"0.26.6\n", "0.26.6", true},
		{"v0.26.6", "0.26.6", true},
		{"qwen 0.27.0-nightly.20261101 build 4.5.6", "0.27.0-nightly.20261101", true},
		{"", "", false},
		{"command not found", "", false},
		{"1.2", "", false},
	} {
		got, ok := ParseAgentVersion(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseAgentVersion(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// pathAgent describes one PATH-resolved launch target for the table below.
type pathAgent struct {
	name   string
	bin    string
	vetted string
	// run drives the target's Run function over d.
	run func(d Deps) Result
	// out is `--version` output for the vetted version, in the agent's own shape.
	out string
}

var pathAgents = []pathAgent{
	{"claude", claudeBin, VettedClaudeVersion, func(d Deps) Result { return RunClaude(d, nil) }, VettedClaudeVersion + " (Claude Code)\n"},
	{"qwen", qwenBin, VettedQwenVersion, func(d Deps) Result { return RunQwen(d, nil) }, VettedQwenVersion + "\n"},
}

// versionRig builds Deps for a PATH agent with coding mode on and the binary found,
// plus a recorder of every AgentVersion call.
type versionRig struct {
	calls   []string
	envs    [][]string
	out     string
	err     error
	deps    Deps
	setBin  string
	launchN int
}

func newVersionRig(a pathAgent, cfg config.VillaConfig) *versionRig {
	r := &versionRig{setBin: "/usr/bin/" + a.bin}
	r.deps = Deps{
		LoadConfig:        func() (config.VillaConfig, error) { return cfg, nil },
		LookPath:          func(bin string) (string, bool) { return "/usr/bin/" + bin, bin == a.bin },
		QwenHome:          func() (string, error) { return "/h", nil },
		WriteQwenSettings: func(string, []byte) error { return nil },
		LaunchClaude:      func(string, []string, []string) error { r.launchN++; return nil },
		AgentVersion: func(bin string, env []string) (string, error) {
			r.calls = append(r.calls, bin)
			r.envs = append(r.envs, env)
			return r.out, r.err
		},
	}
	return r
}

var codingCfg = config.VillaConfig{Model: "qwen3", CodingMode: true, InferenceSecret: "s"}

// TestVettedVersionMatchesRaisesNoWarning guards the quiet path: the vetted version,
// in each agent's own output shape, launches with no warning. The probe gets the
// resolved binary path and the launch env, so it runs under the same contract.
func TestVettedVersionMatchesRaisesNoWarning(t *testing.T) {
	for _, a := range pathAgents {
		t.Run(a.name, func(t *testing.T) {
			r := newVersionRig(a, codingCfg)
			r.out = a.out
			res := a.run(r.deps)
			if !res.ReadyToLaunch || len(res.Warnings) != 0 {
				t.Fatalf("Result = %+v; want ReadyToLaunch and no warnings", res)
			}
			if len(r.calls) != 1 || r.calls[0] != r.setBin {
				t.Errorf("AgentVersion calls = %v; want exactly [%s]", r.calls, r.setBin)
			}
			if strings.Join(r.envs[0], "\n") != strings.Join(res.LaunchEnv, "\n") {
				t.Errorf("AgentVersion env differs from the launch env; the probe must run under the contract")
			}
		})
	}
}

// TestUnvettedVersionWarnsAndStillLaunches guards the privacy statement: a different
// installed version raises exactly one warning naming both versions, saying the claim
// holds for the vetted one only and pointing at the re-vet steps, and never blocks.
func TestUnvettedVersionWarnsAndStillLaunches(t *testing.T) {
	for _, a := range pathAgents {
		t.Run(a.name, func(t *testing.T) {
			r := newVersionRig(a, codingCfg)
			r.out = "9.9.9 (build)\n"
			res := a.run(r.deps)
			if !res.ReadyToLaunch || res.Err != nil {
				t.Fatalf("Result = %+v; a version warning must not block the launch", res)
			}
			if len(res.Warnings) != 1 {
				t.Fatalf("warnings = %+v; want exactly one", res.Warnings)
			}
			w := res.Warnings[0]
			if w.Code != "agent_version_unvetted" {
				t.Errorf("Code = %q; want agent_version_unvetted", w.Code)
			}
			for _, want := range []string{"9.9.9", a.vetted, "docs/RELEASING.md", "vetted version only"} {
				if !strings.Contains(w.Msg, want) {
					t.Errorf("Msg %q lacks %q", w.Msg, want)
				}
			}
		})
	}
}

// TestUnreadableVersionWarnsUnknown guards typed-Unknown: a failed probe or output
// with no version is its own warning, distinct from a confident mismatch, and the
// launch still proceeds.
func TestUnreadableVersionWarnsUnknown(t *testing.T) {
	for _, a := range pathAgents {
		for name, set := range map[string]func(*versionRig){
			"probe error":   func(r *versionRig) { r.err = errors.New("timed out") },
			"no version in": func(r *versionRig) { r.out = "usage: nothing numeric\n" },
		} {
			t.Run(a.name+"/"+name, func(t *testing.T) {
				r := newVersionRig(a, codingCfg)
				set(r)
				res := a.run(r.deps)
				if !res.ReadyToLaunch || len(res.Warnings) != 1 {
					t.Fatalf("Result = %+v; want ReadyToLaunch and one warning", res)
				}
				w := res.Warnings[0]
				if w.Code != "agent_version_unknown" {
					t.Errorf("Code = %q; want agent_version_unknown", w.Code)
				}
				for _, want := range []string{a.vetted, "docs/RELEASING.md", "vetted version only"} {
					if !strings.Contains(w.Msg, want) {
						t.Errorf("Msg %q lacks %q", w.Msg, want)
					}
				}
			})
		}
	}
}

// TestRefusalsNeverProbeTheVersion guards ordering: the probe runs only after every
// earlier gate passed, so a refused launch never executes the agent binary.
func TestRefusalsNeverProbeTheVersion(t *testing.T) {
	for _, a := range pathAgents {
		t.Run(a.name+"/coding mode off", func(t *testing.T) {
			r := newVersionRig(a, config.VillaConfig{Model: "qwen3"})
			res := a.run(r.deps)
			if !res.CodingModeOff || len(r.calls) != 0 {
				t.Errorf("CodingModeOff = %v, probe calls = %v; want true and none", res.CodingModeOff, r.calls)
			}
		})
		t.Run(a.name+"/binary absent", func(t *testing.T) {
			r := newVersionRig(a, codingCfg)
			r.deps.LookPath = func(string) (string, bool) { return "", false }
			res := a.run(r.deps)
			if !res.BinaryAbsent || len(r.calls) != 0 {
				t.Errorf("BinaryAbsent = %v, probe calls = %v; want true and none", res.BinaryAbsent, r.calls)
			}
		})
	}
	t.Run("qwen/settings write failed", func(t *testing.T) {
		r := newVersionRig(pathAgents[1], codingCfg)
		r.deps.WriteQwenSettings = func(string, []byte) error { return errors.New("disk full") }
		res := RunQwen(r.deps, nil)
		if res.Err == nil || len(r.calls) != 0 {
			t.Errorf("Err = %v, probe calls = %v; want an error and none", res.Err, r.calls)
		}
	})
}

// TestMissingVersionSeamIsUnknownNotSilent guards fail-closed wiring: a Deps with no
// AgentVersion seam reads as an unreadable version, never as a silent pass.
func TestMissingVersionSeamIsUnknownNotSilent(t *testing.T) {
	r := newVersionRig(pathAgents[0], codingCfg)
	r.deps.AgentVersion = nil
	res := RunClaude(r.deps, nil)
	if !res.ReadyToLaunch || len(res.Warnings) != 1 || res.Warnings[0].Code != "agent_version_unknown" {
		t.Errorf("Result = %+v; want ReadyToLaunch and one agent_version_unknown warning", res)
	}
}
