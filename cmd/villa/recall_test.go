package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/openwebui"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// newRecallCmd builds the bare cobra harness the run bodies are driven through
// off-hardware (verify_memory_test.go precedent): a context-bearing command with
// out/err buffers attached per test.
func newRecallCmd() *cobra.Command {
	c := &cobra.Command{}
	c.SetContext(context.Background())
	return c
}

// renderableChatDoc returns a minimal chat document RenderTranscript can render
// (one user turn on the currentId chain).
func renderableChatDoc(id string) recall.ChatDoc {
	return recall.ChatDoc{
		ID:        id,
		Title:     "t-" + id,
		CreatedAt: 1781040000,
		History: recall.ChatHistory{
			CurrentID: "m1",
			Messages: map[string]recall.ChatMsg{
				"m1": {ID: "m1", Role: "user", Content: "hello from " + id},
			},
		},
	}
}

// fakeRecallEnv is the off-hardware rig for the recall run bodies: a fully-happy
// recallDeps over an in-memory state store plus an ordered call trace. writeState
// DEEP-COPIES the chats map so the persisted snapshot cannot alias the run body's
// working state — the incremental-persist assertions are only honest if a missed
// persist call is actually observable.
type fakeRecallEnv struct {
	deps recallDeps
	// owui is the ONE fake the protocol runs against. Its trace replaces the
	// per-function call log the twelve stubbed seams used to keep.
	owui  *fakeOWUI
	state recall.State
}

func copyRecallState(s recall.State) recall.State {
	cp := s
	if s.Chats != nil {
		cp.Chats = make(map[string]recall.ChatState, len(s.Chats))
		for k, v := range s.Chats {
			cp.Chats[k] = v
		}
	}
	return cp
}

func newFakeRecallEnv() *fakeRecallEnv {
	env := &fakeRecallEnv{owui: newFakeOWUI()}
	fixedNow := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	env.deps = recallDeps{
		loadedMemoryEnabled: func() bool { return true },
		loadedConfig: func() (config.VillaConfig, error) {
			c := config.DefaultVillaConfig()
			c.MemoryEnabled = true
			return c, nil
		},
		client: func(string) *openwebui.Client { return env.owui.client() },
		signIn: func(ctx context.Context, c *openwebui.Client) (string, error) {
			return c.SignIn(ctx, owuiServiceAccountEmail, owuiServiceAccountPassword, owuiServiceAccountName)
		},
		readState: func() (recall.State, error) { return copyRecallState(env.state), nil },
		writeState: func(s recall.State) error {
			env.owui.log("persist")
			env.state = copyRecallState(s)
			return nil
		},
		now: func() time.Time { return fixedNow },
	}
	return env
}

// callIndex returns the index of the FIRST occurrence of name in calls, -1 if absent.
func callIndex(calls []string, name string) int {
	for i, c := range calls {
		if c == name {
			return i
		}
	}
	return -1
}

// lastCallIndex returns the index of the LAST occurrence of name, -1 if absent.
func lastCallIndex(calls []string, name string) int {
	last := -1
	for i, c := range calls {
		if c == name {
			last = i
		}
	}
	return last
}

