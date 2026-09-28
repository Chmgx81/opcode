package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Todo statuses.
const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoDone       = "done"
)

// maxTodos bounds a list that has stopped being a plan.
const maxTodos = 50

// Todo is one task in the model's list.
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | done
}

// TodoList is the shared, live task state. The registry tool mutates
// it; the UI layer observes through OnChange (which receives a copy —
// never the shared slice).
type TodoList struct {
	mu    sync.Mutex
	items []Todo

	// OnChange, when set, is called after every successful update.
	OnChange func(items []Todo)
}

// Items returns a copy of the current list.
func (t *TodoList) Items() []Todo {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Todo, len(t.items))
	copy(out, t.items)
	return out
}

// Set replaces the list and notifies. Returns the summary line the
// tool result reports.
func (t *TodoList) Set(items []Todo) string {
	t.mu.Lock()
	t.items = items
	t.mu.Unlock()
	if t.OnChange != nil {
		t.OnChange(t.Items())
	}
	done, doing := 0, 0
	for _, it := range items {
		switch it.Status {
		case TodoDone:
			done++
		case TodoInProgress:
			doing++
		}
	}
	return fmt.Sprintf("todos updated: %d total, %d done, %d in progress, %d pending",
		len(items), done, doing, len(items)-done-doing)
}

// TodoWrite is the model's task list tool. Draft-Only tier: updating
// the plan changes nothing. Full-replacement semantics — the model
// sends the complete list on every update, so there is nothing to
// merge, no IDs, and no stale state.
type TodoWrite struct {
	List *TodoList
}

func (TodoWrite) Name() string { return "todo_write" }

func (TodoWrite) Description() string {
	return "Update your task list for multi-step work. Send the COMPLETE list every time " +
		"(replacement, not a merge). Each item: {\"content\": string, \"status\": \"pending\"|\"in_progress\"|\"done\"}. " +
		"Use for any task with 3+ steps; keep exactly one item in_progress at a time; mark items done as you finish them."
}

func (TodoWrite) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"todos": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"content": {"type": "string"},
						"status": {"type": "string", "enum": ["pending", "in_progress", "done"]}
					},
					"required": ["content", "status"]
				}
			}
		},
		"required": ["todos"]
	}`)
}

// Tier implements Tool: Draft-Only — a plan, never an application.
func (TodoWrite) Tier() Tier { return TierDraftOnly }

func (t TodoWrite) Execute(ctx context.Context, args string) (string, error) {
	var a struct {
		Todos []Todo `json:"todos"`
	}
	if err := parseArgs(args, &a); err != nil {
		return "", err
	}
	if t.List == nil {
		return "", fmt.Errorf("todo_write: no task state wired")
	}
	if len(a.Todos) == 0 {
		return "", fmt.Errorf("todo_write: todos must not be empty — send the full list, or stop using the tool")
	}
	if len(a.Todos) > maxTodos {
		return "", fmt.Errorf("todo_write: %d items exceeds the %d-item bound — split the work", len(a.Todos), maxTodos)
	}
	for i, it := range a.Todos {
		if it.Content == "" {
			return "", fmt.Errorf("todo_write: item %d has empty content", i+1)
		}
		switch it.Status {
		case TodoPending, TodoInProgress, TodoDone:
		default:
			return "", fmt.Errorf("todo_write: item %d has unknown status %q (pending|in_progress|done)", i+1, it.Status)
		}
	}
	return t.List.Set(a.Todos), nil
}
