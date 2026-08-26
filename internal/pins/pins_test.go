// Package pins tests guard the four jobs the compiled-in table does — schema,
// allowlist, fallback, serial floor — and the one discipline that makes it
// possible: accessors, never literals.
//
// The table cannot fail at runtime, so there are no error paths to exercise. What
// there is instead is a set of agreement checks: every accessor resolves, every
// declared registry matches the pin it actually resolves to, and every component
// villa pins in the tree is present here. Each of those is a way the table could
// silently become a lie about the stack.
package pins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// TestEveryAccessorResolves is the fallback job. When no manifest is available the
// table IS the pin set, so an entry whose accessor returns an empty pin would leave
// a component with nothing to run and no error to say so — the table cannot fail,
// which means a hole in it is silent by construction.
func TestEveryAccessorResolves(t *testing.T) {
	for _, e := range Table() {
		t.Run(string(e.Component), func(t *testing.T) {
			if e.Vetted == nil {
				t.Fatal("entry has no vetted-pin accessor")
			}
			pin := e.Vetted()
			if strings.TrimSpace(pin.Ref) == "" {
				t.Error("vetted pin is empty; this component would have nothing to run and nothing to report")
			}
			if e.Shape == ChecksummedAsset && strings.TrimSpace(pin.Checksum) == "" {
				t.Error("a checksummed asset with no checksum cannot be verified before it is placed on disk")
			}
			if e.Shape != ChecksummedAsset && pin.Checksum != "" {
				t.Error("a digest-pinned image carries a checksum; the digest IS the checksum, and two copies of one fact can disagree")
			}
		})
	}
}

// TestRegistryAgreesWithTheResolvedPin is the allowlist job, and the check that
// keeps it meaningful. The allowlist bounds what villa may pull from, so a declared
// registry that does not match the host in the pin it resolves to would let a
// manifest be refused for naming the host villa is actually already using — or,
// worse, accepted for naming one it is not.
func TestRegistryAgreesWithTheResolvedPin(t *testing.T) {
	for _, e := range Table() {
		t.Run(string(e.Component), func(t *testing.T) {
			if e.Registry == "" {
				t.Fatal("entry declares no registry host; the allowlist would have nothing to check against")
			}
			if e.Shape == ChecksummedAsset {
				// A checksummed asset's pin is a version, not a reference carrying a
				// host, so the host is asserted against the URL template the download
				// actually uses instead.
				if got := crushRegistry(); got != e.Registry {
					t.Errorf("registry = %q but the release URL template points at %q", e.Registry, got)
				}
				return
			}
			ref := e.Vetted().Ref
			host, _, _ := strings.Cut(ref, "/")
			if host != e.Registry {
				t.Errorf("registry = %q but the vetted pin resolves to host %q — the allowlist disagrees with reality", e.Registry, host)
			}
		})
	}
}

// TestShapesAreDeclaredNotDerived pins the shape vocabulary. Shape is part of the
// `villa update --check --json` contract, so a consumer reads it to decide how to
// describe a change; an unrecognised value would reach that consumer as data.
func TestShapesAreDeclaredNotDerived(t *testing.T) {
	valid := map[Shape]bool{RollingDigest: true, VersionTag: true, ChecksummedAsset: true}
	for _, e := range Table() {
		if !valid[e.Shape] {
			t.Errorf("%s declares shape %q, which is not one of the three the --json contract names", e.Component, e.Shape)
		}
		// A version tag names a version; a rolling tag by definition does not. This
		// is the distinction --check must not flatten, so the data has to carry it.
		switch e.Shape {
		case VersionTag, ChecksummedAsset:
			if e.Version == "" {
				t.Errorf("%s is a %s but names no version; --check could not tell a rebuild from a bump", e.Component, e.Shape)
			}
		case RollingDigest:
			if e.Version != "" {
				t.Errorf("%s is a rolling digest yet names version %q; a rolling tag has no version to name", e.Component, e.Version)
			}
		}
	}
}

// TestComponentIDsAreUniqueAndSubsystemsAreNamed guards the store's key space. A
// ComponentID is written into pinstate and into signed manifests, so a duplicate
// would make one component's effective pin overwrite another's.
func TestComponentIDsAreUniqueAndSubsystemsAreNamed(t *testing.T) {
	seen := map[ComponentID]bool{}
	for _, e := range Table() {
		if seen[e.Component] {
			t.Errorf("component id %q appears twice; one entry's effective pin would overwrite the other's", e.Component)
		}
		seen[e.Component] = true
		if e.Subsystem.String() == "unknown" {
			t.Errorf("%s belongs to no named subsystem, so no proof gates an update to it", e.Component)
		}
	}
}

// TestFloorsTravelWithTheROCmPinsOnly is the §4.4 decision as a test. Floors are a
// claim about what was tested on hardware, and only the ROCm images make one — the
// Vulkan fallback is the fallback precisely because it demands less. An entry
// growing floors it does not have would make `update` gate on thresholds nobody
// measured.
func TestFloorsTravelWithTheROCmPinsOnly(t *testing.T) {
	want := map[ComponentID]bool{
		BackendROCm724:     true,
		BackendROCm644:     true,
		BackendROCm644WMMA: true,
	}
	for _, e := range Table() {
		if got := e.HasFloors(); got != want[e.Component] {
			t.Errorf("%s HasFloors = %v, want %v", e.Component, got, want[e.Component])
		}
		if e.HasFloors() && e.Floors().Kernel == "" {
			t.Errorf("%s carries floors with no kernel floor; the preflight gate would have nothing to compare", e.Component)
		}
	}
}

