package skills

import (
	"strings"
	"testing"
)

func TestRenderBodyARGUMENTS(t *testing.T) {
	body := "Fix issue $ARGUMENTS following standards."
	result := RenderBody(body, nil, []string{"123"}, "123")
	if !strings.Contains(result, "Fix issue 123 following standards.") {
		t.Errorf("RenderBody $ARGUMENTS: got %q", result)
	}
}

func TestRenderBodyIndexed(t *testing.T) {
	body := "Migrate $ARGUMENTS[0] from $ARGUMENTS[1] to $ARGUMENTS[2]."
	args := []string{"SearchBar", "React", "Vue"}
	result := RenderBody(body, nil, args, "SearchBar React Vue")
	expected := "Migrate SearchBar from React to Vue."
	if !strings.Contains(result, expected) {
		t.Errorf("RenderBody indexed: got %q, want %q", result, expected)
	}
}

func TestRenderBodyShorthand(t *testing.T) {
	body := "Migrate $0 from $1 to $2."
	args := []string{"SearchBar", "React", "Vue"}
	result := RenderBody(body, nil, args, "SearchBar React Vue")
	expected := "Migrate SearchBar from React to Vue."
	if result != expected {
		t.Errorf("RenderBody shorthand: got %q, want %q", result, expected)
	}
	// Should NOT have ARGUMENTS appended since $N is a valid placeholder
	if strings.Contains(result, "ARGUMENTS:") {
		t.Errorf("RenderBody shorthand: should not append ARGUMENTS when $N placeholder exists, got %q", result)
	}
}

func TestRenderBodyNamed(t *testing.T) {
	body := "Say hello to $name in a friendly way."
	argNames := []string{"name"}
	args := []string{"World"}
	result := RenderBody(body, argNames, args, "World")
	expected := "Say hello to World in a friendly way."
	if result != expected {
		t.Errorf("RenderBody named: got %q, want %q", result, expected)
	}
	// Should NOT have ARGUMENTS appended since $name is a valid placeholder
	if strings.Contains(result, "ARGUMENTS:") {
		t.Errorf("RenderBody named: should not append ARGUMENTS when $name placeholder exists, got %q", result)
	}
}

func TestRenderBodyFallbackAppend(t *testing.T) {
	body := "Do something useful."
	result := RenderBody(body, nil, []string{"extra", "args"}, "extra args")
	if !strings.Contains(result, "ARGUMENTS: extra args") {
		t.Errorf("RenderBody fallback: got %q, want ARGUMENTS appended", result)
	}
}

func TestRenderBodyNoArgs(t *testing.T) {
	body := "Original body with $ARGUMENTS placeholder."
	result := RenderBody(body, nil, nil, "")
	if result != body {
		t.Errorf("RenderBody no args: got %q, want original body unchanged", result)
	}
}

func TestRenderBodyOutOfBoundsIndex(t *testing.T) {
	body := "Use $ARGUMENTS[5] here."
	args := []string{"only-one"}
	result := RenderBody(body, nil, args, "only-one")
	if strings.Contains(result, "$ARGUMENTS[5]") {
		t.Errorf("RenderBody out-of-bounds should replace with empty, got %q", result)
	}
}
