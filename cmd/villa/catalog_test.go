package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/catalogadd"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/download"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

const (
	addCommit = "4c22107061e656fb2a87a3ec2491bb61975eb581"
	addURL    = "https://huggingface.co/Youssofal/Heretic-GGUF/resolve/" + addCommit + "/Q6_K/Heretic-35B-A3B-Q6_K.gguf"
	addFile   = "Heretic-35B-A3B-Q6_K.gguf"
	addID     = "heretic-35b-a3b-q6_k"
)

// addFixture is the file the fake pull writes: a valid GGUF header and nothing after
// it, which is all `catalog add` and CAT-01 ever read of a model.
func addFixture() []byte {
	return gguf.FixtureForTest("qwen35moe", map[string]uint64{
		"qwen35moe.block_count":             40,
		"qwen35moe.full_attention_interval": 4,
		"qwen35moe.attention.head_count_kv": 2,
		"qwen35moe.attention.key_length":    256,
		"qwen35moe.context_length":          262144,
	})
}

// scratchHost points the XDG roots at temp dirs, so the verb's real wiring (config,
// the catalog file, the models dir, the stack lock) runs against a host nobody owns,
// and swaps the pull for one that writes the fixture. fetches counts the pulls.
func scratchHost(t *testing.T) (cfgDir, dataDir string, fetches *int) {
	t.Helper()
	cfgDir, dataDir = t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	n := 0
	prev := catalogFetchFn
	t.Cleanup(func() { catalogFetchFn = prev })
	catalogFetchFn = func(_ context.Context, url, filename, dir string) (download.Fetched, error) {
		n++
		body := addFixture()
		if err := os.WriteFile(filepath.Join(dir, filename), body, 0o600); err != nil {
			return download.Fetched{}, err
		}
		sum := sha256.Sum256(body)
		return download.Fetched{SHA256: hex.EncodeToString(sum[:]), SizeBytes: uint64(len(body))}, nil
	}
	return cfgDir, dataDir, &n
}

func runAdd(t *testing.T, url string, ov catalogadd.Overrides) (int, string, string) {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	code := runCatalogAdd(cmd, url, ov, liveCatalogAddDeps(cmdContext(cmd)))
	return code, out.String(), errOut.String()
}

// TestCatalogAddEndToEnd drives the real wiring on a scratch host: the file is pulled
// into the models dir, the managed catalog is created as the seed plus the entry,
// catalog_path is set, the written catalog loads without a warning, and CAT-01 (the
// doctor check) finds the entry consistent with the file it was built from.
func TestCatalogAddEndToEnd(t *testing.T) {
	cfgDir, dataDir, fetches := scratchHost(t)

	code, out, errOut := runAdd(t, addURL, catalogadd.Overrides{})
	if code != exitPass {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if *fetches != 1 {
		t.Fatalf("pulls = %d, want 1", *fetches)
	}

	managed := filepath.Join(cfgDir, "villa", "catalog-local.json")
	cfg, err := config.LoadVilla()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CatalogPath != managed {
		t.Fatalf("catalog_path = %q, want %q", cfg.CatalogPath, managed)
	}
	if fi, err := os.Stat(managed); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("catalog file: %v %v (want mode 0600)", fi, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "villa", "models", addFile)); err != nil {
		t.Fatalf("the pulled file is not in the models dir: %v", err)
	}

	cat, warnings, err := catalog.Load(managed)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("catalog.Load: %v %v", err, warnings)
	}
	m, ok := cat.FindByID(addID)
	if !ok || catalog.OriginOf(m) != catalog.OriginOperator {
		t.Fatalf("entry = %+v, %v", m, ok)
	}
	if m.NgramSafe || m.Projector != nil {
		t.Fatalf("an added entry must start with no speculation qualification and no projector: %+v", m)
	}

	cat01 := liveCatalogGeometry(cfg)()
	if len(cat01) != 1 || cat01[0].Status != preflight.StatusPass {
		t.Fatalf("CAT-01 = %+v, want one PASS for %s", cat01, addID)
	}

	for _, want := range []string{addID, "Q6_K", m.Shards[0].SHA256, "first use", "villa model swap " + addID, "catalog_path", "villa backup"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestCatalogAddRefusesAMovingURL: nothing is pulled and nothing is created.
func TestCatalogAddRefusesAMovingURL(t *testing.T) {
	cfgDir, _, fetches := scratchHost(t)

	code, _, errOut := runAdd(t, "https://huggingface.co/o/r/resolve/main/m.gguf", catalogadd.Overrides{})
	if code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut, "commit") {
		t.Errorf("the refusal must say what a pinned URL is: %q", errOut)
	}
	if *fetches != 0 {
		t.Fatalf("a refused URL was pulled %d time(s)", *fetches)
	}
	if entries, _ := os.ReadDir(cfgDir); len(entries) != 0 {
		t.Fatalf("a refusal left files in the config dir: %v", entries)
	}
}

