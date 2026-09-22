package modelswap

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

// modelswap_test.go holds the swap-ordering asserts relocated from cmd/villa
// model_test.go — the security contract (STATE.md [03-05]): resolve(catalog) →
// fit-guard → auto-pull → CAPTURE before mutation (#237) → SaveVilla BEFORE
// reconcileAndWrite → restart ONLY the inference service, skipping restart+prove
// on a no-op, PROVE the cutover before calling it Switched, rolling back verbatim
// on any mutate error or non-pass verdict. Run is driven through stubbed Deps with
// no live host; the typed Result is asserted directly.

const installService = "villa-llama.service"

// priorUnitBytes is the verbatim prior unit the fake CaptureUnit returns, so the
// rollback tests can assert a byte-equal RestoreUnit.
var priorUnitBytes = []byte("[Container]\nImage=prior\nExec=llama-server --prior\n")

// swapRecorder records each side-effecting seam call so the test can assert ordering
// (SaveVilla BEFORE Restart) and that ONLY the inference unit is restarted.
type swapRecorder struct {
	callOrder         []string
	saved             config.VillaConfig
	pulled            []string
	restarted         []string
	restored          []byte
	downloaded        map[string]bool // models considered already-on-disk
	fitOverrides      map[string]bool // model id -> Fits result
	reconcileNoChange bool            // make reconcileAndWrite report "nothing changed"

	captureErr   error  // CaptureUnit error (uncapturable prior unit)
	writeErr     error  // ReconcileAndWrite error (mutate failure)
	restartErr   error  // first Restart error (mutate failure)
	restoreErr   error  // RestoreUnit error during rollback (rollback-incomplete)
	rbRestartErr error  // Restart error during rollback (rollback-incomplete)
	proveStatus  string // Prove verdict Status (prove.StatusPass = pass, default)
	proveDetail  string // Prove verdict Detail

	restartCalls int // counts Restart invocations to distinguish forward vs rollback
}

func newSwapStub(rec *swapRecorder) Deps {
	return Deps{
		InstallServiceName: installService,
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "current-model", Backend: "vulkan"}, nil
		},
		ResolveCatalog: func(name string) (catalog.Model, bool) {
			known := map[string]catalog.Model{
				"fits-model":    {ID: "fits-model", Quant: "Q4", DefaultCtx: 4096},
				"fits-undl":     {ID: "fits-undl", Quant: "Q4", DefaultCtx: 4096},
				"toobig-model":  {ID: "toobig-model", Quant: "Q4", DefaultCtx: 4096},
				"current-model": {ID: "current-model", Quant: "Q4", DefaultCtx: 4096},
			}
			m, ok := known[name]
			return m, ok
		},
		Fits: func(m catalog.Model) (bool, string) {
			if rec.fitOverrides != nil {
				if ok := rec.fitOverrides[m.ID]; !ok {
					return false, "won't fit envelope (test)"
				}
			}
			return true, ""
		},
		IsDownloaded: func(m catalog.Model) bool {
			return rec.downloaded[m.ID]
		},
		Pull: func(m catalog.Model) error {
			rec.callOrder = append(rec.callOrder, "pull:"+m.ID)
			rec.pulled = append(rec.pulled, m.ID)
			return nil
		},
		CaptureUnit: func() ([]byte, error) {
			if rec.captureErr != nil {
				return nil, rec.captureErr
			}
			rec.callOrder = append(rec.callOrder, "capture")
			return append([]byte(nil), priorUnitBytes...), nil
		},
		SaveConfig: func(c config.VillaConfig) error {
			rec.callOrder = append(rec.callOrder, "save:"+c.Model)
			rec.saved = c
			return nil
		},
		ReconcileAndWrite: func(_ config.VillaConfig) (bool, error) {
			rec.callOrder = append(rec.callOrder, "write")
			if rec.writeErr != nil {
				return false, rec.writeErr
			}
			return !rec.reconcileNoChange, nil
		},
		RestoreUnit: func(b []byte) error {
			rec.callOrder = append(rec.callOrder, "restore")
			rec.restored = append([]byte(nil), b...)
			return rec.restoreErr
		},
		DaemonReload: func() error {
			rec.callOrder = append(rec.callOrder, "daemon-reload")
			return nil
		},
		Restart: func(service string) error {
			rec.callOrder = append(rec.callOrder, "restart:"+service)
			rec.restarted = append(rec.restarted, service)
			rec.restartCalls++
			// First restart is the forward cutover; a later one is the rollback re-ready.
			if rec.restartCalls == 1 {
				return rec.restartErr
			}
			return rec.rbRestartErr
		},
		Prove: func(context.Context) prove.Verdict {
			rec.callOrder = append(rec.callOrder, "prove")
			status := rec.proveStatus
			if status == "" {
				status = prove.StatusPass
			}
			return prove.Verdict{Status: status, Detail: rec.proveDetail}
		},
	}
}

