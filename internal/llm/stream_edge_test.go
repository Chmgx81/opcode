package llm

import (
	"context"
	"testing"
)

func TestAddUnindexedSoloCall(t *testing.T) {
	var asm toolCallAssembler
	asm.add(0, "id-1", "read_file", `{"path": "a`)
	if !asm.hasContent() {
		t.Fatal("hasContent = false after adding a fragment")
	}
	// One call in flight: an index-less fragment joins it.
	if !asm.addUnindexed("", "", `.go"}`) {
		t.Fatal("addUnindexed refused a solo call")
	}
	calls := asm.flush()
	if len(calls) != 1 {
		t.Fatalf("flush = %d calls, want 1", len(calls))
	}
	if calls[0].Name != "read_file" || calls[0].Arguments != `{"path": "a.go"}` {
		t.Errorf("reassembled = %+v, want the joined call", calls[0])
	}
}

// TestAnthropicToolCallEmittedExactlyOnce: a stream test must assert
// on the sequence, not the last value. Keeping only the final
// ToolCallEvent structurally cannot see a duplicate, which is how
// every tool call ran twice per round for a release.
func TestAnthropicToolCallEmittedExactlyOnce(t *testing.T) {
	srv := anthropicServer(t, []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"write_file"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_stop"}`,
	}, nil, 0)
	defer srv.Close()

	p := NewAnthropic(srv.URL, "k")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var calls []ToolCall
	for ev := range events {
		if ev.Type == ErrorEvent {
			t.Fatalf("error event: %v", ev.Err)
		}
		if ev.Type == ToolCallEvent {
			calls = append(calls, ev.Call)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("emitted %d tool call events for one tool_use block, want 1: %+v", len(calls), calls)
	}
	if calls[0].ID != "tu_1" || calls[0].Name != "write_file" {
		t.Errorf("call = %+v", calls[0])
	}
}

// TestAnthropicParallelToolCallsKeepOrder: the EOF flush walks the
// block map in index order, not Go's randomized map order, so two
// parallel calls cannot swap places between runs.
func TestAnthropicParallelToolCallsKeepOrder(t *testing.T) {
	for run := 0; run < 20; run++ {
		srv := anthropicServer(t, []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"a","name":"read_file"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"b","name":"bash"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
			`{"type":"message_stop"}`,
		}, nil, 0)

		p := NewAnthropic(srv.URL, "k")
		events, err := p.StreamChat(context.Background(), ChatRequest{Model: "claude"})
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}
		var names []string
		for ev := range events {
			if ev.Type == ToolCallEvent {
				names = append(names, ev.Call.Name)
			}
		}
		srv.Close()
		if len(names) != 2 || names[0] != "read_file" || names[1] != "bash" {
			t.Fatalf("run %d: order = %v, want [read_file bash]", run, names)
		}
	}
}

func TestAddUnindexedAmbiguous(t *testing.T) {
	var asm toolCallAssembler
	// Zero calls: nothing to join, dropped.
	if asm.addUnindexed("", "read_file", "{}") {
		t.Error("addUnindexed accepted with zero calls in flight")
	}
	asm.add(0, "id-1", "read_file", "{}")
	asm.add(1, "id-2", "bash", "{}")
	// Two calls: no honest routing, dropped.
	if asm.addUnindexed("", "", "junk") {
		t.Error("addUnindexed accepted with two calls in flight")
	}
	if calls := asm.flush(); len(calls) != 2 {
		t.Errorf("flush = %d calls, want the original 2", len(calls))
	}
}

func TestSseDataToleratesMissingSpace(t *testing.T) {
	for _, line := range []string{"data: {\"a\":1}", "data:{\"a\":1}", "data:"} {
		data, ok := sseData([]byte(line))
		if !ok {
			t.Errorf("sseData(%q) = not ok, want ok", line)
		} else if len(data) == 0 && line != "data:" {
			t.Errorf("sseData(%q) dropped the payload", line)
		}
	}
	if _, ok := sseData([]byte(": keep-alive")); ok {
		t.Error("sseData treated a keep-alive comment as data")
	}
	if _, ok := sseData([]byte("event: message")); ok {
		t.Error("sseData treated an event field as data")
	}
}

func TestAnthropicFlushesToolUseAtEOF(t *testing.T) {
	// No content_block_stop: a clean EOF must still emit the call,
	// like the OpenAI client flushes at stream end.
	srv := anthropicServer(t, []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_9","name":"bash"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\": \"echo hi\"}"}}`,
		`{"type":"message_stop"}`,
	}, nil, 0)
	defer srv.Close()

	p := NewAnthropic(srv.URL, "k")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var call *ToolCall
	for ev := range events {
		if ev.Type == ToolCallEvent {
			c := ev.Call
			call = &c
		}
		if ev.Type == ErrorEvent {
			t.Fatalf("error event: %v", ev.Err)
		}
	}
	if call == nil {
		t.Fatal("no tool call event at EOF without content_block_stop")
	}
	if call.ID != "tu_9" || call.Name != "bash" || call.Arguments != `{"command": "echo hi"}` {
		t.Errorf("call = %+v", call)
	}
}

func TestOpenAIIndexLessFragmentJoinsSoloCall(t *testing.T) {
	// A second fragment for the same call with no "index" field —
	// servers that omit it must still reassemble one call, not split
	// it into a half-JSON tool call aimed at nothing.
	srv := sseServer(t, []string{
		toolFragment(0, "call_1", "read_file", `{"path": "a`),
		`{"choices": [{"delta": {"tool_calls": [{"function": {"arguments": ".go\"}"}}]}}]}`,
		finish("tool_calls"),
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "k")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var call *ToolCall
	for ev := range events {
		if ev.Type == ToolCallEvent {
			c := ev.Call
			call = &c
		}
		if ev.Type == ErrorEvent {
			t.Fatalf("error event: %v", ev.Err)
		}
	}
	if call == nil {
		t.Fatal("no tool call event")
	}
	if call.Name != "read_file" || call.Arguments != `{"path": "a.go"}` {
		t.Errorf("call = %+v", call)
	}
}
