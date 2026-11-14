package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// tools_serve.go is the thin cobra caller for the HIDDEN `villa tools-serve` subcommand:
// the internal container entrypoint of villa's own tool server (ADR-0042). One process
// serves ONE set, `web` or `recall`, so the unit that parses untrusted web pages never holds
// read access to the recall index. Open WebUI reaches each unit over its network by
// container DNS only (no host port); the bearer comes from the 0600 EnvironmentFile the
// unit mounts (VILLA_TOOLS_KEY in the container env), never the 0644 unit.
//
// It mirrors websafe.go: the cobra RunE wires liveToolsServeDeps() then
// os.Exit(runToolsServe(...)), and runToolsServe RETURNS the exit code so
// tools_serve_test.go drives it with a stubbed Serve and in-memory upstreams. The pure core
// is internal/toolserver; this file only binds the network addresses (composed from
// internal/config), the live clients and the socket.

const (
	// toolsKeyEnv carries the unit's bearer. Unlike websafe, an empty value is a refusal
	// to start and the core has no accept-any path behind it.
	toolsKeyEnv = orchestrate.ToolsKeyEnv
	// toolsQdrantCollection is the one Qdrant collection Open WebUI keeps knowledge in:
	// <QDRANT_COLLECTION_PREFIX>_knowledge, with the prefix villa renders ("open-webui").
	toolsQdrantCollection = "open-webui_knowledge"
	// toolsDefaultPort is the web unit's port. The recall unit passes its own.
	toolsDefaultPort = config.ToolsWebPort
	// toolsDefaultScopeFile is where the recall unit finds the scope document, inside the
	// directory the unit mounts read-only.
	toolsDefaultScopeFile = orchestrate.ToolsScopeMountDir + "/" + recall.ScopeFileName
	// toolsUpstreamTimeout is the client-level bound on a request to a fixed upstream. Each
	// leg has its own, tighter, context deadline in the core.
	toolsUpstreamTimeout = 10 * time.Second
)

// toolsServeDeps are the seams runToolsServe drives, so a test can stub the upstreams, the
// bearer, the scope file and the serve call without a socket.
type toolsServeDeps struct {
	// Key is the expected bearer. runToolsServe refuses to serve when it is empty.
	Key string
	// Upstream performs requests to SearXNG, the embedder and Qdrant. The live wiring is a
	// plain client that follows no redirect.
	Upstream func(*http.Request) (*http.Response, error)
	// FetchClient fetches result pages. The live wiring is websafe.SafeClient over
	// toolserver.WebBounds(), the same SSRF guard villa-websafe uses.
	FetchClient *http.Client
	// OpenScope opens the recall scope document at path.
	OpenScope func(path string) (io.ReadCloser, error)
	// Logf receives the one-line request log. Nil discards it.
	Logf func(format string, args ...any)
	// Serve binds addr and serves h until the context ends or it errors.
	Serve func(ctx context.Context, addr string, h http.Handler) error
}

// newToolsServe builds the HIDDEN `villa tools-serve` subcommand. Hidden because it is not
// a user verb: it runs only inside the villa-tools-web and villa-tools-recall containers.
func newToolsServe() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "tools-serve",
		Hidden: true,
		Short:  "Serve villa's tool server (internal; runs inside the villa-tools-* containers)",
		Long: "Serve one set of VillaStraylight's read-only OpenAPI tools for Open WebUI: --set web " +
			"(web search) or --set recall (search of the caller's own past conversations). This is an " +
			"INTERNAL container entrypoint, reached over its network by container DNS only (no host " +
			"port). Not a user-facing command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveToolsServeDeps()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "tools-serve: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runToolsServe(cmd, args, deps))
			return nil
		},
	}
	cmd.Flags().String("set", "", "which tools to serve: web or recall")
	cmd.Flags().String("host", "0.0.0.0", "container-internal bind host (never a host port)")
	cmd.Flags().Int("port", toolsDefaultPort, "container-internal listen port")
	cmd.Flags().String("embedding-model", "", "the embedder's model id (recall set)")
	cmd.Flags().String("scope-file", toolsDefaultScopeFile, "the recall scope document (recall set)")
	return cmd
}

