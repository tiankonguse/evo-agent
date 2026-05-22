package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"

	"evo-agent/internal/ui"
)

const (
	todoMaxItems         = 12
	todoReminderInterval = 3 // rounds without update before reminder fires
)

// ── TodoManager ───────────────────────────────────────────────────────────────

// todoItem is the internal representation of a plan entry.
type todoItem struct {
	Content    string
	Status     string
	ActiveForm string
}

// todoManager is the singleton session-plan state.
type todoManager struct {
	mu                sync.RWMutex
	items             []todoItem
	roundsSinceUpdate int
}

// GlobalTodo is the process-wide session-plan manager.
// The todo tool writes to it; the agent loop reads it for reminders;
// the TUI reads it via Snapshot() for rendering.
var GlobalTodo = &todoManager{}

// Update replaces the plan, validates constraints, and returns a rendered summary.
func (t *todoManager) Update(items []todoItemInput) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(items) > todoMaxItems {
		return "", fmt.Errorf("keep the session plan short (max %d items)", todoMaxItems)
	}

	normalized := make([]todoItem, 0, len(items))
	inProgressCount := 0
	for i, raw := range items {
		content := strings.TrimSpace(raw.Content)
		status := strings.ToLower(strings.TrimSpace(raw.Status))
		if content == "" {
			return "", fmt.Errorf("item %d: content required", i)
		}
		switch status {
		case "pending", "in_progress", "completed":
		default:
			return "", fmt.Errorf("item %d: invalid status %q (must be pending/in_progress/completed)", i, status)
		}
		if status == "in_progress" {
			inProgressCount++
		}
		normalized = append(normalized, todoItem{
			Content:    content,
			Status:     status,
			ActiveForm: strings.TrimSpace(raw.ActiveForm),
		})
	}
	if inProgressCount > 1 {
		return "", fmt.Errorf("only one plan item can be in_progress at a time")
	}

	t.items = normalized
	t.roundsSinceUpdate = 0
	return t.render(), nil
}

// NoteRound is called once per agent turn.
// If usedTodo is true the counter resets; otherwise it increments.
func (t *todoManager) NoteRound(usedTodo bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if usedTodo {
		t.roundsSinceUpdate = 0
	} else {
		t.roundsSinceUpdate++
	}
}

// Reminder returns a non-empty string when the plan hasn't been refreshed
// for todoReminderInterval rounds, otherwise "".
func (t *todoManager) Reminder() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.items) == 0 || t.roundsSinceUpdate < todoReminderInterval {
		return ""
	}
	return "<reminder>Refresh your current plan before continuing.</reminder>"
}

// Snapshot returns a copy of the current items for safe concurrent read.
func (t *todoManager) Snapshot() []ui.TodoItem {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]ui.TodoItem, len(t.items))
	for i, item := range t.items {
		out[i] = ui.TodoItem{
			Content:    item.Content,
			Status:     item.Status,
			ActiveForm: item.ActiveForm,
		}
	}
	return out
}

// render produces a human-readable plan listing.
func (t *todoManager) render() string {
	if len(t.items) == 0 {
		return "No session plan yet."
	}
	markers := map[string]string{
		"pending":     "[ ]",
		"in_progress": "[>]",
		"completed":   "[x]",
	}
	lines := make([]string, 0, len(t.items)+1)
	completed := 0
	for _, item := range t.items {
		marker := markers[item.Status]
		line := marker + " " + item.Content
		if item.Status == "in_progress" && item.ActiveForm != "" {
			line += " (" + item.ActiveForm + ")"
		}
		lines = append(lines, line)
		if item.Status == "completed" {
			completed++
		}
	}
	lines = append(lines, fmt.Sprintf("(%d/%d completed)", completed, len(t.items)))
	return strings.Join(lines, "\n")
}

// ── Tool input schema ─────────────────────────────────────────────────────────

type todoItemInput struct {
	Content    string `json:"content"              jsonschema_description:"Task description"`
	Status     string `json:"status"               jsonschema_description:"Status: pending, in_progress, or completed"`
	ActiveForm string `json:"activeForm,omitempty" jsonschema_description:"Present-continuous label shown while in_progress (e.g. 'Writing tests')"`
}

type todoInput struct {
	Items []todoItemInput `json:"items" jsonschema_description:"The complete session plan (max 12 items). Exactly one item should be in_progress when work is underway."`
}

// ── Tool registration ─────────────────────────────────────────────────────────

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "todo",
			Description: anthropic.String(
				"Rewrite the current session plan for multi-step work. " +
					"Keep exactly one step in_progress at a time. " +
					"Refresh the plan as work advances. " +
					"Prefer this tool over prose when a task has 2+ steps.",
			),
			InputSchema: GenerateSchema[todoInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in todoInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			result, err := GlobalTodo.Update(in.Items)
			if err != nil {
				return "", err
			}
			// Notify TUI of updated plan
			ui.EmitTodo(GlobalTodo.Snapshot())
			return result, nil
		},
	})
}
