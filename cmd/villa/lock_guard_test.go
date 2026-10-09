package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

// TestConfigSetWaitsForTheStackLock guards #267: `villa config set` is a load→save
// of the whole config.toml, and a swap's rollback restores the whole captured file,
// so a set that lands inside a swap's prove window is silently reverted. It must
// wait for the lock and read the config only after it has it.
func TestConfigSetWaitsForTheStackLock(t *testing.T) {
	requireWaitsForStackLock(t, func() {
		cmd, _, _ := lifecycleTestCmd()
		runConfigSet(cmd, "ctx=8192", liveConfigDeps())
	})
	cfg, err := config.LoadVilla()
	if err != nil || cfg.Ctx != 8192 {
		t.Errorf("the set must land once the lock is free, got ctx %d, err %v", cfg.Ctx, err)
	}
}

// TestWorkspaceAddRemoveWaitForTheStackLock guards #267: the grant list is written
// into config.toml, so add and remove are config writers a swap rollback can revert.
func TestWorkspaceAddRemoveWaitForTheStackLock(t *testing.T) {
	home := t.TempDir()
	projects := filepath.Join(home, "projects")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	roots := func() (string, string) {
		return filepath.Join(home, ".config", "villa"), filepath.Join(home, ".local", "share", "villa")
	}
	t.Run("add", func(t *testing.T) {
		cfgRoot, dataRoot := roots()
		d, _ := fakeWorkspaceDeps(config.VillaConfig{}, home, cfgRoot, dataRoot)
		requireWaitsForStackLock(t, func() {
			cmd, _, _ := lifecycleTestCmd()
			runWorkspaceAdd(cmd, projects, d)
		})
	})
	t.Run("remove", func(t *testing.T) {
		cfgRoot, dataRoot := roots()
		d, _ := fakeWorkspaceDeps(config.VillaConfig{Workspace: []string{projects}}, home, cfgRoot, dataRoot)
		requireWaitsForStackLock(t, func() {
			cmd, _, _ := lifecycleTestCmd()
			runWorkspaceRemove(cmd, projects, d)
		})
	})
}

// TestRecommendSaveWaitsForTheStackLock guards #267: `recommend --save` loads the
// config on disk and writes it back, the same revertable write as `config set`.
func TestRecommendSaveWaitsForTheStackLock(t *testing.T) {
	requireWaitsForStackLock(t, func() {
		_ = saveRecommendation(io.Discard, fixtureRecommendation(), "")
	})
}

// TestVerifyAgentWaitsForTheStackLock guards #267: `verify agent` stops villa-llama
// and starts it again, which fails a concurrent swap's proof for a reason the swap
// did not cause. The proof (and its stop) must not begin while the lock is held.
func TestVerifyAgentWaitsForTheStackLock(t *testing.T) {
	deps := verifyAgentDeps{
		loadedAgentEnabled: func() bool { return true },
		verifyFn: func(context.Context, verifyAgentDeps) memoryProof {
			return memoryProof{status: preflight.StatusPass}
		},
	}
	requireWaitsForStackLock(t, func() {
		cmd, _, _ := lifecycleTestCmd()
		runVerifyAgent(cmd, nil, deps)
	})
}

// TestBackupWaitsForTheStackLock guards #267: `backup` stops Open WebUI and Qdrant
// to export a clean volume, which a concurrent swap's proof would see as an outage.
func TestBackupWaitsForTheStackLock(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	outPath := filepath.Join(t.TempDir(), "b.tar")
	requireWaitsForStackLock(t, func() {
		cmd, _, _ := newBackupTestCmd()
		runBackup(cmd, outPath, fakeRunDeps(t, map[string][]byte{}))
	})
}

// TestDownWaitsForTheStackLock guards #267: `villa down` stops services, which fails
// a concurrent swap's proof for a reason the swap did not cause and races its
// rollback's restart of what it restarted.
func TestDownWaitsForTheStackLock(t *testing.T) {
	f := newFakeLifecycleDeps(t, twoUnitStack(), orchestrate.Plan{})
	requireWaitsForStackLock(t, func() {
		cmd, _, _ := lifecycleTestCmd()
		runDown(cmd, nil, f.lifecycleDeps)
	})
}

