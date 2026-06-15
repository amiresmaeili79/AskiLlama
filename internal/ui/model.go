package ui

import (
	"fmt"
	"strings"

	"askillama/internal/config"
	"askillama/internal/ollama"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
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
	viewport     viewport.Model
	err          error
	isResponding bool

	state  state
	models []string
	cursor int

	width  int
	height int
	ready  bool

	inputTokens  int
	outputTokens int
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true)

	userLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1A1A1A")).
			Background(lipgloss.Color("#00ADD8")).
			Padding(0, 1)

	assistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#1A1A1A")).
				Background(lipgloss.Color("#FF79C6")).
				Padding(0, 1)

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

	viewportContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7D56F4")).
				Padding(0, 1)

	inputContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#00ADD8")).
				Padding(0, 1)
)

func NewModel(cfg *config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "Ask Ollama something..."
	ti.Focus()
	ti.CharLimit = 2000

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
	content      string
	promptTokens int
	evalTokens   int
	err          error
}

func (m Model) sendMessageCmd(prompt string) tea.Cmd {
	return func() tea.Msg {
		resp, promptTokens, evalTokens, err := m.client.Chat(m.cfg.CurrentModel, m.messages)
		return responseMsg{
			content:      resp,
			promptTokens: promptTokens,
			evalTokens:   evalTokens,
			err:          err,
		}
	}
}

