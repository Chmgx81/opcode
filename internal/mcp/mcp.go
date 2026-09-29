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
	"sync/atomic"
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

// response is any server line. Method is set for server-initiated
// requests and notifications, which are never responses even when they
// carry an id.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Method  string          `json:"method"`
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

// conn is one server process's response router. A background reader
// owns the process's stdout and hands each response to the roundtrip
// waiting on its id; everything else is dropped and counted. A dead
// process closes done instead of leaving a waiter hanging.
//
// Routing by id (rather than one shared line channel) means a response
// can never be dropped behind a burst of notifications, and concurrent
// callers can never consume each other's responses.
type conn struct {
	mu      sync.Mutex
	pending map[int]chan response
	closed  bool
	readErr error
	done    chan struct{} // closed when the reader exits
}

func newConn() *conn {
	return &conn{pending: map[int]chan response{}, done: make(chan struct{})}
}

// register reserves a slot for id's response. It fails once the reader
// has exited: nothing could ever fill the slot.
func (cn *conn) register(id int) (chan response, bool) {
	cn.mu.Lock()
	defer cn.mu.Unlock()
	if cn.closed {
		return nil, false
	}
	ch := make(chan response, 1)
	cn.pending[id] = ch
	return ch, true
}

func (cn *conn) unregister(id int) {
	cn.mu.Lock()
	delete(cn.pending, id)
	cn.mu.Unlock()
}

// readLoop runs until r is exhausted. Delivery never blocks (each slot
// is buffered for its single response), so a chatty server cannot wedge
// the reader, the OS pipe, or the server itself.
func (cn *conn) readLoop(r io.Reader, dropped *atomic.Int64) {
	defer close(cn.done)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var resp response
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil || resp.ID == nil || resp.Method != "" {
			// Non-JSON stdout noise, a notification, or a
			// server-initiated request: none answers a caller.
			dropped.Add(1)
			continue
		}
		cn.mu.Lock()
		ch, ok := cn.pending[*resp.ID]
		delete(cn.pending, *resp.ID)
		cn.mu.Unlock()
		if !ok {
			dropped.Add(1) // late answer to a call that already timed out
			continue
		}
		ch <- resp
	}
	cn.mu.Lock()
	cn.closed = true
	cn.readErr = scanner.Err()
	cn.mu.Unlock()
}

// Client owns one stdio MCP server connection. All roundtrips go
// through the current conn's reader goroutine, so notifications
// interleaving with responses are skipped and a dead process fails its
// waiters instead of hanging them.
type Client struct {
	name string
	cfg  ServerConfig

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	nextID int
	conn   *conn

	toolsMu sync.Mutex
	tools   []ToolDef

	// dropped counts server lines that answered no caller (unsolicited
	// notifications, stdout noise, late responses). Cumulative for the
	// client's lifetime.
	dropped atomic.Int64
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

// envAllowlist is what an MCP server may inherit. Everything else —
// in particular any *_KEY / *_TOKEN / *_SECRET — is dropped; the
// server's own mcp.json env config is appended after, so a server
// that genuinely needs a credential gets it explicitly, in a file
// the user wrote, not by ambient inheritance.
var envAllowlist = []string{"PATH", "HOME", "LANG", "LC_ALL", "TERM", "TMPDIR", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"}

func scrubbedEnv() []string {
	allowed := map[string]bool{}
	for _, k := range envAllowlist {
		allowed[k] = true
	}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if allowed[k] {
			out = append(out, kv)
		}
	}
	return out
}

func (c *Client) start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()

	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	// Least privilege (audit S7): the server sees PATH, HOME, and
	// the locale basics — not the user's API keys. A project server
	// the user just trusted should not also inherit OPENROUTER_API_KEY.
	cmd.Env = scrubbedEnv()
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
	cn := newConn()
	c.conn = cn

	// The reader goroutine owns stdout from here on (StdoutPipe must
	// have exactly one long-lived reader; MCP is one stream).
	go cn.readLoop(stdout, &c.dropped)

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

// DroppedLines reports how many server lines answered no caller
// (notifications, stdout noise, late responses). The count is
// cumulative for the client's lifetime and surfaces in the manager's
// notes, so a chatty server's noise is lost loudly, not silently.
func (c *Client) DroppedLines() int64 {
	return c.dropped.Load()
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
	cn := c.conn
	slot, ok := cn.register(id)
	if !ok {
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, errDead)
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		c.mu.Unlock()
		cn.unregister(id)
		return nil, fmt.Errorf("%s: %w", method, errDead)
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		cn.unregister(id)
		return nil, fmt.Errorf("%s: timed out", method)
	case resp := <-slot:
		return finish(method, resp)
	case <-cn.done:
		// The reader delivers before it exits, so a response that
		// raced the exit is already in the slot.
		select {
		case resp := <-slot:
			return finish(method, resp)
		default:
		}
		cn.mu.Lock()
		readErr := cn.readErr
		cn.mu.Unlock()
		if readErr != nil {
			return nil, fmt.Errorf("%s: %w", method, readErr)
		}
		return nil, fmt.Errorf("%s: %w", method, errDead)
	}
}

func finish(method string, resp response) (json.RawMessage, error) {
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: server error %d: %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
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
	// group; a no-op on Windows), then the process itself so Windows
	// and any server that left its group are covered too.
	if c.cmd.Process != nil {
		killProcGroup(c.cmd.Process.Pid)
		_ = c.cmd.Process.Kill()
	}
	// Wait closes our end of stdout, which unblocks the reader even if
	// a descendant that escaped the group still holds the write end
	// open. Waiting for EOF instead would wedge here, under c.mu, for
	// as long as that descendant lives. The reader never blocks on
	// delivery, so joining it afterwards is prompt; the bound is a
	// backstop, not a normal path.
	err := c.cmd.Wait()
	if c.conn != nil {
		select {
		case <-c.conn.done:
		case <-time.After(2 * time.Second):
		}
	}
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
