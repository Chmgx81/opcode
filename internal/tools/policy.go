package tools

// Permission modes recognized in Phase 1. The full mode system
// (read-only / ask-every-time / auto-accept-safe-ops / full-auto as a
// selectable, switchable mode set) is Phase 2; these two cover what
// config.json can already name today.
const (
	ModeAsk      = "ask"
	ModeFullAuto = "full-auto"
)

// PolicyDecide returns a Gate.Decide function implementing the Phase 1
// subset of Section 7's tiered model:
//
//   - Read-Only tools are always allowed — they cannot change anything.
//   - Action-Allowed tools are allowed without asking only in full-auto
//     mode (still logged by the gate); otherwise the prompt callback
//     decides. A nil prompt denies: fail closed, never open.
//
// Any unrecognized mode is treated as ask — the safe direction; a config
// typo must not silently grant more than was intended.
func PolicyDecide(mode string, prompt func(tool Tool, args string) bool) func(Tool, string) bool {
	return func(tool Tool, args string) bool {
		if tool.Tier() == TierReadOnly {
			return true
		}
		if mode == ModeFullAuto {
			return true
		}
		if prompt == nil {
			return false
		}
		return prompt(tool, args)
	}
}
