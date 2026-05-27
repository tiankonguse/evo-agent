# EVO-AGENT: Reference Implementation & Command System Analysis

## 1. REFERENCE IMPLEMENTATION (refs/refs.ts)

### Overview
`refs/refs.ts` is a **TypeScript reference** for the `/init` command in Claude Code. It demonstrates how to generate CLAUDE.md files and related project documentation interactively.

### Key Structure

#### Two Prompt Versions
1. **OLD_INIT_PROMPT** - Legacy approach: simple analysis and CLAUDE.md creation
2. **NEW_INIT_PROMPT** - Modern approach: 8-phase interactive setup with optional skills/hooks

### The 8-Phase New Init Workflow

**Phase 1: Ask what to set up**
- Project CLAUDE.md vs Personal CLAUDE.local.md vs Both
- Optional skills and hooks setup
- Uses AskUserQuestion for user decisions

**Phase 2: Explore the codebase**
- Launch subagent to read key files:
  - Manifest files (package.json, Cargo.toml, go.mod, etc.)
  - README, Makefile, CI configs
  - Existing CLAUDE.md, .claude/rules/, AGENTS.md
  - Tool configs (.cursor/rules, .cursorrules, .github/copilot-instructions.md, etc.)
- Detect: build/test/lint commands, languages, project structure, code style, gotchas, env vars
- Note what CAN'T be figured out from code alone

**Phase 3: Fill in the gaps**
- Use AskUserQuestion to gather missing information
- For project CLAUDE.md: ask about codebase practices, gotchas, branch conventions, env setup, testing quirks
- For personal CLAUDE.local.md: ask about user role, familiarity, sandbox URLs, worktree setup, communication preferences
- Build a preference queue: {type: hook|skill|note, description, target file, implementation details}

**Phase 4: Write CLAUDE.md (if selected)**
- Minimal, actionable content only: "Would removing this cause Claude to make mistakes?"
- Include:
  - Non-standard build/test/lint commands
  - Code style rules that DIFFER from language defaults
  - Testing instructions and quirks
  - Repo etiquette (branch naming, PR conventions, commit style)
  - Required env vars or setup steps
  - Non-obvious gotchas or architectural decisions
  - Important parts from existing AI tool configs
- Exclude: file structure, standard conventions, generic advice, detailed API docs, frequently-changing info
- Prefix with standard header
- If CLAUDE.md exists: propose diffs, don't overwrite

**Phase 5: Write CLAUDE.local.md (if selected)**
- Personal, gitignored file (add to .gitignore)
- Include: user role, familiarity, sandbox URLs, personal workflow preferences
- Special handling for external/sibling git worktrees: write content to ~/.claude/<project-name>-instructions.md and stub from each worktree

**Phase 6: Create skills (if user chose skills)**
- Consume skill entries from Phase 3 preference queue
- Create at `.claude/skills/<skill-name>/SKILL.md` with YAML frontmatter
- For workflows with side effects: add `disable-model-invocation: true` to require user invocation
- For parametrized skills: use `$ARGUMENTS` placeholders

**Phase 7: Suggest additional optimizations**
- GitHub CLI setup (if missing but needed)
- Linting setup (if not configured)
- Format-on-edit hooks (if formatter detected)
- Check for each gap, ask user via AskUserQuestion

**Phase 8: Summary and next steps**
- Recap what was set up and key points
- Remind user to review and tweak, can run /init again anytime
- Present relevant follow-up to-do list:
  - Frontend design plugin if React/Vue/Svelte detected
  - Playwright plugin if frontend code detected
  - Test framework setup if tests missing/sparse
  - Skill-creator plugin for creating/refining skills
  - Plugin browsing recommendations

### Important Implementation Details

**Constraint Respecting**
- Phase 1 skills+hooks choice is a HARD filter
- If user picked "Skills only": downgrade any hook to skill or CLAUDE.md note
- If "Hooks only": downgrade skills to hooks (where possible) or notes
- If "Neither": everything becomes a CLAUDE.md note

