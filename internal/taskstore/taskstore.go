package taskstore

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/approval"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// schemaVersion is the task record's own self-version. New fields append above
// Schema, which stays last, and every append bumps it (2: observations,
// ADR-0047; 3: taint, ADR-0040; 4: kind and research, ADR-0045). Every write
// stamps it, because the bytes written are always the current shape, a
// pre-upgrade record rewritten by Recover included.
const schemaVersion = 4

// listSchemaVersion is the list view's own version. It is separate from the
// record's because the list does not grow with every record field: it stayed 1
// through observations and taint, and went to 2 when each entry gained kind
// (ADR-0045).
const listSchemaVersion = 2

// Task is one task record (spec §3.2 table), the whole document at
// tasks/<id>.json. Field order is the on-disk contract the show/list goldens
// freeze — do not reorder without a schema bump.
type Task struct {
	ID          string `json:"id"`
	Workspace   string `json:"workspace"`
	Instruction string `json:"instruction"`
	Mode        string `json:"mode"`
	State       State  `json:"state"`
	// Exit is nil until the record reaches a terminal state — a queued or
	// running task has no exit code yet, and the field is omitted rather than
	// forcing a misleading 0. Transition sets it the moment Terminal(to) is
	// true, from Exit(to), and never earlier.
	Exit        *int        `json:"exit,omitempty"`
	SubmittedAt string      `json:"submitted_at"`
	StartedAt   string      `json:"started_at,omitempty"`
	FinishedAt  string      `json:"finished_at,omitempty"`
	Harness     Harness     `json:"harness"`
	Files       []FileEvent `json:"files"`
	Approvals   []Approval  `json:"approvals"`
	Grounding   Grounding   `json:"grounding"`
	// Observations is what villa read at each distinct Files path after the
	// run, in first-reported order. Files is the harness's word; this is villa's.
	Observations []Observation `json:"observations"`
	// Taint is the source that tainted the task (ADR-0040), omitted while the
	// task is clean, so a record from before the field loads untainted. It is
	// set once by SetTaint and never cleared.
	Taint approval.Taint `json:"taint,omitempty"`
	// Kind names what sort of task this is. Empty is a workspace task, so a
	// record written before the field loads as one; the research task
	// (ADR-0045) is the only other kind.
	Kind Kind `json:"kind,omitempty"`
	// Research is the research task's own data, nil for every other kind.
	Research *Research `json:"research,omitempty"`
	Schema   int       `json:"schema"`
}

// Kind is the sort of work a task is (ADR-0045). The zero value is the
// workspace task of spec v1.11.
type Kind string

// KindResearch is a bounded, host-side research task: search, read through
// websafe, report. It has no workspace, no sandbox and no approvals.
const KindResearch Kind = "research"

// IsResearch reports whether the record is a research task.
func (t Task) IsResearch() bool { return t.Kind == KindResearch }

// Research is what a research task carries beyond the common record: the
// budget it was accepted under, the report artifact, and the numbered sources
// the report cites.
type Research struct {
	Budget ResearchBudget `json:"budget"`
	// Report is the report file relative to the villa data root, set once the
	// report is written. Empty when the task ended without one.
	Report  string           `json:"report,omitempty"`
	Sources []ResearchSource `json:"sources"`
}

// ResearchBudget is the round and soft time budget a task was accepted with.
type ResearchBudget struct {
	Rounds  int `json:"rounds"`
	Minutes int `json:"minutes"`
}

// ResearchSource is one fetched page: its number in the report, where it came
// from, and websafe's verdict on it. Flagged never means blocked: the page was
// still read (ADR-0002).
type ResearchSource struct {
	N       int      `json:"n"`
	URL     string   `json:"url"`
	Title   string   `json:"title,omitempty"`
	Flagged bool     `json:"flagged,omitempty"`
	Rules   []string `json:"rules,omitempty"`
}

// ObservationState is what villa saw at a reported path after the run.
// Unobserved is the typed-Unknown: the read failed for a reason other than
// absence, so the path is neither present nor absent.
type ObservationState string

const (
	ObservedPresent ObservationState = "present"
	ObservedAbsent  ObservationState = "absent"
	Unobserved      ObservationState = "unobserved"
)

// Observation is one post-run read of a reported path (ADR-0047). Size and
// SHA256 are set only when present; Error only when unobserved. ObservedAt is
// when the read was made, whatever it found.
type Observation struct {
	Path       string           `json:"path"`
	State      ObservationState `json:"state"`
	Size       *int64           `json:"size,omitempty"`
	SHA256     string           `json:"sha256,omitempty"`
	ObservedAt string           `json:"observed_at"`
	Error      string           `json:"error,omitempty"`
}

