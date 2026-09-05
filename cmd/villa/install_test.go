package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install_test.go covers the command tier's ADAPTERS around install.Run: the
// readiness poll, the proof evaluators, the live probes and the binary-path
// resolver. The flow itself is tested at its interface in
// internal/install/flow_test.go.

func seloffCheck() preflight.CheckResult {
	return preflight.CheckResult{
		ID: "PRE-05", Name: "SELinux container_use_devices boolean", Tier: preflight.TierBlock,
		Status: preflight.StatusWarn, Detail: "container_use_devices is OFF",
		Remediation: "run `setsebool -P container_use_devices=true`.",
	}
}

// TestResolveDashboardBinaryPathIsAbsolute: the live resolver returns the running
// binary's absolute path, and that path exists with an executable bit — what the
// dashboard unit's ExecStart needs at boot. It never falls back to a fixed path.
func TestResolveDashboardBinaryPathIsAbsolute(t *testing.T) {
	p, err := resolveDashboardBinaryPath()
	if err != nil {
		t.Fatalf("resolveDashboardBinaryPath: %v", err)
	}
	if !filepath.IsAbs(p) {
		t.Fatalf("resolved path %q is not absolute", p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("resolved binary path %q does not exist on disk: %v", p, err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("resolved binary path %q is not executable (mode %v)", p, fi.Mode())
	}
}

// TestReadiness503ThenReady: a probe returning 503 then 200 resolves to ready
// (PASS) — 503 is keep-polling, NOT a confident down (Pitfall 2).
func TestReadiness503ThenReady(t *testing.T) {
	calls := 0
	probe := func() (int, error) {
		calls++
		if calls < 2 {
			return http.StatusServiceUnavailable, nil
		}
		return http.StatusOK, nil
	}
	r := pollReadiness(t.Context(), probe, time.Second, time.Millisecond)
	if r.Status != preflight.StatusPass {
		t.Fatalf("503-then-200 status = %v, want PASS (detail=%q)", r.Status, r.Detail)
	}
	if calls < 2 {
		t.Errorf("poll should have retried past the 503, calls=%d", calls)
	}
}

// TestReadinessTimeoutWarns: a probe that never returns 200 yields a WARN (typed-
// Unknown) at the deadline, never a confident FAIL.
func TestReadinessTimeoutWarns(t *testing.T) {
	probe := func() (int, error) { return http.StatusServiceUnavailable, nil }
	r := pollReadiness(t.Context(), probe, 5*time.Millisecond, time.Millisecond)
	if r.Status != preflight.StatusWarn {
		t.Fatalf("timeout status = %v, want WARN (not FAIL)", r.Status)
	}
}

// TestReadinessTransportErrorWarns: a transport error is keep-polling (server may
// still be coming up), resolving to WARN at the deadline.
func TestReadinessTransportErrorWarns(t *testing.T) {
	probe := func() (int, error) { return 0, errors.New("connection refused") }
	r := pollReadiness(t.Context(), probe, 5*time.Millisecond, time.Millisecond)
	if r.Status != preflight.StatusWarn {
		t.Fatalf("transport-error status = %v, want WARN", r.Status)
	}
}

// TestReadinessCancelledContextAbortsBeforeProbe: a context cancelled before the
// loop starts is observed before any probe runs, returning a WARN immediately
// (the deadline/cancellation is checked before each probe, not only after).
func TestReadinessCancelledContextAbortsBeforeProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	probed := false
	probe := func() (int, error) {
		probed = true
		return http.StatusOK, nil
	}
	r := pollReadiness(ctx, probe, time.Minute, time.Millisecond)
	if r.Status != preflight.StatusWarn {
		t.Fatalf("cancelled-ctx status = %v, want WARN", r.Status)
	}
	if probed {
		t.Errorf("a pre-cancelled context must abort before any probe runs")
	}
}

// TestInstallRegistered: the `install` verb is wired into the command tree.
func TestInstallRegistered(t *testing.T) {
	root := newRoot()
	install, _, err := root.Find([]string{"install"})
	if err != nil || install.Name() != "install" {
		t.Fatalf("`install` verb not registered: %v", err)
	}
}

// TestEmbedGGUFFilenameSingleSource asserts the pre-stage Shard filename equals the
// orchestrate single-source accessor UNCONDITIONALLY (Pitfall 3) — the served `-m`
// path and the staged file can never drift.
func TestEmbedGGUFFilenameSingleSource(t *testing.T) {
	if install.NomicEmbedShard.Filename != orchestrate.EmbedGGUFFilename() {
		t.Fatalf("embed GGUF filename drift: NomicEmbedShard.Filename = %q, orchestrate.EmbedGGUFFilename() = %q",
			install.NomicEmbedShard.Filename, orchestrate.EmbedGGUFFilename())
	}
}

// TestNomicShardValues pins the verified integrity values: a typo in the size or
// SHA256 would let an unverified GGUF through, so they are asserted here.
func TestNomicShardValues(t *testing.T) {
	if install.NomicEmbedShard.SizeBytes != 146146432 {
		t.Errorf("NomicEmbedShard.SizeBytes = %d, want 146146432", install.NomicEmbedShard.SizeBytes)
	}
	if install.NomicEmbedShard.SHA256 != "3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7" {
		t.Errorf("NomicEmbedShard.SHA256 = %q, want 3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7", install.NomicEmbedShard.SHA256)
	}
}

// TestLiveEmbedModelPresentSizeGuard: the embed-model presence check treats the
// GGUF as present only when its on-disk size matches NomicEmbedShard.SizeBytes — a
// truncated/tampered file is NOT trusted (returns false → re-pull + re-verify).
func TestLiveEmbedModelPresentSizeGuard(t *testing.T) {
	t.Run("absent file is not present", func(t *testing.T) {
		dir := t.TempDir()
		if liveEmbedModelPresent(dir) {
			t.Error("an absent embed GGUF must not be reported present")
		}
	})

	t.Run("truncated file is not present (integrity guard)", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(embedModelPath(dir), []byte("not the real weight"), 0o600); err != nil {
			t.Fatalf("seed truncated file: %v", err)
		}
		if liveEmbedModelPresent(dir) {
			t.Error("a truncated/tampered embed GGUF must be treated as NOT present so it is re-pulled (IN-03)")
		}
	})

	t.Run("correctly-sized file is present", func(t *testing.T) {
		dir := t.TempDir()
		f, err := os.Create(embedModelPath(dir))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := f.Truncate(int64(install.NomicEmbedShard.SizeBytes)); err != nil {
			t.Fatalf("truncate to expected size: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if !liveEmbedModelPresent(dir) {
			t.Error("a correctly-sized embed GGUF must be reported present (no needless re-pull)")
		}
	})
}

// TestEvalMemoryProof table-drives the PURE proof core over the four outcomes.
func TestEvalMemoryProof(t *testing.T) {
	const wantDim = 768
	cases := []struct {
		name       string
		embedDim   int
		embedErr   error
		writable   bool
		qdrantErr  error
		wantStatus preflight.Status
	}{
		{"embed ok + qdrant writable", wantDim, nil, true, nil, preflight.StatusPass},
		{"wrong dim", 256, nil, true, nil, preflight.StatusFail},
		{"embed err", 0, errors.New("connection refused"), true, nil, preflight.StatusFail},
		{"qdrant not writable", wantDim, nil, false, nil, preflight.StatusFail},
		{"qdrant err", wantDim, nil, false, errors.New("readyz 503"), preflight.StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			embedProbe := func() (int, error) { return tc.embedDim, tc.embedErr }
			qdrantProbe := func() (bool, error) { return tc.writable, tc.qdrantErr }
			got := evalMemoryProof(t.Context(), embedProbe, qdrantProbe, wantDim)
			if got.status != tc.wantStatus {
				t.Errorf("status = %v, want %v (detail %q)", got.status, tc.wantStatus, got.detail)
			}
			if tc.wantStatus == preflight.StatusFail && got.detail == "" {
				t.Errorf("a FAIL verdict must carry a remediation detail")
			}
		})
	}
}

