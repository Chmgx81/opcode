package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// writeMu serializes tilde's own read-modify-write cycles on the user
// config files (auth.json, config.json) so two concurrent /login or
// /theme calls in one process cannot lose each other's update. It does
// not coordinate separate tilde processes; the atomic rename below
// keeps each file whole in that case, but the last writer still wins.
var writeMu sync.Mutex

// WriteFileAtomic replaces path with data without ever exposing a
// partial file: the bytes go to a temp file in the same directory
// (same filesystem, so the rename is atomic), are flushed to disk, and
// are then renamed over the destination. A crash, a full disk, or a
// concurrent reader sees the old complete file or the new complete
// file. The result has exactly mode, even when path already existed
// with looser permissions (os.WriteFile applies mode only on create).
//
// A symlinked path is written through: the link stays a link and its
// target is replaced, so a config managed from dotfiles keeps working.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	committed = true
	// Persist the rename itself. Best effort: not every filesystem
	// supports fsync on a directory, and the data is already safe.
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
