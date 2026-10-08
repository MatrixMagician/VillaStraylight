package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/eval"
	"github.com/MatrixMagician/VillaStraylight/internal/evalstore"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// evalTestSuite is a fixed five-case suite, so the verb's contract is frozen
// independently of the embedded cases.json. The last case is a rerank case, which
// the test config (memory off) skips.
var evalTestSuite = []eval.Case{
	{ID: "arith-a", Prompt: "pa", MaxTokens: 8, Grader: eval.Grader{Kind: eval.KindExact, Want: "4"}},
	{ID: "code-b", Prompt: "pb", MaxTokens: 8, Grader: eval.Grader{Kind: eval.KindExact, Want: "30"}},
	{ID: "json-c", Prompt: "pc", MaxTokens: 8, Grader: eval.Grader{Kind: eval.KindExact, Want: "x"}},
	{ID: "tool-d", Prompt: "pd", MaxTokens: 8, Tools: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "f"}}},
		Grader: eval.Grader{Kind: eval.KindTool, Want: "f", Keys: map[string]any{"x": 1.0}}},
	{ID: "retrieval-e", Prompt: "pe", Documents: []string{"d0", "d1"}, Grader: eval.Grader{Kind: eval.KindRerank, Top: 1}},
}

// evalCompletionCases counts the suite's cases that are sent as completions.
func evalCompletionCases() int {
	n := 0
	for _, c := range evalTestSuite {
		if c.Grader.Kind != eval.KindRerank {
			n++
		}
	}
	return n
}

// evalTestConfig is a tools-off chat stack.
func evalTestConfig() config.VillaConfig {
	return config.VillaConfig{Model: "qwen-test", Quant: "Q4_K_M", Ctx: 8192, Backend: "rocm", InferenceSecret: "k"}
}

// evalStoreMem is a buffer-backed eval-baselines.json.
type evalStoreMem struct{ data []byte }

func (m *evalStoreMem) deps() evalstore.Deps {
	return evalstore.Deps{
		ReadAll:  func() ([]byte, error) { return m.data, nil },
		WriteAll: func(b []byte) error { m.data = append([]byte(nil), b...); return nil },
	}
}

// seed records results as the eval baseline for the test config's key, with an
// older image digest so the report names a provenance change.
func (m *evalStoreMem) seed(t *testing.T, results ...eval.Result) {
	t.Helper()
	b := eval.Baseline{
		Key:        eval.Key{Model: "qwen-test", Quant: "Q4_K_M", SuiteVersion: eval.SuiteVersion},
		Provenance: eval.Provenance{Backend: "rocm", ImageDigest: "sha256:0000", Speculation: "off", Ctx: 8192},
		Results:    results,
	}
	if _, err := evalstore.Put(m.deps(), b); err != nil {
		t.Fatal(err)
	}
}

// fakeEvalDeps answers each prompt from replies; a prompt with no reply is a
// transport error, so its case is unconducted. sent records every request.
func fakeEvalDeps(cfg config.VillaConfig, store *evalStoreMem, replies map[string]string, sent *[]llm.ChatRequest) evalDeps {
	return evalDeps{
		loadedConfig: func() (config.VillaConfig, error) { return cfg, nil },
		inferenceImage: func(config.VillaConfig) (string, string, error) {
			return "rocm", "registry.invalid/llama:test@sha256:1111", nil
		},
		suite: func() ([]eval.Case, error) { return evalTestSuite, nil },
		complete: func(_ config.VillaConfig, model string) func(context.Context, llm.ChatRequest) (llm.Reply, error) {
			return func(_ context.Context, req llm.ChatRequest) (llm.Reply, error) {
				req.Model = model
				if sent != nil {
					*sent = append(*sent, req)
				}
				if r, ok := replies[req.Messages[len(req.Messages)-1].Content]; ok {
					return llm.Reply{Content: r}, nil
				}
				return llm.Reply{}, errors.New("connection refused")
			}
		},
		rerank: func(context.Context, string, []string) ([]float64, error) { return []float64{-2, 0.5}, nil },
		store:  store.deps(),
	}
}

