package main

// podman_volume.go is the SHARED cmd-tier fixed-arg podman VOLUME seam used by the
// Phase-16 backup/restore flow (D-02). It clones the seam-gate-proven pattern of
// uninstall.go's podmanVolumeRm: a package-level injectable `podmanVolume` var that
// runs `exec.Command("podman", args...)` with FIXED ARGS (never a shell, never
// interpolation — T-16-02b) plus the pure arg-builders the regression tests assert
// against. Volume names are config/catalog-resolved constants, so no untrusted
// string is ever shell-interpolated. Adding the export/import vars here (rather than
// a new impure internal module) keeps internal/orchestrate the ONLY intentionally
// impure first-party module (D-02).

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// volumeExportArgs builds the FIXED-ARG argv for exporting one podman volume to a
// file: `volume export <name> --output <out>`. Pure builder (the seam the argv
// equality test asserts against).
func volumeExportArgs(name, out string) []string {
	return []string{"volume", "export", name, "--output", out}
}

// volumeImportArgs builds the FIXED-ARG argv for importing a tar into a (clean,
// already-recreated) podman volume: `volume import <name> <src>`. `podman volume
// import` MERGES, so restore must recreate the volume before calling this. Pure
// builder (asserted by the argv equality test).
func volumeImportArgs(name, src string) []string {
	return []string{"volume", "import", name, src}
}

// volumeExistsArgs builds the FIXED-ARG argv for the volume existence check:
// `volume exists <name>` (exit 0 = exists, exit 1 = absent). Pure builder
// (asserted by the argv equality test) — Phase-23 D-05: backup gates the
// optional qdrant entry on this check.
func volumeExistsArgs(name string) []string {
	return []string{"volume", "exists", name}
}

// volumeExists reports whether the named podman volume exists via the injectable
// podmanVolume seam, FAIL-SOFT (D-05): exit 0 ⇒ true; exit 1 ⇒ false; a missing
// podman binary or any other failure ⇒ false WITH a printed warning — backup
// then simply omits the entry honestly rather than hard-failing on an
// unevaluable check (the typed-Unknown degradation discipline).
func volumeExists(name string, errOut io.Writer) bool {
	stderr, err := podmanVolume(volumeExistsArgs(name))
	exists, warn := classifyVolumeExists(err)
	if warn {
		fmt.Fprintf(errOut, "warning: podman volume exists %q check failed (%v: %s) — treating the volume as absent\n",
			name, err, stderr)
	}
	return exists
}

// volumeExistsTri is the TRI-STATE existence check for DESTRUCTIVE callers
// (restore — Phase-23 review WR-02): exists / absent / UNKNOWN. Unlike the
// fail-soft volumeExists (which collapses an unevaluable check into a confident
// "absent" — the right direction for backup, where the entry is honestly
// omitted), restore selects its capture/quiesce/rollback shape from this signal:
// flattening Unknown to absent would route a destructive VolumeRm past the
// rollback capture on a transient podman failure. The caller must FAIL CLOSED
// on unknown=true (the typed-Unknown doctrine: an Unknown is never a confident
// negative).
func volumeExistsTri(name string, errOut io.Writer) (exists, unknown bool) {
	stderr, err := podmanVolume(volumeExistsArgs(name))
	exists, warn := classifyVolumeExists(err)
	if warn {
		fmt.Fprintf(errOut, "warning: podman volume exists %q check failed (%v: %s) — existence is UNKNOWN\n",
			name, err, stderr)
	}
	return exists, warn
}

// classifyVolumeExists maps a `podman volume exists` run error to the fail-soft
// verdict: nil ⇒ exists; an *exec.ExitError with code 1 ⇒ confidently absent (no
// warning); anything else (podman missing, exec failure, other exit codes) ⇒
// absent-with-warning. Pure classification so the exit-code contract is
// unit-testable without a live podman.
func classifyVolumeExists(err error) (exists, warn bool) {
	if err == nil {
		return true, false
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, false
	}
	return false, true
}

// podmanVolume runs `podman <args...>` with a FIXED-ARG exec (never a shell —
// T-16-02b) and returns the trimmed stderr alongside any error so callers can both
// diagnose a genuine failure AND recognise an already-absent volume. It is a
// package-level var so backup/restore tests can swap in a fake runner and drive the
// flow with no live podman (mirrors uninstall.go's podmanVolumeRm).
var podmanVolume = func(args []string) (stderr string, err error) {
	var buf bytes.Buffer
	cmd := exec.Command("podman", args...) // fixed args, no shell
	cmd.Stderr = &buf
	err = cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

// requirePodman returns orchestrate.ErrToolNotFound when the podman binary is not on
// PATH, so a missing-tool host degrades to a soft, actionable error (mirrors
// removeVolumesLive's LookPath guard) BEFORE any volume op runs.
func requirePodman() error {
	if _, err := exec.LookPath("podman"); err != nil {
		return orchestrate.ErrToolNotFound{Tool: "podman"}
	}
	return nil
}
