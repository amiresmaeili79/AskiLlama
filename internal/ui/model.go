package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"askillama/internal/config"
	"askillama/internal/ollama"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateLoadingModels state = iota
	stateSelectModel
	stateChat
	stateCopy
)

type action struct {
	key         string
	description string
}

var actions = []action{
	{key: "/model", description: "change model"},
	{key: "/new", description: "new session"},
	{key: "/think", description: "set reasoning capability (false/true/low/medium/high/max)"},
	{key: "/stream", description: "toggle stream mode (true/false)"},
	{key: "/system", description: "set system prompt for current session"},
	{key: "/save", description: "save current session: /save [session_name]"},
	{key: "/load", description: "load a saved session: /load [session_name]"},
	{key: "/export", description: "export chat to markdown: /export [file_name].md"},
}

type Model struct {
	cfg             *config.Config
	client          *ollama.Client
	messages        []ollama.Message
	messagesMetrics []*ollama.ResponseMetrics
	textInput       textinput.Model
	viewport        viewport.Model
	err             error
	isResponding    bool

	state  state
	models []string
	cursor int

	width  int
	height int
	ready  bool

	inputTokens  int
	outputTokens int

	popupCursor  int
	thinkSetting string

	copyCursor       int
	infoMessage      string
	renderedMessages []string
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

	systemLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#1A1A1A")).
				Background(lipgloss.Color("#F1FA8C")).
				Padding(0, 1)

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

	popupContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#FF79C6")).
				Padding(0, 1)

	metricsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6272A4")).
			Italic(true)
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
		cfg:          cfg,
		client:       client,
		textInput:    ti,
		state:        initialState,
		thinkSetting: "false",
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
	metrics ollama.ResponseMetrics
	err     error
}

func (m Model) sendMessageCmd(prompt string) tea.Cmd {
	return func() tea.Msg {
		resp, metrics, err := m.client.Chat(m.cfg.CurrentModel, m.messages, m.thinkSetting)
		return responseMsg{
			content: resp,
			metrics: metrics,
			err:     err,
		}
	}
}

type StreamMsg struct {
	Content string
	Metrics ollama.ResponseMetrics
	Done    bool
	Err     error
	Channel chan StreamMsg
}

func listenToStream(ch chan StreamMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return StreamMsg{Done: true}
		}
		msg.Channel = ch
		return msg
	}
}

func (m Model) sendStreamMessageCmd(ch chan StreamMsg) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := m.client.StreamChat(ctx, m.cfg.CurrentModel, m.messages[:len(m.messages)-1], m.thinkSetting, func(content string, done bool, metrics ollama.ResponseMetrics) error {
			ch <- StreamMsg{
				Content: content,
				Done:    done,
				Metrics: metrics,
			}
			return nil
		})
		if err != nil {
			ch <- StreamMsg{Err: err}
		}
		close(ch)
		return nil
	}
}

