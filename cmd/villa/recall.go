// recall.go is the thin cobra caller for the `villa recall` verb tree (D-07,
// RECALL-01/02/03): `recall index [--rebuild]` choreographs the chats→Knowledge
// indexing pipeline and `recall status` reports honest staleness. The DECISION
// logic lives in the pure internal/recall core (Plan diff D-05, RenderTranscript
// D-04, Classify D-06, Load/Save state); this file keeps ONLY the cobra wiring,
// the ordered run bodies (return-not-Exit, modelswap-style numbered steps with
// short-circuit refuse-with-remediation), the exit-code mapping (the AUTHORITATIVE
// preflight constants), and the live host wiring (recallDeps → recall_live.go
// drives + the recall-state.json byte-I/O seam).
//
// GATE DELTA vs `verify memory` (D-07): with the memory stack OFF (or the memory
// config invalid per memory.Decide), BOTH verbs refuse-with-remediation and return
// exitBlocked — an EXPLICIT index/status request can never honestly no-op (verify
// memory's memory-off exit-0 is "nothing to verify"; recall's memory-off is "you
// asked for something that needs the stack you haven't enabled").
//
// Honesty invariants (D-06/Pitfall 8): state is persisted INCREMENTALLY after
// every chat so a mid-run failure never loses completed work;
// last_index_completed_at is stamped ONLY on a clean full pass; `recall status`
// renders an unevaluable live list as the LITERAL "Unknown — could not evaluate",
// never as stale=0. Indexing is strictly SEQUENTIAL (parallel uploads serialize at
// villa-embed anyway and contend with the chat model on the shared gfx1151
// envelope). Discretion (recorded in 21-02-PLAN): status is HUMAN-ONLY this phase
// (no --json — a second frozen contract waits for Phase 23); no --user narrowing
// flag in v1.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
)

// recallDeps are the injectable host seams for the recall verbs, so both run
// bodies are fully testable off-hardware with fake closures (mirrors
// verifyMemoryDeps / modelswap.Deps). Every host effect — config load, REST drive,
// state byte-I/O, clock — is a func field; the live wiring is liveRecallDeps.
type recallDeps struct {
	// loadedMemoryEnabled is the AUTHORITATIVE memory gate source — the PERSISTED
	// config.LoadVilla().MemoryEnabled (live: liveLoadedMemoryEnabled, failing soft
	// to false so a broken config never silently claims memory is on).
	loadedMemoryEnabled func() bool
	// loadedConfig resolves the loopback chat port + the embedding model/dim skew
	// stamps (live: liveLoadedConfig).
	loadedConfig func() config.VillaConfig
	// mintToken mints the admin JWT over loopback (live: mintAdminToken — the
	// existing villa-verify@localhost service-account seam, D-09).
	mintToken func(ctx context.Context, base string) (string, error)
	// owuiHealthy is the cheap pre-mutation reachability gate (live: owuiHealthy).
	owuiHealthy func(ctx context.Context, base string) error
	// listUsers enumerates all users via the admin endpoint (live: owuiListUsers).
	listUsers func(ctx context.Context, base, token string) ([]owuiUser, error)
	// listUserChats lists one user's COMPLETE chat universe (live: owuiListUserChats).
	listUserChats func(ctx context.Context, base, token, userID string) ([]recall.ChatRef, error)
	// getChat fetches one full chat document (live: owuiGetChat).
	getChat func(ctx context.Context, base, token, chatID string) (recall.ChatDoc, error)
	// ensureKnowledge finds-or-creates the recall KB (live: owuiEnsureKnowledge).
	ensureKnowledge func(ctx context.Context, base, token, name, description string) (string, error)
	// uploadTranscript runs upload→poll→add for one transcript (live:
	// owuiUploadTranscript with the size-aware recallUploadTimeout).
	uploadTranscript func(ctx context.Context, base, token, kbID, filename, content string) (string, error)
	// removeKnowledgeFile is the clean-replace/delete primitive (live:
	// owuiRemoveKnowledgeFile — file/remove?delete_file=true, D-04).
	removeKnowledgeFile func(ctx context.Context, base, token, kbID, fileID string) error
	// resetKnowledge is the id-preserving --rebuild primitive (live: owuiResetKnowledge).
	resetKnowledge func(ctx context.Context, base, token, kbID string) error
	// attachKnowledge asserts the served model's meta.knowledge attachment (live:
	// owuiAttachKnowledge — idempotent read-merge-write, D-03/RECALL-02).
	attachKnowledge func(ctx context.Context, base, token, servedModelID, kbID, kbName string) (recall.AttachmentState, error)
	// attachmentState answers status's retrieval question (live: owuiAttachmentState).
	attachmentState func(ctx context.Context, base, token, kbID string) recall.AttachmentState
	// discoverModel resolves the SERVED model id (live: discoverChatModel wrapped
	// with the bearer header — the GGUF filename, not the config slug).
	discoverModel func(ctx context.Context, base, token string) (string, error)
	// readState loads recall-state.json fail-closed (live: recall.Load over os.ReadFile).
	readState func() (recall.State, error)
	// writeState persists the state atomically (live: recall.Save over WriteFileAtomic).
	writeState func(recall.State) error
	// now supplies the clock for the RFC3339 UTC run stamps.
	now func() time.Time
}

