package main

// backup.go wires `villa backup`: a single self-describing local.tar of
// the recreatable workspace state — config.toml + the Open WebUI data volume +
// usage.json + the single bench-reports.jsonl + a manifest of versions, both image
// digests (seam-sourced — never a literal), store schema versions
// (accessor-sourced), per-entry SHA-256 checksums, and the excluded model
// identities for re-pull. Model WEIGHTS are excluded (re-pullable).
//
// The pure quiesce→export→assemble→restart orchestration lives in internal/backup
// (Backup); this file is the thin cobra caller + liveDeps wiring: podman
// volume export via the shared cmd-tier fixed-arg seam (podman_volume.go),
// service stop/start via orchestrate.NewSystemd, file reads via os.ReadFile, and
// the 0600 traversal-guarded output file the archive is written to. runBackup
// RETURNS the exit code (the RunE wrapper calls os.Exit), mirroring runUninstall.
// --json is intentionally NOT implemented this phase (deferred).

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/backup"
	"github.com/MatrixMagician/VillaStraylight/internal/benchstore"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/evalstore"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// backupTimestamp returns an FS-safe timestamp for the default archive name:
// no ':' (which is illegal on some filesystems and confuses shells) — RFC3339-basic
// with colons replaced, e.g. 20260607T142233Z.
func backupTimestamp(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// defaultBackupName is the default output file `villa-backup-<timestamp>.tar` in CWD
// (D-04).
func defaultBackupName(t time.Time) string {
	return fmt.Sprintf("villa-backup-%s.tar", backupTimestamp(t))
}

// newBackup builds `villa backup`: produce the single self-describing.tar.
func newBackup() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Back up the workspace to a single local .tar (config + Open WebUI data + usage/bench stores)",
		Long: "Produce a single self-describing .tar archive of the recreatable workspace state: config.toml, the " +
			"Open WebUI data volume (exported with the service briefly stopped for a clean SQLite copy), the usage " +
			"store, and the saved bench reports, plus a manifest of versions, image digests, store schema versions, " +
			"per-entry SHA-256 checksums, and the identities of the EXCLUDED model weights (re-pullable, recorded for " +
			"re-pull). Model weights themselves are not backed up. Default output is villa-backup-<timestamp>.tar in " +
			"the current directory; override with -o/--output. Strictly local — no data leaves the box.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runBackup(cmd, output, liveDeps())
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output archive path (default villa-backup-<timestamp>.tar in CWD)")
	return cmd
}

// siteRule is when one file entry is in play for one verb, and what a failure to
// resolve its path warns about. A nil gate means always.
type siteRule struct {
	on     func(config.VillaConfig) bool
	noPath string
}

func (r siteRule) enabled(cfg config.VillaConfig) bool { return r.on == nil || r.on(cfg) }

// fileEntrySite is where one of the registry's file entries lives on THIS host and
// which subsystem gates decide it (ADR-0020). It is the cmd tier's half of
// internal/backup's registry: the core names the entries, this table names their
// paths, so no other code reads MemoryOn / AgentOn / WebSearchOn for the archive.
//
// The two rules differ for recall-state.json on purpose. BACKUP gates it on memory,
// so a memory-OFF archive stays layout-identical to the v1 one even when a
// previously-enabled stack left the file behind (shipping the orphan without a
// manifest recall_schema_version would let it escape the fail-closed schema gate on
// restore). RESTORE always wires it: the archive's own entry decides, and the file
// sits directly under the villa data root. Everything else gates the same way both
// ways: crush.json on the coding agent (its path is OUTSIDE the data-store root) and
// searxng-settings.yml on web search (also outside it, 0600, holds the rendered
// SEARXNG_SECRET).
type fileEntrySite struct {
	entry           string
	path            func() (string, error)
	backup, restore siteRule
}

