// Package mcp 提供 MCP API 处理器
// 实现各种 MCP 工具的 HTTP 端点
package mcp

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	repogit "github.com/Lion-Leporidae/sourcelex/internal/git"
	"github.com/Lion-Leporidae/sourcelex/internal/store"
	"github.com/Lion-Leporidae/sourcelex/internal/store/graph"
)

// ========== 请求/响应结构体 ==========

// SemanticSearchRequest 语义搜索请求
type SemanticSearchRequest struct {
	// Query 自然语言查询
	Query string `json:"query" binding:"required"`

	// TopK 返回结果数量
	TopK int `json:"top_k,omitempty"`

	// MinScore 最低置信度阈值 (0-1)，低于此值的结果不返回
	MinScore float64 `json:"min_score,omitempty"`

	// Filter 过滤条件
	Filter map[string]interface{} `json:"filter,omitempty"`
}

// HybridSearchRequest 混合搜索请求
type HybridSearchRequest struct {
	Query   string                 `json:"query" binding:"required"`
	TopK    int                    `json:"top_k,omitempty"`
	Filters map[string]interface{} `json:"filters,omitempty"`
}

// RAGContextRequest RAG 上下文请求
type RAGContextRequest struct {
	Query            string                 `json:"query" binding:"required"`
	TopK             int                    `json:"top_k,omitempty"`
	MinScore         float32                `json:"min_score,omitempty"`
	IncludeCallGraph bool                   `json:"include_call_graph,omitempty"`
	CallGraphDepth   int                    `json:"call_graph_depth,omitempty"`
	IncludeFileCtx   bool                   `json:"include_file_context,omitempty"`
	EnableReranking  bool                   `json:"enable_reranking,omitempty"`
	Filters          map[string]interface{} `json:"filters,omitempty"`
	MaxContextLength int                    `json:"max_context_length,omitempty"`
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

// FunctionGraphResponse 功能图谱响应
type FunctionGraphResponse struct {
	Nodes []EntityInfo `json:"nodes"`
	Edges []EdgeInfo   `json:"edges"`
	Stats GraphStats   `json:"stats"`
}

// EdgeInfo 边信息
type EdgeInfo struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// GraphStats 图统计信息
type GraphStats struct {
	NodeCount  int `json:"node_count"`
	EdgeCount  int `json:"edge_count"`
	CycleCount int `json:"cycle_count,omitempty"`
}

// SubgraphResponse 子图响应
type SubgraphResponse struct {
	CenterID string       `json:"center_id"`
	Depth    int          `json:"depth"`
	Nodes    []EntityInfo `json:"nodes"`
	Edges    []EdgeInfo   `json:"edges"`
}

// CallChainResponse 紧凑调用链响应
type CallChainResponse struct {
	EntityID string `json:"entity_id"`
	Depth    int    `json:"depth"`
	Text     string `json:"text"`
}

// GraphSummaryResponse 调用图摘要响应
type GraphSummaryResponse struct {
	Text      string `json:"text"`
	NodeCount int    `json:"node_count"`
	EdgeCount int    `json:"edge_count"`
}

// PathResponse 路径响应
type PathResponse struct {
	Source string     `json:"source"`
	Target string     `json:"target"`
	Path   []string   `json:"path"`
	Edges  []EdgeInfo `json:"edges"`
}

// ========== 处理器方法 ==========

// handleHealth 健康检查
// GET /health
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "sourcelex-mcp",
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

	// 转换结果（按置信度过滤）
	var searchResults []SearchResult
	for _, r := range results {
		if req.MinScore > 0 && float64(r.Score) < req.MinScore {
			continue
		}
		sr := SearchResult{
			EntityID: r.EntityID,
			Score:    r.Score,
			Metadata: r.Metadata,
		}
		// 从元数据提取字段
		if name, ok := r.Metadata["name"].(string); ok {
			sr.Name = name
		}
		if t, ok := r.Metadata["type"].(string); ok {
			sr.Type = t
		}
		if fp, ok := r.Metadata["file_path"].(string); ok {
			sr.FilePath = fp
		}
		searchResults = append(searchResults, sr)
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
	callers, callerErr := s.store.GetCallersOf(c.Request.Context(), entityID, depth)
	if callerErr != nil {
		s.log.Debug("获取调用者失败", "error", callerErr)
	}

	// 获取被调用者
	callees, calleeErr := s.store.GetCalleesOf(c.Request.Context(), entityID, depth)
	if calleeErr != nil {
		s.log.Debug("获取被调用者失败", "error", calleeErr)
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

// handleGetFunctionGraph 获取完整功能图谱
// GET /api/v1/graph/function?type=function&file=xxx
func (s *Server) handleGetFunctionGraph(c *gin.Context) {
	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	nodeType := c.Query("type")
	filePath := c.Query("file")

	var nodes []graph.Node
	var err error

	if filePath != "" {
		nodes, err = s.store.GetNodesByFile(c.Request.Context(), filePath)
	} else if nodeType != "" {
		nodes, err = s.store.GetNodesByType(c.Request.Context(), graph.NodeType(nodeType))
	} else {
		nodes, err = s.store.GetAllNodes(c.Request.Context())
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	edges, err := s.store.GetAllEdges(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	nodeSet := make(map[string]bool)
	for _, n := range nodes {
		nodeSet[n.ID] = true
	}

	var filteredEdges []EdgeInfo
	for _, e := range edges {
		if nodeSet[e.Source] && nodeSet[e.Target] {
			filteredEdges = append(filteredEdges, EdgeInfo{
				Source: e.Source,
				Target: e.Target,
				Type:   string(e.Type),
			})
		}
	}

	nodeInfos := make([]EntityInfo, len(nodes))
	for i, n := range nodes {
		nodeInfos[i] = EntityInfo{
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
		Data: FunctionGraphResponse{
			Nodes: nodeInfos,
			Edges: filteredEdges,
			Stats: GraphStats{
				NodeCount: len(nodeInfos),
				EdgeCount: len(filteredEdges),
			},
		},
	})
}

// handleGetSubgraph 获取子图
// GET /api/v1/graph/subgraph/:id?depth=2
func (s *Server) handleGetSubgraph(c *gin.Context) {
	entityID := c.Param("id")
	depthStr := c.DefaultQuery("depth", "2")
	depth, _ := strconv.Atoi(depthStr)
	if depth <= 0 {
		depth = 2
	}

	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	subgraph, err := s.store.GetSubgraph(c.Request.Context(), entityID, depth)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	nodeInfos := make([]EntityInfo, len(subgraph.Nodes))
	for i, n := range subgraph.Nodes {
		nodeInfos[i] = EntityInfo{
			ID:        n.ID,
			Name:      n.Name,
			Type:      string(n.Type),
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
			Signature: n.Signature,
		}
	}

	edgeInfos := make([]EdgeInfo, len(subgraph.Edges))
	for i, e := range subgraph.Edges {
		edgeInfos[i] = EdgeInfo{
			Source: e.Source,
			Target: e.Target,
			Type:   string(e.Type),
		}
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: SubgraphResponse{
			CenterID: entityID,
			Depth:    depth,
			Nodes:    nodeInfos,
			Edges:    edgeInfos,
		},
	})
}

// handleFindPath 查找路径
// GET /api/v1/graph/path?from=xxx&to=yyy
func (s *Server) handleFindPath(c *gin.Context) {
	sourceID := c.Query("from")
	targetID := c.Query("to")

	if sourceID == "" || targetID == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "参数 from 和 to 不能为空",
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

	result, err := s.store.FindPath(c.Request.Context(), sourceID, targetID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	if result == nil {
		c.JSON(http.StatusNotFound, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("没有找到从 %s 到 %s 的路径", sourceID, targetID),
		})
		return
	}

	edgeInfos := make([]EdgeInfo, len(result.Edges))
	for i, e := range result.Edges {
		edgeInfos[i] = EdgeInfo{
			Source: e.Source,
			Target: e.Target,
			Type:   string(e.Type),
		}
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: PathResponse{
			Source: sourceID,
			Target: targetID,
			Path:   result.Path,
			Edges:  edgeInfos,
		},
	})
}

// handleDetectCycles 检测循环依赖
// GET /api/v1/graph/cycles
func (s *Server) handleDetectCycles(c *gin.Context) {
	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	cycles, err := s.store.DetectCycles(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: gin.H{
			"cycles":      cycles,
			"cycle_count": len(cycles),
		},
	})
}

// handleTopologicalSort 获取拓扑排序
// GET /api/v1/graph/topo-sort
func (s *Server) handleTopologicalSort(c *gin.Context) {
	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	sorted, err := s.store.TopologicalSort(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: gin.H{
			"sorted":     sorted,
			"node_count": len(sorted),
		},
	})
}

// handleHybridSearch 混合搜索（向量 + 关键词重排序）
// POST /api/v1/search/hybrid
func (s *Server) handleHybridSearch(c *gin.Context) {
	var req HybridSearchRequest
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

	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}

	results, err := s.store.HybridSearch(c.Request.Context(), req.Query, topK, req.Filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	searchResults := make([]SearchResult, len(results))
	for i, r := range results {
		searchResults[i] = SearchResult{
			EntityID: r.EntityID,
			Score:    r.Score,
			Metadata: r.Metadata,
		}
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

// handleContextSearch 上下文感知搜索
// POST /api/v1/search/context
func (s *Server) handleContextSearch(c *gin.Context) {
	var req HybridSearchRequest
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

	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}

	results, err := s.store.ContextSearch(c.Request.Context(), req.Query, topK)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	searchResults := make([]SearchResult, len(results))
	for i, r := range results {
		searchResults[i] = SearchResult{
			EntityID: r.EntityID,
			Score:    r.Score,
			Metadata: r.Metadata,
		}
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

// handleGetCallChain 获取紧凑调用链（token 最优格式）
// GET /api/v1/callchain/:id?depth=2
//
// 返回紧凑文本格式的调用链，比 JSON 节省 95% 的 token
// depth=1 时输出一行式摘要，depth>1 时输出树形展开
func (s *Server) handleGetCallChain(c *gin.Context) {
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

	text, err := s.store.CallChainCompact(c.Request.Context(), entityID, depth)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: CallChainResponse{
			EntityID: entityID,
			Depth:    depth,
			Text:     text,
		},
	})
}

// handleGetGraphSummary 获取完整调用图的紧凑摘要
// GET /api/v1/graph/summary?file=xxx
//
// 返回按文件分组的邻接表格式调用图，一次请求了解全部调用关系
// 100 个函数约 1000 tokens（JSON 需要 10000+）
func (s *Server) handleGetGraphSummary(c *gin.Context) {
	if s.store == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "知识库未初始化",
		})
		return
	}

	fileFilter := c.Query("file")

	text, err := s.store.CallGraphSummary(c.Request.Context(), fileFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	stats, _ := s.store.Stats(c.Request.Context())
	nodeCount := 0
	edgeCount := 0
	if stats != nil {
		nodeCount = int(stats.NodeCount)
		edgeCount = int(stats.EdgeCount)
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: GraphSummaryResponse{
			Text:      text,
			NodeCount: nodeCount,
			EdgeCount: edgeCount,
		},
	})
}

