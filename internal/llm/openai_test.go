package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// sseServer serves a canned sequence of SSE chunks from a handler that can
// also inspect the request body, so tests can assert what tilde put on the
// wire without a real provider.
type capturedRequest struct {
	Model    string
	Messages []wireMessage
	Tools    []wireTool
	Auth     string
}

func sseServer(t *testing.T, chunks []string, capture *capturedRequest, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			var req wireRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
			}
			capture.Model = req.Model
			capture.Messages = req.Messages
			capture.Tools = req.Tools
			capture.Auth = r.Header.Get("Authorization")
		}
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error": {"message": "bad key"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: ")
	}))
}

func collect(t *testing.T, events <-chan ChatEvent) []ChatEvent {
	t.Helper()
	var out []ChatEvent
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func textDelta(s string) string {
	return fmt.Sprintf(`{"choices": [{"delta": {"content": %q}}]}`, s)
}

func toolFragment(index int, id, name, args string) string {
	return fmt.Sprintf(`{"choices": [{"delta": {"tool_calls": [{"index": %d, "id": %q, "function": {"name": %q, "arguments": %q}}]}}]}`,
		index, id, name, args)
}

func toolFragmentNoID(index int, args string) string {
	return fmt.Sprintf(`{"choices": [{"delta": {"tool_calls": [{"index": %d, "function": {"arguments": %q}}]}}]}`,
		index, args)
}

func finish(reason string) string {
	return fmt.Sprintf(`{"choices": [{"delta": {}, "finish_reason": %q}]}`, reason)
}

func TestStreamChatTextDeltas(t *testing.T) {
	srv := sseServer(t, []string{
		textDelta("Hello"),
		textDelta(" world"),
		finish("stop"),
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collect(t, events)
	var text string
	for _, ev := range got {
		if ev.Type != TextEvent {
			t.Errorf("unexpected event type %q", ev.Type)
		}
		text += ev.Text
	}
	if text != "Hello world" {
		t.Errorf("text = %q, want %q", text, "Hello world")
	}
}

func TestStreamChatToolCallSplitAcrossChunks(t *testing.T) {
	// One tool call whose arguments arrive as three fragments.
	srv := sseServer(t, []string{
		toolFragment(0, "call-1", "edit_file", `{"path": "ma`),
		toolFragmentNoID(0, `in.go", "old": `),
		toolFragmentNoID(0, `"a"}`),
		finish("tool_calls"),
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collect(t, events)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1 tool call", len(got))
	}
	call := got[0]
	if call.Type != ToolCallEvent {
		t.Fatalf("type = %q, want tool_call", call.Type)
	}
	if call.Call.ID != "call-1" || call.Call.Name != "edit_file" {
		t.Errorf("ID/Name = %q/%q", call.Call.ID, call.Call.Name)
	}
	want := `{"path": "main.go", "old": "a"}`
	if call.Call.Arguments != want {
		t.Errorf("Arguments = %q, want %q", call.Call.Arguments, want)
	}
}

func TestStreamChatInterleavedToolCalls(t *testing.T) {
	srv := sseServer(t, []string{
		toolFragment(0, "call-a", "read_file", `{"path": "a`),
		toolFragment(1, "call-b", "run_shell", `{"command": "ls`),
		toolFragmentNoID(0, `.txt"}`),
		toolFragmentNoID(1, ` -la"}`),
		finish("tool_calls"),
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collect(t, events)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 tool calls", len(got))
	}
	if got[0].Call.Name != "read_file" || got[0].Call.Arguments != `{"path": "a.txt"}` {
		t.Errorf("call 0 = %+v", got[0].Call)
	}
	if got[1].Call.Name != "run_shell" || got[1].Call.Arguments != `{"command": "ls -la"}` {
		t.Errorf("call 1 = %+v", got[1].Call)
	}
}

func TestStreamChatMissingFinishReasonStillFlushes(t *testing.T) {
	// Some servers truncate without a finish_reason; buffered calls must
	// still be delivered, not silently dropped.
	srv := sseServer(t, []string{
		toolFragment(0, "call-1", "read_file", `{"path": "x"}`),
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collect(t, events)
	if len(got) != 1 || got[0].Type != ToolCallEvent {
		t.Fatalf("events = %+v, want one tool call", got)
	}
}

func TestStreamChatProviderErrorChunk(t *testing.T) {
	srv := sseServer(t, []string{
		`{"error": {"code": 401, "message": "invalid key"}}`,
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collect(t, events)
	if len(got) != 1 || got[0].Type != ErrorEvent {
		t.Fatalf("events = %+v, want one error event", got)
	}
	if got[0].Err == nil {
		t.Error("Err = nil")
	}
}

func TestStreamChatHTTPError(t *testing.T) {
	srv := sseServer(t, nil, nil, http.StatusUnauthorized)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	_, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
}

func TestStreamChatSendsExpectedWireFormat(t *testing.T) {
	var cap capturedRequest
	srv := sseServer(t, []string{finish("stop")}, &cap, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "sk-test")
	events, err := p.StreamChat(context.Background(), ChatRequest{
		Model:  "anthropic/claude-sonnet-4.5",
		System: "be brief",
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				{ID: "c1", Name: "read_file", Arguments: `{"path":"x"}`},
			}},
			{Role: "tool", ToolCallID: "c1", Content: "file contents"},
		},
		Tools: []Tool{{
			Name:        "read_file",
			Description: "read a file",
			Parameters:  json.RawMessage(`{"type": "object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	collect(t, events)

	if cap.Model != "anthropic/claude-sonnet-4.5" {
		t.Errorf("Model = %q", cap.Model)
	}
	if cap.Auth != "Bearer sk-test" {
		t.Errorf("Authorization = %q", cap.Auth)
	}
	// system prompt first, then user, assistant tool call, tool result
	if len(cap.Messages) != 4 {
		t.Fatalf("got %d messages, want 4", len(cap.Messages))
	}
	if cap.Messages[0].Role != "system" || cap.Messages[0].Content != "be brief" {
		t.Errorf("message 0 = %+v", cap.Messages[0])
	}
	if cap.Messages[1].Role != "user" || cap.Messages[1].Content != "hi" {
		t.Errorf("message 1 = %+v", cap.Messages[1])
	}
	asst := cap.Messages[2]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 1 ||
		asst.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("message 2 = %+v", asst)
	}
	tool := cap.Messages[3]
	if tool.Role != "tool" || tool.ToolCallID != "c1" || tool.Content != "file contents" {
		t.Errorf("message 3 = %+v", tool)
	}
	if len(cap.Tools) != 1 || cap.Tools[0].Function.Name != "read_file" {
		t.Errorf("tools = %+v", cap.Tools)
	}
}

func TestStreamChatUsageEvent(t *testing.T) {
	// With stream_options.include_usage, usage arrives in a final chunk
	// after finish_reason — the client must keep reading past it.
	srv := sseServer(t, []string{
		textDelta("hi"),
		finish("stop"),
		`{"choices": [], "usage": {"prompt_tokens": 12, "completion_tokens": 34}}`,
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collect(t, events)
	if len(got) != 2 {
		t.Fatalf("got %d events, want text + usage", len(got))
	}
	if got[0].Type != TextEvent || got[0].Text != "hi" {
		t.Errorf("event 0 = %+v", got[0])
	}
	if got[1].Type != UsageEvent {
		t.Fatalf("event 1 type = %q, want usage", got[1].Type)
	}
	if got[1].Usage.PromptTokens != 12 || got[1].Usage.CompletionTokens != 34 {
		t.Errorf("usage = %+v", got[1].Usage)
	}
}

func TestStreamChatUsageAfterToolCallFinish(t *testing.T) {
	// Usage chunk also comes after finish_reason=tool_calls; the calls
	// must be flushed exactly once, not duplicated by the end-of-stream
	// flush.
	srv := sseServer(t, []string{
		toolFragment(0, "call-1", "read_file", `{"path": "x"}`),
		finish("tool_calls"),
		`{"choices": [], "usage": {"prompt_tokens": 5, "completion_tokens": 7}}`,
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collect(t, events)
	var calls, usage int
	for _, ev := range got {
		switch ev.Type {
		case ToolCallEvent:
			calls++
		case UsageEvent:
			usage++
		}
	}
	if calls != 1 {
		t.Errorf("got %d tool call events, want 1 (no double flush)", calls)
	}
	if usage != 1 {
		t.Errorf("got %d usage events, want 1", usage)
	}
}

func TestStreamChatSkipsNonJSONDataLines(t *testing.T) {
	// Real providers (OpenRouter notably) interleave non-JSON data lines
	// with the chunks; a healthy turn must survive them.
	srv := sseServer(t, []string{
		textDelta("keep"),
		textDelta(" going"),
		`DATA: OPENROUTER PROCESSING`,
		`DONE`,
		finish("stop"),
	}, nil, 0)
	defer srv.Close()

	p := NewOpenAICompat(srv.URL, "test-key")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collect(t, events)
	var text string
	for _, ev := range got {
		if ev.Type == ErrorEvent {
			t.Fatalf("non-JSON data line killed the stream: %v", ev.Err)
		}
		if ev.Type == TextEvent {
			text += ev.Text
		}
	}
	if text != "keep going" {
		t.Errorf("text = %q", text)
	}
}
