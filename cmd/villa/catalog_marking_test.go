package main

// catalog_marking_test.go guards the operator-supplied marking on the three surfaces
// the maintainer named (ADR-0041): `villa model list`, `villa status` and `villa doctor`.
// The marking is derived from the embedded seed, so every case here starts from a
// catalog the seed does or does not recognise and asserts what each surface says.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/catalogadd"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// seedPlusMine is the embedded seed with one operator-supplied entry and one seed entry
// edited by hand, the three origins in one catalog.
func seedPlusMine(t *testing.T) (cat catalog.Catalog, seedID, editedID string) {
	t.Helper()
	cat, err := catalog.Seed()
	if err != nil {
		t.Fatal(err)
	}
	mine := cat.Models[0]
	mine.ID = "my-model"
	cat.Models[1].DefaultCtx++
	cat.Models = append(cat.Models, mine)
	return cat, cat.Models[2].ID, cat.Models[1].ID
}

func listWith(cat catalog.Catalog, model string) *listDeps {
	return &listDeps{
		loadCatalog: func() (catalog.Catalog, []string, error) { return cat, nil, nil },
		loadConfig:  func() (config.VillaConfig, error) { return config.VillaConfig{Model: model}, nil },
	}
}

// TestModelListMarksOperatorEntries: the table says which entries the operator
// supplied or edited, and a seed entry's row is unchanged.
func TestModelListMarksOperatorEntries(t *testing.T) {
	cat, seedID, editedID := seedPlusMine(t)
	cmd, out, _ := newTestCmd()
	if code := runModelList(cmd, listOpts{}, listWith(cat, "my-model")); code != exitPass {
		t.Fatalf("exit %d", code)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		f := strings.Fields(line)
		rows[f[1]] = line
	}
	if !strings.Contains(rows["my-model"], "loaded") || !strings.Contains(rows["my-model"], "(operator-supplied)") {
		t.Errorf("operator row = %q", rows["my-model"])
	}
	if !strings.Contains(rows[editedID], "(operator-edited)") {
		t.Errorf("edited row = %q", rows[editedID])
	}
	if strings.Contains(rows[seedID], "(") {
		t.Errorf("a seed row must carry no marker, got %q", rows[seedID])
	}
}

// TestModelListJSONMarksOperatorEntriesAppendOnly: `origin` is a new key that exists
// only on an operator entry. The output is a bare array with no schema field, so
// omission is what keeps every existing consumer, and every seed row, byte-identical.
func TestModelListJSONMarksOperatorEntriesAppendOnly(t *testing.T) {
	cat, seedID, _ := seedPlusMine(t)
	cmd, out, _ := newTestCmd()
	if code := runModelList(cmd, listOpts{asJSON: true}, listWith(cat, "")); code != exitPass {
		t.Fatalf("exit %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, r := range rows {
		byID[r["id"].(string)] = r
	}
	if byID["my-model"]["origin"] != "operator-supplied" {
		t.Errorf("operator entry = %v", byID["my-model"])
	}
	if _, has := byID[seedID]["origin"]; has {
		t.Errorf("a seed entry must not carry origin: %v", byID[seedID])
	}
	if len(byID[seedID]) != 3 {
		t.Errorf("a seed entry keeps exactly id, quant and loaded: %v", byID[seedID])
	}
}

// TestModelListOnTheSeedIsUnchanged: the default catalog lists byte-for-byte as it did.
func TestModelListOnTheSeedIsUnchanged(t *testing.T) {
	seed, _ := catalog.Seed()
	cmd, out, _ := newTestCmd()
	if code := runModelList(cmd, listOpts{asJSON: true}, listWith(seed, "")); code != exitPass {
		t.Fatalf("exit %d", code)
	}
	if bytes.Contains(out.Bytes(), []byte("origin")) {
		t.Fatalf("the embedded seed's JSON listing gained a key: %s", out.String())
	}
}

// TestLiveModelOriginReadsTheConfiguredCatalog: the status seam resolves the model
// through catalog_path and answers with the label, "" for a seed model, an unknown
// model and an unreadable catalog.
func TestLiveModelOriginReadsTheConfiguredCatalog(t *testing.T) {
	cfgDir, _, _ := scratchHost(t)
	_ = cfgDir
	if code, _, e := runAdd(t, addURL, catalogadd.Overrides{}); code != exitPass {
		t.Fatalf("add: %d %s", code, e)
	}
	cfg, err := config.LoadVilla()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Model = addID
	if got := liveModelOrigin(cfg); got != "operator-supplied" {
		t.Fatalf("origin of the added model = %q", got)
	}
	seed, _ := catalog.Seed()
	cfg.Model = seed.Models[0].ID
	if got := liveModelOrigin(cfg); got != "" {
		t.Fatalf("origin of a seed model = %q, want empty", got)
	}
	cfg.Model = "no-such-model"
	if got := liveModelOrigin(cfg); got != "" {
		t.Fatalf("origin of an unknown model = %q, want empty", got)
	}
}

// TestStatusTableShowsTheModelOrigin: the human table names the model and who vouches
// for it, only when the report carries an origin.
func TestStatusTableShowsTheModelOrigin(t *testing.T) {
	var buf bytes.Buffer
	renderStatusTable(&buf, status.Report{Overall: "PASS", Model: "my-model", ModelOrigin: "operator-supplied"}, false)
	if !strings.Contains(buf.String(), "my-model (operator-supplied)") {
		t.Fatalf("table lacks the origin row:\n%s", buf.String())
	}
	buf.Reset()
	renderStatusTable(&buf, status.Report{Overall: "PASS", Model: "qwen3.5-0.8b"}, false)
	if strings.Contains(buf.String(), "operator") {
		t.Fatalf("a seed model's table gained a row:\n%s", buf.String())
	}
}

// TestLiveCatalogOriginBindsOnlyAnExternalCatalog: doctor's CAT-02/CAT-03 seam is nil
// on the embedded seed, so a default host's report is what it was, and on an external
// catalog it marks the configured operator model.
func TestLiveCatalogOriginBindsOnlyAnExternalCatalog(t *testing.T) {
	scratchHost(t)
	if seam := liveCatalogOrigin(config.VillaConfig{Model: "anything"}); seam != nil {
		t.Fatal("a host on the embedded seed must bind no provenance seam")
	}
	if code, _, e := runAdd(t, addURL, catalogadd.Overrides{}); code != exitPass {
		t.Fatalf("add: %d %s", code, e)
	}
	cfg, _ := config.LoadVilla()
	cfg.Model = addID
	seam := liveCatalogOrigin(cfg)
	if seam == nil {
		t.Fatal("an external catalog must bind the provenance seam")
	}
	got := seam()
	if len(got) != 1 || got[0].ID != "CAT-02" || !strings.Contains(got[0].Detail, "operator-supplied") {
		t.Fatalf("findings = %+v, want one CAT-02", got)
	}
}
