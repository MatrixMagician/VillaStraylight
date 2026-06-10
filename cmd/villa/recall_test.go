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
// persist call is actually observable (D-06).
type fakeRecallEnv struct {
	deps  recallDeps
	calls []string
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
	env := &fakeRecallEnv{}
	fixedNow := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	env.deps = recallDeps{
		loadedMemoryEnabled: func() bool { return true },
		loadedConfig: func() config.VillaConfig {
			c := config.DefaultVillaConfig()
			c.MemoryEnabled = true
			return c
		},
		mintToken: func(context.Context, string) (string, error) {
			env.calls = append(env.calls, "mint")
			return "tok", nil
		},
		owuiHealthy: func(context.Context, string) error {
			env.calls = append(env.calls, "health")
			return nil
		},
		listUsers: func(context.Context, string, string) ([]owuiUser, error) {
			env.calls = append(env.calls, "listUsers")
			return []owuiUser{
				{ID: "u1", Email: "operator@local.test", Role: "admin"},
				{ID: "u-svc", Email: recallServiceAccountEmail, Role: "admin"},
			}, nil
		},
		listUserChats: func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
			env.calls = append(env.calls, "listChats:"+userID)
			return nil, nil
		},
		getChat: func(_ context.Context, _, _, chatID string) (recall.ChatDoc, error) {
			env.calls = append(env.calls, "getChat:"+chatID)
			return renderableChatDoc(chatID), nil
		},
		ensureKnowledge: func(_ context.Context, _, _, _, _ string) (string, error) {
			env.calls = append(env.calls, "ensureKB")
			return "kb1", nil
		},
		uploadTranscript: func(_ context.Context, _, _, _, filename, _ string) (string, error) {
			env.calls = append(env.calls, "upload:"+filename)
			return "file-" + filename, nil
		},
		removeKnowledgeFile: func(_ context.Context, _, _, _, fileID string) error {
			env.calls = append(env.calls, "remove:"+fileID)
			return nil
		},
		resetKnowledge: func(_ context.Context, _, _, kbID string) error {
			env.calls = append(env.calls, "reset:"+kbID)
			return nil
		},
		attachKnowledge: func(_ context.Context, _, _, _, _, _ string) (recall.AttachmentState, error) {
			env.calls = append(env.calls, "attach")
			return recall.AttachmentAttached, nil
		},
		attachmentState: func(_ context.Context, _, _, _ string) recall.AttachmentState {
			env.calls = append(env.calls, "attachState")
			return recall.AttachmentAttached
		},
		discoverModel: func(context.Context, string, string) (string, error) {
			env.calls = append(env.calls, "discover")
			return "served.gguf", nil
		},
		readState: func() (recall.State, error) { return copyRecallState(env.state), nil },
		writeState: func(s recall.State) error {
			env.calls = append(env.calls, "persist")
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

// TestRecallGate locks the D-07 delta from `verify memory`: with the memory stack
// OFF (persisted memory_enabled=false) BOTH recall verbs return exitBlocked with a
// remediation on stderr, and NO drive function runs — an explicit index/status
// request never honestly no-ops (unlike verify memory's exit-0 "nothing to
// verify"). An enabled-but-INVALID memory config is equally refused via
// memory.Decide (fail-closed gate).
func TestRecallGate(t *testing.T) {
	t.Run("memory off blocks recall index without running any drive", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.loadedMemoryEnabled = func() bool { return false }
		env.deps.loadedConfig = func() config.VillaConfig { return config.DefaultVillaConfig() }
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
		if len(env.calls) != 0 {
			t.Errorf("no drive function may run when memory is off; calls = %v", env.calls)
		}
	})

	t.Run("memory off blocks recall status without running any drive", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.loadedMemoryEnabled = func() bool { return false }
		env.deps.loadedConfig = func() config.VillaConfig { return config.DefaultVillaConfig() }
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
		if len(env.calls) != 0 {
			t.Errorf("no drive function may run when memory is off; calls = %v", env.calls)
		}
	})

	t.Run("enabled but invalid memory config blocks via memory.Decide", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.loadedConfig = func() config.VillaConfig {
			c := config.DefaultVillaConfig()
			c.MemoryEnabled = true
			c.EmbeddingDim = -1 // survives normalize; Decide refuses it
			return c
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
		if len(env.calls) != 0 {
			t.Errorf("no drive function may run on an invalid config; calls = %v", env.calls)
		}
	})
}