// TestSwapResolvesThroughCatalog: an unknown id is a typed refuse with zero side
// effects (never interpreted as a path).
func TestSwapResolvesThroughCatalog(t *testing.T) {
	rec := &swapRecorder{downloaded: map[string]bool{}}
	res := Run(newSwapStub(rec), "no-such-model")
	if !res.Refused || !res.Unknown {
		t.Fatalf("unknown id must be a Refused/Unknown result, got %+v", res)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("unknown swap must fire zero seams, got %v", rec.callOrder)
	}
}

// TestSwapFitGuardFirst: a non-fitting model is refused BEFORE any pull/save/restart
// (fit-guard first).
func TestSwapFitGuardFirst(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"toobig-model": true},
		fitOverrides: map[string]bool{}, // nothing fits
	}
	res := Run(newSwapStub(rec), "toobig-model")
	if !res.Refused {
		t.Fatalf("non-fitting target must be Refused, got %+v", res)
	}
	if res.Unknown {
		t.Errorf("a known-but-too-big model must not be flagged Unknown")
	}
	if len(rec.saved.Model) != 0 || len(rec.callOrder) != 0 {
		t.Errorf("non-fitting swap must not pull/save/write/restart, got calls %v", rec.callOrder)
	}
	if !strings.Contains(res.Reason, "fit") {
		t.Errorf("refusal reason should mention fit, got %q", res.Reason)
	}
}

// TestSwapSaveBeforeReconcileAndInferenceOnlyRestart: a fitting absent model →
// pull → SaveVilla called BEFORE reconcileAndWrite → restart targets ONLY the
// inference service (the ordering contract).
//
// Phase-23 (CTRL-05): the restarted slice records EVERY Restart call;
// asserting len==1 with InstallServiceName pins the restart SCOPE of a chat swap
// the memory services (villa-qdrant / villa-embed) and Open WebUI are never
// restarted. The Deps surface itself is the other half of the guarantee: Restart
// is the ONLY service mutator on the struct (no Stop/Start/Reload field exists
// a compile-time truth pinned by TestSwapDepsSurfaceRestartIsOnlyServiceMutator
// below). Because the dashboard's POST /api/models/switch handler (handleSwitch)
// calls this same modelswap.Run verbatim, the scope is permanent for the CLI AND
// the dashboard.
func TestSwapSaveBeforeReconcileAndInferenceOnlyRestart(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{}, // not on disk → auto-pull
		fitOverrides: map[string]bool{"fits-undl": true},
	}
	res := Run(newSwapStub(rec), "fits-undl")
	if !res.Switched {
		t.Fatalf("fitting auto-pull swap must Switch, got %+v", res)
	}
	if !res.Pulled || len(rec.pulled) != 1 || rec.pulled[0] != "fits-undl" {
		t.Errorf("expected auto-pull of fits-undl, got pulled=%v Result.Pulled=%v", rec.pulled, res.Pulled)
	}
	if rec.saved.Model != "fits-undl" {
		t.Errorf("config not persisted to the new model, got %q", rec.saved.Model)
	}

	// pull < capture < save < write < restart < prove; capture precedes save
	// (Pitfall 4, #237) and SaveVilla precedes the reconcileAndWrite "write".
	pullIdx, captureIdx, saveIdx, writeIdx, restartIdx, proveIdx := -1, -1, -1, -1, -1, -1
	for i, c := range rec.callOrder {
		switch {
		case strings.HasPrefix(c, "pull:"):
			pullIdx = i
		case c == "capture":
			captureIdx = i
		case strings.HasPrefix(c, "save:"):
			saveIdx = i
		case c == "write":
			writeIdx = i
		case strings.HasPrefix(c, "restart:") && restartIdx == -1:
			restartIdx = i
		case c == "prove":
			proveIdx = i
		}
	}
	swapInOrder := pullIdx < captureIdx && captureIdx < saveIdx && saveIdx < writeIdx && writeIdx < restartIdx && restartIdx < proveIdx
	if !swapInOrder {
		t.Errorf("expected pull<capture<save<write<restart<prove, got %v", rec.callOrder)
	}
	// ONLY the inference unit is restarted (network/volume untouched).
	if len(rec.restarted) != 1 || rec.restarted[0] != installService {
		t.Errorf("expected only %s restarted, got %v", installService, rec.restarted)
	}
}