// TestUninstallWaitsForTheStackLock guards #267: `villa uninstall` stops the stack,
// removes its units and volumes and reloads the manager, all of which a swap in
// flight would trip over. The whole teardown is one locked window.
func TestUninstallWaitsForTheStackLock(t *testing.T) {
	f := newFakeUninstallDeps(t, sampleUnits(), t.TempDir())
	requireWaitsForStackLock(t, func() {
		cmd, _, _ := uninstallTestCmd()
		runUninstall(cmd, uninstallOpts{keepModels: true}, f.uninstallDeps)
	})
}

// TestVerifySearchWaitsForTheStackLock guards #267: `verify search` applies a transient
// nft bound in the rootless netns every service of the stack shares, and its deferred
// teardown removes it. A swap's proof running inside that window sees a bounded network
// it did not cause, so the proof must not begin while the lock is held.
func TestVerifySearchWaitsForTheStackLock(t *testing.T) {
	deps := searchVerifyDeps{
		loadedWebSearchEnabled: func() bool { return true },
		verifyFn: func(context.Context, searchVerifyDeps) searchProof {
			return pass("stub")
		},
	}
	requireWaitsForStackLock(t, func() {
		runVerifySearch(newSearchCmd(), nil, deps)
	})
}

// requireWaitsForStackLock is the behavioural half of ADR-0010's coverage: while
// another stack mutation holds the REAL blocking flock (a temp XDG dir, the shape
// TestBenchABSwitchWaitsForTheStackLock uses), run must not return; once the lock is
// released it must proceed. A verb that takes no lock, or takes it after its first
// config read or service stop, returns while the lock is held and fails here.
//
// run executes on its own goroutine, so it must not call t.Fatal.
func requireWaitsForStackLock(t *testing.T, run func()) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "villa"), 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "villa", stacklock.FileName)
	prev := acquireStackLock
	acquireStackLock = func() (*stacklock.Lock, error) { return stacklock.Acquire(lockPath) }
	t.Cleanup(func() { acquireStackLock = prev })

	held, err := stacklock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("hold the stack lock: %v", err)
	}
	done := make(chan struct{})
	go func() {
		run()
		close(done)
	}()

	select {
	case <-done:
		_ = held.Release()
		t.Fatal("the verb ran while another stack mutation held the lock")
	case <-time.After(300 * time.Millisecond):
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the verb never proceeded after the lock was released (a second Acquire nested inside the verb blocks forever: flock is per open file description)")
	}
}

// The static half of ADR-0010's coverage. Every place in the control plane that can
// mutate the running stack, meaning write config.toml, write or restore units, or
// obtain the systemd lifecycle seam, must sit in a function this table names, and
// the table must say who holds the stack lock around it. A new writer that is not in
// the table fails the build, whether it calls the sink or only takes its value
// (`f := stackapply.Apply`).

// lockSinks are the package-qualified functions whose reference marks a stack
// mutation. orchestrate.NewSystemd is the only way cmd/villa obtains a Systemd, so a
// function that never references it cannot stop or restart a service.
var lockSinks = map[string]bool{
	"stackapply.Apply":       true,
	"stackapply.Restore":     true,
	"orchestrate.WriteUnits": true,
	"config.SaveVilla":       true,
	"orchestrate.NewSystemd": true,
}

// localSinks are cmd/villa's own functions whose reference marks a stack mutation the
// package-qualified sinks cannot see. applySearchBound puts a transient nft table into
// the rootless netns the whole stack shares, which is a mutation of the running stack
// though it stops no service. Its definition is not a reference to itself.
var localSinks = map[string]bool{
	"applySearchBound": true,
}

// lockRule says how one function that references a sink is covered.
type lockRule struct {
	// lockers are the verbs that run this function's effects and must each call
	// acquireStackLock themselves, before their first config read.
	lockers []string
	// direct marks a function every caller of which must be one of lockers.
	direct bool
	// readOnly marks a function that only obtains the systemd seam to read: any other
	// sink, and any Stop/Start/Restart-style method reference, fails the guard.
	readOnly bool
	// why is required when lockers is empty: who else holds the lock, or why the
	// function needs none.
	why string
}

// systemdMutators are the Systemd methods that change a service or the manager.
var systemdMutators = map[string]bool{
	"Start": true, "Stop": true, "Restart": true, "Enable": true, "Disable": true,
	"EnableLinger": true, "DisableLinger": true, "DaemonReload": true,
}

