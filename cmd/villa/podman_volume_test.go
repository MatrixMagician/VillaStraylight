package main

// podman_volume_test.go guards the shared cmd-tier podman VOLUME seam (D-02): the
// pure arg-builders are FIXED-ARG argv slices (never a shell string, never
// interpolation — T-16-02b), and the injectable podmanVolume var is fake-swappable
// so backup/restore drive off-hardware.

import (
	"errors"
	"reflect"
	"testing"
)

// TestVolumeExportArgsFixed asserts the export argv is the exact fixed-arg form
// `volume export <name> --output <out>` — an argv slice, never a shell string.
func TestVolumeExportArgsFixed(t *testing.T) {
	got := volumeExportArgs("villa-openwebui", "/tmp/owui.tar")
	want := []string{"volume", "export", "villa-openwebui", "--output", "/tmp/owui.tar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("volumeExportArgs = %v, want %v", got, want)
	}
}

// TestVolumeImportArgsFixed asserts the import argv is the exact fixed-arg form
// `volume import <name> <src>`.
func TestVolumeImportArgsFixed(t *testing.T) {
	got := volumeImportArgs("villa-openwebui", "/tmp/owui.tar")
	want := []string{"volume", "import", "villa-openwebui", "/tmp/owui.tar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("volumeImportArgs = %v, want %v", got, want)
	}
}

// TestVolumeArgsNoShellMetachars guards that a volume name carrying shell
// metacharacters is passed through VERBATIM as a single argv element (it is never
// split or interpreted) — proving the fixed-arg seam cannot be shell-injected even
// if a name were somehow attacker-influenced.
func TestVolumeArgsNoShellMetachars(t *testing.T) {
	name := "vol; rm -rf /"
	got := volumeExportArgs(name, "/tmp/o.tar")
	if got[2] != name {
		t.Fatalf("volume name not passed verbatim as one argv element: %q", got[2])
	}
}

// TestPodmanVolumeFakeSwappable asserts the package-level podmanVolume var is
// fake-swappable: a test can replace it and observe the exact argv it received,
// with no live podman.
func TestPodmanVolumeFakeSwappable(t *testing.T) {
	orig := podmanVolume
	t.Cleanup(func() { podmanVolume = orig })

	var seen []string
	podmanVolume = func(args []string) (string, error) {
		seen = args
		return "", errors.New("fake")
	}

	args := volumeExportArgs("v", "/tmp/o.tar")
	if _, err := podmanVolume(args); err == nil {
		t.Fatal("expected fake error")
	}
	if !reflect.DeepEqual(seen, args) {
		t.Fatalf("podmanVolume saw %v, want %v", seen, args)
	}
}
