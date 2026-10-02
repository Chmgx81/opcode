package tui

// inputDecision is what a composer submit became — one typed
// outcome per path, so "why didn't my message send" is a value a
// caller can read instead of behavior to reverse-engineer (the
// adoption doc's #2: Codex's turn_input with NotSubmittedReasons,
// at opcode's scale).
type inputDecision int

const (
	// inputIgnored: an empty draft; nothing happened.
	inputIgnored inputDecision = iota
	// inputShell: the user's "!cmd" escape ran in the shell — no
	// model round trip.
	inputShell
	// inputCommand: a slash command was handled by the command
	// system; no turn.
	inputCommand
	// inputQueued: a turn is running; the draft is held and submits
	// when the turn ends. (Alt+Enter.)
	inputQueued
	// inputSteered: a turn is running; the draft folds in at the
	// next round boundary. It cannot alter the active round —
	// that request is already on the wire.
	inputSteered
	// inputTurnStarted: no turn was running; a new one started.
	inputTurnStarted
)
