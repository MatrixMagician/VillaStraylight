package orchestrate

// statevolume_drift_test.go binds internal/subsystem's persistent-state declaration
// to the units this package actually renders.
//
// The declaration lives in internal/subsystem because it is a property of a
// subsystem, and it re-declares the volume names rather than reading them from here
// because the import only runs one way: orchestrate imports subsystem, never the
// reverse. Two declarations of one fact is exactly the drift a cross-package test
// exists to catch — the same shape as the embed-GGUF-filename drift test.
//
// It asserts against the RENDERED unit text, not against the consts, so the test
// fails if the mount ever moves to a different volume even while the const keeps its
// name. What is snapshotted must be what is mounted.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// mountedVolumeNames extracts the podman NAMED volumes a rendered unit mounts,
// paired with whether the mount is read-only.
//
// A Quadlet Volume= line is `<source>:<dest>[:opts]`. A named-volume source is
// either the bare volume name or the `<name>.volume` unit reference; a host path
// starts with `/` or `%h` and is not a named volume at all. The read-only flag is
// the `ro` option, which is what separates state a subsystem OWNS from a store it
// merely reads.
func mountedVolumeNames(unitText string) map[string]bool {
	re := regexp.MustCompile(`(?m)^Volume=(.+)$`)
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(unitText, -1) {
		parts := strings.Split(strings.TrimSpace(m[1]), ":")
		if len(parts) < 2 {
			continue
		}
		src := parts[0]
		if strings.HasPrefix(src, "/") || strings.HasPrefix(src, "%h") || strings.HasPrefix(src, "~") {
			// A host bind mount, not a podman named volume.
			continue
		}
		src = strings.TrimSuffix(src, ".volume")
		readOnly := false
		for _, opt := range parts[2:] {
			for _, o := range strings.Split(opt, ",") {
				if o == "ro" {
					readOnly = true
				}
			}
		}
		out[src] = readOnly
	}
	return out
}

// statefulFixtureInput renders the whole stack with every stateful subsystem on, so
// the walk sees the units chat and memory actually run.
func statefulFixtureInput() RenderInput {
	in := memoryFixtureInput()
	in.Cfg.WebSearchEnabled = true
	return in
}

// TestStateVolumeDeclarationMatchesTheRenderedMounts: every volume
// subsystem.StateVolume names is genuinely mounted READ-WRITE by that subsystem's
// rendered units.
//
// Without this the declaration is a claim about the units that nothing checks, and
// the snapshot lifecycle would export a volume nothing writes to — a safety net that
// only fails when used.
func TestStateVolumeDeclarationMatchesTheRenderedMounts(t *testing.T) {
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// The units each stateful subsystem's state lives in. Chat is Open WebUI;
	// memory's data is Qdrant's storage (the embedder holds none of its own).
	unitsFor := map[subsystem.Kind][]string{
		subsystem.Chat:   {"villa-openwebui.container"},
		subsystem.Memory: {"villa-qdrant.container"},
	}

	for _, k := range subsystem.Stateful() {
		want, _ := k.StateVolume()
		names, ok := unitsFor[k]
		if !ok {
			t.Fatalf("%v owns persistent state but this test names no unit for it — a new stateful subsystem must be added here", k)
		}
		found := false
		for _, name := range names {
			u := unitByName(t, units, name)
			for vol, readOnly := range mountedVolumeNames(u.Text) {
				if vol != want {
					continue
				}
				if readOnly {
					t.Errorf("%v declares %q as owned state but %s mounts it READ-ONLY", k, want, name)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("%v declares %q as owned state, but no unit in %v mounts a volume by that name — the declaration has drifted from what is rendered", k, want, names)
		}
	}
}

// TestNoStatelessSubsystemMountsAWritableVolume is the other half of the drift
// check: a subsystem that declares NO owned state must not be quietly writing to a
// named volume.
//
// This is the case that would make the declaration dangerous rather than merely
// wrong — an unsnapshotted writable volume is data an update can migrate forward
// with no rollback target at all. villa-embed and villa-llama mount villa-models
// read-only, and that must stay read-only.
func TestNoStatelessSubsystemMountsAWritableVolume(t *testing.T) {
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	statelessUnits := map[subsystem.Kind][]string{
		subsystem.Inference: {"villa-llama.container"},
		subsystem.WebSearch: {"villa-searxng.container", "villa-websafe.container"},
	}
	// The embedder moves with memory but holds no state of its own: its only mount
	// is the read-only model store.
	statelessUnits[subsystem.Memory] = append(statelessUnits[subsystem.Memory], "villa-embed.container")

	for k, names := range statelessUnits {
		owned, _ := k.StateVolume()
		for _, name := range names {
			u := unitByName(t, units, name)
			for vol, readOnly := range mountedVolumeNames(u.Text) {
				if readOnly || vol == owned {
					continue
				}
				t.Errorf("%s mounts %q READ-WRITE, but %v does not declare it as owned state — an update could migrate it forward with nothing to roll back to", name, vol, k)
			}
		}
	}
}

// TestModelsVolumeIsNeverOwnedState: the shared model store is mounted read-only
// everywhere it appears, and no subsystem may declare it.
//
// Model weights are explicitly out of scope for `update`, and snapshotting them
// would copy tens of gigabytes per update. This asserts the rendered reality rather
// than trusting the declaration, so a unit that dropped `ro` would fail here.
func TestModelsVolumeIsNeverOwnedState(t *testing.T) {
	units, err := Render(statefulFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	const models = "villa-models"
	for _, u := range units {
		if !strings.HasSuffix(u.Name, ".container") {
			continue
		}
		if readOnly, ok := mountedVolumeNames(u.Text)[models]; ok && !readOnly {
			t.Errorf("%s mounts %q read-write; the shared model store must stay read-only", u.Name, models)
		}
	}
	for _, k := range subsystem.Every {
		if vol, owns := k.StateVolume(); owns && vol == models {
			t.Errorf("%v declares the shared model store as owned state; update never touches model weights", k)
		}
	}
}
