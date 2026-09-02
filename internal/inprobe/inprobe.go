// Package inprobe is the in-network probe module: one home for the doctrine
// every in-network curl probe in this project must follow, behind a small
// interface.
//
// The probe strategy itself is a podman-run curl inside villa.network. The
// PROCESS invocation stays in the cmd tier (the OS-orchestration layer that
// legitimately invokes podman — see internal/inference's TestSeamGrepGate),
// bound in through the Exec seam. Two adapters cross that seam: the live
// podman-run adapter in cmd/villa, and the hermetic fakes in tests. What lives
// HERE is everything that used to be scattered across the cmd files and
// duplicated between two near-identical mappers:
//
//   - ExitCode: the single point that decides "a genuine process exit code" vs
//     "the process never started" — the distinction the egress negative
//     control's honesty rests on.
//   - MapCoded / MapLiveness: the typed-Unknown health mapping. A podman-level
//     failure means the probe was unevaluable → HealthUnknown, NEVER a
//     fabricated confident state; a curl-level failure was evaluated
//     in-network → a confident down.
//   - Prober: the fixed-arg HTTP-code probe (curl writes ONLY the code) with
//     both mappings behind one interface.
//   - PairCache: the TTL-bounded pair refresh that caps dashboard poll churn
//     at one probe pair per window.
//
// PURE of process I/O: no os/exec invocation, no container-image literal, so
// the seam grep gate stays green over this package. The exec dependency is
// accepted, never created.
package inprobe

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// Exec runs curl in-network via a helper container and returns curl's stdout
// together with the process exit code. It is the seam the live podman-run
// adapter (cmd tier) and the hermetic test fakes both satisfy.
type Exec func(ctx context.Context, helperImage string, curlArgs ...string) (stdout []byte, exitCode int, err error)

// ExitCode is the load-bearing exit-code mapping: the SINGLE point that decides
// "this is a genuine process exit code" vs "the process never started" — the
// distinction the egress negative control's honesty rests on:
//
//   - runErr is an *exec.ExitError (the process ran and exited non-zero):
//     return its ExitCode(). podman propagates the container process's (curl's)
//     exit code unchanged, so a curl CONNECTION/TIMEOUT (6/7/28) surfaces here
//     and a classifier reads it as a genuine block.
//   - runErr is anything else (binary missing, podman daemon error, context
//     cancel/timeout — a non-ExitError failure): the process never produced an
//     exit code, so return -1. The caller MUST treat -1 as infrastructure
//     ("the probe could not run"), NEVER as a curl exit value and NEVER as a
//     block.
//
// runErr == nil is not this function's concern (callers short-circuit to 0).
func ExitCode(runErr error) int {
	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// MapCoded maps one HTTP-code probe outcome to a HealthState with the
// typed-Unknown doctrine: an HTTP code written by curl is a CONFIDENT verdict
// (200→ready, 503→loading, anything else→down); a curl-level failure (exit
// < 125 — connect refused/timeout INSIDE villa.network) is a confident down
// (the network was evaluable, the service was not there); a podman-level
// failure (exit 125/126/127, or podman absent/unstartable → -1) means the
// probe itself was unevaluable → HealthUnknown, NEVER a fabricated confident
// state.
func MapCoded(out []byte, exitCode int, err error) status.HealthState {
	if err != nil {
		if exitCode >= 125 || exitCode < 0 {
			return status.HealthUnknown // podman-level: could not evaluate the probe
		}
		return status.HealthDown // curl-level: evaluated in-network, service unreachable
	}
	switch strings.TrimSpace(string(out)) {
	case "200":
		return status.HealthReady
	case "503":
		return status.HealthLoading
	default:
		return status.HealthDown
	}
}

// MapLiveness maps a liveness-probe outcome for a server with NO health route:
// ANY HTTP code curl wrote means the server answered (→ready — a 401/400/405
// from a single-route server proves it is up); a curl-level failure is a
// confident down; a podman-level failure is unevaluable → HealthUnknown, NEVER
// a fabricated confident state. curl writes "000" when it got no HTTP response
// despite exit 0 (rare); that is down, never a fabricated ready.
func MapLiveness(out []byte, exitCode int, err error) status.HealthState {
	if err != nil {
		if exitCode >= 125 || exitCode < 0 {
			return status.HealthUnknown // podman-level: could not evaluate the probe
		}
		return status.HealthDown // curl-level: evaluated in-network, service unreachable
	}
	if code := strings.TrimSpace(string(out)); code != "" && code != "000" {
		return status.HealthReady
	}
	return status.HealthDown
}

// Prober probes URLs in-network and maps outcomes to HealthState. The fixed
// curl args make curl write ONLY the HTTP code (-s -o /dev/null -w
// %{http_code}); Timeout bounds the WHOLE probe (container start + curl) via
// the context handed to Exec, while MaxTime bounds curl's HTTP leg via
// --max-time.
type Prober struct {
	Exec    Exec
	Image   func() string // the helper image accessor — never a re-typed literal
	Timeout time.Duration // whole-probe bound (context)
	MaxTime time.Duration // curl HTTP-leg bound (--max-time)
}

// run performs one fixed-arg HTTP-code probe against url.
func (p Prober) run(url string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.Timeout)
	defer cancel()
	return p.Exec(ctx, p.Image(),
		"-s", "-o", "/dev/null", "-w", "%{http_code}",
		"--max-time", strconv.Itoa(int(p.MaxTime/time.Second)),
		url)
}

// Coded probes url and maps the outcome via MapCoded (a health route that
// speaks 200/503).
func (p Prober) Coded(url string) status.HealthState {
	return MapCoded(p.run(url))
}

// Liveness probes url and maps the outcome via MapLiveness (a server with no
// health route — any HTTP answer proves it up).
func (p Prober) Liveness(url string) status.HealthState {
	return MapLiveness(p.run(url))
}

// PairCache is the TTL-bounded pair refresh: one refresh probes BOTH services
// of a pair together, and every further read within the TTL window is served
// from the cache, so a dashboard poll spawns at most one probe pair per
// window. Mutex-guarded, keyed only on time (single config per process).
type PairCache struct {
	TTL time.Duration

	mu     sync.Mutex
	at     time.Time
	first  status.HealthState
	second status.HealthState
}

// Pair returns the cached pair, invoking refresh for both values together when
// the TTL window has lapsed.
func (c *PairCache) Pair(refresh func() (status.HealthState, status.HealthState)) (status.HealthState, status.HealthState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < c.TTL {
		return c.first, c.second
	}
	c.first, c.second = refresh()
	c.at = time.Now()
	return c.first, c.second
}

// Reset clears the cache so the next Pair call refreshes (test hook — a cold
// start per test, never a stale pair leaking across tests).
func (c *PairCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = time.Time{}
}
