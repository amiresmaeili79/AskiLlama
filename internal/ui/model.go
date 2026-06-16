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

// renderMessage renders a single chat message (label + content) into a string.
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

	// Append performance metrics next to the assistant label
	if msg.Role == "assistant" && i < len(m.messagesMetrics) && m.messagesMetrics[i] != nil {
		met := m.messagesMetrics[i]
		metricsStr := fmt.Sprintf(" [ %s | TTFT: %s | Total: %s ]",
			formatTPS(met.TokensPerSecond),
			formatDuration(met.TimeToFirstToken),
			formatDuration(met.TotalDuration),
		)
		roleLabel += " " + metricsStyle.Render(metricsStr)
	}

	// Prefix the label with a copy-mode cursor indicator
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

	// Normalize tabs to spaces to prevent terminal wrapping issues
	content := strings.ReplaceAll(msg.Content, "\t", "    ")
	if msg.Role == "assistant" && m.thinkSetting == "false" {
		content = stripReasoning(content)
	}

	var body string
	if msg.Role == "assistant" {
		// Ensure the render cache is initialized before indexing into it
		if len(m.renderedMessages) != len(m.messages) {
			m.renderedMessages = make([]string, len(m.messages))
		}
		// Use cached render when available; stream live for the last in-progress message
		isStreaming := m.isResponding && i == len(m.messages)-1
		if !isStreaming && m.renderedMessages[i] != "" {
			body = m.renderedMessages[i]
		} else {
			renderer, err := glamour.NewTermRenderer(
				glamour.WithStandardStyle("dark"),
				glamour.WithWordWrap(contentWidth),
			)
			if err == nil {
				if rendered, err := renderer.Render(content); err == nil {
					body = strings.TrimRight(rendered, "\n")
					if !isStreaming {
						m.renderedMessages[i] = body
					}
				}
			}
		}
	}

	// Fallback: plain styled text for user/system or when glamour fails
	if body == "" {
		body = lipgloss.NewStyle().Width(contentWidth).Foreground(msgColor).Render(content)
	}

	// Indent user messages and add a blank line after the label
	if msg.Role == "user" {
		body = indentLines(body, "  ")
	}

	// In copy-mode, indent all content to align with the cursor arrow
	if m.state == stateCopy {
		body = indentLines(body, "   ")
	}

	// User messages get a blank line between label and content
	labelSep := "\n"
	if msg.Role == "user" {
		labelSep = "\n\n"
	}

	return roleLabel + labelSep + body
}

// indentLines prepends prefix to every line in s.
func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderChatContent() string {
	if len(m.messages) == 0 {
		return systemMsgStyle.Render(" No messages yet. Start a conversation by typing below!")
	}

	// Inner width accounts for viewport padding/borders
	innerWidth := max(m.viewport.Width-2, 10)

	// Ensure the render cache matches the current message count
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

	// Show a typing indicator when the assistant hasn't produced output yet
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
			thinkBoxWidth := 10
			inputWidth := vWidth + 8 - thinkBoxWidth
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

		// 1. Header: title on left, model name centered, stats on right
		headerLeft := titleStyle.Render(" AskiLlama ")
		modeStr := "⚡ stream"
		if !m.cfg.Stream {
			modeStr = "📦 batch"
		}
		headerRight := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Render(fmt.Sprintf("%s | 📥 %d | 📤 %d", modeStr, m.inputTokens, m.outputTokens))
		headerCenter := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF79C6")).
			Render(m.cfg.CurrentModel)

		header := renderHeader(m.width, headerLeft, headerCenter, headerRight)

		// 2. Viewport container (with border)
		viewportBox := viewportContainerStyle.
			Width(m.viewport.Width + 6).
			Height(m.viewport.Height + 2).
			Render(m.viewport.View())

		// 3. Side-by-side Input container & compact Think-mode box
		thinkBoxWidth := 10
		inputWidth := m.viewport.Width + 8 - thinkBoxWidth

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
			PaddingLeft(2)
		// maxHelpWidth: 2 chars left-padding already added by style, so clip to (m.width - 2)
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

func renderHeader(width int, left string, center string, right string) string {
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

// thinkModeCompact returns a compact emoji+block indicator for the current think setting.
// The block-fill character encodes intensity: ▁=low ▄=medium ▆=high █=max.
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
	default: // "false"
		return "—"
	}
}

// truncateHelp clips text to maxWidth display columns, appending '…' if truncated.
// This guarantees the help bar is always exactly one line regardless of terminal width.
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

	innerWidth := max(m.viewport.Width, 10)

	// Count the lines rendered before the selected message
	lineCountBefore := 0
	for i := range m.messages {
		rendered := m.renderMessage(i, innerWidth)
		msgLines := strings.Count(rendered, "\n") + 1
		if i < m.copyCursor {
			lineCountBefore += msgLines + 2 // +2 for the \n\n message separator
		} else {
			break
		}
	}

	viewportTop := m.viewport.YOffset
	viewportBottom := m.viewport.YOffset + m.viewport.Height

	// Scroll up if the message start is above the viewport
	if lineCountBefore < viewportTop {
		m.viewport.YOffset = lineCountBefore
		// Scroll down only enough to reveal the top of the message (where the arrow is)
	} else if lineCountBefore >= viewportBottom {
		m.viewport.YOffset = lineCountBefore
	}

	if m.viewport.YOffset < 0 {
		m.viewport.YOffset = 0
	}
}