// hasCallPrefix reports whether any recorded call starts with prefix.
func hasCallPrefix(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// TestRecallGate locks the delta from `verify memory`: with the memory stack
// OFF (persisted memory_enabled=false) BOTH recall verbs return exitBlocked with a
// remediation on stderr, and NO drive function runs — an explicit index/status
// request never honestly no-ops (unlike verify memory's exit-0 "nothing to
// verify"). An enabled-but-INVALID memory config is equally refused via
// memory.Decide (fail-closed gate).
func TestRecallGate(t *testing.T) {
	t.Run("memory off blocks recall index without running any drive", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.loadedMemoryEnabled = func() bool { return false }
		env.deps.loadedConfig = func() (config.VillaConfig, error) { return config.DefaultVillaConfig(), nil }
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
			t.Errorf("memory-off index exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if errOut.Len() == 0 || !strings.Contains(errOut.String(), "memory") {
			t.Errorf("refusal must carry a remediation naming the memory stack; stderr = %q", errOut.String())
		}
		if len(env.owui.calls) != 0 {
			t.Errorf("no drive function may run when memory is off; calls = %v", env.owui.calls)
		}
	})

	t.Run("memory off blocks recall status without running any drive", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.loadedMemoryEnabled = func() bool { return false }
		env.deps.loadedConfig = func() (config.VillaConfig, error) { return config.DefaultVillaConfig(), nil }
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallStatus(cmd, nil, env.deps); code != exitBlocked {
			t.Errorf("memory-off status exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if errOut.Len() == 0 {
			t.Errorf("refusal must print a remediation to stderr")
		}
		if len(env.owui.calls) != 0 {
			t.Errorf("no drive function may run when memory is off; calls = %v", env.owui.calls)
		}
	})

	t.Run("enabled but invalid memory config blocks via memory.Decide", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.loadedConfig = func() (config.VillaConfig, error) {
			c := config.DefaultVillaConfig()
			c.MemoryEnabled = true
			c.EmbeddingDim = -1 // survives normalize; Decide refuses it
			return c, nil
		}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
			t.Errorf("invalid-config index exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if !strings.Contains(errOut.String(), "embedding_dim") {
			t.Errorf("refusal must surface the Decide reason; stderr = %q", errOut.String())
		}
		if len(env.owui.calls) != 0 {
			t.Errorf("no drive function may run on an invalid config; calls = %v", env.owui.calls)
		}
	})
}

// TestRecallIndexOrdering locks the run pipeline's ordering and honesty contract
// (Pitfall 7/8): reachability failure short-circuits before any token or
// KB work; a per-chat failure mid-run leaves the ALREADY-COMPLETED chats persisted
// (incremental persist) with last_index_completed_at NOT stamped and attach never
// reached; a clean full pass stamps completed, excludes the service account
// and runs attach strictly AFTER the per-chat loop; an unrenderable chat
// is a RECORDED skip, never a silent drop or a run failure.
func TestRecallIndexOrdering(t *testing.T) {
	t.Run("reachability failure short-circuits before token and KB work", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.owui.unhealthy = true
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
			t.Errorf("unreachable-OWUI exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		for _, banned := range []string{"mint", "ensureKB", "listUsers"} {
			if callIndex(env.owui.calls, banned) != -1 {
				t.Errorf("%s ran after a failed reachability gate; calls = %v", banned, env.owui.calls)
			}
		}
		if hasCallPrefix(env.owui.calls, "upload:") {
			t.Errorf("an upload ran after a failed reachability gate; calls = %v", env.owui.calls)
		}
	})

	t.Run("per-chat failure keeps completed chats persisted and never stamps completed", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 100}, recall.ChatRef{ID: "c2", UserID: "u1", UpdatedAt: 100})
		env.owui.failUploadFor = "c2"
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
			t.Errorf("mid-run failure exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if _, ok := env.state.Chats["c1"]; !ok {
			t.Errorf("chat c1 completed BEFORE the failure and must be persisted (incremental persist); state = %+v", env.state)
		}
		if _, ok := env.state.Chats["c2"]; ok {
			t.Errorf("the failed chat c2 must NOT be recorded as indexed")
		}
		if env.state.LastIndexCompletedAt != "" {
			t.Errorf("last_index_completed_at stamped on a FAILED run — partial-run dishonesty (Pitfall 8)")
		}
		if !strings.Contains(errOut.String(), "c2") {
			t.Errorf("the failure must name the failed chat; stderr = %q", errOut.String())
		}
		if hasCallPrefix(env.owui.calls, "attach") {
			t.Errorf("attach ran despite a failed per-chat loop; calls = %v", env.owui.calls)
		}
	})

	t.Run("clean pass stamps completed, excludes the service account, attaches after the loop", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.owui.setChats("u1",
			recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 100},
			recall.ChatRef{ID: "c2", UserID: "u1", UpdatedAt: 100},
		)

		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
			t.Fatalf("clean-pass exit = %d, want exitPass (%d); stderr = %q", code, exitPass, errOut.String())
		}
		if env.state.LastIndexCompletedAt == "" || env.state.LastIndexCompletedAt < env.state.LastIndexStartedAt {
			t.Errorf("a clean full pass must stamp last_index_completed_at >= started; state = %+v", env.state)
		}
		if callIndex(env.owui.calls, "listChats:u-svc") != -1 {
			t.Errorf("the villa-verify@localhost service account must be excluded from listing; calls = %v", env.owui.calls)
		}
		attachAt := callIndex(env.owui.calls, "attachCreate")
		lastUpload := lastCallIndex(env.owui.calls, "upload:"+recall.TranscriptFilename("c2"))
		if attachAt == -1 || lastUpload == -1 || attachAt < lastUpload {
			t.Errorf("attach must run AFTER the per-chat loop; calls = %v", env.owui.calls)
		}
		if !strings.Contains(out.String(), "added") {
			t.Errorf("a clean pass must print a run summary; stdout = %q", out.String())
		}
	})

	t.Run("unrenderable chat is a recorded skip, not a failure or silent drop", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 100}, recall.ChatRef{ID: "c2", UserID: "u1", UpdatedAt: 100})
		env.owui.docs["c1"] = recall.ChatDoc{ID: "c1"} // no history → RenderTranscript ok=false
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
			t.Fatalf("a skip must not fail the run; exit = %d, stderr = %q", code, errOut.String())
		}
		if _, ok := env.state.Chats["c1"]; ok {
			t.Errorf("a skipped chat must not be recorded as indexed")
		}
		if _, ok := env.state.Chats["c2"]; !ok {
			t.Errorf("the renderable chat must still be indexed after a sibling skip")
		}
		if !strings.Contains(out.String(), "skipped") {
			t.Errorf("the run summary must RECORD the skip (never silent); stdout = %q", out.String())
		}
	})
}

