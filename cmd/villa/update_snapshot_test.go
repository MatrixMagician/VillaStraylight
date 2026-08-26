package main

// update_snapshot_test.go drives the live snapshot path off-hardware: the podman
// volume seam is faked, so the export, the retention rename and every refusal are
// tested without a live host.
//
// The rows that matter are the ones where the snapshot is NOT usable — a podman
// failure, an empty tar, a partial write. Each must leave the previously retained
// snapshot intact, because a half-written rollback target is worse than an old one:
// it only fails when it is needed.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// snapshotEnv points the data root at a temp dir and fakes podman, returning the
// argv the fake was called with.
func snapshotEnv(t *testing.T, export func(name, out string) error) *[]string {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// requirePodman does a real LookPath, so a host without podman would refuse
	// before the fake is reached. Put a stub on PATH so the test measures the
	// snapshot logic rather than the developer's machine.
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "podman")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write podman stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var seen []string
	orig := podmanVolume
	podmanVolume = func(args []string) (string, error) {
		seen = args
		if len(args) < 5 {
			return "", errors.New("unexpected argv")
		}
		if err := export(args[2], args[4]); err != nil {
			return err.Error(), err
		}
		return "", nil
	}
	t.Cleanup(func() { podmanVolume = orig })
	return &seen
}

// writeTar is the fake export's success behaviour: a non-empty file at the
// requested path.
func writeTar(body string) func(name, out string) error {
	return func(_, out string) error { return os.WriteFile(out, []byte(body), 0o600) }
}

// TestSnapshotExportsThroughTheSharedVolumeSeam: the snapshot uses the SAME
// fixed-arg `podman volume export` that `villa backup` uses.
//
// A second implementation would be a second opinion about what a clean snapshot
// is, and the two would drift the moment one of them learned something.
func TestSnapshotExportsThroughTheSharedVolumeSeam(t *testing.T) {
	seen := snapshotEnv(t, writeTar("qdrant-data"))

	got, err := liveSnapshotData(context.Background(), subsystem.Memory)
	if err != nil {
		t.Fatalf("liveSnapshotData: %v", err)
	}

	if len(*seen) < 3 || (*seen)[0] != "volume" || (*seen)[1] != "export" {
		t.Errorf("argv = %v, want the shared fixed-arg volume export", *seen)
	}
	if (*seen)[2] != "villa-qdrant" {
		t.Errorf("exported volume %q, want the one memory declares as owned state", (*seen)[2])
	}
	if got.Volume != "villa-qdrant" {
		t.Errorf("the snapshot records volume %q", got.Volume)
	}
	if got.Bytes == 0 || got.TakenAt == "" {
		t.Errorf("the snapshot records no size or date: %+v", got)
	}
	if _, statErr := os.Stat(got.Path); statErr != nil {
		t.Errorf("the snapshot is not on disk at the recorded path: %v", statErr)
	}
}

// TestTheSnapshotIsNeverWrittenIntoTheModelStore: snapshots are user data, not
// weights, and must not accumulate where `villa update` has no business writing.
func TestTheSnapshotIsNeverWrittenIntoTheModelStore(t *testing.T) {
	snapshotEnv(t, writeTar("owui"))

	got, err := liveSnapshotData(context.Background(), subsystem.Chat)
	if err != nil {
		t.Fatalf("liveSnapshotData: %v", err)
	}
	if strings.Contains(got.Path, "models") {
		t.Errorf("the snapshot landed in the model store: %q", got.Path)
	}
	if filepath.Dir(got.Path) != snapshotDir() {
		t.Errorf("the snapshot landed outside the snapshot directory: %q", got.Path)
	}
}

// TestAFailedExportLeavesNoPartialSnapshot: a snapshot that died midway must not
// be left where the retained one lives.
//
// A truncated tar is a rollback target that only fails when it is used, which is
// the whole class of failure this lifecycle exists to remove.
func TestAFailedExportLeavesNoPartialSnapshot(t *testing.T) {
	snapshotEnv(t, func(_, out string) error {
		// Podman wrote part of the tar and then failed, which is what a full disk
		// looks like from here.
		_ = os.WriteFile(out, []byte("half a tar"), 0o600)
		return errors.New("no space left on device")
	})

	if _, err := liveSnapshotData(context.Background(), subsystem.Chat); err == nil {
		t.Fatal("a failed export reported success")
	}
	entries, _ := os.ReadDir(snapshotDir())
	for _, e := range entries {
		t.Errorf("a partial snapshot was left behind: %s", e.Name())
	}
}

// TestAnEmptyExportIsRefused: podman can exit clean and produce nothing.
//
// Nothing else would notice, which is exactly why it is checked here: a zero-byte
// tar restores into an empty volume, silently discarding the user's data at the
// moment they most needed it back.
func TestAnEmptyExportIsRefused(t *testing.T) {
	snapshotEnv(t, writeTar(""))

	_, err := liveSnapshotData(context.Background(), subsystem.Memory)
	if err == nil {
		t.Fatal("an empty export was accepted as a rollback target")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("the error does not name the empty export: %v", err)
	}
}