// TestQdrantWritableProbeIdempotent: a pre-existing villa-probe collection from an
// interrupted prior run must NOT make the writable proof FAIL. The probe issues a
// best-effort DELETE before the PUT-create, so a fake curl runner that fails a PUT
// against an existing collection (unless a DELETE preceded it) must yield
// writable=true. It also locks the clean-store case and the readyz failure.
func TestQdrantWritableProbeIdempotent(t *testing.T) {
	const base = "http://villa-qdrant:6333"
	coll := base + "/collections/" + villaProbeCollection

	t.Run("stale leftover collection does not cause a FAIL", func(t *testing.T) {
		exists := true
		var calls []string
		curl := func(args ...string) ([]byte, error) {
			method, path := "GET", args[len(args)-1]
			for i, a := range args {
				if a == "-X" && i+1 < len(args) {
					method = args[i+1]
				}
			}
			for i, a := range args {
				if a == "-X" && i+2 < len(args) {
					path = args[i+2]
				}
			}
			calls = append(calls, method+" "+path)
			switch {
			case method == "GET" && path == base+"/readyz":
				return []byte("ok"), nil
			case method == "DELETE" && path == coll:
				exists = false
				return []byte("{}"), nil
			case method == "PUT" && path == coll:
				if exists {
					return nil, errors.New("409 Conflict: collection already exists")
				}
				return []byte("{}"), nil
			default:
				return []byte("{}"), nil
			}
		}

		writable, err := qdrantWritableProbe(curl, base, 768)
		if err != nil {
			t.Fatalf("a leftover probe collection must not FAIL the writable proof, got err: %v\ncalls: %v", err, calls)
		}
		if !writable {
			t.Fatalf("writable = false, want true (the create succeeded after the pre-DELETE)\ncalls: %v", calls)
		}
		delIdx, putIdx := indexOf(calls, "DELETE "+coll), indexOf(calls, "PUT "+coll)
		if delIdx < 0 || putIdx < 0 || delIdx >= putIdx {
			t.Errorf("expected a DELETE before the PUT-create, got call order %v", calls)
		}
	})

	t.Run("clean store still proves writable", func(t *testing.T) {
		curl := func(args ...string) ([]byte, error) { return []byte("{}"), nil }
		writable, err := qdrantWritableProbe(curl, base, 768)
		if err != nil || !writable {
			t.Fatalf("clean-store probe must pass, got writable=%v err=%v", writable, err)
		}
	})

	t.Run("readyz failure FAILs", func(t *testing.T) {
		curl := func(args ...string) ([]byte, error) {
			if args[len(args)-1] == base+"/readyz" {
				return nil, errors.New("connection refused")
			}
			return []byte("{}"), nil
		}
		if _, err := qdrantWritableProbe(curl, base, 768); err == nil {
			t.Fatal("a /readyz failure must surface an error")
		}
	})
}

