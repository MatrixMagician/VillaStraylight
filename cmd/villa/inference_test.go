package main

import (
	"bytes"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// update + assertGolden + goldenPath are shared in this package (detect_test.go /
// preflight_test.go).

// passVerdict is an all-green fixture Verdict (exit 0).
func passVerdict() inference.Verdict {
	return inference.Verdict{
		Status:        inference.StatusPass,
		Detail:        "offload proven (log + sysfs), chat returned 7 tokens, and the context ceiling cleared at ctx 131072",
		Provenance:    "log-scrape + amdgpu mem_info_gtt_used delta",
		LogOffload:    detect.KnownBool(true, "ggml_vulkan device + load_tensors offloaded line"),
		SysfsOffload:  detect.KnownBool(true, "mem_info_gtt_used before/after delta"),
		GTTDeltaBytes: 419430400,
	}
}

// warnVerdict is a WARN fixture (exit 2): offload proven but a ceiling cliff hit.
func warnVerdict() inference.Verdict {
	v := passVerdict()
	v.Status = inference.StatusWarn
	v.Detail = "offload proven and chat OK (7 tokens), but the context ceiling was hit: context-ceiling OOM cliff"
	v.Remediation = "safe up to ~ctx 98304; the near-max-context probe surfaced a cliff — keep context below the ceiling"
	return v
}

// failVerdict is a FAIL fixture (exit 1): confirmed CPU fallback (D-11).
func failVerdict() inference.Verdict {
	return inference.Verdict{
		Status:       inference.StatusFail,
		Detail:       "offload FAILED — log: software renderer \"llvmpipe\" enumerated, not a real GPU; sysfs: GTT-used grew only 0 bytes",
		Remediation:  "GPU offload did not engage — check /dev/dri passthrough, keep-groups, and that the RADV ICD is present (not llvmpipe)",
		Provenance:   "log-scrape + amdgpu mem_info_gtt_used delta",
		LogOffload:   detect.KnownBool(false, "ggml_vulkan device line"),
		SysfsOffload: detect.KnownBool(false, "mem_info_gtt_used before/after delta"),
	}
}

// TestInferenceExitCodes: PASS→0, WARN→2, FAIL→1 (the scriptable contract reused
// from preflight, D-13).
func TestInferenceExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		verdict  inference.Verdict
		wantCode int
	}{
		{"pass", passVerdict(), exitPass},
		{"warn", warnVerdict(), exitWarn},
		{"fail", failVerdict(), exitBlocked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			code := renderInference(&buf, tc.verdict, false, false)
			if code != tc.wantCode {
				t.Errorf("renderInference(%s): exit=%d, want %d", tc.name, code, tc.wantCode)
			}
		})
	}
}

// TestInferenceJSONGolden freezes the --json Verdict shape — the Phase-3 status /
// Phase-5 dashboard contract (D-11). A change to the emitted JSON must be a
// deliberate -update, never an accident.
func TestInferenceJSONGolden(t *testing.T) {
	cases := []struct {
		name    string
		verdict inference.Verdict
		golden  string
	}{
		{"pass", passVerdict(), "inference-pass.json.golden"},
		{"fail", failVerdict(), "inference-fail.json.golden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderInference(&buf, tc.verdict, true, false)
			assertGolden(t, tc.golden, buf.Bytes())
		})
	}
}

// TestInferenceTableContainsVerdict: the human table mode prints the status word and
// the offload detail so a user sees pass/warn/fail at a glance.
func TestInferenceTableContainsVerdict(t *testing.T) {
	var buf bytes.Buffer
	renderInference(&buf, failVerdict(), false, false)
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("FAIL")) {
		t.Errorf("table must print the FAIL status word, got:\n%s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("offload")) {
		t.Errorf("table must print the offload detail, got:\n%s", out)
	}
}

// TestInferenceRegistered asserts the `inference` noun is wired into the root
// command tree with run + validate subcommands and does not collide with the
// Phase-3 lifecycle verbs (D-13).
func TestInferenceRegistered(t *testing.T) {
	root := newRoot()
	var inf *cobraCommandShim
	var hasStatus bool
	for _, c := range root.Commands() {
		if c.Name() == "inference" {
			inf = &cobraCommandShim{has: true}
			subs := map[string]bool{}
			for _, s := range c.Commands() {
				subs[s.Name()] = true
			}
			if !subs["run"] || !subs["validate"] {
				t.Errorf("inference noun must have run + validate subcommands, got %v", subs)
			}
		}
		if c.Name() == "status" {
			hasStatus = true
		}
	}
	if inf == nil || !inf.has {
		t.Fatal("inference noun not registered in root command tree")
	}
	// All Phase-3 lifecycle verbs have now landed: `install` (03-02),
	// `up`/`down`/`restart`/`logs`/`config` (03-03), and `status` (03-04). The
	// former forward-guard against an unregistered `status` is satisfied — assert it
	// is present (the last reserved verb to land, D-13).
	if !hasStatus {
		t.Errorf("Phase-3 `status` verb must be registered (Plan 03-04, D-13)")
	}
}

// cobraCommandShim is a tiny presence flag for the registration assertion.
type cobraCommandShim struct{ has bool }
