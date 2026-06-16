package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"askillama/internal/ollama"
)

// SavedSession is the on-disk representation of a chat session.
// It mirrors the live Model state so that a session can be fully
// reconstructed after the application restarts.
type SavedSession struct {
	Model        string           `json:"model"`
	Stream       bool             `json:"stream"`
	ThinkSetting string           `json:"think_setting"`
	Messages     []ollama.Message `json:"messages"`

	// MessagesMetrics is parallel to Messages: index i holds the performance
	// metrics for Messages[i]. Pointer elements allow nil to represent turns
	// that have no metrics (e.g. user or system messages, or a response that
	// was interrupted before the Done chunk arrived).
	MessagesMetrics []*ollama.ResponseMetrics `json:"messages_metrics"`
}

func (m *Model) saveSession(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("session name cannot be empty")
	}

	// os.UserConfigDir returns the OS-appropriate config root:
	// ~/.config on Linux/XDG, ~/Library/Application Support on macOS, %AppData% on Windows.
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("could not find user config dir: %w", err)
	}

	// MkdirAll is a no-op when the directory already exists, so calling it
	// unconditionally is safe and avoids a separate existence check.
	sessionsDir := filepath.Join(configDir, "askillama", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}

	session := SavedSession{
		Model:           m.cfg.CurrentModel,
		Stream:          m.cfg.Stream,
		ThinkSetting:    m.thinkSetting,
		Messages:        m.messages,
		MessagesMetrics: m.messagesMetrics,
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	filePath := filepath.Join(sessionsDir, name+".json")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	return nil
}

func (m *Model) loadSession(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("session name cannot be empty")
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("could not find user config dir: %w", err)
	}

	filePath := filepath.Join(configDir, "askillama", "sessions", name+".json")

	// Stat before ReadFile to give a user-friendly "not found" error instead of
	// a raw "open …: no such file or directory" message from ReadFile.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("session '%s' not found", name)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read session file: %w", err)
	}

	var session SavedSession
	if err := json.Unmarshal(data, &session); err != nil {
		return fmt.Errorf("failed to parse session file: %w", err)
	}

	// Restore session settings into the live model.
	// CurrentModel is persisted to disk immediately via cfg.Save so that the
	// selection survives a crash before the next explicit save. The error is
	// intentionally ignored — a failed config write is non-fatal here.
	m.cfg.CurrentModel = session.Model
	_ = m.cfg.Save()
	m.cfg.Stream = session.Stream
	m.thinkSetting = session.ThinkSetting
	m.messages = session.Messages
	m.messagesMetrics = session.MessagesMetrics

	// Token counts are derived values, not stored independently, so we
	// recompute them from the loaded metrics rather than risk them going
	// out of sync with the actual message history.
	m.inputTokens = 0
	m.outputTokens = 0
	for _, metrics := range m.messagesMetrics {
		if metrics != nil {
			m.inputTokens += metrics.PromptTokens
			m.outputTokens += metrics.EvalTokens
		}
	}

	return nil
}

// exportChat writes the current conversation to a Markdown file.
// The output is self-contained: it includes a header with metadata and one
// section per message, each separated by a horizontal rule.
func (m *Model) exportChat(filename string) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "askillama-chat.md"
	}
	// Ensure the file always has a .md extension so that Markdown viewers
	// open it correctly even when the user omits the extension.
	if !strings.HasSuffix(strings.ToLower(filename), ".md") {
		filename += ".md"
	}

	var sb strings.Builder
	sb.WriteString("# AskiLlama Chat Export\n\n")
	fmt.Fprintf(&sb, "- **Date**: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "- **Model**: %s\n\n", m.cfg.CurrentModel)
	sb.WriteString("---\n\n")

	for i, msg := range m.messages {
		switch msg.Role {
		case "user":
			sb.WriteString("### 👤 You\n\n")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		case "system":
			sb.WriteString("> ⚙️ **System Prompt**\n")
			sb.WriteString("> ")
			// Prefix every newline with "> " so that multi-line system prompts
			// render as a single Markdown blockquote rather than breaking out of it.
			sb.WriteString(strings.ReplaceAll(msg.Content, "\n", "\n> "))
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("### 🤖 Ollama\n\n")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")

			// messagesMetrics is parallel to m.messages, so index i directly
			// addresses the metrics for this assistant turn. The bounds check
			// guards against a slice that is shorter than m.messages (e.g. if a
			// response was interrupted before metrics were recorded).
			if i < len(m.messagesMetrics) && m.messagesMetrics[i] != nil {
				metrics := m.messagesMetrics[i]
				// Wrapped in a <details> block so the metrics are collapsed by
				// default in GitHub / most Markdown renderers.
				sb.WriteString("<details>\n<summary>⚡ Performance Metrics</summary>\n\n")
				fmt.Fprintf(&sb, "- **Tokens Per Second**: %s\n", formatTPS(metrics.TokensPerSecond))
				fmt.Fprintf(&sb, "- **Time To First Token**: %s\n", formatDuration(metrics.TimeToFirstToken))
				fmt.Fprintf(&sb, "- **Total Generation Time**: %s\n", formatDuration(metrics.TotalDuration))
				fmt.Fprintf(&sb, "- **Prompt Tokens**: %d\n", metrics.PromptTokens)
				fmt.Fprintf(&sb, "- **Eval Tokens**: %d\n", metrics.EvalTokens)
				sb.WriteString("</details>\n\n")
			}
		}
		sb.WriteString("---\n\n")
	}

	if err := os.WriteFile(filename, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	return nil
}
