# Session Persistence

Append-only session transcript layer that survives process exit and supports `/resume`.

Implementation: [`src/internal/session/`](../src/internal/session/)
Reference design: [`refs/ref.md`](../refs/ref.md) (Claude Code session storage model).

## What gets persisted

Every agent run automatically creates a session directory under
`.evo-agent/sessions/{unix_ms}_{UUID}/`. The directory contains:

| File | Purpose |
|---|---|
| `messages.jsonl` | Append-only event stream (one JSON record per line). Never modified after the process exits. |
| `meta.json` | Lightweight sidecar — cumulative tokens, first user prompt, branch, ts. Rewritten after every append; consumed by `/resume`'s picker for cheap listing. |
| `subagent/{unix_ms}_{name}_{UUID}.jsonl` | Per-subagent sidechain transcript. Created on demand when a `task` tool call fires. |

The session id format is `<unix_ms>_<8 hex>`, e.g. `1780227556183_b525857d`.

The numeric millisecond prefix preserves chronological ordering under lexical
sort (so `os.ReadDir` on the sessions root returns entries in time order
without an explicit sort) and is trivially `strconv.ParseInt`-able. The `_`
separator avoids any ambiguity with internal `-` characters.

## JSONL record schema

Every line in `messages.jsonl` and any subagent file shares the same envelope.
Type-specific fields are populated only for the matching record type.

```jsonc
{
  "type": "user|assistant|session_start|compact_boundary|resume_marker|subagent_start|subagent_end",
  "timestamp": "2026-05-31T19:58:42.705+08:00",  // ISO-8601 local wall-clock + numeric offset
  "agent_version": "0.13.0",
  "session_id": "1780227556183_b525857d",
  "cwd": "/abs/project",
  "prompt_id": "p_4f2a",      // shared across records produced by one user turn
  "git_branch": "main",       // best-effort `git rev-parse --abbrev-ref HEAD`

  // type=user|assistant
  "message": { /* anthropic.MessageParam */ },
  "input_tokens": 1234,       // assistant only
  "output_tokens": 567,       // assistant only

  // type=compact_boundary
  "summary": "…digest text…",
  "compact_count": 1,

  // type=resume_marker
  "from_session_id": "…",
  "restored_count": 12,

  // type=subagent_start | subagent_end
  "agent_name": "exploration",
  "subagent_path": "1717196401-exploration-b2c4.jsonl",
  "result": "…final text…"   // subagent_end only
}
```

`message` reuses `anthropic.MessageParam` directly so resume can deserialize
back to a runnable message slice with no translation step.

## Write path (append-only)

The single source of truth is [`recorder.go`](../src/internal/session/recorder.go).
Each call opens the file with `O_APPEND|O_CREATE|O_WRONLY`, writes one JSON
line + `\n`, then closes. Each call is mutex-protected so future concurrency
cannot corrupt a line.

Writes happen at four points in the agent loop:

1. **User turn entry** (`agent.RunQuery` / `RunQueryDirect` / `Run`): one
   `user` record with the freshly built `MessageParam`.
2. **Assistant response** (`agent.Loop`, after `state.Messages = append(...,
   resp.ToParam())`): one `assistant` record with `input_tokens` and
   `output_tokens` from `resp.Usage`.
3. **Tool result** (`agent.Loop`, after the tool_results user message): one
   `user` record (tool results travel as user role).
4. **Compact** (`agent.CompactHistory`): one `compact_boundary` record after
   the summary is generated, before the in-memory message slice is rewritten.

Subagent activity adds two parent records (`subagent_start` / `subagent_end`)
plus full per-message records inside the sidechain file.

The old `transcripts/transcript_<unix>.jsonl` snapshot from
`internal/agent/transcripts.go` is retained — it is still written on every
compact pass for forensic value, but resume reads from `sessions/`.

## Read path (`/resume` and `--resume`)

[`loader.go`](../src/internal/session/loader.go) implements `LoadForResume`:

