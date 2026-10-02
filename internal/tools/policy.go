package tools

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Chmgx81/tilde/internal/sandbox"
)

// Permission modes (Section 7). The mode sets the posture; the gate
// still checks each action's tier per call, so the promises below
// are enforced per call, not hoped for.
//
// Three modes, Codex's preset triad (Read Only / Default / Full
// Access) and Claude Code's plan/default/bypass at tilde's scale:
//
//   - plan: research posture. Read tools run free; Draft-Only tools
//     (present_plan) run free — the mechanical exit from planning;
//     every action-tier call asks the user. The tools are still
//     offered, so the model can propose a write — the approval
//     dialog is the posture.
//   - build: what the sandbox bounds runs without prompting —
//     Landlock-confined shell commands, file writes and patches
//     inside the same writable roots. Everything that escapes
//     (unsandboxed shell calls, writes outside the roots, web
//     fetches, or any action when Landlock is unavailable) asks.
//   - full-auto: everything runs, still logged.
const (
	ModePlan     = "plan"
	ModeBuild    = "build"
	ModeFullAuto = "full-auto"

	// Legacy spellings from earlier phases; NormalizeMode maps them
	// so every existing config keeps working: Phase 30's read-only
	// folded into plan (they were the same posture in the gate), and
	// Phase 30's ask became build.
	ModeReadOnly       = "read-only"
	ModeAsk            = "ask"
	ModeAskEveryTime   = "ask-every-time"
	ModeAutoAcceptSafe = "auto-accept-safe-ops"
)

// Modes is the complete set, in display order: plan, build, act.
var Modes = []string{ModePlan, ModeBuild, ModeFullAuto}

// NormalizeMode canonicalizes a configured mode name: the legacy
// read-only spelling maps to plan (the merged research posture),
// and ask / ask-every-time / auto-accept-safe-ops map to build.
// Anything else unknown stays unknown (and behaves as build in the
// policy) rather than being silently coerced to something
// permissive.
func NormalizeMode(mode string) string {
	switch mode {
	case ModeReadOnly:
		return ModePlan
	case ModeAsk, ModeAskEveryTime, ModeAutoAcceptSafe:
		return ModeBuild
	}
	return mode
}

// ValidMode reports whether mode is one of the three real modes.
// It checks the raw name: legacy spellings are not real modes —
// callers normalize first (config loading does).
func ValidMode(mode string) bool {
	switch mode {
	case ModePlan, ModeBuild, ModeFullAuto:
		return true
	}
	return false
}

// ModeAllowsTool reports whether a tool is offered to the model at
// all under this mode. Since Phase 30 every mode offers every
// tier: offering is not permission, the gate is the single
// enforcement point — a read-only session can propose a write and
// the approval dialog (or a headless denial) is where the posture
// bites.
func ModeAllowsTool(mode string, t Tool) bool {
	return ModeAllowsTier(mode, t.Tier())
}

// ModeAllowsTier is the tier-level form, for callers (like the
// orchestrator's tool defs) that hold a Def, not a Tool.
func ModeAllowsTier(mode string, tier Tier) bool {
	return true
}

// ModeInstruction is the mode's contribution to the system prompt, so
// the model knows the posture it runs under (Section 3.2: mode
// determines the system prompt).
func ModeInstruction(mode string) string {
	switch NormalizeMode(mode) {
	case ModePlan:
		return "Permission mode is plan: research with the read tools (read_file, grep, glob, list_dir) and do not change anything yet — any write, command, or web fetch you call asks the user first. When you understand the task, present exactly one plan with the present_plan tool — markdown with the goal, concrete steps, and risks — and stop. Wait for the user's decision; do not act before it."
	case ModeBuild:
		return "Permission mode is build: sandboxed shell commands — writes kernel-confined to the working directory, /tmp, and dev caches — and file writes or patches inside those roots run without prompting. Anything that escapes the bound (bash with \"sandbox\": false, writes outside those roots, web fetches) asks the user first."
	case ModeFullAuto:
		return "Permission mode is full-auto: tool calls run without prompting and are logged."
	}
	return "Permission mode is unknown: state-changing tool calls will ask the user. Ask the user how to proceed if unsure."
}

