package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/crushapi"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// TestLiveTaskRunDepsIsFullyWired asserts every seam of taskrun.Deps is bound
// by liveTaskRunDeps: a nil func here is a runner that panics on its first
// task, off-hardware and on. It inspects the struct only; nothing is invoked.
func TestLiveTaskRunDepsIsFullyWired(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	d := liveTaskRunDeps(t.Context(), "http://127.0.0.1:8080")
	v := reflect.ValueOf(d)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Func, reflect.Interface, reflect.Pointer:
			if f.IsNil() {
				t.Errorf("Deps.%s is nil", v.Type().Field(i).Name)
			}
		}
	}
}

// TestLiveRenderSandboxArgsResolvesTheImageThroughThePinSeam asserts the
// render carries the pin-resolved image (the vetted one on a host with no pin
// state), the task's container name, and the villa-owned Crush path, so no
// image literal ever lives in this tier.
func TestLiveRenderSandboxArgsResolvesTheImageThroughThePinSeam(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	args, err := liveRenderSandboxArgs(config.VillaConfig{}, "/home/dev/reports", "20260910-120115-4f2a")
	if err != nil {
		t.Fatalf("liveRenderSandboxArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, orchestrate.SandboxImage()) {
		t.Errorf("args lack the vetted sandbox image: %s", joined)
	}
	if containerNameFromArgs(args) != orchestrate.SandboxContainerName("20260910-120115-4f2a") {
		t.Errorf("container name = %q", containerNameFromArgs(args))
	}
	if !strings.Contains(joined, agentBinPath()+":/usr/local/bin/crush") {
		t.Errorf("args lack the villa-owned crush path: %s", joined)
	}
}

// TestLiveRenderSandboxArgsRefusesASensitiveWorkspace asserts the launch path
// re-runs workspace.CheckGrant against an already-granted workspace, so a
// grant made before that check existed is still refused when a task actually
// launches, not just at `workspace add` time (GHSA-3q4q-7cmw-m22m).
func TestLiveRenderSandboxArgsRefusesASensitiveWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if _, err := liveRenderSandboxArgs(config.VillaConfig{}, ssh, "20260910-120115-4f2a"); err == nil {
		t.Error("liveRenderSandboxArgs(~/.ssh) = nil error, want a refusal")
	}
}

// TestReadBridgeEventsDecodesLinesAndSkipsNoise asserts the stdout reader
// yields every event line in order, ignores non-event lines, and closes at EOF.
func TestReadBridgeEventsDecodesLinesAndSkipsNoise(t *testing.T) {
	in := strings.NewReader(`{"event":{"kind":"bridge_ready","time":"2026-09-10T12:00:00Z","version":"v0.76.0"}}
not json at all
{"command":{"kind":"cancel"}}
{"event":{"kind":"files_read","time":"2026-09-10T12:00:01Z","files":["/workspace/a.csv"]}}
`)
	var got []crushapi.Event
	for ev := range readBridgeEvents(in) {
		got = append(got, ev)
	}
	if len(got) != 2 || got[0].Kind != crushapi.KindBridgeReady || got[1].Kind != crushapi.KindFilesRead || got[1].Files[0] != "/workspace/a.csv" {
		t.Errorf("events = %+v", got)
	}
}

// TestLiveWorkspaceReadRefusesAnEscape asserts a bridge-reported path cannot
// read outside the grant.
func TestLiveWorkspaceReadRefusesAnEscape(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "in.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := liveWorkspaceRead(ws, "in.txt"); err != nil || string(data) != "ok" {
		t.Errorf("in-grant read = %q, %v", data, err)
	}
	if _, err := liveWorkspaceRead(ws, "../escape"); err == nil {
		t.Error("a path outside the grant was read")
	}
}

// TestLiveWorkspaceReadRefusesASymlinkLeaf asserts a bridge-reported name
// that is a symlink to a file outside the workspace is refused rather than
// followed: the in-VM agent controls the name, and this read runs on the
// HOST (GHSA-478j-frrx-f99c).
func TestLiveWorkspaceReadRefusesASymlinkLeaf(t *testing.T) {
	ws := t.TempDir()
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "id_ed25519")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "notes.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	if _, err := liveWorkspaceRead(ws, "notes.md"); err == nil {
		t.Error("liveWorkspaceRead followed a symlink leaf")
	}
}

// TestLiveWorkspaceReadRefusesAFIFO asserts a FIFO planted in the workspace
// is refused before it is ever opened: opening one for read blocks forever
// with no writer, which would wedge the long-lived dashboard service
// (GHSA-478j-frrx-f99c).
func TestLiveWorkspaceReadRefusesAFIFO(t *testing.T) {
	ws := t.TempDir()
	fifo := filepath.Join(ws, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := liveWorkspaceRead(ws, "pipe"); err == nil {
		t.Error("liveWorkspaceRead opened a FIFO leaf")
	}
}

// TestLiveWorkspaceReadCapsAnOversizeFile asserts a file larger than
// maxGroundingReadBytes is refused rather than fully buffered: this read
// runs on the HOST against guest-controlled content (GHSA-478j-frrx-f99c).
func TestLiveWorkspaceReadCapsAnOversizeFile(t *testing.T) {
	ws := t.TempDir()
	big := filepath.Join(ws, "big.txt")
	if err := os.WriteFile(big, make([]byte, maxGroundingReadBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := liveWorkspaceRead(ws, "big.txt"); err == nil {
		t.Error("liveWorkspaceRead read past its size cap")
	}

	small := filepath.Join(ws, "small.txt")
	if err := os.WriteFile(small, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := liveWorkspaceRead(ws, "small.txt"); err != nil || string(data) != "ok" {
		t.Errorf("in-cap read = %q, %v", data, err)
	}
}