func (m *Model) renderChatContent() string {
	var s strings.Builder

	if len(m.messages) == 0 {
		return systemMsgStyle.Render(" No messages yet. Start a conversation by typing below!")
	}

	// Inner width accounts for viewport padding/borders
	innerWidth := m.viewport.Width - 2
	if innerWidth < 10 {
		innerWidth = 10
	}

	// Ensure cache is initialized and matches the size of messages
	if len(m.renderedMessages) != len(m.messages) {
		m.renderedMessages = make([]string, len(m.messages))
	}

	for i, msg := range m.messages {
		var labelText string
		var labelStyle lipgloss.Style
		var msgColor lipgloss.Color

		if msg.Role == "user" {
			labelText = " You "
			labelStyle = userLabelStyle
			msgColor = lipgloss.Color("#FAFAFA")
		} else if msg.Role == "system" {
			labelText = " System Prompt "
			labelStyle = systemLabelStyle
			msgColor = lipgloss.Color("#F1FA8C")
		} else {
			labelText = " Ollama "
			labelStyle = assistantLabelStyle
			msgColor = lipgloss.Color("#DDDDDD")
		}

		roleLabel := labelStyle.Render(labelText)
		if msg.Role == "assistant" && i < len(m.messagesMetrics) && m.messagesMetrics[i] != nil {
			metrics := m.messagesMetrics[i]
			metricsStr := fmt.Sprintf(" [ %s | TTFT: %s | Total: %s ]",
				formatTPS(metrics.TokensPerSecond),
				formatDuration(metrics.TimeToFirstToken),
				formatDuration(metrics.TotalDuration),
			)
			roleLabel += " " + metricsStyle.Render(metricsStr)
		}

		if m.state == stateCopy {
			if i == m.copyCursor {
				roleLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Bold(true).Render("-> ") + roleLabel
			} else {
				roleLabel = "   " + roleLabel
			}
		}

		labelWidth := lipgloss.Width(roleLabel)
		contentWidth := innerWidth - labelWidth - 1
		if contentWidth < 10 {
			contentWidth = 10
		}

		// Replace tabs with spaces to prevent terminal wrapping issues
		content := strings.ReplaceAll(msg.Content, "\t", "    ")
		if msg.Role == "assistant" && m.thinkSetting == "false" {
			content = stripReasoning(content)
		}

		// Wrap content and color it
		var wrappedContent string
		if msg.Role == "assistant" {
			// If it is the last message and we are currently responding/streaming,
			// render dynamically without using/saving cache.
			isStreaming := m.isResponding && i == len(m.messages)-1
			if !isStreaming && m.renderedMessages[i] != "" {
				wrappedContent = m.renderedMessages[i]
			} else {
				renderer, err := glamour.NewTermRenderer(
					glamour.WithStandardStyle("dark"),
					glamour.WithWordWrap(contentWidth),
				)
				if err == nil {
					if rendered, err := renderer.Render(content); err == nil {
						wrappedContent = strings.TrimRight(rendered, "\n")
						if !isStreaming {
							m.renderedMessages[i] = wrappedContent
						}
					}
				}
			}
		}

		if wrappedContent == "" {
			wrappedContent = lipgloss.NewStyle().Width(contentWidth).Foreground(msgColor).Render(content)
		}

		if m.state == stateCopy {
			lines := strings.Split(wrappedContent, "\n")
			for idx, line := range lines {
				lines[idx] = "   " + line
			}
			wrappedContent = strings.Join(lines, "\n")
		}
		msgBlockStr := roleLabel + "\n" + wrappedContent
		s.WriteString(msgBlockStr)

		if i < len(m.messages)-1 {
			s.WriteString("\n\n")
		}
	}

	if m.isResponding {
		hasAssistantContent := false
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" && m.messages[len(m.messages)-1].Content != "" {
			hasAssistantContent = true
		}
		if !hasAssistantContent {
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.renderedMessages = nil

		// Only setup components if we meet minimum terminal size
		if m.width >= 80 && m.height >= 18 {
			vHeight := max(m.height-m.baseHeight(), 3)
			vWidth := m.width - 8

			if !m.ready {
				m.viewport = viewport.New(vWidth, vHeight)
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

			// Adjust textinput width based on the left side allocation
			modelBoxWidth := 24
			inputWidth := vWidth + 8 - modelBoxWidth
			m.textInput.Width = inputWidth - 6
		}

	case tea.KeyMsg:
		if m.state == stateChat {
			m.infoMessage = ""
			m.err = nil
		}
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

			// If popup is active, handle popup keys
			if m.isPopupActive() {
				matches := m.getMatchingActions()
				switch msg.String() {
				case "up", "ctrl+k":
					if len(matches) > 0 {
						m.popupCursor = (m.popupCursor - 1 + len(matches)) % len(matches)
					}
					return m, nil
				case "down", "ctrl+j", "tab":
					if len(matches) > 0 {
						m.popupCursor = (m.popupCursor + 1) % len(matches)
					}
					return m, nil
				case "enter":
					if len(matches) > 0 {
						selected := matches[m.popupCursor]
						switch selected.key {
						case "/model":
							m.cfg.CurrentModel = ""
							_ = m.cfg.Save()
							m.state = stateLoadingModels
							m.err = nil
							m.textInput.Reset()
							m.popupCursor = 0
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
							}
							return m, m.fetchModelsCmd()
						case "/new":
							m.messages = nil
							m.messagesMetrics = nil
							m.renderedMessages = nil
							m.inputTokens = 0
							m.outputTokens = 0
							m.err = nil
							m.textInput.Reset()
							m.popupCursor = 0
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
								m.viewport.SetContent(m.renderChatContent())
								m.viewport.GotoBottom()
							}
							return m, nil
						case "/think":
							m.textInput.SetValue("/think ")
							m.textInput.SetCursor(len("/think "))
							m.popupCursor = 0
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
							}
							return m, nil
						case "/stream":
							m.textInput.SetValue("/stream ")
							m.textInput.SetCursor(len("/stream "))
							m.popupCursor = 0
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
							}
							return m, nil
						case "/system":
							m.textInput.SetValue("/system ")
							m.textInput.SetCursor(len("/system "))
							m.popupCursor = 0
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
							}
							return m, nil
						case "/save":
							m.textInput.SetValue("/save ")
							m.textInput.SetCursor(len("/save "))
							m.popupCursor = 0
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
							}
							return m, nil
						case "/load":
							m.textInput.SetValue("/load ")
							m.textInput.SetCursor(len("/load "))
							m.popupCursor = 0
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
							}
							return m, nil
						case "/export":
							m.textInput.SetValue("/export ")
							m.textInput.SetCursor(len("/export "))
							m.popupCursor = 0
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
							}
							return m, nil
						}
					}
				case "esc":
					m.textInput.Reset()
					m.popupCursor = 0
					if m.ready {
						m.viewport.Height = max(m.height-m.baseHeight(), 3)
					}
					return m, nil
				}
			}

			// Viewport scrolling controls
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

			switch msg.Type {
			case tea.KeyEnter:
				val := m.textInput.Value()
				if strings.TrimSpace(val) == "" {
					return m, nil
				}

				switch val {
				case "/model":
					m.cfg.CurrentModel = ""
					_ = m.cfg.Save()
					m.state = stateLoadingModels
					m.err = nil
					m.textInput.Reset()
					m.popupCursor = 0
					if m.ready {
						m.viewport.Height = max(m.height-m.baseHeight(), 3)
					}
					return m, m.fetchModelsCmd()
				case "/new":
					m.messages = nil
					m.messagesMetrics = nil
					m.renderedMessages = nil
					m.inputTokens = 0
					m.outputTokens = 0
					m.err = nil
					m.textInput.Reset()
					m.popupCursor = 0
					if m.ready {
						m.viewport.Height = max(m.height-m.baseHeight(), 3)
						m.viewport.SetContent(m.renderChatContent())
						m.viewport.GotoBottom()
					}
					return m, nil
				case "/think":
					m.textInput.SetValue("/think ")
					m.textInput.SetCursor(len("/think "))
					return m, nil
				case "/stream":
					m.cfg.Stream = !m.cfg.Stream
					_ = m.cfg.Save()
					m.textInput.Reset()
					m.popupCursor = 0
					m.err = nil
					if m.ready {
						m.viewport.Height = max(m.height-m.baseHeight(), 3)
						m.viewport.SetContent(m.renderChatContent())
						m.viewport.GotoBottom()
					}
					return m, nil
				case "/system":
					m.textInput.SetValue("/system ")
					m.textInput.SetCursor(len("/system "))
					return m, nil
				case "/save":
					m.textInput.SetValue("/save ")
					m.textInput.SetCursor(len("/save "))
					return m, nil
				case "/load":
					m.textInput.SetValue("/load ")
					m.textInput.SetCursor(len("/load "))
					return m, nil
				case "/export":
					m.textInput.SetValue("/export ")
					m.textInput.SetCursor(len("/export "))
					return m, nil
				default:
					if strings.HasPrefix(val, "/think ") {
						setting := strings.TrimPrefix(val, "/think ")
						switch setting {
						case "false", "true", "low", "medium", "high", "max":
							m.thinkSetting = setting
							m.renderedMessages = nil
							m.textInput.Reset()
							m.popupCursor = 0
							m.err = nil
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
								m.viewport.SetContent(m.renderChatContent())
								m.viewport.GotoBottom()
							}
							return m, nil
						default:
							m.err = fmt.Errorf("invalid think setting. Choose from: false, true, low, medium, high, max")
							m.textInput.Reset()
							return m, nil
						}
					}
					if strings.HasPrefix(val, "/stream ") {
						setting := strings.TrimPrefix(val, "/stream ")
						switch setting {
						case "true", "yes", "on":
							m.cfg.Stream = true
							_ = m.cfg.Save()
							m.textInput.Reset()
							m.popupCursor = 0
							m.err = nil
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
								m.viewport.SetContent(m.renderChatContent())
								m.viewport.GotoBottom()
							}
							return m, nil
						case "false", "no", "off":
							m.cfg.Stream = false
							_ = m.cfg.Save()
							m.textInput.Reset()
							m.popupCursor = 0
							m.err = nil
							if m.ready {
								m.viewport.Height = max(m.height-m.baseHeight(), 3)
								m.viewport.SetContent(m.renderChatContent())
								m.viewport.GotoBottom()
							}
							return m, nil
						default:
							m.err = fmt.Errorf("invalid stream setting. Choose from: true, false")
							m.textInput.Reset()
							return m, nil
						}
					}
					if strings.HasPrefix(val, "/system ") {
						prompt := strings.TrimSpace(strings.TrimPrefix(val, "/system "))
						if prompt != "" {
							if len(m.messages) > 0 && m.messages[0].Role == "system" {
								m.messages[0].Content = prompt
							} else {
								m.messages = append([]ollama.Message{{Role: "system", Content: prompt}}, m.messages...)
								m.messagesMetrics = append([]*ollama.ResponseMetrics{nil}, m.messagesMetrics...)
							}
						} else {
							if len(m.messages) > 0 && m.messages[0].Role == "system" {
								m.messages = m.messages[1:]
								m.messagesMetrics = m.messagesMetrics[1:]
							}
						}
						m.renderedMessages = nil
						m.textInput.Reset()
						m.popupCursor = 0
						m.err = nil
						if m.ready {
							m.viewport.Height = max(m.height-m.baseHeight(), 3)
							m.viewport.SetContent(m.renderChatContent())
							m.viewport.GotoBottom()
						}
						return m, nil
					}
					if strings.HasPrefix(val, "/save ") {
						name := strings.TrimSpace(strings.TrimPrefix(val, "/save "))
						if err := m.saveSession(name); err != nil {
							m.err = err
						} else {
							m.infoMessage = fmt.Sprintf("Session '%s' saved successfully!", name)
						}
						m.textInput.Reset()
						m.popupCursor = 0
						if m.ready {
							m.viewport.Height = max(m.height-m.baseHeight(), 3)
							m.viewport.SetContent(m.renderChatContent())
							m.viewport.GotoBottom()
						}
						return m, nil
					}
					if strings.HasPrefix(val, "/load ") {
						name := strings.TrimSpace(strings.TrimPrefix(val, "/load "))
						if err := m.loadSession(name); err != nil {
							m.err = err
						} else {
							m.infoMessage = fmt.Sprintf("Session '%s' loaded successfully!", name)
						}
						m.renderedMessages = nil
						m.textInput.Reset()
						m.popupCursor = 0
						if m.ready {
							m.viewport.Height = max(m.height-m.baseHeight(), 3)
							m.viewport.SetContent(m.renderChatContent())
							m.viewport.GotoBottom()
						}
						return m, nil
					}
					if strings.HasPrefix(val, "/export ") {
						filename := strings.TrimSpace(strings.TrimPrefix(val, "/export "))
						if err := m.exportChat(filename); err != nil {
							m.err = err
						} else {
							if filename == "" {
								filename = "askillama-chat.md"
							} else if !strings.HasSuffix(strings.ToLower(filename), ".md") {
								filename += ".md"
							}
							m.infoMessage = fmt.Sprintf("Chat exported to '%s' successfully!", filename)
						}
						m.textInput.Reset()
						m.popupCursor = 0
						if m.ready {
							m.viewport.Height = max(m.height-m.baseHeight(), 3)
							m.viewport.SetContent(m.renderChatContent())
							m.viewport.GotoBottom()
						}
						return m, nil
					}
				}

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
					m.messages = append(m.messages, ollama.Message{Role: "assistant", Content: ""})
					m.messagesMetrics = append(m.messagesMetrics, nil)
					ch := make(chan StreamMsg)
					return m, tea.Batch(
						m.sendStreamMessageCmd(ch),
						listenToStream(ch),
					)
				} else {
					return m, m.sendMessageCmd(val)
				}
			}

		case stateCopy:
			switch msg.String() {
			case "up", "k":
				if m.copyCursor > 0 {
					m.copyCursor--
					m.scrollToSelectedMessage()
					if m.ready {
						m.viewport.SetContent(m.renderChatContent())
					}
				}
				return m, nil
			case "down", "j":
				if m.copyCursor < len(m.messages)-1 {
					m.copyCursor++
					m.scrollToSelectedMessage()
					if m.ready {
						m.viewport.SetContent(m.renderChatContent())
					}
				}
				return m, nil
			case "enter":
				if len(m.messages) > 0 && m.copyCursor >= 0 && m.copyCursor < len(m.messages) {
					selectedMsg := m.messages[m.copyCursor].Content
					err := clipboard.WriteAll(selectedMsg)
					if err != nil {
						m.err = fmt.Errorf("failed to copy: %v", err)
					} else {
						m.infoMessage = "Copied message to clipboard!"
					}
					m.state = stateChat
					m.renderedMessages = nil
					m.textInput.Focus()
					if m.ready {
						m.viewport.SetContent(m.renderChatContent())
						m.viewport.GotoBottom()
					}
				}
				return m, nil
			case "esc", "ctrl+y":
				m.state = stateChat
				m.renderedMessages = nil
				m.textInput.Focus()
				if m.ready {
					m.viewport.SetContent(m.renderChatContent())
					m.viewport.GotoBottom()
				}
				return m, nil
			}
			return m, nil
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
			m.messagesMetrics = append(m.messagesMetrics, &msg.metrics)
			m.inputTokens += msg.metrics.PromptTokens
			m.outputTokens += msg.metrics.EvalTokens
		}

		if m.ready {
			m.viewport.SetContent(m.renderChatContent())
			m.viewport.GotoBottom()
		}
		return m, nil

	case StreamMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.isResponding = false
			// Remove the empty assistant message if it has no content
			if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" && m.messages[len(m.messages)-1].Content == "" {
				m.messages = m.messages[:len(m.messages)-1]
				m.messagesMetrics = m.messagesMetrics[:len(m.messagesMetrics)-1]
			}
			if m.ready {
				m.viewport.SetContent(m.renderChatContent())
				m.viewport.GotoBottom()
			}
			return m, nil
		}

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
			if m.ready {
				m.viewport.SetContent(m.renderChatContent())
				m.viewport.GotoBottom()
			}
			return m, nil
		}

		if m.ready {
			m.viewport.SetContent(m.renderChatContent())
			m.viewport.GotoBottom()
		}

		return m, listenToStream(msg.Channel)

	case tea.MouseMsg:
		if m.state == stateChat && m.ready {
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}

	if m.state == stateChat {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)

		// Clamp popupCursor to ensure it doesn't get out of bounds
		if m.isPopupActive() {
			matches := m.getMatchingActions()
			if len(matches) > 0 && m.popupCursor >= len(matches) {
				m.popupCursor = len(matches) - 1
			}
			if m.popupCursor < 0 {
				m.popupCursor = 0
			}
		}

		// Update viewport height dynamically if needed
		if m.ready {
			m.viewport.Height = max(m.height-m.baseHeight(), 3)
		}
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

	case stateChat, stateCopy:
		if !m.ready {
			return "Initializing layout..."
		}

		var views []string

		// 1. Header Left & Right
		headerLeft := titleStyle.Render(" AskiLlama ")
		modeStr := "⚡ stream"
		if !m.cfg.Stream {
			modeStr = "📦 batch"
		}
		headerRight := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Render(fmt.Sprintf("%s | 📥 %d | 📤 %d", modeStr, m.inputTokens, m.outputTokens))

		header := renderHeader(m.width, headerLeft, headerRight)

		// 2. Viewport container (with border)
		viewportBox := viewportContainerStyle.
			Width(m.viewport.Width + 4).
			Height(m.viewport.Height + 2).
			Render(m.viewport.View())

		// 3. Side-by-side Input container & Model box
		modelBoxWidth := 24
		inputWidth := m.viewport.Width + 8 - modelBoxWidth

		inputBorderColor := "#00ADD8"
		if m.state == stateCopy {
			inputBorderColor = "#50FA7B"
		}
		inputBox := inputContainerStyle.
			BorderForeground(lipgloss.Color(inputBorderColor)).
			Width(inputWidth - 2).
			Render(m.textInput.View())

		modelNameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF79C6"))
		modelDisplayName := m.cfg.CurrentModel
		if m.thinkSetting != "false" {
			if m.thinkSetting == "true" {
				modelDisplayName = "💡 " + modelDisplayName
			} else {
				modelDisplayName = fmt.Sprintf("💡 (%s) %s", m.thinkSetting, modelDisplayName)
			}
		}
		modelBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			Align(lipgloss.Center).
			Width(modelBoxWidth - 2).
			Render(modelNameStyle.Render(modelDisplayName))

		bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, inputBox, modelBox)

		views = append(views, header, "", viewportBox)

		if m.isPopupActive() {
			popupBox := m.renderPopup()
			if popupBox != "" {
				views = append(views, popupBox)
			}
		}

		views = append(views, bottomRow)

		helpTextStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true).
			PaddingLeft(2).
			Width(m.width - 4)
		var helpText string
		if m.state == stateCopy {
			helpText = helpTextStyle.Render("Copy Mode: Up/Down or j/k to select • Enter to copy • Esc / Ctrl+Y to exit")
		} else {
			helpText = helpTextStyle.Render("(write / for actions) • Ctrl+Y: copy mode • PgUp/PgDn: scroll page • Ctrl+Up/Down: scroll line")
		}
		views = append(views, helpText)

		return lipgloss.JoinVertical(lipgloss.Left, views...)
	}

	return ""
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func formatTPS(tps float64) string {
	if tps <= 0 {
		return "0.0 t/s"
	}
	return fmt.Sprintf("%.1f t/s", tps)
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

func (m Model) isPopupActive() bool {
	return m.state == stateChat && strings.HasPrefix(m.textInput.Value(), "/")
}

func (m Model) getMatchingActions() []action {
	val := m.textInput.Value()
	var matches []action
	for _, act := range actions {
		if strings.HasPrefix(act.key, val) {
			matches = append(matches, act)
		}
	}
	return matches
}

func (m Model) popupHeight() int {
	matches := m.getMatchingActions()
	if len(matches) == 0 {
		return 0
	}
	return len(matches) + 2
}

func (m Model) baseHeight() int {
	h := 10 // Header(1) + Spacing(1) + Viewport borders(2) + Input borders(2) + Input line(1) + Help text(1) + Spacing/Safety(2)
	if m.isPopupActive() {
		h += m.popupHeight()
	}
	return h
}

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

func stripReasoning(content string) string {
	for {
		start := strings.Index(content, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], "</think>")
		if end == -1 {
			content = content[:start]
			break
		}
		content = content[:start] + content[start+end+8:]
	}
	return strings.TrimSpace(content)
}

