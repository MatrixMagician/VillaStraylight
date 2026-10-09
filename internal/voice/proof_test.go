package voice

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

var wav = []byte("RIFF....WAVEfmt ")

func speaks(b []byte, err error) func() ([]byte, error) {
	return func() ([]byte, error) { return b, err }
}

func hears(text string, err error) func([]byte) (string, error) {
	return func([]byte) (string, error) { return text, err }
}

func unreachable(unit string) error {
	return fmt.Errorf("%s: curl exit 7: %w", unit, ErrUnreachable)
}

// TestProveTruthTable is one attempt of every row: which leg failed, and how,
// decides the verdict and the unit its remediation names.
func TestProveTruthTable(t *testing.T) {
	cases := []struct {
		name   string
		d      Driver
		status verify.Status
		detail string
	}{
		{
			// The transcript whisper returned for ProofSentence on the dev host, three
			// runs out of three, punctuation and all.
			name:   "both legs answer and the words agree (the live transcript)",
			d:      Driver{Speak: speaks(wav, nil), Transcribe: hears(" The quick brown fox jumps over the lazy dog, and the small cat sleeps in the warm sun.", nil)},
			status: verify.Pass,
			detail: `heard " The quick brown fox jumps over the lazy dog, and the small cat sleeps in the warm sun." (agreement 1.00)`,
		},
		{
			name:   "villa-tts unreachable",
			d:      Driver{Speak: speaks(nil, unreachable("villa-tts")), Transcribe: hears("", nil)},
			status: verify.Reject,
			detail: "villa-tts could not be reached (villa-tts: curl exit 7: voice service unreachable); check `systemctl --user status villa-tts.service`",
		},
		{
			name:   "villa-tts answers with an error",
			d:      Driver{Speak: speaks(nil, errors.New("HTTP 500")), Transcribe: hears("", nil)},
			status: verify.Fail,
			detail: "villa-tts answered with an error (HTTP 500); check `systemctl --user status villa-tts.service`",
		},
		{
			name:   "villa-tts returns no audio",
			d:      Driver{Speak: speaks(nil, nil), Transcribe: hears("", nil)},
			status: verify.Fail,
			detail: "villa-tts returned no audio; check `systemctl --user status villa-tts.service`",
		},
		{
			name:   "villa-stt unreachable",
			d:      Driver{Speak: speaks(wav, nil), Transcribe: hears("", unreachable("villa-stt"))},
			status: verify.Reject,
			detail: "villa-stt could not be reached (villa-stt: curl exit 7: voice service unreachable); check `systemctl --user status villa-stt.service`",
		},
		{
			name:   "villa-stt answers with an error",
			d:      Driver{Speak: speaks(wav, nil), Transcribe: hears("", errors.New("invalid JSON"))},
			status: verify.Fail,
			detail: "villa-stt answered with an error (invalid JSON); check `systemctl --user status villa-stt.service`",
		},
		{
			name:   "the words disagree",
			d:      Driver{Speak: speaks(wav, nil), Transcribe: hears("the quick brown fox", nil)},
			status: verify.Fail,
			detail: `heard "the quick brown fox" for "the quick brown fox jumps over the lazy dog and the small cat sleeps in the warm sun" (agreement 0.36); check ` + "`systemctl --user status villa-stt.service villa-tts.service`",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Prove(tc.d)
			if got.Status != tc.status || got.Detail != tc.detail {
				t.Errorf("Prove = {%v %q}\nwant    {%v %q}", got.Status, got.Detail, tc.status, tc.detail)
			}
		})
	}
}

// TestProveRetriesOnlyWhatIsNotYetAnAnswer: a service still starting is refused or
// answers with an error, and either can clear on a later attempt, so both are
// retried up to Attempts times with a Wait between. A pass, a mismatched
// transcript and empty audio are answers about the pair and end the proof at once.
func TestProveRetriesOnlyWhatIsNotYetAnAnswer(t *testing.T) {
	cases := []struct {
		name          string
		speak         func() ([]byte, error)
		transcribe    func([]byte) (string, error)
		wantCalls     int
		wantWaits     int
		wantLastState verify.Status
	}{
		{"unreachable every time", speaks(nil, unreachable("villa-tts")), hears("", nil), Attempts, Attempts - 1, verify.Reject},
		{"an error every time", speaks(wav, nil), hears("", errors.New("HTTP 503")), Attempts, Attempts - 1, verify.Fail},
		{"a mismatch is an answer", speaks(wav, nil), hears("something else entirely", nil), 1, 0, verify.Fail},
		{"empty audio is an answer", speaks(nil, nil), hears("", nil), 1, 0, verify.Fail},
		{"a pass is an answer", speaks(wav, nil), hears(ProofSentence, nil), 1, 0, verify.Pass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls, waits := 0, 0
			d := Driver{
				Speak:      func() ([]byte, error) { calls++; return tc.speak() },
				Transcribe: tc.transcribe,
				Wait:       func() { waits++ },
			}
			got := Prove(d)
			if calls != tc.wantCalls || waits != tc.wantWaits || got.Status != tc.wantLastState {
				t.Errorf("attempts=%d waits=%d status=%v, want %d/%d/%v", calls, waits, got.Status, tc.wantCalls, tc.wantWaits, tc.wantLastState)
			}
		})
	}
}

