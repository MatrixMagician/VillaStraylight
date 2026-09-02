package main

// update_snapshot.go is the live host side of the stopped snapshot window: the
// stop, the volume export, and the start that every path out of the window runs.
//
// It exists as its own file because it is the only part of `villa update` that
// touches DATA rather than pins, units and config — and because the rule it enforces
// is narrower than the rest of the verb. Everywhere else in the lifecycle an
// unprovable component rolls back. Here an unsnapshottable component is not touched
// at all, because the alternative is what produced the incident: a real
// `villa update chat` migrated Open WebUI's SQLite schema forward, and the retained
// image could not roll that back.
//
// The volume export goes through the SHARED cmd-tier fixed-arg podman volume seam
// (podman_volume.go), the same one `villa backup` uses. A second implementation
// would be a second opinion about what a clean snapshot is.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/snapshotprune"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

// snapshotDir is where the retained data snapshots live: a dedicated directory
// under villa's data root.
//
// Its own directory, not the models volume and not the restore temp dir. Snapshots
// are gigabytes of user data that must survive the process that wrote them, so a
// temp dir would be wrong; and they must never accumulate into the model store,
// which `update` is explicitly not responsible for.
func snapshotDir() string { return filepath.Join(pathsafe.DataRoot(), "update-snapshots") }

// snapshotPath is where one subsystem's snapshot lands.
//
// UNIQUE PER CAPTURE, not a stable per-subsystem name. A stable name would have the
// new export overwrite the retained one at the moment it is written — which is
// before the new update has been proven, and therefore at the exact moment the old
// snapshot is still the only rollback target villa has. Retention is enforced by
// removing the superseded one AFTER a proven commit (internal/snapshotprune), never
// by letting a write clobber it.
//
// The subsystem's volume name leads, so a human listing the directory can tell what
// each file is without consulting the store.
func snapshotPath(k subsystem.Kind, stamp string) string {
	vol, owns := k.StateVolume()
	if !owns {
		return ""
	}
	return filepath.Join(snapshotDir(), vol+"-"+stamp+".tar")
}

// snapshotStamp is the per-capture suffix: a UTC instant in a filename-safe layout
// that sorts chronologically, so `ls` in the snapshot directory reads as a history.
func snapshotStamp(now time.Time) string {
	return now.UTC().Format("20060102T150405Z")
}

// liveSubsystemStop stops every service a subsystem owns, opening the window in
// which its volume can be exported cleanly.
//
// A volume exported from under a running service is a torn copy: `villa backup`
// already stops Open WebUI for exactly this reason, describing the result as "a
// clean SQLite copy". The measured cost is about two seconds for the 2.3 GB Qdrant
// volume, against a restart that was happening anyway.
func liveSubsystemStop(ctx context.Context, sys orchestrate.Systemd, k subsystem.Kind) error {
	_, services := k.Units()
	for _, svc := range services {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := sys.Stop(svc); err != nil {
			return fmt.Errorf("stop %s: %w", svc, err)
		}
	}
	return nil
}

// liveSubsystemStart starts every service a subsystem owns, closing the window.
//
// It runs on EVERY path out of the stopped window, including the failing ones. The
// stop was villa's, so leaving a subsystem down because the snapshot errored would
// turn a refusal into an outage.
//
// The context is DELIBERATELY ignored, which is why it is named _. Every other seam
// in this file honours cancellation; this one must not, because a Ctrl-C that
// leaves chat stopped is villa's outage rather than the user's. The parameter is
// kept so the signature matches the Deps seam it satisfies.
func liveSubsystemStart(_ context.Context, sys orchestrate.Systemd, k subsystem.Kind) error {
	_, services := k.Units()
	var errs []error
	for _, svc := range services {
		if err := sys.Start(svc); err != nil {
			errs = append(errs, fmt.Errorf("start %s: %w", svc, err))
		}
	}
	return errors.Join(errs...)
}