**Proposal Display**
- Use AskUserQuestion's `preview` field for markdown display
- Keep previews compact (no scrolling)
- Structure: one line per item, no blank lines between
- Example format:
  ```
  • **Format-on-edit hook** (automatic) — `ruff format <file>` via PostToolUse
  • **/verify skill** (on-demand) — `make lint && make typecheck && make test`
  • **CLAUDE.md note** (guideline) — "run lint/typecheck/test before marking done"
  ```

---

## 2. COMMAND SYSTEM ARCHITECTURE

### High-Level Overview

The evo-agent command system enables **slash commands** (like `/hello`, `/remember`, `/consolidate`) that users can invoke in the TUI. Commands are **Markdown files** stored in `.evo-agent/command/` with YAML frontmatter.

```
User Input: "/hello Alice"
           ↓
    Dispatch (main.go:126)
           ↓
    skills.Dispatch(query)
           ↓
    Parse: name="hello", rawArgs="Alice"
           ↓
    LookupForSlash("hello") → skillDocument
           ↓
    ParseArgs("Alice") → ["Alice"]
           ↓
    RenderBody(body, argNames, args, rawArgs)
           ↓
    Build SlashResult with XML-wrapped content
           ↓
    Inject into message history
           ↓
    Agent processes with skill/command content
```

### File Structure

```
.evo-agent/
├── command/
│   ├── hello.md          ← Commands (flat files, not in catalog)
│   ├── remember.md
│   └── consolidate.md
├── skill/
│   └── <skill-name>/
│       └── SKILL.md      ← Skills (nested, may be in catalog)
├── mcp.json              ← MCP server configuration
├── memory/               ← Persistent memories
└── ...
```

### Command Markdown Format

Each command is a `.md` file with **YAML frontmatter**:

```yaml
---
name: hello
description: Say hello to someone
argument-hint: [name]        # UI hint for user
arguments: name              # Space/comma-separated named arg list
user-invocable: true         # Default: true (can user invoke via slash?)
---

Say hello to $name in a friendly way.
```

**Frontmatter Fields**
- `name` (required): unique identifier
- `description` (optional): shown in help, defaults to "No description"
- `argument-hint` (optional): UI hint like `[name]` or `[issue-number]`
- `arguments` (optional): space/comma-separated list of named arg names for substitution
- `user-invocable` (optional): default true; false = model-only via load_skill
- `disable-model-invocation` (optional): true = not in catalog, user-slash-only (skills only)

### Processing Pipeline

#### 1. **Loading Phase** (registry.go)

**InitCommands()** scans `.evo-agent/command/*.md`:
- Reads each `.md` file
- Parses frontmatter (YAML between `---` delimiters)
- Stores in `commandDocuments` map (separate from `skillDocuments`)
- Falls back to filename (without `.md`) if `name` field missing

```go
// Separate maps: commands and skills never conflict
var (
    skillDocuments   = map[string]skillDocument{}
    commandDocuments = map[string]skillDocument{}
)

// skillDocument bundles manifest + body
type skillDocument struct {
    Manifest SkillManifest
    Body     string         // Full skill/command text after frontmatter
    Path     string         // Absolute path to file
}
```

#### 2. **Dispatch Phase** (dispatch.go)

**Dispatch(input string)** processes slash commands:

1. **Validation**: Input must start with "/" followed by a letter (avoids file paths like `/usr/bin`)
2. **Parsing**: Split at first space → `name` and `rawArgs`
3. **Lookup**: `LookupForSlash(name)` checks commands first, then skills
4. **Permission check**: Verify `doc.Manifest.UserInvocable`
5. **Argument parsing**: `ParseArgs(rawArgs)` with shell-style quoting
6. **Body rendering**: `RenderBody(body, argNames, args, rawArgs)` substitutes placeholders
7. **Wrapping**: Wrap in XML tags for LLM clarity
8. **Return**: `SlashResult` with prompt, content, and metadata

**SlashResult structure**:
```go
type SlashResult struct {
    Found   bool   // true = recognized command
    Prompt  string // "User invoked /hello (command). Follow instructions below."
    Content string // "<skill name="hello" source="slash" type="command">...</skill>"
    Name    string // Display name
}
```

#### 3. **Main Integration** (main.go:119-145)

