package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	text := "---\nname: test-skill\ndescription: A test skill\n---\nThis is the body.\n"
	meta, body := parseFrontmatter(text)
	if meta["name"] != "test-skill" {
		t.Errorf("name = %q, want %q", meta["name"], "test-skill")
	}
	if meta["description"] != "A test skill" {
		t.Errorf("description = %q, want %q", meta["description"], "A test skill")
	}
	if !strings.Contains(body, "This is the body.") {
		t.Errorf("body = %q, missing expected content", body)
	}
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	text := "Just a plain file."
	meta, body := parseFrontmatter(text)
	if len(meta) != 0 {
		t.Errorf("expected empty meta, got %v", meta)
	}
	if body != text {
		t.Errorf("body = %q, want %q", body, text)
	}
}

func TestInitCatalogLoad(t *testing.T) {
	// Set up a temp skill directory
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".evo-agent", "skill", "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: my-skill\ndescription: Does something useful\n---\nDo X then Y.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Change working directory to temp dir so Init() finds .evo-agent/skill/
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	// Reset global state
	documents = map[string]skillDocument{}
	Init()

	catalog := Catalog()
	if !strings.Contains(catalog, "my-skill") {
		t.Errorf("Catalog() = %q, want to contain 'my-skill'", catalog)
	}
	if !strings.Contains(catalog, "Does something useful") {
		t.Errorf("Catalog() = %q, want description", catalog)
	}

	result := Load("my-skill")
	if !strings.Contains(result, "<skill") {
		t.Errorf("Load() = %q, want XML skill tag", result)
	}
	if !strings.Contains(result, "Do X then Y.") {
		t.Errorf("Load() = %q, want body content", result)
	}
	if !strings.Contains(result, "path=") {
		t.Errorf("Load() = %q, want path attribute", result)
	}
	if !strings.Contains(result, "SKILL.md") {
		t.Errorf("Load() = %q, want SKILL.md in path", result)
	}

	unknown := Load("does-not-exist")
	if !strings.Contains(unknown, "Error") {
		t.Errorf("Load(unknown) = %q, want error message", unknown)
	}
}

func TestCatalogEmpty(t *testing.T) {
	documents = map[string]skillDocument{}
	if catalog := Catalog(); catalog != "" {
		t.Errorf("Catalog() = %q, want empty string when no skills loaded", catalog)
	}
}
