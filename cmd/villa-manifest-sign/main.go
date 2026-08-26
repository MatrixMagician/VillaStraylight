// Command villa-manifest-sign generates a signing keypair and signs pin manifests.
//
// It is a SEPARATE BINARY from villa, deliberately. It runs on the maintainer's
// machine beside the private key; villa itself only ever verifies. Shipping a sign
// verb inside the binary every user runs would put key-handling code on thousands
// of machines that will never hold a key, and would invite exactly the shortcut
// this design exists to prevent — dropping the key into CI so a workflow can sign.
//
// The key never touches CI. .github/workflows/release.yml deliberately does not
// sign: a key in Actions secrets is reachable by anyone who can land a workflow
// change, which would lower manifest trust to "repo write access" — precisely the
// property the compiled-in public key exists to avoid.
//
// Verbs:
//
//	villa-manifest-sign keygen  --out DIR         write a fresh ed25519 keypair
//	villa-manifest-sign build   --serial N --valid-until T   render the table as a manifest
//	villa-manifest-sign sign    --key FILE MANIFEST          write MANIFEST.sig
//	villa-manifest-sign check   MANIFEST                     allowlist-check without signing
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/manifest"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "villa-manifest-sign: %v\n", err)
		os.Exit(1)
	}
}

// run is the whole tool, seamed on its streams so the tests drive it as a user
// would rather than reaching past the CLI into helpers.
func run(args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		usage(errOut)
		return errors.New("no verb given")
	}
	switch args[0] {
	case "keygen":
		return runKeygen(args[1:], out)
	case "build":
		return runBuild(args[1:], out)
	case "sign":
		return runSign(args[1:], out)
	case "check":
		return runCheck(args[1:], out)
	case "-h", "--help", "help":
		usage(out)
		return nil
	default:
		usage(errOut)
		return fmt.Errorf("unknown verb %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `villa-manifest-sign — sign villa pin manifests, offline.

  keygen --out DIR                       write ed25519.key (0600) and ed25519.pub
  build  --serial N --valid-until DATE    render the compiled-in table as a manifest
  sign   --key FILE MANIFEST              write MANIFEST.sig beside MANIFEST
  check  MANIFEST                         allowlist-check a manifest without signing

The private key must live on this machine only. It must never reach CI, a shared
runner, or a secrets store a workflow can read.
`)
}

// runKeygen writes a fresh keypair.
//
// The private key is written at 0600 into a 0700 directory, and the tool REFUSES to
// overwrite an existing one. Overwriting a signing key is unrecoverable in the way
// that matters: every host that has seen a manifest signed by the old key keeps
// trusting only that key until a new villa ships, so a silent clobber would end
// manifest publishing until a release cycle completed.
func runKeygen(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	fs.SetOutput(out)
	dir := fs.String("out", "", "directory to write ed25519.key and ed25519.pub into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("keygen: --out is required")
	}

	keyPath := filepath.Join(*dir, "ed25519.key")
	pubPath := filepath.Join(*dir, "ed25519.pub")
	for _, p := range []string{keyPath, pubPath} {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("keygen: %s already exists; refusing to overwrite a signing key — "+
				"every host that has seen a manifest signed by it trusts only that key until a new villa ships", p)
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("keygen: %w", err)
	}
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		return fmt.Errorf("keygen: mkdir: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return fmt.Errorf("keygen: write private key: %w", err)
	}
	if err := os.WriteFile(pubPath, []byte(hex.EncodeToString(pub)+"\n"), 0o644); err != nil {
		return fmt.Errorf("keygen: write public key: %w", err)
	}

	fmt.Fprintf(out, "wrote %s (0600) and %s\n", keyPath, pubPath)
	fmt.Fprintf(out, "\npublic key, to compile into villa:\n  %s\n", hex.EncodeToString(pub))
	fmt.Fprint(out, "\nBack the private key up offline. If it is lost, no further manifests can be\n"+
		"published until a villa release ships a new public key.\n")
	return nil
}

// runBuild renders the compiled-in table as a manifest.
//
// Generating beats hand-writing: every id, registry and shape is correct by
// construction, so the allowlist check has nothing to catch except values the
// maintainer deliberately changed after this ran.
func runBuild(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(out)
	serial := fs.Uint64("serial", 0, "monotonic manifest serial; never reuse or regress one")
	validUntil := fs.String("valid-until", "", "RFC3339 expiry, or a duration like 180d")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *serial == 0 {
		return errors.New("build: --serial is required and must be non-zero; zero means 'no floor', so a host would accept any replayed manifest")
	}
	if *validUntil == "" {
		return errors.New("build: --valid-until is required; a manifest with no expiry can be frozen and served forever")
	}
	until, err := parseUntil(*validUntil, time.Now())
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}

	doc := manifest.FromTable(*serial, until)
	data, err := manifest.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}

// parseUntil accepts an RFC3339 timestamp or a bare day count like "180d".
//
// The duration form exists because the window is measured in months and Go's
// time.ParseDuration tops out at hours, so a maintainer would otherwise be doing
// arithmetic on a calendar to pick a date — the kind of chore that ends in a
// six-DAY window when six months was meant.
func parseUntil(s string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err == nil && n > 0 {
			return now.AddDate(0, 0, n), nil
		}
	}
	return time.Time{}, fmt.Errorf("--valid-until %q is neither an RFC3339 timestamp nor a day count like 180d", s)
}

// runSign checks the manifest and writes a detached signature beside it.
//
// The allowlist check runs FIRST and refuses to sign on any violation. A manifest
// that oversteps is refused on every host, so catching it here costs one command
// and catching it later costs a publishing cycle plus every user's confusion.
func runSign(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	fs.SetOutput(out)
	keyPath := fs.String("key", "", "path to the ed25519 private key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyPath == "" {
		return errors.New("sign: --key is required")
	}
	if fs.NArg() != 1 {
		return errors.New("sign: exactly one manifest path is required")
	}
	path := fs.Arg(0)

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("sign: read manifest: %w", err)
	}
	doc, err := manifest.Parse(data)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	if problems := manifest.CheckAllowlist(doc); len(problems) > 0 {
		return fmt.Errorf("sign: refusing to sign a manifest that would be refused on every host:\n%s", bullets(problems))
	}

	key, err := loadPrivateKey(*keyPath)
	if err != nil {
		return err
	}
	sig, err := manifest.Sign(key, data)
	if err != nil {
		return err
	}

	sigPath := path + ".sig"
	if err := os.WriteFile(sigPath, []byte(hex.EncodeToString(sig)+"\n"), 0o644); err != nil {
		return fmt.Errorf("sign: write signature: %w", err)
	}
	fmt.Fprintf(out, "signed %s (serial %d, valid until %s)\nwrote %s\n", path, doc.Serial, doc.ValidUntil, sigPath)
	return nil
}

// runCheck allowlist-checks a manifest without touching the key, so a maintainer
// can iterate on a draft without the key present.
func runCheck(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(out)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("check: exactly one manifest path is required")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("check: read manifest: %w", err)
	}
	doc, err := manifest.Parse(data)
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if problems := manifest.CheckAllowlist(doc); len(problems) > 0 {
		return fmt.Errorf("check: this manifest would be refused on every host:\n%s", bullets(problems))
	}
	fmt.Fprintf(out, "ok: %d components, serial %d, valid until %s\n", len(doc.Components), doc.Serial, doc.ValidUntil)
	return nil
}

// loadPrivateKey reads a hex-encoded ed25519 private key and warns when its file
// mode is wider than owner-only.
//
// A warning rather than a refusal: the mode may be wide because of a restore or an
// unusual umask, and refusing to sign would push the maintainer toward working
// around the tool. Saying so is what a maintainer can act on.
func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("sign: stat key: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "warning: %s is mode %o — a signing key should be 0600\n", path, perm)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("sign: read key: %w", err)
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("sign: decode key: %w", err)
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("sign: key is %d bytes, want %d", len(decoded), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(decoded), nil
}

// bullets renders a problem list one per line, so a maintainer fixes all of them in
// one pass rather than discovering them one round trip at a time.
func bullets(problems []error) string {
	var b strings.Builder
	for _, p := range problems {
		b.WriteString("  - ")
		b.WriteString(p.Error())
		b.WriteString("\n")
	}
	return b.String()
}
