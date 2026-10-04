package agent

import (
	"errors"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestRunStopsBeforeAnyWrite guards the early-return contract of Run's step helpers:
// a load error, a hash error, a read error (each an Err wrapping its cause) and a
// confident binary drift (BinaryDrift set) each end Run without a write, a backup, a
// launch (Launch is never called by Run) or a launch hand-off (ReadyToLaunch /
// LaunchEnv). The on-disk config is absent, so any step that ran past the drift gate
// would write the first-run config: a write here is a leak.
func TestRunStopsBeforeAnyWrite(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name      string
		mutate    func(*Deps)
		wantErr   bool
		wantDrift bool
	}{
		{"load error", func(d *Deps) {
			d.LoadConfig = func() (config.VillaConfig, error) { return config.VillaConfig{}, boom }
		}, true, false},
		{"hash error", func(d *Deps) {
			d.HashBinary = func() (string, bool, error) { return "", false, boom }
		}, true, false},
		{"read error", func(d *Deps) {
			d.ReadConfig = func() ([]byte, bool, error) { return nil, false, boom }
		}, true, false},
		{"binary drift", func(d *Deps) {
			d.HashBinary = func() (string, bool, error) { return otherBinSHA, true, nil }
		}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var writes, backups, launches int
			d := Deps{
				LoadConfig:   func() (config.VillaConfig, error) { return renderTestConfig(), nil },
				LookPath:     func(string) (string, bool) { return "", false },
				HashBinary:   func() (string, bool, error) { return pinnedBinarySHA(), true, nil },
				ReadConfig:   func() ([]byte, bool, error) { return nil, false, nil },
				WriteConfig:  func([]byte) error { writes++; return nil },
				BackupConfig: func([]byte) error { backups++; return nil },
				Launch:       func([]string) error { launches++; return nil },
			}
			tc.mutate(&d)
			res := Run(d)
			if tc.wantErr && !errors.Is(res.Err, boom) {
				t.Errorf("Err = %v, want it to wrap the cause", res.Err)
			}
			if !tc.wantErr && res.Err != nil {
				t.Errorf("Err = %v, want nil", res.Err)
			}
			if res.BinaryDrift != tc.wantDrift {
				t.Errorf("BinaryDrift = %v, want %v", res.BinaryDrift, tc.wantDrift)
			}
			if res.ReadyToLaunch || res.LaunchEnv != nil {
				t.Errorf("ReadyToLaunch=%v LaunchEnv=%v, want no launch hand-off", res.ReadyToLaunch, res.LaunchEnv)
			}
			if writes != 0 || backups != 0 || launches != 0 {
				t.Errorf("writes=%d backups=%d launches=%d, want none", writes, backups, launches)
			}
		})
	}
}
