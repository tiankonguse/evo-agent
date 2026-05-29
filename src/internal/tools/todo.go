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

// todoItem is the internal representation of a memory plan entry.
type todoItem struct {
	ID         int
	Content    string
	Status     string // pending, in_progress, completed
	ActiveForm string
}

// todoManager is the singleton memory-plan state.
type todoManager struct {
	mu                sync.RWMutex
	topic             string // plan topic/goal
	items             []todoItem
	nextID            int
	roundsSinceUpdate int
}

// GlobalTodo is the process-wide memory-plan manager.
// The todo_* tools write to it; the agent loop reads it for reminders;
// the TUI reads it via Snapshot() for rendering.
var GlobalTodo = &todoManager{nextID: 1}

// todoToolNames holds the registered todo tool names for precise identification.
var todoToolNames = map[string]bool{}

// ── CRUD operations ─────────────────────────────────────────────────────────

// Init initializes a new memory plan with a topic. Resets all items.
func (t *todoManager) Init(topic string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	topic = strings.TrimSpace(topic)
	if topic == "" {
		return "", fmt.Errorf("topic is required")
	}
	t.topic = topic
	t.items = nil
	t.nextID = 1
	t.roundsSinceUpdate = 0
	return fmt.Sprintf("Memory plan initialized: %s", topic), nil
}

// Create adds a new item to the memory plan.
func (t *todoManager) Create(content, activeForm string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.items) >= todoMaxItems {
		return "", fmt.Errorf("memory plan is full (max %d items)", todoMaxItems)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	item := todoItem{
		ID:         t.nextID,
		Content:    content,
		Status:     "pending",
		ActiveForm: strings.TrimSpace(activeForm),
	}
	t.items = append(t.items, item)
	t.nextID++
	t.roundsSinceUpdate = 0
	return t.renderItem(&item), nil
}

// Get returns details of a single item by ID.
func (t *todoManager) Get(id int) (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for i := range t.items {
		if t.items[i].ID == id {
			return t.renderItem(&t.items[i]), nil
		}
	}
	return "", fmt.Errorf("todo item #%d not found", id)
}

// Update changes the status, content, or activeForm of an item.
func (t *todoManager) Update(id int, status, content, activeForm string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var item *todoItem
	for i := range t.items {
		if t.items[i].ID == id {
			item = &t.items[i]
			break
		}
	}
	if item == nil {
		return "", fmt.Errorf("todo item #%d not found", id)
	}

	if status != "" {
		switch status {
		case "pending", "in_progress", "completed":
		default:
			return "", fmt.Errorf("invalid status %q (must be pending/in_progress/completed)", status)
		}
		// Enforce single in_progress
		if status == "in_progress" {
			for i := range t.items {
				if t.items[i].ID != id && t.items[i].Status == "in_progress" {
					return "", fmt.Errorf("item #%d is already in_progress — complete it first", t.items[i].ID)
				}
			}
		}
		item.Status = status
	}
	if content != "" {
		item.Content = strings.TrimSpace(content)
	}
	if activeForm != "" {
		item.ActiveForm = strings.TrimSpace(activeForm)
	}

	t.roundsSinceUpdate = 0
	return t.renderItem(item), nil
}

// Complete marks an item as completed.
func (t *todoManager) Complete(id int) (string, error) {
	return t.Update(id, "completed", "", "")
}

// List returns a rendered summary of all items.
func (t *todoManager) List() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.render()
}

// ── Reminder logic ──────────────────────────────────────────────────────────

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
	return "<reminder>Update your memory plan (todo_update/todo_complete) before continuing.</reminder>"
}

// CheckTodoUsed scans content blocks and returns true if any todo tool was called.
func CheckTodoUsed(blocks []anthropic.ContentBlockUnion) bool {
	for _, block := range blocks {
		if block.Type == "tool_use" && todoToolNames[block.Name] {
			return true
		}
	}
	return false
}

// ── TUI integration ─────────────────────────────────────────────────────────

// Snapshot returns a copy of the current items for safe concurrent read.
func (t *todoManager) Snapshot() []ui.TodoItem {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]ui.TodoItem, len(t.items))
	for i, item := range t.items {
		out[i] = ui.TodoItem{
			ID:         item.ID,
			Content:    item.Content,
			Status:     item.Status,
			ActiveForm: item.ActiveForm,
		}
	}
	return out
}

// Topic returns the current memory plan topic.
func (t *todoManager) Topic() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.topic
}

// ── Render helpers ──────────────────────────────────────────────────────────

