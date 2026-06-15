package orchestrate

import (
	"strconv"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// TestLlamaInNetworkEndpoint locks the single-source in-network villa-llama endpoint
// (WR-01). The accessor is the ONE place cmd/villa sources the in-network sanity-probe
// target, so it must (a) equal the current rendered shape (http://villa-llama:8080/v1)
// AND (b) be built in DNS/port lockstep — the host comes from the orchestrate
// containerName constant (the rendered ContainerName= DNS name) and the port from the
// inference server port, never a re-typed villa-llama:8080 literal. A future container
// rename or inference port change is caught HERE rather than by a drifted literal.
func TestLlamaInNetworkEndpoint(t *testing.T) {
	got := LlamaInNetworkEndpoint()

	// (a) Current rendered values — the in-network base URL Open WebUI already uses.
	const want = "http://villa-llama:8080/v1"
	if got != want {
		t.Errorf("LlamaInNetworkEndpoint() = %q, want %q", got, want)
	}

	// (b) Built in lockstep: prefix from the containerName constant, contains the
	// inference server port (so a rename/port-change is caught by this test).
	wantPrefix := "http://" + containerName
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("LlamaInNetworkEndpoint() = %q, want prefix %q (host must come from containerName, not a literal)", got, wantPrefix)
	}
	port := strconv.Itoa(inference.ServerPort())
	if !strings.Contains(got, ":"+port+"/") {
		t.Errorf("LlamaInNetworkEndpoint() = %q must contain the inference server port %q (DNS/port lockstep)", got, port)
	}
}
