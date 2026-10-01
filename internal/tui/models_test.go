package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/llm"
)

// modelPickerTestModel builds a Model with the catalog-picker seams
// wired: a captured SwitchModel and a KeyFor that answers for
// "keyed" providers only.
func modelPickerTestModel(t *testing.T) (*Model, *[]string) {
	t.Helper()
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	switched := &[]string{}
	m.opt.SwitchModel = func(provider, model string) error {
		*switched = append(*switched, provider+"/"+model)
		return nil
	}
	m.opt.KeyFor = func(provider string) (string, bool) {
		if provider == "keyed" || provider == "fixture" || provider == "openrouter" {
			return "k", true
		}
		return "", false
	}
	return m, switched
}

func TestModelsPickerListsAllProviders(t *testing.T) {
	m, _ := modelPickerTestModel(t)
	// A custom provider joins the catalog names.
	m.opt.Models.Providers["fixture"] = config.ProviderConfig{BaseURL: "http://127.0.0.1:9/v1"}

	m.openModelsPicker("")
	if m.picker == nil || len(m.picker.items) == 0 {
		t.Fatal("provider picker did not open")
	}
	var labels []string
	hints := map[string]string{}
	for _, it := range m.picker.items {
		if it.Action != "fetch" {
			t.Errorf("provider row %q action = %q", it.Label, it.Action)
		}
		labels = append(labels, it.Label)
		hints[it.Label] = it.Detail
	}
	for _, want := range []string{"anthropic", "fixture", "mistral", "ollama", "openrouter"} {
		if !contains(labels, want) {
			t.Errorf("provider %s missing from %v", want, labels)
		}
	}
	// Keyless remote providers say so; locals and keyed ones invite.
	if !strings.HasPrefix(hints["groq"], "no key") {
		t.Errorf("groq hint = %q, want the no-key /login hint", hints["groq"])
	}
	if !strings.HasPrefix(hints["ollama"], "enter") {
		t.Errorf("ollama hint = %q, want browse (local needs no key)", hints["ollama"])
	}
	if !strings.HasPrefix(hints["openrouter"], "enter") {
		t.Errorf("openrouter hint = %q, want browse (key resolves)", hints["openrouter"])
	}
}

func TestModelsFetchArrivalAndSelect(t *testing.T) {
	m, switched := modelPickerTestModel(t)

	// The fetch lands: the model picker opens with the provider's list.
	m.handleModelsFetched(modelsFetchedMsg{provider: "fixture", models: []llm.ModelInfo{
		{ID: "mini"}, {ID: "big", DisplayName: "The Big One"},
	}})
	if m.picker == nil || m.picker.kind != pickerCatalog {
		t.Fatal("model picker did not open after fetch")
	}
	if len(m.picker.items) != 2 || m.picker.items[0].Model != "big" {
		t.Fatalf("items = %+v (sorted by id)", m.picker.items)
	}
	if m.picker.items[1].Label != "mini" {
		t.Errorf("plain ids keep their label: %+v", m.picker.items[1])
	}

	// Selecting a model switches provider + model.
	m.pickerSelect(m.picker.items[1])
	if len(*switched) != 1 || (*switched)[0] != "fixture/mini" {
		t.Errorf("switch = %v", *switched)
	}
	// The switch refreshed the TUI's wire state for catalog providers.
	if m.opt.ProviderName != "fixture" || m.opt.Model != "mini" {
		t.Errorf("opt state = %s/%s", m.opt.ProviderName, m.opt.Model)
	}
}

func TestModelsFetchErrorAndStaleArrival(t *testing.T) {
	m, _ := modelPickerTestModel(t)

	// The pre-flight no-key case: the copy names the provider and the
	// next step, and is NOT the model advice. A bare "no key" routed
	// through the generic mapper used to answer "the provider does not
	// serve that model" — advice about a model nobody asked about.
	m.handleModelsFetched(modelsFetchedMsg{provider: "groq",
		err: errors.New("no key"), hint: "no api key for groq — /login groq stores one"})
	tr := m.transcript()
	if !strings.Contains(tr, "groq") || !strings.Contains(tr, "/login groq") {
		t.Errorf("no-key error does not name the next step: %s", tr)
	}
	if strings.Contains(tr, "does not serve that model") {
		t.Errorf("a missing key was reported as a bad model: %s", tr)
	}

	// A live fetch failure is provider jargon, and gets the same
	// next-step treatment a turn error gets.
	m.entries = nil
	m.handleModelsFetched(modelsFetchedMsg{provider: "groq",
		err: errors.New("401 Unauthorized")})
	tr = m.transcript()
	if !strings.Contains(tr, "groq") || !strings.Contains(tr, "next:") {
		t.Errorf("fetch error lacks a next step: %s", tr)
	}

	// A fetch that lands while another picker is open is noted, never
	// a surprise picker swap.
	m.picker = newPicker(pickerSessions, "busy", nil)
	m.handleModelsFetched(modelsFetchedMsg{provider: "mistral",
		models: []llm.ModelInfo{{ID: "x"}}})
	if m.picker.kind != pickerSessions {
		t.Error("stale arrival replaced the open picker")
	}
	if !strings.Contains(m.transcript(), "fetched 1 models from mistral") {
		t.Errorf("stale-arrival note missing: %s", m.transcript())
	}
}

func TestFetchModelsCmdUsesSeam(t *testing.T) {
	m, _ := modelPickerTestModel(t)
	m.opt.Models.Providers["fixture"] = config.ProviderConfig{BaseURL: "http://127.0.0.1:9/v1"}
	called := false
	restore := swapFetch(func(ctx context.Context, api, baseURL, key string) ([]llm.ModelInfo, error) {
		called = true
		if baseURL != "http://127.0.0.1:9/v1" {
			t.Errorf("baseURL = %q", baseURL)
		}
		return []llm.ModelInfo{{ID: "one"}}, nil
	})
	defer restore()

	cmd := m.fetchModelsCmd("fixture")
	msg := cmd()
	fetched, ok := msg.(modelsFetchedMsg)
	if !ok || !called {
		t.Fatalf("cmd = %T, called = %v", msg, called)
	}
	if fetched.provider != "fixture" || len(fetched.models) != 1 {
		t.Errorf("msg = %+v", fetched)
	}
}

func TestLoginPickerStartsMaskedFlow(t *testing.T) {
	m, _ := modelPickerTestModel(t)
	m.openLoginPicker()
	if m.picker == nil || len(m.picker.items) == 0 {
		t.Fatal("login picker did not open")
	}
	// Selecting a provider begins the masked key capture for it.
	var anthropic pickerItem
	for _, it := range m.picker.items {
		if it.Label == "anthropic" {
			anthropic = it
		}
	}
	if anthropic.Provider == "" {
		t.Fatal("anthropic missing from the login picker")
	}
	m.pickerSelect(anthropic)
	if m.login == nil || m.login.provider != "anthropic" {
		t.Fatalf("login flow = %+v", m.login)
	}
}

// swapFetch fakes the live model fetch for a test.
func swapFetch(fake func(ctx context.Context, api, baseURL, key string) ([]llm.ModelInfo, error)) func() {
	orig := fetchModels
	fetchModels = fake
	return func() { fetchModels = orig }
}
