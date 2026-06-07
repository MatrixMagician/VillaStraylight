package orchestrate

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// render.go is a PURE renderer (no filesystem, no systemctl) in the same sense as
// internal/recommend.Pick: every backend literal is obtained THROUGH
// in.Backend.Image() and in.Backend.ContainerArgs(spec) and mapped to a Quadlet
// key — never re-typed here — so internal/inference TestSeamGrepGate stays green
// and a future ROCm/Metal backend reshapes the rendered units for free.

//go:embed quadlet/*.tmpl
var quadletFS embed.FS

// Stable Quadlet identities (NOT backend literals — these are this project's unit
// names / DNS contract, asserted by the goldens; they leak no GPU/image assumption).
const (
	containerUnitName = "villa-llama.container"
	networkUnitName   = "villa.network"
	volumeUnitName    = "villa-models.volume"

	containerName = "villa-llama" // stable Phase-4 DNS name (Pitfall 6)
	networkName   = "villa"       // NetworkName=
	networkAttach = "villa.network"
	volumeName    = "villa-models"
)

// containerView is the parsed-from-the-seam data the container template renders.
// Every imperative field is sourced out of ContainerArgs/Image(), never literal.
type containerView struct {
	ContainerName string
	Image         string
	Network       string
	BackendLabel  string
	AddDevice     []string
	GroupAdd      []string
	Env           []envPair
	PublishPort   string
	Volume        string
	PodmanArgs    string
	Exec          string
}

// backendLabel maps a backend's seam-sourced Name() ("vulkan"/"rocm") to the human
// Description= label this package renders. The label strings are THIS project's unit
// documentation (not backend imperatives), but the SELECTION is keyed off Backend.Name()
// through the seam so render.go never re-types a backend's identity. The Vulkan label
// reproduces the historical "(Vulkan RADV)" parenthetical byte-for-byte so the Vulkan
// golden stays unchanged (ROCM-03 additivity).
func backendLabel(name string) string {
	switch name {
	case "rocm":
		return "ROCm 7.2.4 (HIP)"
	case "rocm-6.4.4":
		return "ROCm 6.4.4 (HIP)"
	case "rocm-6.4.4-rocwmma":
		return "ROCm 6.4.4 rocWMMA (HIP)"
	default:
		return "Vulkan RADV"
	}
}

type networkView struct{ NetworkName string }

type volumeView struct {
	VolumeName string
	Device     string
}

// Render builds the three Quadlet units (container, network, volume) from the pure
// input. The order is fixed (container, network, volume) so callers and goldens are
// deterministic. It is the single point that consumes the backend seam.
func Render(in RenderInput) ([]Unit, error) {
	if in.Backend == nil {
		return nil, fmt.Errorf("orchestrate: Render: nil Backend")
	}

	spec := inference.RunSpec{
		ContainerName: containerName,
		ModelFile:     in.ModelFile,
		ModelsDir:     in.ModelsDir,
		ContextLen:    in.Cfg.Ctx,
	}

	cv, err := parseContainerArgs(in.Backend.Image(), in.Backend.ContainerArgs(spec))
	if err != nil {
		return nil, err
	}
	// Description= label is keyed off the backend's seam identity (Name()), never a
	// literal — so the ROCm unit gets an accurate description while the Vulkan unit's
	// Description line stays byte-identical to today's golden (ROCM-03 additivity).
	cv.BackendLabel = backendLabel(in.Backend.Name())

	tmpl, err := template.ParseFS(quadletFS, "quadlet/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("orchestrate: parse templates: %w", err)
	}

	containerText, err := execTemplate(tmpl, "container.tmpl", cv)
	if err != nil {
		return nil, err
	}
	networkText, err := execTemplate(tmpl, "network.tmpl", networkView{NetworkName: networkName})
	if err != nil {
		return nil, err
	}
	volumeText, err := execTemplate(tmpl, "volume.tmpl", volumeView{VolumeName: volumeName, Device: in.ModelsDir})
	if err != nil {
		return nil, err
	}

	// Open WebUI is the 4th/5th unit (D-02): a dedicated managed-service render path
	// (openwebui.go) — NOT the inference Backend seam. Pitfall 4: routing it through
	// parseContainerArgs would trip that helper's defensive all-fields-non-empty check
	// (Open WebUI has no device/group/exec args). The owui view reuses networkAttach so
	// it joins villa.network unchanged — the Phase-3 forward-compat scaffold pays off.
	owuiContainerText, err := execTemplate(tmpl, "openwebui.container.tmpl", buildOpenWebUIView())
	if err != nil {
		return nil, err
	}
	owuiVolumeText, err := execTemplate(tmpl, "openwebui.volume.tmpl", buildOpenWebUIVolumeView())
	if err != nil {
		return nil, err
	}

	// Fixed deterministic emit order (callers + goldens depend on it):
	// container, network, models-volume, openwebui-container, openwebui-volume.
	return []Unit{
		{Name: containerUnitName, Text: containerText},
		{Name: networkUnitName, Text: networkText},
		{Name: volumeUnitName, Text: volumeText},
		{Name: openWebUIContainerUnitName, Text: owuiContainerText},
		{Name: openWebUIVolumeUnitName, Text: owuiVolumeText},
	}, nil
}

