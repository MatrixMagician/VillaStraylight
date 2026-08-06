package orchestrate

// endpoint.go is the SINGLE source of the in-network villa-llama inference endpoint — the
// URL a `--network villa` helper container uses to reach villa-llama by its container DNS
// name. It exists so cmd/villa (the `villa verify agent` egress negative-control sanity
// probe) NEVER re-types a `villa-llama:8080` host literal: the host is composed FROM
// the render.go containerName constant (the rendered ContainerName= DNS name) and the port
// FROM the inference server port (inference.ServerPort()), keeping the in-network endpoint in
// DNS/port lockstep with both the rendered unit and the inference seam (Pitfall 3 / T-4-01).
// This mirrors buildOpenWebUIView's in-network OPENAI_API_BASE_URL composition (openwebui.go)
// and the one-line exported-accessor shape of EmbedImage()/QdrantImage() (memory.go). Pure
// string composition; no host I/O.

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// LlamaInNetworkEndpoint returns the in-network villa-llama OpenAI-compatible base URL
// ("http://villa-llama:8080/v1") composed from the orchestrate containerName constant + the
// inference server port. It is the in-network analogue of the host loopback endpoint
// (inference's endpointURL): a helper container on villa.network reaches villa-llama by DNS
// name at the container-internal port, NOT 127.0.0.1 (which inside a helper container is the
// helper's OWN loopback). Sourcing both identifiers from their owning seams (containerName,
// inference.ServerPort()) means a container rename or a port change updates this endpoint
// automatically — no hand-typed host literal can drift from the rendered ContainerName=.
func LlamaInNetworkEndpoint() string {
	return fmt.Sprintf("http://%s:%d/v1", containerName, inference.ServerPort())
}
