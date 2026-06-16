package ui

import (
	"fmt"
	"strings"

	"askillama/internal/ollama"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Update is the Bubble Tea update function. It is a pure function that takes
// the current model and an incoming message and returns the next model state
// plus any side-effecting command to run.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m = m.handleWindowSize(msg)

	case tea.KeyMsg:
		// Clear transient UI state on any keypress in chat mode.
		if m.state == stateChat {
			m.infoMessage = ""
			m.err = nil
		}
		// Global shortcut — quit from any state.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		var keyCmd tea.Cmd
		m, keyCmd = m.handleKey(msg)
		cmds = append(cmds, keyCmd)

	case modelsFetchedMsg:
		m = m.handleModelsFetched(msg)

	case responseMsg:
		m = m.handleResponseMsg(msg)

	case StreamMsg:
		var streamCmd tea.Cmd
		m, streamCmd = m.handleStreamMsg(msg)
		cmds = append(cmds, streamCmd)

	case tea.MouseMsg:
		if m.state == stateChat && m.ready {
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	// Always forward events to the text input when in chat mode so typing,
	// deletion, and cursor movement work. Also clamp the popup cursor.
	if m.state == stateChat {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)

		if m.isPopupActive() {
			matches := m.getMatchingActions()
			if len(matches) > 0 && m.popupCursor >= len(matches) {
				m.popupCursor = len(matches) - 1
			}
			if m.popupCursor < 0 {
				m.popupCursor = 0
			}
		}

		// Recalculate viewport height — it shrinks when the popup is visible.
		if m.ready {
			m.viewport.Height = max(m.height-m.baseHeight(), 3)
		}
	}

	return m, tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// Window resize
// ---------------------------------------------------------------------------

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) Model {
	m.width = msg.Width
	m.height = msg.Height
	m.renderedMessages = nil

	if m.width < 80 || m.height < 18 {
		return m
	}

	vHeight := max(m.height-m.baseHeight(), 3)
	vWidth := m.width - 8

	if !m.ready {
		m.viewport.Width = vWidth
		m.viewport.Height = vHeight
		m.viewport.YPosition = 0
		m.viewport.SetContent(m.renderChatContent())
		m.ready = true
	} else {
		m.viewport.Width = vWidth
		m.viewport.Height = vHeight
		m.viewport.SetContent(m.renderChatContent())
	}

	if m.state == stateCopy {
		m.scrollToSelectedMessage()
	}

	// Input width = total viewport width minus the compact think-mode box.
	thinkBoxWidth := 10
	inputWidth := vWidth + 8 - thinkBoxWidth
	m.textInput.Width = inputWidth - 6

	return m
}

// ---------------------------------------------------------------------------
// Key dispatch
// ---------------------------------------------------------------------------

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.state {
	case stateLoadingModels:
		return m.handleKeyLoadingModels(msg)
	case stateSelectModel:
		return m.handleKeySelectModel(msg)
	case stateChat:
		return m.handleKeyChat(msg)
	case stateCopy:
		return m.handleKeyCopy(msg)
	}
	return m, nil
}

func (m Model) handleKeyLoadingModels(msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleKeySelectModel(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.models)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.models) > 0 {
			m.cfg.CurrentModel = m.models[m.cursor]
			_ = m.cfg.Save()
			m.state = stateChat
			m.err = nil
			if m.ready {
				m.viewport.SetContent(m.renderChatContent())
				m.viewport.GotoBottom()
			}
		}
	case "r":
		m.state = stateLoadingModels
		m.err = nil
		return m, m.fetchModelsCmd()
	case "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleKeyChat(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Enter copy-mode.
	if msg.String() == "ctrl+y" {
		if len(m.messages) > 0 {
			m.state = stateCopy
			m.copyCursor = len(m.messages) - 1
			m.renderedMessages = nil
			m.textInput.Blur()
			m.scrollToSelectedMessage()
			if m.ready {
				m.viewport.SetContent(m.renderChatContent())
			}
		} else {
			m.err = fmt.Errorf("no messages to copy")
		}
		return m, nil
	}

	// If the autocomplete popup is open, handle its navigation first.
	if m.isPopupActive() {
		if handled, newM, cmd := m.handlePopupKey(msg); handled {
			return newM, cmd
		}
	}

	// Viewport scroll shortcuts — handled before passing to text input.
	switch msg.String() {
	case "pgup":
		m.viewport.HalfPageUp()
		return m, nil
	case "pgdown":
		m.viewport.HalfPageDown()
		return m, nil
	case "ctrl+up":
		m.viewport.ScrollUp(1)
		return m, nil
	case "ctrl+down":
		m.viewport.ScrollDown(1)
		return m, nil
	}

	// Enter: dispatch the typed value as a command or a chat message.
	if msg.Type == tea.KeyEnter {
		return m.handleEnter()
	}

	return m, nil
}

