// Package tui is opcode's Bubble Tea front end: a streaming transcript
// with a collapsible tool timeline, a multiline composer with paste
// collapsing and a command palette, mode cycling, and headless-friendly
// wiring — the orchestrator stays UI-independent.
package tui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/Chmgx81/opcode/internal/config"
	"github.com/Chmgx81/opcode/internal/llm"
	"github.com/Chmgx81/opcode/internal/orchestrator"
	"github.com/Chmgx81/opcode/internal/safe"
	"github.com/Chmgx81/opcode/internal/skills"
	"github.com/Chmgx81/opcode/internal/tools"
)

// version is the fallback shown when Options.Version is empty (tests
// and builds without version wiring). Production passes the real
// build version in — one version everywhere: the banner, --version,
// and the fetch UA agree.
const version = "v0.3.0"

// The opcode logo, shown at the top of a fresh session; it scrolls away
// with the transcript.
//
//go:embed banner.txt
var banner string

// composerPlaceholder is the idle composer's hint. It is dropped
// whole rather than clipped on a pane too narrow for it
// (syncComposerPlaceholder).
const composerPlaceholder = "ask opcode anything…"

// Options wires the TUI to the rest of the system. Everything here is
// injected; the package never loads config itself.
type Options struct {
	Orch       *orchestrator.Orchestrator
	Model      string
	Mode       string
	Cwd        string
	OpcodeHome string
	// Version is the running binary's version (cmd/opcode's
	// buildVersion: ldflags tag, module version, or (devel)). It
	// renders in the greeting, /doctor, and the resume banner.
	// Empty falls back to the version const above.
	Version string
	// UpdateTag is the newer release tag the startup check knows
	// about, or "" when there is nothing to offer. It renders as the
	// footer badge and the /help line: the durable half of the
	// update signal, which the startup note alone is not — that note
	// scrolls away within a screenful. cmd/opcode derives it from
	// update.LatestKnown, so the note, this badge, --version,
	// /doctor, and the exit line all name the same release.
	UpdateTag string
	// ProviderName + BaseURL + API let /login rebuild the LLM client with the
	// new key instead of needing a restart.
	ProviderName string
	BaseURL      string
	API          string // wire type: "openai" or "anthropic"
	AuditPath    string

	// Animations turns off the spinner and toast glyph burst when
	// false (config.json "animations": false) — the reduced-motion
	// posture, Codex's MotionMode at opcode's scale.
	Animations bool

	// Plain is the ASCII/screen-reader posture: --plain,
	// OPCODE_PLAIN, or a detected screen reader swap the glyph
	// vocabulary for ASCII (adaptGlyphs). Color still follows the
	// theme and NO_COLOR; the point is that no glyph disappears,
	// every one degrades.
	Plain bool

	// Skills and MCP managers back the /skills and /mcp commands.
	Skills   *skills.Manager
	MCPNames func() []string // connected server names + tool counts

	// Models backs the /model picker. SwitchModel rebuilds the provider
	// and the model everywhere they live (orchestrator, subagent
	// runner, audit redactor); the TUI stays free of wiring.
	Models      config.ModelsConfig
	SwitchModel func(provider, model string) error
	// Theme names the startup palette (config.json "theme"); empty
	// means the background probe picks dark or light. SetTheme
	// persists a /theme switch; nil makes the switch session-only.
	Theme    string
	SetTheme func(name string) error
	// Effort seeds the reasoning-effort knob ("" = provider
	// default); alt+. / alt+, cycle it live.
	Effort string
	// Redactor is the process-wide secret list — /login adds the
	// new key here so the audit log AND session saves redact it
	// (audit S4/S5). Nil means /login cannot update redaction.
	Redactor *tools.Redactor
	// KeyFor resolves a provider's API key through the credential
	// chain (auth.json, explicit env, derived env) — the /models
	// picker needs it to fetch live model lists. Missing is fine:
	// providers without keys are shown with a /login hint.
	KeyFor func(provider string) (string, bool)
	// SaveCurrentSession persists the current conversation tree;
	// ResumeSession loads another session and re-seeds the orchestrator.
	// /sessions owns only the interaction.
	SaveCurrentSession func()
	ResumeSession      func(path string) (messages int, err error)

	PendingTrust *TrustDecision
	StartupNotes []string
}

