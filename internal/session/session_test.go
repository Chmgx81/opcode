package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Chmgx81/opcode/internal/llm"
)

func TestFromHistoryAndBack(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi", ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "read_file", Arguments: `{"path": "x"}`}}},
		{Role: "tool", ToolCallID: "c1", Content: "contents"},
		{Role: "assistant", Content: "done"},
	}
	s := FromHistory("model-a", "ask-every-time", history)
	if s.Model != "model-a" || s.Mode != "ask-every-time" {
		t.Errorf("meta = %s/%s", s.Model, s.Mode)
	}
	got := s.History()
	if len(got) != len(history) {
		t.Fatalf("round trip lost messages: %d -> %d", len(history), len(got))
	}
	for i := range history {
		if got[i].Role != history[i].Role || got[i].Content != history[i].Content {
			t.Errorf("message %d mismatch: %+v vs %+v", i, got[i], history[i])
		}
		if len(got[i].ToolCalls) != len(history[i].ToolCalls) {
			t.Errorf("message %d tool calls mismatch", i)
		}
	}
}

func TestSaveRedactsAndLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	history := []llm.Message{
		{Role: "user", Content: "my key is sk-super-secret-123 please use it"},
		{Role: "assistant", Content: "ok sk-super-secret-123"},
	}
	s := FromHistory("m", "full-auto", history)
	path := filepath.Join(dir, "sessions", "test.json")
	if err := s.Save(path, []string{"sk-super-secret-123"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "sk-super-secret-123") {
		t.Error("session file leaks the credential")
	}
	if !strings.Contains(string(data), "[redacted]") {
		t.Error("no redaction marker in the session file")
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session file mode = %v, want 0600", perm)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.History()[0].Content != "my key is [redacted] please use it" {
		t.Errorf("redacted content = %q", loaded.History()[0].Content)
	}
}

func TestBranching(t *testing.T) {
	// Rewind to the first user message and continue: the new message
	// becomes a sibling branch, not an overwrite.
	history := []llm.Message{
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
	}
	s := FromHistory("m", "ask-every-time", history)
	root := s.Root

	// Rewind: continue from the user message instead of the leaf.
	s.Active = root
	fork := &Node{ID: s.NextID(), Parent: root, CreatedAt: "now",
		Role: "user", Content: "different question entirely"}
	s.Nodes[fork.ID] = fork
	s.Active = fork.ID

	// Both branches still exist, and History walks the new branch.
	leaves := 0
	for _, n := range s.Nodes {
		hasChild := false
		for _, m := range s.Nodes {
			if m.Parent == n.ID {
				hasChild = true
				break
			}
		}
		if !hasChild {
			leaves++
		}
	}
	if leaves != 2 {
		t.Errorf("leaves = %d, want both branches", leaves)
	}
	h := s.History()
	if len(h) != 2 || h[0].Content != "first question" || h[1].Content != "different question entirely" {
		t.Errorf("branch history = %+v", h)
	}
	// The old answer is still in the file's tree.
	if s.Nodes[s.Root] == nil {
		t.Fatal("root lost")
	}
	var foundOld bool
	for _, n := range s.Nodes {
		if n.Content == "first answer" {
			foundOld = true
		}
	}
	if !foundOld {
		t.Error("the other branch's messages were destroyed — a rewind must branch, not overwrite")
	}
}

func TestLatest(t *testing.T) {
	dir := t.TempDir()
	if s, path, err := Latest(dir); s != nil || path != "" || err != nil {
		t.Fatalf("empty dir: %v %v %v", s, path, err)
	}

	s1 := FromHistory("m", "ask-every-time", []llm.Message{{Role: "user", Content: "a"}})
	if err := s1.Save(filepath.Join(dir, "20260101-000001-aaaa.json"), nil); err != nil {
		t.Fatal(err)
	}
	s2 := FromHistory("m", "ask-every-time", []llm.Message{{Role: "user", Content: "b"}})
	if err := s2.Save(filepath.Join(dir, "20260102-000001-bbbb.json"), nil); err != nil {
		t.Fatal(err)
	}

	got, path, err := Latest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || path == "" {
		t.Fatal("latest not found")
	}
	if strings.HasSuffix(path, "bbbb.json") != true {
		t.Errorf("latest = %s, want the chronologically last", path)
	}
}

func TestNewFileAndEmptySession(t *testing.T) {
	userDir := t.TempDir()
	path := NewFile(userDir)
	if !strings.HasPrefix(path, filepath.Join(Dir(userDir))) {
		t.Errorf("path = %q", path)
	}
	s := New("m", "mode")
	if s.History() != nil {
		t.Error("empty session must have no history")
	}
}

