package config

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	ModelID    string
	APIKey     string
	BaseURL    string
	ProjectDir string

	// ProviderID selects the LLM backend. Valid values: "" / "anthropic"
	// (default) or "openai". Read from PROVIDER_ID. Validated by main.go
	// before constructing the llm.Provider.
	ProviderID string

	// OpenAI-specific knobs. Used only when ProviderID == "openai".
	// OPENAI_BASE_URL defaults to "https://api.openai.com" inside the
	// llm package when empty.
	OpenAIAPIKey  string
	OpenAIBaseURL string
}

// LoadEnv loads .env files: first from the directory of the running binary,
// then from the current working directory (cwd overrides).
//
// Both passes use godotenv.Overload so the file values OVERRIDE any
// pre-existing shell environment variables. This matches the
// expectation users hit in practice: when you set MODEL_ID or
// OPENAI_API_KEY in the project's .env, you want that to win even if
// the same name is exported in your shell rc — otherwise stale shell
// values silently shadow the project config and the only symptom is a
// confusing 401 from the LLM provider. To override .env at runtime,
// edit the file (or pass values via inline env: `MODEL_ID=foo evo-agent`
// will only take effect if MODEL_ID is absent from .env).
func LoadEnv() {
	exe, err := os.Executable()
	if err == nil {
		buildEnv := filepath.Join(filepath.Dir(exe), ".env")
		if _, err := os.Stat(buildEnv); err == nil {
			_ = godotenv.Overload(buildEnv)
		}
	}
	_ = godotenv.Overload(".env")
}

// Load reads environment variables and returns a populated Config.
func Load() *Config {
	cwd, _ := os.Getwd()
	return &Config{
		ModelID:       os.Getenv("MODEL_ID"),
		APIKey:        os.Getenv("ANTHROPIC_API_KEY"),
		BaseURL:       os.Getenv("ANTHROPIC_BASE_URL"),
		ProjectDir:    cwd,
		ProviderID:    os.Getenv("PROVIDER_ID"),
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL: os.Getenv("OPENAI_BASE_URL"),
	}
}
