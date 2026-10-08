package install

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// flow_subsystems_test.go drives Run through the opt-in subsystem paths the flow
// gates on the PERSISTED config: the memory-stack skew WARN (CTRL-05), the SearXNG
// wiring (Phase-29 Task 2) and the villa-websafe wiring (Phase-31). The proof
// verdict cores (evalMemoryProof / evalSearxngProof / evalAgentProof) stay in the
// command tier with their own tests; here every proof is a fake seam returning a
// controlled Proof, so what is asserted is the ORDER and GATING of the effects.

// searxngUnits builds a realistic web-search-on plan: Render appends the searxng
// container unit when WebSearchEnabled, so it MUST be present in the plan for the
// start guard (which gates the searxng start on the unit actually being in the plan).
func searxngUnits() ([]orchestrate.Unit, orchestrate.Plan) {
	units := []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "[Container]\n"},
		{Name: orchestrate.SearXNGContainerUnitName(), Text: "[Container]\n"},
		// Phase-31: web search on also renders the villa-websafe unit; its presence in the plan
		// satisfies the websafe start gate (UnitPresent) the install flow checks.
		{Name: orchestrate.WebsafeContainerUnitName(), Text: "[Container]\n"},
	}
	return units, orchestrate.Plan{Changed: units}
}

// TestInstallMemorySkewWarn drives Run through the memory-on fixture with a
// controllable recall-state seam and asserts the WARN matrix: confident mismatch ⇒
// one WARN line with remediation, everything else (empty stamp, matching stamp,
// unreadable state, memory off) ⇒ silence. Exit codes and the memory proof flow
// are unchanged in every case (read-only): never a block, never an exit-code
// change, never a state write, never an auto-reindex. The comparison is the single
// recall.EmbeddingSkew helper; the state read goes through the injectable
// ReadRecallState seam so these tests stay hermetic.
func TestInstallMemorySkewWarn(t *testing.T) {
	stamped := recall.State{
		KnowledgeID:    "kb1",
		EmbeddingModel: "old-embed-model",
		EmbeddingDim:   512,
	}

	t.Run("confident mismatch WARNs with remediation, read-only, exit unchanged", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		reads := 0
		f.ReadRecallState = func() (recall.State, error) {
			reads++
			return stamped, nil
		}

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("skew WARN must NEVER block: exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		msg := errOut.String()
		for _, want := range []string{
			"WARN",
			"old-embed-model", "512", // the stamped identity
			"nomic-embed-text-v1.5", "768", // the configured identity (DefaultVillaConfig)
			"villa recall index --rebuild", // the sanctioned re-index
			"revert",                       // ...or revert embedding_model/embedding_dim
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("install skew WARN must contain %q; stderr = %q", want, msg)
			}
		}
		// Read-only: exactly one state read, the proof still ran, and no extra
		// mutation fired (the seam surface offers no recall-state writer at all;
		// SaveConfig's single call is install's own config persist, unrelated).
		if reads != 1 {
			t.Errorf("ReadRecallState calls = %d, want exactly 1", reads)
		}
		if f.memoryProofCalls != 1 {
			t.Errorf("the WARN must not displace the readiness proof, proof calls = %d", f.memoryProofCalls)
		}
	})

	t.Run("empty stamp is typed-Unknown - silent", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.ReadRecallState = func() (recall.State, error) {
			return recall.State{}, nil // nothing recorded (fresh install / pre-stamp store)
		}

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "recall index") {
			t.Errorf("an empty stamp must raise no alarm (typed-Unknown); stderr = %q", errOut.String())
		}
	})

	t.Run("matching stamp is silent", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.ReadRecallState = func() (recall.State, error) {
			return recall.State{EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768}, nil
		}

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "recall index") {
			t.Errorf("a matching stamp must print nothing; stderr = %q", errOut.String())
		}
	})

	t.Run("unreadable state is typed-Unknown - silent, never a block", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.ReadRecallState = func() (recall.State, error) {
			return recall.State{}, errors.New("permission denied")
		}

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("an unevaluable state read must never change the exit: %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "recall index") {
			t.Errorf("an unevaluable read must raise no alarm (typed-Unknown); stderr = %q", errOut.String())
		}
	})

	t.Run("memory off never reads the recall state", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = false
		reads := 0
		f.ReadRecallState = func() (recall.State, error) {
			reads++
			return stamped, nil
		}

		_, _, _ = f.run(Opts{})
		if reads != 0 {
			t.Errorf("the skew WARN is memory-on only; ReadRecallState calls = %d, want 0", reads)
		}
	})

	t.Run("nil seam is safe - no panic, silent", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		// ReadRecallState left nil (the test-double default): the WARN helper must
		// degrade silently, mirroring the doctor optional-seam pattern.

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "recall index") {
			t.Errorf("a nil seam must be silent; stderr = %q", errOut.String())
		}
	})
}

