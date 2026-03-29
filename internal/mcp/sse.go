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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
			Name:        "search",
			Description: "在代码知识库中搜索。支持语义搜索（自然语言描述）和精确查找（entity_id）。返回实体详情（类型、文件、行号、签名）及调用关系摘要。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":     map[string]interface{}{"type": "string", "description": "自然语言搜索查询，或实体的 QualifiedName（如 store.SemanticSearch）"},
					"top_k":     map[string]interface{}{"type": "integer", "description": "返回数量，默认 5"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_callchain",
			Description: "获取代码实体的调用关系。返回紧凑文本格式的调用链（调用者 ← 实体 → 被调用者）。也可传 file 参数获取整个文件/仓库的调用图摘要。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"entity_id": map[string]interface{}{"type": "string", "description": "实体 QualifiedName（如 graph.NewSQLiteStore）"},
					"file":      map[string]interface{}{"type": "string", "description": "按文件过滤调用图（传此参数时 entity_id 可省略，返回文件级调用摘要）"},
					"depth":     map[string]interface{}{"type": "integer", "description": "遍历深度，默认 1（推荐 1-2）"},
				},
			},
		},
		{
			Name:        "read_code",
			Description: "读取仓库中的代码。支持按文件路径读取指定行范围，或用正则表达式搜索代码内容。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":         map[string]interface{}{"type": "string", "description": "文件路径（相对于仓库根目录）"},
					"start":        map[string]interface{}{"type": "integer", "description": "起始行号（配合 path 使用）"},
					"end":          map[string]interface{}{"type": "integer", "description": "结束行号（配合 path 使用）"},
					"grep":         map[string]interface{}{"type": "string", "description": "正则搜索模式（不传 path 时使用 grep 搜索整个仓库）"},
					"file_pattern": map[string]interface{}{"type": "string", "description": "grep 时的文件名过滤（如 *.go）"},
				},
			},
		},
		{
			Name:        "manage_repo",
			Description: "管理代码仓库。查看已索引仓库列表、切换活跃仓库、查看工作区统计。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":   map[string]interface{}{"type": "string", "description": "操作: list（列出仓库）、switch（切换仓库）、status（当前状态）", "enum": []string{"list", "switch", "status"}},
					"repo_key": map[string]interface{}{"type": "string", "description": "仓库标识（switch 时必填），格式: repoID@branch"},
				},
				"required": []string{"action"},
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

	result, err := s.executeToolCall(ctx, activeStore, toolName, args, sessionID)
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

// resolveRepoPath 根据 sessionID 解析活跃仓库的路径
func (s *Server) resolveRepoPath(sessionID string) string {
	if s.registry != nil && s.userRepoMgr != nil {
		repoKey := s.userRepoMgr.GetActive(sessionID)
		if rc, err := s.registry.Get(repoKey); err == nil {
			defer rc.Release()
			return rc.RepoPath
		}
	}
	return s.repoPath
}

