package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// agentsFileCap bounds one context file's contribution to the system
// prompt: an AGENTS.md is not a place to store a database.
const agentsFileCap = 32 * 1024

// AgentsContext composes the hierarchical instruction files
// (architecture doc Section 5, step 3): the user's ~/.tilde/AGENTS.md,
// then every directory from the filesystem root down to the working
// directory, one file per directory — AGENTS.override.md beats
// AGENTS.md, which beats CLAUDE.md. The text loads regardless of
// project trust, exactly like the spec says, and is treated with the
// caution of any external content: cloning a repository brings its
// AGENTS.md along, so it is attacker-writable content that the user
// never typed. The caller fences it as untrusted data. More specific
// files come last so they win on conflict.
func AgentsContext(userDir, cwd string) string {
	var files []string

	if name, data, ok := readFirst(userDir, "AGENTS.override.md", "AGENTS.md"); ok {
		files = append(files, "# "+filepath.Join(userDir, name)+"\n"+data)
	}

	// Walk root -> cwd, so the working directory's file lands last.
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	// Build the chain of directories from the root down to cwd.
	var chain []string
	for dir := abs; ; dir = filepath.Dir(dir) {
		chain = append(chain, dir)
		if dir == string(filepath.Separator) || dir == "" {
			break
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		dir := chain[i]
		if dir == userDir {
			continue // already included as the user-level file
		}
		if name, data, ok := readFirst(dir, "AGENTS.override.md", "AGENTS.md", "CLAUDE.md"); ok {
			files = append(files, "# "+filepath.Join(dir, name)+"\n"+data)
		}
	}

	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Project and user instructions (advisory):\n\n")
	b.WriteString(strings.Join(files, "\n\n"))
	return b.String()
}

// readFirst returns the first existing file among names in dir, with
// its name, capped at agentsFileCap bytes with a truncation note.
// Unreadable files are skipped, not fatal.
//
// The read is bounded rather than "read it all, then slice": the file
// is repo content, so its size is the author's choice, and an
// unbounded ReadFile here is an OOM the user cannot cause or predict.
// The cap lands on a rune boundary so the prompt never carries a
// half-character into the model's tokenizer.
func readFirst(dir string, names ...string) (string, string, bool) {
	for _, name := range names {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, agentsFileCap+1))
		f.Close()
		if err != nil && len(data) == 0 {
			continue
		}
		if len(data) > agentsFileCap {
			data = data[:agentsFileCap]
			// utf8.RuneStart reports whether a byte can BEGIN a rune,
			// so testing the last byte walks backwards over a split
			// rune only while it is a continuation. ValidUTF8 is the
			// honest check — a file that was already invalid UTF-8
			// before the cap would otherwise report corrupt forever.
			for len(data) > 0 && !utf8.Valid(data) {
				data = data[:len(data)-1]
			}
			data = append(data, "\n... (file truncated)"...)
		}
		return name, string(data), true
	}
	return "", "", false
}
