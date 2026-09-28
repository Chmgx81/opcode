package config

import (
	"os"
	"path/filepath"
	"strings"
)

// agentsFileCap bounds one context file's contribution to the system
// prompt: an AGENTS.md is not a place to store a database.
const agentsFileCap = 32 * 1024

// AgentsContext composes the hierarchical instruction files
// (architecture doc Section 5, step 3): the user's ~/.tilde/AGENTS.md,
// then every directory from the filesystem root down to the working
// directory, one file per directory — AGENTS.override.md beats
// AGENTS.md, which beats CLAUDE.md. The text is inert: it loads
// regardless of project trust, exactly like the spec says, and is
// treated with the caution of any external content. More specific
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
func readFirst(dir string, names ...string) (string, string, bool) {
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if len(data) > agentsFileCap {
			data = append(data[:agentsFileCap:agentsFileCap], []byte("\n... (file truncated)")...)
		}
		return name, string(data), true
	}
	return "", "", false
}
