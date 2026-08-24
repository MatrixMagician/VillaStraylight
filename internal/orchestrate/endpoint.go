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
	return inNetworkEndpoint(containerName)
}

// inNetworkEndpoint composes the OpenAI-compatible base URL for ANY llama-server on
// villa.network from its container DNS name. Resident slots share the primary's
// container-internal port (each has its own netns), so they share this composition
// too — which is what keeps every endpoint the Open WebUI env lists in port lockstep
// with the inference seam.
// ResidentInNetworkEndpoint composes the OpenAI-compatible base URL for one resident
// slot, from the same container-name and port composition the rendered unit uses. It
// exists so a caller reconciling Open WebUI's stored connection list cannot re-type a
// URL that the renderer might later change: the env line and the reconciled document
// are composed by the same two functions.
func ResidentInNetworkEndpoint(model string) (string, error) {
	name, err := ResidentUnitName(model)
	if err != nil {
		return "", err
	}
	return inNetworkEndpoint(name), nil
}

func inNetworkEndpoint(host string) string {
	return fmt.Sprintf("http://%s:%d/v1", host, inference.ServerPort())
}