// TestSwapDepsSurfaceRestartIsOnlyServiceMutator (CTRL-05): the Deps struct
// is the injection surface for EVERY host-touching action the swap can perform, so
// pinning its exact field set makes the restart scope structural: Restart is the
// only service mutator (there is no Stop/Start/Reload/Down field a future change
// could quietly call for the memory stack). Adding ANY new field to Deps breaks
// this test, forcing a conscious review. Because the dashboard handleSwitch
// drives the same Run/Deps, this pin covers both the CLI and the dashboard.
func TestSwapDepsSurfaceRestartIsOnlyServiceMutator(t *testing.T) {
	want := map[string]bool{
		"LoadConfig":         true,
		"ResolveCatalog":     true,
		"Fits":               true,
		"IsDownloaded":       true,
		"Pull":               true,
		"CaptureUnit":        true,
		"SaveConfig":         true,
		"ReconcileAndWrite":  true,
		"RestoreUnit":        true,
		"DaemonReload":       true,
		"Restart":            true,
		"Prove":              true,
		"InstallServiceName": true,
	}
	tp := reflect.TypeOf(Deps{})
	if tp.NumField() != len(want) {
		t.Fatalf("Deps has %d fields, want %d — a new seam was added; review it against D-09 (chat swap must never mutate the memory stack) before extending this pin", tp.NumField(), len(want))
	}
	for i := range tp.NumField() {
		name := tp.Field(i).Name
		if !want[name] {
			t.Errorf("unexpected Deps field %q — review against D-09 before extending this pin", name)
		}
	}
}

// TestSwapAlreadyDownloadedSkipsPull: a fitting already-present model swaps without a
// re-pull (save before restart, inference-only).
func TestSwapAlreadyDownloadedSkipsPull(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"fits-model": true},
		fitOverrides: map[string]bool{"fits-model": true},
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.Switched {
		t.Fatalf("fitting swap should Switch, got %+v", res)
	}
	if res.Pulled || len(rec.pulled) != 0 {
		t.Errorf("already-downloaded model must not be re-pulled, got %v", rec.pulled)
	}
	saveIdx, restartIdx := -1, -1
	for i, c := range rec.callOrder {
		if strings.HasPrefix(c, "save:") {
			saveIdx = i
		}
		if strings.HasPrefix(c, "restart:") && restartIdx == -1 {
			restartIdx = i
		}
	}
	if saveIdx == -1 || restartIdx == -1 || saveIdx > restartIdx {
		t.Errorf("expected save BEFORE restart, call order: %v", rec.callOrder)
	}
}

// TestSwapNoOpSkipsRestart: a no-op (units already up to date) persists config but
// skips the restart.
func TestSwapNoOpSkipsRestart(t *testing.T) {
	rec := &swapRecorder{
		downloaded:        map[string]bool{"fits-model": true},
		fitOverrides:      map[string]bool{"fits-model": true},
		reconcileNoChange: true,
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.NoOp || res.Switched {
		t.Fatalf("no-op swap must report NoOp (not Switched), got %+v", res)
	}
	if rec.saved.Model != "fits-model" {
		t.Errorf("config must still be persisted on a no-op, got %q", rec.saved.Model)
	}
	if len(rec.restarted) != 0 {
		t.Errorf("a no-op reconcile must NOT restart the service, got %v", rec.restarted)
	}
}

// TestSwapPullFailureIsErrNotRefuse: a pull failure is an Err (FailedStep=pull), not a
// policy refusal — the cmd layer maps both to exit 1 but the distinction matters for
// the dashboard handler.
func TestSwapPullFailureIsErrNotRefuse(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{},
		fitOverrides: map[string]bool{"fits-undl": true},
	}
	d := newSwapStub(rec)
	d.Pull = func(catalog.Model) error { return errors.New("checksum mismatch") }
	res := Run(d, "fits-undl")
	if res.Refused {
		t.Errorf("a pull failure is an Err, not a Refused")
	}
	if res.Err == nil || res.FailedStep != "pull" {
		t.Errorf("expected Err at step pull, got %+v", res)
	}
	if len(rec.restarted) != 0 || rec.saved.Model != "" {
		t.Errorf("a pull failure must short-circuit before save/restart, got calls %v", rec.callOrder)
	}
}

