package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Chmgx81/opcode/internal/tools"
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
	if err := proc.Kill(); err != nil {
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

func TestChattyServerFloodSurvivesAndStillServes(t *testing.T) {
	// C8: the fixture answers a tools/call, then floods 5000
	// notification lines while the client is idle between requests.
	// The reader must keep consuming (dropping with a count), or the OS
	// pipe fills and the server wedges in write().
	c := connectFixture(t, "chatty")

	out, err := c.Call(context.Background(), "echo", json.RawMessage(`{"text": "before flood"}`))
	if err != nil || out != "echo: before flood" {
		t.Fatalf("first call: out=%q err=%v", out, err)
	}

	// Let the flood land: no call is waiting, so every line is counted
	// as dropped rather than buffered.
	time.Sleep(500 * time.Millisecond)
	if got := c.DroppedLines(); got < 5000 {
		t.Fatalf("dropped = %d, want >= 5000 unsolicited lines counted", got)
	}

	// The connection must still complete a request after the flood.
	out, err = c.Call(context.Background(), "echo", json.RawMessage(`{"text": "after flood"}`))
	if err != nil || out != "echo: after flood" {
		t.Fatalf("call after flood: out=%q err=%v", out, err)
	}

	// Stop must not wedge either. The double Stop from Cleanup is a
	// nil-op.
	done := make(chan struct{})
	go func() {
		_ = c.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop wedged on a flooded server (Wait/reader race)")
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
		"dead": {Command: "/nonexistent-binary-opcode"},
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

func TestNotesSurfaceDroppedLines(t *testing.T) {
	// The drop count is user-visible (the /mcp view), so a chatty
	// server's lost noise is reported, not silent.
	m := NewManager()
	t.Cleanup(m.Close)

	toolList, _ := m.Connect(context.Background(), Config{MCPServers: map[string]ServerConfig{
		"chatty": {Command: fixturePath("chatty")[0], Args: fixturePath("chatty")[1:]},
	}})
	if len(toolList) != 1 {
		t.Fatalf("tools = %v", namesOf(toolList))
	}
	if _, err := toolList[0].Execute(context.Background(), `{"text": "hi"}`); err != nil {
		t.Fatalf("execute: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	joined := strings.Join(m.Notes(), "\n")
	if !strings.Contains(joined, "lines dropped") {
		t.Errorf("drops not surfaced in notes: %v", joined)
	}
}

// connectFixtureArgs starts the fixture with explicit extra args and
// extra env (mode first).
func connectFixtureArgs(t *testing.T, env map[string]string, args ...string) *Client {
	t.Helper()
	python := fixturePath("echo")[0]
	c := NewClient("fixture", ServerConfig{
		Command: python,
		Args:    append([]string{"testdata/fixture_server.py"}, args...),
		Env:     env,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop() })
	return c
}

func callEcho(c *Client, text string, within time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	args, _ := json.Marshal(map[string]string{"text": text})
	return c.Call(ctx, "echo", args)
}

func TestReadLoopRouting(t *testing.T) {
	notif := `{"jsonrpc":"2.0","method":"notifications/x"}`
	tests := []struct {
		name        string
		input       string
		waitFor     int
		wantResult  string // "" = no delivery expected
		wantDropped int64
	}{
		{"response behind 10000 notifications",
			strings.Repeat(notif+"\n", 10000) + `{"jsonrpc":"2.0","id":7,"result":{"ok":1}}` + "\n",
			7, `{"ok":1}`, 10000},
		{"non-JSON noise and blank lines are skipped",
			"starting up...\n\n   \n" + `{"id":1,"result":{}}` + "\n",
			1, `{}`, 1},
		{"server request with our id is not a response",
			`{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n",
			3, "", 1},
		{"response for an id nobody waits on",
			`{"jsonrpc":"2.0","id":99,"result":{}}` + "\n",
			3, "", 1},
		{"null id is not routed",
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse"}}` + "\n",
			3, "", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cn := newConn()
			slot, ok := cn.register(tc.waitFor)
			if !ok {
				t.Fatal("register failed on a fresh conn")
			}
			var dropped atomic.Int64
			cn.readLoop(strings.NewReader(tc.input), &dropped) // returns at EOF
			select {
			case resp := <-slot:
				if tc.wantResult == "" {
					t.Fatalf("unexpected delivery: %s", resp.Result)
				}
				if string(resp.Result) != tc.wantResult {
					t.Errorf("result = %s, want %s", resp.Result, tc.wantResult)
				}
			default:
				if tc.wantResult != "" {
					t.Fatal("response was not delivered to its waiter")
				}
			}
			if got := dropped.Load(); got != tc.wantDropped {
				t.Errorf("dropped = %d, want %d", got, tc.wantDropped)
			}
		})
	}
}

func TestReadLoopOversizedLineFailsWaitersInsteadOfHanging(t *testing.T) {
	// A line past the 8 MiB scanner limit ends the stream. Waiters must
	// be released with the reason, and later registrations refused.
	cn := newConn()
	if _, ok := cn.register(1); !ok {
		t.Fatal("register failed")
	}
	var dropped atomic.Int64
	cn.readLoop(strings.NewReader(strings.Repeat("x", 9*1024*1024)+"\n"), &dropped)

	select {
	case <-cn.done:
	default:
		t.Fatal("done not closed after the reader exited")
	}
	if cn.readErr == nil {
		t.Error("oversized line should record the scanner error")
	}
	if _, ok := cn.register(2); ok {
		t.Error("register must fail once the reader has exited")
	}
}

func TestResponseBehindNotificationBurstIsNotDropped(t *testing.T) {
	// The fixture sends 5000 notifications BEFORE each response. A
	// reader that drops on a full channel can drop the response itself,
	// which surfaces as a timeout instead of the answer.
	c := connectFixture(t, "burst")
	for i := 0; i < 10; i++ {
		text := "burst-" + strconv.Itoa(i)
		out, err := callEcho(c, text, 5*time.Second)
		if err != nil || out != "echo: "+text {
			t.Fatalf("call %d: out=%q err=%v", i, out, err)
		}
	}
}

func TestConcurrentCallsEachGetTheirOwnResponse(t *testing.T) {
	// Client is safe for concurrent Call (roundtrip releases the mutex
	// while waiting), so every response must reach its own caller, not
	// be consumed and discarded by a neighbour.
	c := connectFixture(t, "echo")
	for round := 0; round < 10; round++ {
		const n = 8
		var wg sync.WaitGroup
		errs := make([]error, n)
		outs := make([]string, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				outs[i], errs[i] = callEcho(c, "c"+strconv.Itoa(i), 3*time.Second)
			}(i)
		}
		wg.Wait()
		for i := 0; i < n; i++ {
			if want := "echo: c" + strconv.Itoa(i); errs[i] != nil || outs[i] != want {
				t.Fatalf("round %d caller %d: out=%q err=%v, want %q", round, i, outs[i], errs[i], want)
			}
		}
	}
}

func TestServerRequestReusingOurIDIsNotTakenAsResponse(t *testing.T) {
	// JSON-RPC lets a server send its own requests (e.g. ping). One
	// carrying the same numeric id as our pending request has a
	// "method" and must not be mistaken for the response.
	c := connectFixture(t, "ping-first")
	out, err := callEcho(c, "real answer", 5*time.Second)
	if err != nil || out != "echo: real answer" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestStopDoesNotWedgeOnDescendantHoldingStdout(t *testing.T) {
	// A descendant that left the process group (setsid) survives the
	// group kill and keeps the stdout pipe open, so EOF never arrives.
	// Stop holds the client mutex; waiting for EOF there would freeze
	// every later call and the TUI's exit.
	if runtime.GOOS == "windows" {
		t.Skip("fixture relies on sleep and setsid")
	}
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	c := connectFixtureArgs(t, nil, "escape", pidfile)
	// Registered after connectFixtureArgs's Stop cleanup, so it runs
	// first: killing the sleeper also un-wedges a Stop that hangs.
	t.Cleanup(func() {
		if raw, err := os.ReadFile(pidfile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})
	if _, err := os.Stat(pidfile); err != nil {
		t.Fatalf("fixture never spawned its escaped child: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_ = c.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("Stop blocked waiting for a stdout EOF that an escaped descendant withholds")
	}
}

func TestServerEnvIsScrubbedButKeepsPathAndHome(t *testing.T) {
	// S7: a server needs PATH/HOME to run; it must NOT inherit the
	// parent's credentials, but gets what mcp.json `env` grants.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENROUTER_API_KEY", "sk-parent-secret")
	t.Setenv("OPCODE_TEST_SECRET_TOKEN", "parent-token")

	c := connectFixtureArgs(t, map[string]string{"GRANTED_BY_CONFIG": "yes"}, "env")

	tests := []struct {
		name, variable, want string
	}{
		{"PATH survives", "PATH", "PATH=" + os.Getenv("PATH")},
		{"HOME survives", "HOME", "HOME=" + home},
		{"API key not inherited", "OPENROUTER_API_KEY", "OPENROUTER_API_KEY unset"},
		{"token not inherited", "OPCODE_TEST_SECRET_TOKEN", "OPCODE_TEST_SECRET_TOKEN unset"},
		{"config env granted", "GRANTED_BY_CONFIG", "GRANTED_BY_CONFIG=yes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := callEcho(c, tc.variable, 5*time.Second)
			if err != nil || out != tc.want {
				t.Errorf("out=%q err=%v, want %q", out, err, tc.want)
			}
		})
	}
}

func namesOf(ts []Tool) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Name())
	}
	return out
}
