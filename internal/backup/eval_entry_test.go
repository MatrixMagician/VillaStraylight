package backup

// eval_entry_test.go guards eval-baselines.json as the registry's newest file row
// (ADR-0020, #275): it is archived when offered and skipped when absent, its schema
// is recorded in the manifest and checked by CompareSkew, and restore replaces the
// document VERBATIM — rolling it back verbatim too — and names every current
// baseline the archive lacks, because a baseline cannot be re-recorded once the
// regression it exists to catch has happened (ADR-0018).

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

const evalDest = "/data/eval-baselines.json"

// lineKeys is the fake document parser: each non-empty line of a "document" is one
// baseline key. The real parser is the cmd tier's, over evalstore.
func lineKeys(doc []byte) []string {
	var keys []string
	for _, l := range strings.Split(string(doc), "\n") {
		if l != "" {
			keys = append(keys, l)
		}
	}
	return keys
}

// evalArchive assembles a verified archive holding config.toml, the owui volume and
// an eval-baselines.json entry.
func evalArchive(t *testing.T, doc []byte) []byte {
	t.Helper()
	data := []archiveEntry{
		{name: EntryConfig, data: validCfgTOML},
		{name: EntryOpenWebUIVolume, data: []byte("owui-data")},
		{name: EntryEvalBaselines, data: doc},
	}
	members := append([]archiveEntry{{name: EntryManifest, data: manifestJSONFor(t, data)}}, data...)
	return rawMultiTar(t, members)
}

// evalInput is a restore input wired to restore eval baselines into evalDest.
func evalInput(t *testing.T, arch []byte, prior []byte) (*recDeps, RestoreInput) {
	t.Helper()
	r, in := baseInput(t, arch)
	in.Dests[EntryEvalBaselines] = evalDest
	in.EvalKeysOf = lineKeys
	if prior != nil {
		r.readFile[evalDest] = prior
	}
	return r, in
}

// TestBackupArchivesEvalBaselines: an offered eval-baselines.json is the LAST
// archive member (registry order), checksummed, with the store's schema version
// recorded in the manifest; an absent file is skipped like any optional entry.
func TestBackupArchivesEvalBaselines(t *testing.T) {
	doc := []byte(`{"baselines":[],"schema_version":1}`)
	f := newFakeDeps()
	f.files["/cfg/config.toml"] = []byte("model = \"x\"\n")
	f.files[evalDest] = doc

	var out bytes.Buffer
	in := baseBackupInput(&out)
	in.Sources[EntryEvalBaselines] = evalDest
	in.EvalSchemaVersion = 1
	if res, err := Backup(f.deps(), in); err != nil {
		t.Fatalf("Backup: %v (%+v)", err, res)
	}
	names := archiveNames(t, out.Bytes())
	if names[len(names)-1] != EntryEvalBaselines {
		t.Fatalf("eval-baselines.json must be the last member (registry order), got %v", names)
	}
	m := manifestFromArchive(t, out.Bytes())
	if m.EvalSchemaVersion != 1 || m.SchemaVersion != 5 {
		t.Fatalf("manifest must record eval schema 1 under backup schema 5, got eval %d schema %d", m.EvalSchemaVersion, m.SchemaVersion)
	}
	var recorded bool
	for _, e := range m.Entries {
		recorded = recorded || e.Name == EntryEvalBaselines
	}
	if !recorded {
		t.Fatalf("eval-baselines.json has no checksum in %+v", m.Entries)
	}

	delete(f.files, evalDest)
	out.Reset()
	if res, err := Backup(f.deps(), in); err != nil {
		t.Fatalf("an absent eval-baselines.json must be skipped, got %v (%+v)", err, res)
	}
	for _, n := range archiveNames(t, out.Bytes()) {
		if n == EntryEvalBaselines {
			t.Fatalf("an absent eval-baselines.json must not be archived")
		}
	}
}

