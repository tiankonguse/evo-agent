---
name: team
description: View / control persistent teammates (agent team mode)
argument-hint: list | shutdown <name> | inbox <name>
user-invocable: true
---
# /team — Persistent Teammate Control

Use this command to inspect or control the team_* roster without going
through the LLM:

  /team                    → list every teammate (alias for `list`)
  /team list               → same as above
  /team shutdown <name>    → gracefully stop a teammate (history preserved)
  /team inbox <name>       → drain + pretty-print one teammate's inbox
                              (debug helper)

Teammates are spawned via the model-facing `team_spawn` tool, and the
lead's inbox is drained automatically at the top of each agent turn — so
you should rarely need `/team inbox` outside of debugging.

Storage lives at `.evo-agent/team/`:

  config.json              — registered members + statuses
  inbox/<name>.jsonl       — pending inbox messages
  history/<name>.jsonl     — teammate's full message history

For full design see `docs/REFERENCE.md` § Agent Teams.
