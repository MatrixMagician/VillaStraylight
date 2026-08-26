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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
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
// Keyed by SUBSYSTEM, not by digest or timestamp, because the retention rule is one
// snapshot per stateful subsystem — the same one-previous rule the images already
// follow. A timestamped name would accumulate silently.
func snapshotPath(k subsystem.Kind) string {
	vol, owns := k.StateVolume()
	if !owns {
		return ""
	}
	return filepath.Join(snapshotDir(), vol+".tar")
}

// liveSubsystemStop stops every service a subsystem owns, opening the window in
// which its volume can be exported cleanly.
//
// A volume exported from under a running service is a torn copy: `villa backup`
// already stops Open WebUI for exactly this reason, describing the result as "a
// clean SQLite copy". The measured cost is about two seconds for the 2.3 GB Qdrant
// volume, against a restart that was happening anyway.
func liveSubsystemStop(ctx context.Context, sys orchestrate.Systemd, k subsystem.Kind) error {
	_, services := subsystemUnits(k)
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
func liveSubsystemStart(ctx context.Context, sys orchestrate.Systemd, k subsystem.Kind) error {
	_, services := subsystemUnits(k)
	var errs []error
	for _, svc := range services {
		// DELIBERATELY not short-circuiting on ctx: a cancelled run must still
		// bring back the services villa stopped. A Ctrl-C that leaves chat down is
		// villa's outage, not the user's.
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
	// midway never displaces the one already retained. A half-written tar at the
	// retained path would be a rollback target that only fails when used.
	out := snapshotPath(k)
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
		TakenAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
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
