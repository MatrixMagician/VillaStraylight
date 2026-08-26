package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// websafe.go is the thin cobra caller for the HIDDEN `villa websafe-serve` subcommand
// the internal container entrypoint that runs inside the
// villa-websafe Quadlet container (the host villa binary bind-mounted read-only). It builds
// the internal/websafe pure fetch core behind the live SSRF-guarded SafeClient and serves
// the verified OWUI external-loader HTTP contract (the /load handler) on a container-internal
// socket. It is NOT user-facing — OWUI reaches it over villa.network by container DNS only
// (no host port); the bearer comes from the 0600 EnvironmentFile the unit mounts
// (EXTERNAL_WEB_LOADER_API_KEY in the container env), never the 0644 unit.
//
// Mirroring dashboard.go: the cobra RunE wires liveWebsafeDeps() then os.Exit(runWebsafe(...));
// runWebsafe RETURNS the exit code (no os.Exit in the body) so websafe_test.go drives it with
// a stubbed Serve + a stub HTTP client (no live network). internal/websafe stays pure (the
// HTTP client is injected via its Deps seam) — orchestrate remains the only impure module.

// Read deadlines for the container-internal loader socket.
//
// websafeReadHeaderTimeout bounds how long a connected peer may take to send its
// request headers (the slowloris window). It is set above websafe.DefaultBounds().
// Timeout (10s per fetch) so it can never be the thing that cuts off a legitimate
// caller. websafeIdleTimeout bounds an idle keep-alive connection afterwards.
const (
	websafeReadHeaderTimeout = 30 * time.Second
	websafeIdleTimeout       = 120 * time.Second
	// websafeShutdownGrace is how long an in-flight /load may finish after a stop
	// signal. It exceeds the per-fetch Bounds.Timeout (10s) so a fetch already in
	// progress can complete, and stays well inside systemd's default 90s
	// TimeoutStopSec so a stop never escalates to SIGKILL.
	websafeShutdownGrace = 15 * time.Second
)

// websafeDeps are the injectable seams runWebsafe drives, so the test can stub the HTTP
// client, the bearer, and the serve call without binding a real socket or hitting the network.
type websafeDeps struct {
	// Client is the outbound HTTP client the fetch core uses. The live wiring is the
	// SSRF-guarded websafe.SafeClient(DefaultBounds()); tests inject an httptest-backed stub.
	Client *http.Client
	// Bounds are the per-fetch resource limits shared by the client and the Loader.
	Bounds websafe.Bounds
	// Secret is the expected Bearer (EXTERNAL_WEB_LOADER_API_KEY) the handler enforces. The
	// live wiring reads it from the container env (the mounted 0600 EnvironmentFile). runWebsafe
	// refuses to serve when this is empty (fail-closed) — the pure loader's empty-secret
	// accept-any behavior is for unit tests only and must never reach the live serve path.
	Secret string
	// Serve binds the socket at addr and serves the handler until it errors. Stubbed in tests
	// so no real listener is bound; the live wiring is http.ListenAndServe.
	Serve func(ctx context.Context, addr string, h http.Handler) error
}

// newWebsafe builds the HIDDEN `villa websafe-serve` subcommand: the internal container
// entrypoint serving the OWUI external web-loader. Hidden because it is not a user verb — it
// runs only inside the villa-websafe container. The exit-code mapping lives in runWebsafe
// (return-not-Exit body; cobra RunE calls os.Exit), mirroring newDashboard.
func newWebsafe() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "websafe-serve",
		Hidden: true,
		Short:  "Serve the OWUI external web-loader (internal; runs inside the villa-websafe container)",
		Long: "Serve the VillaStraylight web-safe loader: the SSRF-guarded, bounded fetch core that is " +
			"the sole producer of Open WebUI page_content. This is an INTERNAL container " +
			"entrypoint — OWUI reaches it over villa.network by container DNS only (no host port). Not a " +
			"user-facing command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveWebsafeDeps()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "websafe: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runWebsafe(cmd, args, deps))
			return nil
		},
	}
	cmd.Flags().String("host", "0.0.0.0", "container-internal bind host (never a host port)")
	cmd.Flags().Int("port", 8090, "container-internal listen port")
	return cmd
}

