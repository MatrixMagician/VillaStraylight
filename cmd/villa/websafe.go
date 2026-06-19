package main

import (
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// websafe.go is the thin cobra caller for the HIDDEN `villa websafe-serve` subcommand
// (Phase-31 GUARD-01/GROUND-01): the internal container entrypoint that runs inside the
// villa-websafe Quadlet container (the host villa binary bind-mounted read-only). It builds
// the internal/websafe pure fetch core behind the live SSRF-guarded SafeClient and serves
// the verified OWUI external-loader HTTP contract (the /load handler) on a container-internal
// socket. It is NOT user-facing — OWUI reaches it over villa.network by container DNS only
// (no host port, PRIV-01); the bearer comes from the 0600 EnvironmentFile the unit mounts
// (EXTERNAL_WEB_LOADER_API_KEY in the container env), never the 0644 unit.
//
// Mirroring dashboard.go: the cobra RunE wires liveWebsafeDeps() then os.Exit(runWebsafe(...));
// runWebsafe RETURNS the exit code (no os.Exit in the body) so websafe_test.go drives it with
// a stubbed Serve + a stub HTTP client (no live network). internal/websafe stays pure (the
// HTTP client is injected via its Deps seam) — orchestrate remains the only impure module.

// websafeDeps are the injectable seams runWebsafe drives, so the test can stub the HTTP
// client, the bearer, and the serve call without binding a real socket or hitting the network.
type websafeDeps struct {
	// Client is the outbound HTTP client the fetch core uses. The live wiring is the
	// SSRF-guarded websafe.SafeClient(DefaultBounds()); tests inject an httptest-backed stub.
	Client *http.Client
	// Bounds are the per-fetch resource limits shared by the client and the Loader.
	Bounds websafe.Bounds
	// Secret is the expected Bearer (EXTERNAL_WEB_LOADER_API_KEY) the handler enforces. The
	// live wiring reads it from the container env (the mounted 0600 EnvironmentFile); an empty
	// secret accepts any villa.network caller (documented fallback posture).
	Secret string
	// Serve binds the socket at addr and serves the handler until it errors. Stubbed in tests
	// so no real listener is bound; the live wiring is http.ListenAndServe.
	Serve func(addr string, h http.Handler) error
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
			"the sole producer of Open WebUI page_content (GUARD-01). This is an INTERNAL container " +
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
	cmd.Flags().String("host", "0.0.0.0", "container-internal bind host (never a host port; PRIV-01)")
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

	loader := websafe.NewLoader(websafe.Deps{Client: d.Client}, d.Bounds)
	srv := websafe.NewServer(loader, d.Secret)
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	fmt.Fprintf(out, "villa websafe-serve listening on http://%s (container-internal)\n", addr)

	if err := d.Serve(addr, srv.Handler()); err != nil {
		fmt.Fprintf(errOut, "websafe: serve: %v\n", err)
		return exitBlocked
	}
	return exitPass
}

// liveWebsafeDeps wires websafeDeps to the real container runtime: the SSRF-guarded
// SafeClient over the conservative DefaultBounds (so the connect-time Control hook validates
// every dialed IP, GUARD-05), the Bearer read from the container env EXTERNAL_WEB_LOADER_API_KEY
// (sourced from the 0600 EnvironmentFile the villa-websafe unit mounts — never the 0644 unit),
// and a Serve that binds the container-internal socket via http.ListenAndServe.
func liveWebsafeDeps() (*websafeDeps, error) {
	bounds := websafe.DefaultBounds()
	return &websafeDeps{
		Client: websafe.SafeClient(bounds),
		Bounds: bounds,
		Secret: os.Getenv("EXTERNAL_WEB_LOADER_API_KEY"),
		Serve:  func(addr string, h http.Handler) error { return http.ListenAndServe(addr, h) },
	}, nil
}
