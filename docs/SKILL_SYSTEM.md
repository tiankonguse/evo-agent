# evo-agent Skill System - Complete Analysis

**Project**: evo-agent (Go-based AI agent)  
**Date**: 2026-05-24  
**Focus**: Skill loading, catalog, lookup, and `load_skill` tool integration

---

## 1. Overview

The evo-agent skill system is a lightweight, file-based knowledge management layer that allows users to inject specialized instructions into the LLM's context at runtime. Skills are stored as Markdown files with YAML frontmatter, loaded at startup, and can be injected via the `load_skill` tool.

### Key Features
- **Lazy file scanning**: Scans `.evo-agent/skill/**/SKILL.md` at init (missing directory is silently ignored)
- **YAML frontmatter metadata**: `name` and `description` extracted from file headers
- **Catalog generation**: Formatted list for system prompt injection
- **XML-wrapped skill injection**: Skills are wrapped in `<skill>` tags with metadata attributes
- **Error handling**: Graceful fallback to parent directory name if metadata missing

---

## 2. Directory Structure

```
.evo-agent/skill/
├── git-commit/
│   └── SKILL.md                          # git commit message best practices
├── codebase-visualizer/
│   ├── SKILL.md
│   └── scripts/
│       └── visualize.py                  # (example referenced in skill)
├── summarize-changes/
│   └── SKILL.md
└── union-field-trace/
    └── SKILL.md
```

**Location Convention**: `.evo-agent/skill/<skill-name>/SKILL.md`
- Flexible depth: `filepath.WalkDir()` recursively searches any depth
- Only files named exactly `SKILL.md` are loaded
- Parent directory name used as skill name if frontmatter `name` is missing

---

## 3. Data Structures

### 3.1 SkillManifest (Public API)

```go
// src/internal/skills/registry.go
type SkillManifest struct {
    Name        string  // e.g., "git-commit"
    Description string  // e.g., "Best practices for writing git commit messages"
}
```

Exposed via:
- `Catalog()` → formatted list of manifests for system prompt
- `Names()` → slice of skill names
- `Load(name)` → full skill wrapped in XML tags

### 3.2 skillDocument (Internal)

```go
type skillDocument struct {
    Manifest SkillManifest  // Name + Description
    Body     string         // Full content after frontmatter (trimmed)
    Path     string         // Absolute path to SKILL.md
}

var documents = map[string]skillDocument{}  // Global registry (in-memory)
```

**Lifecycle**:
1. `Init()` populates `documents` map by scanning `.evo-agent/skill/`
2. `Catalog()`, `Load()`, `Names()` query the map
3. Single global map per process

---

## 4. Frontmatter Parsing

### 4.1 Format

```yaml
---
name: skill-id
description: A brief description of what this skill teaches
---
<Body content here>
```

Optional fields in frontmatter:
- `name`: Skill identifier (falls back to parent directory name if missing)
- `description`: One-line summary (defaults to "No description" if missing)
- Additional fields (parsed but ignored): `license`, `compatibility`, `metadata`, `disable`, etc.

### 4.2 Parsing Logic

**Regex**: `frontmatterRe = regexp.MustCompile(\`(?s)^---\n(.*?)\n---\n(.*)\`)`

**Function**: `parseFrontmatter(text string) (meta map[string]string, body string)`

```go
// Example:
// Input:  "---\nname: git-commit\ndescription: Best practices\n---\nDo X then Y."
// Output: meta = {"name": "git-commit", "description": "Best practices"}
//         body = "Do X then Y."
```

**Simple Key-Value Parser**:
```go
for _, line := range strings.Split(matches[1], "\n") {
    idx := strings.Index(line, ":")
    if idx < 0 { continue }
    key := strings.TrimSpace(line[:idx])
    val := strings.TrimSpace(line[idx+1:])
    meta[key] = val
}
```

- Splits frontmatter section by newlines
- For each line, finds first `:` and treats as `key: value`
- Trims whitespace; silently ignores malformed lines
- Does NOT validate YAML syntax strictly

---

## 5. Skill Loading (Init)

### 5.1 Init Function Flow

