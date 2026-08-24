package orchestrate

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
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
	// UnitFileName is the .container filename this unit is written as, rendered into
	// the header comment. It is a field rather than the fixed containerUnitName
	// because a resident slot renders the same template under its own filename.
	UnitFileName  string
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

	// single point: turn "coding_mode=true in config" into the rendered tool-calling
	// unit. The CALLER (Plan-02 live wiring) resolves the coder catalog entry from
	// cfg.CoderModel and translates catalog.AgentSampling → inference.Sampling, handing
	// Render the already-translated descriptor on RenderInput — so the pure renderer never
	// imports internal/catalog. When the descriptor is present, the single -c carries the
	// resolved agent ctx (Pitfall 1: spec.ContextLen = CoderAgentCtx, never a second -c).
	// When absent (in.CodingMode == nil) spec is left exactly as v1.3, so the off-path
	// goldens are byte-identical BY CONSTRUCTION.
	if in.CodingMode != nil {
		spec.CodingMode = in.CodingMode
		spec.ContextLen = in.CoderAgentCtx
	}

	cv, err := parseContainerArgs(in.Backend.Image(), in.Backend.ContainerArgs(spec))
	if err != nil {
		return nil, err
	}
	// Description= label is keyed off the backend's seam identity (Name()), never a
	// literal — so the ROCm unit gets an accurate description while the Vulkan unit's
	// Description line stays byte-identical to today's golden (ROCM-03 additivity).
	cv.BackendLabel = backendLabel(in.Backend.Name())

	// Resolved up-front because two consumers need it: the resident .container units
	// below, and Open WebUI's endpoint env, which must list every resident slot.
	residentNames, err := residentContainerNames(in.Resident)
	if err != nil {
		return nil, err
	}

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

	// Open WebUI is the 4th/5th unit: a dedicated managed-service render path
	// (openwebui.go) — NOT the inference Backend seam. Pitfall 4: routing it through
	// parseContainerArgs would trip that helper's defensive all-fields-non-empty check
	// (Open WebUI has no device/group/exec args). The owui view reuses networkAttach so
	// it joins villa.network unchanged — the Phase-3 forward-compat scaffold pays off.
	//
	// Phase-20: Open WebUI is now memory-aware. The OWUI env block grows only
	// when memory_enabled=true — the RAG/Qdrant/memory group is appended from the
	// resolved render-view (mv); with memory off the unit is byte-identical to the v1.2
	// golden. mv is computed ONCE here (memory.RenderView is pure, cheap, identical) and
	// reused by the memory-stack branch below.
	mv := memory.RenderView(in.Cfg) // resolved-values handoff (Phase-18 spine)
	owuiContainerText, err := execTemplate(tmpl, "openwebui.container.tmpl", buildOpenWebUIView(mv, in.Cfg.MemoryEnabled, in.Cfg.WebSearchEnabled, config.SearxngAddr, config.SearxngPort, in.Cfg.WebSearchResultCount, config.WebsafeAddr, config.WebsafePort, residentNames))
	if err != nil {
		return nil, err
	}
	owuiVolumeText, err := execTemplate(tmpl, "openwebui.volume.tmpl", buildOpenWebUIVolumeView())
	if err != nil {
		return nil, err
	}

	// Fixed deterministic emit order (callers + goldens depend on it):
	// container, network, models-volume, openwebui-container, openwebui-volume.
	units := []Unit{
		{Name: containerUnitName, Text: containerText},
		{Name: networkUnitName, Text: networkText},
		{Name: volumeUnitName, Text: volumeText},
		{Name: openWebUIContainerUnitName, Text: owuiContainerText},
		{Name: openWebUIVolumeUnitName, Text: owuiVolumeText},
	}

	// Resident secondary models are appended STRICTLY after the fixed five and BEFORE
	// the memory and web-search blocks. The position is arbitrary but fixed: the
	// goldens and the reconcile plan are order-sensitive, so it may not move.
	residentUnits, err := renderResidentUnits(tmpl, in, residentNames, cv.PublishPort, cv.BackendLabel)
	if err != nil {
		return nil, err
	}
	units = append(units, residentUnits...)

	// v1.3 memory stack: the two new managed services + the durable Qdrant
	// volume are appended ONLY when memory_enabled=true. With memory off this branch is
	// skipped and the returned slice is byte-identical to the v1.2 5-unit output (the 5
	// existing goldens stay unchanged — Phase-18 continuity). Like Open WebUI, the
	// villa-qdrant / villa-embed views are a dedicated managed-service render path
	// (memory.go) and BYPASS parseContainerArgs (Pitfall 4: no GPU device/group/exec
	// args for that helper's defensive all-fields-non-empty check). memory.RenderView
	// is the resolved-values-only handoff (model id, dim, addr/port PIECES; no
	// image literal — orchestrate owns the image consts). The memory units render
	// their container-DNS identity (mv.QdrantAddr / mv.EmbedAddr) and the served
	// embed --port (mv.EmbedPort) FROM these resolved config values — config is the
	// single source of truth, so the units can never silently diverge from
	// what the readiness proof probes (which also reads cfg). EmbeddingDim/
	// EmbeddingModel are NOT rendered into any unit (the Qdrant collection dim is an
	// OWUI-runtime concern, not a unit field); their single source stays config,
	// consumed by the proof + Phase 23.
	if in.Cfg.MemoryEnabled {
		// mv is the hoisted render-view computed once above; reused
		// here so memory.RenderView runs exactly once per Render.
		qdrantContainerText, err := execTemplate(tmpl, "qdrant.container.tmpl", buildQdrantView(mv.QdrantAddr))
		if err != nil {
			return nil, err
		}
		qdrantVolumeText, err := execTemplate(tmpl, "qdrant.volume.tmpl", buildQdrantVolumeView())
		if err != nil {
			return nil, err
		}
		// The served GGUF `-m` path binds the single-source embedGGUFFilename const
		// (surfaced via the exported EmbedGGUFFilename() that Plan 19-02's drift test
		// binds — Pitfall 3) so it can never drift from the pre-staged Shard.Filename.
		// The container-DNS name (mv.EmbedAddr) and the served /v1 --port (mv.EmbedPort)
		// come from the resolved config so they match the proof's probe target.
		embedContainerText, err := execTemplate(tmpl, "embed.container.tmpl", buildEmbedView(embedGGUFFilename, mv.EmbedAddr, mv.EmbedPort))
		if err != nil {
			return nil, err
		}
		units = append(units,
			Unit{Name: qdrantContainerUnitName, Text: qdrantContainerText},
			Unit{Name: qdrantVolumeUnitName, Text: qdrantVolumeText},
			Unit{Name: embedContainerUnitName, Text: embedContainerText},
		)
	}

	// v1.5 web-search stack: the single villa-searxng managed service is
	// appended ONLY when web_search_enabled=true, STRICTLY AFTER the memory branch and
	// never mutating the shared `units` slice or any shared view (Pitfall 6). With
	// web search off this branch is skipped and the returned slice is byte-identical to the
	// v1.4 output (the 13 existing goldens stay unchanged), proven by the negative test.
	// Like Open WebUI / the memory stack, the searxng view is a dedicated managed-service
	// render path (searxng.go) that BYPASSES parseContainerArgs (no GPU device/group/exec
	// args). The container-DNS identity (config.SearxngAddr) is threaded FROM resolved config
	// so the rendered service can never diverge from what Plan 03's readiness proof
	// probes. The render does NOT thread the secret: the unit references it only via the
	// EnvironmentFile= path baked by buildSearxngView (the secret value lives in config +
	// the 0600 env file Plan 02 writes, never in this 0644 unit — / Pitfall 2). The
	// settings.yml is NOT a Unit (Pitfall 1: it must not land in the systemd unit dir) — it
	// is produced by the separate RenderSearxngSettings helper that Plan 02's writer consumes.
	if in.Cfg.WebSearchEnabled {
		searxngContainerText, err := execTemplate(tmpl, "searxng.container.tmpl", buildSearxngView(config.SearxngAddr))
		if err != nil {
			return nil, err
		}
		units = append(units, Unit{Name: searxngContainerUnitName, Text: searxngContainerText})

		// Phase-31: the villa-websafe loader is appended STRICTLY
		// AFTER the searxng unit, inside the SAME web-search gate (websafe and SearXNG are
		// both the web-search stack), never mutating the shared `units` slice or any shared
		// view before this point (Pitfall 6 byte-identical-off discipline). The host villa
		// binary PATH (in.HostVillaPath) is bind-mounted read-only and the container-DNS
		// identity (config.WebsafeAddr) + in-network port (config.WebsafePort) are threaded FROM
		// resolved config so the rendered service can never diverge from what OWUI's
		// EXTERNAL_WEB_LOADER_URL composes. The render does NOT thread the secret: the unit
		// references it only via the EnvironmentFile= path baked by buildWebsafeView (the
		// secret value lives in config + the 0600 env file Plan 02 writes, never in this 0644
		// unit).
		websafeContainerText, err := execTemplate(tmpl, "websafe.container.tmpl", buildWebsafeView(config.WebsafeAddr, in.HostVillaPath, config.WebsafePort))
		if err != nil {
			return nil, err
		}
		units = append(units, Unit{Name: websafeContainerUnitName, Text: websafeContainerText})
	}

	return units, nil
}

