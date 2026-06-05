package preflight

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

// okPodman builds a deps value whose podman/systemd seams succeed, parameterized by
// the subuid/subgid fixture so each case varies only the identity files.
func okPodman(subuidFile, subgidFile, username, uid string) podmanDeps {
	return podmanDeps{
		username:      username,
		uid:           uid,
		subuidPath:    subuidFile,
		subgidPath:    subgidFile,
		podmanVersion: func() (string, bool, bool) { return "podman version 5.8.2\n", true, true },
		systemdUserOK: func() (bool, bool) { return true, true },
	}
}

func TestCheckPodmanRootless(t *testing.T) {
	present := fixture(t, "subuid-present")
	absent := fixture(t, "subuid-absent")
	byUID := fixture(t, "subuid-by-uid-only")
	emptyRange := fixture(t, "subuid-empty-range")

	t.Run("subuid present by username passes (BLOCK)", func(t *testing.T) {
		got := checkPodmanRootless(okPodman(present, present, "oliverh", "1000"))
		if got.Tier != TierBlock || got.Status != StatusPass {
			t.Fatalf("got tier=%v status=%v, want BLOCK/PASS", got.Tier, got.Status)
		}
	})

	t.Run("subuid matched by numeric UID passes (Pitfall 6)", func(t *testing.T) {
		// Username does NOT appear in the file; only uid 1000 does.
		got := checkPodmanRootless(okPodman(byUID, byUID, "someotheruser", "1000"))
		if got.Status != StatusPass {
			t.Fatalf("uid-keyed subuid should pass, got status=%v (%s)", got.Status, got.Detail)
		}
	})

	t.Run("subuid absent fails (BLOCK)", func(t *testing.T) {
		got := checkPodmanRootless(okPodman(absent, absent, "oliverh", "1000"))
		if got.Tier != TierBlock || got.Status != StatusFail {
			t.Fatalf("got tier=%v status=%v, want BLOCK/FAIL", got.Tier, got.Status)
		}
		if got.Remediation == "" {
			t.Error("a failing subuid check must carry a remediation hint")
		}
	})

	t.Run("present-but-empty range is not ready (fails)", func(t *testing.T) {
		got := checkPodmanRootless(okPodman(emptyRange, emptyRange, "oliverh", "1000"))
		if got.Status != StatusFail {
			t.Fatalf("empty subuid range should fail, got %v", got.Status)
		}
	})

	t.Run("podman missing downgrades to WARN (D-15)", func(t *testing.T) {
		d := okPodman(present, present, "oliverh", "1000")
		d.podmanVersion = func() (string, bool, bool) { return "", false, false }
		got := checkPodmanRootless(d)
		if got.Tier != TierBlock || got.Status != StatusWarn {
			t.Fatalf("podman absent should downgrade to BLOCK/WARN, got tier=%v status=%v", got.Tier, got.Status)
		}
	})

	t.Run("systemctl missing downgrades to WARN (D-15)", func(t *testing.T) {
		d := okPodman(present, present, "oliverh", "1000")
		d.systemdUserOK = func() (bool, bool) { return false, false }
		got := checkPodmanRootless(d)
		if got.Status != StatusWarn {
			t.Fatalf("systemctl absent should downgrade to WARN, got %v", got.Status)
		}
	})
}

func TestSubuidReady(t *testing.T) {
	present := fixture(t, "subuid-present")
	absent := fixture(t, "subuid-absent")

	if ready, err := subuidReady("oliverh", "1000", present); !ready || err != nil {
		t.Errorf("present subuid should be ready by username (ready=%v err=%v)", ready, err)
	}
	if ready, err := subuidReady("nobody", "9999", present); ready || err != nil {
		t.Errorf("absent identity should not be ready (ready=%v err=%v)", ready, err)
	}
	if ready, err := subuidReady("oliverh", "1000", absent); ready || err != nil {
		t.Errorf("file without the user's entry should not be ready (ready=%v err=%v)", ready, err)
	}
	if ready, err := subuidReady("oliverh", "1000", filepath.Join("testdata", "does-not-exist")); ready || err != nil {
		// A missing file is a confident absence (not ready), NOT a read error.
		t.Errorf("missing file should be (false,nil) and must not panic (ready=%v err=%v)", ready, err)
	}
}

// TestSubuidReadyScanErrorIsUnevaluable asserts WR-05: a read/scan error (here
// induced by opening a directory, which scans into an "is a directory" error) is
// reported as (false, err) — distinct from a confident "not ready" — so the caller
// can WARN rather than manufacture a false BLOCK FAIL.
func TestSubuidReadyScanErrorIsUnevaluable(t *testing.T) {
	dir := t.TempDir() // os.Open succeeds on a dir; bufio.Scan then errors.
	ready, err := subuidReady("oliverh", "1000", dir)
	if ready {
		t.Errorf("scan error should not report ready")
	}
	if err == nil {
		t.Errorf("scan error should surface a non-nil error (got nil), so the check can WARN not FAIL")
	}
}

// TestCheckPodmanRootlessScanErrorWarns asserts that a subuid read error downgrades
// PRE-02 to WARN (D-15), never a false FAIL (WR-05).
func TestCheckPodmanRootlessScanErrorWarns(t *testing.T) {
	dir := t.TempDir() // unreadable-as-file path → scan error in subuidReady.
	d := okPodman(dir, dir, "oliverh", "1000")
	got := checkPodmanRootless(d)
	if got.Tier != TierBlock || got.Status != StatusWarn {
		t.Fatalf("subuid read error should downgrade to BLOCK/WARN, got tier=%v status=%v (%s)", got.Tier, got.Status, got.Detail)
	}
}

func TestMain(m *testing.M) {
	// Ensure tests run from the package dir so testdata/ relative paths resolve.
	os.Exit(m.Run())
}
