// Package research is the round loop of `villa research` (ADR-0045): plan search
// queries from a question, search, fetch the result pages through the websafe
// guard, read them with the served model, repeat for a fixed number of rounds,
// write a report with numbered sources, and have grounding audit it.
//
// The package is a pure core. Searching, fetching, the model, the audit, the
// taint and the clock are all Deps; the runner in internal/taskrun binds them to
// the host and owns the task record. Run never prints, never writes a file and
// never returns an error: every ending is a Result whose Outcome the runner maps
// onto a task state.
//
// The security properties are structural, not asserted in prose:
//   - Fetched text reaches the model only as the websafe fence wrote it, in a
//     user-role message labelled by number, never in a system message.
//   - A page is dropped, not sent, when it is not exactly a websafe fence.
//   - Search results contribute URLs and nothing else. No title or snippet enters a
//     prompt, and the model is never shown a URL.
//   - The task is tainted before the first fetch is requested, and a taint that
//     cannot be written ends the run.
package research

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/grounding"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// Version names the loop's prompts. It is recorded as the harness version, so a
// record says which prompts wrote it; bump it when a prompt changes.
const Version = "1"

// The budget knobs and their hard caps (ADR-0045 §4).
const (
	DefaultRounds  = 2
	MaxRounds      = 4
	DefaultMinutes = 10
	MaxMinutes     = 30

	// QueriesPerRound and PagesPerRound bound one round's outbound traffic.
	QueriesPerRound = 3
	PagesPerRound   = 4
	// PageBytes caps the text of one source in a prompt. It is a first estimate,
	// sized so the audit call, which sees every source, stays near 16k tokens at
	// the defaults.
	PageBytes = 8 << 10
)

// Per-call timeouts. The time budget is soft: a call in flight finishes under its
// own bound.
const (
	SearchTimeout   = 30 * time.Second
	FetchTimeout    = 90 * time.Second
	CompleteTimeout = 5 * time.Minute
)

// Budget is what a research task may spend: rounds of search and read, and a soft
// wall-clock limit after which no new round starts.
type Budget struct {
	Rounds  int
	Minutes int
}

// Resolve fills a zero knob with its default and refuses a negative one or one
// above its hard cap.
func (b Budget) Resolve() (Budget, error) {
	if b.Rounds < 0 || b.Rounds > MaxRounds {
		return Budget{}, fmt.Errorf("rounds must be between 1 and %d (default %d), got %d", MaxRounds, DefaultRounds, b.Rounds)
	}
	if b.Minutes < 0 || b.Minutes > MaxMinutes {
		return Budget{}, fmt.Errorf("minutes must be between 1 and %d (default %d), got %d", MaxMinutes, DefaultMinutes, b.Minutes)
	}
	if b.Rounds == 0 {
		b.Rounds = DefaultRounds
	}
	if b.Minutes == 0 {
		b.Minutes = DefaultMinutes
	}
	return b, nil
}

// Page is one fetched page as the loader answered: its websafe-fenced text and
// the guard's verdict. Content is never raw web text.
type Page struct {
	URL     string
	Title   string
	Content string
	Flagged bool
	Rules   []string
}

// Source is a page the loop kept: numbered in the order it was kept, its fenced
// text clipped to PageBytes. Content is for the prompts and the audit and is not
// part of the record.
type Source struct {
	N       int
	URL     string
	Title   string
	Flagged bool
	Rules   []string
	Content string
}

// Deps is the loop's seam.
type Deps struct {
	// Search returns result URLs for one query. Nothing else of a result crosses.
	Search func(ctx context.Context, query string) ([]string, error)
	// Fetch asks the loader for the pages behind urls. A URL the loader skipped is
	// absent from the answer; an error means the loader itself could not be
	// reached.
	Fetch func(ctx context.Context, urls []string) ([]Page, error)
	// Complete runs one chat completion on the served model.
	Complete func(ctx context.Context, req llm.ChatRequest) (string, error)
	// Audit is grounding.Audit bound to the same model.
	Audit func(ctx context.Context, doc grounding.Document, sources []grounding.Source) grounding.DocumentReport
	// Token returns a fresh unguessable string for one report's source-list
	// delimiter (the websafe fence's nonce, for the same reason).
	Token func() (string, error)
	// Taint marks the task as having read the web (approval.TaintWeb). It is called
	// once, before the first Fetch.
	Taint func() error
	// Narrate reports one line of progress.
	Narrate func(string)
	Now     func() time.Time
}

// Request is one research question under a resolved budget.
type Request struct {
	Question string
	Budget   Budget
}

// Outcome is how a run ended, in the runner's vocabulary: done, flagged (finished
// and needs a look) or failed (could not do the work).
type Outcome string

// The three outcomes a run can have.
const (
	Done    Outcome = "done"
	Flagged Outcome = "flagged"
	Failed  Outcome = "failed"
)

