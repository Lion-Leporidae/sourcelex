// Package agent provides an AI-powered code analysis agent.
// It orchestrates LLM calls with knowledge base tool use to answer
// questions about codebases indexed by Sourcelex.
package agent

import (
	"context"
	"fmt"

	"github.com/Lion-Leporidae/sourcelex/internal/agent/llm"
	"github.com/Lion-Leporidae/sourcelex/internal/logger"
	"github.com/Lion-Leporidae/sourcelex/internal/store"
)

const defaultMaxIterations = 10

const systemPrompt = `你是 Sourcelex 代码知识库智能助手。你可以通过工具查询已索引的代码库，帮助用户理解代码结构、分析调用关系、定位功能实现。

工作方式：
1. 理解用户的问题，判断需要调用哪些工具
2. 使用工具获取代码知识库中的信息
3. 基于工具返回的真实数据，给出准确、有条理的回答

注意事项：
- 只基于工具返回的实际数据回答，不要编造代码内容
- 如果搜索没有找到相关结果，如实告知用户
- 回答时引用具体的文件路径、函数名、行号等信息
- 使用中文回答
- 对于代码片段使用 Markdown 代码块格式`

// Event represents a streaming event sent to the client
type Event struct {
	Type    string `json:"type"`    // "status", "chunk", "done", "error"
	Message string `json:"message,omitempty"`
	Content string `json:"content,omitempty"`
}

// ChatMessage represents a message in conversation history
type ChatMessage struct {
	Role    string `json:"role"`    // "user", "assistant"
	Content string `json:"content"`
}

// Config holds configuration for the CodeAgent
type Config struct {
	Provider      llm.Provider
	Store         *store.KnowledgeStore
	Log           *logger.Logger
	MaxIterations int
}

// CodeAgent orchestrates LLM + tool calling for code analysis
type CodeAgent struct {
	provider      llm.Provider
	store         *store.KnowledgeStore
	log           *logger.Logger
	maxIterations int
	tools         []llm.ToolDefinition
}

// New creates a new CodeAgent
func New(cfg Config) *CodeAgent {
	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}
	log := cfg.Log
	if log == nil {
		log = logger.NewDefault()
	}
	return &CodeAgent{
		provider:      cfg.Provider,
		store:         cfg.Store,
		log:           log,
		maxIterations: maxIter,
		tools:         AllTools(),
	}
}

// Chat processes a user query synchronously and returns the final answer
func (a *CodeAgent) Chat(ctx context.Context, query string, history []ChatMessage) (string, error) {
	messages := a.buildMessages(query, history)

	for i := 0; i < a.maxIterations; i++ {
		resp, err := a.provider.Complete(ctx, llm.CompletionRequest{
			Messages:    messages,
			Tools:       a.tools,
			Temperature: 0.1,
		})
		if err != nil {
			return "", fmt.Errorf("LLM 调用失败: %w", err)
		}

		if len(resp.ToolCalls) == 0 {
			return resp.Content, nil
		}

		// Append assistant message with tool calls
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			a.log.Debug("执行工具调用", "tool", tc.Name, "args", tc.Arguments)

			result, err := ExecuteTool(ctx, a.store, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("工具执行失败: %s", err.Error())
			}

			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return "", fmt.Errorf("达到最大迭代次数 (%d)，可能问题过于复杂", a.maxIterations)
}

// ChatStream processes a user query and streams events back.
// Events: status (during tool calls), chunk (answer text), done, error
func (a *CodeAgent) ChatStream(ctx context.Context, query string, history []ChatMessage) <-chan Event {
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)

		messages := a.buildMessages(query, history)

		for i := 0; i < a.maxIterations; i++ {
			resp, err := a.provider.Complete(ctx, llm.CompletionRequest{
				Messages:    messages,
				Tools:       a.tools,
				Temperature: 0.1,
			})
			if err != nil {
				ch <- Event{Type: "error", Message: fmt.Sprintf("LLM 调用失败: %s", err.Error())}
				return
			}

			if len(resp.ToolCalls) == 0 {
				// Final answer — stream it via LLM streaming if possible
				a.streamFinalAnswer(ctx, messages, resp.Content, ch)
				return
			}

			// Tool calling phase
			messages = append(messages, llm.Message{
				Role:      llm.RoleAssistant,
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			for _, tc := range resp.ToolCalls {
				ch <- Event{Type: "status", Message: fmt.Sprintf("正在调用工具: %s", toolDisplayName(tc.Name))}

				a.log.Debug("流式执行工具调用", "tool", tc.Name)
				result, err := ExecuteTool(ctx, a.store, tc.Name, tc.Arguments)
				if err != nil {
					result = fmt.Sprintf("工具执行失败: %s", err.Error())
				}

				messages = append(messages, llm.Message{
					Role:       llm.RoleTool,
					Content:    result,
					ToolCallID: tc.ID,
				})
			}
		}

		ch <- Event{Type: "error", Message: "达到最大迭代次数，可能问题过于复杂"}
	}()

	return ch
}

// streamFinalAnswer attempts to use LLM streaming for the final answer.
// Falls back to sending the already-received content if streaming fails.
func (a *CodeAgent) streamFinalAnswer(ctx context.Context, messages []llm.Message, fallbackContent string, ch chan<- Event) {
	// Try to get a streaming response for better UX
	streamCh, err := a.provider.CompleteStream(ctx, llm.CompletionRequest{
		Messages:    messages,
		Temperature: 0.1,
	})
	if err != nil {
		// Fallback: send the already-received content
		ch <- Event{Type: "chunk", Content: fallbackContent}
		ch <- Event{Type: "done"}
		return
	}

	for chunk := range streamCh {
		if chunk.Error != nil {
			ch <- Event{Type: "chunk", Content: fallbackContent}
			ch <- Event{Type: "done"}
			return
		}
		if chunk.Done {
			break
		}
		if chunk.Content != "" {
			ch <- Event{Type: "chunk", Content: chunk.Content}
		}
	}
	ch <- Event{Type: "done"}
}

func (a *CodeAgent) buildMessages(query string, history []ChatMessage) []llm.Message {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
	}

	for _, h := range history {
		switch h.Role {
		case "user":
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: h.Content})
		case "assistant":
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: h.Content})
		}
	}

	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: query})
	return messages
}

func toolDisplayName(name string) string {
	names := map[string]string{
		"semantic_search":      "语义搜索",
		"get_entity":           "获取实体信息",
		"get_callers":          "查找调用者",
		"get_callees":          "查找被调用函数",
		"get_subgraph":         "获取关系子图",
		"find_path":            "查找调用路径",
		"get_workspace_stats":  "获取工作区统计",
		"detect_cycles":        "检测循环依赖",
	}
	if dn, ok := names[name]; ok {
		return dn
	}
	return name
}
