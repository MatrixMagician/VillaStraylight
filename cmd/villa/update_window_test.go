package main

import (
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
