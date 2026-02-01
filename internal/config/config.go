// Package config provides configuration loading and management using Viper.
// It supports YAML config files, environment variables, and sensible defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config represents the application configuration structure.
// This maps directly to the config.yaml structure defined in the architecture doc.
type Config struct {
	Paths       PathsConfig       `mapstructure:"paths"`
	Git         GitConfig         `mapstructure:"git"`
	VectorStore VectorStoreConfig `mapstructure:"vector_store"`
	GraphStore  GraphStoreConfig  `mapstructure:"graph_store"`
	MCP         MCPConfig         `mapstructure:"mcp"`
	Logging     LoggingConfig     `mapstructure:"logging"`
}

// PathsConfig defines data and temp directories
type PathsConfig struct {
	DataDir string `mapstructure:"data_dir"`
	TempDir string `mapstructure:"temp_dir"`
}

// GitConfig defines Git-related settings
type GitConfig struct {
	CloneDepth  int               `mapstructure:"clone_depth"`
	Credentials map[string]string `mapstructure:"credentials"`
}

// VectorStoreConfig defines vector storage settings
type VectorStoreConfig struct {
	Type         string            `mapstructure:"type"`          // faiss, qdrant, milvus
	EmbedderType string            `mapstructure:"embedder_type"` // huggingface, openai
	ChunkSize    int               `mapstructure:"chunk_size"`
	ChunkOverlap int               `mapstructure:"chunk_overlap"`
	HuggingFace  HuggingFaceConfig `mapstructure:"huggingface"`
}

// HuggingFaceConfig defines HuggingFace API settings
type HuggingFaceConfig struct {
	APIToken  string `mapstructure:"api_token"` // HuggingFace API token
	ModelID   string `mapstructure:"model_id"`  // Model ID for embeddings
	Dimension int    `mapstructure:"dimension"` // Vector dimension
}

// GraphStoreConfig defines graph storage settings
type GraphStoreConfig struct {
	Type string `mapstructure:"type"` // networkx, cayley, dgraph
}

// MCPConfig defines MCP server settings
type MCPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// LoggingConfig defines logging settings
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // json, text
}

// Load reads configuration from the specified file or default locations.
// It also applies environment variable overrides with REPOMIND_ prefix.
func Load(cfgFile string) (*Config, error) {
	// 设置默认值
	setDefaults()

	// 环境变量前缀
	viper.SetEnvPrefix("REPOMIND")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if cfgFile != "" {
		// 使用指定的配置文件
		viper.SetConfigFile(cfgFile)
	} else {
		// 搜索默认位置
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath("./configs")
		viper.AddConfigPath(".")

		// 用户主目录
		if home, err := os.UserHomeDir(); err == nil {
			viper.AddConfigPath(filepath.Join(home, ".repomind"))
		}
	}

	// 读取配置文件（不存在则使用默认值）
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
		// 配置文件不存在，使用默认值
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 确保目录存在
	if err := ensureDirectories(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// setDefaults sets sensible default values for all configuration options
func setDefaults() {
	// Paths
	viper.SetDefault("paths.data_dir", "./data")
	viper.SetDefault("paths.temp_dir", "./temp")

	// Git
	viper.SetDefault("git.clone_depth", 1)

	// Vector Store
	viper.SetDefault("vector_store.type", "qdrant")
	viper.SetDefault("vector_store.embedder_type", "huggingface")
	viper.SetDefault("vector_store.chunk_size", 1024)
	viper.SetDefault("vector_store.chunk_overlap", 200)

	// Graph Store
	viper.SetDefault("graph_store.type", "memory")

	// MCP
	viper.SetDefault("mcp.host", "0.0.0.0")
	viper.SetDefault("mcp.port", 8000)

	// Logging
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "text")
}

// ensureDirectories creates necessary directories if they don't exist
func ensureDirectories(cfg *Config) error {
	dirs := []string{cfg.Paths.DataDir, cfg.Paths.TempDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}
	return nil
}