// TrustDecision is one pending project-trust question. Approved lists
// the literal files that will become runnable (Section 7).
type TrustDecision struct {
	ProjectDir string
	Approved   []string
	OnAnswer   func(trusted bool)
	OpenedAt   time.Time // the type-ahead guard, as above
}

// permRequest is one pending permission decision. sel is the
// highlighted option (0 yes, 1 yes-and-always, 2 no — the safest
// default, per spec 9.2); scope is option 2's literal "don't ask
// again" grant, computed when the request opened.
type permRequest struct {
	tool     string
	tier     tools.Tier
	args     string
	scope    string
	sel      int
	openedAt time.Time // the type-ahead guard: keys inside typeAheadGuard cannot answer
	reply    chan bool
}

type permRequestMsg struct{ req *permRequest }

// planRequest is one pending plan decision: the model presented a
// plan (present_plan tool); the reply carries the verdict.
type planRequest struct {
	plan     string
	openedAt time.Time // the type-ahead guard, as above
	reply    chan planVerdict
}

type planVerdict struct {
	proceed bool
	auto    bool
}

type planRequestMsg struct{ req *planRequest }

// orchestratorMsg wraps one orchestrator event as a tea.Msg.
type orchestratorMsg orchestrator.Event

type loginFlow struct{ provider string }

// subagentMsg wraps one subagent progress event as a tea.Msg.
type subagentMsg subagentEvent

type subagentEvent struct {
	Title string
	Kind  string
	Text  string
	Usage llm.Usage
}

// statusTickMsg drives the working-status line (elapsed time).
type statusTickMsg time.Time

// Subagent event kinds, in sync with internal/subagent.
const (
	subagentText  = "text"
	subagentTool  = "tool"
	subagentDone  = "done"
	subagentError = "error"
	subagentUsage = "usage"
)

// Entry kinds in the transcript. Results carry enough structure to
// render collapsed or expanded, and edit_file results render as a
// colored diff hunk.
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryTool
	entryResult
	entryOK
	entryErr
	entryDim
	entrySteer
	entryQueued
	entryCompaction
	entrySubagent
	entryPlan
	entryReasoning
	entryDiff
)

type entry struct {
	kind entryKind
	text string // primary text / tool args / note text
	tool string // tool name (entryTool, entryResult)
	// Diff details for edit_file results; empty for other tools.
	path     string
	old, new string
	full     string // full result text (expansion)
	summary  string // collapsed one-liner
	subTitle string // subagent label
	dur      string // reasoning: how long the model thought (preformatted)

	// Markdown cache (entryAssistant): rendered once per width so View
	// doesn't re-run glamour on every frame.
	rendered  []string
	renderedW int
}

// queued is one follow-up waiting for the current turn to finish; it
// may carry images, which attach when the queued turn actually runs.
type queued struct {
	text   string
	images []llm.Image
}

