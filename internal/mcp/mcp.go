// Package mcp connects MCP servers (Section 3.5) over stdio and exposes
// their tools as ordinary tilde tools. The model never learns that MCP
// exists; it just sees more tools.
//
// Protocol: JSON-RPC 2.0, newline-delimited, per the MCP stdio
// transport. Handshake: initialize -> notifications/initialized ->
// tools/list; calls are tools/call.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// protocolVersion is the MCP revision tilde speaks. 2024-11-05 is the
// broadly supported baseline.
const protocolVersion = "2024-11-05"

// Timeouts: a slow server must never block startup (Section 3.5), and
// a hung call must never wedge a turn.
const (
	connectTimeout = 5 * time.Second
	callTimeout    = 30 * time.Second
)

// errDead marks a server process that has exited; Call restarts once
// on this before giving up.
var errDead = fmt.Errorf("server process exited")

// ServerConfig is one entry of mcp.json.
type ServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// Config is the parsed mcp.json.
type Config struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// LoadConfig reads one mcp.json. A missing file is an empty config —
// the common case — but a malformed one is a hard error: guessing at a
// server list is worse than stopping.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	for name, sc := range cfg.MCPServers {
		if sc.Command == "" {
			return Config{}, fmt.Errorf("%s: server %q has no command", path, name)
		}
		if strings.HasPrefix(sc.Command, "http://") || strings.HasPrefix(sc.Command, "https://") {
			return Config{}, fmt.Errorf("%s: server %q: http transport is not supported yet (stdio only)", path, name)
		}
	}
	return cfg, nil
}

// --- wire types ---

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ClientInfo      clientInfo      `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolDefWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []toolDefWire `json:"tools"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError"`
}

// ToolDef is one discovered MCP tool, server-relative (no mcp__ prefix).
type ToolDef struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// lineOrErr is one line from the server, or the reader's terminal error.
type lineOrErr struct {
	line string
	err  error
}

// Client owns one stdio MCP server connection. All roundtrips go
// through a background reader goroutine so notifications interleaving
// with responses are simply skipped, and a dead process becomes a
// closed channel instead of a hang.
type Client struct {
	name string
	cfg  ServerConfig

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	nextID int
	lines  chan lineOrErr

	toolsMu sync.Mutex
	tools   []ToolDef
}

// NewClient prepares a client for one configured server. Nothing spawns
// until Start.
func NewClient(name string, cfg ServerConfig) *Client {
	return &Client{name: name, cfg: cfg}
}

func (c *Client) Name() string { return c.name }

// Start spawns the server, handshakes, and fetches the tool list. It
// is also the restart path: any previous process is stopped first.
func (c *Client) Start(ctx context.Context) error {
	if err := c.start(ctx); err != nil {
		return err
	}
	// start() left the mutex released; the handshake and tool fetch
	// take it themselves as needed.
	if err := c.handshake(ctx); err != nil {
		_ = c.Stop()
		return fmt.Errorf("MCP server %s: %w", c.name, err)
	}
	tools, err := c.listTools(ctx)
	if err != nil {
		_ = c.Stop()
		return fmt.Errorf("MCP server %s: %w", c.name, err)
	}
	c.toolsMu.Lock()
	c.tools = tools
	c.toolsMu.Unlock()
	return nil
}

func (c *Client) start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()

	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range c.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Own process group: a hung server is killed with its children,
	// and the TUI's Ctrl+C never reaches it directly. (No-op on
	// Windows; see procsys_windows.go.)
	setProcGroup(cmd)
	// Servers log to stderr; Phase 4 has no debug view, so discard it.
	cmd.Stderr = nil

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start MCP server %s: %w", c.name, err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.nextID = 1
	lines := make(chan lineOrErr, 64)
	c.lines = lines

	// The reader goroutine owns stdout from here on (StdoutPipe must
	// have exactly one long-lived reader; MCP is one stream).
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "" {
				continue
			}
			lines <- lineOrErr{line: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			lines <- lineOrErr{err: err}
		}
	}()

	// The handshake runs after start() releases the mutex: roundtrip
	// locks it itself, and the mutex is not reentrant.
	return nil
}

