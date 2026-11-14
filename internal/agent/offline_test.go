package agent

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// offlineLaunchEnvs returns each agent's LaunchEnv under the given Deps.Offline and an
// inherited environment that already carries an operator's proxy settings.
func offlineLaunchEnvs(t *testing.T, offline bool) map[string][]string {
	t.Helper()
	restore := osEnviron
	osEnviron = func() []string {
		return []string{
			"PATH=/usr/bin",
			"HTTPS_PROXY=http://corp:3128", "https_proxy=http://corp:3128",
			"HTTP_PROXY=http://corp:3128", "http_proxy=http://corp:3128",
			"NO_PROXY=.corp.example", "no_proxy=.corp.example",
		}
	}
	defer func() { osEnviron = restore }()

	cfg := config.VillaConfig{Model: "qwen3", CodingMode: true, InferenceSecret: "s"}
	out := map[string][]string{}

	crush := &runRecorder{cfg: cfg, binPresent: true, binSHA: pinnedPolicyBinSHA, configPresent: true, onDisk: renderedRef(t, cfg)}
	d := crush.deps()
	d.Offline = offline
	out["crush"] = Run(d).LaunchEnv

	cl := &claudeRecorder{cfg: cfg, lookPath: map[string]string{claudeBin: "/bin/claude"}}
	d = cl.deps()
	d.Offline = offline
	out["claude"] = RunClaude(d, nil).LaunchEnv

	qw := &qwenRecorder{cfg: cfg, lookPath: map[string]string{qwenBin: "/bin/qwen"}, home: "/h"}
	d = qw.deps()
	d.Offline = offline
	out["qwen"] = RunQwen(d, nil).LaunchEnv
	return out
}

// envValues returns every value carried for key, so a merge or a duplicate shows up.
func envValues(env []string, key string) []string {
	var vs []string
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			vs = append(vs, v)
		}
	}
	return vs
}

// TestOfflineContractEveryAgent guards the shared fail-closed contract: with Offline,
// every agent's launch env points both proxy variables (either case) at the closed
// loopback port, and an operator's own proxy settings are replaced, not merged.
func TestOfflineContractEveryAgent(t *testing.T) {
	for name, env := range offlineLaunchEnvs(t, true) {
		t.Run(name, func(t *testing.T) {
			for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
				if got := envValues(env, k); !reflect.DeepEqual(got, []string{offlineProxyURL}) {
					t.Errorf("%s = %v, want exactly [%s]", k, got, offlineProxyURL)
				}
			}
			for _, k := range []string{"NO_PROXY", "no_proxy"} {
				got := envValues(env, k)
				if len(got) != 1 || strings.Contains(got[0], "corp") {
					t.Errorf("%s = %v, want a single villa-owned value with the operator's dropped", k, got)
				}
			}
			if !containsEnv(env, "PATH=/usr/bin") {
				t.Errorf("env %v lost the inherited PATH", env)
			}
		})
	}
}

// TestOfflineNoProxyCoversTheModelServer guards the one reachable address: NO_PROXY
// names the host of the base URL every agent is given, in the form it is written.
func TestOfflineNoProxyCoversTheModelServer(t *testing.T) {
	u, err := url.Parse(providerBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	claudeBase := strings.TrimSuffix(providerBaseURL, "/v1")
	for name, env := range offlineLaunchEnvs(t, true) {
		if name == "claude" && !containsEnv(env, "ANTHROPIC_BASE_URL="+claudeBase) {
			t.Fatalf("claude is not launched against %s", claudeBase)
		}
		for _, k := range []string{"NO_PROXY", "no_proxy"} {
			got := envValues(env, k)
			if len(got) != 1 || !containsEnv(strings.Split(got[0], ","), u.Hostname()) {
				t.Errorf("%s %s = %v does not name the model server host %q", name, k, got, u.Hostname())
			}
		}
	}
}

// TestDefaultLaunchEnvCarriesNoOfflineChange guards the default contract: without
// Offline the operator's proxy settings pass through untouched and villa adds none.
func TestDefaultLaunchEnvCarriesNoOfflineChange(t *testing.T) {
	for name, env := range offlineLaunchEnvs(t, false) {
		t.Run(name, func(t *testing.T) {
			for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
				if got := envValues(env, k); !reflect.DeepEqual(got, []string{"http://corp:3128"}) {
					t.Errorf("%s = %v, want the operator's value untouched", k, got)
				}
			}
			for _, k := range []string{"NO_PROXY", "no_proxy"} {
				if got := envValues(env, k); !reflect.DeepEqual(got, []string{".corp.example"}) {
					t.Errorf("%s = %v, want the operator's value untouched", k, got)
				}
			}
		})
	}
}

// TestOfflineOnlyChangesTheProxyVariables guards that Offline is additive over the
// default contract: every non-proxy variable is the same, in the same order.
func TestOfflineOnlyChangesTheProxyVariables(t *testing.T) {
	notProxy := func(env []string) []string {
		var out []string
		for _, e := range env {
			if k, _, _ := strings.Cut(e, "="); !strings.HasSuffix(strings.ToUpper(k), "_PROXY") {
				out = append(out, e)
			}
		}
		return out
	}
	on, off := offlineLaunchEnvs(t, true), offlineLaunchEnvs(t, false)
	for name := range on {
		if !reflect.DeepEqual(notProxy(on[name]), notProxy(off[name])) {
			t.Errorf("%s: non-proxy env differs under Offline:\n on=%v\noff=%v", name, notProxy(on[name]), notProxy(off[name]))
		}
	}
}
