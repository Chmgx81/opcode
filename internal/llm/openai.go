package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAICompat speaks the OpenAI chat/completions wire format, which is
// what OpenRouter (the default) and local servers such as Ollama/vLLM
// expose. The model comes in with each request as a config string; nothing
// here is model-specific.
type OpenAICompat struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func NewOpenAICompat(baseURL, apiKey string) *OpenAICompat {
	return &OpenAICompat{BaseURL: baseURL, APIKey: apiKey, Client: &http.Client{}}
}

// --- wire types (this file's private translation layer) ---

type wireRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Tools    []wireTool    `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	// Index is set only on streamed fragments, never on outgoing calls.
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireToolFunc `json:"function"`
}

type wireToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string         `json:"content"`
			ToolCalls []wireToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	// OpenRouter reports mid-stream errors inside a chunk this way.
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// toWire converts tilde's message history into the wire format.
func toWire(req ChatRequest) wireRequest {
	msgs := make([]wireMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, wireMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		wm := wireMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: wireFunction{Name: tc.Name, Arguments: tc.Arguments},
			})
		}
		msgs = append(msgs, wm)
	}
	wr := wireRequest{Model: req.Model, Messages: msgs, Stream: true}
	for _, t := range req.Tools {
		wr.Tools = append(wr.Tools, wireTool{
			Type: "function",
			Function: wireToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return wr
}

// StreamChat POSTs the request and returns a channel of events. Text
// deltas stream as they arrive; tool calls are buffered by the assembler
// and only emitted once the model's fragments are complete.
func (p *OpenAICompat) StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	body, err := json.Marshal(toWire(req))
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", httpReq.URL, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	events := make(chan ChatEvent, 16)
	go p.consume(resp.Body, events)
	return events, nil
}

// consume parses the SSE body into events and closes the channel when
// done. A non-200 status was already handled before this runs.
func (p *OpenAICompat) consume(body io.ReadCloser, events chan<- ChatEvent) {
	defer body.Close()
	defer close(events)

	var asm toolCallAssembler
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		// SSE frames look like "data: {json}". Comment lines (":...")
		// and event/id lines are ignored.
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			events <- ChatEvent{Type: ErrorEvent, Err: fmt.Errorf("parse stream chunk: %w", err)}
			return
		}
		if chunk.Error != nil {
			events <- ChatEvent{Type: ErrorEvent, Err: fmt.Errorf("provider error %d: %s",
				chunk.Error.Code, chunk.Error.Message)}
			return
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			events <- ChatEvent{Type: TextEvent, Text: choice.Delta.Content}
		}
		for _, frag := range choice.Delta.ToolCalls {
			if frag.Index == nil {
				continue
			}
			asm.add(*frag.Index, frag.ID, frag.Function.Name, frag.Function.Arguments)
		}
		// finish_reason marks the end of the model's output: whatever is
		// buffered is now complete. Some servers omit it on truncation,
		// so flush after the loop too.
		if choice.FinishReason != nil {
			for _, call := range asm.flush() {
				events <- ChatEvent{Type: ToolCallEvent, Call: call}
			}
			return
		}
	}
	if err := scanner.Err(); err != nil {
		events <- ChatEvent{Type: ErrorEvent, Err: fmt.Errorf("read stream: %w", err)}
		return
	}
	for _, call := range asm.flush() {
		events <- ChatEvent{Type: ToolCallEvent, Call: call}
	}
}
