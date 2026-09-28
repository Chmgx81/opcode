// Package tui is tilde's Bubble Tea front end (Phase 1): streamed tokens
// rendered as they arrive, a status bar, permission prompts, steer vs.
// follow-up input handling, and /login, /logout.
//
// The TUI never reaches into the agent loop: it consumes the same
// orchestrator event channel the Phase 0 bare loop did, and talks back
// only through Send (new turn), Steer (mid-turn steering), and the gate's
// prompt callback.
package tui

import (
	"context"
	_ "embed"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"tilde/internal/llm"
	"tilde/internal/orchestrator"
	"tilde/internal/tools"
)

// version tracks the architecture doc revision.
const version = "v0.1"

// The tilde logo, shown at the top of a fresh session like the
// reference apps' welcome screens; it scrolls away with the transcript.
//
//go:embed banner.txt
var banner string

// Options wires the TUI to the rest of the system. Everything here is
// injected; the package never loads config itself.
type Options struct {
	Orch      *orchestrator.Orchestrator
	Model     string // model name, status bar
	Mode      string // permission mode name, status bar
	Cwd       string // working directory, status bar
	TildeHome string // user-level dir: /login, /logout write here
	// ProviderName + BaseURL let /login rebuild the LLM client with the
	// new key instead of needing a restart.
	ProviderName string
	BaseURL      string
	AuditPath    string // audit log path, to rebuild the redactor after /login

	// PendingTrust, when non-nil, shows the project-trust prompt at
	// startup: the project has an executable surface the user has not
	// approved (or it changed). OnAnswer persists the decision and
	// re-discovers skills; the TUI owns only the question.
	PendingTrust *TrustDecision

	// StartupNotes render in the greeting (MCP server status, config
	// warnings): dim lines that scroll away with the transcript.
	StartupNotes []string
}

// TrustDecision is one pending project-trust question. Approved lists
// the literal files that will become runnable, so the prompt can show
// exactly what the user is approving (Section 7).
type TrustDecision struct {
	ProjectDir string
	Approved   []string
	OnAnswer   func(trusted bool)
}

// permRequest is one pending permission decision. The gate's prompt
// callback blocks on reply; Update answers it from a keypress.
type permRequest struct {
	tool  string
	tier  tools.Tier
	args  string
	reply chan bool
}

// permRequestMsg delivers a permission request to the tea program.
type permRequestMsg struct{ req *permRequest }

// orchestratorMsg wraps one orchestrator event as a tea.Msg.
type orchestratorMsg orchestrator.Event

// loginFlow is active while /login captures a key with masked input.
type loginFlow struct{ provider string }

// subagentMsg wraps one subagent progress event as a tea.Msg.
type subagentMsg subagentEvent

// subagentEvent mirrors subagent.Event without importing the package
// into the TUI's hot types; the wiring converts.
type subagentEvent struct {
	Title string
	Kind  string
	Text  string
	Usage llm.Usage
}

// Model is the Bubble Tea model. Pointer receiver so the gate's prompt
// closure and the pump goroutine share one instance.
type Model struct {
	opt     Options
	program *tea.Program

	input   textinput.Model
	spinner spinner.Model
	width   int
	height  int

	working bool               // a turn is in flight
	cancel  context.CancelFunc // cancels the in-flight turn

	awaitingTrust *TrustDecision // non-nil while the trust prompt is up
	awaitingPerm  *permRequest   // non-nil while the permission prompt is shown
	allowAll      bool           // "a" answered once: skip action-tier prompts this session

	queue []string // follow-ups (Alt+Enter while working)

	usage  llm.Usage // cumulative token usage
	lines  []string  // finished transcript lines
	stream strings.Builder
	login  *loginFlow

	// Subagent progress: streaming text accumulates per title and
	// flushes at boundaries (tool call, done, error) so the log stays
	// readable instead of one line per delta.
	subMu      sync.Mutex
	subStreams map[string]*strings.Builder
}

func New(opt Options) *Model {
	input := textinput.New()
	input.Prompt = "~ "
	input.PromptStyle = accentStyle
	input.Placeholder = "type a message, /login, /logout, or /exit"
	input.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))

	m := &Model{
		opt:     opt,
		input:   input,
		spinner: sp,
	}
	// Greeting block, in the reference welcome screens' shape: logo,
	// app name and version, model and mode, working directory — stacked
	// lines that scroll away with the transcript.
	for _, line := range strings.Split(strings.TrimRight(banner, "\n"), "\n") {
		m.lines = append(m.lines, accentStyle.Render(line))
	}
	m.lines = append(m.lines, "")
	m.lines = append(m.lines, lipgloss.NewStyle().Bold(true).Render("tilde "+version))
	m.lines = append(m.lines, dimStyle.Render(opt.Model+" · "+opt.Mode))
	if opt.Cwd != "" {
		m.lines = append(m.lines, dimStyle.Render(opt.Cwd))
	}
	for _, note := range opt.StartupNotes {
		m.lines = append(m.lines, dimStyle.Render(note))
	}
	if len(opt.StartupNotes) > 0 {
		m.lines = append(m.lines, "")
	}
	m.awaitingTrust = opt.PendingTrust
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

// Run starts the program and blocks until it exits.
func Run(m *Model) error {
	p := tea.NewProgram(m)
	m.program = p
	_, err := p.Run()
	return err
}

// Prompt returns the callback the permission gate should call when a
// tool needs a decision. It blocks until the user answers, so it is only
// suitable from the orchestrator's goroutine during a turn.
func (m *Model) Prompt() func(tool tools.Tool, args string) bool {
	return m.decide
}

// decide is the gate's prompt callback, running on the orchestrator's
// goroutine. It shows the prompt and blocks until the user answers.
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

// startTurn sends a new user message and pumps the turn's events into the
// tea program until the channel closes.
func (m *Model) startTurn(text string) {
	if m.working {
		return
	}
	m.working = true

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := m.opt.Orch.Send(ctx, text)

	go func() {
		// program is nil before Run starts (and in tests that drive
		// Update directly); events are dropped then, never panic.
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

// finishStream flushes the accumulated assistant text into the
// transcript.
func (m *Model) finishStream() {
	if s := strings.TrimRight(m.stream.String(), "\n"); s != "" {
		m.lines = append(m.lines, s)
	}
	m.stream.Reset()
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