// handleRAGContext 获取 RAG 上下文
// POST /api/v1/rag/context
func (s *Server) handleRAGContext(c *gin.Context) {
	var req RAGContextRequest
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

	ragReq := store.RAGRequest{
		Query:            req.Query,
		TopK:             req.TopK,
		MinScore:         req.MinScore,
		IncludeCallGraph: req.IncludeCallGraph,
		CallGraphDepth:   req.CallGraphDepth,
		IncludeFileContext: req.IncludeFileCtx,
		EnableReranking:  req.EnableReranking,
		Filters:          req.Filters,
		MaxContextLength: req.MaxContextLength,
	}

	result, err := s.store.RAGPipeline(c.Request.Context(), ragReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data:    result,
	})
}

// MultiRepoRAGRequest 跨仓库 RAG 请求
type MultiRepoRAGRequest struct {
	Query            string   `json:"query" binding:"required"`
	TopK             int      `json:"top_k,omitempty"`
	RepoKeys         []string `json:"repo_keys,omitempty"`
	IncludeCallGraph bool     `json:"include_call_graph,omitempty"`
	CallGraphDepth   int      `json:"call_graph_depth,omitempty"`
	EnableReranking  bool     `json:"enable_reranking,omitempty"`
	MaxContextLength int      `json:"max_context_length,omitempty"`
}

