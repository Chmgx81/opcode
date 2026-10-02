//go:build windows

package tools

import "os"

// openWritableUnix on Windows: the sandbox is Linux-only, so the
// swap-a-symlink-under-the-write attacker does not exist on this
// platform — commands are gated by prompts, not confined. The write
// keeps its open-time credentials check, the same backstop the read
// path has.
func openWritableUnix(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if isCredentialsInfo(fi) {
		f.Close()
		return nil, errCredentialsFile
	}
	return f, nil
}
