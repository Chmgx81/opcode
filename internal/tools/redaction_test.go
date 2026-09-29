package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The registered values come back exactly as registered: session save
// passes them to its own writer, which knows nothing of the variants.
func TestRedactorSecretsAreUnchanged(t *testing.T) {
	r := NewRedactor("sk-a\"b", "plain")
	r.Add("", "line1-of-key\nline2-of-key")
	want := []string{"sk-a\"b", "plain", "line1-of-key\nline2-of-key"}
	got := r.Secrets()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Secrets() = %q, want %q", got, want)
	}
}

// Overlapping secrets must not leave a fragment of either behind, in
// whatever order they were registered: replacing one first used to
// split the other ("ab[redacted]3").
func TestRedactorOverlappingSecretsLeaveNoFragment(t *testing.T) {
	for _, order := range [][]string{{"c12", "abc123"}, {"abc123", "c12"}} {
		r := NewRedactor(order...)
		got := r.Redact("token=abc123;")
		if got != "token=[redacted];" {
			t.Errorf("order %v: Redact = %q", order, got)
		}
	}
	// A secret that only partially overlaps another still hides fully.
	r := NewRedactor("abcd", "cdef")
	if got := r.Redact("xabcdefx"); got != "x[redacted]x" {
		t.Errorf("chained overlap: Redact = %q", got)
	}
}

// A placeholder must not be rescanned: a secret that occurs inside
// "[redacted]" must not chew up earlier replacements.
func TestRedactorDoesNotRescanPlaceholder(t *testing.T) {
	r := NewRedactor("secret-key-1", "red")
	got := r.Redact("a secret-key-1 b")
	if got != "a [redacted] b" {
		t.Errorf("Redact = %q", got)
	}
}

func TestRedactorShortSecretIsRedactedEverywhere(t *testing.T) {
	r := NewRedactor("k3y")
	for in, want := range map[string]string{
		"k3y":               "[redacted]",
		"KEY=k3y\n":         "KEY=[redacted]\n",
		"k3yk3y":            "[redacted]",
		"nothing to see":    "nothing to see",
		`{"a":"k3y","b":1}`: `{"a":"[redacted]","b":1}`,
	} {
		if got := r.Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

// What `cat auth.json` prints is the JSON spelling of the value, not
// the value: quotes, backslashes, control characters and (with Go's
// default encoder) <, > and & are escaped.
func TestRedactorCoversJSONEscapedForms(t *testing.T) {
	secret := "ab\"cd\\ef<g>&h\tij"
	r := NewRedactor(secret)

	std, err := json.Marshal(map[string]string{"key": secret}) // HTML-escapes: \u003c
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]string{"key": secret}); err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"raw":                     "key: " + secret,
		"json (html-escaped)":     string(std),
		"json (no html escaping)": plain.String(),
	} {
		got := r.Redact(doc)
		for _, frag := range []string{"cd", "ef", "ij"} {
			if strings.Contains(got, frag) {
				t.Errorf("%s: fragment %q survived: %q", name, frag, got)
			}
		}
		if !strings.Contains(got, "[redacted]") {
			t.Errorf("%s: nothing redacted: %q", name, got)
		}
	}
}

func TestRedactorCoversMultiLineSecrets(t *testing.T) {
	secret := "-----BEGIN KEY-----\nMIIBVQIBADANBgkqhkiG9w0BAQEF\nAASCAT8wggE7AgEAAkEA0xyz\n-----END KEY-----"
	r := NewRedactor(secret)
	leaks := []string{"MIIBVQIBADANBgkqhkiG9w0BAQEF", "AASCAT8wggE7AgEAAkEA0xyz"}

	forms := map[string]string{
		"raw":          secret,
		"crlf":         strings.ReplaceAll(secret, "\n", "\r\n"),
		"json escaped": mustJSONString(t, secret),
		"grep -n":      "auth.pem:2:" + strings.Split(secret, "\n")[1] + "\nauth.pem:3:" + strings.Split(secret, "\n")[2],
		"indented":     "    " + strings.ReplaceAll(secret, "\n", "\n    "),
	}
	for name, doc := range forms {
		got := r.Redact(doc)
		for _, leak := range leaks {
			if strings.Contains(got, leak) {
				t.Errorf("%s: %q survived: %q", name, leak, got)
			}
		}
	}
}

