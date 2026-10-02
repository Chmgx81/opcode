package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Chmgx81/opcode/internal/tools"
)

// Tool adapts one discovered MCP tool to opcode's tool interface. Every
// MCP tool is Action-Allowed: the protocol's readOnlyHint is
// self-reported by the server, and a server claiming to be read-only is
// not a permission model. The gate still applies, so ask mode prompts.
type Tool struct {
	client   *Client
	def      ToolDef
	wireName string
}

// WireName is the collision-proof name the model sees.
func WireName(server, tool string) string { return "mcp__" + server + "__" + tool }

func (t Tool) Name() string { return t.wireName }
func (t Tool) Description() string {
	if t.def.Description == "" {
		return "Tool " + t.def.Name + " from MCP server " + t.client.name
	}
	return t.def.Description
}
func (t Tool) Parameters() json.RawMessage { return t.def.InputSchema }
func (t Tool) Tier() tools.Tier            { return tools.TierActionAllowed }

func (t Tool) Execute(ctx context.Context, args string) (string, error) {
	if args == "" {
		args = "{}"
	}
	return t.client.Call(ctx, t.def.Name, json.RawMessage(args))
}

// Manager owns the connected clients. Servers connect in parallel with
// a per-server timeout; a slow or dead one is skipped with a visible
// note and the session continues without it (Section 3.5: startup must
// never block on a bad server).
type Manager struct {
	mu      sync.Mutex
	clients map[string]*Client
}

func NewManager() *Manager {
	return &Manager{clients: map[string]*Client{}}
}

// Connect starts every server in cfg not already connected and returns
// the tools and startup notes for the servers added by this call.
//
// The collision rule lives here: a later Connect cannot replace an
// already-connected server's name — connecting a project's mcp.json
// after the user's means a project can never silently hijack the
// user's "github" server; its same-named entry is skipped with a note.
func (m *Manager) Connect(ctx context.Context, cfg Config) ([]Tool, []string) {
	type result struct {
		name   string
		client *Client
		err    error
	}

	var names, collided []string
	m.mu.Lock()
	for name := range cfg.MCPServers {
		if _, exists := m.clients[name]; exists {
			collided = append(collided, name)
			continue
		}
		names = append(names, name)
		m.clients[name] = NewClient(name, cfg.MCPServers[name])
	}
	clients := make([]*Client, 0, len(names))
	for _, name := range names {
		clients = append(clients, m.clients[name])
	}
	m.mu.Unlock()

	results := make([]result, len(clients))
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *Client) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, connectTimeout)
			defer cancel()
			results[i] = result{name: c.Name(), client: c, err: c.Start(cctx)}
		}(i, c)
	}
	wg.Wait()

	var allTools []Tool
	var notes []string
	for _, name := range collided {
		notes = append(notes, fmt.Sprintf(
			"mcp: %s skipped (a server with this name is already connected; the project entry does not replace it)", name))
	}
	for _, r := range results {
		if r.err != nil {
			notes = append(notes, fmt.Sprintf("mcp: %s failed to connect: %v", r.name, r.err))
			m.mu.Lock()
			delete(m.clients, r.name)
			m.mu.Unlock()
			continue
		}
		for _, def := range r.client.Tools() {
			allTools = append(allTools, Tool{
				client:   r.client,
				def:      def,
				wireName: WireName(r.name, def.Name),
			})
		}
		notes = append(notes, fmt.Sprintf("mcp: %s (%d tools)", r.name, len(r.client.Tools())))
	}
	return allTools, notes
}

// Notes describes the connected servers (name and tool count), for the
// TUI's /mcp command and startup display.
func (m *Manager) Notes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for name, c := range m.clients {
		note := fmt.Sprintf("%s · %d tools", name, len(c.Tools()))
		// Dropped lines are reported, not hidden: the user should
		// know a server is noisier than opcode is willing to buffer.
		if n := c.DroppedLines(); n > 0 {
			note += fmt.Sprintf(" · %d lines dropped", n)
		}
		out = append(out, note)
	}
	return out
}

// Close stops every server.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clients {
		_ = c.Stop()
	}
	m.clients = map[string]*Client{}
}
