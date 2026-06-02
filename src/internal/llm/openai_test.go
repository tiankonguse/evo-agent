package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// fakeOpenAI captures the most recent request body and returns a
// caller-supplied response. Lets us drive openaiProvider end-to-end
// without touching the real API.
type fakeOpenAI struct {
	server      *httptest.Server
	lastReqBody []byte
	lastAuth    string
	respStatus  int
	respBody    string
}

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	t.Helper()
	f := &fakeOpenAI{respStatus: 200}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		f.lastAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		f.lastReqBody = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.respStatus)
		_, _ = io.WriteString(w, f.respBody)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func TestOpenAIProvider_RoundTripText(t *testing.T) {
	fake := newFakeOpenAI(t)
	fake.respBody = `{
		"id": "chatcmpl-test",
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"finish_reason": "stop",
			"message": {"role": "assistant", "content": "pong"}
		}],
		"usage": {"prompt_tokens": 4, "completion_tokens": 1, "total_tokens": 5}
	}`

	p, err := newOpenAIProvider(Config{
		OpenAIAPIKey:  "sk-test",
		OpenAIBaseURL: fake.server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := p.SendMessage(context.Background(), anthropic.MessageNewParams{
		Model:     "gpt-4o",
		MaxTokens: 50,
		System: []anthropic.TextBlockParam{
			{Text: "be terse"},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("ping")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// ── Assert request body shape ───────────────────────────────────
	if !strings.HasPrefix(fake.lastAuth, "Bearer sk-test") {
		t.Errorf("Authorization = %q", fake.lastAuth)
	}
	var sent map[string]any
	if err := json.Unmarshal(fake.lastReqBody, &sent); err != nil {
		t.Fatalf("request body not JSON: %v / %s", err, string(fake.lastReqBody))
	}
	if sent["model"] != "gpt-4o" {
		t.Errorf("sent.model = %v", sent["model"])
	}
	msgs := sent["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("sent.messages = %d, want 2", len(msgs))
	}
	if msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("[0].role = %v", msgs[0].(map[string]any)["role"])
	}
	if msgs[1].(map[string]any)["role"] != "user" || msgs[1].(map[string]any)["content"] != "ping" {
		t.Errorf("[1] = %v", msgs[1])
	}

	// ── Assert response shape — the agent loop's downstream contract ──
	if msg.StopReason != anthropic.StopReasonEndTurn {
		t.Errorf("StopReason = %q", msg.StopReason)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("Content = %d", len(msg.Content))
	}
	if tb, ok := msg.Content[0].AsAny().(anthropic.TextBlock); !ok || tb.Text != "pong" {
		t.Errorf("AsAny = %T %v", msg.Content[0].AsAny(), msg.Content[0])
	}
	// ToParam must round-trip — agent loop calls this at loop.go:155.
	mp := msg.ToParam()
	if mp.Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("ToParam Role = %q", mp.Role)
	}
	if mp.Content[0].OfText == nil || mp.Content[0].OfText.Text != "pong" {
		t.Errorf("ToParam content = %+v", mp.Content)
	}
}

func TestOpenAIProvider_ErrorSurfacesBody(t *testing.T) {
	fake := newFakeOpenAI(t)
	fake.respStatus = 400
	fake.respBody = `{"error":{"message":"invalid model","type":"invalid_request_error"}}`

	p, _ := newOpenAIProvider(Config{
		OpenAIAPIKey:  "sk-x",
		OpenAIBaseURL: fake.server.URL,
	})
	_, err := p.SendMessage(context.Background(), anthropic.MessageNewParams{
		Model:     "bogus",
		MaxTokens: 1,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	})
	if err == nil {
		t.Fatal("expected error on 400 response")
	}
	if !strings.Contains(err.Error(), "invalid model") {
		t.Errorf("error did not surface body: %v", err)
	}
}

func TestNew_OpenAIRequiresKey(t *testing.T) {
	_, err := New(Config{ProviderID: "openai"})
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY missing")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("error message: %v", err)
	}
}

func TestNew_AnthropicDefault(t *testing.T) {
	// Empty ProviderID must default to anthropic without error.
	if _, err := New(Config{ProviderID: "", AnthropicAPIKey: "test"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := New(Config{ProviderID: "anthropic", AnthropicAPIKey: "test"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_UnknownProviderRejected(t *testing.T) {
	_, err := New(Config{ProviderID: "deepseek"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