// Result is a finished run. Report is empty when no report was written. Grounding
// carries the audit's verdict with the citation check's findings added to its
// unsupported claims. Reason says why a run did not end done, and what to do.
type Result struct {
	Outcome   Outcome
	Reason    string
	Report    string
	Sources   []Source
	Grounding grounding.DocumentReport
}

// reportDocPath names the audited document in the record and in the audit.
const reportDocPath = "report"

type session struct {
	d       Deps
	req     Request
	sources []Source
	seen    map[string]bool
	notes   []string
	tainted bool
}

// Run executes the loop. ctx cancels it; a cancelled run returns Failed and the
// runner, which owns the cancel, decides the record's state.
func Run(ctx context.Context, d Deps, req Request) Result {
	r := &session{d: d, req: req, seen: map[string]bool{}}
	return r.loop(ctx)
}

func (r *session) loop(ctx context.Context) Result {
	deadline := r.d.Now().Add(time.Duration(r.req.Budget.Minutes) * time.Minute)

	out, err := r.complete(ctx, planRequest(r.req.Question), "plan the searches")
	if err != nil {
		return failed(err.Error())
	}
	queries := parseLines(out, "QUERY:", QueriesPerRound)
	if len(queries) == 0 {
		r.d.Narrate("the plan held no query; searching the question itself")
		queries = []string{r.req.Question}
	}

	for round := 1; round <= r.req.Budget.Rounds; round++ {
		if round > 1 {
			if len(queries) == 0 {
				r.d.Narrate("nothing is missing; no further round")
				break
			}
			if !r.d.Now().Before(deadline) {
				r.d.Narrate(fmt.Sprintf("time budget of %d minutes reached; no further round", r.req.Budget.Minutes))
				break
			}
		}
		r.d.Narrate(fmt.Sprintf("round %d of %d: %s", round, r.req.Budget.Rounds, quoteAll(queries)))

		urls, res := r.search(ctx, queries)
		if res != nil {
			return *res
		}
		if len(urls) == 0 {
			if len(r.sources) == 0 {
				return flagged("the search returned no results. Check that SearXNG's engines are answering (`villa doctor`), or rephrase the question.")
			}
			r.d.Narrate("the follow-up search found nothing new; no further round")
			break
		}

		fresh, res := r.fetch(ctx, urls)
		if res != nil {
			return *res
		}
		if len(fresh) == 0 {
			if len(r.sources) == 0 {
				return flagged("no page could be read: the loader skipped every result (a blocked address, an error status or a timeout). Try again or rephrase the question.")
			}
			r.d.Narrate("the loader skipped every page of this round; no further round")
			break
		}

		out, err := r.complete(ctx, readRequest(r.req.Question, fresh), fmt.Sprintf("read round %d", round))
		if err != nil {
			return failed(err.Error())
		}
		r.notes = append(r.notes, fmt.Sprintf("Round %d:\n%s", round, notesWithoutFollowups(out)))
		queries = parseLines(out, "FOLLOWUP:", FollowupsPerRound)
	}

	return r.finish(ctx)
}

// search asks every query and returns the new URLs, up to PagesPerRound. It ends
// the run (second result) only for the one case that is infrastructure: the
// search could not be reached and nothing has been read yet. A later round that
// loses the search keeps what was read.
func (r *session) search(ctx context.Context, queries []string) ([]string, *Result) {
	var urls []string
	var lastErr error
	for _, q := range queries {
		cctx, cancel := context.WithTimeout(ctx, SearchTimeout)
		found, err := r.d.Search(cctx, q)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ptr(failed("cancelled"))
			}
			lastErr = err
			r.d.Narrate(fmt.Sprintf("search %q failed: %v", q, err))
			continue
		}
		for _, u := range found {
			if u = strings.TrimSpace(u); u != "" && !r.seen[u] && len(urls) < PagesPerRound {
				r.seen[u] = true
				urls = append(urls, u)
			}
		}
	}
	if len(urls) == 0 && lastErr != nil && len(r.sources) == 0 {
		return nil, ptr(failed("web search is not answering: " + lastErr.Error() + ". Check `villa doctor` and that villa-searxng is running (`villa status`)."))
	}
	return urls, nil
}

