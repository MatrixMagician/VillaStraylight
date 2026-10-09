package voice

import (
	"context"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/residency"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

const liveTranscript = " The quick brown fox jumps over the lazy dog, and the small cat sleeps in the warm sun."

// offloadDeps is a residency seam set whose fold returns verdict and records what
// it was handed.
func offloadDeps(verdict inference.Verdict, folded *inference.RunningOffloadInput, folds *int) residency.Deps {
	return residency.Deps{
		GPUBusy: func() detect.Int { return detect.KnownInt(0, "idle") },
		GTTUsed: func() detect.Bytes { return detect.KnownBytes(30<<30, "test") },
		Journal: func(string) (string, bool) { return "journal", true },
		Fold: func(in inference.RunningOffloadInput) inference.Verdict {
			*folds++
			*folded = in
			return verdict
		},
	}
}

func sttTarget() residency.Target {
	return residency.Target{Service: "villa-stt.service", WeightBytes: 1624555275, SampleInterval: time.Millisecond}
}

// TestProveOnGPUFoldsWhereWhisperRan: a round trip that agrees is not enough. The
// verdict also says where whisper ran: a CPU fallback is a Fail with remediation, an
// unevaluable placement is a Reject (nothing was proven either way), and a round
// trip that did not pass is returned as it was, without a fold.
func TestProveOnGPUFoldsWhereWhisperRan(t *testing.T) {
	agrees := Driver{Speak: speaks(wav, nil), Transcribe: hears(liveTranscript, nil)}
	cases := []struct {
		name      string
		d         Driver
		verdict   inference.Verdict
		status    verify.Status
		detail    string
		wantFolds int
	}{
		{
			name:      "the round trip agrees and whisper is on the GPU",
			d:         agrees,
			verdict:   inference.Verdict{Status: inference.StatusPass, Detail: "Vulkan0 total size 1623.92 MB on the iGPU"},
			status:    verify.Pass,
			detail:    `heard "` + liveTranscript + `" (agreement 1.00); whisper on the GPU: Vulkan0 total size 1623.92 MB on the iGPU`,
			wantFolds: 1,
		},
		{
			name:      "the round trip agrees but whisper fell back to the CPU",
			d:         agrees,
			verdict:   inference.Verdict{Status: inference.StatusFail, Detail: "only a CPU buffer was loaded", Remediation: "check /dev/dri passthrough"},
			status:    verify.Fail,
			detail:    "whisper is not running on the GPU (only a CPU buffer was loaded): check /dev/dri passthrough; check `systemctl --user status villa-stt.service`",
			wantFolds: 1,
		},
		{
			name:      "the round trip agrees but the placement could not be read",
			d:         agrees,
			verdict:   inference.Verdict{Status: inference.StatusWarn, Detail: "journal empty"},
			status:    verify.Reject,
			detail:    "could not tell where whisper runs (journal empty); check `systemctl --user status villa-stt.service`",
			wantFolds: 1,
		},
		{
			name:    "villa-stt unreachable is the round trip's Reject, not a fold",
			d:       Driver{Speak: speaks(wav, nil), Transcribe: hears("", unreachable("villa-stt"))},
			verdict: inference.Verdict{Status: inference.StatusPass},
			status:  verify.Reject,
			detail:  "villa-stt could not be reached (villa-stt: curl exit 7: voice service unreachable); check `systemctl --user status villa-stt.service`",
		},
		{
			name:    "words that disagree are the round trip's Fail, not a fold",
			d:       Driver{Speak: speaks(wav, nil), Transcribe: hears("the quick brown fox", nil)},
			verdict: inference.Verdict{Status: inference.StatusPass},
			status:  verify.Fail,
			detail:  `heard "the quick brown fox" for "the quick brown fox jumps over the lazy dog and the small cat sleeps in the warm sun" (agreement 0.36); check ` + "`systemctl --user status villa-stt.service villa-tts.service`",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var folded inference.RunningOffloadInput
			folds := 0
			got := ProveOnGPU(context.Background(), tc.d, offloadDeps(tc.verdict, &folded, &folds), sttTarget())
			if got.Status != tc.status || got.Detail != tc.detail {
				t.Errorf("ProveOnGPU = {%v %q}\nwant          {%v %q}", got.Status, got.Detail, tc.status, tc.detail)
			}
			if folds != tc.wantFolds {
				t.Errorf("folds = %d, want %d", folds, tc.wantFolds)
			}
		})
	}
}

// TestProveOnGPUSamplesBusyWhileWhisperTranscribes: the busy reading the fold gets
// is the one taken while the transcription runs, and the fold reads villa-stt's
// journal against the whisper model's footprint.
func TestProveOnGPUSamplesBusyWhileWhisperTranscribes(t *testing.T) {
	transcribing := make(chan struct{})
	done := make(chan struct{})
	d := Driver{
		Speak: speaks(wav, nil),
		Transcribe: func([]byte) (string, error) {
			close(transcribing)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
			return ProofSentence, nil
		},
	}
	var folded inference.RunningOffloadInput
	var journaled string
	folds := 0
	deps := offloadDeps(inference.Verdict{Status: inference.StatusPass}, &folded, &folds)
	deps.GPUBusy = func() detect.Int {
		select {
		case <-transcribing:
			select {
			case <-done:
			default:
				close(done)
			}
			return detect.KnownInt(63, "during")
		default:
			return detect.KnownInt(0, "before")
		}
	}
	deps.Journal = func(service string) (string, bool) { journaled = service; return "journal", true }

	if got := ProveOnGPU(context.Background(), d, deps, sttTarget()); got.Status != verify.Pass {
		t.Fatalf("ProveOnGPU = %v %q, want pass", got.Status, got.Detail)
	}
	if !folded.GPUBusyPercent.Known || folded.GPUBusyPercent.Value != 63 {
		t.Errorf("fold busy = %+v, want 63 (the reading during the transcription)", folded.GPUBusyPercent)
	}
	if journaled != "villa-stt.service" || folded.WeightBytes != 1624555275 {
		t.Errorf("fold read journal %q at weight %d, want villa-stt.service at 1624555275", journaled, folded.WeightBytes)
	}
}

// TestProveOnGPUDeadlineIsAReject: a round trip still running at the protocol's
// deadline measured nothing, so it is a Reject naming villa-stt, never a Fail.
func TestProveOnGPUDeadlineIsAReject(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	d := Driver{
		Speak: speaks(wav, nil),
		Transcribe: func([]byte) (string, error) {
			select {
			case <-release:
			case <-time.After(2 * time.Second):
			}
			return ProofSentence, nil
		},
	}
	var folded inference.RunningOffloadInput
	folds := 0
	tgt := sttTarget()
	tgt.ReadyTimeout = 20 * time.Millisecond
	got := ProveOnGPU(context.Background(), d, offloadDeps(inference.Verdict{Status: inference.StatusPass}, &folded, &folds), tgt)
	want := "the round trip did not finish (generation probe did not complete before timeout (possible load_tensors hang or CPU-fallback stall)); check `systemctl --user status villa-stt.service`"
	if got.Status != verify.Reject || got.Detail != want {
		t.Errorf("ProveOnGPU = {%v %q}\nwant          {reject %q}", got.Status, got.Detail, want)
	}
}
