# OpenAI Chat Completions Protocol — adapter notes

This file documents the OpenAI-side surface that `internal/llm/openai.go`
talks to. For the Anthropic side see [`anthropic-sdk-go.md`](./anthropic-sdk-go.md);
for the boundary abstraction itself see [`REFERENCE.md` § 11.7](./REFERENCE.md).

---

## Endpoint

`POST <OPENAI_BASE_URL>/v1/chat/completions`

`OPENAI_BASE_URL` defaults to `https://api.openai.com`. The adapter trims a
trailing `/` so both `https://api.openai.com` and `https://api.openai.com/`
behave the same. Compatible providers exposing `/v1/chat/completions`
(DeepSeek, Qwen DashScope, OpenRouter, Ollama, …) work by overriding
`OPENAI_BASE_URL`.

Headers:

```
Content-Type: application/json
Authorization: Bearer <OPENAI_API_KEY>
```

---

## Request body (subset)

evo-agent only emits the fields it needs. Anything not listed here is not
sent.

```jsonc
{
  "model": "<MODEL_ID>",
  "max_tokens": 8000,
  "temperature": 0.2,            // omitted unless set on MessageNewParams
  "top_p": 0.9,                  // omitted unless set
  "stop": ["\n\n"],              // omitted unless StopSequences set

  "messages": [
    { "role": "system",    "content": "<joined system prompts>" },
    { "role": "user",      "content": "<text>" },
    { "role": "assistant", "content": "<text>",
      "tool_calls": [
        { "id": "call_…", "type": "function",
          "function": { "name": "bash", "arguments": "{\"command\":\"ls\"}" }
        }
      ]
    },
    { "role": "tool", "tool_call_id": "call_…", "content": "<result>" }
  ],

  "tools": [
    { "type": "function",
      "function": {
        "name": "bash",
        "description": "Run a shell command…",
        "parameters": { "type": "object", "properties": { … }, "required": [ … ] }
      }
    }
  ]
}
```

### `arguments` is a JSON string

OpenAI requires `tool_calls[].function.arguments` to be a string whose
body is itself JSON. The adapter `json.Marshal`s the anthropic
`ToolUseBlockParam.Input` (`any`) and emits the result verbatim. On the
response leg the string is parsed back into a generic value before
being placed in the synthesized `tool_use.input` field.

### `parameters` is a JSON-Schema map

The anthropic `ToolInputSchemaParam` wire shape is identical to what
OpenAI accepts in `function.parameters`: `{type:"object", properties,
required, additionalProperties:false}`. The adapter round-trips it
through `json.Marshal` + `json.Unmarshal` into a `map[string]any` so
OpenAI receives unstructured JSON-Schema (no extra schema-conversion
logic needed).

---

## Response body (subset we read)

```jsonc
{
  "id": "chatcmpl-…",
  "model": "<echoed model name>",
  "choices": [
    {
      "index": 0,
      "finish_reason": "stop" | "tool_calls" | "function_call" | "length" | "content_filter",
      "message": {
        "role": "assistant",
        "content": "<text>" | null,
        "refusal": "<text>" | null,           // some content_filter cases
        "tool_calls": [
          { "id": "…", "type": "function",
            "function": { "name": "…", "arguments": "<JSON string>" }
          }
        ]
      }
    }
  ],
  "usage": {
    "prompt_tokens":     12,
    "completion_tokens": 7,
    "total_tokens":     19
  }
}
```

### `finish_reason` mapping

| OpenAI | Anthropic `StopReason` |
|---|---|
| `stop` | `end_turn` |
| `tool_calls` (also legacy `function_call`) | `tool_use` |
| `length` | `max_tokens` |
| `content_filter` | `refusal` |
| anything else (incl. empty) | `end_turn` (defensive default) |

The agent loop's "is this turn done?" decision is driven by **block
presence** (any `tool_use` block → keep looping), so a misclassified
stop reason is cosmetic.

---

## Errors

Non-2xx responses surface verbatim:

```
openai: 400 Bad Request: {"error":{"message":"invalid model","type":"invalid_request_error"}}
```

The agent loop currently treats every error from `SendMessage` as fatal
for the current turn (`agent/loop.go:149`) — same handling as Anthropic
SDK errors, no special-casing.

---

## Known limitations

- **No streaming**. OpenAI's SSE-streamed Chat Completions are not used;
  the adapter posts and waits for the full body.
- **No Responses API**. `/v1/responses` (the newer surface that more
  closely mirrors Anthropic content blocks) is not supported.
- **Thinking / extended-thinking blocks dropped**. Outbound: any
  Anthropic `OfThinking` / `OfRedactedThinking` content blocks are
  silently removed when translating to OpenAI. Inbound: OpenAI Chat
  Completions does not produce them.
- **No image / document blocks**. Anthropic `OfImage` / `OfDocument`
  user blocks are dropped on translation. evo-agent does not generate
  these today.
- **Tool name length**. OpenAI requires `^[a-zA-Z0-9_-]{1,64}$`;
  Anthropic accepts up to 128. Long MCP-prefixed names
  (`mcp__server__tool`) may exceed 64 chars and produce a 400 from
  OpenAI. If you hit this, rename the MCP server to something shorter.
- **Cache control dropped**. Anthropic `cache_control` breakpoints have
  no OpenAI equivalent and are silently ignored when translating
  outbound.

For the up-to-date authoritative request/response shape see the
[OpenAI API reference](https://platform.openai.com/docs/api-reference/chat/create).
