package openwebui

// openwebui_test.go covers the client's shared diagnostic layer: decode and the body
// classifier it reports through.
//
// The promise under test is that a parse diagnostic explains why a parse failed
// without reproducing what was being parsed. The bodies this package parses include a
// user's whole chat transcript and an auth response, and an error text outlives the
// moment it was printed in: scrollback, a captured log, a pasted bug report.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestDecodeNeverReproducesTheBody: decode's error must not carry the content it
// failed to parse, whether that content is a chat transcript or a credential.
//
// A credential is not only the field a caller looked for. SignIn's extract returning
// false means "no token FIELD", which is not "no credential in the body", so a
// credential under any other key would otherwise reach the error text.
func TestDecodeNeverReproducesTheBody(t *testing.T) {
	for _, tc := range []struct {
		name, body, secret string
	}{
		{"chat transcript", `{"chat":{"messages":[{"content":"my private therapy notes"}]}}`, "my private therapy notes"},
		{"credential under an unexpected key", `{"access_token":"eyJhbGciOiJIUzI1NiJ9.SECRETJWT"}`, "SECRETJWT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var into []string
			err := decode("chats/{id}", []byte(tc.body), &into)
			if err == nil {
				t.Fatalf("expected a parse error")
			}
			if strings.Contains(err.Error(), tc.secret) {
				t.Fatalf("error text reproduces the body content %q:\n  %s", tc.secret, err.Error())
			}
		})
	}
}

// TestBodyShapeClassifiesStructureOnly: the classifier must tell a wrong-shape
// response from a truncated one or an upstream HTML error page, and must reach that
// verdict from structure alone. The echo assertion is what stops it regressing into
// quoting the body it is describing.
func TestBodyShapeClassifiesStructureOnly(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"empty", "", "an empty body"},
		{"whitespace only", "  \n\t ", "an empty body"},
		{"object", `{"secret":"hunter2"}`, "a JSON object"},
		{"array", `["alpha","beta"]`, "a JSON array"},
		{"string scalar", `"a whole transcript"`, "a JSON scalar"},
		{"number scalar", `-12.5e3`, "a JSON scalar"},
		{"HTML error page", "<!DOCTYPE html>\n<title>502 Bad Gateway</title>", "an HTML document"},
		{"non-JSON text", "upstream connect error or disconnect", "non-JSON text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := bodyShape([]byte(tc.body))
			if got != tc.want {
				t.Errorf("bodyShape(%q) = %q, want %q", tc.body, got, tc.want)
			}
			for i := 0; i+4 <= len(tc.body); i++ {
				if run := tc.body[i : i+4]; strings.Contains(got, run) {
					t.Errorf("bodyShape echoed %q from the body into %q", run, got)
				}
			}
		})
	}
}

// TestDecodeStaysDiagnosable: withholding the body must not make a parse failure
// unreadable. The size and the shape are what let an operator tell a truncated
// response from a wrong-shape one without ever seeing its content.
func TestDecodeStaysDiagnosable(t *testing.T) {
	const body = `{"chat":{"messages":[{"content":"my private therapy notes"}]}}`
	var into []string
	err := decode("chats/{id}", []byte(body), &into)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	for _, want := range []string{
		"parse chats/{id}",
		fmt.Sprintf("%d bytes", len(body)),
		"a JSON object",
		"cannot unmarshal object",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must still report %q", err, want)
		}
	}
}

// TestNoParseDiagnosticReproducesAResponseBody sweeps EVERY call site that reports a
// body it could not use, planting a secret in each one. A single site left quoting its
// body reopens the whole hole, so this is a sweep rather than a spot check.
func TestNoParseDiagnosticReproducesAResponseBody(t *testing.T) {
	const secret = "SQUIRREL-hunter2-JWT"
	ctx := context.Background()

	for _, tc := range []struct {
		site string
		call func() error
	}{
		{
			site: "decode",
			call: func() error {
				var into []string
				return decode("chats/{id}", []byte(`{"leak":"`+secret+`"}`), &into)
			},
		},
		{
			site: "signup token",
			call: func() error {
				_, err := New(fakeJSON(map[string]string{
					pathSignIn: `{"detail":"` + secret + `"}`,
					pathSignUp: `{"access_token":"` + secret + `"}`,
				})).SignIn(ctx, "a@b", "pw", "villa")
				return err
			},
		},
		{
			site: "/api/models",
			call: func() error {
				_, err := New(fakeJSON(map[string]string{
					pathModels: `{"data":[],"detail":"` + secret + `"}`,
				})).DiscoverModel(ctx, "tok")
				return err
			},
		},
		{
			site: "/api/models unparseable",
			call: func() error {
				_, err := New(fakeJSON(map[string]string{
					pathModels: `["` + secret + `"]`,
				})).DiscoverModel(ctx, "tok")
				return err
			},
		},
		{
			site: "chats/{id}",
			call: func() error {
				_, err := New(fakeJSON(map[string]string{
					pathChat("c1"): `{"chat":{"messages":[{"content":"` + secret + `"}]}}`,
				})).GetChat(ctx, "tok", "c1")
				return err
			},
		},
		{
			site: "knowledge/create",
			call: func() error {
				_, err := New(fakeJSON(map[string]string{
					pathKnowledgePage(1): `[]`,
					pathKnowledgeCreate:  `{"detail":"` + secret + `"}`,
				})).EnsureKnowledge(ctx, "tok", "Villa Recall", "d")
				return err
			},
		},
		{
			site: "files/ upload",
			call: func() error {
				_, err := New(fakeJSON(map[string]string{
					pathFiles: `{"detail":"` + secret + `"}`,
				})).UploadFile(ctx, "tok", "t.md", "body", time.Second)
				return err
			},
		},
		{
			site: "openai/config",
			call: func() error {
				_, err := New(fakeJSON(map[string]string{
					pathOpenAIConfig: `{"OPENAI_API_KEYS":["` + secret,
				})).SyncEndpoints(ctx, "tok", []string{"http://villa-llama:8080/v1"})
				return err
			},
		},
	} {
		t.Run(tc.site, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("an unusable response body must be an error")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("the %s diagnostic reproduced the response body:\n  %s", tc.site, err.Error())
			}
		})
	}
}
