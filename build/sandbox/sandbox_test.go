// Package sandbox_test proves the villa-shipped office scripts (villa-recalc,
// villa-render, villa-readback; spec v1.11 §7) inside the built sandbox image,
// invoked by name, under the same runtime and network posture a real workspace
// task gets: krun, no egress. It skips off-hardware so CI (the #175 build-only
// job) stays green; this is the on-hardware proof ticket #183's Check names.
package sandbox_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const image = "localhost/villa-sandbox:office"

// requireKrun skips the test unless podman, the sandbox image, and the krun
// runtime are all present, so a laptop or CI box with none of these still
// passes `go test ./...` instead of failing on missing infrastructure.
func requireKrun(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not on PATH")
	}
	if err := exec.Command("podman", "image", "exists", image).Run(); err != nil {
		t.Skipf("%s not built", image)
	}
	if err := exec.Command("podman", "run", "--rm", "--runtime=krun", "--network", "none", image, "true").Run(); err != nil {
		t.Skip("krun runtime not available for the sandbox image")
	}
}

// runScript invokes one of the three villa-shipped scripts by name inside the
// sandbox image, under krun with no network, exactly as a workspace task would
// (RenderSandboxRun's --network villa-sandbox is an egress-capable network for
// reaching llama-server; --network none here proves the scripts need none of
// that, per the ticket's Check).
func runScript(t *testing.T, dir, script string, args ...string) string {
	t.Helper()
	full := append([]string{"run", "--rm", "--runtime=krun", "--network", "none",
		"--volume", dir + ":/work:Z", image, script}, args...)
	cmd := exec.Command("podman", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("podman run %s %v: %v\n%s", script, args, err, out)
	}
	return string(out)
}

// lastLine returns the final non-empty line of script output: the contract
// every one of the three scripts follows so the model can chain them.
func lastLine(t *testing.T, output string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	t.Fatalf("no output line found in:\n%s", output)
	return ""
}

// TestOfficeScripts runs recalc -> render -> readback in sequence against the
// prototype-shaped reconciliation.xlsx fixture (ticket #169; testdata/ mirrors
// docs/prototype/cowork/seed.sh's invoice and bank CSVs, the same data the
// prototype's SUMIF matching failed on), and checks each script's proof of work.
func TestOfficeScripts(t *testing.T) {
	requireKrun(t)

	dir := t.TempDir()
	src, err := os.ReadFile("testdata/reconciliation.xlsx")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	input := filepath.Join(dir, "reconciliation.xlsx")
	if err := os.WriteFile(input, src, 0o644); err != nil {
		t.Fatalf("write fixture into workdir: %v", err)
	}

	// recalc: output exists, is a distinct file from the input, and differs
	// (openpyxl wrote no cached values; LibreOffice's recalc pass adds them,
	// which changes the file's size even though the input is untouched).
	recalcOut := lastLine(t, runScript(t, dir, "villa-recalc", "/work/reconciliation.xlsx"))
	wantRecalc := "/work/reconciliation.recalculated.xlsx"
	if recalcOut != wantRecalc {
		t.Fatalf("villa-recalc last line = %q, want %q", recalcOut, wantRecalc)
	}
	recalcHostPath := filepath.Join(dir, "reconciliation.recalculated.xlsx")
	recalcInfo, err := os.Stat(recalcHostPath)
	if err != nil {
		t.Fatalf("recalc output missing: %v", err)
	}
	inputInfo, err := os.Stat(input)
	if err != nil {
		t.Fatalf("input missing after recalc: %v", err)
	}
	if recalcInfo.Size() == inputInfo.Size() {
		t.Errorf("recalc output is the same size as the input (%d bytes); expected cached values to change it", inputInfo.Size())
	}

	// render: at least one page image, and each file is non-empty.
	renderOut := lastLine(t, runScript(t, dir, "villa-render", "/work/reconciliation.recalculated.xlsx"))
	imagePaths := strings.Fields(renderOut)
	if len(imagePaths) == 0 {
		t.Fatalf("villa-render produced no image paths: %q", renderOut)
	}
	for _, p := range imagePaths {
		hostPath := filepath.Join(dir, filepath.Base(p))
		info, err := os.Stat(hostPath)
		if err != nil {
			t.Fatalf("render image missing: %v", err)
		}
		if info.Size() == 0 {
			t.Errorf("render image %s is empty", hostPath)
		}
	}

	// readback: the recalculated Invoices!F2 cell (=D2-E2, a formula) must read
	// back as a number, proving the recalc pass actually populated cached values.
	readbackOut := runScript(t, dir, "villa-readback", "/work/reconciliation.recalculated.xlsx")
	dumpLines := strings.Split(strings.TrimRight(readbackOut, "\n"), "\n")
	formulaCell := regexp.MustCompile(`^Invoices\tF2\t(-?[0-9.]+)$`)
	found := false
	for _, line := range dumpLines {
		m := formulaCell.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		if _, err := strconv.ParseFloat(m[1], 64); err != nil {
			t.Fatalf("readback F2 value %q is not numeric", m[1])
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("readback dump has no numeric Invoices!F2 line:\n%s", readbackOut)
	}
}

// TestOfficeScriptsUsage: each script refuses bad args rather than doing
// nothing quietly (spec's "prints usage on bad args, exits non-zero").
func TestOfficeScriptsUsage(t *testing.T) {
	requireKrun(t)
	for _, script := range []string{"villa-recalc", "villa-render", "villa-readback"} {
		cmd := exec.Command("podman", "run", "--rm", "--runtime=krun", "--network", "none", image, script)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%s with no args exited 0, want non-zero (output: %s)", script, out)
		}
	}
}

// TestOfficeScriptsOnPath: `which` finds each script by name (spec's "the image
// ships which"; the model invokes these by name, never a path it constructs).
func TestOfficeScriptsOnPath(t *testing.T) {
	requireKrun(t)
	for _, script := range []string{"villa-recalc", "villa-render", "villa-readback"} {
		cmd := exec.Command("podman", "run", "--rm", "--runtime=krun", "--network", "none", image, "which", script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("which %s: %v\n%s", script, err, out)
		}
	}
}