// lockRules is every function that references a lockSinks entry, and how the stack
// lock covers it. A function that only reads systemd (readOnly) needs no lock.
var lockRules = map[string]lockRule{
	// The swap transaction frame takes the lock itself, before the config is read
	// (ADR-0015, TestTransactHoldsTheStackLock); the guard requires every liveTxDeps
	// call to pass acquireStackLock and every dashboard override to be tryStackLock.
	"liveTxDeps": {why: "the frame locks: stackapply.Transact takes TxDeps.Lock first"},

	// The apply verbs take the lock in their cobra caller, before their first config read.
	"applyStack":          {lockers: []string{"runUp", "runRestart"}, direct: true},
	"applyResidentChange": {lockers: []string{"runResidentAdd", "runResidentRm"}, direct: true},
	"liveResidentDeps":    {lockers: []string{"runResidentAdd", "runResidentRm"}}, // runResidentLs only reads
	"liveRestoreDeps":     {lockers: []string{"runRestore"}},
	"liveInstallDeps":     {lockers: []string{"runInstall"}}, // install's own transaction (ADR-0003)

	// `villa update apply`: apply holds the lock across every per-subsystem
	// transaction, including the proofs it runs.
	"liveMutate":           {lockers: []string{"apply"}},
	"liveRestoreSubsystem": {lockers: []string{"apply"}},
	"liveUpdateFlowDeps":   {lockers: []string{"apply"}},
	// The agent proof stops villa-llama. runVerifyAgent locks around it; `update apply`
	// runs the same proof (update_proofs.go) already holding the lock, so the lock is
	// deliberately not inside liveAgentVerify: flock does not nest.
	"liveVerifyAgentDeps": {lockers: []string{"runVerifyAgent"}},

	// The config writers: a swap's rollback restores the whole captured config.toml,
	// so each holds the lock from its read to its write (#267).
	"liveConfigDeps":       {lockers: []string{"runConfigSet"}}, // config show only reads
	"liveWorkspaceCmdDeps": {lockers: []string{"runWorkspaceAdd", "runWorkspaceRemove"}},
	"saveRecommendation":   {lockers: []string{"saveRecommendation"}},

	// backup stops Open WebUI and Qdrant for a clean export (#267).
	"liveDeps": {lockers: []string{"runBackup"}},

	// liveStackDeps is only the binding of the stack-apply module to the host: every
	// caller that runs Apply or Restore is itself a sink above.
	"liveStackDeps": {why: "binding only: a caller that runs stackapply.Apply/Restore is registered above"},

	// Teardown verbs stop or remove the stack by intent. They capture nothing, so they
	// cannot revert another mutation's write, but a swap in flight when they run fails
	// its proof for a reason the swap did not cause, and its rollback restarts what it
	// restarted (#267). runDown and runUninstall hold the lock across their whole
	// window. `logs` shares liveLifecycleDeps but only reads the journal.
	"liveLifecycleDeps": {lockers: []string{"runUp", "runRestart", "runDown"}},
	"liveUninstallDeps": {lockers: []string{"runUninstall"}},

	// verify search applies a transient nft bound in the shared rootless netns and tears
	// it down on the way out. runVerifySearch locks around the proof; `update apply`
	// runs the same proof already holding the lock, so the lock is deliberately not
	// inside liveSearchVerify: flock does not nest.
	"liveSearchVerify": {lockers: []string{"runVerifySearch"}},

	// Read-only readers of systemd: the journal, or IsActive.
	"liveJournalView":   {readOnly: true},
	"liveMeasure":       {readOnly: true},
	"liveResidencyDeps": {readOnly: true},
	"liveStatusDeps":    {readOnly: true},
	"liveUpdateDeps":    {readOnly: true},
	// The image proof reads villa-image's invocation journal (#312); its callers
	// (install, doctor, update) hold or need no lock of their own here.
	"liveImageProof": {readOnly: true},
}

// guardFile is one non-test source file the guard reads.
type guardFile struct{ path, src string }

// isCmd reports whether the file is in cmd/villa, the only tier whose top-level
// functions are verbs a locker can name.
func (f guardFile) isCmd() bool { return strings.HasPrefix(f.path, "cmd/villa/") }

type guardFunc struct {
	file guardFile
	name string
	node ast.Node // the FuncDecl, or the GenDecl of a package-level var
}

