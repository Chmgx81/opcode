package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/Chmgx81/opcode/internal/safe"
	"github.com/Chmgx81/opcode/internal/tools"
)

// diffLineCap bounds the rendered diff: a huge generated diff must
// not flood the transcript, and the footer says what was cut.
const diffLineCap = 400

// showDiff answers "what changed?" — the working tree's git changes
// rendered into the transcript with the verdict colors the edit_file
// results already use. User-invoked and read-only like the ! shell
// escape: no model round trip, no permission prompt, no state change.
func (m *Model) showDiff() {
	// Inside a repo at all? rev-parse answers and its stderr names the
	// difference between "not a repo" and "git is missing".
	out, err := m.runGit("rev-parse --is-inside-work-tree")
	if err != nil {
		if strings.Contains(out, "not a git repository") {
			m.add(entry{kind: entryDim, text: "not a git repository — /diff shows git changes"})
			return
		}
		if strings.Contains(out, "command not found") || strings.Contains(out, "not found") {
			m.add(entry{kind: entryErr, text: "git is not installed — /diff needs it"})
			return
		}
		m.add(entry{kind: entryErr, text: strings.TrimSpace(out)})
		return
	}

	// Untracked files never appear in git diff; status lists them so
	// the review does not silently miss whole new files.
	var rows []string
	if st, err := m.runGit("status --porcelain"); err == nil {
		for _, line := range strings.Split(strings.TrimRight(st, "\n"), "\n") {
			if strings.HasPrefix(line, "??") {
				rows = append(rows, dimStyle.Render(fmt.Sprintf("  ? %s", strings.TrimSpace(line[2:]))))
			}
		}
	}

	diff, err := m.runGit("diff --no-color HEAD")
	if err != nil {
		// A repo with no commits yet has no HEAD to diff against; the
		// plain form shows staged and unstaged against the empty tree.
		var fallbackErr error
		diff, fallbackErr = m.runGit("diff --no-color")
		if fallbackErr != nil {
			m.add(entry{kind: entryErr, text: strings.TrimSpace(diff)})
			return
		}
	}

	body := renderDiffBody(diff)
	if len(body) == 0 && len(rows) == 0 {
		m.add(entry{kind: entryDim, text: "working tree clean"})
		return
	}
	head := accentStyle.Render(GlyphBullet + " diff")
	if len(body) > 0 {
		head += dimStyle.Render("  — working tree vs HEAD")
	} else {
		head += dimStyle.Render("  — nothing tracked changed")
	}
	all := append([]string{head}, body...)
	if len(rows) > 0 {
		all = append(all, dimStyle.Render(fmt.Sprintf("  %d untracked (not in the diff):", len(rows))))
		all = append(all, rows...)
	}
	m.add(entry{kind: entryDiff, text: strings.Join(all, "\n")})
}

// runGit runs one read-only git query against the project directory
// through the same bash path the ! shell escape uses (sandboxed,
// bounded, combined output) and sanitizes what comes back — filenames
// and file content are untrusted display text like any tool result.
func (m *Model) runGit(args string) (string, error) {
	cmd := "git -C " + shellQuote(m.opt.Cwd) + " " + args
	out, err := (tools.Bash{}).Execute(context.Background(), `{"command": `+mustJSON(cmd)+`}`)
	return safe.Text(out), err
}

// shellQuote single-quotes a path for bash, escaping the one character
// a path could carry that would otherwise end the quote.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// renderDiffBody colors git's unified diff with the verdict tokens:
// additions succeed, deletions danger, headers chrome. Verdict colors,
// not syntax tokens — git's diff already tells the reader what
// matters, and the cap keeps a monster diff from flooding the view.
func renderDiffBody(diff string) []string {
	diff = strings.TrimRight(diff, "\n")
	if diff == "" {
		return nil
	}
	lines := strings.Split(diff, "\n")
	if len(lines) > diffLineCap {
		extra := len(lines) - diffLineCap
		lines = lines[:diffLineCap]
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  … %d more lines", extra)))
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "diff --git"):
			out = append(out, boldStyle.Render(l))
		case strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file mode"),
			strings.HasPrefix(l, "deleted file mode"), strings.HasPrefix(l, "Binary files"):
			out = append(out, dimStyle.Render("  "+l))
		case strings.HasPrefix(l, "---"), strings.HasPrefix(l, "+++"):
			out = append(out, subtleStyle.Render(l))
		case strings.HasPrefix(l, "@@"):
			out = append(out, infoStyle.Render(l))
		case strings.HasPrefix(l, "+"):
			out = append(out, okStyle.Render(l))
		case strings.HasPrefix(l, "-"):
			out = append(out, dangerStyle.Render(l))
		default:
			out = append(out, dimStyle.Render(l))
		}
	}
	return out
}