// Model is the Bubble Tea model. Pointer receiver so the gate's prompt
// closure, the pump goroutine, and the palette share one instance.
type Model struct {
	opt     Options
	program *tea.Program

	composer textarea.Model
	spinner  spinner.Model
	width    int
	height   int

	working      bool
	workingSince time.Time
	cancel       context.CancelFunc

	// shell is the in-flight "!command". It runs as a tea.Cmd on its
	// own goroutine under a cancellable context: inline, it froze the
	// whole render loop for the length of the command and could not be
	// interrupted. esc cancels it like it cancels a turn.
	shell *shellRun

	awaitingTrust *TrustDecision
	awaitingPerm  *permRequest
	awaitingPlan  *planRequest
	grantMu       sync.Mutex // guards sessionRules/sessionAllow/toolAllows

	// Session-scoped "don't ask again" grants from the approval
	// dialog: shell commands by token prefix (fail-closed matching,
	// the same rules the config allowlist uses), other tools by name.
	// decide() reads these on the ORCHESTRATOR's dispatch goroutine
	// while grantAlways() writes on the tea goroutine — every access
	// goes through grantMu (audit C4).
	sessionRules []string
	sessionAllow *tools.ShellAllowlist
	toolAllows   map[string]bool

	queue []queued // follow-ups (Alt+Enter while working)

	// Images attached by ctrl+v, keyed by their composer placeholder
	// ("[Image #1]"); consumed on submit like paste tokens.
	images   map[string]llm.Image
	imageSeq int

	usage llm.Usage

	// entries is the structured transcript; View renders it.
	// committed counts the entries already printed to native
	// scrollback (tea.Println) — View skips them, and they are
	// frozen: ctrl+r expansion applies to the live region only.
	entries   []entry
	committed int
	stream    strings.Builder
	login     *loginFlow

	// storedKeys holds what /login saved THIS session, keyed by
	// provider. auth.json is the durable store, but the resolver's
	// startup snapshot does not see a later write — this map is how
	// the pickers, the fetches, and the pre-flight know a key exists
	// without a restart. /logout forgets its copy here too.
	storedKeys map[string]string

	// Composer recall history: submitted prompts, persisted to
	// history.jsonl under OPCODE_HOME. histIdx is the recall
	// position (len(hist) means the live draft); draftSave holds
	// the in-progress draft while a recall is active.
	hist      []string
	histIdx   int
	draftSave string

	// todos is the model's live task list (rendered as a panel at
	// the transcript tail — state, not history).
	todos []tools.Todo

	// reasoning accumulates the model's thinking stream; it renders
	// live (tail-windowed) while it arrives and collapses to a
	// "thought for Ns" line once the answer starts.
	reasoning      strings.Builder
	reasoningSince time.Time

	// streamRendered caches the in-flight stream's markdown render
	// (invalidated by content length or width) so View's per-frame
	// pass costs nothing.
	streamRendered    []string
	streamRenderedLen int
	streamRenderedW   int

	// expandResults toggles ctrl+r result expansion.
	expandResults bool

	// Composer plumbing: large pastes collapse to tokens in the
	// composer and re-expand on submit.
	pastes  []string
	pasteAt map[string]string // token -> content

	// Command palette: open when the composer starts with "/".
	paletteIdx int

	// The active theme and the backup the /theme picker restores on
	// cancel. curTheme is what the session shows; themeBackup is set
	// while the picker is open.
	curTheme    string
	themeBackup string

	// effort is the reasoning-effort knob ("" = provider default).
	// Live state like the permission mode; the next model request
	// carries it.
	effort string

	// The @-mention file picker: live-filtered from the composer's
	// trailing "@query", navigable, insertable. atFiles is the cached
	// project file list; atDismissAt holds the byte position of the "@"
	// Esc dismissed, so the menu stays closed for that mention until a
	// fresh one appears.
	atMenu      []string
	atIdx       int
	atDismissed bool
	atDismissAt int
	atFiles     []string

	// workingVerb is the turn's gerund ("Thinking…", "Pondering…") —
	// the reference apps' dynamic microcopy.
	workingVerb string

	// The ctrl+O transcript pager: an overlay over the whole
	// conversation (committed and live, results expanded), scrolled
	// by transcriptTop. Navigation, not history — the pager reads
	// what scrolled away.
	transcriptOpen bool
	transcriptTop  int

	// quitArmedAt is when the first ctrl+c (or ctrl+d) armed the
	// exit — Codex's double-press quit: only a second press inside
	// quitWindow (update.go) exits; the first just hints (and
	// interrupts a running turn).
	quitArmedAt time.Time

	// Overlay picker (/model, /sessions): filter-as-you-type list.
	picker *picker

	// help overlay ("?"), and how far it is scrolled. The sheet is
	// longer than most terminals are tall — it lists every binding and
	// every command — so it windows itself rather than truncating: a
	// help overlay that silently dropped half the commands was worse
	// than one that scrolls.
	helpOpen bool
	helpTop  int

	// overlayRows is the content-row budget for the floating blocks in
	// this frame, set by View before they render. A field, not an
	// argument: the budget is the frame's arithmetic, and six
	// renderers each recomputing it is six chances to disagree about
	// how tall the terminal is.
	overlayRows int

	// toast is a transient status message above the composer;
	// toastAnim counts remaining animation frames while it plays.
	toast     string
	toastAt   time.Time
	toastAnim int

	// Subagent progress accumulation, flushed at boundaries.
	subMu      sync.Mutex
	subStreams map[string]*strings.Builder
}

// toastFrames is the glyph burst a mode switch plays: the diamond
// blooms open then settles, a short readable transition rather than a
// color flash.
var toastFrames = []string{"◇", "◈", "◆", "◈", "◇", "◇"}

