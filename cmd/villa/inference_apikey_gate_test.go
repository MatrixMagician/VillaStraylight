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
//   - a call that yields a llama-server address (an Endpoint() call on any runner,
//     or an address accessor), or builds a client or an OpenAI wire client from one;
//   - a llama-server route or port literal (/props, /slots, /metrics, /health,
//     /tokenize, the chat and model routes, the /v1 base, :8080).
//
// A route is matched as a SUFFIX of a whitespace-free literal, after any query
// string, so `ep+"/tokenize"`, "%s/props", "http://127.0.0.1:8080/props" and
// "/props?x=1" are all caught. A literal with a space is prose (a help line, a
// verdict detail), never a URL, and is skipped; so are import paths.
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
}

// endpointCalls may call Endpoint() on a runner or on install.Deps: it yields the
// host llama-server address. Matched by selector name, so it also catches the call
// through the inference.Runner interface, not only the chained NewContainerRunner one.
var endpointCalls = map[string]string{
	"cmd/villa/install.go":                "prints the endpoint and hands it to the keyless readiness poll (install.Deps)",
	"internal/install/flow.go":            "install.Deps.Endpoint is a func field yielding a string for the keyless readiness poll and the closing message",
	"internal/inference/runner_podman.go": "the container runner's own keyless /health readiness check, inside the inference package",
}

// routeSpellers may spell a llama-server route or the :8080 port in a literal.
var routeSpellers = map[string]string{
	"internal/inference/client.go": "the client owns the routes",
	"internal/inference/proxy.go":  "villa-inferproxy's two-route allowlist (ADR-0011)",
	"internal/llm/openai.go":       "the OpenAI wire protocol, under the base URL the client gives it",
	// The suffix match also sees these other services' routes and ports, which share a
	// name or a number with a llama-server one but are not the inference unit.
	"internal/openwebui/paths.go":       "Open WebUI's own /health and /api/... routes, not llama-server's",
	"internal/dashboard/api_tasks.go":   "the dashboard's own /api/metrics route",
	"cmd/villa/sandbox_bridge.go":       "the Crush server's /v1/health inside the task VM",
	"cmd/villa/status.go":               "the embedding sidecar's /health probe, a different unit from the inference one",
	"internal/orchestrate/endpoint.go":  "the address accessors themselves (gated in addressCalls) and the inferproxy's base URL",
	"internal/orchestrate/openwebui.go": "Open WebUI's own container port and the embedding sidecar's base URL for its RAG settings",
	"internal/agent/render.go":          "hands Crush its provider base URL; Crush calls llama-server itself (a keyReaders peer)",
	"internal/agent/claude.go":          "trims /v1 off the provider base URL it was handed for Claude Code",
	"internal/voice/voice.go":           "the voice units' own /health and /v1 base, not llama-server's",
}

// llamaRoutes are matched as a suffix of a whitespace-free literal (see the header).
var llamaRoutes = []string{"/props", "/slots", "/metrics", "/health", "/tokenize", "/chat/completions", "/v1/models", "/v1"}

// spelledRoute names the llama-server route or port a string literal spells, or "".
func spelledRoute(s string) string {
	if strings.Contains(s, ":8080") {
		return ":8080"
	}
	if strings.ContainsAny(s, " \t\n") {
		return ""
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	for _, r := range llamaRoutes {
		if strings.HasSuffix(s, r) {
			return r
		}
	}
	return ""
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
		case *ast.ImportSpec:
			return false // an import path ending in /metrics is not a route
		case *ast.CallExpr:
			name := qualified(n.Fun)
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Endpoint" && endpointCalls[rel] == "" {
				report(n, "calls Endpoint() outside the inference client; take the address from inferenceClient(cfg) instead")
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
			if r := spelledRoute(s); r != "" && routeSpellers[rel] == "" {
				report(n, "spells the llama-server route or port "+strconv.Quote(r)+" in "+strconv.Quote(s)+" outside the inference client; call its method instead")
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
		// The five shapes the review of #260 found the exact-match gate missed (#262).
		{"cmd/villa/probe1.go", `package main
var u = "http://127.0.0.1:8080/props"`, 1},
		{"cmd/villa/probe2.go", `package main
import "fmt"
func u(ep string) string { return fmt.Sprintf("%s/props", ep) }`, 1},
		{"cmd/villa/probe3.go", `package main
import "github.com/MatrixMagician/VillaStraylight/internal/inference"
func u(r inference.Runner) string { return r.Endpoint() + "/health" }`, 2},
		{"cmd/villa/probe4.go", `package main
func u(ep string) string { return ep + "/tokenize" }`, 1},
		{"cmd/villa/probe5.go", `package main
import "fmt"
func u(port int) string { return fmt.Sprintf("http://127.0.0.1:%d/props?model=x", port) }`, 1},
		// Prose and import paths are not URLs: a help line naming a route, and an
		// import path that ends in /metrics, must not be findings.
		{"cmd/villa/prose.go", `package main
import "github.com/MatrixMagician/VillaStraylight/internal/metrics"
var _ = metrics.ParsePerf
var help = "polls /health until it returns 200, then reads /props"`, 0},
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
