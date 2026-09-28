// Package trust implements the project-trust gate (Section 7): a
// separate, earlier decision than per-action permissions. Before tilde
// runs anything a project brought with it — a skill's script, an MCP
// server — the user must have approved that project's executable
// surface, and the approval is tied to a fingerprint of exactly that
// surface so a later `git pull` re-asks instead of inheriting trust.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status of a project directory.
type Status int

const (
	// Trusted: previously approved, fingerprint unchanged — or no
	// executable surface at all, which is the same thing in practice:
	// there is nothing the project can make tilde run.
	Trusted Status = iota
	// Untrusted: never approved.
	Untrusted
	// Changed: approved before, but the executable surface differs —
	// ask again (direnv's rule).
	Changed
)

func (s Status) String() string {
	switch s {
	case Trusted:
		return "trusted"
	case Untrusted:
		return "untrusted"
	case Changed:
		return "changed"
	}
	return "unknown"
}

// surfaceFiles are the project-relative paths whose content defines the
// executable surface. mcp.json is included from day one (Phase 4) so
// adding servers later is a trust-visible change, not a silent one.
var surfaceFiles = []string{
	".tilde/config.json",
	".tilde/mcp.json",
}

// Surface lists a project's executable surface: the config/mcp files and
// every script under .tilde/skills/*/scripts/, relative to the project
// root, sorted. mcp.json and skills/config files that don't exist are
// simply absent from the list.
func Surface(projectDir string) ([]string, error) {
	var files []string
	for _, rel := range surfaceFiles {
		if _, err := os.Stat(filepath.Join(projectDir, rel)); err == nil {
			files = append(files, rel)
		}
	}
	skillsDir := filepath.Join(projectDir, ".tilde", "skills")
	err := filepath.Walk(skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(projectDir, path)
		if relErr != nil {
			return relErr
		}
		if strings.Contains(rel, string(filepath.Separator)+"scripts"+string(filepath.Separator)) {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// Fingerprint hashes a project's executable surface: each file's relative
// path, size, and content hash, in sorted order. Any change to any file
// (content, or the set of files) changes the fingerprint.
func Fingerprint(projectDir string) (string, []string, error) {
	files, err := Surface(projectDir)
	if err != nil {
		return "", nil, err
	}
	h := sha256.New()
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(projectDir, rel))
		if err != nil {
			return "", nil, err
		}
		fh := sha256.Sum256(data)
		fmt.Fprintf(h, "%s %d %s\n", rel, len(data), hex.EncodeToString(fh[:]))
	}
	return hex.EncodeToString(h.Sum(nil)), files, nil
}

// Store persists trust decisions in ~/.tilde/trusted-projects.json.
type Store struct {
	path string
	mu   sync.Mutex

	entries map[string]entry
}

type entry struct {
	Fingerprint string   `json:"fingerprint"`
	TrustedAt   string   `json:"trusted_at"`
	Approved    []string `json:"approved"`
}

// LoadStore reads trusted-projects.json from the user-level dir. A
// missing file is an empty store, not an error.
func LoadStore(userDir string) (*Store, error) {
	s := &Store{path: filepath.Join(userDir, "trusted-projects.json"), entries: map[string]entry{}}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read trusted-projects.json: %w", err)
	}
	if err := json.Unmarshal(data, &s.entries); err != nil {
		return nil, fmt.Errorf("parse trusted-projects.json: %w", err)
	}
	if s.entries == nil {
		s.entries = map[string]entry{}
	}
	return s, nil
}

// Status reports whether the project at dir may run its executable
// surface. It returns the fingerprint's file list when a prompt is
// warranted, so the prompt can show exactly what would run.
func (s *Store) Status(projectDir string) (Status, []string, error) {
	fp, files, err := Fingerprint(projectDir)
	if err != nil {
		return Untrusted, nil, err
	}
	if len(files) == 0 {
		// No scripts, no mcp/config: nothing the project can execute.
		return Trusted, nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return Untrusted, nil, err
	}
	e, ok := s.entries[abs]
	if !ok {
		return Untrusted, files, nil
	}
	if e.Fingerprint != fp {
		return Changed, files, nil
	}
	return Trusted, nil, nil
}

// Trust records approval of the project's current executable surface.
func (s *Store) Trust(projectDir string) error {
	fp, files, err := Fingerprint(projectDir)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[abs] = entry{
		Fingerprint: fp,
		TrustedAt:   time.Now().UTC().Format(time.RFC3339),
		Approved:    files,
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Untrust removes a project's stored approval. The TUI can expose this
// later; it exists so the store has an honest inverse.
func (s *Store) Untrust(projectDir string) error {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[abs]; !ok {
		return nil
	}
	delete(s.entries, abs)
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
