// Anthropic Messages API client — the one built-in provider that is
// not OpenAI-compatible. Streams /v1/messages SSE into the same
// ChatEvent stream the OpenAI client produces, so the orchestrator
// is provider-agnostic: text deltas, reasoning deltas (extended
// thinking), completed tool calls, usage, and errors all map onto
// the same events.
package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// anthropicVersion is the required API version header.
const anthropicVersion = "2023-06-01"

// anthropicDefaultMaxTokens: Anthropic requires max_tokens; 0 on the
// request means this cap. It is well above the old 8192 because that
// number was too small twice over — it truncated long code edits, and
// it left no room under the cap for the thinking budget, so the
// medium and high effort postures asked for more thinking tokens than
// the response was allowed to contain and every request 400'd.
const anthropicDefaultMaxTokens = 32000

// Anthropic is an llm.Provider against the Messages API.
type Anthropic struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewAnthropic builds the client. baseURL is the API root
// (e.g. https://api.anthropic.com) — /v1/messages is appended.
func NewAnthropic(baseURL, apiKey string) *Anthropic {
	return &Anthropic{baseURL: strings.TrimSuffix(baseURL, "/"), apiKey: apiKey,
		client: newStreamHTTPClient()}
}

// New selects a provider implementation by wire API: "anthropic"
// gets the Messages client, anything else the OpenAI-compatible one.
// The three construction sites (main, /model, /login) share this.
func New(api, baseURL, apiKey string) Provider {
	if api == "anthropic" {
		return NewAnthropic(baseURL, apiKey)
	}
	return NewOpenAICompat(baseURL, apiKey)
}

// --- wire types -------------------------------------------------------

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMsg     `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream"`
	Thinking  *anthropicThinking `json:"thinking,omitempty"`
}

// anthropicThinking is Anthropic's extended-thinking budget. opcode's
// name→budget mapping: low is the documented minimum, medium a
// moderate default, high a generous cap.
type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// effortBudget maps the knob's names to Anthropic's token budgets.
func effortBudget(effort string) int {
	switch effort {
	case "low":
		return 1024
	case "medium":
		return 8192
	case "high":
		return 16384
	}
	return 0
}

// thinkingBudget returns the budget_tokens to ask for, or 0 to leave
// extended thinking off. Anthropic requires budget_tokens to be at
// least 1024 AND strictly less than max_tokens — the default 8192
// max_tokens is smaller than the medium and high budgets, so asking
// for them verbatim is a hard 400 on every request. Headroom is
// reserved so the model still has room to answer.
func thinkingBudget(effort string, maxTokens int) int {
	n := effortBudget(effort)
	if n < 1024 {
		return 0
	}
	room := maxTokens - 1024
	if room < 1024 {
		return 0
	}
	if n > room {
		return room
	}
	return n
}

// anthropicMsg is one message: role plus content, which is either a
// plain string or an array of typed blocks.
type anthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func textMsg(role, text string) anthropicMsg {
	raw, _ := json.Marshal(text)
	return anthropicMsg{Role: role, Content: raw}
}

func blocksMsg(role string, blocks []map[string]any) anthropicMsg {
	raw, _ := json.Marshal(blocks)
	return anthropicMsg{Role: role, Content: raw}
}

// toAnthropic converts opcode's history into Messages-API form. The
// mapping is the whole compatibility story:
//
//	system            → top-level system param
//	user text         → string content; images become image blocks
//	assistant + calls → text block + one tool_use block per call
//	tool result       → user message with a tool_result block
func toAnthropic(req ChatRequest) anthropicRequest {
	ar := anthropicRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
		Stream:    true,
	}
	if ar.MaxTokens <= 0 {
		ar.MaxTokens = anthropicDefaultMaxTokens
	}
	if n := thinkingBudget(req.ReasoningEffort, ar.MaxTokens); n > 0 {
		ar.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: n}
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			if len(m.Images) == 0 {
				ar.Messages = append(ar.Messages, textMsg("user", m.Content))
				continue
			}
			blocks := []map[string]any{}
			for _, img := range m.Images {
				blocks = append(blocks, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": img.MimeType,
						"data":       img.base64(),
					},
				})
			}
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			ar.Messages = append(ar.Messages, blocksMsg("user", blocks))
		case "assistant":
			var blocks []map[string]any
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any
				if json.Valid([]byte(tc.Arguments)) {
					_ = json.Unmarshal([]byte(tc.Arguments), &input)
				} else {
					input = map[string]any{}
				}
				blocks = append(blocks, map[string]any{
					"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": input,
				})
			}
			if len(blocks) == 0 {
				blocks = []map[string]any{{"type": "text", "text": ""}}
			}
			ar.Messages = append(ar.Messages, blocksMsg("assistant", blocks))
		case "tool":
			// Tool results ride as user messages with tool_result
			// blocks keyed by the tool_use id.
			ar.Messages = append(ar.Messages, blocksMsg("user", []map[string]any{{
				"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content,
			}}))
		}
	}
	for _, t := range req.Tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		ar.Tools = append(ar.Tools, anthropicTool{
			Name: t.Name, Description: t.Description, InputSchema: schema,
		})
	}
	return ar
}

// --- streaming --------------------------------------------------------

