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
}

// LoadEnv loads .env files: first from the directory of the running binary,
// then from the current working directory (cwd overrides).
func LoadEnv() {
	exe, err := os.Executable()
	if err == nil {
		buildEnv := filepath.Join(filepath.Dir(exe), ".env")
		if _, err := os.Stat(buildEnv); err == nil {
			_ = godotenv.Load(buildEnv)
		}
	}
	_ = godotenv.Load(".env")
}

// Load reads environment variables and returns a populated Config.
func Load() *Config {
	cwd, _ := os.Getwd()
	return &Config{
		ModelID:    os.Getenv("MODEL_ID"),
		APIKey:     os.Getenv("ANTHROPIC_API_KEY"),
		BaseURL:    os.Getenv("ANTHROPIC_BASE_URL"),
		ProjectDir: cwd,
	}
}
