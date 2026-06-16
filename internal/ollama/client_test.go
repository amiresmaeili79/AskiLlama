package ollama

import (
	"testing"
	"time"

	"github.com/ollama/ollama/api"
)

// fakeResponse builds an api.ChatResponse with the fields used by extractMetrics.
// The metric fields live on the embedded api.Metrics struct.
func fakeResponse(total, evalDur time.Duration, evalCount, promptCount int, loadDur, promptEvalDur time.Duration) api.ChatResponse {
	return api.ChatResponse{
		Done: true,
		Metrics: api.Metrics{
			TotalDuration:      total,
			EvalDuration:       evalDur,
			EvalCount:          evalCount,
			PromptEvalCount:    promptCount,
			LoadDuration:       loadDur,
			PromptEvalDuration: promptEvalDur,
		},
	}
}

// ---------------------------------------------------------------------------
// buildChatRequest
// ---------------------------------------------------------------------------

func TestBuildChatRequest_StreamFlag(t *testing.T) {
	req := buildChatRequest("llama3", nil, true, "")
	if req.Stream == nil || !*req.Stream {
		t.Fatal("expected stream=true")
	}

	req = buildChatRequest("llama3", nil, false, "")
	if req.Stream == nil || *req.Stream {
		t.Fatal("expected stream=false")
	}
}

func TestBuildChatRequest_ModelAndMessages(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	req := buildChatRequest("mistral", msgs, false, "")
	if req.Model != "mistral" {
		t.Errorf("model: got %q, want %q", req.Model, "mistral")
	}
	if len(req.Messages) != 2 {
		t.Errorf("messages: got %d, want 2", len(req.Messages))
	}
}

func TestBuildChatRequest_ThinkSetting(t *testing.T) {
	tests := []struct {
		setting string
		wantNil bool
		wantVal any // bool or string
	}{
		{"", true, nil},
		{"false", false, false},
		{"true", false, true},
		{"low", false, "low"},
		{"medium", false, "medium"},
		{"high", false, "high"},
		{"max", false, "max"},
		{"unknown", true, nil}, // unrecognised value → Think must be nil
	}

	for _, tc := range tests {
		t.Run(tc.setting, func(t *testing.T) {
			req := buildChatRequest("m", nil, false, tc.setting)
			if tc.wantNil {
				if req.Think != nil {
					t.Errorf("Think should be nil for setting %q, got %v", tc.setting, req.Think)
				}
				return
			}
			if req.Think == nil {
				t.Fatalf("Think is nil for setting %q", tc.setting)
			}
			if req.Think.Value != tc.wantVal {
				t.Errorf("Think.Value: got %v, want %v", req.Think.Value, tc.wantVal)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractMetrics
// ---------------------------------------------------------------------------

func TestExtractMetrics_Basic(t *testing.T) {
	resp := fakeResponse(
		100*time.Millisecond, // TotalDuration
		500*time.Millisecond, // EvalDuration
		10,                   // EvalCount
		5,                    // PromptEvalCount
		0, 0,                 // LoadDuration, PromptEvalDuration
	)
	ttft := 50 * time.Millisecond
	m := extractMetrics(resp, ttft)

	if m.TotalDuration != 100*time.Millisecond {
		t.Errorf("TotalDuration: got %v, want 100ms", m.TotalDuration)
	}
	if m.PromptTokens != 5 {
		t.Errorf("PromptTokens: got %d, want 5", m.PromptTokens)
	}
	if m.EvalTokens != 10 {
		t.Errorf("EvalTokens: got %d, want 10", m.EvalTokens)
	}
	if m.TimeToFirstToken != ttft {
		t.Errorf("TTFT: got %v, want %v", m.TimeToFirstToken, ttft)
	}
	// 10 tokens / 0.5 s = 20 t/s
	if m.TokensPerSecond != 20.0 {
		t.Errorf("TPS: got %.2f, want 20.00", m.TokensPerSecond)
	}
}

func TestExtractMetrics_ZeroEvalDuration_NoTPS(t *testing.T) {
	resp := fakeResponse(0, 0, 5, 3, 0, 0)
	m := extractMetrics(resp, 10*time.Millisecond)
	if m.TokensPerSecond != 0 {
		t.Errorf("expected TPS=0 when EvalDuration=0, got %.2f", m.TokensPerSecond)
	}
}

func TestExtractMetrics_TTFTFallback(t *testing.T) {
	// When the caller passes ttft=0, should fall back to LoadDuration + PromptEvalDuration.
	resp := fakeResponse(0, 0, 0, 0, 20*time.Millisecond, 30*time.Millisecond)
	m := extractMetrics(resp, 0)
	want := 50 * time.Millisecond
	if m.TimeToFirstToken != want {
		t.Errorf("TTFT fallback: got %v, want %v", m.TimeToFirstToken, want)
	}
}

func TestExtractMetrics_BothTTFTZero_NoNegative(t *testing.T) {
	// All zero inputs — must not produce negative values or panic.
	resp := fakeResponse(0, 0, 0, 0, 0, 0)
	m := extractMetrics(resp, 0)
	if m.TimeToFirstToken < 0 {
		t.Errorf("TTFT must not be negative, got %v", m.TimeToFirstToken)
	}
	if m.TokensPerSecond < 0 {
		t.Errorf("TPS must not be negative, got %v", m.TokensPerSecond)
	}
}
