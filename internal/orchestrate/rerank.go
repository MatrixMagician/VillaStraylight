package orchestrate

// rerank.go holds the villa-rerank managed-service constants and view builder
// (ADR-0028). The reranker is the memory stack's second llama-server: it runs the
// embedder's pinned image (one image, two roles, so it resolves its image under
// ComponentEmbedder and moves with that pin) and serves /v1/rerank for Open
// WebUI's hybrid search. Like villa-embed it has no GPU device passthrough and
// publishes no host port.

import (
	"strconv"
	"strings"
)

// rerankGGUFFilename is the ONE filename shared by the served `-m` path and the
// install pre-stage shard, bound at both ends through RerankGGUFFilename().
const rerankGGUFFilename = "bge-reranker-v2-m3-Q8_0.gguf"

// RerankGGUFFilename returns the single-source reranker GGUF filename.
func RerankGGUFFilename() string { return rerankGGUFFilename }

// RerankModelName is the model id Open WebUI sends with each rerank request. The
// server ignores it (it serves one model), so it names the weights for the log.
const RerankModelName = "bge-reranker-v2-m3"

const (
	rerankContainerUnitName = "villa-rerank.container"
	// rerankContextLen matches the embedder's context: bge-reranker-v2-m3 takes
	// 8192 positions and the unit serves one pair at a time.
	rerankContextLen = 8192
	// rerankBatch is the physical batch, and so the longest query-plus-chunk pair
	// the unit scores: a rank-pooled model needs the whole pair in one batch, and
	// a longer pair is refused, which Open WebUI turns into an empty retrieval.
	// The embedder bounds every chunk to its own 512-token batch, so 1024 covers
	// every chunk it admits with a query of up to 512 tokens.
	rerankBatch = 1024
)

// RerankContainerUnitName returns the reranker's .container unit filename, so the
// install flow can assert the unit is in the written plan before starting it.
func RerankContainerUnitName() string { return rerankContainerUnitName }

// rerankView is the data rerank.container.tmpl renders: the embedder's shape with
// the rerank Exec.
type rerankView struct {
	ContainerName string
	Image         string
	Network       string
	Volume        string
	Exec          string
}

// buildRerankView assembles the villa-rerank container view from the resolved
// pin and the config-resolved container-DNS name and port.
func buildRerankView(image, ggufFilename, rerankAddr string, rerankPort int) rerankView {
	return rerankView{
		ContainerName: rerankAddr,
		Image:         image,
		Network:       closedNetworkAttach,
		Volume:        embedModelMount,
		Exec:          buildRerankExec(ggufFilename, rerankPort),
	}
}

// buildRerankExec assembles the rerank llama-server Exec from fixed tokens.
// `--rerank` turns on the /v1/rerank route and rank pooling; `-b`/`-ub` set the
// pair bound described at rerankBatch.
func buildRerankExec(ggufFilename string, rerankPort int) string {
	tokens := []string{
		"llama-server",
		"-m", "/models/" + ggufFilename,
		"--rerank",
		"-c", strconv.Itoa(rerankContextLen),
		"-b", strconv.Itoa(rerankBatch),
		"-ub", strconv.Itoa(rerankBatch),
		"--host", "0.0.0.0",
		"--port", strconv.Itoa(rerankPort),
	}
	return strings.Join(tokens, " ")
}
