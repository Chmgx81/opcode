package tui

import (
	"strings"
)

// A reusable filterable overlay picker, the interaction behind
// /model, /sessions, /theme, /models, and /login: type to filter,
// arrows (or ctrl+n/p) to move, Enter to select, Esc to close. Each
// command builds its items and interprets the
// selection; the picker owns only the list mechanics.

type pickerItem struct {
	Label  string
	Detail string
	// Exactly one payload field is set per picker kind.
	Provider string // /model: provider name
	Model    string // /model: model name; empty keeps the current model
	Path     string // /sessions: session file path
	Theme    string // /theme: theme name
	Action   string // "" select; "fetch" browse a provider's models; "login" store its key
}

type pickerKind int

const (
	pickerModels pickerKind = iota
	pickerSessions
	pickerProviders // /models step 1 and /login: choose a provider
	pickerCatalog   // /models step 2: one provider's live model list
	pickerThemes    // /theme: choose the palette
	pickerModes     // /mode: choose the permission posture
)

type picker struct {
	kind    pickerKind
	purpose string // disambiguates pickerProviders: "models" or "login"
	title   string
	query   string // filter text, typed while open
	items   []pickerItem
	matched []pickerItem // items passing the current query
	idx     int
}

func newPicker(kind pickerKind, title string, items []pickerItem) *picker {
	p := &picker{kind: kind, title: title, items: items}
	p.applyFilter()
	return p
}

// applyFilter recomputes matched from items + query. Substring,
// case-insensitive, across label and detail; empty query matches all.
func (p *picker) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(p.query))
	p.matched = p.matched[:0]
	for _, it := range p.items {
		if q == "" ||
			strings.Contains(strings.ToLower(it.Label+" "+it.Detail), q) {
			p.matched = append(p.matched, it)
		}
	}
	if p.idx >= len(p.matched) {
		p.idx = maxInt(len(p.matched)-1, 0)
	}
}

func (p *picker) current() (pickerItem, bool) {
	if p.idx < 0 || p.idx >= len(p.matched) {
		return pickerItem{}, false
	}
	return p.matched[p.idx], true
}

func (p *picker) up() {
	if len(p.matched) == 0 {
		return
	}
	p.idx--
	if p.idx < 0 {
		p.idx = len(p.matched) - 1
	}
}

func (p *picker) down() {
	if len(p.matched) == 0 {
		return
	}
	p.idx++
	p.idx %= len(p.matched)
}

// keyInput is a small adapter over tea.KeyMsg so picker handling stays
// testable without a terminal.
type keyInput struct {
	name      string // canonical key name
	printable string // text to append, for character keys
}

// handlePickerKey routes one keypress while a picker is open. It returns
// the selected item when Enter chose one. ctrl+c is deliberately not
// handled here so it still quits the program.
func (m *Model) handlePickerKey(k keyInput) (selected *pickerItem, handled bool) {
	p := m.picker
	if p == nil {
		return nil, false
	}
	switch k.name {
	case "enter":
		it, ok := p.current()
		m.picker = nil
		if !ok {
			return nil, true
		}
		return &it, true
	case "esc":
		m.picker = nil
		return nil, true
	case "up", "ctrl+p":
		p.up()
		return nil, true
	case "down", "ctrl+n":
		p.down()
		return nil, true
	case "backspace":
		if n := len(p.query); n > 0 {
			p.query = p.query[:n-1]
			p.applyFilter()
		}
		return nil, true
	}
	if k.printable != "" {
		p.query += k.printable
		p.applyFilter()
		return nil, true
	}
	return nil, true
}
