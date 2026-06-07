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
	"github.com/MatrixMagician/VillaStraylight/internal/benchstore"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// TestMain makes the whole cmd/villa test package hermetic against the BENCH-03
// persistence write-hook: it defaults the package-level benchstoreWrite indirection
// to a no-op for the test run so the existing runBench-driving tests (which do NOT
// stub the hook) never touch the developer's real $XDG_DATA_HOME/villa/bench-reports.jsonl.
// Tests that exercise the hook explicitly override benchstoreWrite with a recording or
// error-returning stub and restore it on cleanup. Per-test t.Setenv(...) still wins for
// the few tests that drive a real temp XDG dir.
func TestMain(m *testing.M) {
	prev := benchstoreWrite
	benchstoreWrite = func(_ benchstore.Deps, _ benchstore.SavedReport) error { return nil }
	code := m.Run()
	benchstoreWrite = prev
	os.Exit(code)
}

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

// ---------------------------------------------------------------------------
// BENCH-03 persistence: live benchstore append seam + cmd-tier fingerprint capture
// ---------------------------------------------------------------------------

// TestBenchstoreWriteAppendsGrowing proves the live append seam (liveBenchstoreDeps)
// writes one JSONL line per call to $XDG_DATA_HOME/villa/bench-reports.jsonl, the store
// grows append-only (a second call does NOT truncate the first), and the file mode is
// 0600 under a 0700 dir (T-14-01/T-14-02 owner-only).
func TestBenchstoreWriteAppendsGrowing(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	d := liveBenchstoreDeps()
	if d.AppendLine == nil {
		t.Fatal("liveBenchstoreDeps must wire AppendLine")
	}

	if err := d.AppendLine([]byte("{\"line\":1}\n")); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := d.AppendLine([]byte("{\"line\":2}\n")); err != nil {
		t.Fatalf("second append: %v", err)
	}

	store := filepath.Join(dataHome, "villa", "bench-reports.jsonl")
	got, err := os.ReadFile(store)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	want := "{\"line\":1}\n{\"line\":2}\n"
	if string(got) != want {
		t.Errorf("store contents = %q, want %q (append-only, no truncation)", string(got), want)
	}

	fi, err := os.Stat(store)
	if err != nil {
		t.Fatalf("stat store: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("store file mode = %o, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Join(dataHome, "villa"))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("store dir mode = %o, want 0700", di.Mode().Perm())
	}
}

// TestBenchstoreWriteReadAllRoundTrips proves the ReadAll seam returns the bytes the
// AppendLine seam wrote, and returns (nil,nil) — not an error — when no store exists yet
// (no reports ≠ failure; mirrors config.LoadVilla returning defaults when absent).
func TestBenchstoreWriteReadAllRoundTrips(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	d := liveBenchstoreDeps()
	if d.ReadAll == nil {
		t.Fatal("liveBenchstoreDeps must wire ReadAll")
	}

	// Absent store → (nil, nil).
	data, err := d.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll on absent store must not error, got %v", err)
	}
	if data != nil {
		t.Errorf("ReadAll on absent store = %q, want nil", string(data))
	}

	if err := d.AppendLine([]byte("{\"x\":1}\n")); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, err = d.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll after append: %v", err)
	}
	if string(data) != "{\"x\":1}\n" {
		t.Errorf("ReadAll = %q, want round-tripped line", string(data))
	}
}

