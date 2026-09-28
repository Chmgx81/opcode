package tools

import (
	"context"
	"strings"
	"testing"
)

func TestPresentPlanTierIsDraftOnly(t *testing.T) {
	if tier := (PresentPlan{}).Tier(); tier != TierDraftOnly {
		t.Errorf("present_plan tier = %v, want draft-only", tier)
	}
}

func TestPresentPlanVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		approve func(string) (bool, bool)
		want    []string
	}{
		{"approved auto", func(string) (bool, bool) { return true, true },
			[]string{"approved", "full-auto", "without prompting"}},
		{"approved asking", func(string) (bool, bool) { return true, false },
			[]string{"approved", "will ask"}},
		{"declined", func(string) (bool, bool) { return false, false },
			[]string{"declined", "Do not make changes"}},
	}
	for _, c := range cases {
		p := PresentPlan{Approve: c.approve}
		out, err := p.Execute(context.Background(), `{"plan": "## Goal\nfix the thing"}`)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s: result %q missing %q", c.name, out, want)
			}
		}
	}
}

func TestPresentPlanFailsClosedAndValidates(t *testing.T) {
	// Nil Approve (headless): nothing proceeds.
	p := PresentPlan{}
	out, err := p.Execute(context.Background(), `{"plan": "x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cannot be reached") || strings.Contains(out, "implement") {
		t.Errorf("nil Approve must fail closed, got %q", out)
	}
	// Empty plan is an argument error, not a silent approval.
	if _, err := p.Execute(context.Background(), `{"plan": ""}`); err == nil {
		t.Error("empty plan must error")
	}
	if _, err := p.Execute(context.Background(), `{}`); err == nil {
		t.Error("missing plan must error")
	}
}