// TestCatalogAddFailedPullWritesNothing: a failed download leaves no catalog file and
// no catalog_path, so the stack still reads the catalog it read before.
func TestCatalogAddFailedPullWritesNothing(t *testing.T) {
	cfgDir, _, _ := scratchHost(t)
	catalogFetchFn = func(_ context.Context, _, _, _ string) (download.Fetched, error) {
		return download.Fetched{}, os.ErrDeadlineExceeded
	}

	code, _, errOut := runAdd(t, addURL, catalogadd.Overrides{})
	if code != exitBlocked || !strings.Contains(errOut, "pull") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "villa", "catalog-local.json")); !os.IsNotExist(err) {
		t.Fatalf("a catalog file exists after a failed pull: %v", err)
	}
	if cfg, _ := config.LoadVilla(); cfg.CatalogPath != "" {
		t.Fatalf("catalog_path = %q after a failed pull", cfg.CatalogPath)
	}
}

// TestCatalogAddExtendsTheConfiguredCatalog: catalog_path already names a file, so
// that file is extended and the setting is not touched.
func TestCatalogAddExtendsTheConfiguredCatalog(t *testing.T) {
	cfgDir, _, _ := scratchHost(t)
	seed, _ := catalog.Seed()
	curated := filepath.Join(t.TempDir(), "curated.json")
	body := `{"schema_version":` + "5" + `,"catalog_version":"curated","models":[]}`
	if err := os.WriteFile(curated, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveVilla(config.VillaConfig{Model: seed.Models[0].ID, CatalogPath: curated}); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runAdd(t, addURL, catalogadd.Overrides{})
	if code != exitPass {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if cfg, _ := config.LoadVilla(); cfg.CatalogPath != curated {
		t.Fatalf("catalog_path = %q, want the operator's %q", cfg.CatalogPath, curated)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "villa", "catalog-local.json")); !os.IsNotExist(err) {
		t.Fatal("a managed catalog was created beside the configured one")
	}
	c, _, err := catalog.Load(curated)
	if err != nil || len(c.Models) != 1 || c.Models[0].ID != addID {
		t.Fatalf("curated catalog = %v, %v", c, err)
	}
	if strings.Contains(out, "villa backup") && !strings.Contains(out, "not") {
		t.Errorf("a file outside the config dir is not in the backup and the output must say so:\n%s", out)
	}
}

// TestCatalogAddIsIdempotent: the second add of the same URL reports the entry as
// already present and does not pull.
func TestCatalogAddIsIdempotent(t *testing.T) {
	_, _, fetches := scratchHost(t)
	if code, _, e := runAdd(t, addURL, catalogadd.Overrides{}); code != exitPass {
		t.Fatalf("first add: %d %s", code, e)
	}
	code, out, _ := runAdd(t, addURL, catalogadd.Overrides{})
	if code != exitPass || !strings.Contains(out, "already") {
		t.Fatalf("second add: exit %d, out %q", code, out)
	}
	if *fetches != 1 {
		t.Fatalf("pulls = %d, want 1", *fetches)
	}
}

// TestCatalogAddHoldsTheStackLockForTheWrites: the lock is taken and released, and the
// pull happened before it was taken.
func TestCatalogAddHoldsTheStackLockForTheWrites(t *testing.T) {
	scratchHost(t)
	var events []string
	prevLock := acquireStackLock
	t.Cleanup(func() { acquireStackLock = prevLock })
	realFetch := catalogFetchFn
	catalogFetchFn = func(ctx context.Context, url, filename, dir string) (download.Fetched, error) {
		events = append(events, "pull")
		return realFetch(ctx, url, filename, dir)
	}
	acquireStackLock = func() (*stacklock.Lock, error) {
		events = append(events, "lock")
		l, err := prevLock()
		return l, err
	}

	if code, _, e := runAdd(t, addURL, catalogadd.Overrides{}); code != exitPass {
		t.Fatalf("exit %d: %s", code, e)
	}
	if strings.Join(events, ",") != "pull,lock" {
		t.Fatalf("events = %v, want the pull before the lock", events)
	}
	// Released: a second acquire must not block.
	l, err := prevLock()
	if err != nil {
		t.Fatalf("the stack lock was not released: %v", err)
	}
	_ = l.Release()
}

// TestCatalogAddFlagsReachTheEntry: the flags are the operator's override of what the
// file name and header suggest.
func TestCatalogAddFlagsReachTheEntry(t *testing.T) {
	scratchHost(t)
	code, _, e := runAdd(t, addURL, catalogadd.Overrides{ID: "mine", Quant: "Q6_K", DisplayName: "My Heretic", Ctx: 16384})
	if code != exitPass {
		t.Fatalf("exit %d: %s", code, e)
	}
	cfg, _ := config.LoadVilla()
	cat, _, _ := catalog.Load(cfg.CatalogPath)
	m, ok := cat.FindByID("mine")
	if !ok || m.DisplayName != "My Heretic" || m.DefaultCtx != 16384 {
		t.Fatalf("entry = %+v, %v", m, ok)
	}
}

// TestBackupRestoreRoundTripsTheManagedCatalog is ADR-0041's acceptance over the REAL
// cmd-tier wiring: add a model (which writes the managed catalog beside config.toml),
// back up, lose the catalog, restore, and the file is the backed-up bytes again, byte
// for byte, so the restored config's model resolves instead of failing every apply.
func TestBackupRestoreRoundTripsTheManagedCatalog(t *testing.T) {
	scratchVillaHome(t, "backend = \"vulkan\"\n")
	stubPodmanVolume(t, func([]string) (string, error) { return "", nil })
	prev := catalogFetchFn
	t.Cleanup(func() { catalogFetchFn = prev })
	catalogFetchFn = func(_ context.Context, _, filename, dir string) (download.Fetched, error) {
		body := addFixture()
		sum := sha256.Sum256(body)
		return download.Fetched{SHA256: hex.EncodeToString(sum[:]), SizeBytes: uint64(len(body))},
			os.WriteFile(filepath.Join(dir, filename), body, 0o600)
	}
	if code, _, e := runAdd(t, addURL, catalogadd.Overrides{}); code != exitPass {
		t.Fatalf("add: %d %s", code, e)
	}
	managed, err := config.LocalCatalogPath()
	if err != nil {
		t.Fatal(err)
	}
	backedUp, err := os.ReadFile(managed)
	if err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "villa-backup.tar")
	bcmd, bout, berr := newBackupTestCmd()
	if code := runBackup(bcmd, archive, fakeRunDeps(t, nil)); code != exitPass {
		t.Fatalf("runBackup = %d; stderr=%q", code, berr.String())
	}
	if !strings.Contains(bout.String(), "catalog: included") {
		t.Fatalf("the backup must say the catalog is in the archive, got %q", bout.String())
	}

	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	rcmd, out, rerr := newRestoreTestCmd()
	in, _, tmpDir, code := liveRestore(rcmd, archive, true)
	if code != exitPass {
		t.Fatalf("liveRestore = %d; stderr=%q", code, rerr.String())
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	if code, _ := runRestore(rcmd, archive, in, realWriteRestoreDeps(), tmpDir); code != exitPass {
		t.Fatalf("runRestore = %d; stderr=%q", code, rerr.String())
	}
	got, err := os.ReadFile(managed)
	if err != nil || !bytes.Equal(got, backedUp) {
		t.Fatalf("catalog-local.json must be restored byte for byte: %v\n got %s\nwant %s", err, got, backedUp)
	}
	if !strings.Contains(out.String(), "catalog-local.json restored") {
		t.Fatalf("the restore must say it restored the catalog, got %q", out.String())
	}
}
