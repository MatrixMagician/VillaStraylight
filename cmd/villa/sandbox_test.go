package main

// sandbox_test.go drives `villa sandbox build` entirely off-hardware: the three
// host effects are fakes, so the table asserts the exit code, what reached the
// pin store, and the argv the live deps would have run — without a podman.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// fakeSandboxBuildDeps records what the verb asked of the host and answers with
// the scripted result.
type fakeSandboxBuildDeps struct {
	buildErr   error
	digest     string
	digestErr  error
	recordErr  error
	contextDir string
	contextLs  []string
	recorded   []string
}

func (f *fakeSandboxBuildDeps) deps() sandboxBuildDeps {
	return sandboxBuildDeps{
		Build: func(_ context.Context, contextDir string, _ io.Writer) error {
			f.contextDir = contextDir
			f.contextLs = lsFiles(contextDir)
			return f.buildErr
		},
		Digest: func(context.Context) (string, error) { return f.digest, f.digestErr },
		Record: func(ref string) error {
			if f.recordErr != nil {
				return f.recordErr
			}
			f.recorded = append(f.recorded, ref)
			return nil
		},
	}
}

// lsFiles lists the regular files under dir as slash-separated relative paths.
// The verb removes the context directory before it returns, so the only place
// its contents can be observed is inside the build call itself.
func lsFiles(dir string) []string {
	var out []string
	fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, path)
		}
		return err
	})
	slices.Sort(out)
	return out
}

// vettedSandboxDigest is the digest half of the compiled-in pin, so the
// matches-vetted case asserts against the real constant rather than a copy.
func vettedSandboxDigest(t *testing.T) string {
	t.Helper()
	_, digest, found := strings.Cut(orchestrate.SandboxImage(), "@")
	if !found {
		t.Fatalf("the vetted sandbox pin %q carries no digest", orchestrate.SandboxImage())
	}
	return digest
}

// TestSandboxBuildOutcomes is the verb's contract: a build or a digest villa
// cannot trust records NOTHING and exits 1, and a digest it can trust is recorded
// whether or not it matches what villa shipped.
func TestSandboxBuildOutcomes(t *testing.T) {
	otherDigest := "sha256:" + strings.Repeat("ab", 32)

	cases := []struct {
		name       string
		fake       fakeSandboxBuildDeps
		wantExit   int
		wantRecord string
		wantOut    []string
		wantErr    []string
	}{
		{
			name:     "a failed build records nothing",
			fake:     fakeSandboxBuildDeps{buildErr: os.ErrPermission},
			wantExit: exitBlocked,
			wantErr:  []string{"the image build failed", "nothing recorded"},
		},
		{
			name:     "an unreadable digest records nothing",
			fake:     fakeSandboxBuildDeps{digestErr: os.ErrNotExist},
			wantExit: exitBlocked,
			wantErr:  []string{"read the built image digest", "nothing recorded"},
		},
		{
			name:     "an empty digest records nothing",
			fake:     fakeSandboxBuildDeps{digest: ""},
			wantExit: exitBlocked,
			wantErr:  []string{`podman reported ""`, "want sha256:<64 hex>", "nothing recorded"},
		},
		{
			name:     "an unparseable digest records nothing",
			fake:     fakeSandboxBuildDeps{digest: "sha256:not-a-digest"},
			wantExit: exitBlocked,
			wantErr:  []string{"want sha256:<64 hex>", "nothing recorded"},
		},
		{
			name:       "a build matching the vetted pin is recorded and says so",
			fake:       fakeSandboxBuildDeps{digest: vettedSandboxDigest(t)},
			wantExit:   exitPass,
			wantRecord: orchestrate.SandboxImage(),
			wantOut:    []string{"recorded as the effective sandbox pin", "matches the vetted pin"},
		},
		{
			name:       "a build differing from the vetted pin is recorded and names update --check",
			fake:       fakeSandboxBuildDeps{digest: otherDigest},
			wantExit:   exitPass,
			wantRecord: sandboxImageTag() + "@" + otherDigest,
			wantOut:    []string{"differs from the vetted pin", "villa update --check", "rebuild"},
		},
		{
			name:     "a pin store that cannot be written fails the verb",
			fake:     fakeSandboxBuildDeps{digest: otherDigest, recordErr: os.ErrPermission},
			wantExit: exitBlocked,
			wantErr:  []string{"record the effective pin"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := tc.fake
			cmd := &cobra.Command{}
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)

			if got := runSandboxBuild(cmd, false, fake.deps()); got != tc.wantExit {
				t.Errorf("exit = %d, want %d\nstdout: %s\nstderr: %s", got, tc.wantExit, out.String(), errOut.String())
			}
			switch tc.wantRecord {
			case "":
				if len(fake.recorded) != 0 {
					t.Errorf("recorded %v, want nothing", fake.recorded)
				}
			default:
				if !slices.Equal(fake.recorded, []string{tc.wantRecord}) {
					t.Errorf("recorded %v, want exactly [%s]", fake.recorded, tc.wantRecord)
				}
			}
			for _, want := range tc.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("stdout does not mention %q:\n%s", want, out.String())
				}
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("stderr does not mention %q:\n%s", want, errOut.String())
				}
			}
		})
	}
}