// runEvalCmd runs the verb body and returns its exit code, stdout and stderr.
func runEvalCmd(record, asJSON bool, d evalDeps) (int, string, string) {
	var out, errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())
	code := runEval(cmd, record, asJSON, d)
	return code, out.String(), errOut.String()
}

// TestEvalReportGolden freezes `villa eval`'s human report and its --json schema-1
// contract (ADR-0018) over one run that exercises every list: a regression (with its
// reply excerpt), an improvement, an unconducted case, a tool case skipped with tools
// mode off, and a changed image digest. The regression outranks the unconducted
// case, so the verdict is FAIL and the exit code is the family's blocked code. Run
// with -update to refreeze.
func TestEvalReportGolden(t *testing.T) {
	newStore := func() *evalStoreMem {
		s := &evalStoreMem{}
		s.seed(t,
			eval.Result{CaseID: "arith-a", Status: eval.Passed, Excerpt: "4"},
			eval.Result{CaseID: "code-b", Status: eval.Failed, Excerpt: "31"},
			eval.Result{CaseID: "json-c", Status: eval.Passed, Excerpt: "x"},
			eval.Result{CaseID: "tool-d", Status: eval.Skipped, Detail: "tools mode off"})
		return s
	}
	replies := map[string]string{"pa": "The answer is 5", "pb": "30"}

	code, out, _ := runEvalCmd(false, true, fakeEvalDeps(evalTestConfig(), newStore(), replies, nil))
	if code != exitBlocked {
		t.Errorf("--json exit = %d, want exitBlocked (%d)", code, exitBlocked)
	}
	assertGolden(t, "eval.json.golden", []byte(out))

	code, out, errOut := runEvalCmd(false, false, fakeEvalDeps(evalTestConfig(), newStore(), replies, nil))
	if code != exitBlocked {
		t.Errorf("human exit = %d, want exitBlocked (%d)", code, exitBlocked)
	}
	assertGolden(t, "eval.golden", []byte(out+"--- stderr ---\n"+errOut))
}

// TestEvalRecordGolden freezes the --record shape of the --json contract: a complete
// run is written as the eval baseline and the score it replaces is reported; a run
// with an unconducted case is refused, writes nothing, and exits with the family's
// Reject code.
func TestEvalRecordGolden(t *testing.T) {
	store := &evalStoreMem{}
	store.seed(t,
		eval.Result{CaseID: "arith-a", Status: eval.Failed, Excerpt: "5"},
		eval.Result{CaseID: "code-b", Status: eval.Failed, Excerpt: "31"},
		eval.Result{CaseID: "json-c", Status: eval.Passed, Excerpt: "x"},
		eval.Result{CaseID: "tool-d", Status: eval.Skipped, Detail: "tools mode off"})
	complete := map[string]string{"pa": "4", "pb": "30", "pc": "x"}

	code, out, _ := runEvalCmd(true, true, fakeEvalDeps(evalTestConfig(), store, complete, nil))
	if code != exitPass {
		t.Errorf("record exit = %d, want exitPass (%d)", code, exitPass)
	}
	assertGolden(t, "eval-record.json.golden", []byte(out))

	doc, err := evalstore.Load(store.deps())
	if err != nil || len(doc.Baselines) != 1 || eval.Tally(doc.Baselines[0].Results).Passed != 3 {
		t.Fatalf("store after record = %+v, %v; want the new run as the one baseline", doc, err)
	}

	before := append([]byte(nil), store.data...)
	code, _, errOut := runEvalCmd(true, false, fakeEvalDeps(evalTestConfig(), store, map[string]string{"pa": "4"}, nil))
	if code != exitWarn {
		t.Errorf("refused record exit = %d, want exitWarn (%d)", code, exitWarn)
	}
	if !bytes.Equal(before, store.data) {
		t.Error("a record with unconducted cases wrote the store")
	}
	if !strings.Contains(errOut, "record: refused") {
		t.Errorf("stderr = %q, want the refusal", errOut)
	}
}

