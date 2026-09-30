package main

// inference_apikey_gate_test.go is the structural guard for ADR-0014: no
// first-party code reaches a llama-server unit except through the authenticated
// inference client (inference.Client), so no caller can drift back to the
// 401-by-omission #252 was.
//
// It parses every non-test .go file under cmd/villa and internal/ (comments are not
// code, so a doc comment naming an accessor is not a finding) and reports three
// kinds of reach around the client:
//
//   - a read of the api key (config.InferenceSecret). Calling llama-server needs it,
//     so a read is either the client's construction or a hand-off to a process that
//     calls llama-server itself;
//   - a call that yields a llama-server address, or builds a client or an OpenAI
//     wire client from one;
//   - a llama-server route literal (/props, /slots, /metrics, the chat and model
//     routes).
//
// Each is allowed only in the files listed with a reason. A new entry needs one: the
// question it answers is "why does this file not go through the client?".
//
// This is a syntax gate, not a type-checker, the same strength as TestSeamGrepGate
// (internal/inference/seam_test.go): it matches package-qualified calls by import
// path and field selectors by name. It replaced a gate that matched three struct
// literals and could not see the curl argv #252 sent without the key.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/MatrixMagician/VillaStraylight/"

// keyReaders may read config.InferenceSecret.
var keyReaders = map[string]string{
	"cmd/villa/inference.go":            "builds the inference client; gives the validate run its --env-file",
	"internal/stackapply/stackapply.go": "generates and persists the key before render (the ADR-0011 migration, ADR-0013)",
	"cmd/villa/model_resident.go":       "hands the key to Open WebUI's connection list; Open WebUI calls llama-server itself",
	"internal/install/flow.go":          "generates and persists the key on install",
	"internal/orchestrate/render.go":    "renders the key into Open WebUI's environment (ADR-0011)",
	"internal/agent/render.go":          "hands the key to Crush's provider config; Crush calls llama-server itself",
	"internal/agent/claude.go":          "hands the key to Claude Code's environment; Claude Code calls llama-server itself",
}

// addressCalls are the package-qualified calls that yield a llama-server address or
// a client for one, keyed by import path + "." + name, with the files allowed each.
var addressCalls = map[string]map[string]string{
	"internal/inference.HostClient": {
		"cmd/villa/inference.go": "the client builder",
	},
	"internal/inference.NewClient": {
		"cmd/villa/inference.go":        "the client builder",
		"cmd/villa/install_hostprep.go": "install.Deps hands the readiness seam an endpoint string; /health is keyless",
	},
	"internal/llm.NewOpenAIClient": {
		"internal/inference/client.go": "the client's chat route",
	},
	"internal/orchestrate.LlamaInNetworkRoot": {
		"cmd/villa/inference.go":  "the in-network client builder",
		"cmd/villa/inferproxy.go": "villa-inferproxy's forward leg (ADR-0011): it relays a task's requests, it does not originate them",
	},
	"internal/orchestrate.LlamaInNetworkEndpoint": {
		"cmd/villa/model_resident.go": "Open WebUI's connection list names the unit; Open WebUI calls it",
	},
	"internal/orchestrate.ResidentInNetworkEndpoint": {
		"cmd/villa/model_resident.go": "Open WebUI's connection list names each resident slot; Open WebUI calls them",
	},
	// inference.NewContainerRunner(...).Endpoint() is the host-endpoint idiom every
	// caller used before the client; it is matched as the chained call.
	"internal/inference.NewContainerRunner().Endpoint": {
		"cmd/villa/install.go": "prints the endpoint and hands it to the keyless readiness poll (install.Deps)",
	},
}

// routeLiterals are llama-server routes, allowed only where the client or the
// inferproxy's allowlist spells them.
var routeLiterals = map[string]map[string]string{}

func init() {
	allowed := map[string]string{
		"internal/inference/client.go": "the client owns the routes",
		"internal/inference/proxy.go":  "villa-inferproxy's two-route allowlist (ADR-0011)",
		"internal/llm/openai.go":       "the OpenAI wire protocol, under the base URL the client gives it",
	}
	for _, r := range []string{"/props", "/slots", "/metrics", "/chat/completions", "/v1/chat/completions", "/v1/models"} {
		routeLiterals[r] = allowed
	}
}

