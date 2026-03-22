// Package llm provides LLM provider abstractions for the agent module.
// Supports OpenAI-compatible and Anthropic APIs with tool calling.
package llm

import "context"

// Role represents a message role in the conversation
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a single message in a conversation
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation requested by the LLM
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition describes a tool available to the LLM
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// CompletionRequest represents a request to the LLM
type CompletionRequest struct {
	Messages    []Message
	Tools       []ToolDefinition
	MaxTokens   int
	Temperature float64
}

// CompletionResponse represents the LLM's response
type CompletionResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string // "stop", "tool_calls"
}

// StreamChunk represents a single chunk in a streaming response
type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}

// Provider defines the interface for LLM providers
type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	CompleteStream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error)
	Name() string
}