// workingVerbs is the gerund pool the working line picks from each
// turn — dynamic microcopy, the reference apps' "Marinating…" feel.
var workingVerbs = []string{
	"Thinking…", "Pondering…", "Crafting…", "Exploring…",
	"Weaving…", "Marinating…", "Meandering…", "Noodling…",
}

// toastTickMsg drives the toast animation.
type toastTickMsg time.Time

// editorDoneMsg reports the external editor (ctrl+e) finished with
// the composer's temp file.
type editorDoneMsg struct {
	path string
	err  error
}

// todoMsg carries the model's current task list to the tea program.
type todoMsg []tools.Todo

// shellRun is one in-flight "!command": the command as the user typed
// it, the cancel for its context, and the reason it is running at all
// (shown on the status line — a command that takes two minutes is not
// something to leave unannounced).
type shellRun struct {
	name   string
	cancel context.CancelFunc
	// done closes when the command's context is released, so a caller
	// that quits out from under it can tell "killed" from "still
	// running".
	done chan struct{}
	once sync.Once
}

// shellDoneMsg reports a finished "!command": its sanitized combined
// output and the error, if any. The output is untrusted text by every
// rule the model's own tool results follow — a command can print an
// escape sequence like anything else.
type shellDoneMsg struct {
	out string
	err error
}

func toastTick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(t time.Time) tea.Msg {
		return toastTickMsg(t)
	})
}

// Commands is the palette's source of truth; the desc renders in the
// picker, the action runs on Enter.
type command struct {
	Name string
	Desc string
}

var commands = []command{
	{"/exit", "quit opcode"},
	{"/quit", "quit opcode"},
	{"/help", "show keys and commands"},
	{"/doctor", "diagnose the setup: config, key, sandbox, trust, mcp"},
	{"/theme", "pick the palette — live preview, esc restores"},
	{"/diff", "show the working tree's git changes"},
	{"/mode", "pick the permission mode (or /mode <name>)"},
	{"/model", "pick or switch the model"},
	{"/sessions", "browse and resume a saved session"},
	{"/skills", "list available skills"},
	{"/mcp", "list MCP servers and tools"},
	{"/models", "browse every provider's models, fetched live"},
	{"/login", "store an API key (masked; bare form picks a provider)"},
	{"/logout", "remove the stored key (/logout <provider>)"},
	{"/update", "point at `opcode update` — it runs in a shell, not here"},
}

// displayVersion reports the running binary's version: the injected
// Options.Version, or the fallback const when unwired (tests).
func (m *Model) displayVersion() string {
	if m.opt.Version != "" {
		return m.opt.Version
	}
	return version
}

