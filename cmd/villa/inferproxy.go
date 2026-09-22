package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// inferproxy.go is the thin cobra caller for the HIDDEN `villa inferproxy-serve`
// subcommand: the internal container entrypoint that runs inside the
// villa-inferproxy Quadlet container (GHSA-gvp9, ADR-0011). It is the sandbox's
// ONLY route to inference now that villa-llama no longer joins
// villa-sandbox.network: it joins BOTH villa.network and villa-sandbox.network,
// forwards ONLY POST /v1/chat/completions and GET /v1/models to villa-llama
// (inference.ProxyAllowed is the ONE allowlist decision, kept pure and testable
// there), injects the real LLAMA_API_KEY bearer on the forwarded leg, and caps
// the request body size. It is NOT user-facing — no host port is published.
//
// Mirroring websafe.go: the cobra RunE wires liveInferproxyDeps() then
// os.Exit(runInferproxy(...)); runInferproxy RETURNS the exit code (no os.Exit in
// the body) so inferproxy_test.go drives it with a stubbed Serve (no live
// listener) and a fake upstream (httptest, no live villa-llama).

// inferproxyMaxBodyBytes bounds one forwarded request body. 4 MiB comfortably
// covers a large prompt without letting an unbounded body exhaust memory on a
// container with no swap.
const inferproxyMaxBodyBytes = 4 << 20

// Read/idle deadlines for the container-internal proxy socket, mirroring
// websafe.go's reasoning: ReadHeaderTimeout bounds the slowloris window,
// IdleTimeout bounds an idle keep-alive connection. Deliberately NO
// WriteTimeout — a streamed chat completion can legitimately run long, and an
// absolute write deadline would cut off a correct-but-slow response.
// The shutdown grace period is websafe.go's shared websafeShutdownGrace: both
// processes are systemd units stopped with SIGTERM via the SAME
// serveUntilCancelled helper, so there is only one grace-period constant to
// keep in step with systemd's default 90s TimeoutStopSec, not a second one.
const (
	inferproxyReadHeaderTimeout = 30 * time.Second
	inferproxyIdleTimeout       = 120 * time.Second
)

// inferproxyDeps are the injectable seams runInferproxy drives, so the test can
// stub the upstream target, the api key, and the serve call without binding a
// real socket or reaching a real villa-llama.
type inferproxyDeps struct {
	// Target is villa-llama's in-network ROOT endpoint (no /v1 suffix — see
	// orchestrate.LlamaInNetworkRoot's doc comment for why). Live wiring reads
	// it through the orchestrate seam so it can never drift from the rendered
	// unit's ContainerName=/port.
	Target string
	// APIKey is the LLAMA_API_KEY bearer injected on every forwarded request.
	// The live wiring reads it from the container env (the mounted 0600
	// EnvironmentFile, the SAME file villa-llama itself reads). runInferproxy
	// refuses to serve when this is empty (fail-closed): every forwarded
	// request would otherwise be a guaranteed 401, which is a clearer refusal
	// at startup than a live proxy that can never actually work.
	APIKey string
	// MaxBodyBytes bounds a forwarded request body (see inferproxyMaxBodyBytes).
	MaxBodyBytes int64
	// Transport is the outbound RoundTripper the reverse proxy uses to reach
	// villa-llama. Nil selects httputil.ReverseProxy's own default
	// (http.DefaultTransport); tests inject an httptest-backed stub.
	Transport http.RoundTripper
	// Serve binds the socket at addr and serves the handler until it errors.
	// Stubbed in tests so no real listener is bound; the live wiring is
	// serveUntilCancelled(ctx, inferproxyHTTPServer(addr, h)) (websafe.go's
	// shared shutdown helper).
	Serve func(ctx context.Context, addr string, h http.Handler) error
}

// newInferproxy builds the HIDDEN `villa inferproxy-serve` subcommand. Hidden
// because it is not a user verb — it runs only inside the villa-inferproxy
// container.
func newInferproxy() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "inferproxy-serve",
		Hidden: true,
		Short:  "Serve the sandbox's /v1-only reverse proxy to villa-llama (internal; runs inside the villa-inferproxy container)",
		Long: "Serve the VillaStraylight sandbox inference proxy (GHSA-gvp9, ADR-0011): forwards " +
			"ONLY POST /v1/chat/completions and GET /v1/models to villa-llama, injecting the " +
			"real LLAMA_API_KEY bearer, refusing every other route. This is an INTERNAL " +
			"container entrypoint — the task VMs reach it over villa-sandbox.network by " +
			"container DNS only (no host port). Not a user-facing command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveInferproxyDeps()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "inferproxy: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runInferproxy(cmd, args, deps))
			return nil
		},
	}
	cmd.Flags().String("host", "0.0.0.0", "container-internal bind host (never a host port)")
	cmd.Flags().Int("port", config.InferproxyPort, "container-internal listen port")
	return cmd
}

