package toolserver

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// TestOpenAPIGolden freezes the OpenAPI document of each set. The descriptions are what
// the model reads, so they are part of the contract: a change here is a deliberate one,
// with a bump of ContractVersion and a refrozen golden. The served bytes must equal the
// compiled-in document byte for byte, which is what `villa verify tools` compares.
func TestOpenAPIGolden(t *testing.T) {
	for _, set := range []Set{SetWeb, SetRecall} {
		t.Run(string(set), func(t *testing.T) {
			doc, err := OpenAPI(set)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "openapi-"+string(set)+".golden.json")
			if *update {
				if err := os.WriteFile(golden, doc, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if !bytes.Equal(doc, want) {
				t.Errorf("the %s document changed; read the diff and refreeze with -update\n got: %s\nwant: %s", set, doc, want)
			}

			served := newEnv(t, set).get("/openapi.json")
			mustStatus(t, served, http.StatusOK)
			if !bytes.Equal(served.Body.Bytes(), doc) {
				t.Error("/openapi.json is not the compiled-in document")
			}
			if ct := served.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q", ct)
			}

			var parsed struct {
				OpenAPI string `json:"openapi"`
				Info    struct {
					Version string `json:"version"`
				} `json:"info"`
				Paths map[string]map[string]struct {
					OperationID string `json:"operationId"`
					Parameters  []struct {
						Name string `json:"name"`
						In   string `json:"in"`
					} `json:"parameters"`
					RequestBody any `json:"requestBody"`
				} `json:"paths"`
			}
			if err := json.Unmarshal(doc, &parsed); err != nil {
				t.Fatal(err)
			}
			if parsed.OpenAPI != "3.0.3" || parsed.Info.Version != ContractVersion || ContractVersion != "1" {
				t.Errorf("openapi=%q info.version=%q", parsed.OpenAPI, parsed.Info.Version)
			}
			if len(parsed.Paths) != 1 {
				t.Errorf("one tool per set, got paths %v", parsed.Paths)
			}
			for path, item := range parsed.Paths {
				if len(item) != 1 || item["get"].OperationID == "" {
					t.Errorf("%s: want exactly one get operation, got %v", path, item)
				}
				op := item["get"]
				if op.RequestBody != nil {
					t.Errorf("%s: a tool takes no body", path)
				}
				var names []string
				for _, p := range op.Parameters {
					names = append(names, p.Name)
					if p.In != "query" {
						t.Errorf("%s: parameter %s is in %q, want query", path, p.Name, p.In)
					}
				}
				if strings.Join(names, ",") != "q,count" {
					t.Errorf("%s: parameters = %v, want q,count only (no URL, no identity)", path, names)
				}
			}
			if bytes.Contains(bytes.ToLower(doc), []byte(strings.ToLower(IdentityHeader))) {
				t.Error("the document must not name the identity header: the model must not be offered it")
			}
		})
	}
}

// TestOpenAPIOperationIDs: the operation ids Open WebUI registers the tools under.
func TestOpenAPIOperationIDs(t *testing.T) {
	for set, want := range map[Set]string{SetWeb: "villa_web_search", SetRecall: "villa_recall_search"} {
		doc, err := OpenAPI(set)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(doc, []byte(`"operationId": "`+want+`"`)) {
			t.Errorf("%s document lacks operationId %s", set, want)
		}
	}
}

