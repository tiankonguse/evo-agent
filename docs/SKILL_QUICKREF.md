# evo-agent Skill System - Quick Reference

## File Structure

```
.evo-agent/skill/
└── <skill-name>/
    └── SKILL.md              # Required file with YAML frontmatter + body
```

## SKILL.md Format

```markdown
---
name: skill-id                 # Optional (defaults to parent dir name)
description: One-line summary  # Optional (defaults to "No description")
---

# Your skill instructions here

Markdown content, code examples, step-by-step guides, etc.
```

## How Skills Work

### 1. **Startup** (main.go)
- `skills.Init()` scans `.evo-agent/skill/**/SKILL.md`
- Parses frontmatter → builds in-memory map
- Generates catalog → injects into system prompt

### 2. **System Prompt Injection**
```
Skills available:
- skill-name: Brief description
- another-skill: Another description

Use load_skill when a task needs specialized instructions before you act.
```

### 3. **Runtime Usage**
- LLM sees catalog in system prompt
- LLM calls `load_skill("skill-name")` when needed
- Tool returns XML-wrapped skill content:
```xml
<skill name="skill-name" path="/absolute/path/SKILL.md">
[Full skill content here]
</skill>
```

## API Reference

### skills.Init()
- **When**: Called once at startup (main.go, line 59)
- **What**: Scans `.evo-agent/skill/` directory tree
- **Error Handling**: Silently ignores missing directory
- **Output**: Populates global `documents` map

### skills.Catalog() → string
- **Returns**: Markdown bullet list of skills (alphabetical)
- **Format**: `- name: description\n- name2: description2`
- **Empty case**: Returns empty string (not injected to system prompt)

### skills.Load(name) → string
- **Returns**: XML-wrapped skill or error message
- **Format**: `<skill name="x" path="y">content</skill>`
- **Error**: `"Error: Unknown skill 'xyz'. Available skills: skill1, skill2"`

### skills.Names() → []string
- **Returns**: Unsorted slice of all skill names
- **Usage**: TUI sidebar display (sorted before display)

## Creating a New Skill

### Step 1: Create Directory
```bash
mkdir -p .evo-agent/skill/my-skill
```

### Step 2: Create SKILL.md
```bash
cat > .evo-agent/skill/my-skill/SKILL.md << 'EOF'
---
name: my-skill
description: What this skill teaches or helps with
---

## Usage

Clear instructions for when and how to use this skill.

## Examples

- Example 1
- Example 2

## Best Practices

- Practice 1
- Practice 2
EOF
```

### Step 3: Verify
Restart the agent and check:
- TUI sidebar shows your skill in the Skills list
- System prompt includes your skill in the available skills list
- `load_skill("my-skill")` returns your content

## Frontmatter Fields

| Field | Required | Type | Default | Notes |
|-------|----------|------|---------|-------|
| `name` | No | string | parent dir name | Skill identifier; must be unique |
| `description` | No | string | "No description" | Brief one-liner for catalog |
| Other fields | No | any | — | Parsed but ignored (e.g., `license`, `compatibility`) |

## Implementation Details

### Parsing
- **Regex**: `^---\n(.*?)\n---\n(.*)` (YAML frontmatter)
- **Parser**: Simple key-value split on first colon per line
- **Trimming**: All keys/values trimmed; malformed lines ignored

### Performance
- **Load time**: ~1-5ms for 4 skills (single directory scan)
- **Lookup**: O(1) map lookup by name
- **Memory**: ~1KB per skill (plus content size)

### Error Handling
- Missing `.evo-agent/skill/`: silently ignored
- Unreadable file: logged to stderr, walk continues
- Invalid frontmatter: parsed best-effort (missing fields get defaults)
- Unknown skill name: friendly error plus available list

## Real-World Examples

### git-commit Skill
**Purpose**: Teach commit message conventions

**Content**: Best practices, format template, type list

**Trigger**: LLM detects commit-related task, calls `load_skill("git-commit")`

### summarize-changes Skill
**Purpose**: Provide change analysis template

**Content**: Instructions to summarize plus list risks

**Trigger**: User asks "what changed?" or "review my diff"

### union-field-trace Skill
**Purpose**: Complex data lineage analysis

**Content**: ~400 lines with step-by-step MCP tool calls

**Trigger**: Specialized query about field value sources

## Testing

```bash
cd src/internal/skills
go test -v
```

## Common Patterns

### Pattern 1: Task-Specific Guidance
```markdown
---
name: pattern-matching
description: Regex patterns and matching strategies
---

## When to use patterns

Use when you need to:
- Extract text using regular expressions
- Validate string formats
- Split structured data
```

### Pattern 2: Tool Integration Guide
```markdown
---
name: tool-workflow
description: Step-by-step instructions for tool usage
---

## Prerequisites

Check these before proceeding:
- Tool is installed
- Configuration is set

## Workflow

1. Step 1: Do this
2. Step 2: Do that
```

## Limitations

- **No dynamic content**: Skills are static files
- **No parameters**: `load_skill` takes only a skill name, not arguments
- **No versioning**: Single version per skill (no history)
- **Case-sensitive**: Skill names must match exactly
- **No dependencies**: One skill cannot reference another

## Troubleshooting

### Skill not appearing in catalog
- Check file exists at `.evo-agent/skill/name/SKILL.md`
- Restart agent (catalog loaded at startup)
- Check stdout for `[Skills] Loaded N skill(s)` message

### load_skill returns "Unknown skill"
- Verify spelling matches exactly (case-sensitive)
- Check `skills.Catalog()` for correct name
- Ensure SKILL.md was successfully parsed

### Frontmatter not parsed
- Verify `---` on first line (no spaces before)
- Verify `---` on separate line (not inline)
- Check each key has colon separator
- Malformed lines are silently ignored; defaults apply

## API Design Rationale

File-based storage is used for simplicity and version control. YAML frontmatter follows standard conventions. SKILL.md naming is clear and discoverable. Catalog injection into system prompt means the LLM is always aware of available skills without needing an extra tool call. XML wrapping provides path metadata. In-memory maps provide fast O(1) lookups after the startup scan. Alphabetical sorting ensures deterministic, predictable results.