// TestInstallWebSearchWiring is the Task-2 install-flow gate: with web search ON, install
// writes BOTH config artifacts (settings.yml + the 0600 secret env) BEFORE starting the
// searxng service, then runs the readiness proof and folds a PASS into success / refuses
// a FAIL with exitBlocked. With web search OFF, NONE of the searxng seams fire (the
// web-search-off install is byte-identical — no start, no proof, no writes).
func TestInstallWebSearchWiring(t *testing.T) {
	t.Run("web search on: writes config + starts + proves, PASS folds into success", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true
		f.searxngProofStatus = preflight.StatusPass

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("web-search-on install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.searxngSettingsCalls != 1 {
			t.Errorf("settings.yml must be written once, WriteSearxngSettings calls = %d", f.searxngSettingsCalls)
		}
		if f.searxngSecretEnvCalls != 1 {
			t.Errorf("the 0600 secret env file must be written once, WriteSearxngSecretEnv calls = %d", f.searxngSecretEnvCalls)
		}
		if !slices.Contains(f.startOrder, searxngServiceName) {
			t.Errorf("web-search-on must start %s, startOrder = %v", searxngServiceName, f.startOrder)
		}
		if f.searxngProofCalls != 1 {
			t.Errorf("web-search-on must run the readiness proof once, proof calls = %d", f.searxngProofCalls)
		}
		// BOTH config files must be written BEFORE the searxng service starts (Pitfall 3:
		// the container reads its settings.yml + secret on first boot).
		startIdx := slices.Index(f.callOrder, "start:"+searxngServiceName)
		if startIdx < 0 {
			t.Fatalf("searxng start not recorded in callOrder = %v", f.callOrder)
		}
		if si := slices.Index(f.callOrder, "writeSearxngSettings"); si < 0 || si > startIdx {
			t.Errorf("settings.yml must be written BEFORE the searxng start (settings idx=%d, start idx=%d); callOrder = %v", si, startIdx, f.callOrder)
		}
		if ei := slices.Index(f.callOrder, "writeSearxngSecretEnv"); ei < 0 || ei > startIdx {
			t.Errorf("the secret env file must be written BEFORE the searxng start (env idx=%d, start idx=%d); callOrder = %v", ei, startIdx, f.callOrder)
		}
	})

	t.Run("web search on: proof FAIL refuses with exitBlocked", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true
		f.searxngProofStatus = preflight.StatusFail
		f.searxngProofDetail = "the search service did not answer"

		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("a searxng proof FAIL must return exitBlocked, got %d; stderr = %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "search service not ready") {
			t.Errorf("the FAIL must refuse-with-remediation naming the search service; stderr = %q", errOut.String())
		}
	})

	t.Run("web search on but unit absent from plan: fails closed", func(t *testing.T) {
		// A web-search-on install whose searxng unit is NOT in the rendered plan must fail
		// closed with an INTERNAL-ERROR remediation — never `systemctl start` a unit systemd
		// has never seen.
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		plan := orchestrate.Plan{Changed: units}
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true

		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("a missing searxng unit must fail closed (exitBlocked), got %d; stderr = %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "INTERNAL ERROR") {
			t.Errorf("a missing searxng unit must surface an INTERNAL-ERROR remediation; stderr = %q", errOut.String())
		}
		if slices.Contains(f.startOrder, searxngServiceName) {
			t.Errorf("a unit absent from the plan must NOT be started, startOrder = %v", f.startOrder)
		}
		if f.searxngProofCalls != 0 {
			t.Errorf("a fail-closed start gate must not reach the proof, proof calls = %d", f.searxngProofCalls)
		}
	})

	t.Run("web search off: no start, no proof, no config writes (byte-identical path)", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = false

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("web-search-off install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.searxngSettingsCalls != 0 || f.searxngSecretEnvCalls != 0 {
			t.Errorf("web-search-off must write no config files, settings=%d env=%d", f.searxngSettingsCalls, f.searxngSecretEnvCalls)
		}
		if slices.Contains(f.startOrder, searxngServiceName) {
			t.Errorf("web-search-off must not start the searxng service, startOrder = %v", f.startOrder)
		}
		if f.searxngProofCalls != 0 {
			t.Errorf("web-search-off must not run the readiness proof, proof calls = %d", f.searxngProofCalls)
		}
	})
}

