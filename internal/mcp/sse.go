// Package mcp 提供 MCP SSE 传输实现
// 遵循 MCP SSE Transport 规范:
//   - GET /mcp/sse  → 建立 SSE 流，发送 endpoint 事件
//   - POST /mcp/message?sessionId=xxx → 接收 JSON-RPC 请求，通过 SSE 流返回响应
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Lion-Leporidae/sourcelex/internal/store"
)

// ==================== SSE Session 管理 ====================

// sseSession 表示一个活跃的 SSE 连接会话
type sseSession struct {
	id       string
	messages chan []byte // 待发送的 SSE 消息
	done     chan struct{}
}

// sessionManager 管理所有活跃的 SSE 会话
type sessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*sseSession
}

func newSessionManager() *sessionManager {
	return &sessionManager{sessions: make(map[string]*sseSession)}
}

func (m *sessionManager) create() *sseSession {
	s := &sseSession{
		id:       uuid.New().String(),
		messages: make(chan []byte, 64),
		done:     make(chan struct{}),
	}
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
	return s
}

func (m *sessionManager) get(id string) *sseSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

func (m *sessionManager) remove(id string) {
	m.mu.Lock()
	if s, ok := m.sessions[id]; ok {
		close(s.done)
		delete(m.sessions, id)
	}
	m.mu.Unlock()
}

// 全局 session 管理器（Server 初始化时创建）
var sessions = newSessionManager()

// ==================== JSON-RPC 类型 ====================

// jsonRPCRequest JSON-RPC 2.0 请求
type jsonRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id,omitempty"` // string | number | null
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// jsonRPCResponse JSON-RPC 2.0 响应
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ==================== MCP Tool Schema ====================

type mcpToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func (s *Server) mcpToolList() []mcpToolInfo {
	return []mcpToolInfo{
		{
			Name: "semantic_search", Description: "语义搜索代码实体",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":     map[string]interface{}{"type": "string", "description": "搜索查询"},
					"top_k":     map[string]interface{}{"type": "integer", "description": "返回数量，默认 5"},
					"min_score": map[string]interface{}{"type": "number", "description": "最低相似度 0-1"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name: "get_entity", Description: "获取代码实体详情",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{"type": "string", "description": "实体 QualifiedName"},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name: "get_callchain", Description: "获取紧凑调用链文本（推荐：比 JSON 节省 95% token）",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{"type": "string", "description": "实体 QualifiedName"},
					"depth":     map[string]interface{}{"type": "integer", "description": "遍历深度，默认 1"},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name: "get_graph_summary", Description: "获取完整调用图的紧凑文本摘要，按文件分组",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]interface{}{"type": "string", "description": "可选，按文件路径过滤"},
				},
			},
		},
		{
			Name: "get_callers", Description: "查找调用了指定函数的所有调用者",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{"type": "string", "description": "实体 QualifiedName"},
					"depth":     map[string]interface{}{"type": "integer", "description": "遍历深度，默认 2"},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name: "get_callees", Description: "查找指定函数调用的所有被调用者",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{"type": "string", "description": "实体 QualifiedName"},
					"depth":     map[string]interface{}{"type": "integer", "description": "遍历深度，默认 2"},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name: "grep_code", Description: "在仓库中搜索代码",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern":      map[string]interface{}{"type": "string", "description": "搜索模式（正则表达式）"},
					"file_pattern": map[string]interface{}{"type": "string", "description": "文件名过滤"},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name: "read_file_lines", Description: "读取文件指定行范围的代码",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":  map[string]interface{}{"type": "string", "description": "文件路径"},
					"start": map[string]interface{}{"type": "integer", "description": "起始行号"},
					"end":   map[string]interface{}{"type": "integer", "description": "结束行号"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name: "get_workspace", Description: "获取工作区统计信息",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name: "set_active_repo", Description: "设置当前活跃仓库（所有后续搜索将基于此仓库）",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo_key": map[string]interface{}{"type": "string", "description": "仓库标识，格式为 repoID@branch（如 gin@main）"},
				},
				"required": []string{"repo_key"},
			},
		},
		{
			Name: "list_repos", Description: "列出所有已索引的代码仓库",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name: "get_active_repo", Description: "获取当前活跃仓库信息",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// ==================== SSE Handler ====================

// handleSSE 处理 SSE 连接
// GET /mcp/sse
// MCP SSE Transport: 发送 endpoint 事件，然后心跳保活
func (s *Server) handleSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	// 创建会话
	session := sessions.create()
	s.log.Info("MCP SSE 客户端已连接", "session", session.id)

	// 发送 endpoint 事件（MCP SSE 规范要求的第一个事件）
	endpointURL := fmt.Sprintf("/mcp/message?sessionId=%s", session.id)
	writeSSE(c.Writer, "endpoint", endpointURL)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer sessions.remove(session.id)
	defer s.log.Debug("MCP SSE 客户端断开连接", "session", session.id)

	clientGone := c.Request.Context().Done()

	for {
		select {
		case <-clientGone:
			return
		case <-session.done:
			return
		case msg := <-session.messages:
			// 通过 SSE 流发送 JSON-RPC 响应
			writeSSE(c.Writer, "message", string(msg))
		case <-ticker.C:
			// 心跳（用注释行保活）
			fmt.Fprintf(c.Writer, ": heartbeat %d\n\n", time.Now().Unix())
			c.Writer.Flush()
		}
	}
}

