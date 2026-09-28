package tools

// Permission modes (Section 7). The mode sets the default posture across
// tiers; the gate still checks each action's tier, so "read-only mode
// can't touch your filesystem" is enforced per call, not hoped for.
//
// Three modes: look, ask, act. The old fourth mode,
// auto-accept-safe-ops, behaved identically to ask-every-time in the
// gate (drafts auto-run in every mode) — it survives only as a legacy
// config alias.
const (
	ModeReadOnly     = "read-only"
	ModePlan         = "plan"
	ModeAskEveryTime = "ask-every-time"
	ModeFullAuto     = "full-auto"

	// ModeAutoAcceptSafe is deprecated; NormalizeMode maps it to
	// ask-every-time, the more restrictive direction.
	ModeAutoAcceptSafe = "auto-accept-safe-ops"
)

// Modes is the complete set, in display order: look, plan, ask, act.
var Modes = []string{ModeReadOnly, ModePlan, ModeAskEveryTime, ModeFullAuto}

// NormalizeMode canonicalizes a configured mode name. The Phase 1
// spelling "ask" and the removed auto-accept-safe-ops map to
// ask-every-time so existing configs keep working — always toward the
// more restrictive posture; anything else unknown stays unknown (and
// fails closed in the policy) rather than being silently coerced to
// something permissive.
func NormalizeMode(mode string) string {
	switch mode {
	case "ask", ModeAutoAcceptSafe:
		return ModeAskEveryTime
	}
	return mode
}

// ValidMode reports whether mode is one of the four real modes.
func ValidMode(mode string) bool {
	switch mode {
	case ModeReadOnly, ModePlan, ModeAskEveryTime, ModeFullAuto:
		return true
	}
	return false
}

// ModeAllowsTool reports whether a tool is offered to the model at all
// under this mode (Section 3.2: mode determines the allowed tool set).
// Read-only mode offers only Read-Only tools; plan mode additionally
// offers Draft-Only tools — present_plan is the mechanical exit from
// planning. In neither mode can the model even see an action-tier
// tool; the gate's denial is only the backstop for a hallucinated
// call.
func ModeAllowsTool(mode string, t Tool) bool {
	return ModeAllowsTier(mode, t.Tier())
}

// ModeAllowsTier is the tier-level form of the policy, for callers
// (like the orchestrator's tool defs) that hold a Def, not a Tool.
func ModeAllowsTier(mode string, tier Tier) bool {
	switch NormalizeMode(mode) {
	case ModeReadOnly:
		return tier == TierReadOnly
	case ModePlan:
		return tier == TierReadOnly || tier == TierDraftOnly
	}
	return true
}

// ModeInstruction is the mode's contribution to the system prompt, so
// the model knows the posture it runs under (Section 3.2: mode
// determines the system prompt).
func ModeInstruction(mode string) string {
	switch NormalizeMode(mode) {
	case ModeReadOnly:
		return "Permission mode is read-only: tools that modify state are not available. Do not attempt to write, edit, or run commands."
	case ModePlan:
		return "Permission mode is plan: you cannot change anything yet. Research the codebase with read tools, then present exactly one plan with the present_plan tool — markdown with the goal, concrete steps, and risks — and stop. Wait for the user's decision; do not act before it."
	case ModeAskEveryTime:
		return "Permission mode is ask-every-time: each call to a state-changing tool asks the user before running."
	case ModeFullAuto:
		return "Permission mode is full-auto: tool calls run without prompting and are logged."
	}
	return "Permission mode is unknown: state-changing tool calls will ask the user. Ask the user how to proceed if unsure."
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
			case ModeReadOnly, ModePlan:
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