// TestAFailedSnapshotDoesNotDisplaceTheRetainedOne: the previously retained
// snapshot is the live rollback target, so a failed attempt must leave it alone.
func TestAFailedSnapshotDoesNotDisplaceTheRetainedOne(t *testing.T) {
	seen := snapshotEnv(t, writeTar("good-data"))
	first, err := liveSnapshotData(context.Background(), subsystem.Chat)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	_ = seen

	// The next attempt fails after writing part of a tar.
	orig := podmanVolume
	podmanVolume = func(args []string) (string, error) {
		_ = os.WriteFile(args[4], []byte("junk"), 0o600)
		return "disk full", errors.New("no space left on device")
	}
	t.Cleanup(func() { podmanVolume = orig })

	if _, err := liveSnapshotData(context.Background(), subsystem.Chat); err == nil {
		t.Fatal("the failing snapshot reported success")
	}

	body, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatalf("the retained snapshot is gone: %v", err)
	}
	if string(body) != "good-data" {
		t.Errorf("the retained rollback target was overwritten by a failed attempt: %q", body)
	}
}

// TestSnapshottingAStatelessSubsystemIsAnError: the flow never calls this for a
// stateless subsystem, so reaching it means a miswiring.
//
// Returning an error rather than an empty snapshot keeps that loud. A quiet "no
// data was taken" would look identical to a successful stateless update and would
// let a future stateful subsystem be mutated unsnapshotted.
func TestSnapshottingAStatelessSubsystemIsAnError(t *testing.T) {
	snapshotEnv(t, writeTar("x"))

	for _, k := range []subsystem.Kind{subsystem.Inference, subsystem.WebSearch, subsystem.Agent} {
		if _, err := liveSnapshotData(context.Background(), k); err == nil {
			t.Errorf("%v owns no persistent state but its snapshot succeeded", k)
		}
	}
}

// TestSnapshotPathIsPerSubsystemAndStable: one snapshot per stateful subsystem,
// matching the one-previous rule the images already follow.
//
// A timestamped or digest-keyed name would accumulate silently, which on memory's
// 2.8 GB snapshots fills a disk without anyone deciding to.
func TestSnapshotPathIsPerSubsystemAndStable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	seen := map[string]subsystem.Kind{}
	for _, k := range subsystem.Stateful() {
		p := snapshotPath(k)
		if p == "" {
			t.Errorf("%v owns persistent state but has no snapshot path", k)
			continue
		}
		if p != snapshotPath(k) {
			t.Errorf("%v's snapshot path is not stable across calls", k)
		}
		if other, clash := seen[p]; clash {
			t.Errorf("%v and %v share the snapshot path %q; one would overwrite the other's rollback target", k, other, p)
		}
		seen[p] = k
	}
	for _, k := range subsystem.Every {
		if !k.OwnsPersistentState() && snapshotPath(k) != "" {
			t.Errorf("%v owns no persistent state but has a snapshot path %q", k, snapshotPath(k))
		}
	}
}

// TestStopAndStartCoverEveryServiceTheSubsystemOwns.
//
// Memory holds two services, and exporting Qdrant's volume while the embedder
// still writes through it would produce the torn copy the stop exists to prevent.
// The start half matters more: villa stopped them, so every one must come back.
func TestStopAndStartCoverEveryServiceTheSubsystemOwns(t *testing.T) {
	for _, k := range subsystem.Stateful() {
		_, services := subsystemUnits(k)
		if len(services) == 0 {
			t.Errorf("%v owns persistent state but no services, so nothing would be stopped before its volume is exported", k)
		}
	}
}

// TestHumanBytesReadsAsADiskCost pins the wording the user sees for the measured
// figures: memory's 2.8 GB and chat's sub-300 MB snapshot.
func TestHumanBytesReadsAsADiskCost(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{2_800_000_000, "2.8 GB"},
		{267_000_000, "267 MB"},
		{4_096, "4 kB"},
		{12, "12 B"},
	}
	for _, tc := range cases {
		if got := humanBytes(tc.n); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestSnapshotRefusesWithoutPodman: an unavailable podman blocks updating a
// stateful subsystem. That is the accepted cost, and it is asserted so nobody
// softens it into a warning later.
func TestSnapshotRefusesWithoutPodman(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // an empty PATH: no podman anywhere

	_, err := liveSnapshotData(context.Background(), subsystem.Chat)
	if err == nil {
		t.Fatal("the snapshot succeeded with no podman on PATH")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Errorf("the refusal came from running podman rather than from finding it absent: %v", err)
	}
}
