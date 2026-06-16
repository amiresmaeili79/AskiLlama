package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"askillama/internal/config"
	"askillama/internal/ollama"
)

// newSessionTestModel builds a Model with some conversation history suitable
// for session round-trip testing. cfg.filePath is intentionally left empty so
// cfg.Save() is a no-op (it writes to "" which fails silently).
func newSessionTestModel(t *testing.T) *Model {
	t.Helper()
	cfg := &config.Config{
		CurrentModel: "llama3",
		Stream:       true,
	}
	m := NewModel(cfg)
	m.thinkSetting = "medium"
	m.messages = []ollama.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	evalTokens := 5
	promptTokens := 3
	m.messagesMetrics = []*ollama.ResponseMetrics{
		nil,
		{
			EvalTokens:    evalTokens,
			PromptTokens:  promptTokens,
			TotalDuration: 100 * time.Millisecond,
		},
	}
	m.inputTokens = promptTokens
	m.outputTokens = evalTokens
	return &m
}

// ---------------------------------------------------------------------------
// saveSessionTo / loadSessionFrom — round-trip
// ---------------------------------------------------------------------------

func TestSession_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := newSessionTestModel(t)

	if err := src.saveSessionTo(dir, "roundtrip"); err != nil {
		t.Fatalf("saveSessionTo: %v", err)
	}

	dst := newSessionTestModel(t)
	dst.messages = nil
	dst.messagesMetrics = nil
	dst.thinkSetting = "false"
	dst.cfg.CurrentModel = ""

	if err := dst.loadSessionFrom(dir, "roundtrip"); err != nil {
		t.Fatalf("loadSessionFrom: %v", err)
	}

	if dst.cfg.CurrentModel != src.cfg.CurrentModel {
		t.Errorf("CurrentModel: got %q, want %q", dst.cfg.CurrentModel, src.cfg.CurrentModel)
	}
	if dst.cfg.Stream != src.cfg.Stream {
		t.Errorf("Stream: got %v, want %v", dst.cfg.Stream, src.cfg.Stream)
	}
	if dst.thinkSetting != src.thinkSetting {
		t.Errorf("thinkSetting: got %q, want %q", dst.thinkSetting, src.thinkSetting)
	}
	if len(dst.messages) != len(src.messages) {
		t.Fatalf("messages: got %d, want %d", len(dst.messages), len(src.messages))
	}
	for i, msg := range dst.messages {
		if msg.Content != src.messages[i].Content || msg.Role != src.messages[i].Role {
			t.Errorf("messages[%d]: got %+v, want %+v", i, msg, src.messages[i])
		}
	}
}

func TestSession_TokenCountsRecalculated(t *testing.T) {
	dir := t.TempDir()
	src := newSessionTestModel(t)
	// Force wrong counters before save to verify they're recomputed on load.
	src.inputTokens = 9999
	src.outputTokens = 9999

	if err := src.saveSessionTo(dir, "tokens"); err != nil {
		t.Fatalf("saveSessionTo: %v", err)
	}

	dst := newSessionTestModel(t)
	if err := dst.loadSessionFrom(dir, "tokens"); err != nil {
		t.Fatalf("loadSessionFrom: %v", err)
	}

	// messagesMetrics[1] has PromptTokens=3, EvalTokens=5
	if dst.inputTokens != 3 {
		t.Errorf("inputTokens: got %d, want 3", dst.inputTokens)
	}
	if dst.outputTokens != 5 {
		t.Errorf("outputTokens: got %d, want 5", dst.outputTokens)
	}
}

// ---------------------------------------------------------------------------
// loadSessionFrom — not found
// ---------------------------------------------------------------------------

func TestSession_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	m := newSessionTestModel(t)

	err := m.loadSessionFrom(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// saveSessionTo — empty name
// ---------------------------------------------------------------------------

func TestSession_EmptyName(t *testing.T) {
	m := newSessionTestModel(t)

	// saveSession and loadSession validate the name before touching the filesystem,
	// so no temp directory is needed.
	if err := m.saveSession(""); err == nil {
		t.Error("expected error for empty name")
	}
	if err := m.loadSession(""); err == nil {
		t.Error("expected error for empty name on load")
	}
}

// ---------------------------------------------------------------------------
// exportChat
// ---------------------------------------------------------------------------

func TestExportChat_ContainsMessages(t *testing.T) {
	m := newSessionTestModel(t)
	dir := t.TempDir()
	outFile := filepath.Join(dir, "out.md")

	if err := m.exportChat(outFile); err != nil {
		t.Fatalf("exportChat: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"# AskiLlama Chat Export",
		"👤 You",
		"hello",
		"🤖 Ollama",
		"hi there",
		"llama3",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("export missing %q", want)
		}
	}
}

func TestExportChat_MetricsBlock(t *testing.T) {
	m := newSessionTestModel(t)
	dir := t.TempDir()
	outFile := filepath.Join(dir, "metrics.md")

	if err := m.exportChat(outFile); err != nil {
		t.Fatalf("exportChat: %v", err)
	}
	data, _ := os.ReadFile(outFile)
	content := string(data)

	if !strings.Contains(content, "Performance Metrics") {
		t.Error("expected performance metrics block in export")
	}
	if !strings.Contains(content, "<details>") {
		t.Error("expected <details> block for collapsed metrics")
	}
}

func TestExportChat_MdExtensionAutoAppended(t *testing.T) {
	m := newSessionTestModel(t)
	dir := t.TempDir()

	// Provide a filename without .md extension.
	noExt := filepath.Join(dir, "noext")
	if err := m.exportChat(noExt); err != nil {
		t.Fatalf("exportChat: %v", err)
	}

	// The file should have been written with .md appended.
	if _, err := os.Stat(noExt + ".md"); os.IsNotExist(err) {
		t.Error("expected noext.md to exist, not found")
	}
}

func TestExportChat_DefaultFilename(t *testing.T) {
	m := newSessionTestModel(t)

	// Change working directory to a temp dir so the default file lands there.
	dir := t.TempDir()
	old, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(old) })

	if err := m.exportChat(""); err != nil {
		t.Fatalf("exportChat with empty name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "askillama-chat.md")); os.IsNotExist(err) {
		t.Error("expected askillama-chat.md to be created")
	}
}