func mustJSONString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A key stored with a trailing newline or space still shows up
// without it.
func TestRedactorTrimsWhitespaceForm(t *testing.T) {
	r := NewRedactor("sk-with-newline\n")
	if got := r.Redact("Authorization: sk-with-newline "); strings.Contains(got, "sk-with-newline") {
		t.Errorf("trimmed form leaked: %q", got)
	}
}

func TestRedactorConcurrentAddAndRedact(t *testing.T) {
	r := NewRedactor("seed-secret")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r.Add(fmt.Sprintf("secret-%d-%d", i, j))
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = r.Redact("seed-secret secret-1-1 plain")
			}
		}()
	}
	wg.Wait()
	if got := r.Redact("secret-3-7"); got != "[redacted]" {
		t.Errorf("Redact after concurrent Add = %q", got)
	}
}

// The end-to-end shape of the audit-S1 attack, with the values that
// make naive redaction fail: the file is cat'd through the sandboxed
// shell and read through read_file, and the model-facing result and
// the returned error come back clean while the audit log holds no
// copy either.
func TestGateRedactsEncodedCredentialsInResults(t *testing.T) {
	dir := t.TempDir()
	secrets := map[string]string{
		"short":     "k3y",
		"escaped":   "sk-a\"b\\c<d>",
		"multiline": "MIIBVQIBADANBgkq\nAASCAT8wggE7AgEA",
	}
	redactor := NewRedactor()
	auth := map[string]string{}
	for name, s := range secrets {
		redactor.Add(s)
		auth[name] = s
	}
	cred := filepath.Join(dir, "auth.json")
	doc, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, doc, 0o600); err != nil {
		t.Fatal(err)
	}
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"), redactor)
	g := &Gate{Audit: log}

	check := func(what, out string) {
		t.Helper()
		for name, s := range secrets {
			for _, frag := range append(strings.Split(s, "\n"), mustJSONString(t, s)[1:len(mustJSONString(t, s))-1]) {
				if len(frag) >= 8 && strings.Contains(out, frag) {
					t.Errorf("%s leaks %s (%q):\n%s", what, name, frag, out)
				}
			}
		}
		if strings.Contains(out, "k3y") {
			t.Errorf("%s leaks the short key:\n%s", what, out)
		}
	}
	out, err := g.Execute(context.Background(), Bash{}, `{"command": "cat `+cred+`"}`)
	if err != nil {
		t.Fatalf("bash cat: %v", err)
	}
	check("bash cat", out)
	out, err = g.Execute(context.Background(), ReadFile{}, `{"path": "`+cred+`"}`)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	check("read_file", out)
	out, err = g.Execute(context.Background(), Bash{}, `{"command": "cat `+cred+` >&2; exit 4"}`)
	if err == nil {
		t.Fatal("expected a nonzero exit")
	}
	check("failing bash", out)

	data, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatal(err)
	}
	check("audit log", string(data))
}

type failingTool struct{ err error }

func (failingTool) Name() string                { return "failing" }
func (failingTool) Description() string         { return "" }
func (failingTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (failingTool) Tier() Tier                  { return TierReadOnly }
func (f failingTool) Execute(context.Context, string) (string, error) {
	return "", f.err
}

// The error crosses the model boundary next to the result, so it is
// redacted too — without losing errors.Is/As for the caller.
func TestGateRedactsSecretsInReturnedError(t *testing.T) {
	sentinel := errors.New("sentinel")
	tool := failingTool{err: fmt.Errorf("could not use sk-in-error: %w", sentinel)}
	g := &Gate{Audit: NewAuditLog(filepath.Join(t.TempDir(), "audit.jsonl"), NewRedactor("sk-in-error"))}

	_, err := g.Execute(context.Background(), tool, `{}`)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-in-error") {
		t.Errorf("error text leaks the credential: %q", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is lost through redaction: %v", err)
	}

	// An error with nothing to hide is returned as is.
	plain := failingTool{err: sentinel}
	if _, err := g.Execute(context.Background(), plain, `{}`); err != sentinel {
		t.Errorf("clean error was rewrapped: %#v", err)
	}
}
