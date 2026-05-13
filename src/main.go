package main

import (
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"evo-agent/internal/agent"
	"evo-agent/internal/config"
	"evo-agent/internal/tools"
)

func BuildOptions(cfg *config.Config) []option.RequestOption {
	opts := []option.RequestOption{}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
		os.Unsetenv("ANTHROPIC_AUTH_TOKEN")
	}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	} else {
		opts = append(opts, option.WithAPIKey("dummy"))
	}
	return opts
}

func main() {
	config.LoadEnv()
	cfg := config.Load()

	if cfg.ModelID == "" {
		fmt.Fprintln(os.Stderr, "Error: MODEL_ID not set in environment")
		os.Exit(1)
	}

	opts := BuildOptions(cfg)
	client := anthropic.NewClient(opts...)

	tools.InitMCP()
	defer tools.ShutdownMCP()

	// 打印工具列表
	tools.PrintToolList()

	a := agent.New(&client, cfg)
	a.Run(os.Stdin)
}
