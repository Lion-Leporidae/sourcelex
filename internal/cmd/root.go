// Package cmd provides the CLI command structure using Cobra.
// This follows the pattern used by major Go projects like Kubernetes and Hugo.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/repomind/repomind-go/internal/config"
	"github.com/repomind/repomind-go/internal/logger"
)

var (
	cfgFile string
	cfg     *config.Config
	log     *logger.Logger
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "repomind",
	Short: "RepoMind - 代码知识库系统",
	Long: `RepoMind 是一个代码知识库系统，提供：
  • 代码语义搜索
  • 函数调用关系分析
  • MCP 协议服务

使用示例:
  repomind store --repo https://github.com/user/repo
  repomind serve --port 8000`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initializeApp()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// 全局 flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "配置文件路径 (默认: ./configs/config.yaml)")

	// 添加子命令
	rootCmd.AddCommand(storeCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
}

// initializeApp initializes config and logger
func initializeApp() error {
	var err error

	// 加载配置
	cfg, err = config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 初始化日志
	log, err = logger.New(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}

	log.Info("RepoMind 初始化完成",
		"data_dir", cfg.Paths.DataDir,
		"log_level", cfg.Logging.Level,
	)

	return nil
}

// GetConfig returns the loaded configuration
func GetConfig() *config.Config {
	return cfg
}

// GetLogger returns the initialized logger
func GetLogger() *logger.Logger {
	return log
}
