package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// code.go is the cmd-tier `villa code` agent launcher: the live host wiring that drives
// the pure internal/agent core (Plan 26-01). It holds the cobra surface (a single
// NoArgs verb), the Result→exit mapping, liveAgentDeps (the host seams), and the XDG
// path helpers (agentBinPath / crushConfigPath).
//
// CRITICAL — discipline this file MUST honor:
//   - backend-marker-free (T-26-09 / TestSeamGrepGate walks cmd/villa): no container
//     image/device literal, no GPU residency/HSA/fault marker, no coding-mode llama
//     flags. `villa code` launches against the ALREADY-served loopback endpoint; it
//     renders no inference units and resolves no backend.
//   - NO coding-flag literal: this file assigns the coding toggle NOWHERE — neither a
//     boolean assignment nor a struct-literal field for it appears here (the
//     no-auto-flip structural guard anchors on those boolean-literal forms —
//     D-12/T-26-13). `villa code` NEVER mutates the coding toggle; a coding-mode-OFF
//     config (read-only) is a WARN (pointing at `villa coding-mode enter`), never a
//     flip — the launch still proceeds.
//   - explicit villa-owned exec (D-05): Launch execs the EXPLICIT agentBinPath(),
//     NEVER a PATH lookup, so a user-installed `crush` on PATH cannot be hijacked.
//
// Decisions realized: AGENT-03 (belt-and-braces env lockdown before exec, D-11),
// D-05 (explicit villa-owned binary), D-13 (binary-absent + present-but-differs drift
// remediation, never a crash, never auto-correct), D-12 (no auto-flip; coding-off WARN).

// newCode builds the `villa code` launcher verb. It is a sibling of `villa coding-mode`
// (NOT a subcommand): `villa coding-mode enter|exit` flips the running stack; `villa
// code` launches the strictly-local terminal coding agent (Crush) against the served
// endpoint with the telemetry/autoupdate lockdown env. RunE returns the mapped exit
// code via os.Exit (the runCode body returns the int so tests assert output+code).
func newCode() *cobra.Command {
	return &cobra.Command{
		Use:   "code",
		Short: "Launch the strictly-local terminal coding agent (locked-down Crush)",
		Long: "Launch the villa-owned Crush coding agent against the already-served local inference endpoint, " +
			"with a belt-and-braces telemetry/autoupdate lockdown env (CRUSH_DISABLE_METRICS / DO_NOT_TRACK / " +
			"CRUSH_DISABLE_PROVIDER_AUTO_UPDATE) applied before exec. It execs the EXPLICIT villa-owned binary " +
			"(never a PATH lookup), renders the reference crush.json from your config.toml on first run, and " +
			"surfaces a drifted binary or hand-edited crush.json with remediation (never silently auto-corrects). " +
			"It NEVER flips coding mode — if coding mode is off it warns (pointing at `villa coding-mode enter`) " +
			"but still launches against the current endpoint.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runCode(cmd, liveAgentDeps())
			os.Exit(code)
			return nil
		},
	}
}

// runCode delegates to agent.Run and maps the typed Result to exit codes + messages
// (clone of runCodingMode's body-returns-int shape so tests assert output+code without
// a subprocess). The first-run config-absent render is handled INSIDE agent.Run (it
// writes via Deps.WriteConfig then launches); runCode only reports the typed outcome.
// LSP warnings (and the first-run-rendered / binary-drift-unknown / coding-off WARNs)
// print to stderr as advisory (D-10), never blocking the launch.
func runCode(cmd *cobra.Command, d *agent.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	res := agent.Run(*d)

	// Advisory warnings first (LSP-missing, coding-mode-off, first-run-rendered,
	// binary-drift-unknown) — surfaced regardless of the outcome, never blocking.
	for _, w := range res.Warnings {
		fmt.Fprintf(errOut, "villa code: warning [%s]: %s\n", w.Code, w.Msg)
	}

	switch {
	case res.BinaryAbsent:
		fmt.Fprintf(errOut, "villa code: refusing — %s\n", res.Reason)
		fmt.Fprintf(errOut, "  remediation: install the pinned Crush binary via the Phase-27 `villa install` agent addon\n")
		return exitBlocked
	case res.BinaryDrift:
		fmt.Fprintf(errOut, "villa code: refusing — %s\n", res.Reason)
		fmt.Fprintf(errOut, "  remediation: re-install the pinned Crush binary; villa does not auto-correct a drifted binary\n")
		return exitBlocked
	case res.ConfigDrift:
		fmt.Fprintf(errOut, "villa code: refusing — %s\n", res.Reason)
		fmt.Fprintf(errOut, "  remediation: review your crush.json edits or re-render; villa never overwrites your file automatically\n")
		return exitBlocked
	case res.Err != nil:
		fmt.Fprintf(errOut, "villa code: failed — %s: %v\n", res.Reason, res.Err)
		return exitBlocked
	case res.Launched:
		// On a real exec the process is replaced and we never reach here; this branch is
		// for the test seam where the injected Launch returns nil without exec'ing.
		fmt.Fprintf(out, "villa code: launched Crush against the local endpoint\n")
		return exitPass
	default:
		// A non-launching clean success (defensive; the clean path always launches).
		return exitPass
	}
}