// liveRecallDeps wires recallDeps to the real host: the persisted config seams,
// the recall_live.go REST drives, and the recall-state.json atomic store.
func liveRecallDeps() recallDeps {
	return recallDeps{
		loadedMemoryEnabled: liveLoadedMemoryEnabled,
		loadedConfig:        liveLoadedConfig,
		mintToken:           mintAdminToken,
		owuiHealthy:         owuiHealthy,
		listUsers:           owuiListUsers,
		listUserChats:       owuiListUserChats,
		getChat:             owuiGetChat,
		ensureKnowledge:     owuiEnsureKnowledge,
		uploadTranscript: func(ctx context.Context, base, token, kbID, filename, content string) (string, error) {
			return owuiUploadTranscript(ctx, base, token, kbID, filename, content, recallUploadTimeout(content))
		},
		removeKnowledgeFile: owuiRemoveKnowledgeFile,
		resetKnowledge:      owuiResetKnowledge,
		attachKnowledge:     owuiAttachKnowledge,
		attachmentState:     owuiAttachmentState,
		discoverModel: func(ctx context.Context, base, token string) (string, error) {
			return discoverChatModel(ctx, base, bearerHeader(token))
		},
		readState: liveRecallStateLoad,
		writeState: func(s recall.State) error {
			return recall.Save(recall.Deps{WriteAll: func(data []byte) error {
				return recall.WriteFileAtomic(recall.RecallStatePath(), data)
			}}, s)
		},
		now: time.Now,
	}
}

// liveRecallStateLoad is the SHARED fail-closed recall-state.json reader (the
// "recall-state read: use recall.Load" rule — never a second JSON reader): an
// absent store reads as the empty state ("nothing indexed", typed-Unknown for the
// D-10 skew guards), a corrupt/future-schema blob fail-closes to empty inside
// recall.Load, and only a real I/O fault surfaces as an error. It is the live
// wiring for BOTH recallDeps.readState and installDeps.readRecallState (the
// Phase-23 install WARN surface) so the two guards can never drift onto
// different readers.
func liveRecallStateLoad() (recall.State, error) {
	return recall.Load(recall.Deps{ReadAll: func() ([]byte, error) {
		data, err := os.ReadFile(recall.RecallStatePath())
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil // absent store ⇒ empty state ("nothing indexed")
		}
		return data, err
	}})
}

// newRecall builds the `villa recall` parent command (D-07).
func newRecall() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recall",
		Short: "Index past conversations into Knowledge for semantic retrieval",
		Long: "Index your Open WebUI chat history into a villa-managed Knowledge collection so " +
			"new chats can retrieve past conversations BY MEANING, with citations — strictly " +
			"local (OWUI's own chunk → embed → Qdrant path over the existing loopback port; no " +
			"new host port, zero outbound). Requires the memory stack (memory_enabled=true + " +
			"`villa install`); both subcommands refuse with remediation otherwise.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newRecallIndex(), newRecallStatus())
	return cmd
}

