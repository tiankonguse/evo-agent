---
name: loop
description: Schedule a recurring prompt or slash command on an interval (defaults to 10m)
argument-hint: [interval] <prompt>
arguments: input
user-invocable: true
---

# /loop — schedule a recurring prompt

Parse the input below into `[interval] <prompt…>` and schedule it with the
`cron_create` tool.

## Parsing (in priority order)

1. **Leading token**: if the first whitespace-delimited token matches
   `^\d+[smhd]$` (e.g. `5m`, `2h`), that's the interval; the rest is the
   prompt.
2. **Trailing "every" clause**: otherwise, if the input ends with
   `every <N><unit>` or `every <N> <unit-word>` (e.g. `every 20m`,
   `every 5 minutes`, `every 2 hours`), extract that as the interval and
   strip it from the prompt. Only match when what follows "every" is a
   time expression — `check every PR` has no interval.
3. **Default**: otherwise, interval is `10m` and the entire input is the
   prompt.

If the resulting prompt is empty, show usage `/loop [interval] <prompt>` and
stop — do not call `cron_create`.

Examples:
- `5m /git-commit` → interval `5m`, prompt `/git-commit` (rule 1)
- `check the deploy every 20m` → interval `20m`, prompt `check the deploy` (rule 2)
- `run tests every 5 minutes` → interval `5m`, prompt `run tests` (rule 2)
- `check the deploy` → interval `10m`, prompt `check the deploy` (rule 3)
- `check every PR` → interval `10m`, prompt `check every PR` (rule 3 — "every" not followed by time)
- `5m` → empty prompt → show usage

## Interval → cron

Supported suffixes: `s` (seconds, rounded up to nearest minute, min 1),
`m` (minutes), `h` (hours), `d` (days). Convert:

| Interval pattern    | Cron expression       | Notes                                        |
|---------------------|-----------------------|----------------------------------------------|
| `Nm` where N ≤ 59   | `*/N * * * *`         | every N minutes                              |
| `Nm` where N ≥ 60   | `0 */H * * *`         | round to hours (H = N/60, must divide 24)    |
| `Nh` where N ≤ 23   | `0 */N * * *`         | every N hours                                |
| `Nd`                | `0 0 */N * *`         | every N days at midnight local               |
| `Ns`                | treat as `ceil(N/60)m`| cron minimum granularity is 1 minute         |

**If the interval doesn't cleanly divide its unit** (e.g. `7m` →
`*/7 * * * *` gives uneven gaps at :56 → :00; `90m` → 1.5h which cron
can't express), pick the nearest clean interval and tell the user what
you rounded to before scheduling.

**Avoid :00 and :30 minute marks** when you have any flexibility — every
session asking for "hourly" landing on `0 * * * *` is a fleet-wide spike.
For `1h` prefer `7 * * * *`; for `30m` prefer `*/30 * * * *` only when the
user explicitly asks for the half-hour mark.

## Action

1. Call `cron_create` with:
   - `cron`: the expression from the table above
   - `prompt`: the parsed prompt verbatim (slash commands like `/git-commit`
     are passed through unchanged — the model will run them when the task
     fires)
   - `recurring`: `true`
   - `durable`: `false` (session-only — only set `true` if the user
     explicitly asked it to persist)
2. Briefly confirm in one or two lines: what's scheduled, the cron
   expression, the human-readable cadence, that recurring tasks auto-expire
   after 7 days, and that they can cancel sooner with `cron_delete` (include
   the job ID).
3. **Then immediately execute the parsed prompt now** — don't wait for the
   first cron fire. If the prompt is a slash command, dispatch it; otherwise
   act on it directly.

## Input

$ARGUMENTS

If the input above is empty, print this usage and stop:

    Usage: /loop [interval] <prompt>

    Run a prompt or slash command on a recurring interval.

    Intervals: Ns, Nm, Nh, Nd (e.g. 5m, 30m, 2h, 1d). Minimum granularity is 1 minute.
    If no interval is specified, defaults to 10m.

    Examples:
      /loop 5m /git-commit
      /loop 30m check the deploy
      /loop check the deploy          (defaults to 10m)
      /loop check the deploy every 20m
