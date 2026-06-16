package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// formatDuration formats a duration as milliseconds (< 1s) or seconds (≥ 1s).
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// formatTPS formats a tokens-per-second value for display.
// Non-positive values are shown as "0.0 t/s" to guard against negative or
// uninitialized metrics.
func formatTPS(tps float64) string {
	if tps <= 0 {
		return "0.0 t/s"
	}
	return fmt.Sprintf("%.1f t/s", tps)
}

// thinkModeCompact returns a compact emoji+block indicator for the current
// think setting. The block-fill character encodes intensity:
// ▁ = low, ▄ = medium, ▆ = high, █ = max.
func thinkModeCompact(setting string) string {
	switch setting {
	case "true":
		return "💡"
	case "low":
		return "💡▁"
	case "medium":
		return "💡▄"
	case "high":
		return "💡▆"
	case "max":
		return "💡█"
	default: // "false" or anything unrecognised
		return "💡❌"
	}
}

// truncateHelp clips text to maxWidth display columns, appending '…' if
// truncated. This guarantees the help bar is always exactly one line regardless
// of terminal width. Uses lipgloss.Width for correct multi-byte/ANSI handling.
func truncateHelp(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= maxWidth {
		return text
	}
	// Walk runes from the right until it fits with the ellipsis appended.
	runes := []rune(text)
	for i := len(runes) - 1; i > 0; i-- {
		candidate := string(runes[:i]) + "…"
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
	}
	return "…"
}

// stripReasoning removes all <think>…</think> blocks from content.
// An unclosed <think> tag causes everything from it to the end to be removed.
// Used when thinkSetting == "false" to hide model reasoning from the UI.
func stripReasoning(content string) string {
	for {
		start := strings.Index(content, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], "</think>")
		if end == -1 {
			// Unclosed tag — strip from here to end of string.
			content = content[:start]
			break
		}
		content = content[:start] + content[start+end+8:]
	}
	return strings.TrimSpace(content)
}

// renderHeader builds the one-line header string with left, center, and right
// segments. The center segment is placed as close to the true horizontal
// midpoint as possible; left and right segments are given at least one space
// of padding so they never collide.
func renderHeader(width int, left, center, right string) string {
	leftWidth := lipgloss.Width(left)
	centerWidth := lipgloss.Width(center)
	rightWidth := lipgloss.Width(right)

	// Try to place center exactly in the middle of the full header width.
	centerStart := (width - centerWidth) / 2
	leftPad := centerStart - leftWidth
	if leftPad < 1 {
		leftPad = 1
	}
	rightPad := width - leftWidth - leftPad - centerWidth - rightWidth
	if rightPad < 1 {
		rightPad = 1
	}

	return left + strings.Repeat(" ", leftPad) + center + strings.Repeat(" ", rightPad) + right
}

// indentLines prepends prefix to every line in s.
func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
