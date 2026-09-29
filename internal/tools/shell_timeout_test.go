package tools

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestBashTimeoutKillsGrandchildren is the audit C7 regression: the
// context watchdog kills only the direct bash child, so a command
// that backgrounded work used to leave it running past the timeout.
// The child is a process-group leader now, and the timeout kills the
// whole group — the grandchild must be gone (or a reaped-pending
// zombie) shortly after.
func TestBashTimeoutKillsGrandchildren(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process groups are only wired up on Linux (see deathAttr)")
	}
	dir := t.TempDir()
	pidFile := dir + "/gpid"
	// Background a long sleep, record its pid, then hang so the
	// deadline fires with the grandchild alive.
	cmd := `sleep 300 & echo $! > ` + pidFile + `; sleep 300`
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := (Bash{}).Execute(ctx, `{"command": `+strconv.Quote(cmd)+`}`)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("command never recorded the grandchild pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("bad pid %q: %v", data, err)
	}
	// The group kill is synchronous with Execute's return, but the
	// SIGKILL needs a scheduling beat to land; poll briefly.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if pidGone(pid) {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("grandchild %d survived the bash timeout", pid)
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A command that cannot start (no bash on PATH) must come back as an
// ordinary tool error: there is no process to watch or kill, and the
// message keeps the "bash: " prefix the model and the TUI expect. A
// watchdog that dereferenced cmd.Process unconditionally would panic
// here.
func TestBashStartFailureIsAnError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	out, err := (Bash{}).Execute(context.Background(), `{"command": "echo hi"}`)
	if err == nil {
		t.Fatalf("expected a start error, got output %q", out)
	}
	if !strings.HasPrefix(err.Error(), "bash: ") {
		t.Errorf("error = %q, want the bash: prefix", err)
	}
	if out != "" {
		t.Errorf("output = %q, want empty", out)
	}
}

// Execute must not leave the watchdog behind, whether the command
// finishes normally, fails, or is killed by its deadline.
func TestBashLeavesNoGoroutines(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process groups are only wired up on Linux (see deathAttr)")
	}
	before := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		_, _ = (Bash{}).Execute(context.Background(), `{"command": "echo ok"}`)
		_, _ = (Bash{}).Execute(context.Background(), `{"command": "exit 3"}`)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, _ = (Bash{}).Execute(ctx, `{"command": "sleep 30"}`)
		cancel()
	}
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines grew from %d to %d after 30 Executes", before, runtime.NumGoroutine())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pidGone reports whether pid no longer runs: gone entirely, or a
// zombie (SIGKILL delivered, waiting to be reaped — it can do no
// work either way).
func pidGone(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	// The state letter is the field after the (comm) parentheses.
	if i := strings.LastIndexByte(string(data), ')'); i >= 0 && i+1 < len(data) {
		state := strings.TrimSpace(string(data[i+1:]))
		return strings.HasPrefix(state, "Z")
	}
	return false
}
