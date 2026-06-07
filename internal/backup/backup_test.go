package backup

import "testing"

// baseManifest / baseCurrent are a fully-matching manifest/current pair; tests
// mutate one field to exercise a single classification.
func baseManifest() Manifest {
	return BuildManifest(ManifestInput{
		VillaVersion:        "v1.2.0",
		Host:                HostFingerprint{Arch: "amd64", IGPU: "gfx1151", Kernel: "6.18.4"},
		InferenceImage:      "inf@sha256:aaa",
		OpenWebUIImage:      "owui@sha256:bbb",
		ConfigSchemaVersion: 1,
		UsageSchemaVersion:  1,
		BenchSchemaVersion:  1,
	})
}

func baseCurrent() CurrentInstall {
	return CurrentInstall{
		VillaVersion:        "v1.2.0",
		InferenceImage:      "inf@sha256:aaa",
		OpenWebUIImage:      "owui@sha256:bbb",
		Host:                HostFingerprint{Arch: "amd64", IGPU: "gfx1151", Kernel: "6.18.4"},
		ConfigSchemaVersion: 1,
		UsageSchemaVersion:  1,
		BenchSchemaVersion:  1,
	}
}

// TestSkewClassification is the table-driven BAK-03 / D-08 classifier test.
func TestSkewClassification(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(m *Manifest, c *CurrentInstall)
		wantBlock bool
		wantWarnN int
		wantField string // a warning Field that MUST be present (when wantWarnN>0)
	}{
		{
			name:   "fully matching: no findings",
			mutate: func(m *Manifest, c *CurrentInstall) {},
		},
		{
			name:      "villa version mismatch -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.VillaVersion = "v9.9.9" },
			wantWarnN: 1,
			wantField: "villa_version",
		},
		{
			name:      "inference digest mismatch -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.InferenceImage = "inf@sha256:zzz" },
			wantWarnN: 1,
			wantField: "inference_image",
		},
		{
			name:      "owui digest mismatch -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.OpenWebUIImage = "owui@sha256:zzz" },
			wantWarnN: 1,
			wantField: "openwebui_image",
		},
		{
			name:      "host fingerprint mismatch -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.Host.Kernel = "6.99.0" },
			wantWarnN: 1,
			wantField: "host",
		},
		{
			name:      "older usage store schema -> WARN",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.UsageSchemaVersion = 2 },
			wantWarnN: 1,
			wantField: "usage_schema_version",
		},
		{
			name:      "checksum failed -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { c.ChecksumFailed = true },
			wantBlock: true,
		},
		{
			name:      "newer manifest schema -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { m.SchemaVersion = backupSchemaVersion + 1 },
			wantBlock: true,
		},
		{
			name:      "unreadable manifest schema -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { m.SchemaVersion = 0 },
			wantBlock: true,
		},
		{
			name:      "newer config store schema -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { m.ConfigSchemaVersion = 5; c.ConfigSchemaVersion = 1 },
			wantBlock: true,
		},
		{
			name:      "newer bench store schema -> BLOCK",
			mutate:    func(m *Manifest, c *CurrentInstall) { m.BenchSchemaVersion = 5; c.BenchSchemaVersion = 1 },
			wantBlock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := baseManifest()
			c := baseCurrent()
			tt.mutate(&m, &c)
			v := CompareSkew(m, c)

			if v.Block != tt.wantBlock {
				t.Fatalf("Block = %v, want %v (reason=%q)", v.Block, tt.wantBlock, v.BlockReason)
			}
			if tt.wantBlock {
				if v.BlockReason == "" {
					t.Errorf("Block=true but BlockReason is empty")
				}
				// A BLOCK short-circuits — no warnings should accumulate.
				if len(v.Warnings) != 0 {
					t.Errorf("Block=true but got %d warnings", len(v.Warnings))
				}
				return
			}
			if len(v.Warnings) != tt.wantWarnN {
				t.Fatalf("got %d warnings, want %d: %+v", len(v.Warnings), tt.wantWarnN, v.Warnings)
			}
			if tt.wantField != "" {
				found := false
				for _, w := range v.Warnings {
					if w.Field == tt.wantField {
						found = true
						if w.Remediation == "" {
							t.Errorf("warning %q has empty remediation", w.Field)
						}
					}
				}
				if !found {
					t.Errorf("no warning with Field=%q in %+v", tt.wantField, v.Warnings)
				}
			}
		})
	}
}

// TestSkewMatchingNoFindings asserts a fully-matching manifest yields the zero
// verdict (no Block, no Warnings) — the happy path.
func TestSkewMatchingNoFindings(t *testing.T) {
	v := CompareSkew(baseManifest(), baseCurrent())
	if v.Block || len(v.Warnings) != 0 {
		t.Fatalf("matching manifest produced findings: %+v", v)
	}
}