// TestBenchstoreWriteTraversalRefused proves the append seam refuses to write outside
// the villa data dir (T-14-01): with XDG_DATA_HOME pointed at a temp dir, a store path
// is confined under <xdg>/villa, so no traversal escape can land a write elsewhere. We
// drive the guard by setting XDG_DATA_HOME to a path whose villa subdir cannot be the
// parent of an escaping target — exercised via Append on a crafted Deps is covered in the
// benchstore unit tests; here we assert the live seam's MkdirAll+guard at least confines
// the write to the resolved store (the file lands ONLY under <xdg>/villa).
func TestBenchstoreWriteConfinedToDataDir(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	d := liveBenchstoreDeps()
	if err := d.AppendLine([]byte("{\"y\":2}\n")); err != nil {
		t.Fatalf("append: %v", err)
	}
	// The ONLY file created must be under <xdg>/villa — nothing escapes to <xdg> root.
	if _, err := os.Stat(filepath.Join(dataHome, "bench-reports.jsonl")); err == nil {
		t.Error("write escaped to XDG root — must be confined under <xdg>/villa")
	}
	if _, err := os.Stat(filepath.Join(dataHome, "villa", "bench-reports.jsonl")); err != nil {
		t.Errorf("store not under <xdg>/villa: %v", err)
	}
}

// TestBenchAssertStoreUnderRoot proves the MEANINGFUL T-14-01 guard (WR-02): unlike the
// previous inert check (store against its own parent dir, which can never escape), the
// guard validates the resolved store against the TRUSTED data-home root — the actual
// untrusted vector being $XDG_DATA_HOME. A legit absolute root passes; an empty root, a
// NON-ABSOLUTE root, and a `..`-escaping store are all rejected (loud-but-non-fatal at
// the call site). This is the test that fails if the guard ever regresses to a no-op.
func TestBenchAssertStoreUnderRoot(t *testing.T) {
	root := t.TempDir() // absolute
	legit := filepath.Join(root, "villa", "bench-reports.jsonl")
	if err := benchAssertStoreUnderRoot(legit, root); err != nil {
		t.Errorf("legit store under absolute root rejected: %v", err)
	}

	// Empty root: rejected.
	if err := benchAssertStoreUnderRoot(legit, ""); err == nil {
		t.Error("empty data-home root must be rejected")
	}

	// Non-absolute root (a relative $XDG_DATA_HOME): rejected.
	if err := benchAssertStoreUnderRoot(filepath.Join("rel", "villa", "x.jsonl"), "rel"); err == nil {
		t.Error("non-absolute data-home root must be rejected")
	}

	// A store that escapes the root (a `store` NOT derived from `root`): rejected. This
	// is the raw traversal-escape branch — an absolute store path that resolves OUTSIDE
	// the resolved root.
	escape := filepath.Join(filepath.Dir(root), "evil", "villa", "x.jsonl")
	if err := benchAssertStoreUnderRoot(escape, root); err == nil {
		t.Errorf("store escaping the data-home root must be rejected: %q", escape)
	}
}

// TestBenchstoreWriteRejectsNonAbsoluteXDG proves the live append seam SKIPS the write
// (returns an error the caller surfaces as a non-fatal WARN) when $XDG_DATA_HOME is a
// NON-ABSOLUTE value — the actual untrusted-env vector. The spec requires an absolute
// XDG_DATA_HOME; a relative one would resolve the store against an unpredictable CWD, so
// the guard refuses rather than landing the store somewhere unintended. This is the
// end-to-end counterpart to TestBenchAssertStoreUnderRoot and proves the guard is no
// longer a no-op (it previously checked the store against its own parent dir and could
// never reject anything).
func TestBenchstoreWriteRejectsNonAbsoluteXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative-data-home")

	d := liveBenchstoreDeps()
	if err := d.AppendLine([]byte("{\"y\":2}\n")); err == nil {
		t.Error("append must be refused for a non-absolute XDG_DATA_HOME (traversal guard)")
	}
}