// TestEvalExitCodes pins the verify family's exit contract on eval (ADR-0018): a
// clean comparison exits pass, a run with no eval baseline is Reject (warn, never
// pass), and an unreadable config, a failed suite load, an unreadable store or a
// failed baseline write blocks.
func TestEvalExitCodes(t *testing.T) {
	all := map[string]string{"pa": "4", "pb": "30", "pc": "x"}
	seeded := func() *evalStoreMem {
		s := &evalStoreMem{}
		s.seed(t,
			eval.Result{CaseID: "arith-a", Status: eval.Passed}, eval.Result{CaseID: "code-b", Status: eval.Passed},
			eval.Result{CaseID: "json-c", Status: eval.Passed}, eval.Result{CaseID: "tool-d", Status: eval.Skipped})
		return s
	}

	if code, out, errOut := runEvalCmd(false, false, fakeEvalDeps(evalTestConfig(), seeded(), all, nil)); code != exitPass ||
		!strings.Contains(out, "eval: PASS") || errOut != "" {
		t.Errorf("clean run: exit %d, stdout %q, stderr %q; want exitPass with the verdict on stdout", code, out, errOut)
	}
	if code, _, errOut := runEvalCmd(false, false, fakeEvalDeps(evalTestConfig(), &evalStoreMem{}, all, nil)); code != exitWarn ||
		!strings.Contains(errOut, "no eval baseline") {
		t.Errorf("no baseline: exit %d, stderr %q; want exitWarn naming the missing baseline", code, errOut)
	}

	for name, mutate := range map[string]func(*evalDeps){
		"config": func(d *evalDeps) {
			d.loadedConfig = func() (config.VillaConfig, error) { return config.VillaConfig{}, errors.New("bad toml") }
		},
		"backend": func(d *evalDeps) {
			d.inferenceImage = func(config.VillaConfig) (string, string, error) { return "", "", errors.New("unknown backend") }
		},
		"suite": func(d *evalDeps) { d.suite = func() ([]eval.Case, error) { return nil, errors.New("bad suite") } },
		"store read": func(d *evalDeps) {
			d.store.ReadAll = func() ([]byte, error) { return nil, errors.New("permission denied") }
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := fakeEvalDeps(evalTestConfig(), seeded(), all, nil)
			mutate(&d)
			if code, _, errOut := runEvalCmd(false, false, d); code != exitBlocked || !strings.HasPrefix(errOut, "eval: ") {
				t.Errorf("exit %d, stderr %q; want exitBlocked with the reason", code, errOut)
			}
		})
	}

	d := fakeEvalDeps(evalTestConfig(), seeded(), all, nil)
	d.store.WriteAll = func([]byte) error { return errors.New("disk full") }
	if code, _, errOut := runEvalCmd(true, false, d); code != exitBlocked || !strings.Contains(errOut, "disk full") {
		t.Errorf("failed write: exit %d, stderr %q; want exitBlocked naming the write error", code, errOut)
	}
}

// TestEvalSendsGreedyRequestsToTheServedModel: the verb's completions carry the
// served model (the coder in swap-residency coding mode) and the suite's greedy
// request, and with tools mode on the tool case is sent with its tools rather than
// skipped.
func TestEvalSendsGreedyRequestsToTheServedModel(t *testing.T) {
	cfg := evalTestConfig()
	cfg.CodingMode, cfg.CoderModel, cfg.CoderQuant, cfg.CoderAgentCtx = true, "coder-test", "Q8_0", 32768
	var sent []llm.ChatRequest
	runEvalCmd(false, false, fakeEvalDeps(cfg, &evalStoreMem{}, map[string]string{}, &sent))

	if len(sent) != evalCompletionCases() {
		t.Fatalf("sent %d requests, want %d (coding mode turns tools on, so no completion case is skipped)", len(sent), evalCompletionCases())
	}
	for _, req := range sent {
		if req.Model != "coder-test" || req.Temperature == nil || *req.Temperature != 0 || req.MaxTokens != 8 {
			t.Errorf("request = model %q, temperature %v, max_tokens %d", req.Model, req.Temperature, req.MaxTokens)
		}
	}
	if len(sent[3].Tools) != 1 {
		t.Errorf("tool case sent without its tools: %+v", sent[3])
	}
}

