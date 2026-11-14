package main

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// TestRecallIndexWritesTheScope: every state save of `villa recall index` is followed by a
// rewrite of the scope file the recall tool server reads, so a chat indexed in this run is
// in its owner's scope when the run ends (ADR-0042). The scope derives from the state just
// saved and nothing else.
func TestRecallIndexWritesTheScope(t *testing.T) {
	env := newFakeRecallEnv()
	env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 100}, recall.ChatRef{ID: "c2", UserID: "u1", UpdatedAt: 100})
	var written []recall.State
	env.deps.writeScope = func(s recall.State) error {
		written = append(written, copyRecallState(s))
		return nil
	}

	cmd := newRecallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
		t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}
	if len(written) < 3 {
		t.Fatalf("scope written %d times, want after the knowledge save and after each of two chats", len(written))
	}
	last := recall.ScopeOf(written[len(written)-1])
	if last.KnowledgeID == "" || len(last.Files) != 2 {
		t.Errorf("final scope = %+v, want the knowledge id and both chats' files", last)
	}
	if !reflect.DeepEqual(last, recall.ScopeOf(env.state)) {
		t.Errorf("final scope %+v differs from the scope of the saved state %+v", last, recall.ScopeOf(env.state))
	}
	for i := 1; i < len(written); i++ {
		if len(written[i].Chats) < len(written[i-1].Chats) {
			t.Errorf("scope save %d lost chats: %d then %d", i, len(written[i-1].Chats), len(written[i].Chats))
		}
	}
}

// TestRecallIndexScopeFailureWarnsAndKeepsTheRun: the state is the record, and the scope
// is derived from it. A scope that cannot be written is a stale tool, not a failed index,
// so the run warns, names the remedy and still succeeds.
func TestRecallIndexScopeFailureWarnsAndKeepsTheRun(t *testing.T) {
	env := newFakeRecallEnv()
	env.owui.setChats("u1", recall.ChatRef{ID: "c1", UserID: "u1", UpdatedAt: 100})
	env.deps.writeScope = func(recall.State) error { return errors.New("disk full") }

	cmd := newRecallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if code := runRecallIndex(cmd, nil, env.deps, false, false); code != exitPass {
		t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}
	if _, ok := env.state.Chats["c1"]; !ok {
		t.Error("the chat was not recorded as indexed")
	}
	if got := errOut.String(); !bytes.Contains([]byte(got), []byte("recall scope")) || !bytes.Contains([]byte(got), []byte("villa up")) {
		t.Errorf("stderr = %q, want a warning naming the recall scope and `villa up`", got)
	}
}
