package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tilde/internal/skills"
)

// LoadSkill returns a skill's full instructions — tier 2 of progressive
// disclosure. Read-Only: loading instructions changes nothing; it is the
// skill's scripts that are Action-Allowed.
type LoadSkill struct {
	Manager *skills.Manager
}

func (LoadSkill) Name() string { return "load_skill" }

func (LoadSkill) Description() string {
	return "Load a skill's full instructions by name. Call this when the user's request matches a skill's description in the available-skills list."
}

func (LoadSkill) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Skill name from the available-skills list"}
		},
		"required": ["name"]
	}`)
}

func (LoadSkill) Tier() Tier { return TierReadOnly }

func (t LoadSkill) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	skill, ok := t.Manager.Get(a.Name)
	if !ok {
		return "", fmt.Errorf("load_skill: no skill named %q", a.Name)
	}
	out := skill.Body
	if len(skill.Scripts) > 0 {
		out += "\n\nScripts available in this skill (run with the run_skill_script tool): " +
			strings.Join(skill.Scripts, ", ")
	}
	return out, nil
}

// RunSkillScript runs a skill's script as a subprocess with the fixed
// I/O contract: stdin is the caller's JSON context, stdout must be JSON.
// Action-Allowed tier — this is the point where a skill executes code,
// and for project-level skills it is exactly what the trust gate exists
// to approve. Untrusted project skills never reach the manager, so they
// are unreachable here by construction.
type RunSkillScript struct {
	Manager *skills.Manager
}

func (RunSkillScript) Name() string { return "run_skill_script" }

func (RunSkillScript) Description() string {
	return "Run a skill's script. Input is a JSON object passed on the script's stdin; the script must answer with JSON on stdout."
}

func (RunSkillScript) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"skill": {"type": "string", "description": "Skill name"},
			"script": {"type": "string", "description": "Script file name within the skill's scripts directory"},
			"input": {"type": "string", "description": "JSON object passed to the script on stdin"}
		},
		"required": ["skill", "script", "input"]
	}`)
}

func (RunSkillScript) Tier() Tier { return TierActionAllowed }

// scriptTimeout bounds a skill script so a hung one cannot wedge a turn.
const scriptTimeout = 60 * time.Second

func (t RunSkillScript) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Skill  string `json:"skill"`
		Script string `json:"script"`
		Input  string `json:"input"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	skill, ok := t.Manager.Get(a.Skill)
	if !ok {
		return "", fmt.Errorf("run_skill_script: no skill named %q", a.Skill)
	}
	// The script must be one of the discovered basenames — this both
	// enforces the fixed contract and closes path traversal.
	matched := ""
	for _, s := range skill.Scripts {
		if s == a.Script {
			matched = s
			break
		}
	}
	if matched == "" {
		return "", fmt.Errorf("run_skill_script: skill %q has no script %q (available: %s)",
			a.Skill, a.Script, strings.Join(skill.Scripts, ", "))
	}
	if a.Input == "" {
		a.Input = "{}"
	}
	if !json.Valid([]byte(a.Input)) {
		return "", fmt.Errorf("run_skill_script: input must be a JSON object")
	}

	scriptPath := filepath.Join(skill.Dir, "scripts", matched)
	if _, err := os.Stat(scriptPath); err != nil {
		return "", fmt.Errorf("run_skill_script: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, scriptTimeout)
	defer cancel()
	// Executable files run directly so shebangs work; the rest go
	// through sh. Scripts from untrusted projects are already excluded
	// by discovery.
	var cmd *exec.Cmd
	if info, err := os.Stat(scriptPath); err == nil && info.Mode()&0o111 != 0 {
		cmd = exec.CommandContext(runCtx, scriptPath)
	} else {
		cmd = exec.CommandContext(runCtx, "sh", scriptPath)
	}
	cmd.Stdin = strings.NewReader(a.Input)
	out, err := cmd.Output()
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("run_skill_script: timed out after %s", scriptTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("run_skill_script: %w", err)
	}
	result := strings.TrimSpace(string(out))
	if !json.Valid([]byte(result)) {
		return "", fmt.Errorf("run_skill_script: script %s broke the I/O contract — stdout must be JSON, got: %.200s",
			matched, result)
	}
	return result, nil
}
