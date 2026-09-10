package main

// tools_mode.go is the cmd-tier `villa tools-mode` noun: show the persisted
// tool-calling state of the chat unit, or flip it transactionally (spec v1.11 §3.5).
//
// It drives backendswap.RunTools over the SAME liveBackendSwapDeps wiring
// `villa speculation set` uses, because the change is the same one: re-render the
// inference unit and restart it, rolling back verbatim if the new unit does not
// prove healthy. Two seams are overridden for this verb. The fit guard sees the ctx
// FLOOR tools mode serves (max(cfg.Ctx, the catalog entry's agent_ctx)), because the
// shared closure re-checks the model but not the raised context. The cutover proof is
// the residency proof PLUS the read→edit tool-call probe `install --coding-agent`
// already runs, because a unit that is resident but cannot complete a tool call has
// not entered tools mode in any sense the operator cares about.
//
// No llama-server flag literal appears here. The token the rendered unit carries is
// DERIVED from the inference seam by diffing the args it emits with tool calling on
// and off, so the marker stays where TestSeamGrepGate requires it.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// inferenceUnitFile is the rendered inference unit the tools-mode transaction
// captures, restores and reads back for the drift check. It is the same unit
// liveBackendSwapDeps captures; naming it once keeps the two in step.
const inferenceUnitFile = "villa-llama.container"

// newToolsMode builds the `villa tools-mode` noun and its show/enter/exit subcommands.
func newToolsMode() *cobra.Command {
	tm := &cobra.Command{
		Use:   "tools-mode",
		Short: "Inspect or switch tool calling on the chat unit (enter/exit)",
		Long: "Show whether the chat unit is served for tool calling, or switch it with a transactional " +
			"cutover. `enter` serves the SAME chat model with its own chat template — it is not a model " +
			"swap, and coding mode already implies it. The cutover re-renders ONLY the villa-llama unit, " +
			"refuses-with-remediation when the context floor tools mode serves no longer fits the memory " +
			"envelope, and rolls back verbatim if the new unit does not prove both resident and able to " +
			"complete a tool call.",
		Args: cobra.NoArgs,
	}
	tm.AddCommand(newToolsModeShow(), newToolsModeEnter(), newToolsModeExit())
	return tm
}

// newToolsModeShow builds `villa tools-mode show [--json]`.
func newToolsModeShow() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show whether the chat unit is served for tool calling",
		Long: "Print the tool-calling state read from config.toml (the source of truth). It reports the " +
			"answered gate, not the raw flag: coding mode implies tools mode, so a stack in coding mode " +
			"reports on. --json emits the machine-readable form.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runToolsModeShow(cmd, asJSON))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the tools-mode state as JSON")
	return cmd
}

// toolsModeShowEntry is the `tools-mode show --json` shape. It carries the answered
// gate alongside the raw flag so a reader can tell an explicit opt-in from the one
// coding mode implies.
type toolsModeShowEntry struct {
	Tools      bool `json:"tools"`
	ToolsMode  bool `json:"tools_mode"`
	CodingMode bool `json:"coding_mode"`
}

// runToolsModeShow loads config and renders the tool-calling state, RETURNING the
// exit code so tests assert output + code.
func runToolsModeShow(cmd *cobra.Command, asJSON bool) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	cfg, err := config.LoadVilla()
	if err != nil {
		fmt.Fprintf(errOut, "tools-mode show: load config: %v\n", err)
		return exitBlocked
	}
	entry := toolsModeShowEntry{
		Tools:      subsystem.ToolsOn(cfg),
		ToolsMode:  cfg.ToolsMode,
		CodingMode: cfg.CodingMode,
	}

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entry); err != nil {
			fmt.Fprintf(errOut, "tools-mode show: encode json: %v\n", err)
			return exitBlocked
		}
		return exitPass
	}
	fmt.Fprintf(out, "%-12s %s\n", "tools", backendswap.ToolsLabel(entry.Tools))
	if entry.CodingMode && !entry.ToolsMode {
		fmt.Fprintf(out, "%-12s %s\n", "implied by", "coding mode")
	}
	return exitPass
}

// newToolsModeEnter builds `villa tools-mode enter`.
func newToolsModeEnter() *cobra.Command {
	return &cobra.Command{
		Use:   "enter",
		Short: "Serve the chat unit for tool calling (transactional cutover)",
		Long: "Turn tool calling on for the served chat model: check that the context floor tools mode " +
			"serves still fits, capture the prior unit verbatim, persist config + regenerate ONLY the " +
			"villa-llama unit + restart it, and PROVE the cutover with the residency proof and a real " +
			"read→edit tool-call round-trip. Any mutate error or a non-pass proof rolls back verbatim. " +
			"Exits 0 on switch/no-op, 1 on refusal/error/rollback.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runToolsMode(cmd, true, liveToolsModeDeps(true)))
			return nil
		},
	}
}

