---
name: remember
description: Persist important information from this conversation to memory
argument-hint: [hint]
arguments: hint
user-invocable: true
---

Call the `remember` tool to spawn a memory extraction subagent.

If the user provided a hint (e.g. "/remember save my preferences"), pass it as the `hint` parameter.
Otherwise call `remember` with no hint for automatic extraction.

Do not attempt to write memory files yourself — the subagent handles all file operations.
