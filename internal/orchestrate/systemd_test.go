package orchestrate

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestIsActive covers the is-active state read, including the CR-02 tighten: an empty
// stdout WITH a non-zero exit is ErrCommandFailed (indeterminate-but-bad), DISTINCT
// from a missing binary (ErrToolNotFound, cannot measure) and from the failed/inactive
// states (which print their word to stdout and must be returned despite the non-zero
// exit).
func TestIsActive(t *testing.T) {
	cases := []struct {
		name      string
		out       string
		found     bool
		ok        bool
		wantState string
		wantErr   error // nil, or a sentinel to errors.As-match
	}{
		{name: "active clean", out: "active\n", found: true, ok: true, wantState: "active"},
		{name: "failed prints state despite non-zero exit", out: "failed\n", found: true, ok: false, wantState: "failed"},
		{name: "inactive prints state despite non-zero exit", out: "inactive\n", found: true, ok: false, wantState: "inactive"},
		{name: "empty stdout + non-zero exit → ErrCommandFailed", out: "", found: true, ok: false, wantErr: ErrCommandFailed{}},
		{name: "empty stdout + clean exit → silent, no error", out: "  \n", found: true, ok: true, wantState: ""},
		{name: "missing binary → ErrToolNotFound", out: "", found: false, ok: false, wantErr: ErrToolNotFound{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
				return tc.out, tc.found, tc.ok
			}}
			state, err := s.IsActive("villa-llama.service")

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("IsActive err = %v, want nil", err)
				}
				if state != tc.wantState {
					t.Fatalf("IsActive state = %q, want %q", state, tc.wantState)
				}
				return
			}

			if state != "" {
				t.Fatalf("IsActive state = %q, want empty on error", state)
			}
			switch tc.wantErr.(type) {
			case ErrCommandFailed:
				if !errors.As(err, &ErrCommandFailed{}) {
					t.Fatalf("IsActive err = %v, want ErrCommandFailed", err)
				}
			case ErrToolNotFound:
				if !errors.As(err, &ErrToolNotFound{}) {
					t.Fatalf("IsActive err = %v, want ErrToolNotFound", err)
				}
			}
		})
	}
}

// recordedCall captures one runCmd invocation so a test can assert how the residency
// scrape was scoped.
type recordedCall struct {
	name string
	args []string
}

// TestResidencyJournal covers the F-3 invocation-scoping: the residency scrape for the
// RUNNING server must target the CURRENT systemd invocation (so a multi-restart unit's
// stale oldest journal is not read), falling back to the whole-unit read when no
// invocation id is available, and degrading to ("", false) when journalctl is absent.
func TestResidencyJournal(t *testing.T) {
	const svc = "villa-llama.service"

	t.Run("invocation present → scopes journalctl by _SYSTEMD_INVOCATION_ID", func(t *testing.T) {
		var calls []recordedCall
		s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
			calls = append(calls, recordedCall{name: name, args: args})
			if name == "systemctl" {
				return "0ff87ecd677243c0ae053fea24dec5c9\n", true, true
			}
			// journalctl
			return "load_tensors: Vulkan0 model buffer size = 20583.34 MiB\n", true, true
		}}

		out, ok := s.ResidencyJournal(svc)
		if !ok || !strings.Contains(out, "Vulkan0 model buffer size") {
			t.Fatalf("ResidencyJournal = (%q, %v), want the residency line and ok=true", out, ok)
		}

		// The InvocationID was looked up via `systemctl --user show -p InvocationID`.
		if !hasCall(calls, "systemctl", "show", "-p", "InvocationID") {
			t.Fatalf("expected an InvocationID lookup; calls = %+v", calls)
		}
		// The journal was scoped to that invocation, NOT the whole unit (`-u`).
		j := findCall(calls, "journalctl")
		if j == nil {
			t.Fatalf("expected a journalctl call; calls = %+v", calls)
		}
		joined := strings.Join(j.args, " ")
		if !strings.Contains(joined, "_SYSTEMD_INVOCATION_ID=0ff87ecd677243c0ae053fea24dec5c9") {
			t.Fatalf("journalctl not scoped to the invocation: %q", joined)
		}
		if containsArg(j.args, "-u") {
			t.Fatalf("journalctl used whole-unit -u scope instead of the invocation: %q", joined)
		}
	})

	t.Run("empty InvocationID → falls back to whole-unit -u read", func(t *testing.T) {
		var calls []recordedCall
		s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
			calls = append(calls, recordedCall{name: name, args: args})
			if name == "systemctl" {
				return "\n", true, true // no invocation id (service never started)
			}
			return "device_info:\n", true, true
		}}

		out, ok := s.ResidencyJournal(svc)
		if !ok || out == "" {
			t.Fatalf("ResidencyJournal = (%q, %v), want fallback output and ok=true", out, ok)
		}
		j := findCall(calls, "journalctl")
		if j == nil || !containsArg(j.args, "-u") || !containsArg(j.args, svc) {
			t.Fatalf("expected fallback `journalctl --user -u %s`; calls = %+v", svc, calls)
		}
		if strings.Contains(strings.Join(j.args, " "), "_SYSTEMD_INVOCATION_ID=") {
			t.Fatalf("fallback must not scope by invocation id: %+v", j.args)
		}
	})

	t.Run("journalctl missing → (\"\", false)", func(t *testing.T) {
		s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
			if name == "systemctl" {
				return "abc123\n", true, true
			}
			return "", false, false // journalctl not on PATH
		}}
		out, ok := s.ResidencyJournal(svc)
		if ok || out != "" {
			t.Fatalf("ResidencyJournal = (%q, %v), want (\"\", false) when journalctl is missing", out, ok)
		}
	})
}

