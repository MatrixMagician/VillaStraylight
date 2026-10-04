package evalstore

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/eval"
)

// memory is a buffer-backed store seam that counts writes.
type memory struct {
	data   []byte
	writes int
}

func (m *memory) deps() Deps {
	return Deps{
		ReadAll:  func() ([]byte, error) { return m.data, nil },
		WriteAll: func(b []byte) error { m.writes++; m.data = append([]byte(nil), b...); return nil },
	}
}

// baselineFor builds an eval baseline for model with one result of status s.
func baselineFor(model string, s eval.CaseStatus) eval.Baseline {
	return eval.Baseline{
		Key:        eval.Key{Model: model, Quant: "Q4", SuiteVersion: 1},
		Provenance: eval.Provenance{Backend: "rocm", ImageDigest: "sha256:aaa", Speculation: "off", Ctx: 8192},
		Results:    []eval.Result{{CaseID: "a", Status: s, Excerpt: "x"}},
	}
}

// TestPutKeepsOneEvalBaselinePerKey guards ADR-0018's store: one document holds
// every model's eval baseline, at most one per key; a Put for a new key adds one, a
// Put for a recorded key replaces it and hands back the one it replaced, and the
// document is stamped with this store's schema version.
func TestPutKeepsOneEvalBaselinePerKey(t *testing.T) {
	m := &memory{}
	if prior, err := Put(m.deps(), baselineFor("a", eval.Failed)); err != nil || prior != nil {
		t.Fatalf("first Put = %v, %v; want no prior and no error", prior, err)
	}
	if _, err := Put(m.deps(), baselineFor("b", eval.Passed)); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	prior, err := Put(m.deps(), baselineFor("a", eval.Passed))
	if err != nil || prior == nil || prior.Results[0].Status != eval.Failed {
		t.Fatalf("replacing Put = %+v, %v; want the replaced baseline back", prior, err)
	}

	doc, err := Load(m.deps())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc.SchemaVersion != SchemaVersion() || len(doc.Baselines) != 2 {
		t.Fatalf("doc = %+v, want schema %d and two baselines", doc, SchemaVersion())
	}
	got := doc.Find(eval.Key{Model: "a", Quant: "Q4", SuiteVersion: 1})
	if got == nil || got.Results[0].Status != eval.Passed || got.Provenance.ImageDigest != "sha256:aaa" {
		t.Errorf("Find(a) = %+v, want the replacing baseline with its provenance", got)
	}
	if doc.Find(eval.Key{Model: "a", Quant: "Q4", SuiteVersion: 2}) != nil {
		t.Error("a baseline was found under another suite version")
	}
}

// TestLoadFailsClosedToNoEvalBaseline: an absent, corrupt or future-schema store
// loads as empty, which the comparison reports as "no eval baseline" (Reject), never
// as a fabricated pass.
func TestLoadFailsClosedToNoEvalBaseline(t *testing.T) {
	for name, data := range map[string]string{
		"absent":  "",
		"corrupt": "{not json",
		"future":  `{"baselines":[],"schema_version":99}`,
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := Load((&memory{data: []byte(data)}).deps())
			if err != nil || len(doc.Baselines) != 0 {
				t.Fatalf("Load = %+v, %v; want an empty document", doc, err)
			}
		})
	}
}

// TestPutRefusesToOverwriteWhatItCannotRead guards the baselines against data loss
// (ADR-0018: a baseline cannot be re-recorded after the regression it exists to
// catch). A store this villa cannot read, or a read that errors, refuses the Put and
// writes nothing, rather than replacing every recorded baseline with one.
func TestPutRefusesToOverwriteWhatItCannotRead(t *testing.T) {
	for name, data := range map[string]string{
		"corrupt": "{not json",
		"future":  `{"baselines":[],"schema_version":99}`,
	} {
		t.Run(name, func(t *testing.T) {
			m := &memory{data: []byte(data)}
			if _, err := Put(m.deps(), baselineFor("a", eval.Passed)); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
				t.Fatalf("Put err = %v, want a refusal", err)
			}
			if m.writes != 0 || string(m.data) != data {
				t.Errorf("the unreadable store was written (%d writes)", m.writes)
			}
		})
	}

	readErr := Deps{
		ReadAll:  func() ([]byte, error) { return nil, errors.New("permission denied") },
		WriteAll: func([]byte) error { t.Error("written after a read error"); return nil },
	}
	if _, err := Put(readErr, baselineFor("a", eval.Passed)); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Put err = %v, want the read error", err)
	}

	flaky := 0
	second := Deps{
		ReadAll: func() ([]byte, error) {
			flaky++
			if flaky == 1 {
				return []byte("{not json"), nil
			}
			return nil, errors.New("gone")
		},
		WriteAll: func([]byte) error { t.Error("written after an unreadable store"); return nil },
	}
	if _, err := Put(second, baselineFor("a", eval.Passed)); err == nil {
		t.Fatal("Put wrote over a store whose second read failed")
	}
}

// TestPutReportsAFailedWrite: a write error is returned, so `villa eval --record`
// never reports a baseline it did not persist.
func TestPutReportsAFailedWrite(t *testing.T) {
	d := Deps{
		ReadAll:  func() ([]byte, error) { return nil, nil },
		WriteAll: func([]byte) error { return errors.New("disk full") },
	}
	if _, err := Put(d, baselineFor("a", eval.Passed)); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Put err = %v, want the write error", err)
	}
}

// TestStoreIsTheEvalBaselinesDocument pins the on-disk name under the villa data
// root, which the backup entry and the docs name, and the wire field names of the
// document.
func TestStoreIsTheEvalBaselinesDocument(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	if got, want := Path(), filepath.Join(root, "villa", "eval-baselines.json"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
	if err := WriteFileAtomic(Path(), []byte("{}")); err != nil {
		t.Errorf("WriteFileAtomic under the data root: %v", err)
	}

	m := &memory{}
	if _, err := Put(m.deps(), baselineFor("a", eval.Passed)); err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(m.data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"baselines", "schema_version"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("document has no %q field: %s", k, m.data)
		}
	}
}
