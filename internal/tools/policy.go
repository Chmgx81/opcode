package tools

// Permission modes (Section 7). The mode sets the default posture across
// tiers; the gate still checks each action's tier, so "read-only mode
// can't touch your filesystem" is enforced per call, not hoped for.
const (
	ModeReadOnly       = "read-only"
	ModeAskEveryTime   = "ask-every-time"
	ModeAutoAcceptSafe = "auto-accept-safe-ops"
	ModeFullAuto       = "full-auto"
)

// Modes is the complete set, in display order.
var Modes = []string{ModeReadOnly, ModeAskEveryTime, ModeAutoAcceptSafe, ModeFullAuto}

// NormalizeMode canonicalizes a configured mode name. The Phase 1
// spelling "ask" maps to ask-every-time so existing configs keep
// working; anything else unknown stays unknown (and fails closed in the
// policy) rather than being silently coerced to something permissive.
func NormalizeMode(mode string) string {
	if mode == "ask" {
		return ModeAskEveryTime
	}
	return mode
}

// ValidMode reports whether mode is one of the four real modes.
func ValidMode(mode string) bool {
	switch mode {
	case ModeReadOnly, ModeAskEveryTime, ModeAutoAcceptSafe, ModeFullAuto:
		return true
	}
	return false
}

// ModeAllowsTool reports whether a tool is offered to the model at all
// under this mode (Section 3.2: mode determines the allowed tool set).
// In read-only mode the model never sees action-tier tools; the gate's
// denial is only the backstop for a hallucinated call.
func ModeAllowsTool(mode string, t Tool) bool {
	if mode == ModeReadOnly {
		return t.Tier() == TierReadOnly
	}
	return true
}

// ModeInstruction is the mode's contribution to the system prompt, so
// the model knows the posture it runs under (Section 3.2: mode
// determines the system prompt).
func ModeInstruction(mode string) string {
	switch mode {
	case ModeReadOnly:
		return "Permission mode is read-only: tools that modify state are not available. Do not attempt to write, edit, or run commands."
	case ModeAskEveryTime:
		return "Permission mode is ask-every-time: each call to a state-changing tool asks the user before running."
	case ModeAutoAcceptSafe:
		return "Permission mode is auto-accept-safe-ops: read-only operations run automatically; state-changing calls ask the user first."
	case ModeFullAuto:
		return "Permission mode is full-auto: tool calls run without prompting and are logged."
	}
	return "Permission mode is unknown: state-changing tool calls will ask the user."
}

// PolicyDecide returns a Gate.Decide function implementing the Phase 2
// decision matrix:
//
//   - Read-Only and Draft-Only tools are always allowed — running them
//     cannot change state. (Applying a Draft-Only proposal is a separate,
//     gated action that no Phase 2 tool performs.)
//   - Action-Allowed tools: denied without prompting in read-only mode —
//     that mode's promise is that nothing mutates; allowed without
//     prompting (still logged) in full-auto; prompted otherwise.
//   - A nil prompt denies: fail closed, never open. Unknown modes behave
//     as ask-every-time — a config typo must not widen permissions.
func PolicyDecide(mode string, prompt func(tool Tool, args string) bool) func(Tool, string) bool {
	return func(tool Tool, args string) bool {
		switch tool.Tier() {
		case TierReadOnly, TierDraftOnly:
			return true
		case TierActionAllowed:
			switch NormalizeMode(mode) {
			case ModeFullAuto:
				return true
			case ModeReadOnly:
				return false
			default:
				if prompt == nil {
					return false
				}
				return prompt(tool, args)
			}
		default:
			return false
		}
	}
}