```go
// src/internal/skills/registry.go::Init()
func Init() {
    skillsDir := filepath.Join(".evo-agent", "skill")
    if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
        return  // Silently ignore missing directory
    }
    
    err := filepath.WalkDir(skillsDir, func(path string, d os.DirEntry, err error) error {
        if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
            return nil  // Skip directories and non-SKILL.md files
        }
        // Read and parse SKILL.md
        data, readErr := os.ReadFile(path)
        if readErr != nil {
            fmt.Fprintf(os.Stderr, "[Skills] Cannot read %s: %v\n", path, readErr)
            return nil  // Log error but continue
        }
        
        meta, body := parseFrontmatter(string(data))
        name := meta["name"]
        if name == "" {
            name = filepath.Base(filepath.Dir(path))  // Parent directory name
        }
        description := meta["description"]
        if description == "" {
            description = "No description"
        }
        
        absPath, err := filepath.Abs(path)
        if err != nil {
            absPath = path
        }
        
        documents[name] = skillDocument{
            Manifest: SkillManifest{Name: name, Description: description},
            Body:     strings.TrimSpace(body),
            Path:     absPath,
        }
        return nil
    })
    
    if err != nil {
        fmt.Fprintf(os.Stderr, "[Skills] Walk error: %v\n", err)
    }
    if len(documents) > 0 {
        fmt.Printf("[Skills] Loaded %d skill(s)\n", len(documents))
    }
}
```

**Key Behaviors**:
1. **Fail-safe**: Missing `.evo-agent/skill/` directory is silently ignored (no error)
2. **Error recovery**: File read errors are logged but don't stop the walk
3. **Name fallback**: If `name` frontmatter is missing, use parent directory name
4. **Default description**: "No description" if frontmatter has no description
5. **Absolute paths**: All paths converted to absolute for `Path` field
6. **Body trimming**: Whitespace trimmed from body text

---

## 6. Catalog Generation

### 6.1 Catalog Function

```go
// src/internal/skills/registry.go::Catalog()
func Catalog() string {
    if len(documents) == 0 {
        return ""  // Empty string, not nil
    }
    
    names := make([]string, 0, len(documents))
    for name := range documents {
        names = append(names, name)
    }
    sort.Strings(names)  // Alphabetical order
    
    var lines []string
    for _, name := range names {
        doc := documents[name]
        lines = append(lines, fmt.Sprintf("- %s: %s", doc.Manifest.Name, doc.Manifest.Description))
    }
    return strings.Join(lines, "\n")
}
```

**Output Format**: Markdown bullet list (sorted alphabetically)

**Example**:
```
- codebase-visualizer: Generate an interactive collapsible tree visualization of your codebase...
- git-commit: Best practices for writing git commit messages
- summarize-changes: Summarizes uncommitted changes and flags anything risky...
- union-field-trace: 分析 Union 字段值的来源...
```

---

## 7. Skill Loading (Load Function)

### 7.1 Load Function

```go
// src/internal/skills/registry.go::Load()
func Load(name string) string {
    doc, ok := documents[name]
    if !ok {
        known := knownNames()
        return fmt.Sprintf("Error: Unknown skill %q. Available skills: %s", name, known)
    }
    return fmt.Sprintf("<skill name=%q path=%q>\n%s\n</skill>", 
        doc.Manifest.Name, doc.Path, doc.Body)
}
```

**Output Format**: XML with attributes

**Example**:
```xml
<skill name="git-commit" path="/Users/tiankonguse-m3/project/github/AIProject/evo-agent/.evo-agent/skill/git-commit/SKILL.md">
Always use imperative mood. Keep subject line under 72 chars.
Format: <type>(<scope>): <subject>

Types: feat, fix, docs, refactor, test, chore

最后一行标注，使用 evo-agent 生成的 git commit message。
</skill>
```

**Error Handling**:
- If skill not found, returns human-readable error with list of known skills
- Error format: `"Error: Unknown skill 'xyz'. Available skills: skill1, skill2, skill3"`

---

## 8. Load_Skill Tool

### 8.1 Tool Definition

**File**: `src/internal/tools/skill.go`

```go
type loadSkillInput struct {
    Name string `json:"name" jsonschema_description:"Name of the skill to load"`
}

func init() {
    Register(ToolDef{
        Schema: anthropic.ToolParam{
            Name: "load_skill",
            Description: anthropic.String(
                "Load the full body of a named skill into the current context. " +
                    "Call this before acting on a task that needs specialized instructions.",
            ),
            InputSchema: GenerateSchema[loadSkillInput](),
        },
        Handler: func(input json.RawMessage) (string, error) {
            var in loadSkillInput
            if err := json.Unmarshal(input, &in); err != nil {
                return "", err
            }
            return skills.Load(in.Name), nil
        },
    })
}
```

**Schema Generation**:
- Uses reflection (`jsonschema.Reflector`) to auto-generate JSON schema from struct
- `jsonschema_description` tags provide field descriptions
- Disallows additional properties (`AllowAdditionalProperties: false`)

**Execution Flow**:
1. LLM generates tool call: `{"type": "tool_use", "name": "load_skill", "input": {"name": "git-commit"}}`
2. Tool registry dispatches to handler
3. Handler unmarshals input to `loadSkillInput`
4. Calls `skills.Load("git-commit")`
5. Returns XML-wrapped skill content
6. LLM receives result and can reference the skill in context