// newRecallIndex builds `villa recall index`: the incremental (default) or
// --rebuild (id-preserving reset + full re-index) indexing run. The exit-code
// mapping lives ENTIRELY in runRecallIndex (return-not-Exit body; cobra RunE calls
// os.Exit) — exitPass on a clean full pass, exitBlocked on the gate or any failed
// step (completed work stays persisted; re-run to resume).
func newRecallIndex() *cobra.Command {
	var rebuild bool
	var sharedRecallAck bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index past chats into the recall Knowledge collection (incremental; --rebuild for clean-recreate)",
		Long: "Drive the chats → Knowledge pipeline over the existing loopback port: list every " +
			"user's chats (archived included; the villa service account excluded), diff against " +
			"recall-state.json, clean-replace changed chats (remove old transcript, re-upload), " +
			"and idempotently attach the recall collection to the SERVED model so retrieval works " +
			"in every new chat. State persists after EVERY chat — a failed run never loses " +
			"completed work; re-run to resume. --rebuild resets the collection (id-preserving) " +
			"and re-indexes everything.\n\n" +
			"SINGLE-OPERATOR ASSUMPTION (D-09): recall pools EVERY non-service user's chats into " +
			"ONE shared collection attached to the served model — every user could then retrieve " +
			"every other user's conversations. On a box with more than one human user the run " +
			"REFUSES unless you pass --i-understand-shared-recall to acknowledge the cross-user " +
			"exposure (until per-user-scoped KBs land).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runRecallIndex(cmd, args, liveRecallDeps(), rebuild, sharedRecallAck))
			return nil
		},
	}
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "reset the recall Knowledge collection (id-preserving) and re-index everything")
	cmd.Flags().BoolVar(&sharedRecallAck, "i-understand-shared-recall", false, "acknowledge that recall pools ALL users' chats into one shared collection (required when >1 human user exists)")
	return cmd
}

// newRecallStatus builds `villa recall status`: the honest staleness report
// (indexed / last-indexed / stale vs the LIVE chat list, plus the model-attachment
// state). Exit codes: exitPass (current + attached), exitWarn (stale, unevaluable
// Unknown, or attachment missing/unknown), exitBlocked (memory gate only).
func newRecallStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report the recall index's staleness and retrieval attachment (honest, typed-Unknown)",
		Long: "Compare recall-state.json against the LIVE Open WebUI chat list and report " +
			"indexed count, last index run (complete vs partial), stale breakdown " +
			"(new/changed/deleted), and whether the recall collection is attached to the served " +
			"model. When Open WebUI cannot be evaluated the stale count is reported as " +
			"\"Unknown — could not evaluate\" — NEVER as 0.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runRecallStatus(cmd, args, liveRecallDeps()))
			return nil
		},
	}
}

// recallGate is the shared D-07 enablement gate for both verbs: the persisted
// memory_enabled AND memory.Decide's fields-valid verdict must both hold. A
// refusal prints remediation to errOut and reports block=true — the deliberate
// delta from verify memory's memory-off exit-0 (an explicit recall request can
// never honestly no-op).
func recallGate(verb string, deps recallDeps, errOut interface{ Write([]byte) (int, error) }) (config.VillaConfig, bool) {
	cfg := deps.loadedConfig()
	d := memory.Decide(cfg)
	if !deps.loadedMemoryEnabled() || !d.Enabled {
		fmt.Fprintf(errOut, "recall %s: the memory stack is not enabled — recall requires it: set memory_enabled=true in config.toml and run `villa install`, then re-run.\n", verb)
		return cfg, true
	}
	if !d.Valid {
		fmt.Fprintf(errOut, "recall %s: the memory configuration is invalid — %s — fix config.toml and run `villa install`, then re-run.\n", verb, strings.Join(d.Reasons, "; "))
		return cfg, true
	}
	return cfg, false
}