// parseGuard parses every file and returns its top-level declarations. A package-level
// var or const is a guardFunc named "<package level>": a sink taken there has no
// caller to hold a lock, so it is never registrable.
func parseGuard(files []guardFile) ([]guardFunc, []string) {
	var decls []guardFunc
	var problems []string
	for _, gf := range files {
		f, err := parser.ParseFile(token.NewFileSet(), gf.path, gf.src, 0)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", gf.path, err))
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					decls = append(decls, guardFunc{gf, d.Name.Name, d})
				}
			case *ast.GenDecl:
				decls = append(decls, guardFunc{gf, "<package level>", d})
			}
		}
	}
	return decls, problems
}

// bodyIdents walks n and calls fn for every identifier that is a plain reference:
// a field or method name after a dot is not a reference to a top-level function.
func bodyIdents(n ast.Node, fn func(*ast.Ident)) {
	sels := map[*ast.Ident]bool{}
	ast.Inspect(n, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			sels[s.Sel] = true
		}
		return true
	})
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !sels[id] {
			fn(id)
		}
		return true
	})
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// callsAcquire reports whether the function body calls acquireStackLock().
func callsAcquire(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && isIdent(c.Fun, "acquireStackLock") {
			found = true
		}
		return !found
	})
	return found
}

// isNewSystemd reports whether e is a call of orchestrate.NewSystemd().
func isNewSystemd(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	return ok && isIdent(sel.X, "orchestrate") && sel.Sel.Name == "NewSystemd"
}

// systemdVars returns the names n binds to orchestrate.NewSystemd().
// ponytail: by name, so an alias (`s := sys`) is not followed; a type-aware check
// (go/types) if a read-only entry is ever laundered through one.
func systemdVars(n ast.Node) map[string]bool {
	vars := map[string]bool{}
	ast.Inspect(n, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			for i, rhs := range a.Rhs {
				if id, ok := a.Lhs[min(i, len(a.Lhs)-1)].(*ast.Ident); ok && isNewSystemd(rhs) {
					vars[id.Name] = true
				}
			}
		}
		return true
	})
	return vars
}

// isSystemd reports whether e is a Systemd value: NewSystemd() itself or a variable
// bound to it.
func isSystemd(e ast.Expr, vars map[string]bool) bool {
	if id, ok := e.(*ast.Ident); ok {
		return vars[id.Name]
	}
	return isNewSystemd(e)
}

