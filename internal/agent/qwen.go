// qwen.go is the third `villa code` launch target, next to Crush (agent.go) and
// Claude Code (claude.go): it execs a PATH-resolved `qwen` (Qwen Code) against the
// already-served inference endpoint. Unlike Claude Code, villa owns Qwen Code's
// configuration (ADR-0039): RunQwen writes settings.json into a villa-owned
// directory on EVERY launch and points the agent at it with QWEN_HOME, so the user's
// own ~/.qwen is never read and there is nothing to drift. villa never installs the
// binary, and coding mode is a hard gate for the same reason as Claude Code's.
//
// The launch contract's controls are the environment variables: a repository's own
// .qwen/settings.json can re-enable usage statistics over any user-level file (the
// #373 capture), so the settings keys here are belt and braces. The inference secret
// goes only in the env (ADR-0011, ADR-0014), never in the file.
//
// Seam discipline (same as claude.go): no import of internal/inference or
// internal/detect, no backend marker. The only non-config literal is providerBaseURL.
package agent

import (
	"encoding/json"
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// qwenBin is the PATH-resolved binary name RunQwen looks up.
const qwenBin = "qwen"

// qwenKeyEnv is the env var the provider entry's envKey names; its value is the
// inference secret.
const qwenKeyEnv = "OPENAI_API_KEY"

// qwenSettings is the villa-written settings.json. Field order is the render order,
// so the output is byte-deterministic. The opt-out booleans carry no omitempty: a
// false must be written.
type qwenSettings struct {
	ModelProviders struct {
		OpenAI []qwenProvider `json:"openai"`
	} `json:"modelProviders"`
	Security struct {
		Auth struct {
			SelectedType string `json:"selectedType"`
		} `json:"auth"`
	} `json:"security"`
	Model struct {
		Name           string `json:"name"`
		CatalogRefresh bool   `json:"catalogRefresh"`
	} `json:"model"`
	Privacy struct {
		UsageStatisticsEnabled bool `json:"usageStatisticsEnabled"`
	} `json:"privacy"`
	General struct {
		EnableAutoUpdate bool `json:"enableAutoUpdate"`
	} `json:"general"`
	UI struct {
		EnableUserFeedback bool `json:"enableUserFeedback"`
	} `json:"ui"`
	Telemetry struct {
		Enabled bool `json:"enabled"`
	} `json:"telemetry"`
}

type qwenProvider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	EnvKey  string `json:"envKey"`
	BaseURL string `json:"baseUrl"`
}

// RenderQwenSettings produces the deterministic settings.json bytes from config.
// It is pure and carries no secret.
func RenderQwenSettings(cfg config.VillaConfig) ([]byte, error) {
	_, base := servedModelID(cfg)
	var s qwenSettings
	s.ModelProviders.OpenAI = []qwenProvider{{
		ID:      base,
		Name:    base + " (local)",
		EnvKey:  qwenKeyEnv,
		BaseURL: providerBaseURL,
	}}
	s.Security.Auth.SelectedType = "openai"
	s.Model.Name = base
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("agent: render qwen settings: %w", err)
	}
	return append(b, '\n'), nil
}

// RunQwen is the orchestration for `villa code --agent qwen`. Coding mode is a hard
// gate and an absent binary a refusal; both return before anything is written. It
// then writes settings.json into the villa-owned Qwen home (every launch, so a hand
// edit cannot persist) and returns a launch-ready Result. It never execs itself.
func RunQwen(d Deps, extraArgs []string) Result {
	var res Result

	cfg, err := d.LoadConfig()
	if err != nil {
		res.Err = fmt.Errorf("agent: load config: %w", err)
		res.Reason = "could not load villa config — fix config.toml and retry"
		return res
	}

	if !subsystem.CodingModeOn(cfg) {
		res.CodingModeOff = true
		res.Reason = "coding mode is OFF — Qwen Code needs the coder unit's tool-call template (--jinja), " +
			"which villa renders only in coding mode; run `villa coding-mode enter` first"
		return res
	}

	path, found := d.LookPath(qwenBin)
	if !found {
		res.BinaryAbsent = true
		res.Reason = "qwen is not on PATH — install Qwen Code " +
			"(https://github.com/QwenLM/qwen-code) and retry; villa never installs it"
		return res
	}

	home, err := d.QwenHome()
	if err != nil {
		res.Err = fmt.Errorf("agent: resolve qwen home: %w", err)
		res.Reason = "could not resolve villa's Qwen Code config directory"
		return res
	}
	settings, err := RenderQwenSettings(cfg)
	if err != nil {
		res.Err = err
		res.Reason = "could not render Qwen Code settings"
		return res
	}
	if err := d.WriteQwenSettings(home, settings); err != nil {
		res.Err = fmt.Errorf("agent: write qwen settings: %w", err)
		res.Reason = "could not write villa's Qwen Code settings.json, so the telemetry opt-out is not in place; not launching"
		return res
	}

	res.LaunchBin = path
	res.LaunchArgs = append([]string(nil), extraArgs...)
	res.LaunchEnv = qwenEnv(cfg, home)
	res.ReadyToLaunch = true
	return res
}

// qwenEnv builds the launch env: osEnviron() plus constant-literal appends. The
// first two opt-outs are the measured minimal set (#373, ADR-0039); the secret
// rides only here.
func qwenEnv(cfg config.VillaConfig, home string) []string {
	return append(osEnviron(),
		"QWEN_HOME="+home,
		"QWEN_USAGE_STATISTICS_ENABLED=false",
		"QWEN_CODE_MODELS_DEV_REFRESH=off",
		envDoNotTrack,
		qwenKeyEnv+"="+cfg.InferenceSecret,
	)
}
