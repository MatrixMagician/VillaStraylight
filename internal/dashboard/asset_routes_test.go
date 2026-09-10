package dashboard

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

var apiLiteralRe = regexp.MustCompile(`["'](/api/[^"']*)["']`)

// TestAssetRoutesAreRegistered greps every quoted /api/... string literal out of
// the served dashboard.html and dashboard.js and asserts each one is either an
// exact registered route pattern or a proper prefix of one (the JS builds
// dynamic task URLs by concatenation, e.g. "/api/tasks/" + id + "/events", so
// the literal fragment it contains is a prefix of the golden `/api/tasks/{id}/events`
// pattern, never the whole templated path).
func TestAssetRoutesAreRegistered(t *testing.T) {
	srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})
	var patterns []string
	for _, rt := range srv.apiRoutes() {
		patterns = append(patterns, rt.pattern)
	}

	sub, err := Assets()
	if err != nil {
		t.Fatalf("Assets: %v", err)
	}
	for _, name := range []string{"dashboard.html", "dashboard.js"} {
		b, err := fs.ReadFile(sub, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range apiLiteralRe.FindAllStringSubmatch(string(b), -1) {
			lit := m[1]
			if !literalMatchesARoute(lit, patterns) {
				t.Errorf("%s references %q, which is not an exact route pattern or a prefix of one (patterns: %v)", name, lit, patterns)
			}
		}
	}
}

func literalMatchesARoute(lit string, patterns []string) bool {
	for _, p := range patterns {
		if p == lit || strings.HasPrefix(p, lit) {
			return true
		}
	}
	return false
}
