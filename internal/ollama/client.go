package ollama

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/ollama/ollama/api"
)

// Re-export api.Message as Message to avoid ui depending directly on SDK types
type Message = api.Message

// ResponseMetrics holds the generation metrics for a chat response
type ResponseMetrics struct {
	TokensPerSecond  float64
	TimeToFirstToken time.Duration
	TotalDuration    time.Duration
	PromptTokens     int
	EvalTokens       int
}

// Client wraps the official Ollama Go SDK Client
type Client struct {
	sdkClient *api.Client
}

// NewClient creates a new Client wrapping the official Ollama Go Client
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		parsedURL, _ = url.Parse("http://localhost:11434")
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Minute,
	}

	sdkClient := api.NewClient(parsedURL, httpClient)
	return &Client{
		sdkClient: sdkClient,
	}
}

// ListModels fetches the list of local model names from Ollama
func (c *Client) ListModels() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.sdkClient.List(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(resp.Models))
	for i, m := range resp.Models {
		names[i] = m.Name
	}
	return names, nil
}

// buildChatRequest constructs an api.ChatRequest for the given model, messages, and think setting.
// The stream flag controls whether the SDK will deliver the response as a stream or a single payload.
//
// thinkSetting controls the model's extended-thinking budget:
//   - "true" / "false" → enable or disable thinking entirely
//   - "low" / "medium" / "high" / "max" → set a named effort level (model-dependent)
//   - anything else (e.g. "") → leave the field unset, letting the model use its default
func buildChatRequest(model string, messages []Message, stream bool, thinkSetting string) *api.ChatRequest {
	req := &api.ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   &stream,
	}

	switch thinkSetting {
	case "false":
		req.Think = &api.ThinkValue{Value: false}
	case "true":
		req.Think = &api.ThinkValue{Value: true}
	case "low", "medium", "high", "max":
		req.Think = &api.ThinkValue{Value: thinkSetting}
	}

	return req
}

// extractMetrics builds a ResponseMetrics from a completed (resp.Done == true) ChatResponse.
// ttft is the time-to-first-token measured by the caller; when it is zero (e.g. non-streaming
// requests where no chunk timing is available) we fall back to resp.LoadDuration +
// resp.PromptEvalDuration reported by Ollama itself.
func extractMetrics(resp api.ChatResponse, ttft time.Duration) ResponseMetrics {
	m := ResponseMetrics{
		TotalDuration: resp.TotalDuration,
		PromptTokens:  resp.PromptEvalCount,
		EvalTokens:    resp.EvalCount,
		// Prefer caller-measured TTFT; fall back to Ollama's own load+prompt-eval duration.
		TimeToFirstToken: ttft,
	}
	if m.TimeToFirstToken == 0 {
		m.TimeToFirstToken = resp.LoadDuration + resp.PromptEvalDuration
	}
	if resp.EvalDuration > 0 {
		m.TokensPerSecond = float64(resp.EvalCount) / resp.EvalDuration.Seconds()
	}
	return m
}

// Chat sends a non-streaming chat request and returns the complete response text and metrics.
// It creates its own context with a 10-minute timeout.
func (c *Client) Chat(model string, messages []Message, thinkSetting string) (string, ResponseMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	req := buildChatRequest(model, messages, false /* stream */, thinkSetting)

	// For non-streaming requests the SDK still invokes the callback once per response chunk,
	// but in practice only one call arrives with resp.Done == true.
	// We accumulate content defensively in case the SDK ever splits the payload.
	startTime := time.Now()
	var responseText string
	var metrics ResponseMetrics

	err := c.sdkClient.Chat(ctx, req, func(resp api.ChatResponse) error {
		responseText += resp.Message.Content
		if resp.Done {
			// Non-streaming has no per-chunk timing, so ttft stays zero and
			// extractMetrics will fall back to Ollama's reported durations.
			// If those are also zero (cold-start edge case) we use wall-clock elapsed time.
			ttft := resp.LoadDuration + resp.PromptEvalDuration
			if ttft == 0 {
				ttft = time.Since(startTime)
			}
			metrics = extractMetrics(resp, ttft)
		}
		return nil
	})
	if err != nil {
		return "", ResponseMetrics{}, err
	}

	return responseText, metrics, nil
}

// StreamChat sends a streaming chat request and delivers each response chunk to onChunk.
// The caller is responsible for providing a context (e.g. with cancellation support).
// onChunk receives:
//   - content: the incremental text for this chunk (may be empty on the final chunk)
//   - done:    true on the last chunk, at which point metrics are populated
//   - metrics: non-zero only when done == true
func (c *Client) StreamChat(ctx context.Context, model string, messages []Message, thinkSetting string,
	onChunk func(content string, done bool, metrics ResponseMetrics) error) error {
	req := buildChatRequest(model, messages, true /* stream */, thinkSetting)

	// Track time-to-first-token: record wall-clock time when the first non-empty
	// content chunk arrives. This is more accurate than Ollama's reported durations
	// for streaming because it includes any network latency between server and client.
	startTime := time.Now()
	var ttft time.Duration
	var hasFirstToken bool

	return c.sdkClient.Chat(ctx, req, func(resp api.ChatResponse) error {
		if !hasFirstToken && resp.Message.Content != "" {
			ttft = time.Since(startTime)
			hasFirstToken = true
		}

		// metrics is only meaningful on the final chunk; it stays zero-valued otherwise.
		var metrics ResponseMetrics
		if resp.Done {
			metrics = extractMetrics(resp, ttft)
		}

		return onChunk(resp.Message.Content, resp.Done, metrics)
	})
}
