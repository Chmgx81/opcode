package tui

import (
	"strings"
	"testing"

	"github.com/Chmgx81/tilde/internal/llm"
)

// TestDecideInputOutcomes: every submit path returns its typed
// outcome — "why didn't my message send" is a value, not behavior
// to reverse-engineer. One case per decision.
func TestDecideInputOutcomes(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.width, m.height = 80, 30

	// Empty draft: nothing happens.
	if d, _ := m.decideInput(false); d != inputIgnored {
		t.Errorf("empty draft = %v, want inputIgnored", d)
	}

	// Shell escape: user-run, no model round trip. It runs as a Cmd, so
	// the output lands when the message comes back — the shape that
	// keeps the render loop free while a command runs.
	m.composer.SetValue("!echo shell-ok")
	d, cmd := m.decideInput(false)
	if d != inputShell {
		t.Errorf("shell escape = %v, want inputShell", d)
	}
	if cmd == nil {
		t.Fatal("shell escape returned no command")
	}
	if m.shell == nil {
		t.Error("no in-flight shell run while the command is out")
	}
	m.Update(cmd())
	if last := m.entries[len(m.entries)-1]; last.kind != entryDim ||
		!strings.Contains(last.text, "shell-ok") {
		t.Errorf("shell escape output missing: %+v", last)
	}
	if m.shell != nil {
		t.Error("the shell run outlived its reply")
	}

	// Slash command: handled by the command system, no turn.
	m.composer.SetValue("/help")
	if d, _ := m.decideInput(false); d != inputCommand {
		t.Errorf("slash command = %v, want inputCommand", d)
	}
	if !m.helpOpen {
		t.Error("/help did not open the help overlay")
	}
	m.helpOpen = false

	// Idle + text: a turn starts.
	m2, fp := newText(t, dir, [][]llm.ChatEvent{
		{{Type: llm.TextEvent, Text: "hello"}},
	})
	m2.width, m2.height = 80, 30
	m2.composer.SetValue("start a turn")
	if d, _ := m2.decideInput(false); d != inputTurnStarted {
		t.Errorf("idle submit = %v, want inputTurnStarted", d)
	}
	if !m2.working {
		t.Error("inputTurnStarted but m.working is false")
	}
	waitFor(t, func() bool { return fp.requestCount() >= 1 })

	// Working + plain Enter: the draft folds in at the next round.
	m2.composer.SetValue("steer me")
	if d, _ := m2.decideInput(false); d != inputSteered {
		t.Errorf("working plain submit = %v, want inputSteered", d)
	}
	// Working + Alt+Enter: the draft is held for turn end.
	m2.composer.SetValue("queue me")
	if d, _ := m2.decideInput(true); d != inputQueued {
		t.Errorf("working alt submit = %v, want inputQueued", d)
	}
	if len(m2.queue) != 1 || m2.queue[0].text != "queue me" {
		t.Errorf("queue = %+v, want one held draft", m2.queue)
	}

	// The typed outcomes cover every path the composer can take:
	// the enum is exhaustive by construction (iota has no holes).
	if inputIgnored == inputShell || inputSteered == inputQueued {
		t.Error("distinct paths collapsed into one outcome")
	}
}