var fileEntrySites = []fileEntrySite{
	{entry: backup.EntryUsage, path: func() (string, error) { return usage.Path(), nil }},
	{entry: backup.EntryBenchReports, path: func() (string, error) { return benchReportsStorePath(), nil }},
	{
		entry:  backup.EntryRecallState,
		path:   func() (string, error) { return recall.StatePath(), nil },
		backup: siteRule{on: subsystem.MemoryOn},
	},
	{
		entry:   backup.EntryCrushConfig,
		path:    crushConfigPath,
		backup:  siteRule{subsystem.AgentOn, "cannot resolve crush.json path (agent config not archived)"},
		restore: siteRule{subsystem.AgentOn, "cannot resolve crush.json path (agent config will not be restored)"},
	},
	{
		entry:   backup.EntrySearxngSettings,
		path:    orchestrate.SearXNGSettingsFilePath,
		backup:  siteRule{subsystem.WebSearchOn, "cannot resolve settings.yml path (web-search config not archived)"},
		restore: siteRule{subsystem.WebSearchOn, "cannot resolve settings.yml path (web-search config will not be restored)"},
	},
	// eval-baselines.json (ADR-0018, #275): ungated, since `villa eval` is always
	// available, and it lives under the data root so the store-root writer restores it.
	{entry: backup.EntryEvalBaselines, path: func() (string, error) { return evalstore.Path(), nil }},
}

// fileEntryPaths resolves the path of every file entry the verb's rule puts in
// play. A path that cannot be resolved is warned about and left out: the entry is
// then not archived (or not restored) rather than failing the whole verb.
func fileEntryPaths(cfg config.VillaConfig, errOut io.Writer, verb string, pick func(fileEntrySite) siteRule) map[string]string {
	paths := map[string]string{}
	for _, s := range fileEntrySites {
		rule := pick(s)
		if !rule.enabled(cfg) {
			continue
		}
		p, err := s.path()
		if err != nil {
			fmt.Fprintf(errOut, "%s: warning: %s: %v\n", verb, rule.noPath, err)
			continue
		}
		paths[s.entry] = p
	}
	return paths
}

// liveBackupSources is backup.Input.Sources for this host and config: the
// config.toml path plus every file entry whose backup gate is on.
func liveBackupSources(cfg config.VillaConfig, cfgPath string, errOut io.Writer) map[string]string {
	paths := fileEntryPaths(cfg, errOut, "backup", func(s fileEntrySite) siteRule { return s.backup })
	paths[backup.EntryConfig] = cfgPath
	return paths
}

// resolveBackupOutput is the absolute archive path: the default name in CWD when
// none was given.
func resolveBackupOutput(output string) (string, error) {
	if output == "" {
		output = defaultBackupName(time.Now())
	}
	abs, err := filepath.Abs(filepath.Clean(output))
	if err != nil {
		return "", fmt.Errorf("bad output path %q: %w", output, err)
	}
	return abs, nil
}

// runBackup takes the stack lock, gathers the seam-/accessor-sourced backup.Input
// (the output path is traversal-guarded against its parent dir by
// backup.RunBackup), drives the pure RunBackup orchestrator over liveDeps, and
// RETURNS the exit code. The stage→assemble→publish sequence (same-dir temp files,
// atomic rename only after a fully-successful write) lives in internal/backup
// (#239, ADR-0012); this function is parse-input, call, render.
//
// The backup stops Open WebUI and Qdrant to export a clean volume, which would fail
// a concurrent swap's proof, and it archives config.toml: the stack lock (ADR-0010)
// is held from the first config read to the restart.
func runBackup(cmd *cobra.Command, output string, d backup.Deps) int {
	errOut := cmd.ErrOrStderr()
	lock, err := acquireStackLock()
	if err != nil {
		fmt.Fprintf(errOut, "backup: %v\n", err)
		return exitBlocked
	}
	defer func() { _ = lock.Release() }()

	in, err := buildBackupInput(errOut, output)
	if err != nil {
		fmt.Fprintf(errOut, "backup: %v\n", err)
		return exitBlocked
	}
	res, rerr := backup.RunBackup(d, in)
	if rerr != nil {
		fmt.Fprintf(errOut, "backup: failed at %s: %v\n", res.FailedStep, rerr)
		return exitBlocked
	}
	narrateBackup(cmd.OutOrStdout(), errOut, in, res)
	return exitPass
}

