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
	"github.com/Lion-Leporidae/sourcelex/internal/store"
)

// Server MCP 服务器
// 整合 HTTP 服务和知识库查询
type Server struct {
	// router Gin 路由引擎
	router *gin.Engine

	// httpServer HTTP 服务器实例
	httpServer *http.Server

	// store 知识存储
	store *store.KnowledgeStore

	// gitRepo Git 仓库（用于历史分析，可能为 nil）
	gitRepo *repogit.Repository

	// log 日志器
	log *logger.Logger

	// host 监听地址
	host string

	// port 监听端口
	port int
}

// Config 服务器配置
type Config struct {
	Host    string
	Port    int
	Store   *store.KnowledgeStore
	GitRepo *repogit.Repository
	Log     *logger.Logger
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
	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// 添加中间件
	router.Use(gin.Recovery())             // 恢复 panic
	router.Use(corsMiddleware())           // CORS 跨域
	router.Use(loggingMiddleware(cfg.Log)) // 请求日志

	server := &Server{
		router:  router,
		store:   cfg.Store,
		gitRepo: cfg.GitRepo,
		log:     cfg.Log,
		host:    cfg.Host,
		port:    cfg.Port,
	}

	// 注册路由
	server.setupRoutes()

	return server
}

// setupRoutes 设置 API 路由
// 对应架构文档: MCP服务暴露层 - 工具集
func (s *Server) setupRoutes() {
	// 健康检查
	s.router.GET("/health", s.handleHealth)

	// API v1 组
	v1 := s.router.Group("/api/v1")
	{
		// 核心工具 (Core Tools)
		v1.GET("/workspace", s.handleGetWorkspace) // 工作区信息

		// 搜索工具 (Search Tools)
		v1.POST("/search/semantic", s.handleSemanticSearch) // 语义搜索
		v1.POST("/search/hybrid", s.handleHybridSearch)     // 混合搜索
		v1.POST("/search/context", s.handleContextSearch)   // 上下文感知搜索
		v1.GET("/entity/:id", s.handleGetEntity)            // 实体查询

		// RAG 工具
		v1.POST("/rag/context", s.handleRAGContext)         // RAG 上下文组装

		// 关系工具 (Relation Tools)
		v1.GET("/callmap/:id", s.handleGetCallMap) // 调用关系（JSON 详细格式）
		v1.GET("/callers/:id", s.handleGetCallers) // 谁调用了此函数
		v1.GET("/callees/:id", s.handleGetCallees) // 此函数调用了谁

		// 紧凑调用链工具 (Compact Call Chain Tools) — AI 优先使用
		v1.GET("/callchain/:id", s.handleGetCallChain)   // 紧凑调用链（token 最优）
		v1.GET("/graph/summary", s.handleGetGraphSummary) // 全图调用摘要（token 最优）

		// 功能图谱工具 (Function Graph Tools)
		v1.GET("/graph/function", s.handleGetFunctionGraph)  // 功能图谱
		v1.GET("/graph/subgraph/:id", s.handleGetSubgraph)   // 子图
		v1.GET("/graph/path", s.handleFindPath)              // 路径查找
		v1.GET("/graph/cycles", s.handleDetectCycles)        // 循环依赖检测
		v1.GET("/graph/topo-sort", s.handleTopologicalSort)  // 拓扑排序

		// 历史分析工具 (History Analysis Tools)
		v1.GET("/history/commits", s.handleGetCommits)            // 提交历史搜索
		v1.GET("/history/commit/:hash", s.handleGetCommitDetail)  // 提交详情
		v1.GET("/history/file", s.handleGetFileHistory)            // 文件变更历史
		v1.GET("/history/blame", s.handleGetBlame)                 // 文件 Blame
		v1.GET("/history/entity", s.handleGetEntityHistory)        // 实体变更历史
	}

	// MCP 协议端点
	s.router.GET("/mcp/sse", s.handleSSE)
	s.router.POST("/mcp/request", s.handleMCPRequest)
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