// recallLiveChats builds the live chat universe: every user EXCEPT the
// villa-verify@localhost service account (D-09 — the only identity villa can
// deterministically exclude; all remaining human users on this single-operator box
// are the operator), each listed via the admin archived-inclusive endpoint
// (Pitfall 1). Any listing failure is an error — never a partial universe.
func recallLiveChats(ctx context.Context, deps recallDeps, base, token string) ([]recall.ChatRef, error) {
	users, err := deps.listUsers(ctx, base, token)
	if err != nil {
		return nil, err
	}
	return recallChatsForUsers(ctx, deps, base, token, users)
}

// recallHumanUsers returns the users that are NOT the villa service account (D-09)
// — the operator(s) whose chats recall would pool into the shared collection. The
// WR-05 single-operator guard counts these.
func recallHumanUsers(users []owuiUser) []owuiUser {
	var humans []owuiUser
	for _, u := range users {
		if u.Email == recallServiceAccountEmail {
			continue
		}
		humans = append(humans, u)
	}
	return humans
}

// recallChatsForUsers builds the chat universe across the given users, excluding
// the service account (D-09). Any per-user listing failure is an error — never a
// partial universe.
func recallChatsForUsers(ctx context.Context, deps recallDeps, base, token string, users []owuiUser) ([]recall.ChatRef, error) {
	var live []recall.ChatRef
	for _, u := range users {
		if u.Email == recallServiceAccountEmail {
			continue
		}
		chats, err := deps.listUserChats(ctx, base, token, u.ID)
		if err != nil {
			return nil, err
		}
		live = append(live, chats...)
	}
	return live, nil
}

// chatOutcome is the typed result of indexing one chat (WR-02): the caller
// increments its run counters from this verdict directly, never by checking
// state.Chats presence after the fact (which an unrelated future mutation could
// silently flip, miscounting the operator-facing summary line).
type chatOutcome int

const (
	// outcomeFail — a host step failed; the run must short-circuit (exitBlocked).
	outcomeFail chatOutcome = iota
	// outcomeUploaded — the transcript was (re-)uploaded and recorded in state.
	outcomeUploaded
	// outcomeSkipped — the chat was unrenderable; its stale entry was dropped and
	// the skip recorded (never a silent drop, D-04).
	outcomeSkipped
)