// TestBenchFingerprintHonorsKnownGuard proves the comparability fingerprint is captured
// at the cmd tier from config + .Known-guarded detect.Probe(): config-sourced
// model/quant/ctx are carried verbatim, the benched backend is recorded, and the host
// gfx id / kernel are ONLY populated when detect's typed-Optional .Known is true — an
// UNKNOWN host fact serializes to the empty sentinel, NEVER a fabricated value (T-14-04).
// This is hardware-agnostic: on gfx1151 the probe is Known (value carried); off-hardware
// it is Unknown (""). Either way the captured field must EXACTLY match the .Known guard.
func TestBenchFingerprintHonorsKnownGuard(t *testing.T) {
	// Drive config deterministically via a temp XDG_CONFIG_HOME so the test does not read
	// the developer's real config.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := config.VillaConfig{Model: "qwen3", Quant: "UD-Q4_K_M", Ctx: 8192, Backend: "vulkan"}
	fp := captureBenchFingerprint(cfg, "vulkan")

	if fp.Model != "qwen3" || fp.Quant != "UD-Q4_K_M" || fp.Ctx != 8192 {
		t.Errorf("config fields not carried verbatim: %+v", fp)
	}
	if fp.Backend != "vulkan" {
		t.Errorf("Backend = %q, want vulkan (recorded as the benched backend)", fp.Backend)
	}

	// The captured host facts must EXACTLY mirror the .Known guard over the same probe:
	// Known → the value; Unknown → "" (never fabricated). Probe() is the same source the
	// capture reads, so re-reading it gives the ground truth to assert against.
	hp := detect.Probe()
	wantGfx := ""
	if hp.IGPUGfxID.Known {
		wantGfx = hp.IGPUGfxID.Value
	}
	if fp.HostGfxID != wantGfx {
		t.Errorf("HostGfxID = %q, want %q (.Known guard must be honored — no fabricated gfx id)", fp.HostGfxID, wantGfx)
	}
	// When the gfx id is UNKNOWN, the empty sentinel makes the pair not-comparable (the
	// no-false-equal posture) — assert the sentinel rather than a fabricated identity.
	if !hp.IGPUGfxID.Known && fp.HostGfxID != "" {
		t.Errorf("UNKNOWN gfx id must serialize to \"\", got %q (no fabricated host identity)", fp.HostGfxID)
	}

	wantKver := ""
	if hp.KernelVersion.Known {
		wantKver = hp.KernelVersion.Value
	}
	if fp.KernelVersion != wantKver {
		t.Errorf("KernelVersion = %q, want %q (.Known guard must be honored)", fp.KernelVersion, wantKver)
	}
}

// ---------------------------------------------------------------------------
// BENCH-03 persistence write-hook: runBench fires benchstoreWrite after success.
// ---------------------------------------------------------------------------

// withBenchstoreWrite overrides the package-level benchstoreWrite hook for one test,
// capturing every persisted SavedReport (and optionally returning err), restoring the
// prior hook on cleanup. It drives the write-hook WITHOUT touching XDG or a live host.
func withBenchstoreWrite(t *testing.T, err error) *[]benchstore.SavedReport {
	t.Helper()
	var captured []benchstore.SavedReport
	prev := benchstoreWrite
	benchstoreWrite = func(_ benchstore.Deps, r benchstore.SavedReport) error {
		captured = append(captured, r)
		return err
	}
	t.Cleanup(func() { benchstoreWrite = prev })
	return &captured
}

// TestBenchWriteHookFiresSingle proves a successful single-mode runBench fires the
// benchstore write EXACTLY ONCE with mode=="single", pp/tg carried separately from the
// Result's Stats, and the captured fingerprint populated (BENCH-03 persist-on-success).
func TestBenchWriteHookFiresSingle(t *testing.T) {
	withReachable(t, true)
	withConfiguredBackend(t, "vulkan")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	got := withBenchstoreWrite(t, nil)

	rec := &benchRecorder{}
	d := newBenchStub(rec, false, "vulkan", cannedTimings{120.5, 42.25}, cannedTimings{}, true, 0)
	cmd, _, _ := benchTestCmd()

	if code := runBench(cmd, benchSpec(3, 1), false, false, d); code != exitPass {
		t.Fatalf("clean single exit = %d, want %d (exitPass)", code, exitPass)
	}
	if len(*got) != 1 {
		t.Fatalf("write-hook must fire exactly once, fired %d", len(*got))
	}
	r := (*got)[0]
	if r.Mode != "single" {
		t.Errorf("Mode = %q, want single", r.Mode)
	}
	if r.Single == nil {
		t.Fatal("single report must carry Single side")
	}
	if r.Single.PromptPerSec != 120.5 || r.Single.PredictedPerSec != 42.25 {
		t.Errorf("pp/tg not carried separately from Stats: %+v", r.Single)
	}
	if r.AB != nil {
		t.Error("single report must not carry an AB block")
	}
	if r.Fingerprint.Model == "" && r.Fingerprint.Backend == "" {
		t.Error("fingerprint must be populated (model/backend captured at cmd tier)")
	}
	if r.Fingerprint.Backend != "vulkan" {
		t.Errorf("fingerprint backend = %q, want vulkan (the benched backend)", r.Fingerprint.Backend)
	}
	if r.VoidExhausted {
		t.Error("a clean run must record VoidExhausted=false")
	}
}

