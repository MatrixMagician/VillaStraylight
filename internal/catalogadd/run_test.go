package catalogadd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

const goodURL = "https://huggingface.co/Youssofal/Heretic-GGUF/resolve/" + commit + "/Q6_K/Heretic-35B-A3B-Q6_K.gguf"

// host is an in-memory stand-in for everything Run touches: the catalog files, the
// config's catalog_path, the models dir and the stack lock. Every effect is recorded
// in calls, in order, so a test can assert both what happened and that nothing else did.
type host struct {
	t          *testing.T
	configured string
	defaultP   string
	files      map[string][]byte
	loads      func(path string, n int) (catalog.Catalog, bool, error) // optional override
	loadCount  map[string]int
	fetch      func(url, filename string) (FileFacts, error)
	header     func(filename string) (gguf.Header, error)
	lockErr    error
	writeErr   error
	setErr     error
	calls      []string
	locked     bool
}

func newHost(t *testing.T) *host {
	t.Helper()
	h := &host{t: t, defaultP: "/cfg/villa/catalog-local.json", files: map[string][]byte{}, loadCount: map[string]int{}}
	h.fetch = func(string, string) (FileFacts, error) { return testFacts(), nil }
	h.header = func(string) (gguf.Header, error) { return hybridHeader(t, nil), nil }
	return h
}

func (h *host) deps() Deps {
	return Deps{
		ConfiguredPath: func() (string, error) { return h.configured, nil },
		DefaultPath:    func() (string, error) { return h.defaultP, nil },
		LoadFile: func(path string) (catalog.Catalog, error) {
			h.loadCount[path]++
			if h.loads != nil {
				if c, handled, err := h.loads(path, h.loadCount[path]); handled {
					return c, err
				}
			}
			b, ok := h.files[path]
			if !ok {
				return catalog.Catalog{}, fmt.Errorf("open %s: %w", path, fs.ErrNotExist)
			}
			dir := h.t.TempDir()
			p := filepath.Join(dir, "c.json")
			if err := os.WriteFile(p, b, 0o600); err != nil {
				h.t.Fatal(err)
			}
			return catalog.LoadFile(p)
		},
		Seed: catalog.Seed,
		Fetch: func(_ context.Context, url, filename string) (FileFacts, error) {
			h.calls = append(h.calls, "fetch")
			return h.fetch(url, filename)
		},
		ReadHeader: func(filename string) (gguf.Header, error) {
			h.calls = append(h.calls, "header")
			return h.header(filename)
		},
		Lock: func() (func(), error) {
			if h.lockErr != nil {
				return nil, h.lockErr
			}
			h.calls = append(h.calls, "lock")
			h.locked = true
			return func() { h.locked = false; h.calls = append(h.calls, "unlock") }, nil
		},
		WriteFile: func(path string, data []byte) error {
			if !h.locked {
				h.t.Error("catalog file written without the stack lock")
			}
			if h.writeErr != nil {
				return h.writeErr
			}
			h.calls = append(h.calls, "write "+path)
			h.files[path] = data
			return nil
		},
		SetCatalogPath: func(path string) error {
			if !h.locked {
				h.t.Error("config written without the stack lock")
			}
			if h.setErr != nil {
				return h.setErr
			}
			h.calls = append(h.calls, "set "+path)
			h.configured = path
			return nil
		},
	}
}

// loaded parses what Run wrote at path with the real loader, which is the check that
// matters: Load must accept the file with no warning.
func (h *host) loaded(path string) catalog.Catalog {
	h.t.Helper()
	b, ok := h.files[path]
	if !ok {
		h.t.Fatalf("no file written at %s (calls %v)", path, h.calls)
	}
	p := filepath.Join(h.t.TempDir(), "c.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		h.t.Fatal(err)
	}
	c, warnings, err := catalog.Load(p)
	if err != nil || len(warnings) != 0 {
		h.t.Fatalf("catalog.Load of the written file: %v, warnings %v", err, warnings)
	}
	return c
}

func (h *host) nothingChanged() {
	h.t.Helper()
	if len(h.files) != 0 {
		h.t.Errorf("files written: %v", h.files)
	}
	for _, c := range h.calls {
		if strings.HasPrefix(c, "write") || strings.HasPrefix(c, "set") {
			h.t.Errorf("effect %q after a refusal", c)
		}
	}
	if h.configured != "" {
		h.t.Errorf("catalog_path changed to %q", h.configured)
	}
}

