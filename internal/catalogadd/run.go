package catalogadd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

// Deps is every host effect Run performs, as func fields the cmd tier binds to the
// real host and a test binds to a fake (the house `Deps` shape).
type Deps struct {
	// ConfiguredPath is config.toml's catalog_path, "" when unset.
	ConfiguredPath func() (string, error)
	// DefaultPath is the managed file, $XDG_CONFIG_HOME/villa/catalog-local.json.
	DefaultPath func() (string, error)
	// LoadFile reads an external catalog without Load's fallback to the seed. A file
	// that does not exist must return an error wrapping fs.ErrNotExist.
	LoadFile func(path string) (catalog.Catalog, error)
	// Seed is the embedded seed catalog, the base of a new file.
	Seed func() (catalog.Catalog, error)
	// Fetch pulls the file into the models dir and returns its digest and size. It
	// is the one outbound request, the same a model pull makes.
	Fetch func(ctx context.Context, url, filename string) (FileFacts, error)
	// ReadHeader opens the pulled file by its bare name and reads its GGUF header.
	ReadHeader func(filename string) (gguf.Header, error)
	// Lock takes the stack lock (ADR-0010) and returns its release.
	Lock func() (release func(), err error)
	// WriteFile replaces the catalog file atomically at 0600.
	WriteFile func(path string, data []byte) error
	// SetCatalogPath loads config.toml, sets catalog_path and saves it.
	SetCatalogPath func(path string) error
}

// Opts is one run's input.
type Opts struct {
	URL       string
	Overrides Overrides
}

// Result is what a run did, for the cmd tier to render.
type Result struct {
	// Model is the entry that was added, or the one already present.
	Model   catalog.Model
	Outcome Outcome
	// Path is the catalog file written or extended.
	Path string
	// Created reports that Path did not exist and was started from the seed.
	Created bool
	// SetPath reports that config.toml's catalog_path was empty and now names Path.
	SetPath bool
	// Managed reports that Path is the managed file, which `villa backup` archives.
	Managed bool
	// Notes name the values derived by policy rather than read from the file.
	Notes []string
}

// target is the catalog file a run reads, extends and writes.
type target struct {
	path    string
	base    catalog.Catalog
	created bool
	managed bool
	// configured is true when catalog_path already named path.
	configured bool
}

// loadTarget resolves the file and its current contents. A configured file that is
// unusable refuses; only an absent managed file is created, from the seed.
func loadTarget(d Deps) (target, error) {
	configured, err := d.ConfiguredPath()
	if err != nil {
		return target{}, fmt.Errorf("read catalog_path: %w", err)
	}
	def, err := d.DefaultPath()
	if err != nil {
		return target{}, fmt.Errorf("resolve the managed catalog path: %w", err)
	}

	if configured != "" {
		c, err := d.LoadFile(configured)
		if err != nil {
			return target{}, fmt.Errorf("catalog_path names %s, which villa cannot use (%w); fix or clear it, then add again. Nothing was changed", configured, err)
		}
		return target{path: configured, base: c, managed: sameFile(configured, def), configured: true}, nil
	}

	c, err := d.LoadFile(def)
	switch {
	case err == nil:
		return target{path: def, base: c, managed: true}, nil
	case errors.Is(err, fs.ErrNotExist):
		seed, serr := d.Seed()
		if serr != nil {
			return target{}, fmt.Errorf("load the embedded seed: %w", serr)
		}
		return target{path: def, base: seed, created: true, managed: true}, nil
	default:
		return target{}, fmt.Errorf("%s exists but villa cannot use it (%w); move it aside or fix it, then add again. Nothing was changed", def, err)
	}
}

func sameFile(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

// encode is the on-disk form of a catalog: indented JSON, one trailing newline.
func encode(c catalog.Catalog) ([]byte, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Run is the whole verb behind one call (ADR-0041): validate the source, decide from
// it alone whether this is a no-op or a refusal, pull and hash the file, read its
// header, build the entry, and then, holding the stack lock, merge it into the
// catalog file, write that file, and set catalog_path if it was empty.
//
// The pull runs without the lock: a multi-gigabyte download must not block every swap
// on the host, and nothing in it touches the stack. The catalog is read again under
// the lock, so a concurrent add cannot be overwritten. The catalog file is written
// before catalog_path is set, because a config naming a file that does not exist is
// worse than a file nothing names.
func Run(ctx context.Context, d Deps, o Opts) (Result, error) {
	src, err := ParseSource(o.URL)
	if err != nil {
		return Result{}, err
	}
	names, err := ResolveNames(src, o.Overrides)
	if err != nil {
		return Result{}, err
	}

	tgt, err := loadTarget(d)
	if err != nil {
		return Result{}, err
	}
	outcome, err := Precheck(tgt.base, names.ID, src)
	if err != nil {
		return Result{}, err
	}
	if outcome == AlreadyPresent {
		existing, _ := tgt.base.FindByID(names.ID)
		return Result{Model: existing, Outcome: AlreadyPresent, Path: tgt.path, Managed: tgt.managed}, nil
	}

	facts, err := d.Fetch(ctx, src.URL, src.Filename)
	if err != nil {
		return Result{}, fmt.Errorf("pull %s: %w", src.Filename, err)
	}
	hdr, err := d.ReadHeader(src.Filename)
	if err != nil {
		return Result{}, fmt.Errorf("read the GGUF header of %s: %w", src.Filename, err)
	}
	hf, err := HeaderFactsOf(hdr)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", src.Filename, err)
	}
	model, notes, err := Build(src, names, facts, hf, o.Overrides)
	if err != nil {
		return Result{}, err
	}

	release, err := d.Lock()
	if err != nil {
		return Result{}, fmt.Errorf("take the stack lock: %w", err)
	}
	defer release()

	tgt, err = loadTarget(d)
	if err != nil {
		return Result{}, err
	}
	merged, outcome, err := Merge(tgt.base, model)
	if err != nil {
		return Result{}, err
	}
	res := Result{Model: model, Outcome: outcome, Path: tgt.path, Created: tgt.created, Managed: tgt.managed, Notes: notes}
	if outcome == AlreadyPresent {
		res.Model, _ = tgt.base.FindByID(names.ID)
		res.Created, res.Notes = false, nil
		return res, nil
	}

	data, err := encode(merged)
	if err != nil {
		return Result{}, fmt.Errorf("encode the catalog: %w", err)
	}
	if err := d.WriteFile(tgt.path, data); err != nil {
		return Result{}, fmt.Errorf("write %s: %w", tgt.path, err)
	}
	if !tgt.configured {
		if err := d.SetCatalogPath(tgt.path); err != nil {
			return res, fmt.Errorf("%s was written, but catalog_path could not be set (%w); run `villa config set catalog_path=%s`", tgt.path, err, tgt.path)
		}
		res.SetPath = true
	}
	return res, nil
}
