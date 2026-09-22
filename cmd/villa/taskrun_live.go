package main

// taskrun_live.go binds internal/taskrun's Deps to the host: podman for the
// sandbox launch and the kill by name, the pin resolver for the sandbox image
// (the same path preflight's PRE-09 resolves it through, never a literal here),
// the chat unit over loopback for the grounding audit, PRE-09 itself for the
// readiness refusal, and a contained read of workspace files. It is the ONE
// place the runner touches the host.
//
// The bridge over the container's stdio is the whole channel (spec v1.11 3.4):
// events are JSON lines off stdout, commands are JSON lines into stdin, and a
// cancel is `podman kill` by the container's name, since killing the attached
// podman client would leave the container running.

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/taskrun"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
	"github.com/MatrixMagician/VillaStraylight/internal/workspace"
)

// auditTimeout bounds one grounding call. The spec's measurements ran 13 to
// 42 s per document; five minutes leaves room for a long document on a busy
// unit without letting a wedged server hold the task forever.
const auditTimeout = 5 * time.Minute

// liveTaskRunDeps wires the runner to the host. endpoint is the inference
// unit's loopback URL the dashboard already scrapes, so the audit talks to the
// exact server the status row reports on.
func liveTaskRunDeps(ctx context.Context, endpoint string) taskrun.Deps {
	return taskrun.Deps{
		Store:      taskstore.New(pathsafe.DataRoot()),
		LoadConfig: config.LoadVilla,
		Launch:     liveLaunchSandbox,
		KillByName: func(name string) error { return liveKillContainer(ctx, name) },
		RenderArgs: liveRenderSandboxArgs,
		Registered: func(cfg config.VillaConfig, path string) (string, bool) {
			return workspace.Registered(cfg, path, liveWorkspaceDeps())
		},
		ToolsOn:      subsystem.ToolsOn,
		SandboxReady: liveSandboxReady,
		Audit:        liveGroundingAudit(endpoint),
		ReadFile:     liveWorkspaceRead,
		Now:          time.Now,
		Rand:         rand.Reader,
	}
}

// liveSandboxImage resolves the effective sandbox image through the pin
// resolver, the vetted constant being the fallback, the same shape
// liveSandboxDeps uses for PRE-09's functional probe.
func liveSandboxImage() string {
	if res, ok := liveResolver().Resolve(pins.SandboxImage); ok && res.Current.Ref != "" {
		return res.Current.Ref
	}
	return orchestrate.SandboxImage()
}

// liveRenderSandboxArgs renders one task's podman arguments: the pinned image,
// the running villa binary and the villa-owned Crush binary bind-mounted in.
//
// It re-runs workspace.CheckGrant against ws before rendering anything: the
// grant may predate the sensitive-directory and executable-containment
// checks (GHSA-3q4q-7cmw-m22m), since it was validated only once, at
// registration, and config.toml is hand-editable. This is the actual
// launch — Submit's Registered lookup at task-creation time only confirms
// list membership.
func liveRenderSandboxArgs(cfg config.VillaConfig, ws, id string) ([]string, error) {
	if err := workspace.CheckGrant(ws, liveWorkspaceDeps()); err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("taskrun: locate the villa binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return orchestrate.RenderSandboxRun(orchestrate.SandboxRunInput{
		Cfg:           cfg,
		Image:         liveSandboxImage(),
		Workspace:     ws,
		TaskID:        id,
		HostVillaPath: self,
		CrushPath:     agentBinPath(),
	})
}

// liveSandboxReady is PRE-09's verdict as a refusal: a confident FAIL refuses
// with the check's finding, a WARN (unevaluable) does not, since the launch
// itself will refuse honestly if the microVM cannot start.
func liveSandboxReady() (bool, string) {
	for _, res := range preflight.RunSandbox(liveSandboxDeps()) {
		if res.Status == preflight.StatusFail {
			return false, res.Detail + ". " + res.Remediation
		}
	}
	return true, ""
}