// PolicyDecide returns a Gate.Decide function implementing the
// Phase 30 decision matrix:
//
//   - The credentials file is denied in every mode, before the
//     matrix, for every tool that names it in a path argument (read,
//     write, edit, patch, grep, glob, list): no posture — not even
//     full-auto — hands the user's keys to the model's context or
//     lets the model rewrite them. A user who wants to see them can
//     run "!cat ~/.tilde/auth.json" themselves.
//   - Read-Only and Draft-Only tools are always allowed — running
//     them cannot change state.
//   - Action-Allowed tools in full-auto are always allowed.
//   - Action-Allowed tools in build mode run without prompting when
//     the call is bounded (sandbox.Active() Landlock-confined shell
//     command, or a file write inside the sandbox's writable
//     roots); escapes prompt.
//   - plan prompts for every action-tier call.
//   - A nil prompt denies: fail closed, never open. Unknown modes
//     behave as build — a config typo must not widen permissions.
func PolicyDecide(mode string, prompt func(tool Tool, args string) bool) func(Tool, string) bool {
	return func(tool Tool, args string) bool {
		if touchesCredentials(tool, args) {
			return false
		}
		switch tool.Tier() {
		case TierReadOnly, TierDraftOnly:
			return true
		case TierActionAllowed:
			switch NormalizeMode(mode) {
			case ModeFullAuto:
				return true
			case ModeBuild:
				if boundedAction(tool, args) {
					return true
				}
				return askPrompt(prompt, tool, args)
			default: // plan, unknown
				return askPrompt(prompt, tool, args)
			}
		default:
			return false
		}
	}
}

func askPrompt(prompt func(Tool, string) bool, tool Tool, args string) bool {
	if prompt == nil {
		return false
	}
	return prompt(tool, args)
}

// boundedAction reports whether a call's effects are already
// confined, so ask mode can run it without prompting: a sandboxed
// shell command under an active Landlock ruleset, or an
// in-process file write inside the same writable roots the sandbox
// grants. Anything else — the explicit {"sandbox": false} escape,
// an out-of-roots path, unparsable args, or an unavailable
// sandbox — is unbounded and asks.
func boundedAction(tool Tool, args string) bool {
	switch tool.(type) {
	case Bash:
		if ShellEscaped(args) {
			return false
		}
		// Credit the sandbox only when it is actually enforced;
		// without Landlock a "sandboxed" command runs free.
		return sandbox.Active()
	case WriteFile, EditFile:
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(args), &a) != nil || a.Path == "" {
			return false
		}
		return pathInWritableRoots(a.Path)
	case ApplyPatch:
		// A patch is bounded only when every file it touches is
		// inside the roots — a partial bound is no bound.
		paths := patchPaths(args)
		if len(paths) == 0 {
			return false
		}
		for _, p := range paths {
			if !pathInWritableRoots(p) {
				return false
			}
		}
		return true
	}
	return false
}

// ShellEscaped reports whether a bash call opted out of the
// sandbox. Unparsable args read as an escape — fail closed. Shared
// with the TUI, whose approval dialog names the escape for what it
// is.
func ShellEscaped(args string) bool {
	var a struct {
		Sandbox *bool `json:"sandbox"`
	}
	if json.Unmarshal([]byte(args), &a) != nil {
		return true
	}
	return a.Sandbox != nil && !*a.Sandbox
}

// pathInWritableRoots reports whether path — resolved the way the
// kernel resolves it when it opens the file — lands inside one of the
// sandbox's writable roots, the identical bound a sandboxed shell
// command gets. In-process writes cannot be Landlocked, so the gate
// bounds them by path.
//
// The resolution has to be the kernel's, component by component, and
// not filepath.Clean's: Clean (which filepath.Abs calls) collapses
// "link/.." lexically, before the link is ever followed, so with
// docs -> /outside/vault inside the project it reported
// "project/../secret.txt" — inside the roots — while the write landed
// in /outside. The gate and the kernel have to agree about where the
// write goes, or the gate approves nothing. A lexical in-tree path
// that resolves outside must not auto-run.
//
// The path need not exist yet — writing a file that is being created,
// or one whose directory is being created, is ordinary work, so a
// missing tail is joined lexically. It is joined only when no symlink
// has been followed: after one, "missing" means a dangling link, and
// the honest answer to a dangling link is no answer, so it is refused
// along with loops and unreadable components.
func pathInWritableRoots(path string) bool {
	resolved, ok := resolveLikeKernel(path)
	if !ok {
		return false
	}
	for _, root := range sandbox.WritableRoots() {
		if withinDir(resolved, resolvedWritableRoot(root)) {
			return true
		}
	}
	return false
}