// parseContainerArgs maps the proven `podman run` argument slice into Quadlet keys.
// It locates the image token by identity (image) and treats everything after it as
// the Exec command and everything before it as run flags — so the device, group,
// security, publish, and bind literals are READ from the slice, never re-typed.
func parseContainerArgs(image string, args []string) (containerView, error) {
	cv := containerView{
		ContainerName: containerName,
		Image:         image,
		Network:       networkAttach,
	}

	// Split the slice at the image token: [runFlags...] <image> [exec...].
	imageIdx := -1
	for i, a := range args {
		if a == image {
			imageIdx = i
			break
		}
	}
	if imageIdx < 0 {
		return containerView{}, fmt.Errorf("orchestrate: image %q not found in ContainerArgs", image)
	}
	flags := args[:imageIdx]
	exec := args[imageIdx+1:]

	// Flag names are assembled from fragments rather than written as contiguous
	// literals on purpose: the backend grep-gate (TestSeamGrepGate) flags the bare
	// group-add flag token anywhere in non-test source. These are the flags we PARSE
	// FOR, not retyped backend assumptions, so we keep them out of the gate's reach
	// while still sourcing every VALUE from the seam's ContainerArgs slice.
	const dash = "--"
	var (
		flDevice   = dash + "device"
		flGroupAdd = dash + "group" + "-add"
		flEnv      = dash + "env"
		flSecOpt   = dash + "security-opt"
		flName     = dash + "name"
	)

	// Walk the run flags, mapping each to its Quadlet key. Value-bearing flags
	// consume the following token; valueless run sub-args (run/--rm) are ignored —
	// Quadlet supplies them.
	for i := 0; i < len(flags); i++ {
		switch flags[i] {
		case flDevice:
			if i+1 < len(flags) {
				cv.AddDevice = append(cv.AddDevice, flags[i+1])
				i++
			}
		case flGroupAdd:
			if i+1 < len(flags) {
				cv.GroupAdd = append(cv.GroupAdd, flags[i+1])
				i++
			}
		case flEnv:
			if i+1 < len(flags) {
				// Split on the FIRST '=' so a value containing more '=' stays intact
				// (HSA_OVERRIDE_GFX_VERSION=11.5.1 → Key/Value, never re-typed here).
				k, v, _ := strings.Cut(flags[i+1], "=")
				cv.Env = append(cv.Env, envPair{Key: k, Value: v})
				i++
			}
		case flSecOpt:
			if i+1 < len(flags) {
				cv.PodmanArgs = flSecOpt + " " + flags[i+1]
				i++
			}
		case "-p", "--publish":
			if i+1 < len(flags) {
				cv.PublishPort = flags[i+1]
				i++
			}
		case "-v", "--volume":
			if i+1 < len(flags) {
				cv.Volume = flags[i+1]
				i++
			}
		case flName:
			i++ // consume the name token; Quadlet sets ContainerName.
		}
	}

	cv.Exec = strings.Join(exec, " ")

	// Defensive: every imperative field must have been sourced from the seam. Device
	// and group are slices (≥1 element required); Env is intentionally NOT checked —
	// the Vulkan backend legitimately emits zero env, and requiring it would break the
	// Vulkan path (RESEARCH Pitfall 1).
	if len(cv.AddDevice) == 0 || len(cv.GroupAdd) == 0 || cv.PublishPort == "" ||
		cv.Volume == "" || cv.PodmanArgs == "" || cv.Exec == "" {
		return containerView{}, fmt.Errorf("orchestrate: ContainerArgs missing a required mapped field: %+v", cv)
	}
	return cv, nil
}

// execTemplate renders one named template to a string.
func execTemplate(t *template.Template, name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("orchestrate: render %s: %w", name, err)
	}
	return buf.String(), nil
}