// TestRecallCleanReplace locks the clean-replace discipline: a CHANGED chat is
// re-indexed by remove-old-transcript-file-THEN-re-upload (delete-then-re-add — the
// remove must precede the upload so stale vectors never coexist with fresh ones),
// and a DELETED chat drives the file remove and drops its state entry.
func TestRecallCleanReplace(t *testing.T) {
	t.Run("a changed chat removes the old file BEFORE re-uploading", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = recall.State{
			SchemaVersion: recall.SchemaVersion(),
			KnowledgeID:   "kb1",
			Chats: map[string]recall.ChatState{
				"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "old-f1", IndexedAt: "2026-06-09T00:00:00Z"},
			},
		}
		env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 200})
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
			t.Fatalf("update run exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		removeAt := callIndex(env.owui.calls, "remove:old-f1")
		uploadAt := callIndex(env.owui.calls, "upload:"+recall.TranscriptFilename("c1"))
		if removeAt == -1 || uploadAt == -1 || removeAt > uploadAt {
			t.Errorf("clean-replace must remove the OLD file before re-uploading; calls = %v", env.owui.calls)
		}
		got := env.state.Chats["c1"]
		if got.OWUIUpdatedAt != 200 || got.FileID == "old-f1" || got.FileID == "" {
			t.Errorf("the state entry must record the NEW updated_at and file id; got %+v", got)
		}
	})

	t.Run("a deleted chat removes its file and drops the state entry", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = recall.State{
			SchemaVersion: recall.SchemaVersion(),
			KnowledgeID:   "kb1",
			Chats: map[string]recall.ChatState{
				"c2": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "old-f2", IndexedAt: "2026-06-09T00:00:00Z"},
			},
		}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
			t.Fatalf("delete run exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if callIndex(env.owui.calls, "remove:old-f2") == -1 {
			t.Errorf("a deleted chat must drive knowledge/file/remove for its recorded file id; calls = %v", env.owui.calls)
		}
		if _, ok := env.state.Chats["c2"]; ok {
			t.Errorf("a deleted chat's state entry must be dropped")
		}
	})
}

