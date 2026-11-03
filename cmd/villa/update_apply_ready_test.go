package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

// readyFake is the host the update readiness wait polls: a scripted /health status
// per in-network service and a scripted Open WebUI health, each counted.
type readyFake struct {
	mu     sync.Mutex
	script map[string][]string // host -> status codes, the last one repeats
	probes map[string]int
	owui   []error // Open WebUI Health results, the last one repeats
	owuiN  int
}

func (f *readyFake) exec(_ context.Context, _ string, args ...string) ([]byte, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	url := args[len(args)-1]
	for host, codes := range f.script {
		if !strings.Contains(url, "//"+host+":") || !strings.HasSuffix(url, "/health") {
			continue
		}
		n := f.probes[host]
		f.probes[host]++
		return []byte(codes[min(n, len(codes)-1)]), 0, nil
	}
	return nil, -1, errors.New("unexpected probe " + url)
}

func (f *readyFake) deps(timeout time.Duration) updateReadyDeps {
	return updateReadyDeps{
		exec:  f.exec,
		image: "helper",
		owuiHealth: func(context.Context) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			n := f.owuiN
			f.owuiN++
			return f.owui[min(n, len(f.owui)-1)]
		},
		timeout:  timeout,
		interval: time.Millisecond,
	}
}

func newReadyFake(script map[string][]string, owui ...error) *readyFake {
	if len(owui) == 0 {
		owui = []error{nil}
	}
	return &readyFake{script: script, probes: map[string]int{}, owui: owui}
}

// innerProof counts how often the wrapped proof ran.
type innerProof struct{ calls int }

func (p *innerProof) run(context.Context, subsystem.Kind) updateflow.Proof {
	p.calls++
	return updateflow.Proof{Status: updateflow.ProofPass, Detail: "inner"}
}

// TestTheMemoryUpdateProofWaitsForALoadingEmbedder (#359): inference's apply restarts
// villa-embed, and memory's ProveCurrent runs next. llama-server answers 503 while
// its model loads, so the proof must wait for 200 rather than drive the RAG smoke
// into a loading embedder and refuse a healthy stack.
func TestTheMemoryUpdateProofWaitsForALoadingEmbedder(t *testing.T) {
	f := newReadyFake(map[string][]string{"villa-embed": {"503", "503", "200"}})
	inner := &innerProof{}

	got := awaitReady(f.deps(time.Second), memoryHost, inner.run)(t.Context(), subsystem.Memory)

	if got.Status != updateflow.ProofPass || inner.calls != 1 {
		t.Fatalf("proof = %+v after %d inner runs, want one pass", got, inner.calls)
	}
	if n := f.probes["villa-embed"]; n != 3 {
		t.Errorf("probed the embedder %d times, want 3 (503, 503, 200)", n)
	}
}

// TestTheMemoryUpdateProofRejectsAnEmbedderThatNeverLoads: a wait that expires is a
// proof that could not be conducted, so a Reject naming the unit, and the RAG smoke
// is never driven into it.
func TestTheMemoryUpdateProofRejectsAnEmbedderThatNeverLoads(t *testing.T) {
	f := newReadyFake(map[string][]string{"villa-embed": {"503"}})
	inner := &innerProof{}

	got := awaitReady(f.deps(30*time.Millisecond), memoryHost, inner.run)(t.Context(), subsystem.Memory)

	if got.Status != updateflow.ProofReject {
		t.Fatalf("status = %s, want reject", got.Status)
	}
	for _, want := range []string{"villa-embed.service", "not ready after"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail %q does not contain %q", got.Detail, want)
		}
	}
	if inner.calls != 0 {
		t.Errorf("the proof ran %d times against a service that never loaded", inner.calls)
	}
}

// TestTheMemoryUpdateProofWaitsForTheRerankerOnlyWhenItIsOn: the reranker is a
// memory service only with its gate on (ADR-0028); the wait never names a unit the
// host does not render.
func TestTheMemoryUpdateProofWaitsForTheRerankerOnlyWhenItIsOn(t *testing.T) {
	for _, on := range []bool{false, true} {
		cfg := memoryHost
		cfg.Reranker = on
		f := newReadyFake(map[string][]string{
			"villa-embed":  {"200"},
			"villa-rerank": {"503", "200"},
		})
		inner := &innerProof{}

		got := awaitReady(f.deps(time.Second), cfg, inner.run)(t.Context(), subsystem.Memory)

		want := 0
		if on {
			want = 2
		}
		if n := f.probes["villa-rerank"]; n != want {
			t.Errorf("reranker gate %v: probed %d times, want %d", on, n, want)
		}
		if !got.Pass() {
			t.Errorf("reranker gate %v: proof = %+v, want pass", on, got)
		}
	}
}

