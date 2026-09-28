package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, tool Tool, args string) (string, error) {
	t.Helper()
	return tool.Execute(context.Background(), args)
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello tilde"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, ReadFile{}, `{"path": "`+path+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "hello tilde" {
		t.Errorf("out = %q", out)
	}
}

func TestReadFileMissing(t *testing.T) {
	_, err := run(t, ReadFile{}, `{"path": "/no/such/file"}`)
	if err == nil {
		t.Fatal("expected error reading a missing file")
	}
	if !strings.Contains(err.Error(), "read_file") {
		t.Errorf("error lacks tool name: %v", err)
	}
}

func TestReadFileBadArgs(t *testing.T) {
	if _, err := run(t, ReadFile{}, `not json`); err == nil {
		t.Error("expected error for malformed JSON")
	}
	if _, err := run(t, ReadFile{}, `{}`); err == nil {
		t.Error("expected error for missing path")
	}
}

func TestWriteFileCreatesFileAndParents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "out.txt")
	out, err := run(t, WriteFile{}, `{"path": "`+path+`", "content": "line one\nline two\n"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, path) {
		t.Errorf("out = %q", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(data) != "line one\nline two\n" {
		t.Errorf("content = %q", string(data))
	}
}

func TestEditFileReplacesOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "code.txt")
	os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644)

	out, err := run(t, EditFile{}, `{"path": "`+path+`", "old": "beta", "new": "BETA"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "one occurrence") {
		t.Errorf("out = %q", out)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "alpha\nBETA\ngamma\n" {
		t.Errorf("content = %q", string(data))
	}
}

func TestEditFileNoMatchIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "code.txt")
	os.WriteFile(path, []byte("alpha\n"), 0o644)

	if _, err := run(t, EditFile{}, `{"path": "`+path+`", "old": "zeta", "new": "x"}`); err == nil {
		t.Error("expected error for no match")
	}
}

func TestEditFileAmbiguousMatchIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "code.txt")
	os.WriteFile(path, []byte("a a a\n"), 0o644)

	_, err := run(t, EditFile{}, `{"path": "`+path+`", "old": "a", "new": "b"}`)
	if err == nil {
		t.Fatal("expected error for ambiguous match")
	}
	if !strings.Contains(err.Error(), "3 times") {
		t.Errorf("error should report occurrence count: %v", err)
	}
	// The file must be untouched when the edit is refused.
	data, _ := os.ReadFile(path)
	if string(data) != "a a a\n" {
		t.Errorf("file was modified: %q", string(data))
	}
}

func TestRunShellOutput(t *testing.T) {
	out, err := run(t, RunShell{}, `{"command": "printf 'out'; printf 'err' >&2"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "outerr" {
		t.Errorf("combined output = %q, want outerr", out)
	}
}

func TestRunShellNonzeroExitReportsOutput(t *testing.T) {
	out, err := run(t, RunShell{}, `{"command": "echo boom; exit 3"}`)
	if err == nil {
		t.Fatal("expected error on nonzero exit")
	}
	if !strings.Contains(err.Error(), "exit status") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("output on failure = %q, want it preserved for the model", out)
	}
}

func TestRunShellEmptyCommandIsError(t *testing.T) {
	if _, err := run(t, RunShell{}, `{"command": "  "}`); err == nil {
		t.Error("expected error for empty command")
	}
}

func TestToolTiers(t *testing.T) {
	read := ReadFile{}
	if read.Tier() != TierReadOnly {
		t.Error("read_file must be Read-Only")
	}
	for _, tool := range []Tool{WriteFile{}, EditFile{}, RunShell{}} {
		if tool.Tier() != TierActionAllowed {
			t.Errorf("%s must be Action-Allowed, got %s", tool.Name(), tool.Tier())
		}
	}
}

func TestRegistry(t *testing.T) {
	var r Registry
	r.Register(ReadFile{})
	r.Register(RunShell{})

	if _, ok := r.Get("read_file"); !ok {
		t.Error("read_file not found")
	}
	if _, ok := r.Get("nope"); ok {
		t.Error("unknown tool found")
	}
	defs := r.Defs()
	if len(defs) != 2 || defs[0].Name != "read_file" || defs[1].Name != "run_shell" {
		t.Errorf("defs = %+v", defs)
	}
	for _, d := range defs {
		if d.Parameters == nil {
			t.Errorf("%s has no parameter schema", d.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal(d.Parameters, &schema); err != nil {
			t.Errorf("%s parameter schema is not valid JSON: %v", d.Name, err)
		}
	}
}
