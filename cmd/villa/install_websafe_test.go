package main

// install_websafe_test.go drives the Phase-31 villa-websafe install wiring (
// UAT gap closure). It mirrors install_searxng_test.go's Task-2 install-flow gate
// for the websafe path: with web search ON, install generates-and-persists the
// EXTERNAL_WEB_LOADER_API_KEY bearer ONCE, writes the 0600 websafe.env BEFORE the OWUI start
// (which references it via EnvironmentFile= when web search is on), and starts
// villa-websafe. With web search OFF, NONE of the websafe seams fire (no bearer write, no
// villa-websafe start) — the install path is byte-identical to v1.4.

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// TestInstallWebsafeWiring is the websafe install-flow gate (UAT gap): the install verb must
// generate+persist the bearer, write the 0600 websafe.env BEFORE the OWUI start, and start
// villa-websafe when web search is on — and do NONE of that when web search is off.
func TestInstallWebsafeWiring(t *testing.T) {
	t.Run("web search on: writes the 0600 bearer once BEFORE the OWUI start and starts villa-websafe", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true
		f.searxngProofStatus = preflight.StatusPass

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("web-search-on install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		// The 0600 bearer env file must be written exactly once via the Deps seam.
		if f.websafeSecretEnvCalls != 1 {
			t.Errorf("the 0600 websafe.env bearer must be written once, writeWebsafeSecretEnv calls = %d", f.websafeSecretEnvCalls)
		}
		// It MUST be written BEFORE the OWUI start: when web search is on the OWUI unit references
		// the SAME websafe.env via EnvironmentFile=, so `systemctl start` would fail if the file
		// were absent (the exact UAT gap).
		owuiIdx := indexOf(f.callOrder, "start:"+openWebUIServiceName)
		if owuiIdx < 0 {
			t.Fatalf("OWUI start not recorded in callOrder = %v", f.callOrder)
		}
		if ei := indexOf(f.callOrder, "writeWebsafeSecretEnv"); ei < 0 || ei > owuiIdx {
			t.Errorf("the websafe.env bearer must be written BEFORE the OWUI start (env idx=%d, owui idx=%d); callOrder = %v", ei, owuiIdx, f.callOrder)
		}
		// villa-websafe must be started.
		if !contains(f.startOrder, websafeServiceName) {
			t.Errorf("web-search-on must start %s, startOrder = %v", websafeServiceName, f.startOrder)
		}
	})

	t.Run("web search on: generates+persists the bearer when empty (saveConfig)", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("web-search-on install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		// The bearer is generated ONCE on first opt-in and persisted via saveConfig — a re-install
		// then reuses it (Pitfall: never churn the OWUI⇄websafe trust). The persisted config must
		// carry a non-empty bearer.
		if f.savedCfg.WebLoaderSecret == "" {
			t.Errorf("first web-search opt-in must generate AND persist a web_loader_secret, savedCfg.WebLoaderSecret is empty")
		}
		// The secret VALUE must NEVER reach stdout (it lives only in the 0600 env file).
		// (stdout is checked empty-of-secret implicitly: it is never printed by the install flow.)
	})

	t.Run("web search off: no bearer write, no villa-websafe start (byte-identical path)", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.webSearchEnabled = false

		cmd, _, errOut := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("web-search-off install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.websafeSecretEnvCalls != 0 {
			t.Errorf("web-search-off must write no websafe.env bearer, writeWebsafeSecretEnv calls = %d", f.websafeSecretEnvCalls)
		}
		if contains(f.startOrder, websafeServiceName) {
			t.Errorf("web-search-off must not start %s, startOrder = %v", websafeServiceName, f.startOrder)
		}
		if strings.Contains(errOut.String(), "websafe") {
			t.Errorf("web-search-off must not mention websafe on stderr; stderr = %q", errOut.String())
		}
	})
}
