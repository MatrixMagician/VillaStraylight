package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/bench"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// bench_test.go drives the thin `villa bench` cobra caller through a stubbed bench.Deps:
// the Result→exit mapping (no-endpoint→exitBlocked, void-exhaustion→exitWarn, clean→
// exitPass), the --ab original-restore wiring (the cmd passes the correct orig target and
// --ab actually engages Switch/Restore; the core-level always-restore invariant is proven
// in 09-02), and a frozen --json golden carrying pp/tg as TWO SEPARATE keys per side.
// The reachability pre-check is the package-level benchEndpointReachable indirection,
// overridden here so no live host is touched. The *update flag is declared in
// detect_test.go (package-shared) — this file reuses it, does NOT re-declare it.

// benchTestCmd clones statusTestCmd: a throwaway cobra command whose Out/Err are
// buffers so a test asserts rendered output + the returned exit code without a
// subprocess.
func benchTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "bench"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// benchRecorder records the --ab Switch/Restore targets in call order so
// TestBenchABRestoresOriginal asserts the final flip restores the original backend.
type benchRecorder struct {
	callOrder []string // each "switch:<target>" / "restore:<target>"
}

// cannedTimings is the deterministic per-side timings the stubbed Measure returns so
// the --json golden is reproducible. ppA/tgA drive side A; ppB/tgB drive side B once a
// Switch has fired.
type cannedTimings struct {
	pp, tg float64
}

// newBenchStub builds a stubbed *bench.Deps driving runBench without a live host. When
// ab is false only Measure is set (single-backend path). When ab is true Switch/Restore/
// LoadConfig are wired too; Measure returns sideB timings after a Switch fired so the
// two --ab sides differ deterministically. resident gates whether a run counts; void
// makes the FIRST `voidCount` measured runs non-resident to exercise void-exhaustion.
func newBenchStub(rec *benchRecorder, ab bool, origBackend string, a, b cannedTimings, resident bool, voidCount int) *bench.Deps {
	switched := false
	measured := 0
	d := &bench.Deps{
		Measure: func(_ context.Context) (bench.RunTimings, bool, string, error) {
			measured++
			t := a
			if switched {
				t = b
			}
			isResident := resident
			if measured <= voidCount {
				isResident = false
			}
			return bench.RunTimings{
				PromptPerSec:    t.pp,
				PredictedPerSec: t.tg,
				PromptN:         16,
				PredictedN:      128,
			}, isResident, "", nil
		},
	}
	if !ab {
		return d
	}
	d.LoadConfig = func() (config.VillaConfig, error) {
		return config.VillaConfig{Model: "qwen3", Backend: origBackend}, nil
	}
	d.Switch = func(_ context.Context, target string) error {
		rec.callOrder = append(rec.callOrder, "switch:"+target)
		switched = true
		return nil
	}
	d.Restore = func(_ context.Context, original string) error {
		rec.callOrder = append(rec.callOrder, "restore:"+original)
		switched = false
		return nil
	}
	return d
}

// withReachable overrides the package-level reachability pre-check for one test and
// restores it on cleanup.
func withReachable(t *testing.T, reachable bool) {
	t.Helper()
	prev := benchEndpointReachable
	benchEndpointReachable = func() bool { return reachable }
	t.Cleanup(func() { benchEndpointReachable = prev })
}

// withConfiguredBackend overrides the package-level benchConfiguredBackend seam for one
// test (mirrors withReachable) so the single-mode label is exercised without a live host
// / the developer's real config, restoring it on cleanup.
func withConfiguredBackend(t *testing.T, backend string) {
	t.Helper()
	prev := benchConfiguredBackend
	benchConfiguredBackend = func() string { return backend }
	t.Cleanup(func() { benchConfiguredBackend = prev })
}

// benchSpec is the deterministic spec the cmd-layer tests run under.
func benchSpec(reps, warmup int) bench.BenchSpec {
	return bench.BenchSpec{
		Reps:        reps,
		Warmup:      warmup,
		Prompt:      benchPrompt,
		NPredict:    128,
		Seed:        benchSeed,
		Temp:        benchTemp,
		Timeout:     benchProveTimeout,
		MinResident: benchMinResident(reps),
	}
}

// TestBenchRegistered: the `bench` noun is wired into the command tree.
func TestBenchRegistered(t *testing.T) {
	root := newRoot()
	c, _, err := root.Find([]string{"bench"})
	if err != nil || c.Name() != "bench" {
		t.Fatalf("`bench` noun not registered: %v", err)
	}
}

