# Scheduled Tasks (cron)

> Status: implemented in v0.17.x — `internal/tools/cron.go`, `internal/tools/cron_tools.go`

Lets the model schedule prompts to run at a future time using a 5-field cron
expression. When a scheduled task fires, its prompt is enqueued and injected
as a synthetic `<scheduled-task>` user message at the top of the next agent
turn — the same pattern already used by `bg_run` (`bgtask.go`).

## Why this exists

Some workflows need the agent to wake itself up later: poll a long-running
build, remind the user to push a release branch, re-check a flaky test once
infra recovers. Without scheduling, the only option is for the user to type
the prompt again.

Scheduled tasks let the model say *"I'll check back in 10 minutes"* — and
actually do it.

## Storage layout

Per-session, durable tasks only:

```
.evo-agent/sessions/<sessionID>/scheduled_tasks/
  tasks.json          ← single JSON file with all durable tasks
```

Session-only tasks live in process memory and never touch disk. Each session
gets its own scheduled_tasks/ directory so two parallel sessions don't fight
over the same file (this differs from Claude Code's project-shared
`.claude/scheduled_tasks.json` because evo-agent has no per-project lock
file machinery).

## Cron expression syntax

Standard 5-field local-time cron:

```
minute hour day-of-month month day-of-week
0-59   0-23 1-31         1-12  0-6 (0=Sun, 7=Sun alias)
```

Per-field syntax: `*`, `*/N`, `N`, `N-M`, `N,M,...`. Unsupported: `L`, `W`,
`?`, name aliases (`MON`, `JAN`).

When BOTH day-of-month and day-of-week are constrained, a date matches if
EITHER field matches (vixie-cron semantics).

| Example         | Meaning                  |
| :-------------- | :----------------------- |
| `*/5 * * * *`   | every 5 minutes          |
| `0 9 * * *`     | every day at 9:00am      |
| `30 14 28 2 *`  | Feb 28 at 2:30pm one-shot|
| `0 9 * * 1-5`   | weekdays at 9am          |

## Tools

### `cron_create`

Schedule a new task.

| field       | type   | default | meaning |
| :---------- | :----- | :------ | :------ |
| `cron`      | string | —       | 5-field cron expression |
| `prompt`    | string | —       | text injected when the task fires |
| `recurring` | bool   | `true`  | `true` = repeat until 7-day expiry / explicit delete; `false` = fire once then auto-delete |
| `durable`   | bool   | `false` | `true` = persist to disk (survives `--resume`); `false` = session-only |

Returns: `Scheduled task <id> [<mode>/<store>] cron=... — auto-expires after 7 days...`.

### `cron_list`

No arguments. Returns one line per task:

```
<id>  <cron>  [recurring|one-shot/durable|session]  last_fired=<...>  prompt="<preview>"
```

### `cron_delete`

| field | type   | default | meaning |
| :---- | :----- | :------ | :------ |
| `id`  | string | —       | 8-char hex id from `cron_create` |

Idempotent — unknown ids return a "no scheduled task with id" message instead
of erroring.

## Lifecycle

1. **Init.** `main.go` calls `tools.GlobalCron.Init(sess.Dir, sess.ID)` after
   the session is opened. Init creates `<sessionDir>/scheduled_tasks/`,
   loads any prior durable tasks from `tasks.json`, and starts the
   1-second background ticker goroutine.

2. **Tick.** A goroutine wakes every second, computes the current minute
   index (`hour*60 + minute`), and skips evaluation if the minute hasn't
   changed since the last tick — this prevents firing the same `* * * * *`
   task 60 times per minute. Each task's cron is matched against the
   current time (with the deterministic 1-4 minute jitter offset applied
   to tasks targeting `:00` / `:30`). Matches are queued, recurring
   `lastFiredAt` is updated, one-shots and 7-day-expired tasks are deleted,
   and durable changes are flushed back to `tasks.json`.

3. **Drain.** Before each LLM call in `agent.Loop()`,
   `tools.GlobalCron.DrainNotifications()` returns and clears the queue.
   Non-empty drains are formatted as a single
   `<scheduled-task>...</scheduled-task>` block and appended to
   `state.Messages` as a synthetic user message.

4. **Stop.** `defer tools.GlobalCron.Stop()` in `main.go` closes the stop
   channel; the goroutine exits cleanly.

## Resume behaviour

On `evo-agent --resume <id>`, `Init` is called against the resumed session's
directory. The same `tasks.json` is rehydrated, so durable tasks survive
process restarts.

Session-only (`durable=false`) tasks die with the process — by design. The
scheduler does NOT detect tasks that "should have fired" while the process
was offline (matching Python ref's behaviour of optional `detect_missed_tasks`
that this implementation skips for simplicity).

## Limits & jitter

- **Max 50 tasks per session.** Matches Claude Code's `MAX_JOBS=50`.
- **7-day auto-expiry for recurring tasks.** Bounds how long a forgotten
  loop can run.
- **Jitter:** tasks targeting `:00` or `:30` get a deterministic 1-4 minute
  forward offset (keyed off the cron string hash). Stops two parallel
  sessions from firing the same `0 * * * *` job on the exact same wall-clock
  minute. Off-minute crons (e.g. `7 * * * *`) get no jitter.

## Internals

- `cron.go` — `parseCron()`, `matchCron()`, `nextRun()` (scheduler core +
  background ticker + persistence).
- `cron_tools.go` — three tool registrations (`cron_create`, `cron_list`,
  `cron_delete`).
- `cron_test.go` — unit coverage for parsing, matching, nextRun, scheduler
  CRUD, single-fire-per-minute guard, one-shot deletion, notification
  formatting.