// TestBenchPersistABOneRecord proves an --ab runBench persists ONE record with mode=="ab"
// and the AB block (From/To + per-metric deltas), NOT two records.
func TestBenchPersistABOneRecord(t *testing.T) {
	withReachable(t, true)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	got := withBenchstoreWrite(t, nil)

	rec := &benchRecorder{}
	d := newBenchStub(rec, true, "vulkan", cannedTimings{100, 40}, cannedTimings{150, 60}, true, 0)
	cmd, _, _ := benchTestCmd()

	if code := runBench(cmd, benchSpec(3, 1), true, false, d); code != exitPass {
		t.Fatalf("clean ab exit = %d, want %d (exitPass)", code, exitPass)
	}
	if len(*got) != 1 {
		t.Fatalf("--ab must persist ONE record, persisted %d", len(*got))
	}
	r := (*got)[0]
	if r.Mode != "ab" {
		t.Errorf("Mode = %q, want ab", r.Mode)
	}
	if r.AB == nil {
		t.Fatal("ab report must carry an AB block")
	}
	if r.Single != nil {
		t.Error("ab report must not carry a Single side")
	}
	if r.AB.From == "" || r.AB.To == "" {
		t.Errorf("AB block must name From/To, got %+v", r.AB)
	}
	// per-metric deltas (B − A) carried separately.
	if r.AB.DeltaPromptPerSec == 0 && r.AB.DeltaPredictedPerSec == 0 {
		t.Error("AB block must carry per-metric deltas")
	}
}

// TestBenchWriteNonFatal proves a benchstore write error is LOUD-but-NON-FATAL: runBench
// returns the SAME exit code it would without persistence (exitPass for a clean run) and
// writes a WARN to stderr — the measurement exit code is unchanged (T-14-05 availability).
func TestBenchWriteNonFatal(t *testing.T) {
	withReachable(t, true)
	withConfiguredBackend(t, "vulkan")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	got := withBenchstoreWrite(t, fmt.Errorf("disk full"))

	rec := &benchRecorder{}
	d := newBenchStub(rec, false, "vulkan", cannedTimings{120.5, 42.25}, cannedTimings{}, true, 0)
	cmd, _, errOut := benchTestCmd()

	code := runBench(cmd, benchSpec(3, 1), false, false, d)
	if code != exitPass {
		t.Fatalf("write error must NOT change the exit code: got %d, want %d (exitPass)", code, exitPass)
	}
	if len(*got) != 1 {
		t.Fatalf("the hook must still fire on a clean run, fired %d", len(*got))
	}
	if !bytes.Contains(errOut.Bytes(), []byte("failed to persist")) {
		t.Errorf("a write error must print a loud stderr WARN, got %q", errOut.String())
	}
}