// TestRecallSingleOperatorGuard locks: recall pools EVERY human user's chats
// into one shared collection, so on a box with more than one human user the index
// run REFUSES (fail-closed) until the operator passes --i-understand-shared-recall.
// A single human user proceeds; the service account never counts toward the human
// total.
func TestRecallSingleOperatorGuard(t *testing.T) {
	twoHumans := func(env *fakeRecallEnv) {
		env.owui.users = []openwebui.User{
			{ID: "u1", Email: "operator@local.test", Role: "admin"},
			{ID: "u2", Email: "guest@local.test", Role: "user"},
			{ID: "u-svc", Email: owuiServiceAccountEmail, Role: "admin"},
		}
	}

	t.Run("more than one human user refuses without the ack flag and pools no chats", func(t *testing.T) {
		env := newFakeRecallEnv()
		twoHumans(env)
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
			t.Fatalf("multi-human index exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if !strings.Contains(errOut.String(), "--i-understand-shared-recall") {
			t.Errorf("the refusal must name the override flag (remediation); stderr = %q", errOut.String())
		}
		if hasCallPrefix(env.owui.calls, "listChats:") {
			t.Errorf("no user's chats may be listed once the guard refuses; calls = %v", env.owui.calls)
		}
		if hasCallPrefix(env.owui.calls, "upload:") || callIndex(env.owui.calls, "attach") != -1 {
			t.Errorf("nothing may be uploaded or attached after the guard refuses; calls = %v", env.owui.calls)
		}
	})

	t.Run("more than one human user proceeds with the ack flag", func(t *testing.T) {
		env := newFakeRecallEnv()
		twoHumans(env)
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, true); code != exitPass {
			t.Fatalf("multi-human index WITH ack exit = %d, want exitPass (%d); stderr = %q", code, exitPass, errOut.String())
		}
		if callIndex(env.owui.calls, "listChats:u1") == -1 || callIndex(env.owui.calls, "listChats:u2") == -1 {
			t.Errorf("with the ack flag both humans' chats must be listed; calls = %v", env.owui.calls)
		}
		if callIndex(env.owui.calls, "listChats:u-svc") != -1 {
			t.Errorf("the service account must still be excluded even with the ack; calls = %v", env.owui.calls)
		}
	})

	t.Run("multi-human refusal with --rebuild is side-effect-free", func(t *testing.T) {
		// Phase-23 review: a refusal must be SIDE-EFFECT-FREE. Pre-fix, the
		// guard ran AFTER the state/KB step, so a refused --rebuild had already
		// reset (wiped) the collection, ensureKnowledge had created the KB, and the
		// state had been persisted with the started stamp + the CONFIGURED embedding
		// identity overwriting the recorded truth the skew guard depends on.
		env := newFakeRecallEnv()
		twoHumans(env)
		env.state = recall.State{
			SchemaVersion:  recall.SchemaVersion(),
			KnowledgeID:    "kb1",
			EmbeddingModel: "nomic-embed-text-v1.5",
			EmbeddingDim:   768,
		}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, true /* --rebuild */, false); code != exitBlocked {
			t.Fatalf("multi-human --rebuild exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if hasCallPrefix(env.owui.calls, "reset:") {
			t.Errorf("a refused --rebuild must NOT have reset the collection; calls = %v", env.owui.calls)
		}
		if callIndex(env.owui.calls, "ensureKB") != -1 {
			t.Errorf("a refusal must NOT create the KB; calls = %v", env.owui.calls)
		}
		if callIndex(env.owui.calls, "persist") != -1 {
			t.Errorf("a refusal must NOT persist state (stamp overwrite); calls = %v", env.owui.calls)
		}
		if st := env.state; st.EmbeddingModel != "nomic-embed-text-v1.5" || st.EmbeddingDim != 768 {
			t.Errorf("the recorded embedding stamp must survive a refusal, got %q/%d", st.EmbeddingModel, st.EmbeddingDim)
		}
	})

	t.Run("single human user needs no ack flag", func(t *testing.T) {
		env := newFakeRecallEnv() // default rig: one human + the service account
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
			t.Fatalf("single-human index exit = %d, want exitPass (%d); stderr = %q", code, exitPass, errOut.String())
		}
	})
}

