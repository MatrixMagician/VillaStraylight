package main

// sandbox.go wires `villa sandbox build`: the verb that builds the workspace
// agent's task image on THIS host and records the digest it produced as the
// effective pin for pins.SandboxImage.
//
// It closes the one gap the v1.11 spec names outside §13. The sandbox image is
// the only pin villa builds rather than pulls, and a dnf build is not
// byte-reproducible, so another host's build of the same Containerfile at the
// same pinned package versions yields a different digest. A fresh host therefore
// held no image at all, and a hand-built one still left the runner resolving a
// vetted digest the host does not have.
//
// Recording goes through writeEffectivePins — the same store `villa update`
// writes — because liveSandboxImage and liveSandboxDeps already resolve the image
// through the pin resolver. Nothing in the runner changes; this verb only fills
// in the value it reads.
//
// The build context is EMBEDDED (build/sandbox) rather than read from the
// worktree, so the verb works from the shipped binary. No image literal is typed
// here: the tag is orchestrate.SandboxImage() with its digest dropped, which is
// what keeps the literal in the package that owns it.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	sandboxctx "github.com/MatrixMagician/VillaStraylight/build/sandbox"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
)

// sandboxDigestPattern is the one digest form podman reports for a built image.
// Validating it is what stops an empty or garbled `--format` result being
// recorded as a pin the runner would then try to launch.
var sandboxDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// sandboxImageTag is the vetted reference with its digest dropped: the tag podman
// builds to and inspects by. Derived rather than typed so the reference literal
// stays in internal/orchestrate.
func sandboxImageTag() string {
	tag, _, _ := strings.Cut(orchestrate.SandboxImage(), "@")
	return tag
}

// sandboxBuildArgs builds the FIXED-ARG argv for the image build, run with the
// materialised context directory as the working directory. Pure builder, so the
// argv is asserted without a live podman (the podman_volume.go precedent).
func sandboxBuildArgs(tag string) []string {
	return []string{"build", "--tag", tag, "-f", "Containerfile", "."}
}

// sandboxDigestArgs builds the FIXED-ARG argv that reads the built image's
// digest. Pure builder.
func sandboxDigestArgs(tag string) []string {
	return []string{"image", "inspect", "--format", "{{.Digest}}", tag}
}

// sandboxBuildDeps are the three host effects the verb has: run the build, read
// the digest it produced, record that digest as the effective pin.
type sandboxBuildDeps struct {
	// Build runs the image build with contextDir as its working directory,
	// streaming podman's progress to out.
	Build func(ctx context.Context, contextDir string, out io.Writer) error
	// Digest reads the digest of the image the build just tagged.
	Digest func(ctx context.Context) (string, error)
	// Record persists ref as this host's effective sandbox pin.
	Record func(ref string) error
}

// liveSandboxBuildDeps wires the verb to the real host: fixed-arg podman, never a
// shell, and the shared pin-state writer.
func liveSandboxBuildDeps() sandboxBuildDeps {
	tag := sandboxImageTag()
	return sandboxBuildDeps{
		Build: func(ctx context.Context, contextDir string, out io.Writer) error {
			if err := requirePodman(); err != nil {
				return err
			}
			c := exec.CommandContext(ctx, "podman", sandboxBuildArgs(tag)...) // fixed args, no shell
			c.Dir = contextDir
			c.Stdout, c.Stderr = out, out
			return c.Run()
		},
		Digest: func(ctx context.Context) (string, error) {
			var stdout, stderr bytes.Buffer
			c := exec.CommandContext(ctx, "podman", sandboxDigestArgs(tag)...) // fixed args, no shell
			c.Stdout, c.Stderr = &stdout, &stderr
			if err := c.Run(); err != nil {
				return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
			}
			return strings.TrimSpace(stdout.String()), nil
		},
		Record: func(ref string) error {
			return writeEffectivePins(map[string]string{string(pins.SandboxImage): ref})
		},
	}
}

// sandboxBuildResult is the `sandbox build --json` shape, and the single value
// both renderings read. It carries the vetted reference alongside the built one
// so a reader can see the divergence rather than being told about it.
type sandboxBuildResult struct {
	Image         string `json:"image"`
	Digest        string `json:"digest"`
	Vetted        string `json:"vetted"`
	MatchesVetted bool   `json:"matches_vetted"`
	Recorded      bool   `json:"recorded"`
}

