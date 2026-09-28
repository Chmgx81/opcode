package llm

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestToWireImageMessages(t *testing.T) {
	req := ChatRequest{
		System: "sys",
		Messages: []Message{
			{Role: "user", Content: "what is this?", Images: []Image{
				{MimeType: "image/png", Data: []byte("png-bytes")},
			}},
			{Role: "assistant", Content: "a cat", ToolCalls: []ToolCall{
				{ID: "c1", Name: "look", Arguments: "{}"},
			}},
			{Role: "tool", ToolCallID: "c1", Content: "saw a cat"},
			{Role: "user", Content: "plain"},
		},
	}
	wr := toWire(req)
	if len(wr.Messages) != 5 {
		t.Fatalf("messages = %d, want 5 (system + 4)", len(wr.Messages))
	}

	// The image message: content becomes the parts array — text part,
	// then one image part with the data URL.
	user := wr.Messages[1]
	parts, ok := user.Content.([]wireContentPart)
	if !ok {
		t.Fatalf("image message content is %T, want parts array", user.Content)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "what is this?" {
		t.Errorf("text part wrong: %+v", parts[0])
	}
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != wantURL {
		t.Errorf("image part wrong: %+v", parts[1])
	}

	// The wire form actually serializes as the multimodal array.
	blob, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Content []struct {
			Type     string `json:"type"`
			ImageURL *struct {
				URL string `json:"url"`
			} `json:"image_url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(blob, &probe); err != nil {
		t.Fatalf("content did not serialize as an array: %v", err)
	}
	if len(probe.Content) != 2 || probe.Content[1].ImageURL == nil {
		t.Errorf("serialized content wrong: %s", blob)
	}

	// Text-only and tool messages keep the plain string form.
	if s, ok := wr.Messages[4].Content.(string); !ok || s != "plain" {
		t.Errorf("text-only user content = %#v, want \"plain\"", wr.Messages[4].Content)
	}
	if s, ok := wr.Messages[3].Content.(string); !ok || s != "saw a cat" {
		t.Errorf("tool content = %#v, want the string result", wr.Messages[3].Content)
	}
}
