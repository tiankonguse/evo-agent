package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatchNotSlash(t *testing.T) {
	result := Dispatch("hello world")
	if result.Found {
		t.Error("Dispatch should return Found=false for non-slash input")
	}
}

func TestDispatchFilePath(t *testing.T) {
	// /path/to/file should NOT trigger slash dispatch
	result := Dispatch("/usr/bin/env")
	if result.Found {
		t.Error("Dispatch should return Found=false for file paths")
	}
}

func TestDispatchUnknown(t *testing.T) {
	// Reset state
	skillDocuments = map[string]skillDocument{}
	commandDocuments = map[string]skillDocument{}

	result := Dispatch("/nonexistent")
	if !result.Found {
		t.Error("Dispatch should return Found=true for unknown slash command (with error content)")
	}
	if !strings.Contains(result.Prompt, "Error") {
		t.Errorf("Dispatch unknown: got Prompt=%q, want error message", result.Prompt)
	}
	if result.Content != "" {
		t.Errorf("Dispatch unknown: Content should be empty, got %q", result.Content)
	}
}

func TestDispatchSkill(t *testing.T) {
	// Set up a test skill
	skillDocuments = map[string]skillDocument{
		"greet": {
			Manifest: SkillManifest{
				Name:          "greet",
				Description:   "Greet someone",
				ArgumentHint:  "[name]",
				Arguments:     []string{"name"},
				IsCommand:     false,
				UserInvocable: true,
			},
			Body: "Say hello to $name.",
			Path: "/test/path",
		},
	}
	commandDocuments = map[string]skillDocument{}

	result := Dispatch("/greet World")
	if !result.Found {
		t.Error("Dispatch should find the skill")
	}
	if result.Name != "greet" {
		t.Errorf("Dispatch name = %q, want 'greet'", result.Name)
	}
	if !strings.Contains(result.Prompt, "/greet") {
		t.Errorf("Dispatch Prompt = %q, want mention of /greet", result.Prompt)
	}
	if !strings.Contains(result.Prompt, "World") {
		t.Errorf("Dispatch Prompt = %q, want mention of arguments", result.Prompt)
	}
	if !strings.Contains(result.Content, "Say hello to World.") {
		t.Errorf("Dispatch Content = %q, want substituted body", result.Content)
	}
	if !strings.Contains(result.Content, `<skill name="greet"`) {
		t.Errorf("Dispatch Content = %q, want XML wrapper", result.Content)
	}
}

func TestDispatchNoArgs(t *testing.T) {
	commandDocuments = map[string]skillDocument{
		"help": {
			Manifest: SkillManifest{
				Name:          "help",
				Description:   "Show help",
				IsCommand:     true,
				UserInvocable: true,
			},
			Body: "List all available commands.",
			Path: "/test/path",
		},
	}
	skillDocuments = map[string]skillDocument{}

	result := Dispatch("/help")
	if !result.Found {
		t.Error("Dispatch should find the command")
	}
	if !strings.Contains(result.Prompt, "/help") {
		t.Errorf("Dispatch Prompt = %q, want mention of /help", result.Prompt)
	}
	if !strings.Contains(result.Content, "List all available commands.") {
		t.Errorf("Dispatch Content = %q, want skill body", result.Content)
	}
}

func TestDispatchCommand(t *testing.T) {
	// Set up temp command directory
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, ".evo-agent", "command")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: deploy\ndescription: Deploy to production\nargument-hint: [env]\narguments: env\n---\nDeploy to $env environment.\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "deploy.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to temp dir and reload
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	skillDocuments = map[string]skillDocument{}
	commandDocuments = map[string]skillDocument{}
	InitCommands()

	// Verify command was loaded (includes built-in commands)
	if _, ok := commandDocuments["deploy"]; !ok {
		t.Fatal("expected 'deploy' command to be loaded")
	}

	// Test dispatch
	result := Dispatch("/deploy staging")
	if !result.Found {
		t.Error("Dispatch should find the command")
	}
	if !strings.Contains(result.Content, "Deploy to staging environment.") {
		t.Errorf("Dispatch command Content = %q", result.Content)
	}
	if !strings.Contains(result.Prompt, "/deploy") {
		t.Errorf("Dispatch command Prompt = %q, want mention of /deploy", result.Prompt)
	}

	// Verify command is NOT in catalog
	catalog := Catalog()
	if strings.Contains(catalog, "deploy") {
		t.Errorf("Command should not appear in catalog, got %q", catalog)
	}
}

func TestDispatchCommandPriority(t *testing.T) {
	// Same name in both skill and command — command takes priority for slash
	skillDocuments = map[string]skillDocument{
		"build": {
			Manifest: SkillManifest{
				Name:          "build",
				Description:   "Build skill (model)",
				IsCommand:     false,
				UserInvocable: true,
			},
			Body: "Skill body for build.",
			Path: "/skill/path",
		},
	}
	commandDocuments = map[string]skillDocument{
		"build": {
			Manifest: SkillManifest{
				Name:          "build",
				Description:   "Build command (user)",
				IsCommand:     true,
				UserInvocable: true,
			},
			Body: "Command body for build.",
			Path: "/command/path",
		},
	}

	// Slash dispatch should use command version
	result := Dispatch("/build")
	if !result.Found {
		t.Error("Dispatch should find /build")
	}
	if !strings.Contains(result.Content, "Command body for build.") {
		t.Errorf("Dispatch should use command body, got Content = %q", result.Content)
	}

	// load_skill should still use skill version
	loaded := Load("build")
	if !strings.Contains(loaded, "Skill body for build.") {
		t.Errorf("Load should use skill body, got %q", loaded)
	}
}
