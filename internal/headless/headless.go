// Package headless drives the orchestrator without a TUI (Section 3.9):
// `tilde -p "prompt"` runs one turn and prints the result. This is the
// same orchestrator the TUI drives — headless is a different consumer,
// not a different agent.
package headless

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Chmgx81/tilde/internal/orchestrator"
)

// Options for a one-shot run.
type Options struct {
	// JSON switches output from human text to one JSON event object
	// per line.
	JSON bool
	// SubagentEmit receives subagent progress (labeled lines in text
	// mode, events in JSON mode).
	Out io.Writer
}

// Run drives one turn for prompt and writes the events.
func Run(ctx context.Context, orch *orchestrator.Orchestrator, prompt string, opt Options) error {
	w := opt.Out
	enc := json.NewEncoder(w)

	if opt.JSON {
		enc.Encode(map[string]any{"kind": "start", "prompt": prompt})
	} else {
		fmt.Fprintf(w, "~ %s\n", prompt)
	}

	events := orch.Send(ctx, prompt)
	var err error
	for ev := range events {
		switch ev.Kind {
		case orchestrator.EventText:
			if opt.JSON {
				enc.Encode(map[string]any{"kind": "text", "text": ev.Text})
			} else {
				fmt.Fprint(w, ev.Text)
			}
		case orchestrator.EventToolStart:
			if opt.JSON {
				enc.Encode(map[string]any{
					"kind": "tool", "tool": ev.ToolCall.Name, "args": ev.ToolCall.Arguments})
			} else {
				fmt.Fprintf(w, "\n[tool] %s %s\n", ev.ToolCall.Name, ev.ToolCall.Arguments)
			}
		case orchestrator.EventToolResult:
			if opt.JSON {
				enc.Encode(map[string]any{
					"kind": "tool_result", "tool": ev.ToolCall.Name, "result": ev.ToolResult})
			} else {
				fmt.Fprintf(w, "[result] %s\n", oneLine(ev.ToolResult))
			}
		case orchestrator.EventUsage:
			if opt.JSON {
				enc.Encode(map[string]any{
					"kind": "usage", "prompt_tokens": ev.Usage.PromptTokens,
					"completion_tokens": ev.Usage.CompletionTokens})
			}
		case orchestrator.EventTurnComplete:
			if opt.JSON {
				enc.Encode(map[string]any{"kind": "done"})
			} else if !opt.JSON {
				fmt.Fprintln(w)
			}
		case orchestrator.EventError:
			err = ev.Err
			if opt.JSON {
				enc.Encode(map[string]any{"kind": "error", "error": ev.Err.Error()})
			} else {
				fmt.Fprintf(w, "\nerror: %v\n", ev.Err)
			}
		}
	}
	return err
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return strings.ReplaceAll(s, "\n", " \\n ")
}