// liveSnapshotData exports a stateful subsystem's data volume to the retained
// snapshot path, and records what it cost.
//
// It is called ONLY inside the stopped window. An error here REFUSES the update:
// mutating state villa could not snapshot is what the incident was made of, and the
// accepted cost is that a full disk or an unavailable Podman blocks updating a
// stateful subsystem entirely.
func liveSnapshotData(ctx context.Context, k subsystem.Kind) (pinstate.DataSnapshot, error) {
	vol, owns := k.StateVolume()
	if !owns {
		// Unreachable through the flow, which only calls this for a stateful
		// subsystem. Returning an error rather than an empty snapshot keeps a future
		// miswiring loud instead of silently recording "no data was taken".
		return pinstate.DataSnapshot{}, fmt.Errorf("%s owns no persistent state, so there is nothing to snapshot", k)
	}
	if err := requirePodman(); err != nil {
		return pinstate.DataSnapshot{}, err
	}
	if ctx.Err() != nil {
		return pinstate.DataSnapshot{}, ctx.Err()
	}

	// 0700: the snapshot holds the user's chat database and their chat-derived
	// vectors, the same sensitivity `villa restore` already guards its temp dir at.
	if err := os.MkdirAll(snapshotDir(), 0o700); err != nil {
		return pinstate.DataSnapshot{}, fmt.Errorf("create the snapshot directory: %w", err)
	}

	// Export to a SIBLING temp file and rename into place, so a snapshot that dies
	// midway is never mistaken for a complete one. The destination is unique per
	// capture, so this can never clobber the snapshot that is currently the rollback
	// target — that one is removed only after the new update is proven and
	// committed.
	now := time.Now().UTC()
	out := snapshotPath(k, snapshotStamp(now))
	tmp := out + ".partial"
	_ = os.Remove(tmp)
	if stderr, err := podmanVolume(volumeExportArgs(vol, tmp)); err != nil {
		_ = os.Remove(tmp)
		return pinstate.DataSnapshot{}, fmt.Errorf("podman volume export %s: %w: %s", vol, err, stderr)
	}

	info, err := os.Stat(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return pinstate.DataSnapshot{}, fmt.Errorf("stat the snapshot: %w", err)
	}
	if info.Size() == 0 {
		// A zero-byte tar is not a rollback target. Podman exited clean, so nothing
		// else would have noticed — which is exactly the class of failure that only
		// shows up when the snapshot is needed.
		_ = os.Remove(tmp)
		return pinstate.DataSnapshot{}, fmt.Errorf("the exported %s volume is empty, so it is not a usable rollback target", vol)
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return pinstate.DataSnapshot{}, fmt.Errorf("retain the snapshot: %w", err)
	}

	return pinstate.DataSnapshot{
		Volume:  vol,
		Path:    out,
		Bytes:   info.Size(),
		TakenAt: now.Format(time.RFC3339),
	}, nil
}

