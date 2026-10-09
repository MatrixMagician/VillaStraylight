package main

// verify_voice.go is `villa verify voice`: the spoken round trip (ADR-0030) against
// the running villa-stt and villa-tts, gated on the persisted voice_enabled. The
// verdict is voice.ProveOnGPU's and the exit code is the verify family's: pass 0, fail
// blocked, reject warn. It reads and never mutates, so it takes no stack lock,
// persists nothing and has no --json.

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// verifyVoiceDeps are the host seams of `villa verify voice`.
type verifyVoiceDeps struct {
	loadedConfig func() (config.VillaConfig, error)
	proveFn      func(context.Context) verify.Proof
}

func liveVerifyVoiceDeps() verifyVoiceDeps {
	return verifyVoiceDeps{loadedConfig: liveLoadedConfig, proveFn: liveVoiceProof}
}

func newVerifyVoice() *cobra.Command {
	return &cobra.Command{
		Use:   "voice",
		Short: "Prove speech-to-text and text-to-speech together with one spoken round trip",
		Long: "Have villa-tts speak a fixed sentence and villa-stt transcribe the audio, from inside " +
			"villa.network, then compare the words and read villa-stt's journal for where whisper ran. " +
			"One round trip proves both units. Gated on the persisted voice_enabled; exits 0 (passed, " +
			"or voice off), 1 (a unit answered wrongly, or whisper fell back to the CPU) or 2 (a unit " +
			"could not be reached, or where whisper ran could not be read, so nothing was proven). " +
			"Mutates nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runVerifyVoice(cmd, args, liveVerifyVoiceDeps()))
			return nil
		},
	}
}

// runVerifyVoice reads the config before the gate because the gate is a config
// field: an unreadable config refuses rather than reading as voice off.
func runVerifyVoice(cmd *cobra.Command, _ []string, deps verifyVoiceDeps) int {
	cfg, err := deps.loadedConfig()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "verify voice: cannot read config.toml (%v), fix or remove it, then re-run.\n", err)
		return exitBlocked
	}
	outcome := verify.Run(verify.Subject{
		Name:            "verify voice",
		Enabled:         func() bool { return subsystem.VoiceOn(cfg) },
		DisabledMessage: "voice is not enabled (voice_enabled=false), nothing to verify. Enable it with `villa install --voice`, then re-run.",
		FailLabel:       "speech round-trip proof",
		RejectLabel:     "speech round-trip proof",
		Prove:           func() verify.Proof { return deps.proveFn(cmd.Context()) },
	})
	return renderVerifyOutcome(cmd.OutOrStdout(), cmd.ErrOrStderr(), outcome)
}