// lockFindings returns every way files break the stack-lock rules against rules.
func lockFindings(files []guardFile, rules map[string]lockRule) []string {
	decls, out := parseGuard(files)
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }

	cmdFuncs := map[string]guardFunc{}
	for _, d := range decls {
		if _, isFn := d.node.(*ast.FuncDecl); isFn && d.file.isCmd() && d.node.(*ast.FuncDecl).Recv == nil {
			cmdFuncs[d.name] = d
		}
	}

	sinks := map[string][]string{} // enclosing function → sinks it references
	callers := map[string][]string{}
	for _, d := range decls {
		sysVars := systemdVars(d.node)
		ast.Inspect(d.node, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && lockSinks[id.Name+"."+x.Sel.Name] {
					sinks[d.name] = append(sinks[d.name], id.Name+"."+x.Sel.Name)
				}
				if rules[d.name].readOnly && systemdMutators[x.Sel.Name] && isSystemd(x.X, sysVars) {
					add("%s: %s is registered read-only but references .%s", d.file.path, d.name, x.Sel.Name)
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "Tx" &&
						sel.Sel.Name != "LoadConfig" && sel.Sel.Name != "Capture" {
						add("%s: %s calls .Tx.%s directly, bypassing the swap transaction frame and its lock", d.file.path, d.name, sel.Sel.Name)
					}
				}
				if isIdent(x.Fun, "liveTxDeps") && (len(x.Args) != 1 || !isIdent(x.Args[0], "acquireStackLock")) {
					add("%s: %s wires liveTxDeps with a lock that is not acquireStackLock", d.file.path, d.name)
				}
			case *ast.CompositeLit:
				if sel, ok := x.Type.(*ast.SelectorExpr); ok && isIdent(sel.X, "stackapply") && sel.Sel.Name == "TxDeps" && d.name != "liveTxDeps" {
					add("%s: %s builds a stackapply.TxDeps by hand; liveTxDeps is the one binding whose Lock is the real acquire", d.file.path, d.name)
				}
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Lock" || i >= len(x.Rhs) {
						continue
					}
					if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "Tx" &&
						!isIdent(x.Rhs[i], "tryStackLock") && !isIdent(x.Rhs[i], "acquireStackLock") {
						add("%s: %s assigns a Tx.Lock that is neither tryStackLock nor acquireStackLock", d.file.path, d.name)
					}
				}
			}
			return true
		})
		bodyIdents(d.node, func(id *ast.Ident) {
			if localSinks[id.Name] && id.Name != d.name {
				sinks[d.name] = append(sinks[d.name], id.Name)
			}
			if r, ok := rules[id.Name]; ok && r.direct && id.Name != d.name {
				callers[id.Name] = append(callers[id.Name], d.name)
			}
		})
	}

	for fn, used := range sinks {
		if fn == "<package level>" {
			add("a package-level declaration references %v: no verb can hold the lock around it", slices.Compact(slices.Sorted(slices.Values(used))))
			continue
		}
		if rules[fn].readOnly {
			for _, s := range used {
				if s != "orchestrate.NewSystemd" {
					add("%s is registered read-only but references %s", fn, s)
				}
			}
		}
		if _, ok := rules[fn]; !ok {
			add("%s references %v but is not in lockRules — run it through the swap transaction frame, or take acquireStackLock in its verb and register it with the verb named", fn, slices.Compact(slices.Sorted(slices.Values(used))))
		}
	}
	for fn, r := range rules {
		if len(sinks[fn]) == 0 {
			add("lockRules lists %s, which no longer references a lock sink — drop the entry", fn)
		}
		if len(r.lockers) == 0 && !r.readOnly && strings.TrimSpace(r.why) == "" {
			add("lockRules entry %s names no locker and gives no reason", fn)
		}
		for _, verb := range r.lockers {
			d, ok := cmdFuncs[verb]
			if !ok || !callsAcquire(d.node) {
				add("%s runs %s but does not call acquireStackLock", verb, fn)
			}
		}
		for _, c := range callers[fn] {
			if r.direct && !slices.Contains(r.lockers, c) {
				add("%s uses %s but is not one of its locked verbs %v", c, fn, r.lockers)
			}
		}
	}
	slices.Sort(out)
	return out
}

