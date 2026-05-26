package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	memorySubdir    = ".evo-agent/memory"
	memoryIndexFile = "MEMORY.md"
	maxIndexLines   = 200
)

// MemoryGuidance is injected into the system prompt to guide the agent on
// when to proactively call the remember tool.
const MemoryGuidance = `
## Memory guidance

When to save memories (use the remember tool):
- User states a preference ("I like tabs", "always use pytest") → type: user
- User corrects your approach ("don't do X", "that was wrong because...") → type: feedback
- You learn a project fact NOT easily inferred from current code alone
  (e.g. a rule exists for compliance reasons, or a legacy module must stay untouched
  for business reasons) → type: project
- You learn where an external resource lives (ticket board, dashboard, docs URL)
  → type: reference

When NOT to save:
- Anything easily derivable from code (function signatures, file structure, directory layout)
- Temporary task state (current branch, open PR numbers, current TODOs)
- Secrets or credentials (API keys, passwords)
- Git history or recent changes — git log / git blame are authoritative
- Debugging solutions — the fix is in the code; the commit message has the context
`

// memoryEntry is the internal representation of a persistent memory.
type memoryEntry struct {
	Name        string
	Description string
	Type        string
	Content     string
	File        string
}

// MemoryManager is the process-wide persistent memory store.
// It loads memories from disk at startup and injects them into the system prompt.
// Memory writes are performed by a subagent via read_file/write_file/edit_file.
type MemoryManager struct {
	mu       sync.RWMutex
	dir      string // resolved .evo-agent/memory/ path
	memories map[string]memoryEntry
}

// GlobalMemory is the process-wide memory manager singleton.
var GlobalMemory = &MemoryManager{memories: make(map[string]memoryEntry)}

// conversationMu protects currentConversation from concurrent access.
// The agent loop sets it before Execute() so the remember tool can read it.
var (
	conversationMu      sync.RWMutex
	currentConversation []anthropic.MessageParam
)

// SetConversationMessages stores the current conversation messages.
// Called by the agent loop before Execute() so the remember tool has context.
func SetConversationMessages(msgs []anthropic.MessageParam) {
	conversationMu.Lock()
	currentConversation = msgs
	conversationMu.Unlock()
}

// getConversationMessages returns the current conversation messages.
func getConversationMessages() []anthropic.MessageParam {
	conversationMu.RLock()
	defer conversationMu.RUnlock()
	return currentConversation
}

