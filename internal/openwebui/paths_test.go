package openwebui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestNoEndpointLiteralsOutsideThisPackage enforces the module's central claim: the
// Open WebUI protocol is spoken here and nowhere else.
//
// Endpoint literals previously appeared across the recall drives, the memory verify
// path and the status probe with no shared client, so two callers could drift onto
// different paths for the same operation, and a server-side path change had to be
// found by grep. This gate turns that into a build failure.
//
// Scope: the command tier's non-test sources, which is where the drift actually
// lived and where a new call site would be written. It is deliberately not the whole
// tree, because internal/dashboard serves its OWN /api/models and /api/healthz —
// same spelling, different service — and flagging those would make the gate noise a
// reviewer learns to ignore. The one fake in the command tier legitimately names
// paths, since answering by path is what makes it a transport fake rather than a
// stub, so _test.go files are excluded.
func TestNoEndpointLiteralsOutsideThisPackage(t *testing.T) {
	// The owned prefixes: the versioned API subtree, the model list and the
	// completions route. Each is anchored to a leading quote so a bare mention in
	// prose does not match — the gate is about emitted strings, not documentation.
	owned := regexp.MustCompile(`"/api/v1/|"/api/models"|"/api/chat/completions"`)

	cmdTier := filepath.Join("..", "..", "cmd", "villa")
	entries, err := os.ReadDir(cmdTier)
	if err != nil {
		t.Fatalf("read cmd/villa: %v", err)
	}

	var leaks []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(cmdTier, name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		if owned.MatchString(string(data)) {
			leaks = append(leaks, name)
		}
	}

	if len(leaks) > 0 {
		t.Errorf("Open WebUI endpoint literals leaked into the command tier: %v\n"+
			"Add the path to paths.go and call it through the protocol client instead.", leaks)
	}
}

// fakeJSON serves canned bodies keyed by path, and errors on an unrouted path so a
// test cannot silently pass against a request it never modelled.
func fakeJSON(pages map[string]string) Transport {
	return func(_ context.Context, req Request) ([]byte, error) {
		body, ok := pages[req.Path]
		if !ok {
			return nil, fmt.Errorf("unrouted path %q", req.Path)
		}
		return []byte(body), nil
	}
}

// TestFakeTransportDrivesTheWholeProtocol is the seam's justification: one fake
// substitutes for what used to be a dozen separately-stubbed functions, and because
// the seam sits at the transport, the protocol's real pagination and parsing run.
func TestFakeTransportDrivesTheWholeProtocol(t *testing.T) {
	// Two pages of users, terminating on the empty page exactly as the real endpoint
	// does. A stubbed ListUsers would have skipped this loop entirely.
	users, err := New(fakeJSON(map[string]string{
		pathUsersPage(1): `{"users":[{"id":"u1","email":"a@b"},{"id":"u2","email":"c@d"}]}`,
		pathUsersPage(2): `{"users":[]}`,
	})).ListUsers(context.Background(), "tok")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users across the paged listing, want 2", len(users))
	}
}

// TestPaginationTerminatesAgainstAServerIgnoringPage: the loops must terminate even
// when the server ignores the page parameter and serves page 1 forever, or an index
// run would hang. Termination comes from the dedupe guard, not from trusting the
// server's paging.
func TestPaginationTerminatesAgainstAServerIgnoringPage(t *testing.T) {
	sameEveryPage := func(_ context.Context, _ Request) ([]byte, error) {
		return []byte(`{"users":[{"id":"u1","email":"a@b"}]}`), nil
	}

	done := make(chan struct{})
	var users []User
	var err error
	go func() {
		users, err = New(sameEveryPage).ListUsers(context.Background(), "tok")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ListUsers did not terminate against a server that ignores ?page")
	}

	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("got %d users, want 1 deduplicated", len(users))
	}
}

