package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SearchFiles greps file contents for a pattern — the read-tier way
// to find things. Without it the model must read files one by one
// and guess; with it, read-only and plan modes can navigate a
// codebase the way Codex's shell+rg flow does, without running
// anything.
type SearchFiles struct{}

func (SearchFiles) Name() string { return "search_files" }

func (SearchFiles) Description() string {
	return "Search file contents with a regular expression and return matching lines as path:line: text. Searches the working directory by default; binary files, .git, and vendor caches are skipped. Use this to find where something lives before reading files."
}

func (SearchFiles) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Regular expression (RE2 syntax) to match in each line"},
			"path": {"type": "string", "description": "File or directory to search (default: the working directory)"},
			"glob": {"type": "string", "description": "Optional filename filter, e.g. \"*.go\" — matched against each file's base name"}
		},
		"required": ["pattern"]
	}`)
}

func (SearchFiles) Tier() Tier { return TierReadOnly }

// Search bounds: a huge tree must not dump its whole hit list into
// the context.
const (
	searchMaxMatches = 50
	searchMaxFile    = 1 << 20 // skip files larger than 1 MiB
)

func (SearchFiles) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return "", fmt.Errorf("search_files: pattern is required")
	}
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return "", fmt.Errorf("search_files: bad pattern: %v", err)
	}
	root := a.Path
	if root == "" {
		root = "."
	}
	var glob *regexp.Regexp
	if a.Glob != "" {
		// The glob is matched against base names; translate the
		// shell-style pattern, failing loudly on anything filepath
		// cannot express.
		glob, err = regexp.Compile(globToPattern(a.Glob))
		if err != nil {
			return "", fmt.Errorf("search_files: bad glob: %v", err)
		}
	}

	var out []string
	matches, files := 0, 0
	truncated := false
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if glob != nil && !glob.MatchString(d.Name()) {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > searchMaxFile {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if isBinary(data) {
			return nil
		}
		fileHit := false
		for i, line := range strings.Split(string(data), "\n") {
			if !re.MatchString(line) {
				continue
			}
			if !fileHit {
				fileHit = true
				files++
			}
			matches++
			if len(out) < searchMaxMatches {
				out = append(out, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimRight(line, "\r")))
			} else {
				truncated = true
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("search_files: %w", err)
	}
	if matches == 0 {
		return "no matches", nil
	}
	if truncated {
		out = append(out, fmt.Sprintf("… %d more matches in %d files (narrow the pattern or path)",
			matches-searchMaxMatches, files))
	}
	return strings.Join(out, "\n"), nil
}

// isBinary reports whether data looks like binary content: a NUL
// byte in the first 8 KiB.
func isBinary(data []byte) bool {
	const sniff = 8192
	if len(data) > sniff {
		data = data[:sniff]
	}
	return strings.ContainsRune(string(data), 0)
}

// globToPattern translates a shell-style filename glob into an
// anchored RE2 pattern.
func globToPattern(g string) string {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range g {
		switch r {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '.', '(', ')', '|', '+', '[', ']', '{', '}', '^', '$', '\\':
			b.WriteString("\\" + string(r))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("$")
	return b.String()
}
