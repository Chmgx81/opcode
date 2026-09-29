package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Glob finds files by pattern — the Read/Grep/Glob trio's
// third leg. Read-Only tier: discovery changes nothing, and
// read-only and plan modes can answer "where are the config files"
// without listing directories one by one.
type Glob struct{}

func (Glob) Name() string { return "glob" }

func (Glob) Description() string {
	return "Find files whose paths match a glob pattern (e.g. \"**/*.go\", \"internal/*/test.go\") and return them sorted, one per line. Patterns match paths relative to the search root (default: the working directory); ** crosses directory separators."
}

func (Glob) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern matched against paths relative to the root; ** crosses separators"},
			"path": {"type": "string", "description": "Directory to search under (default: the working directory)"}
		},
		"required": ["pattern"]
	}`)
}

func (Glob) Tier() Tier { return TierReadOnly }

const globMaxResults = 200

func (Glob) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return "", fmt.Errorf("glob: pattern is required")
	}
	root := a.Path
	if root == "" {
		root = "."
	}
	re, err := compileGlob(a.Pattern)
	if err != nil {
		return "", fmt.Errorf("glob: %v", err)
	}

	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel := path
		if r, err := filepath.Rel(root, path); err == nil {
			rel = r
		}
		if re.MatchString(filepath.ToSlash(rel)) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("glob: %w", err)
	}
	if len(out) == 0 {
		return "no matches", nil
	}
	sort.Strings(out)
	if len(out) > globMaxResults {
		extra := len(out) - globMaxResults
		out = append(out[:globMaxResults],
			fmt.Sprintf("… %d more matches (narrow the pattern)", extra))
	}
	return strings.Join(out, "\n"), nil
}

// compileGlob turns a shell-style glob into an anchored RE2 pattern.
// ** crosses directory separators; * and ? do not.
func compileGlob(g string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	i := 0
	for i < len(g) {
		if i+1 < len(g) && g[i] == '*' && g[i+1] == '*' {
			if i+2 < len(g) && g[i+2] == '/' {
				b.WriteString("(?:.*/)?")
				i += 3
				continue
			}
			b.WriteString(".*")
			i += 2
			continue
		}
		switch g[i] {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '.', '(', ')', '|', '+', '[', ']', '{', '}', '^', '$', '\\':
			b.WriteString("\\" + string(g[i]))
		default:
			b.WriteByte(g[i])
		}
		i++
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
