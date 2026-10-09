package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// recordingSystemd is a Systemd whose runner records every systemctl call and
// fails any call that names a unit systemd has never seen.
func recordingSystemd(unknown string) (orchestrate.Systemd, *[]string) {
	var calls []string
	sys := orchestrate.SystemdForTest(func(_ string, args ...string) (string, bool, bool) {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		return "", true, !strings.Contains(call, unknown)
	})
	return sys, &calls
}

// TestStoppedWindowFollowsTheRerankerGate: a memory-on host with `reranker`
// unset has no villa-rerank unit, and `systemctl stop` of a unit systemd has never
// seen fails. The stopped window must stop and start only the memory services the
// config renders, so such a host still updates memory; with the gate on, the
// reranker joins both halves.
func TestStoppedWindowFollowsTheRerankerGate(t *testing.T) {
	off := config.VillaConfig{MemoryEnabled: true}
	sys, calls := recordingSystemd("villa-rerank")
	if err := liveSubsystemStop(t.Context(), sys, off, subsystem.Memory); err != nil {
		t.Fatalf("stop with the reranker unset: %v", err)
	}
	if err := liveSubsystemStart(t.Context(), sys, off, subsystem.Memory); err != nil {
		t.Fatalf("start with the reranker unset: %v", err)
	}
	got := strings.Join(*calls, "\n")
	if strings.Contains(got, "villa-rerank") {
		t.Errorf("the window touched the unrendered reranker:\n%s", got)
	}
	for _, want := range []string{"stop villa-qdrant.service", "stop villa-embed.service", "start villa-qdrant.service", "start villa-embed.service"} {
		if !strings.Contains(got, want) {
			t.Errorf("the window did not run %q:\n%s", want, got)
		}
	}

	on := config.VillaConfig{MemoryEnabled: true, Reranker: true}
	sys, calls = recordingSystemd("no-such-unit")
	if err := liveSubsystemStop(t.Context(), sys, on, subsystem.Memory); err != nil {
		t.Fatalf("stop with the reranker on: %v", err)
	}
	if err := liveSubsystemStart(t.Context(), sys, on, subsystem.Memory); err != nil {
		t.Fatalf("start with the reranker on: %v", err)
	}
	got = strings.Join(*calls, "\n")
	for _, want := range []string{"stop villa-rerank.service", "start villa-rerank.service"} {
		if !strings.Contains(got, want) {
			t.Errorf("with the gate on the window did not run %q:\n%s", want, got)
		}
	}
}

// TestUpdateFollowsTheExtractorGate: the extractor is memory's second optional
// unit (ADR-0033), so every seam of a memory update reads its gate the way it
// reads the reranker's. With `extractor` unset the stopped window and the capture
// never name villa-extract, even when a stale unit file sits on disk; with the gate
// on, the extractor is stopped, started and captured. The restart half is
// TestAMemoryUpdateRestartsTheExtractorOnlyWhenItsGateIsOn.
func TestUpdateFollowsTheExtractorGate(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := filepath.Join(cfgHome, "containers", "systemd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"villa-qdrant.container", "villa-embed.container", "villa-extract.container"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("[Container]\n# "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	off := config.VillaConfig{Model: "qwen3.5-0.8b", Quant: "Q4_K_M", Ctx: 4096, InferenceSecret: "s", MemoryEnabled: true}
	sys, calls := recordingSystemd("villa-extract")
	if err := liveSubsystemStop(t.Context(), sys, off, subsystem.Memory); err != nil {
		t.Fatalf("stop with the extractor unset: %v", err)
	}
	if err := liveSubsystemStart(t.Context(), sys, off, subsystem.Memory); err != nil {
		t.Fatalf("start with the extractor unset: %v", err)
	}
	if got := strings.Join(*calls, "\n"); strings.Contains(got, "villa-extract") {
		t.Errorf("the update touched the unrendered extractor:\n%s", got)
	}
	snap, err := liveCapture(liveStackDeps(), off, subsystem.Memory)
	if err != nil {
		t.Fatalf("capture with the extractor unset: %v", err)
	}
	if got := slices.Sorted(maps.Keys(snap.Units)); strings.Join(got, ",") != "villa-embed.container,villa-qdrant.container" {
		t.Errorf("capture with the extractor unset took %v, want only qdrant and the embedder", got)
	}

	on := off
	on.Extractor = true
	sys, calls = recordingSystemd("no-such-unit")
	if err := liveSubsystemStop(t.Context(), sys, on, subsystem.Memory); err != nil {
		t.Fatalf("stop with the extractor on: %v", err)
	}
	if err := liveSubsystemStart(t.Context(), sys, on, subsystem.Memory); err != nil {
		t.Fatalf("start with the extractor on: %v", err)
	}
	got := strings.Join(*calls, "\n")
	for _, want := range []string{"stop villa-extract.service", "start villa-extract.service"} {
		if !strings.Contains(got, want) {
			t.Errorf("with the gate on the update did not run %q:\n%s", want, got)
		}
	}
	snap, err = liveCapture(liveStackDeps(), on, subsystem.Memory)
	if err != nil {
		t.Fatalf("capture with the extractor on: %v", err)
	}
	if got := string(snap.Units["villa-extract.container"]); got != "[Container]\n# villa-extract.container\n" {
		t.Errorf("capture did not take the extractor's unit verbatim, got %q", got)
	}
}