// TestEvalTargetKeysOnTheServedModel guards ADR-0018's key/provenance split: the
// key is the served model and its quant (the coder's in swap-residency coding mode),
// and the stack settings (backend, image digest, speculation with unset meaning off,
// served ctx, effective tools mode) are provenance.
func TestEvalTargetKeysOnTheServedModel(t *testing.T) {
	chat := evalTarget(evalTestConfig(), "rocm", "registry.invalid/llama:test@sha256:1111")
	if chat.Key != (eval.Key{Model: "qwen-test", Quant: "Q4_K_M", SuiteVersion: eval.SuiteVersion}) {
		t.Errorf("chat key = %+v", chat.Key)
	}
	if chat.Provenance != (eval.Provenance{Backend: "rocm", ImageDigest: "sha256:1111", Speculation: "off", Ctx: 8192}) {
		t.Errorf("chat provenance = %+v", chat.Provenance)
	}

	cfg := evalTestConfig()
	cfg.CodingMode, cfg.CoderModel, cfg.CoderQuant, cfg.CoderAgentCtx, cfg.Speculation = true, "coder-test", "Q8_0", 32768, "ngram"
	coder := evalTarget(cfg, "vulkan", "registry.invalid/llama:tag-only")
	if coder.Key.Model != "coder-test" || coder.Key.Quant != "Q8_0" {
		t.Errorf("coder key = %+v, want the coder and its quant", coder.Key)
	}
	if p := coder.Provenance; p.Ctx != 32768 || !p.ToolsMode || p.Speculation != "ngram" || p.ImageDigest != "registry.invalid/llama:tag-only" {
		t.Errorf("coder provenance = %+v", p)
	}
}

// TestLiveEvalImageResolvesThroughThePinPath: the provenance image comes from the
// configured backend through the pin path (on a host with no pin state, the vetted
// pin the backend seam carries), and an unknown backend fails closed rather than
// defaulting.
func TestLiveEvalImageResolvesThroughThePinPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, _, err := liveEvalImage(config.VillaConfig{Backend: "no-such-backend"}); err == nil {
		t.Fatal("an unknown backend resolved")
	}
	want, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatal(err)
	}
	name, ref, err := liveEvalImage(config.VillaConfig{Backend: "vulkan"})
	if err != nil || name != want.Name() || ref != want.Image() {
		t.Errorf("liveEvalImage = %q, %q, %v; want %q, %q", name, ref, err, want.Name(), want.Image())
	}
}

// TestEvalVerbIsRegistered: `villa eval` is on the root with --record and --json.
func TestEvalVerbIsRegistered(t *testing.T) {
	for _, c := range newRoot().Commands() {
		if c.Name() == "eval" {
			for _, f := range []string{"record", "json"} {
				if c.Flags().Lookup(f) == nil {
					t.Errorf("villa eval has no --%s", f)
				}
			}
			return
		}
	}
	t.Fatal("villa eval is not registered on the root command")
}

// TestEvalJSONIsOneDocument: --json writes exactly one JSON document to stdout and
// nothing to stderr, whatever the verdict.
func TestEvalJSONIsOneDocument(t *testing.T) {
	_, out, errOut := runEvalCmd(false, true, fakeEvalDeps(evalTestConfig(), &evalStoreMem{}, map[string]string{}, nil))
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil || errOut != "" {
		t.Fatalf("stdout is not one JSON document (%v) or stderr is not empty (%q)", err, errOut)
	}
	if v["schema"] != 1.0 || v["verdict"] != "REJECT" {
		t.Errorf("schema/verdict = %v/%v", v["schema"], v["verdict"])
	}
}
