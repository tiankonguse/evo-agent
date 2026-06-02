package goal

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestParseVerdict_RawJSON(t *testing.T) {
	v := ParseVerdict(`{"met": true, "reason": "tests pass"}`)
	if !v.Met || v.Reason != "tests pass" {
		t.Fatalf("got %+v", v)
	}
}

func TestParseVerdict_FencedJSON(t *testing.T) {
	v := ParseVerdict("```json\n{\"met\":false,\"reason\":\"compile fail\"}\n```")
	if v.Met || v.Reason != "compile fail" {
		t.Fatalf("got %+v", v)
	}
}

func TestParseVerdict_EmbeddedInProse(t *testing.T) {
	v := ParseVerdict(`Sure! Here is the verdict:
{"met": false, "reason": "still pending"}
Hope that helps.`)
	if v.Met || v.Reason != "still pending" {
		t.Fatalf("got %+v", v)
	}
}

func TestParseVerdict_Empty(t *testing.T) {
	v := ParseVerdict("")
	if v.Met {
		t.Fatalf("empty must be Met=false")
	}
	if v.Reason == "" {
		t.Fatalf("empty must produce a non-empty diagnostic reason")
	}
}

func TestParseVerdict_Garbage(t *testing.T) {
	v := ParseVerdict("this isn't JSON at all, just words")
	if v.Met {
		t.Fatalf("garbage must be Met=false")
	}
	if v.Reason == "" {
		t.Fatalf("garbage must produce diagnostic reason")
	}
}

func TestParseVerdict_NoReason(t *testing.T) {
	v := ParseVerdict(`{"met": true}`)
	if !v.Met {
		t.Fatalf("met should be true")
	}
	if v.Reason == "" {
		t.Fatalf("missing reason should be filled with placeholder")
	}
}

func TestBuildEvalRequest_TruncatesLongTranscript(t *testing.T) {
	// Build 10 messages; we should only see the last 6.
	var msgs []anthropic.MessageParam
	for i := 0; i < 10; i++ {
		role := anthropic.NewUserMessage
		if i%2 == 1 {
			role = anthropic.NewAssistantMessage
		}
		msgs = append(msgs, role(anthropic.NewTextBlock(
			"msg-"+string(rune('A'+i)),
		)))
	}
	_, user := BuildEvalRequest("ship", msgs, 0)
	// 10 messages, last 6 kept → msg-E through msg-J.
	if !strings.Contains(user, "msg-E") {
		t.Fatalf("expected msg-E in transcript:\n%s", user)
	}
	// msg-A through msg-D should be dropped.
	for _, dropped := range []string{"msg-A", "msg-B", "msg-C", "msg-D"} {
		if strings.Contains(user, dropped) {
			t.Fatalf("did not expect %s in transcript:\n%s", dropped, user)
		}
	}
}

func TestContinuationPrompt_IncludesAllParts(t *testing.T) {
	out := ContinuationPrompt("ship X", "still failing", "plan summary here")
	for _, want := range []string{"<goal-reminder>", "ship X", "still failing", "plan summary here", "</goal-reminder>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ContinuationPrompt missing %q:\n%s", want, out)
		}
	}
}

func TestContinuationPrompt_OmitsEmptyPlanSummary(t *testing.T) {
	out := ContinuationPrompt("g", "r", "")
	if strings.Contains(out, "Persistent plan context") {
		t.Fatalf("should not include plan section when summary is empty:\n%s", out)
	}
}
