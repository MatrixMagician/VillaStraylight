package recommend

import (
	"strings"
	"testing"
)

// TestParseEnvelopeAcceptsBinarySizes guards the accepted forms: a decimal number
// with a mandatory unit, every unit binary (G is GiB, as sandbox_memory's "4g" is).
func TestParseEnvelopeAcceptsBinarySizes(t *testing.T) {
	cases := map[string]uint64{
		"64GiB":   64 << 30,
		"64G":     64 << 30,
		"64g":     64 << 30,
		"64GB":    64 << 30,
		" 48 GiB": 48 << 30,
		"1.5GiB":  3 << 29,
		"2048M":   2 << 30,
		"2048MiB": 2 << 30,
		"1T":      1 << 40,
		"1TiB":    1 << 40,
	}
	for in, want := range cases {
		got, err := ParseEnvelope(in)
		if err != nil || got != want {
			t.Errorf("ParseEnvelope(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

// TestParseEnvelopeFailsClosed guards that nothing ambiguous or implausible becomes
// an envelope: every refusal names the accepted forms.
func TestParseEnvelopeFailsClosed(t *testing.T) {
	for _, in := range []string{
		"", "   ", "64", "GiB", "abc", "0", "0GiB", "-8GiB", "+8GiB", "64XB", "6 4GiB",
		"1e3GiB", "NaNGiB", "InfGiB", "512MiB", "0.5G", "5000GiB", "99999999999999999999GiB",
	} {
		_, err := ParseEnvelope(in)
		if err == nil {
			t.Errorf("ParseEnvelope(%q) accepted; want a refusal", in)
			continue
		}
		if !strings.Contains(err.Error(), "GiB") {
			t.Errorf("ParseEnvelope(%q) error %q does not name the accepted forms", in, err)
		}
	}
}

// TestPickEnvelopeOverrideReplacesDetected guards the replace-never-add rule: a
// smaller simulated envelope shrinks the pick below the detected one, a larger one
// may grow it, and the result is marked simulated while a normal pick is not.
func TestPickEnvelopeOverrideReplacesDetected(t *testing.T) {
	cat := testCatalog()
	p := profileWithEnvelope(64 << 30)

	base := Pick(p, cat, Overrides{}, nil)
	small := Pick(p, cat, Overrides{Envelope: 16 << 30}, nil)
	large := Pick(p, cat, Overrides{Envelope: 160 << 30}, nil)

	if base.SimulatedEnvelope {
		t.Error("a normal pick is marked simulated")
	}
	if !small.SimulatedEnvelope || !large.SimulatedEnvelope {
		t.Error("an enveloped pick is not marked simulated")
	}
	if small.UsableEnvelopeBytes != 16<<30 {
		t.Errorf("simulated envelope = %d, want exactly 16 GiB (replaced, not added)", small.UsableEnvelopeBytes)
	}
	if small.WeightBytes >= base.WeightBytes {
		t.Errorf("16 GiB pick %q (%d) is not smaller than detected 64 GiB pick %q (%d)", small.Model, small.WeightBytes, base.Model, base.WeightBytes)
	}
	if large.WeightBytes <= base.WeightBytes {
		t.Errorf("160 GiB pick %q (%d) is not larger than detected 64 GiB pick %q (%d)", large.Model, large.WeightBytes, base.Model, base.WeightBytes)
	}
	if small.TotalBytes > small.UsableEnvelopeBytes {
		t.Error("OOM guard violated under a simulated envelope")
	}
}

// TestPickEnvelopeOverrideSupersedesAnUnknownEnvelope guards that a simulated
// envelope on a host whose envelope is unknown is not a degraded estimate.
func TestPickEnvelopeOverrideSupersedesAnUnknownEnvelope(t *testing.T) {
	rec := Pick(profileWithEnvelope(0), testCatalog(), Overrides{Envelope: 64 << 30}, nil)
	if rec.Model == "" || rec.Degraded {
		t.Errorf("model %q degraded %v; want a non-degraded pick", rec.Model, rec.Degraded)
	}
}
