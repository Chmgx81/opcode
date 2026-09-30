package llm

import (
	"encoding/json"
	"testing"
)

// TestOpenAIReasoningEffortWire: the OpenAI-compatible request carries
// reasoning_effort exactly when the knob is set — the provider
// default (empty) must not send the field at all.
func TestOpenAIReasoningEffortWire(t *testing.T) {
	req := ChatRequest{Model: "m", ReasoningEffort: "high"}
	body, err := json.Marshal(toWire(req))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatal(err)
	}
	if probe["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v, want high", probe["reasoning_effort"])
	}

	req.ReasoningEffort = ""
	body, _ = json.Marshal(toWire(req))
	probe = nil
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatal(err)
	}
	if _, present := probe["reasoning_effort"]; present {
		t.Errorf("unset effort still sent: %v", probe["reasoning_effort"])
	}
}

// TestAnthropicThinkingWire: the Anthropic request maps the knob's
// names to thinking budgets, and omits the field entirely when unset
// — a model without extended thinking must never see it.
func TestAnthropicThinkingWire(t *testing.T) {
	cases := []struct {
		effort string
		budget float64
	}{
		{"low", 1024},
		{"medium", 8192},
		{"high", 16384},
	}
	for _, c := range cases {
		ar := toAnthropic(ChatRequest{Model: "claude", ReasoningEffort: c.effort})
		if ar.Thinking == nil {
			t.Errorf("%s: thinking field omitted", c.effort)
			continue
		}
		if ar.Thinking.Type != "enabled" || float64(ar.Thinking.BudgetTokens) != c.budget {
			t.Errorf("%s: thinking = %+v, want enabled/%.0f", c.effort, ar.Thinking, c.budget)
		}
	}
	ar := toAnthropic(ChatRequest{Model: "claude"})
	if ar.Thinking != nil {
		t.Errorf("unset effort still sent: %+v", ar.Thinking)
	}
	body, _ := json.Marshal(ar)
	var probe map[string]any
	_ = json.Unmarshal(body, &probe)
	if _, present := probe["thinking"]; present {
		t.Error("the wire carries a thinking key for an unset effort")
	}
}

// TestAnthropicThinkingFitsUnderMaxTokens: Anthropic rejects a
// request whose budget_tokens is not strictly less than max_tokens.
// The medium and high budgets used to exceed the default max_tokens,
// so alt+. turned every Claude request into a 400.
func TestAnthropicThinkingFitsUnderMaxTokens(t *testing.T) {
	for _, eff := range []string{"low", "medium", "high"} {
		for _, max := range []int{0, 1024, 4096, anthropicDefaultMaxTokens} {
			ar := toAnthropic(ChatRequest{Model: "claude", ReasoningEffort: eff, MaxTokens: max})
			if ar.Thinking == nil {
				continue
			}
			if ar.Thinking.BudgetTokens < 1024 {
				t.Errorf("effort %q max_tokens %d: budget %d is below the 1024 minimum",
					eff, max, ar.Thinking.BudgetTokens)
			}
			if ar.Thinking.BudgetTokens >= ar.MaxTokens {
				t.Errorf("effort %q max_tokens %d: budget %d must be < max_tokens or the API 400s",
					eff, max, ar.Thinking.BudgetTokens)
			}
		}
	}
}
