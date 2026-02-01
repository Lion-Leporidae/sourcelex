package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
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

	if repoURL != "" {
		log.Info("准备克隆远程仓库", "url", repoURL, "branch", repoBranch)
		// TODO: Phase 2 - 实现 Git 克隆
	} else {
		log.Info("使用本地仓库", "path", repoPath)
		// TODO: Phase 2 - 实现本地仓库分析
	}

	log.Info("store 命令执行完成")
	return nil
}
