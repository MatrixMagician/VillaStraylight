package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/codingmode"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// coding-mode.go is the cmd-tier `villa coding-mode` noun: the live host wiring that
// drives the pure internal/codingmode transactional core (Plan 25-02). It holds the
// cobra surface (enter/exit), the Result→exit mapping, and liveCodingModeDeps. The
// cutover gate is liveProve, which proves the served target (the coder in swap residency).
//
// CRITICAL — backend-marker discipline: this file must stay LITERAL-FREE of
// backend marker tokens (the per-backend residency device token, the HSA override env
// var, the GPU-fault abort string, and any image/device literal) AND of coding-flag
// literals (--jinja / --cache-reuse / sampling). Backend markers arrive ONLY through
// inference.BackendFor(cfg.Backend).ResidencyProof(); the coding-flag literals live ONLY
// in internal/inference (backend_*.go) behind the seam. The Plan-01-extended
// TestSeamGrepGate WALKS cmd/villa and fails CI on any such leak. Do NOT paste markers
// or coding flags here.
//
// Decisions realized: (explicit verb shape coding-mode enter|exit; NOT `villa code`),
// (exit symmetric to enter), (under-load prove; idle-green is never green),
// (swap vs shared surfaced, never silently degraded).

// ---------------------------------------------------------------------------
// coding-mode noun: `villa coding-mode enter` / `villa coding-mode exit`.
// Two explicit subcommands — NOT `villa code` (reserved for the Phase-26 agent launcher).
// Cloned from the backend.go set noun: RunE returns the mapped exit code (body RETURNS
// the int so tests assert output+code without a subprocess), and the Result→exit mapping
// mirrors runBackendSet.
// ---------------------------------------------------------------------------

// newCodingMode builds the `villa coding-mode` noun and its enter/exit subcommands.
func newCodingMode() *cobra.Command {
	cm := &cobra.Command{
		Use:   "coding-mode",
		Short: "Enter or exit the tool-calling coding mode (transactional cutover)",
		Long: "Flip the running stack into a tool-calling-ready coding mode (or back) with a transactional " +
			"cutover: capture the prior unit + config verbatim, swap the chat model for the fit-guarded coder " +
			"model (swap residency) or apply the tool-calling render delta to the chat endpoint (shared " +
			"residency), restart ONLY the villa-llama unit, and PROVE the cutover (real generation probe + " +
			"GPU-residency proof UNDER LOAD within a bounded timeout). Any mutate error or a non-pass proof " +
			"rolls back verbatim — a failed cutover is a no-op to the running stack. The mode changes ONLY via " +
			"this explicit verb; nothing auto-flips it.",
		Args: cobra.NoArgs,
	}
	cm.AddCommand(newCodingModeEnter(), newCodingModeExit())
	return cm
}

// newCodingModeEnter builds `villa coding-mode enter`.
func newCodingModeEnter() *cobra.Command {
	return &cobra.Command{
		Use:   "enter",
		Short: "Enter coding mode transactionally (capture → cutover → prove → rollback)",
		Long: "Enter the tool-calling coding mode on the running install: resolve the fit-guarded coder model " +
			"at its agent context (refuse-with-remediation if it does not fit), auto-pull the weights if absent " +
			"(swap residency), capture the prior unit verbatim, persist config + regenerate ONLY the villa-llama " +
			"unit + restart it, and PROVE the cutover (real generation probe + GPU-residency proof under load). " +
			"Any mutate error or a non-pass proof rolls back verbatim. Exits 0 on enter/no-op, 1 on " +
			"refusal/error/rollback. Already in coding mode is a clean no-op.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runCodingMode(cmd, codingmode.Enter, liveCodingModeDeps(cmdContext(cmd)))
			os.Exit(code)
			return nil
		},
	}
}

// newCodingModeExit builds `villa coding-mode exit`.
func newCodingModeExit() *cobra.Command {
	return &cobra.Command{
		Use:   "exit",
		Short: "Exit coding mode transactionally (symmetric to enter)",
		Long: "Exit the tool-calling coding mode on the running install: restore the chat model under the SAME " +
			"transactional discipline as enter (capture → mutate → prove → rollback) — not a bare flip. Exits 0 " +
			"on exit/no-op, 1 on error/rollback. Already in chat mode is a clean no-op.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runCodingMode(cmd, codingmode.Exit, liveCodingModeDeps(cmdContext(cmd)))
			os.Exit(code)
			return nil
		},
	}
}