// fetch taints the task, asks the loader, and keeps the pages that are exactly a
// websafe fence, clipped. The taint comes first, so nothing web-derived is
// requested on an unmarked record.
func (r *session) fetch(ctx context.Context, urls []string) ([]Source, *Result) {
	if !r.tainted {
		if err := r.d.Taint(); err != nil {
			return nil, ptr(failed("could not mark the task as having read the web, so nothing was fetched: " + err.Error()))
		}
		r.tainted = true
	}
	cctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	pages, err := r.d.Fetch(cctx, urls)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ptr(failed("cancelled"))
		}
		if len(r.sources) == 0 {
			return nil, ptr(failed("the page loader is not answering: " + err.Error() + ". Check `villa doctor` and that villa-websafe is running (`villa status`)."))
		}
		r.d.Narrate("the page loader failed this round: " + err.Error())
		return nil, nil
	}
	var fresh []Source
	for _, p := range pages {
		content, ok := websafe.ClipFenced(p.Content, PageBytes)
		if !ok {
			r.d.Narrate("dropped a page that was not in a websafe fence: " + label(p.URL))
			continue
		}
		s := Source{N: len(r.sources) + 1, URL: clean(p.URL), Title: label(p.Title), Flagged: p.Flagged, Rules: p.Rules, Content: content}
		r.sources = append(r.sources, s)
		fresh = append(fresh, s)
		line := fmt.Sprintf("source [%d] %s", s.N, label(s.URL))
		if s.Flagged {
			line += " (flagged by websafe: " + label(strings.Join(s.Rules, ", ")) + ")"
		}
		r.d.Narrate(line)
	}
	return fresh, nil
}

// finish writes the report from the notes and audits it, and decides the ending.
func (r *session) finish(ctx context.Context) Result {
	if len(r.sources) == 0 {
		return flagged("no source was read, so there is nothing to report.")
	}
	out, err := r.complete(ctx, reportRequest(r.req.Question, r.notes), "write the report")
	if err != nil {
		return failed(err.Error())
	}
	body := strings.TrimSpace(out)
	if body == "" {
		return failed("the model returned an empty report. Try again; if it repeats, check the chat unit with `villa status`.")
	}
	// The body is model output over untrusted pages. It is inspected after NFKC
	// normalisation and written defanged into a plain-text file; whatever active
	// content it carried is a finding and ends the task flagged (defang.go).
	norm, kinds := inspect(body)
	token, err := r.d.Token()
	if err != nil {
		return failed("could not make the report's delimiter, so no report was written: " + err.Error())
	}
	if strings.Contains(norm, token) || strings.Contains(body, token) {
		kinds = append(kinds, "the report's own source-list delimiter")
	}
	// A token in the body is removed, so the delimiter line is the only one in the file.
	body = strings.ReplaceAll(defang(norm), token, "(delimiter removed)")
	report := render(r.req.Question, body, token, r.sources)

	gsrc := make([]grounding.Source, len(r.sources))
	allFlagged := true
	for i, s := range r.sources {
		gsrc[i] = grounding.Source{Path: fmt.Sprintf("[%d]", s.N), Content: s.Content}
		allFlagged = allFlagged && s.Flagged
	}
	actx, cancel := context.WithTimeout(ctx, CompleteTimeout)
	rep := r.d.Audit(actx, grounding.Document{Path: reportDocPath, Content: body}, gsrc)
	cancel()
	rep.Path = reportDocPath
	rep.Unsupported = append(rep.Unsupported, citationFindings(norm, len(r.sources))...)
	claims := len(rep.Unsupported)
	for _, k := range kinds {
		rep.Unsupported = append(rep.Unsupported, grounding.Unsupported{
			Claim:  snippet(norm, k),
			Reason: "the report carried " + k + "; villa defanged it",
		})
	}

	res := Result{Outcome: Done, Report: report, Sources: r.sources, Grounding: rep}
	var why []string
	switch {
	case !rep.Checked:
		why = append(why, "the grounding audit could not run: "+rep.Err)
	case rep.Err != "":
		why = append(why, "the grounding audit was cut short: "+rep.Err)
	}
	if claims > 0 {
		why = append(why, fmt.Sprintf("%d claim(s) have no supporting source; the report is left as written", claims))
	}
	if len(kinds) > 0 {
		why = append(why, "the report carried "+strings.Join(kinds, ", ")+", which villa defanged")
	}
	if allFlagged {
		why = append(why, "every source was flagged by websafe, so none can be trusted to read cleanly")
	}
	if len(why) > 0 {
		res.Outcome = Flagged
		res.Reason = strings.Join(why, "; ")
	}
	return res
}

// complete runs one completion under its own timeout. A model that answers with
// nothing is an error, so every caller can treat "no text" as unreachable.
func (r *session) complete(ctx context.Context, req llm.ChatRequest, what string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, CompleteTimeout)
	defer cancel()
	out, err := r.d.Complete(cctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("cancelled while trying to %s", what)
		}
		return "", fmt.Errorf("the model could not %s: %w. Check the chat unit with `villa status`", what, err)
	}
	return out, nil
}

func failed(reason string) Result  { return Result{Outcome: Failed, Reason: reason} }
func flagged(reason string) Result { return Result{Outcome: Flagged, Reason: reason} }
func ptr(r Result) *Result         { return &r }

func quoteAll(qs []string) string {
	q := make([]string, len(qs))
	for i, s := range qs {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ", ")
}