1. Stream-scan the file, collecting every record into a slice.
2. Find the index of the **last** `compact_boundary`, remembering its summary.
3. Drop every `user` / `assistant` record at indexes ≤ that boundary index.
4. If a boundary existed, prepend a synthetic `user` message wrapping the
   summary in `<previous-conversation-summary>` tags so the model knows it
   is reading a digest.
5. Replay every post-boundary `user` / `assistant` `Message` field.
6. For every `subagent_end` after the boundary, append a synthetic
   `<subagent-result name="…">…</subagent-result>` user block so the parent
   context still remembers the subagent's conclusion (the body itself is
   not replayed).

The result is a `LoadResult{Messages, RestoredCount, HasCompactedAt, Summary,
SourceID}` consumable as initial history.

## Entry points

### Command line

```bash
evo-agent                        # new session
evo-agent --resume <session-id>  # resume an old session
evo-agent --plain --resume <id>  # plain mode + resume
```

`--resume <id>` always opens a **new** session file and writes a `resume_marker`
into it referencing the source id. The old file is not touched.

### TUI dropdown

Type `/resume` (no args) and a dropdown appears listing all previous sessions
in this project, newest first. Each row shows:

```
  YYYY-MM-DD HH:MM   tokens=12,450   「first prompt up to ~60 chars…」
```

↑/↓ select, Enter accepts and auto-submits `/resume <id>`, Esc cancels.

### Inline form

`/resume <id>` typed into the prompt is intercepted client-side (in main.go's
agent goroutine) — it never reaches the LLM. The current session's `history`
slice is replaced with the loaded messages, a `resume_marker` is appended,
and a confirmation system message is printed.

## Exit behavior

Once `main()` returns (TUI Quit or plain-mode `q`/`exit`/EOF), a deferred
`fmt.Printf` prints:

```
Resume this session with: evo-agent --resume <id>
```

The session file is no longer touched — no metadata refresh, no rewrite. New
processes always open new files.

## File map

| File | What |
|---|---|
| [`session/record.go`](../src/internal/session/record.go) | `Record` envelope + `Type*` constants |
| [`session/ids.go`](../src/internal/session/ids.go) | `NewSessionID`, `NewPromptID`, `NewSubagentFilename`, `slugify` |
| [`session/git.go`](../src/internal/session/git.go) | Best-effort `git rev-parse --abbrev-ref HEAD` |
| [`session/session.go`](../src/internal/session/session.go) | `Session` struct + `NewSession` (writes session_start) |
| [`session/recorder.go`](../src/internal/session/recorder.go) | Main-channel `Recorder` + `meta.json` flush |
| [`session/subagent_recorder.go`](../src/internal/session/subagent_recorder.go) | Sidechain recorder for subagent transcripts |
| [`session/loader.go`](../src/internal/session/loader.go) | `LoadForResume` rebuilds a runnable message slice |
| [`session/list.go`](../src/internal/session/list.go) | `ListSessions` for the resume picker |
| [`session/session_test.go`](../src/internal/session/session_test.go) | Round-trip, compact-clip, sidechain, resume_marker, slugify tests |

## Hooks in the rest of the codebase

- `agent.LoopState` carries `Recorder *session.Recorder` and `PromptID
  string`. `nil` recorder disables persistence transparently.
- `agent.New` registers two subagent callbacks; the named one carries
  `agentName` so subagent files are labeled.
- `agent.CompactHistory` takes `recorder, promptID` and writes a
  `compact_boundary` after summary generation.
- `agent.Agent.AttachSession(s)` wires the active session in from `main()`.
- `tools/task.go` registers `RegisterNamedSubagentRunner` so the task tool's
  `description` becomes the subagent file label.
- `main.go` is the orchestrator: parses `--resume`, calls
  `session.NewSession` / `LoadForResume`, prints the exit hint, and
  intercepts `/resume <id>` client-side in the TUI agent goroutine.

## Verification

```bash
make build               # compile
make vet                 # static checks
go test ./internal/session/ -v   # 6 tests, all pass

# Smoke test:
cd $(mktemp -d)
MODEL_ID=… evo-agent --plain    # send a message, exit
ls .evo-agent/sessions/*/messages.jsonl   # exists and contains a user+assistant pair
evo-agent --plain --resume <id> # restored history, new session id
```
