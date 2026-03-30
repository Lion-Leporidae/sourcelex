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
			Description: "搜索代码知识库。输入自然语言或函数名，返回匹配的实体详情和调用关系。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "搜索内容：自然语言描述 或 函数限定名（如 store.SemanticSearch）"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "read_code",
			Description: "读取仓库中的源代码文件。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "文件路径（如 internal/mcp/server.go 或 internal/mcp/server.go:10-50）"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "switch_repo",
			Description: "切换活跃仓库。不传参数时列出所有可用仓库。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"repo": map[string]interface{}{"type": "string", "description": "仓库标识（如 gin@main）。不传则列出所有仓库。"},
				},
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

// executeToolCall 执行工具调用
func (s *Server) executeToolCall(ctx context.Context, ks *store.KnowledgeStore, toolName string, args map[string]interface{}, sessionID string) (interface{}, error) {
	switch toolName {

	// ========== search ==========
	// 输入任意文本，自动尝试：精确查找 → 语义搜索
	// 每个结果自动附带调用关系
	case "search", "semantic_search", "get_entity", "get_callchain",
		"get_callers", "get_callees", "get_graph_summary":
		query, _ := args["query"].(string)
		if query == "" {
			// 兼容旧参数名
			query, _ = args["entity_id"].(string)
		}
		if query == "" {
			query, _ = args["file"].(string)
			if query != "" {
				// file 参数 → 返回文件级调用图
				return ks.CallGraphSummary(ctx, query)
			}
			// 无参数 → 全局调用图
			return ks.CallGraphSummary(ctx, "")
		}

		// 1) 先尝试精确查找实体
		if node, err := ks.GetEntity(ctx, query); err == nil {
			chain, _ := ks.CallChainCompact(ctx, query, 2)
			return fmt.Sprintf("%s (%s)\n文件: %s:%d-%d\n签名: %s\n\n%s",
				node.Name, string(node.Type), node.FilePath, node.StartLine, node.EndLine,
				node.Signature, chain), nil
		}

		// 2) 语义搜索
		results, err := ks.SemanticSearch(ctx, query, 5)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return "未找到匹配的代码实体", nil
		}

		var b strings.Builder
		for i, r := range results {
			b.WriteString(fmt.Sprintf("%d. %s (%.0f%%)\n   %s\n", i+1, r.EntityID, r.Score*100, r.Content))
			// 第一个结果自动附带调用链
			if i == 0 {
				if chain, err := ks.CallChainCompact(ctx, r.EntityID, 1); err == nil && chain != "" {
					b.WriteString("   " + strings.ReplaceAll(strings.TrimSpace(chain), "\n", "\n   ") + "\n")
				}
			}
			b.WriteString("\n")
		}
		return b.String(), nil

	// ========== read_code ==========
	// 输入路径，自动读取。支持 "file.go:10-50" 格式
	case "read_code", "grep_code", "read_file_lines":
		pathArg, _ := args["path"].(string)
		if pathArg == "" {
			// 兼容旧参数
			pathArg, _ = args["pattern"].(string)
			if pathArg != "" {
				repoPath := s.resolveRepoPath(sessionID)
				if repoPath == "" {
					return nil, fmt.Errorf("仓库路径未配置")
				}
				filePattern, _ := args["file_pattern"].(string)
				return s.doGrep(ctx, repoPath, pathArg, filePattern)
			}
			return nil, fmt.Errorf("path 参数必填")
		}

		repoPath := s.resolveRepoPath(sessionID)
		if repoPath == "" {
			return nil, fmt.Errorf("仓库路径未配置")
		}

		// 解析 "file.go:10-50" 格式
		filePath, start, end := parsePath(pathArg)
		return s.doReadFile(repoPath, filePath, start, end)

	// ========== switch_repo ==========
	// 传 repo → 切换；不传 → 列出所有 + 当前状态
	case "switch_repo", "manage_repo", "list_repos", "set_active_repo",
		"get_active_repo", "get_workspace":
		repoKey, _ := args["repo"].(string)
		if repoKey == "" {
			repoKey, _ = args["repo_key"].(string)
		}
		if repoKey == "" {
			// 兼容旧 action 参数
			action, _ := args["action"].(string)
			if action == "switch" {
				repoKey, _ = args["repo_key"].(string)
			}
		}

		// 有 repo → 切换
		if repoKey != "" {
			if s.userRepoMgr == nil {
				return nil, fmt.Errorf("多仓库模式未启用")
			}
			if s.registry != nil {
				if _, err := s.registry.Get(repoKey); err != nil {
					return nil, fmt.Errorf("仓库不存在: %s", repoKey)
				}
			}
			s.userRepoMgr.SetActive(sessionID, repoKey)
			return fmt.Sprintf("已切换到仓库: %s", repoKey), nil
		}

		// 无 repo → 列出所有 + 当前状态
		var b strings.Builder
		if s.userRepoMgr != nil {
			b.WriteString(fmt.Sprintf("当前仓库: %s\n\n", s.userRepoMgr.GetActive(sessionID)))
		}
		stats, _ := ks.Stats(ctx)
		if stats != nil {
			b.WriteString(fmt.Sprintf("实体: %d  调用关系: %d  向量: %d\n\n", stats.NodeCount, stats.EdgeCount, stats.VectorCount))
		}
		if s.registry != nil {
			repos := s.registry.List()
			b.WriteString(fmt.Sprintf("可用仓库 (%d):\n", len(repos)))
			for _, r := range repos {
				key := r.RepoID + "@" + r.Branch
				source := r.RepoURL
				if source == "" {
					source = r.RepoPath
				}
				b.WriteString(fmt.Sprintf("  %s  %s\n", key, source))
			}
		}
		return b.String(), nil

	default:
		return nil, fmt.Errorf("未知工具: %s", toolName)
	}
}

// parsePath 解析 "file.go:10-50" → (file.go, 10, 50)
func parsePath(path string) (string, int, int) {
	// 尝试匹配 file:start-end
	if idx := strings.LastIndex(path, ":"); idx > 0 {
		lineSpec := path[idx+1:]
		filePath := path[:idx]
		parts := strings.SplitN(lineSpec, "-", 2)
		start := 1
		end := 0
		if v, err := fmt.Sscanf(parts[0], "%d", &start); err == nil && v > 0 {
			if len(parts) == 2 {
				fmt.Sscanf(parts[1], "%d", &end)
			}
			return filePath, start, end
		}
	}
	return path, 1, 0
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
