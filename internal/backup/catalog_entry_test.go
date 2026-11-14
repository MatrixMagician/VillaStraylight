package backup

// catalog_entry_test.go guards catalog-local.json as the registry's newest file row
// (ADR-0020, ADR-0041): the catalog `villa catalog add` manages is archived when it
// exists, restored byte for byte before the stack is restarted, rolled back verbatim,
// and a v5 archive that predates it still restores. Losing the file leaves config.toml
// naming an operator model no catalog carries, and every stack apply then fails closed.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

const catalogDest = "/cfg/villa/catalog-local.json"

// catalogArchive assembles a verified archive holding config.toml, the owui volume and
// a catalog-local.json entry.
func catalogArchive(t *testing.T, doc []byte) []byte {
	t.Helper()
	data := []archiveEntry{
		{name: EntryConfig, data: validCfgTOML},
		{name: EntryOpenWebUIVolume, data: []byte("owui-data")},
		{name: EntryCatalogLocal, data: doc},
	}
	members := append([]archiveEntry{{name: EntryManifest, data: manifestJSONFor(t, data)}}, data...)
	return rawMultiTar(t, members)
}

func catalogInput(t *testing.T, arch, prior []byte) (*recDeps, RestoreInput) {
	t.Helper()
	r, in := baseInput(t, arch)
	in.Dests[EntryCatalogLocal] = catalogDest
	if prior != nil {
		r.readFile[catalogDest] = prior
	}
	return r, in
}

// TestBackupArchivesTheManagedCatalog: an offered catalog-local.json is the LAST
// member (registry order), checksummed, under manifest schema 6; an absent file is
// skipped like any optional entry, so a host that never added a model archives
// exactly what it did before.
func TestBackupArchivesTheManagedCatalog(t *testing.T) {
	doc := []byte(`{"schema_version":5,"catalog_version":"x","models":[]}`)
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files[catalogDest] = doc

	var out bytes.Buffer
	in := baseBackupInput(&out)
	in.Sources[EntryCatalogLocal] = catalogDest
	if res, err := Backup(f.deps(), in); err != nil {
		t.Fatalf("Backup: %v (%+v)", err, res)
	}
	names := archiveNames(t, out.Bytes())
	if names[len(names)-1] != EntryCatalogLocal {
		t.Fatalf("catalog-local.json must be the last member (registry order), got %v", names)
	}
	m := manifestFromArchive(t, out.Bytes())
	if m.SchemaVersion != 6 {
		t.Fatalf("manifest schema = %d, want 6", m.SchemaVersion)
	}
	var recorded bool
	for _, e := range m.Entries {
		recorded = recorded || (e.Name == EntryCatalogLocal && e.SHA256 != "")
	}
	if !recorded {
		t.Fatalf("catalog-local.json has no checksum in %+v", m.Entries)
	}

	delete(f.files, catalogDest)
	out.Reset()
	if res, err := Backup(f.deps(), in); err != nil {
		t.Fatalf("an absent catalog-local.json must be skipped, got %v (%+v)", err, res)
	}
	for _, n := range archiveNames(t, out.Bytes()) {
		if n == EntryCatalogLocal {
			t.Fatal("an absent catalog-local.json must not be archived")
		}
	}
}

// TestRestoreWritesTheCatalogVerbatimBeforeTheRestart: the archive's bytes land at the
// destination, and they land before the stack restarts, because the restart renders
// units from the restored config and resolves its model through this very file.
func TestRestoreWritesTheCatalogVerbatimBeforeTheRestart(t *testing.T) {
	archived := []byte(`{"schema_version":5,"models":[{"id":"mine"}]}` + "\n")
	r, in := catalogInput(t, catalogArchive(t, archived), []byte("old catalog"))

	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
	}
	if got := r.written[catalogDest]; len(got) != 1 || !bytes.Equal(got[0], archived) {
		t.Fatalf("the archive's catalog must be written verbatim and once, got %q", got)
	}
	if o := res.Files[EntryCatalogLocal]; !o.Restored || o.Skipped {
		t.Fatalf("catalog-local.json outcome = %+v, want restored", o)
	}
	write, restart := indexOf(r.calls, "WriteFileAtomic:"+catalogDest), -1
	for i, c := range r.calls {
		if strings.HasPrefix(c, "ReconcileAndWrite:") || strings.HasPrefix(c, "Start:") {
			restart = i
			break
		}
	}
	if write == -1 || restart == -1 || write > restart {
		t.Fatalf("the catalog must be written before the restart, calls %v", r.calls)
	}
}

// TestRestoreRollsTheCatalogBackVerbatim: a failed cutover rewrites the replaced file
// byte for byte, or removes the one the forward path created.
func TestRestoreRollsTheCatalogBackVerbatim(t *testing.T) {
	t.Run("prior file rewritten", func(t *testing.T) {
		prior := []byte("the operator's catalog")
		r, in := catalogInput(t, catalogArchive(t, []byte("archived")), prior)
		r.prove = prove.Verdict{Status: prove.StatusFail}
		res := Restore(r.deps(), in)
		if !res.RolledBack {
			t.Fatalf("want RolledBack, got %+v", res)
		}
		got := r.written[catalogDest]
		if len(got) != 2 || !bytes.Equal(got[0], []byte("archived")) || !bytes.Equal(got[1], prior) {
			t.Fatalf("want the archive's file then the verbatim prior bytes, got %q", got)
		}
	})
	t.Run("no prior file removed", func(t *testing.T) {
		r, in := catalogInput(t, catalogArchive(t, []byte("archived")), nil)
		r.prove = prove.Verdict{Status: prove.StatusFail}
		if res := Restore(r.deps(), in); !res.RolledBack {
			t.Fatalf("want RolledBack, got %+v", res)
		}
		if indexOf(r.calls, "RemoveFile:"+catalogDest) == -1 {
			t.Fatalf("rollback must remove the forward-created catalog, calls %v", r.calls)
		}
	})
}

// TestRestoreV5ArchiveStillRestoresAndLeavesTheCatalogAlone: schema 6 only widens the
// contract, so a v5 archive (no catalog entry) restores and does not touch a catalog
// already on the host.
func TestRestoreV5ArchiveStillRestoresAndLeavesTheCatalogAlone(t *testing.T) {
	m := baseManifest()
	m.SchemaVersion = 5
	arch := buildArchive(t, m, validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := catalogInput(t, arch, []byte("on the host"))
	if res := Restore(r.deps(), in); !res.Restored {
		t.Fatalf("a v5 archive must still restore, got %+v", res)
	}
	if _, touched := r.written[catalogDest]; touched || indexOf(r.calls, "RemoveFile:"+catalogDest) != -1 {
		t.Fatalf("an archive without the entry must leave the catalog alone, calls %v", r.calls)
	}
}

// TestRestoreSkipsTheCatalogWhenNoDestinationIsWired: the archive carries it but this
// install wired no path, which is reported as skipped, never written to "".
func TestRestoreSkipsTheCatalogWhenNoDestinationIsWired(t *testing.T) {
	r, in := baseInput(t, catalogArchive(t, []byte("archived")))
	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v", res)
	}
	if o := res.Files[EntryCatalogLocal]; !o.Skipped || o.Restored {
		t.Fatalf("outcome = %+v, want skipped", o)
	}
}
