package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/openwebui"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// openwebui_fake_test.go is the ONE fake the recall and verify tests drive the Open
// WebUI protocol through.
//
// It replaces a dozen separately-stubbed named functions. Because the seam is the
// transport rather than the operations, this fake answers requests the way the real
// service does — by path — so the protocol's own pagination, parsing and
// read-merge-write choreography run for real in every test that uses it. A stubbed
// operation would have skipped exactly that logic.

// fakeOWUI is an in-memory Open WebUI. Each field is the smallest thing a test needs
// to steer: the users and chats it serves, the documents it holds, and per-path
// error injection.
type fakeOWUI struct {
	// calls is the ordered trace of protocol operations, named after the operation
	// rather than the path so assertions read as choreography.
	calls []string

	users     []openwebui.User
	chats     map[string][]recall.ChatRef // by user id
	docs      map[string]recall.ChatDoc   // by chat id
	knowledge map[string]string           // name -> id
	modelRow  map[string]any
	servedID  string

	// errs injects a failure for the named operation.
	errs map[string]error
	// attachDropsKnowledge makes update/create return 200 while silently failing to
	// persist meta.knowledge — the silent-detach case the attach verify exists for.
	attachDropsKnowledge bool
	// failUploadFor makes the upload of any file whose name contains this substring
	// fail, so a test can break exactly one chat mid-run.
	failUploadFor string
	// signInFails makes token minting fail.
	signInFails bool
	// unhealthy makes the reachability probe fail.
	unhealthy bool
}

func newFakeOWUI() *fakeOWUI {
	return &fakeOWUI{
		users: []openwebui.User{
			{ID: "u1", Email: "operator@local.test", Role: "admin"},
			{ID: "u-svc", Email: owuiServiceAccountEmail, Role: "admin"},
		},
		chats:     map[string][]recall.ChatRef{},
		docs:      map[string]recall.ChatDoc{},
		knowledge: map[string]string{},
		errs:      map[string]error{},
		servedID:  "served.gguf",
	}
}

func (f *fakeOWUI) log(op string) { f.calls = append(f.calls, op) }

// fail returns the injected error for an operation, if any, and logs the attempt so
// a refusal is still visible in the trace.
func (f *fakeOWUI) fail(op string) error { return f.errs[op] }

// setChats seeds one user's chat universe and a renderable document per chat.
func (f *fakeOWUI) setChats(userID string, refs ...recall.ChatRef) {
	f.chats[userID] = refs
	for _, r := range refs {
		if _, ok := f.docs[r.ID]; !ok {
			f.docs[r.ID] = renderableChatDoc(r.ID)
		}
	}
}

