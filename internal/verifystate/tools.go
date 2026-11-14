package verifystate

// tools.go is the second document of this package: the last `villa verify tools` verdict and
// when it ran (ADR-0042). It has the shape and the discipline of verify-search-state.json
// (verdict and timestamp only, fail-closed Load) and its own file, so the two proofs cannot
// overwrite or be read as each other. `tool-server-proof` in doctor reads it.

import "github.com/MatrixMagician/VillaStraylight/internal/jsonstore"

// toolsSchemaVersion is this document's own self-version. Bump only on an incompatible
// verify-tools-state.json change.
const toolsSchemaVersion = 1

var toolsStore = jsonstore.New[State, *State]("verifystate tools", "verify-tools-state.json", toolsSchemaVersion)

// ToolsSchemaVersion exposes the tools document's own schema version.
func ToolsSchemaVersion() int { return toolsSchemaVersion }

// SaveTools stamps the schema version and writes the tools document via the seam.
func SaveTools(d Deps, s State) error { return toolsStore.Save(d, s) }

// LoadTools reads the tools document, failing CLOSED to an empty State on an absent, corrupt
// or version-mismatched file: no recorded result, never a fabricated PASS.
func LoadTools(d Deps) (State, error) { return toolsStore.Load(d) }

// ToolsPath resolves verify-tools-state.json under the villa data root.
func ToolsPath() string { return toolsStore.Path() }

// ToolsWriteFileAtomic is the live WriteAll seam for the tools document: a traversal-guarded
// temp+rename write at 0600 under a 0700 directory.
func ToolsWriteFileAtomic(path string, data []byte) error {
	return toolsStore.WriteFileAtomic(path, data)
}
