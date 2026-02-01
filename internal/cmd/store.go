package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/repomind/repomind-go/internal/analyzer"
	"github.com/repomind/repomind-go/internal/git"
)

var (
	repoURL    string
	repoBranch string
	repoPath   string
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
  repomind store --repo https://github.com/user/repo
  repomind store --path ./local-repo --branch main`,
	RunE: runStore,
}

func init() {
	storeCmd.Flags().StringVar(&repoURL, "repo", "", "Git 仓库 URL")
	storeCmd.Flags().StringVar(&repoPath, "path", "", "本地仓库路径")
	storeCmd.Flags().StringVar(&repoBranch, "branch", "main", "分支名称")
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

	return nil
}
