package tui

import "testing"

// TestTruncateResult pins the rune-based char cap that prevents big tool
// outputs from flooding the TUI. Must NOT slice mid-rune (CJK content),
// must report a meaningful "total" count, and must short-circuit when
// the input is already small.
func TestTruncateResult(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		max            int
		wantPreview    string
		wantTruncated  bool
		wantTotalRunes int
	}{
		{
			name:           "empty input",
			in:             "",
			max:            100,
			wantPreview:    "",
			wantTruncated:  false,
			wantTotalRunes: 0,
		},
		{
			name:           "shorter than cap returns input unchanged",
			in:             "hello world",
			max:            100,
			wantPreview:    "hello world",
			wantTruncated:  false,
			wantTotalRunes: 11,
		},
		{
			name:           "exactly at cap is not truncated",
			in:             "0123456789",
			max:            10,
			wantPreview:    "0123456789",
			wantTruncated:  false,
			wantTotalRunes: 10,
		},
		{
			name:           "ascii over cap is sliced",
			in:             "abcdefghij" + "extra",
			max:            10,
			wantPreview:    "abcdefghij",
			wantTruncated:  true,
			wantTotalRunes: 15,
		},
		{
			name: "cjk slices on rune boundary, not byte boundary",
			// Each "中" is 3 bytes; if we sliced by byte we'd cut a rune
			// in half and produce a replacement char on render. By rune,
			// 3 chars cleanly come out as 3 chars (9 bytes).
			in:             "中文测试一二三四",
			max:            3,
			wantPreview:    "中文测",
			wantTruncated:  true,
			wantTotalRunes: 8,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			preview, truncated, total := truncateResult(c.in, c.max)
			if preview != c.wantPreview {
				t.Errorf("preview = %q; want %q", preview, c.wantPreview)
			}
			if truncated != c.wantTruncated {
				t.Errorf("truncated = %v; want %v", truncated, c.wantTruncated)
			}
			if total != c.wantTotalRunes {
				t.Errorf("totalRunes = %d; want %d", total, c.wantTotalRunes)
			}
		})
	}
}
