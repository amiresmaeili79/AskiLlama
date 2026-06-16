package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View is the Bubble Tea view function. It is a pure function of the model
// state — it must not modify m.
func (m Model) View() string {
	// If the terminal is too small to render, show a centred warning instead.
	if m.width > 0 && m.height > 0 && (m.width < 80 || m.height < 18) {
		warning := fmt.Sprintf(
			" Terminal too small: %dx%d\n Please resize to at least 80x18.",
			m.width, m.height,
		)
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			errorStyle.Render(warning),
		)
	}

	switch m.state {
	case stateLoadingModels:
		return m.viewLoadingModels()
	case stateSelectModel:
		return m.viewSelectModel()
	case stateChat, stateCopy:
		return m.viewChat()
	case stateSettings:
		return m.viewSettings()
	}
	return ""
}

// ---------------------------------------------------------------------------
// Per-state view helpers
// ---------------------------------------------------------------------------

func (m Model) viewLoadingModels() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(" AskiLlama "))
	b.WriteString("\n\n")
	b.WriteString(" Connecting to Ollama and fetching models...\n\n")
	if m.cfg.HostURL != "" {
		b.WriteString(systemMsgStyle.Render(fmt.Sprintf(" Host: %s", m.cfg.HostURL)))
	}

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#888888")).
			Padding(2, 4).
			Render(b.String()),
	)
}

func (m Model) viewSelectModel() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(" AskiLlama - Select Model "))
	b.WriteString("\n\n")

	switch {
	case m.err != nil:
		b.WriteString(errorStyle.Render(fmt.Sprintf(" Error: %v", m.err)))
		b.WriteString("\n\n")
		b.WriteString(" Ensure Ollama is running and accessible.\n\n")
		b.WriteString(" [r] Retry fetching models\n")
		b.WriteString(" [Ctrl+C] Quit\n")

	case len(m.models) == 0:
		b.WriteString(systemMsgStyle.Render(" No models found on this Ollama host.\n\n"))
		b.WriteString(" Please run 'ollama pull <model>' to download a model first.\n\n")
		b.WriteString(" [r] Refresh\n")
		b.WriteString(" [Ctrl+C] Quit\n")

	default:
		b.WriteString(" Select a model to chat with:\n\n")
		for i, name := range m.models {
			if i == m.cursor {
				b.WriteString(cursorStyle.Render("> "))
				b.WriteString(selectedItemStyle.Render(name))
			} else {
				b.WriteString("  ")
				b.WriteString(unselectedItemStyle.Render(name))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n (use Up/Down or j/k to navigate, Enter to select, Ctrl+C to quit)\n")
	}

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(2, 4).
			Render(b.String()),
	)
}

func (m Model) viewChat() string {
	if !m.ready {
		return "Initializing layout..."
	}

	var views []string

	// 1. Header: title on left, model name centred, token stats on right.
	modeStr := "⚡ stream"
	if !m.cfg.Stream {
		modeStr = "📦 batch"
	}
	headerLeft := titleStyle.Render(" AskiLlama ")
	headerCenter := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FF79C6")).
		Render(m.cfg.CurrentModel)
	headerRight := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Render(fmt.Sprintf("%s | 📥 %d | 📤 %d", modeStr, m.inputTokens, m.outputTokens))
	header := renderHeader(m.width, headerLeft, headerCenter, headerRight)

	// 2. Viewport (with rounded border).
	viewportBox := viewportContainerStyle.
		Width(m.viewport.Width + 6).
		Height(m.viewport.Height + 2).
		Render(m.viewport.View())

	// 3. Bottom row: text input (wide) + compact think-mode box (narrow).
	thinkBoxWidth := 10
	inputWidth := m.viewport.Width + 8 - thinkBoxWidth

	// Input border turns green in copy-mode to signal the mode change.
	inputBorderColor := "#00ADD8"
	if m.state == stateCopy {
		inputBorderColor = "#50FA7B"
	}
	inputBox := inputContainerStyle.
		BorderForeground(lipgloss.Color(inputBorderColor)).
		Width(inputWidth - 2).
		Render(m.textInput.View())

	thinkBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 1).
		Align(lipgloss.Center).
		Width(thinkBoxWidth - 2).
		Render(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF79C6")).Render(thinkModeCompact(m.thinkSetting)))

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, inputBox, thinkBox)

	views = append(views, header, "", viewportBox)

	// 4. Autocomplete popup (only when input starts with '/').
	if m.isPopupActive() {
		if popup := m.renderPopup(); popup != "" {
			views = append(views, popup)
		}
	}

	views = append(views, bottomRow)

	// 5. Help bar — single line, truncated to fit.
	helpTextStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Italic(true).
		PaddingLeft(2)
	maxHelpWidth := m.width - 2
	var helpText string
	if m.state == stateCopy {
		helpText = helpTextStyle.Render(truncateHelp("Copy Mode: Up/Down or j/k to select • Enter to copy • Esc / Ctrl+Y to exit", maxHelpWidth))
	} else {
		helpText = helpTextStyle.Render(truncateHelp("(write / for actions) • Ctrl+Y: copy mode • PgUp/PgDn: scroll page • Ctrl+Up/Down: scroll line", maxHelpWidth))
	}
	views = append(views, helpText)

	return lipgloss.JoinVertical(lipgloss.Left, views...)
}