// frontmatterRe matches YAML frontmatter at the top of a file.
var memoryFrontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n(.*)`)

// Init sets the memory directory based on projectDir and loads existing memories.
func (m *MemoryManager) Init(projectDir string) {
	m.mu.Lock()
	m.dir = filepath.Join(projectDir, memorySubdir)
	m.mu.Unlock()

	// Ensure memory directory exists
	os.MkdirAll(m.dir, 0o755)

	m.LoadAll()
}

// Dir returns the resolved memory directory path.
func (m *MemoryManager) Dir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dir
}

// LoadAll scans *.md files (except MEMORY.md) and parses their frontmatter.
func (m *MemoryManager) LoadAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.memories = make(map[string]memoryEntry)

	if _, err := os.Stat(m.dir); os.IsNotExist(err) {
		return
	}

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Name() == memoryIndexFile {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			continue
		}
		meta, body := parseMemoryFrontmatter(string(data))
		if meta == nil {
			continue
		}
		name := meta["name"]
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".md")
		}
		m.memories[name] = memoryEntry{
			Name:        name,
			Description: meta["description"],
			Type:        meta["type"],
			Content:     body,
			File:        entry.Name(),
		}
	}

	if len(m.memories) > 0 {
		fmt.Printf("[Memory] Loaded %d memory(s)\n", len(m.memories))
	}
}

// LoadPrompt formats all memories for injection into the system prompt.
// Returns empty string when no memories exist (no noise in prompt).
func (m *MemoryManager) LoadPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.memories) == 0 {
		return ""
	}

	var sections []string
	sections = append(sections, "# Memories (persistent across sessions)")
	sections = append(sections, "")

	memoryTypes := []string{"user", "feedback", "project", "reference"}

	// Group by type for readability
	for _, memType := range memoryTypes {
		var typed []memoryEntry
		for _, mem := range m.memories {
			if mem.Type == memType {
				typed = append(typed, mem)
			}
		}
		if len(typed) == 0 {
			continue
		}
		// Sort by name for stable output
		sort.Slice(typed, func(i, j int) bool { return typed[i].Name < typed[j].Name })

		sections = append(sections, fmt.Sprintf("## [%s]", memType))
		for _, mem := range typed {
			sections = append(sections, fmt.Sprintf("### %s: %s", mem.Name, mem.Description))
			if strings.TrimSpace(mem.Content) != "" {
				sections = append(sections, strings.TrimSpace(mem.Content))
			}
			sections = append(sections, "")
		}
	}

	return strings.Join(sections, "\n")
}

// ExistingMemoriesList returns a formatted list of existing memory files for the subagent prompt.
func (m *MemoryManager) ExistingMemoriesList() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.memories) == 0 {
		return ""
	}

	var lines []string
	names := make([]string, 0, len(m.memories))
	for name := range m.memories {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		mem := m.memories[name]
		lines = append(lines, fmt.Sprintf("- %s (%s) [%s]: %s", mem.File, mem.Name, mem.Type, mem.Description))
	}
	return strings.Join(lines, "\n")
}

// parseMemoryFrontmatter parses --- delimited frontmatter + body content.
func parseMemoryFrontmatter(text string) (map[string]string, string) {
	matches := memoryFrontmatterRe.FindStringSubmatch(text)
	if matches == nil {
		return nil, ""
	}
	header, body := matches[1], matches[2]
	meta := make(map[string]string)
	for _, line := range strings.Split(header, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		meta[key] = val
	}
	return meta, strings.TrimSpace(body)
}

// ── Extraction prompt (based on refs.ts) ─────────────────────────────────────

func buildExtractionPrompt(memoryDir string, existingMemories string) string {
	manifest := ""
	if existingMemories != "" {
		manifest = "\n\n## Existing memory files\n\n" + existingMemories +
			"\n\nCheck this list before writing — update an existing file rather than creating a duplicate."
	}

	return fmt.Sprintf(`You are acting as the memory extraction subagent. Analyze the conversation context and use the available tools to update persistent memory files.

Memory directory: %s

Available tools: read_file, write_file, edit_file, bash (read-only: ls/find/cat/stat only).
You have a limited turn budget. The efficient strategy is: turn 1 — read all files you might update in parallel; turn 2 — write/edit all files in parallel. Do not interleave reads and writes across multiple turns.%s

## Types of memory

There are four discrete types of memory you can store:

<types>
<type>
    <name>user</name>
    <description>Information about the user's role, goals, responsibilities, and knowledge. Great user memories help tailor future behavior to the user's preferences and perspective.</description>
    <when_to_save>When you learn details about the user's role, preferences, responsibilities, or knowledge.</when_to_save>
    <examples>
    - user is a data scientist, currently focused on observability/logging
    - deep Go expertise, new to React — frame frontend explanations in terms of backend analogues
    </examples>
</type>
<type>
    <name>feedback</name>
    <description>Guidance the user has given about how to approach work — both what to avoid and what to keep doing. Record from failure AND success: if you only save corrections, you will avoid past mistakes but drift away from validated approaches.</description>
    <when_to_save>Any time the user corrects your approach OR confirms a non-obvious approach worked. Include *why* so you can judge edge cases later.</when_to_save>
    <body_structure>Lead with the rule itself, then a **Why:** line and a **How to apply:** line.</body_structure>
    <examples>
    - integration tests must hit a real database, not mocks. Why: prior incident where mock/prod divergence masked a broken migration. How to apply: never mock DB in test files under integration/
    - user wants terse responses with no trailing summaries
    </examples>
