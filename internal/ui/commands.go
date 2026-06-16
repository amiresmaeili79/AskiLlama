package ui

import (
	"fmt"
	"strings"

	"askillama/internal/ollama"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Command table
// ---------------------------------------------------------------------------

// slashCmd maps a key to its handler function.
//
// Key convention:
//   - No trailing space  →  exact match, no argument  (e.g. "/new")
//   - Trailing space     →  prefix match, argument follows  (e.g. "/think ")
//
// The dispatcher uses this convention to:
//
//	a) auto-prefill the input when the user types a bare argument-expecting command
//	b) strip and pass the argument to the handler when the full form is typed
//
// The handler always receives a (possibly empty) argument string and returns
// the updated model plus an optional tea.Cmd.
//
// To add a new slash command:
//  1. Write a cmdFoo handler below.
//  2. Add an entry to slashCommands (with or without trailing space).
//  3. Add the corresponding entry to the actions slice in model.go.
type slashCmd struct {
	key     string
	handler func(m Model, arg string) (Model, tea.Cmd)
}

var slashCommands = []slashCmd{
	// No-argument commands (exact match)
	{"/model", cmdModel},
	{"/new", cmdNew},

	// Argument commands (prefix match — note the trailing space)
	{"/think ", cmdThink},
	{"/stream ", cmdStream},
	{"/system ", cmdSystem},
	{"/save ", cmdSave},
	{"/load ", cmdLoad},
	{"/export ", cmdExport},
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resetInput clears the text input, resets the popup cursor, and recalculates
// the viewport height. Called after every slash command is dispatched.
func resetInput(m *Model) {
	m.textInput.Reset()
	m.popupCursor = 0
	if m.ready {
		m.viewport.Height = max(m.height-m.baseHeight(), 3)
	}
}

// prefillInput sets the text input to value and moves the cursor to the end.
// Used when the user types a bare argument command ("/think") and we want to
// nudge them to complete it ("/think ").
func prefillInput(m *Model, value string) {
	m.textInput.SetValue(value)
	m.textInput.SetCursor(len(value))
	m.popupCursor = 0
	if m.ready {
		m.viewport.Height = max(m.height-m.baseHeight(), 3)
	}
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

// handleSlashCommand processes a complete slash-command value (the full text
// currently in the input box when the user pressed Enter).
//
// Returns the updated model, an optional tea.Cmd, and handled=true when val
// was recognised as a slash command. When handled=false the caller should treat
// val as a regular chat message and send it to Ollama.
func (m Model) handleSlashCommand(val string) (Model, tea.Cmd, bool) {
	for _, c := range slashCommands {
		if strings.HasSuffix(c.key, " ") {
			// Argument command.
			bare := strings.TrimRight(c.key, " ")
			if val == bare {
				// User typed the bare command without an argument — pre-fill
				// the trailing space so they can continue typing.
				prefillInput(&m, c.key)
				return m, nil, true
			}
			if after, ok := strings.CutPrefix(val, c.key); ok {
				m, cmd := c.handler(m, strings.TrimSpace(after))
				return m, cmd, true
			}
		} else {
			// Exact-match command (no argument).
			if val == c.key {
				m, cmd := c.handler(m, "")
				return m, cmd, true
			}
		}
	}

	// Not a recognised slash command.
	return m, nil, false
}

// ---------------------------------------------------------------------------
// Individual command handlers
// ---------------------------------------------------------------------------

// cmdModel handles "/model" — clears the selected model and opens the picker.
func cmdModel(m Model, _ string) (Model, tea.Cmd) {
	m.cfg.CurrentModel = ""
	_ = m.cfg.Save()
	m.state = stateLoadingModels
	m.err = nil
	resetInput(&m)
	return m, m.fetchModelsCmd()
}

// cmdNew handles "/new" — clears the conversation and starts fresh.
func cmdNew(m Model, _ string) (Model, tea.Cmd) {
	m.messages = nil
	m.messagesMetrics = nil
	m.renderedMessages = nil
	m.inputTokens = 0
	m.outputTokens = 0
	m.err = nil
	resetInput(&m)
	m.refreshViewport()
	return m, nil
}

// cmdThink handles "/think <setting>".
// Valid settings: false | true | low | medium | high | max
func cmdThink(m Model, arg string) (Model, tea.Cmd) {
	switch arg {
	case "false", "true", "low", "medium", "high", "max":
		m.thinkSetting = arg
		m.renderedMessages = nil
		m.err = nil
		resetInput(&m)
		m.refreshViewport()
	default:
		m.err = fmt.Errorf("invalid think setting. Choose from: false, true, low, medium, high, max")
		m.textInput.Reset()
	}
	return m, nil
}

// cmdStream handles "/stream <setting>".
// Valid settings: true | yes | on | false | no | off
func cmdStream(m Model, arg string) (Model, tea.Cmd) {
	switch arg {
	case "true", "yes", "on":
		m.cfg.Stream = true
		_ = m.cfg.Save()
		m.err = nil
		resetInput(&m)
		m.refreshViewport()
	case "false", "no", "off":
		m.cfg.Stream = false
		_ = m.cfg.Save()
		m.err = nil
		resetInput(&m)
		m.refreshViewport()
	default:
		m.err = fmt.Errorf("invalid stream setting. Choose from: true, false")
		m.textInput.Reset()
	}
	return m, nil
}

// cmdSystem handles "/system <prompt>".
// An empty prompt removes the system message; a non-empty prompt sets or
// replaces it (always at index 0).
func cmdSystem(m Model, arg string) (Model, tea.Cmd) {
	if arg != "" {
		// Replace the existing system message or prepend a new one.
		if len(m.messages) > 0 && m.messages[0].Role == "system" {
			m.messages[0].Content = arg
		} else {
			m.messages = append([]ollama.Message{{Role: "system", Content: arg}}, m.messages...)
			m.messagesMetrics = append([]*ollama.ResponseMetrics{nil}, m.messagesMetrics...)
		}
	} else {
		// Empty prompt → remove the system message if one exists.
		if len(m.messages) > 0 && m.messages[0].Role == "system" {
			m.messages = m.messages[1:]
			m.messagesMetrics = m.messagesMetrics[1:]
		}
	}
	m.renderedMessages = nil
	m.err = nil
	resetInput(&m)
	m.refreshViewport()
	return m, nil
}

// cmdSave handles "/save <name>".
func cmdSave(m Model, arg string) (Model, tea.Cmd) {
	if err := m.saveSession(arg); err != nil {
		m.err = err
	} else {
		m.infoMessage = fmt.Sprintf("Session '%s' saved successfully!", arg)
	}
	resetInput(&m)
	m.refreshViewport()
	return m, nil
}

// cmdLoad handles "/load <name>".
func cmdLoad(m Model, arg string) (Model, tea.Cmd) {
	if err := m.loadSession(arg); err != nil {
		m.err = err
	} else {
		m.infoMessage = fmt.Sprintf("Session '%s' loaded successfully!", arg)
	}
	m.renderedMessages = nil
	resetInput(&m)
	m.refreshViewport()
	return m, nil
}

// cmdExport handles "/export <filename>".
// The .md extension is enforced by exportChat; we normalise the name here
// only to build the success message that matches what was actually written.
func cmdExport(m Model, arg string) (Model, tea.Cmd) {
	if err := m.exportChat(arg); err != nil {
		m.err = err
	} else {
		filename := arg
		if filename == "" {
			filename = "askillama-chat.md"
		} else if !strings.HasSuffix(strings.ToLower(filename), ".md") {
			filename += ".md"
		}
		m.infoMessage = fmt.Sprintf("Chat exported to '%s' successfully!", filename)
	}
	resetInput(&m)
	m.refreshViewport()
	return m, nil
}
