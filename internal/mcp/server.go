// Package mcp 提供 MCP (Model Context Protocol) 服务实现
// 对应架构文档: MCP服务暴露层 (FastMCP)
//
// MCP 协议是 AI 助手与代码知识库之间的通信协议
// 本包实现:
// - HTTP REST API
// - SSE (Server-Sent Events) 推送
// - MCP 工具集（语义搜索、调用链分析等）
//
// 使用 Gin 框架作为 HTTP 服务基础:
// - 高性能路由
// - 中间件支持
// - JSON 序列化
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	repogit "github.com/Lion-Leporidae/sourcelex/internal/git"
	"github.com/Lion-Leporidae/sourcelex/internal/logger"
	"github.com/Lion-Leporidae/sourcelex/internal/repo"
	"github.com/Lion-Leporidae/sourcelex/internal/store"
)

// Server MCP 服务器
type Server struct {
	router     *gin.Engine
	httpServer *http.Server

	// 多仓库支持
	registry    *repo.Registry
	userRepoMgr *repo.UserRepoManager

	// 向后兼容：单仓库模式
	store    *store.KnowledgeStore
	gitRepo  *repogit.Repository
	repoPath string

	log  *logger.Logger
	host string
	port int
}

// Config 服务器配置
type Config struct {
	Host        string
	Port        int
	Registry    *repo.Registry        // 多仓库模式
	UserRepoMgr *repo.UserRepoManager // 用户仓库绑定
	Store       *store.KnowledgeStore // 向后兼容：单仓库模式
	GitRepo     *repogit.Repository
	Log         *logger.Logger
	RepoPath    string
}

// New 创建 MCP 服务器
// 参数:
//   - cfg: 服务器配置
//
// 返回:
//   - *Server: 服务器实例
//
// 使用示例:
//
//	server := mcp.New(mcp.Config{
//	    Host:  "0.0.0.0",
//	    Port:  8000,
//	    Store: knowledgeStore,
//	    Log:   logger,
//	})
//	server.Start()
func New(cfg Config) *Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.RedirectTrailingSlash = false

	router.Use(gin.Recovery())
	router.Use(corsMiddleware())
	router.Use(loggingMiddleware(cfg.Log))

	server := &Server{
		router:      router,
		registry:    cfg.Registry,
		userRepoMgr: cfg.UserRepoMgr,
		store:       cfg.Store,
		gitRepo:     cfg.GitRepo,
		log:         cfg.Log,
		host:        cfg.Host,
		port:        cfg.Port,
		repoPath:    cfg.RepoPath,
	}

	server.setupRoutes()
	return server
}

// getStore 从请求上下文获取当前用户的活跃仓库 store
// 优先走多仓库模式（registry），回退到单 store 兼容模式
func (s *Server) getStore(c *gin.Context) *store.KnowledgeStore {
	if s.registry != nil && s.userRepoMgr != nil {
		// 从 query param 或 header 获取 session ID
		sessionID := c.GetHeader("X-Session-ID")
		if sessionID == "" {
			sessionID = c.Query("sessionId")
		}
		if sessionID == "" {
			sessionID = "default"
		}
		repoKey := s.userRepoMgr.GetActive(sessionID)
		if rc, err := s.registry.Get(repoKey); err == nil {
			defer rc.Release()
			return rc.Store
		}
	}
	return s.store
}

// getRepoPath 获取当前仓库路径
func (s *Server) getRepoPath(c *gin.Context) string {
	if s.registry != nil && s.userRepoMgr != nil {
		sessionID := c.GetHeader("X-Session-ID")
		if sessionID == "" {
			sessionID = c.Query("sessionId")
		}
		if sessionID == "" {
			sessionID = "default"
		}
		repoKey := s.userRepoMgr.GetActive(sessionID)
		if rc, err := s.registry.Get(repoKey); err == nil {
			defer rc.Release()
			return rc.RepoPath
		}
	}
	return s.repoPath
}

// getGitRepo 获取当前 Git 仓库
func (s *Server) getGitRepo(c *gin.Context) *repogit.Repository {
	if s.registry != nil && s.userRepoMgr != nil {
		sessionID := c.GetHeader("X-Session-ID")
		if sessionID == "" {
			sessionID = c.Query("sessionId")
		}
		if sessionID == "" {
			sessionID = "default"
		}
		repoKey := s.userRepoMgr.GetActive(sessionID)
		if rc, err := s.registry.Get(repoKey); err == nil {
			defer rc.Release()
			return rc.GitRepo
		}
	}
	return s.gitRepo
}