// TestRecallCleanReplaceFailureClearsState locks: clean-replace removes the
// OLD transcript BEFORE upload, so on ANY failure AFTER the remove the stale state
// entry must be cleared (FileID="" / OWUIUpdatedAt=0, persisted) — otherwise the
// next Plan sees neither an Add nor an Update and the removed transcript is never
// re-uploaded, leaving the chat silently absent from retrieval while status reports
// it indexed. The cleared entry must re-qualify the chat as an Add next run.
func TestRecallCleanReplaceFailureClearsState(t *testing.T) {
	env := newFakeRecallEnv()
	env.state = recall.State{
		SchemaVersion: recall.SchemaVersion(),
		KnowledgeID:   "kb1",
		Chats: map[string]recall.ChatState{
			"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "old-f1", IndexedAt: "2026-06-09T00:00:00Z"},
		},
	}
	env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 200})
	// The remove succeeds; the re-upload then fails — the classic mid-step failure.
	env.owui.errs["upload"] = errors.New("embed backend 500")
	cmd := newRecallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
		t.Fatalf("remove-then-fail exit = %d, want exitBlocked (%d)", code, exitBlocked)
	}
	if callIndex(env.owui.calls, "remove:old-f1") == -1 {
		t.Fatalf("the old transcript must have been removed first; calls = %v", env.owui.calls)
	}
	got, ok := env.state.Chats["c1"]
	if !ok {
		t.Fatalf("the chat entry must survive (so its UserID is retained), not be dropped; state = %+v", env.state)
	}
	if got.FileID != "" || got.OWUIUpdatedAt != 0 {
		t.Errorf("after remove-then-fail the entry must be cleared (FileID=\"\", OWUIUpdatedAt=0) so the next run re-qualifies it; got %+v", got)
	}
	// Prove re-qualification: a fresh Plan against the persisted (cleared) state and
	// the same live ref must see c1 as WORK again (Add or Update) — its content is
	// gone from the KB, so the next run MUST re-upload it. With OWUIUpdatedAt cleared
	// to 0, any positive live updated_at re-qualifies it as an Update.
	p := recall.Plan([]recall.ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 200}}, env.state)
	requalified := false
	for _, a := range p.Adds {
		if a.ID == "c1" {
			requalified = true
		}
	}
	for _, u := range p.Updates {
		if u.ID == "c1" {
			requalified = true
		}
	}
	if !requalified {
		t.Errorf("the cleared chat must re-qualify as work (Add or Update) next run; plan = %+v", p)
	}
	if env.state.LastIndexCompletedAt != "" {
		t.Errorf("a failed run must not stamp complete")
	}
}

// TestRecallIncompletePassNotStamped locks: the completed stamp is gated on a
// reconciliation that every planned Add+Update was uploaded-or-skipped this run
// (done == expected). A clean pass over a renderable Add reconciles and stamps; the
// reconciliation must hold for the run to earn last_index_completed_at.
func TestRecallIncompletePassNotStamped(t *testing.T) {
	env := newFakeRecallEnv()
	env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 100}, recall.ChatRef{ID: "c2", UserID: "u1", UpdatedAt: 100})
	env.owui.docs["c2"] = recall.ChatDoc{ID: "c2"} // unrenderable → skip
	cmd := newRecallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
		t.Fatalf("a fully-reconciled pass (1 upload + 1 skip over 2 Adds) must pass and stamp; exit = %d, stderr = %q", code, errOut.String())
	}
	if env.state.LastIndexCompletedAt == "" {
		t.Errorf("a reconciled clean pass (done == expected) must stamp last_index_completed_at; state = %+v", env.state)
	}
	// The summary must reflect 1 added + 1 skipped (counters from typed outcome).
	if !strings.Contains(out.String(), "1 added") || !strings.Contains(out.String(), "1 skipped") {
		t.Errorf("counters must come from the typed outcome (1 added, 1 skipped); out = %q", out.String())
	}
}

