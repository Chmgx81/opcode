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
	"github.com/Chmgx81/tilde/internal/safe"
)

// Options for a one-shot run.
type Options struct {
	// JSON switches output from human text to one JSON event object
	// per line.
	JSON bool
	// Out receives subagent progress (labeled lines in text mode,
	// events in JSON mode); main wires the spawn emitter into it.
	Out io.Writer
}

// Run drives one turn for prompt and writes the events.
func Run(ctx context.Context, orch *orchestrator.Orchestrator, prompt string, opt Options) error {
	w := opt.Out
	enc := json.NewEncoder(w)
	// A JSON consumer piping our output deserves to know the stream
	// broke (a closed pipe, a full disk) rather than receiving a
	// silently truncated one; the first encode error wins and is
	// returned alongside any turn error (audit C11).
	var encodeErr error
	emit := func(v map[string]any) {
		if encodeErr == nil {
			encodeErr = enc.Encode(v)
		}
	}

	if opt.JSON {
		emit(map[string]any{"kind": "start", "prompt": prompt})
	} else {
		fmt.Fprintf(w, "~ %s\n", prompt)
	}

	events := orch.Send(ctx, prompt)
	var err error
	for ev := range events {
		// Text mode prints untrusted bytes straight to the terminal,
		// so they are sanitized here. JSON mode does not need it:
		// encoding/json escapes every control character, so a JSON
		// line can never carry a live sequence — downstream tools
		// get the honest bytes.
		text, args, result := safe.Text(ev.Text),
			safe.Text(ev.ToolCall.Arguments), safe.Text(ev.ToolResult)
		switch ev.Kind {
		case orchestrator.EventText:
			if opt.JSON {
				emit(map[string]any{"kind": "text", "text": ev.Text})
			} else {
				fmt.Fprint(w, text)
			}
		case orchestrator.EventToolStart:
			if opt.JSON {
				emit(map[string]any{
					"kind": "tool", "tool": ev.ToolCall.Name, "args": ev.ToolCall.Arguments})
			} else {
				fmt.Fprintf(w, "\n[tool] %s %s\n", ev.ToolCall.Name, args)
			}
		case orchestrator.EventToolResult:
			if opt.JSON {
				emit(map[string]any{
					"kind": "tool_result", "tool": ev.ToolCall.Name, "result": ev.ToolResult})
			} else {
				fmt.Fprintf(w, "[result] %s\n", oneLine(result))
			}
		case orchestrator.EventUsage:
			if opt.JSON {
				emit(map[string]any{
					"kind": "usage", "prompt_tokens": ev.Usage.PromptTokens,
					"completion_tokens": ev.Usage.CompletionTokens})
			}
		case orchestrator.EventTurnComplete:
			if opt.JSON {
				emit(map[string]any{"kind": "done"})
			} else if !opt.JSON {
				fmt.Fprintln(w)
			}
		case orchestrator.EventCompactionFailed:
			if opt.JSON {
				emit(map[string]any{"kind": "compaction_failed", "error": ev.Err.Error()})
			} else {
				fmt.Fprintf(w, "[compaction skipped] %s\n", ev.Err.Error())
			}
		case orchestrator.EventError:
			err = ev.Err
			if opt.JSON {
				emit(map[string]any{"kind": "error", "error": ev.Err.Error()})
			} else {
				fmt.Fprintf(w, "\nerror: %v\n", ev.Err)
			}
		}
	}
	if err != nil {
		return err
	}
	return encodeErr
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return strings.ReplaceAll(s, "\n", " \\n ")
}