// nestingFindings returns every verb that takes the stack lock and reaches another
// function that takes it again. flock is per open file description, so a second
// Acquire in the same process blocks forever behind the first: the deadlock is
// silent. A function takes the lock when it names acquireStackLock, as a call or as
// the lock handed to liveTxDeps. The reach is by name over cmd/villa's top-level
// functions, so it over-approximates rather than misses.
func nestingFindings(files []guardFile) []string {
	decls, out := parseGuard(files)
	refs := map[string]map[string]bool{}
	takes := map[string]bool{}
	for _, d := range decls {
		if fd, ok := d.node.(*ast.FuncDecl); !ok || !d.file.isCmd() || fd.Recv != nil {
			continue
		}
		refs[d.name] = map[string]bool{}
		bodyIdents(d.node, func(id *ast.Ident) {
			if id.Name == "acquireStackLock" {
				takes[d.name] = true
			}
			refs[d.name][id.Name] = true
		})
	}
	for a := range takes {
		seen := map[string]bool{a: true}
		queue := []string{a}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for next := range refs[cur] {
				if _, isFn := refs[next]; !isFn || seen[next] {
					continue
				}
				seen[next] = true
				queue = append(queue, next)
				if takes[next] {
					out = append(out, fmt.Sprintf("%s takes the stack lock and reaches %s, which takes it again (flock does not nest)", a, next))
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// loadGuardFiles reads every non-test Go file of cmd/villa and internal/ that is not
// itself a sink's home (the stack-apply module, orchestrate and config define them).
func loadGuardFiles(t *testing.T) []guardFile {
	t.Helper()
	var files []guardFile
	for _, root := range []struct{ dir, label string }{{".", "cmd/villa"}, {"../../internal", "internal"}} {
		err := filepath.WalkDir(root.dir, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				switch filepath.Base(path) {
				case "stackapply", "orchestrate", "config", "testdata":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root.dir, path)
			files = append(files, guardFile{path: root.label + "/" + filepath.ToSlash(rel), src: string(src)})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root.dir, err)
		}
	}
	return files
}

// TestEveryStackMutationHoldsTheLock guards #250 and #267: a verb that mutates the
// stack without the stack lock (ADR-0010) fails the build, and so does a verb that
// takes the lock twice. See lockFindings and nestingFindings for the shapes caught.
func TestEveryStackMutationHoldsTheLock(t *testing.T) {
	files := loadGuardFiles(t)
	for _, f := range lockFindings(files, lockRules) {
		t.Error(f)
	}
	for _, f := range nestingFindings(files) {
		t.Error(f)
	}
}

// guardSrc wraps function bodies into one cmd/villa file for the self-tests.
func guardSrc(decls string) []guardFile {
	return []guardFile{{path: "cmd/villa/x.go", src: "package main\n" + decls}}
}

// TestLockGuardCatchesEveryShape self-tests the guard: each way a stack mutation can
// slip past the lock must produce a finding, and a covered writer must produce none.
// Without this the guard could go blind (as the AST-only version did for a function
// value such as `f := stackapply.Apply`) and stay green.
func TestLockGuardCatchesEveryShape(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		rules map[string]lockRule
		want  string // substring of a finding; "" means none expected
	}{
		{"a covered writer is clean",
			`func runV() { l, _ := acquireStackLock(); _ = l; w() }
			 func w() { stackapply.Apply(nil, nil) }`,
			map[string]lockRule{"w": {lockers: []string{"runV"}}}, ""},
		{"a call of stackapply.Apply outside any rule",
			`func w() { stackapply.Apply(nil, nil) }`, nil, "w references [stackapply.Apply]"},
		{"a function value of stackapply.Apply",
			`func w() { f := stackapply.Apply; f(nil, nil) }`, nil, "w references [stackapply.Apply]"},
		{"a function value of stackapply.Restore",
			`func w() { f := stackapply.Restore; f(nil, nil) }`, nil, "w references [stackapply.Restore]"},
		{"a direct orchestrate.WriteUnits",
			`func w() { orchestrate.WriteUnits(orchestrate.Plan{}, "") }`, nil, "w references [orchestrate.WriteUnits]"},
		{"a direct config.SaveVilla",
			`func w() { config.SaveVilla(config.VillaConfig{}) }`, nil, "w references [config.SaveVilla]"},
		{"a config.SaveVilla taken as a value",
			`func w() { d := deps{save: config.SaveVilla}; _ = d }`, nil, "w references [config.SaveVilla]"},
		{"a systemd stop",
			`func w() { sys := orchestrate.NewSystemd(); sys.Stop("x") }`, nil, "w references [orchestrate.NewSystemd]"},
		{"a call of the nft bound",
			`func w() { applySearchBound(nil, "") }`, nil, "w references [applySearchBound]"},
		{"the nft bound taken as a value",
			`func w() { f := applySearchBound; f(nil, "") }`, nil, "w references [applySearchBound]"},
		{"the definition of the nft bound is not a reference to it",
			`func applySearchBound() {}`, nil, ""},
		{"a package-level sink",
			`var save = config.SaveVilla`, nil, "package-level declaration"},
		{"a locker that does not take the lock",
			`func runV() { w() }
			 func w() { config.SaveVilla(config.VillaConfig{}) }`,
			map[string]lockRule{"w": {lockers: []string{"runV"}}}, "runV runs w but does not call acquireStackLock"},
		{"a locker that only names acquireStackLock",
			`func runV() { _ = acquireStackLock }
			 func w() { config.SaveVilla(config.VillaConfig{}) }`,
			map[string]lockRule{"w": {lockers: []string{"runV"}}}, "runV runs w but does not call acquireStackLock"},
		{"a stale rule",
			`func runV() { acquireStackLock() }`,
			map[string]lockRule{"gone": {lockers: []string{"runV"}}}, "gone, which no longer references a lock sink"},
		{"an allowlist entry with no locker and no reason",
			`func w() { config.SaveVilla(config.VillaConfig{}) }`,
			map[string]lockRule{"w": {}}, "names no locker and gives no reason"},
		{"a direct writer called from an unlocked verb",
			`func runV() { acquireStackLock(); w() }
			 func runOther() { w() }
			 func w() { stackapply.Apply(nil, nil) }`,
			map[string]lockRule{"w": {lockers: []string{"runV"}, direct: true}}, "runOther uses w but is not one of its locked verbs"},
		{"a direct writer taken as a value by an unlocked verb",
			`func runV() { acquireStackLock(); w() }
			 func runOther() { f := w; f() }
			 func w() { stackapply.Apply(nil, nil) }`,
			map[string]lockRule{"w": {lockers: []string{"runV"}, direct: true}}, "runOther uses w but is not one of its locked verbs"},
		{"liveTxDeps wired with a no-op lock",
			`func liveSwap() { _ = liveTxDeps(func() (*stacklock.Lock, error) { return nil, nil }) }`, nil, "liveSwap wires liveTxDeps with a lock that is not acquireStackLock"},
		{"liveTxDeps wired with nil",
			`func liveSwap() { _ = liveTxDeps(nil) }`, nil, "liveSwap wires liveTxDeps with a lock that is not acquireStackLock"},
		{"liveTxDeps wired with the real acquire is clean",
			`func liveSwap() { _ = liveTxDeps(acquireStackLock) }`, nil, ""},
		{"a hand-built TxDeps",
			`func liveSwap() { _ = stackapply.TxDeps{Lock: nil} }`, nil, "liveSwap builds a stackapply.TxDeps by hand"},
		{"a Tx.Lock overridden with a no-op",
			`func handler() { d.Tx.Lock = noLock }`, nil, "handler assigns a Tx.Lock that is neither"},
		{"a Tx.Lock overridden with tryStackLock is clean",
			`func handler() { d.Tx.Lock = tryStackLock }`, nil, ""},
		{"a call that bypasses the frame",
			`func handler() { d.Tx.Apply(cfg) }`, nil, "handler calls .Tx.Apply directly"},
		{"a read-only entry that stops a service",
			`func liveJournal() { sys := orchestrate.NewSystemd(); sys.Stop("x") }`,
			map[string]lockRule{"liveJournal": {readOnly: true}}, "liveJournal is registered read-only but references .Stop"},
		{"a read-only entry that stops a service through the constructor",
			`func liveJournal() { orchestrate.NewSystemd().Restart("x") }`,
			map[string]lockRule{"liveJournal": {readOnly: true}}, "liveJournal is registered read-only but references .Restart"},
		{"a read-only entry that writes config",
			`func liveJournal() { orchestrate.NewSystemd(); config.SaveVilla(config.VillaConfig{}) }`,
			map[string]lockRule{"liveJournal": {readOnly: true}}, "liveJournal is registered read-only but references config.SaveVilla"},
		{"a read-only entry that reads is clean",
			`func liveJournal() { t := time.NewTicker(1); defer t.Stop(); orchestrate.NewSystemd().IsActive("x") }`,
			map[string]lockRule{"liveJournal": {readOnly: true}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lockFindings(guardSrc(tc.src), tc.rules)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("want no findings, got %v", got)
				}
				return
			}
			if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, tc.want) }) {
				t.Fatalf("want a finding containing %q, got %v", tc.want, got)
			}
		})
	}
}