// TestRecallStatus locks the typed-Unknown status contract: an unevaluable
// live list renders the LITERAL "Unknown — could not evaluate" (never a numeric
// stale count — Unknown ≠ 0) at exitWarn; a confidently-missing attachment is
// surfaced with the re-run hint (Pitfall 2: model swap silently detaches recall);
// the happy path prints indexed/last-indexed/stale and exits exitPass only when
// stale is KNOWN-zero AND the attachment is present.
func TestRecallStatus(t *testing.T) {
	completeState := func() recall.State {
		return recall.State{
			SchemaVersion:        recall.SchemaVersion(),
			KnowledgeID:          "kb1",
			KnowledgeName:        recallKnowledgeName,
			LastIndexStartedAt:   "2026-06-10T11:00:00Z",
			LastIndexCompletedAt: "2026-06-10T11:05:00Z",
			Chats: map[string]recall.ChatState{
				"c1": {UserID: "u1", OWUIUpdatedAt: 100, FileID: "f1", IndexedAt: "2026-06-10T11:01:00Z"},
			},
		}
	}
	liveCurrent := func(env *fakeRecallEnv) {
		env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 100})
		// The served model row carries the recall collection: retrieval is wired.
		env.owui.modelRow = map[string]any{
			"id":   "served.gguf",
			"meta": map[string]any{"knowledge": []any{map[string]any{"id": "kb1"}}},
		}
	}

	t.Run("listing failure renders Unknown at exitWarn, never stale 0", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = completeState()
		env.owui.errs["listChats"] = errors.New("OWUI down")

		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallStatus(cmd, nil, env.deps); code != exitWarn {
			t.Errorf("unevaluable status exit = %d, want exitWarn (%d)", code, exitWarn)
		}
		if !strings.Contains(out.String(), "Unknown — could not evaluate") {
			t.Errorf("an unevaluable live list must render the literal Unknown; out = %q", out.String())
		}
		if strings.Contains(out.String(), "(new ") {
			t.Errorf("numeric stale counts must NEVER render when the live list is unevaluable; out = %q", out.String())
		}
	})

	t.Run("missing attachment is surfaced with the re-run hint at exitWarn", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = completeState()
		liveCurrent(env)
		// The served model row exists but carries no attachment: the post-model-swap
		// detach case, which must read as confidently Missing rather than Unknown.
		env.owui.modelRow = map[string]any{"id": "served.gguf", "meta": map[string]any{}}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallStatus(cmd, nil, env.deps); code != exitWarn {
			t.Errorf("detached-retrieval status exit = %d, want exitWarn (%d)", code, exitWarn)
		}
		if !strings.Contains(out.String(), "MISSING") || !strings.Contains(out.String(), "villa recall index") {
			t.Errorf("a missing attachment must be surfaced with the re-run hint (Pitfall 2); out = %q", out.String())
		}
	})

	t.Run("a real state-read I/O error blocks, never a soft warn", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.readState = func() (recall.State, error) {
			return recall.State{}, errors.New("permission denied")
		}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallStatus(cmd, nil, env.deps); code != exitBlocked {
			t.Errorf("a real state-read error must block (exitBlocked %d), matching the index path; got %d", exitBlocked, code)
		}
		if !strings.Contains(errOut.String(), "recall-state.json") {
			t.Errorf("the refusal must name the state file; stderr = %q", errOut.String())
		}
	})

	t.Run("current and attached reports counts at exitPass", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = completeState()
		liveCurrent(env)
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallStatus(cmd, nil, env.deps); code != exitPass {
			t.Errorf("current+attached status exit = %d, want exitPass (%d); out = %q", code, exitPass, out.String())
		}
		if !strings.Contains(out.String(), "1 chat") {
			t.Errorf("status must report the indexed count; out = %q", out.String())
		}
		if !strings.Contains(out.String(), "0 (new 0 / changed 0 / deleted 0)") {
			t.Errorf("status must report the stale breakdown; out = %q", out.String())
		}
	})
}

