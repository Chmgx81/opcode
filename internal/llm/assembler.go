package llm

import "strings"

// toolCallAssembler reassembles tool calls from streamed argument
// fragments. OpenAI-compatible APIs stream a tool call's arguments as
// string fragments spread across many chunks, each keyed by an index (and
// carrying the id/name only on the first fragment for that index). Calls
// for different tools interleave, so nothing is "finished" until the model
// stops sending fragments: flush() returns the completed calls in the
// order their fragments first appeared.
type toolCallAssembler struct {
	calls map[int]*toolCallAccum
	order []int
}

type toolCallAccum struct {
	id   string
	name string
	args strings.Builder
}

// add merges one streamed fragment. A fragment may carry the id/name (the
// first chunk for that index), more argument text, or both.
func (a *toolCallAssembler) add(index int, id, name, argsFragment string) {
	acc, ok := a.calls[index]
	if !ok {
		acc = &toolCallAccum{}
		if a.calls == nil {
			a.calls = map[int]*toolCallAccum{}
		}
		a.calls[index] = acc
		a.order = append(a.order, index)
	}
	if id != "" {
		acc.id = id
	}
	if name != "" {
		acc.name = name
	}
	acc.args.WriteString(argsFragment)
}

// hasContent reports whether anything was accumulated.
func (a *toolCallAssembler) hasContent() bool {
	return len(a.order) > 0
}

// flush returns all accumulated calls in first-fragment order and resets
// the assembler, so a second flush (e.g. both on finish_reason and at
// stream end) never re-emits calls.
func (a *toolCallAssembler) flush() []ToolCall {
	out := a.peek()
	a.calls = nil
	a.order = nil
	return out
}

// peek returns the accumulated calls without resetting.
func (a *toolCallAssembler) peek() []ToolCall {
	var out []ToolCall
	for _, idx := range a.order {
		acc := a.calls[idx]
		out = append(out, ToolCall{
			ID:        acc.id,
			Name:      acc.name,
			Arguments: acc.args.String(),
		})
	}
	return out
}
