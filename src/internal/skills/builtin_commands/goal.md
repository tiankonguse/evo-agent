---
name: goal
description: Set, view, or clear a session goal that auto-continues until met
argument-hint: <condition> | clear
user-invocable: true
---

`/goal <condition>` sets a session goal. After every turn that ends with no
tool calls, the same MODEL_ID is invoked as an evaluator to judge whether
the condition holds. If not, the loop synthesizes a `<goal-reminder>` user
message and keeps working — capped at 30 evaluator-driven continuations.
A persistent plan is also created under `.evo-agent/tasks/todo/<plan-name>/` so the
work survives session restarts.

`/goal` (no args) shows status. `/goal clear` cancels (aliases: `stop`,
`off`, `reset`, `cancel`, `none`). The active goal persists across
`evo-agent --resume <id>`.