func (t *todoManager) renderItem(item *todoItem) string {
	marker := map[string]string{
		"pending":     "[ ]",
		"in_progress": "[>]",
		"completed":   "[x]",
	}[item.Status]
	line := fmt.Sprintf("%s #%d: %s", marker, item.ID, item.Content)
	if item.Status == "in_progress" && item.ActiveForm != "" {
		line += " (" + item.ActiveForm + ")"
	}
	return line
}

func (t *todoManager) render() string {
	if len(t.items) == 0 {
		return "No memory plan yet."
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
		line := fmt.Sprintf("%s #%d: %s", marker, item.ID, item.Content)
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

// ── Tool input schemas ──────────────────────────────────────────────────────

type todoInitInput struct {
	Topic string `json:"topic" jsonschema_description:"The goal/topic of this memory plan (e.g. 'Implement health check endpoint')"`
}

type todoCreateInput struct {
	Content    string `json:"content"              jsonschema_description:"Task description"`
	ActiveForm string `json:"activeForm,omitempty" jsonschema_description:"Present-continuous label shown while in_progress (e.g. 'Writing tests')"`
}

type todoListInput struct{}

type todoGetInput struct {
	ID int `json:"id" jsonschema_description:"Task ID"`
}

type todoUpdateInput struct {
	ID         int    `json:"id"                   jsonschema_description:"Task ID"`
	Status     string `json:"status,omitempty"     jsonschema_description:"New status: pending, in_progress, or completed"`
	Content    string `json:"content,omitempty"    jsonschema_description:"New task description"`
	ActiveForm string `json:"activeForm,omitempty" jsonschema_description:"New active form label"`
}

type todoCompleteInput struct {
	ID int `json:"id" jsonschema_description:"Task ID to mark completed"`
}

// ── Tool registration ───────────────────────────────────────────────────────

func init() {
	// Collect todo tool names for precise identification via CheckTodoUsed()
	names := []string{"todo_init", "todo_create", "todo_list", "todo_get", "todo_update", "todo_complete"}
	for _, n := range names {
		todoToolNames[n] = true
	}

	// todo_init: Initialize a memory plan with a topic
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "todo_init",
			Description: anthropic.String(
				"Initialize a new memory plan with a topic/goal. " +
					"Resets all existing items. Must be called before todo_create."),
			InputSchema: GenerateSchema[todoInitInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in todoInitInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			result, err := GlobalTodo.Init(in.Topic)
			if err != nil {
				return "", err
			}
			ui.EmitTodo(GlobalTodo.Snapshot(), GlobalTodo.Topic())
			return result, nil
		},
	})

	// todo_create: Create a memory plan item
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "todo_create",
			Description: anthropic.String(
				"Add a new item to the memory plan. Use for multi-step work (2+ steps). " +
					"Each item starts as pending. Mark in_progress when you begin working on it."),
			InputSchema: GenerateSchema[todoCreateInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in todoCreateInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			result, err := GlobalTodo.Create(in.Content, in.ActiveForm)
			if err != nil {
				return "", err
			}
			ui.EmitTodo(GlobalTodo.Snapshot(), GlobalTodo.Topic())
			return result, nil
		},
	})

	// todo_list: List all memory plan items
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "todo_list",
			Description: anthropic.String(
				"List all items in the memory plan with their status and IDs."),
			InputSchema: GenerateSchema[todoListInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			return GlobalTodo.List(), nil
		},
	})

	// todo_get: Get details of one item
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "todo_get",
			Description: anthropic.String(
				"Get details of a specific memory plan item by ID."),
			InputSchema: GenerateSchema[todoGetInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in todoGetInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			return GlobalTodo.Get(in.ID)
		},
	})

	// todo_update: Update item status/content
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "todo_update",
			Description: anthropic.String(
				"Update a memory plan item's status, content, or active form. " +
					"Only one item can be in_progress at a time."),
			InputSchema: GenerateSchema[todoUpdateInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in todoUpdateInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			result, err := GlobalTodo.Update(in.ID, in.Status, in.Content, in.ActiveForm)
			if err != nil {
				return "", err
			}
			ui.EmitTodo(GlobalTodo.Snapshot(), GlobalTodo.Topic())
			return result, nil
		},
	})

	// todo_complete: Mark item completed
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "todo_complete",
			Description: anthropic.String(
				"Mark a memory plan item as completed. Use as soon as you finish a step."),
			InputSchema: GenerateSchema[todoCompleteInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in todoCompleteInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			result, err := GlobalTodo.Complete(in.ID)
			if err != nil {
				return "", err
			}
			ui.EmitTodo(GlobalTodo.Snapshot(), GlobalTodo.Topic())
			return result, nil
		},
	})
}
