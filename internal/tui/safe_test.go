package tui

import (
	"strings"
	"testing"

	"github.com/Chmgx81/opcode/internal/llm"
	"github.com/Chmgx81/opcode/internal/orchestrator"
)

// poisoned is what a hostile file, shell output, or web page can carry:
// a window-title grab, a screen clear, a keyboard remap, and a lone ESC.
const poisoned = "pre\x1b]0;pwned\x07post\x1b[2Jend\x1b"

// TestToolResultSanitizedForDisplay: a tool result reaches the timeline
// (collapsed summary, expanded full text, and from there the transcript
// pager and committed scrollback) without a single live control byte.
func TestToolResultSanitizedForDisplay(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.handleEvent(orchestrator.Event{
		Kind:       orchestrator.EventToolResult,
		ToolCall:   llm.ToolCall{Name: "read_file", Arguments: `{"path":"/etc/hosts"}`},
		ToolResult: poisoned,
	})
	found := false
	for _, e := range m.entries {
		if e.kind != entryResult {
			continue
		}
		found = true
		if strings.ContainsAny(e.summary, "\x1b\x07") || strings.ContainsAny(e.full, "\x1b\x07") {
			t.Fatalf("result entry carries control bytes: summary %q full %q", e.summary, e.full)
		}
		if e.full != "prepostend" {
			t.Errorf("sanitized result = %q, want %q", e.full, "prepostend")
		}
	}
	if !found {
		t.Fatal("no result entry was added")
	}
}

// TestStreamTextSanitizedForDisplay: model output streams through the
// same boundary — nothing the model emits may drive the terminal.
func TestStreamTextSanitizedForDisplay(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.handleEvent(orchestrator.Event{Kind: orchestrator.EventText, Text: poisoned})
	if s := m.stream.String(); s != "prepostend" {
		t.Errorf("stream = %q, want %q", s, "prepostend")
	}
}

// TestToolLineArgsSanitizedForDisplay: the tool call line renders the
// model's arguments verbatim, so those are sanitized too.
func TestToolLineArgsSanitizedForDisplay(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, [][]llm.ChatEvent{})
	m.handleEvent(orchestrator.Event{
		Kind:     orchestrator.EventToolStart,
		ToolCall: llm.ToolCall{Name: "bash", Arguments: poisoned},
	})
	for _, e := range m.entries {
		if e.kind != entryTool {
			continue
		}
		if strings.ContainsAny(e.text, "\x1b\x07") {
			t.Fatalf("tool line carries control bytes: %q", e.text)
		}
	}
}