// TestBenchFlagValidation proves the bounded-int flags are rejected at the cobra
// boundary (WR-04 / RESEARCH Security Domain V5): --reps/--n-predict < 1 and --warmup < 0
// return a clear usage error BEFORE any run (never a confusing void-exhaustion WARN or an
// out-of-contract negative max_tokens on the wire). The validation runs before runBench's
// os.Exit, so executing with bad flags returns the error in-process.
func TestBenchFlagValidation(t *testing.T) {
	cases := [][]string{
		{"--reps", "0"},
		{"--reps", "-5"},
		{"--n-predict", "0"},
		{"--n-predict", "-1"},
		{"--warmup", "-1"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newBench()
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("bad flags %v must return a validation error, got nil", args)
			}
			if !strings.Contains(err.Error(), "must be") {
				t.Errorf("validation error %q should explain the bound", err)
			}
		})
	}
}

// TestBenchABTargetPlumbed proves the --ab-target flag is plumbed into BenchSpec.ABTarget:
// `villa bench --ab --ab-target rocm-6.4.4` reaches runBench with spec.ABTarget ==
// "rocm-6.4.4"; omitting --ab-target leaves spec.ABTarget == "" (the other() default).
// It captures the spec via the benchRun seam so no os.Exit fires.
func TestBenchABTargetPlumbed(t *testing.T) {
	withReachable(t, true)
	cases := []struct {
		name   string
		args   []string
		wantAB string
	}{
		{"explicit-target", []string{"--ab", "--ab-target", "rocm-6.4.4"}, "rocm-6.4.4"},
		{"unset-target", []string{"--ab"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotSpec bench.BenchSpec
			prev := benchRun
			benchRun = func(_ *cobra.Command, spec bench.BenchSpec, _ bool, _ bool, _ *bench.Deps) int {
				gotSpec = spec
				return exitPass
			}
			t.Cleanup(func() { benchRun = prev })

			cmd := newBench()
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute %v: unexpected error %v", tc.args, err)
			}
			if gotSpec.ABTarget != tc.wantAB {
				t.Errorf("spec.ABTarget = %q, want %q", gotSpec.ABTarget, tc.wantAB)
			}
		})
	}
}

// TestBenchABTargetFailClosed proves an unknown --ab-target is rejected fail-closed
// (BackendFor validation) with an actionable error BEFORE any switch is attempted — the
// benchRun seam is NEVER reached on a bogus target (D-03, T-12-07: a typo is an error,
// never a silent flip).
func TestBenchABTargetFailClosed(t *testing.T) {
	withReachable(t, true)
	reached := false
	prev := benchRun
	benchRun = func(_ *cobra.Command, _ bench.BenchSpec, _ bool, _ bool, _ *bench.Deps) int {
		reached = true
		return exitPass
	}
	t.Cleanup(func() { benchRun = prev })

	cmd := newBench()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--ab", "--ab-target", "bogus"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("unknown --ab-target must return a fail-closed error, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("fail-closed error must name the bad target, got %q", err)
	}
	if reached {
		t.Error("benchRun must NOT be reached when --ab-target is invalid (no switch on a bad target)")
	}
}

// TestBenchABTargetRequiresAB proves --ab-target without --ab is a usage error (the
// explicit target is only meaningful for the --ab path): the benchRun seam is never
// reached and a clear error is returned.
func TestBenchABTargetRequiresAB(t *testing.T) {
	withReachable(t, true)
	reached := false
	prev := benchRun
	benchRun = func(_ *cobra.Command, _ bench.BenchSpec, _ bool, _ bool, _ *bench.Deps) int {
		reached = true
		return exitPass
	}
	t.Cleanup(func() { benchRun = prev })

	cmd := newBench()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--ab-target", "rocm-6.4.4"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("--ab-target without --ab must return a usage error, got nil")
	}
	if !strings.Contains(err.Error(), "--ab") {
		t.Errorf("usage error must mention --ab, got %q", err)
	}
	if reached {
		t.Error("benchRun must NOT be reached when --ab-target lacks --ab")
	}
}

// TestBenchNoEndpoint: with no reachable inference endpoint, bench refuses with
// remediation and exits exitBlocked, firing zero Measure runs.
func TestBenchNoEndpoint(t *testing.T) {
	withReachable(t, false)
	rec := &benchRecorder{}
	d := newBenchStub(rec, false, "vulkan", cannedTimings{100, 40}, cannedTimings{}, true, 0)
	cmd, _, errOut := benchTestCmd()

	code := runBench(cmd, benchSpec(5, 1), false, false, d)
	if code != exitBlocked {
		t.Fatalf("no-endpoint exit = %d, want %d (exitBlocked)", code, exitBlocked)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("no running inference endpoint")) {
		t.Errorf("no-endpoint must print remediation, got %q", errOut.String())
	}
}