// indexOf returns the first index of v in s, or -1.
func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// TestEvalAgentProof asserts the coding-agent install-readiness verdict: PASS only
// on a REAL tool-call edit; FAIL on no-edit and on err. A health-200 is NEVER an
// input — the only signal is the planted read→edit round-trip result.
func TestEvalAgentProof(t *testing.T) {
	cases := []struct {
		name       string
		edited     bool
		err        error
		wantStatus preflight.Status
	}{
		{"real edit passes", true, nil, preflight.StatusPass},
		{"no edit fails (false-green guard)", false, nil, preflight.StatusFail},
		{"round-trip err fails", false, errors.New("crush run: exit 1"), preflight.StatusFail},
		{"err takes precedence over a stale edited=true", true, errors.New("timeout"), preflight.StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toolCall := func() (bool, error) { return tc.edited, tc.err }
			got := evalAgentProof(toolCall)
			if got.status != tc.wantStatus {
				t.Errorf("status = %v, want %v (detail %q)", got.status, tc.wantStatus, got.detail)
			}
			if tc.wantStatus == preflight.StatusFail && got.detail == "" {
				t.Error("a FAIL verdict must carry a remediation detail")
			}
		})
	}
}

// TestCoderShardSingleSource proves the staged coder shard and the served coder
// model resolve from ONE catalog entry (the recommend-picked id), not two
// independent literals — so the staged filename and the served -m path can never
// drift (D-04).
func TestCoderShardSingleSource(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.Model{
		{ID: "qwen3-chat", Role: ""},
		{ID: "qwen3-coder-30b", Role: "coder", Shards: []catalog.Shard{
			{Filename: "qwen3-coder-30b.Q4.gguf", SizeBytes: 18_000_000_000},
		}},
	}}
	rec := recommend.Recommendation{Coder: recommend.CoderFit{Model: "qwen3-coder-30b"}}

	sh, ok := install.CoderShardFor(rec, cat)
	if !ok {
		t.Fatal("CoderShardFor did not resolve the picked coder entry's shard")
	}
	var servedEntry catalog.Model
	for _, m := range cat.Models {
		if m.ID == rec.Coder.Model {
			servedEntry = m
		}
	}
	if servedEntry.ID != rec.Coder.Model {
		t.Fatalf("served coder entry id = %q, want the picked id %q", servedEntry.ID, rec.Coder.Model)
	}
	if len(servedEntry.Shards) == 0 || sh.Filename != servedEntry.Shards[0].Filename {
		t.Errorf("staged shard %q does not derive from the served entry's Shards[0] %v — D-04 single-source break",
			sh.Filename, servedEntry.Shards)
	}
}

