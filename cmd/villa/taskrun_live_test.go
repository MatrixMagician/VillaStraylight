package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
