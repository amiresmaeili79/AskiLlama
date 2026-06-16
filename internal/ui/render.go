package ui

import (
	"fmt"
	"strings"

	"askillama/internal/ollama"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// renderMessage
// ---------------------------------------------------------------------------

// renderMessage renders a single chat message (role label + content) into a
// display string that fits within innerWidth columns.
func (m *Model) renderMessage(i int, innerWidth int) string {
	msg := m.messages[i]

	// --- Role label ---
	var labelText string
	var labelStyle lipgloss.Style
	var msgColor lipgloss.Color

	switch msg.Role {
	case "user":
		labelText = " You "
		labelStyle = userLabelStyle
		msgColor = lipgloss.Color("#FAFAFA")
	case "system":
		labelText = " System Prompt "
		labelStyle = systemLabelStyle
		msgColor = lipgloss.Color("#F1FA8C")
	default: // "assistant"
		labelText = " Ollama "
		labelStyle = assistantLabelStyle
		msgColor = lipgloss.Color("#DDDDDD")
	}

	roleLabel := labelStyle.Render(labelText)

	// Append performance metrics inline with the assistant label.
	if msg.Role == "assistant" && i < len(m.messagesMetrics) && m.messagesMetrics[i] != nil {
		met := m.messagesMetrics[i]
		metricsStr := fmt.Sprintf(" [ %s | TTFT: %s | Total: %s ]",
			formatTPS(met.TokensPerSecond),
			formatDuration(met.TimeToFirstToken),
			formatDuration(met.TotalDuration),
		)
		roleLabel += " " + metricsStyle.Render(metricsStr)
	}

	// In copy-mode, prefix the label with a cursor arrow for the selected
	// message and three spaces for all others, keeping content aligned.
	if m.state == stateCopy {
		if i == m.copyCursor {
			copyArrow := lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Bold(true).Render("-> ")
			roleLabel = copyArrow + roleLabel
		} else {
			roleLabel = "   " + roleLabel
		}
	}

	// --- Content ---
	contentWidth := max(innerWidth-lipgloss.Width(roleLabel)-1, 10)

	// Normalize tabs to spaces to prevent terminal wrapping issues.
	content := strings.ReplaceAll(msg.Content, "\t", "    ")
	if msg.Role == "assistant" && m.thinkSetting == "false" {
		content = stripReasoning(content)
	}

	var body string
	if msg.Role == "assistant" {
		body = m.renderAssistantBody(i, content, contentWidth)
	}

	// Fallback: plain styled text for user/system or when glamour fails.
	if body == "" {
		body = lipgloss.NewStyle().Width(contentWidth).Foreground(msgColor).Render(content)
	}

	// Indent user messages slightly for visual breathing room.
	if msg.Role == "user" {
		body = indentLines(body, "  ")
	}

	// In copy-mode, indent all content to align with the cursor arrow.
	if m.state == stateCopy {
		body = indentLines(body, "   ")
	}

	// User messages get a blank line between label and content; others don't.
	labelSep := "\n"
	if msg.Role == "user" {
		labelSep = "\n\n"
	}

	return roleLabel + labelSep + body
}

// renderAssistantBody renders the assistant message content via glamour
// (Markdown), with a render cache to avoid re-rendering completed messages.
//
// Cache logic:
//   - The last message is considered "live" while m.isResponding is true and
//     is always re-rendered from the current content so streaming updates show.
//   - All other completed messages use the cache. The cache is invalidated
//     (set to nil) whenever the message list length or window size changes.
func (m *Model) renderAssistantBody(i int, content string, contentWidth int) string {
	// Ensure the render cache is the same length as the message slice.
	if len(m.renderedMessages) != len(m.messages) {
		m.renderedMessages = make([]string, len(m.messages))
	}

	isStreaming := m.isResponding && i == len(m.messages)-1
	if !isStreaming && m.renderedMessages[i] != "" {
		return m.renderedMessages[i]
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(contentWidth),
	)
	if err != nil {
		return ""
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return ""
	}
	body := strings.TrimRight(rendered, "\n")
	if !isStreaming {
		m.renderedMessages[i] = body
	}
	return body
}

// ---------------------------------------------------------------------------
// renderChatContent
// ---------------------------------------------------------------------------

// renderChatContent builds the full viewport content string from all messages.
// It also appends a typing indicator and any error/info banner.
func (m *Model) renderChatContent() string {
	if len(m.messages) == 0 {
		return systemMsgStyle.Render(" No messages yet. Start a conversation by typing below!")
	}

	// Inner width accounts for viewport border + padding added by the container style.
	innerWidth := max(m.viewport.Width-2, 10)

	// Ensure the render cache matches the current message count.
	if len(m.renderedMessages) != len(m.messages) {
		m.renderedMessages = make([]string, len(m.messages))
	}

	var s strings.Builder
	for i := range m.messages {
		if i > 0 {
			s.WriteString("\n\n")
		}
		s.WriteString(m.renderMessage(i, innerWidth))
	}

	// Show a typing indicator when the assistant has been asked but hasn't
	// produced any content yet (e.g. during model load / first-token delay).
	if m.isResponding {
		lastMsg := m.messages[len(m.messages)-1]
		if lastMsg.Role != "assistant" || lastMsg.Content == "" {
			s.WriteString("\n\n")
			s.WriteString(systemMsgStyle.Render(" Ollama is typing..."))
		}
	}

	if m.err != nil {
		s.WriteString("\n\n")
		s.WriteString(errorStyle.Render(fmt.Sprintf(" Error: %v", m.err)))
	} else if m.infoMessage != "" {
		s.WriteString("\n\n")
		s.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Bold(true).Render(fmt.Sprintf(" %s", m.infoMessage)))
	}

	return s.String()
}