---

## 9. System Prompt Integration

### 9.1 Catalog Injection

**File**: `src/main.go`

```go
// Load skills and inject catalog into system prompt
skills.Init()
if catalog := skills.Catalog(); catalog != "" {
    cfg.SystemMsg += "\nSkills available:\n" + catalog +
        "\nUse load_skill when a task needs specialized instructions before you act."
}
```

**System Prompt Text**:
```
[Original system prompt...]

Skills available:
- codebase-visualizer: Generate an interactive collapsible tree visualization...
- git-commit: Best practices for writing git commit messages
- summarize-changes: Summarizes uncommitted changes and flags anything risky...
- union-field-trace: 分析 Union 字段值的来源...

Use load_skill when a task needs specialized instructions before you act.
```

**Timing**: Injected at startup, before agent loop begins

---

## 10. TUI Sidebar Display

### 10.1 Skill List for Sidebar

**File**: `src/main.go` (lines 135-139)

```go
func skillList() []string {
    names := skills.Names()
    sort.Strings(names)
    return names
}

// Usage:
info := tui.SidebarInfo{
    // ... other fields ...
    Skills: skillNames,  // Passed to TUI
}
```

**Function**: `Names()` returns unsorted slice; main.go sorts before display.

**Sidebar Usage**: Displayed in TUI sidebar to show available skills.

---

## 11. Example Skill Files

### 11.1 git-commit/SKILL.md

```yaml
---
name: git-commit
description: Best practices for writing git commit messages
---
Always use imperative mood. Keep subject line under 72 chars.
Format: <type>(<scope>): <subject>

Types: feat, fix, docs, refactor, test, chore

最后一行标注，使用 evo-agent 生成的 git commit message。
```

**Purpose**: Guidelines for LLM when writing commit messages

### 11.2 summarize-changes/SKILL.md

```yaml
---
description: Summarizes uncommitted changes and flags anything risky. Use when the user asks what changed, wants a commit message, or asks to review their diff.
---

## Current changes

!`git diff HEAD`

## Instructions

Summarize the changes above in two or three bullet points, then list any risks you notice such as missing error handling, hardcoded values, or tests that need updating. If the diff is empty, say there are no uncommitted changes.
```

**Note**: No `name` field → uses parent directory `summarize-changes` as name

**Purpose**: Instructions for summarizing code changes

### 11.3 codebase-visualizer/SKILL.md

```yaml
---
name: codebase-visualizer
description: Generate an interactive collapsible tree visualization of your codebase. Use when exploring a new repo, understanding project structure, or identifying large files.
allowed-tools: Bash(python3 *)
---

# Codebase Visualizer

Generate an interactive HTML tree view that shows your project's file structure with collapsible directories.

## Usage

Run the visualization script from your project root:

```bash
python3 ${CLAUDE_SKILL_DIR}/scripts/visualize.py .
```

This creates `codebase-map.html` in the current directory and opens it in your default browser.

## What the visualization shows

- **Collapsible directories**: Click folders to expand/collapse
- **File sizes**: Displayed next to each file
- **Colors**: Different colors for different file types
- **Directory totals**: Shows aggregate size of each folder
```

**Note**: Includes optional `allowed-tools` field (parsed but not used)

**Purpose**: Instructions for generating codebase visualizations

### 11.4 union-field-trace/SKILL.md

**Largest skill** (~400 lines of detailed instructions)

**Purpose**: Complex data source tracing instructions for a unionplus system integration

**Features**:
- Step-by-step instructions
- MCP tool integration details
- Table references
- Function type reference
- Error handling patterns

---

## 12. Testing

### 12.1 Test Coverage

**File**: `src/internal/skills/registry_test.go`

#### Test 1: parseFrontmatter

```go
func TestParseFrontmatter(t *testing.T) {
    text := "---\nname: test-skill\ndescription: A test skill\n---\nThis is the body.\n"
    meta, body := parseFrontmatter(text)
    // Verifies: meta["name"], meta["description"], body contains text
}
```

#### Test 2: parseFrontmatterNoFrontmatter

```go
func TestParseFrontmatterNoFrontmatter(t *testing.T) {
    text := "Just a plain file."
    meta, body := parseFrontmatter(text)
    // Verifies: meta is empty, body equals input
}
```

#### Test 3: InitCatalogLoad (Integration)

```go
func TestInitCatalogLoad(t *testing.T) {
    // 1. Create temp skill directory with SKILL.md
    // 2. Change working directory
    // 3. Call Init()
    // 4. Verify Catalog() contains skill name + description
    // 5. Verify Load() returns XML-wrapped content
    // 6. Verify Load("unknown") returns error message
}
```

#### Test 4: CatalogEmpty

