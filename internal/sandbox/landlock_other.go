//go:build !linux

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Supported is always false here: Landlock is a Linux LSM. Commands
// run unsandboxed and the status line says so — never silently.
func Supported() bool { return false }

// ProbeABI reports the unsupported state for callers that want the
// reason, not just the boolean.
func ProbeABI() (int, error) { return 0, ErrUnsupported }

// Apply cannot confine anything on this platform.
func Apply(_ []string) error { return ErrUnsupported }

// Exec replaces this process with argv (still needed if a future
// backend lands on this platform).
func Exec(argv []string) error {
	if len(argv) == 0 {
		return errors.New("exec: empty command")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, os.Environ())
}

// deathAttr is a no-op off Linux; the field is Linux-specific but the
// type exists everywhere, so keep the same shape.
func deathAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }

// KillGroup is a no-op off Linux: without Setpgid there is no group
// to kill, and the direct child was already killed by the context.
func KillGroup(int) error { return nil }