// TestManifestRecordsEvalSchemaOnlyWhenSet: eval_schema_version is omitted at zero
// ("not recorded"), so a manifest built without it is the same document a v4 villa
// wrote, and it round-trips when set.
func TestManifestRecordsEvalSchemaOnlyWhenSet(t *testing.T) {
	without, err := json.Marshal(BuildManifest(ManifestInput{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), "eval_schema_version") {
		t.Fatalf("an unset eval schema must be omitted, got %s", without)
	}
	with, err := json.Marshal(BuildManifest(ManifestInput{EvalSchemaVersion: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), `"eval_schema_version":1`) {
		t.Fatalf("a set eval schema must be recorded, got %s", with)
	}
}

// TestRestoreEvalBaselinesReplaceVerbatimAndNameWhatTheyDrop: the restored document
// is the archive's bytes, unmerged, and the Result names each baseline that existed
// before the restore and is not in the archive — the ones that are now gone.
func TestRestoreEvalBaselinesReplaceVerbatimAndNameWhatTheyDrop(t *testing.T) {
	archived := []byte("m1\nm2\n")
	r, in := evalInput(t, evalArchive(t, archived), []byte("m2\nm3\nm4\n"))

	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v (calls %v)", res, r.calls)
	}
	if got := r.written[evalDest]; len(got) != 1 || !bytes.Equal(got[0], archived) {
		t.Fatalf("the archive's document must be written verbatim and once, got %q", got)
	}
	if strings.Join(res.EvalDropped, ",") != "m3,m4" {
		t.Fatalf("EvalDropped = %v, want [m3 m4] (current baselines the archive lacks)", res.EvalDropped)
	}
	if o := res.Files[EntryEvalBaselines]; !o.Restored || o.Skipped {
		t.Fatalf("eval-baselines.json outcome = %+v, want restored", o)
	}
}

// TestRestoreEvalBaselinesDropNothingWhenArchiveKeepsEveryBaseline: no warning when
// nothing is lost — including a first restore onto an install with no baselines.
func TestRestoreEvalBaselinesDropNothingWhenArchiveKeepsEveryBaseline(t *testing.T) {
	for name, prior := range map[string][]byte{"superset": []byte("m1\n"), "no prior store": nil} {
		t.Run(name, func(t *testing.T) {
			r, in := evalInput(t, evalArchive(t, []byte("m1\nm2\n")), prior)
			res := Restore(r.deps(), in)
			if !res.Restored || len(res.EvalDropped) != 0 {
				t.Fatalf("want Restored with nothing dropped, got %+v", res)
			}
		})
	}
}

// TestRestoreWithoutAnEvalEntryLeavesBaselinesAlone: an archive that does not carry
// eval-baselines.json (a v4 backup, or an install that never recorded one) must not
// touch the current store, and must not warn about baselines it never replaced.
func TestRestoreWithoutAnEvalEntryLeavesBaselinesAlone(t *testing.T) {
	arch := buildArchive(t, baseManifest(), validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := evalInput(t, arch, []byte("m9\n"))
	res := Restore(r.deps(), in)
	if !res.Restored {
		t.Fatalf("want Restored, got %+v", res)
	}
	if _, touched := r.written[evalDest]; touched || indexOf(r.calls, "RemoveFile:"+evalDest) != -1 {
		t.Fatalf("an archive without the entry must leave the store alone, calls %v", r.calls)
	}
	if _, carried := res.Files[EntryEvalBaselines]; carried || len(res.EvalDropped) != 0 {
		t.Fatalf("nothing carried, nothing dropped; got %+v", res)
	}
}

// TestRestoreEvalBaselinesRollBackVerbatim: when the cutover proof fails the
// replaced document is rewritten byte for byte from the capture (so a failed
// restore loses no baseline), and no drop is reported for a restore that did not
// happen. With no prior store, rollback removes the file the forward path created.
func TestRestoreEvalBaselinesRollBackVerbatim(t *testing.T) {
	t.Run("prior store rewritten", func(t *testing.T) {
		prior := []byte("m2\nm3\n")
		r, in := evalInput(t, evalArchive(t, []byte("m1\n")), prior)
		r.prove = prove.Verdict{Status: prove.StatusFail}
		res := Restore(r.deps(), in)
		if !res.RolledBack || res.Restored {
			t.Fatalf("want RolledBack, got %+v", res)
		}
		got := r.written[evalDest]
		if len(got) != 2 || !bytes.Equal(got[0], []byte("m1\n")) || !bytes.Equal(got[1], prior) {
			t.Fatalf("want the archive's document then the verbatim prior bytes, got %q", got)
		}
		if len(res.EvalDropped) != 0 {
			t.Fatalf("a rolled-back restore dropped nothing, got %v", res.EvalDropped)
		}
	})
	t.Run("no prior store removed", func(t *testing.T) {
		r, in := evalInput(t, evalArchive(t, []byte("m1\n")), nil)
		r.prove = prove.Verdict{Status: prove.StatusFail}
		res := Restore(r.deps(), in)
		if !res.RolledBack {
			t.Fatalf("want RolledBack, got %+v", res)
		}
		if indexOf(r.calls, "RemoveFile:"+evalDest) == -1 {
			t.Fatalf("rollback must remove the forward-created store, calls %v", r.calls)
		}
	})
	t.Run("failed forward write", func(t *testing.T) {
		r, in := evalInput(t, evalArchive(t, []byte("m1\n")), []byte("m2\n"))
		r.writeFileErr = errors.New("disk full")
		res := Restore(r.deps(), in)
		if !res.RolledBack || res.FailedStep != "data" || res.Err == nil ||
			!strings.Contains(res.Err.Error(), "restore eval-baselines.json") {
			t.Fatalf("want a data-step rollback naming the entry, got %+v", res)
		}
	})
}

// TestRestoreV4ArchiveStillRestores: schema 5 only widens the contract, so a v4
// archive (no eval entry, no eval schema) stays restorable.
func TestRestoreV4ArchiveStillRestores(t *testing.T) {
	m := baseManifest()
	m.SchemaVersion = 4
	arch := buildArchive(t, m, validCfgTOML, []byte("owui-data"), nil, nil, false)
	r, in := baseInput(t, arch)
	if res := Restore(r.deps(), in); !res.Restored {
		t.Fatalf("a v4 archive must still restore, got %+v", res)
	}
}