// TestInstallWebsafeWiring is the websafe install-flow gate (UAT gap): the install verb must
// generate+persist the bearer, write the 0600 websafe.env BEFORE the OWUI start, and start
// villa-websafe when web search is on — and do NONE of that when web search is off (the
// install path is byte-identical to v1.4).
func TestInstallWebsafeWiring(t *testing.T) {
	t.Run("web search on: writes the 0600 bearer once BEFORE the OWUI start and starts villa-websafe", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true
		f.searxngProofStatus = preflight.StatusPass

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("web-search-on install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		// The 0600 bearer env file must be written exactly once via the Deps seam.
		if f.websafeSecretEnvCalls != 1 {
			t.Errorf("the 0600 websafe.env bearer must be written once, WriteWebsafeSecretEnv calls = %d", f.websafeSecretEnvCalls)
		}
		// It MUST be written BEFORE the OWUI start: when web search is on the OWUI unit references
		// the SAME websafe.env via EnvironmentFile=, so `systemctl start` would fail if the file
		// were absent (the exact UAT gap).
		owuiIdx := slices.Index(f.callOrder, "start:"+openWebUIServiceName)
		if owuiIdx < 0 {
			t.Fatalf("OWUI start not recorded in callOrder = %v", f.callOrder)
		}
		if ei := slices.Index(f.callOrder, "writeWebsafeSecretEnv"); ei < 0 || ei > owuiIdx {
			t.Errorf("the websafe.env bearer must be written BEFORE the OWUI start (env idx=%d, owui idx=%d); callOrder = %v", ei, owuiIdx, f.callOrder)
		}
		// villa-websafe must be started.
		if !slices.Contains(f.startOrder, websafeServiceName) {
			t.Errorf("web-search-on must start %s, startOrder = %v", websafeServiceName, f.startOrder)
		}
	})

	t.Run("web search on: generates+persists the bearer when empty (SaveConfig)", func(t *testing.T) {
		units, plan := searxngUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = true

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("web-search-on install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		// The bearer is generated ONCE on first opt-in and persisted via SaveConfig — a re-install
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
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = false

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("web-search-off install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.websafeSecretEnvCalls != 0 {
			t.Errorf("web-search-off must write no websafe.env bearer, WriteWebsafeSecretEnv calls = %d", f.websafeSecretEnvCalls)
		}
		if slices.Contains(f.startOrder, websafeServiceName) {
			t.Errorf("web-search-off must not start %s, startOrder = %v", websafeServiceName, f.startOrder)
		}
		if strings.Contains(errOut.String(), "websafe") {
			t.Errorf("web-search-off must not mention websafe on stderr; stderr = %q", errOut.String())
		}
	})
}

// TestInstallWebSearchRefusalsEndInNewline guards the two INTERNAL ERROR backstops
// that fire when a web-search unit is missing from the rendered plan: the refusal
// must terminate the stderr line with a real newline, not the two bytes `\n`
// (#118). The refusals fire rarely, which is how the escaped terminator survived.
func TestInstallWebSearchRefusalsEndInNewline(t *testing.T) {
	for _, tc := range []struct {
		missing string
	}{
		{missing: orchestrate.SearXNGContainerUnitName()},
		{missing: orchestrate.WebsafeContainerUnitName()},
	} {
		t.Run(tc.missing, func(t *testing.T) {
			units, _ := searxngUnits()
			units = slices.DeleteFunc(units, func(u orchestrate.Unit) bool { return u.Name == tc.missing })
			f := newFakeDeps(t, units, orchestrate.Plan{Changed: units}, passChecks())
			f.webSearchEnabled = true

			code, _, errOut := f.run(Opts{})
			if code == exitPass {
				t.Fatalf("a missing %s must refuse, got exitPass", tc.missing)
			}
			out := errOut.String()
			if !strings.Contains(out, "INTERNAL ERROR") {
				t.Fatalf("expected the INTERNAL ERROR refusal on stderr, got %q", out)
			}
			if strings.Contains(out, `\n`) || !strings.HasSuffix(out, "\n") {
				t.Errorf("refusal must end in a real newline, got %q", out)
			}
		})
	}
}

// voiceUnits builds a voice-on plan: Render emits both voice units when the gate is
// on, so both must be in the plan for the start gates to pass.
func voiceUnits() ([]orchestrate.Unit, orchestrate.Plan) {
	units := []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "[Container]\n"},
		{Name: orchestrate.STTContainerUnitName(), Text: "[Container]\n"},
		{Name: orchestrate.TTSContainerUnitName(), Text: "[Container]\n"},
	}
	return units, orchestrate.Plan{Changed: units}
}