// TestBenchPersistSkipsOnConfigLoadError proves persistBenchReport (WR-04) does NOT
// swallow a config.LoadVilla error: on a malformed config it SKIPS persistence — the
// benchstore write-hook NEVER fires — with a loud-but-non-fatal WARN, rather than durably
// persisting a zeroed (Model=""/Quant=""/Ctx=0) fingerprint that could later auto-select
// and compare against another empty-fingerprint record. The exit code is unchanged.
func TestBenchPersistSkipsOnConfigLoadError(t *testing.T) {
	withReachable(t, true)
	withConfiguredBackend(t, "vulkan")
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// Point XDG_CONFIG_HOME at a temp dir holding a MALFORMED config.toml so
	// config.LoadVilla returns a parse error (not the absent-file default path).
	cfgHome := t.TempDir()
	villaDir := filepath.Join(cfgHome, "villa")
	if err := os.MkdirAll(villaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(villaDir, "config.toml"), []byte("this is = not [valid toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	got := withBenchstoreWrite(t, nil)

	rec := &benchRecorder{}
	d := newBenchStub(rec, false, "vulkan", cannedTimings{120.5, 42.25}, cannedTimings{}, true, 0)
	cmd, _, errOut := benchTestCmd()

	code := runBench(cmd, benchSpec(3, 1), false, false, d)
	if code != exitPass {
		t.Fatalf("a config-load error must NOT change the exit code: got %d, want %d (exitPass)", code, exitPass)
	}
	if len(*got) != 0 {
		t.Fatalf("persistence must be SKIPPED on a config-load error (no zeroed fingerprint), but the hook fired %d time(s)", len(*got))
	}
	if !bytes.Contains(errOut.Bytes(), []byte("skipping saved report")) {
		t.Errorf("a config-load error must print a loud skip WARN, got %q", errOut.String())
	}
}

// ---------------------------------------------------------------------------
// BENCH-04 read-only compare/list surface (--compare / --list).
// ---------------------------------------------------------------------------

// stubBenchstoreReadAll builds a benchstore.Deps whose ReadAll returns the marshaled
// JSONL of the given saved reports (one compact line each) and whose AppendLine/Now
// panic if ever called — runBenchCompare is READ-ONLY, so a write would be a bug.
func stubBenchstoreReadAll(t *testing.T, reports ...benchstore.SavedReport) benchstore.Deps {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range reports {
		line, err := benchstore.Marshal(r)
		if err != nil {
			t.Fatalf("marshal stub report: %v", err)
		}
		buf.Write(line)
	}
	data := buf.Bytes()
	return benchstore.Deps{
		ReadAll: func() ([]byte, error) {
			if len(data) == 0 {
				return nil, nil
			}
			return data, nil
		},
		AppendLine: func([]byte) error {
			t.Fatal("runBenchCompare must be READ-ONLY — AppendLine must never be called")
			return nil
		},
	}
}

// comparableReport builds a deterministic single-mode SavedReport over the same
// model/quant/ctx/host (the comparability key) on the given backend with the given
// pp/tg, captured at the given time. void marks the residency band not-authoritative.
func comparableReport(backend, capturedAt string, pp, tg float64, void bool) benchstore.SavedReport {
	return benchstore.SavedReport{
		CapturedAt: capturedAt,
		Mode:       "single",
		Spec:       benchstore.SavedSpec{Prompt: benchPrompt, Reps: 5, Warmup: 1, NPredict: 128, Seed: 42},
		Single: &benchstore.SavedSide{
			Backend:         backend,
			PromptPerSec:    pp,
			PredictedPerSec: tg,
			Kept:            5,
		},
		VoidExhausted: void,
		Fingerprint: benchstore.Fingerprint{
			Model: "qwen3", Quant: "UD-Q4_K_M", Ctx: 8192, Backend: backend, HostGfxID: "gfx1151",
		},
		SchemaVersion: 1,
	}
}

// TestBenchCompareFlagExclusive proves the read-only --compare/--list flags reject
// combination with the live-measurement flags (--ab/--ab-target/changed --reps/--warmup)
// AND each other, at the cobra boundary — runBenchCompare is never reached on a bad combo.
func TestBenchCompareFlagExclusive(t *testing.T) {
	cases := [][]string{
		{"--compare", "--ab"},
		{"--list", "--ab"},
		{"--compare", "--ab-target", "rocm-6.4.4"},
		{"--compare", "--reps", "3"},
		{"--list", "--warmup", "2"},
		{"--compare", "--list"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newBench()
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(args)
			if err := cmd.Execute(); err == nil {
				t.Fatalf("read-only flags %v combined with a live flag must error, got nil", args)
			}
		})
	}
}

// TestBenchCompareList proves --list over a stub store of 2 saved reports prints both
// (index, captured_at, model/quant/backend, pp/tg, void) and exits 0; an empty store
// refuses with remediation and exits 1.
func TestBenchCompareList(t *testing.T) {
	a := comparableReport("vulkan", "2026-06-07T10:00:00Z", 120, 40, false)
	b := comparableReport("rocm-7.2.4", "2026-06-07T11:00:00Z", 150, 55, false)

	cmd, out, _ := benchTestCmd()
	code := runBenchCompare(cmd, true, false, false, stubBenchstoreReadAll(t, a, b))
	if code != exitPass {
		t.Fatalf("--list exit = %d, want %d (exitPass)", code, exitPass)
	}
	s := out.String()
	for _, want := range []string{"vulkan", "rocm-7.2.4", "qwen3", "120", "150"} {
		if !strings.Contains(s, want) {
			t.Errorf("--list output missing %q\n%s", want, s)
		}
	}

	cmd2, _, errOut := benchTestCmd()
	if code := runBenchCompare(cmd2, true, false, false, stubBenchstoreReadAll(t)); code != exitBlocked {
		t.Fatalf("--list empty store exit = %d, want %d (exitBlocked)", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "villa bench") {
		t.Errorf("empty --list must print remediation, got %q", errOut.String())
	}
}

// TestBenchCompareComparable proves --compare over 2 comparable reports (same
// model/quant/host, differing backend) prints Δpp and Δtg on SEPARATE lines, exit 0.
func TestBenchCompareComparable(t *testing.T) {
	a := comparableReport("vulkan", "2026-06-07T10:00:00Z", 120, 40, false)
	b := comparableReport("rocm-7.2.4", "2026-06-07T11:00:00Z", 150, 55, false)

	cmd, out, _ := benchTestCmd()
	code := runBenchCompare(cmd, false, true, false, stubBenchstoreReadAll(t, a, b))
	if code != exitPass {
		t.Fatalf("--compare comparable exit = %d, want %d (exitPass)", code, exitPass)
	}
	s := out.String()
	// pp/tg deltas on SEPARATE lines.
	ppIdx := strings.Index(s, "Δpp")
	tgIdx := strings.Index(s, "Δtg")
	if ppIdx < 0 || tgIdx < 0 {
		t.Fatalf("--compare must render Δpp and Δtg lines, got %q", s)
	}
	between := s[min(ppIdx, tgIdx):max(ppIdx, tgIdx)]
	if !strings.Contains(between, "\n") {
		t.Errorf("Δpp and Δtg must be on SEPARATE lines (no newline between), got %q", s)
	}
}

// TestBenchCompareVoidSide proves a comparable pair with exactly one VoidExhausted side
// STILL prints Δpp/Δtg AND flags the void side as not-authoritative, exiting 0 (RESEARCH
// Q3/A5: the void flag is advisory, not a refusal).
func TestBenchCompareVoidSide(t *testing.T) {
	a := comparableReport("vulkan", "2026-06-07T10:00:00Z", 120, 40, false)
	b := comparableReport("rocm-7.2.4", "2026-06-07T11:00:00Z", 150, 55, true) // void side

	cmd, out, _ := benchTestCmd()
	code := runBenchCompare(cmd, false, true, false, stubBenchstoreReadAll(t, a, b))
	if code != exitPass {
		t.Fatalf("--compare void-side exit = %d, want %d (exitPass — void is advisory)", code, exitPass)
	}
	s := out.String()
	if !strings.Contains(s, "Δpp") || !strings.Contains(s, "Δtg") {
		t.Errorf("a comparable pair must STILL print deltas even with a void side, got %q", s)
	}
	if !strings.Contains(strings.ToLower(s), "not authoritative") {
		t.Errorf("the void side must be flagged 'not authoritative', got %q", s)
	}
}

// TestBenchCompareNotComparable proves --compare over a mismatched-model pair prints
// "not comparable" + the differing field(s) and NO numeric delta, exiting 2.
func TestBenchCompareNotComparable(t *testing.T) {
	a := comparableReport("vulkan", "2026-06-07T10:00:00Z", 120, 40, false)
	b := comparableReport("rocm-7.2.4", "2026-06-07T11:00:00Z", 150, 55, false)
	b.Fingerprint.Model = "llama3" // mismatched model → not comparable

	cmd, out, _ := benchTestCmd()
	code := runBenchCompare(cmd, false, true, false, stubBenchstoreReadAll(t, a, b))
	if code != exitWarn {
		t.Fatalf("--compare not-comparable exit = %d, want %d (exitWarn)", code, exitWarn)
	}
	s := out.String()
	if !strings.Contains(strings.ToLower(s), "not comparable") {
		t.Errorf("must print 'not comparable', got %q", s)
	}
	if !strings.Contains(s, "model") {
		t.Errorf("must name the differing field (model), got %q", s)
	}
	if strings.Contains(s, "Δpp") || strings.Contains(s, "Δtg") {
		t.Errorf("a not-comparable refusal must print NO delta, got %q", s)
	}
}

// TestBenchCompareNoReports proves --compare over an empty or single-report store refuses
// with remediation and exits 1 (insufficient reports).
func TestBenchCompareNoReports(t *testing.T) {
	cmd, _, errOut := benchTestCmd()
	if code := runBenchCompare(cmd, false, true, false, stubBenchstoreReadAll(t)); code != exitBlocked {
		t.Fatalf("--compare empty store exit = %d, want %d (exitBlocked)", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "villa bench") {
		t.Errorf("empty --compare must print remediation, got %q", errOut.String())
	}

	a := comparableReport("vulkan", "2026-06-07T10:00:00Z", 120, 40, false)
	cmd2, _, errOut2 := benchTestCmd()
	if code := runBenchCompare(cmd2, false, true, false, stubBenchstoreReadAll(t, a)); code != exitBlocked {
		t.Fatalf("--compare single-report store exit = %d, want %d (exitBlocked)", code, exitBlocked)
	}
	if !strings.Contains(errOut2.String(), "villa bench") {
		t.Errorf("single-report --compare must print remediation, got %q", errOut2.String())
	}
}

// TestBenchCompareReadOnly proves runBenchCompare never touches the measurement path or
// the backend swap: the stub Deps.AppendLine fatals if called, and benchBackendSwap is
// spied to prove it is never invoked.
func TestBenchCompareReadOnly(t *testing.T) {
	prevSwap := benchBackendSwap
	swapCalled := false
	benchBackendSwap = func(target string) error { swapCalled = true; return nil }
	t.Cleanup(func() { benchBackendSwap = prevSwap })

	a := comparableReport("vulkan", "2026-06-07T10:00:00Z", 120, 40, false)
	b := comparableReport("rocm-7.2.4", "2026-06-07T11:00:00Z", 150, 55, false)

	cmd, _, _ := benchTestCmd()
	_ = runBenchCompare(cmd, false, true, false, stubBenchstoreReadAll(t, a, b))
	if swapCalled {
		t.Error("runBenchCompare must NOT touch the backend swap (read-only)")
	}
}

// TestBenchCompareGolden freezes the --compare --json contract byte-for-byte: a
// comparable pair (with EXACTLY ONE void side so the a_void_exhausted/b_void_exhausted
// flags are frozen with a true value present) AND a not-comparable refusal, concatenated
// into one golden. pp/tg deltas are SEPARATE keys; the not-comparable case carries
// differing_fields and zeroed deltas. Run with -update to regenerate.
func TestBenchCompareGolden(t *testing.T) {
	// Comparable pair, side B void (RESEARCH Q3/A5 — flag, never suppress the delta).
	ca := comparableReport("vulkan", "2026-06-07T10:00:00Z", 120, 40, false)
	cb := comparableReport("rocm-7.2.4", "2026-06-07T11:00:00Z", 150, 55, true)

	// Not-comparable pair (mismatched model).
	na := comparableReport("vulkan", "2026-06-07T12:00:00Z", 120, 40, false)
	nb := comparableReport("rocm-7.2.4", "2026-06-07T13:00:00Z", 150, 55, false)
	nb.Fingerprint.Model = "llama3"

	var buf bytes.Buffer

	cmd1, out1, _ := benchTestCmd()
	if code := runBenchCompare(cmd1, false, true, true, stubBenchstoreReadAll(t, ca, cb)); code != exitPass {
		t.Fatalf("comparable --json exit = %d, want %d", code, exitPass)
	}
	buf.Write(out1.Bytes())

	cmd2, out2, _ := benchTestCmd()
	if code := runBenchCompare(cmd2, false, true, true, stubBenchstoreReadAll(t, na, nb)); code != exitWarn {
		t.Fatalf("not-comparable --json exit = %d, want %d", code, exitWarn)
	}
	buf.Write(out2.Bytes())

	golden := filepath.Join("testdata", "bench-compare.json.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("bench --compare --json does not match golden.\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

// TestBenchCompareNoBlendedKey locks the milestone honesty invariant over the compare
// golden: NO blended tok/s key (tok_per_sec / tokens_per_sec / throughput), and the
// per-metric pp/tg deltas ARE present as SEPARATE keys (delta_prompt_per_sec /
// delta_predicted_per_sec), plus the void-side flags (a_void_exhausted/b_void_exhausted)
// are exercised (RESEARCH Q3/A5).
func TestBenchCompareNoBlendedKey(t *testing.T) {
	golden := filepath.Join("testdata", "bench-compare.json.golden")
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	for _, blended := range [][]byte{[]byte("tok_per_sec"), []byte("tokens_per_sec"), []byte("throughput")} {
		if bytes.Contains(data, blended) {
			t.Errorf("compare golden contains a blended tok/s key %q — pp and tg MUST stay SEPARATE", blended)
		}
	}
	for _, want := range []string{"delta_prompt_per_sec", "delta_predicted_per_sec", "a_void_exhausted", "b_void_exhausted", "differing_fields"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("compare golden must contain %q (pp/tg deltas separate + void-side flags + refusal fields)", want)
		}
	}
}

// TestBenchVoidPersist proves a void-exhausted run is STILL persisted (persist-always
// policy A5): the hook fires on the exitWarn path too, recording VoidExhausted=true.
func TestBenchVoidPersist(t *testing.T) {
	withReachable(t, true)
	withConfiguredBackend(t, "vulkan")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	got := withBenchstoreWrite(t, nil)

	rec := &benchRecorder{}
	// Every measured run non-resident → void-exhaustion → exitWarn.
	d := newBenchStub(rec, false, "vulkan", cannedTimings{100, 40}, cannedTimings{}, false, 0)
	cmd, _, _ := benchTestCmd()

	if code := runBench(cmd, benchSpec(5, 1), false, false, d); code != exitWarn {
		t.Fatalf("void-exhaustion exit = %d, want %d (exitWarn)", code, exitWarn)
	}
	if len(*got) != 1 {
		t.Fatalf("a void-exhausted run must STILL be persisted (A5), persisted %d", len(*got))
	}
	if !(*got)[0].VoidExhausted {
		t.Error("a void-exhausted run must record VoidExhausted=true")
	}
}