func (m Model) renderChatContent() string {
	var s strings.Builder

	if len(m.messages) == 0 {
		return systemMsgStyle.Render(" No messages yet. Start a conversation by typing below!")
	}

	// Inner width accounts for viewport padding/borders
	innerWidth := m.viewport.Width - 2
	if innerWidth < 10 {
		innerWidth = 10
	}

	for i, msg := range m.messages {
		var labelText string
		var labelStyle lipgloss.Style
		var msgColor lipgloss.Color

		if msg.Role == "user" {
			labelText = " You "
			labelStyle = userLabelStyle
			msgColor = lipgloss.Color("#FAFAFA")
		} else {
			labelText = " Ollama "
			labelStyle = assistantLabelStyle
			msgColor = lipgloss.Color("#DDDDDD")
		}

		roleLabel := labelStyle.Render(labelText)
		labelWidth := lipgloss.Width(roleLabel)
		contentWidth := innerWidth - labelWidth - 1
		if contentWidth < 10 {
			contentWidth = 10
		}

		// Replace tabs with spaces to prevent terminal wrapping issues
		content := strings.ReplaceAll(msg.Content, "\t", "    ")

		// Wrap content and color it
		wrappedContent := lipgloss.NewStyle().Width(contentWidth).Foreground(msgColor).Render(content)
		lines := strings.Split(wrappedContent, "\n")

		for idx, line := range lines {
			if idx == 0 {
				s.WriteString(roleLabel)
				s.WriteString("\n")
				s.WriteString(line)
			} else {
				s.WriteString("\n")
				s.WriteString(line)
			}
		}

		if i < len(m.messages)-1 {
			s.WriteString("\n\n")
		}
	}

	if m.isResponding {
		s.WriteString("\n\n")
		s.WriteString(systemMsgStyle.Render(" Ollama is typing..."))
	}

	if m.err != nil {
		s.WriteString("\n\n")
		s.WriteString(errorStyle.Render(fmt.Sprintf(" Error: %v", m.err)))
	}

	return s.String()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Only setup components if we meet minimum terminal size
		if m.width >= 80 && m.height >= 18 {
			// Title bar (1) + Spacing (1) + Viewport borders (2) + Input borders (2) + Input line (1) + Spacing (1) + 1 line safety buffer
			vHeight := max(m.height-9, 3)
			vWidth := m.width - 4

			if !m.ready {
				m.viewport = viewport.New(vWidth, vHeight)
				m.viewport.YPosition = 0
				m.viewport.HighPerformanceRendering = false
				m.viewport.SetContent(m.renderChatContent())
				m.ready = true
			} else {
				m.viewport.Width = vWidth
				m.viewport.Height = vHeight
				m.viewport.SetContent(m.renderChatContent())
			}

			// Adjust textinput width based on the left side allocation
			modelBoxWidth := 24
			inputWidth := vWidth + 2 - modelBoxWidth
			m.textInput.Width = inputWidth - 6
		}

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

					// Initialize the viewport content and size if we just loaded
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

		case stateChat:
			// Viewport scrolling controls
			switch msg.String() {
			case "pgup":
				m.viewport.HalfViewUp()
				return m, nil
			case "pgdown":
				m.viewport.HalfViewDown()
				return m, nil
			case "ctrl+up":
				m.viewport.LineUp(1)
				return m, nil
			case "ctrl+down":
				m.viewport.LineDown(1)
				return m, nil
			}

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
				m.err = nil

				if m.ready {
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
				}

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
			m.inputTokens += msg.promptTokens
			m.outputTokens += msg.evalTokens
		}

		if m.ready {
			m.viewport.SetContent(m.renderChatContent())
			m.viewport.GotoBottom()
		}
		return m, nil

	case tea.MouseMsg:
		if m.state == stateChat && m.ready {
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}

	if m.state == stateChat {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	// Size Warning
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
		var loadingBox strings.Builder
		loadingBox.WriteString(titleStyle.Render(" AskiLlama "))
		loadingBox.WriteString("\n\n")
		loadingBox.WriteString(" Connecting to Ollama and fetching models...\n\n")
		if m.cfg.HostURL != "" {
			loadingBox.WriteString(systemMsgStyle.Render(fmt.Sprintf(" Host: %s", m.cfg.HostURL)))
		}

		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#888888")).
				Padding(2, 4).
				Render(loadingBox.String()),
		)

	case stateSelectModel:
		var modelSelectBox strings.Builder
		modelSelectBox.WriteString(titleStyle.Render(" AskiLlama - Select Model "))
		modelSelectBox.WriteString("\n\n")

		if m.err != nil {
			modelSelectBox.WriteString(errorStyle.Render(fmt.Sprintf(" Error: %v", m.err)))
			modelSelectBox.WriteString("\n\n")
			modelSelectBox.WriteString(" Ensure Ollama is running and accessible.\n\n")
			modelSelectBox.WriteString(" [r] Retry fetching models\n")
			modelSelectBox.WriteString(" [Ctrl+C] Quit\n")
		} else if len(m.models) == 0 {
			modelSelectBox.WriteString(systemMsgStyle.Render(" No models found on this Ollama host.\n\n"))
			modelSelectBox.WriteString(" Please run 'ollama pull <model>' to download a model first.\n\n")
			modelSelectBox.WriteString(" [r] Refresh\n")
			modelSelectBox.WriteString(" [Ctrl+C] Quit\n")
		} else {
			modelSelectBox.WriteString(" Select a model to chat with:\n\n")
			for i, modelName := range m.models {
				if i == m.cursor {
					modelSelectBox.WriteString(cursorStyle.Render("> "))
					modelSelectBox.WriteString(selectedItemStyle.Render(modelName))
					modelSelectBox.WriteString("\n")
				} else {
					modelSelectBox.WriteString("  ")
					modelSelectBox.WriteString(unselectedItemStyle.Render(modelName))
					modelSelectBox.WriteString("\n")
				}
			}
			modelSelectBox.WriteString("\n (use Up/Down or j/k to navigate, Enter to select, Ctrl+C to quit)\n")
		}

		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(lipgloss.Color("#7D56F4")).
				Padding(2, 4).
				Render(modelSelectBox.String()),
		)

	case stateChat:
		if !m.ready {
			return "Initializing layout..."
		}

		var views []string

		// 1. Header Left & Right
		headerLeft := titleStyle.Render(" AskiLlama ")
		headerRight := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Render(fmt.Sprintf("📥 %d | 📤 %d", m.inputTokens, m.outputTokens))

		header := renderHeader(m.width, headerLeft, headerRight)

		// 2. Viewport container (with border)
		viewportBox := viewportContainerStyle.
			Width(m.viewport.Width).
			Height(m.viewport.Height).
			Render(m.viewport.View())

		// 3. Side-by-side Input container & Model box
		modelBoxWidth := 24
		inputWidth := m.viewport.Width + 2 - modelBoxWidth

		inputBox := inputContainerStyle.
			Width(inputWidth - 2).
			Render(m.textInput.View())

		modelNameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF79C6"))
		modelBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			Align(lipgloss.Center).
			Width(modelBoxWidth - 2).
			Render(modelNameStyle.Render(m.cfg.CurrentModel))

		bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, inputBox, modelBox)

		views = append(views, header, "", viewportBox, bottomRow)

		return lipgloss.JoinVertical(lipgloss.Left, views...)
	}

	return ""
}

func renderHeader(width int, left string, right string) string {
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	spaces := width - leftWidth - rightWidth
	if spaces < 1 {
		spaces = 1
	}
	return left + strings.Repeat(" ", spaces) + right
}
