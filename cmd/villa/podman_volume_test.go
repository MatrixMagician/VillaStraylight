package main

// podman_volume_test.go guards the shared cmd-tier podman VOLUME seam (D-02): the
// pure arg-builders are FIXED-ARG argv slices (never a shell string, never
// interpolation — T-16-02b), and the injectable podmanVolume var is fake-swappable
// so backup/restore drive off-hardware.

import (
	"bytes"
	"errors"
	"os/exec"
	"reflect"
	"strings"
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

// TestVolumeExistsArgsFixed asserts the existence-check argv is the exact
// fixed-arg form `volume exists <name>` — an argv slice, never a shell string
// (Phase-23 D-05: backup gates the qdrant entry on this check).
func TestVolumeExistsArgsFixed(t *testing.T) {
	got := volumeExistsArgs("villa-qdrant")
	want := []string{"volume", "exists", "villa-qdrant"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("volumeExistsArgs = %v, want %v", got, want)
	}
}

// TestClassifyVolumeExists asserts the fail-soft exit-code classification
// (D-05): exit 0 ⇒ exists; exit 1 ⇒ absent (no warning); any other error ⇒
// absent WITH a warning (backup then honestly omits the entry — never a hard
// failure on an unevaluable check).
func TestClassifyVolumeExists(t *testing.T) {
	if exists, warn := classifyVolumeExists(nil); !exists || warn {
		t.Fatalf("nil error must classify as exists (no warning), got exists=%v warn=%v", exists, warn)
	}
	// A real exit-1 *exec.ExitError (hermetic: /usr/bin/false).
	exit1 := exec.Command("false").Run()
	if exit1 == nil {
		t.Skip("cannot fabricate an exit-1 error on this host")
	}
	if exists, warn := classifyVolumeExists(exit1); exists || warn {
		t.Fatalf("exit 1 must classify as absent WITHOUT warning, got exists=%v warn=%v", exists, warn)
	}
	if exists, warn := classifyVolumeExists(errors.New("podman exploded")); exists || !warn {
		t.Fatalf("other error must classify as absent WITH warning, got exists=%v warn=%v", exists, warn)
	}
}

// TestVolumeExistsOverSeam asserts the existence helper drives the injectable
// podmanVolume seam (no live podman): a nil error ⇒ true; a generic error ⇒
// false plus a printed warning (fail-soft).
func TestVolumeExistsOverSeam(t *testing.T) {
	orig := podmanVolume
	t.Cleanup(func() { podmanVolume = orig })

	var seen []string
	podmanVolume = func(args []string) (string, error) {
		seen = args
		return "", nil
	}
	var warnBuf bytes.Buffer
	if !volumeExists("v1", &warnBuf) {
		t.Fatal("nil seam error must report exists=true")
	}
	if !reflect.DeepEqual(seen, []string{"volume", "exists", "v1"}) {
		t.Fatalf("volumeExists drove argv %v, want [volume exists v1]", seen)
	}
	if warnBuf.Len() != 0 {
		t.Fatalf("no warning expected on exists, got %q", warnBuf.String())
	}

	podmanVolume = func(args []string) (string, error) {
		return "boom stderr", errors.New("podman exploded")
	}
	warnBuf.Reset()
	if volumeExists("v1", &warnBuf) {
		t.Fatal("generic seam error must report exists=false (fail-soft)")
	}
	if !strings.Contains(warnBuf.String(), "warning") {
		t.Fatalf("generic error must print a warning, got %q", warnBuf.String())
	}
}

// TestVolumeExistsTri asserts the tri-state restore-side helper (WR-02): a nil
// seam error ⇒ exists (not unknown); a generic failure ⇒ UNKNOWN=true with a
// printed warning — never silently collapsed into a confident "absent" the way
// the backup-side fail-soft helper does.
func TestVolumeExistsTri(t *testing.T) {
	orig := podmanVolume
	t.Cleanup(func() { podmanVolume = orig })

	podmanVolume = func(args []string) (string, error) { return "", nil }
	var warnBuf bytes.Buffer
	exists, unknown := volumeExistsTri("v1", &warnBuf)
	if !exists || unknown {
		t.Fatalf("nil seam error must report exists=true unknown=false, got %v/%v", exists, unknown)
	}

	podmanVolume = func(args []string) (string, error) { return "boom stderr", errors.New("podman exploded") }
	warnBuf.Reset()
	exists, unknown = volumeExistsTri("v1", &warnBuf)
	if exists || !unknown {
		t.Fatalf("generic seam error must report UNKNOWN (exists=false unknown=true), got %v/%v", exists, unknown)
	}
	if !strings.Contains(warnBuf.String(), "UNKNOWN") {
		t.Fatalf("unknown cell must print a warning naming the unknown state, got %q", warnBuf.String())
	}

	// A real exit-1 *exec.ExitError is a CONFIDENT absent — not unknown.
	exit1 := exec.Command("false").Run()
	if exit1 != nil {
		podmanVolume = func(args []string) (string, error) { return "", exit1 }
		warnBuf.Reset()
		exists, unknown = volumeExistsTri("v1", &warnBuf)
		if exists || unknown {
			t.Fatalf("exit 1 must report confident absent (exists=false unknown=false), got %v/%v", exists, unknown)
		}
		if warnBuf.Len() != 0 {
			t.Fatalf("confident absent must print no warning, got %q", warnBuf.String())
		}
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
