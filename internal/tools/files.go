package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// ReadFile returns a file's contents. Read-Only tier: it can't change
// anything, so the gate always allows it under every permission mode.
type ReadFile struct{}

func (ReadFile) Name() string { return "read_file" }

func (ReadFile) Description() string {
	return "Read a file's contents from disk. Paths are relative to the current working directory unless absolute. Content is capped at 1 MiB; a larger file is truncated and says so."
}

func (ReadFile) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path of the file to read"}
		},
		"required": ["path"]
	}`)
}

func (ReadFile) Tier() Tier { return TierReadOnly }

func (ReadFile) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if a.Path == "" {
		return "", fmt.Errorf("read_file: path is required")
	}
	// read_file is read-tier, so it is never prompted: the cap is the
	// only thing between a large file and an out-of-memory crash.
	// Truncation is reported in the text, the way web_fetch does it,
	// so the model knows it is not seeing the whole file.
	f, err := openReadable(a.Path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileReadBytes+1))
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	truncated := len(data) > maxFileReadBytes
	if truncated {
		data = data[:maxFileReadBytes]
	}
	out := string(data)
	if truncated {
		out += "\n… (truncated at 1 MiB; read the rest in sections with offset or grep)"
	}
	return out, nil
}

// ListDir lists a directory's entries, one per line, directories
// marked with a trailing slash. Read-Only tier: read-only and plan
// modes can explore project structure, not just read files whose
// paths they already guessed.
type ListDir struct{}

func (ListDir) Name() string { return "list_dir" }

func (ListDir) Description() string {
	return "List a directory's entries (one per line; directories carry a trailing /). Use it to discover project structure before reading files."
}

func (ListDir) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Directory to list (default: the working directory)"}
		}
	}`)
}

func (ListDir) Tier() Tier { return TierReadOnly }

func (ListDir) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	dir := a.Path
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("list_dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	// Bound the contribution: a huge directory must not dump its
	// whole index into the context.
	const maxEntries = 500
	if len(names) > maxEntries {
		return strings.Join(names[:maxEntries], "\n") +
			fmt.Sprintf("\n… %d more entries (list a narrower path)", len(names)-maxEntries), nil
	}
	if len(names) == 0 {
		return "(empty directory)", nil
	}
	return strings.Join(names, "\n"), nil
}

// WriteFile creates or overwrites a file with the given content, creating
// parent directories as needed. Action-Allowed tier: it mutates state.
type WriteFile struct{}

func (WriteFile) Name() string { return "write_file" }

func (WriteFile) Description() string {
	return "Create or overwrite a file with the given content. Parent directories are created if missing."
}

func (WriteFile) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path of the file to write"},
			"content": {"type": "string", "description": "Full content to write to the file"}
		},
		"required": ["path", "content"]
	}`)
}

func (WriteFile) Tier() Tier { return TierActionAllowed }

func (WriteFile) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if a.Path == "" {
		return "", fmt.Errorf("write_file: path is required")
	}
	// Defense in depth: the gate denies the credentials file in every
	// mode, but a direct Execute with a nil Decide (tests, headless
	// wiring mistakes) must not be the way around it. The roots
	// re-check is the same discipline: the gate resolved the path at
	// decision time, and a swapped directory between then and now
	// must not turn an approved write into an outside-the-roots one.
	if isCredentialsFile(a.Path, credentialsPath()) {
		return "", fmt.Errorf("write_file: %w", errCredentialsFile)
	}
	if !pathInWritableRoots(a.Path) {
		return "", fmt.Errorf("write_file: %s resolves outside the writable roots", a.Path)
	}
	if err := os.MkdirAll(parentDir(a.Path), 0o755); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	if err := writeGuarded(a.Path, []byte(a.Content)); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	info, err := os.Stat(a.Path)
	if err != nil {
		return fmt.Sprintf("wrote %s", a.Path), nil
	}
	return fmt.Sprintf("wrote %d bytes to %s", info.Size(), a.Path), nil
}

// EditFile replaces the one occurrence of old with new. Action-Allowed
// tier. Exactly one match is required: zero matches means the model is
// working from a stale view, and multiple matches make the intended
// target ambiguous — both are errors rather than guesses.
type EditFile struct{}

func (EditFile) Name() string { return "edit_file" }

func (EditFile) Description() string {
	return "Edit a file by replacing exactly one occurrence of a string with another. Errors if the string is not found or appears more than once."
}

func (EditFile) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path of the file to edit"},
			"old": {"type": "string", "description": "Existing text to replace (must occur exactly once)"},
			"new": {"type": "string", "description": "Replacement text"}
		},
		"required": ["path", "old", "new"]
	}`)
}

func (EditFile) Tier() Tier { return TierActionAllowed }

func (EditFile) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if a.Path == "" || a.Old == "" {
		return "", fmt.Errorf("edit_file: path and old are required")
	}
	// Same defense-in-depth deny as write_file: the gate is the
	// posture, this is the backstop for direct Execute callers.
	if isCredentialsFile(a.Path, credentialsPath()) {
		return "", fmt.Errorf("edit_file: %w", errCredentialsFile)
	}
	if !pathInWritableRoots(a.Path) {
		return "", fmt.Errorf("edit_file: %s resolves outside the writable roots", a.Path)
	}
	data, err := readFileGuarded(a.Path)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	switch n := strings.Count(string(data), a.Old); n {
	case 0:
		return "", fmt.Errorf("edit_file: %q not found in %s", a.Old, a.Path)
	case 1:
		updated := strings.Replace(string(data), a.Old, a.New, 1)
		if err := writeGuarded(a.Path, []byte(updated)); err != nil {
			return "", fmt.Errorf("edit_file: %w", err)
		}
		return fmt.Sprintf("replaced one occurrence in %s", a.Path), nil
	default:
		return "", fmt.Errorf("edit_file: %q occurs %d times in %s; provide a longer, unique match", a.Old, n, a.Path)
	}
}

func parentDir(path string) string {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return "."
	}
	return path[:i]
}