</type>
<type>
    <name>project</name>
    <description>Information about ongoing work, goals, initiatives, bugs, or incidents within the project that is not derivable from code or git history. Helps understand broader context and motivation.</description>
    <when_to_save>When you learn who is doing what, why, or by when. Convert relative dates to absolute dates.</when_to_save>
    <body_structure>Lead with the fact or decision, then a **Why:** line and a **How to apply:** line.</body_structure>
    <examples>
    - merge freeze begins 2026-03-05 for mobile release cut
    - auth middleware rewrite is driven by legal/compliance, not tech-debt — scope decisions should favor compliance over ergonomics
    </examples>
</type>
<type>
    <name>reference</name>
    <description>Pointers to where information can be found in external systems. Allows remembering where to look for up-to-date information outside the project directory.</description>
    <when_to_save>When you learn about resources in external systems and their purpose.</when_to_save>
    <examples>
    - pipeline bugs are tracked in Linear project "INGEST"
    - grafana.internal/d/api-latency is the oncall latency dashboard
    </examples>
</type>
</types>

## What NOT to save in memory

- Code patterns, conventions, architecture, file paths, or project structure — these can be derived by reading the current project state.
- Git history, recent changes, or who-changed-what — git log / git blame are authoritative.
- Debugging solutions or fix recipes — the fix is in the code; the commit message has the context.
- Ephemeral task details: in-progress work, temporary state, current conversation context.
- Secrets or credentials.

These exclusions apply even when the user explicitly asks you to save. If they ask you to save a PR list or activity summary, save only what was *surprising* or *non-obvious* about it.

## How to save memories

Saving a memory is a two-step process:

**Step 1** — Write the memory to its own file (e.g., user_role.md, feedback_testing.md) using this frontmatter format:

`+"```"+`markdown
---
name: {{memory name}}
description: {{one-line description — used to decide relevance in future conversations, so be specific}}
type: {{user, feedback, project, reference}}
---

{{memory content — for feedback/project types, structure as: rule/fact, then **Why:** and **How to apply:** lines}}
`+"```"+`

**Step 2** — Update MEMORY.md index. MEMORY.md is an index, not a memory — each entry should be one line, under ~150 characters: `+"`- [Title](file.md) — one-line hook`"+`. It has no frontmatter. Never write memory content directly into MEMORY.md.

Rules:
- MEMORY.md is always loaded into the system prompt — lines after 200 will be truncated, so keep it concise
- Organize memory semantically by topic, not chronologically
- Update or remove memories that turn out to be wrong or outdated
- Do not write duplicate memories — update an existing file rather than creating a new one
- Use read_file to check existing files before deciding to create vs update

## Before recommending from memory

A memory that names a specific function, file, or flag is a claim that it existed *when the memory was written*. It may have been renamed, removed, or never merged. Before recommending it: verify it still exists.

Now analyze the conversation and extract any memories worth persisting. If nothing warrants saving, respond with "No new memories to save."
`, memoryDir, manifest)
}

// ── Consolidation prompt ─────────────────────────────────────────────────────

func buildConsolidatePrompt(memoryDir string, existingMemories string) string {
	return fmt.Sprintf(`Memory consolidation subagent. Your job is to clean up, merge, and prune the memory store.

Memory directory: %s
Tools: read_file, write_file, edit_file, bash (including rm to delete files).

## Current memories

%s

## Tasks

1. **Read** all memory files to understand their full content.
2. **Merge** memories that cover the same topic into a single file. Keep the best name and combine content.
3. **Delete** memories that are stale, outdated, no longer relevant, or superseded by newer ones. Use bash rm to remove files.
4. **Rewrite** memories whose content is unclear or poorly structured. Use the standard frontmatter format.
5. **Rebuild** MEMORY.md index after all changes — one line per remaining memory.

## Rules