// TestLookupIsTheAllowlist: the bool Lookup returns is what refuses a manifest
// naming a component villa does not ship. A manifest that could introduce
// components would be a remote-code-execution channel with extra steps, so this is
// asserted directly rather than left implied by the map.
func TestLookupIsTheAllowlist(t *testing.T) {
	if _, ok := Lookup(Qdrant); !ok {
		t.Error("Lookup refused a component the table names")
	}
	if _, ok := Lookup(ComponentID("some-component-villa-never-shipped")); ok {
		t.Error("Lookup accepted a component the table does not name; a manifest could introduce an executable")
	}
	if RegistryAllowed("registry.example.invalid") {
		t.Error("RegistryAllowed accepted a host the table never pulls from; a stolen key could redirect a pull")
	}
	for _, host := range []string{registryDockerIO, registryGHCR, registryGCR, registryGitHub} {
		if !RegistryAllowed(host) {
			t.Errorf("RegistryAllowed rejected %q, a host the table already pulls from", host)
		}
	}
}

// TestForGroupsByProofUnit: the update flow walks subsystems, because the proof
// unit is the verify verb's scope. Memory must yield BOTH Qdrant and the embedder —
// splitting them would produce a pairing with no proof and no meaning.
func TestForGroupsByProofUnit(t *testing.T) {
	cases := map[subsystem.Kind][]ComponentID{
		subsystem.Inference: {BackendROCm724, BackendROCm644, BackendROCm644WMMA, BackendVulkan},
		subsystem.Chat:      {OpenWebUI},
		subsystem.Memory:    {Qdrant, Embedder},
		subsystem.WebSearch: {SearXNG, Websafe},
		subsystem.Agent:     {Crush},
	}
	for k, want := range cases {
		got := For(k)
		if len(got) != len(want) {
			t.Errorf("%v has %d components, want %d", k, len(got), len(want))
			continue
		}
		for i, id := range want {
			if got[i].Component != id {
				t.Errorf("%v component %d = %q, want %q", k, i, got[i].Component, id)
			}
		}
	}
	if len(For(subsystem.CodingMode)) != 0 {
		t.Error("coding mode has pinned components; it is a configuration of the stack, not a component of it")
	}
}

// TestTableIsNotMutableByCallers: Table returns a fresh slice each call, so a
// caller appending to what it got cannot rewrite villa's statement about what it
// vetted. A package-level var would make the allowlist writable from anywhere.
func TestTableIsNotMutableByCallers(t *testing.T) {
	first := Table()
	n := len(first)
	//nolint:staticcheck // the append is the point: it must not be observable.
	_ = append(first, Entry{Component: "injected"})
	if len(Table()) != n {
		t.Error("appending to a returned table changed the table; the allowlist is writable from any caller")
	}
}

// TestEveryDigestPinnedImageInTheTreeIsInTheTable is the completeness check, and
// the only one that can catch a component being ADDED to the stack without being
// added here. A component villa runs but never pinned in this table is one `update`
// can never see, and its absence is silent: nothing else in the tree knows the
// table is supposed to be exhaustive.
//
// It works by scanning the seam files for `@sha256:` digest literals and asserting
// each one is reachable from some entry's accessor. Scanning beats a hand-written
// list, because a list needs the same maintenance the table does and would go stale
// in exactly the commit that matters.
func TestEveryDigestPinnedImageInTheTreeIsInTheTable(t *testing.T) {
	// The seam files allowed to hold an image literal, per TestSeamGrepGate.
	seamFiles := []string{
		filepath.Join("..", "inference", "backend_vulkan.go"),
		filepath.Join("..", "inference", "backend_rocm.go"),
		filepath.Join("..", "orchestrate", "memory.go"),
		filepath.Join("..", "orchestrate", "openwebui.go"),
		filepath.Join("..", "orchestrate", "searxng.go"),
		filepath.Join("..", "orchestrate", "websafe.go"),
	}

	inTable := map[string]bool{}
	for _, e := range Table() {
		inTable[e.Vetted().Ref] = true
	}

	for _, file := range seamFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, ref := range digestRefs(string(data)) {
			if !inTable[ref] {
				t.Errorf("%s pins an image the table does not name:\n  %s\n"+
					"A component villa runs but never pinned here is one `villa update` can never see.",
					filepath.ToSlash(file), ref)
			}
		}
	}
}

// digestRefs extracts every `<host>/<path>@sha256:<hex>` reference appearing as a
// Go string literal in src. It reads the literals out of the seam rather than
// re-typing any, so this test file trips no grep gate of its own.
func digestRefs(src string) []string {
	var refs []string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		rest := line
		for {
			open := strings.Index(rest, `"`)
			if open < 0 {
				break
			}
			rest = rest[open+1:]
			end := strings.Index(rest, `"`)
			if end < 0 {
				break
			}
			lit := rest[:end]
			rest = rest[end+1:]
			if strings.Contains(lit, "@sha256:") && strings.Contains(lit, "/") {
				refs = append(refs, lit)
			}
		}
	}
	return refs
}

// TestSerialFloorIsCompiledIn: the serial floor's home is the one place that cannot
// be absent. If it lived only in the state store, deleting that store would reset
// the anti-downgrade protection to zero and re-open the replay attack.
func TestSerialFloorIsCompiledIn(t *testing.T) {
	if Serial() == 0 {
		t.Error("the compiled-in serial is zero, which means 'no floor'; a deleted state store would then accept any manifest")
	}
	if Serial() != VettedSerial {
		t.Errorf("Serial() = %d but VettedSerial = %d; the accessor and the constant must be one value", Serial(), VettedSerial)
	}
}
