package orchestrate

// resident.go renders the SECONDARY resident model units: one extra
// villa-llama-<slug>.container per config.Resident entry, so several models stay
// loaded at once instead of the inference unit restarting to swap one for another.
//
// Admission — whether a proposed resident set fits the memory envelope — belongs to
// internal/residentset and is deliberately absent here. This file only turns an
// already-decided set into Quadlet units, and holds no swapping, eviction or
// routing logic of any kind.
//
// Every imperative literal still arrives THROUGH in.Backend.Image()/ContainerArgs()
// and parseContainerArgs, exactly as the primary unit does, so internal/inference
// TestSeamGrepGate stays green and a resident slot can never drift from the primary's
// image, device passthrough or security posture. The only field overridden after the
// parse is the HOST publish port: each container has its own netns, so the
// container-internal port is identical for every slot and only the host side differs.

import (
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// ResidentUnit is one already-resolved secondary resident model. The CALLER resolves
// the catalog model id to a GGUF filename and hands over the resolved descriptor, so
// the pure renderer never imports internal/catalog — the same handoff RenderInput
// already uses for the coding-mode descriptor.
type ResidentUnit struct {
	// Model is the catalog model id. It is the sole source of the unit slug, so two
	// entries with the same id are a rendering conflict, not two slots.
	Model string
	// ModelFile is the GGUF filename inside the bound models dir (catalog-resolved).
	ModelFile string
	// Ctx is this slot's own context length — its single -c.
	Ctx int
	// Port is the HOST loopback port this slot publishes on, taken verbatim from
	// config (never derived from list position).
	Port int
}

// residentSlug turns a catalog model id into the unit-name fragment: lowercased,
// every character outside [a-z0-9-] replaced by '-', runs of '-' collapsed, and the
// result trimmed. Model ids carry dots ("qwen3.6-35b-a3b"), which systemd unit names
// tolerate but which would read as a unit-type suffix, so they are folded away here.
func residentSlug(model string) string {
	out := make([]byte, 0, len(model))
	for _, r := range strings.ToLower(model) {
		if !('a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '-') {
			r = '-'
		}
		if r == '-' && (len(out) == 0 || out[len(out)-1] == '-') {
			continue
		}
		out = append(out, byte(r))
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// ResidentUnitName returns one resident slot's container/DNS name,
// "villa-llama-<slug>", composed from the primary containerName constant so a rename
// of the primary carries through. It is exported because the command tier addresses
// a slot's unit and service by name — to start it, to stop it, and to delete the file
// a removed slot orphans — and must not re-type the composition the renderer uses.
func ResidentUnitName(model string) (string, error) {
	slug := residentSlug(model)
	if slug == "" {
		return "", fmt.Errorf("orchestrate: resident model %q has no usable unit-name characters", model)
	}
	return containerName + "-" + slug, nil
}

// residentContainerNames maps each resident entry to its container/DNS name. It fails
// closed on a model id that slugs to nothing and on two entries that would claim the
// same unit name: either would silently produce a malformed or duplicated unit file
// from a hand-edited config.
func residentContainerNames(resident []ResidentUnit) ([]string, error) {
	names := make([]string, 0, len(resident))
	seen := make(map[string]bool, len(resident))
	for _, r := range resident {
		name, err := ResidentUnitName(r.Model)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, fmt.Errorf("orchestrate: resident models collide on unit name %q — give each slot a distinct model id", name)
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}

// renderResidentUnits renders one .container per resident slot. primaryPublish is the
// primary unit's already-parsed PublishPort, which supplies both the loopback address
// and the container-internal port so neither is re-typed here; only the host port
// field is rewritten per slot.
func renderResidentUnits(tmpl *template.Template, in RenderInput, names []string, primaryPublish, label string) ([]Unit, error) {
	if err := validateResidentPorts(in.Resident, primaryPublish); err != nil {
		return nil, err
	}
	units := make([]Unit, 0, len(in.Resident))
	for i, r := range in.Resident {
		cv, err := parseContainerArgs(in.Backend.Image(), in.Backend.ContainerArgs(inference.RunSpec{
			ContainerName: names[i],
			ModelFile:     r.ModelFile,
			ModelsDir:     in.ModelsDir,
			ContextLen:    r.Ctx,
		}))
		if err != nil {
			return nil, err
		}
		publish, err := withHostPort(primaryPublish, r.Port)
		if err != nil {
			return nil, err
		}
		cv.UnitFileName = names[i] + ".container"
		cv.ContainerName = names[i]
		cv.PublishPort = publish
		cv.BackendLabel = label

		text, err := execTemplate(tmpl, "container.tmpl", cv)
		if err != nil {
			return nil, err
		}
		units = append(units, Unit{Name: cv.UnitFileName, Text: text})
	}
	return units, nil
}

// validateResidentPorts refuses a resident set whose slots cannot all bind. Two slots
// sharing a host port, or a slot claiming the primary's, render units that podman
// starts and immediately kills with an address-in-use error nothing here explains.
// residentset.Admit refuses the same collision as POLICY, but a hand-edited config.toml
// never passes through it and reaches this renderer directly, and CLAUDE.md names a
// hand-edited config as untrusted input that must fail closed with a remediation. This
// guard is structural validity, the same category as the unit-name collision above, not
// a second copy of the admission decision.
func validateResidentPorts(resident []ResidentUnit, primaryPublish string) error {
	primary, err := hostPort(primaryPublish)
	if err != nil {
		return err
	}
	seen := make(map[int]bool, len(resident))
	for _, r := range resident {
		if strconv.Itoa(r.Port) == primary {
			return fmt.Errorf("orchestrate: resident model %q claims host port %d, which the primary inference unit already publishes — give the slot a distinct port", r.Model, r.Port)
		}
		if seen[r.Port] {
			return fmt.Errorf("orchestrate: two resident models claim host port %d — give each slot a distinct port", r.Port)
		}
		seen[r.Port] = true
	}
	return nil
}

// hostPort returns the host-port field of an addr:hostPort:containerPort publish spec.
func hostPort(publish string) (string, error) {
	parts := strings.Split(publish, ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("orchestrate: publish spec %q is not addr:hostPort:containerPort", publish)
	}
	return parts[1], nil
}

// withHostPort rewrites the host-port field of an addr:hostPort:containerPort publish
// spec, leaving the loopback address and the container-internal port exactly as the
// backend seam emitted them.
func withHostPort(publish string, port int) (string, error) {
	parts := strings.Split(publish, ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("orchestrate: publish spec %q is not addr:hostPort:containerPort", publish)
	}
	parts[1] = strconv.Itoa(port)
	return strings.Join(parts, ":"), nil
}
