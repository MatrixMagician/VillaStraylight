package orchestrate

// inference_secret.go holds the GHSA-qxg9 (ADR-0011) llama-server bearer: the
// single home of the 0600 EnvironmentFile literals that carry it. Mirrors
// websafe.go's websafeSecretEnv* trio and searxng.go's searxngSecretEnv* pair —
// the same "the render never reads the secret, only the path" discipline.
//
// One file, two keys: LLAMA_API_KEY is what llama-server itself reads natively
// (its documented --api-key env var), and OPENAI_API_KEY is what Open WebUI's own
// container process needs for its OPENAI_API_KEY/OPENAI_API_KEYS connection env —
// same secret VALUE, two consumer-side names, so a single 0600 file protects
// every process that must present it. villa-llama, every resident slot,
// villa-openwebui and villa-inferproxy all reference this ONE path; none of them
// re-types the secret VALUE, only this path.

import "strings"

// inferenceSecretEnvFileName is the BARE filename of the 0600 env file carrying
// the LLAMA_API_KEY / OPENAI_API_KEY bearer. Single source the container units
// (via EnvironmentFile=) and the writer (RenderInferenceSecretEnv) share.
const inferenceSecretEnvFileName = "llama.env"

// inferenceSecretEnvFilePath is the container-side EnvironmentFile= path every
// consumer unit references. %h is Quadlet's home-dir specifier (resolved at
// unit-load), so the env file lives in the villa inference config dir. Unlike
// villa-websafe's file, this one is referenced UNCONDITIONALLY (inference has no
// opt-in gate) by every unit parseContainerArgs builds.
const inferenceSecretEnvFilePath = "%h/.config/villa/inference/" + inferenceSecretEnvFileName

// InferenceSecretEnvFilePath returns the EnvironmentFile= path villa-llama, every
// resident slot, villa-openwebui and villa-inferproxy all reference. EXPORTED so
// the install/writer flow can persist the secret at exactly this path (mirrors
// WebsafeSecretEnvFilePath).
func InferenceSecretEnvFilePath() string { return inferenceSecretEnvFilePath }

// RenderInferenceSecretEnv renders the 0600 env-file body the writer persists and
// EVERY inference-adjacent unit references via EnvironmentFile= (mirrors
// RenderWebsafeSecretEnv/RenderSearxngSecretEnv). It is the SINGLE source of the
// env-file FORMAT — two fixed `KEY=<value>` lines, no shell interpolation. The
// secret value is the crypto/rand secret from config.InferenceSecret; it is NEVER
// logged and NEVER rendered into any 0644 unit.
func RenderInferenceSecretEnv(secret string) (name, text string) {
	return inferenceSecretEnvFileName, inferenceSecretEnvBody(secret)
}

// inferenceSecretEnvBody renders the fixed two-line body: llama-server's own env
// var and Open WebUI's, both set to the SAME secret so one file protects both
// consumer-side names (see file doc comment above).
func inferenceSecretEnvBody(secret string) string {
	lines := []string{
		"LLAMA_API_KEY=" + secret,
		"OPENAI_API_KEY=" + secret,
	}
	return strings.Join(lines, "\n") + "\n"
}
