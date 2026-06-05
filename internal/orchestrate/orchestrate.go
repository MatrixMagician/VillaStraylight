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
}

// Plan is the result of a Reconcile: the rendered units whose on-disk hash differs
// (or are absent) versus those already identical on disk. An empty Changed slice is
// a true no-op (CLI-01 idempotency core).
type Plan struct {
	// Changed are units that must be (re)written — absent or hash-mismatched on disk.
	Changed []Unit
	// Unchanged are units already byte-identical on disk (no write, no restart).
	Unchanged []Unit
}
