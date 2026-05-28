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
	Name                   string
	Description            string
	ArgumentHint           string   // e.g. "[issue-number]"
	Arguments              []string // named positional args for $name substitution
	IsCommand              bool     // true = loaded from command/ dir, not in catalog
	DisableModelInvocation bool     // true = not in catalog, only user /slash trigger
	UserInvocable          bool     // true = user can invoke via /slash (default true)
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

	// Separate maps: skills and commands can have the same name without conflict.
	skillDocuments   = map[string]skillDocument{}
	commandDocuments = map[string]skillDocument{}
)

// Init scans .evo-agent/skill/**/SKILL.md and loads all skills.
// Missing directory is silently ignored (consistent with MCP config behaviour).
func Init() {
	skillsDir := filepath.Join(".evo-agent", "skill")
	if _, err := os.Stat(skillsDir); err == nil {
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
			argHint := meta["argument-hint"]
			arguments := parseArguments(meta["arguments"])
			disableModel := meta["disable-model-invocation"] == "true" // default false
			userInvocable := meta["user-invocable"] != "false"         // default true

			skillDocuments[name] = skillDocument{
				Manifest: SkillManifest{
					Name:                   name,
					Description:            description,
					ArgumentHint:           argHint,
					Arguments:              arguments,
					IsCommand:              false,
					DisableModelInvocation: disableModel,
					UserInvocable:          userInvocable,
				},
				Body: strings.TrimSpace(body),
				Path: absPath,
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[Skills] Walk error: %v\n", err)
		}
	}
	fmt.Printf("[Skills] Loaded %d skill(s)\n", len(skillDocuments))

	// Also load commands from .evo-agent/command/
	InitCommands()
}

// Catalog returns a formatted list of all available skills for the system prompt.
// Returns an empty string when no skills are loaded.
// Only includes skills that are model-invocable (not disabled).
func Catalog() string {
	if len(skillDocuments) == 0 {
		return ""
	}
	names := make([]string, 0, len(skillDocuments))
	for name, doc := range skillDocuments {
		if !doc.Manifest.DisableModelInvocation {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)

	var lines []string
	for _, name := range names {
		doc := skillDocuments[name]
		hint := ""
		if doc.Manifest.ArgumentHint != "" {
			hint = " " + doc.Manifest.ArgumentHint
		}
		lines = append(lines, fmt.Sprintf("- %s%s: %s", doc.Manifest.Name, hint, doc.Manifest.Description))
	}
	return strings.Join(lines, "\n")
}

// Load returns the full skill body wrapped in XML tags, ready to inject into context.
// Only looks up from skills (not commands). Used by the load_skill tool.
// Returns a human-readable error string when the skill name is unknown.
func Load(name string) string {
	doc, ok := skillDocuments[name]
	if !ok {
		known := knownSkillNames()
		return fmt.Sprintf("Error: Unknown skill %q. Available skills: %s", name, known)
	}
	return fmt.Sprintf("<skill name=%q path=%q>\n%s\n</skill>", doc.Manifest.Name, doc.Path, doc.Body)
}

// Names returns the names of all loaded skills that are model-invocable (excludes disabled).
func Names() []string {
	names := make([]string, 0, len(skillDocuments))
	for name, doc := range skillDocuments {
		if !doc.Manifest.DisableModelInvocation {
			names = append(names, name)
		}
	}
	return names
}

// GetManifest returns the manifest for a named skill or command.
// Commands take priority over skills when names conflict (for display purposes).
// Returns a zero SkillManifest if not found.
func GetManifest(name string) SkillManifest {
	if doc, ok := commandDocuments[name]; ok {
		return doc.Manifest
	}
	if doc, ok := skillDocuments[name]; ok {
		return doc.Manifest
	}
	return SkillManifest{}
}

// GetSkillManifest returns the manifest for a skill only (not command).
func GetSkillManifest(name string) (SkillManifest, bool) {
	doc, ok := skillDocuments[name]
	if !ok {
		return SkillManifest{}, false
	}
	return doc.Manifest, true
}

// GetCommandManifest returns the manifest for a command only (not skill).
func GetCommandManifest(name string) (SkillManifest, bool) {
	doc, ok := commandDocuments[name]
	if !ok {
		return SkillManifest{}, false
	}
	return doc.Manifest, true
}

// LookupForSlash finds a document for slash invocation.
// Commands take priority over skills when names conflict.
func LookupForSlash(name string) (skillDocument, bool) {
	if doc, ok := commandDocuments[name]; ok {
		return doc, true
	}
	if doc, ok := skillDocuments[name]; ok {
		return doc, true
	}
	return skillDocument{}, false
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

// parseArguments splits an arguments frontmatter value into a slice.
// Accepts space-separated or comma-separated values.
func parseArguments(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var parts []string
	if strings.Contains(raw, ",") {
		parts = strings.Split(raw, ",")
	} else {
		parts = strings.Fields(raw)
	}
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func knownSkillNames() string {
	names := make([]string, 0, len(skillDocuments))
	for name := range skillDocuments {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// InitCommands scans .evo-agent/command/*.md and loads all commands.
// Commands are like skills but stored as flat files and NOT included in the catalog.
func InitCommands() {
	count := 0
	cmdDir := filepath.Join(".evo-agent", "command")
	if _, err := os.Stat(cmdDir); err == nil {
		entries, err := os.ReadDir(cmdDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[Commands] ReadDir error: %v\n", err)
		} else {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
					continue
				}
				path := filepath.Join(cmdDir, entry.Name())
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					fmt.Fprintf(os.Stderr, "[Commands] Cannot read %s: %v\n", path, readErr)
					continue
				}
				meta, body := parseFrontmatter(string(data))
				name := meta["name"]
				if name == "" {
					name = strings.TrimSuffix(entry.Name(), ".md")
				}
				description := meta["description"]
				if description == "" {
					description = "No description"
				}
				argHint := meta["argument-hint"]
				arguments := parseArguments(meta["arguments"])
				userInvocable := meta["user-invocable"] != "false" // default true

				absPath, err := filepath.Abs(path)
				if err != nil {
					absPath = path
				}
				commandDocuments[name] = skillDocument{
					Manifest: SkillManifest{
						Name:          name,
						Description:   description,
						ArgumentHint:  argHint,
						Arguments:     arguments,
						IsCommand:     true,
						UserInvocable: userInvocable,
					},
					Body: strings.TrimSpace(body),
					Path: absPath,
				}
				count++
			}
		}
	}
	fmt.Printf("[Commands] Loaded %d command(s)\n", count)

	// Load built-in commands (embedded in binary); user commands take priority.
	LoadBuiltinCommands()
}

// CommandNames returns the names of all user-invocable commands
// plus skills with disable-model-invocation: true.
func CommandNames() []string {
	seen := map[string]bool{}
	names := make([]string, 0)
	// Commands
	for name, doc := range commandDocuments {
		if doc.Manifest.UserInvocable {
			names = append(names, name)
			seen[name] = true
		}
	}
	// Skills with disable-model-invocation (user-only slash)
	for name, doc := range skillDocuments {
		if doc.Manifest.DisableModelInvocation && doc.Manifest.UserInvocable && !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
