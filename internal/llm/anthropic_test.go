package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// anthropicServer serves recorded Messages-API SSE events, optionally
// capturing the request body for wire-format assertions.
func anthropicServer(t *testing.T, events []string, capture *map[string]any, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
			}
			*capture = req
		}
		if r.Header.Get("x-api-key") == "" {
			t.Error("x-api-key header missing")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header missing")
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
}

func TestAnthropicStreamTextAndUsage(t *testing.T) {
	srv := anthropicServer(t, []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":12,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	}, nil, 0)
	defer srv.Close()

	p := NewAnthropic(srv.URL, "k")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "claude", System: "be brief"})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var usage Usage
	for ev := range events {
		switch ev.Type {
		case TextEvent:
			text.WriteString(ev.Text)
		case UsageEvent:
			usage = ev.Usage
		case ErrorEvent:
			t.Fatalf("error event: %v", ev.Err)
		}
	}
	if text.String() != "hello" {
		t.Errorf("text = %q", text.String())
	}
	if usage.PromptTokens != 12 || usage.CompletionTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestAnthropicToolCallAccumulatesFragments(t *testing.T) {
	srv := anthropicServer(t, []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"write_file"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\": \"a.go\""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":", \"content\": \"x\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
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
	}
	if call == nil {
		t.Fatal("no tool call event")
	}
	if call.ID != "tu_1" || call.Name != "write_file" ||
		call.Arguments != `{"path": "a.go", "content": "x"}` {
		t.Errorf("call = %+v", call)
	}
}

func TestAnthropicThinkingMapsToReasoning(t *testing.T) {
	srv := anthropicServer(t, []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weighing options"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"done"}}`,
		`{"type":"message_stop"}`,
	}, nil, 0)
	defer srv.Close()

	p := NewAnthropic(srv.URL, "k")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var reasoning, text strings.Builder
	for ev := range events {
		switch ev.Type {
		case ReasoningEvent:
			reasoning.WriteString(ev.Text)
		case TextEvent:
			text.WriteString(ev.Text)
		}
	}
	if reasoning.String() != "weighing options" {
		t.Errorf("reasoning = %q", reasoning.String())
	}
	if text.String() != "done" {
		t.Errorf("text = %q", text.String())
	}
}

func TestAnthropicHTTPErrorReadable(t *testing.T) {
	srv := anthropicServer(t, nil, nil, http.StatusUnauthorized)
	defer srv.Close()

	p := NewAnthropic(srv.URL, "bad-key")
	_, err := p.StreamChat(context.Background(), ChatRequest{Model: "claude"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("error should carry the provider message: %v", err)
	}
	if strings.Contains(err.Error(), `"type"`) {
		t.Errorf("raw JSON leaked: %v", err)
	}
}

func TestAnthropicWireFormat(t *testing.T) {
	var req map[string]any
	srv := anthropicServer(t, []string{`{"type":"message_stop"}`}, &req, 0)
	defer srv.Close()

	p := NewAnthropic(srv.URL, "k")
	_, err := p.StreamChat(context.Background(), ChatRequest{
		Model:     "claude-sonnet-4.5",
		System:    "you are tilde",
		MaxTokens: 0, // provider default applies
		Messages: []Message{
			{Role: "user", Content: "look", Images: []Image{{MimeType: "image/png", Data: []byte("png")}}},
			{Role: "assistant", Content: "reading", ToolCalls: []ToolCall{
				{ID: "tu_9", Name: "read_file", Arguments: `{"path":"x.go"}`},
			}},
			{Role: "tool", ToolCallID: "tu_9", Content: "42 lines"},
			{Role: "user", Content: "go on"},
		},
		Tools: []Tool{{Name: "read_file", Description: "Read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if req["system"] != "you are tilde" {
		t.Errorf("system = %v", req["system"])
	}
	// max_tokens is required; the default applies when unset.
	if mt, ok := req["max_tokens"].(float64); !ok || mt != anthropicDefaultMaxTokens {
		t.Errorf("max_tokens = %v", req["max_tokens"])
	}
	msgs := req["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want 4", len(msgs))
	}

	// User message with an image: image block first, text block after.
	first := msgs[0].(map[string]any)
	content := first["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("image message content = %v", content)
	}
	img := content[0].(map[string]any)
	if img["type"] != "image" {
		t.Errorf("first block = %v", img)
	}
	src := img["source"].(map[string]any)
	if src["media_type"] != "image/png" || src["data"] != base64.StdEncoding.EncodeToString([]byte("png")) {
		t.Errorf("image source = %v", src)
	}
	if content[1].(map[string]any)["text"] != "look" {
		t.Errorf("text block = %v", content[1])
	}

	// Assistant message: text block + tool_use block with parsed input.
	second := msgs[1].(map[string]any)
	blocks := second["content"].([]any)
	if len(blocks) != 2 || blocks[0].(map[string]any)["type"] != "text" {
		t.Fatalf("assistant blocks = %v", blocks)
	}
	tu := blocks[1].(map[string]any)
	if tu["type"] != "tool_use" || tu["id"] != "tu_9" || tu["name"] != "read_file" {
		t.Errorf("tool_use = %v", tu)
	}
	input := tu["input"].(map[string]any)
	if input["path"] != "x.go" {
		t.Errorf("tool input = %v", input)
	}

	// Tool result: a user message with a tool_result block.
	third := msgs[2].(map[string]any)
	if third["role"] != "user" {
		t.Errorf("tool result role = %v", third["role"])
	}
	tr := third["content"].([]any)[0].(map[string]any)
	if tr["type"] != "tool_result" || tr["tool_use_id"] != "tu_9" || tr["content"] != "42 lines" {
		t.Errorf("tool_result = %v", tr)
	}

	// Tools carry input_schema.
	tools := req["tools"].([]any)
	t0 := tools[0].(map[string]any)
	if t0["name"] != "read_file" {
		t.Errorf("tool = %v", t0)
	}
	if _, ok := t0["input_schema"]; !ok {
		t.Errorf("input_schema missing: %v", t0)
	}
}

func TestNewSelectsByWireAPI(t *testing.T) {
	if _, ok := New("anthropic", "https://api.anthropic.com", "k").(*Anthropic); !ok {
		t.Error("anthropic api must build the Messages client")
	}
	if _, ok := New("", "https://api.openai.com/v1", "k").(*OpenAICompat); !ok {
		t.Error("default api must build the OpenAI-compatible client")
	}
	if _, ok := New("openai", "https://api.mistral.ai/v1", "k").(*OpenAICompat); !ok {
		t.Error("openai api must build the OpenAI-compatible client")
	}
}
