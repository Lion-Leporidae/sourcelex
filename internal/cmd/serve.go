package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var (
	serveHost string
	servePort int
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动 MCP 服务器",
	Long: `启动 HTTP/SSE 服务器，提供 MCP 协议接口。

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

	addr := fmt.Sprintf("%s:%d", serveHost, servePort)
	log.Info("启动 MCP 服务器", "address", addr)

	// 简单的健康检查端点
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "ok"}`))
	})

	// SSE 端点占位
	http.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		fmt.Fprintf(w, "data: {\"message\": \"MCP SSE endpoint ready\"}\n\n")
	})

	log.Info("服务器已启动，按 Ctrl+C 停止")
	return http.ListenAndServe(addr, nil)
}
