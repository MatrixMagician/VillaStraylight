package main

// main_test.go drives the tool through run(), the same entry point a maintainer's
// shell reaches, rather than reaching past the CLI into helpers. The flags, the
// refusals and the file modes are the product here — a helper that works behind a
// CLI that refuses to call it is not a working tool.
//
// Every key is generated into a t.TempDir. No key material is committed.

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/manifest"
)

// runTool invokes the tool and returns its stdout plus any error.
func runTool(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(args, &out, &errOut)
	return out.String() + errOut.String(), err
}

// TestKeygenBuildSignVerify is the whole workflow end to end, which is the demo
// this ticket promises: sign a manifest, verify the signature, watch verification
// fail when a byte changes.
func TestKeygenBuildSignVerify(t *testing.T) {
	dir := t.TempDir()

	// 1. Generate a key.
	out, err := runTool(t, "keygen", "--out", dir)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	if !strings.Contains(out, "public key, to compile into villa") {
		t.Error("keygen did not print the public key a maintainer must compile in")
	}

	keyPath := filepath.Join(dir, "ed25519.key")
	pubPath := filepath.Join(dir, "ed25519.pub")

	// The private key is owner-only. A signing key readable by the group is a
	// signing key shared with the group.
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("private key mode = %o, want 0600", perm)
	}

	// 2. Build a manifest from the compiled-in table.
	manifestPath := filepath.Join(dir, "pins.json")
	built, err := runTool(t, "build", "--serial", "7", "--valid-until", "180d")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(built), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// 3. Sign it.
	if _, err := runTool(t, "sign", "--key", keyPath, manifestPath); err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigHex, err := os.ReadFile(manifestPath + ".sig")
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	sig, err := hex.DecodeString(strings.TrimSpace(string(sigHex)))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}

	// 4. Verify it, as villa would.
	pubHex, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	pub, err := hex.DecodeString(strings.TrimSpace(string(pubHex)))
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	published, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := manifest.Verify(pub, published, sig); err != nil {
		t.Fatalf("the published manifest did not verify: %v", err)
	}

	// 5. Change one byte and watch it fail.
	tampered := append([]byte(nil), published...)
	tampered[len(tampered)/2] ^= 0x01
	if err := manifest.Verify(pub, tampered, sig); err == nil {
		t.Error("a tampered manifest verified")
	}
}

// TestKeygenRefusesToOverwriteAKey: overwriting a signing key is unrecoverable in
// the way that matters — every host that saw a manifest signed by the old key
// trusts only that key until a new villa ships, so a silent clobber ends publishing
// for a release cycle.
func TestKeygenRefusesToOverwriteAKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := runTool(t, "keygen", "--out", dir); err != nil {
		t.Fatalf("first keygen: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "ed25519.key"))
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	out, err := runTool(t, "keygen", "--out", dir)
	if err == nil {
		t.Fatal("a second keygen overwrote an existing signing key")
	}
	if !strings.Contains(err.Error()+out, "refusing to overwrite") {
		t.Errorf("the refusal does not explain itself: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(dir, "ed25519.key"))
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the key changed despite the refusal")
	}
}

// TestSignRefusesAManifestTheAllowlistWouldReject is the catch-it-here property. A
// manifest that oversteps is refused on EVERY host, so discovering it in the field
// costs a publishing cycle plus every user's confusion.
func TestSignRefusesAManifestTheAllowlistWouldReject(t *testing.T) {
	dir := t.TempDir()
	if _, err := runTool(t, "keygen", "--out", dir); err != nil {
		t.Fatalf("keygen: %v", err)
	}

	// A manifest that introduces a component villa does not ship.
	doc := manifest.FromTable(3, futureExpiry())
	doc.Components = append(doc.Components, manifest.Component{
		ID: "attacker-supplied-tool", Registry: "docker.io", Shape: "rolling_digest",
		Ref: "docker.io/evil/tool@sha256:00",
	})
	data, err := manifest.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(dir, "pins.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, err := runTool(t, "sign", "--key", filepath.Join(dir, "ed25519.key"), path)
	if err == nil {
		t.Fatal("the tool signed a manifest that would be refused on every host")
	}
	if !strings.Contains(err.Error()+out, "not in villa's compiled-in table") {
		t.Errorf("the refusal does not name the offending entry: %v", err)
	}
	if _, statErr := os.Stat(path + ".sig"); statErr == nil {
		t.Error("a signature was written despite the refusal")
	}
}

// TestCheckNeedsNoKey: a maintainer iterating on a draft should not have to unlock
// the key to find out whether it is valid.
func TestCheckNeedsNoKey(t *testing.T) {
	dir := t.TempDir()
	data, err := manifest.Marshal(manifest.FromTable(5, futureExpiry()))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	path := filepath.Join(dir, "pins.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, err := runTool(t, "check", path)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(out, "serial 5") {
		t.Errorf("check did not report what it checked: %q", out)
	}
}

// TestBuildRefusesAZeroSerialAndAMissingExpiry: both defaults are the unsafe
// direction, so neither may be reached by omission. A zero serial means "no floor";
// a missing expiry means a manifest that can be frozen and served forever.
func TestBuildRefusesAZeroSerialAndAMissingExpiry(t *testing.T) {
	if _, err := runTool(t, "build", "--valid-until", "180d"); err == nil {
		t.Error("build accepted a zero serial")
	}
	if _, err := runTool(t, "build", "--serial", "1"); err == nil {
		t.Error("build accepted a missing expiry")
	}
	if _, err := runTool(t, "build", "--serial", "1", "--valid-until", "next tuesday"); err == nil {
		t.Error("build accepted an unparseable expiry")
	}
}

// TestValidUntilAcceptsDays exists because the window is measured in MONTHS and
// time.ParseDuration tops out at hours. Without the day form a maintainer does
// calendar arithmetic to pick a date, which is the chore that ends in a six-DAY
// window when six months was meant.
func TestValidUntilAcceptsDays(t *testing.T) {
	out, err := runTool(t, "build", "--serial", "1", "--valid-until", "180d")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	doc, err := manifest.Parse([]byte(out))
	if err != nil {
		t.Fatalf("parse built manifest: %v", err)
	}
	if doc.ValidUntil == "" {
		t.Error("the built manifest carries no expiry")
	}
}

// TestNoPrivateKeyIsCommitted walks the repository for anything that looks like
// key material. The rule is not "no key in this package" but "no key in this
// repository", because a private key in a repo is a private key on every clone,
// every runner and every fork.
func TestNoPrivateKeyIsCommitted(t *testing.T) {
	root := filepath.Join("..", "..")
	markers := []string{
		"BEGIN PRIVATE KEY",
		"BEGIN OPENSSH PRIVATE KEY",
		"BEGIN EC PRIVATE KEY",
		"BEGIN RSA PRIVATE KEY",
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // an unreadable path is not evidence of a key
		}
		if info.IsDir() {
			if name := info.Name(); name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Size() > 1<<20 {
			return nil // a key is small; skip images and binaries
		}
		if strings.HasSuffix(path, ".key") {
			t.Errorf("%s looks like committed key material", path)
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		// This test file names the markers it searches for, so skip itself.
		if strings.HasSuffix(path, "main_test.go") {
			return nil
		}
		for _, m := range markers {
			if bytes.Contains(data, []byte(m)) {
				t.Errorf("%s contains %q — no private key may be committed, test-only or otherwise", path, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// futureExpiry is a fixed expiry far enough ahead that a fixture never lapses
// while the test suite runs.
func futureExpiry() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