func hasCall(calls []recordedCall, name string, wantArgs ...string) bool {
	for _, c := range calls {
		if c.name != name {
			continue
		}
		all := true
		for _, w := range wantArgs {
			if !containsArg(c.args, w) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func findCall(calls []recordedCall, name string) *recordedCall {
	for i := range calls {
		if calls[i].name == name {
			return &calls[i]
		}
	}
	return nil
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}

// TestEnable covers the additive unit-level Enable seam (A4): it must issue a
// FIXED-ARG `systemctl --user enable <service>` (never `sh -c`), succeed when the
// tool runs cleanly, and degrade to a typed ErrToolNotFound when systemctl is absent
// (mirrors Start's not-found contract) so [Install] WantedBy=default.target can be
// applied for boot-survival without ever shelling out.
func TestEnable(t *testing.T) {
	const svc = "villa-dashboard.service"

	t.Run("fixed-arg systemctl --user enable <service>", func(t *testing.T) {
		var calls []recordedCall
		s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
			calls = append(calls, recordedCall{name: name, args: args})
			return "", true, true
		}}
		if err := s.Enable(svc); err != nil {
			t.Fatalf("Enable err = %v, want nil", err)
		}
		c := findCall(calls, "systemctl")
		if c == nil {
			t.Fatalf("expected a systemctl call; calls = %+v", calls)
		}
		// Fixed args: --user enable <service>, and NEVER a shell.
		if c.name == "sh" || containsArg(c.args, "-c") {
			t.Fatalf("Enable must never shell out: %+v", c)
		}
		want := []string{"--user", "enable", svc}
		if !slices.Equal(c.args, want) {
			t.Fatalf("Enable args = %v, want %v", c.args, want)
		}
	})

	t.Run("missing systemctl → ErrToolNotFound", func(t *testing.T) {
		s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
			return "", false, false // systemctl not on PATH
		}}
		err := s.Enable(svc)
		if !errors.As(err, &ErrToolNotFound{}) {
			t.Fatalf("Enable err = %v, want ErrToolNotFound", err)
		}
	})

	t.Run("non-zero exit → generic failure error", func(t *testing.T) {
		s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
			return "", true, false // ran but failed
		}}
		if err := s.Enable(svc); err == nil {
			t.Fatalf("Enable err = nil, want a non-nil failure error")
		}
	})
}

// TestDisable covers the unit-level Disable seam (T-05-18): a fixed-arg
// `systemctl --user disable <service>` (never a shell) that revokes boot-survival on
// uninstall, degrading to ErrToolNotFound when systemctl is absent.
func TestDisable(t *testing.T) {
	const svc = "villa-dashboard.service"

	t.Run("fixed-arg systemctl --user disable <service>", func(t *testing.T) {
		var calls []recordedCall
		s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
			calls = append(calls, recordedCall{name: name, args: args})
			return "", true, true
		}}
		if err := s.Disable(svc); err != nil {
			t.Fatalf("Disable err = %v, want nil", err)
		}
		c := findCall(calls, "systemctl")
		if c == nil {
			t.Fatalf("expected a systemctl call; calls = %+v", calls)
		}
		if c.name == "sh" || containsArg(c.args, "-c") {
			t.Fatalf("Disable must never shell out: %+v", c)
		}
		want := []string{"--user", "disable", svc}
		if !slices.Equal(c.args, want) {
			t.Fatalf("Disable args = %v, want %v", c.args, want)
		}
	})

	t.Run("missing systemctl → ErrToolNotFound", func(t *testing.T) {
		s := Systemd{runCmd: func(name string, args ...string) (string, bool, bool) {
			return "", false, false
		}}
		if err := s.Disable(svc); !errors.As(err, &ErrToolNotFound{}) {
			t.Fatalf("Disable err = %v, want ErrToolNotFound", err)
		}
	})
}
