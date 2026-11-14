package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/catalogadd"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/download"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// catalogFetchFn is the downloader seam of `catalog add`. It defaults to download.Fetch
// (the model-pull path with the checksum learned, ADR-0041) and is overridden in tests
// so the verb runs end to end without a network.
var catalogFetchFn = func(ctx context.Context, url, filename, modelsDir string) (download.Fetched, error) {
	return download.Fetch(ctx, url, filename, modelsDir)
}

// newCatalog builds the `villa catalog` noun.
func newCatalog() *cobra.Command {
	c := &cobra.Command{
		Use:   "catalog",
		Short: "Add a model of your own to the catalog",
		Long: "Manage the external catalog file `catalog_path` names (see docs/CONFIGURATION.md). The only " +
			"outbound traffic is the model pull `catalog add` makes.",
		Args: cobra.NoArgs,
	}
	c.AddCommand(newCatalogAdd())
	return c
}

// newCatalogAdd builds `villa catalog add <url>`: a thin caller that parses flags,
// runs catalogadd.Run over the live host, and renders the Result (ADR-0041).
func newCatalogAdd() *cobra.Command {
	var ov catalogadd.Overrides
	cmd := &cobra.Command{
		Use:   "add <url>",
		Short: "Pull a commit-pinned GGUF and add it to the catalog as an operator-supplied entry",
		Long: "Download the GGUF at <url> into the models dir, hash it, read its header for the KV-cache " +
			"geometry, and write a catalog entry for it, then point `catalog_path` at the file when it names none.\n\n" +
			"<url> must be https://huggingface.co/<org>/<repo>/resolve/<40-hex commit>/<file>.gguf: a branch or " +
			"tag name is a moving target and is refused, as is any other host and a numbered shard of a split model.\n\n" +
			"The entry starts with speculation unqualified, no vision projector and unified_memory_safe false " +
			"(never auto-selected); select it with `villa model swap <id>`. The sha256 is computed from the file " +
			"villa received, so it is trust on first use at that commit. Exits 0 on success or when the model is " +
			"already catalogued, 1 on any refusal or failure.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := runCatalogAdd(cmd, args[0], ov, liveCatalogAddDeps(cmdContext(cmd)))
			os.Exit(code)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&ov.ID, "id", "", "catalog id (default: the file name, lowercased)")
	f.StringVar(&ov.Quant, "quant", "", "quant label (default: parsed from the file name; required when it has none)")
	f.StringVar(&ov.DisplayName, "name", "", "display name (default: the file name)")
	f.IntVar(&ov.Ctx, "ctx", 0, "default context length (default: the header's trained context, capped at 32768)")
	return cmd
}

// liveCatalogAddDeps wires catalogadd.Deps to the real host: config.toml, the catalog
// files, the models dir, the verified downloader and the stack lock. ctx is the
// command's SIGINT/SIGTERM-cancelled context, so Ctrl-C interrupts the pull; the
// .part file is kept and the next add resumes it.
func liveCatalogAddDeps(ctx context.Context) catalogadd.Deps {
	return catalogadd.Deps{
		ConfiguredPath: func() (string, error) {
			cfg, err := config.LoadVilla()
			return cfg.CatalogPath, err
		},
		DefaultPath: config.LocalCatalogPath,
		LoadFile:    catalog.LoadFile,
		Seed:        catalog.Seed,
		Fetch: func(_ context.Context, url, filename string) (catalogadd.FileFacts, error) {
			dir := modelsDir()
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return catalogadd.FileFacts{}, fmt.Errorf("cannot create the models dir %q: %w", dir, err)
			}
			got, err := catalogFetchFn(ctx, url, filename, dir)
			return catalogadd.FileFacts{SHA256: got.SHA256, SizeBytes: got.SizeBytes}, err
		},
		ReadHeader: func(filename string) (gguf.Header, error) {
			dir := modelsDir()
			path := filepath.Join(dir, filename)
			if err := pathsafe.Inside(path, dir); err != nil {
				return gguf.Header{}, err
			}
			f, err := os.Open(path) //nolint:gosec // confined to the models dir immediately above
			if err != nil {
				return gguf.Header{}, err
			}
			defer f.Close()
			return gguf.ReadHeader(f)
		},
		Lock: func() (func(), error) {
			lock, err := acquireStackLock()
			if err != nil {
				return nil, err
			}
			return func() { _ = lock.Release() }, nil
		},
		WriteFile: writeCatalogFile,
		SetCatalogPath: func(path string) error {
			cfg, err := config.LoadVilla()
			if err != nil {
				return err
			}
			cfg.CatalogPath = path
			return config.SaveVilla(cfg)
		},
	}
}