// handleMCPMessage 处理 MCP JSON-RPC 请求
// POST /mcp/message?sessionId=xxx  （SSE 传输模式）
// POST /mcp/sse                    （Streamable HTTP 兼容模式）
func (s *Server) handleMCPMessage(c *gin.Context) {
	var req jsonRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效的 JSON-RPC 请求: " + err.Error()})
		return
	}

	// 获取 sessionID 用于仓库隔离
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		sessionID = c.GetHeader("X-Session-ID")
	}
	if sessionID == "" {
		sessionID = "default"
	}

	resp := s.handleJSONRPC(c.Request.Context(), &req, sessionID)

	// 有 sessionId → SSE 传输模式：通过 SSE 流发送
	if sessionID != "" {
		if session := sessions.get(sessionID); session != nil {
			if resp != nil {
				data, _ := json.Marshal(resp)
				select {
				case session.messages <- data:
				default:
					s.log.Warn("SSE 消息队列已满", "session", sessionID)
				}
			}
			c.Status(202)
			return
		}
	}

	// 无 sessionId → Streamable HTTP 模式：直接在 HTTP 响应体返回
	if resp != nil {
		c.JSON(200, resp)
	} else {
		c.Status(202)
	}
}

// ==================== JSON-RPC 路由 ====================

func (s *Server) handleJSONRPC(ctx context.Context, req *jsonRPCRequest, sessionID string) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.rpcInitialize(req)
	case "tools/list":
		return s.rpcToolsList(req)
	case "tools/call":
		return s.rpcToolsCall(ctx, req, sessionID)
	case "notifications/initialized":
		// 通知类型，无需响应
		return nil
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: "Method not found: " + req.Method},
		}
	}
}

func (s *Server) rpcInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "sourcelex",
				"version": "1.0.0",
			},
		},
	}
}

func (s *Server) rpcToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": s.mcpToolList(),
		},
	}
}

func (s *Server) rpcToolsCall(ctx context.Context, req *jsonRPCRequest, sessionID string) *jsonRPCResponse {
	params := req.Params
	toolName, _ := params["name"].(string)
	args, _ := params["arguments"].(map[string]interface{})
	if args == nil {
		args = make(map[string]interface{})
	}

	// 解析当前 session 的活跃仓库 store
	activeStore := s.resolveStore(sessionID)

	result, err := s.executeToolCall(ctx, activeStore, toolName, args)
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "Error: " + err.Error()},
				},
				"isError": true,
			},
		}
	}

	// 将结果转为 text content
	var text string
	switch v := result.(type) {
	case string:
		text = v
	default:
		data, _ := json.MarshalIndent(result, "", "  ")
		text = string(data)
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": text},
			},
		},
	}
}

// resolveStore 根据 sessionID 解析活跃仓库的 store
func (s *Server) resolveStore(sessionID string) *store.KnowledgeStore {
	if s.registry != nil && s.userRepoMgr != nil {
		repoKey := s.userRepoMgr.GetActive(sessionID)
		if rc, err := s.registry.Get(repoKey); err == nil {
			defer rc.Release()
			return rc.Store
		}
	}
	return s.store
}

