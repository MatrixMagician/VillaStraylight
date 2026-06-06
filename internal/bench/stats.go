package bench

import (
	"math"
	"sort"
)

// stats.go holds the stdlib-only aggregation helpers. There is no stats library in
// go.mod by design (the single-static-binary constraint forbids gonum) so these are
// hand-rolled over sort/math. They are PURE and panic-free: a degenerate input
// (empty / single-element) returns 0 rather than fabricating a value, mirroring the
// detect/metrics "typed-Unknown, never a fabricated number" discipline.
//
// pp and tg are ALWAYS passed as SEPARATE slices by the caller (statsOf) and are
// never concatenated here — the per-metric separation is structural, enforced by the
// caller, and these helpers simply never see a blended slice.

// median returns the middle element of xs (odd length) or the mean of the two
// middle elements (even length). It sorts a COPY so the caller's slice is untouched.
// Empty input returns 0.
func median(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// stddev returns the SAMPLE standard deviation (n-1 denominator) of xs. It returns 0
// for n<2 (a single sample has no spread; an empty slice none at all) — never NaN,
// never a panic.
func stddev(xs []float64) float64 {
	n := len(xs)
	if n < 2 {
		return 0
	}
	var mean float64
	for _, x := range xs {
		mean += x
	}
	mean /= float64(n)
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(n-1))
}
