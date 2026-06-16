package ui

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// formatDuration
// ---------------------------------------------------------------------------

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0ms"},
		{1 * time.Millisecond, "1ms"},
		{500 * time.Millisecond, "500ms"},
		{999 * time.Millisecond, "999ms"},
		{1 * time.Second, "1.00s"},
		{1500 * time.Millisecond, "1.50s"},
		{10 * time.Second, "10.00s"},
	}
	for _, tc := range tests {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// formatTPS
// ---------------------------------------------------------------------------

func TestFormatTPS(t *testing.T) {
	tests := []struct {
		tps  float64
		want string
	}{
		{0, "0.0 t/s"},
		{-1, "0.0 t/s"},
		{23.7, "23.7 t/s"},
		{100.0, "100.0 t/s"},
		{0.5, "0.5 t/s"},
	}
	for _, tc := range tests {
		if got := formatTPS(tc.tps); got != tc.want {
			t.Errorf("formatTPS(%v) = %q, want %q", tc.tps, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// thinkModeCompact
// ---------------------------------------------------------------------------

func TestThinkModeCompact(t *testing.T) {
	tests := []struct {
		setting string
		want    string
	}{
		{"false", "💡❌"},
		{"", "💡❌"},        // unknown falls to default
		{"garbage", "💡❌"}, // unknown falls to default
		{"true", "💡"},
		{"low", "💡▁"},
		{"medium", "💡▄"},
		{"high", "💡▆"},
		{"max", "💡█"},
	}
	for _, tc := range tests {
		if got := thinkModeCompact(tc.setting); got != tc.want {
			t.Errorf("thinkModeCompact(%q) = %q, want %q", tc.setting, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// truncateHelp
// ---------------------------------------------------------------------------

func TestTruncateHelp(t *testing.T) {
	t.Run("fits", func(t *testing.T) {
		s := "short"
		if got := truncateHelp(s, 80); got != s {
			t.Errorf("expected unchanged, got %q", got)
		}
	})

	t.Run("zero_width", func(t *testing.T) {
		if got := truncateHelp("hello", 0); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("negative_width", func(t *testing.T) {
		if got := truncateHelp("hello", -5); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("truncated_ends_with_ellipsis", func(t *testing.T) {
		long := strings.Repeat("a", 100)
		got := truncateHelp(long, 10)
		if !strings.HasSuffix(got, "…") {
			t.Errorf("expected ellipsis suffix, got %q", got)
		}
		// Must fit within the budget.
		if len([]rune(got)) > 11 { // 10 chars + ellipsis (1 rune)
			t.Errorf("result too long: %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// stripReasoning
// ---------------------------------------------------------------------------

func TestStripReasoning(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"no_tags",
			"Hello world",
			"Hello world",
		},
		{
			"single_block",
			"<think>internal</think>Hello",
			"Hello",
		},
		{
			"multiple_blocks",
			"<think>a</think>Hi<think>b</think> there",
			"Hi there",
		},
		{
			"unclosed_tag",
			"prefix<think>never closed",
			"prefix",
		},
		{
			"only_think",
			"<think>only reasoning</think>",
			"",
		},
		{
			"whitespace_trimmed",
			"<think>x</think>  result  ",
			"result",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripReasoning(tc.input); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// renderHeader
// ---------------------------------------------------------------------------

func TestRenderHeader(t *testing.T) {
	left := "L"
	center := "C"
	right := "R"
	result := renderHeader(20, left, center, right)

	if !strings.HasPrefix(result, left) {
		t.Errorf("result should start with %q, got %q", left, result)
	}
	if !strings.HasSuffix(result, right) {
		t.Errorf("result should end with %q, got %q", right, result)
	}
	if !strings.Contains(result, center) {
		t.Errorf("result should contain center %q, got %q", center, result)
	}
	// Center must appear between left and right.
	li := strings.Index(result, left)
	ci := strings.Index(result, center)
	ri := strings.Index(result, right)
	if !(li < ci && ci < ri) {
		t.Errorf("order wrong: left=%d center=%d right=%d in %q", li, ci, ri, result)
	}
}

// ---------------------------------------------------------------------------
// indentLines
// ---------------------------------------------------------------------------

func TestIndentLines(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		prefix string
		want   string
	}{
		{"single_line", "hello", "  ", "  hello"},
		{"multi_line", "a\nb\nc", "  ", "  a\n  b\n  c"},
		{"empty_string", "", "> ", "> "},
		{"empty_prefix", "abc", "", "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := indentLines(tc.input, tc.prefix); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
