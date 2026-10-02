package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Chmgx81/opcode/internal/tools"
)

// The whole frame, every state, every width a terminal can be. This is
// the invariant the package exists to keep: no row wider than the
// terminal, no frame taller than it. It is a slow, dumb sweep on
// purpose — a per-renderer assertion misses whichever renderer nobody
// thought about, and this is the one that notices.

// narrowPoison is a hostile payload that has already crossed the
// display boundary, so what is under test here is layout, not
// sanitization: a fourteen-character unbreakable word with no spaces in
// it, which is what every wrap path has to cope with.
const narrowPoison = "pre]0;pwnedapostend" // sanitized escape residue

// TestFrameFitsEveryStateAtEveryWidth: no row wider than the terminal,
// no frame taller, for every state and every width from eight columns
// up. The states list is the point: a renderer nobody thought about is
// exactly the one that overflows.
func TestFrameFitsEveryStateAtEveryWidth(t *testing.T) {
	dir := t.TempDir()
	states := narrowStateSet(t, dir, narrowPoison)
	for name, set := range states {
		for w := 8; w <= 120; w++ {
			for _, h := range []int{8, 12, 20, 40} {
				m, _ := newText(t, dir, nil)
				m.entries = nil
				// A transcript with every entry kind in it, so the
				// timeline is exercised in all of the states above.
				m.add(entry{kind: entryUser, text: narrowPoison})
				m.add(entry{kind: entryTool, tool: "read_file", text: narrowPoison})
				m.add(entry{kind: entryResult, tool: "read_file",
					summary: narrowPoison, full: narrowPoison})
				m.add(entry{kind: entryAssistant, text: "## " + narrowPoison + "\n\n- " + narrowPoison})
				set(m)
				resize(m, w, h)
				frame := m.View()
				rows := strings.Split(frame, "\n")
				if len(rows) > h {
					t.Errorf("%s at %dx%d: the frame is %d rows", name, w, h, len(rows))
				}
				for _, l := range rows {
					if n := lipgloss.Width(l); n > w {
						t.Errorf("%s at %dx%d: a row is %d wide: %q",
							name, w, h, n, clipForLog(l))
					}
				}
			}
		}
	}
}

func clipForLog(s string) string {
	if len(s) > 70 {
		return s[:70]
	}
	return s
}

// TestFrameFitsHostileTextAtEveryWidth: the same sweep with a payload
// that has NOT been through the sanitizer, so a path that forgets to
// sanitize shows up as a width failure here too. The assert is on the
// frame's shape; the byte-level check lives in hostile_test.go.
func TestFrameFitsHostileTextAtEveryWidth(t *testing.T) {
	dir := t.TempDir()
	raw := "pre\x1b]0;pwned\x07post\x1b[2Jend\x1b"
	for name, set := range narrowStateSet(t, dir, raw) {
		for _, w := range []int{8, 14, 20, 40, 80} {
			m, _ := newText(t, dir, nil)
			m.entries = nil
			m.add(entry{kind: entryUser, text: raw})
			m.add(entry{kind: entryResult, tool: "read_file", summary: raw, full: raw})
			set(m)
			resize(m, w, 30)
			for _, l := range strings.Split(m.View(), "\n") {
				if n := lipgloss.Width(l); n > w {
					t.Errorf("%s at width %d: a row is %d wide: %q",
						name, w, n, clipForLog(l))
				}
			}
		}
	}
}

// narrowStateSet is the states list, built once. Kept apart from the
// test so both sweeps above share exactly one list — a second copy is a
// list that drifts.
func narrowStateSet(t *testing.T, dir, poison string) map[string]func(*Model) {
	t.Helper()
	_ = dir
	return map[string]func(*Model){
		"idle": func(m *Model) {},
		"working": func(m *Model) {
			m.working = true
			m.workingSince = time.Now()
			m.usage.PromptTokens, m.usage.CompletionTokens = 999999, 42
			m.workingVerb = "Marinating…"
		},
		"shell running": func(m *Model) { m.shell = &shellRun{name: poison} },
		"todos": func(m *Model) {
			m.todos = []tools.Todo{
				{Content: poison, Status: tools.TodoDone},
				{Content: poison, Status: tools.TodoInProgress},
				{Content: poison, Status: tools.TodoPending},
			}
		},
		"live thinking": func(m *Model) { m.reasoning.WriteString(poison) },
		"live stream":   func(m *Model) { m.stream.WriteString(poison) },
		"trust": func(m *Model) {
			m.awaitingTrust = &TrustDecision{Approved: []string{poison}}
		},
		"plan": func(m *Model) {
			m.awaitingPlan = &planRequest{plan: poison}
			m.entries = append(m.entries, entry{kind: entryPlan, text: poison})
		},
		"permission": func(m *Model) {
			m.awaitingPerm = newPermReq("bash", `{"command":`+mustJSON(poison)+`}`)
		},
		"login":   func(m *Model) { m.login = &loginFlow{provider: poison} },
		"pager":   func(m *Model) { m.transcriptOpen = true },
		"help":    func(m *Model) { m.openHelp(); m.helpTop = 3 },
		"palette": func(m *Model) { m.composer.SetValue("/" + poison) },
		"toast":   func(m *Model) { m.showToast(poison) },
		"queued": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entryQueued, text: poison})
		},
		"steering": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entrySteer, text: poison})
		},
		"ok note": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entryOK, text: poison})
		},
		"error note": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entryErr, text: poison})
		},
		"subagent": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entrySubagent,
				subTitle: "[" + poison + "] ", text: poison})
		},
		"compaction": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entryCompaction, text: poison})
		},
		"reasoning receipt": func(m *Model) {
			m.entries = append(m.entries,
				entry{kind: entryReasoning, dur: "2s", text: poison})
		},
		"expanded results": func(m *Model) {
			m.expandResults = true
			m.entries = append(m.entries,
				entry{kind: entryResult, tool: "read_file", summary: poison, full: poison},
				entry{kind: entryResult, tool: "edit_file", path: "a.go", old: poison, new: poison},
				entry{kind: entryResult, tool: "write_file", path: "a.go", full: poison})
		},
		"/diff": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entryDiff,
				text: strings.Join(renderDiffBody(
					"diff --git a/"+poison+" b/"+poison+"\n+"+poison+"\n-"+poison), "\n")})
		},
		"markdown": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entryAssistant,
				text: "## " + poison + "\n\n- " + poison + "\n\n> " + poison +
					"\n\n| " + poison + " |\n|---|\n| " + poison + " |\n"})
		},
		"shell echo": func(m *Model) {
			m.entries = append(m.entries, entry{kind: entryUser, text: "! " + poison})
		},
		"picker": func(m *Model) {
			m.picker = newPicker(pickerSessions, poison,
				[]pickerItem{{Label: poison, Detail: poison}})
		},
		"@ mention": func(m *Model) {
			m.composer.SetValue("@" + poison)
			m.atMenu = []string{poison}
		},
		"update badge": func(m *Model) { m.opt.UpdateTag = "v9.9.9-rc.1-longtag" },
		"effort":       func(m *Model) { m.effort = "medium" },
	}
}