// TestBenchVoidExhaustion: when too few runs are resident the Result is a void-
// exhaustion WARN → exitWarn, not a confident band.
func TestBenchVoidExhaustion(t *testing.T) {
	withReachable(t, true)
	rec := &benchRecorder{}
	// Every measured run non-resident → no kept runs → void-exhaustion.
	d := newBenchStub(rec, false, "vulkan", cannedTimings{100, 40}, cannedTimings{}, false, 0)
	cmd, _, errOut := benchTestCmd()

	code := runBench(cmd, benchSpec(5, 1), false, false, d)
	if code != exitWarn {
		t.Fatalf("void-exhaustion exit = %d, want %d (exitWarn)", code, exitWarn)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("insufficient residency-checked runs")) {
		t.Errorf("void-exhaustion must print the honest WARN, got %q", errOut.String())
	}
}

// TestBenchCleanPass: a fully-resident single-backend run renders the band and exits
// exitPass.
func TestBenchCleanPass(t *testing.T) {
	withReachable(t, true)
	rec := &benchRecorder{}
	d := newBenchStub(rec, false, "vulkan", cannedTimings{120.5, 42.25}, cannedTimings{}, true, 0)
	cmd, out, _ := benchTestCmd()

	code := runBench(cmd, benchSpec(3, 1), false, false, d)
	if code != exitPass {
		t.Fatalf("clean single-backend exit = %d, want %d (exitPass)", code, exitPass)
	}
	s := out.String()
	if !bytes.Contains(out.Bytes(), []byte("pp tok/s")) || !bytes.Contains(out.Bytes(), []byte("tg tok/s")) {
		t.Errorf("clean run must render separate pp and tg figures, got %q", s)
	}
}

