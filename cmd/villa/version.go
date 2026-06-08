package main

// version.go provides the build-stamped villa binary version (Phase 16, D-09 /
// OQ1). No version constant existed before this phase; the backup manifest's
// villa_version field (and the BAK-03 version-skew compare) needs a single source.
//
// version defaults to "dev" and is overridden at build time via
// `-ldflags "-X main.version=<v>"` (wired in the Makefile build target). Keeping
// it a package-level var (not a const) is what makes the linker -X stamp work.

// version is the villa binary version, stamped by the Makefile's -ldflags at
// build time; "dev" when built without a stamp (e.g. `go run`, `go test`).
var version = "dev"

// villaVersion returns the build-stamped villa version — the single source for
// manifest.villa_version (D-09) and the BAK-03 version-skew comparison. It never
// returns empty: an unstamped build reports "dev".
func villaVersion() string {
	if version == "" {
		return "dev"
	}
	return version
}