func (m *Model) scrollToSelectedMessage() {
	if len(m.messages) == 0 || m.copyCursor < 0 || m.copyCursor >= len(m.messages) {
		return
	}

	innerWidth := m.viewport.Width
	if innerWidth < 10 {
		innerWidth = 10
	}

	lineCountBefore := 0
	selectedMessageLines := 0

	for i, msg := range m.messages {
		var labelText string
		var labelStyle lipgloss.Style

		if msg.Role == "user" {
			labelText = " You "
			labelStyle = userLabelStyle
		} else if msg.Role == "system" {
			labelText = " System Prompt "
			labelStyle = systemLabelStyle
		} else {
			labelText = " Ollama "
			labelStyle = assistantLabelStyle
		}

		roleLabel := labelStyle.Render(labelText)
		if msg.Role == "assistant" && i < len(m.messagesMetrics) && m.messagesMetrics[i] != nil {
			metrics := m.messagesMetrics[i]
			metricsStr := fmt.Sprintf(" [ %s | TTFT: %s | Total: %s ]",
				formatTPS(metrics.TokensPerSecond),
				formatDuration(metrics.TimeToFirstToken),
				formatDuration(metrics.TotalDuration),
			)
			roleLabel += " " + metricsStyle.Render(metricsStr)
		}

		if m.state == stateCopy {
			if i == m.copyCursor {
				roleLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Bold(true).Render("-> ") + roleLabel
			} else {
				roleLabel = "   " + roleLabel
			}
		}

		labelWidth := lipgloss.Width(roleLabel)
		contentWidth := innerWidth - labelWidth - 1
		if contentWidth < 10 {
			contentWidth = 10
		}

		content := strings.ReplaceAll(msg.Content, "\t", "    ")
		if msg.Role == "assistant" && m.thinkSetting == "false" {
			content = stripReasoning(content)
		}

		var wrappedContent string
		if msg.Role == "assistant" {
			if len(m.renderedMessages) != len(m.messages) {
				m.renderedMessages = make([]string, len(m.messages))
			}
			isStreaming := m.isResponding && i == len(m.messages)-1
			if !isStreaming && m.renderedMessages[i] != "" {
				wrappedContent = m.renderedMessages[i]
			} else {
				renderer, err := glamour.NewTermRenderer(
					glamour.WithStandardStyle("dark"),
					glamour.WithWordWrap(contentWidth),
				)
				if err == nil {
					if rendered, err := renderer.Render(content); err == nil {
						wrappedContent = strings.TrimRight(rendered, "\n")
						if !isStreaming {
							m.renderedMessages[i] = wrappedContent
						}
					}
				}
			}
		}

		if wrappedContent == "" {
			wrappedContent = lipgloss.NewStyle().Width(contentWidth).Render(content)
		}

		msgLines := 1 + strings.Count(wrappedContent, "\n")

		if i < m.copyCursor {
			lineCountBefore += msgLines + 2 // 2 is for \n\n separating messages
		} else if i == m.copyCursor {
			selectedMessageLines = msgLines
			break
		}
	}

	viewportTop := m.viewport.YOffset
	viewportBottom := m.viewport.YOffset + m.viewport.Height

	if lineCountBefore < viewportTop {
		m.viewport.YOffset = lineCountBefore
	} else if lineCountBefore+selectedMessageLines > viewportBottom {
		m.viewport.YOffset = lineCountBefore + selectedMessageLines - m.viewport.Height
	}

	if m.viewport.YOffset < 0 {
		m.viewport.YOffset = 0
	}
}