// TestTheChatUpdateProofWaitsForALoadingOpenWebUI (#359): the inference update's
// apply restarts Open WebUI, and the chat proof's health call is a single shot.
func TestTheChatUpdateProofWaitsForALoadingOpenWebUI(t *testing.T) {
	boom := errors.New("connection refused")
	f := newReadyFake(nil, boom, boom, nil)
	inner := &innerProof{}

	got := awaitReady(f.deps(time.Second), config.VillaConfig{}, inner.run)(t.Context(), subsystem.Chat)

	if got.Status != updateflow.ProofPass || inner.calls != 1 {
		t.Fatalf("proof = %+v after %d inner runs, want one pass", got, inner.calls)
	}
	if f.owuiN != 3 {
		t.Errorf("Open WebUI health asked %d times, want 3", f.owuiN)
	}
}

// TestTheChatUpdateProofRejectsAnOpenWebUIThatNeverAnswers names the unit.
func TestTheChatUpdateProofRejectsAnOpenWebUIThatNeverAnswers(t *testing.T) {
	f := newReadyFake(nil, errors.New("connection refused"))
	inner := &innerProof{}

	got := awaitReady(f.deps(30*time.Millisecond), config.VillaConfig{}, inner.run)(t.Context(), subsystem.Chat)

	if got.Status != updateflow.ProofReject || !strings.Contains(got.Detail, "villa-openwebui.service") ||
		!strings.Contains(got.Detail, "not ready after") || inner.calls != 0 {
		t.Errorf("proof = %+v after %d inner runs, want a reject naming villa-openwebui.service", got, inner.calls)
	}
}

// TestOtherSubsystemProofsAreNotGated: the wait is for the two proofs that drive a
// service an earlier subsystem's apply restarts; the rest keep their own handling.
func TestOtherSubsystemProofsAreNotGated(t *testing.T) {
	for _, k := range []subsystem.Kind{subsystem.Inference, subsystem.WebSearch, subsystem.Agent, subsystem.Voice, subsystem.Image} {
		f := newReadyFake(nil, errors.New("down"))
		inner := &innerProof{}
		got := awaitReady(f.deps(30*time.Millisecond), memoryHost, inner.run)(t.Context(), k)
		if !got.Pass() || inner.calls != 1 || f.owuiN != 0 {
			t.Errorf("%s: proof %+v, inner %d, owui probes %d, want the proof alone", k, got, inner.calls, f.owuiN)
		}
	}
}

// TestEveryUpdateProofWaitsForReadiness: ProveCurrent, ProveNew and ProveRestored
// all go through the one wait. The first is the refusal the issue names; the other
// two drive the same freshly restarted services.
func TestEveryUpdateProofWaitsForReadiness(t *testing.T) {
	inner := &innerProof{}
	oldReady, oldProof := liveUpdateReady, liveProofFuncs[subsystem.Memory]
	t.Cleanup(func() { liveUpdateReady, liveProofFuncs[subsystem.Memory] = oldReady, oldProof })
	liveProofFuncs[subsystem.Memory] = func(ctx context.Context) updateflow.Proof {
		return inner.run(ctx, subsystem.Memory)
	}

	for _, name := range []string{"ProveCurrent", "ProveNew", "ProveRestored"} {
		f := newReadyFake(map[string][]string{"villa-embed": {"503", "200"}})
		liveUpdateReady = func(config.VillaConfig) updateReadyDeps { return f.deps(time.Second) }
		d := updateFlowDeps(orchestrate.Systemd{}, liveStackDeps(), memoryHost, nil)
		prove := map[string]func(context.Context, subsystem.Kind) updateflow.Proof{
			"ProveCurrent": d.ProveCurrent, "ProveNew": d.ProveNew, "ProveRestored": d.ProveRestored,
		}[name]
		before := inner.calls
		if got := prove(t.Context(), subsystem.Memory); !got.Pass() || inner.calls != before+1 || f.probes["villa-embed"] != 2 {
			t.Errorf("%s: proof %+v, embedder probed %d times, want 2", name, got, f.probes["villa-embed"])
		}
	}
}
