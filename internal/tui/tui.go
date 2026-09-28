// Package tui is tilde's Bubble Tea front end: a streaming transcript
// with a collapsible tool timeline, a multiline composer with paste
// collapsing and a command palette, mode cycling, and headless-friendly
// wiring — the orchestrator stays UI-independent.
package tui

import (
	"context"
	_ "embed"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/Chmgx81/tilde/internal/config"
	"github.com/Chmgx81/tilde/internal/llm"
	"github.com/Chmgx81/tilde/internal/orchestrator"
	"github.com/Chmgx81/tilde/internal/skills"
	"github.com/Chmgx81/tilde/internal/tools"
)

// version tracks the architecture doc revision.
const version = "v0.2"

// The tilde logo, shown at the top of a fresh session; it scrolls away
// with the transcript.
//
//go:embed banner.txt
var banner string

// Options wires the TUI to the rest of the system. Everything here is
// injected; the package never loads config itself.
type Options struct {
	Orch      *orchestrator.Orchestrator
	Model     string
	Mode      string
	Cwd       string
	TildeHome string
	// ProviderName + BaseURL let /login rebuild the LLM client with the
	// new key instead of needing a restart.
	ProviderName string
	BaseURL      string
	AuditPath    string

	// Skills and MCP managers back the /skills and /mcp commands.
	Skills   *skills.Manager
	MCPNames func() []string // connected server names + tool counts

	// Models backs the /model picker. SwitchModel rebuilds the provider
	// and the model everywhere they live (orchestrator, subagent
	// runner, audit redactor); the TUI stays free of wiring.
	Models      config.ModelsConfig
	SwitchModel func(provider, model string) error
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
}

// permRequest is one pending permission decision.
type permRequest struct {
	tool  string
	tier  tools.Tier
	args  string
	reply chan bool
}

type permRequestMsg struct{ req *permRequest }

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

	// Markdown cache (entryAssistant): rendered once per width so View
	// doesn't re-run glamour on every frame.
	rendered  []string
	renderedW int
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

	awaitingTrust *TrustDecision
	awaitingPerm  *permRequest
	allowAll      bool

	queue []string // follow-ups (Alt+Enter while working)

	usage llm.Usage

	// entries is the structured transcript; View renders it.
	entries []entry
	stream  strings.Builder
	login   *loginFlow

	// expandResults toggles ctrl+r result expansion.
	expandResults bool

	// Composer plumbing: large pastes collapse to tokens in the
	// composer and re-expand on submit.
	pastes  []string
	pasteAt map[string]string // token -> content

	// Command palette: open when the composer starts with "/".
	paletteIdx int

	// Overlay picker (/model, /sessions): filter-as-you-type list.
	picker      *picker
	pendingPick *pickerItem // selected item awaiting its command's action

	// help overlay ("?").
	helpOpen bool

	// toast is a transient status message above the composer.
	toast   string
	toastAt time.Time

	// Subagent progress accumulation, flushed at boundaries.
	subMu      sync.Mutex
	subStreams map[string]*strings.Builder
}

// Commands is the palette's source of truth; the desc renders in the
// picker, the action runs on Enter.
type command struct {
	Name string
	Desc string
}

var commands = []command{
	{"/exit", "quit tilde"},
	{"/help", "show keys and commands"},
	{"/mode", "show or switch permission mode"},
	{"/model", "pick or switch the model"},
	{"/sessions", "browse and resume a saved session"},
	{"/skills", "list available skills"},
	{"/mcp", "list MCP servers and tools"},
	{"/login", "store an API key (masked)"},
	{"/logout", "remove the stored key"},
}

func New(opt Options) *Model {
	ta := textarea.New()
	ta.Placeholder = "ask tilde anything…"
	// The placeholder must read as a hint, not as typed text: dim it
	// with the design token, not the textarea's brighter default.
	ta.FocusedStyle.Placeholder = dimStyle
	ta.BlurredStyle.Placeholder = dimStyle
	ta.Prompt = GlyphPrompt + " "
	ta.CharLimit = 0
	// One line when empty, Claude-Code-style: the composer grows with
	// typed content (resizeComposer) instead of reserving rows that
	// render as empty prompt lines. Hints live on the footer line, not
	// in the placeholder.
	ta.SetHeight(1)
	// The textarea's own width defaults to 40 columns — placeholder and
	// typed text wrap there no matter how wide the terminal is. Size it
	// to the composer box's content width (terminal minus box chrome);
	// the first WindowSizeMsg corrects it for the real terminal.
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
	}
	// Identity block: logo, name and version, model and mode, working
	// directory, then startup notes — stacked lines that scroll away.
	for _, line := range strings.Split(strings.TrimRight(banner, "\n"), "\n") {
		m.entries = append(m.entries, entry{kind: entryDim, text: accentStyle.Render(line)})
	}
	m.entries = append(m.entries, entry{kind: entryDim, text: ""})
	m.entries = append(m.entries, entry{kind: entryDim, text: boldStyle.Render("tilde " + version)})
	m.entries = append(m.entries, entry{kind: entryDim, text: dimStyle.Render(opt.Model + " · " + opt.Mode)})
	if opt.Cwd != "" {
		m.entries = append(m.entries, entry{kind: entryDim, text: dimStyle.Render(opt.Cwd)})
	}
	for _, note := range opt.StartupNotes {
		m.entries = append(m.entries, entry{kind: entryDim, text: dimStyle.Render(note)})
	}
	if len(opt.StartupNotes) > 0 {
		m.entries = append(m.entries, entry{kind: entryDim, text: ""})
	}
	m.awaitingTrust = opt.PendingTrust
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
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
// uses the query result.
func Run(m *Model) error {
	if lipgloss.ColorProfile() == termenv.Ascii {
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
	if m.allowAll {
		return true
	}
	req := &permRequest{
		tool:  tool.Name(),
		tier:  tool.Tier(),
		args:  args,
		reply: make(chan bool, 1),
	}
	m.program.Send(permRequestMsg{req})
	return <-req.reply
}

// startTurn sends a new user message and pumps the turn's events into
// the tea program until the channel closes.
func (m *Model) startTurn(text string) {
	if m.working {
		return
	}
	m.working = true
	m.workingSince = time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := m.opt.Orch.Send(ctx, text)

	go func() {
		for ev := range ch {
			if m.program != nil {
				m.program.Send(orchestratorMsg(ev))
			}
		}
	}()
}

func (m *Model) cancelTurn() {
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
