package tui

// The /theme command: the palette as a user choice. The picker
// previews live (moving the highlight re-skins the session in
// place), Esc restores the theme that was active when the picker
// opened, and Enter persists through Options.SetTheme.

// openThemePicker builds the theme list. The active theme is marked
// in the detail so the picker opens showing the truth.
func (m *Model) openThemePicker() {
	var items []pickerItem
	for _, name := range ThemeNames() {
		detail := ThemeDesc(name)
		if name == m.curTheme {
			detail += " — active"
		}
		items = append(items, pickerItem{Label: name, Detail: detail, Theme: name})
	}
	m.themeBackup = m.curTheme
	m.picker = newPicker(pickerThemes, "theme", items)
}

// applyTheme switches the palette and drops every render cache that
// embeds the old one: the per-entry markdown caches and the
// in-flight stream cache. Committed native scrollback cannot be
// re-rendered — those lines keep their original colors.
func (m *Model) applyTheme(name string) bool {
	if !applyThemeName(name) {
		return false
	}
	m.curTheme = name
	for i := range m.entries {
		m.entries[i].rendered = nil
		m.entries[i].renderedW = 0
	}
	m.streamRendered, m.streamRenderedLen, m.streamRenderedW = nil, 0, 0
	// The composer copies its prompt and placeholder styles by value
	// at construction (and on every shell-mode toggle), so it keeps
	// rendering whatever palette was live then — re-copy them or the
	// prompt glyph survives the switch in the old color.
	m.syncComposerPrompt()
	m.composer.FocusedStyle.Placeholder = subtleStyle
	m.composer.BlurredStyle.Placeholder = subtleStyle
	return true
}

// selectTheme applies a chosen theme and persists it. Without a
// SetTheme writer the switch is honest about being session-only.
func (m *Model) selectTheme(name string) {
	if !m.applyTheme(name) {
		m.add(entry{kind: entryErr, text: "unknown theme: " + name + " (" + themeListForError() + ")"})
		return
	}
	m.themeBackup = ""
	if m.opt.SetTheme == nil {
		m.add(entry{kind: entryDim, text: "theme: " + name + " (this session only)"})
		return
	}
	if err := m.opt.SetTheme(name); err != nil {
		m.add(entry{kind: entryErr, text: "theme switched, but not saved: " + err.Error()})
		return
	}
	m.add(entry{kind: entryDim, text: "theme: " + name + " — saved to config.json"})
}

// themePreview runs after every keypress while (or just after) a
// picker is open. While the theme picker is open, the highlighted
// theme renders live; once it closes without a selection, the backup
// restores — a cancelled preview must never strand its palette.
func (m *Model) themePreview() {
	if m.picker != nil {
		if m.picker.kind != pickerThemes {
			return
		}
		if it, ok := m.picker.current(); ok && it.Theme != m.curTheme {
			m.applyTheme(it.Theme)
		}
		return
	}
	if m.themeBackup == "" {
		return
	}
	if m.themeBackup != m.curTheme {
		m.applyTheme(m.themeBackup)
		m.add(entry{kind: entryDim, text: "theme restored — " + m.themeBackup})
	}
	m.themeBackup = ""
}

// themeListForError names the choices in an unknown-theme error.
func themeListForError() string {
	list := "valid: "
	for i, name := range ThemeNames() {
		if i > 0 {
			list += ", "
		}
		list += name
	}
	return list
}

// handleThemeCommand backs /theme: bare opens the picker, a name
// switches directly.
func (m *Model) handleThemeCommand(arg string) {
	if arg == "" {
		m.openThemePicker()
		return
	}
	m.selectTheme(arg)
}