// handleMultiRepoRAG 跨仓库 RAG 上下文组装
// POST /api/v1/rag/multi
func (s *Server) handleMultiRepoRAG(c *gin.Context) {
	var req MultiRepoRAGRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "无效的请求参数: " + err.Error(),
		})
		return
	}

	if s.registry == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "多仓库模式未启用",
		})
		return
	}

	ragReq := store.RAGRequest{
		Query:            req.Query,
		TopK:             req.TopK,
		IncludeCallGraph: req.IncludeCallGraph,
		CallGraphDepth:   req.CallGraphDepth,
		EnableReranking:  req.EnableReranking,
		MaxContextLength: req.MaxContextLength,
	}

	result, err := s.registry.RAGAll(c.Request.Context(), ragReq, req.RepoKeys)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data:    result,
	})
}

// ========== 历史分析处理器 ==========

// CommitInfoResponse 提交信息响应
type CommitInfoResponse struct {
	Hash      string                `json:"hash"`
	ShortHash string                `json:"short_hash"`
	Author    string                `json:"author"`
	Email     string                `json:"email"`
	Message   string                `json:"message"`
	Timestamp time.Time             `json:"timestamp"`
	Files     []FileChangeResponse  `json:"files,omitempty"`
}

// FileChangeResponse 文件变更响应
type FileChangeResponse struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// FileHistoryResponse 文件历史响应
type FileHistoryResponse struct {
	Path    string               `json:"path"`
	Entries []FileHistoryItem    `json:"entries"`
}

