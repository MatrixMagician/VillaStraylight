package main

// voice.go is the live half of the voice proof (ADR-0030): the two curl legs that
// speak voice.ProofSentence on villa-tts and transcribe the audio on villa-stt. Both
// run from the in-network helper because neither unit publishes a host port, and the
// audio passes from one leg to the next on curl's stdin, so it never touches the host
// filesystem. The verdict is voice.Prove's; this file only reports what curl saw.
// Install, `verify voice` and `update voice` call the same liveVoiceProof. It reads
// and never mutates, so it takes no stack lock.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
	"github.com/MatrixMagician/VillaStraylight/internal/voice"
)

// curlExitHTTPError is curl's exit code under -f for an HTTP status of 400 or more:
// the unit was reached and answered with an error.
const curlExitHTTPError = 22

// voiceRetryDelay is the pause between voice.Prove's cold-start attempts.
const voiceRetryDelay = 3 * time.Second

// classifyVoiceCurl maps one leg's curl outcome onto the two errors voice.Prove
// tells apart. An HTTP error status is a plain error: the unit answered. Anything
// else that failed (a refused connection, a timeout, a helper that never started)
// wraps voice.ErrUnreachable, because nothing about the unit was measured.
func classifyVoiceCurl(exitCode int, err error) error {
	if err == nil {
		return nil
	}
	if exitCode == curlExitHTTPError {
		return fmt.Errorf("HTTP error status: %w", err)
	}
	return fmt.Errorf("%w (curl exit %d: %v)", voice.ErrUnreachable, exitCode, err)
}

// voiceCurl runs one in-network curl: stdin (nil for none) and curl's arguments in,
// curl's stdout and exit code out.
type voiceCurl func(stdin []byte, args []string) ([]byte, int, error)

// voiceDriver binds voice.Driver's legs to one curl seam, so the requests and the
// classification are tested without podman.
func voiceDriver(curl voiceCurl, wait func()) voice.Driver {
	return voice.Driver{
		Speak: func() ([]byte, error) {
			body, err := json.Marshal(map[string]string{
				"model":           orchestrate.TTSModel(),
				"voice":           orchestrate.TTSVoice(),
				"input":           voice.ProofSentence,
				"response_format": "wav",
			})
			if err != nil {
				return nil, err
			}
			out, code, err := curl(nil, []string{
				"-sf", "--max-time", "60", "-X", "POST", voice.TTS.RouteURL(),
				"-H", "Content-Type: application/json", "-d", string(body),
			})
			if cerr := classifyVoiceCurl(code, err); cerr != nil {
				return nil, cerr
			}
			return out, nil
		},
		Transcribe: func(wav []byte) (string, error) {
			out, code, err := curl(wav, []string{
				"-sf", "--max-time", "120", voice.STT.RouteURL(),
				"-F", "file=@-;filename=probe.wav;type=audio/wav",
				"-F", "response_format=json",
			})
			if cerr := classifyVoiceCurl(code, err); cerr != nil {
				return "", cerr
			}
			var reply struct {
				Text string `json:"text"`
			}
			if jerr := json.Unmarshal(out, &reply); jerr != nil {
				return "", fmt.Errorf("unreadable reply: %w", jerr)
			}
			return reply.Text, nil
		},
		Wait: wait,
	}
}

// liveVoiceDriver is voiceDriver on the host: probeCurl from the helper image, and a
// wait that ends early when ctx is cancelled.
func liveVoiceDriver(ctx context.Context) voice.Driver {
	helper := orchestrate.EmbedImage() // probe helper, never a pin (spec §7.1)
	return voiceDriver(
		func(stdin []byte, args []string) ([]byte, int, error) {
			return probeCurl(ctx, helper, stdin, args)
		},
		func() {
			select {
			case <-ctx.Done():
			case <-time.After(voiceRetryDelay):
			}
		},
	)
}

// liveVoiceProof is the spoken round trip against the running units.
func liveVoiceProof(ctx context.Context) verify.Proof {
	return voice.Prove(liveVoiceDriver(ctx))
}

// voiceInstallProof maps the verdict onto install's two states. A Reject refuses the
// install as a Fail does: the operator opted in, the units were just started, and a
// pair still unreachable after the bounded cold start is a broken install.
func voiceInstallProof(p verify.Proof) install.Proof {
	if p.Status == verify.Pass {
		return install.Proof{Status: preflight.StatusPass, Detail: p.Detail}
	}
	return install.Proof{Status: preflight.StatusFail, Detail: p.Detail}
}