// buildBackupInput gathers everything host-derived: the output path, the config (the single source
// of truth for backend selection and the data the manifest records), the image
// digest from the SEAM (never a literal), the source path of every entry, and the
// subsystem identities.
func buildBackupInput(errOut io.Writer, output string) (backup.Input, error) {
	absOut, err := resolveBackupOutput(output)
	if err != nil {
		return backup.Input{}, err
	}
	cfg, err := config.LoadVilla()
	if err != nil {
		return backup.Input{}, fmt.Errorf("load config: %w", err)
	}
	be, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return backup.Input{}, err
	}
	cfgPath, err := config.Path()
	if err != nil {
		return backup.Input{}, fmt.Errorf("resolve config path: %w", err)
	}
	in := backup.Input{
		CreatedAt:           time.Now().UTC().Format(time.RFC3339),
		VillaVersion:        villaVersion(),
		Host:                liveHostFingerprint(),
		InferenceImage:      be.Image(),
		OpenWebUIImage:      orchestrate.OpenWebUIImage(),
		ConfigSchemaVersion: 0, // VillaConfig carries no schema_version field (not recorded).
		UsageSchemaVersion:  usage.SchemaVersion(),
		BenchSchemaVersion:  benchstore.SavedReportSchemaVersion(),
		EvalSchemaVersion:   evalstore.SchemaVersion(),
		OutputPath:          absOut,
		OpenWebUIVolumeName: orchestrate.OpenWebUIVolumeName(),
		Sources:             liveBackupSources(cfg, cfgPath, errOut),
		ExcludedModels:      excludedModelIdentities(cfg),
		FileMissing:         os.IsNotExist,
	}
	applyBackupQdrant(&in, cfg, errOut)
	applyBackupMemory(&in, cfg)
	applyBackupAgent(&in, cfg, errOut)
	return in, nil
}

// applyBackupQdrant adds the optional Phase-23 qdrant volume entry: gated on
// cfg.MemoryEnabled AND a fail-soft existence check over the podmanVolume seam —
// memory off or volume absent means the entry is honestly omitted (and the core
// makes ZERO qdrant Deps calls). The decision is live-host-derived, so it cannot
// move into the pure core.
func applyBackupQdrant(in *backup.Input, cfg config.VillaConfig, errOut io.Writer) {
	if subsystem.MemoryOn(cfg) && volumeExists(orchestrate.QdrantVolumeName(), errOut) {
		// Seam-sourced volume identity — NEVER a literal here.
		in.QdrantVolumeName = orchestrate.QdrantVolumeName()
	}
}

// applyBackupMemory records the manifest's embedding identity and recall store
// schema, ONLY on a memory-on backup: config is the single source of truth for the
// embedding model/dim; the recall schema comes from its accessor. A memory-off
// backup omits all three (the "not recorded" convention) — cfg self-heals embedding
// defaults even when memory is off, so this gate is what keeps memory-off
// manifests claim-free.
func applyBackupMemory(in *backup.Input, cfg config.VillaConfig) {
	if subsystem.MemoryOn(cfg) {
		in.EmbeddingModel = cfg.EmbeddingModel
		in.EmbeddingDim = cfg.EmbeddingDim
		in.RecallSchemaVersion = recall.SchemaVersion()
	}
}

// applyBackupAgent records the coding-agent binary IDENTITY (on-disk sha256 +
// pinned version + policy pin sha256) in the manifest, agent-on ONLY, while the
// binary BYTES are EXCLUDED, exactly like model weights. The rendered crush.json
// itself goes in through Sources. An agent-off backup leaves all three empty so the
// archive stays v2-identical. The BinaryAbsent signal degrades to an empty sha; the
// identity record is still written from the pinned policy version/pin.
func applyBackupAgent(in *backup.Input, cfg config.VillaConfig, errOut io.Writer) {
	if !subsystem.AgentOn(cfg) {
		return
	}
	binSHA, _, herr := hashFileSHA256(agentBinPath())
	if herr != nil {
		fmt.Fprintf(errOut, "backup: warning: cannot hash the coding-agent binary (identity left empty): %v\n", herr)
	}
	policy := agent.LoadCrushPolicy()
	in.AgentBinarySHA256 = binSHA
	in.AgentVersion = policy.Version
	if asset, ok := policy.Assets["linux/amd64"]; ok {
		in.AgentPinSHA256 = asset.BinarySHA256
	}
}

