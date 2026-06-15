package ui

import (
	"fmt"
	"strings"

	"askillama/internal/config"
	"askillama/internal/ollama"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateLoadingModels state = iota
	stateSelectModel
	stateChat
)

type Model struct {
	cfg          *config.Config
	client       *ollama.Client
	messages     []ollama.Message
	textInput    textinput.Model
	err          error
	isResponding bool

	state  state
	models []string
	cursor int
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			MarginBottom(1)

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true)

	userLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00ADD8")) // Go blue

	assistantLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF79C6")) // Dracula pink

	cursorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4"))

	selectedItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	unselectedItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DDDDDD"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5555")).
			Bold(true)
)

func NewModel(cfg *config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "Ask Ollama something..."
	ti.Focus()
	ti.CharLimit = 1000
	ti.Width = 60

	client := ollama.NewClient(cfg.HostURL)

	initialState := stateChat
	if cfg.CurrentModel == "" {
		initialState = stateLoadingModels
	}

	return Model{
		cfg:       cfg,
		client:    client,
		textInput: ti,
		state:     initialState,
	}
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, textinput.Blink)
	if m.state == stateLoadingModels {
		cmds = append(cmds, m.fetchModelsCmd())
	}
	return tea.Batch(cmds...)
}

type modelsFetchedMsg struct {
	models []string
	err    error
}

func (m Model) fetchModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := m.client.ListModels()
		return modelsFetchedMsg{models: models, err: err}
	}
}

type responseMsg struct {
	content string
	err     error
}

func (m Model) sendMessageCmd(prompt string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.Chat(m.cfg.CurrentModel, m.messages)
		return responseMsg{content: resp, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global shortcut to quit
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}

		switch m.state {
		case stateLoadingModels:
			if msg.Type == tea.KeyEsc {
				return m, tea.Quit
			}

		case stateSelectModel:
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
				}
			case "r":
				m.state = stateLoadingModels
				m.err = nil
				return m, m.fetchModelsCmd()
			case "esc":
				return m, tea.Quit
			}

		case stateChat:
			switch msg.Type {
			case tea.KeyEsc:
				// Go back to model selection
				m.cfg.CurrentModel = ""
				_ = m.cfg.Save()
				m.state = stateLoadingModels
				m.messages = nil
				m.err = nil
				return m, m.fetchModelsCmd()
			case tea.KeyEnter:
				val := m.textInput.Value()
				if strings.TrimSpace(val) == "" {
					return m, nil
				}

				userMsg := ollama.Message{Role: "user", Content: val}
				m.messages = append(m.messages, userMsg)
				m.textInput.Reset()
				m.isResponding = true

				return m, m.sendMessageCmd(val)
			}
		}

	case modelsFetchedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = stateSelectModel
		} else {
			m.models = msg.models
			m.state = stateSelectModel
			m.err = nil
			if len(m.models) == 0 {
				m.err = fmt.Errorf("no models found. Please run 'ollama pull <model>' first")
			}
		}
		return m, nil

	case responseMsg:
		m.isResponding = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			assistantMsg := ollama.Message{Role: "assistant", Content: msg.content}
			m.messages = append(m.messages, assistantMsg)
		}
		return m, nil
	}

	if m.state == stateChat {
		m.textInput, cmd = m.textInput.Update(msg)
	}
	return m, cmd
}

func (m Model) View() string {
	var s strings.Builder

	switch m.state {
	case stateLoadingModels:
		s.WriteString(titleStyle.Render(" AskiLlama "))
		s.WriteString("\n\n Connecting to Ollama and fetching models...\n\n")
		if m.cfg.HostURL != "" {
			s.WriteString(systemMsgStyle.Render(fmt.Sprintf(" Host: %s\n", m.cfg.HostURL)))
		}

	case stateSelectModel:
		s.WriteString(titleStyle.Render(" AskiLlama - Select Model "))
		s.WriteString("\n\n")

		if m.err != nil {
			s.WriteString(errorStyle.Render(fmt.Sprintf(" Error: %v", m.err)) + "\n\n")
			s.WriteString(" Ensure Ollama is running and accessible.\n\n")
			s.WriteString(" [r] Retry fetching models\n")
			s.WriteString(" [Ctrl+C] Quit\n")
		} else if len(m.models) == 0 {
			s.WriteString(systemMsgStyle.Render(" No models found on this Ollama host.\n\n"))
			s.WriteString(" Please run 'ollama pull <model>' to download a model first.\n\n")
			s.WriteString(" [r] Refresh\n")
			s.WriteString(" [Ctrl+C] Quit\n")
		} else {
			s.WriteString(" Select a model to chat with:\n\n")
			for i, modelName := range m.models {
				if i == m.cursor {
					s.WriteString(cursorStyle.Render("> ") + selectedItemStyle.Render(modelName) + "\n")
				} else {
					s.WriteString("  " + unselectedItemStyle.Render(modelName) + "\n")
				}
			}
			s.WriteString("\n (use Up/Down or j/k to navigate, Enter to select, Ctrl+C to quit)\n")
		}

	case stateChat:
		s.WriteString(titleStyle.Render(fmt.Sprintf(" AskiLlama - Chatting with %s ", m.cfg.CurrentModel)))
		s.WriteString("\n\n")

		for _, msg := range m.messages {
			if msg.Role == "user" {
				s.WriteString(userLabelStyle.Render(" You: ") + msg.Content + "\n")
			} else {
				s.WriteString(assistantLabelStyle.Render(" Ollama: ") + msg.Content + "\n\n")
			}
		}

		if m.isResponding {
			s.WriteString("\n " + systemMsgStyle.Render("Ollama is typing...") + "\n")
		}

		if m.err != nil {
			s.WriteString("\n " + errorStyle.Render(fmt.Sprintf("Error: %v", m.err)) + "\n")
		}

		s.WriteString("\n" + m.textInput.View() + "\n")
		s.WriteString("\n (Esc to select another model, Ctrl+C to quit)\n")
	}

	return s.String()
}
