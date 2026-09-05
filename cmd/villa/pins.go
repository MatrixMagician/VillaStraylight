package main

// pins.go is the cmd tier's binding between the pin resolver and the renderer.
//
// It is the ONE place a rendered unit learns what this host is actually running.
// Every `villa` verb that renders — install, up, restart, backend set, model swap,
// coding-mode, restore, doctor, status, the resident set — goes through
// livePinnedRender, so an effective pin recorded by an update reaches all of them
// or none of them. Eight independently-wired call sites would eventually be seven.
//
// Reading the store is HOST I/O, which is why this lives here and not in
// internal/orchestrate: the renderer stays pure, and this file is the seam that
// feeds it.

import (
	"cmp"
	"fmt"
	"os"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pinresolve"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// livePinStateDeps wires the pin state store to the real filesystem, mirroring the
// verifystate / recall wiring.
func livePinStateDeps() pinstate.Deps {
	path := pinstate.Path()
	return pinstate.Deps{
		ReadAll: func() ([]byte, error) {
			data, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				return nil, nil // absent store ⇒ Load fails closed to vetted pins
			}
			return data, err
		},
		WriteAll: func(data []byte) error { return pinstate.WriteFileAtomic(path, data) },
	}
}

// liveResolver loads the host's pin state and joins it to the compiled-in table.
//
// An unreadable store yields the ZERO state rather than an error, and the resolver
// turns that into vetted pins for everything. That is deliberate and it is why this
// returns no error: a machine that has never run `villa update` has no store, which
// is the normal state of every fresh install and must not make `villa up` fail.
func liveResolver() pinresolve.Resolver {
	state, err := pinstate.Load(livePinStateDeps())
	if err != nil {
		// A real read error (permissions, a directory where the file should be).
		// Falling back to vetted pins keeps the stack renderable; the divergence
		// report is what surfaces the problem, not a refusal to render.
		state = pinstate.State{}
	}
	return pinresolve.New(state)
}

// livePinFunc is the RenderInput.Pin seam: a component id to the image this host
// should run, or "" for "use the compiled-in pin".
//
// It answers ONLY for components the table names. An id it does not recognise gets
// "", which the renderer reads as the vetted fallback — so a typo in a component
// constant renders the pin villa shipped rather than an empty Image= line.
func livePinFunc(r pinresolve.Resolver) func(string) string {
	return func(component string) string {
		res, ok := r.Resolve(pins.ComponentID(component))
		if !ok || !res.FromStore {
			return ""
		}
		return res.Current.Ref
	}
}

// livePinnedBackend applies the inference subsystem's effective pin to a resolved
// backend, by wrapping it inside the seam.
//
// The backend takes this path rather than RenderInput.Pin because it already flows
// through in.Backend, which is the seam that owns every image literal. Giving one
// component two routes to a pin is how two answers eventually appear.
//
// ONLY the active backend is repinned, and only its own component id is consulted:
// the other three backend images stay on their vetted pins because proving them
// would require swapping to each in turn, and because the vulkan landing spot must
// stay known-good — it is the rollback target precisely because it is proven.
func livePinnedBackend(r pinresolve.Resolver, backend inference.Backend) inference.Backend {
	if backend == nil {
		return nil
	}
	id, ok := backendComponent(backend.Name())
	if !ok {
		return backend
	}
	res, ok := r.Resolve(id)
	if !ok || !res.FromStore {
		return backend
	}
	return inference.Repinned(backend, res.Current.Ref)
}

// backendComponent maps a backend's seam-sourced Name() to its component id.
//
// It keys off Name() rather than the config string because Name() is what the
// backend reports about itself after BackendFor has already fail-closed on an
// unknown value — so an unrecognised name here means the table and the seam
// disagree, and the safe answer is "no effective pin", not a guess.
func backendComponent(name string) (pins.ComponentID, bool) {
	switch name {
	case "rocm":
		return pins.BackendROCm724, true
	case "rocm-6.4.4":
		return pins.BackendROCm644, true
	case "rocm-6.4.4-rocwmma":
		return pins.BackendROCm644WMMA, true
	case "vulkan":
		return pins.BackendVulkan, true
	}
	return "", false
}

