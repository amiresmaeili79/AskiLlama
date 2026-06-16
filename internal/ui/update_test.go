package ui

import (
	"errors"
	"strings"
	"testing"

	"askillama/internal/config"
	"askillama/internal/ollama"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestModel builds a minimal Model suitable for unit tests.
// The viewport is not initialised (ready=false) so rendering is skipped,
// which avoids needing a real terminal.
func newTestModel() Model {
	cfg := &config.Config{
		CurrentModel: "llama3",
		Stream:       false,
	}
	m := NewModel(cfg)
	m.state = stateChat
	return m
}

// pressEnter synthesises a tea.KeyMsg for the Enter key.
func pressEnter() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

// pressKey synthesises a tea.KeyMsg for a named key string (e.g. "up", "k").
func pressKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// ---------------------------------------------------------------------------
// modelsFetchedMsg
// ---------------------------------------------------------------------------

func TestUpdate_ModelsFetched_Success(t *testing.T) {
	m := newTestModel()
	m.state = stateLoadingModels

	next, _ := m.Update(modelsFetchedMsg{models: []string{"llama3", "mistral"}, err: nil})
	m2 := next.(Model)

	if m2.state != stateSelectModel {
		t.Errorf("state: got %v, want stateSelectModel", m2.state)
	}
	if len(m2.models) != 2 {
		t.Errorf("models: got %d, want 2", len(m2.models))
	}
	if m2.err != nil {
		t.Errorf("unexpected error: %v", m2.err)
	}
}

func TestUpdate_ModelsFetched_Error(t *testing.T) {
	m := newTestModel()
	m.state = stateLoadingModels

	next, _ := m.Update(modelsFetchedMsg{err: errors.New("connection refused")})
	m2 := next.(Model)

	if m2.state != stateSelectModel {
		t.Errorf("state: got %v, want stateSelectModel", m2.state)
	}
	if m2.err == nil {
		t.Error("expected an error, got nil")
	}
}

func TestUpdate_ModelsFetched_Empty(t *testing.T) {
	m := newTestModel()
	m.state = stateLoadingModels

	next, _ := m.Update(modelsFetchedMsg{models: []string{}, err: nil})
	m2 := next.(Model)

	if m2.err == nil {
		t.Error("expected error for empty model list")
	}
}

// ---------------------------------------------------------------------------
// responseMsg (non-streaming)
// ---------------------------------------------------------------------------

func TestUpdate_ResponseMsg_Success(t *testing.T) {
	m := newTestModel()
	m.isResponding = true
	m.messages = []ollama.Message{{Role: "user", Content: "hi"}}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil}

	metrics := ollama.ResponseMetrics{PromptTokens: 3, EvalTokens: 7}
	next, _ := m.Update(responseMsg{content: "hello there", metrics: metrics, err: nil})
	m2 := next.(Model)

	if m2.isResponding {
		t.Error("isResponding should be false after response")
	}
	if len(m2.messages) != 2 {
		t.Fatalf("messages: got %d, want 2", len(m2.messages))
	}
	last := m2.messages[len(m2.messages)-1]
	if last.Role != "assistant" || last.Content != "hello there" {
		t.Errorf("last message: got %+v", last)
	}
	if m2.outputTokens != 7 {
		t.Errorf("outputTokens: got %d, want 7", m2.outputTokens)
	}
	if m2.inputTokens != 3 {
		t.Errorf("inputTokens: got %d, want 3", m2.inputTokens)
	}
}

func TestUpdate_ResponseMsg_Error(t *testing.T) {
	m := newTestModel()
	m.isResponding = true

	next, _ := m.Update(responseMsg{err: errors.New("timeout")})
	m2 := next.(Model)

	if m2.isResponding {
		t.Error("isResponding should be false on error")
	}
	if m2.err == nil {
		t.Error("expected error")
	}
}

// ---------------------------------------------------------------------------
// StreamMsg
// ---------------------------------------------------------------------------

func TestUpdate_StreamMsg_Accumulates(t *testing.T) {
	m := newTestModel()
	m.isResponding = true
	m.messages = []ollama.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: ""},
	}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil, nil}

	ch := make(chan StreamMsg, 1)

	next, _ := m.Update(StreamMsg{Content: "hel", Done: false, Channel: ch})
	m2 := next.(Model)

	last := m2.messages[len(m2.messages)-1]
	if last.Content != "hel" {
		t.Errorf("content after first chunk: got %q, want %q", last.Content, "hel")
	}
	if !m2.isResponding {
		t.Error("should still be responding after non-Done chunk")
	}
}

