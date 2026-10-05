package backup

// registry_test.go guards the ordered entry registry (ADR-0020) and the error
// paths of the table-driven Backup and Restore steps: every failure names the step
// it belongs to, and a file entry the install has no destination for is reported as
// skipped rather than written to an empty path.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// TestRegistryIsTheTarOrder guards the registry as the single source of an
// archive's layout: the names are unique, every row has a label for its error
// messages, the required rows are exactly the two Backup refuses to omit, and the
// file rows are the registry's KindFile rows in order.
func TestRegistryIsTheTarOrder(t *testing.T) {
	seen := map[string]bool{}
	var files []string
	for _, r := range registry {
		if seen[r.Name] {
			t.Fatalf("duplicate registry row %q", r.Name)
		}
		seen[r.Name] = true
		if r.Label == "" {
			t.Errorf("row %q has no Label, so its error messages would name nothing", r.Name)
		}
		if r.Required != (r.Name == EntryOpenWebUIVolume || r.Name == EntryConfig) {
			t.Errorf("row %q Required = %v: only the Open WebUI volume and config.toml are required", r.Name, r.Required)
		}
		if r.Kind == KindFile {
			files = append(files, r.Name)
		}
	}
	var got []string
	for _, r := range fileRows {
		got = append(got, r.Name)
	}
	if strings.Join(got, ",") != strings.Join(files, ",") {
		t.Fatalf("fileRows = %v, want the registry's KindFile rows %v", got, files)
	}
	if registry[0].Name != EntryOpenWebUIVolume || registry[1].Name != EntryConfig {
		t.Fatalf("the archive layout starts volume, config; registry starts %q, %q", registry[0].Name, registry[1].Name)
	}
}

// failReader is an io.ReadCloser whose Read and Close fail on demand.
type failReader struct {
	readErr, closeErr error
	data              io.Reader
}

func (f *failReader) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.data.Read(p)
}

func (f *failReader) Close() error { return f.closeErr }

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// TestBackupFailuresNameTheirStep: each way a source can fail is a Result whose
// FailedStep names the step, and the error text names the entry and path. A
// tolerably-absent OPTIONAL streamed entry is skipped, not an error.
func TestBackupFailuresNameTheirStep(t *testing.T) {
	boom := errors.New("boom")
	stream := func(body string) func(string) (io.ReadCloser, int64, error) {
		return func(string) (io.ReadCloser, int64, error) {
			return &failReader{data: strings.NewReader(body)}, int64(len(body)), nil
		}
	}
	tests := []struct {
		name     string
		mutate   func(in *Input, d *Deps)
		wantStep string
		wantErr  string
	}{
		{"required source path missing",
			func(in *Input, _ *Deps) { delete(in.Sources, EntryConfig) },
			"read", "missing required source path for config.toml"},
		{"hard read error",
			func(_ *Input, d *Deps) { d.ReadFile = func(string) ([]byte, error) { return nil, boom } },
			"read", "backup: read openwebui-volume.tar"},
		{"stream open error",
			func(_ *Input, d *Deps) {
				d.OpenFile = func(string) (io.ReadCloser, int64, error) { return nil, 0, boom }
			},
			"read", "backup: open openwebui-volume.tar"},
		{"stream read error",
			func(_ *Input, d *Deps) {
				d.OpenFile = func(string) (io.ReadCloser, int64, error) {
					return &failReader{readErr: boom}, 0, nil
				}
			},
			"checksum", "backup: checksum openwebui-volume.tar"},
		{"stream close error",
			func(_ *Input, d *Deps) {
				d.OpenFile = func(string) (io.ReadCloser, int64, error) {
					return &failReader{closeErr: boom, data: strings.NewReader("x")}, 1, nil
				}
			},
			"checksum", "backup: close openwebui-volume.tar"},
		{"archive write error",
			func(in *Input, _ *Deps) { in.OutputWriter = failWriter{} },
			"write", "disk full"},
		{"stop of the second service fails",
			func(in *Input, d *Deps) {
				in.QdrantVolumeName, in.TempQdrantTar = "qdrant-vol", "/tmp/qdrant-vol.tar"
				d.Stop = func(s string) error {
					if s == d.QdrantServiceName {
						return boom
					}
					return nil
				}
			},
			"stop", "backup: stop qdrant.service"},
		{"export of the second volume fails",
			func(in *Input, d *Deps) {
				in.QdrantVolumeName, in.TempQdrantTar = "qdrant-vol", "/tmp/qdrant-vol.tar"
				d.VolumeExport = func(name, _ string) error {
					if name == "qdrant-vol" {
						return boom
					}
					return nil
				}
			},
			"volume", "backup: volume export qdrant-vol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeDeps()
			f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
			d := f.deps()
			d.OpenFile = nil
			in := baseBackupInput(&bytes.Buffer{})
			tt.mutate(&in, &d)
			res, err := Backup(d, in)
			if err == nil {
				t.Fatalf("want an error, got %+v", res)
			}
			if res.FailedStep != tt.wantStep {
				t.Errorf("FailedStep = %q, want %q (err %v)", res.FailedStep, tt.wantStep, err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
			if res.Err != err {
				t.Errorf("Result.Err and the returned error must be the same value")
			}
		})
	}

	t.Run("an absent optional streamed volume is skipped", func(t *testing.T) {
		f := newFakeDeps()
		f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
		d := f.deps()
		d.OpenFile = func(path string) (io.ReadCloser, int64, error) {
			if path == "/tmp/qdrant-vol.tar" {
				return nil, 0, os.ErrNotExist
			}
			return stream("OWUI")(path)
		}
		var out bytes.Buffer
		in := baseBackupInput(&out)
		in.QdrantVolumeName, in.TempQdrantTar = "qdrant-vol", "/tmp/qdrant-vol.tar"
		if res, err := Backup(d, in); err != nil {
			t.Fatalf("a tolerably-absent optional entry must not fail the backup: %v (%+v)", err, res)
		}
		for _, n := range archiveNames(t, out.Bytes()) {
			if n == EntryQdrantVolume {
				t.Fatalf("absent qdrant tar must be skipped, archive has %v", archiveNames(t, out.Bytes()))
			}
		}
	})
}

// TestRestoreVolumeSwapStepFailureNamesTheStep: each of the four clean-recreate
// steps, failing on the forward path, rolls back at "volume" and names its step in
// the error — the rollback then retries the same step, so the restore is reported
// rolled back, never restored.
func TestRestoreVolumeSwapStepFailureNamesTheStep(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		set     func(r *recDeps)
		wantErr string
	}{
		{"volume rm", func(r *recDeps) { r.volumeRmErr = boom }, "volume rm villa-openwebui"},
		{"reconcile", func(r *recDeps) { r.reconcileErr = boom }, "reconcile/recreate units"},
		{"ensure volume", func(r *recDeps) { r.ensureVolErr = boom }, "ensure volume villa-openwebui"},
		{"volume import", func(r *recDeps) { r.volumeImportErr = boom }, "volume import villa-openwebui"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
			r, in := baseInput(t, arch)
			tt.set(r)
			res := Restore(r.deps(), in)
			if !res.RolledBack || res.Restored {
				t.Fatalf("want RolledBack, got %+v", res)
			}
			if res.FailedStep != "volume" || res.Err == nil || !strings.Contains(res.Err.Error(), tt.wantErr) {
				t.Fatalf("want a volume failure naming %q, got step %q err %v", tt.wantErr, res.FailedStep, res.Err)
			}
		})
	}
}