// newToolsModeExit builds `villa tools-mode exit`.
func newToolsModeExit() *cobra.Command {
	return &cobra.Command{
		Use:   "exit",
		Short: "Stop serving the chat unit for tool calling (transactional cutover)",
		Long: "Turn tool calling off again under the same transactional discipline as enter: capture, " +
			"persist, re-render, restart, prove, and roll back verbatim on any failure. The proof is the " +
			"residency proof alone — a stack with tool calling off has no tool call to make. Coding mode " +
			"is a separate opt-in and is NOT cleared here.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runToolsMode(cmd, false, liveToolsModeDeps(false)))
			return nil
		},
	}
}

// runToolsMode performs the transactional switch and RETURNS the exit code, mapping
// the shared Result exactly as runSpeculationSet does so one Result shape reads the
// same way across every transactional verb.
func runToolsMode(cmd *cobra.Command, on bool, d *backendswap.Deps) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	verb := "enter"
	if !on {
		verb = "exit"
	}

	res := backendswap.RunTools(*d, on)
	switch {
	case res.Refused:
		switch {
		case res.Reason != "":
			fmt.Fprintf(errOut, "tools-mode %s: refusing — %s\n", verb, res.Reason)
		case res.Err != nil:
			fmt.Fprintf(errOut, "tools-mode %s: refusing — %s failed: %v\n", verb, res.FailedStep, res.Err)
		default:
			fmt.Fprintf(errOut, "tools-mode %s: refusing\n", verb)
		}
		return exitBlocked
	case res.RolledBack:
		fmt.Fprintf(errOut, "tools-mode %s: failed at %q — rolled back; tools mode is %s again\n",
			verb, res.FailedStep, res.From)
		if res.Reason != "" {
			fmt.Fprintf(errOut, "  detail: %s\n", res.Reason)
		}
		if res.Err != nil {
			fmt.Fprintf(errOut, "  error:  %v\n", res.Err)
		}
		return exitBlocked
	case res.Err != nil:
		fmt.Fprintf(errOut, "tools-mode %s: failed at %q: %v\n", verb, res.FailedStep, res.Err)
		return exitBlocked
	case res.NoOp:
		fmt.Fprintf(out, "tools mode is already %s — no change\n", res.To)
		return exitPass
	default:
		fmt.Fprintf(out, "switched tools mode %s -> %s — config persisted, %s regenerated and restarted, cutover proven\n",
			res.From, res.To, installServiceName)
		return exitPass
	}
}

// liveToolsModeDeps wires the transactional core to the real host by taking the
// proven backend-swap wiring and replacing the two seams this verb answers
// differently: the fit guard (which must see the served ctx FLOOR, not just the
// preserved model) and the cutover proof (which must include a real tool call on the
// way in). Everything else — capture, save, reconcile, restore, reload, restart — is
// the same closure `villa backend set` and `villa speculation set` run through.
func liveToolsModeDeps(on bool) *backendswap.Deps {
	d := liveBackendSwapDeps()
	d.FitsModel = toolsFits
	d.Prove = liveToolsProve(on)
	return d
}

// toolsCtxFloor is the context tools mode serves: max(cfg.Ctx, the served catalog
// entry's agent_ctx). An entry that declares no agent context leaves cfg.Ctx alone,
// which is every chat entry in today's catalog — the floor exists for the measured
// value spec §13 item 1 will write, and refusing on it before the catalog carries one
// would be refusing on a number nobody measured.
func toolsCtxFloor(cat catalog.Catalog, cfg config.VillaConfig) int {
	if m, ok := cat.FindByID(cfg.Model); ok && m.AgentCtx > cfg.Ctx {
		return m.AgentCtx
	}
	return cfg.Ctx
}

// toolsFits is the enter-path fit guard: does the PRESERVED model still fit the
// memory envelope at the context tools mode will serve? It is the backend-swap fit
// closure with the ctx floor threaded through the recommendation, which the shared
// closure cannot do because it carries no context override.
//
// A non-fit is a refuse-with-remediation with zero side effects, so an operator whose
// envelope shrank is told what it needs rather than having the chat unit restarted
// out from under them and rolled back.
func toolsFits(cfg config.VillaConfig) (bool, string) {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return false, "catalog load failed"
	}
	ctxFloor := toolsCtxFloor(cat, cfg)
	rec := recommend.Pick(detect.Probe(), cat,
		recommend.Overrides{Model: cfg.Model, Quant: cfg.Quant, Ctx: ctxFloor, Speculation: cfg.Speculation},
		recommend.MemoryInputs{Enabled: subsystem.MemoryOn(cfg), EmbeddingModel: cfg.EmbeddingModel},
		webSearchInputsFrom(cfg))
	if rec.Fits {
		return true, ""
	}
	return false, fmt.Sprintf("tools mode serves ctx %d: needs %d bytes vs %d usable — lower `ctx` in config.toml or pick a smaller quant, then re-run `villa tools-mode enter`",
		ctxFloor, rec.TotalBytes, rec.UsableEnvelopeBytes)
}