// TestKnowledgeListAcceptsBothServedShapes: the pinned digest serves a paginated
// envelope, older versions served a bare array. Parsing only one would make
// find-or-create miss the existing collection and spawn a duplicate on every run.
func TestKnowledgeListAcceptsBothServedShapes(t *testing.T) {
	const name = "Villa Recall"
	for _, tc := range []struct {
		shape string
		page1 string
	}{
		{"paginated envelope", `{"items":[{"id":"kb-existing","name":"Villa Recall"}]}`},
		{"bare array", `[{"id":"kb-existing","name":"Villa Recall"}]`},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			created := false
			transport := func(_ context.Context, req Request) ([]byte, error) {
				switch req.Path {
				case pathKnowledgePage(1):
					return []byte(tc.page1), nil
				case pathKnowledgeCreate:
					created = true
					return []byte(`{"id":"kb-new"}`), nil
				}
				return []byte(`{"items":[]}`), nil
			}

			id, err := New(transport).EnsureKnowledge(context.Background(), "tok", name, "d")
			if err != nil {
				t.Fatalf("EnsureKnowledge: %v", err)
			}
			if created {
				t.Error("find-or-create created a duplicate collection despite an existing match")
			}
			if id != "kb-existing" {
				t.Errorf("id = %q, want the existing collection", id)
			}
		})
	}
}

// TestOrphanedUploadIsCleanedUp: a file that was uploaded and embedded but never
// joined the collection is unreachable by the clean-replace path, so it and its
// vectors would accumulate on every retry. The add failure must trigger a delete,
// and must still surface as the real error.
func TestOrphanedUploadIsCleanedUp(t *testing.T) {
	deleted := false
	transport := func(_ context.Context, req Request) ([]byte, error) {
		switch {
		case req.Path == pathFiles:
			return []byte(`{"id":"f1"}`), nil
		case strings.HasSuffix(req.Path, "/process/status"):
			return []byte(`{"status":"completed"}`), nil
		case strings.Contains(req.Path, "/file/add"):
			return nil, fmt.Errorf("collection is full")
		case req.Path == pathFileDelete("f1") && req.Method == "DELETE":
			deleted = true
			return []byte(`{}`), nil
		}
		return nil, fmt.Errorf("unrouted %q", req.Path)
	}

	_, err := New(transport).UploadToKnowledge(context.Background(), "tok", "kb1", "t.txt", "body", time.Minute)
	if err == nil {
		t.Fatal("a failed add must surface as an error")
	}
	if !strings.Contains(err.Error(), "collection is full") {
		t.Errorf("the cleanup must not mask the real failure, got %q", err)
	}
	if !deleted {
		t.Error("an orphaned file must be deleted, or it accumulates with its vectors on every retry")
	}
}

// TestProcessingTimeoutIsAnError: a document that never finishes processing was NOT
// indexed. Reporting otherwise would be a false green, so the timeout is an error.
func TestProcessingTimeoutIsAnError(t *testing.T) {
	pending := func(_ context.Context, req Request) ([]byte, error) {
		if req.Path == pathFiles {
			return []byte(`{"id":"f1"}`), nil
		}
		return []byte(`{"status":"pending"}`), nil
	}

	// A fake clock jumps past the deadline rather than spending real seconds on it.
	now := time.Now()
	c := New(pending).WithClock(
		func() time.Time { now = now.Add(30 * time.Second); return now },
		func(time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		},
	)

	_, err := c.UploadFile(context.Background(), "tok", "t.txt", "body", time.Minute)
	if err == nil {
		t.Fatal("a processing timeout must be an error — the document was not indexed")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("the error must name the timeout, got %q", err)
	}
}

// TestFailedProcessingStatusIsAnError: a server that reports failure is a confident
// negative and must not be waited out to a timeout.
func TestFailedProcessingStatusIsAnError(t *testing.T) {
	failed := func(_ context.Context, req Request) ([]byte, error) {
		if req.Path == pathFiles {
			return []byte(`{"id":"f1"}`), nil
		}
		return []byte(`{"status":"failed"}`), nil
	}

	_, err := New(failed).UploadFile(context.Background(), "tok", "t.txt", "body", time.Minute)
	if err == nil || !strings.Contains(err.Error(), `status "failed"`) {
		t.Errorf("a failed processing status must be reported as such, got %v", err)
	}
}

// TestEmptyIDInA200IsAnError: an id-less success body is a protocol violation, and
// treating it as a silent skip would leave the caller believing work happened.
func TestEmptyIDInA200IsAnError(t *testing.T) {
	empty := func(_ context.Context, _ Request) ([]byte, error) { return []byte(`{"id":""}`), nil }

	if _, err := New(empty).EnsureKnowledge(context.Background(), "tok", "n", "d"); err == nil {
		t.Error("an empty id in a 200 body must be an error, never a silent skip")
	}
}
