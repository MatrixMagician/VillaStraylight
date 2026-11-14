// offline.go is the opt-in fail-closed launch contract shared by every `villa code`
// target (ADR-0039, amendment #389). The default contract switches an agent's telemetry
// off; it cannot remove a request the agent has no switch for (Crush's release check).
// With --offline the launch env also points the proxy variables at a closed loopback
// port and exempts only the model server, so an agent's HTTP(S) request to anywhere else
// fails to connect instead of leaving the box.
//
// The cost is stated in `villa code --help`: the agent's own shell inherits these
// variables, so `git clone` or `go mod download` over HTTPS fails in this mode.
package agent

import (
	"net/url"
	"strings"
)

// offlineProxyURL is a closed loopback port: nothing listens, so a proxied request is
// refused at once. The value the #385 prototype measured.
const offlineProxyURL = "http://127.0.0.1:1"

// offlineProxyKeys are the variables set to offlineProxyURL, in both cases because tools
// differ on which they read.
var offlineProxyKeys = []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"}

// offlineNoProxyKeys carry the one address that stays reachable.
var offlineNoProxyKeys = []string{"NO_PROXY", "no_proxy"}

// offlineEnv is the single fail-closed value every target shares: the KEY=VALUE pairs
// Offline adds. NO_PROXY names the host of providerBaseURL, the address every agent is
// launched against (Claude Code's ANTHROPIC_BASE_URL is the same host).
func offlineEnv() []string {
	noProxy := ""
	if u, err := url.Parse(providerBaseURL); err == nil {
		noProxy = u.Hostname()
	}
	env := make([]string, 0, len(offlineProxyKeys)+len(offlineNoProxyKeys))
	for _, k := range offlineProxyKeys {
		env = append(env, k+"="+offlineProxyURL)
	}
	for _, k := range offlineNoProxyKeys {
		env = append(env, k+"="+noProxy)
	}
	return env
}

// withOffline returns env unchanged when offline is false. Otherwise it drops every
// inherited entry for a variable offlineEnv sets (an operator's proxy is overridden,
// never merged, and a duplicate key would leave the winner to the agent's runtime) and
// appends offlineEnv.
func withOffline(env []string, offline bool) []string {
	if !offline {
		return env
	}
	add := offlineEnv()
	owned := make(map[string]bool, len(add))
	for _, kv := range add {
		k, _, _ := strings.Cut(kv, "=")
		owned[k] = true
	}
	out := make([]string, 0, len(env)+len(add))
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); !owned[k] {
			out = append(out, kv)
		}
	}
	return append(out, add...)
}
