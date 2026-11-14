// scope.go is the pure projection of recall-state.json onto what the tool server needs to
// scope a query: the recall collection's id and which transcript files belong to which
// user. Ids only, like the state it is derived from.
//
// PURE: no I/O. The host writes the document (ADR-0042); the tool server reads it on every
// request. An owner never changes once a transcript is uploaded, so a stale scope can miss
// a new chat but cannot cross users.
package recall

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/jsonstore"
)

// ScopeSchemaVersion is the scope document's own version. A reader that sees another value
// fails closed.
const ScopeSchemaVersion = 1

const (
	// scopeDirName is the directory under the villa data root the recall unit mounts,
	// and ScopeFileName the one file in it. The unit mounts the directory, never the file:
	// an atomic rename replaces the inode a file mount would hold.
	scopeDirName = "recall-scope"
	// ScopeFileName is the scope document's name inside that directory.
	ScopeFileName = "scope.json"
)

// Scope is the recall scope document: the Knowledge collection's id and a map from each
// indexed transcript's file id to the id of the user who owns the chat.
type Scope struct {
	SchemaVersion int               `json:"schema_version"`
	KnowledgeID   string            `json:"knowledge_id"`
	Files         map[string]string `json:"files"`
}

// GetSchemaVersion and SetSchemaVersion let the shared store stamp the document's version.
func (s *Scope) GetSchemaVersion() int  { return s.SchemaVersion }
func (s *Scope) SetSchemaVersion(v int) { s.SchemaVersion = v }

// scopeStore binds the shared persistence layer to the scope document.
var scopeStore = jsonstore.New[Scope, *Scope]("recall scope", scopeDirName+"/"+ScopeFileName, ScopeSchemaVersion)

// ScopePath is where the host writes the scope document: <data root>/recall-scope/scope.json.
func ScopePath() string { return scopeStore.Path() }

// ScopeDir is the directory villa-tools-recall bind-mounts read-only.
func ScopeDir() string { return filepath.Dir(scopeStore.Path()) }

// WriteScope writes the scope document atomically, directory 0700 and file 0600, under the
// villa data root. It stamps the schema version itself, so a caller cannot write a document
// the reader would fail closed on. It does no filtering: ScopeOf decides what a scope holds.
func WriteScope(sc Scope) error {
	return scopeStore.Save(jsonstore.Deps{WriteAll: func(data []byte) error {
		return scopeStore.WriteFileAtomic(scopeStore.Path(), data)
	}}, sc)
}

// ScopeOf projects the state onto a Scope. A chat with no file id or no owner is left out:
// a transcript with no recorded owner is nobody's, and a guess would widen a read.
func ScopeOf(s State) Scope {
	files := make(map[string]string, len(s.Chats))
	for _, c := range s.Chats {
		if c.FileID == "" || c.UserID == "" {
			continue
		}
		files[c.FileID] = c.UserID
	}
	return Scope{SchemaVersion: ScopeSchemaVersion, KnowledgeID: s.KnowledgeID, Files: files}
}

// FilesOf returns the file ids owned by userID, sorted. An empty user id owns none.
func (sc Scope) FilesOf(userID string) []string {
	if userID == "" {
		return nil
	}
	var out []string
	for fileID, owner := range sc.Files {
		if owner == userID {
			out = append(out, fileID)
		}
	}
	slices.Sort(out)
	return out
}

// ChatIDOf is the inverse of TranscriptFilename: the chat id a transcript upload's name
// carries, and false for a name that is not one.
func ChatIDOf(name string) (string, bool) {
	id, ok := strings.CutPrefix(name, transcriptPrefix)
	if !ok {
		return "", false
	}
	id, ok = strings.CutSuffix(id, transcriptSuffix)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}
