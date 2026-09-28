// Package skills discovers and serves skills (Section 3.4): folders with
// a SKILL.md (flat YAML frontmatter: name, description; then the body)
// plus optional scripts/, references/, assets/.
//
// Progressive disclosure is mechanical, not heuristic: the index (name +
// description only) is what sits in the system prompt; the body is only
// reachable through the load_skill tool; bundled resources only through
// the paths the body itself names.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Scope of a skill's origin.
const (
	ScopeUser    = "user"
	ScopeProject = "project"
)

// Skill is one discovered skill folder.
type Skill struct {
	Name        string
	Description string
	Dir         string   // absolute folder path
	Scope       string   // user or project
	Body        string   // SKILL.md content after the frontmatter
	Scripts     []string // basenames in scripts/, sorted
}

// Source is a directory to scan.
type Source struct {
	Dir     string
	Scope   string
	Trusted bool // project sources only when the trust gate approved
}

// Manager holds the discovered skills by name, safely replaceable: the
// trust prompt can re-discover into the same instance mid-session and
// the tools built on it see the new set immediately.
type Manager struct {
	mu     sync.RWMutex
	skills map[string]*Skill
	// skipped records skills that failed to load, with the reason, so
	// startup can warn instead of silently dropping folders.
	skipped []string
}

// Load (re)discovers all sources into the manager. Later sources win on
// name conflicts, so pass user before project: a project skill overrides
// the user's same-named one, matching config precedence.
func (m *Manager) Load(sources []Source) error {
	found := map[string]*Skill{}
	var skipped []string

	for _, src := range sources {
		if src.Scope == ScopeProject && !src.Trusted {
			// Untrusted project skills are not even parsed — the point
			// of the trust gate: nothing from the project loads.
			continue
		}
		entries, err := os.ReadDir(src.Dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read skills dir %s: %w", src.Dir, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(src.Dir, e.Name())
			skill, err := loadSkill(dir, src.Scope)
			if err != nil {
				skipped = append(skipped, fmt.Sprintf("%s: %v", dir, err))
				continue
			}
			found[skill.Name] = skill
		}
	}

	sort.Strings(skipped)
	m.mu.Lock()
	m.skills = found
	m.skipped = skipped
	m.mu.Unlock()
	return nil
}

// loadSkill parses one skill folder.
func loadSkill(dir, scope string) (*Skill, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, fmt.Errorf("SKILL.md: %w", err)
	}
	name, desc, body, err := parseFrontmatter(string(data))
	if err != nil {
		return nil, err
	}

	s := &Skill{Name: name, Description: desc, Dir: dir, Scope: scope, Body: body}

	scriptsDir := filepath.Join(dir, "scripts")
	entries, err := os.ReadDir(scriptsDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				s.Scripts = append(s.Scripts, e.Name())
			}
		}
		sort.Strings(s.Scripts)
	}
	return s, nil
}

// parseFrontmatter handles flat `key: value` frontmatter delimited by
// `---` lines, then returns the body. Frontmatter here is deliberately
// tiny — the agentskills convention needs name and description — so a
// hand-rolled flat parser is smaller and clearer than a YAML dependency.
func parseFrontmatter(data string) (name, desc, body string, err error) {
	lines := strings.Split(data, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return "", "", "", fmt.Errorf("frontmatter must start with ---")
	}
	fields := map[string]string{}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			end = i
			break
		}
		idx := strings.Index(lines[i], ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(lines[i][:idx])
		val := strings.TrimSpace(lines[i][idx+1:])
		fields[key] = strings.Trim(val, `"'`)
	}
	if end < 0 {
		return "", "", "", fmt.Errorf("frontmatter not closed")
	}
	name = fields["name"]
	desc = fields["description"]
	if name == "" || desc == "" {
		return "", "", "", fmt.Errorf("frontmatter needs name and description")
	}
	return name, desc, strings.TrimSpace(strings.Join(lines[end+1:], "\n")), nil
}

// Get returns a skill by name.
func (m *Manager) Get(name string) (*Skill, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.skills[name]
	return s, ok
}

// Names returns all discovered skill names, sorted.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.skills))
	for name := range m.skills {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Skipped lists skills that failed to load and why.
func (m *Manager) Skipped() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.skipped...)
}

// Index is the metadata-only skills index for the system prompt — tier 1
// of progressive disclosure: name + description per skill, plus how to
// pull the body when a request matches.
func (m *Manager) Index() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.skills) == 0 {
		return ""
	}
	names := make([]string, 0, len(m.skills))
	for name := range m.skills {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("Available skills (call the load_skill tool with the skill's name to load its full instructions when the user's request matches its description):\n")
	for _, name := range names {
		fmt.Fprintf(&b, "- %s: %s\n", name, m.skills[name].Description)
	}
	return strings.TrimRight(b.String(), "\n")
}
