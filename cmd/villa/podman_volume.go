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
