package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ReadFile returns a file's contents. Read-Only tier: it can't change
// anything, so the gate always allows it under every permission mode.
type ReadFile struct{}

func (ReadFile) Name() string { return "read_file" }

func (ReadFile) Description() string {
	return "Read a file's contents from disk. Paths are relative to the current working directory unless absolute."
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
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	return string(data), nil
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
	if err := os.MkdirAll(parentDir(a.Path), 0o755); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
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
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	switch n := strings.Count(string(data), a.Old); n {
	case 0:
		return "", fmt.Errorf("edit_file: %q not found in %s", a.Old, a.Path)
	case 1:
		updated := strings.Replace(string(data), a.Old, a.New, 1)
		if err := os.WriteFile(a.Path, []byte(updated), 0o644); err != nil {
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
