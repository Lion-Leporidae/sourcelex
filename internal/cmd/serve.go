package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Lion-Leporidae/sourcelex/internal/agent"
	"github.com/Lion-Leporidae/sourcelex/internal/agent/llm"
	repogit "github.com/Lion-Leporidae/sourcelex/internal/git"
	"github.com/Lion-Leporidae/sourcelex/internal/mcp"
	"github.com/Lion-Leporidae/sourcelex/internal/store"
	"github.com/Lion-Leporidae/sourcelex/internal/store/graph"
	"github.com/Lion-Leporidae/sourcelex/internal/store/vector"
	"github.com/Lion-Leporidae/sourcelex/internal/web"
)

var (
	serveHost    string
	servePort    int
	serveRepoID  string
)

// serveCmd represents the serve command
// 启动 MCP 服务器，提供 HTTP REST API 和 SSE 推送
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动 MCP 服务器",
	Long: `启动 HTTP/SSE 服务器，提供 MCP 协议接口。

使用 Gin 框架提供高性能 HTTP 服务，支持:
- 语义搜索 API
- 调用链查询 API
- SSE 实时推送

支持多仓库：使用 --repo 指定仓库名（默认自动选择最近索引的仓库）

示例:
  sourcelex serve
  sourcelex serve --port 9000
  sourcelex serve --repo gin`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "0.0.0.0", "监听地址")
	serveCmd.Flags().IntVar(&servePort, "port", 8000, "监听端口")
	serveCmd.Flags().StringVar(&serveRepoID, "repo", "", "指定仓库名（留空自动选择最近索引的仓库）")
}

// findRepoDataDir 查找仓库数据目录
// 如果指定了 repoID 则直接查找，否则选择最近索引的仓库
func findRepoDataDir(dataDir, repoID string) (string, *RepoMetadata, error) {
	if repoID != "" {
		// 直接查找指定仓库
		dir := filepath.Join(dataDir, repoID)
		meta, err := loadRepoMetadata(dir)
		if err != nil {
			return "", nil, fmt.Errorf("仓库 '%s' 不存在或未索引: %w", repoID, err)
		}
		return dir, meta, nil
	}

	// 自动选择最近索引的仓库
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return "", nil, fmt.Errorf("读取数据目录失败: %w", err)
	}

	var bestDir string
	var bestMeta *RepoMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(dataDir, entry.Name())
		meta, err := loadRepoMetadata(dir)
		if err != nil {
			continue
		}
		if bestMeta == nil || meta.IndexedAt.After(bestMeta.IndexedAt) {
			bestDir = dir
			bestMeta = meta
		}
	}

	if bestMeta == nil {
		return "", nil, fmt.Errorf("数据目录下没有已索引的仓库，请先运行 store 命令")
	}
	return bestDir, bestMeta, nil
}

