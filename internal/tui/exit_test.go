package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Chmgx81/opcode/internal/llm"
)

func ctrlCKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlC} }
func ctrlDKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlD} }

// TestCtrlCDoublePressExits: Codex's exit posture — the first
// ctrl+c must not quit (it interrupts a running turn and hints);
// only a second press inside the window exits.
func TestCtrlCDoublePressExits(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.working = true

	// First press: interrupt + hint, no quit.
	_, cmd := m.Update(ctrlCKey())
	if cmd != nil {
		t.Error("first ctrl+c must not quit")
	}
	if m.toast != "interrupted — ctrl+c again to exit" {
		t.Errorf("first ctrl+c toast = %q", m.toast)
	}

	// Second press inside the window: quit.
	_, cmd = m.Update(ctrlCKey())
	if cmd == nil {
		t.Error("second ctrl+c inside the window must quit")
	}
}

// TestCtrlCHintWhenIdle: an idle first press just arms the exit.
func TestCtrlCHintWhenIdle(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	_, cmd := m.Update(ctrlCKey())
	if cmd != nil {
		t.Error("idle first ctrl+c must not quit")
	}
	if m.toast != "ctrl+c again to exit" {
		t.Errorf("idle first ctrl+c toast = %q", m.toast)
	}

	_, cmd = m.Update(ctrlCKey())
	if cmd == nil {
		t.Error("second ctrl+c must quit")
	}
}

// TestCtrlDIsTheSameDoublePress: ctrl+d arms and exits like
// ctrl+c — Codex parity.
func TestCtrlDIsTheSameDoublePress(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)

	if _, cmd := m.Update(ctrlDKey()); cmd != nil {
		t.Error("first ctrl+d must not quit")
	}
	if _, cmd := m.Update(ctrlDKey()); cmd == nil {
		t.Error("second ctrl+d must quit")
	}
}

// TestCtrlCStopsAnInFlightShellCommand: a running "!command" is work in
// flight, so the first ctrl+c stops it and hints, exactly like a turn.
// Without it, quitting during a long command left the process group
// running and the program waiting on it.
func TestCtrlCStopsAnInFlightShellCommand(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.composer.SetValue("! sleep 60")
	if _, cmd := m.decideInput(false); cmd == nil {
		t.Fatal("the shell escape returned no command")
	}
	if m.shell == nil {
		t.Fatal("no in-flight shell run")
	}
	_, cmd := m.Update(ctrlCKey())
	if cmd != nil {
		t.Error("first ctrl+c must not quit")
	}
	if m.shell != nil {
		t.Error("ctrl+c did not stop the running command")
	}
	if m.toast != "interrupted — ctrl+c again to exit" {
		t.Errorf("toast = %q, want the interrupt hint", m.toast)
	}
	// The second press inside the window still exits.
	if _, cmd := m.Update(ctrlCKey()); cmd == nil {
		t.Error("second ctrl+c must quit")
	}
}

// TestCtrlCExitDoesNotStrandTheCommand: the exit path quits the
// program while a command is still out. It has to stop the command on
// its way, or quitting mid-command leaves the process group running
// with nobody left to reap it.
func TestCtrlCExitDoesNotStrandTheCommand(t *testing.T) {
	dir := t.TempDir()
	m, _ := newText(t, dir, nil)
	m.composer.SetValue("! sleep 60")
	if _, cmd := m.decideInput(false); cmd == nil {
		t.Fatal("the shell escape returned no command")
	}
	// The exit path: the window is already armed, so one press quits.
	m.quitArmedAt = time.Now()
	run := m.shell
	if run == nil {
		t.Fatal("no in-flight shell run")
	}
	if _, cmd := m.Update(ctrlCKey()); cmd == nil {
		t.Fatal("an armed ctrl+c must quit")
	}
	// The context the command is running under must be cancelled, and
	// the run must not still be tracked.
	if m.shell != nil {
		t.Error("quitting left the shell run tracked")
	}
	select {
	case <-run.done:
	case <-time.After(5 * time.Second):
		t.Error("the command's context was never cancelled on the exit path")
	}
}

