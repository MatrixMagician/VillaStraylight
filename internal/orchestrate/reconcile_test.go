package orchestrate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeUnitsToDir is a test helper that seeds unitDir with the given units' text
// on disk (the "previously rendered" state Reconcile diffs against).
func writeUnitsToDir(t *testing.T, dir string, units []Unit) {
	t.Helper()
	for _, u := range units {
		if err := os.WriteFile(filepath.Join(dir, u.Name), []byte(u.Text), 0o644); err != nil {
			t.Fatalf("seed %s: %v", u.Name, err)
		}
	}
}

// TestReconcileNoOpAndDiff (table): identical on-disk units → Plan.Changed empty;
// a flipped model → Plan.Changed == exactly the container unit (core).
func TestReconcileNoOpAndDiff(t *testing.T) {
	base, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render base: %v", err)
	}

	changedInput := fixtureInput()
	changedInput.ModelFile = "deepseek-r1-distill-llama-70b.gguf"
	changedInput.Cfg.Model = "deepseek-r1-distill-llama-70b"
	flipped, err := Render(changedInput)
	if err != nil {
		t.Fatalf("Render flipped: %v", err)
	}

	tests := []struct {
		name        string
		onDisk      []Unit
		want        []Unit
		wantChanged []string // unit names expected in Plan.Changed
	}{
		{
			name:        "identical config is a true no-op",
			onDisk:      base,
			want:        base,
			wantChanged: nil,
		},
		{
			name:        "flipped model changes exactly the inference unit",
			onDisk:      base,
			want:        flipped,
			wantChanged: []string{"villa-llama.container"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeUnitsToDir(t, dir, tc.onDisk)

			plan, err := Reconcile(tc.want, dir)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}

			gotChanged := make([]string, 0, len(plan.Changed))
			for _, u := range plan.Changed {
				gotChanged = append(gotChanged, u.Name)
			}

			if len(gotChanged) != len(tc.wantChanged) {
				t.Fatalf("Changed = %v, want %v", gotChanged, tc.wantChanged)
			}
			for i, name := range tc.wantChanged {
				if gotChanged[i] != name {
					t.Errorf("Changed[%d] = %q, want %q (full: %v)", i, gotChanged[i], name, gotChanged)
				}
			}

			// Unchanged + Changed must partition the rendered set.
			if got, want := len(plan.Changed)+len(plan.Unchanged), len(tc.want); got != want {
				t.Errorf("Changed(%d)+Unchanged(%d) = %d, want full set %d",
					len(plan.Changed), len(plan.Unchanged), got, want)
			}
		})
	}
}

// TestAtomicUnitWrite: after WriteUnits no *.tmp remains in unitDir and the target
// content equals the rendered text; writing into a path outside unitDir errors.
func TestAtomicUnitWrite(t *testing.T) {
	dir := t.TempDir()
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	plan := Plan{Changed: units}
	if err := WriteUnits(plan, dir); err != nil {
		t.Fatalf("WriteUnits: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file after WriteUnits: %s", e.Name())
		}
	}

	for _, u := range units {
		got, err := os.ReadFile(filepath.Join(dir, u.Name))
		if err != nil {
			t.Fatalf("read written %s: %v", u.Name, err)
		}
		if string(got) != u.Text {
			t.Errorf("written %s does not match rendered text", u.Name)
		}
	}

	// A unit name that escapes the unit dir must be refused (path-traversal guard).
	escaping := Plan{Changed: []Unit{{Name: filepath.Join("..", "escape.container"), Text: "x"}}}
	if err := WriteUnits(escaping, dir); err == nil {
		t.Errorf("WriteUnits accepted a path outside unitDir; expected refusal")
	}
}

// TestReconcileNamesTheRegistryUnitsItNoLongerRenders (#330): a registry unit on disk
// that the render no longer produces is Removed, carrying its on-disk bytes, while a
// unit villa never declared (the operator's own, a resident slot) is never touched.
func TestReconcileNamesTheRegistryUnitsItNoLongerRenders(t *testing.T) {
	on, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render on: %v", err)
	}
	offInput := statefulFixtureInput()
	offInput.Cfg.WebSearchEnabled = false
	off, err := Render(offInput)
	if err != nil {
		t.Fatalf("Render off: %v", err)
	}

	dir := t.TempDir()
	writeUnitsToDir(t, dir, on)
	writeUnitsToDir(t, dir, []Unit{
		{Name: "villa-llama-qwen3-8b.container", Text: "resident slot"},
		{Name: "my-own.container", Text: "operator's"},
	})

	plan, err := Reconcile(off, dir)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	want := []string{"villa-searxng.container", "villa-websafe.container"}
	if got := unitNames(plan.Removed); !slices.Equal(got, want) {
		t.Fatalf("Removed = %v, want %v", got, want)
	}
	onDisk := map[string]string{}
	for _, u := range on {
		onDisk[u.Name] = u.Text
	}
	for _, u := range plan.Removed {
		if u.Text != onDisk[u.Name] {
			t.Errorf("Removed %s carries %q, want its on-disk bytes", u.Name, u.Text)
		}
	}
}

// TestReconcileRemovesNothingWhenEveryRegistryUnitIsRendered: the steady state has no
// removal set, and an absent registry unit is not one either.
func TestReconcileRemovesNothingWhenEveryRegistryUnitIsRendered(t *testing.T) {
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	dir := t.TempDir()
	writeUnitsToDir(t, dir, units)
	plan, err := Reconcile(units, dir)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(plan.Removed) != 0 {
		t.Errorf("Removed = %v, want none", unitNames(plan.Removed))
	}
}

// TestOrphansFailsOnAnUnreadableUnit: a read error other than absence is an error,
// never a silent "not an orphan".
func TestOrphansFailsOnAnUnreadableUnit(t *testing.T) {
	_, err := Orphans(nil, func(string) ([]byte, error) { return nil, os.ErrPermission })
	if err == nil {
		t.Fatal("Orphans swallowed a permission error")
	}
}

// TestRemoveUnitsRemovesTheRemovedSet: every Removed unit's file goes, an already
// absent one is the goal state, Changed is never touched, and a name resolving
// outside the unit dir is refused.
func TestRemoveUnitsRemovesTheRemovedSet(t *testing.T) {
	dir := t.TempDir()
	writeUnitsToDir(t, dir, []Unit{
		{Name: "villa-searxng.container", Text: "x"},
		{Name: "villa-llama.container", Text: "kept"},
	})
	plan := Plan{
		Changed: []Unit{{Name: "villa-llama.container"}},
		Removed: []Unit{{Name: "villa-searxng.container"}, {Name: "villa-websafe.container"}},
	}
	if err := RemoveUnits(plan, dir); err != nil {
		t.Fatalf("RemoveUnits: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "villa-searxng.container")); !os.IsNotExist(err) {
		t.Errorf("villa-searxng.container still on disk (stat err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "villa-llama.container")); err != nil {
		t.Errorf("a Changed unit was removed: %v", err)
	}
	escaping := Plan{Removed: []Unit{{Name: filepath.Join("..", "escape.container")}}}
	if err := RemoveUnits(escaping, dir); err == nil {
		t.Error("RemoveUnits accepted a path outside the unit dir")
	}
}
