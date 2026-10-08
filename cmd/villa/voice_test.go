package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
	"github.com/MatrixMagician/VillaStraylight/internal/voice"
)

// TestClassifyVoiceCurl: an HTTP error status means the unit was reached and
// answered, which the proof may call a Fail; every other non-zero outcome (a refused
// connection, a timeout, a helper that never started) means nothing was measured,
// which must stay a Reject.
func TestClassifyVoiceCurl(t *testing.T) {
	ran := errors.New("exit status N")
	for _, tc := range []struct {
		name        string
		code        int
		err         error
		wantErr     bool
		unreachable bool
	}{
		{"exit 0", 0, nil, false, false},
		{"HTTP error status (22)", 22, ran, true, false},
		{"could not resolve host (6)", 6, ran, true, true},
		{"connection refused (7)", 7, ran, true, true},
		{"timed out (28)", 28, ran, true, true},
		{"curl absent in the helper (127)", 127, ran, true, true},
		{"helper never started (-1)", -1, ran, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyVoiceCurl(tc.code, tc.err)
			if (err != nil) != tc.wantErr {
				t.Fatalf("classifyVoiceCurl(%d) = %v, want an error: %v", tc.code, err, tc.wantErr)
			}
			if got := errors.Is(err, voice.ErrUnreachable); got != tc.unreachable {
				t.Errorf("classifyVoiceCurl(%d) unreachable = %v, want %v (err %v)", tc.code, got, tc.unreachable, err)
			}
		})
	}
}

// curlCall is one request the voice driver handed the curl seam.
type curlCall struct {
	stdin []byte
	args  []string
}

// fakeVoiceCurl answers by URL: the TTS route returns ttsOut with ttsCode, the STT
// route sttOut with sttCode. Every call is recorded.
type fakeVoiceCurl struct {
	ttsOut  []byte
	ttsCode int
	sttOut  []byte
	sttCode int
	calls   []curlCall
}

func (f *fakeVoiceCurl) run(stdin []byte, args []string) ([]byte, int, error) {
	f.calls = append(f.calls, curlCall{stdin: stdin, args: args})
	out, code := f.sttOut, f.sttCode
	if contains(args, voice.TTS.RouteURL()) {
		out, code = f.ttsOut, f.ttsCode
	}
	if code != 0 {
		return out, code, errors.New("curl failed")
	}
	return out, 0, nil
}

// argAfter returns the argument following flag, or "".
func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// TestVoiceDriverRoundTrip drives voice.Prove through the live driver's request
// shapes with only curl faked: the sentence goes to villa-tts's speech route as
// JSON naming the rendered model and voice, and the audio that comes back goes to
// villa-stt's transcription route on stdin.
func TestVoiceDriverRoundTrip(t *testing.T) {
	wav := []byte("RIFF....WAVEfmt ")

	t.Run("a faithful transcript passes", func(t *testing.T) {
		curl := &fakeVoiceCurl{ttsOut: wav, sttOut: []byte(`{"text":" The quick brown fox jumps over the lazy dog, and the small cat sleeps in the warm sun."}`)}
		p := voice.Prove(voiceDriver(curl.run, nil))
		if p.Status != verify.Pass {
			t.Fatalf("status = %v, want pass (detail %q)", p.Status, p.Detail)
		}
		if len(curl.calls) != 2 {
			t.Fatalf("want one speak and one transcribe, got %d calls", len(curl.calls))
		}

		speak := curl.calls[0]
		if speak.stdin != nil {
			t.Errorf("the speak leg sends its body with -d, not on stdin")
		}
		if !contains(speak.args, voice.TTS.RouteURL()) || argAfter(speak.args, "-X") != "POST" {
			t.Errorf("the speak leg must POST to %s; args = %q", voice.TTS.RouteURL(), speak.args)
		}
		var body map[string]string
		if err := json.Unmarshal([]byte(argAfter(speak.args, "-d")), &body); err != nil {
			t.Fatalf("the speak body is not JSON: %v; args = %q", err, speak.args)
		}
		want := map[string]string{
			"model":           orchestrate.TTSModel(),
			"voice":           orchestrate.TTSVoice(),
			"input":           voice.ProofSentence,
			"response_format": "wav",
		}
		for k, v := range want {
			if body[k] != v {
				t.Errorf("speak body %q = %q, want %q", k, body[k], v)
			}
		}

		listen := curl.calls[1]
		if string(listen.stdin) != string(wav) {
			t.Errorf("the transcribe leg must pipe the spoken audio on stdin, got %q", listen.stdin)
		}
		if !contains(listen.args, voice.STT.RouteURL()) || !contains(listen.args, "file=@-;filename=probe.wav;type=audio/wav") {
			t.Errorf("the transcribe leg must post the stdin audio as the file field to %s; args = %q", voice.STT.RouteURL(), listen.args)
		}
	})

	t.Run("an unreachable villa-stt is a reject naming it", func(t *testing.T) {
		curl := &fakeVoiceCurl{ttsOut: wav, sttCode: 7}
		p := voice.Prove(voiceDriver(curl.run, nil))
		if p.Status != verify.Reject {
			t.Fatalf("status = %v, want reject (detail %q)", p.Status, p.Detail)
		}
		if !strings.Contains(p.Detail, "villa-stt could not be reached") {
			t.Errorf("detail = %q, want it to name villa-stt", p.Detail)
		}
	})

	t.Run("an error status from villa-tts is a fail", func(t *testing.T) {
		curl := &fakeVoiceCurl{ttsCode: 22}
		p := voice.Prove(voiceDriver(curl.run, nil))
		if p.Status != verify.Fail {
			t.Fatalf("status = %v, want fail (detail %q)", p.Status, p.Detail)
		}
		if !strings.Contains(p.Detail, "villa-tts answered with an error") {
			t.Errorf("detail = %q, want it to name villa-tts", p.Detail)
		}
	})

	t.Run("an unreadable transcription reply is a fail, not a reject", func(t *testing.T) {
		curl := &fakeVoiceCurl{ttsOut: wav, sttOut: []byte("<html>bad gateway</html>")}
		p := voice.Prove(voiceDriver(curl.run, nil))
		if p.Status != verify.Fail {
			t.Fatalf("status = %v, want fail (detail %q)", p.Status, p.Detail)
		}
		if !strings.Contains(p.Detail, "villa-stt answered with an error") {
			t.Errorf("detail = %q, want it to name villa-stt", p.Detail)
		}
	})
}

// TestVoiceInstallProof: install has two verdicts. A reject refuses the install as a
// fail does, because the operator opted in, the units were just started, and a pair
// that cannot be reached after the bounded cold start is a broken install.
func TestVoiceInstallProof(t *testing.T) {
	for _, tc := range []struct {
		in   verify.Status
		want preflight.Status
	}{
		{verify.Pass, preflight.StatusPass},
		{verify.Fail, preflight.StatusFail},
		{verify.Reject, preflight.StatusFail},
	} {
		got := voiceInstallProof(verify.Proof{Status: tc.in, Detail: "d"})
		if got.Status != tc.want || got.Detail != "d" {
			t.Errorf("voiceInstallProof(%v) = %+v, want status %v with the detail kept", tc.in, got, tc.want)
		}
	}
}