// liveRestoreData imports a data snapshot back into the subsystem's volume,
// REPLACING what is there.
//
// `podman volume import` MERGES into existing contents and does NOT auto-create the
// volume — `internal/backup/restore.go` already records both facts. A naive import
// over a migrated volume therefore leaves a hybrid of the old and new schemas,
// which is worse than either: the migrated volume is at least self-consistent, and
// a hybrid fails only once something reads the wrong half.
//
// So this clones the proven clean-recreate ordering rather than inventing one:
//
//	rm (not-found tolerant) → create → import
//
// It is called ONLY while the subsystem is stopped. `podman volume rm` fails on a
// volume a running container holds, so a live service would turn the rollback into
// a failure at its most dangerous moment.
func liveRestoreData(ctx context.Context, k subsystem.Kind, snap pinstate.DataSnapshot) error {
	if !snap.Taken() {
		return fmt.Errorf("%s has no recorded data snapshot to restore", k)
	}
	if err := requirePodman(); err != nil {
		return err
	}

	// SURFACE a snapshot that has gone missing rather than fail-softing it. Someone
	// clearing disk by hand has removed the rollback target, and continuing would
	// clean-recreate the volume and import nothing — deleting the user's data in the
	// name of restoring it.
	info, err := os.Stat(snap.Path)
	if err != nil {
		return fmt.Errorf("the recorded %s snapshot is not readable at %s, so the data cannot be restored: %w", k, snap.Path, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("the recorded %s snapshot at %s is empty, so restoring it would discard the current data", k, snap.Path)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// The volume the SNAPSHOT came from, not one re-derived from today's
	// declaration. If a future change moved a subsystem's volume, importing into the
	// new name would restore nothing and leave the old data orphaned.
	vol := snap.Volume
	if vol == "" {
		return fmt.Errorf("the recorded %s snapshot names no volume, so villa cannot tell where to restore it", k)
	}

	// (1) REMOVE, tolerating an already-absent volume: clean-recreate is idempotent,
	// and `podman volume rm` has no tolerance flag, so the not-found stderr is
	// inspected exactly as the restore path already does.
	if stderr, err := podmanVolume(volumeRmArgs(vol)); err != nil && !isVolumeNotFound(stderr) {
		return fmt.Errorf("podman volume rm %s: %w: %s", vol, err, stderr)
	}

	// (2) CREATE explicitly, because import does not auto-create.
	if stderr, err := podmanVolume([]string{"volume", "create", vol}); err != nil && !isVolumeAlreadyExists(stderr) {
		return fmt.Errorf("podman volume create %s: %w: %s", vol, err, stderr)
	}

	// (3) IMPORT into the now-empty volume, so this is a replace rather than a
	// merge.
	if stderr, err := podmanVolume(volumeImportArgs(vol, snap.Path)); err != nil {
		return fmt.Errorf("podman volume import %s: %w: %s", vol, err, stderr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cleanup: the only step in this project that deletes a data snapshot
// ---------------------------------------------------------------------------

// runSnapshotCleanup releases superseded data snapshots after a committed update.
//
// A FAILED REMOVAL IS A WARN, NEVER A ROLLBACK. Identical reasoning to the image
// prune: cleanup runs after the post-mutation proof has already passed, so the
// update has succeeded before cleanup is attempted. Rolling back a proven-good
// update because a cleanup step failed would be perverse, and the failure leaves
// MORE safety, not less — an orphaned snapshot is an inert leftover.
//
// It never runs on a rolled-back or halted subsystem. That is where the live
// rollback target lives, and removing a snapshot the stack was just restored from
// would delete the evidence the restore depended on.
func runSnapshotCleanup(w io.Writer, res updateflow.Result) {
	state, err := pinstate.Load(livePinStateDeps())
	known := err == nil
	if err != nil {
		state = pinstate.State{}
	}

	for _, s := range cleanable(res) {
		plan := snapshotprune.Decide(snapshotprune.Input{
			State:      state,
			StateKnown: known,
			Superseded: s.SupersededSnapshot,
			Subsystem:  s.Subsystem,
			Present:    liveSnapshotPresent,
		})
		printSnapshotCleanupPlan(w, s.Subsystem, plan)
	}
}

// cleanable is the subsystems whose superseded snapshots may be considered.
//
// ONLY the committed ones. A rolled-back subsystem's snapshot is the data it was
// just RESTORED from, and an untried one has none. It is a named function rather
// than a condition inside the loop so a test can assert the selection directly — a
// test that re-derived the rule would pass against a version that had it backwards.
func cleanable(res updateflow.Result) []updateflow.SubsystemResult {
	var out []updateflow.SubsystemResult
	for _, s := range res.Subsystems {
		if s.Outcome.Committed() && !s.RollbackIncomplete {
			out = append(out, s)
		}
	}
	return out
}

// liveSnapshotPresent reports whether a recorded snapshot is still on disk.
//
// A failure to ask is treated as PRESENT, not absent. Reporting "your rollback
// snapshot is gone" because a stat momentarily failed would be a false alarm about
// a safety property, and a false alarm about safety is worse than silence: it
// teaches the user to ignore the real one.
func liveSnapshotPresent(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// A clean, confident "no".
			return false
		}
		// A permission error or a transient failure. Not evidence of absence.
		return true
	}
	// A zero-byte file is not a snapshot. Treating it as present would report
	// rollback protection villa cannot actually provide.
	return info.Size() > 0
}

// printSnapshotCleanupPlan acts on a plan and narrates it.
//
// EVERY outcome is printed, including the no-ops. Disk that silently did not come
// back looks like a bug, and a retained snapshot the user cannot account for is
// exactly the sort of thing that gets deleted by hand.
func printSnapshotCleanupPlan(w io.Writer, k subsystem.Kind, plan snapshotprune.Plan) {
	if plan.Blocked {
		fmt.Fprintf(w, "\n  releasing previous %s snapshot .................. skipped\n", k)
		fmt.Fprintf(w, "    %s\n", plan.BlockedReason)
		return
	}

	for _, d := range plan.Decisions {
		switch d.Action {
		case snapshotprune.Retain:
			fmt.Fprintf(w, "\n  releasing previous %s snapshot .................. retained\n", k)
			fmt.Fprintf(w, "    %s — %s\n", filepath.Base(d.Path), d.Reason)

		case snapshotprune.Missing:
			// Surfaced, not fail-softed: rollback protection is incomplete and the
			// user is the only one who can decide what to do about it.
			fmt.Fprintf(w, "\n  WARNING: data rollback protection for %s is incomplete.\n", d.Subsystem)
			fmt.Fprintf(w, "    %s is %s\n", filepath.Base(d.Path), d.Reason)

		case snapshotprune.Remove:
			if err := os.Remove(d.Path); err != nil && !os.IsNotExist(err) {
				// A WARN. The update succeeded and is running; villa merely could
				// not reclaim the disk. It must not read as a failed update.
				fmt.Fprintf(w, "\n  releasing previous %s snapshot .................. WARN\n", k)
				fmt.Fprintf(w, "    could not remove %s: %v\n"+
					"    The update itself succeeded and is running normally. The snapshot is still\n"+
					"    on disk, which is harmless — it is simply disk villa could not reclaim.\n",
					d.Path, err)
				continue
			}
			fmt.Fprintf(w, "\n  releasing previous %s snapshot .................. removed %s\n", k, humanBytes(d.Bytes))
			fmt.Fprintf(w, "    %s — %s\n", filepath.Base(d.Path), d.Reason)
		}
	}
}

// liveSnapshotSizes reports what a snapshot of each stateful subsystem's volume
// would cost, so `--dry-run` can state it BEFORE it is spent.
//
// Measured on real hardware: memory's volume is 2.8 GB and chat's is under 300 MB.
// On a small disk that is a decision input, and discovering it afterwards is
// discovering it too late.
//
// It reads the volume's MOUNTPOINT and walks it, rather than parsing `podman system
// df -v`, because that command reports every volume on the host in a
// human-formatted table — a second parser for a display format, when the size villa
// needs is a property of a directory it can measure directly.
//
// A subsystem villa cannot measure is OMITTED rather than reported as zero. Zero is
// a claim about a cost, and "villa could not tell" is not that claim.
func liveSnapshotSizes() map[subsystem.Kind]int64 {
	if err := requirePodman(); err != nil {
		return nil
	}
	out := map[subsystem.Kind]int64{}
	for _, k := range subsystem.Stateful() {
		vol, owns := k.StateVolume()
		if !owns {
			continue
		}
		mount, err := volumeMountpoint(vol)
		if err != nil {
			continue
		}
		size, err := dirSize(mount)
		if err != nil {
			continue
		}
		out[k] = size
	}
	return out
}

// volumeMountpoint resolves where podman keeps a named volume's data.
func volumeMountpoint(vol string) (string, error) {
	var buf bytes.Buffer
	// FIXED ARGS, never a shell. The format string is a compiled-in constant and
	// the volume name comes from the subsystem declaration, so nothing here is
	// caller-supplied.
	cmd := exec.Command("podman", "volume", "inspect", vol, "--format", "{{.Mountpoint}}")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", err
	}
	mount := strings.TrimSpace(buf.String())
	if mount == "" {
		return "", fmt.Errorf("podman reported no mountpoint for volume %s", vol)
	}
	return mount, nil
}

// dirSize sums the apparent size of every regular file under a directory.
//
// Apparent size, not disk usage: the snapshot is a tar, and a tar carries the file
// contents rather than their block allocation. A sparse or compressed filesystem
// would make the on-disk figure smaller than the tar villa is about to write, which
// is the wrong direction to be wrong in when the number is a disk-space warning.
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			// A file that vanished mid-walk, or one villa cannot read. Skip it
			// rather than abandoning the estimate: an approximate cost is far more
			// useful than none.
			return nil //nolint:nilerr // a partial estimate beats no estimate
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// humanBytes renders a snapshot size the way a person reads a disk cost.
//
// Base 1000, matching what `df` and the disk vendors report, because the number is
// compared against free space rather than against a memory allocation.
func humanBytes(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.0f MB", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0f kB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}
