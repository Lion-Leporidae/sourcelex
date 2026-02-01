// Package mcp 提供 MCP API 处理器
// 实现各种 MCP 工具的 HTTP 端点
package mcp

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ========== 请求/响应结构体 ==========

// SemanticSearchRequest 语义搜索请求
// 对应架构: search_by_semantic()
type SemanticSearchRequest struct {
	// Query 自然语言查询
	Query string `json:"query" binding:"required"`

	// TopK 返回结果数量
	TopK int `json:"top_k,omitempty"`

	// Filter 过滤条件
	Filter map[string]interface{} `json:"filter,omitempty"`
}

// SearchResult 搜索结果项
type SearchResult struct {
	EntityID  string                 `json:"entity_id"`
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`
	FilePath  string                 `json:"file_path"`
	StartLine int                    `json:"start_line"`
	EndLine   int                    `json:"end_line"`
	Signature string                 `json:"signature,omitempty"`
	Score     float32                `json:"score"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// EntityInfo 实体详细信息
// 对应架构: get_entity_info()
type EntityInfo struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	FilePath   string                 `json:"file_path"`
	StartLine  int                    `json:"start_line"`
	EndLine    int                    `json:"end_line"`
	Signature  string                 `json:"signature,omitempty"`
	DocComment string                 `json:"doc_comment,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// CallMapResponse 调用关系响应
// 对应架构: get_call_map()
type CallMapResponse struct {
	EntityID string       `json:"entity_id"`
	Callers  []EntityInfo `json:"callers"` // 谁调用了它
	Callees  []EntityInfo `json:"callees"` // 它调用了谁
}

// WorkspaceInfo 工作区信息
// 对应架构: get_workspace_info()
type WorkspaceInfo struct {
	VectorCount int64 `json:"vector_count"`
	NodeCount   int64 `json:"node_count"`
	EdgeCount   int64 `json:"edge_count"`
}

// APIResponse 通用 API 响应
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// ========== 处理器方法 ==========

// handleHealth 健康检查
// GET /health
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "repomind-mcp",
	})
}

// handleGetWorkspace 获取工作区信息
// GET /api/v1/workspace
// 对应架构: get_workspace_info()
func (s *Server) handleGetWorkspace(c *gin.Context) {
	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	stats, err := s.store.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: WorkspaceInfo{
			VectorCount: stats.VectorCount,
			NodeCount:   stats.NodeCount,
			EdgeCount:   stats.EdgeCount,
		},
	})
}

// handleSemanticSearch 语义搜索
// POST /api/v1/search/semantic
// 对应架构: search_by_semantic()
//
// 请求体:
//
//	{
//	  "query": "计算两数之和的函数",
//	  "top_k": 10
//	}
func (s *Server) handleSemanticSearch(c *gin.Context) {
	var req SemanticSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "无效的请求参数: " + err.Error(),
		})
		return
	}

	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	// 设置默认值
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}

	// 执行语义搜索
	results, err := s.store.SemanticSearch(c.Request.Context(), req.Query, topK)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// 转换结果
	searchResults := make([]SearchResult, len(results))
	for i, r := range results {
		searchResults[i] = SearchResult{
			EntityID: r.EntityID,
			Score:    r.Score,
			Metadata: r.Metadata,
		}
		// 从元数据提取字段
		if name, ok := r.Metadata["name"].(string); ok {
			searchResults[i].Name = name
		}
		if t, ok := r.Metadata["type"].(string); ok {
			searchResults[i].Type = t
		}
		if fp, ok := r.Metadata["file_path"].(string); ok {
			searchResults[i].FilePath = fp
		}
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data:    searchResults,
	})
}

// handleGetEntity 获取实体信息
// GET /api/v1/entity/:id
// 对应架构: get_entity_info()
func (s *Server) handleGetEntity(c *gin.Context) {
	entityID := c.Param("id")
	if entityID == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "实体 ID 不能为空",
		})
		return
	}

	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	node, err := s.store.GetEntity(c.Request.Context(), entityID)
	if err != nil {
		c.JSON(http.StatusNotFound, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: EntityInfo{
			ID:        node.ID,
			Name:      node.Name,
			Type:      string(node.Type),
			FilePath:  node.FilePath,
			StartLine: node.StartLine,
			EndLine:   node.EndLine,
			Signature: node.Signature,
		},
	})
}

// handleGetCallMap 获取调用关系图
// GET /api/v1/callmap/:id?depth=2
// 对应架构: get_call_map()
func (s *Server) handleGetCallMap(c *gin.Context) {
	entityID := c.Param("id")
	depthStr := c.DefaultQuery("depth", "1")
	depth, _ := strconv.Atoi(depthStr)
	if depth <= 0 {
		depth = 1
	}

	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	// 获取调用者
	callers, err := s.store.GetCallersOf(c.Request.Context(), entityID, depth)
	if err != nil {
		s.log.Debug("获取调用者失败", "error", err)
	}

	// 获取被调用者
	callees, err := s.store.GetCalleesOf(c.Request.Context(), entityID, depth)
	if err != nil {
		s.log.Debug("获取被调用者失败", "error", err)
	}

	// 转换结果
	callerInfos := make([]EntityInfo, len(callers))
	for i, n := range callers {
		callerInfos[i] = EntityInfo{
			ID:        n.ID,
			Name:      n.Name,
			Type:      string(n.Type),
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
			Signature: n.Signature,
		}
	}

	calleeInfos := make([]EntityInfo, len(callees))
	for i, n := range callees {
		calleeInfos[i] = EntityInfo{
			ID:        n.ID,
			Name:      n.Name,
			Type:      string(n.Type),
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
			Signature: n.Signature,
		}
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: CallMapResponse{
			EntityID: entityID,
			Callers:  callerInfos,
			Callees:  calleeInfos,
		},
	})
}

// handleGetCallers 获取调用者
// GET /api/v1/callers/:id?depth=2
func (s *Server) handleGetCallers(c *gin.Context) {
	entityID := c.Param("id")
	depthStr := c.DefaultQuery("depth", "1")
	depth, _ := strconv.Atoi(depthStr)
	if depth <= 0 {
		depth = 1
	}

	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	callers, err := s.store.GetCallersOf(c.Request.Context(), entityID, depth)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	infos := make([]EntityInfo, len(callers))
	for i, n := range callers {
		infos[i] = EntityInfo{
			ID:        n.ID,
			Name:      n.Name,
			Type:      string(n.Type),
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
			Signature: n.Signature,
		}
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data:    infos,
	})
}

// handleGetCallees 获取被调用者
// GET /api/v1/callees/:id?depth=2
func (s *Server) handleGetCallees(c *gin.Context) {
	entityID := c.Param("id")
	depthStr := c.DefaultQuery("depth", "1")
	depth, _ := strconv.Atoi(depthStr)
	if depth <= 0 {
		depth = 1
	}

	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	callees, err := s.store.GetCalleesOf(c.Request.Context(), entityID, depth)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	infos := make([]EntityInfo, len(callees))
	for i, n := range callees {
		infos[i] = EntityInfo{
			ID:        n.ID,
			Name:      n.Name,
			Type:      string(n.Type),
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
			Signature: n.Signature,
		}
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data:    infos,
	})
}