// TestBenchSingleNamesBackend proves single-mode names the measured backend in BOTH the
// human header and the --json single.backend field (BENCH-01/02 UAT Test 1 gap): with the
// configured backend "vulkan", the rendered header reads `backend (vulkan):` (NOT the empty
// `backend ():` form) and decoded `single.backend == "vulkan"`. The fix is a cmd-layer
// wiring (runBench sets res.Backend from the benchConfiguredBackend seam in the !ab path);
// the pure internal/bench core stays config-unaware.
func TestBenchSingleNamesBackend(t *testing.T) {
	withReachable(t, true)
	withConfiguredBackend(t, "vulkan")

	// (1) human single mode — header names the backend, never the empty form.
	rec := &benchRecorder{}
	d := newBenchStub(rec, false, "vulkan", cannedTimings{120.5, 42.25}, cannedTimings{}, true, 0)
	cmd, out, _ := benchTestCmd()
	if code := runBench(cmd, benchSpec(3, 1), false, false, d); code != exitPass {
		t.Fatalf("single human exit = %d, want %d (exitPass)", code, exitPass)
	}
	if !bytes.Contains(out.Bytes(), []byte("backend (vulkan):")) {
		t.Errorf("single-mode header must name the configured backend `backend (vulkan):`, got %q", out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("backend ():")) {
		t.Errorf("single-mode header must NOT be the empty `backend ():` form, got %q", out.String())
	}

	// (2) --json single mode — single.backend equals the configured backend.
	rec2 := &benchRecorder{}
	d2 := newBenchStub(rec2, false, "vulkan", cannedTimings{120.5, 42.25}, cannedTimings{}, true, 0)
	cmd2, out2, _ := benchTestCmd()
	if code := runBench(cmd2, benchSpec(3, 1), false, true, d2); code != exitPass {
		t.Fatalf("single --json exit = %d, want %d (exitPass)", code, exitPass)
	}
	var decoded struct {
		Single struct {
			Backend string `json:"backend"`
		} `json:"single"`
	}
	if err := json.Unmarshal(out2.Bytes(), &decoded); err != nil {
		t.Fatalf("decode single --json: %v (out: %s)", err, out2.String())
	}
	if decoded.Single.Backend != "vulkan" {
		t.Errorf("single --json single.backend = %q, want %q (configured backend)", decoded.Single.Backend, "vulkan")
	}
}

// TestBenchABRestoresOriginal: an --ab invocation through the stubbed Deps ends with a
// Restore targeting the ORIGINAL backend, AND --ab actually engages Switch then Restore
// (the cmd wiring passes the correct orig target; the core-level always-restore is
// proven in 09-02).
func TestBenchABRestoresOriginal(t *testing.T) {
	withReachable(t, true)
	rec := &benchRecorder{}
	d := newBenchStub(rec, true, "vulkan", cannedTimings{120, 40}, cannedTimings{150, 55}, true, 0)
	cmd, _, _ := benchTestCmd()

	code := runBench(cmd, benchSpec(3, 1), true, false, d)
	if code != exitPass {
		t.Fatalf("clean --ab exit = %d, want %d (exitPass)", code, exitPass)
	}
	if len(rec.callOrder) == 0 {
		t.Fatalf("--ab must engage Switch/Restore, got zero recorded ops")
	}
	// A Switch must have fired (the flip path), and the LAST op must restore the original.
	sawSwitch := false
	for _, op := range rec.callOrder {
		if op == "switch:rocm" {
			sawSwitch = true
		}
	}
	if !sawSwitch {
		t.Errorf("--ab must Switch to the other backend, got callOrder=%v", rec.callOrder)
	}
	last := rec.callOrder[len(rec.callOrder)-1]
	if last != "restore:vulkan" {
		t.Errorf("--ab must END by restoring the ORIGINAL backend, got last op %q (callOrder=%v)", last, rec.callOrder)
	}
}

// TestBenchABFailedRestoreWarns proves a failed restore-to-original in the live --ab
// Restore closure is made LOUD (WR-01 / RESEARCH Pitfall 4): it prints a WARNING with
// recovery guidance to stderr and propagates the error, rather than silently leaving the
// user on the non-default backend. It drives the real liveBenchDeps Restore closure with
// a stubbed benchBackendSwap that fails, capturing os.Stderr.
func TestBenchABFailedRestoreWarns(t *testing.T) {
	prev := benchBackendSwap
	benchBackendSwap = func(target string) error {
		return fmt.Errorf("bring-up of %s failed", target)
	}
	t.Cleanup(func() { benchBackendSwap = prev })

	// Capture os.Stderr (the WARNING is written there, not the cobra Err buffer).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prevStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = prevStderr })

	d := liveBenchDeps(true, benchSpec(3, 1))
	restoreErr := d.Restore(context.Background(), "vulkan")

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	os.Stderr = prevStderr

	if restoreErr == nil {
		t.Error("failed restore must propagate an error, got nil")
	}
	got := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("failed to restore original backend")) {
		t.Errorf("failed restore must print a LOUD WARNING, got %q", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("villa backend set vulkan")) {
		t.Errorf("failed restore must print recovery guidance for the original backend, got %q", got)
	}
}

// TestBenchJSON freezes the --json contract byte-for-byte: pp and tg tok/s are TWO
// SEPARATE keys per side (prompt_per_sec / predicted_per_sec with their stddevs) plus
// the per-metric delta — never a blended key (the Phase-10 read contract). Run with
// -update to regenerate.
func TestBenchJSON(t *testing.T) {
	withReachable(t, true)
	rec := &benchRecorder{}
	// Deterministic --ab: side A vulkan, side B rocm, distinct pp/tg so the golden
	// exercises every separate key + the deltas. reps=2/warmup=1, both sides resident.
	d := newBenchStub(rec, true, "vulkan", cannedTimings{120, 40}, cannedTimings{150, 55}, true, 0)
	cmd, out, _ := benchTestCmd()

	code := runBench(cmd, benchSpec(2, 1), true, true, d)
	if code != exitPass {
		t.Fatalf("--json --ab exit = %d, want %d (out: %s)", code, exitPass, out.String())
	}

	golden := filepath.Join("testdata", "bench.json.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Errorf("bench --json does not match golden.\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
	}
}

// TestBenchJSONNoBlendedKey locks the milestone honesty invariant against future drift:
// the --json contract carries pp and tg as TWO SEPARATE keys (prompt_per_sec /
// predicted_per_sec) and NEVER a blended tok/s figure. Reads the frozen golden and asserts
// ZERO occurrences of any blended key. If this fails, someone re-introduced a blended
// throughput number — the exact dishonesty Phase 9 exists to prevent.
func TestBenchJSONNoBlendedKey(t *testing.T) {
	golden := filepath.Join("testdata", "bench.json.golden")
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	for _, blended := range [][]byte{[]byte("tok_per_sec"), []byte("tokens_per_sec")} {
		if bytes.Contains(data, blended) {
			t.Errorf("golden contains a blended tok/s key %q — pp and tg MUST stay SEPARATE "+
				"(prompt_per_sec / predicted_per_sec only)", blended)
		}
	}
}
