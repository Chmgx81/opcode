package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
			data, err := os.ReadFile(act.path)
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
