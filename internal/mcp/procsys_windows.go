//go:build windows

package mcp

import "os/exec"

// Windows has no POSIX process groups: setProcGroup is a no-op and
// killProcGroup kills nothing itself — the caller still closes stdin
// and waits. Grandchildren a server spawned may survive a kill; that
// is a stated limitation on Windows, not a silent one.
func setProcGroup(cmd *exec.Cmd) {}

func killProcGroup(pid int) {}
