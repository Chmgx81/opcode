//go:build !windows

package skills

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSkillFileThatIsAFIFOIsRefusedNotBlockedOn(t *testing.T) {
	// Opening a FIFO with no writer blocks forever; a hostile skill
	// folder must not be able to hang startup that way.
	root := t.TempDir()
	dir := filepath.Join(root, "fifo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "SKILL.md"), 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	done := make(chan *Manager, 1)
	go func() {
		m := &Manager{}
		_ = m.Load([]Source{{Dir: root, Scope: ScopeUser}})
		done <- m
	}()
	select {
	case m := <-done:
		if len(m.Names()) != 0 || !skippedContains(m, "fifo", "not a regular file") {
			t.Errorf("names=%v skipped=%v", m.Names(), m.Skipped())
		}
	case <-time.After(5 * time.Second):
		// Unblock the stuck reader so the test binary can exit.
		if f, err := os.OpenFile(filepath.Join(dir, "SKILL.md"), os.O_WRONLY, 0); err == nil {
			f.Close()
		}
		t.Fatal("Load blocked on a FIFO named SKILL.md")
	}
}
