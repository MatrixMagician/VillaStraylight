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
