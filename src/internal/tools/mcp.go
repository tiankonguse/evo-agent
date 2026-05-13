package tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// ---------- Config structs (matches .evo_agent/mcp.json + mcp-schema.json) ----------

// MCPServerConfig holds the parameters for one MCP server entry.
type MCPServerConfig struct {
	// Transport type: "stdio", "sse", or "streamableHttp" (required)
	Type string `json:"type"`
	// If true the server is ignored at startup
	Disabled bool `json:"disabled"`
	// Request timeout in seconds (default 30)
	Timeout     int    `json:"timeout"`
	Description string `json:"description"`

	// stdio fields
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`

	// sse / streamableHttp fields
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// MCPConfig is the top-level structure of .evo_agent/mcp.json.
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// timeout returns the configured timeout, defaulting to 30 s.
func (c *MCPServerConfig) timeout() time.Duration {
	if c.Timeout > 0 {
		return time.Duration(c.Timeout) * time.Second
	}
	return 30 * time.Second
}

// ---------- Shared types ----------

type mcpToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type jsonrpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// mcpClient is the common interface for all MCP transports.
type mcpClient interface {
	getTools() []mcpToolSpec
	callTool(toolName string, arguments json.RawMessage) (string, error)
	stop()
}

// ---------- stdio transport ----------

type mcpProcess struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID int
	tools  []mcpToolSpec
}

func startMCPProcess(name string, cfg MCPServerConfig) (*mcpProcess, error) {
	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = env
	cmd.Stderr = os.Stderr

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %s: stdin pipe: %w", name, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %s: stdout pipe: %w", name, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp %s: start: %w", name, err)
	}

	p := &mcpProcess{
		name:   name,
		cmd:    cmd,
		stdin:  stdinPipe,
		stdout: bufio.NewReader(stdoutPipe),
	}

	initParams, _ := json.Marshal(map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "evo-agent", "version": "1.0"},
	})
	resp, err := p.call("initialize", initParams)
	if err != nil {
		p.stop()
		return nil, fmt.Errorf("mcp %s: initialize: %w", name, err)
	}
	if resp.Error != nil {
		p.stop()
		return nil, fmt.Errorf("mcp %s: initialize error: %s", name, resp.Error.Message)
	}
	p.notify("notifications/initialized")

	listResp, err := p.call("tools/list", json.RawMessage(`{}`))
	if err != nil {
		p.stop()
		return nil, fmt.Errorf("mcp %s: tools/list: %w", name, err)
	}
	var listResult struct {
		Tools []mcpToolSpec `json:"tools"`
	}
	if listResp.Result != nil {
		json.Unmarshal(listResp.Result, &listResult)
	}
	p.tools = listResult.Tools
	return p, nil
}

func (p *mcpProcess) call(method string, params json.RawMessage) (*jsonrpcEnvelope, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.nextID++
	msg := jsonrpcEnvelope{JSONRPC: "2.0", ID: p.nextID, Method: method, Params: params}
	line, _ := json.Marshal(msg)
	line = append(line, '\n')
	if _, err := p.stdin.Write(line); err != nil {
		return nil, err
	}
	rawLine, err := p.stdout.ReadString('\n')
	if err != nil {
		return nil, err
	}
	var resp jsonrpcEnvelope
	if err := json.Unmarshal([]byte(rawLine), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (p *mcpProcess) notify(method string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	msg := jsonrpcEnvelope{JSONRPC: "2.0", Method: method}
	line, _ := json.Marshal(msg)
	line = append(line, '\n')
	p.stdin.Write(line) //nolint:errcheck
}

func (p *mcpProcess) getTools() []mcpToolSpec { return p.tools }

func (p *mcpProcess) callTool(toolName string, arguments json.RawMessage) (string, error) {
	params, _ := json.Marshal(map[string]interface{}{
		"name":      toolName,
		"arguments": json.RawMessage(arguments),
	})
	resp, err := p.call("tools/call", params)
	if err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("MCP error: %s", resp.Error.Message)
	}
	return extractTextContent(resp.Result), nil
}

func (p *mcpProcess) stop() {
	p.stdin.Close()
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
	}
}

// ---------- streamableHttp transport ----------
// Each request is an independent POST; response is JSON or SSE.

type mcpHTTPClient struct {
	name    string
	url     string
	headers map[string]string
	http    *http.Client
	mu      sync.Mutex
	nextID  int
	tools   []mcpToolSpec
}

func connectMCPStreamableHTTP(name string, cfg MCPServerConfig) (*mcpHTTPClient, error) {
	c := &mcpHTTPClient{
		name:    name,
		url:     cfg.URL,
		headers: cfg.Headers,
		http:    &http.Client{Timeout: cfg.timeout()},
	}

	initParams, _ := json.Marshal(map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "evo-agent", "version": "1.0"},
	})
	resp, err := c.call("initialize", initParams)
	if err != nil {
		return nil, fmt.Errorf("mcp %s: initialize: %w", name, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp %s: initialize error: %s", name, resp.Error.Message)
	}
	c.postNotify("notifications/initialized")

	listResp, err := c.call("tools/list", json.RawMessage(`{}`))
	if err != nil {
		return nil, fmt.Errorf("mcp %s: tools/list: %w", name, err)
	}
	var listResult struct {
		Tools []mcpToolSpec `json:"tools"`
	}
	if listResp.Result != nil {
		json.Unmarshal(listResp.Result, &listResult)
	}
	c.tools = listResult.Tools
	return c, nil
}

func (c *mcpHTTPClient) call(method string, params json.RawMessage) (*jsonrpcEnvelope, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	msg := jsonrpcEnvelope{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, _ := json.Marshal(msg)

	req, err := http.NewRequest("POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	httpResp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if strings.Contains(httpResp.Header.Get("Content-Type"), "text/event-stream") {
		return parseSSEOnce(httpResp.Body, id)
	}
	var env jsonrpcEnvelope
	if err := json.NewDecoder(httpResp.Body).Decode(&env); err != nil {
		return nil, err
	}
	return &env, nil
}

func (c *mcpHTTPClient) postNotify(method string) {
	msg := jsonrpcEnvelope{JSONRPC: "2.0", Method: method}
	body, _ := json.Marshal(msg)
	req, err := http.NewRequest("POST", c.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if resp, err := c.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (c *mcpHTTPClient) getTools() []mcpToolSpec { return c.tools }

func (c *mcpHTTPClient) callTool(toolName string, arguments json.RawMessage) (string, error) {
	params, _ := json.Marshal(map[string]interface{}{
		"name":      toolName,
		"arguments": json.RawMessage(arguments),
	})
	resp, err := c.call("tools/call", params)
	if err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("MCP error: %s", resp.Error.Message)
	}
	return extractTextContent(resp.Result), nil
}

func (c *mcpHTTPClient) stop() {} // stateless, nothing to tear down

// ---------- SSE transport ----------
// Establishes a persistent GET SSE connection; sends requests via POST to the
// endpoint URL received in the "endpoint" SSE event; reads responses from the stream.

type mcpSSEClient struct {
	name        string
	baseURL     string
	postURL     string // received from "endpoint" SSE event
	headers     map[string]string
	http        *http.Client
	mu          sync.Mutex
	nextID      int
	tools       []mcpToolSpec
	sseBody     io.ReadCloser // open SSE response body
	sseScanner  *bufio.Scanner
	pendingResp map[int]chan *jsonrpcEnvelope
}

func connectMCPSSE(name string, cfg MCPServerConfig) (*mcpSSEClient, error) {
	c := &mcpSSEClient{
		name:        name,
		baseURL:     cfg.URL,
		headers:     cfg.Headers,
		http:        &http.Client{},
		pendingResp: make(map[int]chan *jsonrpcEnvelope),
	}

	// Open SSE stream (no timeout on the connection itself)
	req, err := http.NewRequest("GET", cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp %s: sse GET: %w", name, err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp %s: sse connect: %w", name, err)
	}
	c.sseBody = resp.Body
	c.sseScanner = bufio.NewScanner(resp.Body)

	// Wait for "endpoint" event
	postURL, err := c.waitForEndpoint(cfg.timeout())
	if err != nil {
		c.sseBody.Close()
		return nil, fmt.Errorf("mcp %s: sse endpoint: %w", name, err)
	}
	c.postURL = postURL

	// Start background reader goroutine
	go c.readLoop()

	// Handshake
	initParams, _ := json.Marshal(map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "evo-agent", "version": "1.0"},
	})
	initResp, err := c.call("initialize", initParams)
	if err != nil {
		c.stop()
		return nil, fmt.Errorf("mcp %s: initialize: %w", name, err)
	}
	if initResp.Error != nil {
		c.stop()
		return nil, fmt.Errorf("mcp %s: initialize error: %s", name, initResp.Error.Message)
	}
	c.postNotify("notifications/initialized")

	// Fetch tool list
	listResp, err := c.call("tools/list", json.RawMessage(`{}`))
	if err != nil {
		c.stop()
		return nil, fmt.Errorf("mcp %s: tools/list: %w", name, err)
	}
	var listResult struct {
		Tools []mcpToolSpec `json:"tools"`
	}
	if listResp.Result != nil {
		json.Unmarshal(listResp.Result, &listResult)
	}
	c.tools = listResult.Tools
	return c, nil
}

// waitForEndpoint scans the SSE stream until an "endpoint" event is received.
func (c *mcpSSEClient) waitForEndpoint(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var eventType string
	for c.sseScanner.Scan() {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timeout waiting for endpoint event")
		}
		line := c.sseScanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if eventType == "endpoint" {
				return data, nil
			}
			eventType = ""
		}
	}
	return "", fmt.Errorf("SSE stream closed before endpoint event")
}

// readLoop runs in background, routing incoming SSE data lines to waiting callers.
func (c *mcpSSEClient) readLoop() {
	for c.sseScanner.Scan() {
		line := c.sseScanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var env jsonrpcEnvelope
		if err := json.Unmarshal([]byte(data), &env); err != nil {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pendingResp[env.ID]
		if ok {
			delete(c.pendingResp, env.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- &env
		}
	}
}

// call POSTs a JSON-RPC request and waits for the response via the SSE stream.
func (c *mcpSSEClient) call(method string, params json.RawMessage) (*jsonrpcEnvelope, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan *jsonrpcEnvelope, 1)
	c.pendingResp[id] = ch
	c.mu.Unlock()

	msg := jsonrpcEnvelope{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, _ := json.Marshal(msg)

	req, err := http.NewRequest("POST", c.postURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	httpResp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	httpResp.Body.Close()

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(30 * time.Second):
		c.mu.Lock()
		delete(c.pendingResp, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for SSE response (id=%d)", id)
	}
}

func (c *mcpSSEClient) postNotify(method string) {
	msg := jsonrpcEnvelope{JSONRPC: "2.0", Method: method}
	body, _ := json.Marshal(msg)
	req, err := http.NewRequest("POST", c.postURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if resp, err := c.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (c *mcpSSEClient) getTools() []mcpToolSpec { return c.tools }

func (c *mcpSSEClient) callTool(toolName string, arguments json.RawMessage) (string, error) {
	params, _ := json.Marshal(map[string]interface{}{
		"name":      toolName,
		"arguments": json.RawMessage(arguments),
	})
	resp, err := c.call("tools/call", params)
	if err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", fmt.Errorf("MCP error: %s", resp.Error.Message)
	}
	return extractTextContent(resp.Result), nil
}

func (c *mcpSSEClient) stop() {
	if c.sseBody != nil {
		c.sseBody.Close()
	}
}

// ---------- Shared helper ----------

// parseSSEOnce reads an SSE response body and returns the first JSON-RPC response.
func parseSSEOnce(body io.Reader, wantID int) (*jsonrpcEnvelope, error) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var env jsonrpcEnvelope
		if err := json.Unmarshal([]byte(data), &env); err != nil {
			continue
		}
		if env.ID == wantID || env.Result != nil || env.Error != nil {
			return &env, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("SSE stream ended without response (id=%d)", wantID)
}

// extractTextContent joins the text fields from a tools/call result content array.
func extractTextContent(result json.RawMessage) string {
	if result == nil {
		return ""
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(result, &r)
	var parts []string
	for _, c := range r.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ---------- Package-level MCP router ----------

var mcpServers = map[string]mcpClient{}

// InitMCP loads .evo_agent/mcp.json and connects to all enabled servers.
// Missing config file is silently ignored.
func InitMCP() {
	data, err := os.ReadFile(".evo_agent/mcp.json")
	if err != nil {
		return
	}

	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[MCP] Failed to parse .evo_agent/mcp.json: %v\n", err)
		return
	}

	for name, serverCfg := range cfg.MCPServers {
		if serverCfg.Disabled {
			continue
		}

		var client mcpClient
		var connErr error

		switch serverCfg.Type {
		case "stdio":
			client, connErr = startMCPProcess(name, serverCfg)
		case "streamableHttp":
			client, connErr = connectMCPStreamableHTTP(name, serverCfg)
		case "sse":
			client, connErr = connectMCPSSE(name, serverCfg)
		default:
			fmt.Fprintf(os.Stderr, "[MCP] Server %q has unknown type %q (want stdio/sse/streamableHttp)\n", name, serverCfg.Type)
			continue
		}

		if connErr != nil {
			fmt.Fprintf(os.Stderr, "[MCP] Failed to connect to %q: %v\n", name, connErr)
			continue
		}
		mcpServers[name] = client
		fmt.Printf("[MCP] Connected to %q (%d tools)\n", name, len(client.getTools()))
	}
}

// ShutdownMCP gracefully stops all MCP server connections.
func ShutdownMCP() {
	for _, c := range mcpServers {
		c.stop()
	}
}

// MCPTools returns Anthropic tool schemas for all MCP tools.
// Tool names are prefixed as mcp__{server}__{tool}.
func MCPTools() []anthropic.ToolUnionParam {
	var out []anthropic.ToolUnionParam
	for serverName, client := range mcpServers {
		for _, t := range client.getTools() {
			prefixed := "mcp__" + serverName + "__" + t.Name

			schema := anthropic.ToolInputSchemaParam{}
			if t.InputSchema != nil {
				var raw map[string]interface{}
				if json.Unmarshal(t.InputSchema, &raw) == nil {
					schema.Properties = raw["properties"]
					if req, ok := raw["required"]; ok {
						if reqSlice, ok := req.([]interface{}); ok {
							for _, r := range reqSlice {
								if s, ok := r.(string); ok {
									schema.Required = append(schema.Required, s)
								}
							}
						}
					}
				}
			}

			tool := anthropic.ToolParam{
				Name:        prefixed,
				Description: anthropic.String(t.Description),
				InputSchema: schema,
			}
			out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
		}
	}
	return out
}

// DispatchMCP routes an mcp__{server}__{tool} call to the correct server.
func DispatchMCP(name string, input json.RawMessage) (string, error) {
	rest := strings.TrimPrefix(name, "mcp__")
	sep := strings.Index(rest, "__")
	if sep < 0 {
		return "", fmt.Errorf("invalid MCP tool name: %s", name)
	}
	serverName := rest[:sep]
	toolName := rest[sep+2:]

	client, ok := mcpServers[serverName]
	if !ok {
		return "", fmt.Errorf("MCP server not found: %s", serverName)
	}
	return client.callTool(toolName, input)
}

func PrintToolList() {
	for serverName, client := range mcpServers {
		fmt.Printf("Server: %s\n", serverName)
		for _, t := range client.getTools() {
			fmt.Printf("  - %s: %s\n", t.Name, t.Description)
		}
	}
}
