package ui

import (
	"askillama/internal/ollama"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) handleKeySettings(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "shift+tab":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
	case "down", "tab":
		if m.settingsCursor < 2 {
			m.settingsCursor++
		}
	case "enter":
		switch m.settingsCursor {
		case 0:
			m.cfg.Stream = !m.cfg.Stream
		case 2:
			m.cfg.HostURL = m.settingsURLInput.Value()
			_ = m.cfg.Save()
			m.client = ollama.NewClient(m.cfg.HostURL)
			m.state = stateChat
			m.infoMessage = "Settings saved successfully"
			if m.ready {
				m.viewport.SetContent(m.renderChatContent())
				m.viewport.GotoBottom()
			}
			return m, nil
		}
	case " ":
		switch m.settingsCursor {
		case 0:
			m.cfg.Stream = !m.cfg.Stream
		case 1:
			// If Space is pressed when URL input is focused, let it type space
			var cmd tea.Cmd
			m.settingsURLInput.Focus()
			m.settingsURLInput, cmd = m.settingsURLInput.Update(msg)
			return m, cmd
		}
	case "esc":
		m.state = stateChat
		if m.ready {
			m.viewport.SetContent(m.renderChatContent())
			m.viewport.GotoBottom()
		}
		return m, nil
	default:
		// If j/k is pressed and we are not in the text input
		if m.settingsCursor != 1 {
			if msg.String() == "k" && m.settingsCursor > 0 {
				m.settingsCursor--
			} else if msg.String() == "j" && m.settingsCursor < 2 {
				m.settingsCursor++
			}
		}
	}

	var cmd tea.Cmd
	if m.settingsCursor == 1 {
		m.settingsURLInput.Focus()
		m.settingsURLInput, cmd = m.settingsURLInput.Update(msg)
	} else {
		m.settingsURLInput.Blur()
	}

	return m, cmd
}

func (m Model) viewSettings() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(" AskiLlama - Universal Settings "))
	b.WriteString("\n\n")

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
	keyStyle := lipgloss.NewStyle().Width(15)

	// Stream Toggle (Cursor 0)
	var streamVal string
	if m.cfg.Stream {
		streamVal = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Bold(true).Render("ON")
	} else {
		streamVal = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Bold(true).Render("OFF")
	}

	streamLine := keyStyle.Render("Stream Mode:") + streamVal

	if m.settingsCursor == 0 {
		b.WriteString(cursorStyle.Render("> "))
		b.WriteString(selectedItemStyle.Render(streamLine))
	} else {
		b.WriteString("  ")
		b.WriteString(unselectedItemStyle.Render(streamLine))
	}
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(helpStyle.Render("(Enable or disable streaming responses)"))
	b.WriteString("\n\n")

	// Ollama URL (Cursor 1)
	urlLine := keyStyle.Render("Ollama URL:") + m.settingsURLInput.View()
	if m.settingsCursor == 1 {
		b.WriteString(cursorStyle.Render("> "))
		b.WriteString(selectedItemStyle.Render(urlLine))
	} else {
		b.WriteString("  ")
		b.WriteString(unselectedItemStyle.Render(urlLine))
	}
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(helpStyle.Render("(The address where your Ollama instance is running)"))
	b.WriteString("\n\n")

	// Save Button (Cursor 2)
	saveLine := "[ Apply and Save ]"
	if m.settingsCursor == 2 {
		b.WriteString(cursorStyle.Render("> "))
		b.WriteString(selectedItemStyle.Render(saveLine))
	} else {
		b.WriteString("  ")
		b.WriteString(unselectedItemStyle.Render(saveLine))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render(" (Up/Down or j/k: navigate, Enter/Space: toggle/select, Esc: cancel)"))
	b.WriteString("\n")

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
