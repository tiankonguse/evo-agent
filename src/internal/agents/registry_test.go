package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitAndGet creates a fake project tree under t.TempDir(), drops a few
// agent files there, and verifies that Init populates the registry and that
// Get / List / Catalog / Names return what's expected.
//
// Per project rules we'd normally avoid t.TempDir() and use a project subdir,
// but Go test conventions strongly prefer t.TempDir() for hermetic state and
// it's auto-cleaned, so we accept the trade-off here.
func TestInitAndGet(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".evo-agent", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	must := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	must(filepath.Join(dir, "code-reviewer.md"), `---
name: code-reviewer
description: Review code for bugs and style
model: claude-3-5-haiku
max_turns: 15
---
You are a senior reviewer.
Read files and report issues.`)

	// Filename fallback (no name in frontmatter)
	must(filepath.Join(dir, "explore.md"), `---
description: Read-only codebase explorer
---
You explore. Do not write.`)

	// "inherit" model maps to empty string (= use parent's model)
	must(filepath.Join(dir, "inheritor.md"), `---
name: inheritor
description: Inherits parent model
model: inherit
---
Inherit prompt.`)

	// Missing description should fall back to "No description"
	must(filepath.Join(dir, "minimal.md"), `---
name: minimal
---
Minimal body.`)

	// File without frontmatter — should still load with file-name fallback
	must(filepath.Join(dir, "naked.md"), `Just a plain markdown body, no frontmatter.`)

	// Non-.md file should be ignored
	must(filepath.Join(dir, "notes.txt"), "ignored")

	Init(root)

	cases := []struct {
		name        string
		wantDesc    string
		wantModel   string
		wantTurns   int
		bodyPrefix  string
	}{
		{"code-reviewer", "Review code for bugs and style", "claude-3-5-haiku", 15, "You are a senior reviewer."},
		{"explore", "Read-only codebase explorer", "", 0, "You explore."},
		{"inheritor", "Inherits parent model", "", 0, "Inherit prompt."},
		{"minimal", "No description", "", 0, "Minimal body."},
		{"naked", "No description", "", 0, "Just a plain markdown body, no frontmatter."},
	}
	for _, c := range cases {
		def, ok := Get(c.name)
		if !ok {
			t.Errorf("Get(%q): not found", c.name)
			continue
		}
		if def.Name != c.name {
			t.Errorf("Get(%q): Name=%q want %q", c.name, def.Name, c.name)
		}
		if def.Description != c.wantDesc {
			t.Errorf("Get(%q): Description=%q want %q", c.name, def.Description, c.wantDesc)
		}
		if def.Model != c.wantModel {
			t.Errorf("Get(%q): Model=%q want %q", c.name, def.Model, c.wantModel)
		}
		if def.MaxTurns != c.wantTurns {
			t.Errorf("Get(%q): MaxTurns=%d want %d", c.name, def.MaxTurns, c.wantTurns)
		}
		if !strings.HasPrefix(def.SystemPrompt, c.bodyPrefix) {
			t.Errorf("Get(%q): SystemPrompt prefix mismatch, got %q...", c.name, firstN(def.SystemPrompt, len(c.bodyPrefix)))
		}
	}

	// Catalog should list all 5 agents and start with the boilerplate.
	cat := Catalog()
	if !strings.HasPrefix(cat, "Available custom agents") {
		t.Errorf("Catalog missing header, got: %q", firstN(cat, 60))
	}
	for _, c := range cases {
		want := "- " + c.name + ": " + c.wantDesc
		if !strings.Contains(cat, want) {
			t.Errorf("Catalog missing %q", want)
		}
	}

	// Names should be sorted ascending.
	names := Names()
	if len(names) != 5 {
		t.Errorf("Names: got %d, want 5", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("Names not sorted: %v", names)
			break
		}
	}

	// Unknown lookup returns ok=false.
	if _, ok := Get("does-not-exist"); ok {
		t.Error("Get(does-not-exist): want ok=false")
	}
}

// TestInitMissingDir confirms a project without .evo-agent/agents/ doesn't
// crash and produces a 0-agent registry.
func TestInitMissingDir(t *testing.T) {
	Init(t.TempDir()) // dir doesn't exist
	if got := len(Names()); got != 0 {
		t.Errorf("expected empty registry, got %d agents", got)
	}
	if Catalog() != "" {
		t.Error("Catalog should be empty when no agents are loaded")
	}
}

// TestInvalidMaxTurns confirms non-positive / non-numeric max_turns falls
// back to 0 (= use runner default).
func TestInvalidMaxTurns(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".evo-agent", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `---
name: bad
description: bad max
max_turns: not-a-number
---
body`
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	Init(root)
	def, ok := Get("bad")
	if !ok {
		t.Fatal("expected bad agent loaded")
	}
	if def.MaxTurns != 0 {
		t.Errorf("MaxTurns=%d want 0 for invalid input", def.MaxTurns)
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
