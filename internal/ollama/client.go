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
		Timeout: 60 * time.Second,
	}

	sdkClient := api.NewClient(parsedURL, httpClient)
	return &Client{
		sdkClient: sdkClient,
	}
}

// ListModels fetches the list of local model names from Ollama
func (c *Client) ListModels() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

// Chat sends a chat request to Ollama and returns the complete text response (non-streaming)
func (c *Client) Chat(model string, messages []Message) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream := false
	req := &api.ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   &stream,
	}

	var responseText string
	err := c.sdkClient.Chat(ctx, req, func(resp api.ChatResponse) error {
		responseText += resp.Message.Content
		return nil
	})
	if err != nil {
		return "", err
	}

	return responseText, nil
}