// liveToolsProve is the cutover gate. It composes the shared residency proof with the
// read→edit tool-call probe, in that order: a model that is not resident cannot be
// asked to make a tool call, and reporting the residency failure is the more useful
// answer.
//
// The tool-call probe runs only on the way IN, and only when the agent addon is
// installed — it is the only tool-call harness on the host. When it cannot run, the
// verdict SAYS the tool call went unproven rather than reporting a bare residency
// pass as a proven tools-mode cutover.
func liveToolsProve(on bool) func(context.Context, string) prove.Verdict {
	return func(ctx context.Context, target string) prove.Verdict {
		v := liveProve(ctx, target)
		if !on || !v.Pass() {
			return v
		}

		cfg, err := config.LoadVilla()
		if err != nil || !subsystem.AgentOn(cfg) {
			return noteVerdict(v, "the tool call went unproven: the coding-agent addon is not installed, so the host has no tool-call harness")
		}
		if _, statErr := os.Stat(agentBinPath()); statErr != nil {
			return noteVerdict(v, "the tool call went unproven: the agent binary is not on disk (`villa install --coding-agent`)")
		}

		probeCtx, cancel := context.WithTimeout(ctx, agentProofBudget)
		defer cancel()
		completed, probeErr := liveAgentToolCallProbe(probeCtx)()
		switch {
		case probeErr != nil:
			return prove.Verdict{
				Status: prove.StatusFail,
				Detail: fmt.Sprintf("resident, but the tool-call round-trip failed to run: %v", probeErr),
			}
		case !completed:
			return prove.Verdict{
				Status: prove.StatusFail,
				Detail: "resident, but the served model did not complete the read→edit tool-call round-trip",
			}
		}
		return noteVerdict(v, "and completed a real read→edit tool-call round-trip")
	}
}

// noteVerdict appends a note to a passing verdict's detail without changing its
// status, so what was and was not proven travels with the result.
func noteVerdict(v prove.Verdict, note string) prove.Verdict {
	if v.Detail == "" {
		v.Detail = note
		return v
	}
	v.Detail += "; " + note
	return v
}

// toolsFlagToken is the token the rendered inference unit carries when tool calling
// is on, DERIVED rather than typed: it is the argument the backend seam adds when
// RunSpec.Tools flips on. Deriving it keeps the flag literal inside
// internal/inference (TestSeamGrepGate) and means a rename of the flag cannot leave
// this check silently asserting a token nothing emits any more.
func toolsFlagToken(backendName string) (string, error) {
	b, err := inference.BackendFor(backendName)
	if err != nil {
		return "", err
	}
	off := map[string]int{}
	for _, a := range b.ContainerArgs(inference.RunSpec{}) {
		off[a]++
	}
	var extra []string
	for _, a := range b.ContainerArgs(inference.RunSpec{Tools: true}) {
		if off[a] > 0 {
			off[a]--
			continue
		}
		extra = append(extra, a)
	}
	if len(extra) != 1 {
		return "", fmt.Errorf("the inference seam adds %d arguments for tool calling, want exactly one", len(extra))
	}
	return extra[0], nil
}

// unitCarriesToolsFlag reports whether a rendered unit's Exec line carries the given
// token as a whole argument. It scans the Exec line rather than the whole file so a
// token appearing in a comment or a label is not mistaken for a served flag.
func unitCarriesToolsFlag(unit []byte, token string) bool {
	sc := bufio.NewScanner(bytes.NewReader(unit))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if f == token {
				return true
			}
		}
	}
	return false
}

// liveToolsDrift is doctor's TMD-01 seam: does the on-disk inference unit carry the
// tool-calling flag, and does config say it should? A unit that cannot be read or a
// backend that cannot be resolved reports ok=false, which doctor renders as a
// typed-Unknown WARN — never as a matching PASS.
func liveToolsDrift() (served, want, ok bool) {
	cfg, err := config.LoadVilla()
	if err != nil {
		return false, false, false
	}
	token, err := toolsFlagToken(cfg.Backend)
	if err != nil {
		return false, false, false
	}
	dir, err := quadletUnitDir()
	if err != nil {
		return false, false, false
	}
	unit, err := os.ReadFile(filepath.Join(dir, inferenceUnitFile))
	if err != nil {
		return false, false, false
	}
	return unitCarriesToolsFlag(unit, token), subsystem.ToolsOn(cfg), true
}
