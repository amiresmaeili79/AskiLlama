package ui

import (
	"askillama/internal/config"
	"askillama/internal/ollama"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// State machine
// ---------------------------------------------------------------------------

type state int

const (
	stateLoadingModels state = iota // waiting for the model list to arrive
	stateSelectModel                // user is picking a model from the list
	stateChat                       // normal chat interaction
	stateCopy                       // browsing messages to copy one
	stateSettings                   // universal settings app
)

// ---------------------------------------------------------------------------
// Slash-command registry
// ---------------------------------------------------------------------------

// action describes a single slash-command shown in the autocomplete popup.
type action struct {
	key         string
	description string
}

// actions is the authoritative list of supported slash commands.
// Order here determines the order they appear in the popup.
var actions = []action{
	{key: "/model", description: "change model"},
	{key: "/new", description: "new session"},
	{key: "/think", description: "set reasoning capability (false/true/low/medium/high/max)"},
	{key: "/settings", description: "open settings"},
	{key: "/system", description: "set system prompt for current session"},
	{key: "/save", description: "save current session: /save [session_name]"},
	{key: "/load", description: "load a saved session: /load [session_name]"},
	{key: "/export", description: "export chat to markdown: /export [file_name].md"},
}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

// Model is the root Bubble Tea model for AskiLlama.
type Model struct {
	cfg    *config.Config
	client *ollama.Client

	// Conversation state
	messages        []ollama.Message
	messagesMetrics []*ollama.ResponseMetrics // parallel to messages; nil = no metrics for that turn
	isResponding    bool
	err             error
	infoMessage     string

	// UI components
	textInput textinput.Model
	viewport  viewport.Model

	// App state machine
	state  state
	models []string // available Ollama models (stateSelectModel)
	cursor int      // cursor in the model list

	// Layout
	width  int
	height int
	ready  bool // true once the viewport has been initialised with real dimensions

	// Token counters (cumulative across the session)
	inputTokens  int
	outputTokens int

	// Popup autocomplete
	popupCursor int

	// Think / reasoning setting ("false", "true", "low", "medium", "high", "max")
	thinkSetting string

	// Copy mode
	copyCursor int

	// Render cache: renderedMessages[i] stores the glamour-rendered body for
	// messages[i]. Invalidated (set to nil) on window resize or theme change.
	renderedMessages []string

	// Settings state
	settingsCursor   int
	settingsURLInput textinput.Model
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewModel creates a new Model with the given configuration.
func NewModel(cfg *config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "Ask Ollama something..."
	ti.Focus()
	ti.CharLimit = 2000

	client := ollama.NewClient(cfg.HostURL)

	// If a model is already configured, start in chat mode directly.
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

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

// Init returns the initial command(s) for Bubble Tea to run on startup.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	if m.state == stateLoadingModels {
		cmds = append(cmds, m.fetchModelsCmd())
	}
	return tea.Batch(cmds...)
}

// ---------------------------------------------------------------------------
// Query helpers (pure, no state mutation)
// ---------------------------------------------------------------------------

// isPopupActive reports whether the autocomplete popup should be shown.
// The popup is active whenever the user has typed a '/' prefix in chat mode.
func (m Model) isPopupActive() bool {
	return m.state == stateChat && len(m.textInput.Value()) > 0 && m.textInput.Value()[0] == '/'
}

// getMatchingActions returns the subset of registered actions whose key starts
// with the current text-input value.
func (m Model) getMatchingActions() []action {
	val := m.textInput.Value()
	var matches []action
	for _, act := range actions {
		if len(act.key) >= len(val) && act.key[:len(val)] == val {
			matches = append(matches, act)
		}
	}
	return matches
}

// popupHeight returns the number of terminal rows the popup will occupy,
// including its border. Returns 0 when the popup is not visible.
func (m Model) popupHeight() int {
	matches := m.getMatchingActions()
	if len(matches) == 0 {
		return 0
	}
	return len(matches) + 2 // +2 for the rounded-border top and bottom lines
}

// baseHeight returns the fixed number of terminal rows consumed
// (header, borders, input row, help bar) so that the viewport height can be
// calculated as  terminalHeight - baseHeight.
func (m Model) baseHeight() int {
	// Header(1) + blank(1) + viewport borders(2) + input row with borders(3) + help(1) + safety(2)
	h := 10
	if m.isPopupActive() {
		h += m.popupHeight()
	}
	return h
}