// TestRunOnAFreshHost: with no catalog_path and no file, the file created is the
// embedded seed plus the new entry (an external catalog REPLACES the seed, so a file
// holding only the new entry would drop every model the host runs), catalog_path is
// set to it, and the stack lock is held around exactly the two writes.
func TestRunOnAFreshHost(t *testing.T) {
	h := newHost(t)
	res, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Outcome != Added || !res.Created || !res.SetPath || !res.Managed || res.Path != h.defaultP {
		t.Fatalf("Result = %+v", res)
	}
	want := []string{"fetch", "header", "lock", "write " + h.defaultP, "set " + h.defaultP, "unlock"}
	if !slices.Equal(h.calls, want) {
		t.Fatalf("calls = %v\nwant    %v", h.calls, want)
	}

	got := h.loaded(h.defaultP)
	seed, _ := catalog.Seed()
	if len(got.Models) != len(seed.Models)+1 {
		t.Fatalf("file has %d entries, want the seed's %d plus one", len(got.Models), len(seed.Models))
	}
	for i, m := range seed.Models {
		if catalog.OriginOf(got.Models[i]) != catalog.OriginSeed || got.Models[i].ID != m.ID {
			t.Fatalf("seed entry %q was not carried through unchanged", m.ID)
		}
	}
	added, ok := got.FindByID("heretic-35b-a3b-q6_k")
	if !ok || catalog.OriginOf(added) != catalog.OriginOperator {
		t.Fatalf("added entry = %+v, %v", added, ok)
	}
	if added.NgramSafe || added.Projector != nil || added.UnifiedMemorySafe {
		t.Fatalf("the entry must start unqualified, text-only and not auto-selectable: %+v", added)
	}
	if got.CatalogVersion != seed.CatalogVersion {
		t.Fatalf("catalog_version = %q, want the seed's %q", got.CatalogVersion, seed.CatalogVersion)
	}
}

// TestRunExtendsTheConfiguredCatalog: an existing catalog_path is extended in place
// and the setting is never replaced, even by a path villa would have chosen itself.
func TestRunExtendsTheConfiguredCatalog(t *testing.T) {
	h := newHost(t)
	h.configured = "/home/op/curated.json"
	seed, _ := catalog.Seed()
	curated := catalog.Catalog{SchemaVersion: catalog.SupportedSchema, CatalogVersion: "curated-1", Models: seed.Models[:2]}
	b, err := encode(curated)
	if err != nil {
		t.Fatal(err)
	}
	h.files[h.configured] = b

	res, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Created || res.SetPath || res.Managed || res.Path != h.configured {
		t.Fatalf("Result = %+v", res)
	}
	if h.configured != "/home/op/curated.json" {
		t.Fatalf("catalog_path replaced with %q", h.configured)
	}
	if _, ok := h.files[h.defaultP]; ok {
		t.Fatal("a second catalog file was created beside the configured one")
	}
	got := h.loaded(h.configured)
	if len(got.Models) != 3 || got.CatalogVersion != "curated-1" {
		t.Fatalf("extended file = %v version %q; the operator's curated subset must stay a subset", ids(got), got.CatalogVersion)
	}
	for _, c := range h.calls {
		if strings.HasPrefix(c, "set ") {
			t.Fatalf("config written although catalog_path was already set: %v", h.calls)
		}
	}
}

// TestRunExtendsTheManagedFileWhenConfigWasReset: no catalog_path but the managed
// file exists (the config was reset or restored without it), so the file is extended,
// not recreated from the seed, and catalog_path is set back to it.
func TestRunExtendsTheManagedFileWhenConfigWasReset(t *testing.T) {
	h := newHost(t)
	seed, _ := catalog.Seed()
	earlier := newModel("earlier", "https://huggingface.co/o/e/resolve/"+commit+"/e.gguf", "e.gguf", shaB)
	b, _ := encode(catalog.Catalog{SchemaVersion: catalog.SupportedSchema, CatalogVersion: seed.CatalogVersion, Models: append(slices.Clone(seed.Models), earlier)})
	h.files[h.defaultP] = b

	res, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || !res.SetPath {
		t.Fatalf("Result = %+v", res)
	}
	got := h.loaded(h.defaultP)
	if _, ok := got.FindByID("earlier"); !ok {
		t.Fatal("an earlier addition was lost when the managed file was extended")
	}
}

