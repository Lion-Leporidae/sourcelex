// Package web provides the embedded web UI and agent API endpoints for Sourcelex.
// Static files are embedded via Go's embed package for single-binary distribution.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Lion-Leporidae/sourcelex/internal/agent"
	"github.com/Lion-Leporidae/sourcelex/internal/logger"
	"github.com/Lion-Leporidae/sourcelex/internal/store"
	"github.com/Lion-Leporidae/sourcelex/internal/store/graph"
)

//go:embed static/*
var staticFS embed.FS

// Config holds dependencies for the web handler
type Config struct {
	Agent *agent.CodeAgent
	Store *store.KnowledgeStore
	Log   *logger.Logger
}

// Handler serves the web UI and agent API
type Handler struct {
	agent *agent.CodeAgent
	store *store.KnowledgeStore
	log   *logger.Logger
}

// NewHandler creates a new web Handler
func NewHandler(cfg Config) *Handler {
	log := cfg.Log
	if log == nil {
		log = logger.NewDefault()
	}
	return &Handler{
		agent: cfg.Agent,
		store: cfg.Store,
		log:   log,
	}
}

// SetupRoutes registers web UI and agent API routes on the given Gin engine
func (h *Handler) SetupRoutes(router *gin.Engine) {
	// Vite 构建的 SPA 静态文件
	subFS, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(subFS))

	// Agent API
	agentGroup := router.Group("/agent")
	{
		agentGroup.POST("/chat", h.handleChat)
		agentGroup.POST("/chat/stream", h.handleChatStream)
	}

	// Graph data API for visualization
	router.GET("/agent/graph/data", h.handleGraphData)
	router.GET("/agent/stats", h.handleStats)

	// SPA: 静态资源 + index.html fallback
	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// 不处理 API 路径
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/mcp/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// 尝试作为静态文件提供
		if path != "/" {
			stripped := strings.TrimPrefix(path, "/")
			if _, err := fs.Stat(subFS, stripped); err == nil {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		// SPA fallback: 返回 index.html
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			c.String(http.StatusNotFound, "index.html not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	// Root path
	router.GET("/", func(c *gin.Context) {
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			c.String(http.StatusNotFound, "index.html not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
}

// --- request / response types ---

type chatRequest struct {
	Message string              `json:"message" binding:"required"`
	History []agent.ChatMessage `json:"history,omitempty"`
}

type chatResponse struct {
	Success bool   `json:"success"`
	Answer  string `json:"answer,omitempty"`
	Error   string `json:"error,omitempty"`
}

type graphDataResponse struct {
	Nodes []nodeInfo `json:"nodes"`
	Edges []edgeInfo `json:"edges"`
}

type nodeInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Signature string `json:"signature,omitempty"`
}

type edgeInfo struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// --- handlers ---

// handleChat processes a chat request synchronously
func (h *Handler) handleChat(c *gin.Context) {
	if h.agent == nil {
		c.JSON(http.StatusServiceUnavailable, chatResponse{
			Error: "Agent 未配置。请在配置文件中设置 agent.provider 和对应的 API Key",
		})
		return
	}

	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, chatResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	answer, err := h.agent.Chat(c.Request.Context(), req.Message, req.History)
	if err != nil {
		h.log.Error("Agent 对话失败", "error", err)
		c.JSON(http.StatusInternalServerError, chatResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, chatResponse{Success: true, Answer: answer})
}

// handleChatStream processes a chat request and streams SSE events
func (h *Handler) handleChatStream(c *gin.Context) {
	if h.agent == nil {
		c.JSON(http.StatusServiceUnavailable, chatResponse{
			Error: "Agent 未配置。请在配置文件中设置 agent.provider 和对应的 API Key",
		})
		return
	}

	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, chatResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	events := h.agent.ChatStream(c.Request.Context(), req.Message, req.History)

	c.Stream(func(w io.Writer) bool {
		event, ok := <-events
		if !ok {
			return false
		}

		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(data))
		return true
	})
}

// handleGraphData returns all nodes and edges for graph visualization
func (h *Handler) handleGraphData(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "知识库未初始化"})
		return
	}

	nodes, err := h.store.GetAllNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	edges, err := h.store.GetAllEdges(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := graphDataResponse{
		Nodes: convertNodes(nodes),
		Edges: convertEdges(edges),
	}
	c.JSON(http.StatusOK, resp)
}

// handleStats returns workspace statistics
func (h *Handler) handleStats(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "知识库未初始化"})
		return
	}

	stats, err := h.store.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"vector_count": stats.VectorCount,
		"node_count":   stats.NodeCount,
		"edge_count":   stats.EdgeCount,
	})
}

func convertNodes(nodes []graph.Node) []nodeInfo {
	result := make([]nodeInfo, len(nodes))
	for i, n := range nodes {
		result[i] = nodeInfo{
			ID:        n.ID,
			Name:      n.Name,
			Type:      string(n.Type),
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
			Signature: n.Signature,
		}
	}
	return result
}

func convertEdges(edges []graph.Edge) []edgeInfo {
	result := make([]edgeInfo, len(edges))
	for i, e := range edges {
		result[i] = edgeInfo{
			Source: e.Source,
			Target: e.Target,
			Type:   string(e.Type),
		}
	}
	return result
}
