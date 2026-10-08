package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// TestRunVerifyVoice: each verdict of the spoken round trip reaches the operator as
// one line on the stream its severity belongs to, with the family's exit code. An
// off gate is a clean exit that names how to turn voice on, and an unreadable config
// refuses rather than reading as voice off.
func TestRunVerifyVoice(t *testing.T) {
	const heard = `heard "the quick brown fox jumps over the lazy dog and the small cat sleeps in the warm sun" (agreement 1.00)`
	for _, tc := range []struct {
		name       string
		cfg        config.VillaConfig
		cfgErr     error
		proof      verify.Proof
		wantCode   int
		wantStdout string
		wantStderr string
		wantProved bool
	}{
		{
			name:       "voice off",
			wantCode:   exitPass,
			wantStdout: "verify voice: voice is not enabled (voice_enabled=false), nothing to verify. Enable it with `villa install --voice`, then re-run.\n",
		},
		{
			name:       "pass",
			cfg:        config.VillaConfig{VoiceEnabled: true},
			proof:      verify.Proof{Status: verify.Pass, Detail: heard},
			wantCode:   exitPass,
			wantStdout: "verify voice: " + heard + "\n",
			wantProved: true,
		},
		{
			name:       "fail",
			cfg:        config.VillaConfig{VoiceEnabled: true},
			proof:      verify.Proof{Status: verify.Fail, Detail: "villa-tts returned no audio"},
			wantCode:   exitBlocked,
			wantStderr: "verify voice: speech round-trip proof FAILED: villa-tts returned no audio\n",
			wantProved: true,
		},
		{
			name:       "reject",
			cfg:        config.VillaConfig{VoiceEnabled: true},
			proof:      verify.Proof{Status: verify.Reject, Detail: "villa-stt could not be reached (voice service unreachable)"},
			wantCode:   exitWarn,
			wantStderr: "verify voice: speech round-trip proof could not be conducted (REJECT): villa-stt could not be reached (voice service unreachable)\n",
			wantProved: true,
		},
		{
			name:       "unreadable config",
			cfgErr:     errors.New("toml: line 3: expected '='"),
			wantCode:   exitBlocked,
			wantStderr: "verify voice: cannot read config.toml (toml: line 3: expected '='), fix or remove it, then re-run.\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proved := false
			deps := verifyVoiceDeps{
				loadedConfig: func() (config.VillaConfig, error) { return tc.cfg, tc.cfgErr },
				proveFn: func(context.Context) verify.Proof {
					proved = true
					return tc.proof
				},
			}
			cmd := &cobra.Command{}
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)

			if code := runVerifyVoice(cmd, nil, deps); code != tc.wantCode {
				t.Errorf("exit = %d, want %d", code, tc.wantCode)
			}
			if out.String() != tc.wantStdout {
				t.Errorf("stdout = %q, want %q", out.String(), tc.wantStdout)
			}
			if errOut.String() != tc.wantStderr {
				t.Errorf("stderr = %q, want %q", errOut.String(), tc.wantStderr)
			}
			if proved != tc.wantProved {
				t.Errorf("proof ran = %v, want %v", proved, tc.wantProved)
			}
		})
	}
}

// TestVerifyVoiceRegistered: `villa verify voice` is reachable from the verb tree.
func TestVerifyVoiceRegistered(t *testing.T) {
	sub, _, err := newVerify().Find([]string{"voice"})
	if err != nil || sub.Name() != "voice" {
		t.Fatalf("villa verify has no voice subcommand (found %v, err %v)", sub, err)
	}
}
