package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Chmgx81/tilde/internal/tools"
)

func fixturePath(mode string) []string {
	python, err := exec.LookPath("python3")
	if err != nil {
		python = "python3"
	}
	return []string{python, "testdata/fixture_server.py", mode}
}

func connectFixture(t *testing.T, mode string) *Client {
	t.Helper()
	c := NewClient("fixture", ServerConfig{Command: fixturePath(mode)[0], Args: fixturePath(mode)[1:]})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })
	return c
}

func TestHandshakeAndToolDiscovery(t *testing.T) {
	c := connectFixture(t, "echo")

	got := c.Tools()
	if len(got) != 1 || got[0].Name != "echo" {
		t.Fatalf("tools = %+v, want one echo tool", got)
	}
	if got[0].Description == "" || got[0].InputSchema == nil {
		t.Errorf("tool metadata incomplete: %+v", got[0])
	}
	var schema map[string]any
	if err := json.Unmarshal(got[0].InputSchema, &schema); err != nil {
		t.Errorf("inputSchema is not valid JSON: %v", err)
	}
}

func TestToolCallRoundtrip(t *testing.T) {
	c := connectFixture(t, "echo")
	out, err := c.Call(context.Background(), "echo", json.RawMessage(`{"text": "hello mcp"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out != "echo: hello mcp" {
		t.Errorf("out = %q", out)
	}
}

func TestIsErrorPropagates(t *testing.T) {
	c := connectFixture(t, "echo")
	_, err := c.Call(context.Background(), "echo", json.RawMessage(`{"text": "fail-me"}`))
	if err == nil {
		t.Fatal("isError result must surface as a tool error")
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("error should carry the server's message: %v", err)
	}
}

func TestRestartOnceAfterCrash(t *testing.T) {
	// die-once: the server crashes on its first tools/call (a marker
	// file records that the crash happened), then behaves normally.
	marker := filepath.Join(t.TempDir(), "crashed")
	args := []string{"testdata/fixture_server.py", "die-once", marker}
	c := NewClient("fixture", ServerConfig{Command: fixturePath("echo")[0], Args: args})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })

	// The first call crashes the server; the client restarts it (full
	// re-handshake) and the retry succeeds.
	out, err := c.Call(context.Background(), "echo", json.RawMessage(`{"text": "after crash"}`))
	if err != nil {
		t.Fatalf("restart-once must make the call succeed: %v", err)
	}
	if out != "echo: after crash" {
		t.Errorf("out = %q", out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("fixture never recorded its crash — the test proved nothing")
	}
}

func TestDeadServerRestart(t *testing.T) {
	c := connectFixture(t, "echo")

	// Kill the process out of band; the next call must restart and work.
	c.mu.Lock()
	proc := c.cmd.Process
	c.mu.Unlock()
	if err := syscall.Kill(-proc.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	out, err := c.Call(context.Background(), "echo", json.RawMessage(`{"text": "revived"}`))
	if err != nil {
		t.Fatalf("call after kill: %v", err)
	}
	if out != "echo: revived" {
		t.Errorf("out = %q", out)
	}
}

func TestSilentServerTimesOut(t *testing.T) {
	// Through the Manager — the real startup path: the per-server
	// connect timeout must bound a server that never answers, so
	// startup never blocks on it.
	m := NewManager()
	t.Cleanup(m.Close)

	start := time.Now()
	_, notes := m.Connect(context.Background(), Config{MCPServers: map[string]ServerConfig{
		"silent": {Command: fixturePath("silent")[0], Args: fixturePath("silent")[1:]},
	}})
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "silent failed to connect") {
		t.Fatalf("silent server not reported as failed: %v", notes)
	}
	if !strings.Contains(joined, "timed out") {
		t.Errorf("failure should say timed out: %v", notes)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Errorf("Connect took %s; the 5s per-server timeout should bound it", elapsed)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()

	// Missing file: empty, not an error.
	cfg, err := LoadConfig(filepath.Join(dir, "mcp.json"))
	if err != nil || len(cfg.MCPServers) != 0 {
		t.Fatalf("missing file: cfg=%v err=%v", cfg, err)
	}

	path := filepath.Join(dir, "mcp.json")
	os.WriteFile(path, []byte(`{"mcpServers": {"demo": {"command": "echo", "args": ["hi"]}}}`), 0o644)
	cfg, err = LoadConfig(path)
	if err != nil || cfg.MCPServers["demo"].Command != "echo" {
		t.Fatalf("cfg=%v err=%v", cfg, err)
	}

	// Malformed and empty-command entries fail loudly.
	os.WriteFile(path, []byte(`{`), 0o644)
	if _, err := LoadConfig(path); err == nil {
		t.Error("malformed file must error")
	}
	os.WriteFile(path, []byte(`{"mcpServers": {"x": {"args": []}}}`), 0o644)
	if _, err := LoadConfig(path); err == nil {
		t.Error("server without a command must error")
	}
	// HTTP transport is a clear rejection, not a silent no-op.
	os.WriteFile(path, []byte(`{"mcpServers": {"x": {"command": "https://example.com/sse"}}}`), 0o644)
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "stdio only") {
		t.Errorf("http server must be rejected: %v", err)
	}
}

func TestManagerConnectParallelWithNotes(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)

	cfg := Config{MCPServers: map[string]ServerConfig{
		"good": {Command: fixturePath("echo")[0], Args: fixturePath("echo")[1:]},
		"dead": {Command: "/nonexistent-binary-tilde"},
	}}
	toolList, notes := m.Connect(context.Background(), cfg)

	if len(toolList) != 1 || toolList[0].Name() != "mcp__good__echo" {
		t.Errorf("tools = %v", namesOf(toolList))
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "mcp: good (1 tools)") {
		t.Errorf("success note missing: %v", notes)
	}
	if !strings.Contains(joined, "dead failed to connect") {
		t.Errorf("failure note missing: %v", notes)
	}

	// Tier and description flow through the adapter.
	if toolList[0].Tier() != tools.TierActionAllowed {
		t.Error("MCP tools must be Action-Allowed")
	}
	if toolList[0].Description() != "Echoes its input back" {
		t.Errorf("description = %q", toolList[0].Description())
	}

	// The adapter executes a real call end to end.
	out, err := toolList[0].Execute(context.Background(), `{"text": "via adapter"}`)
	if err != nil || out != "echo: via adapter" {
		t.Errorf("adapter call: out=%q err=%v", out, err)
	}
}

func TestManagerProjectCannotShadowUserServer(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)

	fx := ServerConfig{Command: fixturePath("echo")[0], Args: fixturePath("echo")[1:]}
	user := Config{MCPServers: map[string]ServerConfig{"shared": fx}}
	project := Config{MCPServers: map[string]ServerConfig{
		"shared": {Command: "/nonexistent-hijacker"}, // must never run
	}}

	if _, _ = m.Connect(context.Background(), user); false {
		t.Fatal("unreachable")
	}
	toolList, notes := m.Connect(context.Background(), project)

	if len(toolList) != 0 {
		t.Errorf("project server shadowed the user's: %v", namesOf(toolList))
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "skipped") {
		t.Errorf("collision note missing: %v", notes)
	}
	// And the working user server is still the one connected.
	c, _ := m.clients["shared"]
	if c == nil || len(c.Tools()) != 1 {
		t.Error("user server lost after project connect")
	}
}

func namesOf(ts []Tool) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name())
	}
	return out
}