// entryNarration is the one line `villa backup` prints for an optional entry:
// off (never offered: its subsystem is disabled), absent (offered, but no file on
// disk) or included. Narration stays in the cmd tier, keyed by entry name (ADR-0012).
type entryNarration struct {
	entry, off, absent, included string
}

var (
	recallBackupNarration = entryNarration{
		entry:    backup.EntryRecallState,
		off:      "memory: recall state not included (memory disabled)",
		absent:   "memory: recall state not included (no recall-state.json)",
		included: "memory: recall state included (" + backup.EntryRecallState + ")",
	}
	crushBackupNarration = entryNarration{
		entry:    backup.EntryCrushConfig,
		off:      "coding agent: not included (agent disabled)",
		absent:   "coding agent: crush.json not included (no rendered crush.json)",
		included: "coding agent: crush.json included (" + backup.EntryCrushConfig + ")",
	}
	searxngBackupNarration = entryNarration{
		entry:    backup.EntrySearxngSettings,
		off:      "web search: not included (web search disabled)",
		absent:   "web search: settings.yml not included (no rendered settings.yml)",
		included: "web search: settings.yml included (" + backup.EntrySearxngSettings + ")",
	}
	// Eval baselines are ungated, so this entry is never "off".
	evalBackupNarration = entryNarration{
		entry:    backup.EntryEvalBaselines,
		absent:   "eval: baselines not included (no eval-baselines.json)",
		included: "eval: baselines included (" + backup.EntryEvalBaselines + ")",
	}
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// narrateBackupEntry prints the honest line for one optional entry: never leave the
// operator guessing whether it made it into the archive.
func narrateBackupEntry(out io.Writer, n entryNarration, sources map[string]string) {
	path := sources[n.entry]
	line := n.included
	switch {
	case path == "":
		line = n.off
	case !fileExists(path):
		line = n.absent
	}
	fmt.Fprintln(out, line)
}

// narrateBackup reports the written archive and what went into it. A failed
// best-effort service restart is surfaced: the backup succeeded, but a service is
// likely down — warn rather than exit 0 silently.
func narrateBackup(out, errOut io.Writer, in backup.Input, res backup.Result) {
	fmt.Fprintf(out, "backup written to %s\n", in.OutputPath)
	if res.RestartWarning != "" {
		fmt.Fprintf(errOut, "warning: %s\n", res.RestartWarning)
	}
	narrateBackupQdrant(out, in)
	narrateBackupEntry(out, recallBackupNarration, in.Sources)
	narrateExcludedModels(out, in.ExcludedModels)
	narrateBackupEntry(out, crushBackupNarration, in.Sources)
	narrateBackupEntry(out, searxngBackupNarration, in.Sources)
	narrateBackupEntry(out, evalBackupNarration, in.Sources)
	narrateExcludedAgent(out, in)
}

// narrateBackupQdrant states whether the qdrant volume made it into the archive.
func narrateBackupQdrant(out io.Writer, in backup.Input) {
	if in.QdrantVolumeName != "" {
		fmt.Fprintf(out, "memory: Qdrant volume included (%s)\n", backup.EntryQdrantVolume)
		return
	}
	fmt.Fprintf(out, "memory: Qdrant volume not included (memory disabled or volume absent)\n")
}

func narrateExcludedModels(out io.Writer, models []backup.ExcludedModel) {
	if len(models) == 0 {
		return
	}
	fmt.Fprintf(out, "excluded model weights (re-pullable, recorded in manifest for re-pull):\n")
	for _, m := range models {
		fmt.Fprintf(out, "  - %s (quant %s, ctx %s)\n", m.ID, m.Quant, m.Ctx)
	}
}

// narrateExcludedAgent reports the coding-agent binary: identity recorded for
// re-stage, bytes EXCLUDED exactly like model weights.
func narrateExcludedAgent(out io.Writer, in backup.Input) {
	if in.AgentBinarySHA256 == "" && in.AgentVersion == "" && in.AgentPinSHA256 == "" {
		return
	}
	fmt.Fprintf(out, "coding agent: binary excluded, identity recorded for re-stage (pinned %s) — re-stage with `villa install --coding-agent`\n", in.AgentVersion)
}

// assertBackupOutputInside verifies the resolved output path stays within its parent
// dir (T-16-02a tar/output traversal guard), mirroring the config/usage guard shape.
func assertBackupOutputInside(path, dir string) error {
	if err := pathsafe.Inside(path, dir); err != nil {
		return fmt.Errorf("output escapes its parent dir: %w", err)
	}
	return nil
}

// liveHostFingerprint flattens detect.Probe()'s typed-Unknown HostProfile into the
// plain-string backup.HostFingerprint (keeping internal/backup free of detect — the
// SeamGrepGate-clean discipline). Unknown values become the empty sentinel so the
// manifest records "" rather than a fabricated value.
func liveHostFingerprint() backup.HostFingerprint {
	hp := detect.Probe()
	return backup.HostFingerprint{
		Arch:   knownOrEmpty(hp.Arch.Known, hp.Arch.Value),
		IGPU:   knownOrEmpty(hp.IGPUGfxID.Known, hp.IGPUGfxID.Value),
		Kernel: knownOrEmpty(hp.KernelVersion.Known, hp.KernelVersion.Value),
	}
}

// knownOrEmpty returns v when known, else "" (honest empty sentinel, never a
// fabricated value on an Unknown probe).
func knownOrEmpty(known bool, v string) string {
	if known {
		return v
	}
	return ""
}

// excludedModelIdentities records the IDENTITY of the model weight the backup
// excludes, sourced from config (the single source of truth) so restore can
// report it for re-pull. Identity only — never any content. An empty config model
// yields no record.
func excludedModelIdentities(cfg config.VillaConfig) []backup.ExcludedModel {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	ctx := ""
	if cfg.Ctx > 0 {
		ctx = strconv.Itoa(cfg.Ctx)
	}
	return []backup.ExcludedModel{{
		ID:     cfg.Model,
		Quant:  cfg.Quant,
		Ctx:    ctx,
		Source: "catalog",
	}}
}

// liveDeps wires the pure backup.Backup seam to the real host: service
// stop/start via orchestrate.NewSystemd, podman volume export via the shared
// fixed-arg cmd-tier seam (podman_volume.go), and file reads via os.ReadFile.
func liveDeps() backup.Deps {
	sys := orchestrate.NewSystemd()
	return backup.Deps{
		OpenWebUIServiceName: openWebUIServiceName,
		// QdrantServiceName is derived from the orchestrate unit-name accessor (the
		// same unitServiceName mapping doctor/status use) — never a re-typed literal
		// (D-05).
		QdrantServiceName: unitServiceName(orchestrate.QdrantContainerUnitName()),
		Stop:              sys.Stop,
		Start:             sys.Start,
		VolumeExport: func(name, outPath string) error {
			if err := requirePodman(); err != nil {
				return err
			}
			stderr, err := podmanVolume(volumeExportArgs(name, outPath))
			if err != nil {
				return fmt.Errorf("podman volume export %s: %w: %s", name, err, stderr)
			}
			return nil
		},
		ReadFile: os.ReadFile,
		// OpenFile is the streaming seam: the exported volume tars (the one
		// entry class that can reach many GiB) are checksummed and tar-copied via
		// io.Copy from this reader instead of being buffered whole in memory.
		OpenFile: func(path string) (io.ReadCloser, int64, error) {
			f, err := os.Open(path)
			if err != nil {
				return nil, 0, err
			}
			fi, err := f.Stat()
			if err != nil {
				_ = f.Close()
				return nil, 0, err
			}
			return f, fi.Size(), nil
		},
		// CreateTemp/Rename/Remove are RunBackup's stage→publish seam (#239):
		// same-directory temp files and the atomic publish rename.
		CreateTemp: func(dir, pattern string) (string, io.WriteCloser, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return "", nil, err
			}
			return f.Name(), f, nil
		},
		Rename: os.Rename,
		Remove: os.Remove,
	}
}
