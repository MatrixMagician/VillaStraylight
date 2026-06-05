package dashboard

import (
	"embed"
	"io/fs"
)

// assetsFS embeds the no-build single-page UI (HTML shell + one CSS file + vanilla
// JS) so `villa dashboard` ships as a pure `go build` static binary — NO node/npm
// toolchain (D-01). `all:assets` includes dotfiles for completeness, mirroring the
// legacy web/embed.go //go:embed all:dist idiom.
//
//go:embed all:assets
var assetsFS embed.FS

// Assets returns the embedded assets/ subtree as an fs.FS for http.FileServer and the
// html/template parse. fs.Sub re-roots the FS at assets/ so request paths map cleanly.
func Assets() (fs.FS, error) {
	return fs.Sub(assetsFS, "assets")
}