// handlePopupKey processes key events while the autocomplete popup is visible.
// Returns (true, model, cmd) when the key was consumed by the popup.
func (m Model) handlePopupKey(msg tea.KeyMsg) (bool, Model, tea.Cmd) {
	matches := m.getMatchingActions()
	switch msg.String() {
	case "up", "ctrl+k":
		if len(matches) > 0 {
			m.popupCursor = (m.popupCursor - 1 + len(matches)) % len(matches)
		}
		return true, m, nil

	case "down", "ctrl+j", "tab":
		if len(matches) > 0 {
			m.popupCursor = (m.popupCursor + 1) % len(matches)
		}
		return true, m, nil

	case "enter":
		if len(matches) > 0 {
			selected := matches[m.popupCursor].key
			// Look up the command in the table to decide what to do:
			//   - No-argument command (key has no trailing space) → dispatch now.
			//   - Argument command (key has trailing space) → pre-fill so the
			//     user can type the argument before pressing Enter again.
			for _, c := range slashCommands {
				if c.key == selected {
					// Exact-match (no-arg): dispatch immediately.
					newM, cmd := c.handler(m, "")
					return true, newM, cmd
				}
				if strings.TrimRight(c.key, " ") == selected {
					// Prefix-match (with-arg): pre-fill the full key including
					// the trailing space so the cursor lands after it.
					prefillInput(&m, c.key)
					return true, m, nil
				}
			}
			return true, m, nil
		}
		// No popup matches (e.g. user typed "/think high") — let Enter fall
		// through to handleEnter() so the command gets dispatched normally.
		return false, m, nil

	case "esc":
		m.textInput.Reset()
		m.popupCursor = 0
		if m.ready {
			m.viewport.Height = max(m.height-m.baseHeight(), 3)
		}
		return true, m, nil
	}
	return false, m, nil
}

// handleEnter processes the Enter key in chat mode: dispatches slash commands
// or sends the typed text to Ollama.
func (m Model) handleEnter() (Model, tea.Cmd) {
	val := m.textInput.Value()
	if len([]rune(val)) == 0 {
		return m, nil
	}

	// Try slash-command dispatch first.
	if newM, cmd, handled := m.handleSlashCommand(val); handled {
		return newM, cmd
	}

	// Not a command — send as a chat message.
	userMsg := ollama.Message{Role: "user", Content: val}
	m.messages = append(m.messages, userMsg)
	m.messagesMetrics = append(m.messagesMetrics, nil)
	m.textInput.Reset()
	m.isResponding = true
	m.err = nil

	if m.ready {
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
	}

	if m.cfg.Stream {
		m.appendAssistantPlaceholder()
		ch := make(chan StreamMsg)
		return m, tea.Batch(
			m.sendStreamMessageCmd(ch),
			listenToStream(ch),
		)
	}
	return m, m.sendMessageCmd()
}

func (m Model) handleKeyCopy(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.copyCursor > 0 {
			m.copyCursor--
			m.scrollToSelectedMessage()
			if m.ready {
				m.viewport.SetContent(m.renderChatContent())
			}
		}

	case "down", "j":
		if m.copyCursor < len(m.messages)-1 {
			m.copyCursor++
			m.scrollToSelectedMessage()
			if m.ready {
				m.viewport.SetContent(m.renderChatContent())
			}
		}

	case "enter":
		if len(m.messages) > 0 && m.copyCursor >= 0 && m.copyCursor < len(m.messages) {
			selectedContent := m.messages[m.copyCursor].Content
			if err := clipboard.WriteAll(selectedContent); err != nil {
				m.err = fmt.Errorf("failed to copy: %v", err)
			} else {
				m.infoMessage = "Copied message to clipboard!"
			}
			m.state = stateChat
			m.renderedMessages = nil
			m.textInput.Focus()
			m.refreshViewport()
		}

	case "esc", "ctrl+y":
		m.state = stateChat
		m.renderedMessages = nil
		m.textInput.Focus()
		m.refreshViewport()
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Message handlers
// ---------------------------------------------------------------------------

func (m Model) handleModelsFetched(msg modelsFetchedMsg) Model {
	m.state = stateSelectModel
	if msg.err != nil {
		m.err = msg.err
		return m
	}
	m.models = msg.models
	m.err = nil
	if len(m.models) == 0 {
		m.err = fmt.Errorf("no models found. Please run 'ollama pull <model>' first")
	}
	return m
}

func (m Model) handleResponseMsg(msg responseMsg) Model {
	m.isResponding = false
	if msg.err != nil {
		m.err = msg.err
	} else {
		m.err = nil
		assistantMsg := ollama.Message{Role: "assistant", Content: msg.content}
		m.messages = append(m.messages, assistantMsg)
		m.messagesMetrics = append(m.messagesMetrics, &msg.metrics)
		m.inputTokens += msg.metrics.PromptTokens
		m.outputTokens += msg.metrics.EvalTokens
	}
	m.refreshViewport()
	return m
}

func (m Model) handleStreamMsg(msg StreamMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.err = msg.Err
		m.isResponding = false
		// Remove the empty assistant placeholder if no content arrived.
		if len(m.messages) > 0 &&
			m.messages[len(m.messages)-1].Role == "assistant" &&
			m.messages[len(m.messages)-1].Content == "" {
			m.messages = m.messages[:len(m.messages)-1]
			m.messagesMetrics = m.messagesMetrics[:len(m.messagesMetrics)-1]
		}
		m.refreshViewport()
		return m, nil
	}

	// Accumulate the chunk into the last assistant message.
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
		m.messages[len(m.messages)-1].Content += msg.Content
	}

	if msg.Done {
		m.isResponding = false
		m.inputTokens += msg.Metrics.PromptTokens
		m.outputTokens += msg.Metrics.EvalTokens
		if len(m.messagesMetrics) > 0 {
			m.messagesMetrics[len(m.messagesMetrics)-1] = &msg.Metrics
		}
		m.refreshViewport()
		return m, nil
	}

	if m.ready {
		m.viewport.SetContent(m.renderChatContent())
		m.viewport.GotoBottom()
	}

	// Schedule the next chunk read.
	return m, listenToStream(msg.Channel)
}

// ---------------------------------------------------------------------------
// textinput.Blink forwarder — required for cursor animation
// ---------------------------------------------------------------------------

// ensure textinput.Blink is imported (it's called from Init).
var _ = textinput.Blink
