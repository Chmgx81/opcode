package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/safe"
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
	// hint is set by the checks that never left the process (no key,
	// unknown provider). Their message is already in plain words and
	// names its own next step, so it bypasses errorWithNextStep — whose
	// matcher would read "no key for openrouter" as a bad model name
	// and answer with the wrong advice.
	hint string
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
	isLocal := pc.BaseURL == "" || config.IsLocalBaseURL(pc.BaseURL)
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
		// Browsing does not change the model, but the picker takes the
		// keyboard and a turn in flight does not need one: say the
		// actual reason rather than a switch that is not happening.
		m.add(entry{kind: entryErr, text: "finish or interrupt the turn before browsing models"})
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
	if len(items) == 0 {
		m.add(entry{kind: entryDim,
			text: "no providers known — add one to models.json, or /models <provider> to fetch one by name"})
		return nil
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
				err:  fmt.Errorf("unknown provider %q", provider),
				hint: "unknown provider " + provider + " — /models lists the known ones"}
		}
	}
	var key string
	if m.opt.KeyFor != nil {
		k, ok := m.opt.KeyFor(provider)
		// Saying so up front beats a doomed request that comes back
		// as a bare 401 (audit C12).
		if !ok && !config.IsLocalBaseURL(pc.BaseURL) {
			return func() tea.Msg {
				return modelsFetchedMsg{provider: provider,
					err: fmt.Errorf("no key for %s", provider),
					hint: "no api key for " + provider + " — /login " + provider +
						" stores one, /doctor checks the resolution chain"}
			}
		}
		key = k
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
		// A local check (no key, unknown provider) already says what
		// to do in plain words, and passes that copy as the hint: the
		// next step is a key, not a retry, and the error mapper would
		// answer a missing key with "the provider does not serve that
		// model". A live fetch's failure IS provider jargon — an HTTP
		// status, a body — so it goes through the same mapper a turn
		// error does, and through safe.Text like all untrusted text.
		if msg.hint != "" {
			m.add(entry{kind: entryErr, text: msg.hint})
			return
		}
		m.add(entry{kind: entryErr, text: errorWithNextStep(msg.provider,
			"could not list models from "+msg.provider+": "+safe.Text(msg.err.Error()))})
		return
	}
	if m.picker != nil || m.login != nil {
		m.add(entry{kind: entryDim,
			text: fmt.Sprintf("fetched %d models from %s — close this view, then /models to browse",
				len(msg.models), msg.provider)})
		return
	}
	if len(msg.models) == 0 {
		// A live list that came back empty is a fact about the
		// provider, not a failure of tilde, and the way out is the
		// same picker over a different provider.
		m.add(entry{kind: entryDim, text: msg.provider +
			" listed no models — /models picks another provider, or set one by name with /model <model>"})
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
	if len(items) == 0 {
		m.add(entry{kind: entryDim, text: "no providers known — add one to models.json, " +
			"or /login <provider> by name to store a key for it"})
		return
	}
	m.picker = newPicker(pickerProviders, "login — pick a provider", items)
	m.picker.purpose = "login"
}