// FileHistoryItem 文件历史条目
type FileHistoryItem struct {
	Commit CommitInfoResponse `json:"commit"`
	Change FileChangeResponse `json:"change"`
}

// BlameResponse Blame 响应
type BlameResponse struct {
	Path  string             `json:"path"`
	Lines []BlameLineResponse `json:"lines"`
}

// BlameLineResponse Blame 行信息响应
type BlameLineResponse struct {
	LineNumber int       `json:"line_number"`
	Hash       string    `json:"hash"`
	Author     string    `json:"author"`
	Timestamp  time.Time `json:"timestamp"`
	Content    string    `json:"content"`
}

// requireGitRepo 检查 Git 仓库是否可用的辅助方法
func (s *Server) requireGitRepo(c *gin.Context) bool {
	if s.getGitRepo(c) == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "Git 仓库未加载，历史分析功能不可用。请先运行 store 命令索引一个仓库",
		})
		return false
	}
	return true
}

// convertCommitInfo 转换 CommitInfo 到响应格式
func convertCommitInfo(info *repogit.CommitInfo) CommitInfoResponse {
	resp := CommitInfoResponse{
		Hash:      info.Hash,
		ShortHash: info.ShortHash,
		Author:    info.Author,
		Email:     info.Email,
		Message:   info.Message,
		Timestamp: info.Timestamp,
	}
	for _, f := range info.Files {
		resp.Files = append(resp.Files, FileChangeResponse{
			Path:      f.Path,
			OldPath:   f.OldPath,
			Status:    f.Status,
			Additions: f.Additions,
			Deletions: f.Deletions,
		})
	}
	return resp
}

