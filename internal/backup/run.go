package backup

// run.go holds RunBackup, the stage→assemble→publish orchestrator that used to
// live inline in cmd/villa's runBackup (#239, ADR-0012): the output-path
// traversal guard, the three same-directory temp files (the archive itself, the
// OpenWebUI volume export, and the optional qdrant volume export), and the
// atomic rename onto the destination ONLY after a fully successful Backup. A
// failed run removes only its own temp files — a pre-existing archive at
// OutputPath is never truncated or deleted. Every effect is a Deps func field
// (CreateTemp/Rename/Remove); RunBackup performs no direct os calls.

import (
	"fmt"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// tempFilePattern names the three same-directory staging files RunBackup
// creates, mirroring the cmd-tier names the prior inline sequence used.
const (
	outputTempPattern = ".villa-backup-*.tar"
	volumeTempPattern = ".villa-owui-vol-*.tar"
	qdrantTempPattern = ".villa-memory-vol-*.tar"
)

// RunBackup resolves in.OutputPath's parent, traversal-guards it, stages the
// archive and any requested volume-export scratch files in that same directory,
// drives the pure Backup orchestrator over them, and — only on full success —
// renames the finished archive onto in.OutputPath. in.OutputWriter,
// in.TempVolumeTar and in.TempQdrantTar are owned by RunBackup: any value the
// caller sets on them is ignored and overwritten. Whether the qdrant entry is
// requested at all is still the caller's decision (in.QdrantVolumeName != "" —
// a subsystem gate the cmd tier resolves from live host state); RunBackup
// decides nothing about WHICH entries to include, only how to stage and publish
// whichever entries the caller asked for.
func RunBackup(d Deps, in Input) (Result, error) {
	parent := filepath.Dir(in.OutputPath)
	if err := pathsafe.Inside(in.OutputPath, parent); err != nil {
		err = fmt.Errorf("backup: output escapes its parent dir: %w", err)
		return Result{Err: err, FailedStep: "path"}, err
	}

	outPath, outFile, err := d.CreateTemp(parent, outputTempPattern)
	if err != nil {
		err = fmt.Errorf("backup: open output temp file in %q: %w", parent, err)
		return Result{Err: err, FailedStep: "stage"}, err
	}
	in.OutputWriter = outFile

	volPath, volFile, err := d.CreateTemp(parent, volumeTempPattern)
	if err != nil {
		_ = outFile.Close()
		_ = d.Remove(outPath) // the prior archive at OutputPath stays untouched
		err = fmt.Errorf("backup: temp volume file: %w", err)
		return Result{Err: err, FailedStep: "stage"}, err
	}
	_ = volFile.Close() // podman writes the path itself; this only reserves the name
	defer func() { _ = d.Remove(volPath) }()
	in.TempVolumeTar = volPath

	if in.QdrantVolumeName != "" {
		qPath, qFile, err := d.CreateTemp(parent, qdrantTempPattern)
		if err != nil {
			_ = outFile.Close()
			_ = d.Remove(outPath)
			err = fmt.Errorf("backup: temp qdrant volume file: %w", err)
			return Result{Err: err, FailedStep: "stage"}, err
		}
		_ = qFile.Close()
		defer func() { _ = d.Remove(qPath) }()
		in.TempQdrantTar = qPath
	}

	res, berr := Backup(d, in)
	if cerr := outFile.Close(); cerr != nil && berr == nil {
		berr = cerr
		res.Err = cerr
		res.FailedStep = "write"
	}
	if berr != nil {
		_ = d.Remove(outPath) // only the torn temp; a prior archive is preserved
		return res, berr
	}

	if rerr := d.Rename(outPath, in.OutputPath); rerr != nil {
		_ = d.Remove(outPath)
		err := fmt.Errorf("backup: publish output %q: %w", in.OutputPath, rerr)
		return Result{Err: err, FailedStep: "publish"}, err
	}
	return res, nil
}
