package main

// taskapi.go is the CLI's ONE seam to the workspace agent: the loopback task
// API served by villa-dashboard.service (spec §5). `villa work` and
// `villa task` are HTTP clients of the runner, never a second copy of it, so
// this file is the only place in cmd/villa that knows a route path, and
// internal/taskrun is not imported here at all. The runner lives in the
// long-lived process because that is where an approval must be answerable from
// the terminal AND the dashboard.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
)

// taskAPIDeps is the injectable transport seam: a base URL and a client. Tests
// point it at an httptest server, which exercises the real request shape (the
// same-origin headers included) rather than a hand-written double.
type taskAPIDeps struct {
	base   string
	client *http.Client
}

// liveTaskAPIDeps binds the seam to the dashboard service's loopback bind. No
// client timeout: the event stream is open for the whole life of a task.
func liveTaskAPIDeps(cfg config.VillaConfig) taskAPIDeps {
	return taskAPIDeps{
		base:   "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.DashboardPort)),
		client: &http.Client{},
	}
}

// errAPIUnreachable is a transport-level failure talking to the dashboard
// service. It is distinct from an API error (which means the service answered)
// because only this one has "start the service" as its remediation.
var errAPIUnreachable = errors.New("dashboard service is not answering")

// dashboardDownRemediation is the one sentence every unreachable-service
// refusal ends with.
const dashboardDownRemediation = "Start it with: systemctl --user start villa-dashboard.service"

// do performs one request and returns the response body, mapping a non-2xx to
// the API's own error message so villa never paraphrases the runner.
func (a taskAPIDeps) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, payload)
	if err != nil {
		return nil, err
	}
	if body != nil {
		// The API's same-origin guard demands both on a mutation.
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", a.base)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAPIUnreachable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, apiError(resp.StatusCode, data)
	}
	return data, nil
}

// apiError unwraps the API's {"error": ...} body, falling back to the status
// when the body is not that shape.
func apiError(code int, body []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
		return fmt.Errorf("dashboard API: %s (HTTP %d)", e.Error, code)
	}
	return fmt.Errorf("dashboard API: HTTP %d: %s", code, strings.TrimSpace(string(body)))
}

// submit posts one task. A refusal is a 202 with state refused, not an error:
// the exit code comes from the record alone.
func (a taskAPIDeps) submit(ctx context.Context, workspace, instruction, mode string) (taskstore.Task, error) {
	data, err := a.do(ctx, http.MethodPost, "/api/tasks", map[string]string{
		"workspace": workspace, "instruction": instruction, "mode": mode,
	})
	if err != nil {
		return taskstore.Task{}, err
	}
	var t taskstore.Task
	if err := json.Unmarshal(data, &t); err != nil {
		return taskstore.Task{}, fmt.Errorf("dashboard API: unreadable task record: %w", err)
	}
	return t, nil
}

// show returns the record's bytes VERBATIM, which is what `--json` prints.
func (a taskAPIDeps) show(ctx context.Context, id string) ([]byte, error) {
	return a.do(ctx, http.MethodGet, "/api/tasks/"+id, nil)
}

// list returns the ListView bytes verbatim.
func (a taskAPIDeps) list(ctx context.Context) ([]byte, error) {
	return a.do(ctx, http.MethodGet, "/api/tasks", nil)
}

// answer applies approve, deny or cancel. body is nil for the two that carry
// none, so an empty request body reaches the API exactly as its decoder
// expects.
func (a taskAPIDeps) answer(ctx context.Context, verb, id string, body any) error {
	_, err := a.do(ctx, http.MethodPost, "/api/tasks/"+id+"/"+verb, body)
	return err
}

// events opens the task's SSE stream. The caller owns the returned body and
// must close it.
func (a taskAPIDeps) events(ctx context.Context, id string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/api/tasks/"+id+"/events", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAPIUnreachable, err)
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		return nil, apiError(resp.StatusCode, data)
	}
	return resp.Body, nil
}

// sseFrame is one server-sent event: its type and its data payload.
type sseFrame struct {
	event string
	data  []byte
}

// scanSSE calls fn for every complete frame until the stream ends or fn
// returns false. Only `event:` and `data:` are understood; the API sends
// nothing else, and an unknown field is ignored rather than fatal.
func scanSSE(r io.Reader, fn func(sseFrame) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var f sseFrame
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if f.event != "" || len(f.data) > 0 {
				if !fn(f) {
					return nil
				}
			}
			f = sseFrame{}
		case strings.HasPrefix(line, "event:"):
			f.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			f.data = append(f.data, strings.TrimSpace(strings.TrimPrefix(line, "data:"))...)
		}
	}
	return sc.Err()
}