// transport answers a protocol request the way the real service would: by path.
func (f *fakeOWUI) transport() openwebui.Transport {
	return func(_ context.Context, req openwebui.Request) ([]byte, error) {
		p := req.Path
		switch {
		case p == "/health":
			f.log("health")
			if f.unhealthy {
				return nil, fmt.Errorf("connection refused")
			}
			return []byte(`{}`), nil

		case strings.HasPrefix(p, "/api/v1/auths/"):
			f.log("mint")
			if f.signInFails {
				return nil, fmt.Errorf("unauthorized")
			}
			return []byte(`{"token":"tok"}`), nil

		case p == "/api/models":
			f.log("discover")
			if err := f.fail("discover"); err != nil {
				return nil, err
			}
			return json.Marshal(map[string]any{"data": []map[string]string{{"id": f.servedID}}})

		case strings.HasPrefix(p, "/api/v1/users/"):
			f.log("listUsers")
			if err := f.fail("listUsers"); err != nil {
				return nil, err
			}
			// Page 1 serves everyone; later pages are empty, which is how the
			// protocol's pagination terminates.
			if !strings.HasSuffix(p, "page=1") {
				return json.Marshal(map[string]any{"users": []openwebui.User{}})
			}
			return json.Marshal(map[string]any{"users": f.users})

		case strings.HasPrefix(p, "/api/v1/chats/list/user/"):
			userID := strings.TrimPrefix(p, "/api/v1/chats/list/user/")
			userID = strings.SplitN(userID, "?", 2)[0]
			f.log("listChats:" + userID)
			if err := f.fail("listChats"); err != nil {
				return nil, err
			}
			if !strings.HasSuffix(p, "page=1") {
				return []byte(`[]`), nil
			}
			items := make([]map[string]any, 0, len(f.chats[userID]))
			for _, r := range f.chats[userID] {
				items = append(items, map[string]any{"id": r.ID, "updated_at": r.UpdatedAt})
			}
			return json.Marshal(items)

		case strings.HasPrefix(p, "/api/v1/chats/"):
			chatID := strings.TrimPrefix(p, "/api/v1/chats/")
			f.log("getChat:" + chatID)
			if err := f.fail("getChat"); err != nil {
				return nil, err
			}
			doc, ok := f.docs[chatID]
			if !ok {
				return nil, fmt.Errorf("no such chat %q", chatID)
			}
			return json.Marshal(map[string]any{
				"id": doc.ID, "title": doc.Title, "created_at": doc.CreatedAt,
				"chat": map[string]any{"history": doc.History},
			})

		case p == "/api/v1/knowledge/create":
			f.log("ensureKB")
			if err := f.fail("ensureKB"); err != nil {
				return nil, err
			}
			var body struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Body, &body)
			id := "kb1"
			f.knowledge[body.Name] = id
			return json.Marshal(map[string]string{"id": id})

		case strings.HasPrefix(p, "/api/v1/knowledge/") && strings.Contains(p, "/reset"):
			kbID := strings.TrimSuffix(strings.TrimPrefix(p, "/api/v1/knowledge/"), "/reset")
			f.log("reset:" + kbID)
			return []byte(`{}`), f.fail("reset")

		case strings.HasPrefix(p, "/api/v1/knowledge/") && strings.Contains(p, "/file/add"):
			f.log("addFile")
			return []byte(`{}`), f.fail("addFile")

		case strings.HasPrefix(p, "/api/v1/knowledge/") && strings.Contains(p, "/file/remove"):
			var body struct {
				FileID string `json:"file_id"`
			}
			_ = json.Unmarshal(req.Body, &body)
			f.log("remove:" + body.FileID)
			return []byte(`{}`), f.fail("remove")

		case strings.HasPrefix(p, "/api/v1/knowledge/"):
			f.log("listKB")
			if err := f.fail("listKB"); err != nil {
				return nil, err
			}
			if !strings.HasSuffix(p, "page=1") {
				return []byte(`{"items":[]}`), nil
			}
			items := make([]map[string]string, 0, len(f.knowledge))
			for name, id := range f.knowledge {
				items = append(items, map[string]string{"id": id, "name": name})
			}
			return json.Marshal(map[string]any{"items": items})

		case strings.HasSuffix(p, "/process/status"):
			return []byte(`{"status":"completed"}`), nil

		case p == "/api/v1/files/":
			name := ""
			if req.Upload != nil {
				name = req.Upload.Filename
			}
			f.log("upload:" + name)
			if err := f.fail("upload"); err != nil {
				return nil, err
			}
			if f.failUploadFor != "" && strings.Contains(name, f.failUploadFor) {
				return nil, fmt.Errorf("embed backend 500")
			}
			return json.Marshal(map[string]string{"id": "file-" + name})

		case strings.HasPrefix(p, "/api/v1/files/"):
			f.log("deleteFile")
			return []byte(`{}`), nil

		case strings.HasPrefix(p, "/api/v1/models/model/update"):
			f.log("attachUpdate")
			if err := f.fail("attach"); err != nil {
				return nil, err
			}
			if !f.attachDropsKnowledge {
				var row map[string]any
				_ = json.Unmarshal(req.Body, &row)
				f.modelRow = row
			}
			return []byte(`{}`), nil

		case p == "/api/v1/models/create":
			f.log("attachCreate")
			if err := f.fail("attach"); err != nil {
				return nil, err
			}
			if !f.attachDropsKnowledge {
				var row map[string]any
				_ = json.Unmarshal(req.Body, &row)
				f.modelRow = row
			}
			return []byte(`{}`), nil

		case strings.HasPrefix(p, "/api/v1/models/model"):
			f.log("modelRow")
			if err := f.fail("modelRow"); err != nil {
				return nil, err
			}
			if f.modelRow == nil {
				return nil, fmt.Errorf("NOT_FOUND")
			}
			return json.Marshal(f.modelRow)
		}
		return nil, fmt.Errorf("fakeOWUI: unrouted path %q", p)
	}
}

// client builds a protocol client over this fake.
func (f *fakeOWUI) client() *openwebui.Client { return openwebui.New(f.transport()) }