// handleGetCommits 获取提交历史
// GET /api/v1/history/commits?limit=20&author=xxx&since=xxx&until=xxx&keyword=xxx&file=xxx
func (s *Server) handleGetCommits(c *gin.Context) {
	if !s.requireGitRepo(c) {
		return
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 20
	}

	opts := repogit.LogOptions{
		MaxCount: limit,
		Author:   c.Query("author"),
		Keyword:  c.Query("keyword"),
		FilePath: c.Query("file"),
	}

	if since := c.Query("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			opts.Since = t
		} else if t, err := time.Parse("2006-01-02", since); err == nil {
			opts.Since = t
		}
	}
	if until := c.Query("until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			opts.Until = t
		} else if t, err := time.Parse("2006-01-02", until); err == nil {
			opts.Until = t
		}
	}

	commits, err := s.getGitRepo(c).Log(c.Request.Context(), opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("查询提交历史失败: %v", err),
		})
		return
	}

	results := make([]CommitInfoResponse, len(commits))
	for i, ci := range commits {
		results[i] = convertCommitInfo(&ci)
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: gin.H{
			"commits": results,
			"count":   len(results),
		},
	})
}

// handleGetCommitDetail 获取提交详情
// GET /api/v1/history/commit/:hash
func (s *Server) handleGetCommitDetail(c *gin.Context) {
	if !s.requireGitRepo(c) {
		return
	}

	hash := c.Param("hash")
	if hash == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "提交哈希不能为空",
		})
		return
	}

	detail, err := s.getGitRepo(c).CommitDetail(hash)
	if err != nil {
		c.JSON(http.StatusNotFound, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("获取提交详情失败: %v", err),
		})
		return
	}

	resp := convertCommitInfo(detail)

	// 计算统计信息
	totalAdditions, totalDeletions := 0, 0
	for _, f := range detail.Files {
		totalAdditions += f.Additions
		totalDeletions += f.Deletions
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: gin.H{
			"commit": resp,
			"stats": gin.H{
				"files_changed":   len(detail.Files),
				"total_additions": totalAdditions,
				"total_deletions": totalDeletions,
			},
		},
	})
}

// handleGetFileHistory 获取文件变更历史
// GET /api/v1/history/file?path=xxx&limit=20
func (s *Server) handleGetFileHistory(c *gin.Context) {
	if !s.requireGitRepo(c) {
		return
	}

	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "参数 path 不能为空",
		})
		return
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 20
	}

	entries, err := s.getGitRepo(c).FileHistory(c.Request.Context(), filePath, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("查询文件历史失败: %v", err),
		})
		return
	}

	items := make([]FileHistoryItem, len(entries))
	for i, e := range entries {
		items[i] = FileHistoryItem{
			Commit: convertCommitInfo(&e.Commit),
			Change: FileChangeResponse{
				Path:      e.Change.Path,
				OldPath:   e.Change.OldPath,
				Status:    e.Change.Status,
				Additions: e.Change.Additions,
				Deletions: e.Change.Deletions,
			},
		}
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: FileHistoryResponse{
			Path:    filePath,
			Entries: items,
		},
	})
}

// handleGetBlame 获取文件 Blame 信息
// GET /api/v1/history/blame?path=xxx
func (s *Server) handleGetBlame(c *gin.Context) {
	if !s.requireGitRepo(c) {
		return
	}

	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "参数 path 不能为空",
		})
		return
	}

	result, err := s.getGitRepo(c).Blame(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("Blame 失败: %v", err),
		})
		return
	}

	lines := make([]BlameLineResponse, len(result.Lines))
	for i, l := range result.Lines {
		lines[i] = BlameLineResponse{
			LineNumber: l.LineNumber,
			Hash:       l.Hash,
			Author:     l.Author,
			Timestamp:  l.Timestamp,
			Content:    l.Content,
		}
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: BlameResponse{
			Path:  filePath,
			Lines: lines,
		},
	})
}

