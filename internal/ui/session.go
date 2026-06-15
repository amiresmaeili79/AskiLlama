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

type SavedSession struct {
	Model           string                    `json:"model"`
	Stream          bool                      `json:"stream"`
	ThinkSetting    string                    `json:"think_setting"`
	Messages        []ollama.Message          `json:"messages"`
	MessagesMetrics []*ollama.ResponseMetrics `json:"messages_metrics"`
}

func (m *Model) saveSession(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("session name cannot be empty")
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("could not find user config dir: %w", err)
	}

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

	// Restore session settings
	m.cfg.CurrentModel = session.Model
	_ = m.cfg.Save()
	m.cfg.Stream = session.Stream
	m.thinkSetting = session.ThinkSetting
	m.messages = session.Messages
	m.messagesMetrics = session.MessagesMetrics

	// Calculate token counts
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

func (m *Model) exportChat(filename string) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "askillama-chat.md"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".md") {
		filename += ".md"
	}

	var sb strings.Builder
	sb.WriteString("# AskiLlama Chat Export\n\n")
	sb.WriteString(fmt.Sprintf("- **Date**: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("- **Model**: %s\n\n", m.cfg.CurrentModel))
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
			sb.WriteString(strings.ReplaceAll(msg.Content, "\n", "\n> "))
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("### 🤖 Ollama\n\n")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")

			if i < len(m.messagesMetrics) && m.messagesMetrics[i] != nil {
				metrics := m.messagesMetrics[i]
				sb.WriteString("<details>\n<summary>⚡ Performance Metrics</summary>\n\n")
				sb.WriteString(fmt.Sprintf("- **Tokens Per Second**: %s\n", formatTPS(metrics.TokensPerSecond)))
				sb.WriteString(fmt.Sprintf("- **Time To First Token**: %s\n", formatDuration(metrics.TimeToFirstToken)))
				sb.WriteString(fmt.Sprintf("- **Total Generation Time**: %s\n", formatDuration(metrics.TotalDuration)))
				sb.WriteString(fmt.Sprintf("- **Prompt Tokens**: %d\n", metrics.PromptTokens))
				sb.WriteString(fmt.Sprintf("- **Eval Tokens**: %d\n", metrics.EvalTokens))
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
