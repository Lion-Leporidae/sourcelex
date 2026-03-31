package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Lion-Leporidae/sourcelex/internal/analyzer"
	"github.com/Lion-Leporidae/sourcelex/internal/git"
	"github.com/Lion-Leporidae/sourcelex/internal/monitor"
	"github.com/Lion-Leporidae/sourcelex/internal/store"
	"github.com/Lion-Leporidae/sourcelex/internal/store/graph"
	"github.com/Lion-Leporidae/sourcelex/internal/store/vector"
)

// RepoMetadata 仓库元数据，持久化到数据目录供 serve 命令使用
type RepoMetadata struct {
	RepoID       string                   `json:"repo_id"`
	RepoPath     string                   `json:"repo_path"`
	RepoURL      string                   `json:"repo_url,omitempty"`
	Branch       string                   `json:"branch,omitempty"`
	IndexedAt    time.Time                `json:"indexed_at"`
	APIEndpoints []APIEndpointMeta        `json:"api_endpoints,omitempty"`
}

// APIEndpointMeta API 端点元数据（持久化格式）
type APIEndpointMeta struct {
	Method    string `json:"method"`
	Path      string `json:"path"`
	HandlerID string `json:"handler_id,omitempty"`
	Framework string `json:"framework,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
}

// deriveRepoID 从仓库 URL 或路径派生一个短 ID 用作子目录名
// https://github.com/gin-gonic/gin.git → gin
// /Users/foo/myproject → myproject
func deriveRepoID(repoURL, repoPath string) string {
	name := ""
	if repoURL != "" {
		name = filepath.Base(repoURL)
		name = strings.TrimSuffix(name, ".git")
	} else if repoPath != "" {
		name = filepath.Base(repoPath)
	}
	// 清理非法字符
	re := regexp.MustCompile(`[^a-zA-Z0-9_\-.]`)
	name = re.ReplaceAllString(name, "_")
	if name == "" || name == "." || name == ".." {
		name = "default"
	}
	return name
}

var (
	repoURL      string
	repoBranch   string
	repoPath     string
	forceRebuild bool
)

// storeCmd represents the store command
var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "存储代码仓库到知识库",
	Long: `将 Git 仓库的代码分析并存储到知识库中。

支持:
  • 远程仓库 URL (HTTPS/SSH)
  • 本地仓库路径
  • 指定分支/标签/提交

示例:
  sourcelex store --repo https://github.com/user/repo
  sourcelex store --path ./local-repo --branch main`,
	RunE: runStore,
}

func init() {
	storeCmd.Flags().StringVar(&repoURL, "repo", "", "Git 仓库 URL")
	storeCmd.Flags().StringVar(&repoPath, "path", "", "本地仓库路径")
	storeCmd.Flags().StringVar(&repoBranch, "branch", "main", "分支名称")
	storeCmd.Flags().BoolVarP(&forceRebuild, "force", "f", false, "强制重建（删除已有数据后重新构建）")
}

func runStore(cmd *cobra.Command, args []string) error {
	if repoURL == "" && repoPath == "" {
		return fmt.Errorf("必须指定 --repo 或 --path")
	}

	log := GetLogger()
	cfg := GetConfig()
	ctx := context.Background()

	var targetPath string

	if repoURL != "" {
		// 克隆远程仓库
		log.Info("准备克隆远程仓库", "url", repoURL, "branch", repoBranch)
		destPath := filepath.Join(cfg.Paths.TempDir, "repos", filepath.Base(repoURL))

		// 检查目标目录是否已存在
		if _, err := os.Stat(destPath); err == nil {
			if forceRebuild {
				log.Info("检测到已有仓库，强制模式下将删除重建", "path", destPath)
				if err := os.RemoveAll(destPath); err != nil {
					return fmt.Errorf("删除已有仓库失败: %w", err)
				}
				log.Info("已删除旧仓库")

				// 只删除该仓库的数据子目录（不影响其他仓库）
				repoDataDir := filepath.Join(cfg.Paths.DataDir, deriveRepoID(repoURL, ""))
				if err := os.RemoveAll(repoDataDir); err != nil {
					log.Warn("删除仓库数据目录失败", "path", repoDataDir, "error", err)
				} else {
					log.Info("已删除旧数据目录", "path", repoDataDir)
				}
			} else {
				// 非强制模式：询问用户
				fmt.Printf("仓库已存在于 %s\n是否删除并重新克隆? [y/N]: ", destPath)
				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" {
					log.Info("用户取消操作")
					return fmt.Errorf("操作已取消。使用 --force 或 -f 标志强制重建")
				}
				if err := os.RemoveAll(destPath); err != nil {
					return fmt.Errorf("删除已有仓库失败: %w", err)
				}
				log.Info("已删除旧仓库")
			}
		}

		repo, err := git.Clone(ctx, git.CloneOptions{
			URL:      repoURL,
			Branch:   repoBranch,
			Depth:    cfg.Git.CloneDepth,
			DestPath: destPath,
		})
		if err != nil {
			return fmt.Errorf("克隆仓库失败: %w", err)
		}
		targetPath = repo.Path()
		log.Info("仓库克隆完成", "path", targetPath)
	} else {
		// 使用本地仓库
		absPath, err := filepath.Abs(repoPath)
		if err != nil {
			return fmt.Errorf("解析路径失败: %w", err)
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return fmt.Errorf("路径不存在: %s", absPath)
		}
		targetPath = absPath
		log.Info("使用本地仓库", "path", targetPath)
	}

	// 创建分析器并构建索引
	log.Info("开始构建索引")
	codeAnalyzer := analyzer.New(targetPath, log)
	result, err := codeAnalyzer.BuildIndex(ctx)
	if err != nil {
		return fmt.Errorf("分析失败: %w", err)
	}

	// 输出分析结果
	log.Info("分析完成",
		"total_files", result.FileCount,
		"entities", result.EntityCount,
		"new_files", result.NewFiles,
		"modified_files", result.ModifiedFiles,
		"skipped_files", result.SkippedFiles,
	)

	// 显示提取的实体摘要
	funcCount, classCount, methodCount := 0, 0, 0
	for _, e := range result.Entities {
		switch e.Type {
		case "function":
			funcCount++
		case "class":
			classCount++
		case "method":
			methodCount++
		}
	}

	log.Info("实体统计",
		"functions", funcCount,
		"classes", classCount,
		"methods", methodCount,
	)

	// 显示 API 端点摘要
	if len(result.APIEndpoints) > 0 {
		log.Info("API 端点统计", "total", len(result.APIEndpoints))
		for _, ep := range result.APIEndpoints {
			log.Debug("API 端点", "method", ep.Method, "path", ep.Path, "handler", ep.HandlerID, "file", ep.File)
		}
	}

	// 初始化存储层（先触发 GC 释放分析阶段的中间数据）
	runtime.GC()
	log.Info("初始化存储层")

	// 按仓库 ID 隔离数据目录
	repoID := deriveRepoID(repoURL, repoPath)
	repoDataDir := filepath.Join(cfg.Paths.DataDir, repoID)
	if err := os.MkdirAll(repoDataDir, 0755); err != nil {
		return fmt.Errorf("创建仓库数据目录失败: %w", err)
	}
	log.Info("仓库数据目录", "repo_id", repoID, "path", repoDataDir)

	// 1. 创建 HuggingFace 嵌入器
	embedder, err := vector.NewHuggingFaceEmbedder(vector.HuggingFaceConfig{
		APIToken:  cfg.VectorStore.HuggingFace.APIToken,
		ModelID:   cfg.VectorStore.HuggingFace.ModelID,
		Dimension: cfg.VectorStore.HuggingFace.Dimension,
	})
	if err != nil {
		return fmt.Errorf("创建嵌入器失败: %w", err)
	}
	log.Info("嵌入器初始化完成", "model", cfg.VectorStore.HuggingFace.ModelID)

	// 2. 创建 chromem-go 向量存储（本地持久化，无需外部服务）
	vectorPath := filepath.Join(repoDataDir, "vectors")
	vectorStore, err := vector.NewChromemStore(vector.ChromemConfig{
		PersistPath:    vectorPath,
		CollectionName: "code_vectors",
		VectorDim:      cfg.VectorStore.HuggingFace.Dimension,
	})
	if err != nil {
		return fmt.Errorf("创建向量存储失败: %w", err)
	}
	defer vectorStore.Close()
	log.Info("向量存储初始化完成", "type", "chromem", "path", vectorPath)

	// 3. 创建 SQLite 图存储
	graphPath := filepath.Join(repoDataDir, "graph.db")
	graphStore, err := graph.NewSQLiteStore(graph.SQLiteConfig{
		DBPath: graphPath,
	})
	if err != nil {
		return fmt.Errorf("创建图存储失败: %w", err)
	}
	defer graphStore.Close()
	log.Info("图存储初始化完成", "type", "sqlite", "path", graphPath)

	// 4. 创建统一知识存储
	knowledgeStore := store.New(store.Config{
		VectorStore: vectorStore,
		GraphStore:  graphStore,
		Embedder:    embedder,
		RepoPath:    targetPath,
		Log:         log,
	})
	defer knowledgeStore.Close()

	// 5. 启动资源监控器
	resMonitor, err := monitor.New(2 * time.Second)
	if err != nil {
		log.Warn("创建资源监控器失败", "error", err)
	} else {
		resMonitor.Start(ctx)
		defer func() {
			resMonitor.Stop()
			// 打印最终统计
			if finalStats, err := resMonitor.Collect(); err == nil {
				resMonitor.PrintFinal(finalStats)
			}
		}()
	}

	// 6. 存储实体到知识库（RepoMap 模式：传入 relations 用于生成调用关系摘要）
	log.Info("开始存储实体到知识库", "count", len(result.Entities))
	if err := knowledgeStore.StoreEntities(ctx, result.Entities, result.Relations); err != nil {
		return fmt.Errorf("存储实体失败: %w", err)
	}
	log.Info("实体存储完成")
	result.Entities = nil // 释放实体内存
	runtime.GC()

	// 7. 存储调用关系到图数据库
	if len(result.Relations) > 0 {
		log.Info("开始存储调用关系", "count", len(result.Relations))
		storeRelations := make([]store.Relation, 0, len(result.Relations))
		for _, r := range result.Relations {
			storeRelations = append(storeRelations, store.Relation{
				SourceID:   r.CallerID,
				TargetID:   r.CalleeID,
				Type:       "calls",
				SourceFile: r.CallerFile,
				Line:       r.Line,
				Confidence: r.Confidence,
			})
		}
		result.Relations = nil
		if err := knowledgeStore.StoreRelations(ctx, storeRelations); err != nil {
			log.Warn("存储调用关系失败", "error", err)
		} else {
			log.Info("调用关系存储完成")
		}
	}

	// 7. 确保数据目录存在
	if err := os.MkdirAll(repoDataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 8. 输出存储统计
	stats, err := knowledgeStore.Stats(ctx)
	if err != nil {
		log.Warn("获取存储统计失败", "error", err)
	} else {
		log.Info("存储统计",
			"vectors", stats.VectorCount,
			"nodes", stats.NodeCount,
			"edges", stats.EdgeCount,
		)
	}

	// 9. 保存仓库元数据（供 serve 命令加载 git 历史）
	meta := RepoMetadata{
		RepoID:    repoID,
		RepoPath:  targetPath,
		RepoURL:   repoURL,
		Branch:    repoBranch,
		IndexedAt: time.Now(),
	}
	// 将 API 端点写入元数据
	for _, ep := range result.APIEndpoints {
		meta.APIEndpoints = append(meta.APIEndpoints, APIEndpointMeta{
			Method:    ep.Method,
			Path:      ep.Path,
			HandlerID: ep.HandlerID,
			Framework: ep.Framework,
			File:      ep.File,
			Line:      ep.Line,
		})
	}
	metaPath := filepath.Join(repoDataDir, "metadata.json")
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err == nil {
		if writeErr := os.WriteFile(metaPath, metaData, 0644); writeErr != nil {
			log.Warn("保存仓库元数据失败", "error", writeErr)
		} else {
			log.Info("仓库元数据已保存", "path", metaPath)
		}
	}

	log.Info("知识库更新完成")
	return nil
}
