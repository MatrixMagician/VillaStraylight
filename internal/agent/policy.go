package agent

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// crushPolicyBytes is the COMPILED-IN Crush pin policy (AGENT-01). Because
// it is embedded at build time it is NOT an external/runtime input — a malformed
// policy is a build-time programming error caught by loadCrushPolicy's panic,
// never a runtime parse of attacker-controlled data. It carries the
// pinned version, the per-platform release asset table (name + tarball SHA-256 +
// size + the extracted-binary SHA-256), and the release URL template. It clones
// the internal/preflight/floors.go `go:embed rocm-policy.json` shape verbatim.
//
//go:embed crush-policy.json
var crushPolicyBytes []byte

// CrushPolicy is the decoded shape of crush-policy.json: a pinned version, a
// per-platform asset table (keyed "linux/amd64"; the structure allows a future
// "darwin/arm64"), and the release URL template (with a "{asset}" placeholder).
// Mirrors the rocm-policy.json flat-JSON minimalism.
type CrushPolicy struct {
	// Version is the pinned Crush release (e.g. "v0.76.0"). Autoupdate is forced
	// off by construction — there is no upgrade verb this phase.
	Version string `json:"version"`
	// Assets is the per-platform release artifact table, keyed by "<os>/<arch>"
	// (e.g. "linux/amd64" for the Fedora/Strix Halo target).
	Assets map[string]CrushAsset `json:"assets"`
	// URLTmpl is the release URL template; the live install seam (Plan 02)
	// substitutes "{asset}" with the resolved asset Name.
	URLTmpl string `json:"urlTemplate"`
}

// CrushAsset is one platform's pinned release artifact. SHA256 is the TARBALL
// checksum (from charmbracelet/crush's published checksums.txt) used by the
// install-verify gate; BinarySHA256 is the EXTRACTED-binary checksum used
// by drift detection. These are DIFFERENT values (Pitfall 6): the
// tarball hash is over the .tar.gz, the binary hash is over the extracted `crush`
// binary inside it.
type CrushAsset struct {
	// Name is the release artifact filename (e.g. "crush_0.76.0_Linux_x86_64.tar.gz").
	Name string `json:"name"`
	// SHA256 is the TARBALL checksum (install-verify gate).
	SHA256 string `json:"sha256"`
	// Size is the tarball size in bytes (size-then-checksum verify).
	Size uint64 `json:"size"`
	// BinarySHA256 is the EXTRACTED-binary checksum (drift detection, /
	// Pitfall 6). It starts as the UNPINNED sentinel below; Plan 03 (26-03)
	// replaces it on-hardware with the real binary SHA-256 (extract the verified
	// tarball once → sha256sum crush). NOTE: JSON cannot carry comments, so the
	// sentinel contract is documented here, on the struct, not in the JSON file.
	// While it equals the sentinel, drift degrades to a typed-Unknown WARN rather
	// than a false confident FAIL (see drift.go / unpinnedBinarySentinel).
	BinarySHA256 string `json:"binarySha256"`
}

// unpinnedBinarySentinel is the placeholder BinarySHA256 value shipped until the
// Plan-03 on-hardware pin records the real extracted-binary SHA-256 (Pitfall 6 /
// Open-Q2). drift.go treats this value (and an empty string) as "binary hash not
// yet pinned" → BinaryDriftUnknown (typed-Unknown WARN), never a false drift on a
// freshly, correctly installed binary.
const unpinnedBinarySentinel = "UNPINNED-binary-sha256-set-by-26-03-on-hardware"

// loadCrushPolicy decodes the embedded crush-policy.json. It PANICS on a
// malformed embed: that is a build-time programming error (the bytes are compiled
// in, never runtime input), so failing loud at startup is correct and
// there is no attacker-controlled path to this panic. Clones loadROCmPolicy.
func loadCrushPolicy() CrushPolicy {
	var p CrushPolicy
	if err := json.Unmarshal(crushPolicyBytes, &p); err != nil {
		panic(fmt.Sprintf("agent: malformed embedded crush-policy.json: %v", err))
	}
	return p
}

// LoadCrushPolicy is the EXPORTED accessor for the embedded crush-policy.json (the
// pinned version + per-platform asset table + release URL template). Phase 27's install
// addon (cmd/villa/install_agent.go) resolves the pinned linux/amd64 asset + URL through
// it to compose agent.Install (checksum-before-extract) without re-implementing the
// verified install — keeping asset/URL resolution testable in cmd/villa. It delegates to
// the unexported loadCrushPolicy, so the panic-on-malformed build-time discipline
// is shared by both the package-internal and the exported entry points.
func LoadCrushPolicy() CrushPolicy {
	return loadCrushPolicy()
}

// VerifyTarball is the PURE half of the install gate: it asserts
// the downloaded tarball bytes match the pinned asset by SIZE then SHA-256
// (hex-encoded, case-insensitive), refusing-with-remediation on any mismatch
// never a silent pass, never a fallback. The live stream-and-place wiring (Plan
// 02) calls this BEFORE extraction so an unverified/mismatched Crush binary is
// never placed on disk. It clones the internal/download verify discipline
// (size-then-EqualFold-checksum, fail-closed) without taking a host dependency.
func VerifyTarball(asset CrushAsset, got []byte) error {
	if uint64(len(got)) != asset.Size {
		return fmt.Errorf(
			"agent: refusing to install Crush — tarball size mismatch: got %d bytes, want %d; re-download the pinned %s and retry",
			len(got), asset.Size, asset.Name,
		)
	}
	sum := sha256.Sum256(got)
	gotSum := hex.EncodeToString(sum[:])
	if !strings.EqualFold(gotSum, asset.SHA256) {
		return fmt.Errorf(
			"agent: refusing to install an unverified Crush binary — tarball checksum mismatch: got %s, want %s; the downloaded %s does not match the pinned policy",
			gotSum, asset.SHA256, asset.Name,
		)
	}
	return nil
}
