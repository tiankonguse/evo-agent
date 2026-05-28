package prompt

import (
	"runtime"
	"strings"
	"testing"

	"evo-agent/internal/config"
)

// ── Mock providers ───────────────────────────────────────────────────────────

type mockMemory struct {
	prompt string
}

func (m *mockMemory) LoadPrompt() string { return m.prompt }

type mockSkills struct {
	catalog    string
	slashNames []string
}

func (m *mockSkills) Catalog() string      { return m.catalog }
func (m *mockSkills) SlashNames() []string { return m.slashNames }


// ── Helpers ──────────────────────────────────────────────────────────────────

func testCfg(projectDir, modelID string) *config.Config {
	return &config.Config{
		ProjectDir: projectDir,
		ModelID:    modelID,
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestBuild_IntroSection(t *testing.T) {
	cfg := testCfg("/home/user/project", "test-model")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if !strings.Contains(result, "interactive agent") {
		t.Error("Build() should contain intro instructions")
	}
}

func TestBuild_SystemSection(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if !strings.Contains(result, "# System") {
		t.Error("Build() should contain System section header")
	}
	if !strings.Contains(result, "automatically compress") {
		t.Error("Build() should mention context compression")
	}
}

func TestBuild_DoingTasksSection(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if !strings.Contains(result, "# Doing tasks") {
		t.Error("Build() should contain Doing tasks section")
	}
	if !strings.Contains(result, "software engineering tasks") {
		t.Error("Build() should describe task types")
	}
}

func TestBuild_ActionsSection(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if !strings.Contains(result, "# Executing actions with care") {
		t.Error("Build() should contain Actions section")
	}
	if !strings.Contains(result, "reversibility") {
		t.Error("Build() should mention reversibility")
	}
}

func TestBuild_ToolUsageSection(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if !strings.Contains(result, "# Using your tools") {
		t.Error("Build() should contain tool usage section")
	}
	if !strings.Contains(result, "read_file instead of cat") {
		t.Error("Build() should guide tool preferences")
	}
	if !strings.Contains(result, "parallel") {
		t.Error("Build() should mention parallel tool calls")
	}
}

func TestBuild_ToneStyleSection(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if !strings.Contains(result, "# Tone and style") {
		t.Error("Build() should contain tone/style section")
	}
	if !strings.Contains(result, "concise") {
		t.Error("Build() should mention conciseness")
	}
}

func TestBuild_AgentMd(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	b.SetAgentMd("This project uses Go.")
	result := b.Build()

	if !strings.Contains(result, "# Project Guidance (Agent.md)") {
		t.Error("Build() should contain Agent.md header")
	}
	if !strings.Contains(result, "This project uses Go.") {
		t.Error("Build() should contain Agent.md content")
	}
}

func TestBuild_AgentMd_Empty(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if strings.Contains(result, "Project Guidance") {
		t.Error("Build() should NOT contain Agent.md section when content is empty")
	}
}

func TestBuild_Memories(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	mem := &mockMemory{prompt: "# Memories (persistent across sessions)\n\n## [user]\n### pref: likes tabs"}
	b := NewBuilder(cfg, mem, nil)
	result := b.Build()

	if !strings.Contains(result, "Memories (persistent across sessions)") {
		t.Error("Build() should contain memory section")
	}
	if !strings.Contains(result, "likes tabs") {
		t.Error("Build() should contain memory content")
	}
}

func TestBuild_MemoryGuidance(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	b.SetMemoryGuidance("## Memory guidance\nWhen to save memories...")
	result := b.Build()

	if !strings.Contains(result, "Memory guidance") {
		t.Error("Build() should contain memory guidance")
	}
}

func TestBuild_SkillsCatalog(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	sk := &mockSkills{catalog: "- git-commit: Commit with conventional format"}
	b := NewBuilder(cfg, nil, sk)
	result := b.Build()

	if !strings.Contains(result, "Skills available:") {
		t.Error("Build() should contain skills header")
	}
	if !strings.Contains(result, "git-commit: Commit with conventional format") {
		t.Error("Build() should contain skill listing")
	}
	if !strings.Contains(result, "Use load_skill when") {
		t.Error("Build() should contain load_skill hint")
	}
}

func TestBuild_SkillsCatalog_Empty(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	sk := &mockSkills{catalog: ""}
	b := NewBuilder(cfg, nil, sk)
	result := b.Build()

	if strings.Contains(result, "Skills available:") {
		t.Error("Build() should NOT contain skills section when catalog is empty")
	}
}

func TestBuild_SlashCommands(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	sk := &mockSkills{slashNames: []string{"git-commit", "init"}}
	b := NewBuilder(cfg, nil, sk)
	result := b.Build()

	if !strings.Contains(result, "# Slash commands") {
		t.Error("Build() should contain slash command intro")
	}
}

func TestBuild_SlashCommands_Empty(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	sk := &mockSkills{slashNames: []string{}}
	b := NewBuilder(cfg, nil, sk)
	result := b.Build()

	if strings.Contains(result, "# Slash commands") {
		t.Error("Build() should NOT contain slash commands when no slash names")
	}
}

func TestBuild_DynamicBoundary(t *testing.T) {
	cfg := testCfg("/tmp", "test-model")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if !strings.Contains(result, DynamicBoundary) {
		t.Error("Build() should contain DYNAMIC_BOUNDARY marker")
	}
}

func TestBuild_Environment(t *testing.T) {
	cfg := testCfg("/home/user/proj", "claude-sonnet-4-20250514")
	b := NewBuilder(cfg, nil, nil)
	result := b.Build()

	if !strings.Contains(result, "# Environment") {
		t.Error("Build() should contain environment header")
	}
	if !strings.Contains(result, "Working directory: /home/user/proj") {
		t.Error("Build() should contain working directory")
	}
	if !strings.Contains(result, "You are powered by the model claude-sonnet-4-20250514") {
		t.Error("Build() should contain model name")
	}
	expected := runtime.GOOS + "/" + runtime.GOARCH
	if !strings.Contains(result, "Platform: "+expected) {
		t.Errorf("Build() should contain platform %s", expected)
	}
	if !strings.Contains(result, "Is git repository:") {
		t.Error("Build() should contain git status")
	}
	if !strings.Contains(result, "Shell:") {
		t.Error("Build() should contain shell info")
	}
}


func TestBuild_SectionOrder(t *testing.T) {
	cfg := testCfg("/tmp", "m")
	mem := &mockMemory{prompt: "MEMORY_SECTION_MARKER"}
	sk := &mockSkills{catalog: "SKILLS_SECTION_MARKER", slashNames: []string{"test"}}
	b := NewBuilder(cfg, mem, sk)
	b.SetAgentMd("AGENTMD_SECTION_MARKER")
	b.SetMemoryGuidance("GUIDANCE_SECTION_MARKER")
	result := b.Build()

	// Verify static sections come before boundary, dynamic after
	idxIntro := strings.Index(result, "interactive agent")
	idxSystem := strings.Index(result, "# System")
	idxDoingTasks := strings.Index(result, "# Doing tasks")
	idxActions := strings.Index(result, "# Executing actions")
	idxToolUsage := strings.Index(result, "# Using your tools")
	idxTone := strings.Index(result, "# Tone and style")
	idxOutput := strings.Index(result, "# Output efficiency")
	idxAgent := strings.Index(result, "AGENTMD_SECTION_MARKER")
	idxMem := strings.Index(result, "MEMORY_SECTION_MARKER")
	idxGuidance := strings.Index(result, "GUIDANCE_SECTION_MARKER")
	idxSkills := strings.Index(result, "SKILLS_SECTION_MARKER")
	idxSlash := strings.Index(result, "# Slash commands")
	idxBoundary := strings.Index(result, DynamicBoundary)
	idxEnv := strings.Index(result, "# Environment")

	// Static ordering
	if idxIntro >= idxSystem {
		t.Error("Intro should come before System")
	}
	if idxSystem >= idxDoingTasks {
		t.Error("System should come before Doing tasks")
	}
	if idxDoingTasks >= idxActions {
		t.Error("Doing tasks should come before Actions")
	}
	if idxActions >= idxToolUsage {
		t.Error("Actions should come before Tool usage")
	}
	if idxToolUsage >= idxTone {
		t.Error("Tool usage should come before Tone")
	}
	if idxTone >= idxOutput {
		t.Error("Tone should come before Output efficiency")
	}
	if idxOutput >= idxSlash {
		t.Error("Output efficiency should come before Slash commands")
	}
	if idxSlash >= idxGuidance {
		t.Error("Slash commands should come before guidance")
	}
	if idxGuidance >= idxBoundary {
		t.Error("Guidance should come before boundary")
	}

	// Boundary separates static from dynamic
	if idxBoundary >= idxAgent {
		t.Error("Boundary should come before Agent.md")
	}
	if idxAgent >= idxSkills {
		t.Error("Agent.md should come before Skills")
	}
	if idxSkills >= idxMem {
		t.Error("Skills should come before memories")
	}
	if idxMem >= idxEnv {
		t.Error("Memories should come before Environment")
	}
}

func TestBuildSections_ReturnsArray(t *testing.T) {
	cfg := testCfg("/tmp", "m")
	b := NewBuilder(cfg, nil, nil)
	sections := b.BuildSections()

	if len(sections) < 5 {
		t.Errorf("BuildSections() should return multiple sections, got %d", len(sections))
	}

	// Should contain the boundary marker as a standalone section
	found := false
	for _, s := range sections {
		if s == DynamicBoundary {
			found = true
			break
		}
	}
	if !found {
		t.Error("BuildSections() should contain DynamicBoundary as a standalone element")
	}
}
