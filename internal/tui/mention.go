package tui

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The @-mention file picker: typing "@" in the composer opens a
// live-filtered list of project files. The query is the composer's
// trailing text after the last "@"; Enter inserts the highlighted
// path; Esc dismisses it until a new mention starts. Arrow keys move
// the highlight. This is the visible affordance that "@ attaches a
// file" previously lacked entirely — the mention only ever expanded
// on submit.

// atPattern matches a trailing mention: "@query" at the end of the
// composer, at a line start or after whitespace.
var atPattern = regexp.MustCompile(`(?:^|\s)@([^\s@]*)$`)

const (
	atMenuMax   = 8    // visible rows
	atFilesMax  = 1000 // walked files cap, to bound giant trees
	atFilesWalk = 6    // depth cap
)

// skipDirs are never offered as mentions.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	".hg": true, ".svn": true, "__pycache__": true, ".venv": true,
}

// projectFiles walks the working directory once per session and caches
// shallow-first paths relative to it.
func (m *Model) projectFiles() []string {
	if m.atFiles != nil {
		return m.atFiles
	}
	root := m.opt.Cwd
	if root == "" {
		return nil
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip, the picker must not fail
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.Count(rel, string(filepath.Separator)) >= atFilesWalk {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, rel)
		if len(files) >= atFilesMax {
			return fs.SkipAll
		}
		return nil
	})
	// Shallow paths first, alphabetical within a depth: "@" then Enter
	// most often wants the near file.
	sort.Slice(files, func(i, j int) bool {
		di, dj := strings.Count(files[i], string(filepath.Separator)),
			strings.Count(files[j], string(filepath.Separator))
		if di != dj {
			return di < dj
		}
		return files[i] < files[j]
	})
	m.atFiles = files
	return files
}

// refreshAtMenu recomputes the menu from the composer's trailing
// mention. Esc's dismissal holds until the query changes.
func (m *Model) refreshAtMenu() {
	m.atMenu = m.atMenu[:0]
	m.atIdx = 0
	if m.login != nil || m.picker != nil {
		return
	}
	v := m.composer.Value()
	loc := atPattern.FindStringSubmatchIndex(v)
	if loc == nil {
		// No trailing mention: the menu is closed, and a dismissal from
		// an earlier mention does not apply to the next one.
		m.atDismissed = false
		return
	}
	if m.atDismissed && loc[2]-1 == m.atDismissAt {
		return // this exact mention was Esc-dismissed
	}
	ql := strings.ToLower(v[loc[2]:loc[3]])
	for _, f := range m.projectFiles() {
		if strings.Contains(strings.ToLower(f), ql) {
			m.atMenu = append(m.atMenu, f)
			if len(m.atMenu) >= atMenuMax {
				break
			}
		}
	}
}

// completeAt replaces the trailing "@query" with "@path " and closes
// the menu.
func (m *Model) completeAt() {
	v := m.composer.Value()
	loc := atPattern.FindStringSubmatchIndex(v)
	if loc == nil || m.atIdx >= len(m.atMenu) {
		m.atMenu = nil
		return
	}
	path := m.atMenu[m.atIdx]
	v = v[:loc[2]-1] + "@" + path + " "
	m.composer.SetValue(v)
	m.composer.CursorEnd()
	m.atMenu = nil
	m.atDismissed = false
	m.resizeComposer()
}

// atMenuOpen reports whether the mention menu owns Enter/arrows/Esc.
func (m *Model) atMenuOpen() bool { return len(m.atMenu) > 0 }

// dismissAt closes the menu and remembers the query, so typing more
// of the same mention does not reopen it until a fresh "@" appears.
func (m *Model) dismissAt() {
	if loc := atPattern.FindStringSubmatchIndex(m.composer.Value()); loc != nil {
		m.atDismissAt = loc[2] - 1 // remember WHICH "@" was dismissed
		m.atDismissed = true
	}
	m.atMenu = nil
}

// shellMode reports whether the composer is a direct shell escape
// (leading "!"), which the composer renders with the amber indication.
func (m *Model) shellMode() bool {
	return strings.HasPrefix(m.composer.Value(), "!")
}

// syncComposerPrompt swaps the composer prompt glyph with the mode:
// "!" amber for shell escapes, "~" otherwise.
func (m *Model) syncComposerPrompt() {
	if m.shellMode() {
		m.composer.Prompt = GlyphWarn + " "
	} else {
		m.composer.Prompt = GlyphPrompt + " "
	}
}