// runRecallIndex is the ordered index pipeline (modelswap-style numbered steps,
// each short-circuiting with a refuse-with-remediation naming the failed step):
// gate → reachability → token → users + single-operator guard (a refusal is
// SIDE-EFFECT-FREE: it runs BEFORE any state/KB mutation — review WR-01) →
// state+KB (reset on --rebuild; started stamped, completed CLEARED, persisted —
// a crash mid-run must read as a partial run) →
// chat list (service account excluded) → Plan diff → sequential execute with
// per-chat incremental persist (Deletes, then Updates as remove-old→render→
// upload, then Adds; an unrenderable chat is a RECORDED skip) → idempotent attach
// → completed stamp ONLY on the clean full pass. It RETURNS the exit code so
// recall_test.go can drive it deterministically.
func runRecallIndex(cmd *cobra.Command, _ []string, deps recallDeps, rebuild, sharedRecallAck bool) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	ctx := cmd.Context()

	// (1) GATE (D-07): memory off OR invalid ⇒ exitBlocked, never a no-op.
	cfg, blocked := recallGate("index", deps, errOut)
	if blocked {
		return exitBlocked
	}

	// (2) REACHABILITY (Pitfall 7): knowledge/create needs villa-embed up behind
	// OWUI — refuse BEFORE any mutating step, naming the services to check.
	base := fmt.Sprintf("http://%s:%d", verifyMemoryLoopbackAddr, cfg.ChatPort)
	if err := deps.owuiHealthy(ctx, base); err != nil {
		fmt.Fprintf(errOut, "recall index: Open WebUI is not reachable at %s (%v) — check `systemctl --user status villa-openwebui.service` and the villa-embed service, then re-run.\n", base, err)
		return exitBlocked
	}

	// (3) TOKEN (D-09): the existing admin service-account JWT, in memory only.
	token, err := deps.mintToken(ctx, base)
	if err != nil {
		fmt.Fprintf(errOut, "recall index: could not mint the admin token (%v) — check the Open WebUI service account, then re-run.\n", err)
		return exitBlocked
	}

	// (3a) LIST USERS + SINGLE-OPERATOR GUARD (WR-05/D-09, hoisted per the
	// Phase-23 review WR-01): a refusal must be SIDE-EFFECT-FREE, so the guard
	// runs BEFORE any state/KB mutation below — previously a refused --rebuild
	// had already reset the collection, and every refused run had already
	// re-stamped the embedding identity for an index pass that never happened
	// (destroying the recorded truth the skew guard depends on). The token is
	// the only dependency, so this is the earliest the guard can run; the
	// fetched users are reused for the chat listing at step (5).
	users, err := deps.listUsers(ctx, base, token)
	if err != nil {
		fmt.Fprintf(errOut, "recall index: FAILED listing users (%v) — check Open WebUI, then re-run.\n", err)
		return exitBlocked
	}
	// Recall pools EVERY human user's chats into ONE shared collection attached
	// to the served model, so every user could retrieve every other user's
	// conversations. On >1 human user the run REFUSES with remediation until the
	// operator explicitly acknowledges the shared-recall exposure (until
	// per-user-scoped KBs land). Fail-closed, not silent.
	humans := recallHumanUsers(users)
	if len(humans) > 1 && !sharedRecallAck {
		fmt.Fprintf(errOut, "recall index: REFUSING — found %d human users on this box, but recall pools ALL users' chats into one shared collection visible (with citations) to every user of the served model. This is a cross-user disclosure. If this is a single-operator box (or you accept the shared exposure), re-run with --i-understand-shared-recall; otherwise wait for per-user-scoped recall.\n", len(humans))
		return exitBlocked
	}

	// (4) STATE + KB: fail-closed read; --rebuild resets the KB (id-preserving)
	// and clears the chats map; ensure the KB; stamp started, CLEAR completed,
	// persist — a crash from here on reads as a partial run (D-06/Pitfall 8).
	state, err := deps.readState()
	if err != nil {
		fmt.Fprintf(errOut, "recall index: could not read recall-state.json (%v) — fix the data dir, then re-run.\n", err)
		return exitBlocked
	}
	// (4a) D-10 SKEW GUARD (Phase-23, T-23-15/T-23-16): compare the RECORDED
	// embedding identity against config IMMEDIATELY after the fail-closed read and
	// BEFORE any state mutation — the stamp overwrite below would destroy the
	// recorded truth and make the skew undetectable forever (Pitfall 6).
	// recall.EmbeddingSkew is THE single comparison (Plan 23-01) — never re-rolled
	// here. An empty stamp is typed-Unknown: no recorded truth, no alarm. --rebuild
	// is the sanctioned bypass (OQ4): the rebuild path id-preservingly resets the KB
	// and clean-replaces the collection content, and the fresh stamp then records
	// the new identity.
	if !rebuild && recall.EmbeddingSkew(state, cfg.EmbeddingModel, cfg.EmbeddingDim) == recall.SkewMismatch {
		fmt.Fprintf(errOut, "recall index: REFUSING — the index was built with %s (dim %d) but config now says %s (dim %d); indexing into a mismatched-dimension collection corrupts retrieval. Re-run with --rebuild to re-index cleanly, or revert the config.\n",
			state.EmbeddingModel, state.EmbeddingDim, cfg.EmbeddingModel, cfg.EmbeddingDim)
		return exitBlocked
	}
	if rebuild {
		if state.KnowledgeID != "" {
			if err := deps.resetKnowledge(ctx, base, token, state.KnowledgeID); err != nil {
				fmt.Fprintf(errOut, "recall index: FAILED at knowledge/reset (%v) — re-run `villa recall index --rebuild`.\n", err)
				return exitBlocked
			}
		}
		state.Chats = nil
	}
	kbID, err := deps.ensureKnowledge(ctx, base, token, recallKnowledgeName, recallKnowledgeDescription)
	if err != nil {
		fmt.Fprintf(errOut, "recall index: FAILED at ensure-knowledge (%v) — check Open WebUI and villa-embed, then re-run.\n", err)
		return exitBlocked
	}
	state.KnowledgeID = kbID
	state.KnowledgeName = recallKnowledgeName
	state.EmbeddingModel = cfg.EmbeddingModel // Phase-23 skew guards (D-05)
	state.EmbeddingDim = cfg.EmbeddingDim
	state.LastIndexStartedAt = deps.now().UTC().Format(time.RFC3339)
	state.LastIndexCompletedAt = ""
	if state.Chats == nil {
		state.Chats = map[string]recall.ChatState{}
	}
	if err := deps.writeState(state); err != nil {
		fmt.Fprintf(errOut, "recall index: could not persist recall-state.json (%v) — fix the data dir, then re-run.\n", err)
		return exitBlocked
	}

	// (5) LIST (D-09): build the complete chat universe over the users already
	// fetched (and guard-cleared) at step (3a) — service account excluded; any
	// listing failure refuses — an index run cannot proceed on a partial universe.
	live, err := recallChatsForUsers(ctx, deps, base, token, users)
	if err != nil {
		fmt.Fprintf(errOut, "recall index: FAILED listing chats (%v) — check Open WebUI, then re-run.\n", err)
		return exitBlocked
	}

	// (6) PLAN (D-05): the pure diff decides; the rest of this body only executes.
	plan := recall.Plan(live, state)

	// (7) EXECUTE sequentially (never parallel — RESEARCH anti-pattern), state
	// persisted after EVERY chat (D-06): completed work is never lost.
	var added, updated, deleted, skipped int
	persist := func() bool {
		if err := deps.writeState(state); err != nil {
			fmt.Fprintf(errOut, "recall index: could not persist recall-state.json (%v) — fix the data dir, then re-run.\n", err)
			return false
		}
		return true
	}
	// indexChat clean-replaces one chat (D-04): remove the OLD transcript first
	// (Updates), then fetch → render → upload → record + persist. An unrenderable
	// chat is a RECORDED skip that drops any stale entry — never a silent drop. It
	// returns a TYPED outcome (WR-02) so the caller increments counters from the
	// actual operation result, never by presence-after-the-fact in state.Chats
	// (which an unrelated future mutation could silently flip).
	indexChat := func(ref recall.ChatRef, oldFileID string) chatOutcome {
		if oldFileID != "" {
			if err := deps.removeKnowledgeFile(ctx, base, token, kbID, oldFileID); err != nil {
				fmt.Fprintf(errOut, "recall index: FAILED at chat %s (knowledge/file/remove: %v) — completed work is persisted; re-run `villa recall index` to resume.\n", ref.ID, err)
				return outcomeFail
			}
			// The OLD transcript is now gone from the KB. From here on, ANY failure
			// must leave the chat re-qualifying on the next run (WR-01): a stale
			// state entry still claiming FileID=old would make Plan see neither an
			// Add nor an Update, so the removed transcript would never be
			// re-uploaded — the chat would be silently absent from retrieval while
			// status reported it indexed. Clear the entry's FileID/OWUIUpdatedAt and
			// persist so the next Plan re-Adds (or re-Updates) it.
			cur := state.Chats[ref.ID]
			cur.FileID = ""
			cur.OWUIUpdatedAt = 0
			state.Chats[ref.ID] = cur
			if !persist() {
				return outcomeFail
			}
		}
		doc, err := deps.getChat(ctx, base, token, ref.ID)
		if err != nil {
			fmt.Fprintf(errOut, "recall index: FAILED at chat %s (get: %v) — completed work is persisted; re-run `villa recall index` to resume.\n", ref.ID, err)
			return outcomeFail
		}
		text, renderable := recall.RenderTranscript(doc)
		if !renderable {
			delete(state.Chats, ref.ID)
			if !persist() {
				return outcomeFail
			}
			return outcomeSkipped
		}
		fileID, err := deps.uploadTranscript(ctx, base, token, kbID, recall.TranscriptFilename(ref.ID), text)
		if err != nil {
			fmt.Fprintf(errOut, "recall index: FAILED at chat %s (upload: %v) — completed work is persisted; re-run `villa recall index` to resume.\n", ref.ID, err)
			return outcomeFail
		}
		state.Chats[ref.ID] = recall.ChatState{
			UserID:        ref.UserID,
			OWUIUpdatedAt: ref.UpdatedAt,
			FileID:        fileID,
			IndexedAt:     deps.now().UTC().Format(time.RFC3339),
		}
		if !persist() {
			return outcomeFail
		}
		return outcomeUploaded
	}

	for _, id := range plan.Deletes {
		if prior, ok := state.Chats[id]; ok && prior.FileID != "" {
			if err := deps.removeKnowledgeFile(ctx, base, token, kbID, prior.FileID); err != nil {
				fmt.Fprintf(errOut, "recall index: FAILED at chat %s (knowledge/file/remove: %v) — completed work is persisted; re-run `villa recall index` to resume.\n", id, err)
				return exitBlocked
			}
		}
		delete(state.Chats, id)
		if !persist() {
			return exitBlocked
		}
		deleted++
	}
	for _, ref := range plan.Updates {
		switch indexChat(ref, state.Chats[ref.ID].FileID) {
		case outcomeUploaded:
			updated++
		case outcomeSkipped:
			skipped++
		case outcomeFail:
			return exitBlocked
		}
	}
	for _, ref := range plan.Adds {
		switch indexChat(ref, "") {
		case outcomeUploaded:
			added++
		case outcomeSkipped:
			skipped++
		case outcomeFail:
			return exitBlocked
		}
	}

	// (8) ATTACH (D-03, RECALL-02): idempotently wire the recall KB into the
	// SERVED model's meta.knowledge — an index without retrieval wiring does not
	// satisfy RECALL-02, so a failure here is a FAILURE, never a warning.
	served, err := deps.discoverModel(ctx, base, token)
	if err != nil {
		fmt.Fprintf(errOut, "recall index: FAILED discovering the served model (%v) — retrieval is NOT wired; check villa-llama/Open WebUI, then re-run.\n", err)
		return exitBlocked
	}
	if _, err := deps.attachKnowledge(ctx, base, token, served, kbID, recallKnowledgeName); err != nil {
		fmt.Fprintf(errOut, "recall index: FAILED attaching the recall collection to model %q (%v) — retrieval is NOT wired (RECALL-02); re-run `villa recall index`.\n", served, err)
		return exitBlocked
	}

	// (9) STAMP: only the clean full pass earns last_index_completed_at (D-06,
	// CR-01). Reconcile that EVERY planned Add and Update was either uploaded or
	// recorded-as-skipped in THIS run before stamping complete — not merely that no
	// loop returned early. An incomplete pass (e.g. a chat that became unrenderable
	// AND was clean-replace-removed but not re-uploaded) must stay structurally
	// PARTIAL so the next run re-qualifies it, never falsely "complete" over a KB
	// missing content.
	expected := len(plan.Adds) + len(plan.Updates)
	done := added + updated + skipped
	if done != expected {
		fmt.Fprintf(errOut, "recall index: incomplete pass (%d/%d planned add/update items reconciled) — NOT stamping complete; re-run `villa recall index` to finish.\n", done, expected)
		return exitBlocked
	}
	state.LastIndexCompletedAt = deps.now().UTC().Format(time.RFC3339)
	if !persist() {
		return exitBlocked
	}
	fmt.Fprintf(out, "recall index: complete — %d added, %d updated, %d deleted, %d skipped; %d chats indexed; retrieval attached to %q.\n",
		added, updated, deleted, skipped, len(state.Chats), served)
	return exitPass
}

