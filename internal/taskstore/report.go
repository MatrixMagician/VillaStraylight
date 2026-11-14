package taskstore

// report.go is the research task's report artifact (ADR-0045): one Markdown
// file per task beside its record and log, tasks/<id>.report.md. It is not a
// record, so List and Recover (which read only <id>.json) never see it, and like
// the record it is never deleted.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// reportRel is the report's path relative to the data root, which is what the
// record stores.
func reportRel(id string) string { return "tasks/" + id + ".report.md" }

// ReportPath resolves a record's relative report path against this store's
// data root, for the terminal to print and read.
func (s Store) ReportPath(rel string) string { return filepath.Join(s.root, filepath.FromSlash(rel)) }

// WriteReport writes the task's report atomically at mode 0600 and returns its
// path relative to the data root. The id is shape-checked first, so an id from
// an HTTP path segment cannot steer the write (GHSA-9hj4).
func (s Store) WriteReport(id string, data []byte) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err := pathsafe.AssertRoot(s.root); err != nil {
		return "", fmt.Errorf("taskstore: refusing an unusable data root: %w", err)
	}
	if err := s.fs.mkdirAll(s.tasksDir()); err != nil {
		return "", fmt.Errorf("taskstore: mkdir tasks dir: %w", err)
	}
	rel := reportRel(id)
	if err := pathsafe.WriteFileAtomic(s.root, s.ReportPath(rel), data, 0o600); err != nil {
		return "", fmt.Errorf("taskstore: write report %s: %w", id, err)
	}
	return rel, nil
}

// ReadReport returns the task's report. A task with no report, or an id that
// is not a task id, is ErrNotFound.
func (s Store) ReadReport(id string) ([]byte, error) {
	if !validID(id) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	data, err := s.fs.readFile(s.ReportPath(reportRel(id)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: no report for %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("taskstore: read report %s: %w", id, err)
	}
	return data, nil
}
