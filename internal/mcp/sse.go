// Package mcp 提供 SSE (Server-Sent Events) 支持
// 对应架构文档: MCP服务暴露层 - SSE 通信
//
// SSE 是一种服务器推送技术，允许服务器向客户端发送事件流
// MCP 协议使用 SSE 进行实时通信
//
// 协议格式:
// event: message
// data: {"type": "result", "content": "..."}
package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"
)

// SSEEvent SSE 事件
type SSEEvent struct {
	// Event 事件类型
	Event string `json:"event,omitempty"`

	// Data 事件数据
	Data interface{} `json:"data"`
}

// MCPMessage MCP 协议消息
type MCPMessage struct {
	// Type 消息类型: request, response, notification
	Type string `json:"type"`

	// ID 请求 ID（用于匹配请求和响应）
	ID string `json:"id,omitempty"`

	// Method 方法名（对于 request 类型）
	Method string `json:"method,omitempty"`

	// Params 参数（对于 request 类型）
	Params map[string]interface{} `json:"params,omitempty"`

	// Result 结果（对于 response 类型）
	Result interface{} `json:"result,omitempty"`

	// Error 错误信息
	Error *MCPError `json:"error,omitempty"`
}

// MCPError MCP 错误
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handleSSE 处理 SSE 连接
// GET /mcp/sse
//
// SSE 连接流程:
// 1. 客户端建立 SSE 连接
// 2. 服务器发送心跳保持连接
// 3. 客户端通过其他端点发送请求
// 4. 服务器通过 SSE 推送响应
func (s *Server) handleSSE(c *gin.Context) {
	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// 发送初始化消息
	s.sendSSEEvent(c, SSEEvent{
		Event: "connected",
		Data: map[string]interface{}{
			"message": "MCP SSE 连接已建立",
			"version": "1.0.0",
		},
	})

	// 创建心跳定时器
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// 监听客户端断开
	clientGone := c.Request.Context().Done()

	for {
		select {
		case <-clientGone:
			s.log.Debug("SSE 客户端断开连接")
			return

		case <-ticker.C:
			// 发送心跳
			s.sendSSEEvent(c, SSEEvent{
				Event: "heartbeat",
				Data:  map[string]interface{}{"timestamp": time.Now().Unix()},
			})
		}
	}
}

// sendSSEEvent 发送 SSE 事件
// SSE 格式:
// event: <event_name>
// data: <json_data>
func (s *Server) sendSSEEvent(c *gin.Context, event SSEEvent) {
	data, err := json.Marshal(event.Data)
	if err != nil {
		s.log.Error("SSE JSON 序列化失败", "error", err)
		return
	}

	// 写入事件类型（如果有）
	if event.Event != "" {
		fmt.Fprintf(c.Writer, "event: %s\n", event.Event)
	}

	// 写入数据
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)

	// 刷新输出
	c.Writer.Flush()
}

// sendMCPResponse 发送 MCP 响应
func (s *Server) sendMCPResponse(c *gin.Context, id string, result interface{}) {
	msg := MCPMessage{
		Type:   "response",
		ID:     id,
		Result: result,
	}

	data, _ := json.Marshal(msg)
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	c.Writer.Flush()
}

// sendMCPError 发送 MCP 错误
func (s *Server) sendMCPError(c *gin.Context, id string, code int, message string) {
	msg := MCPMessage{
		Type: "response",
		ID:   id,
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	}

	data, _ := json.Marshal(msg)
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	c.Writer.Flush()
}

// StreamWriter 流式写入器接口
// 用于流式返回大结果
type StreamWriter struct {
	writer  io.Writer
	flusher interface{ Flush() }
}

// NewStreamWriter 创建流式写入器
func NewStreamWriter(c *gin.Context) *StreamWriter {
	return &StreamWriter{
		writer:  c.Writer,
		flusher: c.Writer,
	}
}

// WriteEvent 写入事件
func (sw *StreamWriter) WriteEvent(event string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if event != "" {
		fmt.Fprintf(sw.writer, "event: %s\n", event)
	}
	fmt.Fprintf(sw.writer, "data: %s\n\n", jsonData)
	sw.flusher.Flush()

	return nil
}
