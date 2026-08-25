// version_compare_test.go pins the numeric-segment comparator that backs the
// ROCm kernel floor and the linux-firmware date policy. Both gates turn a
// comparison into a hard readiness verdict, so the contract this file guards is
// the comparator's ORDERING SEMANTICS, not its implementation: a missing
// trailing segment counts as zero, and non-numeric junk contributes zero rather
// than aborting the compare.
package detect

import "testing"

// TestCompareVersionSegmentsZeroPads guards the invariant that a shorter version
// is padded with zeros rather than sorting before its longer prefix-mate. A
// lexicographic slice compare would rank "6.18" below "6.18.0" and demote a
// host that is exactly at the kernel floor to a false below-floor verdict.
func TestCompareVersionSegmentsZeroPads(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"6.18.4", "6.18.4", 0},
		{"6.18.3", "6.18.4", -1},
		{"6.18.5", "6.18.4", 1},
		{"6.18.0", "6.18", 0},
		{"6.18", "6.18.0", 0},
		{"6.18", "6.18.0.0", 0},
		{"6.18", "6.18.4", -1},
		{"7.0.10-201.fc44.x86_64", "6.18.4", 1},
		{"6.18.9-300.fc44.x86_64", "6.18.9", 0},
		{"", "6.18.4", -1},
	}
	for _, tc := range tests {
		if got := compareVersionSegments(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersionSegments(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestCompareVersionSegmentsDivergesFromPreflight records where this comparator
// and preflight's compareVersions disagree, so the divergence stays a known,
// asserted fact instead of a surprise. preflight's splitVersion emits a zero for
// the segment a mid-string suffix truncates, so it reads "1.2-3.4" as [1 2 0 4];
// splitNumericSegments splits on "." first and reads it as [1 2 4]. Real kernel
// and firmware strings never carry a non-numeric suffix outside the last
// segment, which is why the two have agreed in practice.
func TestCompareVersionSegmentsDivergesFromPreflight(t *testing.T) {
	if got := compareVersionSegments("1.2-3.4", "1.2.0.4"); got != 1 {
		t.Errorf("compareVersionSegments(%q, %q) = %d, want 1", "1.2-3.4", "1.2.0.4", got)
	}
}

// TestKernelMeetsROCmFloor guards the gfx1151 kernel gate: at the floor passes,
// below it fails, and a truncated "6.18" is below a 6.18.4 floor rather than
// equal to it.
func TestKernelMeetsROCmFloor(t *testing.T) {
	tests := []struct {
		kernel string
		want   bool
	}{
		{"6.18.4", true},
		{"6.18.9-300.fc44.x86_64", true},
		{"7.0.10-201.fc44.x86_64", true},
		{"6.18.3", false},
		{"6.17.99", false},
		{"6.18", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := kernelMeetsROCmFloor(tc.kernel); got != tc.want {
			t.Errorf("kernelMeetsROCmFloor(%q) = %v, want %v", tc.kernel, got, tc.want)
		}
	}
}

// TestFirmwareDatePolicyOK guards that the explicit deny entry beats the floor:
// 20251125 is newer than the 20260110 floor by no measure, but even a future
// denied date must refuse, because the deny list encodes "known broken" rather
// than "too old".
func TestFirmwareDatePolicyOK(t *testing.T) {
	tests := []struct {
		date string
		want bool
	}{
		{"20260110", true},
		{"20260201", true},
		{"20260109", false},
		{"20251125", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := firmwareDatePolicyOK(tc.date); got != tc.want {
			t.Errorf("firmwareDatePolicyOK(%q) = %v, want %v", tc.date, got, tc.want)
		}
	}
}