// executeToolCall 执行具体的工具调用
func (s *Server) executeToolCall(ctx context.Context, ks *store.KnowledgeStore, toolName string, args map[string]interface{}, sessionID string) (interface{}, error) {
	switch toolName {

	// ========== search: 语义搜索 + 实体查找 ==========
	case "search", "semantic_search", "get_entity":
		query, _ := args["query"].(string)
		entityID, _ := args["entity_id"].(string)

		// 如果 query 像是 QualifiedName（含 .），先尝试精确查找
		if query != "" && strings.Contains(query, ".") {
			if node, err := ks.GetEntity(ctx, query); err == nil {
				// 精确命中，附加调用链摘要
				chain, _ := ks.CallChainCompact(ctx, query, 1)
				return fmt.Sprintf("[精确匹配] %s (%s)\n文件: %s:%d-%d\n签名: %s\n\n%s",
					node.Name, string(node.Type), node.FilePath, node.StartLine, node.EndLine,
					node.Signature, chain), nil
			}
		}
		if entityID != "" {
			if node, err := ks.GetEntity(ctx, entityID); err == nil {
				chain, _ := ks.CallChainCompact(ctx, entityID, 1)
				return fmt.Sprintf("[精确匹配] %s (%s)\n文件: %s:%d-%d\n签名: %s\n\n%s",
					node.Name, string(node.Type), node.FilePath, node.StartLine, node.EndLine,
					node.Signature, chain), nil
			}
		}

		// 语义搜索
		if query == "" {
			query = entityID
		}
		if query == "" {
			return nil, fmt.Errorf("query 参数必填")
		}
		topK := 5
		if tk, ok := args["top_k"].(float64); ok {
			topK = int(tk)
		}
		results, err := ks.SemanticSearch(ctx, query, topK)
		if err != nil {
			return nil, err
		}

		// 格式化为紧凑文本
		var b strings.Builder
		b.WriteString(fmt.Sprintf("搜索 \"%s\" 找到 %d 个结果:\n\n", query, len(results)))
		for i, r := range results {
			b.WriteString(fmt.Sprintf("%d. %s (%.0f%%)\n   %s\n\n", i+1, r.EntityID, r.Score*100, r.Content))
		}
		return b.String(), nil

	// ========== get_callchain: 调用关系统一入口 ==========
	case "get_callchain", "get_callers", "get_callees", "get_graph_summary":
		entityID, _ := args["entity_id"].(string)
		file, _ := args["file"].(string)
		depth := 1
		if d, ok := args["depth"].(float64); ok {
			depth = int(d)
		}

		// 如果传了 file（无 entity_id），返回文件/全局调用图摘要
		if entityID == "" || file != "" {
			return ks.CallGraphSummary(ctx, file)
		}

		// 否则返回实体调用链
		return ks.CallChainCompact(ctx, entityID, depth)

	// ========== read_code: 读文件 + grep ==========
	case "read_code", "grep_code", "read_file_lines":
		filePath, _ := args["path"].(string)
		grepPattern, _ := args["grep"].(string)
		filePattern, _ := args["file_pattern"].(string)

		repoPath := s.resolveRepoPath(sessionID)
		if repoPath == "" {
			return nil, fmt.Errorf("仓库路径未配置")
		}

		// grep 模式
		if grepPattern != "" {
			return s.doGrep(ctx, repoPath, grepPattern, filePattern)
		}

		// 读文件模式
		if filePath != "" {
			start := 1
			end := 0
			if v, ok := args["start"].(float64); ok {
				start = int(v)
			}
			if v, ok := args["end"].(float64); ok {
				end = int(v)
			}
			return s.doReadFile(repoPath, filePath, start, end)
		}

		// 兼容旧版 pattern 参数
		pattern, _ := args["pattern"].(string)
		if pattern != "" {
			return s.doGrep(ctx, repoPath, pattern, filePattern)
		}

		return nil, fmt.Errorf("需要 path（读文件）或 grep（搜索代码）参数")

	// ========== manage_repo: 仓库管理 ==========
	case "manage_repo", "list_repos", "set_active_repo", "get_active_repo", "get_workspace":
		action, _ := args["action"].(string)

		// 兼容旧工具名
		if action == "" {
			switch toolName {
			case "list_repos":
				action = "list"
			case "set_active_repo":
				action = "switch"
			case "get_active_repo", "get_workspace":
				action = "status"
			}
		}

		switch action {
		case "list":
			if s.registry != nil {
				repos := s.registry.List()
				var b strings.Builder
				b.WriteString(fmt.Sprintf("已索引 %d 个仓库:\n\n", len(repos)))
				for _, r := range repos {
					key := r.RepoID + "@" + r.Branch
					source := r.RepoURL
					if source == "" {
						source = r.RepoPath
					}
					b.WriteString(fmt.Sprintf("  %s  (%s)  索引于 %s\n", key, source, r.IndexedAt.Format("2006-01-02 15:04")))
				}
				return b.String(), nil
			}
			return "单仓库模式，无仓库列表", nil

		case "switch":
			repoKey, _ := args["repo_key"].(string)
			if repoKey == "" {
				return nil, fmt.Errorf("repo_key 参数必填")
			}
			if s.userRepoMgr != nil {
				if s.registry != nil {
					if _, err := s.registry.Get(repoKey); err != nil {
						return nil, fmt.Errorf("仓库不存在: %s", repoKey)
					}
				}
				s.userRepoMgr.SetActive(sessionID, repoKey)
				return fmt.Sprintf("已切换到仓库: %s", repoKey), nil
			}
			return nil, fmt.Errorf("多仓库模式未启用")

		case "status":
			var b strings.Builder
			stats, _ := ks.Stats(ctx)
			if stats != nil {
				b.WriteString(fmt.Sprintf("实体: %d  调用关系: %d  向量: %d\n", stats.NodeCount, stats.EdgeCount, stats.VectorCount))
			}
			if s.userRepoMgr != nil {
				b.WriteString(fmt.Sprintf("活跃仓库: %s\n", s.userRepoMgr.GetActive(sessionID)))
			}
			return b.String(), nil

		default:
			return nil, fmt.Errorf("未知 action: %s（可选: list, switch, status）", action)
		}

	default:
		return nil, fmt.Errorf("未知工具: %s", toolName)
	}
}

// doGrep 在仓库中执行 grep 搜索
func (s *Server) doGrep(ctx context.Context, repoPath, pattern, filePattern string) (string, error) {
	args := []string{"-rn", "--color=never", "-E", "-m", "30"}
	if filePattern != "" {
		args = append(args, "--include="+filePattern)
	}
	args = append(args, "--exclude-dir=.git", "--exclude-dir=node_modules", "--exclude-dir=vendor")
	args = append(args, pattern, ".")

	cmd := exec.CommandContext(ctx, "grep", args...)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "未找到匹配", nil
		}
		return "", fmt.Errorf("grep 执行失败: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) > 30 {
		lines = lines[:30]
		lines = append(lines, fmt.Sprintf("... 截断（共 30+ 匹配）"))
	}
	return strings.Join(lines, "\n"), nil
}

// doReadFile 读取文件指定行
func (s *Server) doReadFile(repoPath, filePath string, start, end int) (string, error) {
	absPath := filepath.Join(repoPath, filePath)
	// 安全检查
	if !strings.HasPrefix(absPath, repoPath) {
		return "", fmt.Errorf("无效路径")
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	if start < 1 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return fmt.Sprintf("文件共 %d 行，起始行 %d 超出范围", len(lines), start), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("// %s:%d-%d (共 %d 行)\n", filePath, start, end, len(lines)))
	for i := start - 1; i < end && i < len(lines); i++ {
		b.WriteString(fmt.Sprintf("%4d: %s\n", i+1, lines[i]))
	}
	return b.String(), nil
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
