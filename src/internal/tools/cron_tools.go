package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// cron_tools.go — three model-invocable tools backed by the CronScheduler:
//   cron_create — schedule a new task
//   cron_list   — list all scheduled tasks
//   cron_delete — cancel a task by id

type cronCreateInput struct {
	Cron      string `json:"cron" jsonschema_description:"5-field cron expression in local time: 'M H DoM Mon DoW'. Examples: '*/5 * * * *' = every 5 minutes; '0 9 * * *' = daily 9am; '30 14 28 2 *' = Feb 28 at 2:30pm one-shot."`
	Prompt    string `json:"prompt" jsonschema_description:"Prompt to inject as a synthetic user message when the task fires."`
	Recurring *bool  `json:"recurring,omitempty" jsonschema_description:"true (default) = repeat until 7-day expiry or explicit delete. false = fire once at the next match then auto-delete."`
	Durable   *bool  `json:"durable,omitempty" jsonschema_description:"true = persist to .evo-agent/sessions/<id>/scheduled_tasks/tasks.json so the task survives --resume. false (default) = session-only, dies with the process."`
}

type cronDeleteInput struct {
	ID string `json:"id" jsonschema_description:"Task id returned by cron_create."`
}

type cronListInput struct{}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "cron_create",
			Description: anthropic.String(
				"Schedule a prompt to run at a future time using a 5-field cron expression. " +
					"By default the task is recurring (repeats on every match) and session-only (lives in memory). " +
					"Use recurring=false for one-shot reminders ('remind me at 3pm'). " +
					"Use durable=true to persist across --resume."),
			InputSchema: GenerateSchema[cronCreateInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in cronCreateInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if strings.TrimSpace(in.Cron) == "" {
				return "", fmt.Errorf("cron: cron expression is required")
			}
			if strings.TrimSpace(in.Prompt) == "" {
				return "", fmt.Errorf("cron: prompt is required")
			}
			recurring := true
			if in.Recurring != nil {
				recurring = *in.Recurring
			}
			durable := false
			if in.Durable != nil {
				durable = *in.Durable
			}
			id, err := GlobalCron.Create(in.Cron, in.Prompt, recurring, durable)
			if err != nil {
				return "", err
			}
			mode := "recurring"
			if !recurring {
				mode = "one-shot"
			}
			store := "session-only"
			if durable {
				store = "durable"
			}
			return fmt.Sprintf(
				"Scheduled task %s [%s/%s] cron=%q (%s). Auto-expires after %d days for recurring tasks; one-shot tasks delete after firing.",
				id, mode, store, in.Cron, HumanCron(in.Cron), cronAutoExpiryDays,
			), nil
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "cron_list",
			Description: anthropic.String("List every scheduled task (recurring + one-shot, session + durable) with id, cron, prompt preview, and last-fired time."),
			InputSchema: GenerateSchema[cronListInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			tasks := GlobalCron.List()
			if len(tasks) == 0 {
				return "No scheduled tasks.", nil
			}
			var b strings.Builder
			for _, t := range tasks {
				mode := "recurring"
				if !t.Recurring {
					mode = "one-shot"
				}
				store := "session"
				if t.Durable {
					store = "durable"
				}
				preview := t.Prompt
				if len(preview) > 80 {
					preview = preview[:80] + "…"
				}
				lastFired := "never"
				if t.LastFired > 0 {
					lastFired = fmt.Sprintf("%dms-ago",
						(GlobalCron.nowMs()-t.LastFired))
				}
				b.WriteString(fmt.Sprintf(
					"%s  %s  [%s/%s]  last_fired=%s  prompt=%q\n",
					t.ID, t.Cron, mode, store, lastFired, preview,
				))
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name:        "cron_delete",
			Description: anthropic.String("Cancel a scheduled task by id. Idempotent for unknown ids (returns a not-found message)."),
			InputSchema: GenerateSchema[cronDeleteInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in cronDeleteInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}
			if strings.TrimSpace(in.ID) == "" {
				return "", fmt.Errorf("cron: id is required")
			}
			if !GlobalCron.Delete(in.ID) {
				return fmt.Sprintf("No scheduled task with id %q.", in.ID), nil
			}
			return fmt.Sprintf("Cancelled scheduled task %s.", in.ID), nil
		},
	})
}
