package tui

import (
	"strings"
	"testing"

	"github.com/Chmgx81/tilde/internal/llm"
)

// TestCycleEffort: alt+. climbs unset → low → medium → high → unset,
// alt+, descends, the orchestrator follows each step, and the toast
// names the posture.
func TestCycleEffort(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.width, m.height = 80, 30

	if m.effort != "" {
		t.Fatalf("initial effort = %q, want unset", m.effort)
	}
	m.cycleEffort(1)
	if m.effort != "low" || m.opt.Orch.Effort() != "low" {
		t.Errorf("after one up: effort=%q orch=%q, want low/low", m.effort, m.opt.Orch.Effort())
	}
	m.cycleEffort(1)
	m.cycleEffort(1)
	if m.effort != "high" {
		t.Errorf("after three ups: effort=%q, want high", m.effort)
	}
	m.cycleEffort(1)
	if m.effort != "" {
		t.Errorf("the cycle did not wrap to the provider default: %q", m.effort)
	}
	m.cycleEffort(-1)
	if m.effort != "high" {
		t.Errorf("alt+, from unset: effort=%q, want high", m.effort)
	}

	view := m.View()
	if !strings.Contains(view, GlyphDoing+" high") {
		t.Errorf("footer missing the effort segment:\n%s", view)
	}
	m.cycleEffort(-1)
	m.cycleEffort(-1)
	m.cycleEffort(-1)
	if m.effort != "" {
		t.Fatalf("effort after descending the cycle: %q", m.effort)
	}
	view = m.View()
	if strings.Contains(view, "◐ low") || strings.Contains(view, "high") && strings.Contains(view, GlyphDoing) {
		t.Errorf("footer still shows an effort segment after unsetting:\n%s", view)
	}
}

// TestEffortReachesRequest: the knob is not decoration — the next
// model request carries the posture on the wire.
func TestEffortReachesRequest(t *testing.T) {
	dir := t.TempDir()
	m, fp := newText(t, dir, [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hello"}},
	})
	m.width, m.height = 80, 30
	m.cycleEffort(1) // low
	m.cycleEffort(1) // medium
	if m.effort != "medium" {
		t.Fatalf("effort = %q, want medium", m.effort)
	}
	typeAndEnter(m, "hi there")
	waitFor(t, func() bool { return fp.requestCount() >= 1 })
	if fp.gotRequests[0].ReasoningEffort != "medium" {
		t.Errorf("request effort = %q, want medium", fp.gotRequests[0].ReasoningEffort)
	}
}
