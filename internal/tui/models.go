package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/llm"
)

// fetchModels is the seam tests swap; production points at the llm
// package's live fetch.
var fetchModels = llm.FetchModels

// modelsFetchedMsg carries one provider's live model list back to the
// tea program.
type modelsFetchedMsg struct {
	provider string
	models   []llm.ModelInfo
	err      error
}

// providerConfigFor resolves a provider's wire config: an explicit
// models.json entry wins (catalog-merged by config.LoadModels), then
// the built-in catalog.
func (m *Model) providerConfigFor(name string) (config.ProviderConfig, bool) {
	if pc, ok := m.opt.Models.Providers[name]; ok && pc.BaseURL != "" {
		return pc, true
	}
	if spec, ok := config.BuiltInProviders[name]; ok {
		return config.ProviderConfig{BaseURL: spec.BaseURL, API: spec.API, APIKeyEnv: spec.APIKeyEnv}, true
	}
	return config.ProviderConfig{}, false
}

// allProviders lists every provider the pickers offer: the built-in
// catalog plus any models.json customs, sorted, explicit entries
// winning on collision (the catalog merge already did that).
func (m *Model) allProviders() []string {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for name := range m.opt.Models.Providers {
		add(name)
	}
	for name := range config.BuiltInProviders {
		add(name)
	}
	sort.Strings(names)
	return names
}

// providerDetail is the one-line hint under a provider row: whether
// its models can be fetched right now. Local servers need no key.
func (m *Model) providerDetail(name string, pc config.ProviderConfig) string {
	isLocal := pc.BaseURL == "" ||
		strings.Contains(pc.BaseURL, "localhost") || strings.Contains(pc.BaseURL, "127.0.0.1")
	if !isLocal && m.opt.KeyFor != nil {
		if _, ok := m.opt.KeyFor(name); !ok {
			return "no key — /login " + name
		}
	}
	return "enter to browse models"
}

// openModelsPicker implements /models: no argument lists every
// provider; an argument fetches that provider's models directly.
func (m *Model) openModelsPicker(arg string) tea.Cmd {
	if m.working {
		m.add(entry{kind: entryErr, text: "finish or interrupt the turn before switching models"})
		return nil
	}
	if arg != "" {
		return m.fetchModelsCmd(arg)
	}
	items := make([]pickerItem, 0, 16)
	for _, name := range m.allProviders() {
		pc, ok := m.providerConfigFor(name)
		if !ok {
			continue
		}
		items = append(items, pickerItem{
			Label: name, Detail: m.providerDetail(name, pc), Provider: name, Action: "fetch",
		})
	}
	m.picker = newPicker(pickerProviders, "browse models — pick a provider", items)
	m.picker.purpose = "models"
	return nil
}

// fetchModelsCmd fetches one provider's live model list off the UI
// goroutine; the arrival message opens (or discards) the picker.
func (m *Model) fetchModelsCmd(provider string) tea.Cmd {
	pc, ok := m.providerConfigFor(provider)
	if !ok {
		return func() tea.Msg {
			return modelsFetchedMsg{provider: provider,
				err: fmt.Errorf("unknown provider %q", provider)}
		}
	}
	var key string
	if m.opt.KeyFor != nil {
		key, _ = m.opt.KeyFor(provider)
	}
	m.add(entry{kind: entryDim, text: "fetching models from " + provider + "…"})
	api, base := pc.API, pc.BaseURL
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		models, err := fetchModels(ctx, api, base, key)
		return modelsFetchedMsg{provider: provider, models: models, err: err}
	}
}

// handleModelsFetched opens the model picker for a successful fetch.
// A result that arrives after the user moved on (another picker open,
// or none) is noted dimly instead of surprising them.
func (m *Model) handleModelsFetched(msg modelsFetchedMsg) {
	if msg.err != nil {
		m.add(entry{kind: entryErr,
			text: "could not list models from " + msg.provider + ": " + msg.err.Error()})
		return
	}
	if m.picker != nil || m.login != nil {
		m.add(entry{kind: entryDim,
			text: fmt.Sprintf("fetched %d models from %s — /models to browse", len(msg.models), msg.provider)})
		return
	}
	if len(msg.models) == 0 {
		m.add(entry{kind: entryErr, text: msg.provider + " lists no models"})
		return
	}
	// Sorted regardless of source, so the picker is stable.
	sort.Slice(msg.models, func(i, j int) bool { return msg.models[i].ID < msg.models[j].ID })
	items := make([]pickerItem, 0, len(msg.models))
	for _, mo := range msg.models {
		label := mo.ID
		if mo.DisplayName != "" && mo.DisplayName != mo.ID {
			label = mo.DisplayName + " (" + mo.ID + ")"
		}
		items = append(items, pickerItem{
			Label: label, Provider: msg.provider, Model: mo.ID,
		})
	}
	m.picker = newPicker(pickerCatalog, "models — "+msg.provider, items)
}

// openLoginPicker implements bare /login: every provider, one key at
// a time. /login <provider> skips the picker.
func (m *Model) openLoginPicker() {
	items := make([]pickerItem, 0, 16)
	for _, name := range m.allProviders() {
		if _, ok := m.providerConfigFor(name); !ok {
			continue
		}
		items = append(items, pickerItem{
			Label: name, Detail: "store an API key", Provider: name, Action: "login",
		})
	}
	m.picker = newPicker(pickerProviders, "login — pick a provider", items)
	m.picker.purpose = "login"
}
