package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The V4A patch markers, matching Codex's apply-patch crate so the
// same patches work in both tools.
const (
	beginPatchMarker = "*** Begin Patch"
	endPatchMarker   = "*** End Patch"
	addFileMarker    = "*** Add File: "
	deleteFileMarker = "*** Delete File: "
	updateFileMarker = "*** Update File: "
)

// patchAction is one file operation parsed out of a patch.
type patchAction struct {
	kind  string // "add", "update", "delete"
	path  string
	hunks []string // update: the hunk lines (context/removed/added)
	add   []string // add: the file content lines
}

// parsePatch splits a V4A patch body into per-file actions. The
// optional `apply_patch <<'EOF' … EOF` heredoc wrapper is accepted
// and stripped, the way Codex's parser does.
func parsePatch(body string) ([]patchAction, error) {
	body = strings.TrimSpace(body)
	if s := strings.Index(body, "<<'EOF'"); s >= 0 {
		body = body[s+len("<<'EOF'"):]
		if e := strings.LastIndex(body, "EOF"); e >= 0 {
			body = body[:e]
		}
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != beginPatchMarker {
		return nil, fmt.Errorf("apply_patch: must begin with %q", beginPatchMarker)
	}
	var actions []patchAction
	cur := -1 // index into actions of the file being filled
	for i := 1; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.TrimSpace(l) == endPatchMarker:
			return actions, nil
		case strings.HasPrefix(l, addFileMarker):
			actions = append(actions, patchAction{kind: "add", path: strings.TrimSpace(l[len(addFileMarker):])})
			cur = len(actions) - 1
		case strings.HasPrefix(l, deleteFileMarker):
			actions = append(actions, patchAction{kind: "delete", path: strings.TrimSpace(l[len(deleteFileMarker):])})
			cur = len(actions) - 1
		case strings.HasPrefix(l, updateFileMarker):
			actions = append(actions, patchAction{kind: "update", path: strings.TrimSpace(l[len(updateFileMarker):])})
			cur = len(actions) - 1
		default:
			if cur < 0 {
				if strings.TrimSpace(l) == "" {
					continue
				}
				return nil, fmt.Errorf("apply_patch: line %d outside any file section: %q", i+1, l)
			}
			switch actions[cur].kind {
			case "add":
				if !strings.HasPrefix(l, "+") {
					return nil, fmt.Errorf("apply_patch: %s: add-file content must start with +", actions[cur].path)
				}
				actions[cur].add = append(actions[cur].add, strings.TrimPrefix(l, "+"))
			case "update":
				if !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "+") &&
					!strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "@@") {
					return nil, fmt.Errorf("apply_patch: %s: hunk lines must start with ' ', '+', '-', or '@@': %q",
						actions[cur].path, l)
				}
				actions[cur].hunks = append(actions[cur].hunks, l)
			}
		}
	}
	return nil, fmt.Errorf("apply_patch: missing %q", endPatchMarker)
}

// patchPaths lists the file paths a patch touches — the permission
// gate uses it to decide boundedness (every touched path inside the
// sandbox's writable roots).
func patchPaths(argsJSON string) []string {
	var a struct {
		Patch string `json:"patch"`
	}
	if err := parseJSONLoose(argsJSON, &a); err != nil {
		return nil
	}
	actions, err := parsePatch(a.Patch)
	if err != nil {
		return nil
	}
	var out []string
	for _, act := range actions {
		out = append(out, act.path)
	}
	return out
}

// applyHunks rewrites lines by finding each hunk's context/removed
// sequence and splicing in the context/added one. Exact match
// first, then a leading-whitespace-normalized pass — the model's
// context lines are hints, not whitespace contracts (Codex's
// seek_sequence does the same two-pass search).
func applyHunks(lines []string, hunks []string) ([]string, error) {
	// Split the hunk stream into hunks at each @@ marker; a hunk
	// without @@ is its own hunk.
	var groups [][]string
	for _, l := range hunks {
		if strings.HasPrefix(l, "@@") || len(groups) == 0 {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], l)
	}
	for _, g := range groups {
		var find, repl []string
		for _, l := range g {
			switch {
			case strings.HasPrefix(l, "@@"):
				// The @@ line itself is a context hint, not content.
			case strings.HasPrefix(l, " "), strings.HasPrefix(l, "-"):
				content := l[1:]
				find = append(find, content)
				if !strings.HasPrefix(l, "-") {
					repl = append(repl, content)
				}
			case strings.HasPrefix(l, "+"):
				repl = append(repl, l[1:])
			}
		}
		if len(find) == 0 {
			return nil, fmt.Errorf("hunk has no context or removed lines to locate it: %q", strings.Join(g, " / "))
		}
		pos, ok := seekSequence(lines, find, 0)
		if !ok {
			pos, ok = seekSequenceNormalized(lines, find, 0)
		}
		if !ok {
			return nil, fmt.Errorf("could not find context for hunk %q — the file may have changed since it was read",
				strings.Join(g, " / "))
		}
		out := make([]string, 0, len(lines)+len(repl)-len(find))
		out = append(out, lines[:pos]...)
		out = append(out, repl...)
		out = append(out, lines[pos+len(find):]...)
		lines = out
	}
	return lines, nil
}

func seekSequence(lines, find []string, start int) (int, bool) {
	for i := start; i+len(find) <= len(lines); i++ {
		match := true
		for j := range find {
			if lines[i+j] != find[j] {
				match = false
				break
			}
		}
		if match {
			return i, true
		}
	}
	return 0, false
}

// seekSequenceNormalized compares with leading whitespace stripped,
// so indentation drift between the model's context and the file
// still applies.
func seekSequenceNormalized(lines, find []string, start int) (int, bool) {
	trim := func(s string) string { return strings.TrimLeft(s, " \t") }
	for i := start; i+len(find) <= len(lines); i++ {
		match := true
		for j := range find {
			if trim(lines[i+j]) != trim(find[j]) {
				match = false
				break
			}
		}
		if match {
			return i, true
		}
	}
	return 0, false
}

// writeFileLines joins lines and writes the file, creating parent
// directories as needed.
func writeFileLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return writeGuarded(path, []byte(content))
}

// parseJSONLoose unmarshals into v, treating unparsable input as
// empty — callers decide what an unparseable payload means.
func parseJSONLoose(args string, v any) error {
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	return json.Unmarshal([]byte(args), v)
}
