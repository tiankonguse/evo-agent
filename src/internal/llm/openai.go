package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// defaultOpenAIBaseURL is the public OpenAI endpoint. The Config field
// OpenAIBaseURL overrides it for compatible providers (DeepSeek, Qwen,
// OpenRouter, Ollama, local proxies, …).
const defaultOpenAIBaseURL = "https://api.openai.com"

// openaiHTTPTimeout caps the time we spend on a single LLM round-trip.
// Long enough for a multi-thousand-token completion on a slow model,
// short enough that a hung server eventually surfaces an error to the
// agent loop instead of blocking the REPL forever.
const openaiHTTPTimeout = 5 * time.Minute

// openaiProvider implements Provider against the OpenAI Chat
// Completions API (`POST /v1/chat/completions`). It owns its own HTTP
// client so the Anthropic SDK's retry/backoff logic does not leak into
// the OpenAI path.
//
// Translation happens in pure helpers in translate.go so this file
// stays focused on transport.
type openaiProvider struct {
	apiKey  string
	baseURL string // never has a trailing "/"
	httpc   *http.Client
}

// newOpenAIProvider validates the OpenAI-side config and constructs a
// fresh HTTP client. An empty OpenAIAPIKey is rejected here because
// silently sending requests with no credential just produces a 401
// downstream and confuses users.
func newOpenAIProvider(cfg Config) (*openaiProvider, error) {
	if cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required when PROVIDER_ID=openai")
	}
	base := cfg.OpenAIBaseURL
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	return &openaiProvider{
		apiKey:  cfg.OpenAIAPIKey,
		baseURL: strings.TrimRight(base, "/"),
		httpc:   &http.Client{Timeout: openaiHTTPTimeout},
	}, nil
}

// SendMessage translates the supplied anthropic.MessageNewParams into
// an OpenAI Chat Completions request, posts it, and synthesizes a
// fully-populated *anthropic.Message from the response. See translate.go
// for the field-level mapping table and the rationale for the
// JSON-marshal-then-UnmarshalJSON synthesis approach.
//
// Debug controls (env var OPENAI_DEBUG):
//   - 1 / true / yes / on → dump full request + response to stderr;
//     Authorization header is REDACTED so dumps are safe to paste.
//   - raw                 → same dump, but Authorization header is
//     printed verbatim. Use only when you actively need to verify that
//     the right API key is being sent (e.g. shell env shadowing a
//     value from .env). NEVER paste the resulting dump publicly.
func (p *openaiProvider) SendMessage(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	req := paramsToOpenAI(params)
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("openai: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	mode := openaiDebugMode()
	if mode != debugOff {
		dumpHTTPRequest(httpReq, body, mode)
	}

	resp, err := p.httpc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: transport: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}

	if mode != debugOff {
		dumpHTTPResponse(resp, raw)
	}

	if resp.StatusCode/100 != 2 {
		// Surface the server-side body verbatim — it usually contains a
		// JSON error object with a useful message and code.
		return nil, fmt.Errorf("openai: %d %s: %s",
			resp.StatusCode, http.StatusText(resp.StatusCode), strings.TrimSpace(string(raw)))
	}

	var oresp openaiResponse
	if err := json.Unmarshal(raw, &oresp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}
	return openAIToMessage(&oresp)
}

// debugMode is a small enum capturing the three valid OPENAI_DEBUG
// states. Keeps the dump callsites typed instead of comparing strings
// repeatedly.
type debugMode int

const (
	debugOff debugMode = iota
	debugRedacted
	debugRaw
)

// openaiDebugMode parses OPENAI_DEBUG. Anything outside the recognised
// set is treated as off so a stray value never silently leaks the key.
func openaiDebugMode() debugMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPENAI_DEBUG"))) {
	case "1", "true", "yes", "on":
		return debugRedacted
	case "raw", "2", "unsafe":
		return debugRaw
	}
	return debugOff
}

// dumpHTTPRequest prints the outgoing request (line, headers, body) to
// stderr. In debugRedacted mode the Authorization header is replaced
// with "Bearer <redacted>" so the dump is safe to share. In debugRaw
// mode the header is printed verbatim and a one-line warning is
// emitted before the dump as a reminder.
func dumpHTTPRequest(req *http.Request, body []byte, mode debugMode) {
	clone := req.Clone(req.Context())
	if mode == debugRedacted && clone.Header.Get("Authorization") != "" {
		clone.Header.Set("Authorization", "Bearer <redacted>")
	}
	clone.Body = io.NopCloser(bytes.NewReader(nil)) // body printed separately
	dump, err := httputil.DumpRequestOut(clone, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[openai-debug] DumpRequestOut: %v\n", err)
		return
	}
	if mode == debugRaw {
		fmt.Fprintln(os.Stderr, "⚠ OPENAI_DEBUG=raw — Authorization header below is UNREDACTED. Do NOT paste this dump publicly.")
	}
	fmt.Fprintln(os.Stderr, "─── OpenAI request ─────────────────────────────────")
	fmt.Fprint(os.Stderr, string(dump))
	fmt.Fprintln(os.Stderr, prettyJSON(body))
	fmt.Fprintln(os.Stderr, "────────────────────────────────────────────────────")
}

// dumpHTTPResponse prints the incoming response (status, headers, body)
// to stderr. Body is pretty-printed when it parses as JSON, otherwise
// printed raw. Truncated to 16 KiB so a runaway server cannot flood the
// terminal.
func dumpHTTPResponse(resp *http.Response, body []byte) {
	dump, err := httputil.DumpResponse(resp, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[openai-debug] DumpResponse: %v\n", err)
		return
	}
	const maxBody = 16 * 1024
	shown := body
	if len(shown) > maxBody {
		shown = append(append([]byte{}, shown[:maxBody]...), []byte("\n…[truncated]")...)
	}
	fmt.Fprintln(os.Stderr, "─── OpenAI response ────────────────────────────────")
	fmt.Fprint(os.Stderr, string(dump))
	fmt.Fprintln(os.Stderr, prettyJSON(shown))
	fmt.Fprintln(os.Stderr, "────────────────────────────────────────────────────")
}

// prettyJSON returns indented JSON when data parses, otherwise returns
// the raw bytes unchanged. Useful for debug dumps where the body may
// occasionally be plain text (HTML error page, gateway 502, …).
func prettyJSON(data []byte) string {
	if len(bytes.TrimSpace(data)) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(data)
	}
	return string(out)
}
