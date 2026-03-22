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
// 2. 服务器发送连接确认和能力列表
// 3. 服务器通过心跳保持连接
// 4. 服务器推送 MCP 工具列表
func (s *Server) handleSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// 发送连接确认
	s.sendSSEEvent(c, SSEEvent{
		Event: "connected",
		Data: map[string]interface{}{
			"message": "MCP SSE 连接已建立",
			"version": "1.0.0",
			"capabilities": map[string]interface{}{
				"tools":  true,
				"search": true,
				"graph":  true,
			},
		},
	})

	// 发送可用工具列表
	s.sendSSEEvent(c, SSEEvent{
		Event: "tools",
		Data: map[string]interface{}{
			"tools": s.getMCPTools(),
		},
	})

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	clientGone := c.Request.Context().Done()

	for {
		select {
		case <-clientGone:
			s.log.Debug("SSE 客户端断开连接")
			return

		case <-ticker.C:
			s.sendSSEEvent(c, SSEEvent{
				Event: "heartbeat",
				Data:  map[string]interface{}{"timestamp": time.Now().Unix()},
			})
		}
	}
}

// getMCPTools 返回 MCP 可用工具列表
func (s *Server) getMCPTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "search_semantic",
			"description": "语义搜索代码实体",
			"parameters": map[string]interface{}{
				"query": "string - 搜索查询",
				"top_k": "int - 返回数量",
			},
		},
		{
			"name":        "get_entity",
			"description": "获取代码实体详情",
			"parameters": map[string]interface{}{
				"id": "string - 实体 ID",
			},
		},
		{
			"name":        "get_callchain",
			"description": "获取紧凑调用链文本（推荐：比 JSON 节省 95% token）。depth=1 返回一行摘要，depth>1 返回树形展开",
			"parameters": map[string]interface{}{
				"id":    "string - 实体 ID（QualifiedName）",
				"depth": "int - 遍历深度（默认 1）",
			},
		},
		{
			"name":        "get_graph_summary",
			"description": "获取完整调用图的紧凑文本摘要（推荐：一次请求了解全部调用关系）。按文件分组的邻接表",
			"parameters": map[string]interface{}{
				"file": "string - 可选，按文件路径过滤",
			},
		},
		{
			"name":        "get_callmap",
			"description": "获取调用关系图（JSON 详细格式，需要结构化数据时使用）",
			"parameters": map[string]interface{}{
				"id":    "string - 实体 ID",
				"depth": "int - 遍历深度",
			},
		},
		{
			"name":        "get_callers",
			"description": "获取调用者列表",
			"parameters": map[string]interface{}{
				"id":    "string - 实体 ID",
				"depth": "int - 遍历深度",
			},
		},
		{
			"name":        "get_callees",
			"description": "获取被调用者列表",
			"parameters": map[string]interface{}{
				"id":    "string - 实体 ID",
				"depth": "int - 遍历深度",
			},
		},
		{
			"name":        "get_function_graph",
			"description": "获取功能图谱",
			"parameters": map[string]interface{}{
				"type": "string - 节点类型过滤",
				"file": "string - 文件路径过滤",
			},
		},
		{
			"name":        "get_subgraph",
			"description": "获取以指定实体为中心的子图",
			"parameters": map[string]interface{}{
				"id":    "string - 中心实体 ID",
				"depth": "int - 遍历深度",
			},
		},
		{
			"name":        "find_path",
			"description": "查找两个实体之间的调用路径",
			"parameters": map[string]interface{}{
				"from": "string - 源实体 ID",
				"to":   "string - 目标实体 ID",
			},
		},
		{
			"name":        "detect_cycles",
			"description": "检测循环依赖",
			"parameters": map[string]interface{}{},
		},
		{
			"name":        "get_workspace",
			"description": "获取工作区统计信息",
			"parameters": map[string]interface{}{},
		},
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

// handleMCPRequest 处理 MCP 工具调用请求
// POST /mcp/request
func (s *Server) handleMCPRequest(c *gin.Context) {
	var msg MCPMessage
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(400, gin.H{"error": "无效的 MCP 请求: " + err.Error()})
		return
	}

	if msg.Type != "request" {
		c.JSON(400, gin.H{"error": "仅接受 request 类型消息"})
		return
	}

	ctx := c.Request.Context()
	var result interface{}
	var reqErr error

	switch msg.Method {
	case "search_semantic":
		query, _ := msg.Params["query"].(string)
		topK := 10
		if tk, ok := msg.Params["top_k"].(float64); ok {
			topK = int(tk)
		}
		result, reqErr = s.store.SemanticSearch(ctx, query, topK)

	case "get_entity":
		id, _ := msg.Params["id"].(string)
		result, reqErr = s.store.GetEntity(ctx, id)

	case "get_callchain":
		id, _ := msg.Params["id"].(string)
		depth := 1
		if d, ok := msg.Params["depth"].(float64); ok {
			depth = int(d)
		}
		result, reqErr = s.store.CallChainCompact(ctx, id, depth)

	case "get_graph_summary":
		file, _ := msg.Params["file"].(string)
		result, reqErr = s.store.CallGraphSummary(ctx, file)

	case "get_callmap":
		id, _ := msg.Params["id"].(string)
		depth := 1
		if d, ok := msg.Params["depth"].(float64); ok {
			depth = int(d)
		}
		callers, _ := s.store.GetCallersOf(ctx, id, depth)
		callees, _ := s.store.GetCalleesOf(ctx, id, depth)
		result = map[string]interface{}{"callers": callers, "callees": callees}

	case "get_workspace":
		result, reqErr = s.store.Stats(ctx)

	default:
		reqErr = fmt.Errorf("未知的 MCP 方法: %s", msg.Method)
	}

	if reqErr != nil {
		c.JSON(200, MCPMessage{
			Type: "response",
			ID:   msg.ID,
			Error: &MCPError{
				Code:    -1,
				Message: reqErr.Error(),
			},
		})
		return
	}

	c.JSON(200, MCPMessage{
		Type:   "response",
		ID:     msg.ID,
		Result: result,
	})
}
