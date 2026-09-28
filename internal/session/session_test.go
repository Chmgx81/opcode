package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chmgx81/tilde/internal/llm"
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
	if len(s.BranchIDs()) != 2 {
		t.Errorf("leaves = %v, want both branches", s.BranchIDs())
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
