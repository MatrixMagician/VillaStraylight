package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// TestUpKeepsCodingModeTheWayEnterRendersIt guards #249: once `villa coding-mode
// enter` has written the coding-mode villa-llama unit, `villa up` re-renders the
// stack from the SAME persisted config and must produce the same villa-llama text.
// Before the stack-apply module every verb but coding-mode and install rendered
// villa-llama from cfg.Model with no coding descriptor and no agent ctx, so `up`
// saw a changed unit and restarted it without coding mode while config.toml still
// said coding mode was on.
//
// Both residencies are covered because they differ in what coding mode changes:
// swap serves the coder entry, shared serves the chat entry with the tool-calling
// delta and the agent ctx.
func TestUpKeepsCodingModeTheWayEnterRendersIt(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.VillaConfig
	}{
		{"swap residency", config.VillaConfig{
			Model: "qwen3.6-35b-a3b", Quant: "UD-Q4_K_M", Ctx: 8192, Backend: "vulkan",
			CodingMode: true, CoderModel: "qwen3-coder-30b-a3b", CoderAgentCtx: 65536,
		}},
		{"shared residency", config.VillaConfig{
			Model: "qwen3.6-35b-a3b", Quant: "UD-Q4_K_M", Ctx: 8192, Backend: "vulkan",
			CodingMode: true, CoderAgentCtx: 32768,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("HOME", t.TempDir())
			// No systemctl on PATH: the enter write still lands in the temp unit dir,
			// and its daemon-reload is refused as a missing tool instead of reaching
			// the host's user manager.
			t.Setenv("PATH", t.TempDir())
			if err := config.SaveVilla(tc.cfg); err != nil {
				t.Fatalf("SaveVilla: %v", err)
			}

			var missing orchestrate.ErrToolNotFound
			if _, err := liveCodingModeDeps(context.Background()).ReconcileAndWrite(tc.cfg); err != nil && !errors.As(err, &missing) {
				t.Fatalf("coding-mode enter render: %v", err)
			}
			dir, err := quadletUnitDir()
			if err != nil {
				t.Fatalf("quadletUnitDir: %v", err)
			}
			entered, err := os.ReadFile(filepath.Join(dir, "villa-llama.container"))
			if err != nil {
				t.Fatalf("coding-mode enter wrote no villa-llama unit: %v", err)
			}

			units, _, err := liveLifecycleDeps().renderStack()
			if err != nil {
				t.Fatalf("up render: %v", err)
			}
			var upText string
			for _, u := range units {
				if u.Name == "villa-llama.container" {
					upText = u.Text
				}
			}
			if upText != string(entered) {
				t.Errorf("up renders a different villa-llama than coding-mode enter wrote, so up would restart it out of coding mode (#249)\n--- enter ---\n%s\n--- up ---\n%s", entered, upText)
			}
		})
	}
}
