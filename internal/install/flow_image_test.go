package install

import (
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// flow_image_test.go drives Run through the image-generation opt-in (#312): the
// pre-stage of the three weight files, the start gated on the rendered unit, the
// offload proof, and the reservations the chat fit is sized against.

// imageUnits is a realistic image-on plan: Render appends villa-image.container
// when the gate is on, and the start guard demands it be in the plan.
func imageUnits() ([]orchestrate.Unit, orchestrate.Plan) {
	units := []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "[Container]\n"},
		{Name: orchestrate.ImageContainerUnitName(), Text: "[Container]\n"},
	}
	return units, orchestrate.Plan{Changed: units}
}

var imageServiceName = DefaultUnits().Image

// TestInstallImageWiring: image on pulls the weights only when absent, starts
// villa-image after the chat UI, proves offload once, re-proves the chat model
// beside the eager-loaded image unit, and folds a PASS into success; a FAIL of
// either proof refuses and rolls back; an absent unit fails closed; image off
// touches none of it.
func TestInstallImageWiring(t *testing.T) {
	t.Run("image on with weights present: start after the chat UI, prove once", func(t *testing.T) {
		units, plan := imageUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.imageEnabled = true

		code, out, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("image-on install exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.imageEnsureCalls != 0 {
			t.Errorf("present weights must not be re-pulled, EnsureImageModel calls = %d", f.imageEnsureCalls)
		}
		if f.imageProofCalls != 1 {
			t.Errorf("image-on must run the offload proof once, proof calls = %d", f.imageProofCalls)
		}
		if f.chatProofCalls != 1 {
			t.Errorf("image-on must re-prove the chat model once after the image proof, chat proof calls = %d", f.chatProofCalls)
		}
		imgProof := slices.Index(f.callOrder, "imageProof")
		chatProof := slices.Index(f.callOrder, "chatProof")
		if imgProof < 0 || chatProof < 0 || chatProof < imgProof {
			t.Errorf("the chat re-proof must follow the image proof; callOrder = %v", f.callOrder)
		}
		chatIdx := slices.Index(f.startOrder, DefaultUnits().ChatUI)
		imgIdx := slices.Index(f.startOrder, imageServiceName)
		if imgIdx < 0 || chatIdx < 0 || imgIdx < chatIdx {
			t.Errorf("villa-image must start after the chat UI, startOrder = %v", f.startOrder)
		}
		if !strings.Contains(out.String(), "image generation ready") {
			t.Errorf("a passing proof must be narrated; stdout = %q", out.String())
		}
	})

	t.Run("image on with weights absent: pull before any start", func(t *testing.T) {
		units, plan := imageUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.imageEnabled = true
		f.imagePresent = false

		code, out, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.imageEnsureCalls != 1 {
			t.Fatalf("absent weights must be pulled once, EnsureImageModel calls = %d", f.imageEnsureCalls)
		}
		pull := slices.Index(f.callOrder, "ensureImageModel")
		firstStart := slices.IndexFunc(f.callOrder, func(c string) bool { return strings.HasPrefix(c, "start:") })
		if pull < 0 || firstStart < 0 || pull > firstStart {
			t.Errorf("the pull must precede every start; callOrder = %v", f.callOrder)
		}
		if !strings.Contains(out.String(), "z-image-turbo") || !strings.Contains(out.String(), "3 files") {
			t.Errorf("the pull narration must name the entry and the file count; stdout = %q", out.String())
		}
	})

	t.Run("image on: proof FAIL refuses with exitBlocked", func(t *testing.T) {
		units, plan := imageUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.imageEnabled = true
		f.imageProofStatus = preflight.StatusFail
		f.imageProofDetail = "2375.91 MiB of params in system RAM"

		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("an image proof FAIL must return exitBlocked, got %d; stderr = %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "image generation not ready") || !strings.Contains(errOut.String(), "system RAM") {
			t.Errorf("the FAIL must refuse naming image generation and carry the detail; stderr = %q", errOut.String())
		}
		if !slices.Contains(f.stopOrder, imageServiceName) {
			t.Errorf("the rollback must stop the service this run started, stopOrder = %v", f.stopOrder)
		}
	})

	t.Run("image on: chat model FAIL after a passing image proof refuses and rolls back", func(t *testing.T) {
		units, plan := imageUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.imageEnabled = true
		f.chatProofStatus = preflight.StatusFail
		f.chatProofDetail = "offloaded only 20/48 layers — 28 layers stayed on the CPU (partial offload)"

		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("a chat proof FAIL beside the image unit must return exitBlocked, got %d; stderr = %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "chat model") || !strings.Contains(errOut.String(), "partial offload") {
			t.Errorf("the FAIL must refuse naming the chat model and carry the detail; stderr = %q", errOut.String())
		}
		if f.imageProofCalls != 1 {
			t.Errorf("the image proof must have passed first, proof calls = %d", f.imageProofCalls)
		}
		if !slices.Contains(f.stopOrder, imageServiceName) {
			t.Errorf("the rollback must stop the image service this run started, stopOrder = %v", f.stopOrder)
		}
	})

	t.Run("image on but unit absent from plan: fails closed", func(t *testing.T) {
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		f := newFakeDeps(t, units, orchestrate.Plan{Changed: units}, passChecks())
		f.imageEnabled = true

		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("a missing image unit must fail closed (exitBlocked), got %d; stderr = %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "INTERNAL ERROR") {
			t.Errorf("a missing image unit must surface an INTERNAL-ERROR remediation; stderr = %q", errOut.String())
		}
		if slices.Contains(f.startOrder, imageServiceName) || f.imageProofCalls != 0 {
			t.Errorf("a unit absent from the plan must be neither started nor proven; startOrder = %v, proofs = %d", f.startOrder, f.imageProofCalls)
		}
	})

	t.Run("image on with an unknown image_model: blocks before any mutation", func(t *testing.T) {
		units, plan := imageUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.imageEnabled = true
		f.imageModel = "no-such-image"

		code, _, errOut := f.run(Opts{})
		if code != exitBlocked {
			t.Fatalf("exit = %d, want exitBlocked; stderr = %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "no-such-image") || !strings.Contains(errOut.String(), "z-image-turbo") {
			t.Errorf("the block must name the unknown id and the default; stderr = %q", errOut.String())
		}
		if f.saveCalls != 0 || f.startCalls != 0 {
			t.Errorf("an unknown id must block before the first mutation: saves = %d, starts = %d", f.saveCalls, f.startCalls)
		}
	})

	t.Run("image off: no presence check, no pull, no start, no proof", func(t *testing.T) {
		units, plan := imageUnits()
		f := newFakeDeps(t, units, plan, passChecks())

		code, _, errOut := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
		}
		if f.imagePresentCalls != 0 || f.imageEnsureCalls != 0 || f.imageProofCalls != 0 || f.chatProofCalls != 0 || slices.Contains(f.startOrder, imageServiceName) {
			t.Errorf("image off touched the image path: present=%d ensure=%d proof=%d chatProof=%d starts=%v", f.imagePresentCalls, f.imageEnsureCalls, f.imageProofCalls, f.chatProofCalls, f.startOrder)
		}
	})
}