// runRecallStatus is the honest staleness report (D-06): villa-side truths
// (indexed, last-index stamps, complete-vs-partial) always render from state; the
// stale breakdown renders ONLY when the live chat list was evaluable — any
// mint/list failure degrades to the LITERAL "Unknown — could not evaluate" at
// exitWarn, NEVER stale=0. The attachment state folds in (Pitfall 2: a model swap
// silently detaches recall). Exit codes: exitPass only when stale is KNOWN-zero
// AND attached; exitWarn otherwise; exitBlocked only for the gate.
func runRecallStatus(cmd *cobra.Command, _ []string, deps recallDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	ctx := cmd.Context()

	// GATE — identical to index (D-07): an explicit status request on a disabled
	// stack is refused, never answered with fabricated zeros.
	cfg, blocked := recallGate("status", deps, errOut)
	if blocked {
		return exitBlocked
	}

	state, err := deps.readState()
	if err != nil {
		// WR-06: a real state-file I/O error (recall.Load already fail-closes a
		// corrupt blob to empty state, so this only fires on e.g. a permissions
		// fault) is a hard, unevaluable condition — exitBlocked, identical to the
		// index path. exitWarn is reserved for the typed-Unknown live-list case
		// below (the only legitimate soft state).
		fmt.Fprintf(errOut, "recall status: could not read recall-state.json (%v) — fix the data dir, then re-run.\n", err)
		return exitBlocked
	}

	// Build the LIVE list; liveKnown=false on ANY failure — never a partial
	// false-current (D-06). Attachment is evaluated only with a minted token.
	base := fmt.Sprintf("http://%s:%d", verifyMemoryLoopbackAddr, cfg.ChatPort)
	var live []recall.ChatRef
	liveKnown := false
	attachment := recall.AttachmentUnknown
	if token, err := deps.mintToken(ctx, base); err == nil {
		if chats, err := recallLiveChats(ctx, deps, base, token); err == nil {
			live = chats
			liveKnown = true
		}
		if state.KnowledgeID != "" {
			attachment = deps.attachmentState(ctx, base, token, state.KnowledgeID)
		} else {
			// No KB recorded ⇒ nothing can be attached — confidently missing
			// (a first `villa recall index` creates and attaches it).
			attachment = recall.AttachmentMissing
		}
	}

	rep := recall.Classify(live, liveKnown, attachment, state)

	fmt.Fprintf(out, "recall status:\n")
	noun := "chats"
	if rep.Indexed == 1 {
		noun = "chat"
	}
	fmt.Fprintf(out, "  indexed:    %d %s\n", rep.Indexed, noun)
	switch {
	case rep.LastIndexStartedAt == "":
		fmt.Fprintf(out, "  last index: never — run `villa recall index`\n")
	case rep.CompleteRun:
		fmt.Fprintf(out, "  last index: completed %s (started %s)\n", rep.LastIndexCompletedAt, rep.LastIndexStartedAt)
	default:
		fmt.Fprintf(out, "  last index: PARTIAL — started %s, never completed (remainder treated as stale)\n", rep.LastIndexStartedAt)
	}
	if rep.StaleKnown {
		fmt.Fprintf(out, "  stale:      %d (new %d / changed %d / deleted %d)\n", rep.Stale, rep.New, rep.Changed, rep.Deleted)
	} else {
		fmt.Fprintf(out, "  stale:      Unknown — could not evaluate\n")
	}
	switch rep.Attachment {
	case recall.AttachmentAttached:
		fmt.Fprintf(out, "  retrieval:  attached — the recall collection is wired into the served model\n")
	case recall.AttachmentMissing:
		fmt.Fprintf(out, "  retrieval:  MISSING — retrieval is OFF; run `villa recall index` to re-attach (required after `villa model swap`)\n")
	default:
		fmt.Fprintf(out, "  retrieval:  unknown — could not evaluate\n")
	}
	for _, reason := range rep.Reasons {
		fmt.Fprintf(out, "  note:       %s\n", reason)
	}

	if rep.StaleKnown && rep.Stale == 0 && rep.Attachment == recall.AttachmentAttached {
		return exitPass
	}
	return exitWarn
}