// clientBypasses parses one file's source and returns every reach around the client
// that its path is not allowed.
func clientBypasses(rel string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	imports := map[string]string{} // local name → module-relative path
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name] = strings.TrimPrefix(p, modulePath)
	}
	qualified := func(e ast.Expr) string {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || imports[id.Name] == "" {
			return ""
		}
		return imports[id.Name] + "." + sel.Sel.Name
	}

	var found []string
	report := func(n ast.Node, what string) {
		found = append(found, rel+":"+strconv.Itoa(fset.Position(n.Pos()).Line)+": "+what)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if n.Sel.Name == "InferenceSecret" && keyReaders[rel] == "" {
				report(n, "reads the api key (InferenceSecret) outside the inference client; build inferenceClient(cfg) instead")
			}
		case *ast.CallExpr:
			name := qualified(n.Fun)
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Endpoint" {
				if inner, ok := sel.X.(*ast.CallExpr); ok && qualified(inner.Fun) == "internal/inference.NewContainerRunner" {
					name = "internal/inference.NewContainerRunner().Endpoint"
				}
			}
			if allowed, gated := addressCalls[name]; gated && allowed[rel] == "" {
				report(n, "calls "+name+" outside the inference client; take the address from inferenceClient(cfg) instead")
			}
		case *ast.BasicLit:
			if n.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(n.Value)
			if err != nil {
				return true
			}
			if allowed, gated := routeLiterals[s]; gated && allowed[rel] == "" {
				report(n, "spells the llama-server route "+strconv.Quote(s)+" outside the inference client; call its method instead")
			}
		}
		return true
	})
	return found, nil
}

// TestInferenceReachedOnlyThroughClient walks cmd/villa and internal/ and fails on
// every reach around the inference client that is not allowed with a reason.
func TestInferenceReachedOnlyThroughClient(t *testing.T) {
	root := filepath.Join("..", "..")
	var (
		found  []string
		walked int
	)
	for _, dir := range []string{"cmd/villa", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			hits, err := clientBypasses(filepath.ToSlash(rel), src)
			walked++
			found = append(found, hits...)
			return err
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	// A walk that visits nothing passes vacuously; the tree has hundreds of files.
	if walked < 100 {
		t.Fatalf("the gate parsed only %d file(s) — the walk root is wrong", walked)
	}
	sort.Strings(found)
	for _, f := range found {
		t.Error(f)
	}
}

// TestClientGateCatchesTheBypassesItReplaced feeds the gate the shapes it exists to
// refuse, so a gate that silently stopped matching fails here rather than passing
// the tree: #252's in-network drive (an address accessor plus a route literal), the
// old metrics scrape's route, and a command reading the key to call llama-server.
func TestClientGateCatchesTheBypassesItReplaced(t *testing.T) {
	for _, c := range []struct {
		rel, src string
		want     int
	}{
		{"cmd/villa/doctor.go", `package main
import "github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
var url = orchestrate.LlamaInNetworkEndpoint() + "/chat/completions"`, 2},
		{"internal/metrics/llamacpp.go", `package metrics
func scrape(endpoint string) string { return endpoint + "/metrics" }`, 1},
		{"cmd/villa/bench.go", `package main
import (
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)
func measure(cfg config.VillaConfig, b inference.Backend) {
	_ = llm.NewOpenAIClient(llm.Options{BaseURL: inference.NewContainerRunner(b, inference.RunSpec{}).Endpoint(), APIKey: cfg.InferenceSecret})
}`, 3},
		{"cmd/villa/status.go", `package main
// orchestrate.LlamaInNetworkEndpoint() + "/props" in a comment is not code.
func ok() {}`, 0},
	} {
		got, err := clientBypasses(c.rel, []byte(c.src))
		if err != nil {
			t.Fatalf("%s: parse: %v", c.rel, err)
		}
		if len(got) != c.want {
			t.Errorf("%s: %d finding(s), want %d:\n%s", c.rel, len(got), c.want, strings.Join(got, "\n"))
		}
	}
}
