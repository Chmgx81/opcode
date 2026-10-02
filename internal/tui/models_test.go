package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chmgx81/opcode/internal/config"
	"github.com/Chmgx81/opcode/internal/llm"
	"github.com/Chmgx81/opcode/internal/orchestrator"
	"github.com/Chmgx81/opcode/internal/tools"
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

// A model picked in the UI must survive a restart, exactly like a key
// does: config.json keeps the model, models.json keeps the provider —
// a model id alone is meaningless without the provider that serves it.
func TestSwitchModelPersistsProviderAndModel(t *testing.T) {
	m, switched := modelPickerTestModel(t)
	// Pre-existing files with values a persistence rewrite must keep.
	if err := os.WriteFile(filepath.Join(m.opt.OpcodeHome, "config.json"),
		[]byte(`{"model":"old-model","theme":"green","future_key":[1,2]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.opt.OpcodeHome, "models.json"),
		[]byte(`{"providers":{"fixture":{"base_url":"http://127.0.0.1:9/v1"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	m.switchModel("mistral", "mistral-small-latest")

	if got := *switched; len(got) != 1 || got[0] != "mistral/mistral-small-latest" {
		t.Fatalf("switch callback saw %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(m.opt.OpcodeHome, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["model"] != "mistral-small-latest" {
		t.Errorf("config.json model = %v", cfg["model"])
	}
	if cfg["theme"] != "green" || cfg["future_key"] == nil {
		t.Errorf("unrelated config keys lost: %v", cfg)
	}
	raw, err = os.ReadFile(filepath.Join(m.opt.OpcodeHome, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var models map[string]any
	if err := json.Unmarshal(raw, &models); err != nil {
		t.Fatal(err)
	}
	if models["default_provider"] != "mistral" {
		t.Errorf("models.json default_provider = %v", models["default_provider"])
	}
	if models["providers"] == nil {
		t.Errorf("models.json providers lost: %v", models)
	}
}

// A first run with no model anywhere: sending must not start a turn
// that 400s at the provider — the picker opens instead, and the note
// says why.
func TestSendWithoutModelOpensPickerInsteadOfTurn(t *testing.T) {
	m, _ := modelPickerTestModel(t)
	m.opt.Model = ""
	before := len(m.entries)

	m.composer.SetValue("hello?")
	m.submitInput(false)

	if m.working {
		t.Fatal("a turn started with no model configured")
	}
	if m.picker == nil || len(m.picker.items) == 0 {
		t.Fatal("the model picker did not open")
	}
	found := false
	for _, e := range m.entries[before:] {
		if strings.Contains(e.text, "no model picked yet") {
			found = true
		}
	}
	if !found {
		t.Errorf("no explanation entry; entries after submit: %d", len(m.entries)-before)
	}
}

// The onboarding itself: a Model built with no model shows the welcome
// steps and already has the provider picker open, so step one is on
// screen without reading any docs.
func TestFirstRunOpensOnboardingPicker(t *testing.T) {
	dir := t.TempDir()
	fp := &scriptedProvider{}
	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	orch := orchestrator.New(fp, "", "sys", &reg, &tools.Gate{})
	orch.SetMode(tools.ModeBuild)
	m := New(Options{
		Orch: orch, Model: "", Mode: tools.ModeBuild, Cwd: dir, OpcodeHome: dir,
		ProviderName: "openrouter", BaseURL: "http://example.test/v1",
		AuditPath: filepath.Join(dir, "audit.jsonl"), Animations: true,
		Models: config.ModelsConfig{DefaultProvider: "openrouter"},
	})
	m.width, m.height = 80, 30

	welcomed, pickerOpen := false, false
	for _, e := range m.entries {
		if strings.Contains(e.text, "welcome") {
			welcomed = true
		}
	}
	if m.picker != nil && len(m.picker.items) > 0 {
		pickerOpen = true
	}
	if !welcomed {
		t.Error("no welcome entries on a first run")
	}
	if !pickerOpen {
		t.Error("the provider picker did not open on a first run")
	}
	// The header must not render an empty slot where the model goes.
	m.View()
	if strings.Contains(m.View(), " · build") && !strings.Contains(m.View(), "no model yet") {
		t.Error("header shows an empty model slot")
	}
}

// TestModelPickerIsTheHub: /model lists what a user can actually
// reach — models.json choices first (the active one marked), then
// providers with a resolvable key whose live list is one enter away,
// then providers without one, each naming the /login that unlocks it.
// The first-run user who has only stored a key must not land on a
// dead end about models.json.
func TestModelPickerIsTheHub(t *testing.T) {
	m, _ := modelPickerTestModel(t)
	m.openModelPicker()
	if m.picker == nil {
		t.Fatal("/model opened no picker")
	}
	var fetch, login, pinned int
	var anthropic pickerItem
	for _, it := range m.picker.items {
		switch it.Action {
		case "fetch":
			fetch++
			if !strings.Contains(it.Detail, "live models") {
				t.Errorf("fetch row %q does not say what enter does: %q", it.Label, it.Detail)
			}
		case "login":
			login++
			if !strings.Contains(it.Detail, "/login "+it.Label) {
				t.Errorf("login row %q does not name its command: %q", it.Label, it.Detail)
			}
			if it.Label == "anthropic" {
				anthropic = it
			}
		default:
			pinned++ // a models.json model row, or a pinned provider
		}
	}
	// The fixture pins two models (test-model, other-model) under
	// openrouter; "keyed" resolves through the wired KeyFor but is
	// not pinned, so it offers its live list.
	if pinned != 2 {
		t.Errorf("pinned rows = %d, want the two models.json models", pinned)
	}
	if fetch == 0 {
		t.Error("no keyed provider offers its live list")
	}
	if login == 0 {
		t.Error("no unkeyed provider names /login")
	}
	if anthropic.Provider == "" {
		t.Fatal("anthropic has no login row")
	}
	// Enter on an unkeyed provider starts the key capture — the hub
	// is the whole journey, not a map of it.
	m.picker = nil
	m.pickerSelect(anthropic)
	if m.login == nil || m.login.provider != "anthropic" {
		t.Errorf("enter on an unkeyed provider started %+v, want the anthropic login", m.login)
	}
}

// TestLoginContinuesToModels: a stored key exists to pick a model
// with. The login's success fetches that provider's live list itself,
// and the fetch's arrival opens the catalog picker — the resolver's
// startup snapshot never sees the write, so the session's own copy is
// what makes the fetch work without a restart.
func TestLoginContinuesToModels(t *testing.T) {
	m, _ := modelPickerTestModel(t)
	m.beginLogin("anthropic")
	m.composer.SetValue("sk-a-test-key")
	cmd := m.submitLogin()
	if cmd == nil {
		t.Fatal("a stored key did not continue to the model list")
	}
	if _, ok := m.sessionKeyFor("anthropic"); !ok {
		t.Error("the stored key is invisible to the session until a restart")
	}
	restore := swapFetch(func(ctx context.Context, api, baseURL, key string) ([]llm.ModelInfo, error) {
		if key != "sk-a-test-key" {
			t.Errorf("fetch used key %q, want the one just stored", key)
		}
		return []llm.ModelInfo{{ID: "claude-x"}}, nil
	})
	defer restore()
	msg := cmd()
	fetched, ok := msg.(modelsFetchedMsg)
	if !ok {
		t.Fatalf("cmd = %T, want a models fetch", msg)
	}
	if fetched.err != nil {
		t.Fatalf("the fetch failed: %v", fetched.err)
	}
	m.handleModelsFetched(fetched)
	if m.picker == nil || m.picker.kind != pickerCatalog {
		t.Fatalf("the catalog picker did not open: %+v", m.picker)
	}
}

// TestSwitchToUnkeyedProviderWarns: the switch lands, but the user
// finds out at the switch — not at the first send — that the
// provider has no key, with the fix named.
func TestSwitchToUnkeyedProviderWarns(t *testing.T) {
	m, switched := modelPickerTestModel(t)
	m.switchModel("anthropic", "claude-x")
	if len(*switched) == 0 || (*switched)[0] != "anthropic/claude-x" {
		t.Fatalf("the switch did not land: %v", *switched)
	}
	tr := m.transcript()
	if !strings.Contains(tr, "no api key for anthropic") || !strings.Contains(tr, "/login anthropic") {
		t.Errorf("the switch did not warn about the missing key:\n%s", tr)
	}
}
