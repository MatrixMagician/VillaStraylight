package inprobe

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// TestExitCode anchors the load-bearing exit-code mapping with real fixed-arg
// execs (no podman/curl needed — hermetic and off-hardware).
func TestExitCode(t *testing.T) {
	t.Run("process ran and exited non-zero → its real exit code", func(t *testing.T) {
		runErr := exec.Command("sh", "-c", "exit 7").Run()
		if runErr == nil {
			t.Fatalf("expected a non-nil run error for `sh -c 'exit 7'`")
		}
		if got := ExitCode(runErr); got != 7 {
			t.Errorf("ExitCode(exit-7 error) = %d, want 7", got)
		}
	})

	t.Run("binary never started → -1, NEVER a curl exit value (infra, never a block)", func(t *testing.T) {
		runErr := exec.Command("villa-no-such-binary-inprobe-exitcode-xyzzy").Run()
		if runErr == nil {
			t.Fatalf("expected a non-nil run error for a nonexistent binary")
		}
		if _, ok := errors.AsType[*exec.ExitError](runErr); ok {
			t.Fatalf("a nonexistent binary must NOT surface as *exec.ExitError; got %T", runErr)
		}
		if got := ExitCode(runErr); got != -1 {
			t.Errorf("ExitCode(never-started error) = %d, want -1", got)
		}
	})
}

// TestMapCoded proves the typed-Unknown doctrine for a health route that speaks
// HTTP codes: a code is a CONFIDENT verdict; a curl-level failure is a
// confident down; a podman-level failure is unevaluable → HealthUnknown, never
// a fabricated confident state.
func TestMapCoded(t *testing.T) {
	cases := []struct {
		name string
		out  string
		code int
		err  error
		want status.HealthState
	}{
		{"200 → ready", "200", 0, nil, status.HealthReady},
		{"503 → loading", "503", 0, nil, status.HealthLoading},
		{"404 → down", "404", 0, nil, status.HealthDown},
		{"curl connect failure (exit 7) → down", "", 7, errors.New("exit status 7"), status.HealthDown},
		{"podman generic failure (exit 125) → unknown", "", 125, errors.New("exit status 125"), status.HealthUnknown},
		{"podman command not found (exit 127) → unknown", "", 127, errors.New("exit status 127"), status.HealthUnknown},
		{"podman absent (could not start) → unknown", "", -1, errors.New("exec: podman: not found"), status.HealthUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MapCoded([]byte(c.out), c.code, c.err); got != c.want {
				t.Errorf("MapCoded(%q, %d, %v) = %q, want %q", c.out, c.code, c.err, got, c.want)
			}
		})
	}
}

// TestMapLiveness proves the no-health-route variant: ANY HTTP answer proves
// the server up (401/400/405 from a single-route server included); curl "000"
// honesty (no HTTP response despite exit 0) is down, never a fabricated ready.
func TestMapLiveness(t *testing.T) {
	cases := []struct {
		name string
		out  string
		code int
		err  error
		want status.HealthState
	}{
		{"200 → ready", "200", 0, nil, status.HealthReady},
		{"401 (single-route server answered) → ready", "401", 0, nil, status.HealthReady},
		{"405 → ready", "405", 0, nil, status.HealthReady},
		{"curl 000 (no HTTP response despite exit 0) → down, never fabricated ready", "000", 0, nil, status.HealthDown},
		{"empty output → down", "", 0, nil, status.HealthDown},
		{"curl connect failure (exit 7) → down", "", 7, errors.New("exit status 7"), status.HealthDown},
		{"podman-level failure (exit 126) → unknown", "", 126, errors.New("exit status 126"), status.HealthUnknown},
		{"podman absent → unknown", "", -1, errors.New("exec: podman: not found"), status.HealthUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MapLiveness([]byte(c.out), c.code, c.err); got != c.want {
				t.Errorf("MapLiveness(%q, %d, %v) = %q, want %q", c.out, c.code, c.err, got, c.want)
			}
		})
	}
}

// TestProberArgs pins the fixed-arg probe contract: curl writes ONLY the HTTP
// code, --max-time carries the HTTP-leg bound, the URL is the last arg, and the
// helper image flows from the accessor — never a re-typed literal.
func TestProberArgs(t *testing.T) {
	var gotImage string
	var gotArgs []string
	p := Prober{
		Exec: func(_ context.Context, helperImage string, curlArgs ...string) ([]byte, int, error) {
			gotImage = helperImage
			gotArgs = curlArgs
			return []byte("200"), 0, nil
		},
		Image:   func() string { return "helper-image-from-accessor" },
		Timeout: 10_000_000_000, // 10s
		MaxTime: 3_000_000_000,  // 3s
	}
	if got := p.Coded("http://villa-qdrant:6333/readyz"); got != status.HealthReady {
		t.Fatalf("Coded = %q, want ready", got)
	}
	if gotImage != "helper-image-from-accessor" {
		t.Errorf("helper image = %q, want the accessor value", gotImage)
	}
	want := []string{"-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "3", "http://villa-qdrant:6333/readyz"}
	if strings.Join(gotArgs, " ") != strings.Join(want, " ") {
		t.Errorf("curl args = %v, want %v", gotArgs, want)
	}
}

// TestPairCache proves the TTL pair discipline: one refresh probes BOTH
// services together, every further read within the window is a cache hit, and
// Reset forces a cold refresh.
func TestPairCache(t *testing.T) {
	refreshes := 0
	c := &PairCache{TTL: 15_000_000_000} // 15s
	refresh := func() (status.HealthState, status.HealthState) {
		refreshes++
		return status.HealthReady, status.HealthLoading
	}

	a, b := c.Pair(refresh)
	if a != status.HealthReady || b != status.HealthLoading {
		t.Fatalf("first Pair = (%q, %q), want (ready, loading)", a, b)
	}
	if refreshes != 1 {
		t.Fatalf("first Pair must refresh exactly once: refreshes = %d", refreshes)
	}
	_, _ = c.Pair(refresh)
	_, _ = c.Pair(refresh)
	if refreshes != 1 {
		t.Errorf("calls within the TTL window must be cache hits: refreshes = %d, want 1", refreshes)
	}
	c.Reset()
	_, _ = c.Pair(refresh)
	if refreshes != 2 {
		t.Errorf("Reset must force a cold refresh: refreshes = %d, want 2", refreshes)
	}
}