// TestSwapCaptureFailureRefuses is #237: an uncapturable prior unit must refuse
// with ZERO side effects (no save/write/restart) rather than mutate a stack it
// cannot restore.
func TestSwapCaptureFailureRefuses(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"fits-model": true},
		fitOverrides: map[string]bool{"fits-model": true},
		captureErr:   errors.New("unit file unreadable"),
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.Refused || res.FailedStep != "capture" {
		t.Fatalf("capture failure must Refuse at step capture, got %+v", res)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("capture failure must fire no save/write/restart, got %v", rec.callOrder)
	}
}

// TestSwapMutateFailureRollsBackVerbatim is #237: a failure AFTER the capture (here
// ReconcileAndWrite) restores the verbatim captured prior unit and config, so the
// config is never left out of step with the running unit.
func TestSwapMutateFailureRollsBackVerbatim(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"fits-model": true},
		fitOverrides: map[string]bool{"fits-model": true},
		writeErr:     errors.New("render failed"),
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.RolledBack || res.Switched {
		t.Fatalf("mutate failure must roll back (not switch), got %+v", res)
	}
	if res.FailedStep != "regenerate units" {
		t.Errorf("FailedStep = %q, want regenerate units", res.FailedStep)
	}
	if !bytes.Equal(rec.restored, priorUnitBytes) {
		t.Errorf("rollback must RestoreUnit byte-equal to the captured prior unit; got %q want %q", rec.restored, priorUnitBytes)
	}
	if rec.saved.Model != "current-model" {
		t.Errorf("rollback must SaveConfig(priorCfg) restoring the prior model, got %q", rec.saved.Model)
	}
	if rec.restarted[len(rec.restarted)-1] != installService {
		t.Errorf("rollback restart must target %s, got %v", installService, rec.restarted)
	}
}

// TestSwapProveFailureRollsBack is #237's headline fix: "a switch is reported
// without a proof" — a non-pass Prove verdict must roll back rather than report
// Switched, exactly like the other swap cores.
func TestSwapProveFailureRollsBack(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"fits-model": true},
		fitOverrides: map[string]bool{"fits-model": true},
		proveStatus:  prove.StatusFail,
		proveDetail:  "residency FAIL",
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.RolledBack || res.Switched {
		t.Fatalf("non-pass prove must roll back (not switch), got %+v", res)
	}
	if res.FailedStep != "prove" {
		t.Errorf("FailedStep = %q, want prove", res.FailedStep)
	}
	if res.Reason != "residency FAIL" {
		t.Errorf("Reason = %q, want the prove verdict's detail", res.Reason)
	}
	if !bytes.Equal(rec.restored, priorUnitBytes) {
		t.Errorf("rollback must RestoreUnit byte-equal to the captured prior unit")
	}
}

// TestSwapNoOpSkipsProve: a no-op (units already up to date) needs no cutover
// proof — nothing was cut over.
func TestSwapNoOpSkipsProve(t *testing.T) {
	rec := &swapRecorder{
		downloaded:        map[string]bool{"fits-model": true},
		fitOverrides:      map[string]bool{"fits-model": true},
		reconcileNoChange: true,
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.NoOp {
		t.Fatalf("expected NoOp, got %+v", res)
	}
	if strings.Contains(strings.Join(rec.callOrder, ","), "prove") {
		t.Errorf("a no-op must not invoke Prove, got %v", rec.callOrder)
	}
}

// TestSwapRollbackIncompleteReported is #237, mirroring Pitfall 5: a rollback step
// that itself fails must never be presented as a clean restoration.
func TestSwapRollbackIncompleteReported(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"fits-model": true},
		fitOverrides: map[string]bool{"fits-model": true},
		proveStatus:  prove.StatusFail,
		rbRestartErr: errors.New("systemd refused restart"),
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.RolledBack {
		t.Fatalf("expected RolledBack=true even on incomplete rollback, got %+v", res)
	}
	if !strings.Contains(res.Reason, "did not fully complete") {
		t.Errorf("an incomplete rollback must be flagged honestly in Reason, got %q", res.Reason)
	}
}
