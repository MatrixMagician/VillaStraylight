package orchestrate

// inference_secret_write_test.go guards the impure llama.env writer (GHSA-qxg9,
// ADR-0011): the written bytes equal the pure RenderInferenceSecretEnv render, the
// write lands 0600 inside the resolved dir, and the resolved dir's leaf matches the
// host side of InferenceSecretEnvFilePath().

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWriteInferenceSecretEnvRoundTrip proves the writer persists exactly the
// RenderInferenceSecretEnv bytes into the resolved dir at 0600, single source of
// truth (no second renderer).
func TestWriteInferenceSecretEnvRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa", "inference")
	name, want := RenderInferenceSecretEnv("hunter2hunter2")
	if err := WriteInferenceSecretEnvTo(dir, name, want); err != nil {
		t.Fatalf("WriteInferenceSecretEnvTo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != want {
		t.Errorf("written bytes = %q, want %q (RenderInferenceSecretEnv, single source of truth)", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat written file: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %o, want 0600 (the live LLAMA_API_KEY/OPENAI_API_KEY bearer, GHSA-qxg9)", info.Mode().Perm())
		}
	}
}

// TestWriteInferenceSecretEnvIdempotent proves a second write with the SAME secret
// overwrites the file with byte-identical content — reused, never rotated.
func TestWriteInferenceSecretEnvIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa", "inference")
	name, text := RenderInferenceSecretEnv("stableSecretValue")
	if err := WriteInferenceSecretEnvTo(dir, name, text); err != nil {
		t.Fatalf("first WriteInferenceSecretEnvTo: %v", err)
	}
	if err := WriteInferenceSecretEnvTo(dir, name, text); err != nil {
		t.Fatalf("second WriteInferenceSecretEnvTo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != text {
		t.Errorf("second write produced %q, want unchanged %q", got, text)
	}
}

// TestWriteInferenceSecretEnvRefusesTraversal proves a name that escapes dir is
// refused (assertInsideDir, reused from reconcile.go via writeSearxngFile).
func TestWriteInferenceSecretEnvRefusesTraversal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa", "inference")
	if err := WriteInferenceSecretEnvTo(dir, "../escape.env", "x"); err == nil {
		t.Fatal("WriteInferenceSecretEnvTo with a traversal name = nil error, want a refusal")
	}
}

// TestInferenceSecretEnvDirIsInference proves the resolved dir is villa's OWN
// inference config dir (never the systemd unit dir, never websafe/searxng's dir).
func TestInferenceSecretEnvDirIsInference(t *testing.T) {
	dir, err := inferenceSecretEnvDir()
	if err != nil {
		t.Fatalf("inferenceSecretEnvDir: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(dir), "villa/inference") {
		t.Errorf("inferenceSecretEnvDir() = %q, want a path ending in villa/inference", dir)
	}
}