// TestRecallRebuild locks the --rebuild contract: the run drives the
// id-preserving knowledge reset BEFORE any re-index work, clears the prior chats
// map (so every live chat re-indexes as an Add — no per-chat removes against the
// already-reset KB), and persists a fresh full index.
func TestRecallRebuild(t *testing.T) {
	env := newFakeRecallEnv()
	env.state = recall.State{
		SchemaVersion: recall.SchemaVersion(),
		KnowledgeID:   "kb1",
		Chats: map[string]recall.ChatState{
			"c1": {UserID: "u1", OWUIUpdatedAt: 200, FileID: "old-f1", IndexedAt: "2026-06-09T00:00:00Z"},
		},
	}
	env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 200})
	cmd := newRecallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if code := runRecallIndex(cmd, nil, env.deps, true, false); code != exitPass {
		t.Fatalf("rebuild exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}
	resetAt := callIndex(env.owui.calls, "reset:kb1")
	uploadAt := callIndex(env.owui.calls, "upload:"+recall.TranscriptFilename("c1"))
	if resetAt == -1 || uploadAt == -1 || resetAt > uploadAt {
		t.Errorf("--rebuild must reset the KB (id-preserving) BEFORE re-indexing; calls = %v", env.owui.calls)
	}
	if uploadAt == -1 {
		t.Errorf("an unchanged chat must still re-index after --rebuild (cleared state); calls = %v", env.owui.calls)
	}
	if callIndex(env.owui.calls, "remove:old-f1") != -1 {
		t.Errorf("no per-chat remove may run against an already-reset KB; calls = %v", env.owui.calls)
	}
	got := env.state.Chats["c1"]
	if got.FileID == "" || got.FileID == "old-f1" {
		t.Errorf("the rebuilt index must record the fresh file id; got %+v", got)
	}
	if env.state.LastIndexCompletedAt == "" {
		t.Errorf("a clean rebuild must stamp last_index_completed_at")
	}
}

// skewedRecallCfg returns a memory-on config whose embedding identity confidently
// diverges from the nomic/768 stamp the skew tests record — the mismatch
// fixture (model AND dim differ; either alone would also be a mismatch).
func skewedRecallCfg() (config.VillaConfig, error) {
	c := config.DefaultVillaConfig()
	c.MemoryEnabled = true
	c.EmbeddingModel = "other-embed-model"
	c.EmbeddingDim = 512
	return c, nil
}

// TestRecallIndexSkewGuard locks the fail-closed refusal at the ONE verb that
// mutates the index (CTRL-05): a confident embedding model/dim
// mismatch between the recall-state stamp and config REFUSES (exitBlocked,
// refuse-with-remediation) BEFORE any state mutation — the stamp is the recorded
// truth and must survive the refusal (Pitfall 6). --rebuild is the sanctioned
// bypass (OQ4: the rebuild path id-preservingly resets the KB and clean-replaces
// collection content; the fresh stamp then records the new identity). An empty
// stamp is typed-Unknown — no recorded truth, no alarm. The comparison is the
// single Plan 23-01 helper (recall.EmbeddingSkew), never re-rolled.
func TestRecallIndexSkewGuard(t *testing.T) {
	stamped := recall.State{
		KnowledgeID:    "kb1",
		KnowledgeName:  "villa-recall",
		EmbeddingModel: "nomic-embed-text-v1.5",
		EmbeddingDim:   768,
	}

	t.Run("confident mismatch refuses exitBlocked with remediation, stamp preserved", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = copyRecallState(stamped)
		env.deps.loadedConfig = skewedRecallCfg

		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
			t.Fatalf("skew run exit = %d, want exitBlocked; stderr = %q", code, errOut.String())
		}
		msg := errOut.String()
		for _, want := range []string{
			"nomic-embed-text-v1.5", "768", // the stamped identity
			"other-embed-model", "512", // the configured identity
			"corrupts retrieval", // the consequence
			"--rebuild",          // the sanctioned fix
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("refusal must name %q; stderr = %q", want, msg)
			}
		}
		// Pitfall 6: the refusal must run BEFORE any state mutation — zero persists,
		// no KB ensure/reset, and the recorded stamp survives verbatim.
		if callIndex(env.owui.calls, "persist") != -1 {
			t.Errorf("a skew refusal must never persist state; calls = %v", env.owui.calls)
		}
		if callIndex(env.owui.calls, "ensureKB") != -1 || hasCallPrefix(env.owui.calls, "reset:") {
			t.Errorf("a skew refusal must fire no KB mutation; calls = %v", env.owui.calls)
		}
		if st := env.state; st.EmbeddingModel != "nomic-embed-text-v1.5" || st.EmbeddingDim != 768 {
			t.Errorf("the recorded stamp must survive the refusal, got %q/%d", st.EmbeddingModel, st.EmbeddingDim)
		}
	})

	t.Run("consecutive mismatched runs BOTH refuse (Pitfall 6 regression)", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = copyRecallState(stamped)
		env.deps.loadedConfig = skewedRecallCfg

		for i := 1; i <= 2; i++ {
			cmd := newRecallCmd()
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
				t.Fatalf("run %d exit = %d, want exitBlocked — a prior refusal must NOT have overwritten the stamp (Pitfall 6); stderr = %q", i, code, errOut.String())
			}
		}
	})

	t.Run("--rebuild bypasses the refusal and the fresh stamp records the new identity", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = copyRecallState(stamped)
		env.deps.loadedConfig = skewedRecallCfg

		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, true, false); code != exitPass {
			t.Fatalf("--rebuild skew run exit = %d, want exitPass (the sanctioned re-index, OQ4); stderr = %q", code, errOut.String())
		}
		if callIndex(env.owui.calls, "reset:kb1") == -1 {
			t.Errorf("--rebuild must id-preservingly reset the KB; calls = %v", env.owui.calls)
		}
		if st := env.state; st.EmbeddingModel != "other-embed-model" || st.EmbeddingDim != 512 {
			t.Errorf("the fresh stamp must record the NEW identity, got %q/%d", st.EmbeddingModel, st.EmbeddingDim)
		}
		if env.state.LastIndexCompletedAt == "" {
			t.Errorf("a clean rebuild must stamp last_index_completed_at")
		}
	})

	t.Run("empty stamp is typed-Unknown - run proceeds with no alarm", func(t *testing.T) {
		env := newFakeRecallEnv()
		// Zero state: no EmbeddingModel recorded (pre-Phase-21 store / fresh install).
		env.deps.loadedConfig = skewedRecallCfg

		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
			t.Fatalf("empty-stamp run exit = %d, want exitPass (no recorded truth, no alarm); stderr = %q", code, errOut.String())
		}
		if strings.Contains(errOut.String(), "REFUSING") {
			t.Errorf("an empty stamp must never refuse; stderr = %q", errOut.String())
		}
		if st := env.state; st.EmbeddingModel != "other-embed-model" || st.EmbeddingDim != 512 {
			t.Errorf("the first stamp must record the configured identity, got %q/%d", st.EmbeddingModel, st.EmbeddingDim)
		}
	})
}