- Each memory file uses frontmatter: name, description, type (user/feedback/project/reference)
- MEMORY.md is an index only — one line per entry, no content
- After merging, delete the redundant source files
- If a memory contradicts a newer one, keep only the newer
- Summarize what you changed at the end
`, memoryDir, existingMemories)
}

// ── Tool registration ────────────────────────────────────────────────────────

type rememberInput struct {
	Hint string `json:"hint,omitempty" jsonschema_description:"Optional hint about what to remember (e.g. 'save user preferences from this conversation'). Leave empty for automatic extraction."`
}

type consolidateInput struct {
	Focus string `json:"focus,omitempty" jsonschema_description:"Optional focus area (e.g. 'merge all feedback memories', 'remove stale project memories'). Leave empty for full consolidation."`
}

func init() {
	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "remember",
			Description: anthropic.String(
				"Spawn a memory extraction subagent that analyzes the conversation and " +
					"persists important information to .evo-agent/memory/ for future sessions. " +
					"Saves user preferences, feedback/corrections, non-obvious project facts, " +
					"and external resource pointers. Call this when the user says /remember " +
					"or when you detect information worth persisting across sessions.",
			),
			InputSchema: GenerateSchema[rememberInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in rememberInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}

			if subagentRunner == nil {
				return "Error: subagent runner not initialized", nil
			}

			memDir := GlobalMemory.Dir()
			if memDir == "" {
				return "Error: memory system not initialized", nil
			}

			// Build the extraction system prompt
			existing := GlobalMemory.ExistingMemoriesList()
			sysPrompt := buildExtractionPrompt(memDir, existing)

			// Build messages: conversation history + extraction instruction
			messages := getConversationMessages()
			// Append a user message with explicit tool-use instruction
			trigger := "[MEMORY EXTRACTION TASK] You MUST use write_file to save memories to " + memDir + "/. " +
				"Do NOT reply conversationally. Analyze the conversation above and write .md files. "
			if in.Hint != "" {
				trigger += "Focus on: " + in.Hint
			}
			subMessages := make([]anthropic.MessageParam, len(messages))
			copy(subMessages, messages)
			subMessages = append(subMessages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(trigger),
			))

			// Spawn the memory subagent with system prompt + conversation messages
			result := subagentRunner(sysPrompt, subMessages)

			// Reload memories after subagent finishes writing
			GlobalMemory.LoadAll()

			return result, nil
		},
	})

	Register(ToolDef{
		Schema: anthropic.ToolParam{
			Name: "consolidate_memory",
			Description: anthropic.String(
				"Spawn a subagent to consolidate memories: merge duplicates, " +
					"remove stale/outdated entries, and reorganize the memory index. " +
					"Call this when memories accumulate and need cleanup.",
			),
			InputSchema: GenerateSchema[consolidateInput](),
		},
		Handler: func(input json.RawMessage) (string, error) {
			var in consolidateInput
			if err := json.Unmarshal(input, &in); err != nil {
				return "", err
			}

			if subagentRunner == nil {
				return "Error: subagent runner not initialized", nil
			}

			memDir := GlobalMemory.Dir()
			if memDir == "" {
				return "Error: memory system not initialized", nil
			}

			existing := GlobalMemory.ExistingMemoriesList()
			if existing == "" {
				return "No memories to consolidate.", nil
			}

			sysPrompt := buildConsolidatePrompt(memDir, existing)

			trigger := "[MEMORY CONSOLIDATION TASK] You MUST use read_file, write_file, edit_file, and bash (rm) to consolidate memories in " + memDir + "/. " +
				"Do NOT reply conversationally. Read all files, then merge/delete/rewrite as needed. "
			if in.Focus != "" {
				trigger += "Focus on: " + in.Focus
			}
			subMessages := []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(trigger)),
			}

			result := subagentRunner(sysPrompt, subMessages)

			// Reload memories after consolidation
			GlobalMemory.LoadAll()

			return result, nil
		},
	})
}
