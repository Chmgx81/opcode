package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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
	if err := os.WriteFile(path, []byte("hello opcode"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, ReadFile{}, `{"path": "`+path+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "hello opcode" {
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

// TestReadFileCapsHugeFiles: read_file is read-tier, so it runs in
// every mode and is never prompted — which made an uncapped read a way
// to exhaust the whole process's memory (under a memory limit the Go
// runtime's OOM is fatal and loses the turn). It must be capped, and it
// must say so: a silent truncation reads to the model like a complete
// file.
func TestReadFileCapsHugeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	chunk := strings.Repeat("x", 64<<10)
	for written := 0; written < 4*maxFileReadBytes; written += len(chunk) {
		if _, err := f.WriteString(chunk); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	out, err := run(t, ReadFile{}, `{"path": "`+path+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(out) > maxFileReadBytes+200 {
		t.Errorf("read_file returned %d bytes; it must be capped near %d", len(out), maxFileReadBytes)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("the model was not told the file was cut: %q", out[len(out)-120:])
	}
	// The cap must not break ordinary reads.
	small := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(small, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, ReadFile{}, `{"path": "`+small+`"}`); err != nil || out != "hello" {
		t.Errorf("ordinary read = (%q, %v)", out, err)
	}
}

// edit_file and apply_patch both read the whole file they work on, so
// the same cap applies to them — but silently truncating would corrupt
// the operation (the context would appear not to match), so they refuse
// and say why instead.
func TestEditFileRefusesFilePastTheReadCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	chunk := strings.Repeat("y", 64<<10)
	for written := 0; written < maxFileReadBytes+len(chunk); written += len(chunk) {
		if _, err := f.WriteString(chunk); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	_, err = run(t, EditFile{}, `{"path": "`+path+`", "old": "y", "new": "z"}`)
	if err == nil {
		t.Fatal("an edit of an over-cap file must be refused, not silently truncated")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("error should name the cap: %v", err)
	}
}

// TestBashCapsFloodingOutput: bash runs without prompting in build
// mode (sandboxed) and in full-auto, so a command that prints without
// end used to grow one bytes.Buffer until the process died — under a
// memory limit the Go runtime's OOM is fatal and loses the turn. The
// output is capped, the command is stopped, and the model is told
// which of the two happened.
//
// This test lives in files_test.go only because the fix is shared with
// the read caps there; it is a shell test.
func TestBashCapsFloodingOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the cap stops the command through the process-group kill (see deathAttr)")
	}
	out, err := run(t, Bash{}, `{"command": "yes x", "sandbox": false}`)
	if err == nil {
		t.Fatal("a flooding command must be stopped and reported")
	}
	if len(out) > bashMaxBytes+200 {
		t.Errorf("bash returned %d bytes of output; it must be capped near %d", len(out), bashMaxBytes)
	}
	if !strings.Contains(err.Error(), "more than 2 MiB") {
		t.Errorf("error should name the cap: %v", err)
	}
	if !strings.Contains(out, "output capped at 2 MiB") {
		t.Errorf("the model was not told the output was cut: %q", out[len(out)-120:])
	}
	// Ordinary output is untouched.
	if out, err := run(t, Bash{}, `{"command": "echo ok", "sandbox": false}`); err != nil || !strings.Contains(out, "ok") {
		t.Errorf("ordinary command = (%q, %v)", out, err)
	}
	// Output that lands exactly on the cap is not truncation, so it must
	// not be reported as such.
	exact := fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'x'", bashMaxBytes)
	if out, err := run(t, Bash{}, `{"command": `+strconv.Quote(exact)+`, "sandbox": false}`); err != nil {
		t.Errorf("output exactly at the cap = (%d bytes, %v)", len(out), err)
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

func TestBashOutput(t *testing.T) {
	out, err := run(t, Bash{}, `{"command": "printf 'out'; printf 'err' >&2"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "outerr" {
		t.Errorf("combined output = %q, want outerr", out)
	}
}

func TestBashNonzeroExitReportsOutput(t *testing.T) {
	out, err := run(t, Bash{}, `{"command": "echo boom; exit 3"}`)
	if err == nil {
		t.Fatal("expected error on nonzero exit")
	}
	if !strings.Contains(err.Error(), "exited with status") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("output on failure = %q, want it preserved for the model", out)
	}
}

func TestBashEmptyCommandIsError(t *testing.T) {
	if _, err := run(t, Bash{}, `{"command": "  "}`); err == nil {
		t.Error("expected error for empty command")
	}
}

func TestToolTiers(t *testing.T) {
	read := ReadFile{}
	if read.Tier() != TierReadOnly {
		t.Error("read_file must be Read-Only")
	}
	list := ListDir{}
	if list.Tier() != TierReadOnly {
		t.Error("list_dir must be Read-Only")
	}
	for _, tool := range []Tool{WriteFile{}, EditFile{}, Bash{}} {
		if tool.Tier() != TierActionAllowed {
			t.Errorf("%s must be Action-Allowed, got %s", tool.Name(), tool.Tier())
		}
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, ListDir{}, `{"path": "`+dir+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "a.txt\nadir/\nb.txt"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}

	// No path lists the working directory; an empty directory says
	// so instead of returning nothing.
	empty := t.TempDir()
	out, err = run(t, ListDir{}, `{"path": "`+empty+`"}`)
	if err != nil {
		t.Fatalf("Execute on empty dir: %v", err)
	}
	if out != "(empty directory)" {
		t.Errorf("empty dir out = %q", out)
	}

	if _, err := run(t, ListDir{}, `{"path": "`+filepath.Join(dir, "a.txt")+`"}`); err == nil {
		t.Error("listing a file must error")
	}
	if _, err := run(t, ListDir{}, `{"path": "`+filepath.Join(dir, "missing")+`"}`); err == nil {
		t.Error("listing a missing directory must error")
	}
}

func TestListDirCapsHugeDirectories(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 600; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := run(t, ListDir{}, `{"path": "`+dir+`"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := strings.Count(out, "\n") + 1; got > 501 {
		t.Errorf("huge directory returned %d lines, want the 500-entry cap plus the count note", got)
	}
	if !strings.Contains(out, "100 more entries") {
		t.Errorf("cap note missing: %q", out)
	}
}

func TestRegistry(t *testing.T) {
	var r Registry
	r.Register(ReadFile{})
	r.Register(Bash{})

	if _, ok := r.Get("read_file"); !ok {
		t.Error("read_file not found")
	}
	if _, ok := r.Get("nope"); ok {
		t.Error("unknown tool found")
	}
	defs := r.Defs()
	if len(defs) != 2 || defs[0].Name != "read_file" || defs[1].Name != "bash" {
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
