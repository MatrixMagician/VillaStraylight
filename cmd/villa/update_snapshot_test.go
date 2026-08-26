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
	"reflect"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
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

// ---------------------------------------------------------------------------
// The restore half
// ---------------------------------------------------------------------------

// restoreEnv fakes podman for the restore path, recording every argv in order so
// the clean-recreate SEQUENCE is assertable rather than merely its steps.
func restoreEnv(t *testing.T, fail map[string]error) *[][]string {
	t.Helper()
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "podman")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write podman stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var calls [][]string
	orig := podmanVolume
	podmanVolume = func(args []string) (string, error) {
		calls = append(calls, args)
		if err, bad := fail[args[1]]; bad {
			return err.Error(), err
		}
		return "", nil
	}
	t.Cleanup(func() { podmanVolume = orig })
	return &calls
}

// tarOnDisk writes a stand-in snapshot and returns the record pointing at it.
func tarOnDisk(t *testing.T, vol string) pinstate.DataSnapshot {
	t.Helper()
	path := filepath.Join(t.TempDir(), vol+".tar")
	if err := os.WriteFile(path, []byte("snapshot"), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	return pinstate.DataSnapshot{Volume: vol, Path: path, Bytes: 8, TakenAt: "2026-08-26T12:00:00Z"}
}

// verbs flattens the recorded argv list to its podman subcommands.
func verbs(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c[1])
	}
	return out
}

// namesVolume reports whether an argv targets the given volume. The name's
// position varies by verb (`volume rm --force <name>` against `volume import
// <name> <src>`), so the assertion checks membership rather than a fixed index.
func namesVolume(argv []string, vol string) bool {
	for _, a := range argv {
		if a == vol {
			return true
		}
	}
	return false
}

// TestTheRestoreReplacesRatherThanMerges is the load-bearing ordering assertion.
//
// `podman volume import` MERGES into existing contents and does not auto-create,
// so importing a pre-migration snapshot straight over a migrated volume leaves a
// hybrid of both schemas — worse than either, because it looks readable. The
// clean-recreate ordering is what makes the restore a replace.
func TestTheRestoreReplacesRatherThanMerges(t *testing.T) {
	calls := restoreEnv(t, nil)
	snap := tarOnDisk(t, "villa-openwebui")

	if err := liveRestoreData(context.Background(), subsystem.Chat, snap); err != nil {
		t.Fatalf("liveRestoreData: %v", err)
	}

	got := verbs(*calls)
	want := []string{"rm", "create", "import"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("podman verbs = %v, want %v — an import without the rm+create merges into the migrated volume", got, want)
	}
	for _, c := range *calls {
		if c[0] != "volume" {
			t.Errorf("argv %v does not target a volume", c)
		}
		if !namesVolume(c, "villa-openwebui") {
			t.Errorf("argv %v does not name the volume the snapshot came from", c)
		}
	}
	last := (*calls)[len(*calls)-1]
	if last[3] != snap.Path {
		t.Errorf("the import read %q, not the recorded snapshot %q", last[3], snap.Path)
	}
}

// TestTheRestoreImportsIntoTheVolumeTheSnapshotCameFrom.
//
// The volume comes from the SNAPSHOT record, not from today's declaration. If a
// future change moved a subsystem's volume, importing into the new name would
// restore nothing and orphan the old data.
func TestTheRestoreImportsIntoTheVolumeTheSnapshotCameFrom(t *testing.T) {
	calls := restoreEnv(t, nil)
	snap := tarOnDisk(t, "villa-openwebui-from-an-older-villa")

	if err := liveRestoreData(context.Background(), subsystem.Chat, snap); err != nil {
		t.Fatalf("liveRestoreData: %v", err)
	}
	for _, c := range *calls {
		if !namesVolume(c, "villa-openwebui-from-an-older-villa") {
			t.Errorf("argv %v ignores the volume the snapshot records", c)
		}
	}
}

// TestAnAbsentVolumeIsToleratedOnTheRemove: clean-recreate is idempotent, and
// `podman volume rm` has no tolerance flag, so the not-found stderr is inspected
// exactly as the restore path already does.
func TestAnAbsentVolumeIsToleratedOnTheRemove(t *testing.T) {
	calls := restoreEnv(t, map[string]error{"rm": errors.New("no such volume")})
	snap := tarOnDisk(t, "villa-qdrant")

	if err := liveRestoreData(context.Background(), subsystem.Memory, snap); err != nil {
		t.Fatalf("an already-absent volume failed the restore: %v", err)
	}
	if got := verbs(*calls); !reflect.DeepEqual(got, []string{"rm", "create", "import"}) {
		t.Errorf("podman verbs = %v; the restore did not continue past an absent volume", got)
	}
}

// TestAMissingSnapshotRefusesBeforeTheVolumeIsTouched.
//
// Someone cleared disk by hand and the rollback target is gone. Removing the
// volume and importing nothing would destroy the user's current data in the name
// of restoring it, so villa must not reach the rm at all.
func TestAMissingSnapshotRefusesBeforeTheVolumeIsTouched(t *testing.T) {
	calls := restoreEnv(t, nil)
	snap := pinstate.DataSnapshot{Volume: "villa-openwebui", Path: filepath.Join(t.TempDir(), "gone.tar")}

	err := liveRestoreData(context.Background(), subsystem.Chat, snap)
	if err == nil {
		t.Fatal("a missing snapshot was restored anyway")
	}
	if len(*calls) != 0 {
		t.Errorf("the volume was touched despite a missing snapshot: %v", *calls)
	}
}

// TestAnEmptySnapshotRefusesBeforeTheVolumeIsTouched: a zero-byte tar restores
// into an empty volume, silently discarding the data at the moment it is most
// needed.
func TestAnEmptySnapshotRefusesBeforeTheVolumeIsTouched(t *testing.T) {
	calls := restoreEnv(t, nil)
	path := filepath.Join(t.TempDir(), "empty.tar")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write empty tar: %v", err)
	}

	err := liveRestoreData(context.Background(), subsystem.Chat, pinstate.DataSnapshot{Volume: "villa-openwebui", Path: path})
	if err == nil {
		t.Fatal("an empty snapshot was restored anyway")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("the error does not name the empty snapshot: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("the volume was removed before the empty snapshot was noticed: %v", *calls)
	}
}

// TestAFailedImportIsReportedRatherThanSwallowed: this is the worst state in the
// lifecycle, and the core turns it into rollback-incomplete. It must arrive as an
// error rather than a silent success.
func TestAFailedImportIsReportedRatherThanSwallowed(t *testing.T) {
	restoreEnv(t, map[string]error{"import": errors.New("unexpected EOF")})
	snap := tarOnDisk(t, "villa-qdrant")

	err := liveRestoreData(context.Background(), subsystem.Memory, snap)
	if err == nil {
		t.Fatal("a failed import reported success")
	}
	if !strings.Contains(err.Error(), "import") {
		t.Errorf("the error does not name the failed import: %v", err)
	}
}

// TestRestoringAnUntakenSnapshotIsAnError: the flow never calls this without a
// snapshot, so reaching it means a miswiring. A quiet success would look like a
// completed data rollback that never happened.
func TestRestoringAnUntakenSnapshotIsAnError(t *testing.T) {
	calls := restoreEnv(t, nil)

	if err := liveRestoreData(context.Background(), subsystem.Chat, pinstate.DataSnapshot{}); err == nil {
		t.Error("restoring an untaken snapshot reported success")
	}
	if len(*calls) != 0 {
		t.Errorf("the volume was touched for an untaken snapshot: %v", *calls)
	}
}