// handleGetEntityHistory 获取实体的变更历史
// GET /api/v1/history/entity?id=xxx&limit=10
// 结合图存储中的实体信息（文件路径+行号）和 Git 历史
func (s *Server) handleGetEntityHistory(c *gin.Context) {
	if !s.requireGitRepo(c) {
		return
	}

	entityID := c.Query("id")
	if entityID == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "参数 id 不能为空",
		})
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	// 1. 从图存储获取实体信息
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
			Error:   fmt.Sprintf("实体不存在: %v", err),
		})
		return
	}

	// 2. 根据实体所在文件查询变更历史
	commits, err := s.getGitRepo(c).CommitsByEntity(c.Request.Context(), node.FilePath, node.StartLine, node.EndLine, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("查询实体历史失败: %v", err),
		})
		return
	}

	commitResponses := make([]CommitInfoResponse, len(commits))
	for i, ci := range commits {
		commitResponses[i] = convertCommitInfo(&ci)
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: gin.H{
			"entity": EntityInfo{
				ID:        node.ID,
				Name:      node.Name,
				Type:      string(node.Type),
				FilePath:  node.FilePath,
				StartLine: node.StartLine,
				EndLine:   node.EndLine,
				Signature: node.Signature,
			},
			"commits": commitResponses,
			"count":   len(commitResponses),
		},
	})
}

// ========== 代码读取工具 ==========

// GrepRequest grep 搜索请求
type GrepRequest struct {
	Pattern     string `json:"pattern" binding:"required"`    // 搜索模式（正则表达式）
	FilePattern string `json:"file_pattern,omitempty"`        // 文件 glob 过滤（如 "*.go"）
	MaxResults  int    `json:"max_results,omitempty"`         // 最大结果数
}

// GrepMatch grep 匹配结果
type GrepMatch struct {
	FilePath   string `json:"file_path"`
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
}

// handleGrepCode 在仓库中搜索代码
// POST /api/v1/grep
func (s *Server) handleGrepCode(c *gin.Context) {
	var req GrepRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "无效的请求参数: " + err.Error(),
		})
		return
	}

	if s.getRepoPath(c) == "" {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "仓库路径未配置",
		})
		return
	}

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}

	// 使用 grep -rn 进行搜索
	args := []string{"-rn", "--color=never", "-E"}

	// 文件模式过滤
	if req.FilePattern != "" {
		args = append(args, "--include="+req.FilePattern)
	}

	// 排除隐藏目录和常见非代码目录
	args = append(args, "--exclude-dir=.git", "--exclude-dir=node_modules", "--exclude-dir=vendor", "--exclude-dir=.sourcelex_cache*")

	args = append(args, req.Pattern, ".")

	cmd := exec.CommandContext(c.Request.Context(), "grep", args...)
	cmd.Dir = s.getRepoPath(c)

	output, err := cmd.Output()
	if err != nil {
		// grep 返回 1 表示没有匹配，不是错误
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			c.JSON(http.StatusOK, APIResponse{
				Success: true,
				Data: gin.H{
					"matches": []GrepMatch{},
					"count":   0,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("grep 执行失败: %v", err),
		})
		return
	}

	// 解析 grep 输出
	var matches []GrepMatch
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		if len(matches) >= maxResults {
			break
		}

		// 格式: ./file:line:content
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}

		filePath := strings.TrimPrefix(parts[0], "./")
		lineNum, _ := strconv.Atoi(parts[1])

		matches = append(matches, GrepMatch{
			FilePath:   filePath,
			LineNumber: lineNum,
			Content:    parts[2],
		})
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: gin.H{
			"matches": matches,
			"count":   len(matches),
			"pattern": req.Pattern,
		},
	})
}

// handleReadFileLines 读取文件指定行范围
// GET /api/v1/file/lines?path=xxx&start=1&end=50
func (s *Server) handleReadFileLines(c *gin.Context) {
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "参数 path 不能为空",
		})
		return
	}

	if s.getRepoPath(c) == "" {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "仓库路径未配置",
		})
		return
	}

	startLine, _ := strconv.Atoi(c.DefaultQuery("start", "1"))
	endLine, _ := strconv.Atoi(c.DefaultQuery("end", "0"))
	if startLine < 1 {
		startLine = 1
	}

	// 安全检查：防止路径遍历攻击
	absPath := filepath.Join(s.getRepoPath(c), filePath)
	if !strings.HasPrefix(absPath, s.getRepoPath(c)) {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "无效的文件路径",
		})
		return
	}

	file, err := os.Open(absPath)
	if err != nil {
		c.JSON(http.StatusNotFound, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("打开文件失败: %v", err),
		})
		return
	}
	defer file.Close()

	// 逐行读取
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var lines []string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if endLine > 0 && lineNum > endLine {
			break
		}
		if lineNum >= startLine {
			lines = append(lines, scanner.Text())
		}
	}

	if err := scanner.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("读取文件失败: %v", err),
		})
		return
	}

	actualEnd := startLine + len(lines) - 1
	if len(lines) == 0 {
		actualEnd = startLine
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: gin.H{
			"path":       filePath,
			"start_line": startLine,
			"end_line":   actualEnd,
			"total_lines": lineNum,
			"content":    strings.Join(lines, "\n"),
			"lines":      lines,
		},
	})
}

