package ui

import (
	"context"

	"askillama/internal/ollama"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

// modelsFetchedMsg is sent when the model list fetch completes (or fails).
type modelsFetchedMsg struct {
	models []string
	err    error
}

// responseMsg is sent when a non-streaming chat response completes.
type responseMsg struct {
	content string
	metrics ollama.ResponseMetrics
	err     error
}

// StreamMsg carries a single streamed chunk from the Ollama API.
// When Done is true the stream is finished and Metrics are populated.
// Channel is the channel the caller should continue listening on; it is set
// by listenToStream before the message is dispatched to Update.
type StreamMsg struct {
	Content string
	Metrics ollama.ResponseMetrics
	Done    bool
	Err     error
	Channel chan StreamMsg
}

// ---------------------------------------------------------------------------
// Command factories
// ---------------------------------------------------------------------------

// fetchModelsCmd returns a tea.Cmd that fetches the list of local Ollama models.
func (m Model) fetchModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := m.client.ListModels()
		return modelsFetchedMsg{models: models, err: err}
	}
}

// sendMessageCmd returns a tea.Cmd that sends a non-streaming chat request.
func (m Model) sendMessageCmd() tea.Cmd {
	return func() tea.Msg {
		resp, metrics, err := m.client.Chat(m.cfg.CurrentModel, m.messages, m.thinkSetting)
		return responseMsg{content: resp, metrics: metrics, err: err}
	}
}

// listenToStream returns a tea.Cmd that reads one message from ch and
// re-attaches the channel so Update can schedule another listen.
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

// sendStreamMessageCmd starts a goroutine that streams the chat response into
// ch, then returns a tea.Cmd that kicks off the goroutine. The caller must
// also dispatch listenToStream(ch) so that Update receives the chunks.
//
// Note: m.messages[len-1] is the empty assistant placeholder added before this
// command is fired, so we slice it off before sending to the API.
func (m Model) sendStreamMessageCmd(ch chan StreamMsg) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := m.client.StreamChat(
			ctx,
			m.cfg.CurrentModel,
			m.messages[:len(m.messages)-1], // exclude the empty assistant placeholder
			m.thinkSetting,
			func(content string, done bool, metrics ollama.ResponseMetrics) error {
				ch <- StreamMsg{Content: content, Done: done, Metrics: metrics}
				return nil
			},
		)
		if err != nil {
			ch <- StreamMsg{Err: err}
		}
		close(ch)
		return nil
	}
}