// TestSaveDoesNotMutateTheCallersTree: redaction is a property of the
// FILE, not of the conversation. The caller's tree is the live history
// the model is talking about; rewriting it in place replaces text the
// model already saw with "[redacted]", and a second Save of the same
// tree then differs from the first.
func TestSaveDoesNotMutateTheCallersTree(t *testing.T) {
	dir := t.TempDir()
	s := FromHistory("m", "plan", []llm.Message{
		{Role: "user", Content: "my key is sk-secret-value"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "bash", Arguments: `{"command": "curl -H 'tok: sk-secret-value'"}`},
			{ID: "c2", Name: "read_file", Arguments: `{"path": "notes.txt"}`}}},
		{Role: "user", Images: []llm.Image{{MimeType: "image/png", Data: []byte("png bytes")}}},
	})
	before := deepCopy(t, s)

	path := filepath.Join(dir, "s.json")
	if err := s.Save(path, []string{"sk-secret-value"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !reflect.DeepEqual(s, before) {
		t.Errorf("Save changed the caller's tree:\nbefore %s\nafter  %s", mustJSON(t, before), mustJSON(t, s))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-secret-value") {
		t.Errorf("the saved file leaks the credential:\n%s", data)
	}
	if !strings.Contains(string(data), "[redacted]") {
		t.Error("nothing was redacted in the saved file")
	}
	// Idempotent from the caller's side: the same tree saves the same
	// bytes twice, which is only true if Save wrote a copy.
	if err := s.Save(path, []string{"sk-secret-value"}); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Error("saving the same tree twice produced two different files")
	}
}

// deepCopy clones a session through JSON, so the comparison against the
// caller's own pointers cannot be fooled by shared state.
func deepCopy(t *testing.T, s *Session) *Session {
	t.Helper()
	var out Session
	if err := json.Unmarshal([]byte(mustJSON(t, s)), &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func mustJSON(t *testing.T, s *Session) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestImagesSurviveSaveAndResume: the model saw the attachment once, so
// a resumed session has to see it again. Without it the turn resumes as
// an empty user message, which every provider rejects — "screenshot,
// /exit, --continue" broke the session outright.
func TestImagesSurviveSaveAndResume(t *testing.T) {
	first := llm.Image{MimeType: "image/png", Data: []byte("\x89PNG first shot")}
	second := llm.Image{MimeType: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff, 0xe0}}
	history := []llm.Message{
		{Role: "user", Content: "what does this show?", Images: []llm.Image{first}},
		{Role: "assistant", Content: "a chart"},
		// An attachment with no text at all: the shape that used to
		// resume as an empty, rejected user message.
		{Role: "user", Images: []llm.Image{second}},
		{Role: "assistant", Content: "another chart"},
	}
	path := filepath.Join(t.TempDir(), "s.json")
	if err := FromHistory("m", "plan", history).Save(path, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := loaded.History()
	if len(got) != len(history) {
		t.Fatalf("resumed %d messages, want %d", len(got), len(history))
	}
	if len(got[0].Images) != 1 || got[0].Images[0].MimeType != first.MimeType ||
		!bytes.Equal(got[0].Images[0].Data, first.Data) {
		t.Errorf("the attachment was lost: %+v", got[0].Images)
	}
	if got[0].Content != "what does this show?" {
		t.Errorf("content = %q, want the caption", got[0].Content)
	}
	// An image-only turn keeps its image and is NOT given placeholder
	// text: it is a complete message.
	if len(got[2].Images) != 1 || !bytes.Equal(got[2].Images[0].Data, second.Data) {
		t.Errorf("the image-only turn lost its image: %+v", got[2])
	}
	if got[2].Content != "" {
		t.Errorf("content = %q, want it left alone", got[2].Content)
	}
}

// TestLoadSubstitutesTextForAnImageOnlyMessage: a session file written
// before attachments were persisted (or edited by hand) can hold a user
// message with no content at all. Resumed as it stands, that is an
// empty user turn and every provider rejects it, so the session could
// not continue at all.
func TestLoadSubstitutesTextForAnImageOnlyMessage(t *testing.T) {
	tests := []struct {
		name string
		node string
		want bool
	}{
		{"user message with nothing in it", `{"id":"a","role":"user","content":""}`, true},
		{"user message with text", `{"id":"a","role":"user","content":"hello"}`, false},
		{"assistant message with nothing in it", `{"id":"a","role":"assistant","content":""}`, false},
		{"tool result with nothing in it", `{"id":"a","role":"tool","tool_call_id":"c1","content":""}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.json")
			body := `{"id":"x","root":"a","active":"a","nodes":{"a":` + tc.node + `}}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got := s.History()[0].Content
			if substituted := got == missingImageNote; substituted != tc.want {
				t.Errorf("content = %q, want substituted=%v", got, tc.want)
			}
		})
	}
}