// writeCatalogFile replaces the catalog at path atomically at 0600. The directory is
// created 0700 when absent (a fresh host has no config dir yet), a symlink target is
// refused as catalog.Load refuses one, and the containment root is the file's own
// directory: the operator named the path, and what the guard rules out is a name that
// resolves anywhere else.
func writeCatalogFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink (refused)", path)
	}
	return pathsafe.WriteFileAtomic(dir, path, data, 0o600)
}

// runCatalogAdd runs the verb and RETURNS the exit code (no os.Exit) so tests assert
// output and code. All rendering lives here; catalogadd decides, this prints.
func runCatalogAdd(cmd *cobra.Command, url string, ov catalogadd.Overrides, d catalogadd.Deps) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	res, err := catalogadd.Run(cmdContext(cmd), d, catalogadd.Opts{URL: url, Overrides: ov})
	if err != nil {
		fmt.Fprintf(errOut, "catalog add: %v\n", err)
		return exitBlocked
	}
	renderCatalogAdd(out, res)
	return exitPass
}

// renderCatalogAdd prints what was added and, for every value villa chose rather than
// read, why, so the operator can see the one policy number (default_ctx) and the
// entry's deliberate absences.
func renderCatalogAdd(out io.Writer, res catalogadd.Result) {
	m := res.Model
	if res.Outcome == catalogadd.AlreadyPresent {
		fmt.Fprintf(out, "%s is already in %s from this source; left unchanged (nothing pulled)\n", m.ID, res.Path)
		return
	}
	s := m.Shards[0]
	fmt.Fprintf(out, "added %s (%s) to %s\n", m.ID, m.Quant, res.Path)
	fmt.Fprintf(out, "  source       %s\n", s.URL)
	fmt.Fprintf(out, "  sha256       %s (hashed from the file villa received: trust on first use at this commit)\n", s.SHA256)
	fmt.Fprintf(out, "  size         %d bytes\n", s.SizeBytes)
	fmt.Fprintf(out, "  geometry     n_layers=%d n_kv_heads=%d head_dim=%d (from the GGUF header)\n", m.NLayers, m.NKVHeads, m.HeadDim)
	if m.SWA != nil {
		fmt.Fprintf(out, "  sliding      n_layers=%d n_kv_heads=%d head_dim=%d window=%d\n", m.SWA.NLayers, m.SWA.NKVHeads, m.SWA.HeadDim, m.SWA.Window)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(out, "  %s\n", n)
	}
	if res.Created {
		fmt.Fprintf(out, "created %s from the embedded seed plus this entry\n", res.Path)
	}
	if res.SetPath {
		fmt.Fprintf(out, "set catalog_path=%s\n", res.Path)
	}
	fmt.Fprintf(out, "note: speculation is unqualified and there is no vision projector; unified_memory_safe is false, so `villa recommend` will not auto-select it.\n")
	fmt.Fprintf(out, "next: `villa model swap %s`\n", m.ID)
	if res.Managed {
		fmt.Fprintf(out, "note: %s is archived by `villa backup`.\n", filepath.Base(res.Path))
	} else {
		fmt.Fprintf(out, "note: %s is outside the villa config dir, so `villa backup` does not archive it; back it up yourself.\n", res.Path)
	}
}
