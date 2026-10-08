package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// stubHangingPodman puts a `podman` on PATH that records every argv (one line per
// call) and, for `run`, blocks like a container whose curl is still generating.
func stubHangingPodman(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nd=\"$(dirname \"$0\")\"\necho \"$*\" >> \"$d/calls\"\n" +
		"if [ \"$1\" = run ]; then exec sleep 30; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0o700); err != nil {
		t.Fatalf("write podman stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func recordedCalls(t *testing.T, dir string) []string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(dir, "calls"))
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// TestCancelledProbeStopsItsContainer guards #329: cancelling the context kills the
// `podman run` client but not the container, so curl kept llama-server decoding a
// round nothing was waiting for. A cancelled drive must name its container and
// `podman rm -f` it.
func TestCancelledProbeStopsItsContainer(t *testing.T) {
	dir := stubHangingPodman(t)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	_, err := runProbeCurlIn(ctx, "img", inference.CurlRequest{Args: []string{"http://x/y"}})
	if err == nil {
		t.Fatal("a cancelled probe returned no error")
	}

	calls := recordedCalls(t, dir)
	var name string
	for _, c := range calls {
		f := strings.Fields(c)
		for i, a := range f {
			if f[0] == "run" && a == "--name" && i+1 < len(f) {
				name = f[i+1]
			}
		}
	}
	if name == "" {
		t.Fatalf("podman run was not given --name, so nothing can stop it; calls: %q", calls)
	}
	if want := "rm -f " + name; !containsLine(calls, want) {
		t.Errorf("no %q after cancel; calls: %q", want, calls)
	}
}

// TestProbeNamesAreUniquePerDrive guards the name against two overlapping drives
// (the residency rounds run concurrently) removing each other's container.
func TestProbeNamesAreUniquePerDrive(t *testing.T) {
	a, b := probeContainerName(), probeContainerName()
	if a == b || !strings.HasPrefix(a, "villa-probe-") {
		t.Errorf("names %q and %q, want distinct villa-probe-* names", a, b)
	}
}

// TestProbeCurlMaxTimeFollowsTheDeadline guards the belt-and-braces bound: with a
// deadline on the context, curl is told to give up when it passes, and a caller's own
// --max-time (given in the curl args) still wins because curl honours the last one.
func TestProbeCurlMaxTimeFollowsTheDeadline(t *testing.T) {
	dir := stubRecordingPodman(t)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	_, _ = runProbeCurlIn(ctx, "img", inference.CurlRequest{Args: []string{"http://x/y"}}, "-sf", "--max-time", "5")
	argv, _ := os.ReadFile(filepath.Join(dir, "argv"))
	s := string(argv)
	first, last := strings.Index(s, "--max-time\n"), strings.LastIndex(s, "--max-time\n")
	if first < 0 || first == last {
		t.Fatalf("want a deadline-derived --max-time before the caller's; argv:\n%s", s)
	}
	if !strings.Contains(s[last:], "--max-time\n5\n") {
		t.Errorf("the caller's --max-time 5 is not last; argv:\n%s", s)
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}