// ---------------------------------------------------------------------------
// renderPopup
// ---------------------------------------------------------------------------

// renderPopup renders the autocomplete popup that appears when the user types
// a '/' prefix in the text input.
func (m Model) renderPopup() string {
	matches := m.getMatchingActions()
	if len(matches) == 0 {
		return ""
	}

	var s strings.Builder
	for i, act := range matches {
		isSelected := (i == m.popupCursor)
		var line string
		if isSelected {
			cursorSymbol := cursorStyle.Render("> ")
			actionKey := selectedItemStyle.Render(act.key)
			actionDesc := lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA")).Italic(true).Render(" (" + act.description + ")")
			line = fmt.Sprintf("%s%s%s", cursorSymbol, actionKey, actionDesc)
		} else {
			cursorSymbol := "  "
			actionKey := unselectedItemStyle.Render(act.key)
			actionDesc := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Italic(true).Render(" (" + act.description + ")")
			line = fmt.Sprintf("%s%s%s", cursorSymbol, actionKey, actionDesc)
		}
		s.WriteString(line)
		if i < len(matches)-1 {
			s.WriteString("\n")
		}
	}

	return popupContainerStyle.
		Width(m.viewport.Width).
		Render(s.String())
}

// ---------------------------------------------------------------------------
// scrollToSelectedMessage
// ---------------------------------------------------------------------------

// scrollToSelectedMessage adjusts the viewport's YOffset so that the message
// at m.copyCursor is visible. It scrolls the minimum amount necessary —
// jumping only to the top of the selected message, not re-centering.
func (m *Model) scrollToSelectedMessage() {
	if len(m.messages) == 0 || m.copyCursor < 0 || m.copyCursor >= len(m.messages) {
		return
	}

	innerWidth := max(m.viewport.Width, 10)

	// Count display lines rendered by all messages before the selected one.
	// Each message is separated by "\n\n" (2 extra lines).
	lineCountBefore := 0
	for i := range m.messages {
		if i >= m.copyCursor {
			break
		}
		rendered := m.renderMessage(i, innerWidth)
		lineCountBefore += strings.Count(rendered, "\n") + 1 + 2
	}

	viewportTop := m.viewport.YOffset
	viewportBottom := m.viewport.YOffset + m.viewport.Height

	if lineCountBefore < viewportTop {
		// Selected message is above the viewport — scroll up.
		m.viewport.YOffset = lineCountBefore
	} else if lineCountBefore >= viewportBottom {
		// Selected message is below the viewport — scroll down just enough to
		// reveal its top line (where the arrow indicator appears).
		m.viewport.YOffset = lineCountBefore
	}

	if m.viewport.YOffset < 0 {
		m.viewport.YOffset = 0
	}
}

// ---------------------------------------------------------------------------
// refreshViewport
// ---------------------------------------------------------------------------

// refreshViewport re-renders the chat content and scrolls to the bottom.
// It is a no-op when the viewport is not yet initialised.
func (m *Model) refreshViewport() {
	if m.ready {
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
	}
}

// appendAssistantPlaceholder adds an empty assistant message + nil metrics
// entry to act as the streaming target before any chunks arrive.
func (m *Model) appendAssistantPlaceholder() {
	m.messages = append(m.messages, ollama.Message{Role: "assistant", Content: ""})
	m.messagesMetrics = append(m.messagesMetrics, nil)
}