func New(opt Options) *Model {
	// The glyph posture must be installed before anything reads it: the
	// composer prompt and the header bake in GlyphPrompt/GlyphBrand at
	// construction. Run() applies it again (it also owns the theme
	// probe), which is idempotent — this call is what keeps a --plain
	// header and composer from flashing the Unicode forms first.
	adaptGlyphs(opt.Plain)
	ta := textarea.New()
	ta.Placeholder = composerPlaceholder
	// Strip the textarea's stock look, which reads as a highlight: the
	// default focused CursorLine paints a black background rectangle
	// (visible on any terminal whose floor isn't pure #000000), and the
	// stock prompt is bright white. The placeholder itself is a hint
	// and dims with the design token.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = accentStyle
	ta.BlurredStyle.Prompt = accentStyle
	ta.FocusedStyle.Placeholder = subtleStyle
	ta.BlurredStyle.Placeholder = subtleStyle
	ta.Prompt = GlyphPrompt + " "
	ta.CharLimit = 0
	// One line when empty, Claude-Code-style: the composer grows with
	// typed content (resizeComposer) instead of reserving rows that
	// render as empty prompt lines. Hints live on the footer line, not
	// in the placeholder.
	ta.SetHeight(1)
	// The textarea's own width defaults to 40 columns — placeholder and
	// typed text wrap there no matter how wide the terminal is. Size it
	// to the bare prompt line's width (terminal minus the prompt and a
	// right margin); the first WindowSizeMsg corrects it for the real
	// terminal.
	ta.SetWidth(72)
	ta.ShowLineNumbers = false
	ta.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))

	m := &Model{
		opt:        opt,
		composer:   ta,
		spinner:    sp,
		pasteAt:    map[string]string{},
		subStreams: map[string]*strings.Builder{},
		storedKeys: map[string]string{},
		effort:     opt.Effort,
	}
	// Prompt recall history: loaded once at startup, appended per
	// submit. A corrupt line is skipped, never fatal.
	m.hist = loadHistory(filepath.Join(opt.OpcodeHome, "history.jsonl"))
	m.histIdx = len(m.hist)
	// The reference header: the logo at the left, the identity block
	// beside it — version, model and mode, working directory — then
	// startup notes below. All of it scrolls away with the transcript.
	var logo []string
	for _, line := range strings.Split(strings.TrimRight(banner, "\n"), "\n") {
		logo = append(logo, accentStyle.Render(line))
	}
	info := []string{boldStyle.Render(GlyphBrand + " opcode " + m.displayVersion())}
	modelLine := opt.Model
	if modelLine == "" {
		// A first run with no config.json yet: the header must not
		// render an empty slot where the model goes.
		modelLine = "no model yet"
	}
	// One identity line — this model, in this mode, in this
	// directory — instead of a form's worth of rows beside the mark.
	idLine := modelLine + " · " + opt.Mode
	if opt.Cwd != "" {
		idLine += " · " + opt.Cwd
	}
	info = append(info, dimStyle.Render(idLine))
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		strings.Join(logo, "\n"), "   ", strings.Join(info, "\n"))
	m.entries = append(m.entries, entry{kind: entryDim, text: header})
	m.entries = append(m.entries, entry{kind: entryDim, text: ""})
	for _, note := range opt.StartupNotes {
		m.entries = append(m.entries, entry{kind: entryDim, text: dimStyle.Render(note)})
	}
	if len(opt.StartupNotes) > 0 {
		m.entries = append(m.entries, entry{kind: entryDim, text: ""})
	}
	if opt.Model == "" {
		// First run, no config.json: the UI is the onboarding. Three
		// steps, all in here, no file editing — and the provider list
		// is already open so the first one is visible without reading
		// anything.
		m.entries = append(m.entries,
			entry{kind: entryDim, text: "welcome to opcode — setup is three steps, all in this window:"},
			entry{kind: entryDim, text: "  1. pick a provider below — each row says whether it needs a key (esc closes it, /models reopens it)"},
			entry{kind: entryDim, text: "  2. paste its API key when asked — masked on screen, stored 0600 in auth.json"},
			entry{kind: entryDim, text: "  3. pick a model from the live list — the choice is remembered; /login and /model work any time"},
			entry{kind: entryDim, text: ""})
		m.openModelsPicker("")
	}
	m.awaitingTrust = opt.PendingTrust
	return m
}

// Init implements tea.Model. The terminal title carries the working
// directory, like the reference apps' window titles.
func (m *Model) Init() tea.Cmd {
	if m.opt.Cwd != "" {
		// OSC titles are an untrusted-text injection surface
		// (control and bidi characters can hijack the terminal
		// window title); the cwd is user-controlled text, so it
		// is sanitized before it reaches the terminal.
		return tea.Batch(textarea.Blink,
			tea.SetWindowTitle(sanitizeTitle("opcode — "+m.opt.Cwd)))
	}
	return textarea.Blink
}

