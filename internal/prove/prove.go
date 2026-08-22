// Package prove owns the ONE prove verdict the three transactional cores gate
// their cutover on: backend swap, coding mode, and restore.
//
// Each of those packages previously declared its own character-for-character
// identical verdict type, with no per-package invariant to tell them apart, and
// the restore path carried a live adapter whose whole job was transliterating one
// into another field by field. One type deletes the adapter and gives the
// residency-proof module a single verdict to return.
//
// It imports NOTHING. That is the point: the reason each core declared its own
// copy was to stay free of internal/inference and internal/detect imports and of
// backend literals, so a shared verdict may never acquire either. The cmd tier
// composes the real verdict (readiness + a real generation probe + the residency
// proof) behind its Prove seam and maps it into this value.
package prove

// StatusPass is the success sentinel. A cutover succeeds ONLY when Verdict.Status
// equals this constant, and the cmd tier sets it ONLY when a real generation probe
// AND a positive residency proof both pass. It is a status sentinel, not a backend
// token: a ready+health-200-but-residency-FAIL verdict is NEVER success, and a
// silent CPU fallback must map to a non-pass status.
const StatusPass = "pass"

// StatusFail is the non-pass sentinel the cmd tier sets on a refusal. Any status
// other than StatusPass triggers rollback, so this constant names the common case
// rather than widening the contract.
const StatusFail = "fail"

// Verdict is the prove outcome a transactional cutover gates on.
type Verdict struct {
	// Status is the prove outcome. The cutover succeeds ONLY when Status equals
	// StatusPass; any other value triggers rollback — is-active/health-200 alone is
	// NEVER success.
	Status string
	// Detail is the human explanation carried into the Result on a non-pass verdict.
	Detail string
}

// Pass reports whether the verdict is a true offload-honest pass.
func (v Verdict) Pass() bool { return v.Status == StatusPass }