// TestRunRefusesAnUnpinnedURLBeforeAnyEffect: the headline refusal reaches no seam at
// all, which is the proof that no request is made for a moving target.
func TestRunRefusesAnUnpinnedURLBeforeAnyEffect(t *testing.T) {
	h := newHost(t)
	_, err := Run(t.Context(), h.deps(), Opts{URL: "https://huggingface.co/o/r/resolve/main/m.gguf"})
	if !errors.Is(err, ErrNotPinned) {
		t.Fatalf("Run = %v, want ErrNotPinned", err)
	}
	h.nothingChanged()
	if len(h.calls) != 0 || len(h.loadCount) != 0 {
		t.Fatalf("a refused URL touched the host: calls %v loads %v", h.calls, h.loadCount)
	}
}

// TestRunRefusesAnUnusableConfiguredCatalog: catalog.Load would fall back to the seed
// here, and a writer that did the same would replace the operator's file. Run reads
// through LoadFile and stops before the download.
func TestRunRefusesAnUnusableConfiguredCatalog(t *testing.T) {
	tests := []struct {
		name string
		file []byte // nil means the file does not exist
	}{
		{"missing", nil},
		{"malformed", []byte("{nope")},
		{"older schema", []byte(`{"schema_version":3,"catalog_version":"x","models":[]}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t)
			h.configured = "/home/op/curated.json"
			if tt.file != nil {
				h.files[h.configured] = tt.file
			}
			before := cloneFiles(h.files)

			_, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
			if err == nil || !strings.Contains(err.Error(), h.configured) {
				t.Fatalf("Run = %v, want a refusal naming %s", err, h.configured)
			}
			if len(h.calls) != 0 {
				t.Fatalf("a refused catalog still reached %v", h.calls)
			}
			if fmt.Sprint(before) != fmt.Sprint(h.files) {
				t.Fatal("the operator's file was modified")
			}
		})
	}
}

func cloneFiles(m map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range m {
		out[k] = slices.Clone(v)
	}
	return out
}

// TestRunWritesNothingWhenTheDownloadOrHeaderFails: every failure between the pull and
// the lock leaves the catalog, the config and the lock untouched.
func TestRunWritesNothingWhenTheDownloadOrHeaderFails(t *testing.T) {
	boom := errors.New("connection reset")
	tests := []struct {
		name  string
		setup func(*host)
		want  string
	}{
		{"download fails", func(h *host) { h.fetch = func(string, string) (FileFacts, error) { return FileFacts{}, boom } }, "connection reset"},
		{"hash disagrees with the server", func(h *host) {
			h.fetch = func(string, string) (FileFacts, error) {
				return FileFacts{}, errors.New("upstream X-Linked-Etag differs from the received sha256")
			}
		}, "X-Linked-Etag"},
		{"header unreadable", func(h *host) {
			h.header = func(string) (gguf.Header, error) { return gguf.Header{}, errors.New("not a GGUF file") }
		}, "GGUF"},
		{"header cannot be sized", func(h *host) {
			h.header = func(string) (gguf.Header, error) {
				return gguf.ReadHeader(strings.NewReader(string(gguf.FixtureForTest("x", map[string]uint64{"x.block_count": 2}))))
			}
		}, "x.attention"},
		{"unusable digest", func(h *host) {
			h.fetch = func(string, string) (FileFacts, error) { return FileFacts{SHA256: "nope", SizeBytes: 1}, nil }
		}, "sha256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t)
			tt.setup(h)
			_, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run = %v, want an error mentioning %q", err, tt.want)
			}
			h.nothingChanged()
			if slices.Contains(h.calls, "lock") {
				t.Fatal("the stack lock was taken for a run that had nothing to write")
			}
		})
	}
}

// TestRunReAdd: the same source is a no-op reported as already present (and nothing is
// downloaded), a different source under the same id refuses before the download, and a
// different digest for the same source refuses at the write.
func TestRunReAdd(t *testing.T) {
	first := newHost(t)
	if _, err := Run(t.Context(), first.deps(), Opts{URL: goodURL}); err != nil {
		t.Fatal(err)
	}
	written := first.files[first.defaultP]

	t.Run("same source", func(t *testing.T) {
		h := newHost(t)
		h.configured = h.defaultP
		h.files[h.defaultP] = slices.Clone(written)
		res, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
		if err != nil || res.Outcome != AlreadyPresent {
			t.Fatalf("Run = %+v, %v", res, err)
		}
		if len(h.calls) != 0 {
			t.Fatalf("an already-present model reached %v; it must cost no download", h.calls)
		}
	})

	t.Run("same id, different source", func(t *testing.T) {
		h := newHost(t)
		h.configured = h.defaultP
		h.files[h.defaultP] = slices.Clone(written)
		other := "https://huggingface.co/Someone/Else-GGUF/resolve/" + commit + "/Q6_K/Heretic-35B-A3B-Q6_K.gguf"
		_, err := Run(t.Context(), h.deps(), Opts{URL: other, Overrides: Overrides{ID: "heretic-35b-a3b-q6_k"}})
		if !errors.Is(err, ErrIDTaken) {
			t.Fatalf("Run = %v, want ErrIDTaken", err)
		}
		if len(h.calls) != 0 {
			t.Fatalf("refusal came after %v; it must cost no download", h.calls)
		}
	})

	t.Run("same source, different digest", func(t *testing.T) {
		h := newHost(t)
		h.configured = h.defaultP
		h.files[h.defaultP] = slices.Clone(written)
		// The pre-check sees the entry as present, so force the digest comparison by
		// presenting the file as lacking it until the lock is held (a concurrent writer).
		var n int
		h.loads = func(path string, call int) (catalog.Catalog, bool, error) {
			n = call
			if call == 1 {
				seed, _ := catalog.Seed()
				return catalog.Catalog{SchemaVersion: catalog.SupportedSchema, CatalogVersion: seed.CatalogVersion, Models: seed.Models}, true, nil
			}
			return catalog.Catalog{}, false, nil
		}
		h.fetch = func(string, string) (FileFacts, error) {
			return FileFacts{SHA256: strings.Repeat("c", 64), SizeBytes: testSize}, nil
		}
		before := cloneFiles(h.files)
		_, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
		if !errors.Is(err, ErrShaMismatch) {
			t.Fatalf("Run = %v (loads %d), want ErrShaMismatch", err, n)
		}
		if fmt.Sprint(before) != fmt.Sprint(h.files) {
			t.Fatal("the file changed on a refused re-add")
		}
	})
}

// TestRunFailureOrder: the catalog file is written before catalog_path is set (a
// config naming a file that does not exist is worse than a file nothing names), and a
// failed write or a failed lock sets nothing.
func TestRunFailureOrder(t *testing.T) {
	t.Run("write fails", func(t *testing.T) {
		h := newHost(t)
		h.writeErr = errors.New("disk full")
		_, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
		if err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("Run = %v", err)
		}
		h.nothingChanged()
		if h.locked {
			t.Fatal("the stack lock was not released after a failed write")
		}
	})
	t.Run("lock fails", func(t *testing.T) {
		h := newHost(t)
		h.lockErr = errors.New("another stack mutation is in progress")
		if _, err := Run(t.Context(), h.deps(), Opts{URL: goodURL}); err == nil {
			t.Fatal("Run proceeded without the stack lock")
		}
		h.nothingChanged()
	})
	t.Run("config write fails after the file is written", func(t *testing.T) {
		h := newHost(t)
		h.setErr = errors.New("read-only config")
		_, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
		if err == nil || !strings.Contains(err.Error(), "catalog_path") || !strings.Contains(err.Error(), h.defaultP) {
			t.Fatalf("Run = %v, want an error naming catalog_path and the file to point it at", err)
		}
		if _, ok := h.files[h.defaultP]; !ok {
			t.Fatal("the catalog file is the durable half and must remain")
		}
		if h.locked {
			t.Fatal("the stack lock was not released")
		}
	})
}

// TestRunReportsWhatItDerived: the notes carry the default_ctx cap, so the operator
// sees the one value that is policy and not measurement.
func TestRunReportsWhatItDerived(t *testing.T) {
	h := newHost(t)
	res, err := Run(t.Context(), h.deps(), Opts{URL: goodURL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "default_ctx") {
		t.Fatalf("notes = %v", res.Notes)
	}
	if res.Model.Shards[0].SHA256 != testSHA {
		t.Fatalf("Result.Model = %+v", res.Model)
	}
}
