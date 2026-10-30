package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// useConfiguredCatalog points XDG_CONFIG_HOME at a temp dir whose config.toml names
// the fixture catalog, so a verb that reads the persisted config sees catalog_path.
func useConfiguredCatalog(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "villa"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "model = \"hasdraft\"\nquant = \"Q4_K_M\"\ncatalog_path = \"" + draftCatalogFile(t) + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "villa", "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestModelVerbsResolveTheConfiguredCatalog guards the documented catalog_path
// override: an entry that exists only in the file config.toml names must resolve
// through every catalog-reading verb, not just recommend. Before the fix `model
// swap`, `model list` and the stack apply read the embedded seed unconditionally,
// so a host with a curated catalog could not swap to, list or render its own entry.
func TestModelVerbsResolveTheConfiguredCatalog(t *testing.T) {
	useConfiguredCatalog(t)

	t.Run("model swap resolves the entry", func(t *testing.T) {
		if _, ok := liveSwapDeps(context.Background()).ResolveCatalog("hasdraft"); !ok {
			t.Fatal("model swap could not resolve an entry that only the configured catalog_path carries")
		}
	})
	t.Run("model list shows the entry", func(t *testing.T) {
		cat, _, err := liveListDeps().loadCatalog()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := cat.FindByID("hasdraft"); !ok {
			t.Fatal("model list did not load the configured catalog")
		}
	})
	t.Run("stack apply resolves the entry", func(t *testing.T) {
		cat, err := liveStackDeps().Catalog()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := cat.FindByID("hasdraft"); !ok {
			t.Fatal("stack apply rendered from the embedded seed, so villa up would fail on a configured-catalog model")
		}
	})
}

// TestCatalogFlagOutranksTheConfiguredCatalog keeps the seam's precedence: an
// explicit override wins over the persisted catalog_path, and with neither set the
// embedded seed answers.
func TestCatalogFlagOutranksTheConfiguredCatalog(t *testing.T) {
	useConfiguredCatalog(t)
	prev := modelCatalogPath
	t.Cleanup(func() { modelCatalogPath = prev })

	modelCatalogPath = "/explicit/catalog.json"
	if got := resolvedCatalogPath(); got != "/explicit/catalog.json" {
		t.Fatalf("override lost to config: %q", got)
	}
	modelCatalogPath = ""
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := resolvedCatalogPath(); got != "" {
		t.Fatalf("no config and no override must mean the embedded seed, got %q", got)
	}
}