// runCodingMode delegates to codingmode.Run and maps the typed Result to exit codes +
// messages (clone of runBackendSet). It surfaces the residency mode (swap vs shared) on a
// successful enter so a shared cutover is never silently presented as a swap, and
// the honest rollback-incomplete Reason verbatim on a rollback (Pitfall 5). The body
// returns the int (no os.Exit) so tests assert output+code.
func runCodingMode(cmd *cobra.Command, dir codingmode.Direction, d *codingmode.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// The transaction frame holds the stack lock (ADR-0010) throughout.
	res := codingmode.Run(*d, dir)
	switch {
	case res.Refused:
		if res.Reason != "" {
			fmt.Fprintf(errOut, "coding-mode %s: refusing — %s\n", dir, res.Reason)
		} else if res.Err != nil {
			fmt.Fprintf(errOut, "coding-mode %s: refusing — %s failed: %v\n", dir, res.FailedStep, res.Err)
		} else {
			fmt.Fprintf(errOut, "coding-mode %s: refusing\n", dir)
		}
		return exitBlocked
	case res.RolledBack:
		// A mutate error or a non-pass prove verdict rolled back verbatim. Reason already
		// folds in an honest rollback-incomplete message when the restore did not fully
		// complete (Pitfall 5).
		fmt.Fprintf(errOut, "coding-mode %s: cutover failed at %q — rolled back; prior state restored\n",
			dir, res.FailedStep)
		if res.Reason != "" {
			fmt.Fprintf(errOut, "  detail: %s\n", res.Reason)
		}
		if res.Err != nil {
			fmt.Fprintf(errOut, "  error:  %v\n", res.Err)
		}
		return exitBlocked
	case res.Err != nil:
		// A non-rollback failure path (e.g. pull). Run rolls back mutate errors; a pull
		// error happens BEFORE any mutation, so there is nothing to roll back.
		fmt.Fprintf(errOut, "coding-mode %s: failed at %q: %v\n", dir, res.FailedStep, res.Err)
		return exitBlocked
	case res.NoOp:
		if dir == codingmode.Enter {
			fmt.Fprintf(out, "already in coding mode — no change\n")
		} else {
			fmt.Fprintf(out, "already in chat mode — no change\n")
		}
		return exitPass
	default: // Switched
		if dir == codingmode.Enter {
			// Surface the residency mode so shared is never silent.
			switch res.Residency {
			case codingmode.ResidencyShared:
				fmt.Fprintf(out, "entered coding mode (shared residency: tool-calling render delta applied to the chat model %q — no model swap) — %s restarted, cutover proven under load\n",
					res.FromModel, installServiceName)
			default: // swap
				fmt.Fprintf(out, "entered coding mode (swap residency: chat model %q -> coder model %q) — %s restarted, cutover proven under load\n",
					res.FromModel, res.ToModel, installServiceName)
			}
		} else {
			fmt.Fprintf(out, "exited coding mode — chat model %q restored, %s restarted, cutover proven under load\n",
				res.ToModel, installServiceName)
		}
		return exitPass
	}
}

// liveCodingModeDeps wires the enter path to the real host — the composed modelswap
// resolve→fit-guard→pull for the coder model — over the live transaction frame,
// whose proof (liveProve) drives the served target the render chose.
//
// ctx is the command's SIGINT/SIGTERM-cancelled context, captured by the Pull
// closure so Ctrl-C can interrupt the multi-GB coder-weight transfer. Cancelling
// mid-stream is safe: the partial ".part" file is kept and resumed via HTTP Range.
func liveCodingModeDeps(ctx context.Context) *codingmode.Deps {
	return &codingmode.Deps{
		Tx: liveTxDeps(acquireStackLock),
		// ResolveCoder: compose recommend.Pick(...).Coder (the agent-ctx fit-math + residency
		// verdict) — fit-guard FIRST (Pitfall 4). A non-fitting coder at agent ctx
		// is a refuse-with-remediation; the residency verdict (swap/shared) is a PURE fit-math
		// output, never a preference.
		ResolveCoder: func(_ config.VillaConfig) (codingmode.CoderTarget, bool, string) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return codingmode.CoderTarget{}, false, "catalog load failed"
			}
			// Persisted memory inputs (fail-soft): the coder fit must see the same shrunken
			// envelope the user was recommended (ordering).
			rec := recommend.Pick(detect.Probe(), cat, recommend.Overrides{}, liveLoadedReservations())
			coder := rec.Coder
			if coder.Residency == codingmode.ResidencyShared {
				// Shared residency: no coder fits standalone — apply render-delta-only on
				// the chat endpoint. Still a valid enter target (NOT a refusal); surfaced as shared.
				return codingmode.CoderTarget{
					AgentCtx:  coder.AgentCtx,
					Residency: codingmode.ResidencyShared,
				}, true, ""
			}
			if !coder.Fits || coder.Model == "" {
				// Defensive: a swap verdict must carry a fitting model. Treat an unfit/empty
				// swap as a refuse-with-remediation (never a silent OOM at container start).
				return codingmode.CoderTarget{}, false,
					fmt.Sprintf("no coder model fits the agent-ctx envelope (needs %d bytes vs %d usable)",
						coder.TotalBytes, rec.UsableEnvelopeBytes)
			}
			m, ok := cat.FindByID(coder.Model)
			downloaded := false
			if ok {
				_, statErr := os.Stat(filepath.Join(modelsDir(), m.PrimaryFile()))
				downloaded = statErr == nil
			}
			return codingmode.CoderTarget{
				Model:      coder.Model,
				Quant:      coder.Quant,
				AgentCtx:   coder.AgentCtx,
				Residency:  codingmode.ResidencySwap,
				Downloaded: downloaded,
			}, true, ""
		},
		// Pull: auto-download the verified coder weights (reuse the same downloader as
		// `model pull` / `model swap` via the pullFn seam — compose, don't fork).
		Pull: func(t codingmode.CoderTarget) error {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return err
			}
			m, ok := cat.FindByID(t.Model)
			if !ok {
				return fmt.Errorf("coder model %q is not in the catalog", t.Model)
			}
			dir := modelsDir()
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return mkErr
			}
			return pullFn(ctx, m, dir)
		},
	}
}