// anthropicEvent is one SSE data payload; Type discriminates.
type anthropicEvent struct {
	Type    string `json:"type"`
	Message struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Index        int `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// StreamChat performs one streaming request and returns the event
// channel; it closes when the stream ends.
func (a *Anthropic) StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	body, err := json.Marshal(toAnthropic(req))
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	// A derived context lets the idle watchdog cancel this request
	// alone, without touching the caller's context (timeout.go).
	streamCtx, cancel := context.WithCancel(ctx)
	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost,
		a.baseURL+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		cancel()
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		cancel()
		// Host only, and in plain words — the full URL plus a raw
		// net error is noise (audit U4).
		return nil, fmt.Errorf("could not reach %s: %w", httpReq.URL.Host, err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, statusError(resp, msg)
	}

	events := make(chan ChatEvent, 32)
	// The stream reader owns the body now: consume closes `events`
	// on every path, and Close follows in its defer. Without this,
	// every Anthropic turn leaked the response body and its TCP
	// connection (audit C1) — a long session would exhaust fds.
	stream := newIdleBody(resp.Body, cancel)
	go a.consume(&closingReader{r: stream, closer: stream}, events)
	return events, nil
}

// closingReader closes the HTTP response body exactly once when the
// consume goroutine finishes, on success or error alike.
type closingReader struct {
	r      io.Reader
	closer io.Closer
	once   sync.Once
}

func (c *closingReader) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *closingReader) Close() { c.once.Do(func() { _ = c.closer.Close() }) }

// anthropicBlock is one content block the model opened. Anthropic
// interleaves text, thinking and tool_use blocks by index, streaming
// each one's deltas before stopping it.
type anthropicBlock struct {
	kind string // "text" | "thinking" | "tool_use"
	id   string
	name string
	json strings.Builder
}

// consume parses the SSE stream into ChatEvents. Tool calls arrive
// as input_json_delta fragments and are emitted complete at
// content_block_stop — the same policy as the OpenAI client.
func (a *Anthropic) consume(r *closingReader, events chan<- ChatEvent) {
	defer close(events)
	defer r.Close()

	blocks := map[int]*anthropicBlock{}
	inputTokens := 0

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] == ':' {
			continue // keep-alive comments
		}
		data, ok := sseData(line)
		if !ok {
			continue
		}
		var ev anthropicEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue // tolerate unknown event types
		}
		switch ev.Type {
		case "message_start":
			inputTokens = ev.Message.Usage.InputTokens
		case "content_block_start":
			blocks[ev.Index] = &anthropicBlock{kind: ev.ContentBlock.Type,
				id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
		case "content_block_delta":
			b := blocks[ev.Index]
			if b == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				events <- ChatEvent{Type: TextEvent, Text: ev.Delta.Text}
			case "thinking_delta":
				events <- ChatEvent{Type: ReasoningEvent, Text: ev.Delta.Thinking}
			case "input_json_delta":
				b.json.WriteString(ev.Delta.PartialJSON)
			}
		case "content_block_stop":
			if b := blocks[ev.Index]; b != nil && b.kind == "tool_use" {
				events <- ChatEvent{Type: ToolCallEvent, Call: ToolCall{
					ID: b.id, Name: b.name, Arguments: b.json.String(),
				}}
				// Drop the block once it has been emitted. Leaving it
				// here re-emitted it in the EOF flush below, so every
				// tool call ran twice per round — a write applied twice,
				// a command executed twice, and a loop of it.
				delete(blocks, ev.Index)
			}
		case "message_delta":
			if ev.Usage.OutputTokens > 0 || inputTokens > 0 {
				events <- ChatEvent{Type: UsageEvent, Usage: Usage{
					PromptTokens:     inputTokens,
					CompletionTokens: ev.Usage.OutputTokens,
				}}
			}
		case "error":
			msg := "stream error"
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			}
			events <- ChatEvent{Type: ErrorEvent, Err: fmt.Errorf("%s", msg)}
			return
		}
	}
	if err := scanner.Err(); err != nil {
		events <- ChatEvent{Type: ErrorEvent, Err: fmt.Errorf("read stream: %w", err)}
		return
	}
	// A clean EOF without content_block_stop: the OpenAI client
	// flushes buffered calls at stream end, and so does this one —
	// a tool_use block open at EOF is emitted, not dropped. Blocks
	// that already emitted at content_block_stop are deleted from the
	// map, so this only ever reclaims what never stopped.
	for i := 0; i <= maxBlockIndex(blocks); i++ {
		b := blocks[i]
		if b != nil && b.kind == "tool_use" && (b.id != "" || b.name != "" || b.json.Len() > 0) {
			events <- ChatEvent{Type: ToolCallEvent, Call: ToolCall{
				ID: b.id, Name: b.name, Arguments: b.json.String(),
			}}
		}
	}
}

// maxBlockIndex is the highest content-block index seen. Anthropic
// numbers blocks from zero, so flushing in index order (rather than
// map order) emits the calls the way the model wrote them — two
// parallel calls must not swap places between runs.
func maxBlockIndex(blocks map[int]*anthropicBlock) int {
	max := -1
	for i := range blocks {
		if i > max {
			max = i
		}
	}
	return max
}

// sseData strips the "data:" prefix from one SSE line, with or without
// the conventional trailing space — one server formatting choice must
// not drop events.
func sseData(line []byte) ([]byte, bool) {
	s := string(line)
	if strings.HasPrefix(s, "data:") {
		return []byte(strings.TrimPrefix(s, "data:")), true
	}
	return nil, false
}