// setupRoutes 设置 API 路由
// 对应架构文档: MCP服务暴露层 - 工具集
func (s *Server) setupRoutes() {
	s.router.GET("/health", s.handleHealth)

	v1 := s.router.Group("/api/v1")
	{
		// 多仓库管理 API
		v1.GET("/repos", s.handleListRepos)
		v1.POST("/repos/active", s.handleSetActiveRepo)
		v1.GET("/repos/active", s.handleGetActiveRepo)

		v1.GET("/workspace", s.handleGetWorkspace)
		v1.POST("/search/semantic", s.handleSemanticSearch)
		v1.POST("/search/hybrid", s.handleHybridSearch)
		v1.POST("/search/context", s.handleContextSearch)
		v1.GET("/entity/:id", s.handleGetEntity)
		v1.POST("/rag/context", s.handleRAGContext)
		v1.GET("/callmap/:id", s.handleGetCallMap)
		v1.GET("/callers/:id", s.handleGetCallers)
		v1.GET("/callees/:id", s.handleGetCallees)
		v1.GET("/callchain/:id", s.handleGetCallChain)
		v1.GET("/graph/summary", s.handleGetGraphSummary)
		v1.GET("/graph/function", s.handleGetFunctionGraph)
		v1.GET("/graph/subgraph/:id", s.handleGetSubgraph)
		v1.GET("/graph/path", s.handleFindPath)
		v1.GET("/graph/cycles", s.handleDetectCycles)
		v1.GET("/graph/topo-sort", s.handleTopologicalSort)
		v1.GET("/history/commits", s.handleGetCommits)
		v1.GET("/history/commit/:hash", s.handleGetCommitDetail)
		v1.GET("/history/file", s.handleGetFileHistory)
		v1.GET("/history/blame", s.handleGetBlame)
		v1.GET("/history/entity", s.handleGetEntityHistory)
		v1.POST("/grep", s.handleGrepCode)
		v1.GET("/file/lines", s.handleReadFileLines)
		v1.GET("/file/tree", s.handleFileTree)
	}

	// MCP 协议端点
	s.router.GET("/mcp/sse", s.handleSSE)
	s.router.POST("/mcp/message", s.handleMCPMessage)
	s.router.POST("/mcp/sse", s.handleMCPMessage)
	s.router.POST("/mcp/request", s.handleMCPRequest)
}

// handleListRepos 列出所有已索引仓库
func (s *Server) handleListRepos(c *gin.Context) {
	if s.registry == nil {
		c.JSON(http.StatusOK, gin.H{"repos": []interface{}{}})
		return
	}
	repos := s.registry.List()
	type repoInfo struct {
		RepoID    string `json:"repo_id"`
		RepoURL   string `json:"repo_url,omitempty"`
		RepoPath  string `json:"repo_path"`
		Branch    string `json:"branch"`
		IndexedAt string `json:"indexed_at"`
		Key       string `json:"key"` // repoID@branch
	}
	result := make([]repoInfo, len(repos))
	for i, m := range repos {
		result[i] = repoInfo{
			RepoID:    m.RepoID,
			RepoURL:   m.RepoURL,
			RepoPath:  m.RepoPath,
			Branch:    m.Branch,
			IndexedAt: m.IndexedAt.Format("2006-01-02 15:04:05"),
			Key:       repo.RepoKey(m.RepoID, m.Branch),
		}
	}
	c.JSON(http.StatusOK, gin.H{"repos": result})
}

// handleSetActiveRepo 设置活跃仓库
func (s *Server) handleSetActiveRepo(c *gin.Context) {
	var req struct {
		RepoKey   string `json:"repo_key"`   // "repoID@branch"
		SessionID string `json:"session_id"` // 可选，默认 "default"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SessionID == "" {
		req.SessionID = c.GetHeader("X-Session-ID")
	}
	if req.SessionID == "" {
		req.SessionID = "default"
	}
	if s.userRepoMgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "多仓库模式未启用"})
		return
	}
	// 验证仓库存在
	if s.registry != nil {
		if _, err := s.registry.Get(req.RepoKey); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "仓库不存在: " + req.RepoKey})
			return
		}
	}
	s.userRepoMgr.SetActive(req.SessionID, req.RepoKey)
	s.log.Info("活跃仓库已切换", "session", req.SessionID, "repo", req.RepoKey)
	c.JSON(http.StatusOK, gin.H{"success": true, "active_repo": req.RepoKey})
}

// handleGetActiveRepo 获取当前活跃仓库
func (s *Server) handleGetActiveRepo(c *gin.Context) {
	sessionID := c.GetHeader("X-Session-ID")
	if sessionID == "" {
		sessionID = c.Query("sessionId")
	}
	if sessionID == "" {
		sessionID = "default"
	}
	if s.userRepoMgr == nil {
		c.JSON(http.StatusOK, gin.H{"active_repo": ""})
		return
	}
	c.JSON(http.StatusOK, gin.H{"active_repo": s.userRepoMgr.GetActive(sessionID)})
}

// Start 启动服务器
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.log.Info("MCP 服务器启动", "address", addr)
	return s.httpServer.ListenAndServe()
}

// Router returns the underlying Gin engine for registering additional routes
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("正在关闭 MCP 服务器...")
	return s.httpServer.Shutdown(ctx)
}

// corsMiddleware CORS 跨域中间件
// 允许 AI 助手从不同源访问 API
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// loggingMiddleware 请求日志中间件
func loggingMiddleware(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		log.Debug("HTTP 请求",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(start),
		)
	}
}
