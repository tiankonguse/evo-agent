---
name: resume
description: Resume a previous session by id (use TUI dropdown or pass --resume <id>)
argument-hint: <session-id>
user-invocable: true
---

This command is handled client-side. In the TUI, typing `/resume` opens a dropdown showing previous sessions sorted by time, with token totals and the first prompt — pick one with up/down + Enter. Outside the TUI, run `evo-agent --resume <session-id>` instead.

When a session is restored, every restored message is replayed into the in-memory history, a `resume_marker` record is written into the new session transcript, and conversation continues normally.