// TestProveRecoversFromAColdStart: the first attempts find villa-stt not yet
// listening, then it answers; the verdict is the answer, not the early refusals.
func TestProveRecoversFromAColdStart(t *testing.T) {
	n := 0
	d := Driver{
		Speak: speaks(wav, nil),
		Transcribe: func([]byte) (string, error) {
			n++
			if n < 3 {
				return "", unreachable("villa-stt")
			}
			return ProofSentence, nil
		},
	}
	if got := Prove(d); got.Status != verify.Pass || n != 3 {
		t.Errorf("Prove = %v after %d attempts, want pass after 3", got.Status, n)
	}
}

// TestWordsNormalizesWhatWhisperAddsAndDrops: case, punctuation and spacing differ
// between the sentence spoken and the transcript returned without changing a word.
func TestWordsNormalizesWhatWhisperAddsAndDrops(t *testing.T) {
	got := Words("  The quick-brown FOX, jumps... over 2 dogs!\n")
	want := []string{"the", "quick", "brown", "fox", "jumps", "over", "2", "dogs"}
	if !slices.Equal(got, want) {
		t.Errorf("Words = %q, want %q", got, want)
	}
	if got := Words(" .,! "); len(got) != 0 {
		t.Errorf("Words of punctuation = %q, want none", got)
	}
}

// TestMinAgreementIsTheBoundary pins the threshold through what it decides: twelve
// of the sentence's eighteen words in order score exactly 0.80 and pass, eleven score
// 0.76 and fail. A threshold anywhere else in (0.76, 0.80] or above 0.80 changes one of
// the two verdicts.
func TestMinAgreementIsTheBoundary(t *testing.T) {
	words := Words(ProofSentence)
	if len(words) != 18 {
		t.Fatalf("ProofSentence has %d words, the boundary below assumes 18", len(words))
	}
	twelve := strings.Join(words[:12], " ")
	eleven := strings.Join(words[:11], " ")

	p := Prove(Driver{Speak: speaks(wav, nil), Transcribe: hears(twelve, nil)})
	if p.Status != verify.Pass || !strings.HasSuffix(p.Detail, "(agreement 0.80)") {
		t.Errorf("twelve words: status %v detail %q, want Pass at agreement 0.80", p.Status, p.Detail)
	}
	p = Prove(Driver{Speak: speaks(wav, nil), Transcribe: hears(eleven, nil)})
	if p.Status != verify.Fail || !strings.Contains(p.Detail, "(agreement 0.76)") {
		t.Errorf("eleven words: status %v detail %q, want Fail at agreement 0.76", p.Status, p.Detail)
	}
}

// TestAgreementMergedCompoundWordCostsTwo records why the sentence has no compound:
// on the dev host whisper wrote "river bank" as "riverbank", which drops two words
// from the common subsequence and scored 0.88 against the thirteen-word sentence of the
// first live run, one dropped word above the threshold.
func TestAgreementMergedCompoundWordCostsTwo(t *testing.T) {
	spoken := "the quick brown fox jumps over the lazy dog near the river bank"
	heard := " The quick brown fox jumps over the lazy dog near the riverbank.\n"
	if got := Agreement(spoken, heard); got != 0.88 {
		t.Errorf("Agreement = %v, want 0.88 (2*11 over 13+12)", got)
	}
}

// TestAgreementIsTwiceTheCommonSubsequenceOverBothLengths: 2*LCS/(len+len) over
// the word lists, so a dropped word, an extra word and a reordered word each cost
// agreement, and nothing heard scores zero.
func TestAgreementIsTwiceTheCommonSubsequenceOverBothLengths(t *testing.T) {
	cases := []struct {
		spoken, heard string
		want          float64
	}{
		{"a b c d", "A, b. C d!", 1},
		{"a b c d", "a b d", 6.0 / 7.0},
		{"a b c d", "a b x c d", 8.0 / 9.0},
		{"a b c d", "d c b a", 2.0 / 8.0},
		{"a b c d", "", 0},
		{"", "", 0},
	}
	for _, tc := range cases {
		if got := Agreement(tc.spoken, tc.heard); got != tc.want {
			t.Errorf("Agreement(%q, %q) = %v, want %v", tc.spoken, tc.heard, got, tc.want)
		}
	}
}
