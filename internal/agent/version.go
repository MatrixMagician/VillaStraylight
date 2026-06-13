package agent

// version.go is a VERBATIM clone of internal/preflight/floors.go's
// compareVersions + splitVersion (lines 141-206). It compares the pinned policy
// version against the installed `crush --version` output for presence/drift
// checks. Cloned (not imported) so internal/agent takes no dependency on the
// preflight package and the comparator stays unexported/internal here. The
// distro-suffix tolerance is irrelevant for SemVer crush tags but preserved
// verbatim so the proven behavior is identical.

// compareVersions compares two dotted numeric version strings (e.g. "0.76.0").
// It returns -1 if a < b, 0 if equal, +1 if a > b. Non-numeric or extra segments
// are tolerated: leading numeric runs are compared and anything unparseable in a
// segment is treated as 0 so a malformed version never panics — it just sorts low.
// Tolerates a leading "v" (e.g. "v0.76.0") because splitVersion stops each
// segment at the first non-digit, so the "v" prefix contributes no digits.
func compareVersions(a, b string) int {
	as := splitVersion(a)
	bs := splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

// splitVersion turns "0.76.0-rc1" (or "v0.76.0") into [0 76 0], stopping each
// segment at the first non-digit so suffixes/prefixes don't break the compare.
func splitVersion(v string) []int {
	var out []int
	cur := 0
	inNum := false
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if ch >= '0' && ch <= '9' {
			cur = cur*10 + int(ch-'0')
			inNum = true
			continue
		}
		if ch == '.' {
			out = append(out, cur)
			cur = 0
			inNum = false
			continue
		}
		// Any other character (e.g. '-' in "0.76.0-rc1", or a leading 'v') ends
		// the meaningful portion of a dotted version for comparison.
		if inNum {
			out = append(out, cur)
			cur = 0
			inNum = false
		}
		// Skip to the next '.' boundary; subsequent non-numeric junk is ignored.
		for i+1 < len(v) && v[i+1] != '.' {
			i++
		}
	}
	if inNum {
		out = append(out, cur)
	}
	return out
}
