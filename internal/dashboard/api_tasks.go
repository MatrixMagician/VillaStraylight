package dashboard

// api_tasks.go is the loopback task API (spec v1.11 §5): the route table the
// mux is built from, and the seven task handlers over internal/taskrun. The
// handlers add no lifecycle logic; they decode, call the runner, and serialize
// the record. When no runner is configured (Config.Tasks nil) every task route
// answers 503, so a dashboard on a host without the workspace agent never
// pretends to accept a task.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/taskrun"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

// apiRoute is one method-and-pattern registration on the API mux. The table
// is what routes.golden freezes.
type apiRoute struct {
	method  string
	pattern string
	handler http.HandlerFunc
}

// apiRoutes is the whole API route table, in registration order.
func (s *Server) apiRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/status", s.handleStatus},
		{http.MethodGet, "/api/healthz", s.handleHealthz},
		{http.MethodGet, "/api/metrics", s.handleMetrics},
		{http.MethodGet, "/api/gpu", s.handleGPU},
		{http.MethodGet, "/api/models", s.handleModels},
		{http.MethodPost, "/api/models/switch", s.handleSwitch},
		{http.MethodGet, "/api/workspaces", s.handleWorkspaces},
		{http.MethodGet, "/api/pins", s.handlePins},
		{http.MethodGet, "/api/journal", s.handleJournal},
		{http.MethodGet, "/api/tasks", s.handleTaskList},
		{http.MethodPost, "/api/tasks", s.handleTaskSubmit},
		{http.MethodGet, "/api/tasks/{id}", s.handleTaskShow},
		{http.MethodGet, "/api/tasks/{id}/events", s.handleTaskEvents},
		{http.MethodPost, "/api/tasks/{id}/approve", s.handleTaskApprove},
		{http.MethodPost, "/api/tasks/{id}/deny", s.handleTaskDeny},
		{http.MethodPost, "/api/tasks/{id}/cancel", s.handleTaskCancel},
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

// workspaceEntry is one row of the /api/workspaces shape, mirroring `villa
// workspace list --json`'s workspaceListEntry (cmd/villa/workspace.go).
type workspaceEntry struct {
	Path string `json:"path"`
}

// workspacesView is the /api/workspaces contract: the same {schema,
// workspaces[]} shape workspaceListView freezes for the CLI.
type workspacesView struct {
	Schema     int              `json:"schema"`
	Workspaces []workspaceEntry `json:"workspaces"`
}

// handleWorkspaces answers GET /api/workspaces from cfg.Workspace via the
// SAME status.Deps.LoadConfig seam internal/status already reads (no new
// Config field). It is config-only and always available — a host without the
// workspace agent enabled still has registered workspaces to show read-only,
// so this route is NOT gated by tasksReady.
func (s *Server) handleWorkspaces(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.statusDeps.LoadConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	view := workspacesView{Schema: 1, Workspaces: []workspaceEntry{}}
	for _, p := range cfg.Workspace {
		view.Workspaces = append(view.Workspaces, workspaceEntry{Path: p})
	}
	writeJSON(w, http.StatusOK, view)
}

// tasksReady answers the 503 when the workspace agent is not enabled and
// reports whether the handler may proceed.
func (s *Server) tasksReady(w http.ResponseWriter) bool {
	if s.tasks == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "workspace agent is not enabled"})
		return false
	}
	return true
}

// writeTaskError maps a runner error to its status: unknown id 404, a task not
// in the state the verb needs 409, anything else 500.
func writeTaskError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, taskstore.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, taskrun.ErrNotAwaiting), errors.Is(err, taskrun.ErrTerminal):
		code = http.StatusConflict
	}
	writeJSON(w, code, errorResponse{Error: err.Error()})
}

func (s *Server) handleTaskList(w http.ResponseWriter, _ *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	tasks, err := s.tasks.List()
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskstore.NewListView(tasks))
}

type submitRequest struct {
	Workspace   string `json:"workspace"`
	Instruction string `json:"instruction"`
	Mode        string `json:"mode"`
}

// handleTaskSubmit answers 202 with the record for a queued AND a refused
// submission: the refusal is a record with its remediation in the log, so the
// CLI's exit-code table works from the record alone. Only a malformed request
// is a 400.
func (s *Server) handleTaskSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	var body submitRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	task, err := s.tasks.Submit(r.Context(), taskrun.SubmitRequest{
		Workspace:   body.Workspace,
		Instruction: body.Instruction,
		Mode:        approval.Mode(body.Mode),
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) handleTaskShow(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	task, err := s.tasks.Load(r.PathValue("id"))
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// handleTaskEvents streams the task's narration as SSE: `event: <Kind>` and
// `data: <Narration JSON>`, one flush per event, ending when the runner closes
// the subscription at the terminal state or the client goes away.
func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	id := r.PathValue("id")
	if _, err := s.tasks.Load(id); err != nil {
		writeTaskError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, stop := s.tasks.Subscribe(id)
	defer stop()
	for {
		select {
		case n, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(n)
			if err != nil {
				return
			}
			if _, err := w.Write([]byte("event: " + n.Kind + "\ndata: " + string(data) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

type approveRequest struct {
	All bool `json:"all"`
}

func (s *Server) handleTaskApprove(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	var body approveRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	// An empty body is {"all": false}; anything else that fails to decode is refused.
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	s.answerTask(w, r.PathValue("id"), func(id string) error { return s.tasks.Approve(id, body.All) })
}

func (s *Server) handleTaskDeny(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	s.answerTask(w, r.PathValue("id"), s.tasks.Deny)
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	if !s.tasksReady(w) {
		return
	}
	s.answerTask(w, r.PathValue("id"), s.tasks.Cancel)
}

// answerTask applies one verb and answers with the current record.
func (s *Server) answerTask(w http.ResponseWriter, id string, verb func(string) error) {
	if err := verb(id); err != nil {
		writeTaskError(w, err)
		return
	}
	task, err := s.tasks.Load(id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}
