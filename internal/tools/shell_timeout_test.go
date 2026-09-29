package tools

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
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
			_ = syscall.Kill(pid, syscall.SIGKILL)
			return
		}
		time.Sleep(50 * time.Millisecond)
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