// FileTreeNode 文件树节点
type FileTreeNode struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	IsDir    bool            `json:"is_dir"`
	Children []*FileTreeNode `json:"children,omitempty"`
}

// handleFileTree 获取仓库文件目录树
// GET /api/v1/file/tree
func (s *Server) handleFileTree(c *gin.Context) {
	if s.getRepoPath(c) == "" {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "仓库路径未配置",
		})
		return
	}

	root := &FileTreeNode{
		Name:  filepath.Base(s.getRepoPath(c)),
		Path:  "",
		IsDir: true,
	}

	_ = filepath.WalkDir(s.getRepoPath(c), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
		}

		relPath, _ := filepath.Rel(s.getRepoPath(c), path)
		if relPath == "." {
			return nil
		}

		parts := strings.Split(relPath, string(filepath.Separator))
		current := root
		for i, part := range parts {
			isLast := i == len(parts)-1
			found := false
			for _, child := range current.Children {
				if child.Name == part {
					current = child
					found = true
					break
				}
			}
			if !found {
				node := &FileTreeNode{
					Name:  part,
					Path:  strings.Join(parts[:i+1], "/"),
					IsDir: !isLast || d.IsDir(),
				}
				current.Children = append(current.Children, node)
				current = node
			}
		}
		return nil
	})

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data:    root,
	})
}

// MultiRepoSearchRequest 跨仓库搜索请求
type MultiRepoSearchRequest struct {
	Query    string   `json:"query" binding:"required"`
	TopK     int      `json:"top_k,omitempty"`
	RepoKeys []string `json:"repo_keys,omitempty"` // 为空则搜索所有仓库
}

// MultiRepoSearchResultItem 跨仓库搜索结果项
type MultiRepoSearchResultItem struct {
	RepoKey  string         `json:"repo_key"`
	RepoID   string         `json:"repo_id"`
	Branch   string         `json:"branch"`
	Results  []SearchResult `json:"results"`
}

// handleMultiRepoSearch 跨仓库搜索
// POST /api/v1/search/multi
func (s *Server) handleMultiRepoSearch(c *gin.Context) {
	var req MultiRepoSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "无效的请求参数: " + err.Error(),
		})
		return
	}

	if s.registry == nil {
		c.JSON(http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "多仓库模式未启用",
		})
		return
	}

	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}

	multiResults, err := s.registry.SearchAll(c.Request.Context(), req.Query, topK, req.RepoKeys)
	if err != nil {
		c.JSON(http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	items := make([]MultiRepoSearchResultItem, 0, len(multiResults))
	totalCount := 0
	for _, mr := range multiResults {
		searchResults := make([]SearchResult, len(mr.Results))
		for i, r := range mr.Results {
			searchResults[i] = SearchResult{
				EntityID: r.EntityID,
				Score:    r.Score,
				Metadata: r.Metadata,
			}
			if name, ok := r.Metadata["name"].(string); ok {
				searchResults[i].Name = name
			}
			if t, ok := r.Metadata["type"].(string); ok {
				searchResults[i].Type = t
			}
			if fp, ok := r.Metadata["file_path"].(string); ok {
				searchResults[i].FilePath = fp
			}
			totalCount++
		}
		items = append(items, MultiRepoSearchResultItem{
			RepoKey: mr.RepoKey,
			RepoID:  mr.RepoID,
			Branch:  mr.Branch,
			Results: searchResults,
		})
	}

	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Data: gin.H{
			"repos":       items,
			"repo_count":  len(items),
			"total_count": totalCount,
		},
	})
}
