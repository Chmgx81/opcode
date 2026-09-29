package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ApplyPatch applies a V4A patch — the format Codex's apply_patch
// tool uses — so one call can add, update, and delete several files
// with exact context around every change. Action-Allowed tier; the
// gate credits it as bounded when every touched path sits inside
// the sandbox's writable roots (the common in-tree case).
type ApplyPatch struct{}

func (ApplyPatch) Name() string { return "apply_patch" }

func (ApplyPatch) Description() string {
	return "Apply a V4A patch that can add, update, and delete files in one call. Format: '*** Begin Patch', then sections '*** Add File: <path>' with +content lines, '*** Update File: <path>' with @@ context and ' '/'+'/'-' hunk lines, '*** Delete File: <path>', then '*** End Patch'. Hunk context locates each change; context must match the file you read."
}

func (ApplyPatch) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"patch": {"type": "string", "description": "The full V4A patch text between *** Begin Patch and *** End Patch"}
		},
		"required": ["patch"]
	}`)
}

func (ApplyPatch) Tier() Tier { return TierActionAllowed }

func (ApplyPatch) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Patch string `json:"patch"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	actions, err := parsePatch(a.Patch)
	if err != nil {
		return "", err
	}
	if len(actions) == 0 {
		return "", fmt.Errorf("apply_patch: the patch contains no file sections")
	}

	var results []string
	applied := 0
	for _, act := range actions {
		if act.path == "" {
			return "", fmt.Errorf("apply_patch: a section is missing its file path")
		}
		// The gate's bound check is skipped in full-auto by design,
		// so the tool itself refuses the spellings that escape the
		// project (parent traversal, absolute paths outside the
		// writable roots). And the credentials file is denied here
		// too, not just at the gate (same backstop as
		// write_file/edit_file).
		if !safePatchPath(act.path) {
			return "", fmt.Errorf("apply_patch: refusing path outside the project: %s", act.path)
		}
		if isCredentialsFile(act.path, credentialsPath()) {
			return "", fmt.Errorf("apply_patch: %w", errCredentialsFile)
		}
		switch act.kind {
		case "add":
			if _, err := os.Stat(act.path); err == nil {
				return "", fmt.Errorf("apply_patch: %s already exists", act.path)
			}
			if err := writeFileLines(act.path, act.add); err != nil {
				return "", fmt.Errorf("apply_patch: %w", err)
			}
			results = append(results, fmt.Sprintf("added %s (%d lines)", act.path, len(act.add)))
			applied++
		case "delete":
			if err := os.Remove(act.path); err != nil {
				return "", fmt.Errorf("apply_patch: %w", err)
			}
			results = append(results, "deleted "+act.path)
			applied++
		case "update":
			data, err := readFileGuarded(act.path)
			if err != nil {
				return "", fmt.Errorf("apply_patch: %w", err)
			}
			old := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			out, err := applyHunks(old, act.hunks)
			if err != nil {
				if applied > 0 {
					return "", fmt.Errorf("apply_patch: %w (%d earlier file(s) were already changed)", err, applied)
				}
				return "", fmt.Errorf("apply_patch: %w", err)
			}
			if err := writeFileLines(act.path, out); err != nil {
				return "", fmt.Errorf("apply_patch: %w", err)
			}
			results = append(results, fmt.Sprintf("updated %s", act.path))
			applied++
		}
	}
	return strings.Join(results, "\n"), nil
}

// safePatchPath is the floor full-auto stands on. The gate's
// writable-roots bound still governs prompting; this refuses only
// what no mode should do: parent traversal (which escapes the
// project however it is spelled) and absolute paths outside the
// writable roots. Relative in-tree paths and absolute in-roots
// paths both pass — the model emits either.
func safePatchPath(path string) bool {
	if path == "" {
		return false
	}
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == ".." {
			return false
		}
	}
	if filepath.IsAbs(path) {
		return pathInWritableRoots(path)
	}
	return true
}
