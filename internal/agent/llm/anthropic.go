package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnthropicConfig holds configuration for the Anthropic provider
type AnthropicConfig struct {
	APIKey string
	Model  string
}

// AnthropicProvider implements Provider for the Anthropic Messages API
type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewAnthropicProvider(cfg AnthropicConfig) *AnthropicProvider {
	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &AnthropicProvider{
		apiKey: cfg.APIKey,
		model:  model,
		client: &http.Client{},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

// --- wire types ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                 `json:"stop_reason"`
}

// Streaming event types
type anthropicStreamEvent struct {
	Type         string                `json:"type"`
	ContentBlock *anthropicContentBlock `json:"content_block,omitempty"`
	Delta        *anthropicDelta       `json:"delta,omitempty"`
}

type anthropicDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// --- implementation ---

func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	system, messages := p.convertMessages(req.Messages)

	var tools []anthropicTool
	for _, t := range req.Tools {
		tools = append(tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	aReq := anthropicRequest{
		Model:     p.model,
		System:    system,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: maxTokens,
	}

	body, err := json.Marshal(aReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Anthropic API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Anthropic API 错误 %d: %s", resp.StatusCode, string(respBody))
	}

	var aResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&aResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	result := &CompletionResponse{
		FinishReason: aResp.StopReason,
	}

	for _, block := range aResp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			args, _ := block.Input.MarshalJSON()
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(args),
			})
		}
	}

	if len(result.ToolCalls) > 0 {
		result.FinishReason = "tool_calls"
	}

	return result, nil
}

func (p *AnthropicProvider) CompleteStream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error) {
	system, messages := p.convertMessages(req.Messages)

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	aReq := anthropicRequest{
		Model:     p.model,
		System:    system,
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    true,
	}

	body, err := json.Marshal(aReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Anthropic API 请求失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("Anthropic API 错误 %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamChunk, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var event anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			switch event.Type {
			case "content_block_delta":
				if event.Delta != nil && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
					ch <- StreamChunk{Content: event.Delta.Text}
				}
			case "message_stop":
				ch <- StreamChunk{Done: true}
				return
			}
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

// convertMessages extracts system prompt and converts messages to Anthropic format.
// Handles Anthropic's requirement for role alternation and tool result grouping.
func (p *AnthropicProvider) convertMessages(messages []Message) (string, []anthropicMessage) {
	var system string
	var result []anthropicMessage

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			system = m.Content

		case RoleUser:
			result = append(result, anthropicMessage{
				Role:    "user",
				Content: m.Content,
			})

		case RoleAssistant:
			var blocks []anthropicContentBlock
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: json.RawMessage(tc.Arguments),
				})
			}
			if len(blocks) > 0 {
				result = append(result, anthropicMessage{Role: "assistant", Content: blocks})
			}

		case RoleTool:
			toolResult := anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}
			// Merge consecutive tool results into a single user message
			if n := len(result); n > 0 {
				if last := result[n-1]; last.Role == "user" {
					if blocks, ok := last.Content.([]anthropicContentBlock); ok {
						result[n-1].Content = append(blocks, toolResult)
						continue
					}
				}
			}
			result = append(result, anthropicMessage{
				Role:    "user",
				Content: []anthropicContentBlock{toolResult},
			})
		}
	}

	return system, result
}