// TestRestoreFileEntryWithoutDestinationIsSkipped: an archive that carries a file
// entry the current install has no destination for (its subsystem is off) writes
// NOTHING for it and reports it as Skipped, never as Restored — and an entry the
// archive does not carry has no outcome at all.
func TestRestoreFileEntryWithoutDestinationIsSkipped(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), []byte("usage"), nil, false)
	r, in := baseInput(t, arch)
	delete(in.Dests, EntryUsage)
	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
	}
	if o := res.Files[EntryUsage]; o.Restored || !o.Skipped {
		t.Fatalf("usage.json carried but unwired must be Skipped, got %+v", o)
	}
	if _, carried := res.Files[EntryBenchReports]; carried {
		t.Fatalf("bench-reports.jsonl was not in the archive and must have no outcome, got %+v", res.Files)
	}
	for _, c := range r.calls {
		if strings.HasPrefix(c, "WriteFileAtomic:") {
			t.Fatalf("an unwired destination must make ZERO file writes, got %v", r.calls)
		}
	}
}

// TestRestoreRefusesAnArchiveItCannotRead: an archive that cannot be opened or
// parsed is a fail-closed refusal at "verify" with ZERO side effects — before any
// capture, never a half-applied restore.
func TestRestoreRefusesAnArchiveItCannotRead(t *testing.T) {
	tests := []struct {
		name    string
		open    func() (io.ReadCloser, error)
		wantErr string
	}{
		{"no archive opener", nil, "nil archive opener"},
		{"archive cannot be opened",
			func() (io.ReadCloser, error) { return nil, errors.New("no such file") },
			"open archive"},
		{"archive without a manifest",
			func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(rawMultiTar(t, nil))), nil },
			"has no manifest.json entry"},
		{"not a tar",
			func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(strings.Repeat("junk", 600))), nil
			},
			""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, in := baseInput(t, nil)
			in.OpenArchive = tt.open
			res := Restore(r.deps(), in)
			if !res.Refused || res.FailedStep != "verify" || res.Err == nil {
				t.Fatalf("want Refused at verify, got %+v", res)
			}
			if !strings.Contains(res.Err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", res.Err, tt.wantErr)
			}
			if hasMutate(r.calls) {
				t.Fatalf("an unreadable archive must have ZERO side effects, got %v", r.calls)
			}
		})
	}
}
