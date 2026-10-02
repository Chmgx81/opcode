//go:build !windows

package tools

import (
	"fmt"
	"os"
	"syscall"
)

// openWritableUnix opens path for writing without following the final
// component (O_NOFOLLOW) and refuses files with more than one hard
// link — the write-side twin of openReadable's by-inode check.
//
// Gate-time path checks alone leave a window in which a sandboxed
// command can swap a symlink between the check and the write, landing
// the write outside the writable roots; O_NOFOLLOW closes it. And on
// Landlock ABI 1 (kernels 5.13–5.18, before REFER) a sandboxed command
// can place a hard link to a file outside the roots *inside* them —
// the gate resolves the in-roots path and approves — so a link count
// above one is refused rather than written through. On ABI 2+ the
// kernel already blocks the link; this check is the belt under the
// braces.
func openWritableUnix(path string) (*os.File, error) {
	f, err := os.OpenFile(path,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if sys, ok := fi.Sys().(*syscall.Stat_t); ok && sys.Nlink > 1 {
		f.Close()
		return nil, fmt.Errorf("refusing to write %s: it has %d hard links (the sandbox cannot link files; this file does)", path, sys.Nlink)
	}
	if isCredentialsInfo(fi) {
		f.Close()
		return nil, errCredentialsFile
	}
	return f, nil
}