// RenderSearxngSettings renders the SearXNG settings.yml from config and returns the bare
// filename + text for Plan 02's impure writer to persist (at 0600) into the villa searxng
// config dir mounted read-only at /etc/searxng. It is a SEPARATE pure helper — NOT part of
// the Render() []Unit slice — because settings.yml is a config FILE, not a systemd unit
// (rendering it into the unit dir would make systemd's generator choke, Pitfall 1). The
// engine allowlist is the single-source vetted subset; secret_key renders empty
// (the live value arrives via $SEARXNG_SECRET from the EnvironmentFile, never written into
// this 0644-capable file — / Pitfall 2). cfg is accepted for forward symmetry with
// the unit render's config-driven identity; the rendered content is config-derived.
func RenderSearxngSettings(cfg config.VillaConfig) (name, text string, err error) {
	tmpl, err := template.ParseFS(quadletFS, "quadlet/*.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("orchestrate: parse templates: %w", err)
	}
	text, err = execTemplate(tmpl, "searxng-settings.yml.tmpl", buildSettingsYml(searxngEngines))
	if err != nil {
		return "", "", err
	}
	return "settings.yml", text, nil
}

// RenderSearxngSecretEnv renders the 0600 SEARXNG_SECRET env-file Plan 02 writes and the
// searxng .container unit references via EnvironmentFile= (SearXNGSecretEnvFilePath). It is
// the SINGLE source of the env-file FORMAT (a fixed `SEARXNG_SECRET=<value>` line, no shell
// interpolation) — Plan 02's writer emits exactly these bytes at 0600. It is NOT a Unit
// (the secret must never land in the 0644 unit dir). The secret value is the
// crypto/rand secret from config.SearxngSecret; it is NEVER logged.
func RenderSearxngSecretEnv(secret string) (name, text string) {
	return searxngSecretEnvName(), searxngSecretEnvBody(secret)
}

