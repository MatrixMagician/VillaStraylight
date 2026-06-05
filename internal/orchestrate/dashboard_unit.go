package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// dashboard_unit.go renders + writes the NATIVE `villa-dashboard.service` systemd
// --user unit (D-03/D-04). Unlike the Quadlet `.container` units (render.go), this is
// a plain `.service` that runs the villa binary itself — so it lives in
// ~/.config/systemd/user/, NOT the Quadlet generator dir ~/.config/containers/systemd/
// (Pitfall 5). It reuses the same pure-render (text/template + execTemplate) and the
// same atomic, traversal-guarded write (atomicWrite/assertInsideDir) the Quadlet path
// uses, so the dashboard service is written exactly as safely as a container unit
// (T-05-15) — temp-in-same-dir + fsync + rename, refusing any path resolving outside
// the unit dir.

// DashboardServiceName is the systemd --user unit name for the control dashboard
// (mirrors installServiceName in cmd/villa). It is a NATIVE .service (the villa binary
// running `villa dashboard`), not a Quadlet-derived .container service.
const DashboardServiceName = "villa-dashboard.service"

// dashboardUnitBody is the native .service unit text (PATTERNS/RESEARCH exact body).
// ExecStart is rendered from a caller-supplied, install-time-resolved absolute binary
// path (the running villa binary, via os.Executable→EvalSymlinks→Abs in
// cmd/villa/install.go) — NOT the old fixed `%h/.local/bin/villa`. That fixed path
// assumed the install flow deployed the binary to ~/.local/bin/villa, which it never
// did, so a dev build (./villa from the repo) hit status=203/EXEC at boot and the
// dashboard service stayed inactive after reboot (UAT Test 5). Rendering the actual
// running binary's path makes the unit survive a reboot for both dev and installed
// binaries with no file copying. The trailing newline keeps the rendered file
// POSIX-clean (matches the captured golden).
//
// [Install] WantedBy=default.target only takes effect once the unit is `enable`d
// (Systemd.Enable) — install does that for boot-survival (linger is already on).
const dashboardUnitBody = `[Unit]
Description=VillaStraylight control dashboard (read-only observer)
After=default.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} dashboard
Restart=on-failure

[Install]
WantedBy=default.target
`

// dashboardUnitView is the data the dashboard template renders. BinaryPath is the
// ALREADY-systemd-quoted ExecStart executable token (quoteExecStart) — quoting is done
// in Go, not the template, so the template stays a dumb substitution.
type dashboardUnitView struct {
	BinaryPath string
}

// quoteExecStart returns the binary path formatted for a systemd ExecStart executable
// token. Two distinct systemd parsing rules must be honored:
//
//  1. systemd expands `%`-specifiers (e.g. %h, %t) in ExecStart REGARDLESS of quoting,
//     and `%%` is the only way to emit a literal `%`. A `%` is a legal POSIX path byte,
//     so we ALWAYS double it first — an un-escaped `%` mangles the executable token
//     (203/EXEC at boot, the exact failure class this file fixes).
//  2. systemd splits ExecStart on whitespace into argv, so a path containing whitespace
//     must be wrapped in double-quotes to stay a single token. Inside double-quotes
//     systemd applies C-style unescaping, so a literal `"` or `\` in the path must be
//     backslash-escaped (`\"`, `\\`) to survive intact. A `\` is also escape-significant
//     OUTSIDE quotes, so its mere presence forces the quoted+escaped path too.
//
// A path with none of space/tab/quote/backslash is returned UNQUOTED (only `%`-doubled)
// to keep the golden/happy path POSIX-clean. The install-resolved path normally has no
// special characters, but these guards exist for robustness (e.g. "/home/u/My Apps/").
func quoteExecStart(path string) string {
	// (1) `%`→`%%` applies everywhere and is safe to do unconditionally first; the
	// doubled `%%` contains no quote/backslash so it never interacts with step (2).
	escaped := strings.ReplaceAll(path, "%", "%%")
	if strings.ContainsAny(path, " \t\"\\") {
		// (2) Quote the token and backslash-escape `\` then `"` (order matters: escape
		// pre-existing backslashes before introducing new ones for the quotes).
		escaped = strings.ReplaceAll(escaped, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return escaped
}

// dashboardUnitTemplate is parsed once at package init from the body literal. It
// renders via the same execTemplate helper the Quadlet units use (render.go), so the
// render machinery is shared, not forked. The single template action is {{.BinaryPath}}
// in ExecStart, fed an already-quoted path; everything else is fixed unit text.
var dashboardUnitTemplate = template.Must(template.New(DashboardServiceName).Parse(dashboardUnitBody))

// RenderDashboardUnit renders the native dashboard .service body with ExecStart pointed
// at binaryPath (the install-resolved absolute path of the running villa binary). It is
// PURE (no filesystem, no os.Executable, no systemctl) like orchestrate.Render — the
// impure path resolution lives in the cmd/villa/install.go caller and the impure write
// is WriteDashboardUnit. binaryPath is systemd-quoted here so a spaced path stays a
// single ExecStart token.
func RenderDashboardUnit(binaryPath string) (string, error) {
	view := dashboardUnitView{BinaryPath: quoteExecStart(binaryPath)}
	return execTemplate(dashboardUnitTemplate, DashboardServiceName, view)
}

// WriteDashboardUnit renders the dashboard unit for binaryPath and writes it atomically
// into dir (the user-unit dir from userUnitDir), reusing the traversal-guarded
// atomicWrite so the write is exactly as safe as a Quadlet unit write (T-05-15). Only
// the rendered body content changed for the UAT Test 5 fix; the write path is unchanged.
func WriteDashboardUnit(dir, binaryPath string) error {
	text, err := RenderDashboardUnit(binaryPath)
	if err != nil {
		return err
	}
	return writeUnitFile(dir, DashboardServiceName, text)
}

// writeUnitFile atomically writes text to dir/name, refusing any name that resolves
// outside dir (assertInsideDir, T-05-15) before any write. It is the single-file
// analog of WriteUnits, used for the native dashboard service that does not flow
// through the Quadlet Plan/Reconcile path.
func writeUnitFile(dir, name, text string) error {
	target := filepath.Join(dir, name)
	if err := assertInsideDir(target, dir); err != nil {
		return err
	}
	return atomicWrite(target, []byte(text))
}

// userUnitDir is the fixed rootless systemd --user unit directory
// (~/.config/systemd/user), created if absent so the first install writes cleanly. It
// MIRRORS cmd/villa quadletUnitDir but deliberately joins systemd,user — NOT
// containers,systemd — because the dashboard is a native .service, not a Quadlet
// .container (Pitfall 5). os.UserConfigDir honors $XDG_CONFIG_HOME (V12).
func userUnitDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "systemd", "user")
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return "", mkErr
	}
	return dir, nil
}

// UserUnitDir is the exported accessor cmd/villa wires as the live userUnitDir seam
// (install/uninstall point their unit-dir resolver at it). Kept separate from the
// unexported helper so the package's own tests exercise userUnitDir directly.
func UserUnitDir() (string, error) { return userUnitDir() }