// executeToolCall 执行具体的工具调用
func (s *Server) executeToolCall(ctx context.Context, ks *store.KnowledgeStore, toolName string, args map[string]interface{}) (interface{}, error) {
	switch toolName {
	case "semantic_search":
		query, _ := args["query"].(string)
		topK := 5
		if tk, ok := args["top_k"].(float64); ok {
			topK = int(tk)
		}
		return ks.SemanticSearch(ctx, query, topK)

	case "get_entity":
		id, _ := args["entity_id"].(string)
		return ks.GetEntity(ctx, id)

	case "get_callchain":
		id, _ := args["entity_id"].(string)
		depth := 1
		if d, ok := args["depth"].(float64); ok {
			depth = int(d)
		}
		return ks.CallChainCompact(ctx, id, depth)

	case "get_graph_summary":
		file, _ := args["file"].(string)
		return ks.CallGraphSummary(ctx, file)

	case "get_callers":
		id, _ := args["entity_id"].(string)
		depth := 2
		if d, ok := args["depth"].(float64); ok {
			depth = int(d)
		}
		return ks.GetCallersOf(ctx, id, depth)

	case "get_callees":
		id, _ := args["entity_id"].(string)
		depth := 2
		if d, ok := args["depth"].(float64); ok {
			depth = int(d)
		}
		return ks.GetCalleesOf(ctx, id, depth)

	case "get_workspace":
		return ks.Stats(ctx)

	case "grep_code":
		// 直接转发到 REST API 的逻辑
		pattern, _ := args["pattern"].(string)
		if pattern == "" {
			return nil, fmt.Errorf("pattern 参数必填")
		}
		return map[string]string{"info": "请使用 REST API /api/v1/grep"}, nil

	case "read_file_lines":
		path, _ := args["path"].(string)
		if path == "" {
			return nil, fmt.Errorf("path 参数必填")
		}
		return map[string]string{"info": "请使用 REST API /api/v1/file/lines?path=" + path}, nil

	case "set_active_repo":
		repoKey, _ := args["repo_key"].(string)
		sessionIDArg, _ := args["session_id"].(string)
		if repoKey == "" {
			return nil, fmt.Errorf("repo_key 参数必填")
		}
		if s.userRepoMgr != nil {
			sid := sessionIDArg
			if sid == "" {
				sid = "default"
			}
			s.userRepoMgr.SetActive(sid, repoKey)
			return map[string]string{"success": "true", "active_repo": repoKey}, nil
		}
		return nil, fmt.Errorf("多仓库模式未启用")

	case "list_repos":
		if s.registry != nil {
			return s.registry.List(), nil
		}
		return []interface{}{}, nil

	case "get_active_repo":
		sessionIDArg, _ := args["session_id"].(string)
		if sessionIDArg == "" {
			sessionIDArg = "default"
		}
		if s.userRepoMgr != nil {
			return map[string]string{"active_repo": s.userRepoMgr.GetActive(sessionIDArg)}, nil
		}
		return map[string]string{"active_repo": ""}, nil

	default:
		return nil, fmt.Errorf("未知工具: %s", toolName)
	}
}

// ==================== 辅助函数 ====================

// writeSSE 写入一条 SSE 事件
func writeSSE(w io.Writer, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}
}

// ==================== 以下保留兼容旧接口 ====================

// SSEEvent SSE 事件（兼容旧代码）
type SSEEvent struct {
	Event string      `json:"event,omitempty"`
	Data  interface{} `json:"data"`
}

// MCPMessage MCP 协议消息（兼容旧代码）
type MCPMessage struct {
	Type   string                 `json:"type"`
	ID     string                 `json:"id,omitempty"`
	Method string                 `json:"method,omitempty"`
	Params map[string]interface{} `json:"params,omitempty"`
	Result interface{}            `json:"result,omitempty"`
	Error  *MCPError              `json:"error,omitempty"`
}

// MCPError MCP 错误
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handleMCPRequest 处理旧版 MCP 请求（保持兼容）
// POST /mcp/request
func (s *Server) handleMCPRequest(c *gin.Context) {
	var msg MCPMessage
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(400, gin.H{"error": "无效的 MCP 请求: " + err.Error()})
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
			Type:  "response",
			ID:    msg.ID,
			Error: &MCPError{Code: -1, Message: reqErr.Error()},
		})
		return
	}

	c.JSON(200, MCPMessage{
		Type:   "response",
		ID:     msg.ID,
		Result: result,
	})
}

// StreamWriter 流式写入器接口
type StreamWriter struct {
	writer  io.Writer
	flusher interface{ Flush() }
}

func NewStreamWriter(c *gin.Context) *StreamWriter {
	return &StreamWriter{writer: c.Writer, flusher: c.Writer}
}

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