// livePinnedRender is the render entry point every verb uses instead of
// orchestrate.Render.
//
// It loads the resolver once per render, applies the active backend's effective pin
// by wrapping the Backend, and hands the managed-service components a Pin seam. On
// a host with no pin state — every fresh install — both are no-ops and the rendered
// units are byte-identical to what orchestrate.Render produced before this existed.
func livePinnedRender(in orchestrate.RenderInput) ([]orchestrate.Unit, error) {
	r := liveResolver()
	in.Backend = livePinnedBackend(r, in.Backend)
	in.Pin = livePinFunc(r)
	if in.Speculation == nil {
		spec, err := liveSpeculation(in.Cfg, in.CodingMode != nil)
		if err != nil {
			return nil, err
		}
		in.Speculation = spec
	}
	if in.Projector == "" {
		projector, err := liveProjector(in.Cfg, in.CodingMode != nil)
		if err != nil {
			return nil, err
		}
		in.Projector = projector
	}
	return orchestrate.Render(in)
}

// liveProjector turns the persisted vision decision plus the served catalog entry
// into the projector filename the render needs, the catalog-to-inference
// translation the pure renderer cannot do itself (the liveSpeculation precedent).
//
// Coding mode resolves to "" because coding mode is text-only by construction: the
// coder entry is picked for tool-calling, its agent context is sized without a
// projector, and villa has never qualified one for it.
//
// A config that says vision on for an entry shipping no projector is a refusal
// here rather than a silent text-only render, for the reason ADR-0006 gives about
// a speculation mode: the operator persisted a decision and would have no way to
// tell it had been dropped.
func liveProjector(cfg config.VillaConfig, coding bool) (string, error) {
	if !cfg.Vision || coding {
		return "", nil
	}
	cat, _, err := catalog.Load(cmp.Or(modelCatalogPath, cfg.CatalogPath))
	if err != nil {
		return "", fmt.Errorf("load model catalog: %w", err)
	}
	m, ok := cat.FindByID(cfg.Model)
	if !ok {
		return "", fmt.Errorf("vision: served model %q is not in the catalog", cfg.Model)
	}
	if m.Projector == nil || len(m.Projector.Shards) == 0 {
		return "", fmt.Errorf("vision is on in config but %s ships no projector; run villa recommend --save or villa install", m.ID)
	}
	return m.Projector.Shards[0].Filename, nil
}

// liveSpeculation turns the persisted mode plus the served catalog entry into the
// render descriptor, the catalog-to-inference translation the pure renderer cannot
// do itself (the codingDescriptor precedent).
//
// It is the ONE place config becomes a speculation descriptor, so a config asking
// for a mode the served entry is not qualified for is a refusal here rather than a
// silent downgrade at each render site (ADR-0006). An off or unset mode returns nil
// without reading the catalog at all: a stack that asked for nothing must still
// render when the catalog is unreadable.
func liveSpeculation(cfg config.VillaConfig, coding bool) (*inference.SpeculationSpec, error) {
	if cfg.Speculation == "" || cfg.Speculation == config.SpeculationOff {
		return nil, nil
	}
	served := cfg.Model
	if coding {
		served = cfg.CoderModel
	}
	cat, _, err := catalog.Load(cmp.Or(modelCatalogPath, cfg.CatalogPath))
	if err != nil {
		return nil, fmt.Errorf("load model catalog: %w", err)
	}
	m, ok := cat.FindByID(served)
	if !ok {
		return nil, fmt.Errorf("speculation: served model %q is not in the catalog", served)
	}
	mode, note, ok := recommend.ResolveSpeculation(m, cfg.Speculation)
	if !ok {
		return nil, fmt.Errorf("speculation: %s", note)
	}
	if mode == config.SpeculationOff {
		return nil, nil
	}
	return &inference.SpeculationSpec{Mode: config.SpeculationNgram}, nil
}

// resolverFor builds a resolver over an already-loaded pin state.
//
// It exists so the update verbs bind the same join every render does, without
// re-reading the store: those verbs have already loaded the state to read its
// serial and CheckedAt, and loading it twice invites the two reads disagreeing.
func resolverFor(state pinstate.State) pinresolve.Resolver {
	return pinresolve.New(state)
}
