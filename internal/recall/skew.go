// skew.go is THE single implementation of the D-10 embedding-skew comparison:
// does the recorded recall-index embedding identity (recall-state.json's
// EmbeddingModel/EmbeddingDim stamps) still match the configured one? It is
// shared by the status read-model (Plan 23-01: the Report's embedding_skew WARN
// surface) and the recall-index refusal / install WARN guards (Plan 23-04) —
// consumers compare against the typed states below and NEVER re-roll the
// comparison (a duplicate would be a drift bug).
//
// Typed-Unknown discipline (D-10, the blockOnNewerStore "not recorded ⇒ no
// alarm" convention): an empty recorded EmbeddingModel means there is no stamped
// truth to compare against — the verdict is SkewUnknown, never an alarm and
// never a fabricated match. PURE: no I/O, no clock, never panics.
package recall

// SkewState is the typed verdict of the embedding-skew comparison.
type SkewState string

const (
	// SkewUnknown means no embedding identity has been recorded yet (empty
	// EmbeddingModel stamp) — the comparison is unevaluable. Distinct from
	// Match: an unevaluated comparison must never render as a green "ok".
	SkewUnknown SkewState = "unknown"
	// SkewMatch means the recorded model AND dimension both equal the config.
	SkewMatch SkewState = "match"
	// SkewMismatch means the recorded identity confidently diverges from the
	// config (model OR dimension differ) — indexing into / retrieving from the
	// existing collection would be corrupt.
	SkewMismatch SkewState = "mismatch"
)

// EmbeddingSkew compares the recorded index embedding identity in st against
// the configured cfgModel/cfgDim (D-10). Empty st.EmbeddingModel ⇒ SkewUnknown
// (nothing recorded, no alarm); exact model+dim equality ⇒ SkewMatch; any
// divergence ⇒ SkewMismatch.
func EmbeddingSkew(st State, cfgModel string, cfgDim int) SkewState {
	if st.EmbeddingModel == "" {
		return SkewUnknown
	}
	if st.EmbeddingModel == cfgModel && st.EmbeddingDim == cfgDim {
		return SkewMatch
	}
	return SkewMismatch
}
