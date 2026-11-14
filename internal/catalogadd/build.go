package catalogadd

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/gguf"
)

const (
	// DefaultCtxCap is the ceiling on a derived default_ctx. It is villa policy for
	// an entry it has not measured, not a property of the model: the lowest default
	// of any chat entry the seed qualified for real use. A smaller value fails safe,
	// because the fit guard refuses an entry that does not fit and the operator can
	// raise it with --ctx.
	DefaultCtxCap = 32768

	// KVBytesPerElem is the KV cache element width of an added entry: villa renders
	// no --cache-type-k/-v, so llama-server serves an f16 cache, and every seed entry
	// carries 2.
	KVBytesPerElem = 2
)

var (
	idRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	quantRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)
	// fileQuantRe finds a quant token at the END of a file stem, after a separator.
	// A token mid-name (Q6_K-model) is not a quant suffix and must not be guessed at.
	fileQuantRe = regexp.MustCompile(`(?i)(?:^|[-_.])((?:UD-)?(?:IQ[1-4]_[A-Z0-9]+|Q[2-8]_(?:K(?:_[SMLX]+)?|[01])|BF16|F16|F32|MXFP4))$`)
	slugRe      = regexp.MustCompile(`[^a-z0-9._-]+`)
	sha256Re    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Overrides are the operator's flags. A zero field means "derive it".
type Overrides struct {
	ID          string
	Quant       string
	DisplayName string
	Ctx         int
}

// Names are the three identity fields of an entry, which are known from the URL and
// the flags alone, before anything is downloaded.
type Names struct {
	ID          string
	Quant       string
	DisplayName string
}

// FileFacts are what the pull learned about the file on disk.
type FileFacts struct {
	SHA256    string
	SizeBytes uint64
}

// HeaderFacts are what the GGUF header says: the fit geometry, and the trained
// context length when the header carries one (zero means it does not).
type HeaderFacts struct {
	Geometry   gguf.Geometry
	TrainedCtx uint64
}

// ResolveNames derives id, quant and display name from the file name, or takes the
// operator's override, and refuses rather than guesses when neither yields a valid
// value.
func ResolveNames(src Source, ov Overrides) (Names, error) {
	stem := strings.TrimSuffix(src.Filename, ".gguf")

	id := ov.ID
	if id == "" {
		id = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(stem), "-"), "-")
	}
	if !idRe.MatchString(id) {
		return Names{}, fmt.Errorf("--id %q is not usable: an id is 1 to 64 characters of a-z, 0-9, '.', '_' and '-', starting with a letter or digit", id)
	}

	quant := ov.Quant
	if quant == "" {
		if m := fileQuantRe.FindStringSubmatch(stem); m != nil {
			quant = strings.ToUpper(m[1])
		}
	}
	if quant == "" {
		return Names{}, fmt.Errorf("--quant is required: %q does not end in a quant token (Q6_K, UD-Q4_K_XL, IQ4_XS, Q8_0, BF16, ...)", src.Filename)
	}
	if !quantRe.MatchString(quant) {
		return Names{}, fmt.Errorf("--quant %q is not usable: letters, digits, '_' and '-' only, up to 32 characters", quant)
	}

	name := ov.DisplayName
	if name == "" {
		name = stem
	}
	if len(name) > 120 || strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return Names{}, errors.New("--name must be at most 120 characters with no control characters")
	}
	return Names{ID: id, Quant: quant, DisplayName: name}, nil
}

// HeaderFactsOf reads the fit geometry from a header and refuses one villa cannot
// size. A header with no growing KV cache would give a zero KV term and an entry
// that fits every context, so it is an error here, not a zero.
func HeaderFactsOf(h gguf.Header) (HeaderFacts, error) {
	g, err := h.Geometry()
	if err != nil {
		return HeaderFacts{}, fmt.Errorf("the GGUF header cannot be sized: %w", err)
	}
	if g.KVLayers <= 0 || g.HeadCountKV <= 0 || g.KeyLength <= 0 {
		return HeaderFacts{}, fmt.Errorf("the GGUF header gives kv_layers=%d head_count_kv=%d key_length=%d; each must be positive for villa to size its KV cache", g.KVLayers, g.HeadCountKV, g.KeyLength)
	}
	ctx, _ := h.Uint(h.Arch() + ".context_length")
	return HeaderFacts{Geometry: g, TrainedCtx: ctx}, nil
}

// Build assembles the entry. Notes name every value that was derived by policy
// rather than read, so the operator sees them in the verb's output.
func Build(src Source, names Names, f FileFacts, hf HeaderFacts, ov Overrides) (catalog.Model, []string, error) {
	if !sha256Re.MatchString(f.SHA256) {
		return catalog.Model{}, nil, fmt.Errorf("the file's sha256 %q is not 64 lowercase hex digits", f.SHA256)
	}
	if f.SizeBytes == 0 {
		return catalog.Model{}, nil, errors.New("the file is empty")
	}
	ctx, notes, err := defaultCtx(hf, ov)
	if err != nil {
		return catalog.Model{}, nil, err
	}

	g := hf.Geometry
	m := catalog.Model{
		ID:          names.ID,
		DisplayName: names.DisplayName,
		Quant:       names.Quant,

		WeightBytes:    f.SizeBytes,
		NLayers:        g.KVLayers,
		NKVHeads:       g.HeadCountKV,
		HeadDim:        g.KeyLength,
		KVBytesPerElem: KVBytesPerElem,

		DefaultCtx: ctx,

		// Unqualified: nothing in the file says this model is safe to auto-select on
		// unified memory, so the flag stays false until the operator sets it.
		UnifiedMemorySafe: false,

		Shards: []catalog.Shard{{
			URL:       src.URL,
			Filename:  src.Filename,
			SHA256:    f.SHA256,
			SizeBytes: f.SizeBytes,
		}},
	}
	if g.HasSlidingWindow() {
		m.SWA = &catalog.SlidingWindow{NLayers: g.SWALayers, NKVHeads: g.SWAHeadCountKV, HeadDim: g.SWAKeyLength, Window: g.SWAWindow}
	}
	return m, notes, nil
}

// defaultCtx resolves default_ctx: the flag, else the trained context capped at
// DefaultCtxCap, and refuses a flag the model was not trained for.
func defaultCtx(hf HeaderFacts, ov Overrides) (int, []string, error) {
	switch {
	case ov.Ctx < 0:
		return 0, nil, fmt.Errorf("--ctx must be a positive number of tokens, got %d", ov.Ctx)
	case ov.Ctx > 0:
		if hf.TrainedCtx > 0 && uint64(ov.Ctx) > hf.TrainedCtx {
			return 0, nil, fmt.Errorf("--ctx %d is above the %d tokens the header says the model was trained for", ov.Ctx, hf.TrainedCtx)
		}
		return ov.Ctx, nil, nil
	case hf.TrainedCtx == 0:
		return 0, nil, errors.New("--ctx is required: the GGUF header carries no context_length to derive default_ctx from")
	case hf.TrainedCtx > DefaultCtxCap:
		return DefaultCtxCap, []string{fmt.Sprintf("default_ctx %d: the header's trained context is %d, capped at villa's %d for an unmeasured entry (pass --ctx to change it)", DefaultCtxCap, hf.TrainedCtx, DefaultCtxCap)}, nil
	default:
		return int(hf.TrainedCtx), []string{fmt.Sprintf("default_ctx %d: the header's trained context", hf.TrainedCtx)}, nil
	}
}
