package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SkillManifest holds the metadata for a single skill (cheap to keep in memory).
type SkillManifest struct {
	Name        string
	Description string
}

// skillDocument bundles the manifest with the full skill body text.
type skillDocument struct {
	Manifest SkillManifest
	Body     string
	Path     string // absolute path to SKILL.md
}

var (
	// frontmatterRe matches YAML frontmatter at the top of a file.
	frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n(.*)`)

	documents = map[string]skillDocument{}
)

// Init scans .evo_agent/skill/**/SKILL.md and loads all skills.
// Missing directory is silently ignored (consistent with MCP config behaviour).
func Init() {
	skillsDir := filepath.Join(".evo_agent", "skill")
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		return
	}

	err := filepath.WalkDir(skillsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "[Skills] Cannot read %s: %v\n", path, readErr)
			return nil
		}
		meta, body := parseFrontmatter(string(data))
		name := meta["name"]
		if name == "" {
			// Fall back to the parent directory name
			name = filepath.Base(filepath.Dir(path))
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

// Catalog returns a formatted list of all available skills for the system prompt.
// Returns an empty string when no skills are loaded.
func Catalog() string {
	if len(documents) == 0 {
		return ""
	}
	names := make([]string, 0, len(documents))
	for name := range documents {
		names = append(names, name)
	}
	sort.Strings(names)

	var lines []string
	for _, name := range names {
		doc := documents[name]
		lines = append(lines, fmt.Sprintf("- %s: %s", doc.Manifest.Name, doc.Manifest.Description))
	}
	return strings.Join(lines, "\n")
}

// Load returns the full skill body wrapped in XML tags, ready to inject into context.
// Returns a human-readable error string when the skill name is unknown.
func Load(name string) string {
	doc, ok := documents[name]
	if !ok {
		known := knownNames()
		return fmt.Sprintf("Error: Unknown skill %q. Available skills: %s", name, known)
	}
	return fmt.Sprintf("<skill name=%q path=%q>\n%s\n</skill>", doc.Manifest.Name, doc.Path, doc.Body)
}

// Names returns the names of all loaded skills.
func Names() []string {
	names := make([]string, 0, len(documents))
	for name := range documents {
		names = append(names, name)
	}
	return names
}

// ---------- helpers ----------

func parseFrontmatter(text string) (meta map[string]string, body string) {
	meta = map[string]string{}
	matches := frontmatterRe.FindStringSubmatch(text)
	if matches == nil {
		return meta, text
	}
	for _, line := range strings.Split(matches[1], "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		meta[key] = val
	}
	return meta, matches[2]
}

func knownNames() string {
	names := make([]string, 0, len(documents))
	for name := range documents {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