// Harness names the coding harness and the version the bridge asserted.
type Harness struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// FileEvent is one file the harness reported created or modified.
type FileEvent struct {
	Path   string `json:"path"`
	Action string `json:"action"` // "created" | "modified"
}

// Approval is one answered (or pending) approval request, spec §3.3.
type Approval struct {
	Seq        int    `json:"seq"`
	Tool       string `json:"tool"`
	Action     string `json:"action"`
	Path       string `json:"path"`
	Command    string `json:"command,omitempty"`
	AskedAt    string `json:"asked_at"`
	Answer     string `json:"answer,omitempty"`
	AnsweredAt string `json:"answered_at,omitempty"`
	By         string `json:"by,omitempty"`
}

// Grounding is the audit outcome (spec §6): whether it ran, and per-document
// claim/unsupported-claim tallies.
type Grounding struct {
	Checked   bool                `json:"checked"`
	Documents []GroundingDocument `json:"documents"`
}

// GroundingDocument is one audited file.
type GroundingDocument struct {
	Path        string        `json:"path"`
	Claims      int           `json:"claims"`
	Unsupported []Unsupported `json:"unsupported"`
}

// Unsupported is one claim the audit could not find a supporting passage for.
type Unsupported struct {
	Claim  string `json:"claim"`
	Reason string `json:"reason"`
}

// ListView is the `villa task list --json` shape: id/workspace/state and the
// three timestamps, never the full record.
type ListView struct {
	Schema int            `json:"schema"`
	Tasks  []ListViewTask `json:"tasks"`
}

// ListViewTask is one row of ListView.
type ListViewTask struct {
	ID          string `json:"id"`
	Workspace   string `json:"workspace"`
	State       State  `json:"state"`
	SubmittedAt string `json:"submitted_at"`
	FinishedAt  string `json:"finished_at,omitempty"`
	Exit        *int   `json:"exit,omitempty"`
	Kind        Kind   `json:"kind,omitempty"`
}

// NewListView projects a []Task (as returned by List, already time-ordered)
// into the frozen list shape.
func NewListView(tasks []Task) ListView {
	v := ListView{Schema: listSchemaVersion, Tasks: make([]ListViewTask, len(tasks))}
	for i, t := range tasks {
		v.Tasks[i] = ListViewTask{
			ID:          t.ID,
			Workspace:   t.Workspace,
			State:       t.State,
			SubmittedAt: t.SubmittedAt,
			FinishedAt:  t.FinishedAt,
			Exit:        t.Exit,
			Kind:        t.Kind,
		}
	}
	return v
}

// NewID mints a time-ordered task id: "20060102-150405-" + 4 lowercase hex
// read from rand (2 bytes), e.g. "20260910-120115-4f2a". A read short of 2
// bytes leaves the remainder zero rather than panicking — deterministic under
// a fixed reader (bytes.Reader with a 2-byte fixture), never fatal under a
// live one.
func NewID(now time.Time, rnd io.Reader) string {
	buf := make([]byte, 2)
	_, _ = io.ReadFull(rnd, buf)
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(buf)
}

// fsDeps is the filesystem seam every core method (Create, Load, Transition,
// List, Recover, AppendLog, ReadLog in log.go) reads and writes through —
// never a direct os call in the core, per CLAUDE.md's "a core must not reach
// for os directly". A full-record write goes through pathsafe.WriteFileAtomic
// directly (the sanctioned seam, not raw os), so it needs no field here; what
// pathsafe and jsonstore do NOT provide — reading one file, listing task ids,
// and an append-only open — do.
//
// There is no live/fake split at the package boundary the way benchstore or
// verifystate have: those are Deps-per-call because a cmd-tier live*Deps binds
// them to $XDG_DATA_HOME. A Store's only external knob is root (New(root)), so
// the live filesystem wiring stays internal to this package, exactly as
// jsonstore's own WriteFileAtomic method is.
type fsDeps struct {
	// readFile mirrors os.ReadFile's contract: data and a nil error, or a nil
	// slice and an error satisfying errors.Is(err, os.ErrNotExist) when absent.
	readFile func(path string) ([]byte, error)
	// listIDs returns the sorted task ids (basenames minus ".json") present
	// under dir, or (nil, nil) when dir does not exist yet.
	listIDs func(dir string) ([]string, error)
	// appendLine appends one already-newline-terminated line to path,
	// creating the file and its parent directory if needed.
	appendLine func(path string, line []byte) error
	// mkdirAll ensures a directory tree exists (0700, private records).
	mkdirAll func(dir string) error
}