// TestImageInstallPicksAgainstItsOwnRow: a first `--image` install on an image-off
// config reserves the image row before the chat fit, through PlannedReservations,
// so the done-criterion "the chat model still fits" is sized against the footprint
// the run is about to persist rather than a config that does not yet carry it.
func TestImageInstallPicksAgainstItsOwnRow(t *testing.T) {
	names := func(res []recommend.Reservation) []string {
		var got []string
		for _, r := range res {
			got = append(got, r.Name)
		}
		return got
	}
	if got := names(PlannedReservations(config.VillaConfig{}, Opts{Image: true})); !slices.Equal(got, []string{"image"}) {
		t.Errorf("PlannedReservations(--image) = %v, want [image]", got)
	}
	if got := names(PlannedReservations(config.VillaConfig{}, Opts{WebSearch: true, Image: true})); !slices.Equal(got, []string{"web_search", "image"}) {
		t.Errorf("PlannedReservations(--web-search --image) = %v, want [web_search image]", got)
	}

	units, plan := imageUnits()
	f := newFakeDeps(t, units, plan, passChecks())
	if _, _, errOut := f.run(Opts{Image: true}); f.pickReservations == nil {
		t.Fatalf("Pick was never handed reservations; stderr = %s", errOut.String())
	}
	if got := names(f.pickReservations); !slices.Equal(got, []string{"image"}) {
		t.Errorf("the flow handed Pick %v, want [image]", got)
	}
}