// TestRunInstallRendersToStreams: the cmd layer's whole job after wiring is to
// route the flow's narration to the command's two streams and map the outcome to
// the exit code. A config that cannot be read is the shortest refusal: two
// stderr lines, nothing on stdout, exit 1.
func TestRunInstallRendersToStreams(t *testing.T) {
	cmd := &cobra.Command{Use: "install"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	d := install.Deps{LoadConfig: func() (config.VillaConfig, error) {
		return config.VillaConfig{}, errors.New("toml: line 3: bad value")
	}}
	if code := runInstall(cmd, install.Opts{}, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if out.Len() != 0 {
		t.Errorf("stdout must be empty on a refusal, got %q", out.String())
	}
	want := "install: cannot read the persisted config: toml: line 3: bad value\ninstall: refusing to install from defaults"
	if !strings.HasPrefix(errOut.String(), want) {
		t.Errorf("stderr = %q, want prefix %q", errOut.String(), want)
	}
}

// TestModelFilesPresentIncludesSidecars guards the install's pull decision: an
// entry whose projector is missing reads as not present, so the pull runs and the
// unit never names a file the host does not have.
func TestModelFilesPresentIncludesSidecars(t *testing.T) {
	dir := t.TempDir()
	m := catalog.Model{
		ID:        "m",
		Shards:    []catalog.Shard{{Filename: "m.gguf"}},
		Projector: &catalog.Sidecar{Shards: []catalog.Shard{{Filename: "m-mmproj.gguf"}}, WeightBytes: 1, Provenance: "test"},
	}
	touch := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if modelFilesPresent(dir, m) {
		t.Fatal("nothing on disk must read as not present")
	}
	touch("m.gguf")
	if modelFilesPresent(dir, m) {
		t.Fatal("the model without its projector must read as not present")
	}
	touch("m-mmproj.gguf")
	if !modelFilesPresent(dir, m) {
		t.Fatal("model and projector on disk must read as present")
	}
}