// liveGroundingAudit runs the second pass against the chat unit over loopback
// with the configured model. grounding.Prompt pins temperature 0 and disables
// thinking; the deltas are gathered into the one string the parser reads.
func liveGroundingAudit(endpoint string) func(context.Context, grounding.Document, []grounding.Source) grounding.DocumentReport {
	client := llm.NewOpenAIClient(llm.Options{
		BaseURL: strings.TrimRight(endpoint, "/") + "/v1",
		Timeout: auditTimeout,
	})
	return func(ctx context.Context, doc grounding.Document, sources []grounding.Source) grounding.DocumentReport {
		cfg, err := config.LoadVilla()
		if err != nil {
			return grounding.DocumentReport{Path: doc.Path, Err: "load config: " + err.Error()}
		}
		return grounding.Audit(ctx, grounding.Deps{
			Complete: func(ctx context.Context, req llm.ChatRequest) (string, error) {
				req.Model = cfg.Model
				var sb strings.Builder
				err := client.StreamChat(ctx, req, func(delta string) error {
					sb.WriteString(delta)
					return nil
				})
				return sb.String(), err
			},
		}, doc, sources)
	}
}

// maxGroundingReadBytes caps a workspace file read for the grounding audit.
// This runs on the HOST against guest-reported, guest-controlled content, so
// an oversize or endless source must not make the long-lived dashboard
// service buffer without bound (GHSA-478j-frrx-f99c).
const maxGroundingReadBytes = 4 << 20 // 4 MiB

// liveWorkspaceRead reads one workspace-relative file for the grounding
// audit. The bridge reports paths, and a path is untrusted input: the
// in-VM agent can steer a name at a symlink to a host secret outside the
// workspace, or at a FIFO or device node, and report it as written or read
// (GHSA-478j-frrx-f99c). pathsafe.Inside refuses a lexical escape; Lstat then
// refuses anything whose leaf is not already a plain regular file — this is
// what keeps a FIFO from ever reaching open, where reading it would block
// the service forever; O_NOFOLLOW is the TOCTOU-safe refusal of a symlink
// leaf; io.LimitReader bounds a legitimate-looking huge file.
func liveWorkspaceRead(ws, rel string) ([]byte, error) {
	path := filepath.Join(ws, rel)
	if err := pathsafe.Inside(path, ws); err != nil {
		return nil, err
	}

	lst, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !lst.Mode().IsRegular() {
		return nil, fmt.Errorf("taskrun: %s is not a regular file", rel)
	}

	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxGroundingReadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxGroundingReadBytes {
		return nil, fmt.Errorf("taskrun: %s exceeds the %d byte read limit", rel, maxGroundingReadBytes)
	}
	return data, nil
}

// liveKillContainer is `podman kill <name>`, fixed-arg.
func liveKillContainer(ctx context.Context, name string) error {
	out, err := exec.CommandContext(ctx, "podman", "kill", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman kill %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// execBridge is one launched sandbox: the podman client process attached to
// the container's stdio.
type execBridge struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events <-chan crushapi.Event
	name   string

	waitOnce sync.Once
	waitErr  error
}

// liveLaunchSandbox starts `podman <args>` with stdin and stdout piped. The
// container's stderr (Crush's own logging) goes to the service's stderr, which
// is the journal under systemd.
func liveLaunchSandbox(ctx context.Context, args []string) (taskrun.Bridge, error) {
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("taskrun: sandbox stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("taskrun: sandbox stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("taskrun: start the sandbox: %w", err)
	}
	return &execBridge{
		cmd:    cmd,
		stdin:  stdin,
		events: readBridgeEvents(stdout),
		name:   containerNameFromArgs(args),
	}, nil
}

func (b *execBridge) Events() <-chan crushapi.Event { return b.events }

func (b *execBridge) Send(c crushapi.Command) error {
	line, err := json.Marshal(crushapi.Line{Command: &c})
	if err != nil {
		return err
	}
	_, err = b.stdin.Write(append(line, '\n'))
	return err
}

func (b *execBridge) Wait() error {
	b.waitOnce.Do(func() {
		_ = b.stdin.Close()
		b.waitErr = b.cmd.Wait()
	})
	return b.waitErr
}

func (b *execBridge) Kill() error {
	if b.name == "" {
		return fmt.Errorf("taskrun: sandbox has no container name to kill")
	}
	return liveKillContainer(context.Background(), b.name)
}

// readBridgeEvents decodes the bridge's stdout into events until EOF. A line
// that is not an event line is skipped: stray output must not end a task.
func readBridgeEvents(r io.Reader) <-chan crushapi.Event {
	out := make(chan crushapi.Event, 64)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var line crushapi.Line
			if err := json.Unmarshal(sc.Bytes(), &line); err != nil || line.Event == nil {
				continue
			}
			out <- *line.Event
		}
	}()
	return out
}

// containerNameFromArgs lifts the --name value out of the rendered run line,
// so Kill targets the container rather than the attached client.
func containerNameFromArgs(args []string) string {
	for i, a := range args {
		if a == "--name" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