// liveAgentDeps wires the agent core to the real host (clone of liveCodingModeDeps):
// config load, the LSP PATH probe (references only — never installs, D-10), the on-disk
// crush.json read with the configPresent flag DetectDrift uses to distinguish absent
// from drift, the villa-owned binary hash, the first-run config writer (the ONLY
// auto-write path — D-14, only when ABSENT), and the syscall.Exec launcher of the
// EXPLICIT villa-owned path (D-05, fixed-arg, no shell). Every host action is a seam so
// code_test.go drives runCode without a live host.
func liveAgentDeps() *agent.Deps {
	return &agent.Deps{
		LoadConfig: config.LoadVilla,
		// LookPath references the host PATH; it NEVER installs (D-10).
		LookPath: func(bin string) (string, bool) {
			p, err := exec.LookPath(bin)
			return p, err == nil
		},
		// ReadConfig reads ~/.config/crush/crush.json; a not-exist read maps to
		// configPresent=false (the first-run trigger), distinct from a real read error.
		ReadConfig: func() ([]byte, bool, error) {
			path, err := crushConfigPath()
			if err != nil {
				return nil, false, err
			}
			b, err := os.ReadFile(path) //nolint:gosec // path is the XDG-resolved crush config, not user input
			if os.IsNotExist(err) {
				return nil, false, nil
			}
			if err != nil {
				return nil, false, err
			}
			return b, true, nil
		},
		// HashBinary returns the SHA-256 of the villa-owned binary; present=false when
		// it is absent (feeds BinaryAbsent → Phase-27 remediation, D-13).
		HashBinary: func() (string, bool, error) {
			sum, present, err := hashFileSHA256(agentBinPath())
			return sum, present, err
		},
		// WriteConfig persists the first-run rendered crush.json (MkdirAll 0700 +
		// WriteFile 0600), traversal-guarded against the crush config dir. Invoked by
		// agent.Run ONLY on the config-absent path (D-14: never overwrites an existing file).
		WriteConfig: func(b []byte) error {
			path, err := crushConfigPath()
			if err != nil {
				return err
			}
			dir := filepath.Dir(path)
			if err := assertWithinDir(path, dir); err != nil {
				return err
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return fmt.Errorf("villa code: create crush config dir: %w", err)
			}
			if err := os.WriteFile(path, b, 0o600); err != nil {
				return fmt.Errorf("villa code: write crush.json: %w", err)
			}
			return nil
		},
		// Launch execs the EXPLICIT villa-owned binary with the lockdown env (D-05/D-11):
		// fixed-arg, no shell, NEVER a PATH lookup — a user-installed crush on PATH cannot
		// be hijacked (T-26-09). On success the process image is replaced and this never
		// returns; a returned error is a launch failure.
		Launch: func(env []string) error {
			bin := agentBinPath()
			if err := syscall.Exec(bin, []string{filepath.Base(bin)}, env); err != nil {
				return fmt.Errorf("villa code: exec %q: %w", bin, err)
			}
			return nil
		},
	}
}

// crushConfigPath resolves ~/.config/crush/crush.json (os.UserConfigDir honors XDG
// safely). This is the global Crush config Crush itself reads — NOT under the villa
// config dir — so the rendered reference lands where Crush expects it.
func crushConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("villa code: cannot resolve user config dir: %w", err)
	}
	return filepath.Join(base, "crush", "crush.json"), nil
}

// agentBinDir resolves the villa-owned bin dir for the Crush binary, cloning the
// recall storeRootDir fallback chain: $XDG_DATA_HOME/villa/bin → ~/.local/share/villa/bin
// → /var/tmp/villa/bin. The binary is exec'd from EXACTLY this path (D-05).
func agentBinDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa", "bin")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa", "bin")
	}
	return filepath.Join("/var/tmp", "villa", "bin")
}

// agentBinPath is the EXPLICIT villa-owned Crush binary path — the only path Launch
// ever execs (D-05).
func agentBinPath() string {
	return filepath.Join(agentBinDir(), "crush")
}

// hashFileSHA256 returns the lowercase hex SHA-256 of the file at path. present is
// false (with no error) when the file does not exist — the BinaryAbsent signal.
func hashFileSHA256(path string) (sum string, present bool, err error) {
	f, err := os.Open(path) //nolint:gosec // path is the XDG-resolved villa-owned binary, not user input
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", false, err
	}
	return hex.EncodeToString(h.Sum(nil)), true, nil
}

// assertWithinDir confirms path resolves inside dir, rejecting traversal escapes — the
// first-run write guard (T-26-08 sibling). Cleaned + compared as absolute paths.
func assertWithinDir(path, dir string) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("villa code: refusing to write %q outside %q", absPath, absDir)
	}
	return nil
}
