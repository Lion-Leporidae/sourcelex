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

// OpenAIConfig holds configuration for the OpenAI provider.
// BaseURL can be overridden to use compatible APIs (DeepSeek, Ollama, etc.)
type OpenAIConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

// OpenAIProvider implements Provider for OpenAI-compatible APIs
type OpenAIProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	return &OpenAIProvider{
		apiKey:  cfg.APIKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

// --- wire types ---

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Tools       []oaiTool    `json:"tools,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature float64      `json:"temperature"`
	Stream      bool         `json:"stream,omitempty"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  []oaiToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiToolCallFunc `json:"function"`
}

type oaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
}

type oaiChoice struct {
	Message      oaiMessage `json:"message"`
	Delta        oaiMessage `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

// --- implementation ---

func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	oaiReq := p.buildRequest(req, false)

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("OpenAI API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API 错误 %d: %s", resp.StatusCode, string(respBody))
	}

	var oaiResp oaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("OpenAI 返回空响应")
	}

	choice := oaiResp.Choices[0]
	result := &CompletionResponse{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
	}
	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return result, nil
}

func (p *OpenAIProvider) CompleteStream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error) {
	oaiReq := p.buildRequest(req, true)

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("OpenAI API 请求失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("OpenAI API 错误 %d: %s", resp.StatusCode, string(respBody))
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
			if data == "[DONE]" {
				ch <- StreamChunk{Done: true}
				return
			}
			var sResp oaiResponse
			if err := json.Unmarshal([]byte(data), &sResp); err != nil {
				continue
			}
			if len(sResp.Choices) > 0 && sResp.Choices[0].Delta.Content != "" {
				ch <- StreamChunk{Content: sResp.Choices[0].Delta.Content}
			}
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

func (p *OpenAIProvider) buildRequest(req CompletionRequest, stream bool) oaiRequest {
	messages := make([]oaiMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := oaiMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, oaiToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: oaiToolCallFunc{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
		messages = append(messages, msg)
	}

	var tools []oaiTool
	for _, t := range req.Tools {
		tools = append(tools, oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	return oaiRequest{
		Model:       p.model,
		Messages:    messages,
		Tools:       tools,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Stream:      stream,
	}
}