// TestLockGuardCatchesNestedAcquires self-tests the nesting check: flock is per open
// file description, so a verb that holds the lock and reaches a second Acquire in the
// same process blocks forever behind itself.
func TestLockGuardCatchesNestedAcquires(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"one acquire is clean",
			`func runA() { acquireStackLock(); helper() }
			 func helper() {}`, ""},
		{"an acquire that calls another acquiring verb",
			`func runA() { acquireStackLock(); runB() }
			 func runB() { acquireStackLock() }`, "runA takes the stack lock and reaches runB"},
		{"an acquire that reaches another through a helper",
			`func runA() { acquireStackLock(); mid() }
			 func mid() { runB() }
			 func runB() { acquireStackLock() }`, "runA takes the stack lock and reaches runB"},
		{"an acquire that reaches a frame-locked swap",
			`func runA() { acquireStackLock(); _ = liveSwap() }
			 func liveSwap() { _ = liveTxDeps(acquireStackLock) }`, "runA takes the stack lock and reaches liveSwap"},
		{"a field with the same name is not a reference",
			`func runA() { acquireStackLock(); x.runB() }
			 func runB() { acquireStackLock() }`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nestingFindings(guardSrc(tc.src))
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("want no findings, got %v", got)
				}
				return
			}
			if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, tc.want) }) {
				t.Fatalf("want a finding containing %q, got %v", tc.want, got)
			}
		})
	}
}
