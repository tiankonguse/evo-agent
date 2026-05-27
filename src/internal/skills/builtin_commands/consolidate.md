---
name: consolidate
description: Consolidate memories - merge duplicates, remove stale entries
argument-hint: [focus]
arguments: focus
user-invocable: true
---

Call the `consolidate_memory` tool to spawn a memory consolidation subagent.

If the user provided a focus (e.g. "/consolidate merge feedback memories"), pass it as the `focus` parameter.
Otherwise call `consolidate_memory` with no focus for full consolidation.

Do not attempt to modify memory files yourself — the subagent handles all file operations.