// Run starts the program and blocks until it exits. Bubble Tea's
// bracketed paste mode is on by default, so large pastes arrive as a
// single flagged key event and can be collapsed.
//
// termenv detects the color profile by querying the terminal; under
// script(1), CI, or any non-answering terminal that query fails and
// everything renders without color. Falling back to $TERM keeps the
// brand palette alive in those environments; a real terminal still
// uses the query result. NO_COLOR overrides all of it: when the user
// asks for no color, no color — the fallback must not resurrect it.
func Run(m *Model) error {
	if lipgloss.ColorProfile() == termenv.Ascii &&
		os.Getenv("NO_COLOR") == "" {
		term := os.Getenv("TERM")
		switch {
		case os.Getenv("COLORTERM") != "" || strings.Contains(term, "truecolor"):
			lipgloss.SetColorProfile(termenv.TrueColor)
		case strings.Contains(term, "256color"):
			lipgloss.SetColorProfile(termenv.ANSI256)
		case term != "" && term != "dumb":
			lipgloss.SetColorProfile(termenv.ANSI)
		}
	}
	// Theme: an explicit config theme wins outright; otherwise the
	// dark palette is the default posture and a real terminal is
	// asked for its background (the same OSC exchange as the
	// profile probe) — a light background re-skins the tokens for
	// legibility, because light text on a white terminal is
	// invisible. OPCODE_THEME=light|dark forces the probe posture;
	// NO_COLOR keeps Ascii and the tokens' uncolored forms.
	if m.opt.Theme == "" || !applyThemeName(m.opt.Theme) {
		dark := true
		switch strings.ToLower(os.Getenv("OPCODE_THEME")) {
		case "light":
			dark = false
		case "dark":
		default:
			if lipgloss.ColorProfile() != termenv.Ascii && os.Getenv("NO_COLOR") == "" {
				dark = termenv.HasDarkBackground()
			}
		}
		adaptTheme(dark)
	}
	m.curTheme = m.opt.Theme
	if m.curTheme == "" {
		// Auto posture: name the palette the probe actually installed
		// so the picker can mark the truth.
		m.curTheme = curPalette
	}
	adaptGlyphs(m.opt.Plain)
	// The textarea read the glyph at construction, before the plain
	// vocabulary was installed — re-read it now that it is final.
	// The styles are re-copied for the same reason: the theme applied
	// above postdates the composer's construction-time copies.
	m.composer.Prompt = GlyphPrompt + " "
	m.composer.FocusedStyle.Prompt = accentStyle
	m.composer.BlurredStyle.Prompt = accentStyle
	m.composer.FocusedStyle.Placeholder = subtleStyle
	m.composer.BlurredStyle.Placeholder = subtleStyle
	m.syncComposerPlaceholder()
	// Re-seat the textarea's internal style pointer (see
	// syncComposerPrompt): the composer was copied out of New by
	// value, and without this the prompt renders in the palette that
	// was live at construction, not the one applied above.
	m.composer.Focus()

	// Move to top: clear the visible screen and home the cursor
	// before the program takes over, so the frame always starts at
	// the terminal's top row instead of wherever the shell prompt
	// left the cursor. Only the visible screen is erased — the
	// scrollback above survives. (Maximizing the terminal window
	// itself is the window manager's job; no terminal app can do
	// it — ptyxis' own preference or the window's maximize button
	// is the lever for that.)
	fmt.Fprint(os.Stdout, "\x1b[H\x1b[2J")

	p := tea.NewProgram(m)
	m.program = p
	_, err := p.Run()
	return err
}

// Prompt returns the callback the permission gate calls when a tool
// needs a decision. It blocks until the user answers.
func (m *Model) Prompt() func(tool tools.Tool, args string) bool {
	return m.decide
}

func (m *Model) decide(tool tools.Tool, args string) bool {

	// Session-scoped grants from a previous "don't ask again": shell
	// commands by token prefix (fail-closed on metacharacters, the
	// same matching the config allowlist uses), other tools by name.
	if m.sessionGrants(tool.Name(), args) {
		return true
	}
	req := &permRequest{
		tool: tool.Name(),
		tier: tool.Tier(),
		// The dialog renders args verbatim, so it gets the sanitized
		// display copy; grant matching and execution above and in the
		// gate keep the raw args.
		args:  safe.Text(args),
		scope: alwaysScope(tool.Name(), args),
		sel:   2, // the safest default is highlighted first
		reply: make(chan bool, 1),
	}
	if m.program == nil {
		// No UI is running (tests, or before Run): nothing can be
		// approved, so the action fails closed.
		return false
	}
	m.program.Send(permRequestMsg{req})
	return <-req.reply
}

// sessionGrants reports whether a tool call is covered by a
// session-scoped always-allow rule.
func (m *Model) sessionGrants(tool, args string) bool {
	m.grantMu.Lock()
	defer m.grantMu.Unlock()
	if m.toolAllows[tool] {
		return true
	}
	if m.sessionAllow != nil && tool == "bash" {
		var a struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(args), &a) == nil && a.Command != "" &&
			// The whole command is matched against the rule, and a
			// command carrying a line break never matches. The
			// allowlist's tokenizer splits on any whitespace, newline
			// included, so a grant for "git status" otherwise covered
			// "git status\ncurl evil" — the second command rides in
			// under the first one's prefix. Fail closed instead.
			!hasLineBreak(a.Command) &&
			m.sessionAllow.Allows(a.Command) {
			return true
		}
	}
	return false
}

