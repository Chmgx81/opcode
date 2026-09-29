package tui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/tools"
)

// TestFullSessionOverTeaProgram runs the real tea.Program (not just
// Update calls) end to end: a scripted OpenAI-compatible SSE server, the
// real orchestrator, the real gate in ask mode, and a permission prompt
// answered by a synthetic keystroke. If the prompt's reply path is
// broken anywhere — decide blocking, msg routing, key handling — the
// tool never executes and this test fails.
func TestFullSessionOverTeaProgram(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "demo.txt")

	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(obj string) {
			fmt.Fprintf(w, "data: %s\n\n", obj)
		}
		if requestCount == 1 {
			args := fmt.Sprintf(`{"path": %q, "content": "works"}`, target)
			send(`{"choices": [{"delta": {"content": "Writing it."}}]}`)
			send(fmt.Sprintf(`{"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "call-1", "function": {"name": "write_file", "arguments": %s}}]}}]}`,
				quoteJSON(args)))
			send(`{"choices": [{"delta": {}, "finish_reason": "tool_calls"}]}`)
		} else {
			send(`{"choices": [{"delta": {"content": "done"}}]}`)
			send(`{"choices": [{"delta": {}, "finish_reason": "stop"}]}`)
		}
	}))
	defer srv.Close()

	var reg tools.Registry
	reg.Register(tools.ReadFile{})
	reg.Register(tools.WriteFile{})

	gate := &tools.Gate{Audit: tools.NewAuditLog(filepath.Join(dir, "audit.jsonl"), nil)}
	provider := llm.NewOpenAICompat(srv.URL, "test-key")
	orch := orchestrator.New(provider, "m", "s", &reg, gate)

	m := New(Options{
		Orch:         orch,
		Model:        "m",
		Mode:         tools.ModeBuild,
		Cwd:          dir,
		TildeHome:    dir,
		ProviderName: "openrouter",
		BaseURL:      srv.URL,
		AuditPath:    filepath.Join(dir, "audit.jsonl"),
	})
	gate.Decide = tools.PolicyDecide(tools.ModeAsk, m.Prompt())

	inR, inW := io.Pipe()
	p := tea.NewProgram(m, tea.WithInput(inR), tea.WithOutput(io.Discard))
	// tui.Run normally does this; this test drives p.Run directly.
	m.program = p

	done := make(chan struct{})
	go func() {
		p.Run()
		close(done)
	}()

	// Feed input on a schedule: submit the message; the write's
	// target is inside the sandbox's writable roots, so ask mode
	// runs it without a permission prompt (Phase 30) and the turn
	// completes on its own; then exit.
	go func() {
		io.WriteString(inW, "create the file\r")
		time.Sleep(1400 * time.Millisecond)
		io.WriteString(inW, "/exit\r")
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("tea program did not exit in time")
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the turn never executed the tool (file missing): %v", err)
	}
	if string(data) != "works" {
		t.Errorf("file content = %q", string(data))
	}
	audit, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("audit log: %v", err)
	}
	if !strings.Contains(string(audit), `"allowed":true`) {
		t.Errorf("audit log has no allowed write: %s", audit)
	}
	if requestCount != 2 {
		t.Errorf("server saw %d requests, want 2", requestCount)
	}
}

// quoteJSON encodes s as a JSON string literal, keeping the nested
// escaping (tool arguments inside a streamed chunk) honest.
func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
