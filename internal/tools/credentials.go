package tools

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// errCredentialsFile is what a tool reports when it is pointed at the
// credentials file. It names no path and no content.
var errCredentialsFile = errors.New("access to the credentials file is not allowed")

// credentialsPath is the one file no tool may touch in any mode
// (audit S1): ~/.tilde/auth.json holds every provider key the user
// has, and read-tier calls run free everywhere — without this deny,
// the file's contents would land in the model's context, and from
// there in any transcript. Duplicated from config.UserDir (four
// lines) because config imports this package; the cycle forces the
// choice and the duplication is smaller than an interface would be.
func credentialsPath() string {
	if dir := os.Getenv("TILDE_HOME"); dir != "" {
		return filepath.Join(dir, "auth.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".tilde", "auth.json")
}

// touchesCredentials reports whether a call names the credentials
// file in any path it carries: the "path" argument of read_file,
// list_dir, grep, glob, write_file and edit_file (and of any other
// tool that uses that name), and every file section of an apply_patch
// patch. Arguments are decoded the way the tools decode them — the
// same struct tags, so json's case-insensitive key matching and
// last-duplicate-wins agree — otherwise a variant spelling could
// satisfy the tool but not this check.
//
// Not covered here, deliberately: bash. Its command string is not
// parsed by anyone; that route is bounded by the sandbox's network
// block, by the redaction in Gate.Execute, and by the approval-gated
// unsandboxed escape. And directory-level reach (grep rooted at ~ or
// at ~/.tilde) is handled where the files are opened, by openReadable,
// because refusing every grep that could reach the file would refuse
// grepping the home directory altogether.
func touchesCredentials(tool Tool, args string) bool {
	cred := credentialsPath()
	if cred == "" {
		return false
	}
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &a); err == nil && isCredentialsFile(a.Path, cred) {
		return true
	}
	if _, ok := tool.(ApplyPatch); ok {
		for _, p := range patchPaths(args) {
			if isCredentialsFile(p, cred) {
				return true
			}
		}
	}
	return false
}

// isCredentialsFile reports whether path is the credentials file
// however it is spelled: relative or absolute, with ../ segments,
// through symlinks (including a symlinked home like /home -> /var/home),
// through hard links, or on a case-insensitive filesystem. When both
// files exist the answer is the kernel's — same device and inode —
// which no spelling can fool. When path does not exist yet (a write
// that would create the file, or one through a dangling symlink) the
// spelling is normalized and compared instead.
func isCredentialsFile(path, cred string) bool {
	if path == "" {
		return false
	}
	if pi, err := os.Stat(path); err == nil {
		if ci, err := os.Stat(cred); err == nil {
			return os.SameFile(pi, ci)
		}
	}
	return strings.EqualFold(normalizePath(path), normalizePath(cred))
}

// isCredentialsInfo is the by-inode test for an already-open file: no
// path is involved, so nothing can be swapped between the check and
// the read.
func isCredentialsInfo(fi os.FileInfo) bool {
	cred := credentialsPath()
	if cred == "" {
		return false
	}
	ci, err := os.Stat(cred)
	return err == nil && os.SameFile(fi, ci)
}

// normalizePath makes an absolute path with every symlink in it
// followed, even when the last component does not exist: the parent
// is resolved, and a dangling final link is followed by hand.
func normalizePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	// A final symlink whose target is missing defeats EvalSymlinks;
	// chase it manually (bounded, like the kernel's own limit).
	for i := 0; i < 40; i++ {
		fi, err := os.Lstat(abs)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			break
		}
		target, err := os.Readlink(abs)
		if err != nil {
			break
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(abs), target)
		}
		abs = target
	}
	dir, base := filepath.Split(abs)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Join(dir, base)
}

// openReadable opens path for reading unless it is the credentials
// file. The check is on the opened descriptor, by inode: gate-time
// path checks alone leave a window in which a sandboxed command can
// swap a symlink between the check and the read, and cover no
// directory walk at all (grep from ~ or from ~/.tilde reaches the file
// without ever naming it).
func openReadable(path string) (*os.File, error) {
	f, err := os.Open(path)
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

// readFileGuarded is os.ReadFile through openReadable.
func readFileGuarded(path string) ([]byte, error) {
	f, err := openReadable(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