// runToolsServe builds the core for the chosen set, prints the container-internal listen
// address and serves it. It RETURNS the exit code (no os.Exit in the body) so the test can
// drive it with a stubbed Serve.
func runToolsServe(cmd *cobra.Command, _ []string, d *toolsServeDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	setName, err := cmd.Flags().GetString("set")
	if err != nil {
		fmt.Fprintf(errOut, "tools-serve: %v\n", err)
		return exitBlocked
	}
	set, err := toolserver.ParseSet(setName)
	if err != nil {
		fmt.Fprintf(errOut, "tools-serve: %v\n", err)
		return exitBlocked
	}
	host, err := cmd.Flags().GetString("host")
	if err != nil {
		fmt.Fprintf(errOut, "tools-serve: %v\n", err)
		return exitBlocked
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		fmt.Fprintf(errOut, "tools-serve: %v\n", err)
		return exitBlocked
	}
	model, err := cmd.Flags().GetString("embedding-model")
	if err != nil {
		fmt.Fprintf(errOut, "tools-serve: %v\n", err)
		return exitBlocked
	}
	scopeFile, err := cmd.Flags().GetString("scope-file")
	if err != nil {
		fmt.Fprintf(errOut, "tools-serve: %v\n", err)
		return exitBlocked
	}

	// Fail closed on an empty bearer: an unauthenticated tool server on a container
	// network is a trust-boundary fail-open. The install flow generates the key into the
	// 0600 EnvironmentFile, so an empty value means that file was lost or tampered with.
	if d.Key == "" {
		fmt.Fprintf(errOut, "tools-serve: refusing to serve with an empty bearer (%s unset); "+
			"the unit's 0600 EnvironmentFile must supply it, so re-run `villa install` to regenerate\n", toolsKeyEnv)
		return exitBlocked
	}
	if set == toolserver.SetRecall && model == "" {
		fmt.Fprintf(errOut, "tools-serve: --embedding-model is required for --set recall\n")
		return exitBlocked
	}

	cfg := toolserver.Config{Set: set, Key: d.Key}
	switch set {
	case toolserver.SetWeb:
		cfg.SearxngURL = fmt.Sprintf("http://%s", net.JoinHostPort(config.SearxngAddr, fmt.Sprint(config.SearxngPort)))
	case toolserver.SetRecall:
		cfg.EmbedURL = fmt.Sprintf("http://%s", net.JoinHostPort(config.EmbedAddr, fmt.Sprint(config.EmbedPort)))
		cfg.QdrantURL = fmt.Sprintf("http://%s", net.JoinHostPort(config.QdrantAddr, fmt.Sprint(config.QdrantPort)))
		cfg.EmbeddingModel = model
		cfg.Collection = toolsQdrantCollection
	}
	srv, err := toolserver.New(cfg, toolserver.Deps{
		Now:         time.Now,
		Upstream:    d.Upstream,
		FetchClient: d.FetchClient,
		ReadScope:   func() (io.ReadCloser, error) { return d.OpenScope(scopeFile) },
		Logf:        d.Logf,
	})
	if err != nil {
		fmt.Fprintf(errOut, "tools-serve: %v\n", err)
		return exitBlocked
	}

	addr := net.JoinHostPort(host, fmt.Sprint(port))
	fmt.Fprintf(out, "villa tools-serve (%s) listening on http://%s (container-internal)\n", set, addr)

	if err := d.Serve(cmdContext(cmd), addr, srv.Handler()); err != nil {
		fmt.Fprintf(errOut, "tools-serve: serve: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// liveToolsServeDeps wires toolsServeDeps to the real container runtime: the bearer from
// the container env, the SSRF-guarded SafeClient over the web bounds for result pages, a
// plain no-redirect no-proxy client for the three fixed upstreams, the scope file from the
// read-only mount, and a Serve that builds the explicit http.Server below.
func liveToolsServeDeps() (*toolsServeDeps, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	upstream := &http.Client{
		Timeout:   toolsUpstreamTimeout,
		Transport: tr,
		// Every upstream is a fixed address. A redirect would leave it, so it is answered
		// as the 3xx it is, which the core reads as an upstream failure.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &toolsServeDeps{
		Key:         os.Getenv(toolsKeyEnv),
		Upstream:    upstream.Do,
		FetchClient: websafe.SafeClient(toolserver.WebBounds()),
		OpenScope:   func(path string) (io.ReadCloser, error) { return os.Open(path) },
		Logf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "tools: "+format+"\n", args...)
		},
		Serve: func(ctx context.Context, addr string, h http.Handler) error {
			return serveUntilCancelled(ctx, toolsHTTPServer(addr, h))
		},
	}, nil
}

// toolsHTTPServer builds the configured http.Server. It is separate from the Serve closure
// so a test can assert the caps on the PRODUCTION server without binding a fixed socket.
// There is deliberately no WriteTimeout: each request carries its own deadline in the core.
func toolsHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: toolserver.ReadHeaderTimeout,
		IdleTimeout:       toolserver.IdleTimeout,
		MaxHeaderBytes:    toolserver.MaxHeaderBytes,
	}
}