// handshake performs initialize + initialized.
func (c *Client) handshake(ctx context.Context) error {
	params, _ := json.Marshal(initParams{
		ProtocolVersion: protocolVersion,
		Capabilities:    json.RawMessage(`{}`),
		ClientInfo:      clientInfo{Name: "tilde", Version: "0.1"},
	})
	if _, err := c.roundtrip(ctx, "initialize", params); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	// The initialized notification completes the handshake; no response
	// is expected.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return errDead
	}
	data, _ := json.Marshal(notification{JSONRPC: "2.0", Method: "notifications/initialized"})
	_, err := c.stdin.Write(append(data, '\n'))
	return err
}

// listTools fetches the server's inventory.
func (c *Client) listTools(ctx context.Context) ([]ToolDef, error) {
	raw, err := c.roundtrip(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	var list toolsListResult
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	defs := make([]ToolDef, 0, len(list.Tools))
	for _, t := range list.Tools {
		if t.InputSchema == nil {
			t.InputSchema = json.RawMessage(`{"type": "object"}`)
		}
		defs = append(defs, ToolDef{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return defs, nil
}

// Tools returns the discovered tools.
func (c *Client) Tools() []ToolDef {
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	return append([]ToolDef(nil), c.tools...)
}

// roundtrip sends one request and waits for the matching response,
// skipping any notifications the server emits meanwhile.
func (c *Client) roundtrip(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	c.mu.Lock()
	if c.stdin == nil {
		c.mu.Unlock()
		return nil, errDead
	}
	id := c.nextID
	c.nextID++
	req := request{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, errDead)
	}
	lines := c.lines
	c.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%s: timed out", method)
		case le, ok := <-lines:
			if !ok {
				return nil, fmt.Errorf("%s: %w", method, errDead)
			}
			if le.err != nil {
				return nil, fmt.Errorf("%s: %w", method, le.err)
			}
			var resp response
			if err := json.Unmarshal([]byte(le.line), &resp); err != nil {
				// Not JSON: a chatty server's stdout noise is skipped,
				// not fatal — other lines may still answer.
				continue
			}
			if resp.ID == nil || *resp.ID != id {
				continue // notification or someone else's response
			}
			if resp.Error != nil {
				return nil, fmt.Errorf("%s: server error %d: %s", method, resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}
}

// Call runs one tool. A dead process gets a single restart (with a full
// re-handshake) before the error surfaces.
func (c *Client) Call(ctx context.Context, tool string, arguments json.RawMessage) (string, error) {
	for attempt := 0; ; attempt++ {
		out, err := c.callOnce(ctx, tool, arguments)
		if err == nil {
			return out, nil
		}
		if attempt > 0 || err == nil || !strings.Contains(err.Error(), errDead.Error()) {
			return "", err
		}
		if err := c.Start(ctx); err != nil {
			return "", fmt.Errorf("MCP server %s restart: %w", c.name, err)
		}
	}
}

func (c *Client) callOnce(ctx context.Context, tool string, arguments json.RawMessage) (string, error) {
	if arguments == nil {
		arguments = json.RawMessage(`{}`)
	}
	params, _ := json.Marshal(callParams{Name: tool, Arguments: arguments})
	raw, err := c.roundtrip(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}
	var res callResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("MCP tool %s: malformed result: %w", tool, err)
	}
	var texts []string
	for _, part := range res.Content {
		if part.Type == "text" {
			texts = append(texts, part.Text)
		}
	}
	out := strings.Join(texts, "\n")
	if res.IsError {
		return "", fmt.Errorf("MCP tool %s: %s", tool, truncate(out, 200))
	}
	return out, nil
}

// Stop closes stdin and kills the server's process group.
func (c *Client) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopLocked()
}

func (c *Client) stopLocked() error {
	if c.cmd == nil {
		return nil
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	// Kill the whole process group (Setpgid put the server in its own
	// group; a no-op on Windows).
	if c.cmd.Process != nil {
		killProcGroup(c.cmd.Process.Pid)
	}
	err := c.cmd.Wait()
	c.cmd = nil
	c.stdin = nil
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
