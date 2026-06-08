package backup

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
)

// TestTarWriteReadRoundTrip asserts writeArchive → readArchive round-trips the
// entries in order (manifest.json FIRST) with their bytes intact (D-03).
func TestTarRoundTrip(t *testing.T) {
	entries := []archiveEntry{
		{name: EntryManifest, data: []byte(`{"schema_version":1}`)},
		{name: EntryConfig, data: []byte("model = \"x\"\n")},
		{name: EntryBenchReports, data: []byte("{}\n")},
	}
	var buf bytes.Buffer
	if err := writeArchive(&buf, entries); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}

	var gotNames []string
	got := map[string][]byte{}
	if err := readArchive(&buf, func(name string, data []byte) error {
		gotNames = append(gotNames, name)
		got[name] = data
		return nil
	}); err != nil {
		t.Fatalf("readArchive: %v", err)
	}

	if len(gotNames) != len(entries) {
		t.Fatalf("read %d entries, want %d", len(gotNames), len(entries))
	}
	// manifest.json must be FIRST (deterministic order preserved).
	if gotNames[0] != EntryManifest {
		t.Fatalf("first entry = %q, want %q", gotNames[0], EntryManifest)
	}
	for _, e := range entries {
		if !bytes.Equal(got[e.name], e.data) {
			t.Errorf("entry %q bytes = %q, want %q", e.name, got[e.name], e.data)
		}
	}
}

// TestTarSlipRefusesTraversal asserts readArchive refuses a tar entry whose name
// escapes the extraction dir via "../" — the tar-slip guard (D-11, T-16-01a),
// BEFORE invoking the callback.
func TestTarSlipRefusesTraversal(t *testing.T) {
	buf := rawTar(t, "../escape", []byte("evil"))
	called := false
	err := readArchive(buf, func(name string, data []byte) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatalf("readArchive accepted a ../ traversal entry")
	}
	if called {
		t.Fatalf("callback ran for a traversal entry — guard must fire BEFORE fn")
	}
}

// TestTarSlipRefusesAbsolute asserts readArchive refuses an absolute-path tar
// entry (D-11, T-16-01a).
func TestTarSlipRefusesAbsolute(t *testing.T) {
	buf := rawTar(t, "/etc/passwd", []byte("evil"))
	called := false
	err := readArchive(buf, func(name string, data []byte) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatalf("readArchive accepted an absolute-path entry")
	}
	if called {
		t.Fatalf("callback ran for an absolute-path entry — guard must fire BEFORE fn")
	}
}

// TestTarSlipAllowsInDir asserts an ordinary in-dir entry passes the guard.
func TestTarSlipAllowsInDir(t *testing.T) {
	buf := rawTar(t, "sub/dir/file.txt", []byte("ok"))
	called := false
	if err := readArchive(buf, func(name string, data []byte) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("readArchive refused a legitimate in-dir entry: %v", err)
	}
	if !called {
		t.Fatalf("callback did not run for a legitimate entry")
	}
}

// TestReadArchiveEntryCountCapRefuses asserts readArchive refuses an archive with
// more than maxEntryCount members (WR-04 entry-count bound) before exhausting it.
func TestReadArchiveEntryCountCapRefuses(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for i := 0; i < maxEntryCount+1; i++ {
		name := "e" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		body := []byte("x")
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Format: tar.FormatPAX}); err != nil {
			t.Fatalf("write header %d: %v", i, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write body %d: %v", i, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err := readArchive(&buf, func(name string, data []byte) error { return nil })
	if err == nil {
		t.Fatalf("readArchive accepted an archive exceeding the %d-entry cap", maxEntryCount)
	}
	if !strings.Contains(err.Error(), "more than") {
		t.Fatalf("want an entry-count-cap error, got %v", err)
	}
}

// rawTar builds a one-entry tar with an arbitrary (possibly hostile) name,
// bypassing writeArchive's clean-name path so the reader's guard is exercised.
func rawTar(t *testing.T, name string, data []byte) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Format: tar.FormatPAX}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return &buf
}