// TestSandboxBuildMaterialisesTheEmbeddedContext proves the directory handed to
// podman is a real build context, not an empty temp dir: podman would report a
// missing Containerfile, and the verb would report a build failure with no clue
// that villa never wrote one.
func TestSandboxBuildMaterialisesTheEmbeddedContext(t *testing.T) {
	fake := fakeSandboxBuildDeps{digest: "sha256:" + strings.Repeat("cd", 32)}
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if got := runSandboxBuild(cmd, false, fake.deps()); got != exitPass {
		t.Fatalf("exit = %d, want %d", got, exitPass)
	}
	want := []string{"Containerfile", "requirements.txt", "scripts/villa-readback", "scripts/villa-recalc", "scripts/villa-render"}
	slices.Sort(want)
	if !slices.Equal(fake.contextLs, want) {
		t.Errorf("the build context held %v, want %v", fake.contextLs, want)
	}
}

// TestSandboxBuildContextIsRemoved guards the temp directory against leaking a
// full image build context per invocation.
func TestSandboxBuildContextIsRemoved(t *testing.T) {
	fake := fakeSandboxBuildDeps{digest: "sha256:" + strings.Repeat("ef", 32)}
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	runSandboxBuild(cmd, false, fake.deps())
	if _, err := os.Stat(fake.contextDir); !os.IsNotExist(err) {
		t.Errorf("the build context %s survived the run (err=%v)", fake.contextDir, err)
	}
}

// TestSandboxBuildJSON pins the --json contract's field set and values. It is a
// direct assertion rather than a golden because the vetted digest is a compiled-in
// constant a rebuild legitimately changes, and a golden would then have to be
// refrozen for a reason that has nothing to do with this verb.
func TestSandboxBuildJSON(t *testing.T) {
	digest := "sha256:" + strings.Repeat("12", 32)
	fake := fakeSandboxBuildDeps{digest: digest}
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if got := runSandboxBuild(cmd, true, fake.deps()); got != exitPass {
		t.Fatalf("exit = %d, want %d", got, exitPass)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode --json output %q: %v", out.String(), err)
	}
	want := map[string]any{
		"image":          sandboxImageTag() + "@" + digest,
		"digest":         digest,
		"vetted":         orchestrate.SandboxImage(),
		"matches_vetted": false,
		"recorded":       true,
	}
	if len(got) != len(want) {
		t.Errorf("--json emitted %d fields, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("--json %q = %v, want %v", k, got[k], v)
		}
	}
}

// TestSandboxPodmanArgv freezes the two fixed-arg command lines. They are the
// boundary this verb crosses, and a dropped `-f` or a changed `--format` would
// otherwise only surface on hardware.
func TestSandboxPodmanArgv(t *testing.T) {
	tag := sandboxImageTag()
	if strings.Contains(tag, "@") || tag == "" {
		t.Fatalf("sandboxImageTag() = %q, want the vetted reference without its digest", tag)
	}
	if want := []string{"build", "--tag", tag, "-f", "Containerfile", "."}; !slices.Equal(sandboxBuildArgs(tag), want) {
		t.Errorf("sandboxBuildArgs = %v, want %v", sandboxBuildArgs(tag), want)
	}
	if want := []string{"image", "inspect", "--format", "{{.Digest}}", tag}; !slices.Equal(sandboxDigestArgs(tag), want) {
		t.Errorf("sandboxDigestArgs = %v, want %v", sandboxDigestArgs(tag), want)
	}
}

// TestSandboxVerbIsRegistered guards the wiring: a verb the root tree does not
// carry is a verb the docs describe and nobody can run.
func TestSandboxVerbIsRegistered(t *testing.T) {
	for _, c := range newRoot().Commands() {
		if c.Name() != "sandbox" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() == "build" {
				return
			}
		}
		t.Fatal("`villa sandbox` carries no `build` subcommand")
	}
	t.Fatal("`villa sandbox` is not registered on the root command")
}