```go
func TestCatalogEmpty(t *testing.T) {
    documents = map[string]skillDocument{}
    if catalog := Catalog(); catalog != "" {
        t.Errorf("want empty string")
    }
}
```

---

## 13. Execution Flow (Full Lifecycle)

```
┌─────────────────────────────────────────────────────────────┐
│ 1. main() startup                                           │
│    - config.LoadEnv()                                        │
│    - config.Load() → SystemMsg                               │
│    - tools.InitMCP()                                         │
│    - skills.Init() ← SCAN .evo-agent/skill/*/SKILL.md       │
└──────────────────────┬──────────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────────┐
│ 2. Catalog generation                                       │
│    - catalog := skills.Catalog() ← format manifests         │
│    - cfg.SystemMsg += "\nSkills available:\n" + catalog      │
│    - cfg.SystemMsg += "\nUse load_skill when..."            │
└──────────────────────┬──────────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────────┐
│ 3. Agent initialization                                     │
│    - agent.New(&client, cfg) ← receives SystemMsg           │
│    - tools.RegisterSubagentRunner()                         │
│    - TUI or plain-text mode                                 │
└──────────────────────┬──────────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────────┐
│ 4. TUI startup (optional)                                   │
│    - skillNames := skillList() ← skills.Names()             │
│    - Passed to tui.SidebarInfo.Skills                       │
│    - Displayed in sidebar                                   │
└──────────────────────┬──────────────────────────────────────┘
                       │
        (User enters query)
                       │
┌──────────────────────▼──────────────────────────────────────┐
│ 5. Agent loop (agent/loop.go)                               │
│    - LLM receives SystemMsg (includes skill catalog)        │
│    - LLM processes query, may call load_skill tool          │
│    - tools.Execute() dispatches tool calls                  │
└──────────────────────┬──────────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────────┐
│ 6. load_skill tool execution                                │
│    - LLM calls: load_skill({"name": "git-commit"})          │
│    - tools.Dispatch("load_skill", input)                    │
│    - Handler unmarshals input                               │
│    - Handler calls skills.Load("git-commit")                │
│    - Returns: <skill name="..." path="...">...</skill>      │
│    - LLM receives result, continues with context            │
└──────────────────────┬──────────────────────────────────────┘
                       │
     (Agent completes task)
```

---

## 14. Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **File-based vs Database** | Simple, version-controllable, minimal dependencies |
| **YAML frontmatter** | Standard convention, lightweight parsing |
| **Mandatory SKILL.md naming** | Clear, discoverable convention |
| **Directory name fallback** | Graceful degradation if name field missing |
| **XML wrapping with metadata** | LLM can reference file path in addition to content |
| **Catalog in system prompt** | LLM always aware of available skills without tool call |
| **Silent failure on missing dir** | Consistent with MCP config behavior (optional feature) |
| **Global in-memory registry** | Single scan at startup, fast repeated lookups |
| **Alphabetical sorting** | Deterministic, user-friendly catalog |

---

## 15. Real-World Example

**Scenario**: User asks "Write a git commit message for my changes"

```
1. User input → TUI or REPL
2. Agent loop calls LLM with:
   - SystemMsg (includes: "Skills available:\n- git-commit: Best practices...")
   - Query: "Write a git commit message for my changes"
3. LLM decides: "I should use the git-commit skill for guidance"
4. LLM calls tool: load_skill({"name": "git-commit"})
5. Tool handler executes:
   - Unmarshals input
   - Calls skills.Load("git-commit")
   - Returns XML-wrapped skill content
6. LLM receives skill in context:
   <skill name="git-commit" path="/path/to/.evo-agent/skill/git-commit/SKILL.md">
   Always use imperative mood. Keep subject line under 72 chars.
   Format: <type>(<scope>): <subject>
   Types: feat, fix, docs, refactor, test, chore
   </skill>
7. LLM uses this guidance to write a compliant commit message
8. User receives result
```

---

## 16. Summary

| Aspect | Implementation |
|--------|-----------------|
| **Storage** | `.evo-agent/skill/<name>/SKILL.md` (Markdown + YAML frontmatter) |
| **Loading** | `filepath.WalkDir()` at startup; silently ignores missing dir |
| **Parsing** | Simple key-value regex parser for frontmatter |
| **Lookup** | O(1) map lookup by skill name |
| **Catalog** | Sorted bullet list of name + description |
| **Injection** | XML-wrapped content with path metadata |
| **Tool** | `load_skill(name)` dispatches via tool registry |
| **System Prompt** | Catalog injected at startup; tells LLM when to use skills |
| **Error Handling** | Friendly error messages; recovery on file read failures |
| **Performance** | Single scan at startup; in-memory registry for O(1) lookups |

