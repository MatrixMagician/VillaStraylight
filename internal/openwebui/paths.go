package openwebui

import (
	"fmt"
	"net/url"
	"strconv"
)

// paths.go is the SINGLE definition of every Open WebUI endpoint this control plane
// speaks. Endpoint literals previously appeared in more than one file with no shared
// client, so two callers could drift onto different paths for the same operation.
//
// Anything that varies within a path — an id, a page number — is composed by a
// function here, so no caller ever concatenates a path.

const (
	// pathHealth is the cheap reachability probe.
	pathHealth = "/health"

	// pathSignIn / pathSignUp mint the admin JWT for the villa service account.
	pathSignIn = "/api/v1/auths/signin"
	pathSignUp = "/api/v1/auths/signup"

	// pathModels lists the served models; pathChatCompletions drives a completion.
	pathModels          = "/api/models"
	pathChatCompletions = "/api/chat/completions"

	// pathUsers enumerates users (admin). pathFiles uploads one file.
	pathUsers = "/api/v1/users/"
	pathFiles = "/api/v1/files/"

	// pathKnowledge lists knowledge collections; pathKnowledgeCreate creates one.
	pathKnowledge       = "/api/v1/knowledge/"
	pathKnowledgeCreate = "/api/v1/knowledge/create"

	// pathModelRow reads one Model row; pathModelUpdate writes it; pathModelCreate
	// creates a fresh one.
	pathModelRow    = "/api/v1/models/model"
	pathModelUpdate = "/api/v1/models/model/update"
	pathModelCreate = "/api/v1/models/create"
)

// healthTimeoutSeconds bounds the reachability probe. It is a gate, not a wait: a
// down service must be reported promptly rather than block the caller.
const healthTimeoutSeconds = 5

// ChatsPageSize is the admin chats-list page size the pinned Open WebUI digest
// hard-codes. A page with fewer items is the LAST page, which is how pagination
// terminates.
const ChatsPageSize = 60

// pathUsersPage is the admin users list, one page.
func pathUsersPage(page int) string { return pathUsers + "?page=" + strconv.Itoa(page) }

// pathUserChatsPage is the ADMIN per-user chats list. It is deliberately not the
// self-list endpoint (`GET /api/v1/chats/`), which silently under-indexes: this one
// hard-codes include_archived and applies no folder or pinned filtering, so it is
// the complete chat universe for that user.
func pathUserChatsPage(userID string, page int) string {
	return "/api/v1/chats/list/user/" + userID + "?page=" + strconv.Itoa(page)
}

// pathChat fetches one full chat document.
func pathChat(chatID string) string { return "/api/v1/chats/" + chatID }

// pathKnowledgePage is the knowledge list, one page.
func pathKnowledgePage(page int) string {
	return fmt.Sprintf("%s?page=%d", pathKnowledge, page)
}

// pathFileStatus polls one file's chunk-embed-store progress.
func pathFileStatus(fileID string) string { return pathFiles + fileID + "/process/status" }

// pathFileDelete removes a stand-alone uploaded file and its vectors. Unlike
// pathKnowledgeFileRemove it does NOT require collection membership, so it is the
// cleanup path for a file that was uploaded and embedded but never joined one.
func pathFileDelete(fileID string) string { return pathFiles + fileID }

// pathKnowledgeFileAdd joins an uploaded file to a collection.
func pathKnowledgeFileAdd(kbID string) string { return pathKnowledge + kbID + "/file/add" }

// pathKnowledgeFileRemove is the clean-replace primitive. delete_file=true is
// load-bearing: it deletes the vectors by file id AND content hash and drops the
// per-file vector collection, which is what makes replacement leak-free.
func pathKnowledgeFileRemove(kbID string) string {
	return pathKnowledge + kbID + "/file/remove?delete_file=true"
}

// pathKnowledgeReset is the rebuild primitive. It drops the collection's vectors and
// clears its file list while KEEPING the collection id, so the served model's
// meta.knowledge attachment survives.
//
// It is deliberately NOT `DELETE /knowledge/{id}/delete`, which changes the id and
// strips the collection from every model's meta.knowledge.
func pathKnowledgeReset(kbID string) string { return pathKnowledge + kbID + "/reset" }

// pathModelRowByID reads or updates one Model row. The served model id is the GGUF
// filename, an API-returned value, and is query-escaped.
func pathModelRowByID(modelID string) string {
	return pathModelRow + "?id=" + url.QueryEscape(modelID)
}

// pathModelUpdateByID writes one Model row.
func pathModelUpdateByID(modelID string) string {
	return pathModelUpdate + "?id=" + url.QueryEscape(modelID)
}
