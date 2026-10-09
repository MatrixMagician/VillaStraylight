// Package pins is the compiled-in, enumerable registry of every component villa
// pins: the inference backend images, Open WebUI, Qdrant, the embedder, the
// document extractor, SearXNG, the websafe base image, the Crush binary and the
// sandbox image.
//
// Before this package, "which components does villa pin?" had no answer in code.
// The pins were eight constants in five packages, each correct in isolation and
// none of them walkable. Nothing could ask how many there were, which subsystem a
// pin belonged to, or where its bytes came from.
//
// # The four jobs this table does at once
//
// It is the SCHEMA — what a pin consists of. It is the ALLOWLIST — a signed
// manifest may supply new values only for components named here, from registry
// hosts named here. It is the FALLBACK — when no manifest is available, or the one
// on offer is expired or refused, these are the pins villa uses. And it is the
// SERIAL FLOOR — the anti-downgrade baseline a manifest must exceed.
//
// # Accessors, not literals
//
// Every entry reaches its pin through an accessor that already exists in the
// package that owns it (inference.BackendFor(…).Image(), orchestrate.QdrantImage(),
// agent.LoadCrushPolicy()). NO image literal moves into this package, and none ever
// should: TestSeamGrepGate forbids image tokens outside the inference seam so that a
// future ROCm/Metal backend drops in without touching callers. A pins.go carrying
// literals would need a seam-gate allowlist entry, which is the signal that this
// design has gone wrong.
//
// # It cannot fail
//
// The table is compiled in, so a malformed one is a BUILD-TIME programming error,
// the same class loadCrushPolicy handles by panicking on a bad embed. There are
// deliberately no runtime error paths here: Table() cannot return an error because
// there is no state of the world in which it would have one to return. The
// runtime-fallible half of the pin story is the effective-pin store, which is a
// separate package for exactly that reason.
//
// PURE: no I/O, no os/exec, no container-image literal.
package pins

