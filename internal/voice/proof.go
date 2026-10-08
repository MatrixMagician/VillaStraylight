package voice

// proof.go is the verdict of the voice proof: villa-tts speaks ProofSentence,
// villa-stt transcribes the audio, and the words that come back are compared with
// the words that went in. One round trip proves both units, which is why voice is one
// subsystem (ADR-0029). Install, `verify voice` and `update voice` all call Prove;
// the command tier owns only the two curl legs behind Driver.
//
// A leg that could not reach its service is a Reject, never a Fail: nothing about
// the pair was measured. A leg that was reached and answered wrongly is a Fail.

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// ProofSentence is spoken by villa-tts and expected back from villa-stt. Eighteen
// common words with no compound, no homophone and no number: whisper wrote "river
// bank" as "riverbank" for an earlier sentence, which cost two words of agreement
// and left the pass one word wide.
const ProofSentence = "the quick brown fox jumps over the lazy dog and the small cat sleeps in the warm sun"

// MinAgreement is the word agreement a transcript needs to pass. At eighteen words
// it allows six dropped words and refuses seven, which the boundary test pins.
const MinAgreement = 0.8

// Attempts bounds the cold-start retries. A unit that has just started refuses
// connections or answers 5xx while its model loads.
const Attempts = 10

// ErrUnreachable marks a leg whose service could not be reached or whose probe could
// not run. Prove maps it to Reject; any other leg error is a Fail.
var ErrUnreachable = errors.New("voice service unreachable")

// Driver is the live seam. Speak returns WAV bytes for ProofSentence; Transcribe
// returns the text villa-stt heard; Wait sleeps between attempts and may be nil.
type Driver struct {
	Speak      func() ([]byte, error)
	Transcribe func(wav []byte) (string, error)
	Wait       func()
}

const (
	checkTTS  = "; check `systemctl --user status villa-tts.service`"
	checkSTT  = "; check `systemctl --user status villa-stt.service`"
	checkBoth = "; check `systemctl --user status villa-stt.service villa-tts.service`"
)

// Prove runs up to Attempts round trips. It returns at once on an answer about the
// pair (a pass, a mismatched transcript, empty audio) and retries a refusal or an
// error reply, which a unit still loading its model gives. After the last attempt it
// returns that attempt's verdict.
func Prove(d Driver) verify.Proof {
	var p verify.Proof
	for i := range Attempts {
		var retry bool
		p, retry = attempt(d)
		if !retry {
			return p
		}
		if i < Attempts-1 && d.Wait != nil {
			d.Wait()
		}
	}
	return p
}

// attempt is one round trip and whether its verdict may still change on a retry.
func attempt(d Driver) (verify.Proof, bool) {
	audio, err := d.Speak()
	if err != nil {
		return legFailure("villa-tts", err, checkTTS), true
	}
	if len(audio) == 0 {
		return verify.Proof{Status: verify.Fail, Detail: "villa-tts returned no audio" + checkTTS}, false
	}
	heard, err := d.Transcribe(audio)
	if err != nil {
		return legFailure("villa-stt", err, checkSTT), true
	}
	agreement := Agreement(ProofSentence, heard)
	if agreement < MinAgreement {
		return verify.Proof{
			Status: verify.Fail,
			Detail: fmt.Sprintf("heard %q for %q (agreement %.2f)", heard, ProofSentence, agreement) + checkBoth,
		}, false
	}
	return verify.Proof{Status: verify.Pass, Detail: fmt.Sprintf("heard %q (agreement %.2f)", heard, agreement)}, false
}

func legFailure(unit string, err error, check string) verify.Proof {
	if errors.Is(err, ErrUnreachable) {
		return verify.Proof{Status: verify.Reject, Detail: fmt.Sprintf("%s could not be reached (%v)", unit, err) + check}
	}
	return verify.Proof{Status: verify.Fail, Detail: fmt.Sprintf("%s answered with an error (%v)", unit, err) + check}
}

// Words lowercases s and splits it into words, treating every rune that is neither
// a letter nor a digit as a separator, so punctuation and spacing whisper adds or
// drops do not count against a transcript.
func Words(s string) []string {
	return strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s))
}

// Agreement is 2*LCS/(len(spoken)+len(heard)) over the two word lists: 1 for the
// same words in the same order, 0 when nothing was heard.
func Agreement(spoken, heard string) float64 {
	a, b := Words(spoken), Words(heard)
	if len(a)+len(b) == 0 {
		return 0
	}
	return float64(2*lcs(a, b)) / float64(len(a)+len(b))
}

func lcs(a, b []string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := range a {
		for j := range b {
			if a[i] == b[j] {
				cur[j+1] = prev[j] + 1
			} else {
				cur[j+1] = max(prev[j+1], cur[j])
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