// hasLineBreak reports whether a command spans lines. Bash treats a
// newline as a command separator, so a multi-line "command" is two
// commands and no prefix rule over its first tokens can vouch for it.
func hasLineBreak(s string) bool {
	return strings.ContainsAny(s, "\n\r")
}

// alwaysScope computes option 2's literal grant: for a plain
// program+subcommand, that prefix displayed "<scope>:*". When the
// second field is a flag, the grant must carry the whole command
// verbatim — the naive two-field rule would approve every other
// value of that flag (a grant from "git -C /tmp push" must not
// cover "git -C /etc reset --hard"), and exactly what the user read
// on the dialog is the safe width.
//
// A command spanning lines gets no prefix at all: its fields are
// several commands, and "the first two" is a claim about none of them
// (see hasLineBreak). The dialog says so on the option rather than
// promising a scope that would never match.
func alwaysScope(tool, args string) string {
	if tool != "bash" {
		return tool
	}
	var a struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(args), &a) != nil {
		return tool
	}
	if hasLineBreak(a.Command) {
		return noPrefixScope
	}
	// A metacharacter command gets no prefix either: the allowlist
	// refuses to match one (it fails closed on anything it cannot
	// tokenize exactly), so a rule cut from it would never fire and the
	// dialog would be promising something that cannot happen.
	if strings.ContainsAny(a.Command, ";|&$`><\\") {
		return noPrefixScope
	}
	// Cut from the SANITIZED command, not the raw one: this string is
	// both rendered in the dialog and stored as the session rule, and a
	// command name carrying a control byte is untrusted display text
	// like everything else the model sends. A control byte is not a
	// shell metacharacter, so it survives the check above.
	fields := strings.Fields(safe.Text(a.Command))
	if len(fields) == 0 {
		return tool
	}
	end := len(fields)
	if len(fields) == 1 || !strings.HasPrefix(fields[1], "-") {
		// Plain form: program + subcommand covers its longer forms
		// ("cargo build" covers "cargo build --release").
		if len(fields) > 1 {
			end = 2
		} else {
			end = 1
		}
	}
	return strings.Join(fields[:end], " ") + ":*"
}

// noPrefixScope is the scope label for a command no prefix rule can
// cover. It reads as what actually happens — this one call is allowed
// and nothing is remembered — rather than promising a rule the
// matcher would refuse.
const noPrefixScope = "this one command only (no rule to remember)"

// prefixRule is the stored form of a shell always-allow: the program
// and subcommand, no metacharacters — the ShellAllowlist's matching
// fails closed on anything else anyway. A line break is refused for
// the same reason sessionGrants refuses to match one: the allowlist's
// tokenizer treats it as whitespace, so the rule would match more
// commands than the dialog named. noPrefixScope is refused for the
// same reason again: it is a sentence, not a command, and storing it
// would make a rule that matches nothing while looking like one that
// does.
func prefixRule(scope string) string {
	if scope == noPrefixScope {
		return ""
	}
	rule := strings.TrimSuffix(scope, ":*")
	if hasLineBreak(rule) || strings.ContainsAny(rule, ";|&$`><\\") {
		return ""
	}
	return rule
}

// grantAlways applies option 2: a session-scoped rule, never a file
// on disk, never a provider-side change.
//
// A bash command the rule builder refuses (a metacharacter, a line
// break) gets NO grant at all. The alternative — falling through to
// the per-tool allow — would blanket-allow every bash call while the
// dialog had promised one narrow prefix, which is the one thing this
// dialog must never do. The call itself is still approved; only the
// remembering is skipped, and the note says so.
func (m *Model) grantAlways(req *permRequest) {
	// The write side of the C4 race: the dispatch goroutine may be
	// reading these grants in decide() right now.
	m.grantMu.Lock()
	defer m.grantMu.Unlock()
	if req.tool == "bash" {
		if rule := prefixRule(req.scope); rule != "" {
			m.sessionRules = append(m.sessionRules, rule)
			m.sessionAllow = tools.NewShellAllowlist(m.sessionRules)
			m.showToast("always allowed " + req.scope + " this session")
			return
		}
		// No rule, and specifically none for the per-tool allow: the
		// dialog said one command, so one command is what runs.
		m.showToast("allowed once — opcode cannot remember a rule for a command that chains or spans lines")
		return
	}
	if m.toolAllows == nil {
		m.toolAllows = map[string]bool{}
	}
	m.toolAllows[req.tool] = true
	m.showToast("always allowed " + req.tool + " this session")
}

