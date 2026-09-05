// Package orchestrate turns the single config source of truth (config.toml) plus
// the proven inference.Backend into rootless Podman Quadlet units, content-hash
// reconciles them against what is already on disk, and writes any changed unit
// atomically. It is the ONE impure module of Phase 3 (filesystem + os/exec); every
// lifecycle verb in the later slices renders and reconciles THROUGH it.
//
// Render is a PURE function (no filesystem, no systemctl): it builds an
// inference.RunSpec and obtains every backend literal — the digest-pinned image,
// the GPU device passthrough, the rootless group-add, the loopback host publish,
// the mandatory llama-server flags — THROUGH in.Backend.Image() and
// in.Backend.ContainerArgs(spec). It NEVER re-types those literals, so the
// backend grep-gate (internal/inference TestSeamGrepGate) stays green and a future
// ROCm/Metal backend reshapes the rendered units without touching this package.
//
// Reconcile is likewise pure (sha256 render-vs-disk diff). Only WriteUnits and
// systemd.go touch the host: WriteUnits writes a sibling temp then os.Rename
// (atomic, never a half-written unit) and refuses any target outside the unit dir;
// systemd.go is a thin fixed-arg os/exec seam over systemctl/loginctl/journalctl.
package orchestrate

import (
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// Unit is one rendered Quadlet unit: its on-disk filename (e.g.
// "villa-llama.container") and its full rendered text. It is the currency Render
// produces and Reconcile/WriteUnits consume.
type Unit struct {
	// Name is the unit filename written under the unit dir (never a path).
	Name string
	// Text is the fully rendered unit file content.
	Text string
}

// RenderInput is the pure input to Render: the chosen backend (the literal seam),
// the persisted config selection, and the resolved model file + host models dir.
type RenderInput struct {
	// Backend is the GPU backend whose Image()/ContainerArgs() supply every
	// imperative literal. Never re-typed by this package.
	Backend inference.Backend
	// Cfg is the persisted recommend selection (model/quant/ctx/backend).
	Cfg config.VillaConfig
	// ModelFile is the GGUF filename inside the bound models dir (catalog-resolved).
	ModelFile string
	// ModelsDir is the host directory bind-mounted read-only at the container's
	// models path. A host path, never shell-interpolated.
	ModelsDir string

	// HostVillaPath is the host filesystem path to the running villa binary
	// (os.Executable() captured at install time), bind-mounted READ-ONLY into the
	// villa-websafe container so the hidden `villa websafe-serve` subcommand can run
	// there (Phase-31 Area 1). A host path, NEVER shell-interpolated. Consumed ONLY
	// inside the WebSearchEnabled branch; when web search is off it is unused and the
	// rendered stack is byte-identical to v1.4.
	HostVillaPath string

	// CodingMode is the OPTIONAL pre-translated coding-mode render descriptor
	// nil ⇒ the off path: Render leaves spec.CodingMode nil and the
	// rendered unit is byte-identical to v1.3. Non-nil ⇒ Render sets
	// spec.CodingMode and overrides spec.ContextLen with CoderAgentCtx (the single -c,
	// Pitfall 1). The CALLER (Plan-02 live wiring) resolves the coder catalog entry once
	// and translates catalog.AgentSampling → inference.Sampling, so the pure renderer
	// and internal/inference — never import internal/catalog (clean dependency direction).
	CodingMode *inference.CodingModeSpec
	// CoderAgentCtx is the resolved agent context the coder unit is rendered with. Used
	// ONLY when CodingMode != nil, where it overrides Cfg.Ctx as the single -c value
	// (Pitfall 1: the agent ctx is carried by the existing -c, never a second one).
	CoderAgentCtx int

	// Speculation is the OPTIONAL pre-resolved speculation descriptor (ADR-0006).
	// nil ⇒ the off path: Render leaves spec.Speculation nil and the rendered unit
	// is byte-identical to one from before this field existed. The CALLER resolves
	// the served catalog entry's qualification against the persisted mode, so the
	// pure renderer never imports internal/catalog (the CodingMode precedent above).
	Speculation *inference.SpeculationSpec

	// Projector is the OPTIONAL vision projector filename inside the models dir.
	// "" ⇒ the off path: the rendered unit is byte-identical to one from before
	// this field existed. The CALLER resolves the served catalog entry's sidecar, so
	// the pure renderer never imports internal/catalog (the CodingMode precedent).
	Projector string

	// Resident are the OPTIONAL secondary resident models, one extra .container each.
	// Empty ⇒ the off path by construction: no extra unit is rendered and Open WebUI
	// keeps its singular endpoint env, so the rendered stack is byte-identical to a
	// stack with no resident set. The CALLER resolves each config.ResidentModel's
	// catalog id to a GGUF filename and hands over the resolved descriptor, so the
	// pure renderer never imports internal/catalog (the CodingMode precedent above).
	Resident []ResidentUnit

	// Pin resolves a managed-service component to the image this host should
	// actually run, returning "" for "use the compiled-in pin".
	//
	// It exists because a pin stopped being a compile-time constant: `villa update`
	// records an EFFECTIVE pin per component, and a rendered unit that ignored it
	// would make the whole update path a no-op. A nil Pin — the zero value, and what
	// every pre-update caller passes — means every component resolves to its vetted
	// pin, so the rendered units are byte-identical to before this field existed.
	//
	// It is a FUNC rather than a resolved map because Render must not import the
	// resolver: that package reads the pin state store, and this one is the pure
	// renderer. The same reason memory.RenderView hands over resolved values rather
	// than a config, and the same reason RenderInput carries a resolved
	// CodingModeSpec rather than a catalog entry.
	//
	// The INFERENCE backend is deliberately NOT resolved through this func. It
	// already flows through in.Backend, which is the seam that owns every image
	// literal, so its pin is applied by wrapping that Backend (inference.Repinned)
	// before Render is called. Routing it here as well would give one component two
	// paths to a pin, and eventually two answers.
	Pin func(component string) string
}

// pinOr returns the effective pin for a component, or the compiled-in fallback when
// no Pin seam was supplied or it has no opinion.
//
// One helper, five call sites, so the "nil means vetted" rule is written once. Five
// inline nil checks would be five chances to write one of them backwards, and a
// backwards one renders a unit with an empty Image=.
func (in RenderInput) pinOr(component, fallback string) string {
	if in.Pin == nil {
		return fallback
	}
	if ref := in.Pin(component); ref != "" {
		return ref
	}
	return fallback
}

// The component ids the render path resolves an effective pin under.
//
// They live here, beside the units that consume them, and internal/pins derives its
// ComponentID values from these constants rather than re-typing the strings. That
// direction is forced — pins imports orchestrate, not the other way round — and it
// is also the right one: the id names a thing this package renders, so a rename
// that misses one end fails to compile instead of silently orphaning a host's
// recorded effective pin.
const (
	ComponentQdrant    = "qdrant"
	ComponentEmbedder  = "embedder"
	ComponentOpenWebUI = "open-webui"
	ComponentSearXNG   = "searxng"
	ComponentWebsafe   = "websafe-base"
)

// Plan is the result of a Reconcile: the rendered units whose on-disk hash differs
// (or are absent) versus those already identical on disk. An empty Changed slice is
// a true no-op (idempotency core).
type Plan struct {
	// Changed are units that must be (re)written — absent or hash-mismatched on disk.
	Changed []Unit
	// Unchanged are units already byte-identical on disk (no write, no restart).
	Unchanged []Unit
}