// TestRecallIndexOrdering locks the run pipeline's ordering and honesty contract
// (D-01/D-06/Pitfall 7/8): reachability failure short-circuits before any token or
// KB work; a per-chat failure mid-run leaves the ALREADY-COMPLETED chats persisted
// (incremental persist) with last_index_completed_at NOT stamped and attach never
// reached; a clean full pass stamps completed, excludes the service account
// (D-09), and runs attach strictly AFTER the per-chat loop; an unrenderable chat
// is a RECORDED skip, never a silent drop or a run failure (D-04).
func TestRecallIndexOrdering(t *testing.T) {
	t.Run("reachability failure short-circuits before token and KB work", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.owuiHealthy = func(context.Context, string) error {
			env.calls = append(env.calls, "health")
			return errors.New("connection refused")
		}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
			t.Errorf("unreachable-OWUI exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		for _, banned := range []string{"mint", "ensureKB", "listUsers"} {
			if callIndex(env.calls, banned) != -1 {
				t.Errorf("%s ran after a failed reachability gate; calls = %v", banned, env.calls)
			}
		}
		if hasCallPrefix(env.calls, "upload:") {
			t.Errorf("an upload ran after a failed reachability gate; calls = %v", env.calls)
		}
	})

	t.Run("per-chat failure keeps completed chats persisted and never stamps completed", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
			env.calls = append(env.calls, "listChats:"+userID)
			if userID != "u1" {
				return nil, nil
			}
			return []recall.ChatRef{
				{ID: "c1", UserID: "u1", UpdatedAt: 100},
				{ID: "c2", UserID: "u1", UpdatedAt: 100},
			}, nil
		}
		env.deps.uploadTranscript = func(_ context.Context, _, _, _, filename, _ string) (string, error) {
			env.calls = append(env.calls, "upload:"+filename)
			if strings.Contains(filename, "c2") {
				return "", errors.New("embed backend 500")
			}
			return "file-" + filename, nil
		}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
			t.Errorf("mid-run failure exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if _, ok := env.state.Chats["c1"]; !ok {
			t.Errorf("chat c1 completed BEFORE the failure and must be persisted (incremental persist, D-06); state = %+v", env.state)
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
		if callIndex(env.calls, "attach") != -1 {
			t.Errorf("attach ran despite a failed per-chat loop")
		}
	})

	t.Run("clean pass stamps completed, excludes the service account, attaches after the loop", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
			env.calls = append(env.calls, "listChats:"+userID)
			return []recall.ChatRef{
				{ID: "c1", UserID: userID, UpdatedAt: 100},
				{ID: "c2", UserID: userID, UpdatedAt: 100},
			}, nil
		}
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
		if callIndex(env.calls, "listChats:u-svc") != -1 {
			t.Errorf("the villa-verify@localhost service account must be excluded from listing (D-09); calls = %v", env.calls)
		}
		attachAt := callIndex(env.calls, "attach")
		lastUpload := lastCallIndex(env.calls, "upload:"+recall.TranscriptFilename("c2"))
		if attachAt == -1 || lastUpload == -1 || attachAt < lastUpload {
			t.Errorf("attach must run AFTER the per-chat loop; calls = %v", env.calls)
		}
		if !strings.Contains(out.String(), "added") {
			t.Errorf("a clean pass must print a run summary; stdout = %q", out.String())
		}
	})

	t.Run("unrenderable chat is a recorded skip, not a failure or silent drop", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
			env.calls = append(env.calls, "listChats:"+userID)
			if userID != "u1" {
				return nil, nil
			}
			return []recall.ChatRef{
				{ID: "c1", UserID: "u1", UpdatedAt: 100},
				{ID: "c2", UserID: "u1", UpdatedAt: 100},
			}, nil
		}
		env.deps.getChat = func(_ context.Context, _, _, chatID string) (recall.ChatDoc, error) {
			env.calls = append(env.calls, "getChat:"+chatID)
			if chatID == "c1" {
				return recall.ChatDoc{ID: chatID}, nil // no history → RenderTranscript ok=false
			}
			return renderableChatDoc(chatID), nil
		}
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
			t.Errorf("the run summary must RECORD the skip (never silent, D-04); stdout = %q", out.String())
		}
	})
}

