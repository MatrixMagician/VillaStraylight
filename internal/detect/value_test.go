package detect

import (
	"encoding/json"
	"testing"
)

func TestValueKnownBytes(t *testing.T) {
	const src = "/sys/class/drm/card1/device/mem_info_gtt_total"
	b := KnownBytes(67149381632, src)
	if !b.Known {
		t.Errorf("KnownBytes: Known = false, want true")
	}
	if b.Value != 67149381632 {
		t.Errorf("KnownBytes: Value = %d, want 67149381632", b.Value)
	}
	if b.Source != src {
		t.Errorf("KnownBytes: Source = %q, want %q", b.Source, src)
	}
	if b.Raw != "" {
		t.Errorf("KnownBytes: Raw = %q, want empty", b.Raw)
	}
}

func TestValueUnknownBytes(t *testing.T) {
	const reason = "unparseable gtt_total"
	const raw = "junk\n"
	b := UnknownBytes(reason, raw)
	if b.Known {
		t.Errorf("UnknownBytes: Known = true, want false")
	}
	if b.Value != 0 {
		t.Errorf("UnknownBytes: Value = %d, want 0", b.Value)
	}
	if b.Source != reason {
		t.Errorf("UnknownBytes: Source = %q, want %q", b.Source, reason)
	}
	if b.Raw != raw {
		t.Errorf("UnknownBytes: Raw = %q, want %q", b.Raw, raw)
	}
}

func TestValueUnknownBytesJSONOmitsRaw(t *testing.T) {
	b := UnknownBytes("unparseable gtt_total", "junk\n")
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	got := string(data)
	want := `{"value":0,"known":false,"source":"unparseable gtt_total"}`
	if got != want {
		t.Errorf("json.Marshal(Unknown Bytes) = %s, want %s", got, want)
	}
}

func TestValueKnownConstructorsAllTypes(t *testing.T) {
	if s := KnownStr("gfx1151", "rocminfo"); !s.Known || s.Value != "gfx1151" || s.Source != "rocminfo" {
		t.Errorf("KnownStr produced %+v", s)
	}
	if i := KnownInt(2, "/dev/dri"); !i.Known || i.Value != 2 || i.Source != "/dev/dri" {
		t.Errorf("KnownInt produced %+v", i)
	}
	if bl := KnownBool(true, "rocminfo present"); !bl.Known || !bl.Value {
		t.Errorf("KnownBool produced %+v", bl)
	}
	if s := UnknownStr("reason", "raw"); s.Known || s.Value != "" || s.Raw != "raw" {
		t.Errorf("UnknownStr produced %+v", s)
	}
	if i := UnknownInt("reason", "raw"); i.Known || i.Value != 0 || i.Raw != "raw" {
		t.Errorf("UnknownInt produced %+v", i)
	}
	if bl := UnknownBool("reason", "raw"); bl.Known || bl.Value || bl.Raw != "raw" {
		t.Errorf("UnknownBool produced %+v", bl)
	}
}

// TestOptionalSerialisesIdenticallyPerType pins the contract the generic must not
// break: each of the four spellings marshals to the same shape it always has,
// with Raw excluded and an absent Source omitted. These literals are the frozen
// wire form, written out by hand rather than derived from the code under test, so
// a change to the struct tags fails here rather than silently reshaping --json.
func TestOptionalSerialisesIdenticallyPerType(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want string
	}{
		{"known bytes", KnownBytes(1024, "/proc/meminfo:MemTotal"),
			`{"value":1024,"known":true,"source":"/proc/meminfo:MemTotal"}`},
		{"known str", KnownStr("gfx1151", "rocminfo"),
			`{"value":"gfx1151","known":true,"source":"rocminfo"}`},
		{"known int", KnownInt(2, "/dev/dri"),
			`{"value":2,"known":true,"source":"/dev/dri"}`},
		{"known bool", KnownBool(true, "rocminfo present"),
			`{"value":true,"known":true,"source":"rocminfo present"}`},
		{"unknown str drops raw", UnknownStr("rocminfo unavailable", "junk\n"),
			`{"value":"","known":false,"source":"rocminfo unavailable"}`},
		{"unknown int drops raw", UnknownInt("not sampled", "junk\n"),
			`{"value":0,"known":false,"source":"not sampled"}`},
		{"unknown bool drops raw", UnknownBool("tool missing", "junk\n"),
			`{"value":false,"known":false,"source":"tool missing"}`},
		{"empty source omitted", Bool{Known: true, Value: false},
			`{"value":false,"known":true}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("marshal = %s, want %s", data, tc.want)
			}
		})
	}
}

// TestUnknownIsNotAConfidentZero is the distinction the spine exists for: an
// undetected value must stay distinguishable from a legitimate zero, an empty
// string and a confident false. Collapsing them would let the recommender read
// an undetected envelope as an empty one and size against nothing.
func TestUnknownIsNotAConfidentZero(t *testing.T) {
	if UnknownBytes("no envelope", "") == KnownBytes(0, "measured zero") {
		t.Error("an undetected byte count compares equal to a measured zero")
	}
	if UnknownStr("no model", "") == KnownStr("", "measured empty") {
		t.Error("an undetected string compares equal to a measured empty string")
	}
	if UnknownBool("not evaluable", "") == KnownBool(false, "confidently absent") {
		t.Error("an unevaluable signal compares equal to a confident false")
	}

	// And the Known flag is what carries it, not the value.
	if u := UnknownBool("tool missing", ""); u.Known {
		t.Error("UnknownBool reports Known=true")
	}
	if k := KnownBool(false, "confidently absent"); !k.Known {
		t.Error("a confident false reports Known=false")
	}
}
