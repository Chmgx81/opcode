package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package main\n\nfunc Hello() {\n\t// hello there\n}\n")
	write("sub/b.go", "func Hello() {\n\treturn\n}\n")
	write("notes.txt", "Hello appears twice\nHello again\n")
	write("bin.dat", "Hello\x00binary")
	write(".git/config", "Hello in git")

	out, err := run(t, Grep{}, `{"pattern": "Hello", "path": "`+dir+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Every match carries path:line, binary and .git are skipped.
	for _, want := range []string{"a.go:3", "sub/b.go:1", "notes.txt:1", "notes.txt:2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "bin.dat") || strings.Contains(out, ".git") {
		t.Errorf("binary or .git leaked into results:\n%s", out)
	}

	// Glob filter narrows by base name.
	out, err = run(t, Grep{}, `{"pattern": "Hello", "path": "`+dir+`", "glob": "*.go"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "notes.txt") {
		t.Errorf("glob filter ignored:\n%s", out)
	}
	if !strings.Contains(out, "a.go:3") {
		t.Errorf("glob filtered out the .go match:\n%s", out)
	}

	// No matches say so; a bad pattern errors loudly.
	if out, err = run(t, Grep{}, `{"pattern": "zzzznope", "path": "`+dir+`"}`); err != nil || out != "no matches" {
		t.Errorf("no-match case = (%q, %v)", out, err)
	}
	if _, err = run(t, Grep{}, `{"pattern": "["}`); err == nil {
		t.Error("bad regex must error")
	}
	if _, err = run(t, Grep{}, `{}`); err == nil {
		t.Error("missing pattern must error")
	}
}

func TestSearchFilesCapsHugeResults(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "needle line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, Grep{}, `{"pattern": "needle", "path": "`+dir+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := strings.Count(out, "needle line"); got != searchMaxMatches {
		t.Errorf("returned %d matches, want the %d cap", got, searchMaxMatches)
	}
	if !strings.Contains(out, "more matches") {
		t.Errorf("truncation note missing:\n%s", out)
	}
}

func TestCurrentTime(t *testing.T) {
	out, err := run(t, CurrentTime{}, `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Honest shape: weekday and both UTC and local times present.
	if !strings.Contains(out, "weekday:") || !strings.Contains(out, "UTC:") {
		t.Errorf("current_time = %q", out)
	}
}

func TestApplyPatchEndToEnd(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(old, []byte("line one\nline two\nline three\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	patch := `*** Begin Patch
*** Update File: ` + old + `
@@ line two
-line two
+line two, changed
*** Add File: ` + filepath.Join(dir, "new", "created.txt") + `
+first
+second
*** End Patch`
	args, _ := json.Marshal(map[string]string{"patch": patch})
	out, err := run(t, ApplyPatch{}, string(args))
	if err != nil {
		t.Fatalf("Execute: %v (%s)", err, out)
	}
	data, _ := os.ReadFile(old)
	if string(data) != "line one\nline two, changed\nline three\n" {
		t.Errorf("update hunk wrong: %q", data)
	}
	if data, _ = os.ReadFile(filepath.Join(dir, "new", "created.txt")); string(data) != "first\nsecond\n" {
		t.Errorf("add-file wrong: %q", data)
	}

	// Delete lives in its own call so it cannot mask the update.
	patch = "*** Begin Patch\n*** Delete File: " + old + "\n*** End Patch"
	args, _ = json.Marshal(map[string]string{"patch": patch})
	if _, err := run(t, ApplyPatch{}, string(args)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("delete-file left the file behind")
	}
}

func TestApplyPatchRejectsBadContext(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: " + filepath.Join(dir, "f.txt") +
		"\n@@ nowhere\n-does not exist\n+x\n*** End Patch"
	args, _ := json.Marshal(map[string]string{"patch": patch})
	if _, err := run(t, ApplyPatch{}, string(args)); err == nil {
		t.Error("a hunk whose context is absent must fail, not guess")
	}

	// Missing markers and empty patches fail loudly.
	if _, err := run(t, ApplyPatch{}, `{"patch": "not a patch"}`); err == nil {
		t.Error("missing Begin Patch must error")
	}
	if _, err := run(t, ApplyPatch{}, `{"patch": "*** Begin Patch\n*** End Patch"}`); err == nil {
		t.Error("empty patch must error")
	}
}

// TestApplyPatchBounded: the gate credits a patch as bounded only
// when every touched path is inside the writable roots.
func TestApplyPatchBounded(t *testing.T) {
	promptCalled := false
	prompt := func(Tool, string) bool { promptCalled = true; return true }
	decide := PolicyDecide(ModeAsk, prompt)
	mustJSON := func(patch string) string {
		b, _ := json.Marshal(map[string]string{"patch": patch})
		return string(b)
	}

	// A patch touching relative (in-cwd) paths is bounded.
	promptCalled = false
	if !decide(ApplyPatch{}, mustJSON("*** Begin Patch\n*** Update File: local.go\n-x\n+y\n*** End Patch")) {
		t.Fatal("in-cwd patch denied in ask mode")
	}
	if promptCalled {
		t.Error("in-cwd patch prompted — it is bounded")
	}

	// One out-of-root path makes the whole patch unbounded.
	promptCalled = false
	if !decide(ApplyPatch{}, mustJSON("*** Begin Patch\n*** Update File: local.go\n-x\n+y\n*** Update File: /etc/passwd\n-x\n+y\n*** End Patch")) ||
		!promptCalled {
		t.Error("patch touching /etc must prompt in ask mode")
	}

	// Unparsable patch text is not bounded: fail closed.
	promptCalled = false
	if !decide(ApplyPatch{}, mustJSON("garbage")) || !promptCalled {
		t.Error("unparsable patch must prompt, not auto-run")
	}
}

func TestGlobFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go")
	write("a_test.go")
	write("sub/b.go")
	write("sub/deep/c.go")
	write(".git/hidden.go")

	out, err := run(t, Glob{}, `{"pattern": "**/*_test.go", "path": "`+dir+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "a_test.go") || strings.Contains(out, ".git") {
		t.Errorf("** glob wrong: %s", out)
	}

	out, err = run(t, Glob{}, `{"pattern": "*.go", "path": "`+dir+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// A single * stays at the top level.
	if strings.Contains(out, filepath.Join("sub", "b.go")) || !strings.Contains(out, "a.go") {
		t.Errorf("single-star glob crossed directories: %s", out)
	}

	if out, err = run(t, Glob{}, `{"pattern": "zzz.*", "path": "`+dir+`"}`); err != nil || out != "no matches" {
		t.Errorf("no-match case = (%q, %v)", out, err)
	}
	if _, err = run(t, Glob{}, `{}`); err == nil {
		t.Error("missing pattern must error")
	}
}

func TestWebFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			fmt.Fprint(w, "<html><head><style>body{color:red}</style><title>T</title>"+
				"</head><body><h1>Hello docs</h1><p>Use the <b>API</b> &amp; enjoy.</p>"+
				"<script>alert('x')</script></body></html>")
		case "/redirect":
			http.Redirect(w, r, "/page", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/binary":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte{0x00, 0x01})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := run(t, WebFetch{}, `{"url": "`+srv.URL+`/page"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Readable text survives; script and style do not.
	for _, want := range []string{"Hello docs", "Use the API & enjoy."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "alert") || strings.Contains(out, "color:red") {
		t.Errorf("script or style leaked:\n%s", out)
	}

	// Redirects are followed; redirect loops fail loudly.
	if out, err = run(t, WebFetch{}, `{"url": "`+srv.URL+`/redirect"}`); err != nil || !strings.Contains(out, "Hello docs") {
		t.Errorf("redirect case = (%q, %v)", out, err)
	}
	if _, err = run(t, WebFetch{}, `{"url": "`+srv.URL+`/loop"}`); err == nil {
		t.Error("redirect loop must fail")
	}

	// Non-text reports instead of dumping bytes; non-http schemes
	// and 404s say so.
	if out, err = run(t, WebFetch{}, `{"url": "`+srv.URL+`/binary"}`); err != nil || !strings.Contains(out, "not fetched as text") {
		t.Errorf("binary case = (%q, %v)", out, err)
	}
	if _, err = run(t, WebFetch{}, `{"url": "ftp://example.test"}`); err == nil {
		t.Error("non-http scheme must fail")
	}
	if out, err = run(t, WebFetch{}, `{"url": "`+srv.URL+`/404"}`); err != nil || !strings.Contains(out, "404") {
		t.Errorf("404 case = (%q, %v)", out, err)
	}
}

// TestWebFetchPromptsInBuild: network egress is not bounded — it
// asks in build mode and runs only in full-auto.
func TestWebFetchPromptsInBuild(t *testing.T) {
	promptCalled := false
	prompt := func(Tool, string) bool { promptCalled = true; return true }
	decide := PolicyDecide(ModeBuild, prompt)
	if !decide(WebFetch{}, `{"url": "https://example.test"}`) || !promptCalled {
		t.Error("web_fetch must prompt in build mode")
	}
	promptCalled = false
	full := PolicyDecide(ModeFullAuto, prompt)
	if !full(WebFetch{}, `{"url": "https://example.test"}`) || promptCalled {
		t.Error("web_fetch must run without prompting in full-auto")
	}
	plan := PolicyDecide(ModePlan, prompt)
	promptCalled = false
	if !plan(WebFetch{}, `{"url": "https://example.test"}`) || !promptCalled {
		t.Error("web_fetch must prompt in plan mode")
	}
}
