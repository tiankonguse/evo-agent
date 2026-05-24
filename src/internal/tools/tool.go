package tools

import (
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/invopop/jsonschema"
)

// Handler is the function signature every tool must implement.
type Handler func(input json.RawMessage) (string, error)

// ToolDef bundles a tool's API schema with its handler in one place.
// Register a ToolDef via Register() inside an init() function so that
// adding a new tool only requires touching that tool's own file.
type ToolDef struct {
	Schema  anthropic.ToolParam
	Handler Handler
}

var registry = map[string]ToolDef{}

// Register adds a ToolDef to the global registry.
// Call this from each tool file's init() function.
func Register(def ToolDef) {
	registry[def.Schema.Name] = def
}

// Tools returns all registered tool schemas ready for the Anthropic API.
// Native tools come first, followed by any MCP tools.
func Tools() []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(registry))
	for _, d := range registry {
		tool := d.Schema
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	out = append(out, MCPTools()...)
	return out
}

// Dispatch calls the handler registered for name.
// MCP tools (prefixed mcp__) are routed to the MCP router.
// Returns (output, nil) on success, ("", error) if handler fails, ("", error) if not found.
func Dispatch(name string, input json.RawMessage) (string, error) {
	if strings.HasPrefix(name, "mcp__") {
		return DispatchMCP(name, input)
	}
	if d, ok := registry[name]; ok {
		return d.Handler(input)
	}
	return "", nil
}

// RegistryNames returns the names of all registered built-in tools.
func RegistryNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// ToolsExcept returns all registered tool schemas excluding those named in exclude.
// Used by subagents to prevent recursive spawning (e.g. exclude "task").
func ToolsExcept(exclude ...string) []anthropic.ToolUnionParam {
	skip := make(map[string]bool, len(exclude))
	for _, n := range exclude {
		skip[n] = true
	}
	all := Tools()
	out := make([]anthropic.ToolUnionParam, 0, len(all))
	for _, t := range all {
		if t.OfTool != nil && skip[t.OfTool.Name] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// GenerateSchema uses reflection to build a ToolInputSchemaParam from a Go struct.
// Annotate fields with `jsonschema_description:"..."` to add descriptions.
func GenerateSchema[T any]() anthropic.ToolInputSchemaParam {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return anthropic.ToolInputSchemaParam{
		Properties: schema.Properties,
	}
}