import (
	"slices"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// Shape is how a pin moves, and therefore what a moved pin MEANS. It is carried as
// data rather than derived by branching because it is part of the public `--json`
// contract of `villa update --check`: a consumer reads the shape to decide how to
// describe a change, so it must be a value the table states, not a rule a reader
// re-implements.
type Shape string

const (
	// RollingDigest is a tag that upstream rebuilds without renaming. A moved
	// digest means a new upstream build — expected, with no version to name.
	RollingDigest Shape = "rolling_digest"
	// VersionTag is a tag naming a specific upstream version. A moved digest means
	// THE SAME DECLARED VERSION WAS REBUILT, which is emphatically not a version
	// bump: the image villa validated on hardware is no longer the image that tag
	// names, while nothing about the version changed. Flattening this into "update
	// available" is the dishonesty `--check` exists to avoid.
	VersionTag Shape = "version_tag"
	// ChecksummedAsset is a release artifact fetched by URL and verified by
	// checksum rather than pulled by digest. A moved value is a genuine new release.
	ChecksummedAsset Shape = "checksummed_asset"
)

// ComponentID is the stable identity of one pinned component. It is stable in the
// strong sense: it is written into the pin state store and into signed manifests,
// so renaming one silently orphans a host's effective pin and re-points it at the
// vetted default. Add ids; do not rename them.
//
// The five render-path ids are DERIVED from orchestrate's constants rather than
// re-typed, because the render path resolves an effective pin under the same string
// this table names it by. Re-typing would let a rename land on one side only, which
// fails no build and orphans every host's recorded pin for that component.
type ComponentID string

const (
	// BackendROCm724 is the former default ROCm 7.2.4 image, selected by name.
	BackendROCm724 ComponentID = "backend-rocm-7.2.4"
	// BackendROCm100 is the ROCm 10.0 backend image, the default (ADR-0022).
	BackendROCm100 ComponentID = "backend-rocm-10.0"
	// BackendROCm644 is the TG-tuned ROCm 6.4.4 backend image.
	BackendROCm644 ComponentID = "backend-rocm-6.4.4"
	// BackendROCm644WMMA is the rocWMMA variant of the 6.4.4 backend image.
	BackendROCm644WMMA ComponentID = "backend-rocm-6.4.4-rocwmma"
	// BackendVulkan is the Vulkan RADV backend image — the fallback, and the
	// rollback landing spot.
	BackendVulkan ComponentID = "backend-vulkan-radv"
	// OpenWebUI is the chat UI image.
	OpenWebUI ComponentID = orchestrate.ComponentOpenWebUI
	// Qdrant is the vector store image.
	Qdrant ComponentID = orchestrate.ComponentQdrant
	// Embedder is the image villa-embed serves the embeddings llama-server from.
	// Its pin is byte-identical to BackendVulkan's today and is deliberately a
	// separate component: one image, two roles, and the roles may diverge.
	Embedder ComponentID = orchestrate.ComponentEmbedder
	// Extractor is the Apache Tika image villa-extract serves document
	// extraction from (ADR-0033).
	Extractor ComponentID = orchestrate.ComponentExtractor
	// SearXNG is the metasearch service image.
	SearXNG ComponentID = orchestrate.ComponentSearXNG
	// Websafe is the distroless base the web-guard loader container runs on.
	Websafe ComponentID = orchestrate.ComponentWebsafe
	// Crush is the coding-agent binary — the one pinned component that is not a
	// container image.
	Crush ComponentID = "crush"
	// SandboxImage is the office image a workspace task runs in. It is the only
	// villa-BUILT pin: its bytes come from build/sandbox/Containerfile rather than
	// from a registry, which is why its registry host is localhost.
	SandboxImage ComponentID = orchestrate.ComponentSandbox
	// Whisper is the villa-stt speech-to-text image (whisper.cpp, Vulkan build).
	Whisper ComponentID = orchestrate.ComponentWhisper
	// Kokoro is the villa-tts text-to-speech image (Kokoro-FastAPI, CPU build).
	Kokoro ComponentID = orchestrate.ComponentKokoro
	// ImageServer is the sd.cpp sd-server image villa-image runs (#312).
	ImageServer ComponentID = orchestrate.ComponentImage
)

// Pin is one component's pinned value.
//
// Ref is a digest-pinned image reference for a container component, and the release
// version for a checksummed asset. Checksum is populated only for
// ChecksummedAsset: an image is identified BY its digest, so a separate checksum
// field would be the same fact written twice, and two places to disagree.
type Pin struct {
	Ref      string
	Checksum string
}

// Floors are the host thresholds a pin demands, and they travel WITH the pin.
//
// A registry cannot report the floor a digest needs: a floor is a claim about what
// was tested on hardware, not a property of the bytes. So a newer ROCm image may
// require a newer kernel than the shipped floors encode, and `villa update` cannot
// re-derive that over the network — it must be told, which is why floors are an
// attribute of the ROCm pin and travel in the manifest beside it.
type Floors = preflight.Floor

// Entry is one component's row in the table.
type Entry struct {
	// Component is the stable id, written into the store and into manifests.
	Component ComponentID
	// Subsystem is which part of the stack this component belongs to, and
	// therefore which proof gates an update to it.
	Subsystem subsystem.Kind
	// Unit is the Quadlet .container unit this pin renders into, for a pin whose
	// subsystem renders that unit only on some configs. For(k, cfg) keeps such a
	// pin only when the subsystem's unit registry renders the unit for cfg, so the
	// pin set a host updates is the unit set it runs and both read the one gate in
	// internal/subsystem. Empty for a pin the subsystem always runs.
	Unit string
	// Shape is what a moved pin means for this component.
	Shape Shape
	// Registry is the host the component's bytes come from. This is the allowlist:
	// a manifest naming any other host for this component is refused, so a stolen
	// signing key cannot redirect a pull to a host the operator never trusted.
	Registry string
	// Version is the declared upstream version for a VersionTag or
	// ChecksummedAsset component, and empty for a RollingDigest one — a rolling tag
	// has no version to name, which is the whole reason its shape differs.
	Version string
	// Floors returns the host thresholds this pin demands, or nil where the
	// component has none. Only the ROCm backends carry floors; nothing else in the
	// stack makes a claim about the kernel it needs.
	Floors func() Floors
	// Vetted returns the pin villa's maintainer proved on gfx1151 hardware. It is
	// an accessor into the package that owns the literal, never a copy of it.
	Vetted func() Pin
}

// HasFloors reports whether this component's pin carries host thresholds, so a
// caller can branch without a nil check that reads like a defensive habit.
func (e Entry) HasFloors() bool { return e.Floors != nil }

// VettedSerial is the compiled-in manifest serial: the anti-downgrade floor a
// fresh install starts from.
//
// It is here rather than in the state store because it must survive the store being
// deleted. An absent store falling back to zero would mean "no floor", which
// silently re-opens the replay attack the serial exists to close — so the floor's
// home is the one place that cannot be absent.
const VettedSerial uint64 = 2

// Serial returns the compiled-in manifest serial, as an accessor so callers bind a
// symbol rather than a literal.
func Serial() uint64 { return VettedSerial }

// registry hosts. These are HOST names, not image references: no trailing path
// element, no tag, no digest. Written plainly because a host name carries no
// backend imperative — a bare host is where bytes come from, while a host followed
// by a repository path is an image literal and belongs behind the inference seam.
// That distinction is also what keeps this file clear of the seam grep gate.
const (
	registryDockerIO = "docker.io"
	registryGHCR     = "ghcr.io"
	registryGCR      = "gcr.io"
	registryGitHub   = "github.com"
	// registryLocalhost is where a villa-BUILT image lives. It is on the allowlist
	// for the same reason as the others — RegistryAllowed answers "may villa talk to
	// this host at all" — and admitting it costs nothing a manifest could abuse: a
	// manifest naming localhost redirects a pull to the operator's own store, which
	// is where the sandbox image already is.
	registryLocalhost = "localhost"
)

// backendPin returns the vetted pin for one backend name through the inference
// seam's single polymorphism point.
//
// BackendFor fails closed on an unknown name, and the four names here are compiled
// in beside it, so an error is unreachable: it would mean this table names a
// backend BackendFor does not resolve, which is a build-time programming error of
// exactly the class this package refuses to give a runtime error path. The empty
// pin it would return is caught by TestEveryAccessorResolves rather than by a user.
func backendPin(name string) Pin {
	b, err := inference.BackendFor(name)
	if err != nil {
		return Pin{}
	}
	return Pin{Ref: b.Image()}
}

// crushPin returns the vetted Crush pin: the release version, with the TARBALL
// checksum for the target platform.
//
// The tarball checksum is the right one to carry because it is what the install
// gate verifies before anything is extracted. The extracted-binary checksum is a
// drift-detection value over an artifact that only exists after a successful
// install, so it is not what a manifest would be supplying.
func crushPin() Pin {
	p := agent.LoadCrushPolicy()
	asset, ok := p.Assets[crushPlatform]
	if !ok {
		return Pin{Ref: p.Version}
	}
	return Pin{Ref: p.Version, Checksum: asset.SHA256}
}

// crushPlatform is the platform key the Crush asset table is read under. It is
// fixed rather than derived from the running platform because the table is a
// description of what villa VETTED, and villa is vetted on Fedora/Strix Halo only.
// Deriving it would also put a platform branch in a package the seam gate walks.
const crushPlatform = "linux/amd64"

// crushRegistry extracts the host from the Crush release URL template, so the
// allowlist entry cannot drift from the URL the download actually uses. Every other
// component's registry is a bare host in the image reference; this one lives inside
// a URL, and reading it out is more honest than re-typing it.
func crushRegistry() string {
	tmpl := agent.LoadCrushPolicy().URLTmpl
	rest := strings.TrimPrefix(strings.TrimPrefix(tmpl, "https://"), "http://")
	host, _, _ := strings.Cut(rest, "/")
	return host
}

// Table is every pinned component, in stack order.
//
// It is a function rather than a package-level var so no caller can append to it or
// reorder it: the table is villa's statement about what it vetted, and a mutable
// one would let a bug in any package rewrite the allowlist.
func Table() []Entry {
	rocmFloors := func() Floors { return preflight.Floors() }

	return []Entry{
		{
			Component: BackendROCm724,
			Subsystem: subsystem.Inference,
			// A version tag: `rocm-7.2.4` names a ROCm release, so a moved digest
			// means that release was rebuilt, not that a new one exists.
			Shape:    VersionTag,
			Registry: registryDockerIO,
			Version:  "7.2.4",
			Floors:   rocmFloors,
			Vetted:   func() Pin { return backendPin("rocm-7.2.4") },
		},
		{
			Component: BackendROCm100,
			Subsystem: subsystem.Inference,
			// A rolling channel, not a version tag (ADR-0022): upstream rebuilds
			// this tag daily on llama.cpp master, so a moved digest is a newer
			// build of the same channel, and there is no version to name.
			Shape:    RollingDigest,
			Registry: registryDockerIO,
			Floors:   rocmFloors,
			Vetted:   func() Pin { return backendPin("rocm-10.0") },
		},
		{
			Component: BackendROCm644,
			Subsystem: subsystem.Inference,
			Shape:     VersionTag,
			Registry:  registryDockerIO,
			Version:   "6.4.4",
			Floors:    rocmFloors,
			Vetted:    func() Pin { return backendPin("rocm-6.4.4") },
		},
		{
			Component: BackendROCm644WMMA,
			Subsystem: subsystem.Inference,
			Shape:     VersionTag,
			Registry:  registryDockerIO,
			Version:   "6.4.4-rocwmma",
			Floors:    rocmFloors,
			Vetted:    func() Pin { return backendPin("rocm-6.4.4-rocwmma") },
		},
		{
			Component: BackendVulkan,
			Subsystem: subsystem.Inference,
			// A rolling tag: `vulkan-radv` names no version, so a moved digest is a
			// rebuild with nothing to call it. No floors — the Vulkan path is the
			// fallback precisely because it demands less of the host than ROCm.
			Shape:    RollingDigest,
			Registry: registryDockerIO,
			Vetted:   func() Pin { return backendPin("vulkan") },
		},
		{
			Component: OpenWebUI,
			Subsystem: subsystem.Chat,
			// `:main` is the definition of a rolling tag.
			Shape:    RollingDigest,
			Registry: registryGHCR,
			Vetted:   func() Pin { return Pin{Ref: orchestrate.OpenWebUIImage()} },
		},
		{
			Component: Qdrant,
			Subsystem: subsystem.Memory,
			Shape:     VersionTag,
			Registry:  registryDockerIO,
			Version:   "1.18.2",
			Vetted:    func() Pin { return Pin{Ref: orchestrate.QdrantImage()} },
		},
		{
			Component: Embedder,
			Subsystem: subsystem.Memory,
			Shape:     RollingDigest,
			Registry:  registryDockerIO,
			Vetted:    func() Pin { return Pin{Ref: orchestrate.EmbedImage()} },
		},
		{
			Component: Extractor,
			Subsystem: subsystem.Memory,
			// Memory renders villa-extract only with `extractor = true`, so the pin
			// is bound to the unit and follows that gate (ADR-0033).
			Unit:     orchestrate.ExtractContainerUnitName(),
			Shape:    VersionTag,
			Registry: registryDockerIO,
			Version:  "3.3.1.0",
			Vetted:   func() Pin { return Pin{Ref: orchestrate.ExtractImage()} },
		},
		{
			Component: SearXNG,
			Subsystem: subsystem.WebSearch,
			Shape:     RollingDigest,
			Registry:  registryGHCR,
			Vetted:    func() Pin { return Pin{Ref: orchestrate.SearXNGImage()} },
		},
		{
			Component: Websafe,
			Subsystem: subsystem.WebSearch,
			Shape:     RollingDigest,
			Registry:  registryGCR,
			Vetted:    func() Pin { return Pin{Ref: orchestrate.WebsafeImage()} },
		},
		{
			Component: Crush,
			Subsystem: subsystem.Agent,
			Shape:     ChecksummedAsset,
			Registry:  registryGitHub,
			Version:   agent.LoadCrushPolicy().Version,
			Vetted:    crushPin,
		},
		{
			Component: SandboxImage,
			Subsystem: subsystem.Sandbox,
			// A rolling digest, and the shape is exact rather than convenient: a dnf
			// build is not byte-reproducible, so rebuilding at the SAME pinned package
			// versions yields a new digest with no version to name. That is the
			// rebuild-versus-bump ambiguity the ROCm images have, arrived at from the
			// other direction, and --check reports it the same way.
			Shape:    RollingDigest,
			Registry: registryLocalhost,
			Vetted:   func() Pin { return Pin{Ref: orchestrate.SandboxImage()} },
		},
		{
			Component: Whisper,
			Subsystem: subsystem.Voice,
			// `main-vulkan` is a rolling channel on whisper.cpp master: no version.
			Shape:    RollingDigest,
			Registry: registryGHCR,
			Vetted:   func() Pin { return Pin{Ref: orchestrate.WhisperImage()} },
		},
		{
			Component: Kokoro,
			Subsystem: subsystem.Voice,
			Shape:     VersionTag,
			Registry:  registryGHCR,
			Version:   "0.9.0",
			Vetted:    func() Pin { return Pin{Ref: orchestrate.KokoroImage()} },
		},
		{
			Component: ImageServer,
			Subsystem: subsystem.Image,
			// master-vulkan is rebuilt on every upstream push: a rolling digest with
			// no version to name. Not yet named in the signed manifest (the
			// backend-rocm-10.0 precedent, ADR-0022): it reports as current against
			// this compiled-in pin until a later manifest offers a newer digest.
			Shape:    RollingDigest,
			Registry: registryGHCR,
			Vetted:   func() Pin { return Pin{Ref: orchestrate.ImageServerImage()} },
		},
	}
}

// Lookup returns the entry for a component id.
//
// The bool is the allowlist answer: a false means the table does not name this
// component, which is how a manifest carrying an unknown component is refused. A
// manifest that could introduce components would be a remote-code-execution channel
// with extra steps, so this is the check that keeps it to supplying VALUES.
func Lookup(id ComponentID) (Entry, bool) {
	for _, e := range Table() {
		if e.Component == id {
			return e, true
		}
	}
	return Entry{}, false
}

// For returns every entry of one subsystem that a host running cfg renders, in
// table order. It is what the update flow walks: the proof unit is the subsystem,
// so the pins that move together are the pins that answer to the same proof.
//
// An entry bound to a unit is kept only when the subsystem's unit registry
// renders that unit for cfg. A static per-subsystem list would name a pin for a
// unit the host never rendered, and update would pull, prove and record an
// effective pin for a service that does not exist.
func For(k subsystem.Kind, cfg config.VillaConfig) []Entry {
	var out []Entry
	for _, e := range Table() {
		if e.Subsystem == k && e.RenderedBy(cfg) {
			out = append(out, e)
		}
	}
	return out
}

// RenderedBy reports whether a host running cfg renders this pin's unit: true for
// a pin with no unit, and for a bound one exactly when its subsystem's unit
// registry renders that unit for cfg. It is the one rule every per-subsystem walk
// of the table applies, so no walk can hold a pin the host has no unit for.
func (e Entry) RenderedBy(cfg config.VillaConfig) bool {
	if e.Unit == "" {
		return true
	}
	rendered, _ := e.Subsystem.Units(cfg)
	return slices.Contains(rendered, e.Unit)
}

// RegistryAllowed reports whether a host is one the table already pulls from.
//
// This is the second half of the allowlist and it is deliberately global rather
// than per-component: the question a fetch asks is "may villa talk to this host at
// all", and answering it per component would make the answer depend on which
// component a manifest CLAIMS a host belongs to — which is attacker-supplied.
func RegistryAllowed(host string) bool {
	for _, e := range Table() {
		if e.Registry == host {
			return true
		}
	}
	return false
}
