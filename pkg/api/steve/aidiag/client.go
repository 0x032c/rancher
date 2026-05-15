package aidiag

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/rancher/rancher/pkg/settings"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
}

type ChatChoice struct {
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type ChatChunk struct {
	Choices []ChatChoice `json:"choices"`
}

type AIClient struct {
	httpClient *http.Client
}

func NewAIClient() *AIClient {
	return &AIClient{
		httpClient: &http.Client{},
	}
}

// StreamChat sends a streaming chat completion request and writes SSE data
// to the provided callback for each token received.
func (c *AIClient) StreamChat(ctx context.Context, messages []ChatMessage, onChunk func(content string) error) error {
	endpoint := strings.TrimRight(settings.AIEndpoint.Get(), "/")
	if endpoint == "" {
		return fmt.Errorf("ai-endpoint is not configured")
	}

	apiKey := settings.AIAPIKey.Get()
	if apiKey == "" {
		return fmt.Errorf("ai-api-key is not configured")
	}

	model := settings.AIModel.Get()
	maxTokens, _ := strconv.Atoi(settings.AIMaxTokens.Get())
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	reqBody := ChatRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Stream:      true,
		Temperature: 0.3,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := endpoint + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("AI service request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("AI service returned status %d: %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk ChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if err := onChunk(choice.Delta.Content); err != nil {
					return err
				}
			}
		}
	}

	return scanner.Err()
}
