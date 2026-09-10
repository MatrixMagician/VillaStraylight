package main

// sandbox_bridge.go is the in-sandbox half of the workspace agent: the process
// the microVM's entrypoint runs (spec v1.11 4).
//
// It is the ONLY villa verb that never runs on the host. Inside the guest it
// starts `crush server` on a guest-local unix socket, asserts Crush's version
// against the pin, creates the workspace, and then relays: SSE events out to
// stdout as JSON lines, Command lines in from stdin to API calls.
//
// The container's stdio is the whole boundary. A unix socket, a published port
// and `podman exec` were each measured not to cross a krun boundary, so the
// socket here is guest-local and never bind-mounted, and nothing on the host
// reaches in except by writing a line.
//
// Two rules this file must keep. It carries NO backend literal (TestSeamGrepGate
// walks cmd/villa), and it must work in the CGO-free static binary, since that
// binary is bind-mounted into the guest and exec'd there.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
)

// sandboxChild is the started `crush server` process. Two methods, because two
// things happen to it: it is waited on, and it is stopped.
type sandboxChild interface {
	Wait() error
	Stop() error
}

// sandboxBridgeDeps is the bridge's host seam. Everything that touches the guest
// (starting a process, waiting for a socket, writing a file) is a func field, so
// the relay logic is driven in tests by a fake HTTP server and a fake child.
type sandboxBridgeDeps struct {
	// BaseURL is where crush server answers. Over a unix socket the host part is
	// ignored by the transport, so it is a placeholder there.
	BaseURL string
	// HTTPClient carries the unix-socket dialer in the live path.
	HTTPClient *http.Client
	// Workspace is the guest path the task runs in.
	Workspace string
	// Vetted returns the pinned Crush version the running binary must match.
	Vetted func() string
	// WriteConfig renders and writes crush.json where the child will read it.
	WriteConfig func() error
	// StartCrush launches `crush server`.
	StartCrush func(ctx context.Context) (sandboxChild, error)
	// WaitReady blocks until the server answers or gives up.
	WaitReady func(ctx context.Context) error

	Stdin  io.Reader
	Stdout io.Writer
	Now    func() time.Time
}

// newSandboxBridge builds `villa sandbox-bridge`. It is hidden: an operator never
// runs it, the sandbox render does (orchestrate.RenderSandboxRun).
func newSandboxBridge() *cobra.Command {
	var (
		baseURL   = orchestrate.LlamaInNetworkEndpoint()
		model     string
		ctxTokens int
		workspace = "/workspace"
		socket    = "/tmp/crush.sock"
		configDir = "/tmp/crushcfg"
	)
	cmd := &cobra.Command{
		Use:    "sandbox-bridge",
		Short:  "Relay a workspace task's Crush session over the sandbox's stdio (internal)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.VillaConfig{Model: model, Ctx: ctxTokens}
			return runSandboxBridge(cmdContext(cmd), liveSandboxBridgeDeps(cfg, baseURL, workspace, socket, configDir))
		},
	}
	f := cmd.Flags()
	f.StringVar(&baseURL, "base-url", baseURL, "OpenAI-compatible inference endpoint reachable from the sandbox network")
	f.StringVar(&model, "model", model, "served model id to advertise in the rendered crush.json")
	f.IntVar(&ctxTokens, "ctx", ctxTokens, "context window to advertise in the rendered crush.json")
	f.StringVar(&workspace, "workspace", workspace, "guest path of the workspace grant")
	f.StringVar(&socket, "socket", socket, "guest-local unix socket crush server binds")
	f.StringVar(&configDir, "config-dir", configDir, "guest directory the rendered crush.json is written to")
	return cmd
}

// liveSandboxBridgeDeps binds the bridge to the guest: a unix-socket HTTP
// transport, a fixed-arg `crush server` child, a health poll, and one file write.
func liveSandboxBridgeDeps(cfg config.VillaConfig, baseURL, workspace, socket, configDir string) sandboxBridgeDeps {
	hc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		},
	}
	// The host part is a placeholder: the dialer above ignores it and connects to
	// the socket. It must still be a legal URL host so requests can be built.
	const socketHost = "http://crush"

	d := sandboxBridgeDeps{
		BaseURL:    socketHost,
		HTTPClient: hc,
		Workspace:  workspace,
		Vetted:     vettedCrushVersion,
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Now:        time.Now,
	}
	d.WriteConfig = func() error {
		b, err := agent.RenderSandbox(cfg, baseURL)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			return fmt.Errorf("sandbox-bridge: create %s: %w", configDir, err)
		}
		return os.WriteFile(filepath.Join(configDir, "crush.json"), b, 0o600)
	}
	d.StartCrush = func(ctx context.Context) (sandboxChild, error) {
		c := exec.CommandContext(ctx, "crush", "server", "--host", "unix://"+socket, "--cwd", workspace)
		c.Stdout = io.Discard
		// Crush logs to stderr; it must not land on stdout, which is the protocol.
		c.Stderr = os.Stderr
		if err := c.Start(); err != nil {
			return nil, fmt.Errorf("sandbox-bridge: start crush server: %w", err)
		}
		return execChild{c}, nil
	}
	d.WaitReady = func(ctx context.Context) error {
		return pollHealth(ctx, hc, socketHost)
	}
	return d
}

// execChild adapts an exec.Cmd to sandboxChild.
type execChild struct{ c *exec.Cmd }

