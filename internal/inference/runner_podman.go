package inference

import (
	"bytes"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// This file holds the ONLY `podman` invocations in internal/ outside the detect
// AMD seam. The runner shells to the rootless `podman` CLI with fixed-arg
// exec.Command (no `sh -c`, no socket/bindings dependency — Phase-1 security
// baseline, T-02-08), the way internal/detect/gpu_amd.go runTool does.

// maxLogOutput bounds how much untrusted llama-server stderr we capture for the
// log-scrape, so a runaway/hostile process cannot exhaust memory (T-02-09; mirrors
// internal/detect maxToolOutput / internal/preflight's io.LimitReader bound).
const maxLogOutput = 8 << 10 // 8 KiB

// healthTimeout bounds a single readiness probe so an unreachable endpoint cannot
// hang the caller.
const healthTimeout = 2 * time.Second

// containerRunner is the Runner implementation that runs llama-server in a
// transient (--rm) rootless podman container. It captures stderr for the offload
// log-scrape and tears the container down on the host even when Start failed.
type containerRunner struct {
	backend  Backend
	spec     RunSpec
	stderr   boundedWriter // self-synchronized, bounded llama-server stderr capture
	captured bool
	started  bool
	client   *http.Client
	cmd      *exec.Cmd // the `podman run` child, reaped in Stop (WR-01)
}

// Compile-time assertion that containerRunner satisfies Runner.
var _ Runner = (*containerRunner)(nil)

// NewContainerRunner builds a container Runner for the given backend and run spec.
func NewContainerRunner(backend Backend, spec RunSpec) *containerRunner {
	return &containerRunner{
		backend: backend,
		spec:    spec,
		stderr:  boundedWriter{limit: maxLogOutput},
		client:  &http.Client{Timeout: healthTimeout},
	}
}

// Start launches the container via `podman run` with the backend-rendered fixed-arg
// slice, capturing bounded stderr. It does not block on readiness — callers poll
// Health. exec.LookPath is checked first so a missing podman is a clean error, not
// a panic.
func (r *containerRunner) Start(spec RunSpec) error {
	r.spec = spec
	if _, err := exec.LookPath("podman"); err != nil {
		return err
	}
	args := r.backend.ContainerArgs(spec) // fixed-arg slice; no shell interpolation
	cmd := exec.Command("podman", args...)

	// Bound the stderr capture so a runaway log cannot exhaust memory (T-02-09).
	// The writer is mutex-guarded so the os/exec copier goroutine can write while
	// Logs() reads the buffer mid-run (CR-04).
	cmd.Stderr = &r.stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	r.cmd = cmd
	r.started = true
	r.captured = true
	return nil
}

// Stop tears down the transient container on the host. It is idempotent and safe to
// defer: stopping a never-started or already-gone container is not an error here.
func (r *containerRunner) Stop() error {
	if _, err := exec.LookPath("podman"); err != nil {
		return err
	}
	// Fixed-arg stop of the named container; --rm removes it on stop.
	_ = exec.Command("podman", "stop", "-t", "5", r.spec.ContainerName).Run()
	// Reap the `podman run` child so we leak neither a zombie process nor its
	// stderr-copier goroutine (WR-01); Wait also blocks until the copier finishes,
	// quiescing the capture buffer. Guarded so a second Stop (the deferred safety
	// net) is a no-op rather than a double-Wait panic.
	if r.cmd != nil {
		_ = r.cmd.Wait()
		r.cmd = nil
	}
	r.started = false
	return nil
}

// Health probes the loopback endpoint for readiness, degrading to a typed-Unknown
// (WARN) when the probe cannot be evaluated — never a bare false.
func (r *containerRunner) Health() detect.Bool {
	if !r.started {
		return detect.UnknownBool("runner not started", "")
	}
	resp, err := r.client.Get(r.Endpoint() + "/health")
	if err != nil {
		return detect.UnknownBool("health probe failed (could not evaluate)", err.Error())
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxLogOutput))
	if resp.StatusCode == http.StatusOK {
		return detect.KnownBool(true, r.Endpoint()+"/health")
	}
	return detect.KnownBool(false, r.Endpoint()+"/health")
}

// Endpoint returns the loopback base URL the OpenAI-compatible API is published on.
func (r *containerRunner) Endpoint() string { return endpointURL() }

// Logs returns the captured (bounded) llama-server stderr for the offload scrape.
func (r *containerRunner) Logs() (string, bool) {
	return r.stderr.String(), r.captured
}

// boundedWriter caps how many bytes are written into its buffer, silently
// discarding the overflow (the io.Writer analogue of the io.LimitReader bound the
// detect/preflight tool capture uses, T-02-09). It is mutex-guarded so the os/exec
// stderr-copier goroutine (Write) and Logs() (String) can touch the buffer
// concurrently while the container is live without racing (CR-04).
type boundedWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		return len(p), nil // report consumed; drop the overflow
	}
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

// String returns the captured stderr so far, taking the lock so it never races an
// in-flight Write from the exec copier goroutine.
func (w *boundedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
