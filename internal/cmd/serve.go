package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/repomind/repomind-go/internal/mcp"
	"github.com/repomind/repomind-go/internal/store"
	"github.com/repomind/repomind-go/internal/store/graph"
	"github.com/repomind/repomind-go/internal/store/vector"
)

var (
	serveHost string
	servePort int
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

默认监听 0.0.0.0:8000

示例:
  repomind serve
  repomind serve --port 9000`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "0.0.0.0", "监听地址")
	serveCmd.Flags().IntVar(&servePort, "port", 8000, "监听端口")
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

	// 初始化图存储（内存）
	graphStore := graph.NewMemoryStore()
	log.Info("内存图存储已初始化")

	// 创建知识存储
	// 注意: 向量存储需要 Qdrant 服务运行，这里暂时只使用图存储
	knowledgeStore := store.New(store.Config{
		GraphStore: graphStore,
		Embedder:   embedder,
	})

	// 创建 MCP 服务器
	server := mcp.New(mcp.Config{
		Host:  serveHost,
		Port:  servePort,
		Store: knowledgeStore,
		Log:   log,
	})

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