// PlanApprove returns the callback present_plan calls to surface a
// plan. It blocks on the orchestrator's goroutine until the user
// answers, like the permission prompt.
func (m *Model) PlanApprove() func(plan string) (proceed, auto bool) {
	return func(plan string) (bool, bool) {
		req := &planRequest{
			// The dialog renders the plan verbatim: sanitized copy.
			plan:  safe.Text(plan),
			reply: make(chan planVerdict, 1),
		}
		m.program.Send(planRequestMsg{req})
		v := <-req.reply
		return v.proceed, v.auto
	}
}

// startTurn sends a new user message and pumps the turn's events into
// the tea program until the channel closes. images may be nil.
func (m *Model) startTurn(text string, images []llm.Image) {
	if m.working {
		return
	}
	m.working = true
	m.workingSince = time.Now()
	m.workingVerb = workingVerbs[rand.Intn(len(workingVerbs))]

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := m.opt.Orch.SendImages(ctx, text, images)

	go func() {
		for ev := range ch {
			if m.program != nil {
				m.program.Send(orchestratorMsg(ev))
			}
		}
	}()
}

// cancelShell stops an in-flight "!command". The context's cancel
// reaches the whole process group (tools.Bash kills it), so a
// backgrounded build does not outlive the esc. The state is cleared
// here rather than waiting for the reply: the command is dead from
// this moment, and leaving a status line up for a process that is gone
// reads as a hang.
func (m *Model) cancelShell() {
	if m.shell == nil {
		return
	}
	m.shell.stop()
	m.shell = nil
	m.add(entry{kind: entryErr, text: "shell command interrupted — the command was killed, not the shell"})
}

// stop cancels the command's context and closes done exactly once.
// Both the interrupt and the exit path go through it, so "the command
// was killed" is one fact rather than two code paths that have to
// agree.
func (r *shellRun) stop() {
	r.once.Do(func() {
		r.cancel()
		close(r.done)
	})
}

func (m *Model) cancelTurn() {
	// A pending prompt blocks the orchestrator's goroutine on its
	// reply channel. Answer it before tearing down so quitting
	// cannot wedge the dispatch loop (audit C5). Both channels are
	// buffered, so a user who answered a moment earlier keeps their
	// verdict — the select simply finds them full.
	if req := m.awaitingPerm; req != nil {
		select {
		case req.reply <- false:
		default:
		}
		m.awaitingPerm = nil
	}
	if req := m.awaitingPlan; req != nil {
		select {
		case req.reply <- planVerdict{}:
		default:
		}
		m.awaitingPlan = nil
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

// add appends a transcript entry.
func (m *Model) add(e entry) {
	m.entries = append(m.entries, e)
}

// showToast sets the transient message above the composer.
func (m *Model) showToast(text string) {
	m.toast = text
	m.toastAt = time.Now()
	m.toastAnim = 0
}

// showAnimatedToast sets the message and plays the glyph burst; the
// returned Cmd drives the frames. Reduced motion skips the burst —
// the toast appears, it just does not dance.
func (m *Model) showAnimatedToast(text string) tea.Cmd {
	m.toast = text
	m.toastAt = time.Now()
	if m.opt.Animations {
		m.toastAnim = len(toastFrames)
		return toastTick()
	}
	m.toastAnim = 0
	return nil
}

// TodosChanged returns the callback tools.TodoList notifies through;
// it delivers a copy of the list to the tea program.
func (m *Model) TodosChanged() func(items []tools.Todo) {
	return func(items []tools.Todo) {
		if m.program != nil {
			m.program.Send(todoMsg(items))
		}
	}
}

// SubagentSink returns the function the subagent wiring uses to report
// progress. Events before the program starts are dropped.
func (m *Model) SubagentSink() func(title, kind, text string, usage llm.Usage) {
	return func(title, kind, text string, usage llm.Usage) {
		if m.program != nil {
			m.program.Send(subagentMsg(subagentEvent{
				Title: title, Kind: kind, Text: text, Usage: usage,
			}))
		}
	}
}

// statusTick schedules the next working-status tick.
func statusTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return statusTickMsg(t)
	})
}
