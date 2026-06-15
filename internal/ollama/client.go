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

// Chat sends a chat request to Ollama and returns the complete text response, prompt tokens, and eval tokens
func (c *Client) Chat(model string, messages []Message, thinkSetting string) (string, int, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	stream := false

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

	var responseText string
	var promptTokens, evalTokens int
	err := c.sdkClient.Chat(ctx, req, func(resp api.ChatResponse) error {
		responseText += resp.Message.Content
		if resp.Done {
			promptTokens = resp.PromptEvalCount
			evalTokens = resp.EvalCount
		}
		return nil
	})
	if err != nil {
		return "", 0, 0, err
	}

	return responseText, promptTokens, evalTokens, nil
}

// StreamChat sends a chat request to Ollama and streams the response chunks back via a callback function.
func (c *Client) StreamChat(ctx context.Context, model string, messages []Message, thinkSetting string, onChunk func(content string, done bool, promptTokens, evalTokens int) error) error {
	stream := true

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

	err := c.sdkClient.Chat(ctx, req, func(resp api.ChatResponse) error {
		return onChunk(resp.Message.Content, resp.Done, resp.PromptEvalCount, resp.EvalCount)
	})
	return err
}

