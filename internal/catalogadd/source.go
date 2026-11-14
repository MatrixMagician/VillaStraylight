// Package catalogadd is the pure core of `villa catalog add` (ADR-0041): from a
// commit-pinned model URL, the facts of the file the pull produced and the facts of
// its GGUF header, it builds a catalog entry and merges it into an external catalog.
//
// INVARIANT: no value in an entry is guessed. Each field is read from the file or its
// header, set to the field's documented absence value, or required of the operator,
// and the table in ADR-0041 names which. The package does no host I/O of its own;
// Run drives every effect through Deps, which is what makes the whole verb testable
// off-hardware with a fixture GGUF header.
//
// source.go is the trust boundary on the input: what a source URL may be.
package catalogadd

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Refusals a caller (and a test) matches with errors.Is. The wrapped message says
// what to do about it.
var (
	// ErrNotPinned means the URL's revision is not a full commit id, so it names a
	// moving target and the sha256 recorded for it could not be reproduced elsewhere.
	ErrNotPinned = errors.New("the URL is not pinned to a commit")
	// ErrSharded means the file is one numbered shard of a split model.
	ErrSharded = errors.New("the URL names one shard of a split model")
	// ErrUnsupportedSource means the URL is outside the one accepted shape.
	ErrUnsupportedSource = errors.New("unsupported model URL")
)

const sourceHost = "huggingface.co"

var (
	commitRe  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)
	shardRe   = regexp.MustCompile(`-\d{5}-of-\d{5}\.gguf$`)
)

// Source is a validated, canonical model source.
type Source struct {
	// URL is the canonical https URL, rebuilt from the validated parts, never the
	// operator's string.
	URL string
	// Repo is "<org>/<repo>".
	Repo string
	// Commit is the 40-hex commit id the URL is pinned to.
	Commit string
	// Path is the file's path inside the repository at that commit.
	Path string
	// Filename is the last path segment, the bare name written into the models dir.
	Filename string
}

// ParseSource accepts exactly one URL shape and refuses everything else (ADR-0041):
//
//	https://huggingface.co/<org>/<repo>/resolve/<40 lowercase hex>/<path>.gguf
//
// where every segment is [A-Za-z0-9._+-]+ and the file is a single GGUF, not a
// numbered shard. It never repairs input: a query string, an escape or a branch name
// is refused, because the canonical URL it returns is what villa will record and
// request, and it must be the string that was validated.
func ParseSource(raw string) (Source, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Source{}, fmt.Errorf("%w: the URL is empty", ErrUnsupportedSource)
	}
	for _, r := range raw {
		if r <= ' ' || r == 0x7f {
			return Source{}, fmt.Errorf("%w: the URL contains whitespace or a control character", ErrUnsupportedSource)
		}
	}
	if strings.Contains(raw, "%") {
		return Source{}, fmt.Errorf("%w: percent-escapes are not accepted; give the plain path", ErrUnsupportedSource)
	}
	if strings.ContainsAny(raw, "?") {
		return Source{}, fmt.Errorf("%w: remove the query string (villa records the canonical URL it was given)", ErrUnsupportedSource)
	}
	if strings.ContainsAny(raw, "#") {
		return Source{}, fmt.Errorf("%w: remove the fragment", ErrUnsupportedSource)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Source{}, fmt.Errorf("%w: %v", ErrUnsupportedSource, err)
	}
	if u.Scheme != "https" {
		return Source{}, fmt.Errorf("%w: the scheme must be https", ErrUnsupportedSource)
	}
	if u.User != nil {
		return Source{}, fmt.Errorf("%w: userinfo is not accepted", ErrUnsupportedSource)
	}
	if u.Port() != "" {
		return Source{}, fmt.Errorf("%w: a port is not accepted", ErrUnsupportedSource)
	}
	if !strings.EqualFold(u.Host, sourceHost) {
		return Source{}, fmt.Errorf("%w: only %s is accepted, got host %q", ErrUnsupportedSource, sourceHost, u.Host)
	}

	segs := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(segs) < 5 {
		return Source{}, fmt.Errorf("%w: the path must be /<org>/<repo>/resolve/<commit>/<file>.gguf", ErrUnsupportedSource)
	}
	for _, s := range segs {
		if s == "." || s == ".." || !segmentRe.MatchString(s) {
			return Source{}, fmt.Errorf("%w: path segment %q is not accepted (letters, digits and . _ + - only)", ErrUnsupportedSource, s)
		}
	}
	if segs[2] != "resolve" {
		return Source{}, fmt.Errorf("%w: the path must use /resolve/<commit>/, not /%s/ (the browser view is not a download URL)", ErrUnsupportedSource, segs[2])
	}
	if !commitRe.MatchString(segs[3]) {
		return Source{}, fmt.Errorf("%w: %q is a branch, tag or short id, which is a moving target; use the full 40-character commit id of the revision you mean", ErrNotPinned, segs[3])
	}

	file := segs[len(segs)-1]
	if !strings.HasSuffix(file, ".gguf") || len(file) == len(".gguf") {
		return Source{}, fmt.Errorf("%w: the file must be a .gguf", ErrUnsupportedSource)
	}
	if shardRe.MatchString(file) {
		return Source{}, fmt.Errorf("%w: %q; add a split model by hand with one shard entry per file", ErrSharded, file)
	}

	repo := segs[0] + "/" + segs[1]
	path := strings.Join(segs[4:], "/")
	return Source{
		URL:      "https://" + sourceHost + "/" + repo + "/resolve/" + segs[3] + "/" + path,
		Repo:     repo,
		Commit:   segs[3],
		Path:     path,
		Filename: file,
	}, nil
}
