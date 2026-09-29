// Package session implements tilde's tree-structured session storage
// (architecture doc Section 3.7): every message is a node with a
// parent, rewinding and continuing creates a branch, and all branches
// live in one JSON file. Credentials are redacted before anything is
// written because sessions get exported and shared.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/tools"
)

// Node is one message in the conversation tree.
type Node struct {
	ID         string         `json:"id"`
	Parent     string         `json:"parent,omitempty"`
	CreatedAt  string         `json:"created_at"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// Session is one conversation tree plus its pointer state.
type Session struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Mode  string `json:"mode"`

	// Nodes keyed by ID; Root is the first node; Active is the leaf
	// the conversation continues from (rewinding moves it and the
	// next message becomes a new branch).
	Nodes  map[string]*Node `json:"nodes"`
	Root   string           `json:"root"`
	Active string           `json:"active"`
}

// New creates an empty session.
func New(model, mode string) *Session {
	return &Session{ID: newID(), Model: model, Mode: mode, Nodes: map[string]*Node{}}
}

func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// FromHistory builds a linear session from an orchestrator history.
// Every message becomes a node parented on the previous one — a chain
// that the format can branch later.
func FromHistory(model, mode string, history []llm.Message) *Session {
	s := New(model, mode)
	var prev string
	for _, m := range history {
		node := &Node{
			ID:         s.NextID(),
			Parent:     prev,
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
		s.Nodes[node.ID] = node
		if s.Root == "" {
			s.Root = node.ID
		}
		prev = node.ID
	}
	s.Active = prev
	return s
}

// NextID returns an unused node ID.
func (s *Session) NextID() string {
	for {
		id := newID()
		if _, taken := s.Nodes[id]; !taken {
			return id
		}
	}
}

// History walks root -> Active and returns the messages on that path.
func (s *Session) History() []llm.Message {
	if s.Root == "" {
		return nil
	}
	// Walk up from Active to Root, then reverse.
	// A parent cycle in a hand-edited or damaged file would otherwise
	// loop forever; a node is only ever visited once.
	var chain []string
	seen := make(map[string]bool, len(s.Nodes))
	for id := s.Active; id != ""; {
		node, ok := s.Nodes[id]
		if !ok || seen[id] {
			break
		}
		seen[id] = true
		chain = append(chain, id)
		if id == s.Root {
			break
		}
		id = node.Parent
	}
	// Reverse into root-first order.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	out := make([]llm.Message, 0, len(chain))
	for _, id := range chain {
		n := s.Nodes[id]
		out = append(out, llm.Message{
			Role:       n.Role,
			Content:    n.Content,
			ToolCalls:  n.ToolCalls,
			ToolCallID: n.ToolCallID,
		})
	}
	return out
}

// Save writes the session to path with credential values redacted
// (Section 3.10: sessions get exported and shared).
func (s *Session) Save(path string, secrets []string) error {
	redactor := tools.NewRedactor(secrets...)
	for _, n := range s.Nodes {
		n.Content = redactor.Redact(n.Content)
		for i, tc := range n.ToolCalls {
			tc.Arguments = redactor.Redact(tc.Arguments)
			n.ToolCalls[i] = tc
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Atomic: the autosave rewrites this file every turn, and a crash
	// mid-write must leave the previous complete session in place.
	return config.WriteFileAtomic(path, data, 0o600)
}

// Load reads a session file.
func Load(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse session %s: %w", path, err)
	}
	if s.Nodes == nil {
		s.Nodes = map[string]*Node{}
	}
	return &s, nil
}

// Dir is the sessions directory under the user-level home.
func Dir(userDir string) string { return filepath.Join(userDir, "sessions") }

// Latest returns the newest session file in dir (by name), or nil.
// Names sort chronologically because IDs embed a timestamp prefix.
func Latest(dir string) (*Session, string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, "", nil
	}
	sort.Strings(names)
	path := filepath.Join(dir, names[len(names)-1])
	s, err := Load(path)
	return s, path, err
}

// NewFile creates the file path for a fresh session: the sessions dir
// plus a timestamped, unique name.
func NewFile(userDir string) string {
	name := time.Now().UTC().Format("20060102-150405") + "-" + newID() + ".json"
	return filepath.Join(Dir(userDir), name)
}