// newSandbox builds the `villa sandbox` noun and its build subcommand.
func newSandbox() *cobra.Command {
	sb := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage the workspace agent's task image",
		Args:  cobra.NoArgs,
	}
	sb.AddCommand(newSandboxBuild())
	return sb
}

// newSandboxBuild builds `villa sandbox build [--json]`.
func newSandboxBuild() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the workspace agent's task image and record its digest as the effective pin",
		Long: "Build the image a workspace task runs in, on this host, and record the digest it " +
			"produced as the effective sandbox pin.\n\n" +
			"What it does, in order: write the embedded build context (the Containerfile, the pip " +
			"hash-lock and the three office scripts) to a temporary directory; run `podman build` " +
			"against it; read the built digest with `podman image inspect`; record " +
			"`<image>@<digest>` as the effective pin, which is what the task runner resolves before " +
			"it launches.\n\n" +
			"THIS REACHES THE NETWORK. The build installs packages from Fedora's repositories and " +
			"wheels from PyPI, performed by podman rather than by villa. It is the same class of " +
			"outbound as a model or image pull: on-command, and the only kind villa permits.\n\n" +
			"A dnf build is not byte-reproducible, so the digest this host produces will usually " +
			"differ from the one villa shipped as vetted. That is expected, and villa records and " +
			"reports it rather than refusing: `villa update --check` then reports the sandbox " +
			"component as a rebuild, because a rolling digest has no version to compare.\n\n" +
			"Re-running is safe. The build reuses podman's layer cache and the new digest replaces " +
			"the recorded one. Exits 0 on a recorded build, 1 on any failure — a failed build or an " +
			"unreadable digest records nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runSandboxBuild(cmd, asJSON, liveSandboxBuildDeps()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the built image and pin decision as JSON")
	return cmd
}

// runSandboxBuild materialises the context, builds, validates the digest, records
// it, and RETURNS the exit code so tests assert output + code together.
//
// The order is load-bearing. Nothing is recorded until podman has reported a
// digest in the one form it can report, so a failed build leaves the host
// resolving whatever pin it resolved before.
func runSandboxBuild(cmd *cobra.Command, asJSON bool, d sandboxBuildDeps) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	dir, err := os.MkdirTemp("", "villa-sandbox-build-")
	if err != nil {
		fmt.Fprintf(errOut, "sandbox build: create the build context: %v\n", err)
		return exitBlocked
	}
	defer os.RemoveAll(dir)
	if err := os.CopyFS(dir, sandboxctx.Context); err != nil {
		fmt.Fprintf(errOut, "sandbox build: write the build context: %v\n", err)
		return exitBlocked
	}

	ctx := cmdContext(cmd)
	if err := d.Build(ctx, dir, errOut); err != nil {
		fmt.Fprintf(errOut, "sandbox build: the image build failed: %v — nothing recorded\n", err)
		return exitBlocked
	}

	digest, err := d.Digest(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "sandbox build: read the built image digest: %v — nothing recorded\n", err)
		return exitBlocked
	}
	if !sandboxDigestPattern.MatchString(digest) {
		fmt.Fprintf(errOut, "sandbox build: podman reported %q as the built digest, want sha256:<64 hex> — nothing recorded\n", digest)
		return exitBlocked
	}

	res := sandboxBuildResult{
		Image:  sandboxImageTag() + "@" + digest,
		Digest: digest,
		Vetted: orchestrate.SandboxImage(),
	}
	res.MatchesVetted = res.Image == res.Vetted
	if err := d.Record(res.Image); err != nil {
		fmt.Fprintf(errOut, "sandbox build: record the effective pin: %v\n", err)
		return exitBlocked
	}
	res.Recorded = true

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(errOut, "sandbox build: encode json: %v\n", err)
			return exitBlocked
		}
		return exitPass
	}

	fmt.Fprintf(out, "built and recorded as the effective sandbox pin: %s\n", res.Image)
	if res.MatchesVetted {
		fmt.Fprintf(out, "the digest matches the vetted pin\n")
		return exitPass
	}
	fmt.Fprintf(out, "the digest differs from the vetted pin %s\n", res.Vetted)
	fmt.Fprintf(out, "that is expected — a dnf build is not byte-reproducible; `villa update --check` reports the sandbox as a rebuild\n")
	return exitPass
}
