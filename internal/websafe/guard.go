package websafe

// guard.go is the one entry point for guarding a text that is about to reach the model.
// The loader's fetchOne and villa's tool server both go through Guard, so the order of
// the steps has one home.
//
// Web text runs sanitize, normalize, classify, cut, fence. Recall text runs normalize,
// classify, cut, fence: a chat transcript is plain text, and the markup stripper would
// delete code the user wrote. The classifier sees the whole text, before the cut, so an
// injection placed past the cut still flags. The cut comes before the fence so the
// closing delimiter always survives it.
//
// HONESTY POSTURE: as everywhere in this package, Guard reduces and flags. A Verdict is
// never a block and the text is returned whatever it says.

import (
	"fmt"
	"unicode/utf8"
)

// Source says where a text came from, and so which steps and which fence it gets.
type Source int

const (
	// SourceWeb is a fetched page or a search snippet.
	SourceWeb Source = iota + 1
	// SourceRecall is a passage from the user's own past conversations.
	SourceRecall
)

// Guarded is the result of Guard: the fenced text, the classifier's verdict over the whole
// text and whether the cut shortened it.
type Guarded struct {
	Content   string
	Verdict   Verdict
	Truncated bool
}

// Guard runs raw through the steps for src and returns the fenced text. maxRunes cuts the
// text to that many runes before the fence, and zero or less means no cut. It fails closed:
// an unknown source or a fence with no crypto/rand nonce is an error, never a text with a
// forgeable delimiter.
func Guard(raw string, src Source, maxRunes int) (Guarded, error) {
	var spec fenceSpec
	text := raw
	switch src {
	case SourceWeb:
		spec = webFence
		text = sanitize(text)
	case SourceRecall:
		spec = recallFence
	default:
		return Guarded{}, fmt.Errorf("guard: unknown source %d", src)
	}
	text = normalize(text)
	verdict := classify(text)
	text, truncated := cutRunes(text, maxRunes)
	fenced, err := fenceWith(spec, text)
	if err != nil {
		return Guarded{}, err
	}
	return Guarded{Content: fenced, Verdict: verdict, Truncated: truncated}, nil
}

// GuardTitle defangs a result title the way fetchOne defangs a page title: sanitize,
// normalize, classify, then cut to maxRunes. It is not fenced, because a title is a
// human-facing label that must stay verbatim. The verdict is the caller's to fold into its
// own.
func GuardTitle(raw string, maxRunes int) (string, Verdict) {
	title := normalize(sanitize(raw))
	verdict := classify(title)
	title, _ = cutRunes(title, maxRunes)
	return title, verdict
}

// MergeVerdicts folds two verdicts into one: Detected is the OR and Rules the ordered
// union. It never drops content, because a Verdict is a flag.
func MergeVerdicts(a, b Verdict) Verdict { return mergeVerdicts(a, b) }

// cutRunes returns s cut to at most limit runes, on a rune boundary, and whether it cut.
// A limit of zero or less means no cut.
func cutRunes(s string, limit int) (string, bool) {
	if limit <= 0 || len(s) <= limit || utf8.RuneCountInString(s) <= limit {
		return s, false
	}
	n := 0
	for i := range s {
		if n == limit {
			return s[:i], true
		}
		n++
	}
	return s, false
}