func liveFSDeps() fsDeps {
	return fsDeps{
		readFile:   os.ReadFile,
		listIDs:    liveListIDs,
		appendLine: liveAppendLine,
		mkdirAll:   func(dir string) error { return os.MkdirAll(dir, 0o700) },
	}
}

func liveListIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		ids = append(ids, e.Name()[:len(e.Name())-len(".json")])
	}
	sort.Strings(ids)
	return ids, nil
}

func liveAppendLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

// Store is one task tree rooted at <root>/tasks: one <id>.json record and one
// <id>.log JSON-lines transcript per task, plus the directory-wide List and
// Recover sweeps. root is the villa data root (pathsafe.DataRoot() in
// production, a t.TempDir() in tests) — Store never resolves it itself, so a
// test never has to fight $XDG_DATA_HOME.
type Store struct {
	root string
	fs   fsDeps
}

// New builds a Store rooted at root (a data root, not the tasks/ subdirectory
// itself — Store resolves that).
func New(root string) Store {
	return Store{root: root, fs: liveFSDeps()}
}

func (s Store) tasksDir() string            { return filepath.Join(s.root, "tasks") }
func (s Store) recordPath(id string) string { return filepath.Join(s.tasksDir(), id+".json") }

// ErrNotFound reports a task id with no record on disk.
var ErrNotFound = errors.New("taskstore: task not found")

// idPattern is NewID's exact output shape: an 8-digit date, a 6-digit time
// and 4 lowercase hex digits, hyphen-joined. Load and Transition (which
// Load's fail-closed check protects transitively, since it calls Load first)
// refuse anything else before it reaches recordPath/logPath, so a caller that
// forwards an unescaped id straight from an HTTP path segment (GHSA-9hj4)
// cannot smuggle a "../" traversal past this package.
var idPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

func validID(id string) bool { return idPattern.MatchString(id) }

