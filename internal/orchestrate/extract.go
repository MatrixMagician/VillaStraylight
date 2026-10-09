package orchestrate

// extract.go holds the villa-extract managed-service constants and view builder
// (ADR-0033). The extractor is the memory stack's document extractor: an Apache
// Tika server Open WebUI hands each uploaded file to for its text, OCR included.
// Like villa-embed it has no GPU device passthrough and publishes no host port,
// and like qdrantImage its image is a fixed OSS managed-service literal, a
// different category from the GPU-backend tokens TestSeamGrepGate guards, so the
// isSeam allowlist names this file. It mounts nothing: Tika reads the request
// body and writes no state.

// extractImage is the digest-pinned Apache Tika 3.3.1 server image, the `-full`
// variant that carries tesseract for OCR. It runs as uid 35002, listens on 9998,
// and sets no heap bound of its own, which is why the unit renders one. A tag can
// be pushed again; the digest cannot (reproducibility).
const extractImage = "docker.io/apache/tika:3.3.1.0-full@sha256:d8e6ed96260ad89307a93195a1b856102987a818ac648502f8efbaf313d32470"

// ExtractImage returns the digest-pinned extractor image so callers (the pins
// table) never re-type the literal.
func ExtractImage() string { return extractImage }

const extractContainerUnitName = "villa-extract.container"

// ExtractContainerUnitName returns the extractor's .container unit filename, so
// the install flow can assert the unit is in the written plan before starting it.
func ExtractContainerUnitName() string { return extractContainerUnitName }

// extractView is the data extract.container.tmpl renders.
type extractView struct {
	ContainerName string
	Image         string
	Network       string
}

// buildExtractView assembles the villa-extract container view from the resolved
// pin and the config-resolved container-DNS name.
func buildExtractView(image, extractAddr string) extractView {
	return extractView{
		ContainerName: extractAddr,
		Image:         image,
		Network:       closedNetworkAttach,
	}
}