In the TUI agent goroutine:
```go
if result := skills.Dispatch(query); result.Found {
    if result.Content != "" {
        // Two-block message: prompt + skill content
        history = append(history, anthropic.NewUserMessage(
            anthropic.NewTextBlock(result.Prompt),
            anthropic.NewTextBlock(result.Content),
        ))
    }
    // Run query with skill content injected
    a.RunQueryDirect(&history, &compactState, doneCh)
}
```

### Argument Substitution (render.go)

**RenderBody(body, argNames, args, rawArgs)** substitutes placeholders in order of precedence:

1. **$ARGUMENTS[N]** → args[N] (e.g., `$ARGUMENTS[0]`)
   ```
   Input: "/hello Alice Bob"
   $ARGUMENTS[0] → "Alice"
   $ARGUMENTS[1] → "Bob"
   ```

2. **$name** → named argument by position
   ```
   arguments: name
   /hello Alice
   $name → "Alice"
   ```

3. **$N shorthand** → args[N] (e.g., `$0`, `$1`)
   ```
   $0 → first arg, $1 → second arg, etc.
   ```

4. **$ARGUMENTS** → full rawArgs string
   ```
   /hello Alice Bob Carol
   $ARGUMENTS → "Alice Bob Carol"
   ```

5. **Fallback**: If NO placeholder found, append to body:
   ```
   ARGUMENTS: Alice Bob Carol
   ```

### Example Commands

#### hello.md
```yaml
---
name: hello
argument-hint: [name]
arguments: name
user-invocable: true
---

Say hello to $name in a friendly way.
```

**Usage**: `/hello Alice` → "Say hello to Alice in a friendly way."

#### remember.md
```yaml
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
```

**Usage**:
- `/remember` → shows full body, remembers context
- `/remember save my coding preferences` → passes "save my coding preferences" as hint

### Key Functions Reference

| Function | File | Purpose |
|----------|------|---------|
| `Init()` | registry.go | Load skills from `.evo-agent/skill/**/SKILL.md` |
| `InitCommands()` | registry.go | Load commands from `.evo-agent/command/*.md` |
| `Dispatch(input)` | dispatch.go | Check if input is slash command, process it |
| `LookupForSlash(name)` | registry.go | Find command or skill by name (commands priority) |
| `ParseArgs(raw)` | args.go | Parse shell-style args with quote support |
| `RenderBody(body, argNames, args, rawArgs)` | render.go | Substitute argument placeholders |
| `CommandNames()` | registry.go | Return user-invocable command names + model-disabled skills |
| `Catalog()` | registry.go | Return formatted skill list for system prompt (excludes model-disabled) |
| `Load(name)` | registry.go | Return full skill body wrapped in XML (for load_skill tool) |

### Priority Rules

1. **Command vs Skill with same name**: Commands take priority in `LookupForSlash`
2. **Catalog membership**: Only skills with `disable-model-invocation: false` (default) in catalog
3. **User-invocable**: Default true; false = only accessible via `load_skill` tool
4. **Model-invocation**: Defaults to true for skills; can be disabled via frontmatter

### System Prompt Injection

In main.go (lines 72-79), commands are exposed to the model:

```go
slashNames := skills.SlashNames()  // Get user-invocable command names
if len(slashNames) > 0 {
    cfg.SystemMsg += "\n\nSlash commands: /<skill-name> (e.g., /git-commit) is shorthand for users " +
        "to invoke a skill. When executed, the skill content is expanded into a full prompt. " +
        "Use the load_skill tool to load skills programmatically. " +
        "IMPORTANT: Only use load_skill for skills listed above - do not guess or invent skill names."
}
```

---

## 3. CONNECTION TO /init REFERENCE

The `/init` reference (refs.ts) would integrate with this command system by:

1. **Phase 6 (Create skills)**: Write `.evo-agent/skill/<skill-name>/SKILL.md` files
2. **Commands as part of onboarding**: Suggest relevant commands based on project type (e.g., `/git-commit` for Git repos, `/test` for projects with tests)
3. **Skill catalog injection**: Once skills are created, `Catalog()` includes them in system prompt
4. **User-directed skill loading**: Via `load_skill` tool in agent loop

The command system is **purely runtime**: commands don't auto-generate; they're created manually by `/init` as part of the onboarding process.