// runInferproxy builds the reverse-proxy handler (over the injected target/api
// key/transport) and serves it. It RETURNS the exit code (no os.Exit in the
// body) so inferproxy_test.go drives it deterministically with a stubbed Serve.
func runInferproxy(cmd *cobra.Command, _ []string, d *inferproxyDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	host, err := cmd.Flags().GetString("host")
	if err != nil {
		fmt.Fprintf(errOut, "inferproxy: %v\n", err)
		return exitBlocked
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		fmt.Fprintf(errOut, "inferproxy: %v\n", err)
		return exitBlocked
	}

	// Fail closed on an empty bearer (GHSA-qxg9): every forwarded request
	// would otherwise be a guaranteed 401 from villa-llama, so refuse to start
	// rather than run a proxy that can never actually work. The install flow
	// always generates a crypto/rand secret into the 0600 EnvironmentFile, so
	// an empty value here means that file was lost/tampered.
	if d.APIKey == "" {
		fmt.Fprintf(errOut, "inferproxy: refusing to serve with an empty bearer (LLAMA_API_KEY unset); "+
			"the villa-inferproxy unit's 0600 EnvironmentFile must supply it — re-run `villa install` to regenerate\n")
		return exitBlocked
	}

	target, err := url.Parse(d.Target)
	if err != nil || target.Host == "" {
		fmt.Fprintf(errOut, "inferproxy: invalid upstream target %q: %v\n", d.Target, err)
		return exitBlocked
	}

	handler := buildInferproxyHandler(target, d.APIKey, d.MaxBodyBytes, d.Transport)
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	fmt.Fprintf(out, "villa inferproxy-serve listening on http://%s (container-internal)\n", addr)

	if err := d.Serve(cmdContext(cmd), addr, handler); err != nil {
		fmt.Fprintf(errOut, "inferproxy: serve: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// buildInferproxyHandler wraps an httputil.ReverseProxy targeting root (reused,
// never a hand-rolled forwarding loop) with the ONE allowlist decision
// (inference.ProxyAllowed) and a body-size cap. A request that fails the
// allowlist never reaches the reverse proxy at all — refused with 403, not
// merely unrouted. apiKey is injected as `Authorization: Bearer <apiKey>` on
// EVERY forwarded request, on top of whatever the Director's default
// scheme/host rewrite already does.
func buildInferproxyHandler(root *url.URL, apiKey string, maxBodyBytes int64, transport http.RoundTripper) http.Handler {
	rp := &httputil.ReverseProxy{
		// Rewrite, not the deprecated Director: SetURL does the SAME
		// scheme/host/path-join NewSingleHostReverseProxy's Director used to
		// (root's empty Path means no join at all — see LlamaInNetworkRoot's
		// doc comment), and this is where the real bearer is injected on
		// every forwarded request.
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(root)
			pr.Out.Header.Set("Authorization", "Bearer "+apiKey)
		},
	}
	if transport != nil {
		rp.Transport = transport
	}
	// A backend error (villa-llama unreachable) is reported to the task as a
	// clean 502, never a hung connection or an unhandled panic. An oversized
	// body specifically maps to 413: MaxBytesReader's read error surfaces here
	// (through the outbound RoundTrip, which is what actually drains the body),
	// not through net/http's own automatic-413 machinery — that machinery
	// detects a MaxBytesReader failure on the ORIGINAL server's own read of
	// r.Body, but this reader is drained by the reverse proxy's transport
	// instead, so the mapping has to be done here explicitly.
	rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "inferproxy: request body exceeds the size cap", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "inferproxy: upstream unreachable: "+err.Error(), http.StatusBadGateway)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !inference.ProxyAllowed(r.Method, r.URL.Path) {
			http.Error(w, "inferproxy: refused — not on the /v1 allowlist", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		rp.ServeHTTP(w, r)
	})
}

// liveInferproxyDeps wires inferproxyDeps to the real container runtime:
// villa-llama's in-network ROOT endpoint (orchestrate.LlamaInNetworkRoot, so it
// can never drift from the rendered unit), the bearer read from the container
// env LLAMA_API_KEY (sourced from the 0600 EnvironmentFile the villa-inferproxy
// unit mounts — the SAME file villa-llama itself reads), and a Serve that binds
// the container-internal socket with websafe.go's shared shutdown helper.
func liveInferproxyDeps() (*inferproxyDeps, error) {
	return &inferproxyDeps{
		Target:       orchestrate.LlamaInNetworkRoot(),
		APIKey:       os.Getenv("LLAMA_API_KEY"),
		MaxBodyBytes: inferproxyMaxBodyBytes,
		Serve: func(ctx context.Context, addr string, h http.Handler) error {
			return serveUntilCancelled(ctx, inferproxyHTTPServer(addr, h))
		},
	}, nil
}

// inferproxyHTTPServer builds the configured proxy http.Server. Separate from
// the Serve closure so a test can assert the deadlines are actually set on the
// PRODUCTION server without binding a socket (mirrors websafeHTTPServer).
func inferproxyHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: inferproxyReadHeaderTimeout,
		IdleTimeout:       inferproxyIdleTimeout,
	}
}
