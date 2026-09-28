//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// setProcGroup puts a server in its own process group so a hung server
// is killed with its children and the terminal's Ctrl+C never reaches
// it directly.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcGroup kills the whole group (negative pid).
func killProcGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