// runWebsafe builds the internal/websafe Loader (over the injected SSRF-guarded client +
// Bounds) and Server (with the container-env Bearer), reads host/port from the flags, prints
// the container-internal listen addr, and serves the /load handler. It RETURNS the exit code
// (no os.Exit in the body) so websafe_test.go drives it deterministically with a stubbed
// Serve + stub client.
func runWebsafe(cmd *cobra.Command, _ []string, d *websafeDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	host, err := cmd.Flags().GetString("host")
	if err != nil {
		fmt.Fprintf(errOut, "websafe: %v\n", err)
		return exitBlocked
	}
	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		fmt.Fprintf(errOut, "websafe: %v\n", err)
		return exitBlocked
	}

	// Fail closed on an empty bearer: the pure loader's authOK treats an empty secret as
	// "accept any caller", which is fine for unit tests but must NEVER reach the live serve
	// path — an unauthenticated /load on villa.network is a trust-boundary fail-open. The
	// install flow always generates a crypto/rand bearer into the 0600 EnvironmentFile, so an
	// empty value here means that file was lost/tampered; refuse with remediation.
	if d.Secret == "" {
		fmt.Fprintf(errOut, "websafe: refusing to serve with an empty bearer (EXTERNAL_WEB_LOADER_API_KEY unset); "+
			"the villa-websafe unit's 0600 EnvironmentFile must supply it — re-run `villa install` to regenerate\n")
		return exitBlocked
	}

	loader := websafe.NewLoader(websafe.Deps{Client: d.Client}, d.Bounds)
	srv := websafe.NewServer(loader, d.Secret)
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	fmt.Fprintf(out, "villa websafe-serve listening on http://%s (container-internal)\n", addr)

	if err := d.Serve(cmdContext(cmd), addr, srv.Handler()); err != nil {
		fmt.Fprintf(errOut, "websafe: serve: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// liveWebsafeDeps wires websafeDeps to the real container runtime: the SSRF-guarded
// SafeClient over the conservative DefaultBounds (so the connect-time Control hook validates
// every dialed IP), the Bearer read from the container env EXTERNAL_WEB_LOADER_API_KEY
// (sourced from the 0600 EnvironmentFile the villa-websafe unit mounts — never the 0644 unit),
// and a Serve that binds the container-internal socket.
//
// Serve builds an explicit http.Server rather than calling http.ListenAndServe so the
// read deadlines below are set. http.ListenAndServe uses a zero-value server, which has
// NO read deadline at all: a peer that connects and dribbles request headers holds a
// connection indefinitely, and enough of them wedge the loader (slowloris). This service
// binds 0.0.0.0 inside the container and is reachable by anything on villa.network, so it
// is the more exposed of villa's two servers even though no host port is published.
//
// Deliberately NO WriteTimeout: /load performs bounded outbound fetches (Bounds.Timeout
// per fetch, MaxConcurrent in flight), so a legitimate response can take a while. The
// fetch bounds already cap that work; an absolute write deadline would cut off a
// correct-but-slow batch. ReadHeaderTimeout is set well above Bounds.Timeout for the
// same reason.
func liveWebsafeDeps() (*websafeDeps, error) {
	bounds := websafe.DefaultBounds()
	return &websafeDeps{
		Client: websafe.SafeClient(bounds),
		Bounds: bounds,
		Secret: os.Getenv("EXTERNAL_WEB_LOADER_API_KEY"),
		Serve: func(ctx context.Context, addr string, h http.Handler) error {
			return serveUntilCancelled(ctx, websafeHTTPServer(addr, h))
		},
	}, nil
}

// serveUntilCancelled runs srv until it errors or ctx is cancelled, shutting down
// gracefully on cancellation and reporting a clean stop as nil.
//
// This mirrors (*dashboard.Server).Serve. Both processes are systemd units stopped
// with SIGTERM, and main cancels the command context on that signal; a server that
// ignored it would sit through the stop until systemd escalated to SIGKILL,
// turning every `systemctl stop` into a 90-second wait.
func serveUntilCancelled(ctx context.Context, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), websafeShutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	}
}

// websafeHTTPServer builds the configured loader http.Server. It is separate from
// the Serve closure so a test can assert the deadlines are actually set on the
// PRODUCTION server without binding a socket.
func websafeHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: websafeReadHeaderTimeout,
		IdleTimeout:       websafeIdleTimeout,
	}
}