// TestRecallCleanReplace locks the D-04 clean-replace discipline: a CHANGED chat is
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
		env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
			env.calls = append(env.calls, "listChats:"+userID)
			if userID != "u1" {
				return nil, nil
			}
			return []recall.ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 200}}, nil // newer → Update
		}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
			t.Fatalf("update run exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		removeAt := callIndex(env.calls, "remove:old-f1")
		uploadAt := callIndex(env.calls, "upload:"+recall.TranscriptFilename("c1"))
		if removeAt == -1 || uploadAt == -1 || removeAt > uploadAt {
			t.Errorf("clean-replace must remove the OLD file before re-uploading (D-04); calls = %v", env.calls)
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
		if callIndex(env.calls, "remove:old-f2") == -1 {
			t.Errorf("a deleted chat must drive knowledge/file/remove for its recorded file id; calls = %v", env.calls)
		}
		if _, ok := env.state.Chats["c2"]; ok {
			t.Errorf("a deleted chat's state entry must be dropped")
		}
	})
}

// TestRecallSingleOperatorGuard locks WR-05: recall pools EVERY human user's chats
// into one shared collection, so on a box with more than one human user the index
// run REFUSES (fail-closed) until the operator passes --i-understand-shared-recall.
// A single human user proceeds; the service account never counts toward the human
// total (D-09).
func TestRecallSingleOperatorGuard(t *testing.T) {
	twoHumans := func(env *fakeRecallEnv) {
		env.deps.listUsers = func(context.Context, string, string) ([]owuiUser, error) {
			env.calls = append(env.calls, "listUsers")
			return []owuiUser{
				{ID: "u1", Email: "operator@local.test", Role: "admin"},
				{ID: "u2", Email: "guest@local.test", Role: "user"},
				{ID: "u-svc", Email: recallServiceAccountEmail, Role: "admin"},
			}, nil
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
		if hasCallPrefix(env.calls, "listChats:") {
			t.Errorf("no user's chats may be listed once the guard refuses; calls = %v", env.calls)
		}
		if hasCallPrefix(env.calls, "upload:") || callIndex(env.calls, "attach") != -1 {
			t.Errorf("nothing may be uploaded or attached after the guard refuses; calls = %v", env.calls)
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
		if callIndex(env.calls, "listChats:u1") == -1 || callIndex(env.calls, "listChats:u2") == -1 {
			t.Errorf("with the ack flag both humans' chats must be listed; calls = %v", env.calls)
		}
		if callIndex(env.calls, "listChats:u-svc") != -1 {
			t.Errorf("the service account must still be excluded even with the ack; calls = %v", env.calls)
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

// TestRecallCleanReplaceFailureClearsState locks WR-01: clean-replace removes the
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
	env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
		env.calls = append(env.calls, "listChats:"+userID)
		if userID != "u1" {
			return nil, nil
		}
		return []recall.ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 200}}, nil // newer → Update
	}
	// The remove succeeds; the re-upload then fails — the classic mid-step failure.
	env.deps.uploadTranscript = func(_ context.Context, _, _, _, filename, _ string) (string, error) {
		env.calls = append(env.calls, "upload:"+filename)
		return "", errors.New("embed backend 500")
	}
	cmd := newRecallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitBlocked {
		t.Fatalf("remove-then-fail exit = %d, want exitBlocked (%d)", code, exitBlocked)
	}
	if callIndex(env.calls, "remove:old-f1") == -1 {
		t.Fatalf("the old transcript must have been removed first; calls = %v", env.calls)
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

// TestRecallIncompletePassNotStamped locks CR-01: the completed stamp is gated on a
// reconciliation that every planned Add+Update was uploaded-or-skipped this run
// (done == expected). A clean pass over a renderable Add reconciles and stamps; the
// reconciliation must hold for the run to earn last_index_completed_at.
func TestRecallIncompletePassNotStamped(t *testing.T) {
	env := newFakeRecallEnv()
	env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
		env.calls = append(env.calls, "listChats:"+userID)
		if userID != "u1" {
			return nil, nil
		}
		// One renderable Add + one unrenderable Add (recorded skip): both reconcile.
		return []recall.ChatRef{
			{ID: "c1", UserID: "u1", UpdatedAt: 100},
			{ID: "c2", UserID: "u1", UpdatedAt: 100},
		}, nil
	}
	env.deps.getChat = func(_ context.Context, _, _, chatID string) (recall.ChatDoc, error) {
		env.calls = append(env.calls, "getChat:"+chatID)
		if chatID == "c2" {
			return recall.ChatDoc{ID: chatID}, nil // unrenderable → skip
		}
		return renderableChatDoc(chatID), nil
	}
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
	// The summary must reflect 1 added + 1 skipped (counters from typed outcome, WR-02).
	if !strings.Contains(out.String(), "1 added") || !strings.Contains(out.String(), "1 skipped") {
		t.Errorf("counters must come from the typed outcome (1 added, 1 skipped); out = %q", out.String())
	}
}

// TestRecallAttach locks the D-03 idempotent read-merge-write attach
// (attachKnowledgeRow): an EXISTING Model row is updated with the recall KB merged
// into meta.knowledge while every foreign meta key the operator set is preserved
// (T-21-10 — never clobber) and the KB item is deduplicated by id; an ABSENT row is
// created with the served-model override shape (id == served, base_model_id null).
func TestRecallAttach(t *testing.T) {
	const kbID = "kb1"

	t.Run("merges into an existing row preserving foreign meta keys", func(t *testing.T) {
		// Stateful fakes (WR-04): getRow returns the CURRENT stored row, so the
		// post-update re-GET verification sees the merge that updateRow persisted —
		// exactly the live read-merge-write-then-verify cycle.
		stored := map[string]any{
			"id": "served.gguf",
			"meta": map[string]any{
				"description": "keep me",
				"knowledge": []any{
					map[string]any{"type": "collection", "id": "other-kb", "name": "Other"},
				},
			},
			"params": map[string]any{"temperature": 0.5},
		}
		var updated map[string]any
		createRan := false
		getRow := func() (map[string]any, bool, error) { return stored, true, nil }
		updateRow := func(row map[string]any) error { updated = row; stored = row; return nil }
		createRow := func(map[string]any) error { createRan = true; return nil }

		state, err := attachKnowledgeRow(getRow, updateRow, createRow, "served.gguf", kbID, recallKnowledgeName)
		if err != nil || state != recall.AttachmentAttached {
			t.Fatalf("attach = (%v, %v), want (attached, nil)", state, err)
		}
		if createRan {
			t.Errorf("create must not run when the row exists (Pitfall 3: MODEL_ID_TAKEN)")
		}
		meta, _ := updated["meta"].(map[string]any)
		if meta == nil || meta["description"] != "keep me" {
			t.Errorf("foreign meta keys must be preserved (read-merge-write); meta = %v", meta)
		}
		items, _ := meta["knowledge"].([]any)
		if len(items) != 2 {
			t.Fatalf("knowledge must carry the prior item AND the recall KB; items = %v", items)
		}
		found := false
		for _, it := range items {
			if m, ok := it.(map[string]any); ok && m["id"] == kbID {
				found = true
			}
		}
		if !found {
			t.Errorf("the recall KB item must be merged into meta.knowledge; items = %v", items)
		}
		if p, _ := updated["params"].(map[string]any); p == nil || p["temperature"] != 0.5 {
			t.Errorf("foreign top-level row fields must survive the merge; params = %v", updated["params"])
		}
	})

	t.Run("deduplicates the recall KB by id on re-attach", func(t *testing.T) {
		stored := map[string]any{
			"id": "served.gguf",
			"meta": map[string]any{
				"knowledge": []any{
					map[string]any{"type": "collection", "id": kbID, "name": recallKnowledgeName},
				},
			},
		}
		var updated map[string]any
		getRow := func() (map[string]any, bool, error) { return stored, true, nil }
		updateRow := func(row map[string]any) error { updated = row; stored = row; return nil }
		createRow := func(map[string]any) error { return nil }

		if _, err := attachKnowledgeRow(getRow, updateRow, createRow, "served.gguf", kbID, recallKnowledgeName); err != nil {
			t.Fatalf("re-attach errored: %v", err)
		}
		meta, _ := updated["meta"].(map[string]any)
		items, _ := meta["knowledge"].([]any)
		if len(items) != 1 {
			t.Errorf("re-attach must not duplicate the KB item (idempotent); items = %v", items)
		}
	})

	t.Run("creates the override row when absent", func(t *testing.T) {
		var stored map[string]any
		exists := false
		var created map[string]any
		updateRan := false
		getRow := func() (map[string]any, bool, error) { return stored, exists, nil }
		updateRow := func(map[string]any) error { updateRan = true; return nil }
		createRow := func(row map[string]any) error { created = row; stored = row; exists = true; return nil }

		state, err := attachKnowledgeRow(getRow, updateRow, createRow, "served.gguf", kbID, recallKnowledgeName)
		if err != nil || state != recall.AttachmentAttached {
			t.Fatalf("attach = (%v, %v), want (attached, nil)", state, err)
		}
		if updateRan {
			t.Errorf("update must not run when the row is absent")
		}
		if created["id"] != "served.gguf" || created["base_model_id"] != nil {
			t.Errorf("the created row must override the SERVED model (id == served, base_model_id null); row = %v", created)
		}
		meta, _ := created["meta"].(map[string]any)
		items, _ := meta["knowledge"].([]any)
		if len(items) != 1 {
			t.Errorf("the created row must carry the recall KB in meta.knowledge; items = %v", items)
		}
	})

	t.Run("a silent detach (update returns 200 but the KB does not land) is NOT reported attached", func(t *testing.T) {
		// WR-04 / Pitfall 2: updateRow succeeds (HTTP 200) but OWUI dropped/reshaped
		// meta.knowledge, so the re-GET shows the recall KB absent. attach must
		// return a not-Attached verdict WITH an error so the index run fails
		// honestly instead of stamping a false green.
		// getRow returns a FRESH row each call (as the live server re-read does), so
		// the in-place merge of the first row never pollutes the verify re-GET. The
		// server still reports an empty meta.knowledge → the merge did not land.
		getRow := func() (map[string]any, bool, error) {
			return map[string]any{
				"id":   "served.gguf",
				"meta": map[string]any{"knowledge": []any{}},
			}, true, nil
		}
		// updateRow lies: it returns success but does NOT persist the merge.
		updateRow := func(map[string]any) error { return nil }
		createRow := func(map[string]any) error { return nil }

		state, err := attachKnowledgeRow(getRow, updateRow, createRow, "served.gguf", kbID, recallKnowledgeName)
		if err == nil {
			t.Fatalf("a silent detach must return an error; got state=%v err=nil", state)
		}
		if state == recall.AttachmentAttached {
			t.Errorf("a silent detach must NOT be reported attached; state = %v", state)
		}
	})
}

// TestRecallStatus locks the D-06 typed-Unknown status contract: an unevaluable
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
		env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
			env.calls = append(env.calls, "listChats:"+userID)
			if userID != "u1" {
				return nil, nil
			}
			return []recall.ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 100}}, nil
		}
	}

	t.Run("listing failure renders Unknown at exitWarn, never stale 0", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = completeState()
		env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
			env.calls = append(env.calls, "listChats:"+userID)
			return nil, errors.New("OWUI down")
		}
		cmd := newRecallCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		if code := runRecallStatus(cmd, nil, env.deps); code != exitWarn {
			t.Errorf("unevaluable status exit = %d, want exitWarn (%d)", code, exitWarn)
		}
		if !strings.Contains(out.String(), "Unknown — could not evaluate") {
			t.Errorf("an unevaluable live list must render the literal Unknown (D-06); out = %q", out.String())
		}
		if strings.Contains(out.String(), "(new ") {
			t.Errorf("numeric stale counts must NEVER render when the live list is unevaluable; out = %q", out.String())
		}
	})

	t.Run("missing attachment is surfaced with the re-run hint at exitWarn", func(t *testing.T) {
		env := newFakeRecallEnv()
		env.state = completeState()
		liveCurrent(env)
		env.deps.attachmentState = func(_ context.Context, _, _, _ string) recall.AttachmentState {
			env.calls = append(env.calls, "attachState")
			return recall.AttachmentMissing
		}
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

	t.Run("a real state-read I/O error blocks (WR-06), never a soft warn", func(t *testing.T) {
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

// TestRecallRebuild locks the --rebuild contract (D-04): the run drives the
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
	env.deps.listUserChats = func(_ context.Context, _, _, userID string) ([]recall.ChatRef, error) {
		env.calls = append(env.calls, "listChats:"+userID)
		if userID != "u1" {
			return nil, nil
		}
		// updated_at UNCHANGED vs state — only the cleared map makes this re-index.
		return []recall.ChatRef{{ID: "c1", UserID: "u1", UpdatedAt: 200}}, nil
	}
	cmd := newRecallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if code := runRecallIndex(cmd, nil, env.deps, true, false); code != exitPass {
		t.Fatalf("rebuild exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}
	resetAt := callIndex(env.calls, "reset:kb1")
	uploadAt := callIndex(env.calls, "upload:"+recall.TranscriptFilename("c1"))
	if resetAt == -1 || uploadAt == -1 || resetAt > uploadAt {
		t.Errorf("--rebuild must reset the KB (id-preserving) BEFORE re-indexing; calls = %v", env.calls)
	}
	if uploadAt == -1 {
		t.Errorf("an unchanged chat must still re-index after --rebuild (cleared state); calls = %v", env.calls)
	}
	if callIndex(env.calls, "remove:old-f1") != -1 {
		t.Errorf("no per-chat remove may run against an already-reset KB; calls = %v", env.calls)
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
// diverges from the nomic/768 stamp the skew tests record — the D-10 mismatch
// fixture (model AND dim differ; either alone would also be a mismatch).
func skewedRecallCfg() config.VillaConfig {
	c := config.DefaultVillaConfig()
	c.MemoryEnabled = true
	c.EmbeddingModel = "other-embed-model"
	c.EmbeddingDim = 512
	return c
}

// TestRecallIndexSkewGuard locks the D-10 fail-closed refusal at the ONE verb that
// mutates the index (CTRL-05, T-23-15/T-23-16): a confident embedding model/dim
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
		if callIndex(env.calls, "persist") != -1 {
			t.Errorf("a skew refusal must never persist state; calls = %v", env.calls)
		}
		if callIndex(env.calls, "ensureKB") != -1 || hasCallPrefix(env.calls, "reset:") {
			t.Errorf("a skew refusal must fire no KB mutation; calls = %v", env.calls)
		}
		if env.state.EmbeddingModel != "nomic-embed-text-v1.5" || env.state.EmbeddingDim != 768 {
			t.Errorf("the recorded stamp must survive the refusal, got %q/%d", env.state.EmbeddingModel, env.state.EmbeddingDim)
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
		if callIndex(env.calls, "reset:kb1") == -1 {
			t.Errorf("--rebuild must id-preservingly reset the KB; calls = %v", env.calls)
		}
		if env.state.EmbeddingModel != "other-embed-model" || env.state.EmbeddingDim != 512 {
			t.Errorf("the fresh stamp must record the NEW identity, got %q/%d", env.state.EmbeddingModel, env.state.EmbeddingDim)
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
		if env.state.EmbeddingModel != "other-embed-model" || env.state.EmbeddingDim != 512 {
			t.Errorf("the first stamp must record the configured identity, got %q/%d", env.state.EmbeddingModel, env.state.EmbeddingDim)
		}
	})
}