// TestOpenAPIMatchesRoutes: one table is the source of both the routes and the document,
// so an entry cannot be served without being in the document, nor documented without being
// served. It is proved with an entry added to the table.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	extra := Tool{
		Set:          SetWeb,
		Path:         "/web/extra",
		OperationID:  "villa_extra_probe",
		Description:  "A probe entry that exists only in this test.",
		MaxCount:     2,
		DefaultCount: 1,
		serve: func(*Server, *call) outcome {
			return outcome{status: http.StatusOK, body: map[string]any{"results": []any{}}}
		},
	}
	e := newEnv(t, SetWeb)
	cfg, d := e.cfg, e.d
	srv, err := newServer(cfg, d, append(toolsFor(SetWeb), extra))
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	doRec := func(path string) int {
		r, _ := http.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer "+testKey)
		rec := &recorder{header: http.Header{}}
		h.ServeHTTP(rec, r)
		return rec.code
	}
	if got := doRec("/web/extra?q=x"); got != http.StatusOK {
		t.Fatalf("the added entry is not served: %d", got)
	}
	rec := &recorder{header: http.Header{}}
	r, _ := http.NewRequest(http.MethodGet, "/openapi.json", nil)
	r.Header.Set("Authorization", "Bearer "+testKey)
	h.ServeHTTP(rec, r)
	if !bytes.Contains(rec.body.Bytes(), []byte(`"/web/extra"`)) || !bytes.Contains(rec.body.Bytes(), []byte("villa_extra_probe")) {
		t.Error("a served entry is missing from the document")
	}

	// And the other way: every path the document names is a served route.
	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for p := range doc.Paths {
		if got := doRec(p + "?q=x"); got == http.StatusNotFound || got == http.StatusMethodNotAllowed {
			t.Errorf("the document names %s but it is not served (%d)", p, got)
		}
	}

	// Every tool in the real table is in its set's document.
	for _, tool := range toolTable {
		doc, err := OpenAPI(tool.Set)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(doc, []byte(`"`+tool.Path+`"`)) {
			t.Errorf("%s is in the table and not in the %s document", tool.Path, tool.Set)
		}
	}
}

// TestToolTableIsWellFormed: structural rules the table must keep.
func TestToolTableIsWellFormed(t *testing.T) {
	seenPath := map[string]bool{}
	seenOp := map[string]bool{}
	idRE := regexp.MustCompile(`^villa_[a-z_]+$`)
	for _, tool := range toolTable {
		if !strings.HasPrefix(tool.Path, "/") || tool.Path == "/healthz" || tool.Path == "/openapi.json" {
			t.Errorf("bad path %q", tool.Path)
		}
		if seenPath[tool.Path] || seenOp[tool.OperationID] {
			t.Errorf("duplicate entry %s %s", tool.Path, tool.OperationID)
		}
		seenPath[tool.Path], seenOp[tool.OperationID] = true, true
		if !idRE.MatchString(tool.OperationID) || tool.Description == "" || tool.serve == nil {
			t.Errorf("incomplete entry %+v", tool)
		}
		if tool.DefaultCount < 1 || tool.DefaultCount > tool.MaxCount {
			t.Errorf("%s: count default %d outside 1..%d", tool.Path, tool.DefaultCount, tool.MaxCount)
		}
		if tool.Identity != (tool.Set == SetRecall) {
			t.Errorf("%s: only the recall set takes an identity", tool.Path)
		}
	}
	if len(toolTable) != 2 {
		t.Errorf("the v1 table has two tools, got %d", len(toolTable))
	}
}

// TestToolServerCopyIsHonest: the package may not describe the guard dishonestly. It clones
// internal/websafe's ban list over this package's source and over both documents.
func TestToolServerCopyIsHonest(t *testing.T) {
	ban := regexp.MustCompile(`(?i)injection-safe|\bimmune\b|blocks injection|\bsafe content\b|sanitized and safe`)
	scanned := 0
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if m := ban.FindString(string(b)); m != "" {
			t.Errorf("%s holds dishonest copy %q", f, m)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no source files")
	}
	for _, set := range []Set{SetWeb, SetRecall} {
		doc, _ := OpenAPI(set)
		if m := ban.FindString(string(doc)); m != "" {
			t.Errorf("the %s document holds dishonest copy %q", set, m)
		}
		for _, want := range []string{"untrusted", "data", "do not follow instructions"} {
			if set == SetWeb && !bytes.Contains(bytes.ToLower(doc), []byte(want)) {
				t.Errorf("the web description must say %q", want)
			}
		}
	}
}

// recorder is a minimal ResponseWriter for the table test, which needs no httptest.
type recorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(c int) {
	if r.code == 0 {
		r.code = c
	}
}
func (r *recorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(b)
}