// TestInstallVoiceWiring: with voice on, install pre-stages the whisper model before
// any start, starts villa-stt then villa-tts after the chat UI, and proves the spoken
// round trip once. A FAIL refuses and rolls back; voice off touches none of it.
func TestInstallVoiceWiring(t *testing.T) {
	t.Run("voice on: pre-stages, starts both units after the chat UI, proves, PASS folds into success", func(t *testing.T) {
		units, plan := voiceUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.voiceEnabled = true
		f.voicePresent = false
		f.voiceProofDetail = `heard "the quick brown fox" (agreement 1.00)`

		code, out, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("voice-on install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.voiceEnsureCalls != 1 {
			t.Errorf("an absent whisper model must be pre-staged once, EnsureVoiceModel calls = %d", f.voiceEnsureCalls)
		}
		ensure := slices.Index(f.callOrder, "ensureVoiceModel")
		firstStart := slices.IndexFunc(f.callOrder, func(c string) bool { return strings.HasPrefix(c, "start:") })
		if ensure < 0 || firstStart < 0 || ensure > firstStart {
			t.Errorf("the whisper model must be staged before any start; callOrder = %v", f.callOrder)
		}
		chat := slices.Index(f.startOrder, openWebUIServiceName)
		stt := slices.Index(f.startOrder, sttServiceName)
		tts := slices.Index(f.startOrder, ttsServiceName)
		if chat < 0 || stt < 0 || tts < 0 || chat > stt || stt > tts {
			t.Errorf("want the chat UI, then %s, then %s; startOrder = %v", sttServiceName, ttsServiceName, f.startOrder)
		}
		if f.voiceProofCalls != 1 {
			t.Errorf("voice-on must run the round-trip proof once, proof calls = %d", f.voiceProofCalls)
		}
		if slices.Index(f.callOrder, "voiceProof") < slices.Index(f.callOrder, "start:"+ttsServiceName) {
			t.Errorf("the proof must follow both starts; callOrder = %v", f.callOrder)
		}
		if !strings.Contains(out.String(), `voice ready: heard "the quick brown fox" (agreement 1.00)`) {
			t.Errorf("a PASS must narrate the proof's detail; stdout = %q", out.String())
		}
	})

	t.Run("--voice turns the gate on and persists it", func(t *testing.T) {
		units, plan := voiceUnits()
		f := newFakeDeps(t, units, plan, passChecks())

		code, _, errOut := f.run(Opts{Voice: true})
		if code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if !f.savedCfg.VoiceEnabled {
			t.Error("--voice must persist voice_enabled so a later bare install keeps it")
		}
		if !slices.Contains(f.startOrder, sttServiceName) || f.voiceProofCalls != 1 {
			t.Errorf("--voice must start and prove the voice units; startOrder = %v, proof calls = %d", f.startOrder, f.voiceProofCalls)
		}
	})

	t.Run("a pre-stage failure blocks before any mutation", func(t *testing.T) {
		units, plan := voiceUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.voiceEnabled = true
		f.voicePresent = false
		f.EnsureVoiceModel = func(string) error { return errors.New("checksum mismatch") }

		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("exit = %d, want exitBlocked; stderr = %q", code, errOut.String())
		}
		want := "install: pre-stage speech model " + orchestrate.WhisperModelFilename() + " failed: checksum mismatch"
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr = %q, want %q", errOut.String(), want)
		}
		if f.saveCalls != 0 || f.startCalls != 0 {
			t.Errorf("a pre-stage failure must not mutate: save=%d start=%d", f.saveCalls, f.startCalls)
		}
	})

	t.Run("proof FAIL refuses and rolls back", func(t *testing.T) {
		units, plan := voiceUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.voiceEnabled = true
		f.voiceProofStatus = preflight.StatusFail
		f.voiceProofDetail = "villa-tts returned no audio"

		res, _, errOut := f.runResult(Opts{})
		if res.Outcome != Refused {
			t.Fatalf("outcome = %q, want %q; stderr = %q", res.Outcome, Refused, errOut.String())
		}
		if res.Outcome.ExitCode() != exitBlocked {
			t.Errorf("exit = %d, want exitBlocked", res.Outcome.ExitCode())
		}
		if !strings.Contains(errOut.String(), "install: voice not ready: villa-tts returned no audio") {
			t.Errorf("the FAIL must name voice and carry the detail; stderr = %q", errOut.String())
		}
	})

	for _, missing := range []string{orchestrate.STTContainerUnitName(), orchestrate.TTSContainerUnitName()} {
		t.Run("voice on but "+missing+" absent from the plan fails closed", func(t *testing.T) {
			units, _ := voiceUnits()
			units = slices.DeleteFunc(units, func(u orchestrate.Unit) bool { return u.Name == missing })
			f := newFakeDeps(t, units, orchestrate.Plan{Changed: units}, passChecks())
			f.voiceEnabled = true

			code, _, errOut := f.run(Opts{})
			if code != exitBlocked {
				t.Fatalf("exit = %d, want exitBlocked; stderr = %q", code, errOut.String())
			}
			if !strings.Contains(errOut.String(), "INTERNAL ERROR") || !strings.Contains(errOut.String(), missing) {
				t.Errorf("want the INTERNAL ERROR refusal naming %s; stderr = %q", missing, errOut.String())
			}
			if slices.Contains(f.startOrder, sttServiceName) || slices.Contains(f.startOrder, ttsServiceName) {
				t.Errorf("no voice unit may start when either is absent; startOrder = %v", f.startOrder)
			}
			if f.voiceProofCalls != 0 {
				t.Errorf("a fail-closed start gate must not reach the proof, proof calls = %d", f.voiceProofCalls)
			}
		})
	}

	t.Run("voice off: no pre-stage, no start, no proof", func(t *testing.T) {
		units, plan := voiceUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.voicePresent = false

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.voiceEnsureCalls != 0 || f.voiceProofCalls != 0 {
			t.Errorf("voice off must stage and prove nothing: ensure=%d proof=%d", f.voiceEnsureCalls, f.voiceProofCalls)
		}
		if slices.Contains(f.startOrder, sttServiceName) || slices.Contains(f.startOrder, ttsServiceName) {
			t.Errorf("voice off must start no voice unit; startOrder = %v", f.startOrder)
		}
	})

	t.Run("a voice service running before the install is restarted on rollback, not stopped", func(t *testing.T) {
		units, plan := voiceUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.voiceEnabled = true
		f.voiceProofStatus = preflight.StatusFail
		f.activeState = "active"
		for _, u := range units {
			f.priorUnits[u.Name] = "[Container]\nImage=prior\n"
		}

		f.run(Opts{})
		for _, svc := range []string{sttServiceName, ttsServiceName} {
			lastRestart := lastIndex(f.callOrder, "restart:"+svc)
			if lastRestart < 0 {
				t.Errorf("%s was running before the install, so the rollback must restart it; callOrder = %v", svc, f.callOrder)
				continue
			}
			if lastIndex(f.callOrder, "stop:"+svc) > lastRestart || lastIndex(f.callOrder, "start:"+svc) > lastRestart {
				t.Errorf("%s: the rollback's restart must be the last call for it; callOrder = %v", svc, f.callOrder)
			}
		}
	})
}

// TestInstallVoiceReservesBeforeTheFit: the install that turns voice on sizes its
// pick against the config it persists, so the first `install --voice` already
// subtracts the stt and tts rows rather than leaving them to the next run.
func TestInstallVoiceReservesBeforeTheFit(t *testing.T) {
	units, plan := voiceUnits()
	f := newFakeDeps(t, units, plan, passChecks())

	if code, _, errOut := f.run(Opts{Voice: true}); code != exitPass {
		t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}
	if got := rows(f.pickReservations); got != "stt,tts" {
		t.Errorf("Pick received reservations %q, want stt,tts for the config install persists", got)
	}
	if got := rows(PlannedReservations(config.VillaConfig{}, Opts{Voice: true})); got != "stt,tts" {
		t.Errorf("PlannedReservations with --voice = %q, want stt,tts", got)
	}
}
