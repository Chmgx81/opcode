package tools

import (
	"context"
	"strings"
	"testing"
)

func TestTodoWriteValidatesAndReplaces(t *testing.T) {
	list := &TodoList{}
	var notified []Todo
	list.OnChange = func(items []Todo) { notified = items }
	tw := TodoWrite{List: list}

	if tier := (TodoWrite{}).Tier(); tier != TierDraftOnly {
		t.Errorf("todo_write tier = %v, want draft-only", tier)
	}

	out, err := tw.Execute(context.Background(),
		`{"todos":[{"content":"read the config","status":"in_progress"},{"content":"write tests","status":"pending"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2 total") || !strings.Contains(out, "1 in progress") {
		t.Errorf("summary = %q", out)
	}
	if len(notified) != 2 || notified[0].Content != "read the config" {
		t.Errorf("OnChange = %v", notified)
	}

	// Full replacement: the second call's list wins entirely.
	_, err = tw.Execute(context.Background(),
		`{"todos":[{"content":"read the config","status":"done"},{"content":"write tests","status":"in_progress"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	items := list.Items()
	if len(items) != 2 || items[0].Status != TodoDone || items[1].Status != TodoInProgress {
		t.Errorf("replacement = %v", items)
	}

	// The notification is a copy: mutating it must not corrupt state.
	notified[0].Status = "bogus"
	if list.Items()[0].Status != TodoDone {
		t.Error("OnChange received the shared slice, not a copy")
	}

	for bad, want := range map[string]string{
		`{"todos":[]}`: "empty",
		`{"todos":[{"content":"x","status":"maybe"}]}`: "unknown status",
		`{"todos":[{"content":"","status":"done"}]}`:   "empty content",
		`{}`: "todos",
	} {
		if _, err := tw.Execute(context.Background(), bad); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("args %s: err = %v, want containing %q", bad, err, want)
		}
	}

	// The length bound.
	var big strings.Builder
	big.WriteString(`{"todos":[`)
	for i := 0; i < maxTodos+1; i++ {
		if i > 0 {
			big.WriteString(",")
		}
		big.WriteString(`{"content":"x","status":"pending"}`)
	}
	big.WriteString("]}")
	if _, err := tw.Execute(context.Background(), big.String()); err == nil || !strings.Contains(err.Error(), "bound") {
		t.Errorf("oversized list: err = %v, want the bound error", err)
	}

	// Unwired state fails loudly.
	if _, err := (TodoWrite{}).Execute(context.Background(), `{"todos":[{"content":"x","status":"done"}]}`); err == nil {
		t.Error("nil List must error")
	}
}
