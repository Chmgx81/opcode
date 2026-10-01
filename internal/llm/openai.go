package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
	return &OpenAICompat{BaseURL: baseURL, APIKey: apiKey, Client: newStreamHTTPClient()}
}

// --- wire types (this file's private translation layer) ---

type wireRequest struct {
	Model           string             `json:"model"`
	Messages        []wireMessage      `json:"messages"`
	Tools           []wireTool         `json:"tools,omitempty"`
	Stream          bool               `json:"stream"`
	StreamOptions   *wireStreamOptions `json:"stream_options,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
}

type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireMessage struct {
	Role string `json:"role"`
	// Content is a plain string for text, or the parts array for
	// messages carrying images (the OpenAI multimodal form).
	Content    any            `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// wireContentPart is one element of a multimodal content array: a
// text part or an image part, told apart by Type.
type wireContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *wireImageURL `json:"image_url,omitempty"`
}

type wireImageURL struct {
	URL string `json:"url"`
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
			// Reasoning deltas: OpenRouter's "reasoning" field, or the
			// DeepSeek-compatible "reasoning_content".
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	// OpenRouter reports mid-stream errors inside a chunk this way.
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	// Sent on the final chunk when stream_options.include_usage is set;
	// such chunks carry an empty choices array.
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// readableProviderError turns an HTTP error body into what a human
// should read. OpenAI-compatible servers answer with
// {"error":{"message":...,"code":...}} — show that message, not the
// JSON envelope; anything unparseable falls back to truncated raw
// text so the transcript never carries a wall of response body.
func readableProviderError(body []byte) string {
	var probe struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"` // some servers put it top-level
	}
	if err := json.Unmarshal(body, &probe); err == nil {
		if probe.Error.Message != "" {
			return probe.Error.Message
		}
		if probe.Message != "" {
			return probe.Message
		}
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// statusError renders a non-200 response for the transcript. Auth
// failures name the way out — a bare "401 Unauthorized" leaves the
// user guessing what to do next (audit U3). Other statuses keep the
// provider's own message.
func statusError(resp *http.Response, body []byte) error {
	detail := readableProviderError(body)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		if detail != "" {
			return fmt.Errorf("%s: %s — use /login <provider> to set a new key", resp.Status, detail)
		}
		return fmt.Errorf("%s — the API key was rejected; use /login <provider> to set a new key", resp.Status)
	case http.StatusForbidden:
		if detail != "" {
			return fmt.Errorf("%s: %s — this key lacks access; use /login <provider> or /model to switch", resp.Status, detail)
		}
		return fmt.Errorf("%s — this key lacks access; use /login <provider> or /model to switch", resp.Status)
	}
	if detail != "" {
		return fmt.Errorf("%s: %s", resp.Status, detail)
	}
	return fmt.Errorf("%s", resp.Status)
}

// toWire converts tilde's message history into the wire format.
func toWire(req ChatRequest) wireRequest {
	msgs := make([]wireMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, wireMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		wm := wireMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if len(m.Images) > 0 {
			// Multimodal form: text part first, then one image
			// part per attached image, each as a data URL. Tool
			// messages never carry images (they are assistant
			// results), so this only shapes user messages.
			parts := []wireContentPart{{Type: "text", Text: m.Content}}
			for _, img := range m.Images {
				parts = append(parts, wireContentPart{
					Type: "image_url",
					ImageURL: &wireImageURL{
						URL: "data:" + img.MimeType + ";base64," +
							base64.StdEncoding.EncodeToString(img.Data),
					},
				})
			}
			wm.Content = parts
		}
		for _, tc := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: wireFunction{Name: tc.Name, Arguments: tc.Arguments},
			})
		}
		msgs = append(msgs, wm)
	}
	wr := wireRequest{Model: req.Model, Messages: msgs, Stream: true,
		StreamOptions:   &wireStreamOptions{IncludeUsage: true},
		ReasoningEffort: req.ReasoningEffort}
	for _, t := range req.Tools {
		wr.Tools = append(wr.Tools, wireTool{
			Type:     "function",
			Function: wireToolFunc(t),
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
	// A derived context lets the idle watchdog cancel this request
	// alone, without touching the caller's context (timeout.go).
	streamCtx, cancel := context.WithCancel(ctx)
	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost,
		strings.TrimSuffix(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		cancel()
		// The host is enough for a human to recognize; the full URL
		// plus a raw net error is noise (audit U4).
		return nil, fmt.Errorf("could not reach %s: %w", httpReq.URL.Host, err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, statusError(resp, msg)
	}

	events := make(chan ChatEvent, 16)
	go p.consume(newIdleBody(resp.Body, cancel), events)
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
			// Real providers (OpenRouter notably) interleave non-JSON
			// data lines with the chunks. Skipping keeps the stream
			// alive; failing here kills a healthy turn. Errors that
			// matter arrive inside valid JSON, handled below.
			continue
		}
		if chunk.Error != nil {
			events <- ChatEvent{Type: ErrorEvent, Err: fmt.Errorf("provider error %d: %s",
				chunk.Error.Code, chunk.Error.Message)}
			return
		}
		if chunk.Usage != nil {
			events <- ChatEvent{Type: UsageEvent, Usage: Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
			}}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		r := choice.Delta.Reasoning
		if r == "" {
			r = choice.Delta.ReasoningContent
		}
		if r != "" {
			events <- ChatEvent{Type: ReasoningEvent, Text: r}
		}
		if choice.Delta.Content != "" {
			events <- ChatEvent{Type: TextEvent, Text: choice.Delta.Content}
		}
		for _, frag := range choice.Delta.ToolCalls {
			if frag.Index == nil {
				// A server that omits the index: routable only
				// when one call is in flight (see addUnindexed).
				asm.addUnindexed(frag.ID, frag.Function.Name, frag.Function.Arguments)
				continue
			}
			asm.add(*frag.Index, frag.ID, frag.Function.Name, frag.Function.Arguments)
		}
		// finish_reason marks the end of the model's output: whatever is
		// buffered is now complete. Keep reading afterwards — with
		// stream_options.include_usage the usage arrives in a final
		// chunk after this one. Some servers omit finish_reason entirely,
		// so flush after the loop too.
		if choice.FinishReason != nil && asm.hasContent() {
			for _, call := range asm.flush() {
				events <- ChatEvent{Type: ToolCallEvent, Call: call}
			}
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
