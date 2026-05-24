package skills

import (
	"testing"
)

func TestParseArgsBasic(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"hello", []string{"hello"}},
		{"hello world", []string{"hello", "world"}},
		{"  spaced   out  ", []string{"spaced", "out"}},
		{"one two three", []string{"one", "two", "three"}},
	}
	for _, tt := range tests {
		got := ParseArgs(tt.input)
		if !sliceEqual(got, tt.want) {
			t.Errorf("ParseArgs(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseArgsQuoted(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{`"hello world" second`, []string{"hello world", "second"}},
		{`first "middle part" last`, []string{"first", "middle part", "last"}},
		{`"only quoted"`, []string{"only quoted"}},
		{`""`, nil}, // empty quotes produce empty string which is dropped
	}
	for _, tt := range tests {
		got := ParseArgs(tt.input)
		if !sliceEqual(got, tt.want) {
			t.Errorf("ParseArgs(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseArgsEscapedQuote(t *testing.T) {
	input := `"say \"hi\"" world`
	got := ParseArgs(input)
	want := []string{`say "hi"`, "world"}
	if !sliceEqual(got, want) {
		t.Errorf("ParseArgs(%q) = %v, want %v", input, got, want)
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