func loadRepoMetadata(dir string) (*RepoMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return nil, err
	}
	var meta RepoMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func runServe(cmd *cobra.Command, args []string) error {
	log := GetLogger()
	cfg := GetConfig()

	// 使用配置文件中的端口（如果未指定命令行参数）
	if !cmd.Flags().Changed("port") && cfg.MCP.Port > 0 {
		servePort = cfg.MCP.Port
	}
	if !cmd.Flags().Changed("host") && cfg.MCP.Host != "" {
		serveHost = cfg.MCP.Host
	}

	// 初始化嵌入器
	// 根据配置选择 HuggingFace 或其他嵌入器
	var embedder vector.Embedder
	if cfg.VectorStore.HuggingFace.APIToken != "" {
		hfEmbedder, err := vector.NewHuggingFaceEmbedder(vector.HuggingFaceConfig{
			APIToken:  cfg.VectorStore.HuggingFace.APIToken,
			ModelID:   cfg.VectorStore.HuggingFace.ModelID,
			Dimension: cfg.VectorStore.HuggingFace.Dimension,
		})
		if err != nil {
			log.Warn("HuggingFace 嵌入器初始化失败", "error", err)
		} else {
			embedder = hfEmbedder
			log.Info("HuggingFace 嵌入器已初始化", "model", cfg.VectorStore.HuggingFace.ModelID)
		}
	}

	// 查找仓库数据目录
	repoDataDir, repoMeta, err := findRepoDataDir(cfg.Paths.DataDir, serveRepoID)
	if err != nil {
		return fmt.Errorf("查找仓库数据失败: %w", err)
	}
	log.Info("使用仓库", "repo_id", repoMeta.RepoID, "path", repoMeta.RepoPath, "indexed_at", repoMeta.IndexedAt)

	// 初始化向量存储（加载已持久化的 chromem 数据）
	var vectorStore vector.Store
	vectorPath := filepath.Join(repoDataDir, "vectors")
	if _, err := os.Stat(vectorPath); err == nil {
		vs, err := vector.NewChromemStore(vector.ChromemConfig{
			PersistPath:    vectorPath,
			CollectionName: "code_vectors",
			VectorDim:      cfg.VectorStore.HuggingFace.Dimension,
		})
		if err != nil {
			log.Warn("加载向量存储失败，将使用空存储", "error", err)
		} else {
			vectorStore = vs
			log.Info("向量存储加载完成", "path", vectorPath)
		}
	} else {
		log.Warn("向量存储目录不存在，请先运行 store 命令", "path", vectorPath)
	}

	// 初始化图存储（加载已持久化的 SQLite 数据）
	var graphStore graph.Store
	graphPath := filepath.Join(repoDataDir, "graph.db")
	if _, err := os.Stat(graphPath); err == nil {
		gs, err := graph.NewSQLiteStore(graph.SQLiteConfig{
			DBPath: graphPath,
		})
		if err != nil {
			log.Warn("加载 SQLite 图存储失败，将使用内存图存储", "error", err)
			graphStore = graph.NewMemoryStore()
		} else {
			graphStore = gs
			log.Info("SQLite 图存储加载完成", "path", graphPath)
		}
	} else {
		log.Warn("图存储文件不存在，请先运行 store 命令，将使用空内存图存储", "path", graphPath)
		graphStore = graph.NewMemoryStore()
	}

	// 创建知识存储
	knowledgeStore := store.New(store.Config{
		VectorStore: vectorStore,
		GraphStore:  graphStore,
		Embedder:    embedder,
		Log:         log,
	})
	defer func() {
		log.Info("正在关闭存储连接...")
		if err := knowledgeStore.Close(); err != nil {
			log.Error("关闭存储连接失败", "error", err)
		}
	}()

	// 加载 Git 仓库（用于历史分析）
	var gitRepo *repogit.Repository
	if repoMeta.RepoPath != "" {
		if repo, err := repogit.Open(repoMeta.RepoPath); err == nil {
			gitRepo = repo
			log.Info("Git 仓库已加载（支持历史分析）", "path", repoMeta.RepoPath)
		} else {
			log.Warn("打开 Git 仓库失败，历史分析功能不可用", "path", repoMeta.RepoPath, "error", err)
		}
	}

	// 创建 MCP 服务器
	server := mcp.New(mcp.Config{
		Host:     serveHost,
		Port:     servePort,
		Store:    knowledgeStore,
		GitRepo:  gitRepo,
		Log:      log,
		RepoPath: repoMeta.RepoPath,
	})

	// 初始化 Agent（如果配置了 LLM Provider）
	var codeAgent *agent.CodeAgent
	switch cfg.Agent.Provider {
	case "openai":
		provider := llm.NewOpenAIProvider(llm.OpenAIConfig{
			APIKey:  cfg.Agent.OpenAI.APIKey,
			Model:   cfg.Agent.OpenAI.Model,
			BaseURL: cfg.Agent.OpenAI.BaseURL,
		})
		codeAgent = agent.New(agent.Config{
			Provider: provider,
			Store:    knowledgeStore,
			Log:      log,
		})
		log.Info("Agent 已初始化", "provider", "openai", "model", cfg.Agent.OpenAI.Model)
	case "anthropic":
		provider := llm.NewAnthropicProvider(llm.AnthropicConfig{
			APIKey: cfg.Agent.Anthropic.APIKey,
			Model:  cfg.Agent.Anthropic.Model,
		})
		codeAgent = agent.New(agent.Config{
			Provider: provider,
			Store:    knowledgeStore,
			Log:      log,
		})
		log.Info("Agent 已初始化", "provider", "anthropic", "model", cfg.Agent.Anthropic.Model)
	default:
		if cfg.Agent.Provider != "" {
			log.Warn("未知的 Agent Provider，Agent 功能未启用", "provider", cfg.Agent.Provider)
		} else {
			log.Info("Agent 未配置 LLM Provider，对话功能不可用，图谱和统计仍可使用")
		}
	}

	// 注册 Web UI 和 Agent API 路由
	webHandler := web.NewHandler(web.Config{
		Agent: codeAgent,
		Store: knowledgeStore,
		Log:   log,
	})
	webHandler.SetupRoutes(server.Router())
	log.Info("Web UI 已启动", "url", fmt.Sprintf("http://%s:%d", serveHost, servePort))

	// 优雅关闭处理
	// 捕获 SIGINT 和 SIGTERM 信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 在 goroutine 中启动服务器
	go func() {
		if err := server.Start(); err != nil {
			log.Error("服务器错误", "error", err)
		}
	}()

	log.Info("服务器已启动，按 Ctrl+C 停止")

	// 等待退出信号
	<-quit

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error("服务器关闭失败", "error", err)
		return err
	}

	log.Info("服务器已关闭")
	return nil
}