// TestEscClosesExactlyOneThing: esc is the universal "get out of what I
// am in", and it must close ONE thing. A user pressing it twice expects
// two closed layers, not one closed layer and a swallowed key.
func TestEscClosesExactlyOneThing(t *testing.T) {
	dir := t.TempDir()
	open := map[string]func(*Model){
		"palette": func(m *Model) { m.composer.SetValue("/mo") },
		"help":    func(m *Model) { m.openHelp() },
		"login":   func(m *Model) { m.beginLogin("openrouter") },
		"picker":  func(m *Model) { m.picker = newPicker(pickerModels, "switch model", nil) },
		"@ mention": func(m *Model) {
			// The menu fills from files under the working directory,
			// so it needs one to have anything to show.
			if err := os.WriteFile(filepath.Join(m.opt.Cwd, "note.txt"),
				[]byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			m.composer.SetValue("see @not")
			m.refreshAtMenu()
		},
		"transcript": func(m *Model) { m.transcriptOpen = true },
		"permission": func(m *Model) { m.awaitingPerm = newPermReq("bash", `{"command":"ls"}`) },
	}
	for name, set := range open {
		m, _ := newText(t, dir, nil)
		set(m)
		// The dialog consumes itself when it answers, so its verdict
		// channel is held here rather than read back off the model.
		req := m.awaitingPerm
		// The layer is up. Each one is checked by its own postcondition
		// below; this catches a layer that never opened at all, which
		// would make the rest of the case vacuous.
		switch name {
		case "palette":
			if !m.paletteOpen() {
				t.Fatalf("%s: the layer never opened", name)
			}
		case "login":
			if m.login == nil {
				t.Fatalf("%s: the layer never opened", name)
			}
		case "picker":
			if m.picker == nil {
				t.Fatalf("%s: the layer never opened", name)
			}
		case "help":
			if !m.helpOpen {
				t.Fatalf("%s: the layer never opened", name)
			}
		case "transcript":
			if !m.transcriptOpen {
				t.Fatalf("%s: the layer never opened", name)
			}
		case "permission":
			if req == nil {
				t.Fatalf("%s: the layer never opened", name)
			}
			// Past the type-ahead guard, or esc is swallowed by design.
			req.openedAt = time.Now().Add(-time.Second)
		case "@ mention":
			if !m.atMenuOpen() {
				t.Fatalf("%s: the layer never opened", name)
			}
		}
		m.Update(escKey())
		switch name {
		case "palette":
			if m.paletteOpen() {
				t.Errorf("esc left the palette open")
			}
		case "login":
			if m.login != nil {
				t.Errorf("esc left the login prompt open")
			}
		case "picker":
			if m.picker != nil {
				t.Errorf("esc left the picker open")
			}
		case "help":
			if m.helpOpen {
				t.Errorf("esc left the help sheet open")
			}
		case "transcript":
			if m.transcriptOpen {
				t.Errorf("esc left the transcript pager open")
			}
		case "permission":
			// The dialog consumed itself; the verdict is on its channel.
			select {
			case v := <-req.reply:
				if v {
					t.Error("esc allowed the action instead of denying it")
				}
			default:
				t.Error("esc did not answer the permission dialog")
			}
		case "@ mention":
			if m.atMenuOpen() {
				t.Errorf("esc left the mention menu open")
			}
		}
	}
	// A third esc on a closed frame must not touch the draft: esc is
	// "close what is open", and with nothing open it does nothing.
	m, _ := newText(t, dir, nil)
	m.composer.SetValue("a draft that matters")
	m.Update(escKey())
	if v := m.composer.Value(); v != "a draft that matters" {
		t.Errorf("esc ate the draft with nothing open: %q", v)
	}
}

// TestQuitKeysWorkInEveryLayer: no overlay may swallow ctrl+c — a
// quit key that a picker, a dialog, or the pager eats silently is a
// trap, and the README's "press twice to exit" promise must hold
// everywhere. The first press arms and announces; the second quits.
func TestQuitKeysWorkInEveryLayer(t *testing.T) {
	for _, layer := range []struct {
		name   string
		opener func(m *Model)
	}{
		{"picker", func(m *Model) {
			m.picker = newPicker(pickerThemes, "theme",
				[]pickerItem{{Label: "dark"}})
		}},
		{"pager", func(m *Model) { m.transcriptOpen = true }},
		{"help", func(m *Model) { m.helpOpen = true }},
		{"permission", func(m *Model) {
			m.awaitingPerm = &permRequest{openedAt: time.Now().Add(-time.Second)}
		}},
	} {
		t.Run(layer.name, func(t *testing.T) {
			dir := t.TempDir()
			m, _ := newText(t, dir, [][]llm.ChatEvent{})
			layer.opener(m)

			// First press: armed, announced, still in the layer.
			model, _ := m.Update(keyMsg("ctrl+c"))
			m2 := model.(*Model)
			if m2.quitArmedAt.IsZero() {
				t.Error("the layer swallowed ctrl+c — the exit never armed")
			}
			if m2.toast == "" {
				t.Error("the armed quit said nothing — silent no-op")
			}

			// Second press inside the window: quits from inside the layer.
			model, cmd := m2.Update(keyMsg("ctrl+c"))
			if model.(*Model) == m2 && cmd == nil {
				t.Fatal("the second ctrl+c did not quit")
			}
			if cmd == nil {
				t.Fatal("quit returned no command")
			}
		})
	}
}