// resolvedWritableRoot resolves a writable root the same way a path is
// resolved, so an in-root write is not mistaken for an out-of-root one
// on a machine where a root is itself a symlink (/tmp -> /private/tmp
// on macOS, /home -> /var/home on Fedora Atomic). A root that will not
// resolve at all is kept as written: WritableRoots is the tool's own
// list, not model input, so it is compared lexically rather than
// refused.
func resolvedWritableRoot(dir string) string {
	if resolved, ok := resolveLikeKernel(dir); ok {
		return resolved
	}
	return dir
}

// maxSymlinkHops bounds symlink following, like the kernel's own
// ELOOP limit: a cycle must terminate, not spin.
const maxSymlinkHops = 40

// resolveLikeKernel resolves path to the absolute path the kernel
// would open: symlinks are followed and ".." applies to the already
// resolved prefix, exactly as open(2) does. ok is false when the path
// cannot be resolved honestly — a symlink loop, a dangling link, a
// component that may not be stat'ed, no working directory for a
// relative path.
func resolveLikeKernel(path string) (resolved string, ok bool) {
	if !filepath.IsAbs(path) {
		wd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		// Concatenation, not Join: Join cleans, and cleaning here is
		// the bug being fixed.
		path = wd + string(filepath.Separator) + path
	}
	resolved = filepath.VolumeName(path) + string(filepath.Separator)
	queue := pathComponents(path)
	hops := 0
	// linkParts counts the components still queued that arrived from a
	// followed symlink's target. A missing component may only be joined
	// lexically once none are outstanding: a component that came from a
	// link's target and does not exist means the link dangles, and where
	// a dangling link would land once something creates the file is
	// anybody's guess. That distinction is the whole reason this tracks
	// the queue rather than just "have we followed a link".
	//
	// A target is prepended to the queue, so its components are always
	// consumed before the ones they displaced, and each one drains this
	// count as it is resolved. That is what makes a symlink to an
	// existing directory accept a new file inside it: the directory's
	// own components are consumed, the count reaches zero, and only
	// then may a missing component be joined. Counting only the
	// components that reached an Lstat left the count stuck above zero
	// for any target ending in "..", refusing honest work. The drain
	// happens after the component is resolved, never before, so the
	// last component of a dangling target is still caught.
	linkParts := 0
	for len(queue) > 0 {
		part := queue[0]
		queue = queue[1:]
		switch part {
		case "", ".":
			if linkParts > 0 {
				linkParts--
			}
		case "..":
			resolved = filepath.Dir(resolved)
			if linkParts > 0 {
				linkParts--
			}
		default:
			candidate := filepath.Join(resolved, part)
			fi, err := os.Lstat(candidate)
			if err != nil {
				// Only "does not exist" may be joined lexically: the
				// components after it cannot be symlinks, so there is
				// nothing left to resolve. Every other error (ENOTDIR
				// through a symlinked file, EACCES, ELOOP) has no
				// honest answer, so it is refused.
				if !errors.Is(err, fs.ErrNotExist) || linkParts > 0 {
					return "", false
				}
				return filepath.Join(append([]string{candidate}, queue...)...), true
			}
			if fi.Mode()&os.ModeSymlink == 0 {
				resolved = candidate
				if linkParts > 0 {
					linkParts--
				}
				continue
			}
			if hops++; hops > maxSymlinkHops {
				return "", false
			}
			target, err := os.Readlink(candidate)
			if err != nil {
				return "", false
			}
			if filepath.IsAbs(target) {
				resolved = filepath.VolumeName(target) + string(filepath.Separator)
			}
			parts := pathComponents(target)
			queue = append(parts, queue...)
			// This component is itself consumed; the target's are owed
			// in its place, including the empty head of an absolute
			// target and any "." or "..": the switch above consumes those
			// without an Lstat, and a count that skipped them would stay
			// above zero and refuse every later missing component.
			if linkParts > 0 {
				linkParts--
			}
			linkParts += len(parts)
		}
	}
	return resolved, true
}

// pathComponents splits a path into components without cleaning it.
// Windows accepts both separators, because the model writes paths the
// way humans do; everywhere else a backslash is a file name.
func pathComponents(p string) []string {
	if filepath.Separator == '/' {
		return strings.Split(p, "/")
	}
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
}

func withinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