func TestUpdate_StreamMsg_Done(t *testing.T) {
	m := newTestModel()
	m.isResponding = true
	m.messages = []ollama.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hel"},
	}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil, nil}

	metrics := ollama.ResponseMetrics{PromptTokens: 2, EvalTokens: 5}
	next, _ := m.Update(StreamMsg{Content: "lo", Done: true, Metrics: metrics})
	m2 := next.(Model)

	last := m2.messages[len(m2.messages)-1]
	if last.Content != "hello" {
		t.Errorf("final content: got %q, want %q", last.Content, "hello")
	}
	if m2.isResponding {
		t.Error("should not be responding after Done chunk")
	}
	if m2.outputTokens != 5 {
		t.Errorf("outputTokens: got %d, want 5", m2.outputTokens)
	}
}

func TestUpdate_StreamMsg_Error_RemovesEmptyPlaceholder(t *testing.T) {
	m := newTestModel()
	m.isResponding = true
	m.messages = []ollama.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: ""}, // empty placeholder
	}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil, nil}

	next, _ := m.Update(StreamMsg{Err: errors.New("stream broken")})
	m2 := next.(Model)

	if m2.isResponding {
		t.Error("should not be responding after error")
	}
	// Empty assistant placeholder should be removed on error.
	if len(m2.messages) != 1 {
		t.Errorf("messages: got %d, want 1 (placeholder removed)", len(m2.messages))
	}
	if m2.err == nil {
		t.Error("expected error to be set")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /new
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_New(t *testing.T) {
	m := newTestModel()
	m.messages = []ollama.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil, nil}
	m.inputTokens = 10
	m.outputTokens = 20

	m2, _, handled := m.handleSlashCommand("/new")

	if !handled {
		t.Fatal("expected /new to be handled")
	}
	if len(m2.messages) != 0 {
		t.Errorf("messages not cleared: got %d", len(m2.messages))
	}
	if m2.inputTokens != 0 || m2.outputTokens != 0 {
		t.Errorf("tokens not reset: in=%d out=%d", m2.inputTokens, m2.outputTokens)
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /think
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_Think_Valid(t *testing.T) {
	validSettings := []string{"false", "true", "low", "medium", "high", "max"}
	for _, s := range validSettings {
		t.Run(s, func(t *testing.T) {
			m := newTestModel()
			m2, _, handled := m.handleSlashCommand("/think " + s)
			if !handled {
				t.Fatal("expected /think to be handled")
			}
			if m2.thinkSetting != s {
				t.Errorf("thinkSetting: got %q, want %q", m2.thinkSetting, s)
			}
			if m2.err != nil {
				t.Errorf("unexpected error: %v", m2.err)
			}
		})
	}
}

func TestHandleSlashCommand_Think_Invalid(t *testing.T) {
	m := newTestModel()
	m2, _, handled := m.handleSlashCommand("/think bogus")

	if !handled {
		t.Fatal("expected /think to be handled even with invalid setting")
	}
	if m2.err == nil {
		t.Error("expected an error for invalid think setting")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /stream
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_Stream(t *testing.T) {
	tests := []struct {
		input      string
		wantStream bool
	}{
		{"/stream true", true},
		{"/stream yes", true},
		{"/stream on", true},
		{"/stream false", false},
		{"/stream no", false},
		{"/stream off", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			m := newTestModel()
			m2, _, handled := m.handleSlashCommand(tc.input)
			if !handled {
				t.Fatal("expected /stream to be handled")
			}
			if m2.cfg.Stream != tc.wantStream {
				t.Errorf("cfg.Stream: got %v, want %v", m2.cfg.Stream, tc.wantStream)
			}
		})
	}
}

func TestHandleSlashCommand_Stream_Invalid(t *testing.T) {
	m := newTestModel()
	m2, _, handled := m.handleSlashCommand("/stream maybe")

	if !handled {
		t.Fatal("expected /stream to be handled")
	}
	if m2.err == nil {
		t.Error("expected error for invalid stream setting")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /system
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_System_Set(t *testing.T) {
	m := newTestModel()
	m2, _, handled := m.handleSlashCommand("/system You are a pirate.")

	if !handled {
		t.Fatal("expected /system to be handled")
	}
	if len(m2.messages) == 0 || m2.messages[0].Role != "system" {
		t.Fatal("expected system message to be prepended")
	}
	if m2.messages[0].Content != "You are a pirate." {
		t.Errorf("content: got %q", m2.messages[0].Content)
	}
}

func TestHandleSlashCommand_System_Replace(t *testing.T) {
	m := newTestModel()
	m.messages = []ollama.Message{{Role: "system", Content: "old prompt"}}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil}

	m2, _, _ := m.handleSlashCommand("/system new prompt")

	if m2.messages[0].Content != "new prompt" {
		t.Errorf("expected replaced system prompt, got %q", m2.messages[0].Content)
	}
	if len(m2.messages) != 1 {
		t.Errorf("should not add a second system message, got %d", len(m2.messages))
	}
}

func TestHandleSlashCommand_System_Clear(t *testing.T) {
	m := newTestModel()
	m.messages = []ollama.Message{
		{Role: "system", Content: "old prompt"},
		{Role: "user", Content: "hi"},
	}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil, nil}

	// Empty prompt after /system → remove system message.
	m2, _, _ := m.handleSlashCommand("/system ")

	if len(m2.messages) != 1 || m2.messages[0].Role == "system" {
		t.Errorf("system message not removed, messages: %+v", m2.messages)
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — unrecognised input
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_NotACommand(t *testing.T) {
	m := newTestModel()
	_, _, handled := m.handleSlashCommand("hello world")

	if handled {
		t.Error("plain text should not be handled as a command")
	}
}

// ---------------------------------------------------------------------------
// Copy-mode key handling
// ---------------------------------------------------------------------------

func TestUpdate_CopyMode_Navigation(t *testing.T) {
	m := newTestModel()
	m.state = stateCopy
	m.messages = []ollama.Message{
		{Role: "user", Content: "a"},
		{Role: "user", Content: "b"},
		{Role: "user", Content: "c"},
	}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil, nil, nil}
	m.copyCursor = 1

	// Press up.
	next, _ := m.Update(pressKey("k"))
	m2 := next.(Model)
	if m2.copyCursor != 0 {
		t.Errorf("up: copyCursor=%d, want 0", m2.copyCursor)
	}

	// Press down twice from cursor=0.
	next, _ = m2.Update(pressKey("j"))
	m3 := next.(Model)
	next, _ = m3.Update(pressKey("j"))
	m4 := next.(Model)
	if m4.copyCursor != 2 {
		t.Errorf("down x2: copyCursor=%d, want 2", m4.copyCursor)
	}

	// Can't go past end.
	next, _ = m4.Update(pressKey("j"))
	m5 := next.(Model)
	if m5.copyCursor != 2 {
		t.Errorf("past end: copyCursor=%d, want 2", m5.copyCursor)
	}
}

func TestUpdate_CopyMode_Esc_ReturnsToChatState(t *testing.T) {
	m := newTestModel()
	m.state = stateCopy
	m.messages = []ollama.Message{{Role: "user", Content: "hi"}}
	m.messagesMetrics = []*ollama.ResponseMetrics{nil}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := next.(Model)

	if m2.state != stateChat {
		t.Errorf("state: got %v, want stateChat", m2.state)
	}
}

// ---------------------------------------------------------------------------
// Model-select key handling
// ---------------------------------------------------------------------------

func TestUpdate_SelectModel_Enter(t *testing.T) {
	m := newTestModel()
	m.state = stateSelectModel
	m.models = []string{"llama3", "mistral"}
	m.cursor = 1

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := next.(Model)

	if m2.cfg.CurrentModel != "mistral" {
		t.Errorf("CurrentModel: got %q, want %q", m2.cfg.CurrentModel, "mistral")
	}
	if m2.state != stateChat {
		t.Errorf("state: got %v, want stateChat", m2.state)
	}
}

// ---------------------------------------------------------------------------
// isPopupActive
// ---------------------------------------------------------------------------

func TestIsPopupActive(t *testing.T) {
	m := newTestModel()
	m.state = stateChat

	m.textInput.SetValue("/")
	if !m.isPopupActive() {
		t.Error("expected popup active when input starts with /")
	}

	m.textInput.SetValue("hello")
	if m.isPopupActive() {
		t.Error("expected popup inactive for non-slash input")
	}

	m.state = stateCopy
	m.textInput.SetValue("/")
	if m.isPopupActive() {
		t.Error("expected popup inactive outside stateChat")
	}
}

// ---------------------------------------------------------------------------
// getMatchingActions
// ---------------------------------------------------------------------------

func TestGetMatchingActions(t *testing.T) {
	m := newTestModel()

	m.textInput.SetValue("/")
	matches := m.getMatchingActions()
	if len(matches) != len(actions) {
		t.Errorf("'/' should match all actions, got %d", len(matches))
	}

	m.textInput.SetValue("/th")
	matches = m.getMatchingActions()
	for _, a := range matches {
		if !strings.HasPrefix(a.key, "/th") {
			t.Errorf("unexpected match %q for prefix /th", a.key)
		}
	}

	m.textInput.SetValue("/zzz")
	matches = m.getMatchingActions()
	if len(matches) != 0 {
		t.Errorf("expected no matches for /zzz, got %d", len(matches))
	}
}