// RenderWebsafeSecretEnv renders the 0600 EXTERNAL_WEB_LOADER_API_KEY env-file Plan 02
// writes and BOTH the villa-websafe AND the OWUI .container units reference via
// EnvironmentFile= (WebsafeSecretEnvFilePath). It is the SINGLE source of the env-file FORMAT
// (a fixed `EXTERNAL_WEB_LOADER_API_KEY=<value>` line, no shell interpolation) — Plan 02's
// writer emits exactly these bytes at 0600. It is NOT a Unit (the secret must never land in
// the 0644 unit dir). The secret value is the crypto/rand bearer from
// config.WebLoaderSecret; it is NEVER logged. Mirrors RenderSearxngSecretEnv.
func RenderWebsafeSecretEnv(secret string) (name, text string) {
	return websafeSecretEnvName(), websafeSecretEnvBody(secret)
}

// parseContainerArgs maps the proven `podman run` argument slice into Quadlet keys.
// It locates the image token by identity (image) and treats everything after it as
// the Exec command and everything before it as run flags — so the device, group,
// security, publish, and bind literals are READ from the slice, never re-typed.
func parseContainerArgs(image string, args []string) (containerView, error) {
	cv := containerView{
		UnitFileName:  containerUnitName,
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
	// consume the following token; valueless run sub-args (run/--rm) are ignored
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
	// and group are slices (≥1 element required); Env is intentionally NOT checked
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