func (e execChild) Wait() error { return e.c.Wait() }

func (e execChild) Stop() error {
	if e.c.Process == nil {
		return nil
	}
	return e.c.Process.Kill()
}

// pollHealth waits for crush server's health route. Thirty seconds is generous
// for a process that has only a socket to bind; a longer wait would just delay
// an honest refusal.
func pollHealth(ctx context.Context, hc *http.Client, base string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/health", nil)
		if err != nil {
			return err
		}
		rsp, err := hc.Do(req)
		if err == nil {
			rsp.Body.Close()
			if rsp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("sandbox-bridge: crush server did not answer within 30s")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// vettedCrushVersion reads the pinned Crush release from the pin table. The pin
// is the authority; a binary that does not match it never gets a task.
func vettedCrushVersion() string {
	e, ok := pins.Lookup(pins.Crush)
	if !ok {
		return ""
	}
	return e.Version
}

// runSandboxBridge is the bridge's whole life: prepare, assert, relay, exit.
//
// The ordering is the contract. Nothing that could run a task happens before the
// version assertion, so a drifted Crush is refused with no workspace created and
// a non-zero exit — which is what makes "the pinned harness ran this" a fact and
// not a hope.
func runSandboxBridge(ctx context.Context, d sandboxBridgeDeps) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	emit := func(ev crushapi.Event) {
		ev.Time = d.Now()
		b, err := json.Marshal(crushapi.Line{Event: &ev})
		if err != nil {
			return
		}
		_, _ = d.Stdout.Write(append(b, '\n'))
	}
	refuse := func(err error) error {
		emit(crushapi.Event{Kind: crushapi.KindBridgeError, Error: err.Error()})
		return err
	}

	if err := d.WriteConfig(); err != nil {
		return refuse(err)
	}
	child, err := d.StartCrush(ctx)
	if err != nil {
		return refuse(err)
	}
	defer func() { _ = child.Stop() }()

	if err := d.WaitReady(ctx); err != nil {
		return refuse(err)
	}

	c := crushapi.New(d.BaseURL, d.HTTPClient)
	got, err := c.Version(ctx)
	if err != nil {
		return refuse(err)
	}
	if want := d.Vetted(); !sameCrushVersion(got, want) {
		return refuse(fmt.Errorf(
			"sandbox-bridge: crush version drift: the sandbox runs %s, villa vetted %s; rebuild the sandbox image or re-run `villa install`",
			got, want))
	}

	ws, err := c.CreateWorkspace(ctx, d.Workspace)
	if err != nil {
		return refuse(err)
	}

	events, streamErr := c.Events(ctx, ws)
	emit(crushapi.Event{Kind: crushapi.KindBridgeReady, Version: got})

	commands := readCommands(ctx, d.Stdin)
	var sid crushapi.SessionID

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-events:
			if !ok {
				// The stream ended before run_complete. Report the reason rather
				// than exiting silently: a dropped stream and a finished task look
				// identical from the host otherwise.
				if err := <-streamErr; err != nil {
					return refuse(err)
				}
				return nil
			}
			emit(ev)
			if ev.Kind == crushapi.KindRunComplete {
				cancel()
				_ = child.Stop()
				_ = child.Wait()
				return nil
			}

		case cm, ok := <-commands:
			if !ok {
				// stdin EOF: the host is done with this task.
				cancel()
				_ = child.Stop()
				_ = child.Wait()
				return nil
			}
			if err := applyCommand(ctx, c, ws, &sid, cm); err != nil {
				emit(crushapi.Event{Kind: crushapi.KindBridgeError, Error: err.Error()})
			}
		}
	}
}

// applyCommand turns one stdin line into one API call. An unknown kind is an
// error rather than a silent no-op: the runner must hear that its instruction
// went nowhere.
func applyCommand(ctx context.Context, c *crushapi.Client, ws crushapi.WorkspaceID, sid *crushapi.SessionID, cm crushapi.Command) error {
	switch cm.Kind {
	case crushapi.CmdPrompt:
		s, err := c.SendPrompt(ctx, ws, cm.Prompt)
		if err != nil {
			return err
		}
		*sid = s
		return nil
	case crushapi.CmdGrant:
		return c.Grant(ctx, ws, cm.PermissionID, cm.Answer)
	case crushapi.CmdCancel:
		if *sid == "" {
			return fmt.Errorf("sandbox-bridge: cancel before any prompt")
		}
		return c.Cancel(ctx, ws, *sid)
	default:
		return fmt.Errorf("sandbox-bridge: unknown command kind %q", cm.Kind)
	}
}

// readCommands decodes stdin into Command values on a channel, closing it at EOF.
// A line that will not parse is skipped: one malformed line must not end a task
// that is already running.
func readCommands(ctx context.Context, r io.Reader) <-chan crushapi.Command {
	out := make(chan crushapi.Command)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var line crushapi.Line
			if err := json.Unmarshal(sc.Bytes(), &line); err != nil || line.Command == nil {
				continue
			}
			select {
			case out <- *line.Command:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// sameCrushVersion compares a reported version with the pin, tolerating only the
// leading "v". Anything else is drift: a pin names one release, not a range.
func sameCrushVersion(got, want string) bool {
	if want == "" {
		return false
	}
	return strings.TrimPrefix(got, "v") == strings.TrimPrefix(want, "v")
}