// Create writes a brand-new record. t.ID must be set (NewID) and t.State must
// be Queued or Refused — the only two legal initial states (spec §3.2: refused
// is villa declining before the sandbox ever started). Create refuses to
// overwrite an existing record; records are append-only after submission and
// every later change goes through Transition.
func (s Store) Create(t Task) error {
	if t.ID == "" {
		return fmt.Errorf("taskstore: Create: empty id")
	}
	if t.State != Queued && t.State != Refused {
		return fmt.Errorf("taskstore: Create: initial state %q must be %q or %q", t.State, Queued, Refused)
	}
	if _, err := s.fs.readFile(s.recordPath(t.ID)); err == nil {
		return fmt.Errorf("taskstore: Create: %s already exists", t.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("taskstore: Create: check %s: %w", t.ID, err)
	}
	if t.Files == nil {
		t.Files = []FileEvent{}
	}
	if t.Approvals == nil {
		t.Approvals = []Approval{}
	}
	if t.Grounding.Documents == nil {
		t.Grounding.Documents = []GroundingDocument{}
	}
	if Terminal(t.State) {
		exit := Exit(t.State)
		t.Exit = &exit
	}
	return s.writeRecord(t)
}

// Load reads one record. An absent record is ErrNotFound, never a fabricated
// zero-value task — unlike the single-document stores' fail-closed Load, a
// missing task record is a real, reportable "no such task".
func (s Store) Load(id string) (Task, error) {
	if !validID(id) {
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	data, err := s.fs.readFile(s.recordPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Task{}, fmt.Errorf("taskstore: Load %s: %w", id, err)
	}
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return Task{}, fmt.Errorf("taskstore: Load %s: corrupt record: %w", id, err)
	}
	return t, nil
}

// Transition moves a record from its current state to a new one, refusing any
// edge CanTransition does not permit (including every edge off a terminal
// state). mutate, when non-nil, is applied to the record AFTER State is set
// and BEFORE it is persisted, so the caller can stamp started_at/finished_at,
// append a file event, etc. in the same write. Exit is set the moment the new
// state is terminal, from Exit(to) — never by the caller.
func (s Store) Transition(id string, to State, mutate func(*Task)) (Task, error) {
	recordMu.Lock()
	defer recordMu.Unlock()
	t, err := s.Load(id)
	if err != nil {
		return Task{}, err
	}
	if !CanTransition(t.State, to) {
		return Task{}, fmt.Errorf("taskstore: Transition %s: %q -> %q refused: not a legal edge", id, t.State, to)
	}
	t.State = to
	if mutate != nil {
		mutate(&t)
	}
	if Terminal(to) {
		exit := Exit(to)
		t.Exit = &exit
	}
	if err := s.writeRecord(t); err != nil {
		return Task{}, err
	}
	return t, nil
}

// recordMu serialises the read-modify-write cycle of a record. SetTaint is
// called from outside the worker that owns Transition, and without it a
// transition loaded before a taint landed would write the record back clean.
var recordMu sync.Mutex

// SetTaint marks the task tainted by source (ADR-0040) and reports whether
// this call changed the record. It is one-way and idempotent: a task that is
// already tainted keeps its first source, nothing is written and nothing is
// logged. The first time, one narration line naming the source is appended to
// the task's log, stamped at. A source that is not a known taint is refused.
func (s Store) SetTaint(id string, source approval.Taint, at time.Time) (Task, bool, error) {
	if !source.Valid() {
		return Task{}, false, fmt.Errorf("taskstore: SetTaint %s: %q is not a taint source", id, source)
	}
	recordMu.Lock()
	defer recordMu.Unlock()
	t, err := s.Load(id)
	if err != nil {
		return Task{}, false, err
	}
	if t.Taint.Tainted() {
		return t, false, nil
	}
	t.Taint = source
	if err := s.writeRecord(t); err != nil {
		return Task{}, false, err
	}
	text := "tainted by " + string(source) + ": write and execute actions now ask, in every mode"
	if err := s.AppendLog(id, LogEvent{At: at, Kind: "narration", Text: text}); err != nil {
		return t, true, err
	}
	return t, true, nil
}

func (s Store) writeRecord(t Task) error {
	if t.Research != nil && t.Research.Sources == nil {
		t.Research.Sources = []ResearchSource{}
	}
	if t.Observations == nil {
		t.Observations = []Observation{}
	}
	t.Schema = schemaVersion
	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("taskstore: marshal %s: %w", t.ID, err)
	}
	path := s.recordPath(t.ID)
	if err := pathsafe.AssertRoot(s.root); err != nil {
		return fmt.Errorf("taskstore: refusing an unusable data root: %w", err)
	}
	if err := s.fs.mkdirAll(filepath.Dir(path)); err != nil {
		return fmt.Errorf("taskstore: mkdir tasks dir: %w", err)
	}
	if err := pathsafe.WriteFileAtomic(s.root, path, data, 0o600); err != nil {
		return fmt.Errorf("taskstore: write %s: %w", t.ID, err)
	}
	return nil
}

// List returns every task record in time order. A record that fails to parse
// is a reported error, not a silently-skipped task: a task an operator cannot
// see in `villa task list` is a worse failure than a list command that
// refuses and says why.
func (s Store) List() ([]Task, error) {
	ids, err := s.fs.listIDs(s.tasksDir())
	if err != nil {
		return nil, fmt.Errorf("taskstore: List: %w", err)
	}
	tasks := make([]Task, 0, len(ids))
	for _, id := range ids {
		t, err := s.Load(id)
		if err != nil {
			return nil, fmt.Errorf("taskstore: List: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// Recover marks every non-terminal record Interrupted (spec §3.2: dashboard-
// service start recovery) and returns the ids it changed, in time order.
// Idempotent: a record already terminal (interrupted included) is left
// untouched — a second call changes no file and returns no ids. Recover never
// re-queues; re-running a task is the operator's command.
//
// A record that fails to load or transition is skipped, not fatal (#238):
// every other non-terminal record still needs its container killed by the
// caller, so one corrupt file must not strand the rest at "running" forever.
// Every skip is collected and returned as one joined error.
func (s Store) Recover() ([]string, error) {
	ids, err := s.fs.listIDs(s.tasksDir())
	if err != nil {
		return nil, fmt.Errorf("taskstore: Recover: %w", err)
	}
	var recovered []string
	var errs []error
	for _, id := range ids {
		t, err := s.Load(id)
		if err != nil {
			errs = append(errs, fmt.Errorf("taskstore: Recover: %s: %w", id, err))
			continue
		}
		if Terminal(t.State) {
			continue
		}
		if _, err := s.Transition(id, Interrupted, nil); err != nil {
			errs = append(errs, fmt.Errorf("taskstore: Recover: %s: %w", id, err))
			continue
		}
		recovered = append(recovered, id)
	}
	return recovered, errors.Join(errs...)
}
